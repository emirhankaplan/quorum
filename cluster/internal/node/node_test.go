package node

import (
	"sync"
	"testing"
	"time"

	"github.com/emirhankaplan/quorum/cluster/internal/event"
	"github.com/emirhankaplan/quorum/cluster/internal/kv"
	"github.com/emirhankaplan/quorum/cluster/internal/security"
	"github.com/emirhankaplan/quorum/cluster/internal/transport"
	"github.com/emirhankaplan/quorum/cluster/internal/vv"
)

func testNode() *Node {
	auth := security.New([]byte("s"))
	n := New("n1", auth, transport.NewInProcess(auth), event.NewBus(), 4)
	n.Start()
	return n
}

// Regression test for the NextClock race: concurrent callers must never receive
// the same counter, or two distinct writes would collide into one version vector
// and silently lose data in Reconcile.
func TestNextClockConcurrentlyUnique(t *testing.T) {
	n := testNode()
	base := vv.VersionVector{"n1": 10}
	const goroutines, perG = 16, 50

	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := make(map[uint64]int)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < perG; k++ {
				c := n.NextClock(base)["n1"]
				mu.Lock()
				seen[c]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(seen) != goroutines*perG {
		t.Fatalf("expected %d unique counters, got %d (a collision means a lost write)", goroutines*perG, len(seen))
	}
	for c, cnt := range seen {
		if cnt != 1 {
			t.Fatalf("counter %d was issued %d times — NextClock is not collision-free", c, cnt)
		}
	}
}

// Regression test for the pool deadlock / lost-waiter bugs: hammering Submit
// while Shutdown races must never deadlock, panic, or leave a Future unresolved.
func TestWorkerPoolSubmitDuringShutdown(t *testing.T) {
	p := NewWorkerPool(4, 64)
	const n = 500

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f := Submit(p, func() int { return 1 })
			_ = f.Get() // must always resolve
		}()
	}
	go p.Shutdown() // race the drain against the submits

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a Submit Future never resolved — deadlock or lost job on shutdown")
	}
}

// Drain and Revive must be safe to call concurrently (serialised internally) and
// preserve stored data across a kill.
func TestDrainReviveConcurrent(t *testing.T) {
	n := testNode()
	n.applyPut("k", kv.VersionedValue{Value: "v", Clock: vv.VersionVector{"n1": 1}})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); n.Drain() }()
		go func() { defer wg.Done(); n.Revive() }()
	}
	wg.Wait()
	n.Revive() // settle to a known-alive state

	if !n.Alive() {
		t.Fatal("node should be alive after final Revive")
	}
	if got := n.Dump()["k"]; len(got) != 1 || got[0].Value != "v" {
		t.Fatalf("data must survive drain/revive churn, got %+v", got)
	}
}
