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
		-- rather than deleted, so neither cascades.
		CREATE TABLE games (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			is_active  INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
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

		CREATE INDEX seats_game_id ON seats (game_id);`,
	},
}
