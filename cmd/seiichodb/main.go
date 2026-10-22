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

	"github.com/playbymail/seiicho"
	"github.com/playbymail/seiicho/internal/auth"
	"github.com/playbymail/seiicho/internal/devdata"
	"github.com/playbymail/seiicho/internal/settings"
	"github.com/playbymail/seiicho/internal/store"
	"golang.org/x/term"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "seiichodb: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: seiichodb create|migrate|set-password|activate|deactivate|version [flags]")
	}
	switch cmd, args := args[0], args[1:]; cmd {
	case "create":
		return create(args)
	case "migrate":
		return migrate(args)
	case "set-password":
		return setPassword(args)
	case "activate":
		return setActive(args, true)
	case "deactivate":
		return setActive(args, false)
	case "version":
		fmt.Println(seiicho.Version().String())
		return nil
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// resolve parses and resolves the settings and rejects the in-memory database.
func resolve(s *settings.Settings, args []string) error {
	if err := s.Parse(args); err != nil {
		return err
	}
	if err := s.Resolve(); err != nil {
		return err
	}
	if s.DB == settings.MemoryDB {
		return fmt.Errorf("%s: %s needs a database file; only seiichod accepts an in-memory database", s.DB, s.FlagSet.Name())
	}
	return nil
}

func create(args []string) error {
	s := settings.New("create")
	if err := resolve(s, args); err != nil {
		return err
	}
	db, err := createStore(context.Background(), s.Env, s.DB)
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

func migrate(args []string) error {
	s := settings.New("migrate")
	if err := resolve(s, args); err != nil {
		return err
	}
	from, to, err := store.Migrate(context.Background(), s.DB)
	if err != nil {
		return err
	}
	if from == to {
		fmt.Printf("database %s is current at schema version %d\n", s.DB, to)
	} else {
		fmt.Printf("migrated database %s from schema version %d to %d\n", s.DB, from, to)
	}
	return nil
}

// resolveEmail is resolve for commands that take a required --email flag.
// It returns the normalized email.
func resolveEmail(s *settings.Settings, args []string) (string, error) {
	var email string
	s.FlagSet.StringVar(&email, "email", "", "email address of the account")
	if err := resolve(s, args); err != nil {
		return "", err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return "", errors.New("--email is required")
	}
	return email, nil
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

func setActive(args []string, active bool) error {
	name := "deactivate"
	if active {
		name = "activate"
	}
	s := settings.New(name)
	email, err := resolveEmail(s, args)
	if err != nil {
		return err
	}
	ctx := context.Background()
	db, err := openStore(ctx, s.DB)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.SetAccountActive(ctx, email, active); errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%s: account not found", email)
	} else if err != nil {
		return err
	}
	if active {
		fmt.Printf("activated %s\n", email)
	} else {
		fmt.Printf("deactivated %s and ended its sessions\n", email)
	}
	return nil
}

func setPassword(args []string) error {
	s := settings.New("set-password")
	email, err := resolveEmail(s, args)
	if err != nil {
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

	ctx := context.Background()
	db, err := openStore(ctx, s.DB)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.SetPassword(ctx, email, hash); errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%s: account not found", email)
	} else if err != nil {
		return err
	}
	fmt.Printf("updated password for %s and ended its sessions\n", email)
	return nil
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
