// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command seiichodb creates, migrates, and updates Seiicho databases.
//
// Usage:
//
//	seiichodb create [--env ENV] [--db PATH]
//	seiichodb migrate [--env ENV] [--db PATH]
//	seiichodb set-password [--env ENV] [--db PATH] --email EMAIL
//	seiichodb activate [--env ENV] [--db PATH] --email EMAIL
//	seiichodb deactivate [--env ENV] [--db PATH] --email EMAIL
//	seiichodb version
//
// --env and --db resolve as described in internal/settings. Every command
// needs a database file; an in-memory database would vanish on exit.
//
// create requires a new file. In the development and agent environments, the
// new database starts with the default accounts listed in internal/devdata.
//
// migrate applies any pending schema migrations. The server (seiichod) never
// migrates: it refuses to open a database whose schema is not current.
//
// set-password reads the new password from standard input: it prompts without
// echo on a terminal, and otherwise reads the first line. The password is
// never accepted as a flag, so it stays out of shell history and process lists.
//
// activate and deactivate set whether an account may sign in. Deactivating an
// account also ends its sessions.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/peterbourgon/ff/v4"
	"github.com/playbymail/seiicho"
	"github.com/playbymail/seiicho/internal/auth"
	"github.com/playbymail/seiicho/internal/devdata"
	"github.com/playbymail/seiicho/internal/settings"
	"github.com/playbymail/seiicho/internal/store"
	"golang.org/x/term"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "seiichodb: %v\n", err)
		os.Exit(1)
	}
}

// run builds the command tree and runs the command that args select.
func run(ctx context.Context, args []string) error {
	return settings.Run(ctx, &ff.Command{
		Name:      "seiichodb",
		Usage:     "seiichodb <command> [flags]",
		ShortHelp: "create, migrate, and update Seiicho databases",
		Subcommands: []*ff.Command{
			createCommand(),
			migrateCommand(),
			setPasswordCommand(),
			setActiveCommand(true),
			setActiveCommand(false),
			{
				Name:      "version",
				Usage:     "seiichodb version",
				ShortHelp: "print the version",
				Exec: settings.NoArgs(func(context.Context) error {
					fmt.Println(seiicho.Version().String())
					return nil
				}),
			},
		},
	}, args)
}

// resolve resolves the settings and rejects the in-memory database.
func resolve(s *settings.Settings) error {
	if err := s.Resolve(); err != nil {
		return err
	}
	if s.DB == settings.MemoryDB {
		return fmt.Errorf("%s: %s needs a database file; only seiichod accepts an in-memory database", s.DB, s.Flags.GetName())
	}
	return nil
}

func createCommand() *ff.Command {
	s := settings.New("create")
	return &ff.Command{
		Name:      "create",
		Usage:     "seiichodb create [--env ENV] [--db PATH]",
		ShortHelp: "create a new database file",
		Flags:     s.Flags,
		Exec: settings.NoArgs(func(ctx context.Context) error {
			if err := resolve(s); err != nil {
				return err
			}
			db, err := createStore(ctx, s.Env, s.DB)
			if err != nil {
				return err
			}
			if err := db.Close(); err != nil {
				return err
			}
			fmt.Printf("created database %s\n", s.DB)
			if devdata.Allowed(s.Env) {
				for _, a := range devdata.DefaultAccounts {
					fmt.Printf("created default account %s\n", a.Email)
				}
			}
			return nil
		}),
	}
}

// createStore creates a persistent database, adding the default accounts in
// development and agent. If that fails, it removes the new database.
func createStore(ctx context.Context, env, path string) (*store.Store, error) {
	db, err := store.Create(ctx, path)
	if err != nil {
		return nil, err
	}
	if !devdata.Allowed(env) {
		return db, nil
	}
	if err := devdata.CreateDefaultAccounts(ctx, db, env, time.Now()); err != nil {
		_ = db.Close()
		for _, name := range []string{path, path + "-wal", path + "-shm", path + "-journal"} {
			_ = os.Remove(name)
		}
		return nil, err
	}
	return db, nil
}

func migrateCommand() *ff.Command {
	s := settings.New("migrate")
	return &ff.Command{
		Name:      "migrate",
		Usage:     "seiichodb migrate [--env ENV] [--db PATH]",
		ShortHelp: "apply pending schema migrations",
		Flags:     s.Flags,
		Exec: settings.NoArgs(func(ctx context.Context) error {
			if err := resolve(s); err != nil {
				return err
			}
			from, to, err := store.Migrate(ctx, s.DB)
			if err != nil {
				return err
			}
			if from == to {
				fmt.Printf("database %s is current at schema version %d\n", s.DB, to)
			} else {
				fmt.Printf("migrated database %s from schema version %d to %d\n", s.DB, from, to)
			}
			return nil
		}),
	}
}

// emailSettings are the settings of a command that takes a required --email
// flag. resolve sets email, normalized.
type emailSettings struct {
	*settings.Settings
	email string
	flag  *string
}

func newEmailSettings(name string) *emailSettings {
	s := &emailSettings{Settings: settings.New(name)}
	s.flag = s.Flags.StringConfig(ff.FlagConfig{
		LongName: "email", Placeholder: "EMAIL", NoDefault: true,
		Usage: "email address of the account (required)",
	}, "")
	return s
}

// resolve is resolve for commands that take --email. It normalizes the email.
func (s *emailSettings) resolve() error {
	if err := resolve(s.Settings); err != nil {
		return err
	}
	s.email = strings.ToLower(strings.TrimSpace(*s.flag))
	if s.email == "" {
		return errors.New("--email is required")
	}
	return nil
}

// openStore opens an existing database, suggesting a migration if its schema
// is out of date.
func openStore(ctx context.Context, path string) (*store.Store, error) {
	db, err := store.Open(ctx, path)
	if errors.Is(err, store.ErrSchemaOutdated) {
		return nil, fmt.Errorf("%w (run seiichodb migrate)", err)
	}
	return db, err
}

func setActiveCommand(active bool) *ff.Command {
	name, help := "deactivate", "stop an account from signing in and end its sessions"
	if active {
		name, help = "activate", "let an account sign in again"
	}
	s := newEmailSettings(name)
	return &ff.Command{
		Name:      name,
		Usage:     "seiichodb " + name + " [--env ENV] [--db PATH] --email EMAIL",
		ShortHelp: help,
		Flags:     s.Flags,
		Exec: settings.NoArgs(func(ctx context.Context) error {
			if err := s.resolve(); err != nil {
				return err
			}
			db, err := openStore(ctx, s.DB)
			if err != nil {
				return err
			}
			defer db.Close()
			if err := db.SetAccountActive(ctx, s.email, active); errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("%s: account not found", s.email)
			} else if err != nil {
				return err
			}
			if active {
				fmt.Printf("activated %s\n", s.email)
			} else {
				fmt.Printf("deactivated %s and ended its sessions\n", s.email)
			}
			return nil
		}),
	}
}

func setPasswordCommand() *ff.Command {
	s := newEmailSettings("set-password")
	return &ff.Command{
		Name:      "set-password",
		Usage:     "seiichodb set-password [--env ENV] [--db PATH] --email EMAIL",
		ShortHelp: "set an account's password from standard input and end its sessions",
		Flags:     s.Flags,
		Exec: settings.NoArgs(func(ctx context.Context) error {
			if err := s.resolve(); err != nil {
				return err
			}

			password, err := readPassword()
			if err != nil {
				return err
			}
			if password == "" {
				return errors.New("password must not be empty")
			}
			hash, err := auth.HashPassword(password)
			if err != nil {
				return err
			}

			db, err := openStore(ctx, s.DB)
			if err != nil {
				return err
			}
			defer db.Close()
			if err := db.SetPassword(ctx, s.email, hash); errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("%s: account not found", s.email)
			} else if err != nil {
				return err
			}
			fmt.Printf("updated password for %s and ended its sessions\n", s.email)
			return nil
		}),
	}
}

// readPassword reads a password from standard input, prompting without echo
// when it is a terminal.
func readPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && !(errors.Is(err, io.EOF) && line != "") {
			return "", fmt.Errorf("read password: %w", err)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	fmt.Fprint(os.Stderr, "New password: ")
	first, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	fmt.Fprint(os.Stderr, "Confirm password: ")
	second, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if string(first) != string(second) {
		return "", errors.New("passwords do not match")
	}
	return string(first), nil
}
