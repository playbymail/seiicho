// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package devdata provides the conveniences that exist only in the
// development and agent environments: in-memory databases and default
// accounts with well-known passwords.
package devdata

import (
	"context"
	"fmt"
	"time"

	"github.com/playbymail/seiicho/internal/auth"
	"github.com/playbymail/seiicho/internal/cerrs"
	"github.com/playbymail/seiicho/internal/store"
)

const ErrNotAllowed = cerrs.Error("only allowed in development and agent environments")

// Allowed reports whether env permits the development conveniences.
func Allowed(env string) bool {
	return env == "development" || env == "agent"
}

// DefaultAccount is an account created in every new development or agent
// database. The passwords are public test values, not secrets.
type DefaultAccount struct {
	Email    string
	Password string
	IsAdmin  bool
}

// DefaultAccounts lists the accounts that CreateDefaultAccounts creates.
// TESTING.md documents them; keep the two in sync.
var DefaultAccounts = []DefaultAccount{
	{Email: "admin@example.com", Password: "bad-choices-2026", IsAdmin: true},
	{Email: "acct01@example.com", Password: "worser-choices-2001"},
	{Email: "acct02@example.com", Password: "worser-choices-2002"},
}

// CreateDefaultAccounts adds the default accounts to a new database.
// It returns ErrNotAllowed unless env permits development conveniences.
func CreateDefaultAccounts(ctx context.Context, s *store.Store, env string, now time.Time) error {
	if !Allowed(env) {
		return fmt.Errorf("default accounts: %w", ErrNotAllowed)
	}
	for _, a := range DefaultAccounts {
		hash, err := auth.HashPassword(a.Password)
		if err != nil {
			return err
		}
		if _, err := s.CreateAccount(ctx, a.Email, hash, a.IsAdmin, now); err != nil {
			return fmt.Errorf("default account %s: %w", a.Email, err)
		}
	}
	return nil
}
