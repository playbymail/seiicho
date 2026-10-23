// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/playbymail/seiicho/internal/names"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// MaxSeatsPerHomeWorld is the most seats that can be assigned to one home
// world, counting inactive seats.
const MaxSeatsPerHomeWorld = 15

// DefaultFactionName is the name of a faction created without one.
const DefaultFactionName = "Unnamed Faction"

// OpenHomeWorldsError is returned by AssignSeat when no planet is named and
// the game has more than one open home world. It matches ErrOpenHomeWorlds.
type OpenHomeWorldsError struct {
	// PlanetIDs are the open home worlds, in ascending order.
	PlanetIDs []int64
}

func (e *OpenHomeWorldsError) Error() string {
	ids := make([]string, len(e.PlanetIDs))
	for i, id := range e.PlanetIDs {
		ids[i] = fmt.Sprint(id)
	}
	return fmt.Sprintf("%s: planets %s", ErrOpenHomeWorlds, strings.Join(ids, ", "))
}

func (e *OpenHomeWorldsError) Unwrap() error { return ErrOpenHomeWorlds }

// Empire is a seat's first faction, as AssignSeat creates it.
type Empire struct {
	FactionID int64
	Name      string
	SeatID    int64
	RaceID    int64
	// Home is the empire's home world.
	Home HomeWorld
}

// AssignSeat creates seatID's empire on planetID, or on the game's only open
// home world when planetID is 0, and returns it, as
// internal/store/seat-assignment.md describes. An open home world is a
// planet with is_homeworld set that has fewer than MaxSeatsPerHomeWorld
// seats assigned. The empire is named name, or DefaultFactionName if name is
// empty. The first seat on a home world creates its race; later seats join
// it.
//
// The game must exist and be active; otherwise it returns ErrGameNotFound
// or ErrGameInactive. The seat must be an active player seat in the game
// with no empire; otherwise it returns ErrSeatNotFound, ErrSeatInactive,
// ErrGamemasterSeat, or ErrSeatAssigned. A named planet must be a home world
// in the game with room; otherwise it returns ErrNotHomeWorld or
// ErrHomeWorldFull. Without one, it returns ErrNoOpenHomeWorld, or an
// *OpenHomeWorldsError listing them if there are several. On error, it
// changes nothing. It panics if name is not empty and not already clean, as
// names.Clean returns it.
func (s *Store) AssignSeat(ctx context.Context, gameID, seatID, planetID int64, name string, now time.Time) (e Empire, err error) {
	if name == "" {
		name = DefaultFactionName
	} else if clean, err := names.Clean(name); err != nil || clean != name {
		panic(fmt.Sprintf("store: faction name %q is not clean", name))
	}

	conn, err := s.pool.Take(ctx)
	if err != nil {
		return Empire{}, err
	}
	defer s.pool.Put(conn)

	end, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		return Empire{}, err
	}
	defer end(&err)

	found, active := false, false
	err = sqlitex.Execute(conn, `SELECT is_active FROM games WHERE id = ?;`, &sqlitex.ExecOptions{
		Args: []any{gameID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			found, active = true, stmt.ColumnBool(0)
			return nil
		},
	})
	if err != nil {
		return Empire{}, err
	}
	switch {
	case !found:
		return Empire{}, ErrGameNotFound
	case !active:
		return Empire{}, ErrGameInactive
	}

	found, active = false, false
	isGM, assigned := false, false
	err = sqlitex.Execute(conn,
		`SELECT is_active, is_gm, EXISTS (SELECT 1 FROM factions WHERE seat_id = s.id AND is_empire = 1)
		 FROM seats s WHERE id = ? AND game_id = ?;`,
		&sqlitex.ExecOptions{
			Args: []any{seatID, gameID},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				found, active = true, stmt.ColumnBool(0)
				isGM, assigned = stmt.ColumnBool(1), stmt.ColumnBool(2)
				return nil
			},
		})
	if err != nil {
		return Empire{}, err
	}
	switch {
	case !found:
		return Empire{}, ErrSeatNotFound
	case !active:
		return Empire{}, ErrSeatInactive
	case isGM:
		return Empire{}, ErrGamemasterSeat
	case assigned:
		return Empire{}, ErrSeatAssigned
	}

	// Every home world of the game, with its number of seats.
	type home struct {
		HomeWorld
		seats int
	}
	var homes []home
	err = sqlitex.Execute(conn,
		`SELECT p.id, s.x, s.y, s.z, s.seq, p.orbit,
		        (SELECT count(*) FROM factions f WHERE f.home_planet_id = p.id AND f.is_empire = 1)
		 FROM planets p JOIN stars s ON s.id = p.star_id
		 WHERE s.game_id = ? AND p.is_homeworld = 1 ORDER BY p.id;`,
		&sqlitex.ExecOptions{
			Args: []any{gameID},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				homes = append(homes, home{
					HomeWorld: HomeWorld{
						PlanetID: stmt.ColumnInt64(0), X: stmt.ColumnInt(1), Y: stmt.ColumnInt(2), Z: stmt.ColumnInt(3),
						Seq: stmt.ColumnText(4), Orbit: stmt.ColumnInt(5),
					},
					seats: stmt.ColumnInt(6),
				})
				return nil
			},
		})
	if err != nil {
		return Empire{}, err
	}
	var open []home
	for _, h := range homes {
		if planetID == 0 && h.seats < MaxSeatsPerHomeWorld || h.PlanetID == planetID {
			open = append(open, h)
		}
	}
	switch {
	case planetID != 0 && len(open) == 0:
		return Empire{}, ErrNotHomeWorld
	case planetID != 0 && open[0].seats >= MaxSeatsPerHomeWorld:
		return Empire{}, ErrHomeWorldFull
	case len(open) == 0:
		return Empire{}, ErrNoOpenHomeWorld
	case len(open) > 1:
		ids := make([]int64, len(open))
		for i, h := range open {
			ids[i] = h.PlanetID
		}
		return Empire{}, &OpenHomeWorldsError{PlanetIDs: ids}
	}
	e.Home = open[0].HomeWorld

	err = sqlitex.Execute(conn, `SELECT id FROM races WHERE planet_id = ?;`, &sqlitex.ExecOptions{
		Args: []any{e.Home.PlanetID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			e.RaceID = stmt.ColumnInt64(0)
			return nil
		},
	})
	if err != nil {
		return Empire{}, err
	}
	if e.RaceID == 0 {
		err = sqlitex.Execute(conn, `INSERT INTO races (planet_id) VALUES (?);`,
			&sqlitex.ExecOptions{Args: []any{e.Home.PlanetID}})
		if err != nil {
			return Empire{}, err
		}
		e.RaceID = conn.LastInsertRowID()
	}

	err = sqlitex.Execute(conn,
		`INSERT INTO factions (name, race_id, home_planet_id, is_empire, seat_id, created_at)
		 VALUES (?, ?, ?, 1, ?, ?);`,
		&sqlitex.ExecOptions{Args: []any{name, e.RaceID, e.Home.PlanetID, seatID, now.Unix()}})
	if err != nil {
		return Empire{}, err
	}
	e.FactionID, e.Name, e.SeatID = conn.LastInsertRowID(), name, seatID
	return e, nil
}
