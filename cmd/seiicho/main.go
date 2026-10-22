// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command seiicho runs the Seiicho web application.
//
// Usage:
//
//	seiicho create-database [--env ENV] [--db PATH]
//	seiicho serve [--env ENV] [--db PATH] [--addr HOST:PORT]
//	seiicho version
//
// Settings resolve from an explicit flag, then the environment (including
// environment files loaded by internal/dotenv), then the default:
//
//	--env   SEIICHO_ENV   required: development, test, production, or agent
//	--db    SEIICHO_DB    required: path to the SQLite database file
//	--addr  SEIICHO_ADDR  backend listen address, default 127.0.0.1:18443
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/playbymail/seiicho"
	"github.com/playbymail/seiicho/internal/dotenv"
	"github.com/playbymail/seiicho/internal/server"
	"github.com/playbymail/seiicho/internal/store"
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
		return errors.New("usage: seiicho create-database|serve|version [flags]")
	}
	switch cmd, args := args[0], args[1:]; cmd {
	case "create-database":
		return createDatabase(args)
	case "serve":
		return serve(args)
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
