package ring

import "testing"

func TestPreferenceListSize(t *testing.T) {
	r := New(64)
	for _, n := range []string{"n1", "n2", "n3"} {
		r.Add(n)
	}
	pl := r.PreferenceList("cart", 3)
	if len(pl) != 3 {
		t.Fatalf("want 3 replicas, got %d (%v)", len(pl), pl)
	}
	seen := map[string]bool{}
	for _, p := range pl {
		if seen[p] {
			t.Fatalf("preference list has duplicate node: %v", pl)
		}
		seen[p] = true
	}
}

func TestPreferenceListDeterministic(t *testing.T) {
	r := New(64)
	for _, n := range []string{"n1", "n2", "n3"} {
		r.Add(n)
	}
	a := r.PreferenceList("user:42", 3)
	b := r.PreferenceList("user:42", 3)
	if len(a) != len(b) {
		t.Fatalf("non-deterministic length")
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("preference list not stable: %v vs %v", a, b)
		}
	}
}

func TestPreferenceListCapsAtMembers(t *testing.T) {
	r := New(16)
	r.Add("only")
	pl := r.PreferenceList("k", 3)
	if len(pl) != 1 || pl[0] != "only" {
		t.Fatalf("want [only], got %v", pl)
	}
}

func TestRemoveReHomesMinimally(t *testing.T) {
	r := New(128)
	for _, n := range []string{"n1", "n2", "n3", "n4"} {
		r.Add(n)
	}
	// Count which keys map to which primary before and after removing n4.
	const keys = 2000
	before := make([]string, keys)
	for i := 0; i < keys; i++ {
		before[i] = r.PreferenceList(itoa(i), 1)[0]
	}
	r.Remove("n4")
	moved := 0
	for i := 0; i < keys; i++ {
		now := r.PreferenceList(itoa(i), 1)[0]
		if now != before[i] {
			moved++
		}
	}
	// Removing one of four nodes should move roughly a quarter of keys, and in
	// any case far fewer than all of them (consistent hashing's whole point).
	if moved == 0 || moved > keys/2 {
		t.Fatalf("expected a minority of keys to move, got %d/%d", moved, keys)
	}
	// Keys that did NOT belong to n4 must not have moved.
	for i := 0; i < keys; i++ {
		if before[i] != "n4" {
			if r.PreferenceList(itoa(i), 1)[0] != before[i] {
				t.Fatalf("key %d unnecessarily re-homed from %s", i, before[i])
			}
		}
	}
}

func TestEmptyRing(t *testing.T) {
	r := New(8)
	if pl := r.PreferenceList("k", 3); pl != nil {
		t.Fatalf("empty ring should return nil, got %v", pl)
	}
}
