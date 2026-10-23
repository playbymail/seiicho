// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package cluster

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/playbymail/seiicho/internal/prng"
)

func TestDistance(t *testing.T) {
	for _, tc := range []struct {
		a, b Star
		want int
	}{
		{Star{0, 0, 0, "A"}, Star{0, 0, 0, "B"}, 0},
		{Star{0, 0, 0, "A"}, Star{3, 4, 0, "A"}, 5},
		{Star{0, 0, 0, "A"}, Star{1, 1, 1, "A"}, 2},
		{Star{0, 0, 0, "A"}, Star{1, 0, 0, "A"}, 1},
		{Star{0, 0, 0, "A"}, Star{1, 1, 0, "A"}, 2},
		// 4² = 16 and 17 lie on either side of the boundary.
		{Star{0, 0, 0, "A"}, Star{0, 0, 4, "A"}, 4},
		{Star{0, 0, 0, "A"}, Star{4, 1, 0, "A"}, 5},
		// 24 and 25: √24 rounds up to 5, and √25 is exactly 5.
		{Star{0, 0, 0, "A"}, Star{4, 2, 2, "A"}, 5},
		{Star{5, 5, 5, "A"}, Star{5, 1, 2, "A"}, 5},
		{Star{30, 30, 30, "A"}, Star{0, 0, 0, "A"}, 52},
	} {
		if got := Distance(tc.a, tc.b); got != tc.want {
			t.Errorf("Distance(%v, %v) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
		if got := Distance(tc.b, tc.a); got != tc.want {
			t.Errorf("Distance(%v, %v) = %d, want %d", tc.b, tc.a, got, tc.want)
		}
	}
}

// homeCluster returns a hand-built cluster whose only possible home worlds
// are, by star, the planets that want lists; every star but the last two
// is 1 apart from its neighbor on the x axis.
func homeCluster() (stars []Star, planets []Planet, want map[int]int) {
	add := func(s Star, kinds ...string) int {
		stars = append(stars, s)
		for i, k := range kinds {
			planets = append(planets, Planet{Star: len(stars) - 1, Orbit: i + 1, Kind: k, Habitability: 10})
		}
		return len(stars) - 1
	}
	want = map[int]int{}
	// A two-star system with rocky planets in orbits 3 to 5 is never chosen.
	add(Star{0, 0, 0, "A"}, Rocky, Rocky, Rocky, Rocky, Rocky)
	add(Star{0, 0, 0, "B"}, Rocky, Rocky, Rocky, Rocky, Rocky)
	// Neither are sole stars without a rocky planet in orbits 3 to 5.
	add(Star{1, 0, 0, "A"}, Rocky, Rocky)
	add(Star{2, 0, 0, "A"}, Rocky, Rocky, Giant, Asteroids, Giant, Rocky)
	// A star with 3 planets can be, and so can one with 4.
	add(Star{3, 0, 0, "A"}, Giant, Giant, Rocky)
	want[4] = len(planets) - 1
	add(Star{4, 0, 0, "A"}, Giant, Giant, Giant, Rocky)
	want[5] = len(planets) - 1
	// The lowest habitability wins.
	s := add(Star{5, 0, 0, "A"}, Rocky, Rocky, Rocky, Rocky, Rocky, Rocky)
	planets[len(planets)-4].Habitability = 3
	planets[len(planets)-2].Habitability = 2
	want[s] = len(planets) - 2
	// The lower orbit breaks a tie.
	s = add(Star{6, 0, 0, "A"}, Rocky, Rocky, Giant, Rocky, Rocky)
	planets[len(planets)-2].Habitability = 0
	planets[len(planets)-1].Habitability = 0
	want[s] = len(planets) - 2
	// The candidate is chosen whatever its habitability elsewhere: orbits
	// 1, 2, and 6 do not count.
	s = add(Star{7, 0, 0, "A"}, Rocky, Rocky, Rocky, Giant, Giant, Rocky)
	planets[len(planets)-6].Habitability = 0
	planets[len(planets)-1].Habitability = 0
	want[s] = len(planets) - 4
	return stars, planets, want
}

func TestHomeWorldEligibility(t *testing.T) {
	stars, planets, want := homeCluster()
	if got := candidates(stars, planets); !slices.Equal(got, []int{-1, -1, -1, -1, want[4], want[5], want[6], want[7], want[8]}) {
		t.Errorf("candidates = %v, want %v", got, want)
	}
	// With every star a neighbor of the next, any candidate can be drawn.
	seen := map[int]bool{}
	for seed := range uint64(50) {
		j, err := HomeWorld(stars, planets, nil, 3, 1, prng.New(seed, 0).PRNG())
		if err != nil {
			t.Fatal(err)
		}
		if want[planets[j].Star] != j {
			t.Fatalf("seed %d chose planet %d, %+v, which is not a candidate", seed, j, planets[j])
		}
		seen[j] = true
	}
	if len(seen) != len(want) {
		t.Errorf("50 seeds chose %d of the %d candidates", len(seen), len(want))
	}
}

func TestHomeWorldSkipsStrandedStars(t *testing.T) {
	// The star at the origin is stranded: its nearest system is 10 away.
	// The other is 1 from its neighbor, a two-star system.
	stars := []Star{{0, 0, 0, "A"}, {20, 0, 0, "A"}, {10, 0, 0, "A"}, {21, 0, 0, "A"}, {21, 0, 0, "B"}}
	var planets []Planet
	for _, s := range []int{0, 1} {
		for orbit := 1; orbit <= 3; orbit++ {
			planets = append(planets, Planet{Star: s, Orbit: orbit, Kind: Rocky})
		}
	}
	skipped := 0
	for seed := range uint64(20) {
		src := prng.New(seed, 0)
		j, err := HomeWorld(stars, planets, nil, 3, 9, src.PRNG())
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if planets[j].Star != 1 {
			t.Errorf("seed %d chose a planet of star %+v, want the star with a neighbor", seed, stars[planets[j].Star])
		}
		// The pool is sorted before the shuffle: [0, 1].
		pool := []int{0, 1}
		src.PRNG().Shuffle(2, func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
		if pool[0] == 0 {
			skipped++
		}
	}
	if skipped == 0 {
		t.Error("no seed put the stranded star first")
	}

	// With the neighbor 10 away allowed, either can be chosen.
	if _, err := HomeWorld(stars, planets, nil, 3, 10, prng.New(1, 0).PRNG()); err != nil {
		t.Error(err)
	}
	// With both stranded, the pool is exhausted.
	if _, err := HomeWorld(stars[:3], planets, nil, 3, 9, prng.New(1, 0).PRNG()); !errors.Is(err, ErrNoHomeWorld) {
		t.Errorf("HomeWorld with every star stranded = %v, want ErrNoHomeWorld", err)
	}

	// A home system is not a neighbor. Stars 0 and 1 are each other's only
	// neighbor within 3; once star 1 is a home, star 0 has none.
	stars = []Star{{0, 0, 0, "A"}, {3, 0, 0, "A"}, {10, 0, 0, "A"}}
	if _, err := HomeWorld(stars, planets, nil, 3, 3, prng.New(1, 0).PRNG()); err != nil {
		t.Error(err)
	}
	if _, err := HomeWorld(stars, planets, []int{5}, 3, 3, prng.New(1, 0).PRNG()); !errors.Is(err, ErrNoHomeWorld) {
		t.Errorf("HomeWorld with the only neighbor a home = %v, want ErrNoHomeWorld", err)
	}
}

func TestHomeWorldEmptyPool(t *testing.T) {
	stars, planets, _ := homeCluster()
	for _, tc := range []struct {
		name    string
		stars   []Star
		planets []Planet
		homes   []int
		minHome int
	}{
		{"no stars", nil, nil, nil, 3},
		{"no candidates", stars[:4], planets[:18], nil, 3},
		// A home at the end of the line rules out every star within 16.
		{"every star near a home", stars, planets, []int{len(planets) - 1}, 16},
	} {
		if j, err := HomeWorld(tc.stars, tc.planets, tc.homes, tc.minHome, 16, prng.New(1, 2).PRNG()); !errors.Is(err, ErrNoHomeWorld) {
			t.Errorf("%s: HomeWorld = %d, %v; want ErrNoHomeWorld", tc.name, j, err)
		}
	}
}

func TestHomeWorldPanics(t *testing.T) {
	stars, planets, _ := homeCluster()
	for _, tc := range []struct {
		homes                []int
		minHome, maxNeighbor int
	}{
		{nil, 2, 1}, {nil, 17, 1}, {nil, 3, 0}, {nil, 3, 17},
		{[]int{-1}, 3, 1}, {[]int{len(planets)}, 3, 1},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("HomeWorld(%v, %d, %d) did not panic", tc.homes, tc.minHome, tc.maxNeighbor)
				}
			}()
			HomeWorld(stars, planets, tc.homes, tc.minHome, tc.maxNeighbor, prng.New(1, 2).PRNG())
		}()
	}
}

// seededCluster returns the stars and planets of the game with seed.
func seededCluster(seed uint64) ([]Star, []Planet, prng.Source) {
	root := prng.New(prng.SplitSeed(seed))
	stars := Generate(root.Child(prng.Cluster).PCG())
	planets := Planets(stars, root.Child(prng.Planets).PRNG(), root.Child(prng.PlanetKinds).PRNG(), root.Child(prng.Habitability).PRNG())
	return stars, planets, root
}

// flagAll flags home worlds with the same parameters, from a fresh home-world
// source each run as the store does, until a run fails. It returns the home
// worlds in the order flagged.
func flagAll(t *testing.T, stars []Star, planets []Planet, root prng.Source, minHome, maxNeighbor int) []int {
	t.Helper()
	var homes []int
	for {
		j, err := HomeWorld(stars, planets, homes, minHome, maxNeighbor, root.Child(prng.HomeWorlds).PRNG())
		if errors.Is(err, ErrNoHomeWorld) {
			return homes
		} else if err != nil {
			t.Fatal(err)
		}
		if slices.Contains(homes, j) {
			t.Fatalf("planet %d flagged twice", j)
		}
		// Every new home system has a non-home system within maxNeighbor.
		near := false
		for _, s := range stars {
			home := slices.ContainsFunc(homes, func(h int) bool { return sameSystem(s, stars[planets[h].Star]) })
			if !home && !sameSystem(s, stars[planets[j].Star]) && Distance(s, stars[planets[j].Star]) <= maxNeighbor {
				near = true
			}
		}
		if !near {
			t.Errorf("home world %+v has no non-home system within %d", stars[planets[j].Star], maxNeighbor)
		}
		planets[j].Habitability = 25
		homes = append(homes, j)
	}
}

func TestHomeWorldDistances(t *testing.T) {
	stars, generated, root := seededCluster(0x0123456789abcdef)
	for _, tc := range []struct{ minHome, maxNeighbor int }{{3, 3}, {3, 16}, {6, 6}, {10, 4}, {16, 16}} {
		planets := slices.Clone(generated)
		homes := flagAll(t, stars, planets, root, tc.minHome, tc.maxNeighbor)
		if len(homes) == 0 {
			t.Errorf("%+v flagged no home worlds", tc)
		}
		for a, i := range homes {
			for _, j := range homes[:a] {
				if d := Distance(stars[planets[i].Star], stars[planets[j].Star]); d < tc.minHome {
					t.Errorf("%+v: home worlds %+v and %+v are %d apart", tc, stars[planets[i].Star], stars[planets[j].Star], d)
				}
			}
		}
		// The same parameters and run order give the same home worlds.
		if again := flagAll(t, stars, slices.Clone(generated), root, tc.minHome, tc.maxNeighbor); !slices.Equal(again, homes) {
			t.Errorf("%+v: flagged %v, then %v", tc, homes, again)
		}
	}
}

func TestHomeWorldIgnoresOrder(t *testing.T) {
	stars, planets, root := seededCluster(0x0123456789abcdef)
	homes := flagAll(t, stars, slices.Clone(planets), root, 6, 6)[:5]
	for _, h := range homes {
		planets[h].Habitability = 25
	}

	perm := rand.New(rand.NewPCG(3, 4)).Perm(len(stars))
	shuffled := make([]Star, len(stars))
	for i, j := range perm {
		shuffled[j] = stars[i]
	}
	moved := slices.Clone(planets)
	for i := range moved {
		moved[i].Star = perm[moved[i].Star]
	}
	order := rand.New(rand.NewPCG(5, 6)).Perm(len(moved))
	reordered := make([]Planet, len(moved))
	for i, j := range order {
		reordered[j] = moved[i]
	}
	var movedHomes []int
	for _, h := range homes {
		movedHomes = append(movedHomes, order[h])
	}
	slices.Reverse(movedHomes)

	j, err := HomeWorld(stars, planets, homes, 6, 6, root.Child(prng.HomeWorlds).PRNG())
	if err != nil {
		t.Fatal(err)
	}
	k, err := HomeWorld(shuffled, reordered, movedHomes, 6, 6, root.Child(prng.HomeWorlds).PRNG())
	if err != nil {
		t.Fatal(err)
	}
	if a, b := describe(stars, planets[j:j+1]), describe(shuffled, reordered[k:k+1]); a != b {
		t.Errorf("shuffling the stars, planets, and homes changed the home world from %s to %s", a, b)
	}
}

func TestHomeWorldPins(t *testing.T) {
	type run struct{ minHome, maxNeighbor int }
	flag := func(runs []run) []string {
		stars, planets, root := seededCluster(0x0123456789abcdef)
		var homes []int
		var got []string
		for _, r := range runs {
			j, err := HomeWorld(stars, planets, homes, r.minHome, r.maxNeighbor, root.Child(prng.HomeWorlds).PRNG())
			if err != nil {
				got = append(got, err.Error())
				continue
			}
			planets[j].Habitability = 25
			homes = append(homes, j)
			got = append(got, describe(stars, planets[j:j+1]))
		}
		return got
	}

	// Pin a fixed sequence of runs, so that a change to the selection rules,
	// the order of the draws, or math/rand/v2 cannot silently change the
	// home worlds an existing game flags.
	runs := []run{{6, 6}, {6, 6}, {6, 6}, {16, 16}, {3, 2}, {3, 1}, {10, 4}, {6, 6}, {6, 6}}
	got := flag(runs)
	want := "[[5 3 14A4rocky25] [26 18 17A3rocky25] [5 1 20A3rocky25] [11 29 12A4rocky25] [3 27 10A3rocky25] " +
		"no planet qualifies as a home world [26 29 28A4rocky25] [3 3 29A3rocky25] [13 4 17A3rocky25]]"
	if fmt.Sprint(got) != want {
		t.Errorf("home worlds = %v\nwant %s", got, want)
	}

	// The failed run leaves no trace: without it, the other runs flag the
	// same planets.
	without := flag(slices.Delete(slices.Clone(runs), 5, 6))
	if fmt.Sprint(without) != fmt.Sprint(slices.Delete(got, 5, 6)) {
		t.Errorf("without the failed run, home worlds = %v", without)
	}
}

func TestRaiseHomeDeposits(t *testing.T) {
	deposits := []Deposit{
		{Planet: 4, No: 3, Resource: Metals, Quantity: 1_000_003, Yield: 25},
		{Planet: 4, No: 1, Resource: Gold, Quantity: MinDeposit, Yield: 1},
		{Planet: 4, No: 2, Resource: Fuel, Quantity: 4_692_000_000, Yield: 66},
		{Planet: 4, No: 4, Resource: NonMetals, Quantity: 7, Yield: 75},
	}
	src := prng.New(1, 2)
	raised := slices.Clone(deposits)
	RaiseHomeDeposits(raised, src.PRNG())

	// The rolls are drawn in deposit-number order.
	p := src.PRNG()
	rolls := map[int]int{}
	for no := 1; no <= 4; no++ {
		rolls[no] = p.Roll(1, 1, 10)
	}
	for k, d := range deposits {
		r := raised[k]
		if want := min(MaxHomeYield, d.Yield+rolls[d.No]); r.Yield != want {
			t.Errorf("deposit %d's yield rose from %d to %d, want %d", d.No, d.Yield, r.Yield, want)
		}
		if want := d.Quantity * 125 / 100; r.Quantity != want {
			t.Errorf("deposit %d's quantity rose from %d to %d, want %d", d.No, d.Quantity, r.Quantity, want)
		}
		r.Yield, r.Quantity = d.Yield, d.Quantity
		if r != d {
			t.Errorf("deposit %d changed from %+v to %+v", d.No, d, raised[k])
		}
	}
	// Rounded down: 7 × 1.25 = 8.75.
	if raised[3].Quantity != 8 || raised[1].Quantity != 124_998 {
		t.Errorf("quantities %d and %d, want 8 and 124998", raised[3].Quantity, raised[1].Quantity)
	}
	// A yield set by hand above 65 clamps at 75, as does one at the cap.
	if raised[2].Yield != min(75, 66+rolls[2]) || raised[3].Yield != 75 {
		t.Errorf("yields %d and %d, want %d and 75", raised[2].Yield, raised[3].Yield, min(75, 66+rolls[2]))
	}

	// Yields rise by 1 to 10, each with about the same chance.
	counts := make([]int, 11)
	p = prng.New(3, 4).PRNG()
	for range 10000 {
		d := []Deposit{{No: 1, Quantity: MinDeposit, Yield: 1}}
		RaiseHomeDeposits(d, p)
		counts[d[0].Yield-1]++
	}
	for rise, n := range counts[1:] {
		if n < 900 || n > 1100 {
			t.Errorf("yield rose by %d %d times in 10000, want about 1000", rise+1, n)
		}
	}
	if counts[0] != 0 {
		t.Errorf("yield rose by 0 %d times", counts[0])
	}

	defer func() {
		if recover() == nil {
			t.Error("deposits on two planets did not panic")
		}
	}()
	RaiseHomeDeposits([]Deposit{{Planet: 1, No: 1, Yield: 1}, {Planet: 2, No: 1, Yield: 1}}, p)
}
