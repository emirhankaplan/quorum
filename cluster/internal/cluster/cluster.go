// Package cluster wires the whole engine together: it spins up N nodes sharing
// one in-process transport and one consistent-hash ring, gives each node a
// coordinator, and exposes the operations the HTTP API drives — quorum PUT/GET,
// chaos (kill / partition / latency), and the two security demonstrations
// (spoofed-node and forged-token rejection). It is the single object main()
// hands to the API and stream layers.
package cluster

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/emirhankaplan/quorum/cluster/internal/coordinator"
	"github.com/emirhankaplan/quorum/cluster/internal/event"
	"github.com/emirhankaplan/quorum/cluster/internal/kv"
	"github.com/emirhankaplan/quorum/cluster/internal/node"
	"github.com/emirhankaplan/quorum/cluster/internal/ring"
	"github.com/emirhankaplan/quorum/cluster/internal/security"
	"github.com/emirhankaplan/quorum/cluster/internal/transport"
	"github.com/emirhankaplan/quorum/cluster/internal/vv"
)

// Config parameterises a cluster.
type Config struct {
	NodeIDs []string
	VNodes  int
	Workers int
	Secret  []byte
}

// Cluster is the live engine.
type Cluster struct {
	auth  *security.Authenticator
	tr    *transport.InProcess
	ring  *ring.Ring
	bus   *event.Bus
	order []string
	nodes map[string]*node.Node
	coord map[string]*coordinator.Coordinator

	mu        sync.RWMutex
	partition [][]string
	latencyMs int
}

// New constructs and starts a cluster.
func New(cfg Config) *Cluster {
	if cfg.VNodes < 1 {
		cfg.VNodes = 64
	}
	if cfg.Workers < 1 {
		cfg.Workers = 8
	}
	auth := security.New(cfg.Secret)
	bus := event.NewBus()
	tr := transport.NewInProcess(auth)
	r := ring.New(cfg.VNodes)

	c := &Cluster{
		auth: auth, tr: tr, ring: r, bus: bus,
		nodes: make(map[string]*node.Node),
		coord: make(map[string]*coordinator.Coordinator),
	}
	for _, id := range cfg.NodeIDs {
		n := node.New(id, auth, tr, bus, cfg.Workers)
		n.Start()
		r.Add(id)
		c.nodes[id] = n
		c.coord[id] = coordinator.New(n, tr, r, bus)
		c.order = append(c.order, id)
	}
	return c
}

// Bus exposes the event bus for the stream layer.
func (c *Cluster) Bus() *event.Bus { return c.bus }

// VerifyToken authorises a client capability for (key, op).
func (c *Cluster) VerifyToken(token, key, op string) error {
	_, err := c.auth.VerifyToken(token, key, op, time.Now())
	return err
}

// DefaultToken mints the broad capability the bundled UI uses for its own
// requests (every request still carries and is checked against a token).
func (c *Cluster) DefaultToken() string {
	return c.auth.IssueToken(security.Capability{
		Subject: "quorum-ui", Keys: []string{"*"}, Ops: []string{"get", "put"},
	})
}

func (c *Cluster) resolveCoord(id string) (*coordinator.Coordinator, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if id == "" {
		for _, nid := range c.order {
			if c.nodes[nid].Alive() {
				return c.coord[nid], nil
			}
		}
		return nil, errors.New("no live node available to coordinate")
	}
	n, ok := c.nodes[id]
	if !ok {
		return nil, fmt.Errorf("unknown coordinator %q", id)
	}
	if !n.Alive() {
		return nil, fmt.Errorf("coordinator %q is down — pick a live node", id)
	}
	return c.coord[id], nil
}

// Put coordinates a write through the chosen (or first-live) coordinator.
func (c *Cluster) Put(ctx context.Context, coordID, key, value string, ctxClock vv.VersionVector, n, w int) (coordinator.PutResult, error) {
	co, err := c.resolveCoord(coordID)
	if err != nil {
		return coordinator.PutResult{}, err
	}
	return co.Put(ctx, key, value, ctxClock, n, w), nil
}

// Get coordinates a read.
func (c *Cluster) Get(ctx context.Context, coordID, key string, n, r int) (coordinator.GetResult, error) {
	co, err := c.resolveCoord(coordID)
	if err != nil {
		return coordinator.GetResult{}, err
	}
	return co.Get(ctx, key, n, r), nil
}

// --- chaos controls ---

// Kill drains a node cooperatively, then makes it unreachable on the network.
func (c *Cluster) Kill(id string) error {
	n, ok := c.nodes[id]
	if !ok {
		return fmt.Errorf("unknown node %q", id)
	}
	n.Drain()              // cooperative interruption: finish in-flight, then stop
	c.tr.SetDown(id, true) // network: connection refused
	c.bus.Emit("node", map[string]any{"id": id, "alive": false, "reason": "killed"})
	return nil
}

// Revive restarts a killed node (its data survives).
func (c *Cluster) Revive(id string) error {
	n, ok := c.nodes[id]
	if !ok {
		return fmt.Errorf("unknown node %q", id)
	}
	n.Revive()
	c.tr.SetDown(id, false)
	c.bus.Emit("node", map[string]any{"id": id, "alive": true, "reason": "revived"})
	return nil
}

// Partition splits the cluster into communication groups.
func (c *Cluster) Partition(groups [][]string) error {
	for _, g := range groups {
		for _, id := range g {
			if _, ok := c.nodes[id]; !ok {
				return fmt.Errorf("unknown node %q in partition", id)
			}
		}
	}
	c.tr.Partition(groups)
	c.mu.Lock()
	c.partition = groups
	c.mu.Unlock()
	c.bus.Emit("partition", map[string]any{"groups": groups, "active": true})
	return nil
}

// Heal merges all groups back together.
func (c *Cluster) Heal() {
	c.tr.Heal()
	c.mu.Lock()
	c.partition = nil
	c.mu.Unlock()
	c.bus.Emit("partition", map[string]any{"groups": nil, "active": false})
}

// SetLatency injects a per-hop replication delay (ms).
func (c *Cluster) SetLatency(ms int) {
	if ms < 0 {
		ms = 0
	}
	c.tr.SetLatency(time.Duration(ms) * time.Millisecond)
	c.mu.Lock()
	c.latencyMs = ms
	c.mu.Unlock()
	c.bus.Emit("latency", map[string]any{"ms": ms})
}

// --- security demonstrations (defensive, against Quorum's own cluster) ---

// SpoofResult reports a node-spoofing attempt.
type SpoofResult struct {
	Attacker string `json:"attacker"`
	Target   string `json:"target"`
	Rejected bool   `json:"rejected"`
	Detail   string `json:"detail"`
}

// SpoofNode attempts an inter-node RPC using a forged identity that was never
// minted by this cluster. The transport must reject it with ErrUntrustedNode,
// proving an attacker cannot impersonate a replica to poison a quorum.
func (c *Cluster) SpoofNode(target string) (SpoofResult, error) {
	if _, ok := c.nodes[target]; !ok {
		return SpoofResult{}, fmt.Errorf("unknown target %q", target)
	}
	rogue := security.Identity{NodeID: "rogue-attacker", Sig: "forged-signature-no-secret"}
	_, err := c.tr.Send(context.Background(), rogue, target,
		transport.Request{Op: transport.OpGet, Key: "any"})
	rejected := errors.Is(err, security.ErrUntrustedNode)
	detail := "rejected: " + security.ErrUntrustedNode.Error()
	if err != nil && !rejected {
		detail = "rejected: " + err.Error()
		rejected = true // any failure means the rogue did not get through
	}
	res := SpoofResult{Attacker: "rogue-attacker", Target: target, Rejected: rejected, Detail: detail}
	c.bus.Emit("spoof", res)
	return res, nil
}

// ForgeResult reports a token-forging attempt.
type ForgeResult struct {
	Attacker     string `json:"attacker"`
	Key          string `json:"key"`
	Op           string `json:"op"`
	Rejected     bool   `json:"rejected"`
	Reason       string `json:"reason"`
	TokenPreview string `json:"tokenPreview"`
}

// ForgeToken mints a legitimately-signed narrow capability, then tampers it to
// escalate scope (all keys, read+write) without re-signing — the classic
// privilege-escalation attempt. The authenticator must reject it because the
// attacker cannot produce a valid HMAC over the widened claims.
func (c *Cluster) ForgeToken(key, op string) ForgeResult {
	if key == "" {
		key = "secret"
	}
	if op == "" {
		op = "put"
	}
	// 1. Attacker legitimately holds only a narrow, read-only token.
	legit := c.auth.IssueToken(security.Capability{
		Subject: "attacker", Keys: []string{"public"}, Ops: []string{"get"},
	})
	sig := strings.SplitN(legit, ".", 2)[1]
	// 2. Attacker rewrites the payload to grant themselves everything…
	escalated, _ := json.Marshal(security.Capability{
		Subject: "attacker", Keys: []string{"*"}, Ops: []string{"get", "put"},
	})
	forged := base64.RawURLEncoding.EncodeToString(escalated) + "." + sig
	// 3. …but the recomputed signature over the new claims will not match.
	_, err := c.auth.VerifyToken(forged, key, op, time.Now())
	res := ForgeResult{
		Attacker: "attacker", Key: key, Op: op,
		Rejected: err != nil, TokenPreview: preview(forged),
	}
	if err != nil {
		res.Reason = err.Error()
	} else {
		res.Reason = "ACCEPTED — this should never happen"
	}
	c.bus.Emit("token", res)
	return res
}

func preview(s string) string {
	if len(s) > 48 {
		return s[:48] + "…"
	}
	return s
}

// --- introspection ---

// State is the UI-facing cluster snapshot.
type State struct {
	Nodes     []node.NodeState `json:"nodes"`
	Members   []string         `json:"members"`
	Partition [][]string       `json:"partition"`
	LatencyMs int              `json:"latencyMs"`
}

// State returns the current cluster snapshot.
func (c *Cluster) State() State {
	c.mu.RLock()
	part := c.partition
	lat := c.latencyMs
	c.mu.RUnlock()
	nodes := make([]node.NodeState, 0, len(c.order))
	for _, id := range c.order {
		nodes = append(nodes, c.nodes[id].Snapshot())
	}
	return State{Nodes: nodes, Members: c.ring.Members(), Partition: part, LatencyMs: lat}
}

// Inspect returns a node's stored data for the per-node inspector.
func (c *Cluster) Inspect(id string) (map[string][]kv.VersionedValue, error) {
	n, ok := c.nodes[id]
	if !ok {
		return nil, fmt.Errorf("unknown node %q", id)
	}
	return n.Dump(), nil
}

// NodeIDs returns the ordered node ids.
func (c *Cluster) NodeIDs() []string {
	out := make([]string, len(c.order))
	copy(out, c.order)
	return out
}
