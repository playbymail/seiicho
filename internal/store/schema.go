// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import "zombiezen.com/go/sqlite/sqlitemigration"

// schema lists the migrations in order. Never edit a migration that has been
// applied; append a new one instead.
//
// IDs are never reused, which needs AUTOINCREMENT: without it SQLite can
// reissue the ID of the most recently inserted row once that row is deleted.
var schema = sqlitemigration.Schema{
	AppID: applicationID,
	Migrations: []string{
		`CREATE TABLE accounts (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			email         TEXT    NOT NULL UNIQUE COLLATE NOCASE,
			password_hash BLOB    NOT NULL,
			is_admin      INTEGER NOT NULL DEFAULT 0 CHECK (is_admin IN (0, 1)),
			is_active     INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
			created_at    INTEGER NOT NULL
		) STRICT;

		CREATE TABLE sessions (
			token_hash BLOB    PRIMARY KEY,
			account_id INTEGER NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL
		) STRICT;

		CREATE INDEX sessions_account_id ON sessions (account_id);

		-- A game is one instance of the game. Games and seats are deactivated
		-- rather than deleted, so neither cascades. seed1 and seed2 seed the
		-- game's random sources (see internal/prng). They are uint64 values
		-- stored as the int64 with the same bits, so they may read as negative.
		CREATE TABLE games (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			is_active  INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
			seed1      INTEGER NOT NULL,
			seed2      INTEGER NOT NULL,
			created_at INTEGER NOT NULL
		) STRICT;

		-- A seat is an account's place in a game, as a player or a gamemaster.
		-- Child tables refer to it as seat_id.
		CREATE TABLE seats (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			game_id    INTEGER NOT NULL REFERENCES games (id),
			account_id INTEGER NOT NULL REFERENCES accounts (id),
			is_active  INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
			is_gm      INTEGER NOT NULL DEFAULT 0 CHECK (is_gm IN (0, 1)),
			created_at INTEGER NOT NULL,
			UNIQUE (account_id, game_id)
		) STRICT;

		CREATE INDEX seats_game_id ON seats (game_id);

		-- A star system is the set of a game's stars at one (x, y, z); seq
		-- letters its stars from A. A game's cluster is the set of its stars,
		-- so a game has a board exactly when it has stars. The unique key
		-- also serves lookups by game_id.
		CREATE TABLE stars (
			id      INTEGER PRIMARY KEY AUTOINCREMENT,
			game_id INTEGER NOT NULL REFERENCES games (id),
			x       INTEGER NOT NULL CHECK (x BETWEEN 0 AND 30),
			y       INTEGER NOT NULL CHECK (y BETWEEN 0 AND 30),
			z       INTEGER NOT NULL CHECK (z BETWEEN 0 AND 30),
			seq     TEXT    NOT NULL CHECK (seq IN ('A', 'B', 'C', 'D', 'E')),
			UNIQUE (game_id, x, y, z, seq)
		) STRICT;`,
	},
}
