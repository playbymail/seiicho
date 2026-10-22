# How to generate a game's cluster

A game's board is its cluster: 100 star systems in a cube of coordinates from
0 to 30 on each axis. Generate it once with `seiicho generate-cluster`.

## Before you begin

- The game must exist and be active, and must not have a cluster yet. See
  [How to create a game](create-a-game.md) and
  [How to find account, game, and seat IDs](find-ids.md).

## Generate the cluster

Run `generate-cluster` with the game's ID:

```sh
go run ./cmd/seiicho generate-cluster --env development --db tmp/seiicho.db --game 1
```

```text
generated a cluster of 159 stars for game 1
```

If you leave out `--env` or `--db`, the command uses `SEIICHO_ENV` or
`SEIICHO_DB` instead.

The cluster comes from the seed that the game was created with, so a game
created with the same seed always gets the same cluster. One system has 5
stars, 2 have 4, 7 have 3, 35 have 2, and the remaining 55 have 1: 159 stars
in all. The stars in each system are lettered from `A`.

## If the command fails

If it fails, the command stores nothing.

### `game 1: game not found` or `game 1: game is inactive`

Check the game's ID and whether it is active with `seiicho list-games`.

### `game 1: game already has a cluster`

A game's cluster is generated only once. Nothing was changed.

### `--game is required and must be a positive game ID`

Give the game's ID, such as `--game 1`.
