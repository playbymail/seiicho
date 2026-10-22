// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package store_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/playbymail/seiicho/internal/cluster"
	"github.com/playbymail/seiicho/internal/prng"
	"github.com/playbymail/seiicho/internal/store"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

func TestGenerateCluster(t *testing.T) {
	s, err := store.OpenPrivateMemory(t.Context(), memoryName(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Unix(1_800_000_000, 0)
	if _, err := s.CreateAccount(t.Context(), "admin@example.com", []byte("x"), true, now); err != nil {
		t.Fatal(err)
	}
	// Seeds with the high bit set are stored as negative integers and must
	// read back unchanged.
	seed1, seed2 := prng.SplitSeed(0x0123456789abcdef)
	if seed2>>63 == 0 {
		t.Fatalf("seed2 = %#x; the test needs a seed with the high bit set", seed2)
	}
	g, err := s.CreateGame(t.Context(), seed1, seed2, now)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := s.CreateGame(t.Context(), seed1, seed2, now)
	if err != nil {
		t.Fatal(err)
	}
	withConn(t, s, func(conn *sqlite.Conn) {
		mustExec(t, conn, `UPDATE games SET is_active = 0 WHERE id = ?;`, closed.ID)
	})

	for _, tc := range []struct {
		name   string
		gameID int64
		want   error
	}{
		{"missing game", 999, store.ErrGameNotFound},
		{"inactive game", closed.ID, store.ErrGameInactive},
	} {
		if _, err := s.GenerateCluster(t.Context(), tc.gameID); !errors.Is(err, tc.want) {
			t.Errorf("GenerateCluster, %s = %v, want %v", tc.name, err, tc.want)
		}
	}

	stars, err := s.GenerateCluster(t.Context(), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := cluster.Generate(prng.New(seed1, seed2).Child(prng.Cluster).PCG())
	if fmt.Sprint(stars) != fmt.Sprint(want) {
		t.Error("GenerateCluster did not draw from the game's cluster source")
	}
	var stored []cluster.Star
	withConn(t, s, func(conn *sqlite.Conn) {
		err := sqlitex.Execute(conn, `SELECT x, y, z, seq FROM stars WHERE game_id = ? ORDER BY id;`, &sqlitex.ExecOptions{
			Args: []any{g.ID},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				stored = append(stored, cluster.Star{
					X: stmt.ColumnInt(0), Y: stmt.ColumnInt(1), Z: stmt.ColumnInt(2), Seq: stmt.ColumnText(3),
				})
				return nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	if fmt.Sprint(stored) != fmt.Sprint(want) {
		t.Errorf("stored %d stars that differ from the %d generated", len(stored), len(want))
	}

	if _, err := s.GenerateCluster(t.Context(), g.ID); !errors.Is(err, store.ErrClusterExists) {
		t.Errorf("GenerateCluster again = %v, want ErrClusterExists", err)
	}
	withConn(t, s, func(conn *sqlite.Conn) {
		if n := count(t, conn, `SELECT count(*) FROM stars;`); n != len(want) {
			t.Errorf("%d stars, want %d (the failed attempts must add nothing)", n, len(want))
		}
	})
}

func TestStars(t *testing.T) {
	s, err := store.OpenPrivateMemory(t.Context(), memoryName(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	conn, err := s.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Put(conn)
	game := mustExec(t, conn, `INSERT INTO games (seed1, seed2, created_at) VALUES (1, 2, 1);`)
	other := mustExec(t, conn, `INSERT INTO games (seed1, seed2, created_at) VALUES (1, 2, 1);`)

	const insert = `INSERT INTO stars (game_id, x, y, z, seq) VALUES (?, ?, ?, ?, ?);`
	mustExec(t, conn, insert, game, 0, 0, 0, "A")
	mustExec(t, conn, insert, game, 30, 30, 30, "E")
	mustExec(t, conn, insert, other, 0, 0, 0, "A")
	if err := exec(t, conn, insert, game, 0, 0, 0, "A"); sqlite.ErrCode(err) != sqlite.ResultConstraintUnique {
		t.Errorf("duplicate star = %v, want a unique constraint error", err)
	}
	for _, args := range [][]any{
		{game, -1, 0, 0, "B"}, {game, 0, 31, 0, "B"}, {game, 0, 0, 31, "B"},
		{game, 0, 0, 0, "F"}, {game, 0, 0, 0, "a"}, {game, 0, 0, 0, "1"},
	} {
		if err := exec(t, conn, insert, args...); sqlite.ErrCode(err) != sqlite.ResultConstraintCheck {
			t.Errorf("star %v = %v, want a check constraint error", args[1:], err)
		}
	}
	if err := exec(t, conn, insert, other+1, 0, 0, 0, "A"); sqlite.ErrCode(err) != sqlite.ResultConstraintForeignKey {
		t.Errorf("star in a missing game = %v, want a foreign key error", err)
	}
	if err := exec(t, conn, `INSERT INTO games (created_at) VALUES (1);`); sqlite.ErrCode(err) != sqlite.ResultConstraintNotNull {
		t.Errorf("game without seeds = %v, want a not null constraint error", err)
	}
}
