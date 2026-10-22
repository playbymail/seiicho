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

	"github.com/peterbourgon/ff/v4"
	"github.com/playbymail/seiicho"
	"github.com/playbymail/seiicho/internal/settings"
	"github.com/playbymail/seiicho/internal/store"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "seiicho: %v\n", err)
		os.Exit(1)
	}
}

// run builds the command tree and runs the command that args select.
func run(ctx context.Context, args []string) error {
	return settings.Run(ctx, &ff.Command{
		Name:      "seiicho",
		Usage:     "seiicho <command> [flags]",
		ShortHelp: "command-line interface to the game engine",
		Subcommands: []*ff.Command{
			createGameCommand(),
			addSeatCommand(),
			promoteSeatCommand(),
			{
				Name:      "version",
				Usage:     "seiicho version",
				ShortHelp: "print the version",
				Exec: settings.NoArgs(func(context.Context) error {
					fmt.Println(seiicho.Version().String())
					return nil
				}),
			},
		},
	}, args)
}

// open resolves the settings and opens the database file.
func open(ctx context.Context, s *settings.Settings) (*store.Store, error) {
	if err := s.Resolve(); err != nil {
		return nil, err
	}
	if s.DB == settings.MemoryDB {
		return nil, fmt.Errorf("%s: %s needs a database file; only seiichod accepts an in-memory database", s.DB, s.Flags.GetName())
	}
	db, err := store.Open(ctx, s.DB)
	if errors.Is(err, store.ErrSchemaOutdated) {
		return nil, fmt.Errorf("%w (run seiichodb migrate)", err)
	}
	return db, err
}

func createGameCommand() *ff.Command {
	s := settings.New("create-game")
	return &ff.Command{
		Name:      "create-game",
		Usage:     "seiicho create-game [--env ENV] [--db PATH]",
		ShortHelp: "create a game with the senior active administrator as gamemaster",
		Flags:     s.Flags,
		Exec: settings.NoArgs(func(ctx context.Context) error {
			db, err := open(ctx, s)
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
		}),
	}
}

// seatSettings are the settings of a command that takes required --game and
// --account flags. open sets gameID and accountID.
type seatSettings struct {
	*settings.Settings
	gameID, accountID int64
	game, account     *int64
}

func newSeatSettings(name string) *seatSettings {
	s := &seatSettings{Settings: settings.New(name)}
	s.game = s.Flags.Int64Config(ff.FlagConfig{
		LongName: "game", Placeholder: "ID", NoDefault: true, Usage: "ID of the game (required)",
	}, 0)
	s.account = s.Flags.Int64Config(ff.FlagConfig{
		LongName: "account", Placeholder: "ID", NoDefault: true, Usage: "ID of the account (required)",
	}, 0)
	return s
}

// open checks the IDs, then resolves the settings and opens the database.
func (s *seatSettings) open(ctx context.Context) (*store.Store, error) {
	s.gameID, s.accountID = *s.game, *s.account
	if s.gameID <= 0 {
		return nil, errors.New("--game is required and must be a positive game ID")
	}
	if s.accountID <= 0 {
		return nil, errors.New("--account is required and must be a positive account ID")
	}
	return open(ctx, s.Settings)
}

func addSeatCommand() *ff.Command {
	s := newSeatSettings("add-seat")
	var isGM bool
	s.Flags.BoolVar(&isGM, 0, "gamemaster", "seat the account as a gamemaster")
	return &ff.Command{
		Name:      "add-seat",
		Usage:     "seiicho add-seat [--env ENV] [--db PATH] --game ID --account ID [--gamemaster]",
		ShortHelp: "seat an account in a game",
		Flags:     s.Flags,
		Exec: settings.NoArgs(func(ctx context.Context) error {
			db, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer db.Close()
			seatID, err := db.AddSeat(ctx, s.gameID, s.accountID, isGM, time.Now())
			if err != nil {
				return fmt.Errorf("game %d, account %d: %w", s.gameID, s.accountID, err)
			}
			role := "player"
			if isGM {
				role = "gamemaster"
			}
			fmt.Printf("seated account %d in game %d as %s (seat %d)\n", s.accountID, s.gameID, role, seatID)
			return nil
		}),
	}
}

func promoteSeatCommand() *ff.Command {
	s := newSeatSettings("promote-seat")
	return &ff.Command{
		Name:      "promote-seat",
		Usage:     "seiicho promote-seat [--env ENV] [--db PATH] --game ID --account ID",
		ShortHelp: "make an account's seat in a game a gamemaster seat",
		Flags:     s.Flags,
		Exec: settings.NoArgs(func(ctx context.Context) error {
			db, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer db.Close()
			seatID, promoted, err := db.PromoteSeat(ctx, s.gameID, s.accountID)
			if err != nil {
				return fmt.Errorf("game %d, account %d: %w", s.gameID, s.accountID, err)
			}
			if promoted {
				fmt.Printf("promoted account %d to gamemaster in game %d (seat %d)\n", s.accountID, s.gameID, seatID)
			} else {
				fmt.Printf("account %d is already a gamemaster in game %d (seat %d)\n", s.accountID, s.gameID, seatID)
			}
			return nil
		}),
	}
}
