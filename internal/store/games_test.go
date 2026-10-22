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
	conn, err := s.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, conn, `UPDATE accounts SET is_active = 0 WHERE id = ?;`, retired.ID)
	s.Put(conn) // a private memory store has only one connection
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

	conn, err = s.Conn(t.Context())
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
