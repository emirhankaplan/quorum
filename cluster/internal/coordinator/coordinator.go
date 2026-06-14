// Package coordinator turns one client PUT/GET into a quorum of replica RPCs.
//
// This is the leaderless replication core of Designing Data-Intensive
// Applications (Ch. 5): any node can coordinate a request; a write is sent to
// the key's N preference-list replicas and succeeds once W acknowledge; a read
// queries N replicas, returns once R respond, reconciles their versioned values
// into a sibling set, and read-repairs stale replicas. The fan-out itself is the
// C++ Concurrency in Action pattern (Ch. 8–9): issue N asynchronous calls and
// gather their results through futures/channels, the Go equivalent of
// std::async + std::future replica acknowledgements.
package coordinator

import (
	"context"
	"time"

	"github.com/emirhankaplan/quorum/cluster/internal/event"
	"github.com/emirhankaplan/quorum/cluster/internal/kv"
	"github.com/emirhankaplan/quorum/cluster/internal/node"
	"github.com/emirhankaplan/quorum/cluster/internal/ring"
	"github.com/emirhankaplan/quorum/cluster/internal/transport"
	"github.com/emirhankaplan/quorum/cluster/internal/vv"
)

// Coordinator wraps the local node (identity + logical clock), the ring
// (preference lists), and the transport (reaching replicas).
type Coordinator struct {
	local *node.Node
	tr    transport.Transport
	ring  *ring.Ring
	bus   event.Emitter
}

// New builds a coordinator bound to a local node.
func New(local *node.Node, tr transport.Transport, r *ring.Ring, bus event.Emitter) *Coordinator {
	return &Coordinator{local: local, tr: tr, ring: r, bus: bus}
}

// ReplicaOutcome is the per-replica result, surfaced to the UI.
type ReplicaOutcome struct {
	Node string `json:"node"`
	OK   bool   `json:"ok"`
	Err  string `json:"err,omitempty"`
}

// PutResult reports a coordinated write.
type PutResult struct {
	Key         string           `json:"key"`
	Value       string           `json:"value"`
	Coordinator string           `json:"coordinator"`
	N           int              `json:"n"`
	W           int              `json:"w"`
	Clock       vv.VersionVector `json:"clock"`
	Replicas    []ReplicaOutcome `json:"replicas"`
	Acks        int              `json:"acks"`
	OK          bool             `json:"ok"`
}

// GetResult reports a coordinated read.
type GetResult struct {
	Key          string              `json:"key"`
	Coordinator  string              `json:"coordinator"`
	N            int                 `json:"n"`
	R            int                 `json:"r"`
	Replicas     []ReplicaOutcome    `json:"replicas"`
	Responses    int                 `json:"responses"`
	Values       []kv.VersionedValue `json:"values"`
	Conflict     bool                `json:"conflict"`
	ReadRepaired []string            `json:"readRepaired"`
	OK           bool                `json:"ok"`
}

// Put coordinates a write. ctxClock is the causal context from a prior read
// (may be nil for a blind write). The write succeeds when W replicas ack; we
// still wait for all N outcomes (bounded — the transport fails fast on dead or
// partitioned peers) so the UI can show every replica's fate. A production
// coordinator would return as soon as W ack and finish the rest in the
// background; here completeness of the visualisation wins.
func (c *Coordinator) Put(ctx context.Context, key, value string, ctxClock vv.VersionVector, n, w int) PutResult {
	pref := c.ring.PreferenceList(key, n)
	n = len(pref)
	clock := c.local.NextClock(ctxClock)
	val := kv.VersionedValue{
		Value:       value,
		Clock:       clock,
		UpdatedAt:   time.Now(),
		Coordinator: c.local.ID(),
	}
	c.bus.Emit("write", map[string]any{
		"key": key, "value": value, "coordinator": c.local.ID(),
		"replicas": pref, "clock": clock, "n": n, "w": w,
	})

	type out struct {
		node string
		err  error
	}
	results := make(chan out, n)
	for _, peer := range pref {
		peer := peer
		go func() {
			c.bus.Emit("replicate", map[string]any{"from": c.local.ID(), "to": peer, "key": key, "mode": "write"})
			_, err := c.tr.Send(ctx, c.local.Identity(), peer, transport.Request{Op: transport.OpPut, Key: key, Value: val})
			if err == nil {
				c.bus.Emit("ack", map[string]any{"from": peer, "to": c.local.ID(), "key": key})
			}
			results <- out{peer, err}
		}()
	}

	byNode := make(map[string]ReplicaOutcome, n)
	acks := 0
	for i := 0; i < n; i++ {
		r := <-results
		if r.err == nil {
			acks++
			byNode[r.node] = ReplicaOutcome{Node: r.node, OK: true}
		} else {
			byNode[r.node] = ReplicaOutcome{Node: r.node, OK: false, Err: r.err.Error()}
		}
	}

	res := PutResult{
		Key: key, Value: value, Coordinator: c.local.ID(),
		N: n, W: w, Clock: clock, Acks: acks, OK: acks >= w,
		Replicas: orderOutcomes(pref, byNode),
	}
	return res
}

// Get coordinates a read: query N replicas, return once R respond, reconcile
// into a sibling set, and read-repair any stale replica that answered.
func (c *Coordinator) Get(ctx context.Context, key string, n, r int) GetResult {
	pref := c.ring.PreferenceList(key, n)
	n = len(pref)
	c.bus.Emit("read", map[string]any{"key": key, "coordinator": c.local.ID(), "replicas": pref, "n": n, "r": r})

	type out struct {
		node string
		vals []kv.VersionedValue
		err  error
	}
	results := make(chan out, n)
	for _, peer := range pref {
		peer := peer
		go func() {
			c.bus.Emit("replicate", map[string]any{"from": c.local.ID(), "to": peer, "key": key, "mode": "read"})
			resp, err := c.tr.Send(ctx, c.local.Identity(), peer, transport.Request{Op: transport.OpGet, Key: key})
			results <- out{peer, resp.Values, err}
		}()
	}

	collected := make([]out, 0, n)
	byNode := make(map[string]ReplicaOutcome, n)
	responses := 0
	var union []kv.VersionedValue
	for i := 0; i < n; i++ {
		o := <-results
		collected = append(collected, o)
		if o.err == nil {
			responses++
			byNode[o.node] = ReplicaOutcome{Node: o.node, OK: true}
			union = append(union, o.vals...)
		} else {
			byNode[o.node] = ReplicaOutcome{Node: o.node, OK: false, Err: o.err.Error()}
		}
	}

	resolved := kv.Reconcile(union)
	conflict := kv.HasConflict(resolved)
	if conflict {
		c.bus.Emit("conflict", map[string]any{"key": key, "values": resolved})
	}

	// Read-repair: push the reconciled frontier to any replica that answered
	// with a stale set, so the next read finds the cluster converged.
	var repaired []string
	if responses > 0 && len(resolved) > 0 {
		for _, o := range collected {
			if o.err != nil || !staleAgainst(o.vals, resolved) {
				continue
			}
			for _, v := range resolved {
				_, _ = c.tr.Send(ctx, c.local.Identity(), o.node, transport.Request{Op: transport.OpPut, Key: key, Value: v})
			}
			repaired = append(repaired, o.node)
			c.bus.Emit("repair", map[string]any{"from": c.local.ID(), "to": o.node, "key": key})
		}
	}

	return GetResult{
		Key: key, Coordinator: c.local.ID(), N: n, R: r,
		Responses: responses, OK: responses >= r,
		Values: resolved, Conflict: conflict, ReadRepaired: repaired,
		Replicas: orderOutcomes(pref, byNode),
	}
}

// staleAgainst reports whether a replica's value set differs from the resolved
// frontier (it is missing a current sibling or still holds a superseded value).
func staleAgainst(replica, resolved []kv.VersionedValue) bool {
	if len(replica) != len(resolved) {
		return true
	}
	for _, rv := range resolved {
		found := false
		for _, lv := range replica {
			if vv.Compare(lv.Clock, rv.Clock) == vv.Equal {
				found = true
				break
			}
		}
		if !found {
			return true
		}
	}
	return false
}

func orderOutcomes(pref []string, byNode map[string]ReplicaOutcome) []ReplicaOutcome {
	out := make([]ReplicaOutcome, 0, len(pref))
	for _, p := range pref {
		if o, ok := byNode[p]; ok {
			out = append(out, o)
		}
	}
	return out
}
