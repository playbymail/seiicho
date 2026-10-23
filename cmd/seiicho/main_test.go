// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/playbymail/seiicho/internal/settings"
	"github.com/playbymail/seiicho/internal/store"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

func TestOpenRejectsMemoryDatabase(t *testing.T) {
	for _, env := range []string{"development", "agent", "test", "production"} {
		err := run(t.Context(), []string{"create-game", "--env", env, "--db", settings.MemoryDB, "--seed", "1"})
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
	args := []string{"create-game", "--env", "test", "--db", path, "--seed", "0x0123456789abcdef"}

	for _, tc := range []struct {
		args []string
		want string
	}{
		{args[:5], "--seed is required"},
		{append(args[:5:5], "--seed", "-1"), "must be an unsigned 64-bit integer"},
		{append(args[:5:5], "--seed", "0x1_0000_0000_0000_0000"), "must be an unsigned 64-bit integer"},
		{append(args[:5:5], "--seed", "seed"), "must be an unsigned 64-bit integer"},
	} {
		if err := run(t.Context(), tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v = %v, want %q", tc.args[5:], err, tc.want)
		}
	}
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
	g, err := db.CreateGame(t.Context(), 1, 2, now)
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

func TestGenerateCluster(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.db")
	db, err := store.Create(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateAccount(t.Context(), "admin@example.com", []byte("x"), true, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := run(t.Context(), []string{"create-game", "--env", "test", "--db", path, "--seed", "0x0123456789abcdef"}); err != nil {
		t.Fatal(err)
	}
	args := func(extra ...string) []string {
		return append([]string{"generate-cluster", "--env", "test", "--db", path}, extra...)
	}

	if err := run(t.Context(), args()); err == nil || !strings.Contains(err.Error(), "--game is required") {
		t.Errorf("without --game = %v, want --game is required", err)
	}
	if err := run(t.Context(), args("--game", "999")); !errors.Is(err, store.ErrGameNotFound) {
		t.Errorf("missing game = %v, want ErrGameNotFound", err)
	}
	if err := run(t.Context(), args("--game", "1")); err != nil {
		t.Fatalf("generate-cluster = %v", err)
	}
	if err := run(t.Context(), args("--game", "1")); !errors.Is(err, store.ErrClusterExists) {
		t.Errorf("generate-cluster again = %v, want ErrClusterExists", err)
	}
	if err := run(t.Context(), []string{"create-game", "--env", "test", "--db", path, "--seed", "0x0123456789abcdef"}); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), args("--game", "2", "--sort-stars=false", "--sort-planets=false")); err != nil {
		t.Fatalf("generate-cluster unsorted = %v", err)
	}

	// Count the rows whose ID order differs from their sorted order: none
	// for game 1, which is sorted by default, and some for game 2.
	conn, err := sqlite.OpenConn(path, sqlite.OpenReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, tc := range []struct {
		table, query string
	}{
		{"stars", `SELECT count(*) FROM (
			SELECT row_number() OVER (ORDER BY id) AS byID, row_number() OVER (ORDER BY x, y, z, seq) AS sorted
			FROM stars WHERE game_id = ?) WHERE byID != sorted;`},
		{"planets", `SELECT count(*) FROM (
			SELECT row_number() OVER (ORDER BY p.id) AS byID, row_number() OVER (ORDER BY s.x, s.y, s.z, s.seq, p.orbit) AS sorted
			FROM planets p JOIN stars s ON s.id = p.star_id WHERE s.game_id = ?) WHERE byID != sorted;`},
	} {
		for _, game := range []struct {
			id     int64
			sorted bool
		}{{1, true}, {2, false}} {
			var n int
			err := sqlitex.Execute(conn, tc.query, &sqlitex.ExecOptions{
				Args: []any{game.id},
				ResultFunc: func(stmt *sqlite.Stmt) error {
					n = stmt.ColumnInt(0)
					return nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if game.sorted != (n == 0) {
				t.Errorf("game %d has %d %s out of sorted order, want sorted %v", game.id, n, tc.table, game.sorted)
			}
		}
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
	g, err := db.CreateGame(t.Context(), 1, 2, now)
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

func TestSummarizePlanets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.db")
	db, err := store.Create(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateAccount(t.Context(), "admin@example.com", []byte("x"), true, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	flags := []string{"--env", "test", "--db", path}
	if err := run(t.Context(), append([]string{"create-game", "--seed", "0x0123456789abcdef"}, flags...)); err != nil {
		t.Fatal(err)
	}
	args := func(extra ...string) []string {
		return append(append([]string{"summarize-planets"}, flags...), extra...)
	}

	if _, err := capture(t, args("--by-planet-type")...); err == nil || !strings.Contains(err.Error(), "--game is required") {
		t.Errorf("without --game = %v, want --game is required", err)
	}
	if _, err := capture(t, args("--game", "1")...); err == nil || !strings.Contains(err.Error(), "--by-planet-type is required") {
		t.Errorf("without --by-planet-type = %v, want --by-planet-type is required", err)
	}
	if _, err := capture(t, args("--game", "2", "--by-planet-type")...); !errors.Is(err, store.ErrGameNotFound) {
		t.Errorf("missing game = %v, want ErrGameNotFound", err)
	}
	if got, err := capture(t, args("--game", "1", "--by-planet-type")...); err != nil || got != "TYPE  PLANETS  MIN  AVG  MAX\n" {
		t.Errorf("without a cluster = %q, %v; want only the header", got, err)
	}
	if got, err := capture(t, args("--game", "1", "--by-planet-type", "--json")...); err != nil ||
		got != "{\n  \"game_id\": 1,\n  \"by\": \"planet-type\",\n  \"groups\": []\n}\n" {
		t.Errorf("without a cluster, --json = %q, %v; want empty groups", got, err)
	}
	if err := run(t.Context(), append([]string{"generate-cluster", "--game", "1"}, flags...)); err != nil {
		t.Fatal(err)
	}

	// The seed is fixed, so the summary is too.
	want := `TYPE       PLANETS  MIN  AVG   MAX
rocky      360      0    12.0  23
asteroids  105      0    0.0   0
giant      256      0    7.6   14
`
	if got, err := capture(t, args("--game", "1", "--by-planet-type")...); err != nil || got != want {
		t.Errorf("summarize-planets = %v\n%s\nwant:\n%s", err, got, want)
	}

	got, err := capture(t, args("--game", "1", "--by-planet-type", "--json")...)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		GameID int64  `json:"game_id"`
		By     string `json:"by"`
		Groups []struct {
			PlanetType          string  `json:"planet_type"`
			Planets             int     `json:"planets"`
			MinHabitability     int     `json:"min_habitability"`
			AverageHabitability float64 `json:"average_habitability"`
			MaxHabitability     int     `json:"max_habitability"`
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(got), &out); err != nil {
		t.Fatal(err)
	}
	if out.GameID != 1 || out.By != "planet-type" || len(out.Groups) != 3 {
		t.Fatalf("--json = %s", got)
	}
	planets := 0
	for i, kind := range []string{"rocky", "asteroids", "giant"} {
		g := out.Groups[i]
		planets += g.Planets
		if g.PlanetType != kind || g.AverageHabitability < float64(g.MinHabitability) || g.AverageHabitability > float64(g.MaxHabitability) {
			t.Errorf("--json group %d = %+v", i, g)
		}
	}
	if planets != 721 {
		t.Errorf("--json counts %d planets, want 721", planets)
	}
}

func TestShowHistogram(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.db")
	db, err := store.Create(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateAccount(t.Context(), "admin@example.com", []byte("x"), true, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	flags := []string{"--env", "test", "--db", path}
	if err := run(t.Context(), append([]string{"create-game", "--seed", "0x0123456789abcdef"}, flags...)); err != nil {
		t.Fatal(err)
	}
	args := func(extra ...string) []string {
		return append(append([]string{"show-histogram"}, flags...), extra...)
	}

	if _, err := capture(t, args("--planets-per-star")...); err == nil || !strings.Contains(err.Error(), "--game is required") {
		t.Errorf("without --game = %v, want --game is required", err)
	}
	for _, extra := range [][]string{{"--game", "1"}, {"--game", "1", "--planets-per-star", "--planets-per-star-system"}} {
		if _, err := capture(t, args(extra...)...); err == nil || !strings.Contains(err.Error(), "exactly one") {
			t.Errorf("%v = %v, want exactly one histogram", extra, err)
		}
	}
	if _, err := capture(t, args("--game", "2", "--planets-per-star")...); !errors.Is(err, store.ErrGameNotFound) {
		t.Errorf("missing game = %v, want ErrGameNotFound", err)
	}
	if got, err := capture(t, args("--game", "1", "--planets-per-star")...); err != nil || got != "PLANETS  STARS\n" {
		t.Errorf("without a cluster = %q, %v; want only the header", got, err)
	}
	if err := run(t.Context(), append([]string{"generate-cluster", "--game", "1"}, flags...)); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		flag, unit string
		total      int
	}{
		{"--planets-per-star", "STARS", 159},
		{"--planets-per-star-system", "SYSTEMS", 100},
	} {
		got, err := capture(t, args("--game", "1", tc.flag)...)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
		if fields := strings.Fields(lines[0]); len(fields) != 2 || fields[0] != "PLANETS" || fields[1] != tc.unit {
			t.Errorf("%s header = %q", tc.flag, lines[0])
		}
		count, planets := 0, 0
		for _, line := range lines[1:] {
			if strings.HasSuffix(line, " ") {
				t.Errorf("%s line %q has trailing spaces", tc.flag, line)
			}
			fields := strings.Fields(line)
			p, _ := strconv.Atoi(fields[0])
			n, _ := strconv.Atoi(fields[1])
			bar := ""
			if len(fields) == 3 {
				bar = fields[2]
			}
			if bar != strings.Repeat("#", n) {
				t.Errorf("%s line %q has the wrong bar", tc.flag, line)
			}
			count += n
			planets += p * n
		}
		if count != tc.total || planets != 721 {
			t.Errorf("%s counts %d with %d planets, want %d with 721", tc.flag, count, planets, tc.total)
		}

		got, err = capture(t, args("--game", "1", tc.flag, "--json")...)
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			GameID    int64  `json:"game_id"`
			Histogram string `json:"histogram"`
			Bins      []struct {
				Planets, Count int
			} `json:"bins"`
		}
		if err := json.Unmarshal([]byte(got), &out); err != nil {
			t.Fatal(err)
		}
		if out.GameID != 1 || "--"+out.Histogram != tc.flag || len(out.Bins) != len(lines)-1 {
			t.Errorf("%s --json = %s", tc.flag, got)
		}
	}
}
