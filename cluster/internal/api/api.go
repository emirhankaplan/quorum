// Package api is the HTTP control plane and the client trust boundary. Every
// key-value request is gated by a capability token before it reaches the
// engine — the enforcement point for The Web Application Hacker's Handbook's
// rule that all client input is hostile until authenticated and authorised.
// It also exposes the chaos and security-demo endpoints and serves the built
// single-page UI.
package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/emirhankaplan/quorum/cluster/internal/cluster"
	"github.com/emirhankaplan/quorum/cluster/internal/stream"
	"github.com/emirhankaplan/quorum/cluster/internal/vv"
)

// Server bundles the cluster, the WebSocket hub, and the static UI directory.
type Server struct {
	cl        *cluster.Cluster
	hub       *stream.Hub
	staticDir string
}

// New builds the API server. staticDir (optional) is a directory of built
// frontend assets to serve at "/".
func New(cl *cluster.Cluster, staticDir string) *Server {
	hub := stream.NewHub(cl.Bus(), func() any { return cl.State() })
	return &Server{cl: cl, hub: hub, staticDir: staticDir}
}

// Routes returns the configured handler.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/token", s.handleToken)
	mux.HandleFunc("/api/put", s.handlePut)
	mux.HandleFunc("/api/get", s.handleGet)
	mux.HandleFunc("/api/inspect", s.handleInspect)
	mux.HandleFunc("/api/chaos/kill", s.handleKill)
	mux.HandleFunc("/api/chaos/revive", s.handleRevive)
	mux.HandleFunc("/api/chaos/partition", s.handlePartition)
	mux.HandleFunc("/api/chaos/heal", s.handleHeal)
	mux.HandleFunc("/api/chaos/latency", s.handleLatency)
	mux.HandleFunc("/api/security/spoof", s.handleSpoof)
	mux.HandleFunc("/api/security/forge-token", s.handleForge)
	mux.HandleFunc("/ws", s.hub.Handle)
	if s.staticDir != "" {
		mux.HandleFunc("/", s.serveSPA)
	}
	return withCORS(mux)
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func clampQuorum(n, q int) (int, int) {
	if n < 1 {
		n = 1
	}
	if q < 1 {
		q = 1
	}
	if q > n {
		q = n
	}
	return n, q
}

// --- key-value endpoints ---

type putReq struct {
	Coordinator string           `json:"coordinator"`
	Key         string           `json:"key"`
	Value       string           `json:"value"`
	N           int              `json:"n"`
	W           int              `json:"w"`
	Context     vv.VersionVector `json:"context"`
	Token       string           `json:"token"`
}

func (s *Server) handlePut(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var req putReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Key == "" {
		writeErr(w, http.StatusBadRequest, "key required")
		return
	}
	// Trust boundary: authorise the capability token for this key + operation.
	if err := s.cl.VerifyToken(req.Token, req.Key, "put"); err != nil {
		writeErr(w, http.StatusForbidden, "token rejected: "+err.Error())
		return
	}
	n, wq := clampQuorum(req.N, req.W)
	res, err := s.cl.Put(r.Context(), req.Coordinator, req.Key, req.Value, req.Context, n, wq)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type getReq struct {
	Coordinator string `json:"coordinator"`
	Key         string `json:"key"`
	N           int    `json:"n"`
	R           int    `json:"r"`
	Token       string `json:"token"`
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var req getReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Key == "" {
		writeErr(w, http.StatusBadRequest, "key required")
		return
	}
	if err := s.cl.VerifyToken(req.Token, req.Key, "get"); err != nil {
		writeErr(w, http.StatusForbidden, "token rejected: "+err.Error())
		return
	}
	n, rq := clampQuorum(req.N, req.R)
	res, err := s.cl.Get(r.Context(), req.Coordinator, req.Key, n, rq)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// --- introspection ---

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.cl.State())
}

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"token": s.cl.DefaultToken()})
}

func (s *Server) handleInspect(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("node")
	dump, err := s.cl.Inspect(id)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dump)
}

// --- chaos endpoints ---

type nodeReq struct {
	Node string `json:"node"`
}

func (s *Server) handleKill(w http.ResponseWriter, r *http.Request) {
	var req nodeReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.cl.Kill(req.Node); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.cl.State())
}

func (s *Server) handleRevive(w http.ResponseWriter, r *http.Request) {
	var req nodeReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.cl.Revive(req.Node); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.cl.State())
}

type partitionReq struct {
	Groups [][]string `json:"groups"`
}

func (s *Server) handlePartition(w http.ResponseWriter, r *http.Request) {
	var req partitionReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.cl.Partition(req.Groups); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.cl.State())
}

func (s *Server) handleHeal(w http.ResponseWriter, r *http.Request) {
	s.cl.Heal()
	writeJSON(w, http.StatusOK, s.cl.State())
}

type latencyReq struct {
	Ms int `json:"ms"`
}

func (s *Server) handleLatency(w http.ResponseWriter, r *http.Request) {
	var req latencyReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	s.cl.SetLatency(req.Ms)
	writeJSON(w, http.StatusOK, s.cl.State())
}

// --- security demonstrations ---

type spoofReq struct {
	Target string `json:"target"`
}

func (s *Server) handleSpoof(w http.ResponseWriter, r *http.Request) {
	var req spoofReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	res, err := s.cl.SpoofNode(req.Target)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type forgeReq struct {
	Key string `json:"key"`
	Op  string `json:"op"`
}

func (s *Server) handleForge(w http.ResponseWriter, r *http.Request) {
	var req forgeReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	writeJSON(w, http.StatusOK, s.cl.ForgeToken(req.Key, req.Op))
}

// --- static SPA serving ---

func (s *Server) serveSPA(w http.ResponseWriter, r *http.Request) {
	clean := filepath.Clean(r.URL.Path)
	full := filepath.Join(s.staticDir, clean)
	// Prevent path traversal outside the static root.
	if !strings.HasPrefix(full, filepath.Clean(s.staticDir)) {
		http.NotFound(w, r)
		return
	}
	if info, err := os.Stat(full); err == nil && !info.IsDir() {
		http.ServeFile(w, r, full)
		return
	}
	// SPA fallback: serve index.html for client-side routes.
	http.ServeFile(w, r, filepath.Join(s.staticDir, "index.html"))
}

// withCORS allows the Vite dev server (different origin) to call the API in
// development. It is permissive by design for a local demo tool.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
