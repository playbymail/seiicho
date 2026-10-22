// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package devdata_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/playbymail/seiicho/internal/auth"
	"github.com/playbymail/seiicho/internal/devdata"
	"github.com/playbymail/seiicho/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.OpenPrivateMemory(t.Context(), strings.NewReplacer("/", "_", "#", "_").Replace(t.Name()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateDefaultAccounts(t *testing.T) {
	for _, env := range []string{"development", "agent"} {
		t.Run(env, func(t *testing.T) {
			s := newStore(t)
			if err := devdata.CreateDefaultAccounts(t.Context(), s, env, time.Now()); err != nil {
				t.Fatalf("CreateDefaultAccounts: %v", err)
			}
			for _, want := range []struct {
				email, password string
				isAdmin         bool
			}{
				{"admin@example.com", "bad-choices-2026", true},
				{"acct01@example.com", "worser-choices-2001", false},
				{"acct02@example.com", "worser-choices-2002", false},
			} {
				a, err := s.AccountByEmail(t.Context(), want.email)
				if err != nil {
					t.Fatalf("%s: %v", want.email, err)
				}
				if a.IsAdmin != want.isAdmin {
					t.Errorf("%s: IsAdmin = %v, want %v", want.email, a.IsAdmin, want.isAdmin)
				}
				if !auth.CheckPassword(a.PasswordHash, want.password) {
					t.Errorf("%s: password does not verify", want.email)
				}
			}
		})
	}
}

func TestCreateDefaultAccountsNotAllowed(t *testing.T) {
	for _, env := range []string{"test", "production", ""} {
		t.Run(env, func(t *testing.T) {
			s := newStore(t)
			if err := devdata.CreateDefaultAccounts(t.Context(), s, env, time.Now()); !errors.Is(err, devdata.ErrNotAllowed) {
				t.Fatalf("CreateDefaultAccounts = %v, want ErrNotAllowed", err)
			}
			if _, err := s.AccountByEmail(t.Context(), "admin@example.com"); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("account created in a disallowed environment")
			}
		})
	}
}
