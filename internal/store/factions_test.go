// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package store_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/playbymail/seiicho/internal/store"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// homeWorld returns the planet with the given ID as a store.HomeWorld.
func homeWorld(t *testing.T, s *store.Store, planetID int64) (h store.HomeWorld) {
	t.Helper()
	withConn(t, s, func(conn *sqlite.Conn) {
		err := sqlitex.Execute(conn,
			`SELECT p.id, s.x, s.y, s.z, s.seq, p.orbit FROM planets p JOIN stars s ON s.id = p.star_id WHERE p.id = ?;`,
			&sqlitex.ExecOptions{
				Args: []any{planetID},
				ResultFunc: func(stmt *sqlite.Stmt) error {
					h = store.HomeWorld{PlanetID: stmt.ColumnInt64(0), X: stmt.ColumnInt(1), Y: stmt.ColumnInt(2), Z: stmt.ColumnInt(3),
						Seq: stmt.ColumnText(4), Orbit: stmt.ColumnInt(5)}
					return nil
				},
			})
		if err != nil {
			t.Fatal(err)
		}
	})
	return h
}

// factionRows returns every race and faction, to show that a failed run
// changed nothing.
func factionRows(t *testing.T, s *store.Store) string {
	t.Helper()
	var rows []string
	withConn(t, s, func(conn *sqlite.Conn) {
		for _, query := range []string{
			`SELECT id, planet_id FROM races ORDER BY id;`,
			`SELECT id, name, race_id, home_planet_id, is_empire, seat_id FROM factions ORDER BY id;`,
		} {
			err := sqlitex.Execute(conn, query, &sqlitex.ExecOptions{
				ResultFunc: func(stmt *sqlite.Stmt) error {
					row := make([]string, stmt.ColumnCount())
					for i := range row {
						row[i] = stmt.ColumnText(i)
					}
					rows = append(rows, fmt.Sprint(row))
					return nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	})
	return fmt.Sprint(rows)
}

func TestAssignSeat(t *testing.T) {
	s, err := store.OpenPrivateMemory(t.Context(), memoryName(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Unix(1_800_000_000, 0)
	if _, err := s.CreateAccount(t.Context(), "admin@example.com", []byte("x"), true, now); err != nil {
		t.Fatal(err)
	}
	game := func() store.Game {
		t.Helper()
		g, err := s.CreateGame(t.Context(), 1, 2, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := s.GenerateCluster(t.Context(), g.ID, store.ClusterOptions{SortStars: true, SortPlanets: true}); err != nil {
			t.Fatal(err)
		}
		return g
	}
	g, other := game(), game()
	// seats[i] is a player seat in g for player i.
	var seats []int64
	for i := range store.MaxSeatsPerHomeWorld + 3 {
		a, err := s.CreateAccount(t.Context(), fmt.Sprintf("player%d@example.com", i), []byte("x"), false, now)
		if err != nil {
			t.Fatal(err)
		}
		seat, err := s.AddSeat(t.Context(), g.ID, a.ID, false, now)
		if err != nil {
			t.Fatal(err)
		}
		seats = append(seats, seat)
		if i == 0 {
			if _, err := s.AddSeat(t.Context(), other.ID, a.ID, false, now); err != nil {
				t.Fatal(err)
			}
		}
	}

	// Home worlds are flagged by hand: the tests need only the flag.
	planet := func(gameID int64, n int) int64 {
		t.Helper()
		var id int64
		withConn(t, s, func(conn *sqlite.Conn) {
			err := sqlitex.Execute(conn,
				`SELECT p.id FROM planets p JOIN stars s ON s.id = p.star_id WHERE s.game_id = ? ORDER BY p.id LIMIT 1 OFFSET ?;`,
				&sqlitex.ExecOptions{
					Args: []any{gameID, n},
					ResultFunc: func(stmt *sqlite.Stmt) error {
						id = stmt.ColumnInt64(0)
						return nil
					},
				})
			if err != nil {
				t.Fatal(err)
			}
		})
		return id
	}
	flag := func(planetID int64) {
		t.Helper()
		withConn(t, s, func(conn *sqlite.Conn) {
			mustExec(t, conn, `UPDATE planets SET is_homeworld = 1, habitability = 25 WHERE id = ?;`, planetID)
		})
	}
	home1, home2, plain, elsewhere := planet(g.ID, 10), planet(g.ID, 20), planet(g.ID, 30), planet(other.ID, 10)
	flag(elsewhere)

	assign := func(seat, planetID int64, name string) (store.Empire, error) {
		t.Helper()
		return s.AssignSeat(t.Context(), g.ID, seat, planetID, name, now)
	}
	fails := func(what string, want error, seat, planetID int64) {
		t.Helper()
		before := factionRows(t, s)
		if _, err := assign(seat, planetID, ""); !errors.Is(err, want) {
			t.Errorf("AssignSeat, %s = %v, want %v", what, err, want)
		}
		if factionRows(t, s) != before {
			t.Errorf("AssignSeat, %s, changed the database", what)
		}
	}
	fails("no home world flagged", store.ErrNoOpenHomeWorld, seats[0], 0)
	flag(home1)

	// The first seat on a home world creates its race.
	e, err := assign(seats[0], 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if e.FactionID <= 0 || e.RaceID <= 0 ||
		e != (store.Empire{FactionID: e.FactionID, Name: "Unnamed Faction", SeatID: seats[0], RaceID: e.RaceID, Home: homeWorld(t, s, home1)}) {
		t.Errorf("AssignSeat = %+v, want an Unnamed Faction on planet %d", e, home1)
	}
	race1 := e.RaceID

	flag(home2)
	before := factionRows(t, s)
	_, err = assign(seats[1], 0, "")
	var open *store.OpenHomeWorldsError
	if !errors.As(err, &open) || !errors.Is(err, store.ErrOpenHomeWorlds) || !slices.Equal(open.PlanetIDs, []int64{home1, home2}) {
		t.Errorf("AssignSeat with two open home worlds = %v, want both listed", err)
	} else if want := fmt.Sprintf("game has more than one open home world: planets %d, %d", home1, home2); err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
	if factionRows(t, s) != before {
		t.Error("AssignSeat with two open home worlds changed the database")
	}

	fails("planet that is not a home world", store.ErrNotHomeWorld, seats[1], plain)
	fails("home world in another game", store.ErrNotHomeWorld, seats[1], elsewhere)
	fails("missing planet", store.ErrNotHomeWorld, seats[1], 999_999)
	fails("seat with an empire", store.ErrSeatAssigned, seats[0], home1)
	fails("gamemaster seat", store.ErrGamemasterSeat, g.SeatID, home1)
	fails("seat in another game", store.ErrSeatNotFound, other.SeatID, home1)
	fails("missing seat", store.ErrSeatNotFound, 999, home1)
	if _, err := s.AssignSeat(t.Context(), 999, seats[1], home1, "", now); !errors.Is(err, store.ErrGameNotFound) {
		t.Errorf("AssignSeat in a missing game = %v, want ErrGameNotFound", err)
	}

	// Fill home1. A deactivated seat keeps its place.
	for i := 1; i < store.MaxSeatsPerHomeWorld; i++ {
		e, err := assign(seats[i], home1, fmt.Sprintf("Empire %d", i))
		if err != nil {
			t.Fatalf("seat %d: %v", i, err)
		}
		if e.RaceID != race1 || e.Name != fmt.Sprintf("Empire %d", i) || e.Home.PlanetID != home1 {
			t.Errorf("seat %d: AssignSeat = %+v, want Empire %d of race %d on planet %d", i, e, i, race1, home1)
		}
		if i == 1 {
			withConn(t, s, func(conn *sqlite.Conn) {
				mustExec(t, conn, `UPDATE seats SET is_active = 0 WHERE id = ?;`, seats[1])
			})
		}
	}
	fails("full home world", store.ErrHomeWorldFull, seats[store.MaxSeatsPerHomeWorld], home1)

	// A full home world is not open, so home2 is the only one.
	e, err = assign(seats[store.MaxSeatsPerHomeWorld], 0, "Red Star")
	if err != nil {
		t.Fatal(err)
	}
	if e.Home.PlanetID != home2 || e.RaceID == race1 || e.Name != "Red Star" {
		t.Errorf("AssignSeat with home1 full = %+v, want Red Star of a new race on planet %d", e, home2)
	}

	withConn(t, s, func(conn *sqlite.Conn) {
		mustExec(t, conn, `UPDATE seats SET is_active = 0 WHERE id = ?;`, seats[store.MaxSeatsPerHomeWorld+1])
	})
	fails("inactive seat", store.ErrSeatInactive, seats[store.MaxSeatsPerHomeWorld+1], home2)
	withConn(t, s, func(conn *sqlite.Conn) {
		mustExec(t, conn, `UPDATE games SET is_active = 0 WHERE id = ?;`, g.ID)
	})
	fails("inactive game", store.ErrGameInactive, seats[store.MaxSeatsPerHomeWorld+2], home2)

	defer func() {
		if recover() == nil {
			t.Error("AssignSeat with a name that is not clean did not panic")
		}
	}()
	_, _ = assign(seats[store.MaxSeatsPerHomeWorld+2], home2, " Red Star")
}

func TestPromoteAndListAssignedSeats(t *testing.T) {
	s, err := store.OpenPrivateMemory(t.Context(), memoryName(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Unix(1_800_000_000, 0)
	if _, err := s.CreateAccount(t.Context(), "admin@example.com", []byte("x"), true, now); err != nil {
		t.Fatal(err)
	}
	player, err := s.CreateAccount(t.Context(), "player@example.com", []byte("x"), false, now)
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.CreateGame(t.Context(), 1, 2, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.GenerateCluster(t.Context(), g.ID, store.ClusterOptions{}); err != nil {
		t.Fatal(err)
	}
	seatID, err := s.AddSeat(t.Context(), g.ID, player.ID, false, now)
	if err != nil {
		t.Fatal(err)
	}
	var planetID int64
	withConn(t, s, func(conn *sqlite.Conn) {
		planetID = int64(count(t, conn, `SELECT min(id) FROM planets;`))
		mustExec(t, conn, `UPDATE planets SET is_homeworld = 1, habitability = 25 WHERE id = ?;`, planetID)
	})
	e, err := s.AssignSeat(t.Context(), g.ID, seatID, 0, "", now)
	if err != nil {
		t.Fatal(err)
	}

	// Gamemasters never play.
	if _, _, err := s.PromoteSeat(t.Context(), g.ID, player.ID); !errors.Is(err, store.ErrSeatAssigned) {
		t.Errorf("PromoteSeat of a seat with an empire = %v, want ErrSeatAssigned", err)
	}
	if _, gm := seat(t, s, seatID); gm {
		t.Error("a failed PromoteSeat made the seat a gamemaster seat")
	}

	seats, err := s.Seats(t.Context(), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(seats) != 2 || seats[0].EmpireID != 0 || seats[0].EmpireName != "" || seats[0].HomePlanetID != 0 ||
		seats[1].EmpireID != e.FactionID || seats[1].EmpireName != "Unnamed Faction" || seats[1].HomePlanetID != planetID {
		t.Errorf("Seats = %+v, want the gamemaster without an empire and the player with empire %d on planet %d", seats, e.FactionID, planetID)
	}
}

func TestFactionsSchema(t *testing.T) {
	s, err := store.OpenPrivateMemory(t.Context(), memoryName(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Unix(1_800_000_000, 0)
	a, err := s.CreateAccount(t.Context(), "a@example.com", []byte("x"), false, now)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := s.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Put(conn)

	g := mustExec(t, conn, `INSERT INTO games (seed1, seed2, created_at) VALUES (1, 2, ?);`, now.Unix())
	seat := mustExec(t, conn, `INSERT INTO seats (game_id, account_id, created_at) VALUES (?, ?, ?);`, g, a.ID, now.Unix())
	star := mustExec(t, conn, `INSERT INTO stars (game_id, x, y, z, seq) VALUES (?, 1, 2, 3, 'A');`, g)
	p1 := mustExec(t, conn, `INSERT INTO planets (star_id, orbit, kind) VALUES (?, 1, 'rocky');`, star)
	p2 := mustExec(t, conn, `INSERT INTO planets (star_id, orbit, kind) VALUES (?, 2, 'rocky');`, star)
	r1 := mustExec(t, conn, `INSERT INTO races (planet_id) VALUES (?);`, p1)
	r2 := mustExec(t, conn, `INSERT INTO races (planet_id) VALUES (?);`, p2)
	if err := exec(t, conn, `INSERT INTO races (planet_id) VALUES (?);`, p1); sqlite.ErrCode(err) != sqlite.ResultConstraintUnique {
		t.Errorf("second race for a planet = %v, want a unique constraint error", err)
	}

	empire := mustExec(t, conn,
		`INSERT INTO factions (race_id, home_planet_id, is_empire, seat_id, created_at) VALUES (?, ?, 1, ?, 0);`, r1, p1, seat)
	if n := count(t, conn, fmt.Sprintf(`SELECT count(*) FROM factions WHERE id = %d AND name = 'Unnamed Faction';`, empire)); n != 1 {
		t.Error("a faction inserted without a name is not named Unnamed Faction")
	}
	// A minion needs neither a race nor a home world.
	minion := mustExec(t, conn, `INSERT INTO factions (controlling_faction_id, created_at) VALUES (?, 0);`, empire)
	mustExec(t, conn, `UPDATE factions SET name = 'R&D <1> ~{}!' WHERE id = ?;`, minion)

	for _, tc := range []struct {
		name  string
		query string
		args  []any
		want  sqlite.ResultCode
	}{
		{"second empire for a seat", `INSERT INTO factions (race_id, home_planet_id, is_empire, seat_id, created_at) VALUES (?, ?, 1, ?, 0);`,
			[]any{r1, p1, seat}, sqlite.ResultConstraintUnique},
		{"race of another home world", `INSERT INTO factions (race_id, home_planet_id, seat_id, created_at) VALUES (?, ?, ?, 0);`,
			[]any{r2, p1, seat}, sqlite.ResultConstraintForeignKey},
		{"race without a home world", `INSERT INTO factions (race_id, seat_id, created_at) VALUES (?, ?, 0);`,
			[]any{r1, seat}, sqlite.ResultConstraintCheck},
		{"home world without a race", `INSERT INTO factions (home_planet_id, seat_id, created_at) VALUES (?, ?, 0);`,
			[]any{p1, seat}, sqlite.ResultConstraintCheck},
		{"both controllers", `INSERT INTO factions (seat_id, controlling_faction_id, created_at) VALUES (?, ?, 0);`,
			[]any{seat, empire}, sqlite.ResultConstraintCheck},
		{"neither controller", `INSERT INTO factions (created_at) VALUES (0);`, nil, sqlite.ResultConstraintCheck},
		{"controlling itself", `UPDATE factions SET controlling_faction_id = id WHERE id = ?;`, []any{minion}, sqlite.ResultConstraintCheck},
		{"empire controlled by a faction", `INSERT INTO factions (race_id, home_planet_id, is_empire, controlling_faction_id, created_at) VALUES (?, ?, 1, ?, 0);`,
			[]any{r1, p1, empire}, sqlite.ResultConstraintCheck},
		{"empire without a home world", `INSERT INTO factions (is_empire, seat_id, created_at) VALUES (1, ?, 0);`,
			[]any{seat}, sqlite.ResultConstraintCheck},
		{"missing seat", `INSERT INTO factions (seat_id, created_at) VALUES (?, 0);`, []any{seat + 1}, sqlite.ResultConstraintForeignKey},
		{"changed home world", `UPDATE factions SET race_id = ?, home_planet_id = ? WHERE id = ?;`, []any{r2, p2, empire}, sqlite.ResultConstraintTrigger},
		{"home world set on a minion", `UPDATE factions SET race_id = ?, home_planet_id = ? WHERE id = ?;`, []any{r1, p1, minion}, sqlite.ResultConstraintTrigger},
	} {
		if err := exec(t, conn, tc.query, tc.args...); sqlite.ErrCode(err) != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, err, tc.want)
		}
	}

	for _, name := range []string{"", " a", "a ", "a  b", `a"b`, `a\b`, "a\tb", "Café", "abcdefghijklmnopqrstuvwxyz"} {
		if err := exec(t, conn, `UPDATE factions SET name = ? WHERE id = ?;`, name, minion); sqlite.ErrCode(err) != sqlite.ResultConstraintCheck {
			t.Errorf("name %q = %v, want a check constraint error", name, err)
		}
	}
}
