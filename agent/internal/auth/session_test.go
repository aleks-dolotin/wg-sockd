package auth

import (
	"sync"
	"testing"
	"time"
)

func newTestSessionStore(ttl time.Duration, max int) *SessionStore {
	s := &SessionStore{
		ttl:         ttl,
		maxSessions: max,
		sessions:    make(map[string]*Session),
		nowFunc:     time.Now,
		done:        make(chan struct{}),
	}
	// No cleanup goroutine in tests — we control time manually.
	return s
}

func TestSessionStore_CreateAndGet(t *testing.T) {
	s := newTestSessionStore(15*time.Minute, 100)
	defer s.Close()

	token, err := s.Create("admin")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	sess, ok := s.Get(token)
	if !ok {
		t.Fatal("expected session to exist")
	}
	if sess.Username != "admin" {
		t.Errorf("username = %q, want %q", sess.Username, "admin")
	}
}

func TestSessionStore_GetMissing(t *testing.T) {
	s := newTestSessionStore(15*time.Minute, 100)
	defer s.Close()

	_, ok := s.Get("nonexistent")
	if ok {
		t.Fatal("expected no session for unknown token")
	}
}

func TestSessionStore_TTLExpiry(t *testing.T) {
	now := time.Now()
	s := newTestSessionStore(15*time.Minute, 100)
	s.nowFunc = func() time.Time { return now }
	defer s.Close()

	token, _ := s.Create("admin")

	// Advance time past TTL.
	s.nowFunc = func() time.Time { return now.Add(16 * time.Minute) }

	_, ok := s.Get(token)
	if ok {
		t.Fatal("expected session to be expired")
	}
}

func TestSessionStore_SlidingExpiry(t *testing.T) {
	now := time.Now()
	s := newTestSessionStore(15*time.Minute, 100)
	s.nowFunc = func() time.Time { return now }
	defer s.Close()

	token, _ := s.Create("admin")

	// 10 minutes later — within TTL, touch the session (simulates user-initiated request).
	s.nowFunc = func() time.Time { return now.Add(10 * time.Minute) }
	if _, ok := s.Get(token); !ok {
		t.Fatal("expected session to be valid at +10min")
	}
	s.Touch(token) // explicit touch — only called for user-initiated requests

	// 10 more minutes (20 total from creation, but only 10 from last Touch).
	s.nowFunc = func() time.Time { return now.Add(20 * time.Minute) }
	if _, ok := s.Get(token); !ok {
		t.Fatal("expected session to still be valid at +20min (sliding window: 10min since last touch)")
	}
	s.Touch(token)

	// 10 more minutes (30 total, 10 from last Touch at +20).
	s.nowFunc = func() time.Time { return now.Add(30 * time.Minute) }
	if _, ok := s.Get(token); !ok {
		t.Fatal("expected session to still be valid at +30min (10min since last touch at +20)")
	}
	s.Touch(token)

	// Now jump 16 minutes from last Touch (+30 → +46).
	s.nowFunc = func() time.Time { return now.Add(46 * time.Minute) }
	if _, ok := s.Get(token); ok {
		t.Fatal("expected session to be expired at +46min (16min since last touch at +30)")
	}
}

func TestSessionStore_GetWithoutTouchDoesNotExtend(t *testing.T) {
	now := time.Now()
	s := newTestSessionStore(15*time.Minute, 100)
	s.nowFunc = func() time.Time { return now }
	defer s.Close()

	token, _ := s.Create("admin")

	// Repeated Get without Touch at 10 min — session should still expire at 15 min from creation.
	s.nowFunc = func() time.Time { return now.Add(10 * time.Minute) }
	if _, ok := s.Get(token); !ok {
		t.Fatal("expected session to be valid at +10min")
	}
	// No Touch — simulates background polling.

	// At 16 min — should be expired (15 min from creation, no touch extended it).
	s.nowFunc = func() time.Time { return now.Add(16 * time.Minute) }
	if _, ok := s.Get(token); ok {
		t.Fatal("expected session to be expired at +16min (Get without Touch should not extend)")
	}
}

func TestSessionStore_Delete(t *testing.T) {
	s := newTestSessionStore(15*time.Minute, 100)
	defer s.Close()

	token, _ := s.Create("admin")
	s.Delete(token)

	_, ok := s.Get(token)
	if ok {
		t.Fatal("expected session to be deleted")
	}
}

func TestSessionStore_DeleteByUsername(t *testing.T) {
	s := newTestSessionStore(15*time.Minute, 100)
	defer s.Close()

	t1, _ := s.Create("admin")
	t2, _ := s.Create("admin")
	t3, _ := s.Create("other")

	s.DeleteByUsername("admin")

	if _, ok := s.Get(t1); ok {
		t.Error("expected t1 deleted")
	}
	if _, ok := s.Get(t2); ok {
		t.Error("expected t2 deleted")
	}
	if _, ok := s.Get(t3); !ok {
		t.Error("expected t3 to still exist")
	}
}

func TestSessionStore_LRUEviction(t *testing.T) {
	now := time.Now()
	s := newTestSessionStore(15*time.Minute, 5)
	defer s.Close()

	var tokens []string
	for i := 0; i < 10; i++ {
		s.nowFunc = func() time.Time { return now.Add(time.Duration(i) * time.Second) }
		tok, _ := s.Create("admin")
		tokens = append(tokens, tok)
	}

	// Reset time to within TTL of all sessions.
	s.nowFunc = func() time.Time { return now.Add(10 * time.Second) }

	// First 5 tokens should be evicted.
	for i := 0; i < 5; i++ {
		if _, ok := s.Get(tokens[i]); ok {
			t.Errorf("expected token %d to be evicted", i)
		}
	}
	// Last 5 should exist.
	for i := 5; i < 10; i++ {
		if _, ok := s.Get(tokens[i]); !ok {
			t.Errorf("expected token %d to exist", i)
		}
	}
}

func TestSessionStore_SessionRotation(t *testing.T) {
	s := newTestSessionStore(15*time.Minute, 100)
	defer s.Close()

	old, _ := s.Create("admin")
	s.DeleteByUsername("admin")
	newTok, _ := s.Create("admin")

	if _, ok := s.Get(old); ok {
		t.Error("old session should be invalidated after rotation")
	}
	if _, ok := s.Get(newTok); !ok {
		t.Error("new session should be valid")
	}
}

func TestSessionStore_ConcurrentAccess(t *testing.T) {
	s := newTestSessionStore(15*time.Minute, 1000)
	defer s.Close()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, _ := s.Create("admin")
			s.Get(tok)
			s.Delete(tok)
		}()
	}
	wg.Wait()
}
