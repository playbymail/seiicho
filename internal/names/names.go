// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package names cleans and checks the names of things in a game, as
// internal/store/seat-assignment.md describes. Every name is stored as 1 to
// MaxLen characters of printable ASCII other than '"' and '\', with no
// leading, trailing, or doubled spaces, so JSON never needs to escape it.
package names

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/playbymail/seiicho/internal/cerrs"
)

// MaxLen is the most characters a name may have.
const MaxLen = 25

const (
	ErrEmpty     = cerrs.Error("name is empty")
	ErrTooLong   = cerrs.Error("name is longer than 25 characters")
	ErrCharacter = cerrs.Error("name has a character that is not allowed")
)

// Clean returns s as a name: every non-printing character, as
// unicode.IsPrint defines it, becomes a space; leading and trailing spaces
// are trimmed; and each run of spaces inside becomes one space. It returns
// ErrEmpty, ErrTooLong, or ErrCharacter unless the result is 1 to MaxLen
// characters of printable ASCII other than '"' and '\'.
func Clean(s string) (string, error) {
	var b strings.Builder
	space := false // a space is owed before the next character
	for _, r := range s {
		if r == ' ' || !unicode.IsPrint(r) {
			space = b.Len() > 0
			continue
		}
		if r > '~' || r == '"' || r == '\\' {
			return "", fmt.Errorf("%w: %q", ErrCharacter, r)
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	switch {
	case b.Len() == 0:
		return "", ErrEmpty
	case b.Len() > MaxLen:
		return "", ErrTooLong
	}
	return b.String(), nil
}
