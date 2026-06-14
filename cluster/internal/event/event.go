// Package event is a tiny in-process pub/sub bus used to narrate live cluster
// activity to the browser. The coordinator, nodes, and chaos controls Emit()
// events; the WebSocket hub Subscribe()s and forwards them. Keeping it in its
// own leaf package avoids an import cycle between the engine and the stream
// layer.
package event

import (
	"sync"
	"sync/atomic"
)

// Event is one narrated thing that happened in the cluster.
type Event struct {
	Seq  uint64 `json:"seq"`
	Type string `json:"type"` // write | read | replicate | ack | repair | conflict | node | partition | spoof | token | ops
	Data any    `json:"data"`
}

// Emitter is the write side of the bus, injected into the engine packages.
type Emitter interface {
	Emit(typ string, data any)
}

// Bus is a fan-out broadcaster. Slow subscribers drop events rather than
// blocking the engine (liveness over completeness for a visual stream).
type Bus struct {
	mu   sync.RWMutex
	subs map[int]chan Event
	next int
	seq  atomic.Uint64
}

// NewBus creates an empty bus.
func NewBus() *Bus { return &Bus{subs: make(map[int]chan Event)} }

// Emit broadcasts an event to every subscriber.
func (b *Bus) Emit(typ string, data any) {
	ev := Event{Seq: b.seq.Add(1), Type: typ, Data: data}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs {
		select {
		case ch <- ev:
		default: // subscriber is behind; drop to protect the engine
		}
	}
}

// Subscribe registers a new listener and returns its id and channel.
func (b *Bus) Subscribe() (int, <-chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.next
	b.next++
	ch := make(chan Event, 256)
	b.subs[id] = ch
	return id, ch
}

// Unsubscribe removes a listener and closes its channel.
func (b *Bus) Unsubscribe(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ch, ok := b.subs[id]; ok {
		delete(b.subs, id)
		close(ch)
	}
}
