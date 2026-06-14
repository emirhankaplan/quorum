// Package ring implements a consistent-hashing ring with virtual nodes and the
// preference-list lookup used to place each key on N replicas.
//
// This is the partitioning chapter of Designing Data-Intensive Applications
// (Ch. 6) made concrete: keys are hashed onto a 32-bit ring, each physical node
// owns many virtual points on that ring (so load is spread evenly and adding or
// removing a node only re-homes a small slice of keys), and a key's preference
// list is the next N distinct physical nodes walking clockwise from the key's
// position.
package ring

import (
	"hash/crc32"
	"sort"
	"sync"
)

// Ring is a thread-safe consistent-hash ring.
type Ring struct {
	mu       sync.RWMutex
	vnodes   int               // virtual points per physical node
	points   []uint32          // sorted ring positions
	owner    map[uint32]string // ring position -> physical node id
	members  map[string]bool   // set of physical nodes
}

// New returns a ring with the given number of virtual nodes per physical node.
func New(vnodes int) *Ring {
	if vnodes < 1 {
		vnodes = 1
	}
	return &Ring{
		vnodes:  vnodes,
		owner:   make(map[uint32]string),
		members: make(map[string]bool),
	}
}

func hash(s string) uint32 { return crc32.ChecksumIEEE([]byte(s)) }

func vpoint(node string, i int) uint32 {
	return hash(node + "#" + itoa(i))
}

// itoa avoids importing strconv for a hot path that only needs small ints.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}

// Add inserts a physical node (idempotent) and rebuilds the sorted index.
func (r *Ring) Add(node string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.members[node] {
		return
	}
	r.members[node] = true
	for i := 0; i < r.vnodes; i++ {
		r.owner[vpoint(node, i)] = node
	}
	r.rebuild()
}

// Remove deletes a physical node and its virtual points.
func (r *Ring) Remove(node string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.members[node] {
		return
	}
	delete(r.members, node)
	for i := 0; i < r.vnodes; i++ {
		delete(r.owner, vpoint(node, i))
	}
	r.rebuild()
}

func (r *Ring) rebuild() {
	r.points = r.points[:0]
	for p := range r.owner {
		r.points = append(r.points, p)
	}
	sort.Slice(r.points, func(i, j int) bool { return r.points[i] < r.points[j] })
}

// PreferenceList returns the first n distinct physical nodes encountered when
// walking clockwise from key's hash position. If fewer than n nodes exist, all
// of them are returned. The first element is the natural coordinator.
func (r *Ring) PreferenceList(key string, n int) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.points) == 0 {
		return nil
	}
	if n > len(r.members) {
		n = len(r.members)
	}
	h := hash(key)
	// Binary search for the first ring point >= h.
	start := sort.Search(len(r.points), func(i int) bool { return r.points[i] >= h })
	out := make([]string, 0, n)
	seen := make(map[string]bool, n)
	for i := 0; i < len(r.points) && len(out) < n; i++ {
		idx := (start + i) % len(r.points)
		node := r.owner[r.points[idx]]
		if !seen[node] {
			seen[node] = true
			out = append(out, node)
		}
	}
	return out
}

// Members returns the current physical node set (unordered copy).
func (r *Ring) Members() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.members))
	for m := range r.members {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}
