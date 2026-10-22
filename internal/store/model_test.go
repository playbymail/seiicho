// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package store_test

import (
	"testing"
	"time"

	"github.com/playbymail/seiicho/internal/store"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

func exec(t *testing.T, conn *sqlite.Conn, query string, args ...any) error {
	t.Helper()
	return sqlitex.Execute(conn, query, &sqlitex.ExecOptions{Args: args})
}

func mustExec(t *testing.T, conn *sqlite.Conn, query string, args ...any) int64 {
	t.Helper()
	if err := exec(t, conn, query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return conn.LastInsertRowID()
}

func TestAccountIDsAreNotReused(t *testing.T) {
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
	mustExec(t, conn, `DELETE FROM accounts WHERE id = ?;`, a.ID)
	s.Put(conn) // a private memory store has only one connection
	b, err := s.CreateAccount(t.Context(), "b@example.com", []byte("x"), false, now)
	if err != nil {
		t.Fatal(err)
	}
	if b.ID == a.ID {
		t.Fatalf("account ID %d was reused", a.ID)
	}
}

func TestSeats(t *testing.T) {
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

	g1 := mustExec(t, conn, `INSERT INTO games (created_at) VALUES (?);`, now.Unix())
	g2 := mustExec(t, conn, `INSERT INTO games (created_at) VALUES (?);`, now.Unix())
	seat := mustExec(t, conn, `INSERT INTO seats (game_id, account_id, created_at) VALUES (?, ?, ?);`, g1, a.ID, now.Unix())

	var active, gm bool
	err = sqlitex.Execute(conn, `SELECT is_active, is_gm FROM seats WHERE id = ?;`, &sqlitex.ExecOptions{
		Args: []any{seat},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			active, gm = stmt.ColumnBool(0), stmt.ColumnBool(1)
			return nil
		},
	})
	if err != nil || !active || gm {
		t.Fatalf("new seat is_active = %v, is_gm = %v, %v; want active player", active, gm, err)
	}

	if err := exec(t, conn, `INSERT INTO seats (game_id, account_id, is_gm, created_at) VALUES (?, ?, 1, ?);`, g1, a.ID, now.Unix()); sqlite.ErrCode(err) != sqlite.ResultConstraintUnique {
		t.Errorf("second seat in a game = %v, want a unique constraint error", err)
	}
	mustExec(t, conn, `INSERT INTO seats (game_id, account_id, is_gm, created_at) VALUES (?, ?, 1, ?);`, g2, a.ID, now.Unix())

	if err := exec(t, conn, `INSERT INTO seats (game_id, account_id, created_at) VALUES (?, ?, ?);`, g2+1, a.ID, now.Unix()); sqlite.ErrCode(err) != sqlite.ResultConstraintForeignKey {
		t.Errorf("seat in a missing game = %v, want a foreign key error", err)
	}
	if err := exec(t, conn, `INSERT INTO seats (game_id, account_id, created_at) VALUES (?, ?, ?);`, g1, a.ID+1, now.Unix()); sqlite.ErrCode(err) != sqlite.ResultConstraintForeignKey {
		t.Errorf("seat for a missing account = %v, want a foreign key error", err)
	}
	if err := exec(t, conn, `DELETE FROM games WHERE id = ?;`, g1); sqlite.ErrCode(err) != sqlite.ResultConstraintForeignKey {
		t.Errorf("deleting a game with seats = %v, want a foreign key error", err)
	}
	if err := exec(t, conn, `UPDATE seats SET is_gm = 2 WHERE id = ?;`, seat); sqlite.ErrCode(err) != sqlite.ResultConstraintCheck {
		t.Errorf("is_gm = 2: %v, want a check constraint error", err)
	}
}

func TestGameAndSeatIDsAreNotReused(t *testing.T) {
	s, err := store.OpenPrivateMemory(t.Context(), memoryName(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, err := s.CreateAccount(t.Context(), "a@example.com", []byte("x"), false, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := s.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Put(conn)

	game := mustExec(t, conn, `INSERT INTO games (created_at) VALUES (1);`)
	seat := mustExec(t, conn, `INSERT INTO seats (game_id, account_id, created_at) VALUES (?, ?, 1);`, game, a.ID)
	mustExec(t, conn, `DELETE FROM seats;`)
	mustExec(t, conn, `DELETE FROM games;`)
	if again := mustExec(t, conn, `INSERT INTO games (created_at) VALUES (1);`); again == game {
		t.Errorf("game ID %d was reused", game)
	}
	if again := mustExec(t, conn, `INSERT INTO seats (game_id, account_id, created_at) VALUES (last_insert_rowid(), ?, 1);`, a.ID); again == seat {
		t.Errorf("seat ID %d was reused", seat)
	}
}
