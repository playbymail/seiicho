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

// ResourceSummary summarizes a game's deposits of one Resource: their total
// Quantity, its Share of the quantity of all the game's deposits, from 0 to
// 1, the number of Deposits, and the lowest, average, and highest quantity
// of the resource per star. Stars without the resource count as 0.
type ResourceSummary struct {
	Resource       string
	Quantity       int64
	Share          float64
	Deposits       int
	MinPerStar     int64
	AveragePerStar float64
	MaxPerStar     int64
}

// DepositsByResource summarizes a game's deposits by resource, in the order
// gold, fuel, mets, nmts. Resources that no deposit has are left out, so a
// game without deposits has no summaries. It returns ErrGameNotFound if
// there is no such game.
func (s *Store) DepositsByResource(ctx context.Context, gameID int64) (summaries []ResourceSummary, err error) {
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
	var total int64
	err = sqlitex.Execute(conn,
		`WITH resources (resource, rank) AS (VALUES ('gold', 1), ('fuel', 2), ('mets', 3), ('nmts', 4)),
		 per_star AS (
			SELECT r.resource, r.rank, count(d.id) AS n, coalesce(sum(d.quantity), 0) AS q
			FROM stars s CROSS JOIN resources r
			LEFT JOIN planets p ON p.star_id = s.id
			LEFT JOIN deposits d ON d.planet_id = p.id AND d.resource = r.resource
			WHERE s.game_id = ?
			GROUP BY s.id, r.resource)
		 SELECT resource, sum(q), sum(n), min(q), avg(q), max(q)
		 FROM per_star
		 GROUP BY resource
		 HAVING sum(n) > 0
		 ORDER BY min(rank);`,
		&sqlitex.ExecOptions{
			Args: []any{gameID},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				r := ResourceSummary{
					Resource: stmt.ColumnText(0), Quantity: stmt.ColumnInt64(1), Deposits: stmt.ColumnInt(2),
					MinPerStar: stmt.ColumnInt64(3), AveragePerStar: stmt.ColumnFloat(4), MaxPerStar: stmt.ColumnInt64(5),
				}
				total += r.Quantity
				summaries = append(summaries, r)
				return nil
			},
		})
	for i := range summaries {
		summaries[i].Share = float64(summaries[i].Quantity) / float64(total)
	}
	return summaries, err
}

// ResourceShares are the shares of a quantity held by each resource, each
// from 0 to 1.
type ResourceShares struct {
	Gold, Fuel, Metals, NonMetals float64
}

// KindDeposits summarizes the deposits on a game's planets of one Kind: the
// number of Planets; the lowest, average, and highest number of deposits
// per planet; the lowest, average, and highest wealth per planet, the sum
// of its deposits' quantities; and the share of the kind's wealth held by
// each resource.
type KindDeposits struct {
	Kind            string
	Planets         int
	MinDeposits     int
	AverageDeposits float64
	MaxDeposits     int
	MinWealth       int64
	AverageWealth   float64
	MaxWealth       int64
	Shares          ResourceShares
}

// DepositsByKind summarizes a game's deposits by the kind of their planets,
// in the order rocky, asteroids, giant. Kinds whose planets have no
// deposits are left out, so a game without deposits has no summaries. It
// returns ErrGameNotFound if there is no such game.
func (s *Store) DepositsByKind(ctx context.Context, gameID int64) (summaries []KindDeposits, err error) {
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
		`SELECT kind, count(*), min(n), avg(n), max(n), min(w), avg(w), max(w), sum(w),
			sum(gold), sum(fuel), sum(mets), sum(nmts)
		 FROM (
			SELECT p.kind, count(d.id) AS n, coalesce(sum(d.quantity), 0) AS w,
				coalesce(sum(CASE d.resource WHEN 'gold' THEN d.quantity END), 0) AS gold,
				coalesce(sum(CASE d.resource WHEN 'fuel' THEN d.quantity END), 0) AS fuel,
				coalesce(sum(CASE d.resource WHEN 'mets' THEN d.quantity END), 0) AS mets,
				coalesce(sum(CASE d.resource WHEN 'nmts' THEN d.quantity END), 0) AS nmts
			FROM planets p JOIN stars s ON s.id = p.star_id
			LEFT JOIN deposits d ON d.planet_id = p.id
			WHERE s.game_id = ?
			GROUP BY p.id)
		 GROUP BY kind
		 HAVING sum(n) > 0
		 ORDER BY CASE kind WHEN 'rocky' THEN 1 WHEN 'asteroids' THEN 2 ELSE 3 END;`,
		&sqlitex.ExecOptions{
			Args: []any{gameID},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				total := float64(stmt.ColumnInt64(8))
				summaries = append(summaries, KindDeposits{
					Kind: stmt.ColumnText(0), Planets: stmt.ColumnInt(1),
					MinDeposits: stmt.ColumnInt(2), AverageDeposits: stmt.ColumnFloat(3), MaxDeposits: stmt.ColumnInt(4),
					MinWealth: stmt.ColumnInt64(5), AverageWealth: stmt.ColumnFloat(6), MaxWealth: stmt.ColumnInt64(7),
					Shares: ResourceShares{
						Gold: float64(stmt.ColumnInt64(9)) / total, Fuel: float64(stmt.ColumnInt64(10)) / total,
						Metals: float64(stmt.ColumnInt64(11)) / total, NonMetals: float64(stmt.ColumnInt64(12)) / total,
					},
				})
				return nil
			},
		})
	return summaries, err
}

// StarSizeWealth summarizes the wealth of a game's stars with one number of
// Planets: the number of Stars, and their average wealth per star and per
// planet, where a star's wealth is the sum of its deposits' quantities.
type StarSizeWealth struct {
	Planets          int
	Stars            int
	AveragePerStar   float64
	AveragePerPlanet float64
}

// DepositsByPlanetsPerStar summarizes the wealth of a game's stars by their
// number of planets, from the fewest to the most. Numbers of planets that
// no star with deposits has are left out, so a game without deposits has no
// summaries. It returns ErrGameNotFound if there is no such game.
func (s *Store) DepositsByPlanetsPerStar(ctx context.Context, gameID int64) (summaries []StarSizeWealth, err error) {
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
		`SELECT planets, count(*), avg(w), CAST(sum(w) AS REAL) / sum(planets)
		 FROM (
			SELECT count(DISTINCT p.id) AS planets, count(d.id) AS n, coalesce(sum(d.quantity), 0) AS w
			FROM stars s
			JOIN planets p ON p.star_id = s.id
			LEFT JOIN deposits d ON d.planet_id = p.id
			WHERE s.game_id = ?
			GROUP BY s.id)
		 WHERE n > 0
		 GROUP BY planets
		 ORDER BY planets;`,
		&sqlitex.ExecOptions{
			Args: []any{gameID},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				summaries = append(summaries, StarSizeWealth{
					Planets: stmt.ColumnInt(0), Stars: stmt.ColumnInt(1),
					AveragePerStar: stmt.ColumnFloat(2), AveragePerPlanet: stmt.ColumnFloat(3),
				})
				return nil
			},
		})
	return summaries, err
}

// QuantityBin is one bar of a histogram of deposit quantities: Count
// deposits hold from Min to Max units, inclusive.
type QuantityBin struct {
	Min, Max int64
	Count    int
}

// quantityEdges returns the lower bounds of the bins of DepositQuantities
// after the lowest: 1, 2, and 5 times each power of 10 from 200,000 to well
// beyond the largest budget.
func quantityEdges() []int64 {
	var edges []int64
	for p := int64(100_000); p <= 1_000_000_000_000; p *= 10 {
		edges = append(edges, 2*p, 5*p, 10*p)
	}
	return edges
}

// DepositQuantities returns a histogram of the quantities of a game's
// deposits, on a logarithmic scale. Each bin runs from 1, 2, or 5 times a
// power of 10 to just below the next such bound, except that the lowest,
// for 100,000, starts at cluster.MinDeposit. The bins run from the one
// holding the smallest deposit to the one holding the largest, including
// bins with a Count of 0. A game without deposits has no bins. It returns
// ErrGameNotFound if there is no such game.
func (s *Store) DepositQuantities(ctx context.Context, gameID int64) (bins []QuantityBin, err error) {
	var quantities []int64
	err = s.gameColumn(ctx, gameID,
		`SELECT d.quantity FROM deposits d JOIN planets p ON p.id = d.planet_id JOIN stars s ON s.id = p.star_id
		 WHERE s.game_id = ?;`,
		func(stmt *sqlite.Stmt) { quantities = append(quantities, stmt.ColumnInt64(0)) })
	if err != nil || len(quantities) == 0 {
		return nil, err
	}
	lo := int64(cluster.MinDeposit)
	for _, edge := range quantityEdges() {
		bins = append(bins, QuantityBin{Min: lo, Max: edge - 1})
		lo = edge
	}
	first, last := len(bins), 0
	for _, q := range quantities {
		i, _ := slices.BinarySearchFunc(bins, q, func(b QuantityBin, q int64) int { return cmp.Compare(b.Max, q) })
		bins[i].Count++
		first, last = min(first, i), max(last, i)
	}
	return bins[first : last+1], nil
}

// ValueBin is one bar of a histogram: Count deposits or planets have the
// Value.
type ValueBin struct {
	Value int
	Count int
}

// DepositYields returns a histogram of the yields of a game's deposits, one
// bin per percentage from 1 to cluster.MaxYield, including bins with a Count
// of 0. A game without deposits has no bins. It returns ErrGameNotFound if
// there is no such game.
func (s *Store) DepositYields(ctx context.Context, gameID int64) ([]ValueBin, error) {
	return s.valueHistogram(ctx, gameID,
		`SELECT d.yield_pct FROM deposits d JOIN planets p ON p.id = d.planet_id JOIN stars s ON s.id = p.star_id
		 WHERE s.game_id = ?;`, cluster.MaxYield)
}

// DepositsPerPlanet returns a histogram of the number of deposits on each of
// a game's planets, one bin per number from 1 to cluster.MostDeposits,
// including bins with a Count of 0. Planets without deposits are not
// counted, so a game without deposits has no bins. It returns
// ErrGameNotFound if there is no such game.
func (s *Store) DepositsPerPlanet(ctx context.Context, gameID int64) ([]ValueBin, error) {
	return s.valueHistogram(ctx, gameID,
		`SELECT count(*) FROM deposits d JOIN planets p ON p.id = d.planet_id JOIN stars s ON s.id = p.star_id
		 WHERE s.game_id = ? GROUP BY d.planet_id;`, cluster.MostDeposits)
}

// valueHistogram tallies the values from 1 to hi that query returns for a
// game.
func (s *Store) valueHistogram(ctx context.Context, gameID int64, query string, hi int) ([]ValueBin, error) {
	var values []int
	err := s.gameColumn(ctx, gameID, query, func(stmt *sqlite.Stmt) { values = append(values, stmt.ColumnInt(0)) })
	if err != nil || len(values) == 0 {
		return nil, err
	}
	bins := make([]ValueBin, hi)
	for i := range bins {
		bins[i].Value = i + 1
	}
	for _, v := range values {
		bins[v-1].Count++
	}
	return bins, nil
}

// gameColumn runs query, which takes a game ID, and passes each row to
// result. It returns ErrGameNotFound if there is no such game.
func (s *Store) gameColumn(ctx context.Context, gameID int64, query string, result func(*sqlite.Stmt)) (err error) {
	conn, err := s.pool.Take(ctx)
	if err != nil {
		return err
	}
	defer s.pool.Put(conn)

	// Read both queries from one snapshot.
	defer sqlitex.Save(conn)(&err)

	if err := gameExists(conn, gameID); err != nil {
		return err
	}
	return sqlitex.Execute(conn, query, &sqlitex.ExecOptions{
		Args: []any{gameID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			result(stmt)
			return nil
		},
	})
}
