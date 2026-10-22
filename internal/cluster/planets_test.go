// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package cluster

import (
	"cmp"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/playbymail/seiicho/internal/prng"
)

func TestPlanets(t *testing.T) {
	src := prng.New(prng.SplitSeed(0x0123456789abcdef)).Child(prng.Planets)
	stars := Generate(rand.NewPCG(1, 2))
	planets := Planets(stars, src.PRNG())

	if want := len(stars) + ExtraPlanets; len(planets) != want {
		t.Fatalf("%d planets, want %d", len(planets), want)
	}
	// Planets come in (x, y, z, seq) order of their stars, and each star's
	// orbits run from 1 with no gaps.
	compare := func(a, b Star) int {
		return cmp.Or(cmp.Compare(a.X, b.X), cmp.Compare(a.Y, b.Y), cmp.Compare(a.Z, b.Z), cmp.Compare(a.Seq, b.Seq))
	}
	counts := make([]int, len(stars))
	for i, p := range planets {
		if i > 0 && p.Star != planets[i-1].Star && compare(stars[planets[i-1].Star], stars[p.Star]) >= 0 {
			t.Fatalf("planet %d of star %+v follows star %+v", i, stars[p.Star], stars[planets[i-1].Star])
		}
		counts[p.Star]++
		if p.Orbit != counts[p.Star] {
			t.Errorf("planet %d of star %+v is in orbit %d, want %d", i, stars[p.Star], p.Orbit, counts[p.Star])
		}
		if p.Kind != Rocky {
			t.Errorf("planet %d is %q, want %q", i, p.Kind, Rocky)
		}
	}
	for i, n := range counts {
		if n < 1 || n > MaxOrbits {
			t.Errorf("star %+v has %d planets, want 1 to %d", stars[i], n, MaxOrbits)
		}
	}

	// The result depends on the stars, not their order.
	shuffled := slices.Clone(stars)
	rand.New(rand.NewPCG(3, 4)).Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	if a, b := describe(stars, planets), describe(shuffled, Planets(shuffled, src.PRNG())); a != b {
		t.Error("shuffling the stars changed the planets")
	}

	// Pin the allocation, so that a change to the allocator or to
	// math/rand/v2 cannot silently change the planets an existing seed makes.
	h := fnv.New64a()
	fmt.Fprint(h, describe(stars, planets))
	if got := h.Sum64(); got != 0x32c4623b6cd84606 {
		t.Errorf("planets hash = %#x, want 0x32c4623b6cd84606", got)
	}
}

func TestPlanetsFillsStars(t *testing.T) {
	// 63 stars hold at most 630 planets: 63 to start and 562 more leaves
	// only 5 orbits empty, so most stars must reach MaxOrbits and leave
	// the pool.
	stars := make([]Star, 63)
	for i := range stars {
		stars[i] = Star{X: i % Side, Y: i / Side, Seq: "A"}
	}
	planets := Planets(stars, prng.New(1, 2).PRNG())
	counts := make([]int, len(stars))
	for _, p := range planets {
		counts[p.Star]++
	}
	full := 0
	for _, n := range counts {
		if n > MaxOrbits {
			t.Fatalf("a star has %d planets", n)
		}
		if n == MaxOrbits {
			full++
		}
	}
	if full < 58 {
		t.Errorf("%d full stars, want at least 58", full)
	}

	defer func() {
		if recover() == nil {
			t.Error("Planets with too few stars did not panic")
		}
	}()
	Planets(stars[:62], prng.New(1, 2).PRNG())
}

// describe lists planets by their stars' coordinates rather than indexes.
func describe(stars []Star, planets []Planet) string {
	var s []string
	for _, p := range planets {
		st := stars[p.Star]
		s = append(s, fmt.Sprint(st.X, st.Y, st.Z, st.Seq, p.Orbit, p.Kind))
	}
	return fmt.Sprint(s)
}
