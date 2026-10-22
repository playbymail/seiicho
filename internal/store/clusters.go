// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"context"

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

// GenerateCluster generates a game's cluster and its planets from the game's
// seeds and stores them, returning them in the order stored; each planet's
// Star indexes the returned stars. The game must exist and be active;
// otherwise it returns ErrGameNotFound or ErrGameInactive. It returns
// ErrClusterExists if the game already has stars. On error, it stores
// nothing.
func (s *Store) GenerateCluster(ctx context.Context, gameID int64, opts ClusterOptions) (stars []cluster.Star, planets []cluster.Planet, err error) {
	conn, err := s.pool.Take(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer s.pool.Put(conn)

	end, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		return nil, nil, err
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
		return nil, nil, err
	}
	switch {
	case !found:
		return nil, nil, ErrGameNotFound
	case !active:
		return nil, nil, ErrGameInactive
	case hasStars:
		return nil, nil, ErrClusterExists
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
			return nil, nil, err
		}
		starIDs[i] = conn.LastInsertRowID()
	}

	planets = cluster.Planets(stars, root.Child(prng.Planets).PRNG())
	if opts.SortPlanets {
		cluster.SortPlanets(stars, planets)
	}
	for _, p := range planets {
		err = sqlitex.Execute(conn,
			`INSERT INTO planets (star_id, orbit, kind) VALUES (?, ?, ?);`,
			&sqlitex.ExecOptions{Args: []any{starIDs[p.Star], p.Orbit, p.Kind}})
		if err != nil {
			return nil, nil, err
		}
	}
	return stars, planets, nil
}
