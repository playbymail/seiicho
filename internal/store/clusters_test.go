// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package store_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
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
		if _, _, _, err := s.GenerateCluster(t.Context(), tc.gameID, store.ClusterOptions{}); !errors.Is(err, tc.want) {
			t.Errorf("GenerateCluster, %s = %v, want %v", tc.name, err, tc.want)
		}
	}

	var total, totalDeposits int
	for _, opts := range []store.ClusterOptions{{}, {SortStars: true}, {SortPlanets: true}, {SortStars: true, SortPlanets: true}} {
		g, err := s.CreateGame(t.Context(), seed1, seed2, now)
		if err != nil {
			t.Fatal(err)
		}
		stars, planets, deposits, err := s.GenerateCluster(t.Context(), g.ID, opts)
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
		wantPlanets := cluster.Planets(want, root.Child(prng.Planets).PRNG(), root.Child(prng.PlanetKinds).PRNG(), root.Child(prng.Habitability).PRNG())
		if opts.SortPlanets {
			cluster.SortPlanets(want, wantPlanets)
		}
		if fmt.Sprint(planets) != fmt.Sprint(wantPlanets) {
			t.Errorf("%+v: GenerateCluster did not draw from the game's planet sources", opts)
		}
		wantDeposits := cluster.Deposits(want, wantPlanets, root.Child(prng.ResourceBudgets).PRNG(), root.Child(prng.Deposits).PRNG(), root.Child(prng.DepositYields).PRNG())
		if len(deposits) != len(wantDeposits) {
			t.Errorf("%+v: GenerateCluster returned %d deposits, want %d", opts, len(deposits), len(wantDeposits))
		}
		total += len(planets)
		totalDeposits += len(deposits)

		// Rows are stored in the order returned.
		var stored []cluster.Star
		var storedPlanets, storedDeposits []string
		var depositPlanets []int64
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
				`SELECT s.x, s.y, s.z, s.seq, p.orbit, p.kind, p.habitability
				 FROM planets p JOIN stars s ON s.id = p.star_id
				 WHERE s.game_id = ? ORDER BY p.id;`,
				&sqlitex.ExecOptions{
					Args: []any{g.ID},
					ResultFunc: func(stmt *sqlite.Stmt) error {
						storedPlanets = append(storedPlanets, fmt.Sprint(
							stmt.ColumnInt(0), stmt.ColumnInt(1), stmt.ColumnInt(2), stmt.ColumnText(3),
							stmt.ColumnInt(4), stmt.ColumnText(5), stmt.ColumnInt(6)))
						return nil
					},
				})
			if err != nil {
				t.Fatal(err)
			}
			err = sqlitex.Execute(conn,
				`SELECT p.id, s.x, s.y, s.z, s.seq, p.orbit, d.no, d.resource, d.quantity, d.yield_pct
				 FROM deposits d JOIN planets p ON p.id = d.planet_id JOIN stars s ON s.id = p.star_id
				 WHERE s.game_id = ? ORDER BY d.id;`,
				&sqlitex.ExecOptions{
					Args: []any{g.ID},
					ResultFunc: func(stmt *sqlite.Stmt) error {
						depositPlanets = append(depositPlanets, stmt.ColumnInt64(0))
						storedDeposits = append(storedDeposits, fmt.Sprint(
							stmt.ColumnInt(1), stmt.ColumnInt(2), stmt.ColumnInt(3), stmt.ColumnText(4), stmt.ColumnInt(5),
							stmt.ColumnInt(6), stmt.ColumnText(7), stmt.ColumnInt64(8), stmt.ColumnInt(9)))
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
			generated = append(generated, fmt.Sprint(st.X, st.Y, st.Z, st.Seq, p.Orbit, p.Kind, p.Habitability))
		}
		if fmt.Sprint(storedPlanets) != fmt.Sprint(generated) {
			t.Errorf("%+v: stored %d planets that differ from the %d generated", opts, len(storedPlanets), len(generated))
		}

		// Deposits are stored grouped by planet in the planets' stored
		// order, so their IDs reveal no more than the planets' IDs, and
		// they match the returned deposits, which match those generated.
		if !slices.IsSorted(depositPlanets) {
			t.Errorf("%+v: deposits are not stored grouped by planet in planet ID order", opts)
		}
		var returned []string
		for _, d := range deposits {
			p := planets[d.Planet]
			st := stars[p.Star]
			returned = append(returned, fmt.Sprint(st.X, st.Y, st.Z, st.Seq, p.Orbit, d.No, d.Resource, d.Quantity, d.Yield))
		}
		if fmt.Sprint(storedDeposits) != fmt.Sprint(returned) {
			t.Errorf("%+v: stored %d deposits that differ from the %d returned", opts, len(storedDeposits), len(returned))
		}
		var generatedDeposits []string
		for _, d := range wantDeposits {
			p := wantPlanets[d.Planet]
			st := want[p.Star]
			generatedDeposits = append(generatedDeposits, fmt.Sprint(st.X, st.Y, st.Z, st.Seq, p.Orbit, d.No, d.Resource, d.Quantity, d.Yield))
		}
		slices.Sort(returned)
		slices.Sort(generatedDeposits)
		if fmt.Sprint(returned) != fmt.Sprint(generatedDeposits) {
			t.Errorf("%+v: GenerateCluster did not draw from the game's deposit sources", opts)
		}

		// Each star's deposits add up to its budget, recomputed from the
		// game's seed.
		budgets := cluster.Budgets(stars, root.Child(prng.ResourceBudgets).PRNG())
		sums := make([]int64, len(stars))
		for _, d := range deposits {
			sums[planets[d.Planet].Star] += d.Quantity
		}
		if !slices.Equal(sums, budgets) {
			t.Errorf("%+v: the stars' deposits do not add up to their budgets", opts)
		}

		if _, _, _, err := s.GenerateCluster(t.Context(), g.ID, opts); !errors.Is(err, store.ErrClusterExists) {
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
		if n := count(t, conn, `SELECT count(*) FROM deposits;`); n != totalDeposits {
			t.Errorf("%d deposits, want %d (the failed attempts must add nothing)", n, totalDeposits)
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

	if n := count(t, conn, `SELECT count(*) FROM planets WHERE habitability = 0;`); n != 3 {
		t.Errorf("%d planets default to habitability 0, want 3", n)
	}
	const withHabitability = `INSERT INTO planets (star_id, orbit, kind, habitability, is_homeworld) VALUES (?, ?, ?, ?, ?);`
	mustExec(t, conn, withHabitability, star, 3, "rocky", 24, 0)
	mustExec(t, conn, withHabitability, star, 4, "rocky", 25, 1)
	for _, h := range []any{-1, 26, nil} {
		err := exec(t, conn, withHabitability, star, 5, "rocky", h, 0)
		if c := sqlite.ErrCode(err); c != sqlite.ResultConstraintCheck && c != sqlite.ResultConstraintNotNull {
			t.Errorf("habitability %v = %v, want a constraint error", h, err)
		}
	}
	// A home world always has habitability 25, and no other planet does.
	for _, args := range [][]any{{25, 0}, {24, 1}, {0, 1}, {25, 2}, {0, -1}} {
		if err := exec(t, conn, withHabitability, star, 5, "rocky", args[0], args[1]); sqlite.ErrCode(err) != sqlite.ResultConstraintCheck {
			t.Errorf("habitability %v with is_homeworld %v = %v, want a check constraint error", args[0], args[1], err)
		}
	}
	if err := exec(t, conn, `INSERT INTO planets (star_id, orbit, kind, habitability) VALUES (?, 5, 'rocky', 25);`, star); sqlite.ErrCode(err) != sqlite.ResultConstraintCheck {
		t.Errorf("habitability 25 without is_homeworld = %v, want a check constraint error", err)
	}
	if err := exec(t, conn, `UPDATE planets SET is_homeworld = 1 WHERE star_id = ? AND orbit = 3;`, star); sqlite.ErrCode(err) != sqlite.ResultConstraintCheck {
		t.Errorf("flagging a planet without raising its habitability = %v, want a check constraint error", err)
	}
	if n := count(t, conn, `SELECT count(*) FROM planets WHERE is_homeworld = 0;`); n != 4 {
		t.Errorf("%d planets default to is_homeworld 0, want 4", n)
	}
}

func TestDeposits(t *testing.T) {
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
	planet := mustExec(t, conn, `INSERT INTO planets (star_id, orbit, kind) VALUES (?, 1, 'rocky');`, star)

	const insert = `INSERT INTO deposits (planet_id, no, resource, quantity, yield_pct) VALUES (?, ?, ?, ?, ?);`
	mustExec(t, conn, insert, planet, 1, "gold", 99999, 1)
	mustExec(t, conn, insert, planet, 45, "nmts", int64(4_692_000_000), 25)
	// Home worlds raise yields above the generator's 25.
	mustExec(t, conn, insert, planet, 44, "mets", 99999, 75)
	if err := exec(t, conn, insert, planet, 1, "fuel", 99999, 1); sqlite.ErrCode(err) != sqlite.ResultConstraintUnique {
		t.Errorf("duplicate deposit number = %v, want a unique constraint error", err)
	}
	for _, args := range [][]any{
		{planet, 0, "fuel", 99999, 1}, {planet, 46, "fuel", 99999, 1},
		{planet, 2, "Fuel", 99999, 1}, {planet, 2, "metals", 99999, 1},
		{planet, 2, "fuel", 99998, 1}, {planet, 2, "fuel", 99999, 0}, {planet, 2, "fuel", 99999, 76},
	} {
		if err := exec(t, conn, insert, args...); sqlite.ErrCode(err) != sqlite.ResultConstraintCheck {
			t.Errorf("deposit %v = %v, want a check constraint error", args[1:], err)
		}
	}
	if err := exec(t, conn, insert, planet+1, 2, "fuel", 99999, 1); sqlite.ErrCode(err) != sqlite.ResultConstraintForeignKey {
		t.Errorf("deposit on a missing planet = %v, want a foreign key error", err)
	}
	if err := exec(t, conn, insert, planet, 2, "fuel", nil, 1); sqlite.ErrCode(err) != sqlite.ResultConstraintNotNull {
		t.Errorf("deposit without a quantity = %v, want a not null constraint error", err)
	}
}

func TestPlanetsByKind(t *testing.T) {
	s, err := store.OpenPrivateMemory(t.Context(), memoryName(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Unix(1_800_000_000, 0)
	if _, err := s.CreateAccount(t.Context(), "admin@example.com", []byte("x"), true, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlanetsByKind(t.Context(), 1); !errors.Is(err, store.ErrGameNotFound) {
		t.Errorf("PlanetsByKind of a missing game = %v, want ErrGameNotFound", err)
	}
	g, err := s.CreateGame(t.Context(), 1, 2, now)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.PlanetsByKind(t.Context(), g.ID); err != nil || len(got) != 0 {
		t.Errorf("PlanetsByKind without a cluster = %v, %v; want no summaries", got, err)
	}
	_, planets, _, err := s.GenerateCluster(t.Context(), g.ID, store.ClusterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// A second game's planets must not be counted.
	other, err := s.CreateGame(t.Context(), 3, 4, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.GenerateCluster(t.Context(), other.ID, store.ClusterOptions{}); err != nil {
		t.Fatal(err)
	}

	// Summarize the generated planets to compare with the stored ones.
	var want []store.KindSummary
	for _, kind := range []string{cluster.Rocky, cluster.Asteroids, cluster.Giant} {
		total := 0
		k := store.KindSummary{Kind: kind, MinHabitability: 25}
		for _, p := range planets {
			if p.Kind == kind {
				k.Planets++
				total += p.Habitability
				k.MinHabitability = min(k.MinHabitability, p.Habitability)
				k.MaxHabitability = max(k.MaxHabitability, p.Habitability)
			}
		}
		if k.Planets > 0 {
			k.AverageHabitability = float64(total) / float64(k.Planets)
			want = append(want, k)
		}
	}
	if len(want) != 3 {
		t.Fatalf("the cluster has %d kinds of planet, want 3 to test with", len(want))
	}
	got, err := s.PlanetsByKind(t.Context(), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("PlanetsByKind = %+v, want %+v", got, want)
	}
	for i := range want {
		w := want[i]
		if g := got[i]; g.Kind != w.Kind || g.Planets != w.Planets || g.MinHabitability != w.MinHabitability ||
			g.MaxHabitability != w.MaxHabitability || math.Abs(g.AverageHabitability-w.AverageHabitability) > 1e-9 {
			t.Errorf("PlanetsByKind[%d] = %+v, want %+v", i, g, w)
		}
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
	stars, planets, _, err := s.GenerateCluster(t.Context(), g.ID, store.ClusterOptions{})
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

// depositGame returns a store with game 1, whose generated cluster it also
// returns, and a second game whose deposits must not be counted.
func depositGame(t *testing.T) (*store.Store, []cluster.Star, []cluster.Planet, []cluster.Deposit) {
	t.Helper()
	s, err := store.OpenPrivateMemory(t.Context(), memoryName(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	now := time.Unix(1_800_000_000, 0)
	if _, err := s.CreateAccount(t.Context(), "admin@example.com", []byte("x"), true, now); err != nil {
		t.Fatal(err)
	}
	g, err := s.CreateGame(t.Context(), 1, 2, now)
	if err != nil {
		t.Fatal(err)
	}
	stars, planets, deposits, err := s.GenerateCluster(t.Context(), g.ID, store.ClusterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateGame(t.Context(), 3, 4, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.GenerateCluster(t.Context(), other.ID, store.ClusterOptions{}); err != nil {
		t.Fatal(err)
	}
	return s, stars, planets, deposits
}

// near reports whether a and b are equal to within rounding.
func near(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9*max(1, math.Abs(a), math.Abs(b))
}

func TestDepositSummaries(t *testing.T) {
	s, stars, planets, deposits := depositGame(t)
	resources := []string{cluster.Gold, cluster.Fuel, cluster.Metals, cluster.NonMetals}

	// Tally the generated deposits to compare with the stored ones.
	perStar := make([]map[string]int64, len(stars))
	for i := range perStar {
		perStar[i] = map[string]int64{}
	}
	perPlanet := make([]int, len(planets))
	wealth := make([]int64, len(planets))
	byKind := map[string]map[string]int64{}
	var total int64
	for _, d := range deposits {
		p := planets[d.Planet]
		perStar[p.Star][d.Resource] += d.Quantity
		perPlanet[d.Planet]++
		wealth[d.Planet] += d.Quantity
		if byKind[p.Kind] == nil {
			byKind[p.Kind] = map[string]int64{}
		}
		byKind[p.Kind][d.Resource] += d.Quantity
		total += d.Quantity
	}

	t.Run("by resource", func(t *testing.T) {
		got, err := s.DepositsByResource(t.Context(), 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(resources) {
			t.Fatalf("DepositsByResource = %+v, want %d resources", got, len(resources))
		}
		for i, r := range resources {
			w := store.ResourceSummary{Resource: r, MinPerStar: math.MaxInt64}
			for _, d := range deposits {
				if d.Resource == r {
					w.Quantity += d.Quantity
					w.Deposits++
				}
			}
			for _, m := range perStar {
				w.MinPerStar, w.MaxPerStar = min(w.MinPerStar, m[r]), max(w.MaxPerStar, m[r])
			}
			w.AveragePerStar = float64(w.Quantity) / float64(len(stars))
			w.Share = float64(w.Quantity) / float64(total)
			if g := got[i]; g.Resource != w.Resource || g.Quantity != w.Quantity || g.Deposits != w.Deposits ||
				g.MinPerStar != w.MinPerStar || g.MaxPerStar != w.MaxPerStar ||
				!near(g.AveragePerStar, w.AveragePerStar) || !near(g.Share, w.Share) {
				t.Errorf("DepositsByResource[%d] = %+v, want %+v", i, g, w)
			}
		}
	})

	t.Run("by planet type", func(t *testing.T) {
		got, err := s.DepositsByKind(t.Context(), 1)
		if err != nil {
			t.Fatal(err)
		}
		kinds := []string{cluster.Rocky, cluster.Asteroids, cluster.Giant}
		if len(got) != len(kinds) {
			t.Fatalf("DepositsByKind = %+v, want %d kinds", got, len(kinds))
		}
		for i, kind := range kinds {
			w := store.KindDeposits{Kind: kind, MinDeposits: math.MaxInt, MinWealth: math.MaxInt64}
			var n int
			var sum int64
			for j, p := range planets {
				if p.Kind != kind {
					continue
				}
				w.Planets++
				n += perPlanet[j]
				sum += wealth[j]
				w.MinDeposits, w.MaxDeposits = min(w.MinDeposits, perPlanet[j]), max(w.MaxDeposits, perPlanet[j])
				w.MinWealth, w.MaxWealth = min(w.MinWealth, wealth[j]), max(w.MaxWealth, wealth[j])
			}
			w.AverageDeposits = float64(n) / float64(w.Planets)
			w.AverageWealth = float64(sum) / float64(w.Planets)
			k := byKind[kind]
			w.Shares = store.ResourceShares{
				Gold: float64(k[cluster.Gold]) / float64(sum), Fuel: float64(k[cluster.Fuel]) / float64(sum),
				Metals: float64(k[cluster.Metals]) / float64(sum), NonMetals: float64(k[cluster.NonMetals]) / float64(sum),
			}
			g := got[i]
			if g.Kind != w.Kind || g.Planets != w.Planets || g.MinDeposits != w.MinDeposits || g.MaxDeposits != w.MaxDeposits ||
				g.MinWealth != w.MinWealth || g.MaxWealth != w.MaxWealth ||
				!near(g.AverageDeposits, w.AverageDeposits) || !near(g.AverageWealth, w.AverageWealth) ||
				!near(g.Shares.Gold, w.Shares.Gold) || !near(g.Shares.Fuel, w.Shares.Fuel) ||
				!near(g.Shares.Metals, w.Shares.Metals) || !near(g.Shares.NonMetals, w.Shares.NonMetals) {
				t.Errorf("DepositsByKind[%d] = %+v, want %+v", i, g, w)
			}
		}
	})

	t.Run("by planets per star", func(t *testing.T) {
		got, err := s.DepositsByPlanetsPerStar(t.Context(), 1)
		if err != nil {
			t.Fatal(err)
		}
		size := make([]int, len(stars))
		starWealth := make([]int64, len(stars))
		for j, p := range planets {
			size[p.Star]++
			starWealth[p.Star] += wealth[j]
		}
		type tally struct {
			stars int
			sum   int64
		}
		want := map[int]*tally{}
		for i, n := range size {
			if want[n] == nil {
				want[n] = &tally{}
			}
			want[n].stars++
			want[n].sum += starWealth[i]
		}
		if len(got) != len(want) {
			t.Fatalf("DepositsByPlanetsPerStar = %+v, want %d sizes", got, len(want))
		}
		for i, g := range got {
			w := want[g.Planets]
			if i > 0 && g.Planets <= got[i-1].Planets {
				t.Errorf("DepositsByPlanetsPerStar[%d] is for %d planets, after %d", i, g.Planets, got[i-1].Planets)
			}
			if w == nil || g.Stars != w.stars || !near(g.AveragePerStar, float64(w.sum)/float64(w.stars)) ||
				!near(g.AveragePerPlanet, float64(w.sum)/float64(w.stars*g.Planets)) {
				t.Errorf("DepositsByPlanetsPerStar[%d] = %+v, want %d stars with %d in all", i, g, w.stars, w.sum)
			}
		}
	})

	checkEmpty(t, s)
}

func TestDepositHistograms(t *testing.T) {
	s, _, planets, deposits := depositGame(t)

	quantities, err := s.DepositQuantities(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	bounds := map[int64]bool{cluster.MinDeposit: true}
	for p := int64(100_000); p <= 1_000_000_000_000; p *= 10 {
		bounds[2*p], bounds[5*p], bounds[10*p] = true, true, true
	}
	n := 0
	for i, b := range quantities {
		if i > 0 && b.Min != quantities[i-1].Max+1 {
			t.Errorf("quantity bin %+v does not follow %+v", b, quantities[i-1])
		}
		if !bounds[b.Min] {
			t.Errorf("quantity bin %+v does not start at MinDeposit or 1, 2, or 5 times a power of 10", b)
		}
		want := 0
		for _, d := range deposits {
			if d.Quantity >= b.Min && d.Quantity <= b.Max {
				want++
			}
		}
		if b.Count != want {
			t.Errorf("quantity bin %+v, want a count of %d", b, want)
		}
		n += b.Count
	}
	if n != len(deposits) || quantities[0].Count == 0 || quantities[len(quantities)-1].Count == 0 {
		t.Errorf("quantity bins %+v count %d deposits, want %d from the smallest bin to the largest", quantities, n, len(deposits))
	}

	perPlanet := map[int]int{}
	for _, d := range deposits {
		perPlanet[d.Planet]++
	}
	for _, tc := range []struct {
		name      string
		histogram func(context.Context, int64) ([]store.ValueBin, error)
		hi        int
		want      map[int]int
	}{
		{"DepositYields", s.DepositYields, cluster.MaxYield, func() map[int]int {
			m := map[int]int{}
			for _, d := range deposits {
				m[d.Yield]++
			}
			return m
		}()},
		{"DepositsPerPlanet", s.DepositsPerPlanet, cluster.MostDeposits, func() map[int]int {
			m := map[int]int{}
			for _, n := range perPlanet {
				m[n]++
			}
			return m
		}()},
	} {
		bins, err := tc.histogram(t.Context(), 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(bins) != tc.hi {
			t.Fatalf("%s has %d bins, want %d", tc.name, len(bins), tc.hi)
		}
		for i, b := range bins {
			if b.Value != i+1 || b.Count != tc.want[b.Value] {
				t.Errorf("%s bin %d = %+v, want %d with a count of %d", tc.name, i, b, i+1, tc.want[i+1])
			}
		}
	}
	if len(perPlanet) != len(planets) {
		t.Errorf("%d of %d planets have deposits", len(perPlanet), len(planets))
	}

	// A home world's yield above cluster.MaxYield extends the yield bins to
	// it.
	withConn(t, s, func(conn *sqlite.Conn) {
		mustExec(t, conn, `UPDATE deposits SET yield_pct = 40 WHERE id = (SELECT min(id) FROM deposits);`)
	})
	bins, err := s.DepositYields(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(bins) != 40 || bins[39] != (store.ValueBin{Value: 40, Count: 1}) || bins[30].Count != 0 {
		t.Errorf("DepositYields with a yield of 40 = %+v, want 40 bins, the last counting 1", bins)
	}

	checkEmpty(t, s)
}

// checkEmpty checks that the deposit reports find no game 999, and nothing
// for a game without a cluster or for one whose cluster has no deposits,
// as a game whose deposits were never generated has.
func checkEmpty(t *testing.T, s *store.Store) {
	t.Helper()
	g, err := s.CreateGame(t.Context(), 5, 6, time.Unix(1_800_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	reports := map[string]func(int64) (int, error){
		"DepositsByResource": func(id int64) (int, error) { r, err := s.DepositsByResource(t.Context(), id); return len(r), err },
		"DepositsByKind":     func(id int64) (int, error) { r, err := s.DepositsByKind(t.Context(), id); return len(r), err },
		"DepositsByPlanetsPerStar": func(id int64) (int, error) {
			r, err := s.DepositsByPlanetsPerStar(t.Context(), id)
			return len(r), err
		},
		"DepositQuantities": func(id int64) (int, error) { r, err := s.DepositQuantities(t.Context(), id); return len(r), err },
		"DepositYields":     func(id int64) (int, error) { r, err := s.DepositYields(t.Context(), id); return len(r), err },
		"DepositsPerPlanet": func(id int64) (int, error) { r, err := s.DepositsPerPlanet(t.Context(), id); return len(r), err },
	}
	check := func(when string) {
		for name, report := range reports {
			if n, err := report(g.ID); err != nil || n != 0 {
				t.Errorf("%s %s = %d rows, %v; want none", name, when, n, err)
			}
		}
	}
	for name, report := range reports {
		if _, err := report(999); !errors.Is(err, store.ErrGameNotFound) {
			t.Errorf("%s of a missing game = %v, want ErrGameNotFound", name, err)
		}
	}
	check("without a cluster")
	if _, _, _, err := s.GenerateCluster(t.Context(), g.ID, store.ClusterOptions{}); err != nil {
		t.Fatal(err)
	}
	withConn(t, s, func(conn *sqlite.Conn) {
		mustExec(t, conn, `DELETE FROM deposits WHERE planet_id IN (
			SELECT p.id FROM planets p JOIN stars s ON s.id = p.star_id WHERE s.game_id = ?);`, g.ID)
	})
	check("without deposits")
}

// clusterRows lists every planet and deposit in the store, except those of
// the planet with ID except, so that tests can see whether anything changed.
func clusterRows(t *testing.T, s *store.Store, except int64) string {
	t.Helper()
	var rows []string
	withConn(t, s, func(conn *sqlite.Conn) {
		for _, query := range []string{
			`SELECT id, star_id, orbit, kind, habitability, is_homeworld FROM planets WHERE id != ? ORDER BY id;`,
			`SELECT id, planet_id, no, resource, quantity, yield_pct FROM deposits WHERE planet_id != ? ORDER BY id;`,
		} {
			err := sqlitex.Execute(conn, query, &sqlitex.ExecOptions{
				Args: []any{except},
				ResultFunc: func(stmt *sqlite.Stmt) error {
					var row []string
					for i := range stmt.ColumnCount() {
						row = append(row, stmt.ColumnText(i))
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

// homeDeposits returns the deposits of the planet with planetID, in
// deposit-number order. Their Planet is 0.
func homeDeposits(t *testing.T, s *store.Store, planetID int64) []cluster.Deposit {
	t.Helper()
	var deposits []cluster.Deposit
	withConn(t, s, func(conn *sqlite.Conn) {
		err := sqlitex.Execute(conn, `SELECT no, resource, quantity, yield_pct FROM deposits WHERE planet_id = ? ORDER BY no;`,
			&sqlitex.ExecOptions{
				Args: []any{planetID},
				ResultFunc: func(stmt *sqlite.Stmt) error {
					deposits = append(deposits, cluster.Deposit{
						No: stmt.ColumnInt(0), Resource: stmt.ColumnText(1), Quantity: stmt.ColumnInt64(2), Yield: stmt.ColumnInt(3),
					})
					return nil
				},
			})
		if err != nil {
			t.Fatal(err)
		}
	})
	return deposits
}

func TestFlagHomeWorld(t *testing.T) {
	s, err := store.OpenPrivateMemory(t.Context(), memoryName(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Unix(1_800_000_000, 0)
	if _, err := s.CreateAccount(t.Context(), "admin@example.com", []byte("x"), true, now); err != nil {
		t.Fatal(err)
	}
	seed1, seed2 := prng.SplitSeed(0x0123456789abcdef)
	game := func(opts store.ClusterOptions) int64 {
		t.Helper()
		g, err := s.CreateGame(t.Context(), seed1, seed2, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := s.GenerateCluster(t.Context(), g.ID, opts); err != nil {
			t.Fatal(err)
		}
		return g.ID
	}
	sorted, unsorted := game(store.ClusterOptions{SortStars: true, SortPlanets: true}), game(store.ClusterOptions{})
	bare, err := s.CreateGame(t.Context(), seed1, seed2, now)
	if err != nil {
		t.Fatal(err)
	}
	closed := game(store.ClusterOptions{})
	withConn(t, s, func(conn *sqlite.Conn) {
		mustExec(t, conn, `UPDATE games SET is_active = 0 WHERE id = ?;`, closed)
	})

	before := clusterRows(t, s, 0)
	for _, tc := range []struct {
		name   string
		gameID int64
		want   error
	}{
		{"missing game", 999, store.ErrGameNotFound},
		{"inactive game", closed, store.ErrGameInactive},
		{"game without a cluster", bare.ID, store.ErrNoCluster},
		// No system has a neighbor within 1.
		{"no planet qualifies", sorted, cluster.ErrNoHomeWorld},
	} {
		if _, err := s.FlagHomeWorld(t.Context(), tc.gameID, 3, 1); !errors.Is(err, tc.want) {
			t.Errorf("FlagHomeWorld, %s = %v, want %v", tc.name, err, tc.want)
		}
	}
	if clusterRows(t, s, 0) != before {
		t.Fatal("a failed FlagHomeWorld changed the database")
	}

	// The store flags the planets that cluster.HomeWorld chooses, however
	// the game's rows are ordered, and a failed run leaves no trace.
	root := prng.New(seed1, seed2)
	stars := cluster.Generate(root.Child(prng.Cluster).PCG())
	planets := cluster.Planets(stars, root.Child(prng.Planets).PRNG(), root.Child(prng.PlanetKinds).PRNG(), root.Child(prng.Habitability).PRNG())
	deposits := cluster.Deposits(stars, planets, root.Child(prng.ResourceBudgets).PRNG(), root.Child(prng.Deposits).PRNG(), root.Child(prng.DepositYields).PRNG())
	var homes []int
	for run := range 6 {
		j, err := cluster.HomeWorld(stars, planets, homes, 6, 6, root.Child(prng.HomeWorlds).PRNG())
		if err != nil {
			t.Fatal(err)
		}
		st := stars[planets[j].Star]
		for _, gameID := range []int64{sorted, unsorted} {
			// Fail once in one game only: the games still flag the same
			// planets.
			if run == 2 && gameID == sorted {
				failed := clusterRows(t, s, 0)
				if _, err := s.FlagHomeWorld(t.Context(), gameID, 16, 1); !errors.Is(err, cluster.ErrNoHomeWorld) {
					t.Fatalf("FlagHomeWorld(16, 1) = %v, want ErrNoHomeWorld", err)
				}
				if clusterRows(t, s, 0) != failed {
					t.Fatal("a failed FlagHomeWorld changed the database")
				}
			}
			var planetID int64
			withConn(t, s, func(conn *sqlite.Conn) {
				err := sqlitex.Execute(conn,
					`SELECT p.id FROM planets p JOIN stars s ON s.id = p.star_id
					 WHERE s.game_id = ? AND s.x = ? AND s.y = ? AND s.z = ? AND s.seq = ? AND p.orbit = ?;`,
					&sqlitex.ExecOptions{
						Args: []any{gameID, st.X, st.Y, st.Z, st.Seq, planets[j].Orbit},
						ResultFunc: func(stmt *sqlite.Stmt) error {
							planetID = stmt.ColumnInt64(0)
							return nil
						},
					})
				if err != nil {
					t.Fatal(err)
				}
			})
			old := homeDeposits(t, s, planetID)
			others := clusterRows(t, s, planetID)

			got, err := s.FlagHomeWorld(t.Context(), gameID, 6, 6)
			if err != nil {
				t.Fatal(err)
			}
			want := store.HomeWorld{PlanetID: planetID, X: st.X, Y: st.Y, Z: st.Z, Seq: st.Seq, Orbit: planets[j].Orbit}
			if got != want {
				t.Fatalf("game %d, run %d: FlagHomeWorld = %+v, want %+v", gameID, run, got, want)
			}

			// The planet is flagged with habitability 25, and its deposits
			// are raised as cluster.RaiseHomeDeposits raises them, drawing
			// after the shuffle.
			var hab, flagged int
			withConn(t, s, func(conn *sqlite.Conn) {
				hab = count(t, conn, fmt.Sprintf(`SELECT habitability FROM planets WHERE id = %d;`, planetID))
				flagged = count(t, conn, fmt.Sprintf(`SELECT is_homeworld FROM planets WHERE id = %d;`, planetID))
			})
			if hab != 25 || flagged != 1 {
				t.Errorf("game %d: planet %d has habitability %d and is_homeworld %d, want 25 and 1", gameID, planetID, hab, flagged)
			}
			var wantDeposits []cluster.Deposit
			for _, d := range deposits {
				if d.Planet == j {
					d.Planet = 0
					wantDeposits = append(wantDeposits, d)
				}
			}
			p := root.Child(prng.HomeWorlds).PRNG()
			if _, err := cluster.HomeWorld(stars, planets, homes, 6, 6, p); err != nil {
				t.Fatal(err)
			}
			cluster.RaiseHomeDeposits(wantDeposits, p)
			raised := homeDeposits(t, s, planetID)
			if fmt.Sprint(raised) != fmt.Sprint(wantDeposits) {
				t.Errorf("game %d: home world deposits = %v, want %v", gameID, raised, wantDeposits)
			}
			for k, d := range raised {
				if rise := d.Yield - old[k].Yield; rise < 1 || rise > 10 || d.Quantity != old[k].Quantity*125/100 {
					t.Errorf("game %d: deposit %d went from %+v to %+v", gameID, d.No, old[k], d)
				}
			}

			// Nothing else changed.
			if clusterRows(t, s, planetID) != others {
				t.Errorf("game %d: FlagHomeWorld changed more than the home world and its deposits", gameID)
			}
		}
		planets[j].Habitability = 25
		homes = append(homes, j)
	}
}
