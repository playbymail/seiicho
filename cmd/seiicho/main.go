// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command seiicho is the command-line interface to the game engine.
//
// Usage:
//
//	seiicho create-game [--env ENV] [--db PATH]
//	seiicho add-seat [--env ENV] [--db PATH] --game ID --account ID [--gamemaster]
//	seiicho promote-seat [--env ENV] [--db PATH] --game ID --account ID
//	seiicho version
//
// --env and --db resolve as described in internal/settings. Every command
// needs an existing database file whose schema is current; use seiichodb to
// create or migrate one.
//
// create-game creates a game and seats the senior active administrator, the
// one with the lowest account ID, as its gamemaster. It fails if there is no
// active administrator.
//
// add-seat seats an account in a game as a player, or as a gamemaster with
// --gamemaster. It fails if the game or the account is missing or inactive,
// or if the account already has a seat in the game, active or not.
//
// promote-seat makes an account's seat in a game a gamemaster seat. It fails
// if the game or the account is missing or inactive, or if the account has no
// active seat in the game. Promoting a gamemaster changes nothing.
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
		return errors.New("usage: seiicho create-game|add-seat|promote-seat|version [flags]")
	}
	switch cmd, args := args[0], args[1:]; cmd {
	case "create-game":
		return createGame(args)
	case "add-seat":
		return addSeat(args)
	case "promote-seat":
		return promoteSeat(args)
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

// openSeat is open for commands that take required --game and --account
// flags. Register any further flags on s before calling it.
func openSeat(ctx context.Context, s *settings.Settings, args []string) (db *store.Store, gameID, accountID int64, err error) {
	s.FlagSet.Int64Var(&gameID, "game", 0, "ID of the game")
	s.FlagSet.Int64Var(&accountID, "account", 0, "ID of the account")
	db, err = open(ctx, s, args)
	if err != nil {
		return nil, 0, 0, err
	}
	if gameID <= 0 {
		err = errors.New("--game is required and must be a positive game ID")
	} else if accountID <= 0 {
		err = errors.New("--account is required and must be a positive account ID")
	}
	if err != nil {
		db.Close()
		return nil, 0, 0, err
	}
	return db, gameID, accountID, nil
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

func addSeat(args []string) error {
	ctx := context.Background()
	s := settings.New("add-seat")
	var isGM bool
	s.FlagSet.BoolVar(&isGM, "gamemaster", false, "seat the account as a gamemaster")
	db, gameID, accountID, err := openSeat(ctx, s, args)
	if err != nil {
		return err
	}
	defer db.Close()
	seatID, err := db.AddSeat(ctx, gameID, accountID, isGM, time.Now())
	if err != nil {
		return fmt.Errorf("game %d, account %d: %w", gameID, accountID, err)
	}
	role := "player"
	if isGM {
		role = "gamemaster"
	}
	fmt.Printf("seated account %d in game %d as %s (seat %d)\n", accountID, gameID, role, seatID)
	return nil
}

func promoteSeat(args []string) error {
	ctx := context.Background()
	db, gameID, accountID, err := openSeat(ctx, settings.New("promote-seat"), args)
	if err != nil {
		return err
	}
	defer db.Close()
	seatID, promoted, err := db.PromoteSeat(ctx, gameID, accountID)
	if err != nil {
		return fmt.Errorf("game %d, account %d: %w", gameID, accountID, err)
	}
	if promoted {
		fmt.Printf("promoted account %d to gamemaster in game %d (seat %d)\n", accountID, gameID, seatID)
	} else {
		fmt.Printf("account %d is already a gamemaster in game %d (seat %d)\n", accountID, gameID, seatID)
	}
	return nil
}
