# Proposal 1: Planet kinds and habitability

Status:

- **Step 1, planet kinds: approved and implemented** in `planets.go`
  (`KindWeights`, drawn from the `prng.PlanetKinds` source).
- **Step 2, habitability: draft, not approved.** No code until the design is
  approved.

We are generating a game world, not simulating planetary formation. The
result should be plausible, deterministic, and easy to explain.

Generation has two separate steps:

1. **Planet kind.** Weighted random selection, influenced by the planet's
   orbit and its star's number of planets.
2. **Habitability.** A value within the range allowed for that kind, also
   influenced by orbit.

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

Not approved. Once a planet's kind is known, its habitability comes from a
separate probability distribution influenced by orbit.

Hard limits:

| Kind      | Habitability |
| --------- | ------------ |
| Rocky     | 0–25         |
| Asteroids | 0            |
| Giant     | 0–15         |

Orbit should bias the probabilities, not restrict them. For example, a rocky
planet in orbit 1 should be less likely to get habitability 20 than one in
orbit 4, but both must be possible.

The habitability curves are designed independently of the kind weights. They
are still to be decided.
