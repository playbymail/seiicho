// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package cluster

import (
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/playbymail/seiicho/internal/prng"
)

// depositSources returns the deposit sources of the game with seed.
func depositSources(seed uint64) (budgets, deposits, yields prng.Source) {
	root := prng.New(prng.SplitSeed(seed))
	return root.Child(prng.ResourceBudgets), root.Child(prng.Deposits), root.Child(prng.DepositYields)
}

func TestDeposits(t *testing.T) {
	root := prng.New(prng.SplitSeed(0x0123456789abcdef))
	stars := Generate(rand.NewPCG(1, 2))
	planets := Planets(stars, root.Child(prng.Planets).PRNG(), root.Child(prng.PlanetKinds).PRNG(), root.Child(prng.Habitability).PRNG())
	b, d, y := depositSources(0x0123456789abcdef)
	deposits := Deposits(stars, planets, b.PRNG(), d.PRNG(), y.PRNG())
	checkDeposits(t, stars, planets, deposits, Budgets(stars, b.PRNG()))

	// The same inputs give the same deposits.
	if got, want := describeDeposits(stars, planets, Deposits(stars, planets, b.PRNG(), d.PRNG(), y.PRNG())), describeDeposits(stars, planets, deposits); got != want {
		t.Error("the same stars, planets, and seed gave different deposits")
	}

	// The result depends on the stars and planets, not their order.
	perm := rand.New(rand.NewPCG(3, 4)).Perm(len(stars))
	shuffled := make([]Star, len(stars))
	for i, j := range perm {
		shuffled[j] = stars[i]
	}
	moved := slices.Clone(planets)
	for i := range moved {
		moved[i].Star = perm[moved[i].Star]
	}
	SortPlanets(shuffled, moved)
	if want, got := describeDeposits(stars, planets, deposits), describeDeposits(shuffled, moved, Deposits(shuffled, moved, b.PRNG(), d.PRNG(), y.PRNG())); got != want {
		t.Error("shuffling the stars and planets changed the deposits")
	}

	// Pin the deposits, so that a change to the deposit rules, the order of
	// the draws, or math/rand/v2 cannot silently change the deposits an
	// existing seed makes.
	h := fnv.New64a()
	fmt.Fprint(h, describeDeposits(stars, planets, deposits))
	if got := h.Sum64(); got != 0xacc6dcf9064685d0 {
		t.Errorf("deposits hash = %#x, want 0xacc6dcf9064685d0", got)
	}
	if len(deposits) != 12895 {
		t.Errorf("%d deposits, want 12895", len(deposits))
	}

	// Different seeds give different deposits.
	b2, d2, y2 := depositSources(0xfedcba9876543210)
	if describeDeposits(stars, planets, deposits) == describeDeposits(stars, planets, Deposits(stars, planets, b2.PRNG(), d2.PRNG(), y2.PRNG())) {
		t.Error("different seeds gave the same deposits")
	}
}

func TestDepositsHandBuilt(t *testing.T) {
	// One star for each number of planets from 1 to 10, kinds in turn, and
	// one star with only giants.
	var stars []Star
	var planets []Planet
	for n := 1; n <= MaxOrbits; n++ {
		stars = append(stars, Star{X: n, Seq: "A"})
		for orbit := 1; orbit <= n; orbit++ {
			planets = append(planets, Planet{Star: n - 1, Orbit: orbit, Kind: kindOrder[(n+orbit)%3]})
		}
	}
	stars = append(stars, Star{X: 0, Seq: "A"})
	for orbit := 1; orbit <= 4; orbit++ {
		planets = append(planets, Planet{Star: len(stars) - 1, Orbit: orbit, Kind: Giant})
	}
	for seed := range uint64(20) {
		b, d, y := depositSources(seed)
		deposits := Deposits(stars, planets, b.PRNG(), d.PRNG(), y.PRNG())
		checkDeposits(t, stars, planets, deposits, Budgets(stars, b.PRNG()))
	}
}

func TestBudgetsIgnorePlanets(t *testing.T) {
	// Changing every planet's kind changes the deposits but not the stars'
	// totals, which are their budgets.
	root := prng.New(prng.SplitSeed(0x0123456789abcdef))
	stars := Generate(rand.NewPCG(1, 2))
	planets := Planets(stars, root.Child(prng.Planets).PRNG(), root.Child(prng.PlanetKinds).PRNG(), root.Child(prng.Habitability).PRNG())
	giants := slices.Clone(planets)
	for i := range giants {
		giants[i].Kind = Giant
	}
	b, d, y := depositSources(0x0123456789abcdef)
	before := starTotals(stars, planets, Deposits(stars, planets, b.PRNG(), d.PRNG(), y.PRNG()))
	after := starTotals(stars, giants, Deposits(stars, giants, b.PRNG(), d.PRNG(), y.PRNG()))
	if !slices.Equal(before, after) {
		t.Error("changing planet kinds changed the budgets")
	}
	if want := Budgets(stars, b.PRNG()); !slices.Equal(before, want) {
		t.Error("the stars' deposits do not add up to their recomputed budgets")
	}
}

func TestBudgets(t *testing.T) {
	p := prng.New(9, 10).PRNG()
	lo, hi, mid := int64(math.MaxInt64), int64(0), 0
	for range 100000 {
		x := drawBudget(p)
		if x < StarBudget*85/100 || x > StarBudget*115/100 {
			t.Fatalf("budget %d, want %d to %d", x, StarBudget*85/100, StarBudget*115/100)
		}
		lo, hi = min(lo, x), max(hi, x)
		if x > StarBudget*95/100 && x < StarBudget*105/100 {
			mid++
		}
	}
	// A triangle puts 5/9 of its weight within a third of its middle.
	if mid < 50000 || mid > 60000 {
		t.Errorf("%d of 100000 budgets within 5%% of StarBudget, want about 55,600", mid)
	}
	if lo > StarBudget*86/100 || hi < StarBudget*114/100 {
		t.Errorf("budgets ran from %d to %d, want nearly 85%% to 115%% of StarBudget", lo, hi)
	}
}

func TestDivide(t *testing.T) {
	for _, tc := range []struct {
		x       int64
		weights []int64
		want    []int64
	}{
		// A single deposit receives all of x.
		{1_000_000, []int64{7}, []int64{1_000_000}},
		// R = 10, split 1:1:1 leaves 1 unit, which goes to the first.
		{3*MinDeposit + 10, []int64{5, 5, 5}, []int64{MinDeposit + 4, MinDeposit + 3, MinDeposit + 3}},
		// R = 8, split 1:2:3 gives 1, 2, 4 and leaves 1 for the first.
		{3*MinDeposit + 8, []int64{1, 2, 3}, []int64{MinDeposit + 2, MinDeposit + 2, MinDeposit + 4}},
		// R = 0 gives each deposit exactly MinDeposit.
		{4 * MinDeposit, []int64{1, 9, 2, 12}, []int64{MinDeposit, MinDeposit, MinDeposit, MinDeposit}},
		// R = 5 among six equal weights: the first five get a unit each.
		{6*MinDeposit + 5, []int64{2, 2, 2, 2, 2, 2},
			[]int64{MinDeposit + 1, MinDeposit + 1, MinDeposit + 1, MinDeposit + 1, MinDeposit + 1, MinDeposit}},
	} {
		got := divide(tc.x, tc.weights)
		if !slices.Equal(got, tc.want) {
			t.Errorf("divide(%d, %v) = %v, want %v", tc.x, tc.weights, got, tc.want)
		}
	}

	// The remainder is less than the number of deposits.
	p := prng.New(11, 12).PRNG()
	for range 1000 {
		weights := make([]int64, p.Roll(1, 1, 450))
		for i := range weights {
			weights[i] = int64(p.Roll(1, 2, 2184))
		}
		x := drawBudget(p)
		got := divide(x, weights)
		var sum, shares int64
		r := x - int64(len(weights))*MinDeposit
		var total int64
		for _, w := range weights {
			total += w
		}
		for i, q := range got {
			sum += q
			extra := q - MinDeposit - r*weights[i]/total
			if extra != 0 && extra != 1 {
				t.Fatalf("deposit %d got %d units over its share, want 0 or 1", i, extra)
			}
			if i > 0 && extra == 1 && got[i-1]-MinDeposit-r*weights[i-1]/total == 0 {
				t.Fatalf("deposit %d got a remainder unit that deposit %d did not", i, i-1)
			}
			shares += extra
		}
		if sum != x || shares >= int64(len(weights)) {
			t.Fatalf("divide(%d, %d weights) adds up to %d with %d left over", x, len(weights), sum, shares)
		}
	}

	for _, tc := range []struct {
		x       int64
		weights []int64
	}{{1_000_000, nil}, {3*MinDeposit - 1, []int64{1, 1, 1}}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("divide(%d, %v) did not panic", tc.x, tc.weights)
				}
			}()
			divide(tc.x, tc.weights)
		}()
	}
}

func TestDrawCount(t *testing.T) {
	// Counts are triangular: rocky planets peak at 15 and 16, giants at 18,
	// and asteroid belts at 23.
	p := prng.New(13, 14).PRNG()
	for kind, peaks := range map[string][]int{Rocky: {15, 16}, Giant: {18}, Asteroids: {23}} {
		counts := make([]int, maxDeposits[kind]+1)
		for range 400000 {
			n := drawCount(kind, p)
			if n < 1 || n > maxDeposits[kind] {
				t.Fatalf("%s drew %d deposits, want 1 to %d", kind, n, maxDeposits[kind])
			}
			counts[n]++
		}
		if counts[1] == 0 || counts[maxDeposits[kind]] == 0 {
			t.Errorf("%s never drew 1 or %d deposits", kind, maxDeposits[kind])
		}
		top := slices.Max(counts)
		for _, n := range peaks {
			if counts[n] < top*97/100 {
				t.Errorf("%s drew %d deposits %d times, want about %d", kind, n, counts[n], top)
			}
		}
		for n, c := range counts {
			if !slices.Contains(peaks, n) && c > counts[peaks[0]] {
				t.Errorf("%s drew %d deposits %d times, more than the peak %d", kind, n, c, peaks[0])
			}
		}
	}
}

func TestDrawResource(t *testing.T) {
	const draws = 120000
	p := prng.New(15, 16).PRNG()
	for kind, weights := range eligibility {
		total := 0
		for _, w := range weights {
			total += w
		}
		counts := map[string]int{}
		for range draws {
			counts[drawResource(kind, p)]++
		}
		for i, w := range weights {
			want := draws * w / total
			if got := counts[resourceOrder[i]]; got < want*95/100 || got > want*105/100 {
				t.Errorf("%s drew %s %d times, want about %d", kind, resourceOrder[i], got, want)
			}
		}
	}
}

func TestDrawYield(t *testing.T) {
	// Yields are symmetric triangles: 1 to 3 gives 1, 2, or 3 with chances
	// 1/4, 1/2, and 1/4.
	p := prng.New(17, 18).PRNG()
	counts := make([]int, 4)
	for range 40000 {
		counts[drawYield(Rocky, Gold, p)]++
	}
	for y, want := range []int{0, 10000, 20000, 10000} {
		if got := counts[y]; got < want*95/100 || got > want*105/100 {
			t.Errorf("rocky gold yield %d drawn %d times, want about %d", y, got, want)
		}
	}
	for _, tc := range []struct {
		kind, resource string
		peak           int
	}{{Rocky, Fuel, 8}, {Asteroids, Fuel, 13}, {Giant, NonMetals, 13}} {
		counts := make([]int, MaxYield+1)
		for range 100000 {
			counts[drawYield(tc.kind, tc.resource, p)]++
		}
		if top := slices.Max(counts); counts[tc.peak] < top*97/100 {
			t.Errorf("%s %s yield %d drawn %d times, want the most, about %d", tc.kind, tc.resource, tc.peak, counts[tc.peak], top)
		}
	}
	defer func() {
		if recover() == nil {
			t.Error("a yield for gold on a giant did not panic")
		}
	}()
	drawYield(Giant, Gold, p)
}

// shares is each kind's share of wealth by resource, in percent, as
// planetary-resources.md gives them.
var shares = map[string][4]int{
	Rocky:     {12, 22, 33, 33},
	Asteroids: {12, 26, 39, 23},
	Giant:     {0, 28, 32, 40},
}

func TestValue(t *testing.T) {
	// The approved Value table, from planetary-resources.md.
	want := map[string][4]int64{
		Rocky:     {44, 81, 121, 182},
		Asteroids: {52, 169, 85, 150},
		Giant:     {0, 103, 176, 73},
	}
	for _, kind := range kindOrder {
		if value[kind] != want[kind] {
			t.Errorf("value[%s] = %v, want %v", kind, value[kind], want[kind])
		}
		// Value = share × (sum of the kind's eligibility weights) /
		// eligibility weight, rounded to the nearest integer.
		e, total := eligibility[kind], 0
		for _, w := range e {
			total += w
		}
		for i, share := range shares[kind] {
			var v int64
			if e[i] > 0 {
				v = int64(math.Round(float64(share*total) / float64(e[i])))
			}
			if v != value[kind][i] {
				t.Errorf("value[%s][%s] = %d, want %d from its share", kind, resourceOrder[i], value[kind][i], v)
			}
		}
	}

	// Each kind's shares follow its preference order.
	g, f, m, n := 0, 1, 2, 3
	for kind, order := range map[string][]int{Rocky: {m, n, f, g}, Asteroids: {m, f, n, g}, Giant: {n, m, f}} {
		s := shares[kind]
		for i := 1; i < len(order); i++ {
			a, b := s[order[i-1]], s[order[i]]
			if a < b || a == b && !(kind == Rocky && i == 1) {
				t.Errorf("%s shares %v break the preference order at %s, %s", kind, s, resourceOrder[order[i-1]], resourceOrder[order[i]])
			}
		}
	}
}

// checkDeposits checks every invariant of the deposits generated for stars
// and planets, whose stars have budgets.
func checkDeposits(t *testing.T, stars []Star, planets []Planet, deposits []Deposit, budgets []int64) {
	t.Helper()
	counts := make([]int, len(planets))
	for i, d := range deposits {
		p := planets[d.Planet]
		counts[d.Planet]++
		if d.No != counts[d.Planet] {
			t.Errorf("deposit %d is number %d on its planet, want %d", i, d.No, counts[d.Planet])
		}
		if !slices.Contains(resourceOrder[:], d.Resource) {
			t.Errorf("deposit %d is %q, want one of %q", i, d.Resource, resourceOrder)
		}
		if d.Resource == Gold && p.Kind == Giant {
			t.Errorf("deposit %d is gold on a giant", i)
		}
		if d.Quantity < MinDeposit {
			t.Errorf("deposit %d holds %d, want at least %d", i, d.Quantity, MinDeposit)
		}
		hi := MaxYield
		switch {
		case d.Resource == Gold:
			hi = 3
		case d.Resource == Fuel && p.Kind == Rocky:
			hi = 15
		}
		if d.Yield < 1 || d.Yield > hi {
			t.Errorf("deposit %d is %s on %s with yield %d, want 1 to %d", i, d.Resource, p.Kind, d.Yield, hi)
		}
		if i > 0 {
			q := planets[deposits[i-1].Planet]
			if c := Compare(stars[q.Star], stars[p.Star]); c > 0 || c == 0 && q.Orbit > p.Orbit {
				t.Errorf("deposit %d is out of canonical order", i)
			}
		}
	}
	for j, n := range counts {
		if most := maxDeposits[planets[j].Kind]; n < 1 || n > most {
			t.Errorf("planet %d is %s with %d deposits, want 1 to %d", j, planets[j].Kind, n, most)
		}
	}
	for i, x := range starTotals(stars, planets, deposits) {
		if x != budgets[i] {
			t.Errorf("star %+v's deposits add up to %d, want its budget %d", stars[i], x, budgets[i])
		}
		if x < StarBudget*85/100 || x > StarBudget*115/100 {
			t.Errorf("star %+v has budget %d, want 85%% to 115%% of %d", stars[i], x, StarBudget)
		}
	}
}

// starTotals returns the sum of each star's deposits, indexed like stars.
func starTotals(stars []Star, planets []Planet, deposits []Deposit) []int64 {
	totals := make([]int64, len(stars))
	for _, d := range deposits {
		totals[planets[d.Planet].Star] += d.Quantity
	}
	return totals
}

// describeDeposits lists deposits by their planets' stars' coordinates and
// orbits rather than indexes.
func describeDeposits(stars []Star, planets []Planet, deposits []Deposit) string {
	var s []string
	for _, d := range deposits {
		p := planets[d.Planet]
		st := stars[p.Star]
		s = append(s, fmt.Sprint(st.X, st.Y, st.Z, st.Seq, p.Orbit, d.No, d.Resource, d.Quantity, d.Yield))
	}
	return fmt.Sprint(s)
}
