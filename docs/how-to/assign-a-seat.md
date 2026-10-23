# How to assign a seat to a home world

Give a player seat its empire with `seiicho assign-seat`. The empire is the
seat's first faction, and its home world is where the player starts. Each run
assigns one seat, or fails and changes nothing.

An assignment is permanent. No command moves a seat to another home world or
removes its empire, so a mistake means rebuilding the game.

## Before you begin

- The game must exist and be active, with at least one home world flagged.
  See [How to flag a game's home worlds](flag-home-worlds.md).
- The seat must be an active player seat in the game. Gamemasters never play,
  so a gamemaster seat cannot be assigned. See
  [How to seat an account in a game](seat-an-account.md).
- The command takes the seat's ID, not the account's. To look it up, see
  [How to find account, game, and seat IDs](find-ids.md).

If you leave out `--env` or `--db`, the command uses `SEIICHO_ENV` or
`SEIICHO_DB` instead.

## Assign a seat

If the game has only one open home world, one with fewer than 15 seats
assigned, give just the game and the seat:

```sh
go run ./cmd/seiicho assign-seat --env development --db tmp/seiicho.db --game 1 --seat 2 --name "Red Star"
```

```text
assigned seat 2 in game 1 to planet 136 (system 5 3 14, star A, orbit 4): empire 1 "Red Star", race 1
```

The command prints the home world's planet ID, system coordinates, star
letter, and orbit, then the new empire's faction ID and name and its race's
ID. Add `--json` for the output described in
[seiicho JSON output](../reference/seiicho-json.md).

If the game has more than one open home world, name one with `--planet`:

```sh
go run ./cmd/seiicho assign-seat --env development --db tmp/seiicho.db --game 1 --seat 3 --planet 627
```

```text
assigned seat 3 in game 1 to planet 627 (system 26 18 17, star A, orbit 3): empire 2 "Unnamed Faction", race 2
```

The first seat on a home world creates the race of that world. Every later
seat on it joins the same race.

To see every seat's empire and home world, run
`seiicho list-seats --game 1`.

## Name the empire

Without `--name`, the empire is named `Unnamed Faction`. A name is cleaned
before it is checked:

- Tabs, newlines, and other non-printing characters become spaces.
- Leading and trailing spaces are dropped, and each run of spaces inside
  becomes one.

The cleaned name must be 1 to 25 characters of plain ASCII: letters, digits,
spaces, and punctuation other than `"` and `\`. Accented letters and other
characters outside ASCII are rejected, not replaced. Names need not be unique.

## Fill home worlds in order

A home world holds at most 15 seats. A seat that is later deactivated keeps
its place.

If you flag one home world, assign seats to it until it is full, and only
then flag the next, the game never has more than one open home world, and you
never need `--planet`.

## If the command fails

If it fails, the command changes nothing. Errors name the game and seat you
gave, for example `game 1, seat 2: seat is inactive`.

### `game has no open home world`

Every home world is full, or none has been flagged. Flag another; see
[How to flag a game's home worlds](flag-home-worlds.md).

### `game has more than one open home world: planets 136, 627; name one with --planet`

Rerun the command with `--planet` and one of the listed planet IDs.

### `planet is not a home world in the game`

The planet given to `--planet` is not flagged as a home world, or is in
another game. `seiicho assign-seat` without `--planet` lists the open home
worlds when there are several.

### `home world has the most seats it can hold`

The planet already has 15 seats. Choose another home world.

### `seat not found in the game`, `seat is inactive`, or `seat is a gamemaster seat`

Check the seat with `seiicho list-seats --game ID`. Only an active player seat
can be assigned.

### `seat already has an empire`

The seat has been assigned already. `seiicho list-seats --game ID` shows its
empire and home world.

### `--name: name is empty`, `name is longer than 25 characters`, or `name has a character that is not allowed`

Choose a name that follows [Name the empire](#name-the-empire). The command
checks it before opening the database.

### `--game is required` or `--seat is required`

Give both `--game` and `--seat` as positive IDs, and `--planet`, if you use
it, as a positive planet ID.
