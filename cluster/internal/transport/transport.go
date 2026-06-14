// Package transport is the seam between a node and "the network". Every node
// talks to its peers exclusively through a Transport, never by touching another
// node's memory directly. That single abstraction is what lets Quorum:
//
//   - run the whole cluster as goroutines in one process (InProcess) for a
//     zero-dependency demo, while leaving a clean place to drop in a real gRPC
//     transport later (the "swappable transport" the architecture promises);
//   - inject chaos purely at the network layer — a partition is just a rule
//     that drops messages between two groups, a killed node is just an
//     unreachable address — without the node code knowing or caring;
//   - enforce the security boundary in one place: every Send authenticates the
//     sender's node identity, so a spoofed peer is refused before its bytes ever
//     reach a replica.
package transport

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/emirhankaplan/quorum/cluster/internal/kv"
	"github.com/emirhankaplan/quorum/cluster/internal/security"
)

// RPC names the inter-node operations.
type RPC string

const (
	OpPut RPC = "put_replica"
	OpGet RPC = "get_replica"
)

// Request is a replica-level RPC issued by a coordinator.
type Request struct {
	Op    RPC
	Key   string
	Value kv.VersionedValue // populated for OpPut
}

// Response carries a replica's reply.
type Response struct {
	Values []kv.VersionedValue // populated for OpGet
}

// Handler processes an inbound RPC on the receiving node.
type Handler func(from string, req Request) (Response, error)

// Delivery / security failures, surfaced to the coordinator and UI.
var (
	ErrNodeDown    = errors.New("connection refused: node is down")
	ErrPartitioned = errors.New("network partition: peer unreachable")
	ErrNoRoute     = errors.New("no such node")
)

// Transport is the swappable network interface.
type Transport interface {
	Register(nodeID string, h Handler)
	Send(ctx context.Context, from security.Identity, to string, req Request) (Response, error)
}

// InProcess delivers RPCs by directly invoking the target's Handler on the
// caller's goroutine, gated by authentication, liveness, partition, and latency
// rules. It is safe for concurrent use.
type InProcess struct {
	auth *security.Authenticator

	mu       sync.RWMutex
	handlers map[string]Handler
	down     map[string]bool // killed nodes (connection refused)
	group    map[string]int  // partition group; same group == reachable
	latency  time.Duration   // injected one-way delay per hop
}

// NewInProcess creates an in-process transport that authenticates peers with
// the given Authenticator.
func NewInProcess(auth *security.Authenticator) *InProcess {
	return &InProcess{
		auth:     auth,
		handlers: make(map[string]Handler),
		down:     make(map[string]bool),
		group:    make(map[string]int),
	}
}

// Register wires a node's handler into the transport.
func (t *InProcess) Register(nodeID string, h Handler) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.handlers[nodeID] = h
	t.group[nodeID] = 0
}

// Send authenticates the sender, applies network rules, then delivers.
func (t *InProcess) Send(ctx context.Context, from security.Identity, to string, req Request) (Response, error) {
	// 1. Authenticate the peer. A spoofed/rogue node fails here and never
	//    reaches the target — the core inter-node trust control.
	if !t.auth.VerifyIdentity(from) {
		return Response{}, security.ErrUntrustedNode
	}

	t.mu.RLock()
	h, ok := t.handlers[to]
	downTo := t.down[to]
	downFrom := t.down[from.NodeID]
	sameGroup := t.group[from.NodeID] == t.group[to]
	latency := t.latency
	t.mu.RUnlock()

	if !ok {
		return Response{}, ErrNoRoute
	}
	// 2. Liveness: a killed node (or a killed sender) is unreachable.
	if downTo || downFrom {
		return Response{}, ErrNodeDown
	}
	// 3. Partition: peers in different groups cannot exchange messages.
	if !sameGroup {
		return Response{}, ErrPartitioned
	}
	// 4. Latency: make replication observable and quorum-waiting real.
	if latency > 0 {
		select {
		case <-time.After(latency):
		case <-ctx.Done():
			return Response{}, ctx.Err()
		}
	}
	return h(from.NodeID, req)
}

// --- chaos / control plane (used by the cluster, exposed via the API) ---

// SetDown marks a node reachable/unreachable (the routing side of a kill).
func (t *InProcess) SetDown(nodeID string, down bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.down[nodeID] = down
}

// Partition assigns nodes to communication groups. Nodes only reach peers in
// the same group; nodes not listed keep their current group.
func (t *InProcess) Partition(groups [][]string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for gi, g := range groups {
		for _, n := range g {
			t.group[n] = gi + 1 // +1 so it differs from the default 0
		}
	}
}

// Heal merges everyone back into a single group.
func (t *InProcess) Heal() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for n := range t.group {
		t.group[n] = 0
	}
}

// SetLatency injects a per-hop one-way delay.
func (t *InProcess) SetLatency(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.latency = d
}
