// Package kv defines the versioned value model stored on every replica and the
// reconciliation logic that keeps concurrent writes as siblings.
//
// This is the data side of Designing Data-Intensive Applications, Ch. 5:
// a key does not map to a single value but to a *set* of causally-concurrent
// versioned values. Writes that causally descend existing values supersede
// them; concurrent writes are retained side-by-side until a client merges them.
package kv

import (
	"time"

	"github.com/emirhankaplan/quorum/cluster/internal/vv"
)

// VersionedValue is a single value tagged with the version vector under which
// it was written. Tombstone marks a delete.
type VersionedValue struct {
	Value     string           `json:"value"`
	Clock     vv.VersionVector `json:"clock"`
	Tombstone bool             `json:"tombstone"`
	UpdatedAt time.Time        `json:"updatedAt"`
	// Coordinator records which node coordinated the write — purely for the
	// UI's "who wrote this sibling" narration.
	Coordinator string `json:"coordinator,omitempty"`
}

// Reconcile collapses a slice of versioned values down to its causal frontier:
// the set of values that are not dominated (happened-before) by any other.
// Equal clocks are de-duplicated. The result is the correct sibling set for a
// key — exactly one element when there is no conflict, several when writes were
// concurrent.
func Reconcile(values []VersionedValue) []VersionedValue {
	frontier := make([]VersionedValue, 0, len(values))
	for i := range values {
		candidate := values[i]
		dominated := false
		// Does any *other* value strictly descend (supersede) the candidate?
		// A value compared to itself is Equal, never After, so it can never
		// dominate itself — no self-skip is required.
		for j := range values {
			if j == i {
				continue
			}
			if vv.Compare(values[j].Clock, candidate.Clock) == vv.After {
				dominated = true
				break
			}
		}
		if dominated {
			continue
		}
		// Drop exact duplicates already in the frontier.
		dup := false
		for _, kept := range frontier {
			if vv.Compare(kept.Clock, candidate.Clock) == vv.Equal {
				dup = true
				break
			}
		}
		if !dup {
			frontier = append(frontier, candidate)
		}
	}
	return frontier
}

// Merge folds b into the existing sibling set a and reconciles the result.
// It is the per-replica apply path: a new incoming value either supersedes
// existing siblings, is superseded by them, or joins them as a fresh sibling.
func Merge(existing []VersionedValue, incoming VersionedValue) []VersionedValue {
	return Reconcile(append(append([]VersionedValue{}, existing...), incoming))
}

// HasConflict reports whether a reconciled set contains genuine siblings.
func HasConflict(values []VersionedValue) bool { return len(values) > 1 }
