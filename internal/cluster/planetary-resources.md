# Planetary resources

How the cluster generator gives each planet a kind, a habitability, and its
resource deposits.

Status:

- **Planet kinds: approved and implemented** in `planets.go`
  (`KindWeights`, drawn from the `prng.PlanetKinds` source).
- **Habitability: approved and implemented** in `planets.go`
  (`drawHabitability`, drawn from the `prng.Habitability` source).
- **Deposits: approved and implemented** in `deposits.go` (`Deposits`
  and `Budgets`, drawn from the `prng.ResourceBudgets`, `prng.Deposits`, and
  `prng.DepositYields` sources).

We are generating a game world, not simulating planetary formation. The
result should be plausible, deterministic, and easy to explain.

Generation has three separate steps:

1. **Planet kind.** Weighted random selection, influenced by the planet's
   orbit and its star's number of planets.
2. **Habitability.** A value within the range allowed for that kind.
3. **Deposits.** Each star's resource budget divided among deposits on its
   planets.

## Background: how planets are allocated

`generate-cluster` gives every game 721 planets across 159 stars. Each star
gets one planet, and the remaining 562 are allocated at random with
replacement. A star leaves the eligible pool once it has 10 planets. Orbits
fill from 1 with no gaps, so orbit number is a position in the sequence, not a
distance.

Orbits belong to a star, not a star system. A system with several stars has
each star's planets counted separately, so "planets" below always means the
planets orbiting one star.

Simulated over 2,000 games, the average number of stars with each number of
planets is:

| Planets | 1   | 2    | 3    | 4    | 5    | 6    | 7    | 8   | 9   | 10  |
| ------- | --- | ---- | ---- | ---- | ---- | ---- | ---- | --- | --- | --- |
| Stars   | 4.5 | 16.1 | 28.8 | 34.5 | 30.3 | 21.7 | 12.4 | 6.3 | 2.8 | 1.6 |

## Step 1: Assign planet kinds

Each planet's kind is drawn from weights that depend on its star's number of
planets and its orbit. Each draw is independent. No combinations or
arrangements of kinds within a system are required.

### The matrix

Each cell lists the weights `rocky,asteroids,giant` for a planet in that orbit
(column) of a star with that many planets (row). Every cell sums to 40. For
example, `20,6,14` means a 20 in 40 chance of rocky, 6 in 40 of asteroids, and
14 in 40 of giant. A dash marks an orbit the star does not have.

| Planets | 1        | 2        | 3        | 4        | 5        | 6        | 7        | 8        | 9        | 10       |
| ------- | -------- | -------- | -------- | -------- | -------- | -------- | -------- | -------- | -------- | -------- |
| 1       | 20,6,14  | —        | —        | —        | —        | —        | —        | —        | —        | —        |
| 2       | 26,6,8   | 18,6,16  | —        | —        | —        | —        | —        | —        | —        | —        |
| 3       | 29,5,6   | 21,6,13  | 17,7,16  | —        | —        | —        | —        | —        | —        | —        |
| 4       | 31,5,4   | 24,6,10  | 18,7,15  | 15,7,18  | —        | —        | —        | —        | —        | —        |
| 5       | 32,4,4   | 27,5,8   | 21,7,12  | 16,7,17  | 12,8,20  | —        | —        | —        | —        | —        |
| 6       | 32,4,4   | 27,5,8   | 23,7,10  | 18,7,15  | 12,8,20  | 10,8,22  | —        | —        | —        | —        |
| 7       | 32,4,4   | 29,5,6   | 23,7,10  | 19,7,14  | 13,8,19  | 10,8,22  | 9,8,23   | —        | —        | —        |
| 8       | 32,4,4   | 29,5,6   | 26,6,8   | 21,7,12  | 15,8,17  | 11,8,21  | 9,8,23   | 7,8,25   | —        | —        |
| 9       | 32,4,4   | 29,5,6   | 26,6,8   | 21,7,12  | 16,8,16  | 13,8,19  | 10,8,22  | 7,8,25   | 6,8,26   | —        |
| 10      | 34,4,2   | 30,4,6   | 28,6,6   | 24,6,10  | 18,8,14  | 14,8,18  | 12,8,20  | 8,8,24   | 6,8,26   | 6,8,26   |

### How the matrix is derived

A cell is half "how far out the orbit is" and half "how close it is to the
star's outermost orbit":

1. Each orbit has a row of weights, `A(orbit)`, summing to 20:

   | Orbit | 1  | 2  | 3  | 4  | 5 | 6 | 7  | 8  | 9  | 10 |
   | ----- | -- | -- | -- | -- | - | - | -- | -- | -- | -- |
   | Rocky | 17 | 15 | 14 | 12 | 9 | 7 | 6  | 4  | 3  | 3  |
   | Ast.  | 2  | 2  | 3  | 3  | 4 | 4 | 4  | 4  | 4  | 4  |
   | Giant | 1  | 3  | 3  | 5  | 7 | 9 | 10 | 12 | 13 | 13 |

   Rocky planets are likely in the inner orbits and giants in the outer
   ones, with asteroid belts turning up occasionally throughout.

2. For orbit *k* of a star with *n* planets, scale the orbit to a star with
   10 planets: `r = ceil(10 × k / n)`.
3. The cell is `A(k) + A(r)`.

For a star with 10 planets, `r = k`, so each cell is `A(k)` doubled. For a
star with fewer planets, the outer planets are pulled towards the outer-orbit
weights. The outermost planet of every star uses `A(10)` for its half, so
every star has some chance of an outer giant. A lone planet is both innermost
and outermost and gets `A(1) + A(10)`.

To retune the kinds, edit `A(orbit)`, not individual cells.
`TestKindWeights` pins the matrix above, so update both together.

### Drawing a kind

Once every star has all its planets, the generator visits the stars in
(x, y, z, seq) order and each star's planets in orbit order. For each planet
it rolls 1–40 and compares the roll with the cell's cumulative weights. For
`20,6,14`: 1–20 is rocky, 21–26 is asteroids, and 27–40 is giant.

Kinds are drawn from their own child source, `prng.PlanetKinds`, separate
from the allocation source. Changing the kind weights leaves the allocation
unchanged, and habitability will get a source of its own.

### Expected overall mix

Using the simulated star sizes above, a game's 721 planets come out at
about:

| Rocky     | Asteroids | Giant     |
| --------- | --------- | --------- |
| 376 (52%) | 115 (16%) | 230 (32%) |

## Step 2: Assign habitability

Once a planet has its kind, the generator draws its habitability from the
planet's kind alone. Orbit has no influence.

| Kind      | Habitability | Draw                                 | Average |
| --------- | ------------ | ------------------------------------ | ------- |
| Rocky     | 0–24         | Two draws from 0–24, averaged        | 12      |
| Asteroids | 0            | None                                 | 0       |
| Giant     | 0–15         | Two draws from 0–15, averaged        | 7.5     |

"Averaged" means the sum of the two draws divided by 2, rounded down. In the
code that is `Roll(2, 0, 24) / 2` for rocky planets and `Roll(2, 0, 15) / 2`
for giants.

Averaging two draws makes values near the middle of the range common and
values at either end rare. A rocky planet has habitability 12 about 1 time in
13, and 0 or 24 about 1 time in 300.

Habitability is drawn in the same order as kinds: stars in (x, y, z, seq)
order, and each star's planets in orbit order. It comes from its own child
source, `prng.Habitability`. Asteroid belts draw nothing.

### Storage

The `planets` table stores habitability as
`INTEGER NOT NULL DEFAULT 0 CHECK (habitability BETWEEN 0 AND 25)`.

The check allows 25, but the generator never draws it: rocky planets top out
at 24. Habitability 25 is reserved for players' home worlds, which a later,
separate process will assign. That process is not designed yet.

## Step 3: Assign deposits

Once every planet has its kind and habitability, the generator gives each
planet its resource deposits. Deposits change neither the kinds nor the
habitability.

### Goals

- Every star holds about the same wealth: its budget, X.
- A star allocates all of X. No resource is lost because no planet can hold
  it.
- Fewer planets per star means more wealth per planet.
- Each planet kind has its own resource mix.
- Across the whole game, the resources come out at a 1:3:4:4 ratio.
- The same seed always gives the same deposits.

Fewer deposits make a planet easier to exploit, and fewer planets make a star
easier to exploit. A one-planet star with only a few deposits is the prize
for an economic victory.

### Approach

The generator decides which deposits exist first, and divides X among them
second. Every deposit's resource is eligible on its planet, and X is divided
only among deposits that exist. So all of X is allocated, and nothing has to
be discarded.

Deposits are assigned in five steps:

1. **Budget.** Draw X for each star.
2. **Deposit counts.** Draw how many deposits each planet has.
3. **Resources.** Choose each deposit's resource.
4. **Quantities.** Divide X among the star's deposits.
5. **Yields.** Draw a yield for every deposit.

### Resources

| Resource   | Code   | Game-wide share |
| ---------- | ------ | --------------: |
| Gold       | `gold` |   1/12 (8.3%)   |
| Fuel       | `fuel` |   3/12 (25%)    |
| Metals     | `mets` |   4/12 (33.3%)  |
| Non-metals | `nmts` |   4/12 (33.3%)  |

Resource codes are stored in lowercase, as planet kinds are. Gold is not a
strategic resource, because the engine rules no longer use it for trading.

### Canonical order

Every step visits things in this order:

- stars in (x, y, z, seq) order, as `cluster.Compare` sorts them;
- within a star, planets in orbit order;
- within a planet, deposits in deposit-number order;
- resources in the order gold, fuel, mets, nmts.

A deposit's number is its position on its planet, from 1, in the order the
deposit was created.

### Step 3.1: Budget

```
X = StarBudget × Roll(2, 425, 575) / 1000        StarBudget = 4,080,000,000
```

The division rounds down. X runs from 85% to 115% of `StarBudget`, from
3,468,000,000 to 4,692,000,000. It is triangular, with the most likely value
at 100%.

X does not depend on the star's planets. Every star makes exactly one budget
roll, so changing planet kinds or deposit rules never changes a budget.

Budgets are not stored. X depends only on the game's seed and the star's
place in canonical order, so it can be recomputed whenever it is needed.
Every game has its own seed, so every game has different budgets. Since all
of X is allocated, a star's budget also equals the sum of its deposits.

### Step 3.2: Deposit counts

Each planet draws its number of deposits from its kind's limit:

| Kind      | Deposits | Most likely |
| --------- | -------- | ----------- |
| Rocky     | 1–30     | 15–16       |
| Giant     | 1–35     | 18          |
| Asteroids | 1–45     | 23          |

```
s     = max − 1
count = 1 + Roll(1, 0, s / 2) + Roll(1, 0, s − s / 2)
```

Here `s / 2` rounds down. Adding two dice makes the count triangular, with
its most likely value in the middle of the range. Rocky planets have an odd
span (29), so the two dice differ by one and the peak covers two values.

The count is drawn independently of the planet's wealth, and it is the only
thing that decides how easy the planet is to exploit.

### Step 3.3: Resources

Each deposit's resource is chosen using the eligibility weights for its
planet's kind. A weight of 0 means the resource never appears on that kind.

| Resource | Rocky | Asteroids | Giant |
| -------- | ----: | --------: | ----: |
| gold     |     3 |         3 |     0 |
| fuel     |     3 |         2 |     3 |
| mets     |     3 |         6 |     2 |
| nmts     |     2 |         2 |     6 |

To choose, the generator rolls `Roll(1, 1, total)`, where `total` is the sum
of the kind's weights, and compares the roll with the cumulative weights in
resource order. This works the same way as drawing a planet kind.

These weights set how *often* each resource appears on a kind. How *much* a
deposit holds is set in Step 3.4.

### Step 3.4: Quantities

#### Deposit weights

Every deposit gets a weight:

```
weight = Value[kind][resource] × Roll(2, 1, 6)
```

`Roll(2, 1, 6)` runs from 2 to 12, most likely 7. It varies the size of
deposits of the same resource and kind by up to a factor of six.

`Value` holds each kind's resource preferences. It is tuned so that the game
as a whole comes out at 1:3:4:4:

| Resource | Rocky | Asteroids | Giant |
| -------- | ----: | --------: | ----: |
| gold     |    44 |        52 |     0 |
| fuel     |    81 |       169 |   103 |
| mets     |   121 |        85 |   176 |
| nmts     |   182 |       150 |    73 |

#### Dividing X

Let n be the number of deposits the star has, and W the sum of their
weights. Each deposit receives:

```
R        = X − n × MinDeposit                     MinDeposit = 99,999
quantity = MinDeposit + R × weight / W
```

The division rounds down. Rounding leaves fewer than n units over, and the
first of those deposits in canonical order each get one more unit. The
quantities then add up to exactly X.

**Invariant.** A star has at most 10 × 45 = 450 deposits, which need only
45 million units at `MinDeposit` each. Since X is at least 3.468 billion, R is
always positive. If the constants are retuned and R would be negative, the
generator panics.

A deposit has no maximum. `R × weight` is at most about 10¹³, which fits in
an `int64`.

Each star divides X among all of its deposits, not equally among its planets.
As a result:

- A planet's share depends on how many deposits it has and what they hold.
- A lone planet receives all of X, however many deposits it has.
- A lone planet with few deposits holds huge deposits. With three deposits,
  each holds about 1.3 billion units.

#### Where `Value` comes from

The target is each kind's share of wealth by resource. These shares follow
the preference orders for each kind:

| Kind      | gold | fuel | mets | nmts | Order                       |
| --------- | ---: | ---: | ---: | ---: | --------------------------- |
| Rocky     |  12% |  22% |  33% |  33% | mets = nmts > fuel > gold   |
| Asteroids |  12% |  26% |  39% |  23% | mets > fuel > nmts > gold   |
| Giant     |   0% |  28% |  32% |  40% | nmts > mets > fuel          |

The shares were tuned by simulation so that the game-wide total comes out at
1:3:4:4. Each value is then

```
Value = share × (sum of the kind's eligibility weights) / eligibility weight
```

rounded to the nearest integer. Dividing by the eligibility weight cancels
out how often the resource appears. Multiplying by the sum of the kind's
weights gives every kind the same average weight per deposit, so no kind is
richer per deposit than another.

A side effect: a resource that appears rarely on a kind, but makes up a large
share of its wealth, comes in a few large deposits. Mets on giants and nmts
on asteroids are examples.

**To retune,** edit the share table, recompute `Value`, and check the result
with `summarize-deposits --by-resource`. Keep the two tables in step with each
other and with the test that pins `Value`.

### Step 3.5: Yields

After all quantities are final, the generator draws a yield for every
deposit, in canonical order. A yield is an integer percentage, independent
of quantity:

| Resource | Rocky | Asteroids | Giant       |
| -------- | ----- | --------- | ----------- |
| gold     | 1–3   | 1–3       | not allowed |
| fuel     | 1–15  | 1–25      | 1–25        |
| mets     | 1–25  | 1–25      | 1–25        |
| nmts     | 1–25  | 1–25      | 1–25        |

```
yield = lo + Roll(2, 0, (hi − lo) / 2)
```

Every range spans an even number of steps, so this is a symmetric triangle
with its most likely value at the midpoint:

- 1–3 gives 1, 2, or 3 with chances ¼, ½, ¼.
- 1–15 is most likely 8.
- 1–25 is most likely 13.

Do not copy habitability's `Roll(2, lo, hi) / 2` here. On 1–3 it gives 1, 2,
or 3 with chances 3/9, 5/9, 1/9, which leans towards the low end.

### Random sources

#### What the `prng.Node` comment means

`internal/prng` says of its path nodes:

> Values are permanent: never renumber or reuse one.

Each stage of generation draws from a child of the game's root source,
`root.Child(node)`. The child's seed is computed by mixing the root's seed
with the node's numeric value, so the value itself determines every number
that stage draws.

Games are rebuilt from their stored seeds. If a node's value changes, the
stage it names draws different numbers, and every existing game would rebuild
into a different cluster. If a retired value is reused, a new stage gets the
old stage's numbers, which ties two unrelated stages together.

Giving each stage its own node also means that adding or removing draws in one
stage cannot shift the numbers another stage sees. That is why kinds and
habitability have separate sources.

So new stages get new values, assigned once and never changed.

#### New nodes

| Node              | Value | Draws                                            |
| ----------------- | ----: | ------------------------------------------------ |
| `ResourceBudgets` |     5 | Step 3.1: one roll per star                        |
| `Deposits`        |     6 | Steps 3.2–3.4: per planet, a count; per deposit, a resource and a weight |
| `DepositYields`   |     7 | Step 3.5: one roll per deposit                     |

All three sources are consumed in canonical order. A star's draws from
`Deposits` and `DepositYields` depend on its planets, so changing planet kinds
shifts those draws for later stars. Habitability already behaves this way.
Budgets are never affected.

### Storage

The schema has one table for deposits:

```sql
-- A deposit of one resource on a planet. no numbers a planet's deposits
-- from 1 in creation order. A star's deposits add up to its budget,
-- which is recomputed from the game's seed rather than stored.
CREATE TABLE deposits (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	planet_id INTEGER NOT NULL REFERENCES planets (id),
	no        INTEGER NOT NULL CHECK (no BETWEEN 1 AND 45),
	resource  TEXT    NOT NULL CHECK (resource IN ('gold', 'fuel', 'mets', 'nmts')),
	quantity  INTEGER NOT NULL CHECK (quantity >= 99999),
	yield_pct INTEGER NOT NULL CHECK (yield_pct BETWEEN 1 AND 25),
	UNIQUE (planet_id, no)
) STRICT;
```

### Code layout

Deposit generation lives in `internal/cluster/deposits.go`, separate from
`planets.go`. It has one small function per step: budget, deposit count,
resource choice, weight, division, and yield. Its entry point is:

```go
func Deposits(stars []Star, planets []Planet, budgets, deposits, yields *prng.PRNG) []Deposit
```

`store.GenerateCluster` calls it after `Planets` and stores the result.

### Expected results

These figures come from simulating 300 games with Python's random generator,
using the `Value` table above. They are estimates, not pinned outputs.

#### Resource mix

| Mix                       | gold  | fuel  | mets  | nmts  | Share of wealth |
| ------------------------- | ----: | ----: | ----: | ----: | --------------: |
| Whole game (target 8.3 / 25 / 33.3 / 33.3) | 8.3% | 24.8% | 33.7% | 33.2% | 100% |
| Rocky                     | 12.2% | 22.2% | 32.9% | 32.6% |           48.8% |
| Asteroids                 | 12.1% | 25.7% | 39.3% | 22.9% |           18.9% |
| Giant                     |  0.0% | 28.1% | 31.7% | 40.2% |           32.2% |

#### Planets

| Kind      | Deposits (average) | Wealth per planet, 10th / 50th / 90th percentile |
| --------- | -----------------: | ------------------------------------------------ |
| Rocky     |               15.5 | 301M / 720M / 1,499M                             |
| Asteroids |               23.0 | 430M / 958M / 1,845M                             |
| Giant     |               18.0 | 336M / 775M / 1,594M                             |

#### Wealth per planet, by number of planets per star

Every star allocates all of X, so a planet's average wealth is X divided by
the star's number of planets:

| Planets per star  | 1     | 2     | 3     | 4     | 5   | 6   | 7   | 8   | 9   | 10  |
| ----------------- | ----- | ----- | ----- | ----- | --- | --- | --- | --- | --- | --- |
| Average (million) | 4,073 | 2,037 | 1,360 | 1,019 | 815 | 680 | 584 | 510 | 452 | 405 |

#### Deposits

| Measure                                | Result                     |
| -------------------------------------- | -------------------------- |
| Deposits per game                      | 12,600                     |
| Quantity, 1st / 10th / 50th / 90th / 99th percentile | 7M / 15M / 38M / 99M / 220M |
| Largest deposit                        | 4.2 billion                |
| Deposits over 99,999,999               | 9.9%                       |
| One-planet stars per game              | 4.7                        |
| One-planet stars with ≤ 5 deposits     | 0.22 per game              |
| One-planet stars with ≤ 10 deposits    | 0.85 per game              |

### Statistics

These reports follow the existing `summarize` and `show-histogram`
conventions: a table by default and a JSON object with `--json`, documented in
`docs/reference/seiicho-json.md`.

`seiicho summarize-deposits --game ID (--by-resource | --by-planet-type | --by-planets-per-star) [--json]`

- `--by-resource`: one row per resource, in resource order. Columns:
  quantity, share of the game's total, deposits, and the lowest, average, and
  highest quantity per star. This is the check on the 1:3:4:4 tuning.
- `--by-planet-type`: one row per kind, in the order rocky, asteroids, giant.
  Columns: planets; the lowest, average, and highest deposits per planet; the
  lowest, average, and highest wealth per planet; and the share of each
  resource.
- `--by-planets-per-star`: one row for each number of planets from 1 to 10.
  Columns: stars, average wealth per star, and average wealth per planet. This
  is the check that stars stay equal and that small stars concentrate their
  wealth.

`seiicho show-histogram --game ID (--deposit-quantity | --deposit-yield | --deposits-per-planet) [--json]`

- `--deposit-quantity`: bins on a logarithmic scale, because quantities run
  from about 100 thousand to several billion.
- `--deposit-yield`: one bin per percentage from 1 to 25.
- `--deposits-per-planet`: one bin per count from 1 to 45.

The existing histograms draw one `#` per star. Deposit counts are in the
thousands, so these histograms need their bars scaled. Their JSON bins also
need fields other than `planets`. Both details are settled when the reports
are implemented.

### Tests

#### Budgets

- Every X lies between 85% and 115% of `StarBudget`.
- X is the same whatever the star's planets are.
- A star's deposits add up to exactly X.
- Recomputing X from the seed gives the stored deposits' total.

#### Deposits

- Every planet's deposit count lies between 1 and its kind's maximum.
- Every quantity is at least `MinDeposit`.
- Gold never appears on giants.
- Every resource is one of the four codes.
- Deposit numbers run from 1 without gaps.
- The rounding remainder is less than the number of deposits and goes to the
  first deposits in canonical order.
- `Value` matches the share table, and each kind's preference order holds.

#### Yields

- Gold never exceeds 3%.
- Fuel on rocky planets never exceeds 15%.
- No other yield exceeds 25%.
- Every yield is at least 1%.

#### Reproducibility

- The same stars, planets, and seed give the same deposits and yields.
- A known seed gives pinned outputs, in the same way that `TestKindWeights`
  and the PRNG tests pin theirs.
- Different seeds give different results.
- Generation works for stars with 1 to 10 planets, including stars with only
  giants.
- Changing planet kinds does not change any budget.

### Left to playtesting

These are deliberate choices. Playtesting will show whether they need to be
adjusted.

1. **Jackpots are rare.** Triangular counts give about one one-planet star
   with five or fewer deposits every five games. One-planet stars use the
   same deposit-count draw as every other star.
2. **Wealth by kind follows deposit count.** `Value` gives every kind the
   same average weight per deposit, so no kind is richer than another for
   the same number of deposits. Asteroid belts are the richest planets
   (median 958M, against 720M for rocky planets) only because they have more
   deposits.
