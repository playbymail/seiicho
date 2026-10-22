// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"context"
	"time"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// Account is a registered user.
type Account struct {
	ID           int64
	Email        string
	PasswordHash []byte
	IsAdmin      bool
}

// CreateAccount adds an account. It returns ErrAccountExists, and changes
// nothing, if an account with the same email already exists.
func (s *Store) CreateAccount(ctx context.Context, email string, passwordHash []byte, isAdmin bool, now time.Time) (Account, error) {
	conn, err := s.pool.Take(ctx)
	if err != nil {
		return Account{}, err
	}
	defer s.pool.Put(conn)

	err = sqlitex.Execute(conn,
		`INSERT INTO accounts (email, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?);`,
		&sqlitex.ExecOptions{Args: []any{email, passwordHash, isAdmin, now.Unix()}})
	if sqlite.ErrCode(err) == sqlite.ResultConstraintUnique {
		return Account{}, ErrAccountExists
	} else if err != nil {
		return Account{}, err
	}
	return Account{ID: conn.LastInsertRowID(), Email: email, PasswordHash: passwordHash, IsAdmin: isAdmin}, nil
}

// AccountByEmail returns the account with the given email, ignoring case.
func (s *Store) AccountByEmail(ctx context.Context, email string) (Account, error) {
	conn, err := s.pool.Take(ctx)
	if err != nil {
		return Account{}, err
	}
	defer s.pool.Put(conn)

	return queryAccount(conn,
		`SELECT id, email, password_hash, is_admin FROM accounts WHERE email = ?;`,
		email)
}

// SetPassword replaces the password hash of the account with the given email,
// ignoring case, and ends all of that account's sessions. It returns
// ErrNotFound if there is no such account.
func (s *Store) SetPassword(ctx context.Context, email string, passwordHash []byte) (err error) {
	conn, err := s.pool.Take(ctx)
	if err != nil {
		return err
	}
	defer s.pool.Put(conn)

	end, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		return err
	}
	defer end(&err)

	err = sqlitex.Execute(conn, `UPDATE accounts SET password_hash = ? WHERE email = ?;`,
		&sqlitex.ExecOptions{Args: []any{passwordHash, email}})
	if err != nil {
		return err
	}
	if conn.Changes() == 0 {
		return ErrNotFound
	}
	return sqlitex.Execute(conn,
		`DELETE FROM sessions WHERE account_id = (SELECT id FROM accounts WHERE email = ?);`,
		&sqlitex.ExecOptions{Args: []any{email}})
}

// CreateSession records a session for the account. Only a hash of the
// session token is stored.
func (s *Store) CreateSession(ctx context.Context, tokenHash []byte, accountID int64, now, expires time.Time) error {
	conn, err := s.pool.Take(ctx)
	if err != nil {
		return err
	}
	defer s.pool.Put(conn)

	return sqlitex.Execute(conn,
		`INSERT INTO sessions (token_hash, account_id, created_at, expires_at) VALUES (?, ?, ?, ?);`,
		&sqlitex.ExecOptions{Args: []any{tokenHash, accountID, now.Unix(), expires.Unix()}})
}

// SessionAccount returns the account for an unexpired session.
func (s *Store) SessionAccount(ctx context.Context, tokenHash []byte, now time.Time) (Account, error) {
	conn, err := s.pool.Take(ctx)
	if err != nil {
		return Account{}, err
	}
	defer s.pool.Put(conn)

	return queryAccount(conn,
		`SELECT a.id, a.email, a.password_hash, a.is_admin
		 FROM sessions s JOIN accounts a ON a.id = s.account_id
		 WHERE s.token_hash = ? AND s.expires_at > ?;`,
		tokenHash, now.Unix())
}

// DeleteSession removes a session. Deleting a missing session is not an error.
func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	conn, err := s.pool.Take(ctx)
	if err != nil {
		return err
	}
	defer s.pool.Put(conn)

	return sqlitex.Execute(conn, `DELETE FROM sessions WHERE token_hash = ?;`,
		&sqlitex.ExecOptions{Args: []any{tokenHash}})
}

func queryAccount(conn *sqlite.Conn, query string, args ...any) (Account, error) {
	var a Account
	found := false
	err := sqlitex.Execute(conn, query, &sqlitex.ExecOptions{
		Args: args,
		ResultFunc: func(stmt *sqlite.Stmt) error {
			found = true
			a.ID = stmt.ColumnInt64(0)
			a.Email = stmt.ColumnText(1)
			a.PasswordHash = make([]byte, stmt.ColumnLen(2))
			stmt.ColumnBytes(2, a.PasswordHash)
			a.IsAdmin = stmt.ColumnBool(3)
			return nil
		},
	})
	if err != nil {
		return Account{}, err
	}
	if !found {
		return Account{}, ErrNotFound
	}
	return a, nil
}
