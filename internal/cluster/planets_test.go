// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package cluster

import (
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/playbymail/seiicho/internal/prng"
)

func TestPlanets(t *testing.T) {
	root := prng.New(prng.SplitSeed(0x0123456789abcdef))
	src, kinds, hab := root.Child(prng.Planets), root.Child(prng.PlanetKinds), root.Child(prng.Habitability)
	stars := Generate(rand.NewPCG(1, 2))
	planets := Planets(stars, src.PRNG(), kinds.PRNG(), hab.PRNG())

	if want := len(stars) + ExtraPlanets; len(planets) != want {
		t.Fatalf("%d planets, want %d", len(planets), want)
	}
	// Every star gets orbit 1 first, in (x, y, z, seq) order. Each star's
	// orbits then run from 1 with no gaps.
	counts := make([]int, len(stars))
	for i, p := range planets {
		if i > 0 && i < len(stars) && Compare(stars[planets[i-1].Star], stars[p.Star]) >= 0 {
			t.Fatalf("planet %d of star %+v follows star %+v", i, stars[p.Star], stars[planets[i-1].Star])
		}
		counts[p.Star]++
		if p.Orbit != counts[p.Star] {
			t.Errorf("planet %d of star %+v is in orbit %d, want %d", i, stars[p.Star], p.Orbit, counts[p.Star])
		}
		if !slices.Contains(kindOrder[:], p.Kind) {
			t.Errorf("planet %d is %q, want one of %q", i, p.Kind, kindOrder)
		}
		if hi := maxHabitability[p.Kind]; p.Habitability < 0 || p.Habitability > hi {
			t.Errorf("planet %d is %s with habitability %d, want 0 to %d", i, p.Kind, p.Habitability, hi)
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
	if a, b := describe(stars, planets), describe(shuffled, Planets(shuffled, src.PRNG(), kinds.PRNG(), hab.PRNG())); a != b {
		t.Error("shuffling the stars changed the planets")
	}

	// Pin the allocation, kinds, and habitability, so that a change to the
	// allocator, the kind weights, the habitability draws, or math/rand/v2
	// cannot silently change the planets an existing seed makes.
	h := fnv.New64a()
	fmt.Fprint(h, describe(stars, planets))
	if got := h.Sum64(); got != 0xf82687e5ea24e11e {
		t.Errorf("planets hash = %#x, want 0xf82687e5ea24e11e", got)
	}

	// Sorted, planets come in (x, y, z, seq) order of their stars, then
	// orbit order.
	sorted := slices.Clone(planets)
	SortPlanets(stars, sorted)
	for i := 1; i < len(sorted); i++ {
		a, b := sorted[i-1], sorted[i]
		if c := Compare(stars[a.Star], stars[b.Star]); c > 0 || c == 0 && a.Orbit >= b.Orbit {
			t.Fatalf("sorted planet %d, %+v, follows %+v", i, b, a)
		}
	}
	h.Reset()
	fmt.Fprint(h, describe(stars, sorted))
	if got := h.Sum64(); got != 0x425671f763e01ec {
		t.Errorf("sorted planets hash = %#x, want 0x425671f763e01ec", got)
	}
}

func TestSortStars(t *testing.T) {
	stars := Generate(rand.NewPCG(1, 2))
	SortStars(stars)
	for i := 1; i < len(stars); i++ {
		if Compare(stars[i-1], stars[i]) >= 0 {
			t.Fatalf("star %d, %+v, follows %+v", i, stars[i], stars[i-1])
		}
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
	planets := Planets(stars, prng.New(1, 2).PRNG(), prng.New(3, 4).PRNG(), prng.New(5, 6).PRNG())
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
	Planets(stars[:62], prng.New(1, 2).PRNG(), prng.New(3, 4).PRNG(), prng.New(5, 6).PRNG())
}

func TestKindWeights(t *testing.T) {
	// The approved matrix, from planetary-resources.md: one row per number of planets
	// a star has, one cell per orbit.
	want := `
20,6,14
26,6,8 18,6,16
29,5,6 21,6,13 17,7,16
31,5,4 24,6,10 18,7,15 15,7,18
32,4,4 27,5,8 21,7,12 16,7,17 12,8,20
32,4,4 27,5,8 23,7,10 18,7,15 12,8,20 10,8,22
32,4,4 29,5,6 23,7,10 19,7,14 13,8,19 10,8,22 9,8,23
32,4,4 29,5,6 26,6,8 21,7,12 15,8,17 11,8,21 9,8,23 7,8,25
32,4,4 29,5,6 26,6,8 21,7,12 16,8,16 13,8,19 10,8,22 7,8,25 6,8,26
34,4,2 30,4,6 28,6,6 24,6,10 18,8,14 14,8,18 12,8,20 8,8,24 6,8,26 6,8,26
`
	got := "\n"
	for n := 1; n <= MaxOrbits; n++ {
		var row []string
		for orbit := 1; orbit <= n; orbit++ {
			w := KindWeights(n, orbit)
			if sum := w[0] + w[1] + w[2]; sum != KindTotal {
				t.Errorf("KindWeights(%d, %d) = %v, which sums to %d, want %d", n, orbit, w, sum, KindTotal)
			}
			row = append(row, fmt.Sprintf("%d,%d,%d", w[0], w[1], w[2]))
		}
		got += strings.Join(row, " ") + "\n"
	}
	if got != want {
		t.Errorf("KindWeights matrix:%s\nwant:%s", got, want)
	}

	for _, args := range [][2]int{{1, 0}, {3, 4}, {11, 1}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("KindWeights(%d, %d) did not panic", args[0], args[1])
				}
			}()
			KindWeights(args[0], args[1])
		}()
	}
}

func TestDrawKind(t *testing.T) {
	// A lone planet weighs 20,6,14: drawn often enough, each kind comes up
	// close to 20/40, 6/40, and 14/40 of the time.
	const draws = 40000
	p := prng.New(5, 6).PRNG()
	counts := map[string]int{}
	for range draws {
		counts[drawKind(1, 1, p)]++
	}
	for kind, want := range map[string]int{Rocky: 20000, Asteroids: 6000, Giant: 14000} {
		if got := counts[kind]; got < want*95/100 || got > want*105/100 {
			t.Errorf("%d of %d draws were %s, want about %d", got, draws, kind, want)
		}
	}
	if len(counts) != 3 {
		t.Errorf("draws gave kinds %v", counts)
	}
}

// maxHabitability is the highest habitability each kind can draw.
var maxHabitability = map[string]int{Rocky: 24, Asteroids: 0, Giant: 15}

func TestDrawHabitability(t *testing.T) {
	// The average of two draws is most likely near the middle of the range
	// and least likely at its ends: for Rocky, 12 comes from 49 of the 625
	// pairs and 0 from 2.
	const draws = 62500
	p := prng.New(7, 8).PRNG()
	for kind, middle := range map[string]int{Rocky: 12, Giant: 7} {
		counts := make([]int, maxHabitability[kind]+1)
		for range draws {
			h := drawHabitability(kind, p)
			if h < 0 || h >= len(counts) {
				t.Fatalf("%s drew habitability %d, want 0 to %d", kind, h, len(counts)-1)
			}
			counts[h]++
		}
		for h, n := range counts {
			if n == 0 {
				t.Errorf("%s never drew habitability %d in %d draws", kind, h, draws)
			}
			if h != middle && n >= counts[middle] {
				t.Errorf("%s drew %d %d times and %d %d times, want %d most often", kind, h, n, middle, counts[middle], middle)
			}
		}
	}
	if got := drawHabitability(Asteroids, p); got != 0 {
		t.Errorf("asteroids drew habitability %d, want 0", got)
	}
}

// describe lists planets by their stars' coordinates rather than indexes.
func describe(stars []Star, planets []Planet) string {
	var s []string
	for _, p := range planets {
		st := stars[p.Star]
		s = append(s, fmt.Sprint(st.X, st.Y, st.Z, st.Seq, p.Orbit, p.Kind, p.Habitability))
	}
	return fmt.Sprint(s)
}
