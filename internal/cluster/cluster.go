// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package cluster generates a game's board: a cluster of star systems in a
// cube of integer coordinates.
package cluster

import "math/rand/v2"

const (
	// Side is the length of the cube's side; coordinates run from 0 to Side-1.
	Side = 31
	// Systems is the number of star systems in a cluster.
	Systems = 100
)

// starCounts gives the number of stars in each system, in draw order: the
// first system drawn has 5 stars, the next 2 have 4, the next 7 have 3, the
// next 35 have 2, and the remaining 55 have 1.
var starCounts = []struct{ systems, stars int }{
	{1, 5}, {2, 4}, {7, 3}, {35, 2}, {55, 1},
}

// Seqs letters the stars in a system, in order.
const Seqs = "ABCDE"

// Star is one star in a system at (X, Y, Z). Seq letters it within the
// system, from "A".
type Star struct {
	X, Y, Z int
	Seq     string
}

// Generate draws a cluster from pcg. It shuffles every coordinate in the
// cube, listed in order of x, then y, then z, and takes the first Systems
// coordinates as star systems. It returns their stars, systems in draw order
// and stars in Seq order. The same pcg state always gives the same cluster.
func Generate(pcg *rand.PCG) []Star {
	type coord struct{ x, y, z int }
	coords := make([]coord, 0, Side*Side*Side)
	for x := range Side {
		for y := range Side {
			for z := range Side {
				coords = append(coords, coord{x, y, z})
			}
		}
	}
	rand.New(pcg).Shuffle(len(coords), func(i, j int) {
		coords[i], coords[j] = coords[j], coords[i]
	})

	var stars []Star
	drawn := coords[:Systems]
	for _, sc := range starCounts {
		for _, c := range drawn[:sc.systems] {
			for i := range sc.stars {
				stars = append(stars, Star{X: c.x, Y: c.y, Z: c.z, Seq: Seqs[i : i+1]})
			}
		}
		drawn = drawn[sc.systems:]
	}
	return stars
}
