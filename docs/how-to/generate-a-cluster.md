# How to generate a game's cluster

A game's board is its cluster: 100 star systems in a cube of coordinates from
0 to 30 on each axis, the planets that orbit their stars, and the planets'
resource deposits. Generate it once
with `seiicho generate-cluster`.

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
generated a cluster of 159 stars, 721 planets, and 12895 deposits for game 1
```

If you leave out `--env` or `--db`, the command uses `SEIICHO_ENV` or
`SEIICHO_DB` instead.

The cluster comes from the seed that the game was created with, so a game
created with the same seed always gets the same cluster. One system has 5
stars, 2 have 4, 7 have 3, 35 have 2, and the remaining 55 have 1: 159 stars
in all. The stars in each system are lettered from `A`.

Every star has at least one planet and at most 10, in orbits numbered from 1
with no gaps: 721 planets in all. Each planet is rocky, an asteroid belt, or a
giant. Inner orbits are more likely to be rocky and outer orbits more likely
to be giants, so expect about half rocky, a sixth asteroids, and a third
giants.

Each planet also has a habitability. Rocky planets range from 0 to 24 and
giants from 0 to 15, with values near the middle of each range most common.
Asteroid belts are always 0.

Each planet has from 1 to 30 resource deposits if it is rocky, 1 to 35 if it
is a giant, and 1 to 45 if it is an asteroid belt, with counts near the middle
of each range most common. A deposit holds gold, fuel, metals, or non-metals;
giants never hold gold. Every star's deposits add up to about the same wealth,
so a star with few planets has rich planets. Each deposit also has a yield
from 1% to 25%; gold yields at most 3%, and fuel on rocky planets at most
15%. The number of deposits depends on the seed.

## Check how the planets are spread

To see how many stars have each number of planets, run `show-histogram`:

```sh
go run ./cmd/seiicho show-histogram --env development --db tmp/seiicho.db --game 1 --planets-per-star
```

```text
PLANETS  STARS
1        8      ########
2        13     #############
3        29     #############################
...
```

Use `--planets-per-star-system` instead to count the planets in each star
system, all the stars at one set of coordinates. Give one of the two flags;
add `--json` for the output described in
[seiicho JSON output](../reference/seiicho-json.md).

## Check the planet types and habitability

To see how many planets of each type there are, and the lowest, average, and
highest habitability of each type, run `summarize-planets`:

```sh
go run ./cmd/seiicho summarize-planets --env development --db tmp/seiicho.db --game 1 --by-planet-type
```

```text
TYPE       PLANETS  MIN  AVG   MAX
rocky      360      0    12.0  23
asteroids  105      0    0.0   0
giant      256      0    7.6   14
```

Add `--json` for the output described in
[seiicho JSON output](../reference/seiicho-json.md).

## Choose the order rows are stored in

By default the command stores stars sorted by their coordinates and letter,
and planets sorted by star and then orbit, so two games created with the same
seed get rows in the same order. Sorted planet IDs show how many planets each
star has, which players could use to guess at systems they have not explored.

To store stars in the order they were drawn, or planets in the order they
were allocated, turn the sorts off:

```sh
go run ./cmd/seiicho generate-cluster --env development --db tmp/seiicho.db --game 1 --sort-stars=false --sort-planets=false
```

The flags change only the order of the rows and their IDs, not the cluster,
its planets, or their deposits. Deposits are always stored grouped by planet,
in the order the planets are stored, so their IDs reveal no more than the
planets' IDs do.

## If the command fails

If it fails, the command stores nothing.

### `game 1: game not found` or `game 1: game is inactive`

Check the game's ID and whether it is active with `seiicho list-games`.

### `game 1: game already has a cluster`

A game's cluster is generated only once. Nothing was changed.

### `--game is required and must be a positive game ID`

Give the game's ID, such as `--game 1`.
