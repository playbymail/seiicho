// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command seiicho is the command-line interface to the game engine.
//
// Usage:
//
//	seiicho create-game [--env ENV] [--db PATH]
//	seiicho version
//
// --env and --db resolve as described in internal/settings. Every command
// needs an existing database file whose schema is current; use seiichodb to
// create or migrate one.
//
// create-game creates a game and seats the senior active administrator, the
// one with the lowest account ID, as its gamemaster. It fails if there is no
// active administrator.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/playbymail/seiicho"
	"github.com/playbymail/seiicho/internal/settings"
	"github.com/playbymail/seiicho/internal/store"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "seiicho: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: seiicho create-game|version [flags]")
	}
	switch cmd, args := args[0], args[1:]; cmd {
	case "create-game":
		return createGame(args)
	case "version":
		fmt.Println(seiicho.Version().String())
		return nil
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// open parses and resolves the settings and opens the database file.
func open(ctx context.Context, s *settings.Settings, args []string) (*store.Store, error) {
	if err := s.Parse(args); err != nil {
		return nil, err
	}
	if err := s.Resolve(); err != nil {
		return nil, err
	}
	if s.DB == settings.MemoryDB {
		return nil, fmt.Errorf("%s: %s needs a database file; only seiichod accepts an in-memory database", s.DB, s.FlagSet.Name())
	}
	db, err := store.Open(ctx, s.DB)
	if errors.Is(err, store.ErrSchemaOutdated) {
		return nil, fmt.Errorf("%w (run seiichodb migrate)", err)
	}
	return db, err
}

func createGame(args []string) error {
	ctx := context.Background()
	db, err := open(ctx, settings.New("create-game"), args)
	if err != nil {
		return err
	}
	defer db.Close()
	g, err := db.CreateGame(ctx, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("created game %d with gamemaster %s (seat %d)\n", g.ID, g.GM.Email, g.SeatID)
	return nil
}
