# Guided scenarios

Seven short walkthroughs. Start the app (`make run` or `docker compose up`, then open
<http://localhost:8080>) and follow along. Each one is a self-contained "aha".

---

## 1. Strong consistency — `R + W > N`

1. Click the **strong (R+W>N)** scenario chip (sets `N=3 W=2 R=2`).
2. Type `cart = apples`, **PUT**.
3. **GET** `cart`.

**Observe:** the consistency badge is green (`R + W = 4 > N = 3`); the read returns exactly one
value. Read and write quorums overlap, so the read is guaranteed to see the latest write.

---

## 2. CAP partition → siblings  (the headline demo)

The one-click version is the **▶ auto CAP partition demo** button. By hand:

1. **sloppy (W=1)** chip (so each side can accept a write alone).
2. In **chaos**, select `n1` for side A and click **apply split** → `{n1} | {n2,n3}`.
3. **PUT** `cart = from-side-A` with **write via `n1`**.
4. **PUT** `cart = from-side-B` with **write via `n2`**.
5. Click **heal**.
6. **GET** `cart` (R=2).

**Observe:** a red **CONFLICT** flash on the topology and the **Sibling Merge** panel showing *both*
`from-side-A` (`n1:1`) and `from-side-B` (`n2:1`). Two concurrent writes, neither version vector
descending the other — so both were kept instead of one being silently lost.

---

## 3. Resolve the conflict — convergence

Continuing from scenario 2:

1. In the **Sibling Merge** panel, click a sibling (or edit the merged value), then
   **resolve & write back**.
2. The app re-reads automatically.

**Observe:** the conflict clears and the read returns a single value. The resolution was written with
a version vector that **descends from both siblings** (the merged clock `{n1:1, n2:1}` plus the
coordinator's bump), so it supersedes them everywhere.

---

## 4. Sloppy quorum — availability over consistency

1. **sloppy (W=1, R=1)**.
2. **PUT** then **GET** a key.

**Observe:** the badge is amber (`R + W ≤ N`). Writes and reads succeed with a single replica, so the
cluster stays available under failure — at the cost of possibly reading stale data or creating
siblings (scenario 2).

---

## 5. Kill a node — drain & availability

1. Reset to **strong (N=3 W=2 R=2)** and PUT a key so all replicas hold it.
2. In **chaos**, **kill** one node.
3. **GET** the key (R=2).

**Observe:** the killed node greys out (it drained cooperatively — no crash), the ring re-routes, and
the read still succeeds because two live replicas satisfy `R=2`. **Revive** it: its data is intact
(a kill is a process stop, not a disk wipe), and the **node inspector** confirms it.

---

## 6. Spoof a node — mutual authentication holds

1. **Security** panel → choose a target → **inject rogue node**.

**Observe:** a green **REJECTED — untrusted node: identity signature invalid**. An attacker who does
not hold the cluster secret cannot forge a node identity, so it cannot join replication or poison a
quorum. See [THREAT_MODEL.md](../THREAT_MODEL.md).

---

## 7. Forge a token — capability enforcement holds

1. **Security** panel → **escalate token → PUT secret**.

**Observe:** a green **REJECTED — token signature invalid (tampered)**. The attacker rewrote a narrow,
read-only capability to grant themselves everything, but cannot produce a valid HMAC over the widened
claims, so the API refuses it before the engine is touched.
