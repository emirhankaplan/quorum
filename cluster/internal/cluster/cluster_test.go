package cluster

import (
	"context"
	"testing"

	"github.com/emirhankaplan/quorum/cluster/internal/vv"
)

func newTestCluster(t *testing.T) *Cluster {
	t.Helper()
	return New(Config{
		NodeIDs: []string{"n1", "n2", "n3"},
		VNodes:  64,
		Workers: 4,
		Secret:  []byte("test-secret"),
	})
}

func TestQuorumWriteThenRead(t *testing.T) {
	c := newTestCluster(t)
	ctx := context.Background()

	pr, err := c.Put(ctx, "n1", "cart", "apples", nil, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !pr.OK || pr.Acks < 2 {
		t.Fatalf("write should meet W=2, got acks=%d ok=%v", pr.Acks, pr.OK)
	}

	gr, err := c.Get(ctx, "n2", "cart", 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !gr.OK || gr.Conflict || len(gr.Values) != 1 || gr.Values[0].Value != "apples" {
		t.Fatalf("read should return single value 'apples', got %+v", gr)
	}
}

func TestWriteFailsBelowW(t *testing.T) {
	c := newTestCluster(t)
	ctx := context.Background()
	// Kill two of three replicas: only one can ack.
	_ = c.Kill("n2")
	_ = c.Kill("n3")
	pr, err := c.Put(ctx, "n1", "k", "v", nil, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if pr.OK || pr.Acks != 1 {
		t.Fatalf("with two replicas down and W=2 the write must fail; got acks=%d ok=%v", pr.Acks, pr.OK)
	}
}

func TestAvailableUnderSingleFailure(t *testing.T) {
	c := newTestCluster(t)
	ctx := context.Background()
	_ = c.Kill("n3")
	pr, err := c.Put(ctx, "n1", "k", "v", nil, 3, 2)
	if err != nil || !pr.OK {
		t.Fatalf("W=2 must still succeed with one node down: %+v err=%v", pr, err)
	}
	gr, err := c.Get(ctx, "n1", "k", 3, 2)
	if err != nil || !gr.OK || len(gr.Values) != 1 || gr.Values[0].Value != "v" {
		t.Fatalf("R=2 must still succeed with one node down: %+v err=%v", gr, err)
	}
}

func TestPartitionCreatesSiblings(t *testing.T) {
	c := newTestCluster(t)
	ctx := context.Background()

	// Split into {n1} | {n2,n3} and let each side accept a sloppy (W=1) write.
	if err := c.Partition([][]string{{"n1"}, {"n2", "n3"}}); err != nil {
		t.Fatal(err)
	}
	if pr, _ := c.Put(ctx, "n1", "cart", "from-side-1", nil, 3, 1); !pr.OK {
		t.Fatalf("side-1 write should succeed at W=1: %+v", pr)
	}
	if pr, _ := c.Put(ctx, "n2", "cart", "from-side-2", nil, 3, 1); !pr.OK {
		t.Fatalf("side-2 write should succeed at W=1: %+v", pr)
	}
	c.Heal()

	gr, err := c.Get(ctx, "n1", "cart", 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !gr.Conflict || len(gr.Values) != 2 {
		t.Fatalf("two concurrent writes must surface as siblings, got %d values conflict=%v", len(gr.Values), gr.Conflict)
	}
	got := map[string]bool{}
	for _, v := range gr.Values {
		got[v.Value] = true
	}
	if !got["from-side-1"] || !got["from-side-2"] {
		t.Fatalf("both sibling values must survive, got %v", got)
	}
}

func TestSiblingResolutionConverges(t *testing.T) {
	c := newTestCluster(t)
	ctx := context.Background()
	_ = c.Partition([][]string{{"n1"}, {"n2", "n3"}})
	_, _ = c.Put(ctx, "n1", "cart", "A", nil, 3, 1)
	_, _ = c.Put(ctx, "n2", "cart", "B", nil, 3, 1)
	c.Heal()

	// Read the conflict, merge the sibling clocks, and write the resolution with
	// that causal context — it must descend from both siblings and converge.
	gr, _ := c.Get(ctx, "n1", "cart", 3, 2)
	if !gr.Conflict {
		t.Fatalf("expected a conflict to resolve")
	}
	merged := vv.New()
	for _, v := range gr.Values {
		merged = vv.Merge(merged, v.Clock)
	}
	if pr, _ := c.Put(ctx, "n1", "cart", "A+B-merged", merged, 3, 3); !pr.OK {
		t.Fatalf("resolution write should succeed: %+v", pr)
	}
	final, _ := c.Get(ctx, "n1", "cart", 3, 3)
	if final.Conflict || len(final.Values) != 1 || final.Values[0].Value != "A+B-merged" {
		t.Fatalf("cluster must converge to the merged value, got %+v", final)
	}
}

func TestReadRepair(t *testing.T) {
	c := newTestCluster(t)
	ctx := context.Background()
	// Write reaches only {n2,n3}; n1 is partitioned away and misses it.
	_ = c.Partition([][]string{{"n1"}, {"n2", "n3"}})
	if pr, _ := c.Put(ctx, "n2", "k", "v1", nil, 3, 1); !pr.OK {
		t.Fatalf("write to majority side should succeed: %+v", pr)
	}
	c.Heal()

	// A quorum read coordinated by the stale node must repair it.
	gr, _ := c.Get(ctx, "n1", "k", 3, 2)
	repairedN1 := false
	for _, r := range gr.ReadRepaired {
		if r == "n1" {
			repairedN1 = true
		}
	}
	if !repairedN1 {
		t.Fatalf("expected n1 to be read-repaired, got %v", gr.ReadRepaired)
	}
	dump, _ := c.Inspect("n1")
	if vals, ok := dump["k"]; !ok || len(vals) != 1 || vals[0].Value != "v1" {
		t.Fatalf("n1 should now hold the repaired value, got %+v", dump["k"])
	}
}

func TestKillDrainsAndRevivePreservesData(t *testing.T) {
	c := newTestCluster(t)
	ctx := context.Background()
	_, _ = c.Put(ctx, "n1", "k", "v", nil, 3, 3)

	if err := c.Kill("n2"); err != nil {
		t.Fatal(err)
	}
	// A killed node cannot coordinate.
	if _, err := c.Put(ctx, "n2", "k", "v2", nil, 3, 1); err == nil {
		t.Fatal("a dead node must not coordinate writes")
	}
	// Its data must survive the kill and be intact after revive.
	if err := c.Revive("n2"); err != nil {
		t.Fatal(err)
	}
	dump, _ := c.Inspect("n2")
	if vals, ok := dump["k"]; !ok || len(vals) != 1 || vals[0].Value != "v" {
		t.Fatalf("revived node should retain its data, got %+v", dump["k"])
	}
}

func TestSpoofNodeRejected(t *testing.T) {
	c := newTestCluster(t)
	res, err := c.SpoofNode("n1")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Rejected {
		t.Fatalf("a spoofed node must be rejected, got %+v", res)
	}
}

func TestForgeTokenRejected(t *testing.T) {
	c := newTestCluster(t)
	res := c.ForgeToken("secret", "put")
	if !res.Rejected {
		t.Fatalf("a forged/escalated token must be rejected, got %+v", res)
	}
}

func TestDefaultTokenIsAccepted(t *testing.T) {
	c := newTestCluster(t)
	if err := c.VerifyToken(c.DefaultToken(), "cart", "put"); err != nil {
		t.Fatalf("the UI's default token must be valid: %v", err)
	}
}
