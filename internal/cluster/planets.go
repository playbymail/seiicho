// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package cluster

import (
	"cmp"
	"slices"

	"github.com/playbymail/seiicho/internal/prng"
)

const (
	// MaxOrbits is the most planets a star can have; orbits run from 1 to
	// MaxOrbits.
	MaxOrbits = 10
	// ExtraPlanets is the number of planets allocated after every star has
	// one.
	ExtraPlanets = 562
)

// Planet kinds.
const (
	Rocky     = "rocky"
	Asteroids = "asteroids"
	Giant     = "giant"
)

// Planet is a planet in Orbit around stars[Star], for the stars passed to
// Planets.
type Planet struct {
	Star  int
	Orbit int
	Kind  string
}

// Compare orders stars by (x, y, z, seq).
func Compare(a, b Star) int {
	return cmp.Or(cmp.Compare(a.X, b.X), cmp.Compare(a.Y, b.Y), cmp.Compare(a.Z, b.Z), cmp.Compare(a.Seq, b.Seq))
}

// Planets allocates planets to stars, drawing from alloc, and then gives
// each planet a kind, drawing from kinds. It sorts the stars by
// (x, y, z, seq), gives each one planet, and then makes ExtraPlanets random
// allocations with replacement, each choosing a star with Roll(1, 0, n-1)
// from the n stars that have fewer than MaxOrbits planets. A star's planets
// fill its orbits from 1 with no gaps.
//
// Once every star has all its planets, it visits the stars in
// (x, y, z, seq) order and their planets in orbit order, and draws each
// planet's kind with Roll(1, 1, KindTotal) against the cumulative
// KindWeights for its orbit and its star's number of planets.
//
// It returns the planets in the order they were allocated, so a planet's
// place does not reveal how many planets its star has; SortPlanets puts
// them in star order. The result depends on the stars but not on their
// order. It panics if stars cannot hold ExtraPlanets more planets.
func Planets(stars []Star, alloc, kinds *prng.PRNG) []Planet {
	order := make([]int, len(stars))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(i, j int) int { return Compare(stars[i], stars[j]) })

	// counts and eligible index into order. A star leaves eligible by
	// swapping with the last eligible star. orbits[i][k-1] is the index in
	// planets of order[i]'s planet in orbit k.
	planets := make([]Planet, 0, len(stars)+ExtraPlanets)
	counts := make([]int, len(order))
	eligible := make([]int, len(order))
	orbits := make([][]int, len(order))
	for i, star := range order {
		counts[i] = 1
		eligible[i] = i
		orbits[i] = append(orbits[i], len(planets))
		planets = append(planets, Planet{Star: star, Orbit: 1})
	}
	for range ExtraPlanets {
		if len(eligible) == 0 {
			panic("cluster: too many planets for the stars")
		}
		k := alloc.Roll(1, 0, len(eligible)-1)
		i := eligible[k]
		counts[i]++
		orbits[i] = append(orbits[i], len(planets))
		planets = append(planets, Planet{Star: order[i], Orbit: counts[i]})
		if counts[i] == MaxOrbits {
			last := len(eligible) - 1
			eligible[k], eligible[last] = eligible[last], eligible[k]
			eligible = eligible[:last]
		}
	}
	for i := range order {
		for _, j := range orbits[i] {
			planets[j].Kind = drawKind(counts[i], planets[j].Orbit, kinds)
		}
	}
	return planets
}

// KindTotal is the sum of the weights KindWeights returns.
const KindTotal = 40

// orbitKindWeights[k-1] holds the Rocky, Asteroids, and Giant weights for
// orbit k; each row sums to KindTotal / 2.
var orbitKindWeights = [MaxOrbits][3]int{
	{17, 2, 1},
	{15, 2, 3},
	{14, 3, 3},
	{12, 3, 5},
	{9, 4, 7},
	{7, 4, 9},
	{6, 4, 10},
	{4, 4, 12},
	{3, 4, 13},
	{3, 4, 13},
}

// kindOrder lists the planet kinds in the order of their weights.
var kindOrder = [3]string{Rocky, Asteroids, Giant}

// KindWeights returns the Rocky, Asteroids, and Giant weights for the planet
// in orbit of a star with n planets; they sum to KindTotal. Half the weight
// comes from the orbit itself and half from the orbit scaled to a star with
// MaxOrbits planets, ceil(MaxOrbits × orbit / n), so a star's outer planets
// lean towards giants however many planets it has. It panics unless
// 1 <= orbit <= n <= MaxOrbits.
func KindWeights(n, orbit int) [3]int {
	if orbit < 1 || orbit > n || n > MaxOrbits {
		panic("cluster: invalid orbit")
	}
	scaled := (MaxOrbits*orbit + n - 1) / n
	a, b := orbitKindWeights[orbit-1], orbitKindWeights[scaled-1]
	return [3]int{a[0] + b[0], a[1] + b[1], a[2] + b[2]}
}

// drawKind draws the kind of the planet in orbit of a star with n planets.
func drawKind(n, orbit int, p *prng.PRNG) string {
	roll := p.Roll(1, 1, KindTotal)
	for i, w := range KindWeights(n, orbit) {
		if roll <= w {
			return kindOrder[i]
		}
		roll -= w
	}
	panic("cluster: kind weights do not sum to KindTotal")
}

// SortStars sorts stars by (x, y, z, seq).
func SortStars(stars []Star) {
	slices.SortFunc(stars, Compare)
}

// SortPlanets sorts planets by their stars, in (x, y, z, seq) order, then by
// orbit. stars are the stars the planets were allocated to.
func SortPlanets(stars []Star, planets []Planet) {
	slices.SortFunc(planets, func(a, b Planet) int {
		return cmp.Or(Compare(stars[a.Star], stars[b.Star]), cmp.Compare(a.Orbit, b.Orbit))
	})
}
