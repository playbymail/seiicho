// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package store_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
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
		if _, _, err := s.GenerateCluster(t.Context(), tc.gameID, store.ClusterOptions{}); !errors.Is(err, tc.want) {
			t.Errorf("GenerateCluster, %s = %v, want %v", tc.name, err, tc.want)
		}
	}

	var total int
	for _, opts := range []store.ClusterOptions{{}, {SortStars: true}, {SortPlanets: true}, {SortStars: true, SortPlanets: true}} {
		g, err := s.CreateGame(t.Context(), seed1, seed2, now)
		if err != nil {
			t.Fatal(err)
		}
		stars, planets, err := s.GenerateCluster(t.Context(), g.ID, opts)
		if err != nil {
			t.Fatal(err)
		}
		want := cluster.Generate(prng.New(seed1, seed2).Child(prng.Cluster).PCG())
		if opts.SortStars {
			cluster.SortStars(want)
		}
		if fmt.Sprint(stars) != fmt.Sprint(want) {
			t.Errorf("%+v: GenerateCluster did not draw from the game's cluster source", opts)
		}
		root := prng.New(seed1, seed2)
		wantPlanets := cluster.Planets(want, root.Child(prng.Planets).PRNG(), root.Child(prng.PlanetKinds).PRNG())
		if opts.SortPlanets {
			cluster.SortPlanets(want, wantPlanets)
		}
		if fmt.Sprint(planets) != fmt.Sprint(wantPlanets) {
			t.Errorf("%+v: GenerateCluster did not draw from the game's planet sources", opts)
		}
		total += len(planets)

		// Rows are stored in the order returned.
		var stored []cluster.Star
		var storedPlanets []string
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
			err = sqlitex.Execute(conn,
				`SELECT s.x, s.y, s.z, s.seq, p.orbit, p.kind
				 FROM planets p JOIN stars s ON s.id = p.star_id
				 WHERE s.game_id = ? ORDER BY p.id;`,
				&sqlitex.ExecOptions{
					Args: []any{g.ID},
					ResultFunc: func(stmt *sqlite.Stmt) error {
						storedPlanets = append(storedPlanets, fmt.Sprint(
							stmt.ColumnInt(0), stmt.ColumnInt(1), stmt.ColumnInt(2), stmt.ColumnText(3),
							stmt.ColumnInt(4), stmt.ColumnText(5)))
						return nil
					},
				})
			if err != nil {
				t.Fatal(err)
			}
		})
		if fmt.Sprint(stored) != fmt.Sprint(want) {
			t.Errorf("%+v: stored %d stars that differ from the %d generated", opts, len(stored), len(want))
		}
		var generated []string
		for _, p := range wantPlanets {
			st := want[p.Star]
			generated = append(generated, fmt.Sprint(st.X, st.Y, st.Z, st.Seq, p.Orbit, p.Kind))
		}
		if fmt.Sprint(storedPlanets) != fmt.Sprint(generated) {
			t.Errorf("%+v: stored %d planets that differ from the %d generated", opts, len(storedPlanets), len(generated))
		}

		if _, _, err := s.GenerateCluster(t.Context(), g.ID, opts); !errors.Is(err, store.ErrClusterExists) {
			t.Errorf("%+v: GenerateCluster again = %v, want ErrClusterExists", opts, err)
		}
	}
	withConn(t, s, func(conn *sqlite.Conn) {
		if n := count(t, conn, `SELECT count(*) FROM stars;`); n != 4*159 {
			t.Errorf("%d stars, want %d (the failed attempts must add nothing)", n, 4*159)
		}
		if n := count(t, conn, `SELECT count(*) FROM planets;`); n != total {
			t.Errorf("%d planets, want %d (the failed attempts must add nothing)", n, total)
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

func TestPlanets(t *testing.T) {
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
	star := mustExec(t, conn, `INSERT INTO stars (game_id, x, y, z, seq) VALUES (?, 0, 0, 0, 'A');`, game)

	const insert = `INSERT INTO planets (star_id, orbit, kind) VALUES (?, ?, ?);`
	mustExec(t, conn, insert, star, 1, "rocky")
	mustExec(t, conn, insert, star, 2, "asteroids")
	mustExec(t, conn, insert, star, 10, "giant")
	if err := exec(t, conn, insert, star, 1, "giant"); sqlite.ErrCode(err) != sqlite.ResultConstraintUnique {
		t.Errorf("duplicate orbit = %v, want a unique constraint error", err)
	}
	for _, args := range [][]any{{star, 0, "rocky"}, {star, 11, "rocky"}, {star, 3, "gas"}, {star, 3, "Rocky"}} {
		if err := exec(t, conn, insert, args...); sqlite.ErrCode(err) != sqlite.ResultConstraintCheck {
			t.Errorf("planet %v = %v, want a check constraint error", args[1:], err)
		}
	}
	if err := exec(t, conn, insert, star+1, 3, "rocky"); sqlite.ErrCode(err) != sqlite.ResultConstraintForeignKey {
		t.Errorf("planet of a missing star = %v, want a foreign key error", err)
	}
}

func TestPlanetHistograms(t *testing.T) {
	s, err := store.OpenPrivateMemory(t.Context(), memoryName(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Unix(1_800_000_000, 0)
	if _, err := s.CreateAccount(t.Context(), "admin@example.com", []byte("x"), true, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlanetsPerStar(t.Context(), 1); !errors.Is(err, store.ErrGameNotFound) {
		t.Errorf("PlanetsPerStar of a missing game = %v, want ErrGameNotFound", err)
	}
	if _, err := s.PlanetsPerSystem(t.Context(), 1); !errors.Is(err, store.ErrGameNotFound) {
		t.Errorf("PlanetsPerSystem of a missing game = %v, want ErrGameNotFound", err)
	}
	g, err := s.CreateGame(t.Context(), 1, 2, now)
	if err != nil {
		t.Fatal(err)
	}
	if bins, err := s.PlanetsPerStar(t.Context(), g.ID); err != nil || len(bins) != 0 {
		t.Errorf("PlanetsPerStar without a cluster = %v, %v; want no bins", bins, err)
	}
	stars, planets, err := s.GenerateCluster(t.Context(), g.ID, store.ClusterOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Tally the generated cluster to compare with the stored one.
	perStar := map[int]int{}
	perSystem := map[[3]int]int{}
	for _, p := range planets {
		st := stars[p.Star]
		perStar[p.Star]++
		perSystem[[3]int{st.X, st.Y, st.Z}]++
	}
	for _, tc := range []struct {
		name      string
		histogram func(context.Context, int64) ([]store.Bin, error)
		counts    []int
	}{
		{"PlanetsPerStar", s.PlanetsPerStar, slices.Collect(maps.Values(perStar))},
		{"PlanetsPerSystem", s.PlanetsPerSystem, slices.Collect(maps.Values(perSystem))},
	} {
		want := map[int]int{}
		for _, n := range tc.counts {
			want[n]++
		}
		bins, err := tc.histogram(t.Context(), g.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(bins) == 0 || bins[0].Planets != slices.Min(tc.counts) || bins[len(bins)-1].Planets != slices.Max(tc.counts) {
			t.Fatalf("%s = %v, want bins from %d to %d", tc.name, bins, slices.Min(tc.counts), slices.Max(tc.counts))
		}
		for i, b := range bins {
			if b.Planets != bins[0].Planets+i {
				t.Errorf("%s: bin %d is for %d planets, want %d", tc.name, i, b.Planets, bins[0].Planets+i)
			}
			if b.Count != want[b.Planets] {
				t.Errorf("%s: %d have %d planets, want %d", tc.name, b.Count, b.Planets, want[b.Planets])
			}
		}
	}
}
