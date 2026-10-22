// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import "zombiezen.com/go/sqlite/sqlitemigration"

// schema lists the migrations in order. Never edit a migration that has been
// applied; append a new one instead.
var schema = sqlitemigration.Schema{
	AppID: applicationID,
	Migrations: []string{
		`CREATE TABLE accounts (
			id            INTEGER PRIMARY KEY,
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

		CREATE INDEX sessions_account_id ON sessions (account_id);`,
	},
}
