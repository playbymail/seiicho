// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"cmp"
	"context"
	"slices"

	"github.com/playbymail/seiicho/internal/cluster"
	"github.com/playbymail/seiicho/internal/prng"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// ClusterOptions control the order in which GenerateCluster stores a
// cluster, and so the order of the IDs the rows get.
type ClusterOptions struct {
	// SortStars stores stars sorted by (x, y, z, seq) instead of in draw
	// order.
	SortStars bool
	// SortPlanets stores planets sorted by star, as SortStars orders them,
	// then by orbit, instead of in allocation order. Sorted planets are
	// comparable between runs, but their IDs reveal how many planets each
	// star has.
	SortPlanets bool
}

// GenerateCluster generates a game's cluster, its planets, and their
// deposits from the game's seeds and stores them, returning them in the
// order stored; each planet's Star indexes the returned stars, and each
// deposit's Planet the returned planets. Deposits are stored grouped by
// planet, in the planets' order, so that their IDs reveal no more than the
// planets' IDs do. The game must exist and be active;
// otherwise it returns ErrGameNotFound or ErrGameInactive. It returns
// ErrClusterExists if the game already has stars. On error, it stores
// nothing.
func (s *Store) GenerateCluster(ctx context.Context, gameID int64, opts ClusterOptions) (stars []cluster.Star, planets []cluster.Planet, deposits []cluster.Deposit, err error) {
	conn, err := s.pool.Take(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	defer s.pool.Put(conn)

	end, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		return nil, nil, nil, err
	}
	defer end(&err)

	found, active, hasStars := false, false, false
	var seed1, seed2 uint64
	err = sqlitex.Execute(conn,
		`SELECT is_active, seed1, seed2, EXISTS (SELECT 1 FROM stars WHERE game_id = g.id)
		 FROM games g WHERE id = ?;`,
		&sqlitex.ExecOptions{
			Args: []any{gameID},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				found, active = true, stmt.ColumnBool(0)
				seed1, seed2 = uint64(stmt.ColumnInt64(1)), uint64(stmt.ColumnInt64(2))
				hasStars = stmt.ColumnBool(3)
				return nil
			},
		})
	if err != nil {
		return nil, nil, nil, err
	}
	switch {
	case !found:
		return nil, nil, nil, ErrGameNotFound
	case !active:
		return nil, nil, nil, ErrGameInactive
	case hasStars:
		return nil, nil, nil, ErrClusterExists
	}

	root := prng.New(seed1, seed2)
	stars = cluster.Generate(root.Child(prng.Cluster).PCG())
	if opts.SortStars {
		cluster.SortStars(stars)
	}
	starIDs := make([]int64, len(stars))
	for i, st := range stars {
		err = sqlitex.Execute(conn,
			`INSERT INTO stars (game_id, x, y, z, seq) VALUES (?, ?, ?, ?, ?);`,
			&sqlitex.ExecOptions{Args: []any{gameID, st.X, st.Y, st.Z, st.Seq}})
		if err != nil {
			return nil, nil, nil, err
		}
		starIDs[i] = conn.LastInsertRowID()
	}

	planets = cluster.Planets(stars, root.Child(prng.Planets).PRNG(), root.Child(prng.PlanetKinds).PRNG(), root.Child(prng.Habitability).PRNG())
	if opts.SortPlanets {
		cluster.SortPlanets(stars, planets)
	}
	planetIDs := make([]int64, len(planets))
	for i, p := range planets {
		err = sqlitex.Execute(conn,
			`INSERT INTO planets (star_id, orbit, kind, habitability) VALUES (?, ?, ?, ?);`,
			&sqlitex.ExecOptions{Args: []any{starIDs[p.Star], p.Orbit, p.Kind, p.Habitability}})
		if err != nil {
			return nil, nil, nil, err
		}
		planetIDs[i] = conn.LastInsertRowID()
	}

	deposits = cluster.Deposits(stars, planets, root.Child(prng.ResourceBudgets).PRNG(), root.Child(prng.Deposits).PRNG(), root.Child(prng.DepositYields).PRNG())
	// Deposits come by star; a stable sort groups them by planet and keeps
	// each planet's in number order.
	slices.SortStableFunc(deposits, func(a, b cluster.Deposit) int { return cmp.Compare(a.Planet, b.Planet) })
	for _, d := range deposits {
		err = sqlitex.Execute(conn,
			`INSERT INTO deposits (planet_id, no, resource, quantity, yield_pct) VALUES (?, ?, ?, ?, ?);`,
			&sqlitex.ExecOptions{Args: []any{planetIDs[d.Planet], d.No, d.Resource, d.Quantity, d.Yield}})
		if err != nil {
			return nil, nil, nil, err
		}
	}
	return stars, planets, deposits, nil
}

// Bin is one bar of a histogram: Count stars or systems have Planets planets.
type Bin struct {
	Planets int
	Count   int
}

// PlanetsPerStar returns a histogram of the number of planets each of a
// game's stars has. It returns ErrGameNotFound if there is no such game.
func (s *Store) PlanetsPerStar(ctx context.Context, gameID int64) ([]Bin, error) {
	return s.planetHistogram(ctx, gameID,
		`SELECT count(p.id) FROM stars s LEFT JOIN planets p ON p.star_id = s.id
		 WHERE s.game_id = ? GROUP BY s.id;`)
}

// PlanetsPerSystem returns a histogram of the number of planets in each of a
// game's star systems, the stars at one (x, y, z). It returns ErrGameNotFound
// if there is no such game.
func (s *Store) PlanetsPerSystem(ctx context.Context, gameID int64) ([]Bin, error) {
	return s.planetHistogram(ctx, gameID,
		`SELECT count(p.id) FROM stars s LEFT JOIN planets p ON p.star_id = s.id
		 WHERE s.game_id = ? GROUP BY s.x, s.y, s.z;`)
}

// planetHistogram tallies the planet counts that query returns for a game.
// The bins run from the fewest planets to the most, one per number of
// planets, including those with a Count of 0. A game with no cluster has no
// bins.
func (s *Store) planetHistogram(ctx context.Context, gameID int64, query string) (bins []Bin, err error) {
	conn, err := s.pool.Take(ctx)
	if err != nil {
		return nil, err
	}
	defer s.pool.Put(conn)

	// Read both queries from one snapshot.
	defer sqlitex.Save(conn)(&err)

	if err := gameExists(conn, gameID); err != nil {
		return nil, err
	}
	var counts []int
	err = sqlitex.Execute(conn, query, &sqlitex.ExecOptions{
		Args: []any{gameID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			counts = append(counts, stmt.ColumnInt(0))
			return nil
		},
	})
	if err != nil || len(counts) == 0 {
		return nil, err
	}
	lo, hi := slices.Min(counts), slices.Max(counts)
	bins = make([]Bin, hi-lo+1)
	for i := range bins {
		bins[i].Planets = lo + i
	}
	for _, n := range counts {
		bins[n-lo].Count++
	}
	return bins, nil
}

// KindSummary summarizes a game's planets of one Kind: how many there are,
// and the lowest, average, and highest of their habitability.
type KindSummary struct {
	Kind                string
	Planets             int
	MinHabitability     int
	AverageHabitability float64
	MaxHabitability     int
}

// PlanetsByKind summarizes a game's planets by kind, in the order rocky,
// asteroids, giant. Kinds that no planet has are left out, so a game with no
// cluster has no summaries. It returns ErrGameNotFound if there is no such
// game.
func (s *Store) PlanetsByKind(ctx context.Context, gameID int64) (summaries []KindSummary, err error) {
	conn, err := s.pool.Take(ctx)
	if err != nil {
		return nil, err
	}
	defer s.pool.Put(conn)

	// Read both queries from one snapshot.
	defer sqlitex.Save(conn)(&err)

	if err := gameExists(conn, gameID); err != nil {
		return nil, err
	}
	err = sqlitex.Execute(conn,
		`SELECT p.kind, count(*), min(p.habitability), avg(p.habitability), max(p.habitability)
		 FROM planets p JOIN stars s ON s.id = p.star_id
		 WHERE s.game_id = ?
		 GROUP BY p.kind
		 ORDER BY CASE p.kind WHEN 'rocky' THEN 1 WHEN 'asteroids' THEN 2 ELSE 3 END;`,
		&sqlitex.ExecOptions{
			Args: []any{gameID},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				summaries = append(summaries, KindSummary{
					Kind: stmt.ColumnText(0), Planets: stmt.ColumnInt(1), MinHabitability: stmt.ColumnInt(2),
					AverageHabitability: stmt.ColumnFloat(3), MaxHabitability: stmt.ColumnInt(4),
				})
				return nil
			},
		})
	return summaries, err
}

// gameExists returns ErrGameNotFound if there is no game with gameID.
func gameExists(conn *sqlite.Conn, gameID int64) error {
	found := false
	err := sqlitex.Execute(conn, `SELECT 1 FROM games WHERE id = ?;`, &sqlitex.ExecOptions{
		Args: []any{gameID},
		ResultFunc: func(*sqlite.Stmt) error {
			found = true
			return nil
		},
	})
	if err == nil && !found {
		err = ErrGameNotFound
	}
	return err
}
