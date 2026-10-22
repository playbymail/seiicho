// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"context"

	"zombiezen.com/go/sqlite"
)

// Conn borrows a connection so tests can inspect connection state.
func (s *Store) Conn(ctx context.Context) (*sqlite.Conn, error) { return s.pool.Take(ctx) }

// Put returns a connection borrowed with Conn.
func (s *Store) Put(conn *sqlite.Conn) { s.pool.Put(conn) }
