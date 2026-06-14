// Package vv implements version vectors (a.k.a. vector clocks keyed by node).
//
// Version vectors are the heart of Quorum's conflict detection. They are the
// concrete realisation of the "Detecting Concurrent Writes" section of
// Designing Data-Intensive Applications (Kleppmann, Ch. 5): every value a
// replica stores carries a vector of per-node counters, and by comparing two
// vectors we can tell whether one write happened-before the other or whether
// they are genuinely concurrent — in which case both must be kept as siblings
// rather than one silently overwriting the other.
//
// The whole package is deliberately pure and side-effect free so that the
// subtle ordering logic can be exhaustively unit-tested (see vv_test.go).
package vv

// VersionVector maps a node ID to that node's logical clock counter.
// A missing entry is treated as 0.
type VersionVector map[string]uint64

// Ordering is the result of comparing two version vectors.
type Ordering int

const (
	// Equal means the vectors are identical.
	Equal Ordering = iota
	// Before means a happened-before b (a is an ancestor of b).
	Before
	// After means a happened-after b (a is a descendant of b).
	After
	// Concurrent means neither dominates the other — a true conflict.
	Concurrent
)

func (o Ordering) String() string {
	switch o {
	case Equal:
		return "equal"
	case Before:
		return "before"
	case After:
		return "after"
	default:
		return "concurrent"
	}
}

// New returns an empty version vector.
func New() VersionVector { return VersionVector{} }

// Clone returns a deep copy so callers can mutate without aliasing.
func (vv VersionVector) Clone() VersionVector {
	out := make(VersionVector, len(vv))
	for k, v := range vv {
		out[k] = v
	}
	return out
}

// Incremented returns a copy of vv with node's counter bumped by one.
func (vv VersionVector) Incremented(node string) VersionVector {
	out := vv.Clone()
	out[node]++
	return out
}

// Set returns a copy of vv with node's counter set to at least value.
// (Counters are monotonic, so a lower value is ignored.)
func (vv VersionVector) Set(node string, value uint64) VersionVector {
	out := vv.Clone()
	if value > out[node] {
		out[node] = value
	}
	return out
}

// Compare returns how a relates to b.
//
//	Equal      -> identical
//	After      -> a dominates b (a is strictly newer)
//	Before     -> b dominates a (a is strictly older)
//	Concurrent -> neither dominates (conflict; keep both as siblings)
func Compare(a, b VersionVector) Ordering {
	var aGreater, bGreater bool
	// Walk the union of keys; absent entries are 0.
	for k, av := range a {
		if av > b[k] {
			aGreater = true
		}
	}
	for k, bv := range b {
		if bv > a[k] {
			bGreater = true
		}
	}
	switch {
	case !aGreater && !bGreater:
		return Equal
	case aGreater && !bGreater:
		return After
	case !aGreater && bGreater:
		return Before
	default:
		return Concurrent
	}
}

// Descends reports whether a happened-after-or-equal b (a >= b pointwise),
// i.e. a is a descendant of b. This is the predicate used to decide whether a
// new write supersedes an existing sibling.
func Descends(a, b VersionVector) bool {
	o := Compare(a, b)
	return o == After || o == Equal
}

// IsConcurrent reports whether a and b are concurrent (a true conflict).
func IsConcurrent(a, b VersionVector) bool {
	return Compare(a, b) == Concurrent
}

// Merge returns the pointwise maximum of a and b. Merging is how a coordinator
// produces a vector that descends from every value it has seen — used when a
// client resolves siblings into a single reconciled value.
func Merge(a, b VersionVector) VersionVector {
	out := a.Clone()
	for k, v := range b {
		if v > out[k] {
			out[k] = v
		}
	}
	return out
}
