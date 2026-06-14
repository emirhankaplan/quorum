# Threat Model

Quorum's security pillar is **defensive**. The point is not to attack anything — it is to show that
a distributed system has a trust boundary and that Quorum's holds. Every "attack" in the UI is fired
**at Quorum's own bundled cluster**; there is no capability to target an external host.

This is The Web Application Hacker's Handbook's central thesis applied to a data system: **all input
from a client — or a peer — is hostile until it is authenticated and authorised.**

## Scope

| In scope | Out of scope (non-goals) |
| --- | --- |
| The client → API boundary (capability tokens) | Attacking any host other than the bundled cluster |
| The node ↔ node boundary (identity authentication) | Network-layer encryption in the in-process MVP (see "MVP → production") |
| Preventing silent data loss / unauthorised writes | Persistence, multi-tenant isolation, production secret management |

## Assets we protect

1. **Data integrity** — no write is accepted from an unauthenticated client or an unauthorised scope.
2. **Quorum correctness** — a participant in replication must be a genuine cluster member, so an
   attacker cannot poison a quorum or forge acks by impersonating a replica.
3. **No silent data loss** — concurrent writes are preserved as siblings (a *correctness* property,
   reinforced by the security boundary that keeps illegitimate writes out in the first place).

## Trust boundaries & controls

### 1. Client → API  (capability tokens)

Every `PUT`/`GET` must present a signed **capability token** scoped to specific keys and operations.

- **Signature:** `HMAC-SHA256(secret, canonical(claims))`, verified with a **constant-time** compare
  (`crypto/subtle`) to avoid signature-timing oracles (WAHH Ch. 7).
- **Authorisation:** the token's `keys` and `ops` scopes are enforced per request; `exp` is checked.
- Implemented in `internal/security` (`IssueToken` / `VerifyToken`) and enforced in `internal/api`
  *before* the request reaches the engine.

| Threat | Mitigation | Demo |
| --- | --- | --- |
| **Spoofing** — no/empty token | Request rejected `403`; `ErrMalformedToken` | PUT with no token |
| **Tampering** — edit claims to widen scope | Recomputed HMAC over the new claims fails → `ErrBadSignature` | Security panel → *Forge token* |
| **Elevation of privilege** — use a token outside its scope | `ErrScopeKey` / `ErrScopeOp` | Token scoped to `get` used for `put` |
| **Expired credential** | `ErrTokenExpired` | Token with past `exp` |

### 2. Node ↔ Node  (mutual identity authentication)

Every inter-node message carries the sender's signed `Identity`. The transport verifies it before
delivery; a peer whose signature was not minted by the cluster secret is refused with
`ErrUntrustedNode`.

| Threat | Mitigation | Demo |
| --- | --- | --- |
| **Spoofing a replica** — a rogue node joins to inject/replicate data or forge acks | `Authenticator.VerifyIdentity` rejects any unsigned/forged identity at `Transport.Send` | Security panel → *Spoof a node* |
| **Quorum poisoning** via impersonation | Same control — a non-member cannot participate in replication at all | (consequence of the above) |

## Attacker model

We assume an attacker who can:

- send arbitrary HTTP requests to the client API (craft/replay/tamper tokens), and
- attempt to introduce a rogue node that speaks the inter-node protocol.

We assume the attacker **does not** know the cluster secret. The secret is process-local
(`QUORUM_SECRET` or a per-run random value) and never leaves the server.

## MVP → production

In this in-process demo, both boundaries are enforced with **HMAC signatures**:

- The node `Identity` signature stands in for a **mutual-TLS client certificate**. The exact same
  `VerifyIdentity` gate sits at `Transport.Send`, so swapping `InProcess` for a real **gRPC + mTLS**
  transport moves the check onto the TLS handshake without changing the node, coordinator, or ring
  code (this is the whole reason `Transport` is an interface).
- Capability tokens are already a production-shaped pattern (signed, scoped, expiring) — they would
  graduate to asymmetric signing (e.g. Ed25519) with a rotating key and a revocation list.

## Known limitations (honest list)

- **No replay protection on node identities** in the MVP (identities are static, not nonce-bound) —
  acceptable because the in-process transport has no eavesdropping surface; real mTLS solves this at
  the channel layer.
- **No token revocation list** — tokens rely on short `exp` instead.
- **No rate limiting / request quotas** — out of scope for a local demo.
- **Permissive CORS** in dev so the Vite origin can call the API — would be locked down in prod.
