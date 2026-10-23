// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package names_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/playbymail/seiicho/internal/names"
)

func TestClean(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"Unnamed Faction", "Unnamed Faction"},
		{"  Red\tStar  ", "Red Star"},
		{"a \n\r\x00\x7f b", "a b"},
		{"a b c", "a b c"},
		{"R&D <1> ~{}!", "R&D <1> ~{}!"},
		{strings.Repeat("x", names.MaxLen), strings.Repeat("x", names.MaxLen)},
		{"  " + strings.Repeat("x", names.MaxLen) + "\t", strings.Repeat("x", names.MaxLen)},
	} {
		if got, err := names.Clean(tc.in); err != nil || got != tc.want {
			t.Errorf("Clean(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	for _, tc := range []struct {
		in   string
		want error
	}{
		{"", names.ErrEmpty},
		{" \t\n ", names.ErrEmpty},
		{strings.Repeat("x", names.MaxLen+1), names.ErrTooLong},
		{strings.Repeat("x y", 9), names.ErrTooLong},
		{`a"b`, names.ErrCharacter},
		{`a\b`, names.ErrCharacter},
		{"Café", names.ErrCharacter},
		{"a\xffb", names.ErrCharacter},
	} {
		if got, err := names.Clean(tc.in); !errors.Is(err, tc.want) || got != "" {
			t.Errorf("Clean(%q) = %q, %v; want %v", tc.in, got, err, tc.want)
		}
	}
}
