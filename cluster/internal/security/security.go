// Package security implements Quorum's trust boundary.
//
// This is where The Web Application Hacker's Handbook stops being a reading list
// and becomes load-bearing code. Its central thesis — "all input from a client
// (or peer) is hostile until proven otherwise" — is enforced at two seams:
//
//  1. Node identity (mutual authentication). Every legitimate node holds an
//     identity signed with a cluster secret. A peer that cannot present a valid
//     signature is refused, so an attacker cannot spoof a replica, join the
//     ring, or subvert a quorum by impersonating nodes it does not control.
//     In this in-process MVP the signature stands in for an mTLS client cert;
//     the same Verify() gate plugs straight into a real TLS transport.
//
//  2. Capability tokens (access control). Clients of the key-value API present
//     a signed, scoped capability instead of an ambient session. The server
//     re-derives the signature and rejects any token that is tampered, expired,
//     or used outside its granted keys/operations — exactly the "break the
//     stateless trust" discipline the book preaches for stateful actions over
//     HTTP.
//
// All signing is HMAC-SHA256 over a canonical JSON encoding; the secret never
// leaves the process. Nothing here attacks anything external — it only proves
// Quorum's own defences hold.
package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

// Authenticator holds the cluster secret and mints/verifies identities + tokens.
type Authenticator struct {
	secret []byte
}

// New returns an Authenticator bound to the given cluster secret.
func New(secret []byte) *Authenticator {
	cp := make([]byte, len(secret))
	copy(cp, secret)
	return &Authenticator{secret: cp}
}

func (a *Authenticator) sign(msg []byte) string {
	m := hmac.New(sha256.New, a.secret)
	m.Write(msg)
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (a *Authenticator) equalSig(msg []byte, sig string) bool {
	want := a.sign(msg)
	// Constant-time compare to avoid signature-timing oracles (WAHH Ch. 7).
	return subtle.ConstantTimeCompare([]byte(want), []byte(sig)) == 1
}

// ---------------------------------------------------------------------------
// Node identity (peer authentication)
// ---------------------------------------------------------------------------

// Identity is a node's authenticated credential. It travels with every
// inter-node message so the receiver can confirm the sender is a real cluster
// member and not a spoofed peer.
type Identity struct {
	NodeID string `json:"nodeId"`
	Sig    string `json:"sig"`
}

// Issue mints an identity for a legitimate node.
func (a *Authenticator) Issue(nodeID string) Identity {
	return Identity{NodeID: nodeID, Sig: a.sign([]byte("node:" + nodeID))}
}

// VerifyIdentity reports whether an identity was minted by this cluster.
// A forged or unsigned identity (e.g. an attacker's "rogue" node) fails here.
func (a *Authenticator) VerifyIdentity(id Identity) bool {
	return a.equalSig([]byte("node:"+id.NodeID), id.Sig)
}

// ErrUntrustedNode is returned when a peer fails identity verification.
var ErrUntrustedNode = errors.New("untrusted node: identity signature invalid")

// ---------------------------------------------------------------------------
// Capability tokens (client access control)
// ---------------------------------------------------------------------------

// Capability is the scoped grant inside a token. Keys may contain "*" to mean
// all keys; Ops contains "get" and/or "put".
type Capability struct {
	Subject string   `json:"sub"`
	Keys    []string `json:"keys"`
	Ops     []string `json:"ops"`
	Expires int64    `json:"exp"` // unix seconds; 0 = never
}

func canonical(c Capability) []byte {
	// Canonicalise so the signature is stable regardless of slice order.
	keys := append([]string(nil), c.Keys...)
	ops := append([]string(nil), c.Ops...)
	sort.Strings(keys)
	sort.Strings(ops)
	c.Keys, c.Ops = keys, ops
	b, _ := json.Marshal(c)
	return b
}

// IssueToken returns a signed capability token of the form "<payload>.<sig>",
// where payload is base64url(JSON(capability)).
func (a *Authenticator) IssueToken(c Capability) string {
	payload := base64.RawURLEncoding.EncodeToString(canonical(c))
	return payload + "." + a.sign(canonical(c))
}

// Token-verification failure reasons (surfaced to the UI's forge-token demo).
var (
	ErrMalformedToken = errors.New("malformed token")
	ErrBadSignature   = errors.New("token signature invalid (tampered)")
	ErrTokenExpired   = errors.New("token expired")
	ErrScopeKey       = errors.New("token not scoped for this key")
	ErrScopeOp        = errors.New("token not scoped for this operation")
)

// VerifyToken parses, authenticates, and authorises a token for (key, op).
// now is injectable for deterministic tests.
func (a *Authenticator) VerifyToken(token, key, op string, now time.Time) (Capability, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return Capability{}, ErrMalformedToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Capability{}, ErrMalformedToken
	}
	var c Capability
	if err := json.Unmarshal(raw, &c); err != nil {
		return Capability{}, ErrMalformedToken
	}
	// Re-derive the signature from the *decoded* capability: if an attacker
	// edited the payload (e.g. widened Keys), the recomputed signature will no
	// longer match the one they could not forge.
	if !a.equalSig(canonical(c), parts[1]) {
		return Capability{}, ErrBadSignature
	}
	if c.Expires != 0 && now.Unix() > c.Expires {
		return Capability{}, ErrTokenExpired
	}
	if !scopeContains(c.Ops, op) {
		return Capability{}, ErrScopeOp
	}
	if !scopeContains(c.Keys, key) {
		return Capability{}, ErrScopeKey
	}
	return c, nil
}

func scopeContains(scope []string, want string) bool {
	for _, s := range scope {
		if s == "*" || s == want {
			return true
		}
	}
	return false
}
