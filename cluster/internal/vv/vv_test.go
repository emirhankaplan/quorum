package vv

import "testing"

func TestCompare(t *testing.T) {
	tests := []struct {
		name string
		a, b VersionVector
		want Ordering
	}{
		{"both empty", New(), New(), Equal},
		{"identical", VersionVector{"a": 2, "b": 1}, VersionVector{"a": 2, "b": 1}, Equal},
		{"a dominates", VersionVector{"a": 2, "b": 1}, VersionVector{"a": 1, "b": 1}, After},
		{"b dominates", VersionVector{"a": 1}, VersionVector{"a": 1, "b": 1}, Before},
		{"superset newer", VersionVector{"a": 1, "b": 1}, VersionVector{"a": 1}, After},
		{"concurrent disjoint", VersionVector{"a": 1}, VersionVector{"b": 1}, Concurrent},
		{"concurrent overlap", VersionVector{"a": 2, "b": 1}, VersionVector{"a": 1, "b": 2}, Concurrent},
		{"empty vs nonempty", New(), VersionVector{"a": 1}, Before},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Compare(tt.a, tt.b); got != tt.want {
				t.Fatalf("Compare(%v,%v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestCompareIsAntisymmetric(t *testing.T) {
	a := VersionVector{"x": 3, "y": 1}
	b := VersionVector{"x": 1, "y": 1}
	if Compare(a, b) != After || Compare(b, a) != Before {
		t.Fatalf("expected After/Before symmetry, got %v/%v", Compare(a, b), Compare(b, a))
	}
}

func TestDescends(t *testing.T) {
	if !Descends(VersionVector{"a": 2}, VersionVector{"a": 1}) {
		t.Fatal("a:2 should descend a:1")
	}
	if !Descends(VersionVector{"a": 1}, VersionVector{"a": 1}) {
		t.Fatal("equal vectors should descend each other")
	}
	if Descends(VersionVector{"a": 1}, VersionVector{"b": 1}) {
		t.Fatal("concurrent vectors must not descend")
	}
	if Descends(VersionVector{"a": 1}, VersionVector{"a": 2}) {
		t.Fatal("older must not descend newer")
	}
}

func TestConcurrent(t *testing.T) {
	if !IsConcurrent(VersionVector{"a": 1}, VersionVector{"b": 1}) {
		t.Fatal("disjoint single-node writes are concurrent")
	}
	if IsConcurrent(VersionVector{"a": 2}, VersionVector{"a": 1}) {
		t.Fatal("a:2 vs a:1 is not concurrent")
	}
}

func TestIncrementedIsImmutable(t *testing.T) {
	base := VersionVector{"a": 1}
	next := base.Incremented("a")
	if base["a"] != 1 {
		t.Fatalf("base mutated: %v", base)
	}
	if next["a"] != 2 {
		t.Fatalf("increment failed: %v", next)
	}
	// A second increment from the same base must dominate the first sibling-free.
	if Compare(next, base) != After {
		t.Fatalf("incremented vector should dominate its base")
	}
}

func TestMerge(t *testing.T) {
	a := VersionVector{"a": 2, "b": 1}
	b := VersionVector{"a": 1, "c": 3}
	m := Merge(a, b)
	want := VersionVector{"a": 2, "b": 1, "c": 3}
	if Compare(m, want) != Equal {
		t.Fatalf("Merge = %v, want %v", m, want)
	}
	// The merged vector must descend from both inputs (the point of merging).
	if !Descends(m, a) || !Descends(m, b) {
		t.Fatalf("merge must descend from both inputs")
	}
}

func TestSetMonotonic(t *testing.T) {
	v := VersionVector{"a": 5}
	if got := v.Set("a", 3)["a"]; got != 5 {
		t.Fatalf("Set must not lower a counter, got %d", got)
	}
	if got := v.Set("a", 9)["a"]; got != 9 {
		t.Fatalf("Set should raise, got %d", got)
	}
}
