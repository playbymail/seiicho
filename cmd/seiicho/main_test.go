// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/playbymail/seiicho/internal/cluster"
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

func TestFlagHomeWorld(t *testing.T) {
	flags := depositsDB(t)
	args := func(extra ...string) []string {
		return append(append([]string{"flag-home-world"}, flags...), extra...)
	}
	limits := []string{"--min-home-distance", "6", "--max-neighbor-distance", "6"}

	// The flags are checked before the database is opened.
	bad := []string{"--env", "test", "--db", filepath.Join(t.TempDir(), "missing.db")}
	for _, tc := range []struct {
		extra []string
		want  string
	}{
		{limits, "--game is required"},
		{[]string{"--game", "1", "--max-neighbor-distance", "6"}, "--min-home-distance is required and must be from 3 to 16"},
		{[]string{"--game", "1", "--min-home-distance", "2", "--max-neighbor-distance", "6"}, "--min-home-distance is required and must be from 3 to 16"},
		{[]string{"--game", "1", "--min-home-distance", "17", "--max-neighbor-distance", "6"}, "--min-home-distance is required and must be from 3 to 16"},
		{[]string{"--game", "1", "--min-home-distance", "6"}, "--max-neighbor-distance is required and must be from 1 to 16"},
		{[]string{"--game", "1", "--min-home-distance", "6", "--max-neighbor-distance", "0"}, "--max-neighbor-distance is required and must be from 1 to 16"},
		{[]string{"--game", "1", "--min-home-distance", "16", "--max-neighbor-distance", "17"}, "--max-neighbor-distance is required and must be from 1 to 16"},
	} {
		a := append(append([]string{"flag-home-world"}, bad...), tc.extra...)
		if _, err := capture(t, a...); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v = %v, want %q", tc.extra, err, tc.want)
		}
	}
	if _, err := capture(t, args(append([]string{"--game", "2"}, limits...)...)...); !errors.Is(err, store.ErrGameNotFound) {
		t.Errorf("missing game = %v, want ErrGameNotFound", err)
	}
	if _, err := capture(t, args(append([]string{"--game", "1"}, limits...)...)...); !errors.Is(err, store.ErrNoCluster) {
		t.Errorf("game without a cluster = %v, want ErrNoCluster", err)
	}
	if err := run(t.Context(), append([]string{"generate-cluster", "--game", "1"}, flags...)); err != nil {
		t.Fatal(err)
	}

	// The first home worlds of the seed, as cluster's TestHomeWorldPins
	// pins them.
	got, err := capture(t, args(append([]string{"--game", "1"}, limits...)...)...)
	if err != nil {
		t.Fatal(err)
	}
	if want := regexp.MustCompile(`^flagged planet \d+ as a home world in game 1: system 5 3 14, star A, orbit 4\n$`); !want.MatchString(got) {
		t.Errorf("flag-home-world = %q, want a match for %s", got, want)
	}
	got, err = capture(t, args(append([]string{"--game", "1", "--json"}, limits...)...)...)
	if err != nil {
		t.Fatal(err)
	}
	var h homeWorldJSON
	if err := json.Unmarshal([]byte(got), &h); err != nil {
		t.Fatalf("flag-home-world --json = %q: %v", got, err)
	}
	if h.PlanetID <= 0 || h != (homeWorldJSON{GameID: 1, PlanetID: h.PlanetID, X: 26, Y: 18, Z: 17, Star: "A", Orbit: 3}) {
		t.Errorf("flag-home-world --json = %+v, want the planet in orbit 3 of 26 18 17 A", h)
	}
	if !strings.Contains(got, `"star": "A"`) {
		t.Errorf("flag-home-world --json = %s, want a star field", got)
	}

	// No system has a neighbor within 1.
	got, err = capture(t, args("--game", "1", "--min-home-distance", "3", "--max-neighbor-distance", "1")...)
	if !errors.Is(err, cluster.ErrNoHomeWorld) || got != "" {
		t.Errorf("flag-home-world with no planet qualifying = %q, %v; want ErrNoHomeWorld", got, err)
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

// depositsDB returns the path of a database holding game 1, created with
// seed 0x0123456789abcdef, and the flags that select it. The game has no
// cluster.
func depositsDB(t *testing.T) []string {
	t.Helper()
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
	return flags
}

func TestSummarizeDeposits(t *testing.T) {
	flags := depositsDB(t)
	args := func(extra ...string) []string {
		return append(append([]string{"summarize-deposits"}, flags...), extra...)
	}

	if _, err := capture(t, args("--by-resource")...); err == nil || !strings.Contains(err.Error(), "--game is required") {
		t.Errorf("without --game = %v, want --game is required", err)
	}
	for _, extra := range [][]string{{"--game", "1"}, {"--game", "1", "--by-resource", "--by-planet-type"}} {
		if _, err := capture(t, args(extra...)...); err == nil || !strings.Contains(err.Error(), "exactly one") {
			t.Errorf("%v = %v, want exactly one grouping", extra, err)
		}
	}
	if _, err := capture(t, args("--game", "2", "--by-resource")...); !errors.Is(err, store.ErrGameNotFound) {
		t.Errorf("missing game = %v, want ErrGameNotFound", err)
	}
	for flag, header := range map[string]string{
		"--by-resource":         "RESOURCE  QUANTITY  SHARE  DEPOSITS  MIN/STAR  AVG/STAR  MAX/STAR\n",
		"--by-planet-type":      "TYPE  PLANETS  MIN-DEP  AVG-DEP  MAX-DEP  MIN-WEALTH  AVG-WEALTH  MAX-WEALTH  GOLD  FUEL  METS  NMTS\n",
		"--by-planets-per-star": "PLANETS  STARS  AVG/STAR  AVG/PLANET\n",
	} {
		if got, err := capture(t, args("--game", "1", flag)...); err != nil || got != header {
			t.Errorf("%s without a cluster = %q, %v; want only the header", flag, got, err)
		}
		want := "{\n  \"game_id\": 1,\n  \"by\": \"" + flag[5:] + "\",\n  \"groups\": []\n}\n"
		if got, err := capture(t, args("--game", "1", flag, "--json")...); err != nil || got != want {
			t.Errorf("%s without a cluster, --json = %q, %v; want empty groups", flag, got, err)
		}
	}
	if err := run(t.Context(), append([]string{"generate-cluster", "--game", "1"}, flags...)); err != nil {
		t.Fatal(err)
	}

	// The seed is fixed, so the summaries are too.
	for flag, want := range map[string]string{
		"--by-resource": `RESOURCE  QUANTITY      SHARE  DEPOSITS  MIN/STAR   AVG/STAR    MAX/STAR
gold      48938043097   7.6%   2111      0          307786435   925503250
fuel      160027875958  24.9%  3212      380729087  1006464629  1826649445
mets      217553241779  33.8%  3562      156140315  1368259382  2408672120
nmts      217166119166  33.7%  4010      0          1365824649  2331965621
`,
		"--by-planet-type": `TYPE       PLANETS  MIN-DEP  AVG-DEP  MAX-DEP  MIN-WEALTH  AVG-WEALTH  MAX-WEALTH  GOLD   FUEL   METS   NMTS
rocky      360      1        15.7     30       56235120    825313464   4186080000  11.5%  21.7%  32.8%  34.0%
asteroids  105      6        23.4     45       214655025   1052701257  4084080000  13.5%  25.3%  40.7%  20.5%
giant      256      1        18.7     34       19205316    922026566   4504320000  0.0%   28.6%  31.8%  39.6%
`,
		"--by-planets-per-star": `PLANETS  STARS  AVG/STAR    AVG/PLANET
1        7      3996068571  3996068571
2        18     3993866667  1996933333
3        24     4161430000  1387143333
4        32     4021605000  1005401250
5        28     4050565714  810113143
6        23     4021460870  670243478
7        19     4086012632  583716090
8        6      3967120000  495890000
9        1      4092240000  454693333
10       1      3818880000  381888000
`,
	} {
		if got, err := capture(t, args("--game", "1", flag)...); err != nil || got != want {
			t.Errorf("summarize-deposits %s = %v\n%s\nwant:\n%s", flag, err, got, want)
		}
	}

	got, err := capture(t, args("--game", "1", "--by-resource", "--json")...)
	if err != nil {
		t.Fatal(err)
	}
	var byResource struct {
		GameID int64  `json:"game_id"`
		By     string `json:"by"`
		Groups []struct {
			Resource       string  `json:"resource"`
			Quantity       int64   `json:"quantity"`
			Share          float64 `json:"share"`
			Deposits       int     `json:"deposits"`
			MinPerStar     int64   `json:"min_per_star"`
			AveragePerStar float64 `json:"average_per_star"`
			MaxPerStar     int64   `json:"max_per_star"`
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(got), &byResource); err != nil {
		t.Fatal(err)
	}
	if byResource.GameID != 1 || byResource.By != "resource" || len(byResource.Groups) != 4 {
		t.Fatalf("--by-resource --json = %s", got)
	}
	deposits, share := 0, 0.0
	for i, r := range []string{"gold", "fuel", "mets", "nmts"} {
		g := byResource.Groups[i]
		deposits += g.Deposits
		share += g.Share
		if g.Resource != r || g.AveragePerStar < float64(g.MinPerStar) || g.AveragePerStar > float64(g.MaxPerStar) {
			t.Errorf("--by-resource --json group %d = %+v", i, g)
		}
	}
	if deposits != 12895 || share < 0.999999 || share > 1.000001 {
		t.Errorf("--by-resource --json counts %d deposits with shares adding up to %v, want 12895 and 1", deposits, share)
	}

	got, err = capture(t, args("--game", "1", "--by-planet-type", "--json")...)
	if err != nil {
		t.Fatal(err)
	}
	var byType struct {
		By     string `json:"by"`
		Groups []struct {
			PlanetType      string             `json:"planet_type"`
			Planets         int                `json:"planets"`
			AverageDeposits float64            `json:"average_deposits"`
			Shares          map[string]float64 `json:"shares"`
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(got), &byType); err != nil {
		t.Fatal(err)
	}
	if byType.By != "planet-type" || len(byType.Groups) != 3 {
		t.Fatalf("--by-planet-type --json = %s", got)
	}
	planets := 0
	for i, kind := range []string{"rocky", "asteroids", "giant"} {
		g := byType.Groups[i]
		planets += g.Planets
		sum := g.Shares["gold"] + g.Shares["fuel"] + g.Shares["mets"] + g.Shares["nmts"]
		if g.PlanetType != kind || len(g.Shares) != 4 || sum < 0.999999 || sum > 1.000001 {
			t.Errorf("--by-planet-type --json group %d = %+v", i, g)
		}
	}
	if planets != 721 {
		t.Errorf("--by-planet-type --json counts %d planets, want 721", planets)
	}

	got, err = capture(t, args("--game", "1", "--by-planets-per-star", "--json")...)
	if err != nil {
		t.Fatal(err)
	}
	var bySize struct {
		By     string `json:"by"`
		Groups []struct {
			Planets          int     `json:"planets"`
			Stars            int     `json:"stars"`
			AveragePerStar   float64 `json:"average_per_star"`
			AveragePerPlanet float64 `json:"average_per_planet"`
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(got), &bySize); err != nil {
		t.Fatal(err)
	}
	stars := 0
	for _, g := range bySize.Groups {
		stars += g.Stars
		if d := g.AveragePerPlanet*float64(g.Planets) - g.AveragePerStar; d < -1 || d > 1 {
			t.Errorf("--by-planets-per-star --json group %+v", g)
		}
	}
	if bySize.By != "planets-per-star" || stars != 159 {
		t.Errorf("--by-planets-per-star --json = %s", got)
	}
}

func TestShowDepositHistograms(t *testing.T) {
	flags := depositsDB(t)
	args := func(extra ...string) []string {
		return append(append([]string{"show-histogram"}, flags...), extra...)
	}
	for _, extra := range [][]string{{"--game", "1", "--deposit-yield", "--deposits-per-planet"}, {"--game", "1", "--planets-per-star", "--deposit-quantity"}} {
		if _, err := capture(t, args(extra...)...); err == nil || !strings.Contains(err.Error(), "exactly one") {
			t.Errorf("%v = %v, want exactly one histogram", extra, err)
		}
	}
	for flag, header := range map[string]string{
		"--deposit-quantity":    "QUANTITY  DEPOSITS\n",
		"--deposit-yield":       "YIELD  DEPOSITS\n",
		"--deposits-per-planet": "DEPOSITS  PLANETS\n",
	} {
		if got, err := capture(t, args("--game", "1", flag)...); err != nil || got != header {
			t.Errorf("%s without a cluster = %q, %v; want only the header", flag, got, err)
		}
	}
	if err := run(t.Context(), append([]string{"generate-cluster", "--game", "1"}, flags...)); err != nil {
		t.Fatal(err)
	}

	// The seed is fixed, so the quantity histogram is too. The longest bar
	// has maxBar #s, and every count above 0 has at least one.
	want := `QUANTITY  DEPOSITS
2M        24        #
5M        411       ###
10M       2018      #################
20M       6037      ##################################################
50M       3195      ##########################
100M      1024      ########
200M      173       #
500M      11        #
1B        2         #
`
	if got, err := capture(t, args("--game", "1", "--deposit-quantity")...); err != nil || got != want {
		t.Errorf("--deposit-quantity = %v\n%s\nwant:\n%s", err, got, want)
	}

	for _, tc := range []struct {
		flag, label, unit string
		bins, total       int
	}{
		{"--deposit-yield", "YIELD", "DEPOSITS", 25, 12895},
		{"--deposits-per-planet", "DEPOSITS", "PLANETS", 45, 721},
	} {
		got, err := capture(t, args("--game", "1", tc.flag)...)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
		if fields := strings.Fields(lines[0]); len(fields) != 2 || fields[0] != tc.label || fields[1] != tc.unit {
			t.Errorf("%s header = %q", tc.flag, lines[0])
		}
		if len(lines) != tc.bins+1 {
			t.Fatalf("%s has %d bins, want %d", tc.flag, len(lines)-1, tc.bins)
		}
		count, longest := 0, 0
		for i, line := range lines[1:] {
			if strings.HasSuffix(line, " ") {
				t.Errorf("%s line %q has trailing spaces", tc.flag, line)
			}
			fields := strings.Fields(line)
			v, _ := strconv.Atoi(fields[0])
			n, _ := strconv.Atoi(fields[1])
			bar := ""
			if len(fields) == 3 {
				bar = fields[2]
			}
			if v != i+1 || (n == 0) != (bar == "") || strings.Trim(bar, "#") != "" {
				t.Errorf("%s line %q", tc.flag, line)
			}
			count += n
			longest = max(longest, len(bar))
		}
		if count != tc.total || longest != maxBar {
			t.Errorf("%s counts %d with a longest bar of %d, want %d and %d", tc.flag, count, longest, tc.total, maxBar)
		}
	}

	got, err := capture(t, args("--game", "1", "--deposit-quantity", "--json")...)
	if err != nil {
		t.Fatal(err)
	}
	var quantity struct {
		Histogram string `json:"histogram"`
		Bins      []struct {
			MinQuantity int64 `json:"min_quantity"`
			MaxQuantity int64 `json:"max_quantity"`
			Count       int   `json:"count"`
		} `json:"bins"`
	}
	if err := json.Unmarshal([]byte(got), &quantity); err != nil {
		t.Fatal(err)
	}
	if quantity.Histogram != "deposit-quantity" || len(quantity.Bins) != 9 ||
		quantity.Bins[0].MinQuantity != 2_000_000 || quantity.Bins[0].MaxQuantity != 4_999_999 || quantity.Bins[0].Count != 24 {
		t.Errorf("--deposit-quantity --json = %s", got)
	}
	for flag, field := range map[string]string{"--deposit-yield": "yield_pct", "--deposits-per-planet": "deposits"} {
		got, err := capture(t, args("--game", "1", flag, "--json")...)
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Histogram string           `json:"histogram"`
			Bins      []map[string]int `json:"bins"`
		}
		if err := json.Unmarshal([]byte(got), &out); err != nil {
			t.Fatal(err)
		}
		if "--"+out.Histogram != flag || len(out.Bins) == 0 || len(out.Bins[0]) != 2 || out.Bins[0][field] != 1 {
			t.Errorf("%s --json = %s", flag, got)
		}
	}
}

func TestShortQuantity(t *testing.T) {
	for q, want := range map[int64]string{
		99_999: "100K", 200_000: "200K", 999_999: "1M", 2_000_000: "2M", 50_000_000: "50M",
		1_000_000_000: "1B", 4_692_000_000: "4.69B", 999: "999",
	} {
		if got := shortQuantity(q); got != want {
			t.Errorf("shortQuantity(%d) = %q, want %q", q, got, want)
		}
	}
}
