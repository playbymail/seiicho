// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command seiicho runs the Seiicho web application.
//
// Usage:
//
//	seiicho create-database [--env ENV] [--db PATH]
//	seiicho serve [--env ENV] [--db PATH] [--addr HOST:PORT]
//	seiicho set-password [--env ENV] [--db PATH] --email EMAIL
//	seiicho version
//
// Settings resolve from an explicit flag, then the environment (including
// environment files loaded by internal/dotenv), then the default:
//
//	--env   SEIICHO_ENV   required: development, test, production, or agent
//	--db    SEIICHO_DB    required: path to the SQLite database file
//	--addr  SEIICHO_ADDR  backend listen address, default 127.0.0.1:18443
//
// set-password reads the new password from standard input: it prompts without
// echo on a terminal, and otherwise reads the first line. The password is
// never accepted as a flag, so it stays out of shell history and process lists.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/playbymail/seiicho"
	"github.com/playbymail/seiicho/internal/auth"
	"github.com/playbymail/seiicho/internal/dotenv"
	"github.com/playbymail/seiicho/internal/server"
	"github.com/playbymail/seiicho/internal/store"
	"golang.org/x/term"
)

const defaultAddr = "127.0.0.1:18443"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "seiicho: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: seiicho create-database|serve|set-password|version [flags]")
	}
	switch cmd, args := args[0], args[1:]; cmd {
	case "create-database":
		return createDatabase(args)
	case "serve":
		return serve(args)
	case "set-password":
		return setPassword(args)
	case "version":
		fmt.Println(seiicho.Version().String())
		return nil
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// settings holds the flags shared by commands and tracks which were set.
type settings struct {
	fs   *flag.FlagSet
	env  string
	db   string
	addr string
}

func newSettings(name string) *settings {
	s := &settings{fs: flag.NewFlagSet(name, flag.ContinueOnError)}
	s.fs.StringVar(&s.env, "env", "", "application environment (SEIICHO_ENV)")
	s.fs.StringVar(&s.db, "db", "", "path to the database file (SEIICHO_DB)")
	return s
}

// resolve parses args, loads the environment files, and fills unset flags
// from the environment and then the defaults.
func (s *settings) resolve(args []string) error {
	if err := s.fs.Parse(args); err != nil {
		return err
	}
	if s.fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", s.fs.Args())
	}
	set := map[string]bool{}
	s.fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	if !set["env"] {
		s.env = os.Getenv("SEIICHO_ENV")
	}
	if err := dotenv.Load(s.env); err != nil {
		return fmt.Errorf("env %q: %w", s.env, err)
	}
	if !set["db"] {
		s.db = os.Getenv("SEIICHO_DB")
	}
	if s.db == "" {
		return errors.New("database path is required (--db or SEIICHO_DB)")
	}
	if s.fs.Lookup("addr") != nil && !set["addr"] {
		s.addr = os.Getenv("SEIICHO_ADDR")
		if s.addr == "" {
			s.addr = defaultAddr
		}
	}
	return nil
}

func createDatabase(args []string) error {
	s := newSettings("create-database")
	if err := s.resolve(args); err != nil {
		return err
	}
	db, err := store.Create(context.Background(), s.db)
	if err != nil {
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	fmt.Printf("created database %s\n", s.db)
	return nil
}

func serve(args []string) error {
	s := newSettings("serve")
	s.fs.StringVar(&s.addr, "addr", "", "backend listen address host:port (SEIICHO_ADDR)")
	if err := s.resolve(args); err != nil {
		return err
	}
	if err := validateAddr(s.addr); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, s.db)
	if err != nil {
		return err
	}
	defer db.Close()

	logger := slog.Default()
	srv := &http.Server{
		Addr: s.addr,
		Handler: server.New(server.Config{
			Env:     s.env,
			Store:   db,
			Version: seiicho.Version().String(),
			Logger:  logger,
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.addr, err)
	}
	logger.Info("listening", "addr", ln.Addr().String(), "env", s.env, "version", seiicho.Version().String())

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func setPassword(args []string) error {
	s := newSettings("set-password")
	var email string
	s.fs.StringVar(&email, "email", "", "email address of the account")
	if err := s.resolve(args); err != nil {
		return err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return errors.New("--email is required")
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
	db, err := store.Open(ctx, s.db)
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

// validateAddr requires a host:port address with a numeric port.
func validateAddr(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("listen address %q: %w", addr, err)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 0 || n > 65535 {
		return fmt.Errorf("listen address %q: invalid port", addr)
	}
	return nil
}
