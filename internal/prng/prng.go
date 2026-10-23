// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package prng derives deterministic random sources for the game engine.
//
// A game has one root Source, made from the two words of its seed. Each part
// of the engine draws from a child of the root, named by a path of Node keys,
// so that adding draws to one part never shifts the numbers another part sees.
// Code that derives children from data must order that data itself, for
// example by sorting coordinates; never derive keys from map iteration order.
//
// # Stability
//
// Games are reproduced from their seeds, so everything below is fixed. Never
// change it, and never renumber or reuse a Node value; add new ones instead.
// The tests pin known outputs.
//
// SplitMix64 is Steele, Lea, and Flood's generator. With γ = 0x9e3779b97f4a7c15,
// one step advances the state by γ and returns mix(state), where
//
//	mix(z) = z ^ (z >> 31), after
//	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
//	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
//
// with all arithmetic modulo 2⁶⁴.
//
// SplitSeed(seed) starts SplitMix64 at state seed and returns its first two
// outputs as seed1 and seed2.
//
// Child(k₁, …, kₙ) applies one step per key, in order. A step from source
// (s1, s2) with key k starts SplitMix64 at state s1 ^ mix(s2 ^ k) and returns
// its first two outputs as the new (s1, s2). An empty path returns the source
// itself.
//
// PCG returns math/rand/v2's PCG seeded with (s1, s2). PRNG wraps that PCG
// in math/rand/v2's Rand; Roll(n, lo, hi) sums n draws of Rand.IntN(hi-lo+1)+lo,
// one draw per die, and Shuffle(n, swap) calls Rand.Shuffle(n, swap).
package prng

import "math/rand/v2"

// Node is a key in the path to a child source.
type Node uint64

// Path nodes. Values are permanent: never renumber or reuse one.
const (
	// Cluster is the source of a game's cluster of star systems.
	Cluster Node = 1
	// Planets is the source that allocates a cluster's planets to its stars.
	Planets Node = 2
	// PlanetKinds is the source that gives a cluster's planets their kinds.
	PlanetKinds Node = 3
	// Habitability is the source of a cluster's planets' habitability.
	Habitability Node = 4
	// ResourceBudgets is the source of each star's resource budget: one
	// roll per star.
	ResourceBudgets Node = 5
	// Deposits is the source of a cluster's deposits: per planet, a count;
	// per deposit, a resource and a weight.
	Deposits Node = 6
	// DepositYields is the source of a cluster's deposits' yields: one roll
	// per deposit.
	DepositYields Node = 7
	// HomeWorlds is the source of a home-world run: one shuffle of the
	// pool, then one roll per deposit of the flagged planet. Every run
	// starts it afresh.
	HomeWorlds Node = 8
)

// Source is a node in a tree of deterministic random sources. The zero value
// is a valid source, seeded with (0, 0).
type Source struct {
	s1, s2 uint64
}

// New returns the root source for a seed's two words.
func New(seed1, seed2 uint64) Source {
	return Source{s1: seed1, s2: seed2}
}

// SplitSeed expands a single seed into the two words of a root source.
func SplitSeed(seed uint64) (seed1, seed2 uint64) {
	state := seed
	return splitMix64(&state), splitMix64(&state)
}

// Child derives a source from s and a path of keys. The same source and path
// always give the same child.
func (s Source) Child(path ...Node) Source {
	for _, k := range path {
		state := s.s1 ^ mix(s.s2^uint64(k))
		s = Source{s1: splitMix64(&state), s2: splitMix64(&state)}
	}
	return s
}

// PCG returns a new generator seeded from s. Each call starts the same
// sequence again.
func (s Source) PCG() *rand.PCG {
	return rand.NewPCG(s.s1, s.s2)
}

// PRNG returns a new PRNG seeded from s. Each call starts the same sequence
// again.
func (s Source) PRNG() *PRNG {
	return &PRNG{r: rand.New(s.PCG())}
}

// PRNG rolls dice for the game engine.
type PRNG struct {
	r *rand.Rand
}

// Roll rolls numberOfDice dice, each giving lo through hi with equal
// chance, and returns their sum. Each die draws from the random source. It
// panics if numberOfDice < 1 or lo > hi.
func (p *PRNG) Roll(numberOfDice, lo, hi int) int {
	if numberOfDice < 1 || lo > hi {
		panic("prng: invalid Roll")
	}
	sum := 0
	for range numberOfDice {
		sum += lo + p.r.IntN(hi-lo+1)
	}
	return sum
}

// Shuffle shuffles n elements, calling swap to exchange the elements with
// indexes i and j. It draws from the random source as math/rand/v2's
// Rand.Shuffle does. It panics if n < 0.
func (p *PRNG) Shuffle(n int, swap func(i, j int)) {
	p.r.Shuffle(n, swap)
}

// splitMix64 advances state by one SplitMix64 step and returns the output.
func splitMix64(state *uint64) uint64 {
	*state += 0x9e3779b97f4a7c15
	return mix(*state)
}

// mix is SplitMix64's output function.
func mix(z uint64) uint64 {
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}
