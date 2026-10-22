// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/playbymail/seiicho/internal/store"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

func count(t *testing.T, conn *sqlite.Conn, query string) int {
	t.Helper()
	var n int
	err := sqlitex.Execute(conn, query, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			n = stmt.ColumnInt(0)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func TestCreateGame(t *testing.T) {
	s, err := store.OpenPrivateMemory(t.Context(), memoryName(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Unix(1_800_000_000, 0)
	create := func(email string, isAdmin bool) store.Account {
		t.Helper()
		a, err := s.CreateAccount(t.Context(), email, []byte("x"), isAdmin, now)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}

	// Neither a player nor an inactive administrator can be the gamemaster.
	create("player@example.com", false)
	retired := create("retired@example.com", true)
	if err := s.SetAccountActive(t.Context(), retired.Email, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccountActive(t.Context(), "nobody@example.com", false); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("SetAccountActive for a missing account = %v, want ErrNotFound", err)
	}
	if _, err := s.CreateGame(t.Context(), now); !errors.Is(err, store.ErrNoActiveAdmin) {
		t.Fatalf("CreateGame without an active administrator = %v, want ErrNoActiveAdmin", err)
	}

	senior := create("senior@example.com", true)
	create("junior@example.com", true)
	g, err := s.CreateGame(t.Context(), now)
	if err != nil {
		t.Fatal(err)
	}
	if g.GM.ID != senior.ID {
		t.Errorf("gamemaster = %s, want %s", g.GM.Email, senior.Email)
	}

	conn, err := s.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Put(conn)
	if n := count(t, conn, `SELECT count(*) FROM games;`); n != 1 {
		t.Errorf("%d games, want 1 (the failed attempt must create nothing)", n)
	}
	var gameID, accountID int64
	var active, gm bool
	err = sqlitex.Execute(conn, `SELECT game_id, account_id, is_active, is_gm FROM seats WHERE id = ?;`, &sqlitex.ExecOptions{
		Args: []any{g.SeatID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			gameID, accountID = stmt.ColumnInt64(0), stmt.ColumnInt64(1)
			active, gm = stmt.ColumnBool(2), stmt.ColumnBool(3)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gameID != g.ID || accountID != senior.ID || !active || !gm {
		t.Errorf("seat = game %d, account %d, active %v, gm %v; want game %d, account %d, active gamemaster",
			gameID, accountID, active, gm, g.ID, senior.ID)
	}
	if n := count(t, conn, `SELECT count(*) FROM seats;`); n != 1 {
		t.Errorf("%d seats, want 1", n)
	}
	if err := exec(t, conn, `UPDATE accounts SET is_active = 2 WHERE id = ?;`, senior.ID); sqlite.ErrCode(err) != sqlite.ResultConstraintCheck {
		t.Errorf("is_active = 2: %v, want a check constraint error", err)
	}
}

// withConn borrows a connection for f. In-memory stores have a single
// connection, so a test must return it before calling the store again.
func withConn(t *testing.T, s *store.Store, f func(conn *sqlite.Conn)) {
	t.Helper()
	conn, err := s.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Put(conn)
	f(conn)
}

// seat returns whether the seat is active and a gamemaster seat.
func seat(t *testing.T, s *store.Store, id int64) (active, gm bool) {
	t.Helper()
	conn, err := s.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Put(conn)
	err = sqlitex.Execute(conn, `SELECT is_active, is_gm FROM seats WHERE id = ?;`, &sqlitex.ExecOptions{
		Args: []any{id},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			active, gm = stmt.ColumnBool(0), stmt.ColumnBool(1)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return active, gm
}

func TestAddAndPromoteSeat(t *testing.T) {
	s, err := store.OpenPrivateMemory(t.Context(), memoryName(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Unix(1_800_000_000, 0)
	create := func(email string, isAdmin bool) store.Account {
		t.Helper()
		a, err := s.CreateAccount(t.Context(), email, []byte("x"), isAdmin, now)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	admin := create("admin@example.com", true)
	player := create("player@example.com", false)
	gm := create("gm@example.com", false)
	retired := create("retired@example.com", false)
	if err := s.SetAccountActive(t.Context(), retired.Email, false); err != nil {
		t.Fatal(err)
	}
	g, err := s.CreateGame(t.Context(), now)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := s.CreateGame(t.Context(), now)
	if err != nil {
		t.Fatal(err)
	}
	withConn(t, s, func(conn *sqlite.Conn) {
		mustExec(t, conn, `UPDATE games SET is_active = 0 WHERE id = ?;`, closed.ID)
	})

	invalid := []struct {
		name              string
		gameID, accountID int64
		want              error
	}{
		{"missing game", 999, player.ID, store.ErrGameNotFound},
		{"inactive game", closed.ID, player.ID, store.ErrGameInactive},
		{"missing account", g.ID, 999, store.ErrAccountNotFound},
		{"inactive account", g.ID, retired.ID, store.ErrAccountInactive},
	}
	for _, tc := range invalid {
		if _, err := s.AddSeat(t.Context(), tc.gameID, tc.accountID, false, now); !errors.Is(err, tc.want) {
			t.Errorf("AddSeat, %s = %v, want %v", tc.name, err, tc.want)
		}
	}
	withConn(t, s, func(conn *sqlite.Conn) {
		if n := count(t, conn, `SELECT count(*) FROM seats;`); n != 2 {
			t.Fatalf("%d seats, want 2 (the failed attempts must add nothing)", n)
		}
	})

	playerSeat, err := s.AddSeat(t.Context(), g.ID, player.ID, false, now)
	if err != nil {
		t.Fatal(err)
	}
	if active, isGM := seat(t, s, playerSeat); !active || isGM {
		t.Errorf("player seat: active %v, gm %v; want an active player seat", active, isGM)
	}
	gmSeat, err := s.AddSeat(t.Context(), g.ID, gm.ID, true, now)
	if err != nil {
		t.Fatal(err)
	}
	if active, isGM := seat(t, s, gmSeat); !active || !isGM {
		t.Errorf("gamemaster seat: active %v, gm %v; want an active gamemaster seat", active, isGM)
	}
	for _, id := range []int64{player.ID, gm.ID, admin.ID} {
		if _, err := s.AddSeat(t.Context(), g.ID, id, false, now); !errors.Is(err, store.ErrSeatExists) {
			t.Errorf("AddSeat, account %d again = %v, want ErrSeatExists", id, err)
		}
	}

	// PromoteSeat checks the game and account the same way.
	for _, tc := range invalid {
		if _, _, err := s.PromoteSeat(t.Context(), tc.gameID, tc.accountID); !errors.Is(err, tc.want) {
			t.Errorf("PromoteSeat, %s = %v, want %v", tc.name, err, tc.want)
		}
	}
	unseated := create("unseated@example.com", false)
	if _, _, err := s.PromoteSeat(t.Context(), g.ID, unseated.ID); !errors.Is(err, store.ErrNoActiveSeat) {
		t.Errorf("PromoteSeat without a seat = %v, want ErrNoActiveSeat", err)
	}
	benched := create("benched@example.com", false)
	benchedSeat, err := s.AddSeat(t.Context(), g.ID, benched.ID, false, now)
	if err != nil {
		t.Fatal(err)
	}
	withConn(t, s, func(conn *sqlite.Conn) {
		mustExec(t, conn, `UPDATE seats SET is_active = 0 WHERE id = ?;`, benchedSeat)
	})
	if _, _, err := s.PromoteSeat(t.Context(), g.ID, benched.ID); !errors.Is(err, store.ErrNoActiveSeat) {
		t.Errorf("PromoteSeat with an inactive seat = %v, want ErrNoActiveSeat", err)
	}
	if _, isGM := seat(t, s, benchedSeat); isGM {
		t.Error("PromoteSeat promoted an inactive seat")
	}

	id, promoted, err := s.PromoteSeat(t.Context(), g.ID, player.ID)
	if err != nil || id != playerSeat || !promoted {
		t.Errorf("PromoteSeat, player = %d, %v, %v; want %d, true, nil", id, promoted, err, playerSeat)
	}
	if _, isGM := seat(t, s, playerSeat); !isGM {
		t.Error("PromoteSeat did not make the player a gamemaster")
	}
	id, promoted, err = s.PromoteSeat(t.Context(), g.ID, gm.ID)
	if err != nil || id != gmSeat || promoted {
		t.Errorf("PromoteSeat, gamemaster = %d, %v, %v; want %d, false, nil", id, promoted, err, gmSeat)
	}
}
