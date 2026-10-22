// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package settings_test

import (
	"context"
	"strings"
	"testing"

	"github.com/peterbourgon/ff/v4"
	"github.com/playbymail/seiicho/internal/settings"
)

// tree returns a root command with one subcommand, sub, whose settings are s.
// ran reports whether sub ran.
func tree(s *settings.Settings, ran *bool) *ff.Command {
	return &ff.Command{
		Name: "root",
		Subcommands: []*ff.Command{{
			Name:  "sub",
			Flags: s.Flags,
			Exec: settings.NoArgs(func(context.Context) error {
				*ran = true
				return s.Resolve()
			}),
		}},
	}
}

func TestRun(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		ran     bool
		wantErr string
	}{
		{"subcommand", []string{"sub", "--env", "test", "--db", "x.db"}, true, ""},
		{"no command", nil, false, "no command given"},
		{"unknown command", []string{"nope"}, false, `unknown command "nope"`},
		{"unexpected argument", []string{"sub", "--env", "test", "--db", "x.db", "extra"}, false, "unexpected arguments: [extra]"},
		{"unknown flag", []string{"sub", "--nope"}, false, "unknown flag"},
		{"single-dash long flag", []string{"sub", "-env", "test"}, false, "unknown flag"},
		{"help", []string{"sub", "--help"}, false, ""},
		{"short help", []string{"-h"}, false, ""},
	} {
		var ran bool
		err := settings.Run(t.Context(), tree(settings.New("sub"), &ran), tc.args)
		if tc.wantErr == "" && err != nil {
			t.Errorf("%s: %v", tc.name, err)
		} else if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
			t.Errorf("%s = %v, want %q", tc.name, err, tc.wantErr)
		}
		if ran != tc.ran {
			t.Errorf("%s: ran = %v, want %v", tc.name, ran, tc.ran)
		}
	}
}

func TestResolvePrefersFlags(t *testing.T) {
	t.Setenv("SEIICHO_ENV", "test")
	t.Setenv("SEIICHO_DB", "env.db")
	t.Setenv("SEIICHO_ADDR", "127.0.0.1:2")

	var ran bool
	s := settings.New("sub")
	if err := settings.Run(t.Context(), tree(s, &ran), []string{"sub"}); err != nil {
		t.Fatal(err)
	}
	if s.Env != "test" || s.DB != "env.db" {
		t.Errorf("without flags: env %q, db %q; want the environment", s.Env, s.DB)
	}
	if got := s.FromEnv("addr", "", "SEIICHO_ADDR", "def"); got != "127.0.0.1:2" {
		t.Errorf("FromEnv without the flag = %q, want the environment", got)
	}

	s = settings.New("sub")
	var addr string
	s.Flags.StringVar(&addr, 0, "addr", "", "")
	err := settings.Run(t.Context(), tree(s, &ran), []string{"sub", "--env", "production", "--db", "flag.db", "--addr", "127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Env != "production" || s.DB != "flag.db" {
		t.Errorf("with flags: env %q, db %q; want the flags", s.Env, s.DB)
	}
	if got := s.FromEnv("addr", addr, "SEIICHO_ADDR", "def"); got != "127.0.0.1:1" {
		t.Errorf("FromEnv with the flag = %q, want the flag", got)
	}
}
