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
	"github.com/playbymail/seiicho/internal/settings"
	"github.com/playbymail/seiicho/internal/store"
)

func TestResolveRejectsMemoryDatabase(t *testing.T) {
	for _, env := range []string{"development", "agent", "test", "production"} {
		err := resolve(settings.New("cmd"), []string{"--env", env, "--db", settings.MemoryDB})
		if err == nil || !strings.Contains(err.Error(), "only seiichod accepts") {
			t.Errorf("%s = %v, want rejection", env, err)
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
