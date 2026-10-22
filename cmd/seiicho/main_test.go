// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/playbymail/seiicho/internal/settings"
	"github.com/playbymail/seiicho/internal/store"
)

func TestOpenRejectsMemoryDatabase(t *testing.T) {
	for _, env := range []string{"development", "agent", "test", "production"} {
		err := run(t.Context(), []string{"create-game", "--env", env, "--db", settings.MemoryDB})
		if err == nil || !strings.Contains(err.Error(), "only seiichod accepts") {
			t.Errorf("%s = %v, want rejection", env, err)
		}
	}
}

func TestCreateGame(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.db")
	db, err := store.Create(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	args := []string{"create-game", "--env", "test", "--db", path}

	if err := run(t.Context(), args); !errors.Is(err, store.ErrNoActiveAdmin) {
		t.Fatalf("createGame without an administrator = %v, want ErrNoActiveAdmin", err)
	}

	db, err = store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateAccount(t.Context(), "admin@example.com", []byte("x"), true, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	player, err := db.CreateAccount(t.Context(), "player@example.com", []byte("x"), false, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := run(t.Context(), args); err != nil {
		t.Fatalf("createGame = %v", err)
	}

	withGM := func(id string) []string { return append(args, "--with-gamemaster", id) }
	for _, id := range []string{"0", "-1"} {
		if err := run(t.Context(), withGM(id)); err == nil || !strings.Contains(err.Error(), "must be a positive account ID") {
			t.Errorf("--with-gamemaster %s = %v, want rejection", id, err)
		}
	}
	if err := run(t.Context(), withGM("999")); !errors.Is(err, store.ErrAccountNotFound) {
		t.Errorf("--with-gamemaster for a missing account = %v, want ErrAccountNotFound", err)
	}
	if err := run(t.Context(), withGM(strconv.FormatInt(player.ID, 10))); err != nil {
		t.Fatalf("--with-gamemaster = %v", err)
	}
	got, err := capture(t, "list-games", "--env", "test", "--db", path, "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `"gamemasters": [
      "player@example.com"
    ]`) {
		t.Errorf("list-games after --with-gamemaster = %s, want player@example.com as the second game's gamemaster", got)
	}
}

func TestAddAndPromoteSeat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.db")
	db, err := store.Create(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1, 0)
	if _, err := db.CreateAccount(t.Context(), "admin@example.com", []byte("x"), true, now); err != nil {
		t.Fatal(err)
	}
	player, err := db.CreateAccount(t.Context(), "player@example.com", []byte("x"), false, now)
	if err != nil {
		t.Fatal(err)
	}
	retired, err := db.CreateAccount(t.Context(), "retired@example.com", []byte("x"), false, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetAccountActive(t.Context(), retired.Email, false); err != nil {
		t.Fatal(err)
	}
	g, err := db.CreateGame(t.Context(), now)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	args := func(cmd string, gameID, accountID int64, extra ...string) []string {
		return append([]string{cmd, "--env", "test", "--db", path,
			"--game", strconv.FormatInt(gameID, 10), "--account", strconv.FormatInt(accountID, 10)}, extra...)
	}

	for _, cmd := range []string{"add-seat", "promote-seat"} {
		if err := run(t.Context(), []string{cmd, "--env", "test", "--db", path, "--account", "1"}); err == nil || !strings.Contains(err.Error(), "--game is required") {
			t.Errorf("without --game = %v, want --game is required", err)
		}
		if err := run(t.Context(), []string{cmd, "--env", "test", "--db", path, "--game", "1"}); err == nil || !strings.Contains(err.Error(), "--account is required") {
			t.Errorf("without --account = %v, want --account is required", err)
		}
		if err := run(t.Context(), args(cmd, 999, player.ID)); !errors.Is(err, store.ErrGameNotFound) {
			t.Errorf("missing game = %v, want ErrGameNotFound", err)
		}
		if err := run(t.Context(), args(cmd, g.ID, retired.ID)); !errors.Is(err, store.ErrAccountInactive) {
			t.Errorf("inactive account = %v, want ErrAccountInactive", err)
		}
	}

	if err := run(t.Context(), args("promote-seat", g.ID, player.ID)); !errors.Is(err, store.ErrNoActiveSeat) {
		t.Errorf("promote-seat before add-seat = %v, want ErrNoActiveSeat", err)
	}
	if err := run(t.Context(), args("add-seat", g.ID, player.ID)); err != nil {
		t.Fatalf("add-seat = %v", err)
	}
	if err := run(t.Context(), args("add-seat", g.ID, player.ID, "--gamemaster")); !errors.Is(err, store.ErrSeatExists) {
		t.Errorf("add-seat again = %v, want ErrSeatExists", err)
	}
	if err := run(t.Context(), args("promote-seat", g.ID, player.ID)); err != nil {
		t.Errorf("promote-seat = %v", err)
	}
	if err := run(t.Context(), args("promote-seat", g.ID, player.ID)); err != nil {
		t.Errorf("promote-seat of a gamemaster = %v, want no-op", err)
	}
}

// capture runs the command that args select and returns what it printed.
func capture(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var b strings.Builder
	saved := stdout
	stdout = &b
	defer func() { stdout = saved }()
	err := run(t.Context(), args)
	return b.String(), err
}

func TestListAndShow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.db")
	db, err := store.Create(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 22, 15, 4, 5, 0, time.UTC)
	if _, err := db.CreateAccount(t.Context(), "admin@example.com", []byte("x"), true, now); err != nil {
		t.Fatal(err)
	}
	player, err := db.CreateAccount(t.Context(), "player@example.com", []byte("x"), false, now)
	if err != nil {
		t.Fatal(err)
	}
	g, err := db.CreateGame(t.Context(), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddSeat(t.Context(), g.ID, player.ID, false, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := db.SetAccountActive(t.Context(), player.Email, false); err != nil {
		t.Fatal(err)
	}
	db.Close()
	flags := []string{"--env", "test", "--db", path}

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"list-accounts"}, "" +
			"ID  EMAIL               ADMIN  ACTIVE\n" +
			"1   admin@example.com   yes    yes\n" +
			"2   player@example.com  no     no\n"},
		{[]string{"list-accounts", "--json"}, `[
  {
    "id": 1,
    "email": "admin@example.com",
    "is_admin": true,
    "is_active": true
  },
  {
    "id": 2,
    "email": "player@example.com",
    "is_admin": false,
    "is_active": false
  }
]
`},
		{[]string{"show-account", "--email", " Player@Example.com "}, "" +
			"ID      2\n" +
			"EMAIL   player@example.com\n" +
			"ADMIN   no\n" +
			"ACTIVE  no\n"},
		{[]string{"show-account", "--email", "player@example.com", "--json"}, `{
  "id": 2,
  "email": "player@example.com",
  "is_admin": false,
  "is_active": false
}
`},
		{[]string{"list-games"}, "" +
			"ID  ACTIVE  CREATED              SEATS  GAMEMASTERS\n" +
			"1   yes     2026-10-22 15:04:05  2      admin@example.com\n"},
		{[]string{"list-games", "--json"}, `[
  {
    "id": 1,
    "is_active": true,
    "created_at": "2026-10-22T15:04:05Z",
    "seats": 2,
    "gamemasters": [
      "admin@example.com"
    ]
  }
]
`},
		{[]string{"list-seats", "--game", "1"}, "" +
			"SEAT  ACCOUNT  EMAIL               ROLE        ACTIVE  CREATED\n" +
			"1     1        admin@example.com   gamemaster  yes     2026-10-22 15:04:05\n" +
			"2     2        player@example.com  player      yes     2026-10-22 16:04:05\n"},
		{[]string{"list-seats", "--game", "1", "--json"}, `[
  {
    "id": 1,
    "game_id": 1,
    "account_id": 1,
    "email": "admin@example.com",
    "is_gm": true,
    "is_active": true,
    "created_at": "2026-10-22T15:04:05Z"
  },
  {
    "id": 2,
    "game_id": 1,
    "account_id": 2,
    "email": "player@example.com",
    "is_gm": false,
    "is_active": true,
    "created_at": "2026-10-22T16:04:05Z"
  }
]
`},
	} {
		got, err := capture(t, append(tc.args, flags...)...)
		if err != nil {
			t.Errorf("%v = %v", tc.args, err)
		} else if got != tc.want {
			t.Errorf("%v printed\n%s\nwant\n%s", tc.args, got, tc.want)
		}
	}

	for _, tc := range []struct {
		args    []string
		wantErr error
		wantMsg string
	}{
		{[]string{"show-account", "--email", "nobody@example.com"}, store.ErrAccountNotFound, ""},
		{[]string{"show-account"}, nil, "--email is required"},
		{[]string{"list-seats", "--game", "999"}, store.ErrGameNotFound, ""},
		{[]string{"list-seats"}, nil, "--game is required"},
		{[]string{"list-accounts", "--email", "admin@example.com"}, nil, "email"},
	} {
		_, err := capture(t, append(tc.args, flags...)...)
		if tc.wantErr != nil && !errors.Is(err, tc.wantErr) || err == nil || !strings.Contains(err.Error(), tc.wantMsg) {
			t.Errorf("%v = %v, want %v %q", tc.args, err, tc.wantErr, tc.wantMsg)
		}
	}
}

func TestListEmptyJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.db")
	db, err := store.Create(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	// An empty listing is an empty array, not null, so jq filters still work.
	for _, cmd := range []string{"list-accounts", "list-games"} {
		got, err := capture(t, cmd, "--env", "test", "--db", path, "--json")
		if err != nil || got != "[]\n" {
			t.Errorf("%s --json = %q, %v; want []", cmd, got, err)
		}
	}
}
