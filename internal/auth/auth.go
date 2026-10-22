// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package auth implements password hashing and session tokens.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"

	"golang.org/x/crypto/bcrypt"
)

// HashPassword hashes a password with bcrypt.
//
// The minimum cost is an intentional project decision; see AGENTS.md.
func HashPassword(password string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
}

// CheckPassword reports whether password matches hash.
func CheckPassword(hash []byte, password string) bool {
	return bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil
}

// dummyHash is compared against when an account does not exist, so that
// failed logins take similar time whether or not the account exists.
var dummyHash, _ = HashPassword("not a real password")

// CheckNoPassword spends the time of a password check and returns false.
func CheckNoPassword(password string) bool {
	_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
	return false
}

// RandomPassword returns a random password.
func RandomPassword() string {
	return rand.Text()
}

// NewSessionToken returns a random session token for the client and the hash
// to store on the server.
func NewSessionToken() (token string, hash []byte) {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // never returns an error
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashSessionToken(token)
}

// HashSessionToken returns the stored form of a session token.
func HashSessionToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
