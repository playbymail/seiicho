// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package settings resolves the flags shared by the Seiicho commands.
//
// Each setting resolves from an explicit flag, then the environment
// (including environment files loaded by internal/dotenv), then the default:
//
//	--env  SEIICHO_ENV  required: development, test, production, or agent
//	--db   SEIICHO_DB   required: path to the SQLite database file
package settings

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
	"github.com/playbymail/seiicho/internal/dotenv"
)

// MemoryDB is the --db value that selects a new in-memory database.
const MemoryDB = ":memory:"

// Settings holds the shared flags of one command. Env and DB are set by
// Resolve.
type Settings struct {
	Flags   *ff.FlagSet
	Env     string
	DB      string
	env, db *string
}

// New returns settings with --env and --db registered on a new flag set.
// Callers may register further flags on it; parse it as an ff.Command's Flags.
func New(name string) *Settings {
	s := &Settings{Flags: ff.NewFlagSet(name)}
	s.env = s.Flags.StringConfig(ff.FlagConfig{
		LongName: "env", Placeholder: "ENV", NoDefault: true,
		Usage: "application environment (SEIICHO_ENV)",
	}, "")
	s.db = s.Flags.StringConfig(ff.FlagConfig{
		LongName: "db", Placeholder: "PATH", NoDefault: true,
		Usage: "path to the database file (SEIICHO_DB)",
	}, "")
	return s
}

// Resolve loads the environment files and fills --env and --db from the
// environment when they were not given. Call it after the flags are parsed.
func (s *Settings) Resolve() error {
	s.Env, s.DB = *s.env, *s.db
	if !s.isSet("env") {
		s.Env = os.Getenv("SEIICHO_ENV")
	}
	if err := dotenv.Load(s.Env); err != nil {
		return fmt.Errorf("env %q: %w", s.Env, err)
	}
	if !s.isSet("db") {
		s.DB = os.Getenv("SEIICHO_DB")
	}
	if s.DB == "" {
		return errors.New("database path is required (--db or SEIICHO_DB)")
	}
	return nil
}

// FromEnv returns value if the named flag was given, and otherwise the
// environment variable key, falling back to def. Call it after Resolve.
func (s *Settings) FromEnv(name, value, key, def string) string {
	if s.isSet(name) {
		return value
	}
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// isSet reports whether the named flag was given on the command line.
func (s *Settings) isSet(name string) bool {
	f, ok := s.Flags.GetFlag(name)
	return ok && f.IsSet()
}

// NoArgs adapts f to an ff.Command Exec function for a command that takes
// only flags, rejecting any positional arguments.
func NoArgs(f func(ctx context.Context) error) func(context.Context, []string) error {
	return func(ctx context.Context, args []string) error {
		if len(args) != 0 {
			return fmt.Errorf("unexpected arguments: %v", args)
		}
		return f(ctx)
	}
}

// Run parses args against the command tree rooted at root and runs the
// selected command. For -h or --help, it prints that command's help to
// standard error and returns nil. A root without an Exec function needs a
// subcommand; Run prints the help and returns an error if none matches.
func Run(ctx context.Context, root *ff.Command, args []string) error {
	err := root.Parse(args)
	if errors.Is(err, ff.ErrHelp) {
		fmt.Fprint(os.Stderr, ffhelp.Command(root.GetSelected()))
		return nil
	} else if err != nil {
		return err
	}
	if root.GetSelected() == root && root.Exec == nil {
		fmt.Fprint(os.Stderr, ffhelp.Command(root))
		if rest := root.Flags.GetArgs(); len(rest) != 0 {
			return fmt.Errorf("unknown command %q", rest[0])
		}
		return errors.New("no command given")
	}
	return root.Run(ctx)
}
