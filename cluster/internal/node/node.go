// Package node is a single replica: a versioned key-value store fronted by a
// worker pool, with lock-free counters and a cooperative-interruption shutdown.
//
// It fuses two books. The store and its sibling reconciliation are the
// per-replica side of Designing Data-Intensive Applications (Ch. 5). The
// concurrency machinery is C++ Concurrency in Action made operable in Go: a
// worker pool servicing RPCs and returning futures (Ch. 9), atomic counters for
// a lock-free live ops meter (Ch. 5 of that book — the memory model), and a
// drain that lets in-flight work finish before the node stops (interruptible
// threads, Ch. 9).
package node

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/emirhankaplan/quorum/cluster/internal/event"
	"github.com/emirhankaplan/quorum/cluster/internal/kv"
	"github.com/emirhankaplan/quorum/cluster/internal/security"
	"github.com/emirhankaplan/quorum/cluster/internal/transport"
	"github.com/emirhankaplan/quorum/cluster/internal/vv"
)

// Node is one replica in the cluster.
type Node struct {
	id       string
	identity security.Identity
	tr       transport.Transport
	bus      event.Emitter
	workers  int

	pool atomic.Pointer[WorkerPool] // swapped on revive; read on every RPC

	mu    sync.RWMutex
	store map[string][]kv.VersionedValue

	clock    atomic.Uint64 // this node's logical clock (monotonic)
	ops      atomic.Uint64 // RPCs served — the lock-free live meter
	alive    atomic.Bool
	draining atomic.Bool
}

// NodeState is the UI-facing view of a node.
type NodeState struct {
	ID    string `json:"id"`
	Alive bool   `json:"alive"`
	Keys  int    `json:"keys"`
	Ops   uint64 `json:"ops"`
}

// New builds a node and its worker pool. auth issues the node's identity used
// for inter-node authentication.
func New(id string, auth *security.Authenticator, tr transport.Transport, bus event.Emitter, workers int) *Node {
	n := &Node{
		id:       id,
		identity: auth.Issue(id),
		tr:       tr,
		bus:      bus,
		workers:  workers,
		store:    make(map[string][]kv.VersionedValue),
	}
	n.pool.Store(NewWorkerPool(workers, 1024))
	n.alive.Store(true)
	return n
}

// Start registers the node's RPC handler with the transport.
func (n *Node) Start() { n.tr.Register(n.id, n.handle) }

func (n *Node) ID() string                    { return n.id }
func (n *Node) Identity() security.Identity    { return n.identity }
func (n *Node) Transport() transport.Transport { return n.tr }
func (n *Node) Ops() uint64                     { return n.ops.Load() }
func (n *Node) Alive() bool                     { return n.alive.Load() }

// handle services an inbound replica RPC. The work runs on the worker pool and
// the result is awaited through a Future — the book's "submit task, get future"
// pattern. A dead or draining node refuses service (connection refused).
func (n *Node) handle(from string, req transport.Request) (transport.Response, error) {
	if !n.alive.Load() || n.draining.Load() {
		return transport.Response{}, transport.ErrNodeDown
	}
	n.ops.Add(1) // lock-free; feeds the live throughput meter

	type result struct {
		resp transport.Response
		err  error
	}
	fut := Submit(n.pool.Load(), func() result {
		switch req.Op {
		case transport.OpPut:
			n.applyPut(req.Key, req.Value)
			return result{}
		case transport.OpGet:
			return result{resp: transport.Response{Values: n.localGet(req.Key)}}
		default:
			return result{err: errors.New("unknown rpc")}
		}
	})
	r := fut.Get()
	return r.resp, r.err
}

// applyPut merges an incoming versioned value into the local sibling set:
// it supersedes older values, is dropped if already superseded, or joins as a
// concurrent sibling.
func (n *Node) applyPut(key string, val kv.VersionedValue) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.store[key] = kv.Merge(n.store[key], val)
}

func (n *Node) localGet(key string) []kv.VersionedValue {
	n.mu.RLock()
	defer n.mu.RUnlock()
	src := n.store[key]
	out := make([]kv.VersionedValue, len(src))
	copy(out, src)
	return out
}

// NextClock derives the version vector for a new write coordinated by this node.
// It clones the client-supplied causal context and bumps this node's own
// counter, so successive writes through the same coordinator supersede each
// other while writes coordinated by different nodes remain concurrent — the
// precise behaviour that turns a partition into visible siblings.
func (n *Node) NextClock(base vv.VersionVector) vv.VersionVector {
	if base == nil {
		base = vv.New()
	}
	c := n.clock.Add(1)
	if base[n.id] >= c {
		c = base[n.id] + 1
		n.clock.Store(c)
	}
	return base.Set(n.id, c)
}

// Drain performs a cooperative-interruption shutdown: refuse new work, let
// in-flight and queued jobs finish, then mark the node dead. This is the Go
// counterpart of the interruptible thread in C++ Concurrency in Action (Ch. 9):
// the interruption is observed at a safe point and the worker exits cleanly
// instead of being torn down mid-operation.
func (n *Node) Drain() {
	n.draining.Store(true)
	n.pool.Load().Shutdown()
	n.alive.Store(false)
}

// Revive brings a killed node back online with a fresh worker pool. Its stored
// data survives the kill (the store is never discarded), modelling a process
// restart on durable disk.
func (n *Node) Revive() {
	n.pool.Store(NewWorkerPool(n.workers, 1024))
	n.draining.Store(false)
	n.alive.Store(true)
}

// Snapshot returns the node's UI-facing state.
func (n *Node) Snapshot() NodeState {
	n.mu.RLock()
	keys := len(n.store)
	n.mu.RUnlock()
	return NodeState{ID: n.id, Alive: n.alive.Load(), Keys: keys, Ops: n.ops.Load()}
}

// Dump returns a copy of every key's reconciled sibling set, for the UI's
// per-node data inspector.
func (n *Node) Dump() map[string][]kv.VersionedValue {
	n.mu.RLock()
	defer n.mu.RUnlock()
	out := make(map[string][]kv.VersionedValue, len(n.store))
	for k, v := range n.store {
		cp := make([]kv.VersionedValue, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}
