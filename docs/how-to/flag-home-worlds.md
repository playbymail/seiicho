# How to flag a game's home worlds

A player can start only on a planet flagged as a home world. Flag them one at
a time with `seiicho flag-home-world`, once for each home world the game
needs. Each run flags one planet, or fails and changes nothing.

## Before you begin

- The game must exist, be active, and have a cluster. See
  [How to generate a game's cluster](generate-a-cluster.md) and
  [How to find account, game, and seat IDs](find-ids.md).

If you leave out `--env` or `--db`, the command uses `SEIICHO_ENV` or
`SEIICHO_DB` instead.

## Flag a home world

Run `flag-home-world` with the game's ID and two distances:

```sh
go run ./cmd/seiicho flag-home-world --env development --db tmp/seiicho.db --game 1 --min-home-distance 6 --max-neighbor-distance 6
```

```text
flagged planet 136 as a home world in game 1: system 5 3 14, star A, orbit 4
```

The command prints the planet's ID, its system's coordinates, its star's
letter, and its orbit. Add `--json` for the output described in
[seiicho JSON output](../reference/seiicho-json.md).

A home world is a rocky planet in orbit 3, 4, or 5 of a star that is alone in
its system. Among those, the command picks one at random, drawing from the
game's seed. From a star with more than one, it takes the planet with the
lowest habitability. Flagging sets the planet's habitability to 25 and makes
its deposits richer: each yield rises by 1 to 10 percentage points, to at most
75%, and each quantity by 25%.

Run the command again for each further home world.

## Choose the distances

Both flags are required, and both measure the distance between star systems,
rounded up to a whole number:

| Flag | Range | Rule |
| --- | --- | --- |
| `--min-home-distance` | 3 to 16 | The new home system is at least this far from every home system. |
| `--max-neighbor-distance` | 1 to 16 | The nearest system that is not a home system is at most this far from the new one. |

The first spreads players apart. The second makes sure no player starts
stranded, with nothing in reach to explore.

**Strict limits make runs fail.** A high `--min-home-distance` rules out
systems near the home worlds already flagged, and a low
`--max-neighbor-distance` rules out isolated systems. Below 3, almost no system
qualifies. No game can have more home worlds than it has lone stars with a
rocky planet in orbits 3 to 5; for the alpha game, that is 34.

For the alpha game, created with seed `0x0123456789abcdef`, repeated runs with
`--min-home-distance 6 --max-neighbor-distance 6` flag 24 home worlds before a
run fails. The table in `internal/cluster/home-worlds.md` gives estimates for
other limits.

## If a run fails because no planet qualifies

```text
seiicho: game 1: no planet qualifies as a home world
```

Nothing was changed. Retry with looser limits: a lower
`--min-home-distance`, a higher `--max-neighbor-distance`, or both. The retry
draws exactly as it would have without the failed run, so a failure leaves no
trace in the game.

## Rebuild a game's home worlds

The database is the record of a game's home worlds. The command does not store
the distances it was given. A game rebuilt from its seed gets the same cluster
but no home worlds. To get the same home worlds back, rerun the successful
`flag-home-world` commands with the same distances, in the same order. Failed
runs changed nothing, so leave them out.

Keep a note of each successful run's distances if you may need to rebuild the
game.

## If the command fails

If it fails, the command changes nothing.

### `game 1: game not found` or `game 1: game is inactive`

Check the game's ID and whether it is active with `seiicho list-games`.

### `game 1: game has no cluster`

Generate the cluster first. See
[How to generate a game's cluster](generate-a-cluster.md).

### `--min-home-distance is required and must be from 3 to 16`

Give `--min-home-distance` a value from 3 to 16. Likewise,
`--max-neighbor-distance` must be from 1 to 16, and `--game` a positive game
ID.
