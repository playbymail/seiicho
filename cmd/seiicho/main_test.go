// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/playbymail/seiicho/internal/auth"
	"github.com/playbymail/seiicho/internal/devdata"
	"github.com/playbymail/seiicho/internal/store"
)

func TestResolveMemoryDatabase(t *testing.T) {
	for _, tc := range []struct {
		env         string
		allowMemory bool
		wantErr     string
	}{
		{"development", true, ""},
		{"agent", true, ""},
		{"test", true, "only allowed in development and agent"},
		{"production", true, "only allowed in development and agent"},
		{"development", false, "only serve accepts"},
		{"agent", false, "only serve accepts"},
	} {
		s := newSettings("cmd")
		s.allowMemory = tc.allowMemory
		err := s.resolve([]string{"--env", tc.env, "--db", memoryDB})
		if tc.wantErr == "" && err != nil {
			t.Errorf("%s (allowMemory=%v): %v", tc.env, tc.allowMemory, err)
		} else if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
			t.Errorf("%s (allowMemory=%v) = %v, want %q", tc.env, tc.allowMemory, err, tc.wantErr)
		}
	}
}

func TestOpenServeStoreMemory(t *testing.T) {
	for _, env := range []string{"development", "agent"} {
		db, err := openServeStore(t.Context(), env, memoryDB)
		if err != nil {
			t.Fatalf("%s: %v", env, err)
		}
		assertDefaultAccounts(t, db)
		db.Close()
	}
	for _, env := range []string{"test", "production"} {
		if _, err := openServeStore(t.Context(), env, memoryDB); !errors.Is(err, devdata.ErrNotAllowed) {
			t.Errorf("%s: %v, want ErrNotAllowed", env, err)
		}
	}
}

func TestCreateStoreDefaultAccounts(t *testing.T) {
	for _, env := range []string{"development", "agent"} {
		path := filepath.Join(t.TempDir(), "game.db")
		db, err := createStore(t.Context(), env, path)
		if err != nil {
			t.Fatalf("%s: %v", env, err)
		}
		assertDefaultAccounts(t, db)
		db.Close()
	}
	for _, env := range []string{"test", "production"} {
		path := filepath.Join(t.TempDir(), "game.db")
		db, err := createStore(t.Context(), env, path)
		if err != nil {
			t.Fatalf("%s: %v", env, err)
		}
		if _, err := db.AccountByEmail(t.Context(), "admin@example.com"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("%s: default account created", env)
		}
		db.Close()
	}
}

func TestCreateStoreKeepsExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.db")
	db, err := store.Create(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := createStore(t.Context(), "development", path); !errors.Is(err, store.ErrDatabaseExists) {
		t.Fatalf("createStore = %v, want ErrDatabaseExists", err)
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		t.Fatal("createStore removed a database it did not create")
	}
}

func assertDefaultAccounts(t *testing.T, db *store.Store) {
	t.Helper()
	for _, want := range devdata.DefaultAccounts {
		a, err := db.AccountByEmail(t.Context(), want.Email)
		if err != nil {
			t.Fatalf("%s: %v", want.Email, err)
		}
		if a.IsAdmin != want.IsAdmin || !auth.CheckPassword(a.PasswordHash, want.Password) {
			t.Errorf("%s: account %+v does not match the default", want.Email, a)
		}
	}
}
