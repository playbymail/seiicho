// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package auth_test

import (
	"bytes"
	"testing"

	"github.com/playbymail/seiicho/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

func TestPassword(t *testing.T) {
	hash, err := auth.HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if cost, err := bcrypt.Cost(hash); err != nil || cost != bcrypt.MinCost {
		t.Fatalf("bcrypt cost = %d, %v, want %d", cost, err, bcrypt.MinCost)
	}
	if bytes.Contains(hash, []byte("correct horse")) {
		t.Fatal("hash contains the plaintext password")
	}
	if !auth.CheckPassword(hash, "correct horse") {
		t.Fatal("CheckPassword rejected the correct password")
	}
	if auth.CheckPassword(hash, "wrong") {
		t.Fatal("CheckPassword accepted a wrong password")
	}
	if auth.CheckNoPassword("correct horse") {
		t.Fatal("CheckNoPassword returned true")
	}
}

func TestSessionToken(t *testing.T) {
	token, hash := auth.NewSessionToken()
	other, _ := auth.NewSessionToken()
	if token == other {
		t.Fatal("session tokens repeat")
	}
	if !bytes.Equal(hash, auth.HashSessionToken(token)) {
		t.Fatal("hash does not match token")
	}
	if bytes.Equal(hash, []byte(token)) {
		t.Fatal("hash is the token")
	}
}
