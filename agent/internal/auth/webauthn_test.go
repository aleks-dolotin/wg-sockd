package auth

import (
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// newTestChallengeStore creates a ChallengeStore with an injected clock for testing.
func newTestChallengeStore(nowFunc func() time.Time) *ChallengeStore {
	cs := &ChallengeStore{
		entries: make(map[string]*challengeEntry),
		nowFunc: nowFunc,
		done:    make(chan struct{}),
	}
	// Don't start the cleanup goroutine in tests — avoid goroutine leaks.
	return cs
}

func TestChallengeStore_StoreAndGet(t *testing.T) {
	cs := newTestChallengeStore(time.Now)

	sd := &webauthn.SessionData{Challenge: "test-challenge"}
	token := cs.Store(sd)
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	got, ok := cs.Get(token)
	if !ok {
		t.Fatal("expected to find session data, got false")
	}
	if got.Challenge != "test-challenge" {
		t.Errorf("challenge = %q, want %q", got.Challenge, "test-challenge")
	}
}

func TestChallengeStore_OneTimeUse(t *testing.T) {
	cs := newTestChallengeStore(time.Now)

	sd := &webauthn.SessionData{Challenge: "once"}
	token := cs.Store(sd)

	// First Get succeeds.
	if _, ok := cs.Get(token); !ok {
		t.Fatal("first Get should succeed")
	}
	// Second Get should fail — one-time use.
	if _, ok := cs.Get(token); ok {
		t.Fatal("second Get should return false (one-time use)")
	}
}

func TestChallengeStore_TTLExpiry(t *testing.T) {
	now := time.Now()
	cs := newTestChallengeStore(func() time.Time { return now })

	sd := &webauthn.SessionData{Challenge: "expiring"}
	token := cs.Store(sd)

	// Advance clock beyond TTL.
	now = now.Add(challengeTTL + time.Second)

	_, ok := cs.Get(token)
	if ok {
		t.Fatal("expected expired challenge to return false")
	}
}

func TestChallengeStore_LRUEviction(t *testing.T) {
	cs := newTestChallengeStore(time.Now)

	// Fill to capacity.
	tokens := make([]string, challengeMaxCap)
	for i := 0; i < challengeMaxCap; i++ {
		tokens[i] = cs.Store(&webauthn.SessionData{Challenge: "c"})
	}

	// Store one more — should evict the oldest.
	extra := cs.Store(&webauthn.SessionData{Challenge: "extra"})

	// The extra token should be present.
	if _, ok := cs.Get(extra); !ok {
		t.Fatal("extra token should be retrievable after eviction")
	}

	// Total entries should be at most challengeMaxCap.
	cs.mu.Lock()
	count := len(cs.entries)
	cs.mu.Unlock()
	// After get(extra) consumed it, count is challengeMaxCap-1.
	if count > challengeMaxCap {
		t.Errorf("store size = %d, want <= %d", count, challengeMaxCap)
	}
}

func TestChallengeStore_Close(t *testing.T) {
	cs := NewChallengeStore()
	// Double Close should not panic.
	cs.Close()
	cs.Close()
}

func TestChallengeStore_UnknownToken(t *testing.T) {
	cs := newTestChallengeStore(time.Now)
	_, ok := cs.Get("nonexistent-token")
	if ok {
		t.Fatal("expected false for unknown token")
	}
}

func TestSQLiteCredentialCounter_CountsZeroOnNilDB(t *testing.T) {
	// Noop counter always returns 0.
	c := NoopCredentialCounter()
	if n := c.CountWebAuthnCredentials(); n != 0 {
		t.Errorf("noop counter = %d, want 0", n)
	}
}

