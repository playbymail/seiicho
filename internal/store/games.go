// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"context"
	"errors"
	"time"

	"zombiezen.com/go/sqlite/sqlitex"
)

// Game is a newly created game and its gamemaster seat.
type Game struct {
	ID     int64
	SeatID int64
	// GM is the administrator who holds the gamemaster seat.
	GM Account
}

// CreateGame adds a game and seats the senior active administrator, the one
// with the lowest account ID, as its gamemaster. It returns ErrNoActiveAdmin,
// and creates nothing, if there is no active administrator.
func (s *Store) CreateGame(ctx context.Context, now time.Time) (g Game, err error) {
	conn, err := s.pool.Take(ctx)
	if err != nil {
		return Game{}, err
	}
	defer s.pool.Put(conn)

	end, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		return Game{}, err
	}
	defer end(&err)

	g.GM, err = queryAccount(conn,
		`SELECT id, email, password_hash, is_admin, is_active FROM accounts
		 WHERE is_admin = 1 AND is_active = 1 ORDER BY id LIMIT 1;`)
	if errors.Is(err, ErrNotFound) {
		return Game{}, ErrNoActiveAdmin
	} else if err != nil {
		return Game{}, err
	}

	err = sqlitex.Execute(conn, `INSERT INTO games (created_at) VALUES (?);`,
		&sqlitex.ExecOptions{Args: []any{now.Unix()}})
	if err != nil {
		return Game{}, err
	}
	g.ID = conn.LastInsertRowID()

	err = sqlitex.Execute(conn,
		`INSERT INTO seats (game_id, account_id, is_gm, created_at) VALUES (?, ?, 1, ?);`,
		&sqlitex.ExecOptions{Args: []any{g.ID, g.GM.ID, now.Unix()}})
	if err != nil {
		return Game{}, err
	}
	g.SeatID = conn.LastInsertRowID()
	return g, nil
}
