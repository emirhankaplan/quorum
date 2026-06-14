package security

import (
	"strings"
	"testing"
	"time"
)

func TestNodeIdentityRoundTrip(t *testing.T) {
	a := New([]byte("cluster-secret"))
	id := a.Issue("n1")
	if !a.VerifyIdentity(id) {
		t.Fatal("legit identity should verify")
	}
}

func TestSpoofedNodeRejected(t *testing.T) {
	a := New([]byte("cluster-secret"))
	// An attacker who does not know the cluster secret cannot mint a valid sig.
	attacker := New([]byte("guessed-secret"))
	rogue := attacker.Issue("rogue")
	if a.VerifyIdentity(rogue) {
		t.Fatal("rogue node forged with the wrong secret must be rejected")
	}
	// A blank/garbage signature is also rejected.
	if a.VerifyIdentity(Identity{NodeID: "rogue", Sig: "AAAA"}) {
		t.Fatal("garbage identity must be rejected")
	}
}

func TestCapabilityTokenHappyPath(t *testing.T) {
	a := New([]byte("s"))
	tok := a.IssueToken(Capability{Subject: "ui", Keys: []string{"*"}, Ops: []string{"get", "put"}})
	if _, err := a.VerifyToken(tok, "cart", "put", time.Unix(100, 0)); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
}

func TestTamperedTokenRejected(t *testing.T) {
	a := New([]byte("s"))
	// Attacker wants the wide scope but only holds the narrow token's signature.
	// Splicing the wide payload onto the narrow signature must fail to verify.
	narrow := a.IssueToken(Capability{Keys: []string{"cart"}, Ops: []string{"get"}})
	wide := a.IssueToken(Capability{Keys: []string{"*"}, Ops: []string{"put"}})
	widePayload := strings.SplitN(wide, ".", 2)[0]
	narrowSig := strings.SplitN(narrow, ".", 2)[1]
	forged := widePayload + "." + narrowSig
	if _, err := a.VerifyToken(forged, "anything", "put", time.Unix(0, 0)); err != ErrBadSignature {
		t.Fatalf("tampered token: want ErrBadSignature, got %v", err)
	}
}

func TestScopeEnforced(t *testing.T) {
	a := New([]byte("s"))
	tok := a.IssueToken(Capability{Keys: []string{"cart"}, Ops: []string{"get"}})
	if _, err := a.VerifyToken(tok, "secret", "get", time.Unix(0, 0)); err != ErrScopeKey {
		t.Fatalf("out-of-scope key: want ErrScopeKey, got %v", err)
	}
	if _, err := a.VerifyToken(tok, "cart", "put", time.Unix(0, 0)); err != ErrScopeOp {
		t.Fatalf("out-of-scope op: want ErrScopeOp, got %v", err)
	}
}

func TestExpiry(t *testing.T) {
	a := New([]byte("s"))
	tok := a.IssueToken(Capability{Keys: []string{"*"}, Ops: []string{"get"}, Expires: 50})
	if _, err := a.VerifyToken(tok, "k", "get", time.Unix(100, 0)); err != ErrTokenExpired {
		t.Fatalf("want ErrTokenExpired, got %v", err)
	}
	if _, err := a.VerifyToken(tok, "k", "get", time.Unix(10, 0)); err != nil {
		t.Fatalf("unexpired token rejected: %v", err)
	}
}

func TestMalformed(t *testing.T) {
	a := New([]byte("s"))
	if _, err := a.VerifyToken("not-a-token", "k", "get", time.Now()); err != ErrMalformedToken {
		t.Fatalf("want ErrMalformedToken, got %v", err)
	}
}
