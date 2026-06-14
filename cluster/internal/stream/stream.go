// Package stream bridges the engine's event bus to the browser over WebSocket.
// On connect it sends a full state snapshot, then forwards every event the
// coordinator/nodes/chaos controls emit, plus a periodic state heartbeat so the
// live ops meters keep ticking even during quiet moments. This is the "derived,
// real-time view" idea from Designing Data-Intensive Applications (Ch. 11):
// an immutable stream of events fanned out to build a live read model — here,
// the cluster visualisation in the UI.
package stream

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/emirhankaplan/quorum/cluster/internal/event"
)

// Hub upgrades HTTP connections to WebSockets and pumps events to each client.
type Hub struct {
	bus      *event.Bus
	stateFn  func() any
	upgrader websocket.Upgrader
}

// NewHub builds a hub over the given bus. stateFn returns the current cluster
// snapshot for the initial message and heartbeats.
func NewHub(bus *event.Bus, stateFn func() any) *Hub {
	return &Hub{
		bus:     bus,
		stateFn: stateFn,
		upgrader: websocket.Upgrader{
			// Dev convenience: the Vite dev server runs on a different origin.
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

type envelope struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// Handle serves one WebSocket client until it disconnects.
func (h *Hub) Handle(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	id, ch := h.bus.Subscribe()
	defer h.bus.Unsubscribe(id)

	// Initial full snapshot so a fresh client renders immediately.
	if err := conn.WriteJSON(envelope{Type: "state", Data: h.stateFn()}); err != nil {
		return
	}

	// A reader goroutine exists only to notice the client going away (and to
	// drain control frames). All writes stay on this goroutine.
	closed := make(chan struct{})
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				close(closed)
				return
			}
		}
	}()

	heartbeat := time.NewTicker(time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.WriteJSON(envelope{Type: "event", Data: ev}); err != nil {
				return
			}
		case <-heartbeat.C:
			if err := conn.WriteJSON(envelope{Type: "state", Data: h.stateFn()}); err != nil {
				return
			}
		case <-closed:
			return
		}
	}
}
