// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package cluster

import (
	"cmp"
	"slices"

	"github.com/playbymail/seiicho/internal/prng"
)

const (
	// StarBudget is the average of a star's resource budget, X, which
	// runs from 85% to 115% of it.
	StarBudget = 4_080_000_000
	// MinDeposit is the smallest quantity a deposit holds.
	MinDeposit = 99_999
	// MostDeposits is the most deposits any planet has: the asteroid
	// belts' limit.
	MostDeposits = 45
	// MaxYield is the highest yield any deposit has, as a percentage.
	MaxYield = 25
)

// Resource codes.
const (
	Gold      = "gold"
	Fuel      = "fuel"
	Metals    = "mets"
	NonMetals = "nmts"
)

// resourceOrder lists the resources in canonical order, the order of the
// columns of the tables below.
var resourceOrder = [4]string{Gold, Fuel, Metals, NonMetals}

// maxDeposits is the most deposits a planet of each kind has.
var maxDeposits = map[string]int{Rocky: 30, Asteroids: MostDeposits, Giant: 35}

// eligibility holds each kind's weights for choosing a deposit's resource;
// a weight of 0 means the resource never appears on that kind.
var eligibility = map[string][4]int{
	Rocky:     {3, 3, 3, 2},
	Asteroids: {3, 2, 6, 2},
	Giant:     {0, 3, 2, 6},
}

// value holds each kind's resource preferences, which scale a deposit's
// weight when its star's budget is divided. It is the Value table of
// planetary-resources.md, derived from each kind's share of wealth by
// resource so that the game as a whole comes out at 1:3:4:4.
var value = map[string][4]int64{
	Rocky:     {44, 81, 121, 182},
	Asteroids: {52, 169, 85, 150},
	Giant:     {0, 103, 176, 73},
}

// yieldRanges holds the lowest and highest yield of each resource on each
// kind. Gold on giants has no range because it never appears there.
var yieldRanges = map[string][4][2]int{
	Rocky:     {{1, 3}, {1, 15}, {1, 25}, {1, 25}},
	Asteroids: {{1, 3}, {1, 25}, {1, 25}, {1, 25}},
	Giant:     {{}, {1, 25}, {1, 25}, {1, 25}},
}

// Deposit is deposit number No, from 1, of a Resource on planets[Planet],
// for the planets passed to Deposits. Yield is a percentage.
type Deposit struct {
	Planet   int
	No       int
	Resource string
	Quantity int64
	Yield    int
}

// Budgets returns the budget, X, of each of stars, drawing one roll per
// star from p in (x, y, z, seq) order. A star's budget depends only on its
// place in that order, never on its planets, so it can be recomputed from
// a game's seed. The result is indexed like stars and does not depend on
// their order.
func Budgets(stars []Star, p *prng.PRNG) []int64 {
	budgets := make([]int64, len(stars))
	for _, i := range starOrder(stars) {
		budgets[i] = drawBudget(p)
	}
	return budgets
}

// Deposits gives every planet its deposits and returns them. It visits the
// stars in (x, y, z, seq) order. For each star it draws the budget, X, from
// budgets, and then from deposits it draws each planet's deposit count in
// orbit order, then each deposit's resource, then each deposit's weight,
// and divides X among the star's deposits by weight. Once every star's
// quantities are final, it draws each deposit's yield from yields, in the
// same order.
//
// It returns the deposits in that order: by star, then planet orbit, then
// deposit number. The result depends on the stars and planets but not on
// their order. It panics if a star has no planets, or if its budget cannot
// give each of its deposits MinDeposit.
func Deposits(stars []Star, planets []Planet, budgets, deposits, yields *prng.PRNG) []Deposit {
	// orbits[i] lists the indexes in planets of stars[i]'s planets, in
	// orbit order.
	orbits := make([][]int, len(stars))
	for j, p := range planets {
		orbits[p.Star] = append(orbits[p.Star], j)
	}
	for _, o := range orbits {
		slices.SortFunc(o, func(a, b int) int { return cmp.Compare(planets[a].Orbit, planets[b].Orbit) })
	}

	var out []Deposit
	for _, i := range starOrder(stars) {
		x := drawBudget(budgets)
		first := len(out)
		for _, j := range orbits[i] {
			for no := range drawCount(planets[j].Kind, deposits) {
				out = append(out, Deposit{Planet: j, No: no + 1})
			}
		}
		star := out[first:]
		for k := range star {
			star[k].Resource = drawResource(planets[star[k].Planet].Kind, deposits)
		}
		weights := make([]int64, len(star))
		for k, d := range star {
			weights[k] = drawWeight(planets[d.Planet].Kind, d.Resource, deposits)
		}
		for k, q := range divide(x, weights) {
			star[k].Quantity = q
		}
	}
	for k, d := range out {
		out[k].Yield = drawYield(planets[d.Planet].Kind, d.Resource, yields)
	}
	return out
}

// starOrder returns the indexes of stars in (x, y, z, seq) order.
func starOrder(stars []Star) []int {
	order := make([]int, len(stars))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(i, j int) int { return Compare(stars[i], stars[j]) })
	return order
}

// drawBudget draws a star's budget: StarBudget × Roll(2, 425, 575) / 1000,
// rounded down.
func drawBudget(p *prng.PRNG) int64 {
	return StarBudget * int64(p.Roll(2, 425, 575)) / 1000
}

// drawCount draws the number of deposits on a planet of kind, from 1 to the
// kind's maximum: 1 + Roll(1, 0, s/2) + Roll(1, 0, s-s/2), where s is the
// maximum less 1.
func drawCount(kind string, p *prng.PRNG) int {
	most, ok := maxDeposits[kind]
	if !ok {
		panic("cluster: unknown planet kind")
	}
	s := most - 1
	return 1 + p.Roll(1, 0, s/2) + p.Roll(1, 0, s-s/2)
}

// drawResource draws the resource of a deposit on a planet of kind, with
// Roll(1, 1, total) against the cumulative eligibility weights in
// resourceOrder.
func drawResource(kind string, p *prng.PRNG) string {
	weights, ok := eligibility[kind]
	if !ok {
		panic("cluster: unknown planet kind")
	}
	total := 0
	for _, w := range weights {
		total += w
	}
	roll := p.Roll(1, 1, total)
	for i, w := range weights {
		if roll <= w {
			return resourceOrder[i]
		}
		roll -= w
	}
	panic("cluster: resource roll exceeds the eligibility weights")
}

// drawWeight draws the weight of a deposit of resource on a planet of kind:
// its value × Roll(2, 1, 6).
func drawWeight(kind, resource string, p *prng.PRNG) int64 {
	v := value[kind][resourceIndex(resource)]
	if v == 0 {
		panic("cluster: resource not allowed on planet kind")
	}
	return v * int64(p.Roll(2, 1, 6))
}

// divide divides budget x among deposits with weights. Each receives
// MinDeposit plus its share by weight of the rest, R, rounded down; the
// first deposits each receive one more unit until the quantities add up to
// exactly x. It panics if there are no deposits or if R would be negative.
func divide(x int64, weights []int64) []int64 {
	if len(weights) == 0 {
		panic("cluster: no deposits to divide a budget among")
	}
	r := x - int64(len(weights))*MinDeposit
	if r < 0 {
		panic("cluster: budget too small for its deposits")
	}
	var total int64
	for _, w := range weights {
		total += w
	}
	quantities := make([]int64, len(weights))
	left := r
	for i, w := range weights {
		share := r * w / total
		quantities[i] = MinDeposit + share
		left -= share
	}
	for i := range left {
		quantities[i]++
	}
	return quantities
}

// drawYield draws the yield of a deposit of resource on a planet of kind:
// lo + Roll(2, 0, (hi-lo)/2) for the range lo to hi, a triangle peaking at
// its midpoint.
func drawYield(kind, resource string, p *prng.PRNG) int {
	r := yieldRanges[kind][resourceIndex(resource)]
	lo, hi := r[0], r[1]
	if hi == 0 {
		panic("cluster: resource not allowed on planet kind")
	}
	return lo + p.Roll(2, 0, (hi-lo)/2)
}

// resourceIndex returns resource's place in resourceOrder.
func resourceIndex(resource string) int {
	i := slices.Index(resourceOrder[:], resource)
	if i < 0 {
		panic("cluster: unknown resource")
	}
	return i
}
