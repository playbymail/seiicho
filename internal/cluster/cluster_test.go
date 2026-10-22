// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package cluster

import (
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"testing"
)

func TestGenerate(t *testing.T) {
	stars := Generate(rand.NewPCG(1, 2))

	// Count the stars in each system, keeping the systems in draw order.
	type coord struct{ x, y, z int }
	var order []coord
	count := map[coord]int{}
	for _, s := range stars {
		c := coord{s.X, s.Y, s.Z}
		for _, v := range []int{s.X, s.Y, s.Z} {
			if v < 0 || v >= Side {
				t.Fatalf("star %+v is outside the cube", s)
			}
		}
		if count[c] == 0 {
			order = append(order, c)
		}
		if want := Seqs[count[c] : count[c]+1]; s.Seq != want {
			t.Errorf("star %+v has seq %q, want %q", s, s.Seq, want)
		}
		count[c]++
	}
	if len(order) != Systems {
		t.Fatalf("%d systems, want %d", len(order), Systems)
	}
	if len(stars) != 159 {
		t.Errorf("%d stars, want 159", len(stars))
	}
	for i, c := range order {
		want := 1
		switch {
		case i < 1:
			want = 5
		case i < 3:
			want = 4
		case i < 10:
			want = 3
		case i < 45:
			want = 2
		}
		if count[c] != want {
			t.Errorf("system %d at %v has %d stars, want %d", i, c, count[c], want)
		}
	}
}

func TestGenerateIsDeterministic(t *testing.T) {
	a, b := Generate(rand.NewPCG(1, 2)), Generate(rand.NewPCG(1, 2))
	if fmt.Sprint(a) != fmt.Sprint(b) {
		t.Error("the same seed gave different clusters")
	}
	if fmt.Sprint(a) == fmt.Sprint(Generate(rand.NewPCG(1, 3))) {
		t.Error("different seeds gave the same cluster")
	}

	// Pin the whole cluster, so that a change to the generator or to
	// math/rand/v2 cannot silently change the board an existing seed makes.
	if a[0] != (Star{14, 7, 1, "A"}) || a[len(a)-1] != (Star{23, 26, 17, "A"}) {
		t.Errorf("first and last stars = %+v, %+v; want {14 7 1 A}, {23 26 17 A}", a[0], a[len(a)-1])
	}
	h := fnv.New64a()
	fmt.Fprint(h, a)
	if got := h.Sum64(); got != 0x9f0e80e747e26c48 {
		t.Errorf("cluster hash = %#x, want 0x9f0e80e747e26c48", got)
	}
}
