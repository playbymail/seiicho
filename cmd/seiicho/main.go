// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command seiicho is the command-line interface to the game engine.
//
// Usage:
//
//	seiicho create-game [--env ENV] [--db PATH] --seed SEED [--with-gamemaster ID]
//	seiicho generate-cluster [--env ENV] [--db PATH] --game ID [--sort-stars=BOOL] [--sort-planets=BOOL]
//	seiicho flag-home-world [--env ENV] [--db PATH] --game ID --min-home-distance N --max-neighbor-distance N [--json]
//	seiicho add-seat [--env ENV] [--db PATH] --game ID --account ID [--gamemaster]
//	seiicho promote-seat [--env ENV] [--db PATH] --game ID --account ID
//	seiicho assign-seat [--env ENV] [--db PATH] --game ID --seat ID [--planet ID] [--name NAME] [--json]
//	seiicho list-accounts [--env ENV] [--db PATH] [--json]
//	seiicho show-account [--env ENV] [--db PATH] --email EMAIL [--json]
//	seiicho list-games [--env ENV] [--db PATH] [--json]
//	seiicho list-seats [--env ENV] [--db PATH] --game ID [--json]
//	seiicho show-histogram [--env ENV] [--db PATH] --game ID (--planets-per-star | --planets-per-star-system | --deposit-quantity | --deposit-yield | --deposits-per-planet) [--json]
//	seiicho summarize-planets [--env ENV] [--db PATH] --game ID --by-planet-type [--json]
//	seiicho summarize-deposits [--env ENV] [--db PATH] --game ID (--by-resource | --by-planet-type | --by-planets-per-star) [--json]
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
// generate-cluster generates a game's cluster of star systems, their
// planets, and the planets' deposits from its seed. By default it stores
// stars sorted by (x, y, z, seq) and planets sorted by star and orbit;
// --sort-stars=false stores stars in draw order, and --sort-planets=false
// stores planets in allocation order. Deposits are stored grouped by planet,
// in the planets' order. It fails if the game is missing or inactive, or
// already has a cluster.
//
// flag-home-world flags one planet of a game's cluster as a home world, as
// internal/cluster/home-worlds.md describes: a rocky planet in orbit 3, 4,
// or 5 of the only star in its system, at least --min-home-distance (3 to
// 16) from every home system and at most --max-neighbor-distance (1 to 16)
// from the nearest other system that is not one. It sets the planet's
// habitability to 25 and raises its deposits' yields and quantities. It
// prints the planet's ID, its system's coordinates, its star's letter, and
// its orbit. It fails, changing nothing, if the game is missing or
// inactive, has no cluster, or has no planet that qualifies. Each run starts
// the same random source afresh, so a failed run can be retried with looser
// limits, and the same runs in the same order flag the same planets.
//
// add-seat seats an account in a game as a player, or as a gamemaster with
// --gamemaster. It fails if the game or the account is missing or inactive,
// or if the account already has a seat in the game, active or not.
//
// promote-seat makes an account's seat in a game a gamemaster seat. It fails
// if the game or the account is missing or inactive, if the account has no
// active seat in the game, or if the seat has an empire: gamemasters never
// play. Promoting a gamemaster changes nothing.
//
// assign-seat creates a seat's empire on a home world, as
// internal/store/seat-assignment.md describes. --planet names the home
// world; without it, the game must have exactly one open home world, one with
// fewer than 15 seats assigned, and the seat goes there. The first seat on a
// home world creates its race. --name names the empire, which is otherwise
// Unnamed Faction; it is cleaned as internal/names describes and must then be
// 1 to 25 printable ASCII characters other than '"' and '\'. It prints the
// empire's faction ID and name, its race's ID, and its home world's planet
// ID, system coordinates, star letter, and orbit. It fails if the game is
// missing or inactive; if the seat is not an active player seat in the game
// without an empire; if the planet is not a home world in the game or has 15
// seats; or, without --planet, if the game has no open home world or more
// than one, which it lists.
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
// it is active, when it was created (in UTC), and, if it has an empire, the
// empire's faction ID and name and its home world's planet ID. It fails if
// there is no such game.
//
// show-histogram prints a histogram of a game's cluster: with
// --planets-per-star, how many stars have each number of planets; with
// --planets-per-star-system, how many star systems do; with
// --deposit-quantity, how many deposits hold each range of quantities; with
// --deposit-yield, how many deposits have each yield; and with
// --deposits-per-planet, how many planets have each number of deposits.
// Give exactly one of them.
//
// For planets, each row is a number of planets, from the fewest to the most,
// its count, and a bar of that many #s; numbers that no star or system has
// are listed with a count of 0. For deposits, the bars are scaled so that
// the longest has 50 #s, and any count above 0 has at least one. Quantities
// are binned on a logarithmic scale, from 1, 2, or 5 times a power of 10 up
// to the next such bound, each row labeled with its lower bound, such as
// 2M; the 100K bin starts at the smallest deposit, 99,999. The bins run from
// the smallest deposit's to the largest's. Yields have one row per
// percentage from 1 to 25, or to the highest yield if a home world's is
// higher, and deposits per planet one row per number from 1 to 45, including
// those with a count of 0. A game with no cluster, or
// whose cluster has no deposits, has no rows. It fails if there is no such
// game.
//
// summarize-planets prints a summary of a game's planets. --by-planet-type,
// which is required, gives one row per planet type, in the order rocky,
// asteroids, giant: the type, its number of planets, and the lowest,
// average, and highest habitability of those planets. Types that no planet
// has are left out, so a game with no cluster has no rows. It fails if there
// is no such game.
//
// summarize-deposits prints a summary of a game's deposits. Give exactly one
// of these:
//
//   - --by-resource gives one row per resource, in the order gold, fuel,
//     mets, nmts: its total quantity, its share of the game's total, its
//     number of deposits, and the lowest, average, and highest quantity of
//     it per star, counting stars without it as 0.
//   - --by-planet-type gives one row per planet type, in the order rocky,
//     asteroids, giant: its number of planets; the lowest, average, and
//     highest number of deposits per planet; the lowest, average, and
//     highest wealth per planet, the sum of its deposits; and each
//     resource's share of the type's wealth.
//   - --by-planets-per-star gives one row for each number of planets that a
//     star has, from the fewest to the most: the number of such stars, and
//     their average wealth per star and per planet.
//
// Resources and types without deposits, and numbers of planets that no star
// has, are left out, so a game with no cluster, or whose cluster has no
// deposits, has no rows. It fails if there is no such game.
//
// Commands named list print a table, commands named show print a single
// record, and commands named summarize print a table of totals. With --json,
// a list command prints a JSON array instead, and a show or summarize
// command, flag-home-world, or assign-seat prints a JSON object; times are
// RFC 3339 in UTC. The JSON never includes password hashes.
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
	"github.com/playbymail/seiicho/internal/cluster"
	"github.com/playbymail/seiicho/internal/names"
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
			flagHomeWorldCommand(),
			addSeatCommand(),
			promoteSeatCommand(),
			assignSeatCommand(),
			listAccountsCommand(),
			showAccountCommand(),
			listGamesCommand(),
			listSeatsCommand(),
			showHistogramCommand(),
			summarizePlanetsCommand(),
			summarizeDepositsCommand(),
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
			stars, planets, deposits, err := db.GenerateCluster(ctx, gameID, store.ClusterOptions{
				SortStars: *sortStars, SortPlanets: *sortPlanets,
			})
			if err != nil {
				return fmt.Errorf("game %d: %w", gameID, err)
			}
			fmt.Printf("generated a cluster of %d stars, %d planets, and %d deposits for game %d\n", len(stars), len(planets), len(deposits), gameID)
			return nil
		}),
	}
}

// homeWorldJSON is a home world as flag-home-world prints it.
type homeWorldJSON struct {
	GameID   int64  `json:"game_id"`
	PlanetID int64  `json:"planet_id"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	Z        int    `json:"z"`
	Star     string `json:"star"`
	Orbit    int    `json:"orbit"`
}

func flagHomeWorldCommand() *ff.Command {
	s := newOutputSettings("flag-home-world")
	game := s.Flags.Int64Config(ff.FlagConfig{
		LongName: "game", Placeholder: "ID", NoDefault: true, Usage: "ID of the game (required)",
	}, 0)
	minHome := s.Flags.IntConfig(ff.FlagConfig{
		LongName: "min-home-distance", Placeholder: "N", NoDefault: true,
		Usage: fmt.Sprintf("least distance from every home system, %d to %d (required)", cluster.MinHomeDistanceLo, cluster.MinHomeDistanceHi),
	}, 0)
	maxNeighbor := s.Flags.IntConfig(ff.FlagConfig{
		LongName: "max-neighbor-distance", Placeholder: "N", NoDefault: true,
		Usage: fmt.Sprintf("most distance to the nearest non-home system, %d to %d (required)", cluster.MaxNeighborDistanceLo, cluster.MaxNeighborDistanceHi),
	}, 0)
	return &ff.Command{
		Name:      "flag-home-world",
		Usage:     "seiicho flag-home-world [--env ENV] [--db PATH] --game ID --min-home-distance N --max-neighbor-distance N [--json]",
		ShortHelp: "flag a planet of a game's cluster as a home world",
		Flags:     s.Flags,
		Exec: settings.NoArgs(func(ctx context.Context) error {
			gameID := *game
			if gameID <= 0 {
				return errors.New("--game is required and must be a positive game ID")
			}
			if *minHome < cluster.MinHomeDistanceLo || *minHome > cluster.MinHomeDistanceHi {
				return fmt.Errorf("--min-home-distance is required and must be from %d to %d", cluster.MinHomeDistanceLo, cluster.MinHomeDistanceHi)
			}
			if *maxNeighbor < cluster.MaxNeighborDistanceLo || *maxNeighbor > cluster.MaxNeighborDistanceHi {
				return fmt.Errorf("--max-neighbor-distance is required and must be from %d to %d", cluster.MaxNeighborDistanceLo, cluster.MaxNeighborDistanceHi)
			}
			db, err := open(ctx, s.Settings)
			if err != nil {
				return err
			}
			defer db.Close()
			h, err := db.FlagHomeWorld(ctx, gameID, *minHome, *maxNeighbor)
			if err != nil {
				return fmt.Errorf("game %d: %w", gameID, err)
			}
			if s.json {
				return s.print(homeWorldJSON{GameID: gameID, PlanetID: h.PlanetID, X: h.X, Y: h.Y, Z: h.Z, Star: h.Seq, Orbit: h.Orbit}, nil)
			}
			_, err = fmt.Fprintf(stdout, "flagged planet %d as a home world in game %d: system %d %d %d, star %s, orbit %d\n",
				h.PlanetID, gameID, h.X, h.Y, h.Z, h.Seq, h.Orbit)
			return err
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

// empireJSON is an empire as assign-seat prints it.
type empireJSON struct {
	GameID    int64  `json:"game_id"`
	SeatID    int64  `json:"seat_id"`
	FactionID int64  `json:"faction_id"`
	Name      string `json:"name"`
	RaceID    int64  `json:"race_id"`
	PlanetID  int64  `json:"planet_id"`
	X         int    `json:"x"`
	Y         int    `json:"y"`
	Z         int    `json:"z"`
	Star      string `json:"star"`
	Orbit     int    `json:"orbit"`
}

func assignSeatCommand() *ff.Command {
	s := newOutputSettings("assign-seat")
	game := s.Flags.Int64Config(ff.FlagConfig{
		LongName: "game", Placeholder: "ID", NoDefault: true, Usage: "ID of the game (required)",
	}, 0)
	seat := s.Flags.Int64Config(ff.FlagConfig{
		LongName: "seat", Placeholder: "ID", NoDefault: true, Usage: "ID of the seat (required)",
	}, 0)
	planet := s.Flags.Int64Config(ff.FlagConfig{
		LongName: "planet", Placeholder: "ID", NoDefault: true,
		Usage: "ID of the home world (default: the game's only open home world)",
	}, 0)
	name := s.Flags.StringConfig(ff.FlagConfig{
		LongName: "name", Placeholder: "NAME", NoDefault: true,
		Usage: "name of the empire (default: " + store.DefaultFactionName + ")",
	}, "")
	return &ff.Command{
		Name:      "assign-seat",
		Usage:     "seiicho assign-seat [--env ENV] [--db PATH] --game ID --seat ID [--planet ID] [--name NAME] [--json]",
		ShortHelp: "create a seat's empire on a home world",
		Flags:     s.Flags,
		Exec: settings.NoArgs(func(ctx context.Context) error {
			gameID, seatID, planetID := *game, *seat, *planet
			if gameID <= 0 {
				return errors.New("--game is required and must be a positive game ID")
			}
			if seatID <= 0 {
				return errors.New("--seat is required and must be a positive seat ID")
			}
			if f, ok := s.Flags.GetFlag("planet"); ok && f.IsSet() && planetID <= 0 {
				return errors.New("--planet must be a positive planet ID")
			}
			empireName := ""
			if f, ok := s.Flags.GetFlag("name"); ok && f.IsSet() {
				var err error
				if empireName, err = names.Clean(*name); err != nil {
					return fmt.Errorf("--name: %w", err)
				}
			}
			db, err := open(ctx, s.Settings)
			if err != nil {
				return err
			}
			defer db.Close()
			e, err := db.AssignSeat(ctx, gameID, seatID, planetID, empireName, time.Now())
			if errors.Is(err, store.ErrOpenHomeWorlds) {
				return fmt.Errorf("game %d, seat %d: %w; name one with --planet", gameID, seatID, err)
			} else if err != nil {
				return fmt.Errorf("game %d, seat %d: %w", gameID, seatID, err)
			}
			h := e.Home
			if s.json {
				return s.print(empireJSON{GameID: gameID, SeatID: e.SeatID, FactionID: e.FactionID, Name: e.Name, RaceID: e.RaceID,
					PlanetID: h.PlanetID, X: h.X, Y: h.Y, Z: h.Z, Star: h.Seq, Orbit: h.Orbit}, nil)
			}
			_, err = fmt.Fprintf(stdout, "assigned seat %d in game %d to planet %d (system %d %d %d, star %s, orbit %d): empire %d %q, race %d\n",
				e.SeatID, gameID, h.PlanetID, h.X, h.Y, h.Z, h.Seq, h.Orbit, e.FactionID, e.Name, e.RaceID)
			return err
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
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	if err := w.Flush(); err != nil {
		return err
	}
	// An empty last cell leaves the padding of the cell before it.
	for line := range strings.Lines(b.String()) {
		if _, err := io.WriteString(stdout, strings.TrimRight(line, " \n")+"\n"); err != nil {
			return err
		}
	}
	return nil
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
	// The seat's empire; null if it has none.
	EmpireID     *int64  `json:"empire_id"`
	EmpireName   *string `json:"empire_name"`
	HomePlanetID *int64  `json:"home_planet_id"`
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
			rows := [][]string{{"SEAT", "ACCOUNT", "EMAIL", "ROLE", "ACTIVE", "CREATED", "EMPIRE", "HOME", "NAME"}}
			for _, st := range seats {
				j := seatJSON{ID: st.ID, GameID: st.GameID, AccountID: st.AccountID, Email: st.Email,
					IsGM: st.IsGM, IsActive: st.IsActive, CreatedAt: st.CreatedAt.UTC()}
				role := "player"
				if st.IsGM {
					role = "gamemaster"
				}
				row := []string{fmt.Sprint(st.ID), fmt.Sprint(st.AccountID), st.Email, role,
					yesNo(st.IsActive), st.CreatedAt.UTC().Format(time.DateTime), "", "", ""}
				if st.EmpireID != 0 {
					j.EmpireID, j.EmpireName, j.HomePlanetID = &st.EmpireID, &st.EmpireName, &st.HomePlanetID
					row[6], row[7], row[8] = fmt.Sprint(st.EmpireID), fmt.Sprint(st.HomePlanetID), st.EmpireName
				}
				out = append(out, j)
				rows = append(rows, row)
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

// histogramJSON is a histogram as show-histogram prints it. Bins holds
// binJSON, quantityBinJSON, yieldBinJSON, or depositsBinJSON values,
// depending on the histogram.
type histogramJSON struct {
	GameID    int64  `json:"game_id"`
	Histogram string `json:"histogram"`
	Bins      any    `json:"bins"`
}

type binJSON struct {
	Planets int `json:"planets"`
	Count   int `json:"count"`
}

type quantityBinJSON struct {
	MinQuantity int64 `json:"min_quantity"`
	MaxQuantity int64 `json:"max_quantity"`
	Count       int   `json:"count"`
}

type yieldBinJSON struct {
	YieldPct int `json:"yield_pct"`
	Count    int `json:"count"`
}

type depositsBinJSON struct {
	Deposits int `json:"deposits"`
	Count    int `json:"count"`
}

// maxBar is the length of the longest bar of a deposit histogram.
const maxBar = 50

// scaledBar returns a bar for count when the largest count is top: maxBar #s
// for top, in proportion and rounded to the nearest for smaller counts, and
// at least one # for any count above 0.
func scaledBar(count, top int) string {
	if count == 0 {
		return ""
	}
	return strings.Repeat("#", max(1, (count*maxBar+top/2)/top))
}

// shortQuantity formats a quantity to at most three significant digits with
// a K, M, or B suffix, so 99999 prints as 100K and 2000000000 as 2B.
func shortQuantity(q int64) string {
	f, suffix := float64(q), ""
	for _, u := range []struct {
		size   float64
		suffix string
	}{{1e9, "B"}, {1e6, "M"}, {1e3, "K"}} {
		if f >= u.size*0.9995 {
			f, suffix = f/u.size, u.suffix
			break
		}
	}
	return strconv.FormatFloat(f, 'g', 3, 64) + suffix
}

func showHistogramCommand() *ff.Command {
	s := newOutputSettings("show-histogram")
	game := s.Flags.Int64Config(ff.FlagConfig{
		LongName: "game", Placeholder: "ID", NoDefault: true, Usage: "ID of the game (required)",
	}, 0)
	perStar := s.Flags.BoolLong("planets-per-star", "show the number of planets per star")
	perSystem := s.Flags.BoolLong("planets-per-star-system", "show the number of planets per star system")
	quantity := s.Flags.BoolLong("deposit-quantity", "show the quantities of deposits, on a logarithmic scale")
	yield := s.Flags.BoolLong("deposit-yield", "show the yields of deposits")
	perPlanet := s.Flags.BoolLong("deposits-per-planet", "show the number of deposits per planet")
	return &ff.Command{
		Name:      "show-histogram",
		Usage:     "seiicho show-histogram [--env ENV] [--db PATH] --game ID (--planets-per-star | --planets-per-star-system | --deposit-quantity | --deposit-yield | --deposits-per-planet) [--json]",
		ShortHelp: "show a histogram of a game's cluster",
		Flags:     s.Flags,
		Exec: settings.NoArgs(func(ctx context.Context) error {
			gameID := *game
			if gameID <= 0 {
				return errors.New("--game is required and must be a positive game ID")
			}
			chosen := 0
			for _, b := range []bool{*perStar, *perSystem, *quantity, *yield, *perPlanet} {
				if b {
					chosen++
				}
			}
			if chosen != 1 {
				return errors.New("give exactly one of --planets-per-star, --planets-per-star-system, --deposit-quantity, --deposit-yield, or --deposits-per-planet")
			}
			db, err := open(ctx, s.Settings)
			if err != nil {
				return err
			}
			defer db.Close()
			switch {
			case *quantity:
				bins, err := db.DepositQuantities(ctx, gameID)
				if err != nil {
					return fmt.Errorf("game %d: %w", gameID, err)
				}
				out := make([]quantityBinJSON, 0, len(bins))
				top := 0
				for _, b := range bins {
					out = append(out, quantityBinJSON{MinQuantity: b.Min, MaxQuantity: b.Max, Count: b.Count})
					top = max(top, b.Count)
				}
				rows := [][]string{{"QUANTITY", "DEPOSITS", ""}}
				for _, b := range bins {
					rows = append(rows, []string{shortQuantity(b.Min), fmt.Sprint(b.Count), scaledBar(b.Count, top)})
				}
				return s.print(histogramJSON{GameID: gameID, Histogram: "deposit-quantity", Bins: out}, rows)
			case *yield, *perPlanet:
				name, label, unit, histogram := "deposit-yield", "YIELD", "DEPOSITS", db.DepositYields
				if *perPlanet {
					name, label, unit, histogram = "deposits-per-planet", "DEPOSITS", "PLANETS", db.DepositsPerPlanet
				}
				bins, err := histogram(ctx, gameID)
				if err != nil {
					return fmt.Errorf("game %d: %w", gameID, err)
				}
				var out any
				if *perPlanet {
					counts := make([]depositsBinJSON, 0, len(bins))
					for _, b := range bins {
						counts = append(counts, depositsBinJSON{Deposits: b.Value, Count: b.Count})
					}
					out = counts
				} else {
					yields := make([]yieldBinJSON, 0, len(bins))
					for _, b := range bins {
						yields = append(yields, yieldBinJSON{YieldPct: b.Value, Count: b.Count})
					}
					out = yields
				}
				top := 0
				for _, b := range bins {
					top = max(top, b.Count)
				}
				rows := [][]string{{label, unit, ""}}
				for _, b := range bins {
					rows = append(rows, []string{fmt.Sprint(b.Value), fmt.Sprint(b.Count), scaledBar(b.Count, top)})
				}
				return s.print(histogramJSON{GameID: gameID, Histogram: name, Bins: out}, rows)
			}
			name, unit, histogram := "planets-per-star", "STARS", db.PlanetsPerStar
			if *perSystem {
				name, unit, histogram = "planets-per-star-system", "SYSTEMS", db.PlanetsPerSystem
			}
			bins, err := histogram(ctx, gameID)
			if err != nil {
				return fmt.Errorf("game %d: %w", gameID, err)
			}
			out := make([]binJSON, 0, len(bins))
			rows := [][]string{{"PLANETS", unit, ""}}
			for _, b := range bins {
				out = append(out, binJSON{Planets: b.Planets, Count: b.Count})
				rows = append(rows, []string{fmt.Sprint(b.Planets), fmt.Sprint(b.Count), strings.Repeat("#", b.Count)})
			}
			return s.print(histogramJSON{GameID: gameID, Histogram: name, Bins: out}, rows)
		}),
	}
}

// planetSummaryJSON is a summary as summarize-planets prints it.
type planetSummaryJSON struct {
	GameID int64                   `json:"game_id"`
	By     string                  `json:"by"`
	Groups []planetTypeSummaryJSON `json:"groups"`
}

type planetTypeSummaryJSON struct {
	PlanetType          string  `json:"planet_type"`
	Planets             int     `json:"planets"`
	MinHabitability     int     `json:"min_habitability"`
	AverageHabitability float64 `json:"average_habitability"`
	MaxHabitability     int     `json:"max_habitability"`
}

func summarizePlanetsCommand() *ff.Command {
	s := newOutputSettings("summarize-planets")
	game := s.Flags.Int64Config(ff.FlagConfig{
		LongName: "game", Placeholder: "ID", NoDefault: true, Usage: "ID of the game (required)",
	}, 0)
	byType := s.Flags.BoolLong("by-planet-type", "summarize the planets of each type (required)")
	return &ff.Command{
		Name:      "summarize-planets",
		Usage:     "seiicho summarize-planets [--env ENV] [--db PATH] --game ID --by-planet-type [--json]",
		ShortHelp: "summarize a game's planets",
		Flags:     s.Flags,
		Exec: settings.NoArgs(func(ctx context.Context) error {
			gameID := *game
			if gameID <= 0 {
				return errors.New("--game is required and must be a positive game ID")
			}
			if !*byType {
				return errors.New("--by-planet-type is required")
			}
			db, err := open(ctx, s.Settings)
			if err != nil {
				return err
			}
			defer db.Close()
			summaries, err := db.PlanetsByKind(ctx, gameID)
			if err != nil {
				return fmt.Errorf("game %d: %w", gameID, err)
			}
			out := planetSummaryJSON{GameID: gameID, By: "planet-type", Groups: make([]planetTypeSummaryJSON, 0, len(summaries))}
			rows := [][]string{{"TYPE", "PLANETS", "MIN", "AVG", "MAX"}}
			for _, k := range summaries {
				out.Groups = append(out.Groups, planetTypeSummaryJSON{PlanetType: k.Kind, Planets: k.Planets,
					MinHabitability: k.MinHabitability, AverageHabitability: k.AverageHabitability, MaxHabitability: k.MaxHabitability})
				rows = append(rows, []string{k.Kind, fmt.Sprint(k.Planets), fmt.Sprint(k.MinHabitability),
					strconv.FormatFloat(k.AverageHabitability, 'f', 1, 64), fmt.Sprint(k.MaxHabitability)})
			}
			return s.print(out, rows)
		}),
	}
}

// depositSummaryJSON is a summary as summarize-deposits prints it. Groups
// holds resourceSummaryJSON, kindDepositsJSON, or starSizeWealthJSON values,
// depending on By.
type depositSummaryJSON struct {
	GameID int64  `json:"game_id"`
	By     string `json:"by"`
	Groups any    `json:"groups"`
}

type resourceSummaryJSON struct {
	Resource       string  `json:"resource"`
	Quantity       int64   `json:"quantity"`
	Share          float64 `json:"share"`
	Deposits       int     `json:"deposits"`
	MinPerStar     int64   `json:"min_per_star"`
	AveragePerStar float64 `json:"average_per_star"`
	MaxPerStar     int64   `json:"max_per_star"`
}

type resourceSharesJSON struct {
	Gold      float64 `json:"gold"`
	Fuel      float64 `json:"fuel"`
	Metals    float64 `json:"mets"`
	NonMetals float64 `json:"nmts"`
}

type kindDepositsJSON struct {
	PlanetType      string             `json:"planet_type"`
	Planets         int                `json:"planets"`
	MinDeposits     int                `json:"min_deposits"`
	AverageDeposits float64            `json:"average_deposits"`
	MaxDeposits     int                `json:"max_deposits"`
	MinWealth       int64              `json:"min_wealth"`
	AverageWealth   float64            `json:"average_wealth"`
	MaxWealth       int64              `json:"max_wealth"`
	Shares          resourceSharesJSON `json:"shares"`
}

type starSizeWealthJSON struct {
	Planets          int     `json:"planets"`
	Stars            int     `json:"stars"`
	AveragePerStar   float64 `json:"average_per_star"`
	AveragePerPlanet float64 `json:"average_per_planet"`
}

// percent formats a share from 0 to 1 as a percentage to one decimal place.
func percent(share float64) string {
	return strconv.FormatFloat(100*share, 'f', 1, 64) + "%"
}

// whole formats an average to the nearest whole number.
func whole(f float64) string {
	return strconv.FormatFloat(f, 'f', 0, 64)
}

func summarizeDepositsCommand() *ff.Command {
	s := newOutputSettings("summarize-deposits")
	game := s.Flags.Int64Config(ff.FlagConfig{
		LongName: "game", Placeholder: "ID", NoDefault: true, Usage: "ID of the game (required)",
	}, 0)
	byResource := s.Flags.BoolLong("by-resource", "summarize the deposits of each resource")
	byType := s.Flags.BoolLong("by-planet-type", "summarize the deposits on each type of planet")
	bySize := s.Flags.BoolLong("by-planets-per-star", "summarize the wealth of stars with each number of planets")
	return &ff.Command{
		Name:      "summarize-deposits",
		Usage:     "seiicho summarize-deposits [--env ENV] [--db PATH] --game ID (--by-resource | --by-planet-type | --by-planets-per-star) [--json]",
		ShortHelp: "summarize a game's deposits",
		Flags:     s.Flags,
		Exec: settings.NoArgs(func(ctx context.Context) error {
			gameID := *game
			if gameID <= 0 {
				return errors.New("--game is required and must be a positive game ID")
			}
			chosen := 0
			for _, b := range []bool{*byResource, *byType, *bySize} {
				if b {
					chosen++
				}
			}
			if chosen != 1 {
				return errors.New("give exactly one of --by-resource, --by-planet-type, or --by-planets-per-star")
			}
			db, err := open(ctx, s.Settings)
			if err != nil {
				return err
			}
			defer db.Close()
			switch {
			case *byResource:
				summaries, err := db.DepositsByResource(ctx, gameID)
				if err != nil {
					return fmt.Errorf("game %d: %w", gameID, err)
				}
				groups := make([]resourceSummaryJSON, 0, len(summaries))
				rows := [][]string{{"RESOURCE", "QUANTITY", "SHARE", "DEPOSITS", "MIN/STAR", "AVG/STAR", "MAX/STAR"}}
				for _, r := range summaries {
					groups = append(groups, resourceSummaryJSON{Resource: r.Resource, Quantity: r.Quantity, Share: r.Share,
						Deposits: r.Deposits, MinPerStar: r.MinPerStar, AveragePerStar: r.AveragePerStar, MaxPerStar: r.MaxPerStar})
					rows = append(rows, []string{r.Resource, fmt.Sprint(r.Quantity), percent(r.Share), fmt.Sprint(r.Deposits),
						fmt.Sprint(r.MinPerStar), whole(r.AveragePerStar), fmt.Sprint(r.MaxPerStar)})
				}
				return s.print(depositSummaryJSON{GameID: gameID, By: "resource", Groups: groups}, rows)
			case *byType:
				summaries, err := db.DepositsByKind(ctx, gameID)
				if err != nil {
					return fmt.Errorf("game %d: %w", gameID, err)
				}
				groups := make([]kindDepositsJSON, 0, len(summaries))
				rows := [][]string{{"TYPE", "PLANETS", "MIN-DEP", "AVG-DEP", "MAX-DEP", "MIN-WEALTH", "AVG-WEALTH", "MAX-WEALTH",
					"GOLD", "FUEL", "METS", "NMTS"}}
				for _, k := range summaries {
					sh := k.Shares
					groups = append(groups, kindDepositsJSON{PlanetType: k.Kind, Planets: k.Planets,
						MinDeposits: k.MinDeposits, AverageDeposits: k.AverageDeposits, MaxDeposits: k.MaxDeposits,
						MinWealth: k.MinWealth, AverageWealth: k.AverageWealth, MaxWealth: k.MaxWealth,
						Shares: resourceSharesJSON{Gold: sh.Gold, Fuel: sh.Fuel, Metals: sh.Metals, NonMetals: sh.NonMetals}})
					rows = append(rows, []string{k.Kind, fmt.Sprint(k.Planets),
						fmt.Sprint(k.MinDeposits), strconv.FormatFloat(k.AverageDeposits, 'f', 1, 64), fmt.Sprint(k.MaxDeposits),
						fmt.Sprint(k.MinWealth), whole(k.AverageWealth), fmt.Sprint(k.MaxWealth),
						percent(sh.Gold), percent(sh.Fuel), percent(sh.Metals), percent(sh.NonMetals)})
				}
				return s.print(depositSummaryJSON{GameID: gameID, By: "planet-type", Groups: groups}, rows)
			}
			summaries, err := db.DepositsByPlanetsPerStar(ctx, gameID)
			if err != nil {
				return fmt.Errorf("game %d: %w", gameID, err)
			}
			groups := make([]starSizeWealthJSON, 0, len(summaries))
			rows := [][]string{{"PLANETS", "STARS", "AVG/STAR", "AVG/PLANET"}}
			for _, w := range summaries {
				groups = append(groups, starSizeWealthJSON{Planets: w.Planets, Stars: w.Stars,
					AveragePerStar: w.AveragePerStar, AveragePerPlanet: w.AveragePerPlanet})
				rows = append(rows, []string{fmt.Sprint(w.Planets), fmt.Sprint(w.Stars), whole(w.AveragePerStar), whole(w.AveragePerPlanet)})
			}
			return s.print(depositSummaryJSON{GameID: gameID, By: "planets-per-star", Groups: groups}, rows)
		}),
	}
}
