# Seat assignment

How the gamemaster gives a seat its starting faction on a home world.

Status: **approved and implemented** in `factions.go` (`AssignSeat`), with
name cleaning in `internal/names`.

A seat can start the game only on a planet that has been flagged as a home
world (see `internal/cluster/home-worlds.md`). This document covers the step
after flagging: assigning a seat to one of those planets. Assignment creates
the seat's first faction, its empire, and the race that faction belongs to.

The operator assigns seats one at a time with `seiicho assign-seat`. Each run
assigns one seat or fails without changing anything. Nothing in it is random.
An assignment is permanent: there is no command to unassign or move a seat.

## Terms

- **Faction:** a body that acts in the game. Every faction has a name and is
  controlled by exactly one seat or exactly one other faction. It may have a
  home world and a race.
- **Empire:** the first faction assigned to a seat. A seat has at most one,
  and it always has a home world and a race.
- **Minion:** a faction controlled by another faction. Its home world and
  race are optional. Nothing creates minions yet.
- **Race:** the people of exactly one home world. Every faction whose home
  world is a given planet is of that planet's race. A race has no other
  attributes.
- **Seat assigned to a home world:** a seat whose empire has that planet as
  its home world, whether the seat is active or not.
- **Open home world:** a planet with `is_homeworld = 1` that has fewer than
  15 seats assigned to it.

"Regent" is reserved for a later kind of faction and is not defined here.

## Rules

1. A seat can be assigned only to a planet with `is_homeworld = 1` in the
   seat's game.
2. No more than **15** seats can be assigned to one home world. A seat that
   becomes inactive keeps its place.
3. A race belongs to exactly one home world, and every faction with that home
   world is of that race.
4. A faction's home world and race are fixed when it is created. A faction
   can lose its home world in play, but the relationship never changes.
5. Every faction is controlled by either a seat or another faction, never
   both and never neither.
6. Control has no cycles: following controlling factions from any faction
   always ends at a faction controlled by a seat.
7. A new faction is named `Unnamed Faction` unless the operator names it.
   Every name follows [Names](#names).
8. The first faction assigned to a seat is its empire (`is_empire = 1`).
9. Gamemasters never play. A gamemaster seat cannot be assigned, and a seat
   with an empire cannot become a gamemaster seat.

This command creates only empires, and an empire is controlled by its seat,
so it can never create a cycle. Rule 6 binds whatever later changes control
of a faction; see [Control cycles](#control-cycles).

## Names

These rules apply to every name in the game, not just faction names. A
name is cleaned, then checked:

1. Replace every non-printing character with a space. That is every
   character Go's `unicode.IsPrint` rejects: ASCII control characters
   (U+0000–U+001F and U+007F), and non-printing Unicode such as U+00A0.
2. Trim leading and trailing spaces, and replace each run of internal spaces
   with one space.
3. Reject the name unless it is 1 to 25 characters, every one of them
   printable ASCII (U+0020–U+007E) other than `"` and `\`.

So the stored name is printable ASCII that JSON never needs to escape.
`<`, `>`, and `&` are allowed. The `--json` output keeps Go's default
encoder, which escapes them anyway: `R&D` prints as `R\u0026D`, which is
still the same JSON string.
Other non-ASCII characters, such as `é`, are rejected rather than replaced.
Names need not be unique.

The cleaning lives in one function that every command taking a name calls
before it writes. In zsh, `seiicho assign-seat --name $'  Red\tStar  '`
passes a tab between the words and stores `Red Star`. A name that is empty after cleaning, too long, or has a
rejected character fails the run before the database is opened.

The schema checks the result too, so a name written by any other path
still follows the rules:

```sql
CHECK (length(name) BETWEEN 1 AND 25
	AND name NOT GLOB '*[^ -~]*'
	AND instr(name, '"') = 0
	AND instr(name, '\') = 0
	AND name = trim(name, ' ')
	AND instr(name, '  ') = 0)
```

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
4. Create the empire: named by `--name` or `Unnamed Faction`,
   `is_empire = 1`, controlled by the seat, with the planet as its home world
   and the planet's race.

The command prints the seat, the new faction's ID and name, the race's ID,
and the home world's planet ID, system coordinates, star letter, and orbit.

## Storage

Two new tables and a trigger, squashed into migration 1 as usual during
alpha. Existing databases, including `games/alpha`, must be rebuilt.

```sql
-- A race is the people of exactly one home world. It is created when the
-- first seat is assigned to that planet. The second unique key is the target
-- of factions' composite foreign key.
CREATE TABLE races (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	planet_id INTEGER NOT NULL UNIQUE REFERENCES planets (id),
	UNIQUE (id, planet_id)
) STRICT;

-- A faction acts in the game. It is controlled by exactly one seat or
-- exactly one other faction. Control must not form cycles; the schema
-- rejects only a faction controlling itself, so the code that changes
-- control checks the rest. A faction has both a race and a home world or
-- neither, and the composite foreign key makes them agree; neither changes
-- once set. A seat's first faction is its empire: a seat has at most one,
-- and an empire always has a home world.
CREATE TABLE factions (
	id                     INTEGER PRIMARY KEY AUTOINCREMENT,
	name                   TEXT    NOT NULL DEFAULT 'Unnamed Faction'
		CHECK (length(name) BETWEEN 1 AND 25
			AND name NOT GLOB '*[^ -~]*'
			AND instr(name, '"') = 0
			AND instr(name, '\') = 0
			AND name = trim(name, ' ')
			AND instr(name, '  ') = 0),
	race_id                INTEGER,
	home_planet_id         INTEGER,
	is_empire              INTEGER NOT NULL DEFAULT 0 CHECK (is_empire IN (0, 1)),
	seat_id                INTEGER REFERENCES seats (id),
	controlling_faction_id INTEGER REFERENCES factions (id),
	created_at             INTEGER NOT NULL,
	FOREIGN KEY (race_id, home_planet_id) REFERENCES races (id, planet_id),
	CHECK ((race_id IS NULL) = (home_planet_id IS NULL)),
	CHECK ((seat_id IS NULL) <> (controlling_faction_id IS NULL)),
	CHECK (controlling_faction_id <> id),
	CHECK (is_empire = 0 OR (seat_id IS NOT NULL AND home_planet_id IS NOT NULL))
) STRICT;

CREATE UNIQUE INDEX factions_seat_empire ON factions (seat_id) WHERE is_empire = 1;
CREATE INDEX factions_home_planet_id ON factions (home_planet_id);
CREATE INDEX factions_controlling_faction_id ON factions (controlling_faction_id);

CREATE TRIGGER factions_home_is_fixed
BEFORE UPDATE OF race_id, home_planet_id ON factions
WHEN OLD.race_id IS NOT NEW.race_id OR OLD.home_planet_id IS NOT NEW.home_planet_id
BEGIN
	SELECT RAISE(ABORT, 'a faction''s race and home world cannot change');
END;
```

Losing a home world in play does not touch `home_planet_id`. When ownership
of planets is designed, it will be recorded separately.

What the schema enforces and what the code does:

| Rule                              | Enforced by                                          |
| --------------------------------- | ---------------------------------------------------- |
| Home world has `is_homeworld = 1` | Code, in the assignment transaction                  |
| Seat and planet in the same game  | Code                                                 |
| At most 15 seats per home world   | Code                                                 |
| One race per home world, and factions agree with it | Schema: `races.planet_id` is unique, and the composite foreign key ties a faction's race to its home world |
| Race and home world are fixed     | Schema: trigger                                      |
| One controller, seat or faction   | Schema: `CHECK`                                      |
| No control cycles                 | Schema rejects self-control; code rejects longer cycles |
| One empire per seat               | Schema: partial unique index                         |
| An empire has a seat and a home world | Schema: `CHECK`                                  |
| Gamemasters never play            | Code, in `assign-seat` and `promote-seat`            |

Neither table stores `game_id`, as `planets` and `deposits` do not: the game
is reached through the home world's star. A minion with no home world is
reached through its controlling faction.

### Control cycles

No code changes control yet, so this design adds no cycle check. When a
command or turn step first makes one faction control another, it must check
in the same transaction that the new controller is not the faction itself or
one of the factions it controls, directly or indirectly. A recursive CTE over
`controlling_faction_id` does this.

## Commands

### `assign-seat`

`seiicho assign-seat --game ID --seat ID [--planet ID] [--name NAME] [--json]`

- The game must exist and be active.
- The seat must be an active player seat in the game with no empire.
- `--planet`, if given, must be a home world in the game with fewer than 15
  seats assigned.
- `--name`, if given, names the empire, cleaned and checked as in
  [Names](#names).
- On success it prints the assignment described in
  [Assignment](#assignment). `--json` prints the same as an object,
  documented in `docs/reference/seiicho-json.md`.
- On failure it prints an error and exits with a nonzero status. Nothing is
  written.

`--game` is redundant with `--seat`, since a seat belongs to one game, but
every game command takes it, and it guards against a mistyped seat ID that
happens to exist in another game.

### `promote-seat`

`promote-seat` fails if the seat has an empire, and writes nothing. Its
package comment and how-to say so.

### `list-seats`

`list-seats` adds the seat's empire, if it has one: its faction ID, its
name, and its home world's planet ID. Seats without an empire, including
every gamemaster seat, leave them blank. In `--json`, the seat gains three
fields, `null` without an empire:

| Field | Type | Meaning |
| --- | --- | --- |
| `empire_id` | number or null | ID of the seat's empire |
| `empire_name` | string or null | Name of the seat's empire |
| `home_planet_id` | number or null | ID of the empire's home world |

## Code layout

The rules are queries and checks inside one transaction, so the work lives
in `internal/store` as `AssignSeat`:

```go
// AssignSeat creates seatID's empire on planetID, or on the game's only open
// home world when planetID is 0, and returns it. An empty name means the
// default.
func (s *Store) AssignSeat(ctx context.Context, gameID, seatID, planetID int64, name string, now time.Time) (Empire, error)
```

`MaxSeatsPerHomeWorld = 15` is a constant beside it. Choosing the planet
when there are several open home worlds returns an error that carries their
IDs, so the command can list them. `PromoteSeat` gains the empire check, and
`Seats` returns each seat's empire.

## Tests

- A seat is assigned to a named home world, and the faction is an empire
  named `Unnamed Faction`, controlled by the seat. With `--name`, it has
  that name, cleaned.
- Name cleaning: non-printing characters become spaces; spaces are trimmed
  and runs compressed; 25 characters are accepted and 26 rejected; a name
  that is empty after cleaning, or has `"`, `\`, or non-ASCII such as `é`,
  is rejected.
- The first seat on a home world creates its race; later seats reuse it.
- A planet that is not a home world, or is in another game, is rejected.
- The 16th seat on a home world is rejected; the 15th is accepted. A
  deactivated seat still counts.
- Without `--planet`: one open home world is chosen; none is an error; two
  or more is an error that lists them. A full home world is not open.
- A seat that already has an empire, a gamemaster seat, an inactive seat, a
  seat in another game, and an inactive game are rejected.
- `promote-seat` rejects a seat with an empire and leaves it a player seat.
- `list-seats` shows the empire of assigned seats and blanks for the rest,
  in both text and JSON.
- A failed run writes nothing.
- The schema rejects: a faction with both or neither controller; a faction
  controlling itself; an empire controlled by a faction; an empire with no
  home world; a second empire for a seat; a race without a home world or the
  reverse; a faction whose race belongs to another home world; a second race
  for a planet; and any change to a faction's race or home world, including
  setting them on a minion created without them; and a name that breaks
  the rules in [Names](#names).
- The schema accepts a minion with no race or home world.

## Rebuilding a game

The database is the record of a game's factions and races, as it is of its
seats and home worlds. A game rebuilt from its seed gets its assignments
back only if the gamemaster runs `assign-seat` again in the same order,
after the same `flag-home-world` runs. Runs without `--planet` choose the
same planet only if the same home worlds are flagged and filled in the same
order.

## Settled in review

- A race is exactly one home world, and has no other attributes.
- A faction's race and home world never change. A minion created without
  them never gains them.
- A deactivated seat keeps its place on its home world.
- Gamemasters never play.
- Seats are never unassigned or moved.
- Names follow [Names](#names) and need not be unique.
