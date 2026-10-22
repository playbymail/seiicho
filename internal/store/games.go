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
	// GM is the account that holds the gamemaster seat.
	GM Account
}

// CreateGame adds a game, seeded with seed1 and seed2, and seats the senior
// active administrator, the one with the lowest account ID, as its
// gamemaster. It returns ErrNoActiveAdmin, and creates nothing, if there is
// no active administrator.
func (s *Store) CreateGame(ctx context.Context, seed1, seed2 uint64, now time.Time) (Game, error) {
	return s.createGame(ctx, seed1, seed2, now, func(conn *sqlite.Conn) (Account, error) {
		a, err := queryAccount(conn,
			`SELECT id, email, password_hash, is_admin, is_active FROM accounts
			 WHERE is_admin = 1 AND is_active = 1 ORDER BY id LIMIT 1;`)
		if errors.Is(err, ErrNotFound) {
			return Account{}, ErrNoActiveAdmin
		}
		return a, err
	})
}

// CreateGameWithGamemaster adds a game, seeded with seed1 and seed2, and
// seats the given account, which need not be an administrator, as its
// gamemaster. It returns ErrAccountNotFound or ErrAccountInactive, and
// creates nothing, unless the account exists and is active.
func (s *Store) CreateGameWithGamemaster(ctx context.Context, accountID int64, seed1, seed2 uint64, now time.Time) (Game, error) {
	return s.createGame(ctx, seed1, seed2, now, func(conn *sqlite.Conn) (Account, error) {
		a, err := queryAccount(conn,
			`SELECT id, email, password_hash, is_admin, is_active FROM accounts WHERE id = ?;`,
			accountID)
		if errors.Is(err, ErrNotFound) {
			return Account{}, ErrAccountNotFound
		} else if err != nil {
			return Account{}, err
		}
		if !a.IsActive {
			return Account{}, ErrAccountInactive
		}
		return a, nil
	})
}

// createGame adds a game and seats the account that gamemaster returns as its
// gamemaster, all in one transaction. If gamemaster fails, nothing is created.
func (s *Store) createGame(ctx context.Context, seed1, seed2 uint64, now time.Time, gamemaster func(*sqlite.Conn) (Account, error)) (g Game, err error) {
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

	if g.GM, err = gamemaster(conn); err != nil {
		return Game{}, err
	}

	// The columns are signed; store each seed's bits unchanged.
	err = sqlitex.Execute(conn, `INSERT INTO games (seed1, seed2, created_at) VALUES (?, ?, ?);`,
		&sqlitex.ExecOptions{Args: []any{int64(seed1), int64(seed2), now.Unix()}})
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

// GameSummary describes a game for an operator.
type GameSummary struct {
	ID        int64
	IsActive  bool
	CreatedAt time.Time
	// Seats counts the game's active seats.
	Seats int
	// Gamemasters are the emails of the accounts holding the game's active
	// gamemaster seats, ordered by seat ID.
	Gamemasters []string
}

// Games returns every game, active or not, ordered by ID.
func (s *Store) Games(ctx context.Context) (games []GameSummary, err error) {
	conn, err := s.pool.Take(ctx)
	if err != nil {
		return nil, err
	}
	defer s.pool.Put(conn)

	// Read both queries from one snapshot.
	defer sqlitex.Save(conn)(&err)

	index := map[int64]int{}
	err = sqlitex.Execute(conn,
		`SELECT g.id, g.is_active, g.created_at,
		        (SELECT count(*) FROM seats s WHERE s.game_id = g.id AND s.is_active = 1)
		 FROM games g ORDER BY g.id;`,
		&sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				index[stmt.ColumnInt64(0)] = len(games)
				games = append(games, GameSummary{
					ID:        stmt.ColumnInt64(0),
					IsActive:  stmt.ColumnBool(1),
					CreatedAt: time.Unix(stmt.ColumnInt64(2), 0),
					Seats:     stmt.ColumnInt(3),
				})
				return nil
			},
		})
	if err != nil {
		return nil, err
	}
	err = sqlitex.Execute(conn,
		`SELECT s.game_id, a.email FROM seats s JOIN accounts a ON a.id = s.account_id
		 WHERE s.is_active = 1 AND s.is_gm = 1 ORDER BY s.id;`,
		&sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				g := &games[index[stmt.ColumnInt64(0)]]
				g.Gamemasters = append(g.Gamemasters, stmt.ColumnText(1))
				return nil
			},
		})
	if err != nil {
		return nil, err
	}
	return games, nil
}

// Seat is an account's seat in a game.
type Seat struct {
	ID        int64
	GameID    int64
	AccountID int64
	// Email is the email of the account holding the seat.
	Email     string
	IsGM      bool
	IsActive  bool
	CreatedAt time.Time
}

// Seats returns every seat in a game, active or not, ordered by seat ID. The
// game may be inactive; it returns ErrGameNotFound if there is no such game.
func (s *Store) Seats(ctx context.Context, gameID int64) (seats []Seat, err error) {
	conn, err := s.pool.Take(ctx)
	if err != nil {
		return nil, err
	}
	defer s.pool.Put(conn)

	// Read both queries from one snapshot.
	defer sqlitex.Save(conn)(&err)

	found := false
	err = sqlitex.Execute(conn, `SELECT 1 FROM games WHERE id = ?;`, &sqlitex.ExecOptions{
		Args: []any{gameID},
		ResultFunc: func(*sqlite.Stmt) error {
			found = true
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrGameNotFound
	}
	err = sqlitex.Execute(conn,
		`SELECT s.id, s.game_id, s.account_id, a.email, s.is_gm, s.is_active, s.created_at
		 FROM seats s JOIN accounts a ON a.id = s.account_id
		 WHERE s.game_id = ? ORDER BY s.id;`,
		&sqlitex.ExecOptions{
			Args: []any{gameID},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				seats = append(seats, Seat{
					ID:        stmt.ColumnInt64(0),
					GameID:    stmt.ColumnInt64(1),
					AccountID: stmt.ColumnInt64(2),
					Email:     stmt.ColumnText(3),
					IsGM:      stmt.ColumnBool(4),
					IsActive:  stmt.ColumnBool(5),
					CreatedAt: time.Unix(stmt.ColumnInt64(6), 0),
				})
				return nil
			},
		})
	if err != nil {
		return nil, err
	}
	return seats, nil
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
