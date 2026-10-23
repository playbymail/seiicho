# Home worlds

How the engine picks the planets that players can start on.

Status: **draft, not approved.** See [Open questions](#open-questions).

A seat can start the game only on a planet that has been flagged as a home
world. Assigning seats to home worlds is a later, separate command. It is not
designed here. This document covers the step before it: finding planets
suited to be home worlds, flagging them, and making them better places to
start.

The operator flags home worlds one at a time with `seiicho flag-home-world`.
Each run flags one planet or fails without changing anything. The operator
runs it once for each home world the game needs.

## Terms

- **System:** all of a game's stars at one (x, y, z).
- **Home system:** a system with a planet flagged as a home world.
- **Non-home system:** any other system.
- **Distance:** the distance between two systems, from their coordinates. It
  is the Euclidean distance rounded up, computed with integers as in issue #1:
  let d² = dx² + dy² + dz² and r = ⌊√d²⌋. The distance is r if r² = d², and
  r + 1 otherwise. Stars in the same system are 0 apart.

## Parameters

The operator gives two distances on every run:

| Flag                      | Range  | Rule                                                              |
| ------------------------- | ------ | ----------------------------------------------------------------- |
| `--min-home-distance`     | 3–16   | Every home system is at least this far from the candidate system. |
| `--max-neighbor-distance` | 1–16   | The nearest non-home system is at most this far from the candidate system. |

The command rejects values outside these ranges before it opens the
database.

The first rule spreads players apart. The second makes sure no home world is
stranded: a player always has another system within reach to explore.

**Warning: a run can fail if the limits are too strict.** A high
`--min-home-distance` rules out systems near home worlds that are already
flagged. A low `--max-neighbor-distance` rules out isolated systems. See
[Expected results](#expected-results) for how many home worlds a game takes.

## Eligibility

A system is a **potential home system** if it has exactly one star.

A star is a **potential home star** if it is the only star in a potential
home system and has at least 5 planets.

A planet is a **potential home planet** if it is rocky, orbits a potential
home star, and is in orbit 3, 4, or 5. A star with at least 5 planets always
has those orbits, since orbits fill from 1 with no gaps.

Each potential home star has at most one candidate planet:

- If it has one potential home planet, that planet is the candidate.
- If it has more than one, the candidate is the one with the lowest
  habitability. If two of them tie, the one in the lower orbit wins.

A star that is already a home star is never a candidate again. Its distance
to its own home system is 0, which is always less than
`--min-home-distance`. So each home system has exactly one home world.

## Step 1: Build the pool

Find every potential home star that has a candidate planet and whose system
is at least `--min-home-distance` from every home system. When no home world
has been flagged yet, every potential home star passes this test.

Do not check `--max-neighbor-distance` yet. Every otherwise eligible star
goes in the pool first.

## Step 2: Order the pool

Sort the pool by `Compare`, then shuffle it. Sorting gives the shuffle the
same input however the stars were read from the database.

The shuffle draws from a new child source:

```go
root.Child(prng.HomeWorlds, prng.Node(n))
```

where `n` is the number of home worlds already flagged in the game. Each run
gets its own source, and running the same commands with the same parameters,
in the same order, on the same game flags the same planets.

## Step 3: Draw

Take stars from the front of the shuffled pool, one at a time. For each star,
find the nearest non-home system other than the star's own. If it is at most
`--max-neighbor-distance` away, the star's candidate planet is the new home
world. Otherwise discard the star and take the next one.

If the pool runs out, the run fails with an error and changes nothing.

## Step 4: Update the planet

For the chosen planet, in one transaction:

1. Set `is_homeworld` to 1.
2. Set habitability to 25.
3. For each of its deposits:
   - raise the yield by 10%, rounding down, to at most 75%;
   - raise the quantity by 25%, rounding down: `quantity × 125 / 100` in
     integer arithmetic.

A planet with no deposits still becomes a home world. The planet's kind,
orbit, deposit numbers, and resources do not change. No deposits are added or
removed.

The command prints the planet it flagged and exits with status 0.

## Random sources

| Node         | Value | Draws                                                   |
| ------------ | ----: | ------------------------------------------------------- |
| `HomeWorlds` |     8 | Step 2: one shuffle per run, from the child keyed by `n` |

Like the other nodes, the value is permanent. Home worlds do not use any
cluster source, so flagging them never changes the stars, planets, or
deposits that a seed generates.

## Storage

Two changes to migration 1, squashed in as usual during alpha. Existing
databases, including `games/alpha`, must be rebuilt.

```sql
-- planets: a home world always has habitability 25, and no other planet
-- does.
is_homeworld INTEGER NOT NULL DEFAULT 0 CHECK (is_homeworld IN (0, 1)),
CHECK ((is_homeworld = 1) = (habitability = 25))

-- deposits: home worlds can raise yields above the generator's 25.
yield_pct INTEGER NOT NULL CHECK (yield_pct BETWEEN 1 AND 75),
```

The `deposits` table comment says a star's deposits add up to its budget.
That stops being true for home stars, so the comment, and
`planetary-resources.md`, must say it applies only to stars that are not home
stars.

## Command

`seiicho flag-home-world --game ID --min-home-distance N --max-neighbor-distance N [--json]`

- The game must exist, be active, and have a cluster.
- On success it prints the planet's ID, its system's coordinates, its star's
  letter, and its orbit. `--json` prints the same as an object, documented in
  `docs/reference/seiicho-json.md`.
- On failure it prints an error and exits with a nonzero status. Nothing is
  written.

## Code layout

Selection is pure and lives in `internal/cluster/homeworlds.go`:

```go
// HomeWorld returns the index in planets of the next home world, or an
// error if none qualifies. homes holds the indexes of planets already
// flagged.
func HomeWorld(stars []Star, planets []Planet, homes []int, minHome, maxNeighbor int, p *prng.PRNG) (int, error)
```

The deposit changes are a separate small function, so tests can pin them
without a database. `store` loads the cluster, calls `HomeWorld`, and writes
the update in one transaction.

## Expected results

From the alpha game (seed `0x0123456789abcdef`): 100 systems, 55 with one
star, 24 of those with at least 5 planets, and **21** with a rocky planet in
orbit 3, 4, or 5. The nearest other system to those 21 stars is between 3 and
10 away, and 3–4 away for 10 of them.

Home worlds flagged before a run fails, by repeated runs with the same
parameters (simulated over 300 shuffles; a range means the shuffle matters):

| Max neighbor ↓ / min home → | 3  | 4  | 6  | 8     | 10 | 12    | 16  |
| --------------------------- | -: | -: | -: | ----: | -: | ----: | --: |
| 2                           | 0  | 0  | 0  | 0     | 0  | 0     | 0   |
| 3                           | 6  | 6  | 5  | 5     | 5  | 4     | 4   |
| 4                           | 10 | 10 | 9  | 8     | 8  | 6     | 4–5 |
| 6                           | 18 | 18 | 16 | 13–14 | 12 | 9–11  | 5–7 |
| 16                          | 21 | 21 | 19 | 15–16 | 14 | 10–13 | 6–8 |

So `--max-neighbor-distance` below 3 is likely to fail on every game, and
games with more than about 20 players cannot be supported at any setting.

## Tests

- Systems with more than one star, stars with fewer than 5 planets, and
  stars with no rocky planet in orbits 3–5 are never chosen.
- With several candidate planets, the lowest habitability wins, then the
  lower orbit.
- No two home systems are closer than `--min-home-distance`.
- Every home system has a non-home system within `--max-neighbor-distance`,
  as measured when it was flagged.
- A star that fails the neighbor check is skipped and the next one is tried.
- An empty pool, or a pool where every star fails, returns an error and the
  database is unchanged.
- The update sets habitability to 25, raises yields and quantities as
  specified, clamps yields at 75, and leaves everything else alone.
- The schema rejects habitability 25 without `is_homeworld`, and the
  reverse.
- The same game, parameters, and run order give the same home worlds; the
  result does not depend on the order of the slices passed in.
- A known seed and parameters give pinned home worlds.
- Distance is exact at the rounding boundary: (0,0,0)–(3,4,0) is 5, and
  (0,0,0)–(1,1,1) is 2.
- Parameters outside their ranges are rejected.

## Open questions

1. **Yield increase.** "Raise by 10%, round down" reads as `yield × 110 /
   100`. That leaves every yield from 1 to 9 unchanged, including all gold,
   and gives at most 27, so the clamp at 75 never applies. The alternative is
   10 percentage points (`yield + 10`, at most 35), but then there is nothing
   to round. Which one is meant, and is 75 the right cap?
2. **Lowest habitability.** When a star has several candidate planets, the
   one with the *lowest* habitability becomes the home world, which leaves
   the better planets for colonies. Is that intended?
3. **Budgets.** Raising quantities by 25% breaks "a star's deposits add up to
   its budget" for home stars, and the deposit reports will include the
   raised quantities. Is that acceptable, or should the reports leave home
   worlds out?
4. **Recording the runs.** Home worlds depend on the operator's parameters
   and the order of runs, not on the seed alone, so rebuilding a game from its
   seed does not bring them back. Should the parameters of each run be
   stored?
