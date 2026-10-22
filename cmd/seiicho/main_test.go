// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"path/filepath"
	"strconv"
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

func TestAddAndPromoteSeat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.db")
	db, err := store.Create(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1, 0)
	if _, err := db.CreateAccount(t.Context(), "admin@example.com", []byte("x"), true, now); err != nil {
		t.Fatal(err)
	}
	player, err := db.CreateAccount(t.Context(), "player@example.com", []byte("x"), false, now)
	if err != nil {
		t.Fatal(err)
	}
	retired, err := db.CreateAccount(t.Context(), "retired@example.com", []byte("x"), false, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetAccountActive(t.Context(), retired.Email, false); err != nil {
		t.Fatal(err)
	}
	g, err := db.CreateGame(t.Context(), now)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	args := func(gameID, accountID int64, extra ...string) []string {
		return append([]string{"--env", "test", "--db", path,
			"--game", strconv.FormatInt(gameID, 10), "--account", strconv.FormatInt(accountID, 10)}, extra...)
	}

	for _, cmd := range []func([]string) error{addSeat, promoteSeat} {
		if err := cmd([]string{"--env", "test", "--db", path, "--account", "1"}); err == nil || !strings.Contains(err.Error(), "--game is required") {
			t.Errorf("without --game = %v, want --game is required", err)
		}
		if err := cmd([]string{"--env", "test", "--db", path, "--game", "1"}); err == nil || !strings.Contains(err.Error(), "--account is required") {
			t.Errorf("without --account = %v, want --account is required", err)
		}
		if err := cmd(args(999, player.ID)); !errors.Is(err, store.ErrGameNotFound) {
			t.Errorf("missing game = %v, want ErrGameNotFound", err)
		}
		if err := cmd(args(g.ID, retired.ID)); !errors.Is(err, store.ErrAccountInactive) {
			t.Errorf("inactive account = %v, want ErrAccountInactive", err)
		}
	}

	if err := promoteSeat(args(g.ID, player.ID)); !errors.Is(err, store.ErrNoActiveSeat) {
		t.Errorf("promote-seat before add-seat = %v, want ErrNoActiveSeat", err)
	}
	if err := addSeat(args(g.ID, player.ID)); err != nil {
		t.Fatalf("add-seat = %v", err)
	}
	if err := addSeat(args(g.ID, player.ID, "--gamemaster")); !errors.Is(err, store.ErrSeatExists) {
		t.Errorf("add-seat again = %v, want ErrSeatExists", err)
	}
	if err := promoteSeat(args(g.ID, player.ID)); err != nil {
		t.Errorf("promote-seat = %v", err)
	}
	if err := promoteSeat(args(g.ID, player.ID)); err != nil {
		t.Errorf("promote-seat of a gamemaster = %v, want no-op", err)
	}
}
