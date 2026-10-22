// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package prng

import "testing"

// The values below pin the derivation that the package documents. If one of
// these tests fails, existing games would no longer reproduce from their
// seeds: fix the code, not the test.

func TestSplitMix64(t *testing.T) {
	// The reference sequence for state 1234567.
	state := uint64(1234567)
	for i, want := range []uint64{6457827717110365317, 3203168211198807973, 9817491932198370423} {
		if got := splitMix64(&state); got != want {
			t.Errorf("output %d = %d, want %d", i, got, want)
		}
	}
}

func TestSplitSeed(t *testing.T) {
	s1, s2 := SplitSeed(0x0123456789abcdef)
	if s1 != 0x157a3807a48faa9d || s2 != 0xd573529b34a1d093 {
		t.Errorf("SplitSeed = %#x, %#x; want 0x157a3807a48faa9d, 0xd573529b34a1d093", s1, s2)
	}
}

func TestChild(t *testing.T) {
	root := New(SplitSeed(0x0123456789abcdef))
	for _, tc := range []struct {
		path   []Node
		s1, s2 uint64
	}{
		{nil, 0x157a3807a48faa9d, 0xd573529b34a1d093},
		{[]Node{Cluster}, 0x2fb1d8b1710997d5, 0xc605e49e0297ee4f},
		{[]Node{Cluster, 7}, 0x2674e03cac061b4f, 0x2043da40d46ee7f0},
	} {
		if got := root.Child(tc.path...); got != (Source{tc.s1, tc.s2}) {
			t.Errorf("Child(%v) = %#x, %#x; want %#x, %#x", tc.path, got.s1, got.s2, tc.s1, tc.s2)
		}
	}
	if a, b := root.Child(Cluster, 7), root.Child(Cluster).Child(7); a != b {
		t.Errorf("Child(Cluster, 7) = %v, but Child(Cluster).Child(7) = %v", a, b)
	}
	if a, b := root.Child(1), root.Child(2); a == b {
		t.Errorf("siblings 1 and 2 are both %v", a)
	}
	if a, b := root.Child(1, 2), root.Child(2, 1); a == b {
		t.Errorf("paths 1, 2 and 2, 1 are both %v", a)
	}
}

func TestPCG(t *testing.T) {
	src := New(SplitSeed(0x0123456789abcdef)).Child(Cluster)
	for range 2 {
		p := src.PCG()
		if a, b := p.Uint64(), p.Uint64(); a != 0xfde90842e4654125 || b != 0x8391b764e5886694 {
			t.Errorf("PCG = %#x, %#x; want 0xfde90842e4654125, 0x8391b764e5886694", a, b)
		}
	}
}
