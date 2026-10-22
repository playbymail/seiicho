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

// Planets allocates planets to stars, drawing from p. It sorts the stars by
// (x, y, z, seq), gives each one planet, and then makes ExtraPlanets random
// allocations with replacement, each choosing a star with Roll(1, 0, n-1)
// from the n stars that have fewer than MaxOrbits planets. A star's planets
// fill its orbits from 1 with no gaps. Every planet is Rocky.
//
// It returns the planets ordered by star, in sorted order, then orbit. The
// result depends on the stars but not on their order. It panics if stars
// cannot hold ExtraPlanets more planets.
func Planets(stars []Star, p *prng.PRNG) []Planet {
	order := make([]int, len(stars))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(i, j int) int {
		a, b := stars[i], stars[j]
		return cmp.Or(cmp.Compare(a.X, b.X), cmp.Compare(a.Y, b.Y), cmp.Compare(a.Z, b.Z), cmp.Compare(a.Seq, b.Seq))
	})

	// counts and eligible index into order. A star leaves eligible by
	// swapping with the last eligible star.
	counts := make([]int, len(order))
	eligible := make([]int, len(order))
	for i := range counts {
		counts[i] = 1
		eligible[i] = i
	}
	for range ExtraPlanets {
		if len(eligible) == 0 {
			panic("cluster: too many planets for the stars")
		}
		k := p.Roll(1, 0, len(eligible)-1)
		i := eligible[k]
		counts[i]++
		if counts[i] == MaxOrbits {
			last := len(eligible) - 1
			eligible[k], eligible[last] = eligible[last], eligible[k]
			eligible = eligible[:last]
		}
	}

	var planets []Planet
	for i, star := range order {
		for orbit := 1; orbit <= counts[i]; orbit++ {
			planets = append(planets, Planet{Star: star, Orbit: orbit, Kind: Rocky})
		}
	}
	return planets
}
