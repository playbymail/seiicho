# Seat assignment

How the gamemaster gives a seat its starting faction on a home world.

Status: **draft**, not yet approved. See [Open questions](#open-questions).

A seat can start the game only on a planet that has been flagged as a home
world (see `internal/cluster/home-worlds.md`). This document covers the step
after flagging: assigning a seat to one of those planets. Assignment creates
the seat's first faction, its empire, and the race that faction belongs to.

The operator assigns seats one at a time with `seiicho assign-seat`. Each run
assigns one seat or fails without changing anything. Nothing in it is random.

## Terms

- **Faction:** a body that acts in the game. Every faction has a name, a home
  world, and a race, and is controlled by exactly one seat or exactly one
  other faction.
- **Empire:** the first faction assigned to a seat. A seat has at most one.
- **Race:** the people of a home world. Every faction whose home world is a
  given planet is of the same race.
- **Seat assigned to a home world:** a seat whose empire has that planet as
  its home world.
- **Open home world:** a planet with `is_homeworld = 1` that has fewer than
  15 seats assigned to it.

## Rules

1. A seat can be assigned only to a planet with `is_homeworld = 1` in the
   seat's game.
2. No more than **15** seats can be assigned to one home world.
3. Every faction with the same home world is of the same race.
4. Every faction is controlled by either a seat or another faction, never
   both and never neither.
5. Control has no cycles: following controlling factions from any faction
   always ends at a faction controlled by a seat.
6. A new faction is named `Unnamed Faction`.
7. The first faction assigned to a seat is its empire (`is_empire = 1`).

This command creates only empires, and an empire is controlled by its seat,
so it can never create a cycle. Rule 5 binds whatever later changes control
of a faction; see [Control cycles](#control-cycles).

## Choosing the planet

The gamemaster may name the planet with `--planet ID`. The command then
checks rules 1 and 2 for that planet.

Without `--planet`, the command looks at the game's open home worlds:

- If there is exactly one, the seat is assigned to it.
- If there are none, the run fails. The gamemaster must flag another home
  world first.
- If there are several, the run fails and lists them. The gamemaster must
  name one with `--planet`.

So a gamemaster who flags home worlds one at a time and fills each before
flagging the next never needs `--planet`.

## Assignment

In one transaction:

1. Check the seat: it exists, is in the game, is active, is not a
   gamemaster seat, and has no empire yet.
2. Choose the planet as above.
3. Find the planet's race. If no seat has been assigned there yet, create
   it.
4. Create the empire: named `Unnamed Faction`, `is_empire = 1`, controlled by
   the seat, with the planet as its home world and the planet's race.

The command prints the seat, the new faction's ID and name, the race's ID,
and the home world's planet ID, system coordinates, star letter, and orbit.

## Storage

Two new tables, squashed into migration 1 as usual during alpha. Existing
databases, including `games/alpha`, must be rebuilt.

```sql
-- A race is the people of one home world. It is created when the first seat
-- is assigned to that planet. The second unique key is the target of
-- factions' composite foreign key.
CREATE TABLE races (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	planet_id INTEGER NOT NULL UNIQUE REFERENCES planets (id),
	UNIQUE (id, planet_id)
) STRICT;

-- A faction acts in the game. It is controlled by exactly one seat or
-- exactly one other faction. Control must not form cycles; the schema
-- rejects only a faction controlling itself, so the code that changes
-- control checks the rest. The composite foreign key makes every faction
-- with the same home world the same race. A seat's first faction is its
-- empire, and a seat has at most one.
CREATE TABLE factions (
	id                     INTEGER PRIMARY KEY AUTOINCREMENT,
	name                   TEXT    NOT NULL DEFAULT 'Unnamed Faction',
	race_id                INTEGER NOT NULL,
	home_planet_id         INTEGER NOT NULL,
	is_empire              INTEGER NOT NULL DEFAULT 0 CHECK (is_empire IN (0, 1)),
	seat_id                INTEGER REFERENCES seats (id),
	controlling_faction_id INTEGER REFERENCES factions (id),
	created_at             INTEGER NOT NULL,
	FOREIGN KEY (race_id, home_planet_id) REFERENCES races (id, planet_id),
	CHECK ((seat_id IS NULL) <> (controlling_faction_id IS NULL)),
	CHECK (controlling_faction_id <> id),
	CHECK (is_empire = 0 OR seat_id IS NOT NULL)
) STRICT;

CREATE UNIQUE INDEX factions_seat_empire ON factions (seat_id) WHERE is_empire = 1;
CREATE INDEX factions_home_planet_id ON factions (home_planet_id);
CREATE INDEX factions_controlling_faction_id ON factions (controlling_faction_id);
```

What the schema enforces and what the code does:

| Rule                              | Enforced by                                          |
| --------------------------------- | ---------------------------------------------------- |
| Home world has `is_homeworld = 1` | Code, in the assignment transaction                  |
| Seat and planet in the same game  | Code                                                 |
| At most 15 seats per home world   | Code                                                 |
| Same home world, same race        | Schema: `races.planet_id` is unique, and the composite foreign key ties a faction's race to its home world |
| One controller, seat or faction   | Schema: `CHECK`                                      |
| No control cycles                 | Schema rejects self-control; code rejects longer cycles |
| One empire per seat               | Schema: partial unique index                         |
| An empire is controlled by a seat | Schema: `CHECK`                                      |

Neither table stores `game_id`, as `planets` and `deposits` do not: the game
is reached through the home world's star.

### Control cycles

No code changes control yet, so this design adds no cycle check. When a
command or turn step first makes one faction control another, it must check
in the same transaction that the new controller is not the faction itself or
one of the factions it controls, directly or indirectly. A recursive CTE over
`controlling_faction_id` does this.

## Command

`seiicho assign-seat --game ID --seat ID [--planet ID] [--json]`

- The game must exist and be active.
- The seat must be an active player seat in the game with no empire.
- `--planet`, if given, must be a home world in the game with fewer than 15
  seats assigned.
- On success it prints the assignment described in
  [Assignment](#assignment). `--json` prints the same as an object,
  documented in `docs/reference/seiicho-json.md`.
- On failure it prints an error and exits with a nonzero status. Nothing is
  written.

`--game` is redundant with `--seat`, since a seat belongs to one game, but
every game command takes it, and it guards against a mistyped seat ID that
happens to exist in another game.

## Code layout

The rules are queries and checks inside one transaction, so the work lives
in `internal/store` as `AssignSeat`:

```go
// AssignSeat creates seatID's empire on planetID, or on the game's only open
// home world when planetID is 0, and returns it.
func (s *Store) AssignSeat(ctx context.Context, gameID, seatID, planetID int64, now time.Time) (Empire, error)
```

`MaxSeatsPerHomeWorld = 15` is a constant beside it. Choosing the planet
when there are several open home worlds returns an error that carries their
IDs, so the command can list them.

## Tests

- A seat is assigned to a named home world, and the faction is an empire
  named `Unnamed Faction`, controlled by the seat.
- The first seat on a home world creates its race; later seats reuse it.
- A planet that is not a home world, or is in another game, is rejected.
- The 16th seat on a home world is rejected; the 15th is accepted.
- Without `--planet`: one open home world is chosen; none is an error; two
  or more is an error that lists them. A full home world is not open.
- A seat that already has an empire, a gamemaster seat, an inactive seat, a
  seat in another game, and an inactive game are rejected.
- A failed run writes nothing.
- The schema rejects: a faction with both or neither controller; a faction
  controlling itself; an empire controlled by a faction; a second empire for
  a seat; a faction whose race belongs to another home world; a second race
  for a planet.

## Rebuilding a game

The database is the record of a game's factions and races, as it is of its
seats and home worlds. A game rebuilt from its seed gets its assignments
back only if the gamemaster runs `assign-seat` again in the same order,
after the same `flag-home-world` runs. Runs without `--planet` choose the
same planet only if the same home worlds are flagged and filled in the same
order.

## Open questions

The rules above follow the request. These are the gaps I filled or could
not fill; each needs an answer before approval.

1. **Is a race exactly one home world?** This draft makes races and home
   worlds one-to-one: a race is created with its home world and never has
   another. The request says only that factions sharing a home world share a
   race. Could two home worlds share a race?
2. **Can a faction's home world ever change?** The request makes home world
   a faction attribute, not a race attribute, which suggests it might. The
   composite foreign key forbids a faction whose home world differs from its
   race's. If home worlds can move, drop the foreign key and check rule 3 in
   code at assignment only.
3. **Do factions controlled by factions need a home world and race?** This
   draft makes both required for every faction. Nothing creates such
   factions yet.
4. **Do inactive seats count toward the 15?** This draft counts every empire
   on the planet, so deactivating a seat does not free its place. Should it?
5. **Can a gamemaster seat be assigned?** This draft rejects it.
6. **Can a seat be unassigned or moved?** This draft has no command for
   either. A mistaken assignment means rebuilding the game.
7. **Does a race have a name or other attributes?** This draft gives it
   none. Should it have a default name like `Unnamed Race`?
8. **Should `assign-seat` take `--name`?** This draft always uses the
   default and leaves renaming to a later command. (I read "Unamed Faction"
   in the request as `Unnamed Faction`.)
9. **Should `list-seats` show each seat's empire and home world?** This
   draft leaves the existing reports unchanged.
