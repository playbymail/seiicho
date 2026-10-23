// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package cluster

import (
	"cmp"
	"slices"

	"github.com/playbymail/seiicho/internal/cerrs"
	"github.com/playbymail/seiicho/internal/prng"
)

// The ranges of HomeWorld's parameters, inclusive.
const (
	MinHomeDistanceLo     = 3
	MinHomeDistanceHi     = 16
	MaxNeighborDistanceLo = 1
	MaxNeighborDistanceHi = 16
)

// MaxHomeYield is the highest yield a home world's deposits are raised to,
// as a percentage.
const MaxHomeYield = 75

// ErrNoHomeWorld means that no planet qualifies as the next home world.
const ErrNoHomeWorld = cerrs.Error("no planet qualifies as a home world")

// Distance returns the distance between the systems of stars a and b: the
// Euclidean distance rounded up, computed with integers. Stars in the same
// system are 0 apart.
func Distance(a, b Star) int {
	dx, dy, dz := a.X-b.X, a.Y-b.Y, a.Z-b.Z
	d2 := dx*dx + dy*dy + dz*dz
	r := 0
	for (r+1)*(r+1) <= d2 {
		r++
	}
	if r*r == d2 {
		return r
	}
	return r + 1
}

// sameSystem reports whether stars a and b are in the same system.
func sameSystem(a, b Star) bool {
	return a.X == b.X && a.Y == b.Y && a.Z == b.Z
}

// soleStars reports, for each of stars, whether it is the only star in its
// system: a potential home star. There is no minimum number of planets.
func soleStars(stars []Star) []bool {
	type coord struct{ x, y, z int }
	counts := map[coord]int{}
	for _, s := range stars {
		counts[coord{s.X, s.Y, s.Z}]++
	}
	sole := make([]bool, len(stars))
	for i, s := range stars {
		sole[i] = counts[coord{s.X, s.Y, s.Z}] == 1
	}
	return sole
}

// isHomeOrbit reports whether p could be a potential home planet, if its star
// is a potential home star: it is rocky and in orbit 3, 4, or 5.
func isHomeOrbit(p Planet) bool {
	return p.Kind == Rocky && p.Orbit >= 3 && p.Orbit <= 5
}

// compareCandidates orders a star's potential home planets, best first: the
// lowest habitability, then the lower orbit.
func compareCandidates(a, b Planet) int {
	return cmp.Or(cmp.Compare(a.Habitability, b.Habitability), cmp.Compare(a.Orbit, b.Orbit))
}

// candidates returns, for each of stars, the index in planets of its
// candidate planet, or -1 if it has none.
func candidates(stars []Star, planets []Planet) []int {
	sole := soleStars(stars)
	best := make([]int, len(stars))
	for i := range best {
		best[i] = -1
	}
	for j, p := range planets {
		if !sole[p.Star] || !isHomeOrbit(p) {
			continue
		}
		if k := best[p.Star]; k < 0 || compareCandidates(p, planets[k]) < 0 {
			best[p.Star] = j
		}
	}
	return best
}

// HomeWorld returns the index in planets of the next home world, or
// ErrNoHomeWorld if none qualifies. homes holds the indexes of planets
// already flagged. It follows home-worlds.md:
//
//  1. The pool is every potential home star with a candidate planet whose
//     system is at least minHome from every home system.
//  2. It sorts the pool by Compare and shuffles it with p.
//  3. It takes stars from the front of the pool until one has a non-home
//     system other than its own within maxNeighbor, and returns that star's
//     candidate planet.
//
// The result depends on the stars, planets, and homes, not on their order.
// It panics if minHome or maxNeighbor is outside its range, or if a home is
// not an index in planets.
func HomeWorld(stars []Star, planets []Planet, homes []int, minHome, maxNeighbor int, p *prng.PRNG) (int, error) {
	if minHome < MinHomeDistanceLo || minHome > MinHomeDistanceHi {
		panic("cluster: min home distance out of range")
	}
	if maxNeighbor < MaxNeighborDistanceLo || maxNeighbor > MaxNeighborDistanceHi {
		panic("cluster: max neighbor distance out of range")
	}
	homeStars := make([]Star, 0, len(homes))
	for _, h := range homes {
		if h < 0 || h >= len(planets) {
			panic("cluster: home is not a planet")
		}
		homeStars = append(homeStars, stars[planets[h].Star])
	}
	isHome := func(s Star) bool {
		return slices.ContainsFunc(homeStars, func(h Star) bool { return sameSystem(s, h) })
	}

	// Step 1: build the pool, applying the minimum home distance only.
	best := candidates(stars, planets)
	var pool []int
	for i, s := range stars {
		if best[i] >= 0 && !slices.ContainsFunc(homeStars, func(h Star) bool { return Distance(s, h) < minHome }) {
			pool = append(pool, i)
		}
	}

	// Step 2: order the pool.
	slices.SortFunc(pool, func(i, j int) int { return Compare(stars[i], stars[j]) })
	p.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })

	// Step 3: draw. The nearest non-home system is within maxNeighbor
	// exactly when one of its stars is.
	for _, i := range pool {
		for _, s := range stars {
			if !sameSystem(s, stars[i]) && !isHome(s) && Distance(s, stars[i]) <= maxNeighbor {
				return best[i], nil
			}
		}
	}
	return -1, ErrNoHomeWorld
}

// RaiseHomeDeposits makes a new home world's deposits richer, in deposit
// number order, drawing one roll per deposit from p: each yield rises by
// Roll(1, 1, 10) points, to at most MaxHomeYield, and each quantity by 25%,
// rounded down. Nothing else changes. It panics unless the deposits are all
// on one planet.
func RaiseHomeDeposits(deposits []Deposit, p *prng.PRNG) {
	order := make([]int, len(deposits))
	for k := range order {
		order[k] = k
		if deposits[k].Planet != deposits[0].Planet {
			panic("cluster: deposits on more than one planet")
		}
	}
	slices.SortFunc(order, func(a, b int) int { return cmp.Compare(deposits[a].No, deposits[b].No) })
	for _, k := range order {
		d := &deposits[k]
		d.Yield = min(MaxHomeYield, d.Yield+p.Roll(1, 1, 10))
		d.Quantity = d.Quantity * 125 / 100
	}
}
