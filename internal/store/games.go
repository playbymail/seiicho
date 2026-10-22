// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"context"
	"errors"
	"time"

	"zombiezen.com/go/sqlite"
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

// AddSeat seats an account in a game as a player, or as a gamemaster if isGM
// is set, and returns the new seat's ID. Both the game and the account must
// exist and be active. It returns ErrSeatExists if the account already has a
// seat in the game, active or not.
func (s *Store) AddSeat(ctx context.Context, gameID, accountID int64, isGM bool, now time.Time) (seatID int64, err error) {
	conn, err := s.pool.Take(ctx)
	if err != nil {
		return 0, err
	}
	defer s.pool.Put(conn)

	end, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		return 0, err
	}
	defer end(&err)

	if err := checkGameAndAccount(conn, gameID, accountID); err != nil {
		return 0, err
	}
	err = sqlitex.Execute(conn,
		`INSERT INTO seats (game_id, account_id, is_gm, created_at) VALUES (?, ?, ?, ?);`,
		&sqlitex.ExecOptions{Args: []any{gameID, accountID, isGM, now.Unix()}})
	if sqlite.ErrCode(err) == sqlite.ResultConstraintUnique {
		return 0, ErrSeatExists
	} else if err != nil {
		return 0, err
	}
	return conn.LastInsertRowID(), nil
}

// PromoteSeat makes an account's seat in a game a gamemaster seat and returns
// the seat's ID. Both the game and the account must exist and be active, and
// the account must have an active seat in the game; otherwise it returns
// ErrNoActiveSeat. promoted is false if the seat was already a gamemaster
// seat, in which case nothing changes.
func (s *Store) PromoteSeat(ctx context.Context, gameID, accountID int64) (seatID int64, promoted bool, err error) {
	conn, err := s.pool.Take(ctx)
	if err != nil {
		return 0, false, err
	}
	defer s.pool.Put(conn)

	end, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		return 0, false, err
	}
	defer end(&err)

	if err := checkGameAndAccount(conn, gameID, accountID); err != nil {
		return 0, false, err
	}
	found, isGM := false, false
	err = sqlitex.Execute(conn,
		`SELECT id, is_gm FROM seats WHERE game_id = ? AND account_id = ? AND is_active = 1;`,
		&sqlitex.ExecOptions{
			Args: []any{gameID, accountID},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				found = true
				seatID, isGM = stmt.ColumnInt64(0), stmt.ColumnBool(1)
				return nil
			},
		})
	if err != nil {
		return 0, false, err
	}
	if !found {
		return 0, false, ErrNoActiveSeat
	}
	if isGM {
		return seatID, false, nil
	}
	err = sqlitex.Execute(conn, `UPDATE seats SET is_gm = 1 WHERE id = ?;`,
		&sqlitex.ExecOptions{Args: []any{seatID}})
	if err != nil {
		return 0, false, err
	}
	return seatID, true, nil
}

// checkGameAndAccount returns ErrGameNotFound, ErrGameInactive,
// ErrAccountNotFound, or ErrAccountInactive unless both the game and the
// account exist and are active.
func checkGameAndAccount(conn *sqlite.Conn, gameID, accountID int64) error {
	for _, c := range []struct {
		query              string
		id                 int64
		notFound, inactive error
	}{
		{`SELECT is_active FROM games WHERE id = ?;`, gameID, ErrGameNotFound, ErrGameInactive},
		{`SELECT is_active FROM accounts WHERE id = ?;`, accountID, ErrAccountNotFound, ErrAccountInactive},
	} {
		found, active := false, false
		err := sqlitex.Execute(conn, c.query, &sqlitex.ExecOptions{
			Args: []any{c.id},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				found, active = true, stmt.ColumnBool(0)
				return nil
			},
		})
		if err != nil {
			return err
		}
		if !found {
			return c.notFound
		}
		if !active {
			return c.inactive
		}
	}
	return nil
}
