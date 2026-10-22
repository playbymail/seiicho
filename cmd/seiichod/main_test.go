// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/playbymail/seiicho/internal/auth"
	"github.com/playbymail/seiicho/internal/devdata"
	"github.com/playbymail/seiicho/internal/settings"
	"github.com/playbymail/seiicho/internal/store"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

func TestResolveMemoryDatabase(t *testing.T) {
	for _, tc := range []struct {
		env     string
		wantErr string
	}{
		{"development", ""},
		{"agent", ""},
		{"test", "only allowed in development and agent"},
		{"production", "only allowed in development and agent"},
	} {
		s := settings.New("seiichod")
		if err := s.Flags.Parse([]string{"--env", tc.env, "--db", settings.MemoryDB}); err != nil {
			t.Fatal(err)
		}
		_, err := resolve(s, "")
		if tc.wantErr == "" && err != nil {
			t.Errorf("%s: %v", tc.env, err)
		} else if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
			t.Errorf("%s = %v, want %q", tc.env, err, tc.wantErr)
		}
	}
}

func TestOpenStoreMemory(t *testing.T) {
	for _, env := range []string{"development", "agent"} {
		db, err := openStore(t.Context(), env, settings.MemoryDB)
		if err != nil {
			t.Fatalf("%s: %v", env, err)
		}
		for _, want := range devdata.DefaultAccounts {
			a, err := db.AccountByEmail(t.Context(), want.Email)
			if err != nil {
				t.Fatalf("%s: %v", want.Email, err)
			}
			if a.IsAdmin != want.IsAdmin || !auth.CheckPassword(a.PasswordHash, want.Password) {
				t.Errorf("%s: account %+v does not match the default", want.Email, a)
			}
		}
		db.Close()
	}
	for _, env := range []string{"test", "production"} {
		if _, err := openStore(t.Context(), env, settings.MemoryDB); !errors.Is(err, devdata.ErrNotAllowed) {
			t.Errorf("%s: %v, want ErrNotAllowed", env, err)
		}
	}
}

// TestOpenStoreDoesNotMigrate checks that the server refuses an outdated
// database and points at seiichodb instead of migrating it.
func TestOpenStoreDoesNotMigrate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.db")
	conn, err := sqlite.OpenConn(path, sqlite.OpenReadWrite|sqlite.OpenCreate)
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA application_id = 0x73656969;", nil); err != nil {
		t.Fatal(err)
	}
	conn.Close()

	_, err = openStore(t.Context(), "development", path)
	if !errors.Is(err, store.ErrSchemaOutdated) || !strings.Contains(err.Error(), "seiichodb migrate") {
		t.Fatalf("openStore = %v, want ErrSchemaOutdated naming seiichodb migrate", err)
	}
}
