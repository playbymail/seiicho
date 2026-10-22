// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/playbymail/seiicho/internal/settings"
	"github.com/playbymail/seiicho/internal/store"
)

func TestOpenRejectsMemoryDatabase(t *testing.T) {
	for _, env := range []string{"development", "agent", "test", "production"} {
		_, err := open(t.Context(), settings.New("cmd"), []string{"--env", env, "--db", settings.MemoryDB})
		if err == nil || !strings.Contains(err.Error(), "only seiichod accepts") {
			t.Errorf("%s = %v, want rejection", env, err)
		}
	}
}

func TestCreateGame(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.db")
	db, err := store.Create(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	args := []string{"--env", "test", "--db", path}

	if err := createGame(args); !errors.Is(err, store.ErrNoActiveAdmin) {
		t.Fatalf("createGame without an administrator = %v, want ErrNoActiveAdmin", err)
	}

	db, err = store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateAccount(t.Context(), "admin@example.com", []byte("x"), true, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := createGame(args); err != nil {
		t.Fatalf("createGame = %v", err)
	}
}
