// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package store implements persistent storage in SQLite.
//
// Persistent databases are created with Create, brought up to date with
// Migrate, and opened with Open; the operations are deliberately distinct.
// Open never changes the schema: it rejects a database that needs migrating.
// All three verify that the file belongs to this application before making
// any change to it.
package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"

	"github.com/playbymail/seiicho/internal/cerrs"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
)

// applicationID is the ASCII bytes "seii" as a big-endian 32-bit integer.
const applicationID int32 = 0x73656969 // 1936025961, ASCII "seii"

const (
	ErrDatabaseExists    = cerrs.Error("database already exists")
	ErrNotOwned          = cerrs.Error("database does not belong to this application")
	ErrNotRegularFile    = cerrs.Error("database path is not a regular file")
	ErrInvalidMemoryName = cerrs.Error("invalid memory database name")
	ErrNotFound          = cerrs.Error("not found")
	ErrAccountExists     = cerrs.Error("account already exists")
	ErrNoActiveAdmin     = cerrs.Error("no active administrator")
	ErrGameNotFound      = cerrs.Error("game not found")
	ErrGameInactive      = cerrs.Error("game is inactive")
	ErrAccountNotFound   = cerrs.Error("account not found")
	ErrAccountInactive   = cerrs.Error("account is inactive")
	ErrSeatExists        = cerrs.Error("account already has a seat in the game")
	ErrNoActiveSeat      = cerrs.Error("account has no active seat in the game")
	ErrClusterExists     = cerrs.Error("game already has a cluster")
	ErrNoCluster         = cerrs.Error("game has no cluster")
	ErrSchemaOutdated    = cerrs.Error("database schema is out of date")
	ErrSchemaTooNew      = cerrs.Error("database schema is newer than this application")
)

// connPool is the part of a ZombieZen pool that a Store uses. Migration pools
// back created and in-memory databases; plain pools back opened ones.
type connPool interface {
	Take(ctx context.Context) (*sqlite.Conn, error)
	Put(conn *sqlite.Conn)
	Close() error
}

// Store is a handle to an initialized, migrated database.
type Store struct {
	pool connPool
	// anchor keeps a shared memory database alive for the store's lifetime.
	anchor *sqlite.Conn
}

// Create creates a new persistent database at path.
// The file must not already exist and its parent directory must exist.
func Create(ctx context.Context, path string) (*Store, error) {
	if err := validatePath(path); err != nil {
		return nil, err
	}
	// SQLite would replay a stray journal into the new file, so refuse to
	// create a database next to one.
	for _, sibling := range []string{path + "-wal", path + "-shm", path + "-journal"} {
		if _, err := os.Lstat(sibling); err == nil {
			return nil, fmt.Errorf("%s: %w", sibling, ErrDatabaseExists)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	// Reserve the file exclusively; this also fails if the parent is missing.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil, fmt.Errorf("%s: %w", path, ErrDatabaseExists)
	} else if err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	cleanup := func() {
		for _, name := range []string{path, path + "-wal", path + "-shm", path + "-journal"} {
			_ = os.Remove(name)
		}
	}

	conn, err := sqlite.OpenConn(path, sqlite.OpenReadWrite)
	if err != nil {
		cleanup()
		return nil, err
	}
	err = setApplicationID(conn)
	if cerr := conn.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		cleanup()
		return nil, err
	}

	s, err := openPool(ctx, path, sqlite.OpenReadWrite, 0, prepareConn("wal"))
	if err != nil {
		cleanup()
		return nil, err
	}
	return s, nil
}

// Open opens an existing persistent database at path. The schema must be
// current; Open returns ErrSchemaOutdated rather than applying migrations.
// It never creates the file or its parent directory.
func Open(ctx context.Context, path string) (*Store, error) {
	version, err := probe(path)
	if err != nil {
		return nil, err
	}
	if err := checkVersion(version); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	prepare := prepareConn("wal")
	pool, err := sqlitex.NewPool(path, sqlitex.PoolOptions{
		Flags: sqlite.OpenReadWrite,
		PrepareConn: func(conn *sqlite.Conn) error {
			if err := prepare(conn); err != nil {
				return err
			}
			version, err := pragmaInt(conn, "user_version")
			if err != nil {
				return err
			}
			return checkVersion(int(version))
		},
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	conn, err := pool.Take(ctx)
	if err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	pool.Put(conn)
	return &Store{pool: pool}, nil
}

// Migrate applies any pending migrations to the existing persistent database
// at path and reports the schema versions before and after. It never creates
// the file or its parent directory.
func Migrate(ctx context.Context, path string) (from, to int, err error) {
	from, err = probe(path)
	if err != nil {
		return 0, 0, err
	}
	if from > len(schema.Migrations) {
		return from, from, fmt.Errorf("%s: version %d: %w", path, from, ErrSchemaTooNew)
	}
	s, err := openPool(ctx, path, sqlite.OpenReadWrite, 0, prepareConn("wal"))
	if err != nil {
		return from, from, fmt.Errorf("%s: %w", path, err)
	}
	if err := s.Close(); err != nil {
		return from, len(schema.Migrations), err
	}
	return from, len(schema.Migrations), nil
}

// probe checks that path is an existing database file owned by this
// application, before anything (including WAL) can modify it, and returns
// its schema version.
func probe(path string) (int, error) {
	if err := validatePath(path); err != nil {
		return 0, err
	}
	sb, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if !sb.Mode().IsRegular() {
		return 0, fmt.Errorf("%s: %w", path, ErrNotRegularFile)
	}

	conn, err := sqlite.OpenConn(path, sqlite.OpenReadWrite)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	err = checkApplicationID(conn)
	var version int64
	if err == nil {
		version, err = pragmaInt(conn, "user_version")
	}
	if cerr := conn.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	return int(version), nil
}

// checkVersion requires the schema version that this application expects.
func checkVersion(version int) error {
	switch want := len(schema.Migrations); {
	case version < want:
		return fmt.Errorf("version %d, want %d: %w", version, want, ErrSchemaOutdated)
	case version > want:
		return fmt.Errorf("version %d, want %d: %w", version, want, ErrSchemaTooNew)
	}
	return nil
}

// memoryNamePattern restricts memory database names to characters that need
// no escaping in a SQLite URI.
var memoryNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// OpenSharedMemory opens the named in-memory database shared by every
// connection in this process, creating it if it does not exist.
// The database lives until the returned store is closed.
func OpenSharedMemory(ctx context.Context, name string) (*Store, error) {
	if !memoryNamePattern.MatchString(name) {
		return nil, fmt.Errorf("%q: %w", name, ErrInvalidMemoryName)
	}
	uri := "file:" + name + "?mode=memory&cache=shared"
	flags := sqlite.OpenReadWrite | sqlite.OpenCreate | sqlite.OpenURI

	anchor, err := sqlite.OpenConn(uri, flags)
	if err != nil {
		return nil, err
	}
	if err := initMemory(anchor); err != nil {
		_ = anchor.Close()
		return nil, err
	}
	s, err := openPool(ctx, uri, flags, 0, prepareConn("memory"))
	if err != nil {
		_ = anchor.Close()
		return nil, err
	}
	s.anchor = anchor
	return s, nil
}

// OpenPrivateMemory creates a new in-memory database visible only to the
// returned store. The store has a single connection.
func OpenPrivateMemory(ctx context.Context, name string) (*Store, error) {
	if !memoryNamePattern.MatchString(name) {
		return nil, fmt.Errorf("%q: %w", name, ErrInvalidMemoryName)
	}
	uri := "file:" + name + "?mode=memory&cache=private"
	prepare := prepareConn("memory")
	// The sole connection is the database, so initialize it there.
	return openPool(ctx, uri, sqlite.OpenReadWrite|sqlite.OpenURI, 1, func(conn *sqlite.Conn) error {
		if err := initMemory(conn); err != nil {
			return err
		}
		return prepare(conn)
	})
}

// Close closes all connections to the database.
func (s *Store) Close() error {
	err := s.pool.Close()
	if s.anchor != nil {
		err = errors.Join(err, s.anchor.Close())
	}
	return err
}

// Ping verifies that the database can execute a query.
func (s *Store) Ping(ctx context.Context) error {
	conn, err := s.pool.Take(ctx)
	if err != nil {
		return err
	}
	defer s.pool.Put(conn)
	return sqlitex.ExecuteTransient(conn, "SELECT 1;", nil)
}

// validatePath rejects SQLite URIs and the private memory name so that
// persistent Create, Migrate, and Open always operate on a literal file.
func validatePath(path string) error {
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") {
		return fmt.Errorf("%q: invalid database path", path)
	}
	return nil
}

// openPool opens a migration pool and forces initialization so that the
// returned store is ready to use.
func openPool(ctx context.Context, uri string, flags sqlite.OpenFlags, size int, prepare sqlitex.ConnPrepareFunc) (*Store, error) {
	pool := sqlitemigration.NewPool(uri, schema, sqlitemigration.Options{
		Flags:       flags,
		PoolSize:    size,
		PrepareConn: prepare,
	})
	conn, err := pool.Take(ctx)
	if err != nil {
		_ = pool.Close()
		return nil, err
	}
	pool.Put(conn)
	return &Store{pool: pool}, nil
}

// prepareConn returns a function that enforces the connection invariants:
// ownership, the expected journal mode, and foreign key enforcement.
func prepareConn(journalMode string) sqlitex.ConnPrepareFunc {
	return func(conn *sqlite.Conn) error {
		if err := checkApplicationID(conn); err != nil {
			return err
		}
		var mode string
		err := sqlitex.ExecuteTransient(conn, "PRAGMA journal_mode = "+journalMode+";", &sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				mode = stmt.ColumnText(0)
				return nil
			},
		})
		if err != nil {
			return err
		}
		if !strings.EqualFold(mode, journalMode) {
			return fmt.Errorf("journal_mode = %q, want %q", mode, journalMode)
		}
		if err := sqlitex.ExecuteTransient(conn, "PRAGMA foreign_keys = ON;", nil); err != nil {
			return err
		}
		fk, err := pragmaInt(conn, "foreign_keys")
		if err != nil {
			return err
		}
		if fk != 1 {
			return fmt.Errorf("foreign_keys = %d, want 1", fk)
		}
		return nil
	}
}

// initMemory stamps a fresh in-memory database with the application ID, or
// verifies the ID of one that already exists.
func initMemory(conn *sqlite.Conn) error {
	id, err := pragmaInt(conn, "application_id")
	if err != nil {
		return err
	}
	if id == 0 {
		var tables int
		err := sqlitex.ExecuteTransient(conn, "SELECT count(*) FROM sqlite_master;", &sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				tables = stmt.ColumnInt(0)
				return nil
			},
		})
		if err != nil {
			return err
		}
		if tables == 0 {
			return setApplicationID(conn)
		}
	}
	return checkApplicationID(conn)
}

func setApplicationID(conn *sqlite.Conn) error {
	return sqlitex.ExecuteTransient(conn, fmt.Sprintf("PRAGMA application_id = %d;", applicationID), nil)
}

func checkApplicationID(conn *sqlite.Conn) error {
	id, err := pragmaInt(conn, "application_id")
	if err != nil {
		return err
	}
	if int32(id) != applicationID {
		return fmt.Errorf("application_id = %#x: %w", id, ErrNotOwned)
	}
	return nil
}

func pragmaInt(conn *sqlite.Conn, name string) (int64, error) {
	var value int64
	err := sqlitex.ExecuteTransient(conn, "PRAGMA "+name+";", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			value = stmt.ColumnInt64(0)
			return nil
		},
	})
	return value, err
}
