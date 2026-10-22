// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command seiicho is the command-line interface to the game engine.
//
// Usage:
//
//	seiicho create-game [--env ENV] [--db PATH]
//	seiicho add-seat [--env ENV] [--db PATH] --game ID --account ID [--gamemaster]
//	seiicho promote-seat [--env ENV] [--db PATH] --game ID --account ID
//	seiicho list-accounts [--env ENV] [--db PATH] [--email EMAIL]
//	seiicho list-games [--env ENV] [--db PATH]
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
//
// list-accounts prints every account's ID, email, and whether it is an
// administrator and active. With --email it prints only the account with that
// email, ignoring case, and fails if there is none.
//
// list-games prints every game's ID, whether it is active, when it was created
// (in UTC), its number of active seats, and the emails of its active
// gamemasters.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/peterbourgon/ff/v4"
	"github.com/playbymail/seiicho"
	"github.com/playbymail/seiicho/internal/settings"
	"github.com/playbymail/seiicho/internal/store"
)

// stdout receives the listings, so that tests can capture them.
var stdout io.Writer = os.Stdout

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
			listAccountsCommand(),
			listGamesCommand(),
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

func listAccountsCommand() *ff.Command {
	s := settings.New("list-accounts")
	email := s.Flags.StringConfig(ff.FlagConfig{
		LongName: "email", Placeholder: "EMAIL", NoDefault: true,
		Usage: "list only the account with this email address",
	}, "")
	return &ff.Command{
		Name:      "list-accounts",
		Usage:     "seiicho list-accounts [--env ENV] [--db PATH] [--email EMAIL]",
		ShortHelp: "list accounts and their IDs",
		Flags:     s.Flags,
		Exec: settings.NoArgs(func(ctx context.Context) error {
			db, err := open(ctx, s)
			if err != nil {
				return err
			}
			defer db.Close()
			var accounts []store.Account
			if e := strings.ToLower(strings.TrimSpace(*email)); e != "" {
				a, err := db.AccountByEmail(ctx, e)
				if errors.Is(err, store.ErrNotFound) {
					return fmt.Errorf("%s: %w", e, store.ErrAccountNotFound)
				} else if err != nil {
					return err
				}
				accounts = append(accounts, a)
			} else if accounts, err = db.Accounts(ctx); err != nil {
				return err
			}
			w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tEMAIL\tADMIN\tACTIVE")
			for _, a := range accounts {
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", a.ID, a.Email, yesNo(a.IsAdmin), yesNo(a.IsActive))
			}
			return w.Flush()
		}),
	}
}

func listGamesCommand() *ff.Command {
	s := settings.New("list-games")
	return &ff.Command{
		Name:      "list-games",
		Usage:     "seiicho list-games [--env ENV] [--db PATH]",
		ShortHelp: "list games and their IDs",
		Flags:     s.Flags,
		Exec: settings.NoArgs(func(ctx context.Context) error {
			db, err := open(ctx, s)
			if err != nil {
				return err
			}
			defer db.Close()
			games, err := db.Games(ctx)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tACTIVE\tCREATED\tSEATS\tGAMEMASTERS")
			for _, g := range games {
				gms := strings.Join(g.Gamemasters, ",")
				if gms == "" {
					gms = "-"
				}
				fmt.Fprintf(w, "%d\t%s\t%s\t%d\t%s\n",
					g.ID, yesNo(g.IsActive), g.CreatedAt.UTC().Format(time.DateTime), g.Seats, gms)
			}
			return w.Flush()
		}),
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
