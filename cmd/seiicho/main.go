// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command seiicho is the command-line interface to the game engine.
//
// Usage:
//
//	seiicho create-game [--env ENV] [--db PATH] --seed SEED [--with-gamemaster ID]
//	seiicho generate-cluster [--env ENV] [--db PATH] --game ID [--sort-stars=BOOL] [--sort-planets=BOOL]
//	seiicho add-seat [--env ENV] [--db PATH] --game ID --account ID [--gamemaster]
//	seiicho promote-seat [--env ENV] [--db PATH] --game ID --account ID
//	seiicho list-accounts [--env ENV] [--db PATH] [--json]
//	seiicho show-account [--env ENV] [--db PATH] --email EMAIL [--json]
//	seiicho list-games [--env ENV] [--db PATH] [--json]
//	seiicho list-seats [--env ENV] [--db PATH] --game ID [--json]
//	seiicho version
//
// --env and --db resolve as described in internal/settings. Every command
// needs an existing database file whose schema is current; use seiichodb to
// create or migrate one.
//
// create-game creates a game and seats the senior active administrator, the
// one with the lowest account ID, as its gamemaster. It fails if there is no
// active administrator. With --with-gamemaster, it seats that account as the
// gamemaster instead; the account must exist and be active, but need not be
// an administrator. --seed is an unsigned 64-bit integer, such as
// 0x0123456789abcdef, that the game's random sources derive from; the same
// seed always generates the same game.
//
// generate-cluster generates a game's cluster of star systems and their
// planets from its seed. By default it stores stars sorted by (x, y, z, seq)
// and planets sorted by star and orbit; --sort-stars=false stores stars in
// draw order, and --sort-planets=false stores planets in allocation order. It
// fails if the game is missing or inactive, or already has a cluster.
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
// administrator and active.
//
// show-account prints the same fields for the account with the given email,
// ignoring case. It fails if there is no such account.
//
// list-games prints every game's ID, whether it is active, when it was created
// (in UTC), its number of active seats, and the emails of its active
// gamemasters.
//
// list-seats prints every seat in a game, active or not: the seat ID, the
// account's ID and email, whether it is a gamemaster or player seat, whether
// it is active, and when it was created (in UTC). It fails if there is no such
// game.
//
// Commands named list print a table, and commands named show print a single
// record. With --json, a list command prints a JSON array instead, and a show
// command prints a JSON object; times are RFC 3339 in UTC. The JSON never
// includes password hashes.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/peterbourgon/ff/v4"
	"github.com/playbymail/seiicho"
	"github.com/playbymail/seiicho/internal/prng"
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
			generateClusterCommand(),
			addSeatCommand(),
			promoteSeatCommand(),
			listAccountsCommand(),
			showAccountCommand(),
			listGamesCommand(),
			listSeatsCommand(),
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
	seed := s.Flags.StringConfig(ff.FlagConfig{
		LongName: "seed", Placeholder: "SEED", NoDefault: true,
		Usage: "unsigned 64-bit seed for the game, such as 0x0123456789abcdef (required)",
	}, "")
	gm := s.Flags.Int64Config(ff.FlagConfig{
		LongName: "with-gamemaster", Placeholder: "ID", NoDefault: true,
		Usage: "ID of the account to seat as gamemaster instead of an administrator",
	}, 0)
	return &ff.Command{
		Name:      "create-game",
		Usage:     "seiicho create-game [--env ENV] [--db PATH] --seed SEED [--with-gamemaster ID]",
		ShortHelp: "create a game with a gamemaster",
		Flags:     s.Flags,
		Exec: settings.NoArgs(func(ctx context.Context) error {
			if *seed == "" {
				return errors.New("--seed is required")
			}
			// Base 0 accepts 0x, 0o, and 0b prefixes as well as decimal.
			u, err := strconv.ParseUint(*seed, 0, 64)
			if err != nil {
				return fmt.Errorf("--seed %q must be an unsigned 64-bit integer, such as 0x0123456789abcdef", *seed)
			}
			seed1, seed2 := prng.SplitSeed(u)
			withGM := false
			if f, ok := s.Flags.GetFlag("with-gamemaster"); ok && f.IsSet() {
				if *gm <= 0 {
					return errors.New("--with-gamemaster must be a positive account ID")
				}
				withGM = true
			}
			db, err := open(ctx, s)
			if err != nil {
				return err
			}
			defer db.Close()
			var g store.Game
			if withGM {
				if g, err = db.CreateGameWithGamemaster(ctx, *gm, seed1, seed2, time.Now()); err != nil {
					return fmt.Errorf("account %d: %w", *gm, err)
				}
			} else if g, err = db.CreateGame(ctx, seed1, seed2, time.Now()); err != nil {
				return err
			}
			fmt.Printf("created game %d with gamemaster %s (seat %d)\n", g.ID, g.GM.Email, g.SeatID)
			return nil
		}),
	}
}

func generateClusterCommand() *ff.Command {
	s := settings.New("generate-cluster")
	game := s.Flags.Int64Config(ff.FlagConfig{
		LongName: "game", Placeholder: "ID", NoDefault: true, Usage: "ID of the game (required)",
	}, 0)
	sortStars := s.Flags.BoolLongDefault("sort-stars", true, "store stars sorted by (x, y, z, seq) instead of in draw order")
	sortPlanets := s.Flags.BoolLongDefault("sort-planets", true, "store planets sorted by star and orbit instead of in allocation order")
	return &ff.Command{
		Name:      "generate-cluster",
		Usage:     "seiicho generate-cluster [--env ENV] [--db PATH] --game ID [--sort-stars=BOOL] [--sort-planets=BOOL]",
		ShortHelp: "generate a game's cluster of star systems",
		Flags:     s.Flags,
		Exec: settings.NoArgs(func(ctx context.Context) error {
			gameID := *game
			if gameID <= 0 {
				return errors.New("--game is required and must be a positive game ID")
			}
			db, err := open(ctx, s)
			if err != nil {
				return err
			}
			defer db.Close()
			stars, planets, err := db.GenerateCluster(ctx, gameID, store.ClusterOptions{
				SortStars: *sortStars, SortPlanets: *sortPlanets,
			})
			if err != nil {
				return fmt.Errorf("game %d: %w", gameID, err)
			}
			fmt.Printf("generated a cluster of %d stars and %d planets for game %d\n", len(stars), len(planets), gameID)
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

// outputSettings are the settings of a command that prints a table, or JSON
// with --json.
type outputSettings struct {
	*settings.Settings
	json bool
}

func newOutputSettings(name string) *outputSettings {
	s := &outputSettings{Settings: settings.New(name)}
	s.Flags.BoolVar(&s.json, 0, "json", "print JSON instead of a table")
	return s
}

// print writes v as JSON with --json, and rows as a table otherwise.
func (s *outputSettings) print(v any, rows [][]string) error {
	if s.json {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	return w.Flush()
}

// accountJSON is an account as list-accounts and show-account print it.
type accountJSON struct {
	ID       int64  `json:"id"`
	Email    string `json:"email"`
	IsAdmin  bool   `json:"is_admin"`
	IsActive bool   `json:"is_active"`
}

func newAccountJSON(a store.Account) accountJSON {
	return accountJSON{ID: a.ID, Email: a.Email, IsAdmin: a.IsAdmin, IsActive: a.IsActive}
}

func listAccountsCommand() *ff.Command {
	s := newOutputSettings("list-accounts")
	return &ff.Command{
		Name:      "list-accounts",
		Usage:     "seiicho list-accounts [--env ENV] [--db PATH] [--json]",
		ShortHelp: "list accounts and their IDs",
		Flags:     s.Flags,
		Exec: settings.NoArgs(func(ctx context.Context) error {
			db, err := open(ctx, s.Settings)
			if err != nil {
				return err
			}
			defer db.Close()
			accounts, err := db.Accounts(ctx)
			if err != nil {
				return err
			}
			out := make([]accountJSON, 0, len(accounts))
			rows := [][]string{{"ID", "EMAIL", "ADMIN", "ACTIVE"}}
			for _, a := range accounts {
				out = append(out, newAccountJSON(a))
				rows = append(rows, []string{fmt.Sprint(a.ID), a.Email, yesNo(a.IsAdmin), yesNo(a.IsActive)})
			}
			return s.print(out, rows)
		}),
	}
}

func showAccountCommand() *ff.Command {
	s := newOutputSettings("show-account")
	email := s.Flags.StringConfig(ff.FlagConfig{
		LongName: "email", Placeholder: "EMAIL", NoDefault: true,
		Usage: "email address of the account (required)",
	}, "")
	return &ff.Command{
		Name:      "show-account",
		Usage:     "seiicho show-account [--env ENV] [--db PATH] --email EMAIL [--json]",
		ShortHelp: "show the account with an email address",
		Flags:     s.Flags,
		Exec: settings.NoArgs(func(ctx context.Context) error {
			e := strings.ToLower(strings.TrimSpace(*email))
			if e == "" {
				return errors.New("--email is required")
			}
			db, err := open(ctx, s.Settings)
			if err != nil {
				return err
			}
			defer db.Close()
			a, err := db.AccountByEmail(ctx, e)
			if errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("%s: %w", e, store.ErrAccountNotFound)
			} else if err != nil {
				return err
			}
			return s.print(newAccountJSON(a), [][]string{
				{"ID", fmt.Sprint(a.ID)},
				{"EMAIL", a.Email},
				{"ADMIN", yesNo(a.IsAdmin)},
				{"ACTIVE", yesNo(a.IsActive)},
			})
		}),
	}
}

// gameJSON is a game as list-games prints it.
type gameJSON struct {
	ID          int64     `json:"id"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	Seats       int       `json:"seats"`
	Gamemasters []string  `json:"gamemasters"`
}

func listGamesCommand() *ff.Command {
	s := newOutputSettings("list-games")
	return &ff.Command{
		Name:      "list-games",
		Usage:     "seiicho list-games [--env ENV] [--db PATH] [--json]",
		ShortHelp: "list games and their IDs",
		Flags:     s.Flags,
		Exec: settings.NoArgs(func(ctx context.Context) error {
			db, err := open(ctx, s.Settings)
			if err != nil {
				return err
			}
			defer db.Close()
			games, err := db.Games(ctx)
			if err != nil {
				return err
			}
			out := make([]gameJSON, 0, len(games))
			rows := [][]string{{"ID", "ACTIVE", "CREATED", "SEATS", "GAMEMASTERS"}}
			for _, g := range games {
				gms := append([]string{}, g.Gamemasters...)
				out = append(out, gameJSON{ID: g.ID, IsActive: g.IsActive, CreatedAt: g.CreatedAt.UTC(), Seats: g.Seats, Gamemasters: gms})
				list := strings.Join(gms, ",")
				if list == "" {
					list = "-"
				}
				rows = append(rows, []string{fmt.Sprint(g.ID), yesNo(g.IsActive), g.CreatedAt.UTC().Format(time.DateTime), fmt.Sprint(g.Seats), list})
			}
			return s.print(out, rows)
		}),
	}
}

// seatJSON is a seat as list-seats prints it.
type seatJSON struct {
	ID        int64     `json:"id"`
	GameID    int64     `json:"game_id"`
	AccountID int64     `json:"account_id"`
	Email     string    `json:"email"`
	IsGM      bool      `json:"is_gm"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

func listSeatsCommand() *ff.Command {
	s := newOutputSettings("list-seats")
	game := s.Flags.Int64Config(ff.FlagConfig{
		LongName: "game", Placeholder: "ID", NoDefault: true, Usage: "ID of the game (required)",
	}, 0)
	return &ff.Command{
		Name:      "list-seats",
		Usage:     "seiicho list-seats [--env ENV] [--db PATH] --game ID [--json]",
		ShortHelp: "list the seats in a game",
		Flags:     s.Flags,
		Exec: settings.NoArgs(func(ctx context.Context) error {
			gameID := *game
			if gameID <= 0 {
				return errors.New("--game is required and must be a positive game ID")
			}
			db, err := open(ctx, s.Settings)
			if err != nil {
				return err
			}
			defer db.Close()
			seats, err := db.Seats(ctx, gameID)
			if err != nil {
				return fmt.Errorf("game %d: %w", gameID, err)
			}
			out := make([]seatJSON, 0, len(seats))
			rows := [][]string{{"SEAT", "ACCOUNT", "EMAIL", "ROLE", "ACTIVE", "CREATED"}}
			for _, st := range seats {
				out = append(out, seatJSON{ID: st.ID, GameID: st.GameID, AccountID: st.AccountID, Email: st.Email,
					IsGM: st.IsGM, IsActive: st.IsActive, CreatedAt: st.CreatedAt.UTC()})
				role := "player"
				if st.IsGM {
					role = "gamemaster"
				}
				rows = append(rows, []string{fmt.Sprint(st.ID), fmt.Sprint(st.AccountID), st.Email, role,
					yesNo(st.IsActive), st.CreatedAt.UTC().Format(time.DateTime)})
			}
			return s.print(out, rows)
		}),
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
