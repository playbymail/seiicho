// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command seiichod runs the Seiicho web server.
//
// Usage:
//
//	seiichod [--env ENV] [--db PATH] [--addr HOST:PORT]
//	seiichod --version
//
// --env and --db resolve as described in internal/settings; --addr resolves
// the same way:
//
//	--addr  SEIICHO_ADDR  backend listen address, default 127.0.0.1:18443
//
// seiichod never creates or migrates a database file; use seiichodb for that.
// It refuses to start on a database whose schema is not current.
//
// In the development and agent environments only, --db :memory: runs against
// a fresh, private in-memory database that starts with the default accounts
// listed in internal/devdata and is discarded when the server stops.
package main

import (
	"context"
	"errors"
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
	"github.com/playbymail/seiicho/internal/devdata"
	"github.com/playbymail/seiicho/internal/server"
	"github.com/playbymail/seiicho/internal/settings"
	"github.com/playbymail/seiicho/internal/store"
)

const defaultAddr = "127.0.0.1:18443"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "seiichod: %v\n", err)
		os.Exit(1)
	}
}

// config holds the resolved server settings.
type config struct {
	env, db, addr string
}

// resolve resolves the parsed settings into a config. It rejects the
// in-memory database outside development and agent.
func resolve(s *settings.Settings, addr string) (config, error) {
	if err := s.Resolve(); err != nil {
		return config{}, err
	}
	c := config{env: s.Env, db: s.DB, addr: s.FromEnv("addr", addr, "SEIICHO_ADDR", defaultAddr)}
	if c.db == settings.MemoryDB && !devdata.Allowed(c.env) {
		return c, fmt.Errorf("%s in env %q: in-memory database %w", c.db, c.env, devdata.ErrNotAllowed)
	}
	if err := validateAddr(c.addr); err != nil {
		return c, err
	}
	return c, nil
}

func run(args []string) error {
	s := settings.New("seiichod")
	var addr string
	var version bool
	s.FlagSet.StringVar(&addr, "addr", "", "backend listen address host:port (SEIICHO_ADDR)")
	s.FlagSet.BoolVar(&version, "version", false, "print the version and exit")
	if err := s.Parse(args); err != nil {
		return err
	}
	if version {
		fmt.Println(seiicho.Version().String())
		return nil
	}
	c, err := resolve(s, addr)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := openStore(ctx, c.env, c.db)
	if err != nil {
		return err
	}
	defer db.Close()

	logger := slog.Default()
	srv := &http.Server{
		Addr: c.addr,
		Handler: server.New(server.Config{
			Env:     c.env,
			Store:   db,
			Version: seiicho.Version().String(),
			Logger:  logger,
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ln, err := net.Listen("tcp", c.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", c.addr, err)
	}
	logger.Info("listening", "addr", ln.Addr().String(), "env", c.env, "db", c.db, "version", seiicho.Version().String())

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

// openStore opens an existing, current database. The in-memory database is
// allowed only in development and agent, and starts with the default accounts.
func openStore(ctx context.Context, env, path string) (*store.Store, error) {
	if path != settings.MemoryDB {
		db, err := store.Open(ctx, path)
		if errors.Is(err, store.ErrSchemaOutdated) {
			return nil, fmt.Errorf("%w (run seiichodb migrate)", err)
		}
		return db, err
	}
	if !devdata.Allowed(env) {
		return nil, fmt.Errorf("in-memory database: %w", devdata.ErrNotAllowed)
	}
	db, err := store.OpenPrivateMemory(ctx, "seiicho")
	if err != nil {
		return nil, err
	}
	if err := devdata.CreateDefaultAccounts(ctx, db, env, time.Now()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
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
