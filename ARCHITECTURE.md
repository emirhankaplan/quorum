# Architecture

Quorum is three layers behind one trust boundary, compiled into a single Go binary that also
serves the React UI.

```
                          ┌─────────────────────────────────────────────┐
                          │                  BROWSER (web/)              │
                          │  Topology · N/R/W controls · Sibling merge   │
                          │  Chaos · Security · Event log · Inspector    │
                          └───────────────┬───────────────▲─────────────┘
                            POST /api/*    │               │  /ws (live)
                            (token-checked) │               │
        ┌───────────────────────────────────▼───────────────┴──────────────────────┐
        │                          CONTROL + STREAM PLANE (internal/api, internal/stream)
        │   verify capability token  ·  chaos endpoints  ·  WebSocket hub  ·  static UI
        └───────────────────────────────────┬───────────────▲──────────────────────┘
                                              │               │ events
        ┌─────────────────────────────────────▼───────────────┴──────────────────────┐
        │                          CLUSTER ENGINE (internal/cluster)                   │
        │                                                                              │
        │   coordinator ──ring.PreferenceList(key, N)──▶ [ n1, n2, n3 ]                │
        │        │                                                                     │
        │        ├── fan out via Transport (auth + partition + latency) ───────────┐   │
        │        │                                                                  ▼   │
        │   ┌────▼─────┐      ┌──────────┐      ┌──────────┐         each node:         │
        │   │  node n1 │      │  node n2 │      │  node n3 │   worker pool + futures    │
        │   │ store+VV │      │ store+VV │      │ store+VV │   atomic ops counter       │
        │   └──────────┘      └──────────┘      └──────────┘   cooperative drain        │
        │        ▲ wait for W acks (write) / R responses + read-repair (read)           │
        └──────────────────────────────────────────────────────────────────────────────┘
```

## Packages

| Package | Responsibility | Book it embodies |
| --- | --- | --- |
| `internal/vv` | Version vectors: `Compare` (`Equal`/`Before`/`After`/`Concurrent`), `Descends`, `Merge`. Pure and exhaustively tested. | DDIA Ch. 5 |
| `internal/kv` | Versioned values + `Reconcile` (causal frontier → sibling set). | DDIA Ch. 5 |
| `internal/ring` | Consistent-hash ring with virtual nodes; `PreferenceList(key, N)`. | DDIA Ch. 6 |
| `internal/node` | One replica: versioned store, worker pool, atomic counters, cooperative drain, `NextClock`. | C++ Concurrency Ch. 9 |
| `internal/coordinator` | N/R/W quorum fan-out, W-ack writes, R-quorum reads, read-repair. | DDIA Ch. 5 + C++ futures |
| `internal/transport` | `Transport` interface + `InProcess` impl with auth, partition matrix, latency. | — (the swappable seam) |
| `internal/security` | Node identities + capability tokens, HMAC-SHA256, constant-time compare. | WAHH |
| `internal/event` | In-process pub/sub bus fanned out to the browser. | DDIA Ch. 11 (derived views) |
| `internal/cluster` | Wires everything; exposes PUT/GET, chaos, security demos, state. | — |
| `internal/api` | HTTP control plane + the client trust boundary + static SPA serving. | WAHH |
| `internal/stream` | WebSocket hub: initial snapshot + event forwarding + heartbeat. | — |

## Key flows

### Write (`PUT`)

1. The API verifies the request's capability token for `(key, "put")`.
2. The coordinator derives a new version vector with `node.NextClock(context)` — it clones the
   client's causal context and bumps its own counter, so successive writes through the same
   coordinator supersede each other while writes from different coordinators stay concurrent.
3. It looks up the key's `N` preference-list replicas on the ring and fans out `OpPut` over the
   transport, each call on its own goroutine.
4. It gathers acks; the write is **OK** once `W` replicas acknowledge. (We wait for all `N`
   outcomes — bounded, because the transport fails fast on dead/partitioned peers — so the UI can
   show every replica's fate.)

### Read (`GET`)

1. Token check for `(key, "get")`.
2. Fan out `OpGet` to `N` replicas; return once `R` respond.
3. **Reconcile** the union of returned versioned values into the causal frontier: one value if there
   is no conflict, several siblings if writes were concurrent.
4. **Read-repair:** push the reconciled frontier to any responding replica whose set was stale, so
   the next read finds the cluster converged.

### Chaos

Chaos lives entirely at the transport, so node code is oblivious:

- **Kill** = `node.Drain()` (cooperative interruption: finish in-flight, then stop) + the transport
  marks the node unreachable (`ErrNodeDown`).
- **Partition** = the transport assigns nodes to groups and drops cross-group messages
  (`ErrPartitioned`).
- **Latency** = a per-hop delay applied before delivery.

### The trust boundary

- Every inter-node `Send` carries the sender's signed `Identity`; the transport rejects any peer
  whose signature was not minted by the cluster secret (`ErrUntrustedNode`).
- Every client key-value request must present a capability token scoped to keys + operations; the
  API rejects tampered, expired, or out-of-scope tokens before the engine sees them.

See **[THREAT_MODEL.md](THREAT_MODEL.md)** for the attacker model and how the in-process
authentication maps onto real mutual TLS.
