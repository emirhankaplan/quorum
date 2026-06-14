// Command quorum starts the in-process leaderless key-value cluster, the HTTP
// control plane, and the WebSocket stream — the whole demo in a single binary.
//
//	quorum -addr :8080 -nodes n1,n2,n3 -static ../web/dist
package main

import (
	"context"
	"crypto/rand"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/emirhankaplan/quorum/cluster/internal/api"
	"github.com/emirhankaplan/quorum/cluster/internal/cluster"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	nodesFlag := flag.String("nodes", "n1,n2,n3", "comma-separated node ids")
	vnodes := flag.Int("vnodes", 64, "virtual nodes per physical node on the ring")
	workers := flag.Int("workers", 8, "worker-pool size per node")
	static := flag.String("static", "", "directory of built frontend assets to serve (optional)")
	latency := flag.Int("latency", 40, "initial inter-node latency in ms (makes replication visible)")
	flag.Parse()

	ids := splitClean(*nodesFlag)
	if len(ids) == 0 {
		log.Fatal("at least one node id is required")
	}

	cl := cluster.New(cluster.Config{
		NodeIDs: ids,
		VNodes:  *vnodes,
		Workers: *workers,
		Secret:  clusterSecret(),
	})
	cl.SetLatency(*latency)

	srv := api.New(cl, *static)
	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("quorum: listening on %s | nodes=%v | static=%q", *addr, ids, *static)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("quorum: server error: %v", err)
		}
	}()

	// Graceful shutdown on Ctrl-C / SIGTERM.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("quorum: shutting down…")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
}

func splitClean(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// clusterSecret returns QUORUM_SECRET if set, otherwise a fresh random secret
// for this run. A stable secret across restarts only matters if you persist
// and re-present tokens/identities, which the demo does not.
func clusterSecret() []byte {
	if v := os.Getenv("QUORUM_SECRET"); v != "" {
		return []byte(v)
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("quorum: cannot generate secret: %v", err)
	}
	return b
}
