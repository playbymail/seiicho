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
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/playbymail/seiicho/internal/dotenv"
)

// MemoryDB is the --db value that selects a new in-memory database.
const MemoryDB = ":memory:"

// Settings holds the shared flags of one command.
type Settings struct {
	FlagSet *flag.FlagSet
	Env     string
	DB      string
	// set records the flags given explicitly on the command line.
	set map[string]bool
}

// New returns settings with --env and --db registered on a new flag set.
// Callers may register further flags before calling Parse.
func New(name string) *Settings {
	s := &Settings{FlagSet: flag.NewFlagSet(name, flag.ContinueOnError)}
	s.FlagSet.StringVar(&s.Env, "env", "", "application environment (SEIICHO_ENV)")
	s.FlagSet.StringVar(&s.DB, "db", "", "path to the database file (SEIICHO_DB)")
	return s
}

// Parse parses the command-line arguments, which must all be flags.
func (s *Settings) Parse(args []string) error {
	if err := s.FlagSet.Parse(args); err != nil {
		return err
	}
	if s.FlagSet.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", s.FlagSet.Args())
	}
	s.set = map[string]bool{}
	s.FlagSet.Visit(func(f *flag.Flag) { s.set[f.Name] = true })
	return nil
}

// Resolve loads the environment files and fills --env and --db from the
// environment when they were not given. Call it after Parse.
func (s *Settings) Resolve() error {
	if !s.set["env"] {
		s.Env = os.Getenv("SEIICHO_ENV")
	}
	if err := dotenv.Load(s.Env); err != nil {
		return fmt.Errorf("env %q: %w", s.Env, err)
	}
	if !s.set["db"] {
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
	if s.set[name] {
		return value
	}
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
