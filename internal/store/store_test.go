// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package store_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/playbymail/seiicho/internal/store"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// memoryName derives a unique, URI-safe memory database name from the test.
func memoryName(t *testing.T) string {
	return strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
}

func TestCreateThenOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.db")
	s, err := store.Create(t.Context(), path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	now := time.Unix(1_800_000_000, 0)
	if _, err := s.CreateAccount(t.Context(), "a@example.com", []byte("hash"), false, now); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s, err = store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	a, err := s.AccountByEmail(t.Context(), "A@Example.com")
	if err != nil {
		t.Fatalf("AccountByEmail: %v", err)
	}
	if a.Email != "a@example.com" || !bytes.Equal(a.PasswordHash, []byte("hash")) {
		t.Fatalf("AccountByEmail = %+v", a)
	}
}

func TestCreateRejectsExistingFile(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty":    nil,
		"nonempty": []byte("not a database"),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "game.db")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Create(t.Context(), path); !errors.Is(err, store.ErrDatabaseExists) {
				t.Fatalf("Create = %v, want ErrDatabaseExists", err)
			}
			assertContents(t, path, data)
		})
	}
}

func TestCreateRejectsOwnedDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.db")
	s, err := store.Create(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := store.Create(t.Context(), path); !errors.Is(err, store.ErrDatabaseExists) {
		t.Fatalf("Create = %v, want ErrDatabaseExists", err)
	}
}

func TestCreateRequiresParent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	if _, err := store.Create(t.Context(), filepath.Join(dir, "game.db")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Create = %v, want ErrNotExist", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("parent directory was created: %v", err)
	}
}

func TestOpenRequiresFile(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{
		filepath.Join(dir, "missing.db"),
		filepath.Join(dir, "missing", "game.db"),
	} {
		if _, err := store.Open(t.Context(), path); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Open(%q) = %v, want ErrNotExist", path, err)
		}
		if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Open(%q) created the file", path)
		}
	}
}

func TestOpenRejectsForeignDatabases(t *testing.T) {
	for name, init := range map[string]string{
		"unset id, empty":  "",
		"unset id, tables": "CREATE TABLE t (x);",
		"foreign id":       "PRAGMA application_id = 42; CREATE TABLE t (x);",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "other.db")
			conn, err := sqlite.OpenConn(path, sqlite.OpenReadWrite|sqlite.OpenCreate)
			if err != nil {
				t.Fatal(err)
			}
			if init != "" {
				if err := sqlitex.ExecuteScript(conn, init, nil); err != nil {
					t.Fatal(err)
				}
			}
			conn.Close()
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			if _, err := store.Open(t.Context(), path); !errors.Is(err, store.ErrNotOwned) {
				t.Fatalf("Open = %v, want ErrNotOwned", err)
			}
			assertContents(t, path, before)
			if _, err := os.Stat(path + "-wal"); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("Open enabled WAL on a foreign database")
			}
		})
	}
}

func TestOpenRejectsInvalidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "junk.db")
	data := bytes.Repeat([]byte("not a database "), 100)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(t.Context(), path); err == nil {
		t.Fatal("Open succeeded on an invalid file")
	}
	assertContents(t, path, data)
}

func TestPersistentConnectionInvariants(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.db")
	s, err := store.Create(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	assertInvariants(t, s, "wal")
}

func TestMemoryConnectionInvariants(t *testing.T) {
	s, err := store.OpenSharedMemory(t.Context(), memoryName(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	assertInvariants(t, s, "memory")
}

func TestPrivateMemoryIsolation(t *testing.T) {
	name := memoryName(t)
	a, err := store.OpenPrivateMemory(t.Context(), name)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := store.OpenPrivateMemory(t.Context(), name)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	now := time.Unix(1_800_000_000, 0)
	if _, err := a.CreateAccount(t.Context(), "a@example.com", []byte("x"), false, now); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AccountByEmail(t.Context(), "a@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("private database visible to another store: %v", err)
	}
	conn, err := a.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Put(conn)
	if got := pragma(t, conn, "foreign_keys"); got != "1" {
		t.Fatalf("foreign_keys = %s, want 1", got)
	}
}

func TestSharedMemoryLifetime(t *testing.T) {
	name := memoryName(t)
	a, err := store.OpenSharedMemory(t.Context(), name)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	if _, err := a.CreateAccount(t.Context(), "a@example.com", []byte("x"), false, now); err != nil {
		t.Fatal(err)
	}

	b, err := store.OpenSharedMemory(t.Context(), name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.AccountByEmail(t.Context(), "a@example.com"); err != nil {
		t.Fatalf("shared database not shared: %v", err)
	}
	other, err := store.OpenSharedMemory(t.Context(), name+"_other")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.AccountByEmail(t.Context(), "a@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("differently named database shares state: %v", err)
	}
	other.Close()
	a.Close()
	b.Close()

	c, err := store.OpenSharedMemory(t.Context(), name)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.AccountByEmail(t.Context(), "a@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("shared database outlived its last connection: %v", err)
	}
}

func TestCreateAccountDuplicate(t *testing.T) {
	s, err := store.OpenPrivateMemory(t.Context(), memoryName(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Unix(1_800_000_000, 0)
	if _, err := s.CreateAccount(t.Context(), "a@example.com", []byte("first"), false, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAccount(t.Context(), "A@example.com", []byte("second"), true, now); !errors.Is(err, store.ErrAccountExists) {
		t.Fatalf("CreateAccount = %v, want ErrAccountExists", err)
	}
	a, err := s.AccountByEmail(t.Context(), "a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if a.IsAdmin || string(a.PasswordHash) != "first" {
		t.Fatalf("duplicate create modified account: %+v", a)
	}
}

func TestSessions(t *testing.T) {
	s, err := store.OpenPrivateMemory(t.Context(), memoryName(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Unix(1_800_000_000, 0)
	a, err := s.CreateAccount(t.Context(), "a@example.com", []byte("x"), false, now)
	if err != nil {
		t.Fatal(err)
	}
	token := []byte("token-hash")
	if err := s.CreateSession(t.Context(), token, a.ID, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, err := s.SessionAccount(t.Context(), token, now); err != nil || got.ID != a.ID {
		t.Fatalf("SessionAccount = %+v, %v", got, err)
	}
	if _, err := s.SessionAccount(t.Context(), token, now.Add(time.Hour)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired session: %v, want ErrNotFound", err)
	}
	if err := s.DeleteSession(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionAccount(t.Context(), token, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted session: %v, want ErrNotFound", err)
	}
	if err := s.CreateSession(t.Context(), token, a.ID+1, now, now.Add(time.Hour)); err == nil {
		t.Fatal("session for a missing account violated no foreign key")
	}
}

// assertInvariants borrows several connections at once and checks each one.
func assertInvariants(t *testing.T, s *store.Store, journalMode string) {
	t.Helper()
	var conns []*sqlite.Conn
	defer func() {
		for _, conn := range conns {
			s.Put(conn)
		}
	}()
	for range 3 {
		conn, err := s.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, conn)
		if got := pragma(t, conn, "journal_mode"); got != journalMode {
			t.Errorf("journal_mode = %s, want %s", got, journalMode)
		}
		if got := pragma(t, conn, "foreign_keys"); got != "1" {
			t.Errorf("foreign_keys = %s, want 1", got)
		}
		if got := pragma(t, conn, "application_id"); got != "1936025961" {
			t.Errorf("application_id = %s, want 1936025961", got)
		}
	}
}

func pragma(t *testing.T, conn *sqlite.Conn, name string) string {
	t.Helper()
	var value string
	err := sqlitex.ExecuteTransient(conn, "PRAGMA "+name+";", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			value = stmt.ColumnText(0)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func assertContents(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s was modified", path)
	}
}
