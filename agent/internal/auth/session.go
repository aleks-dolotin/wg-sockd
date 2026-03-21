package auth

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// Session represents an authenticated user session.
type Session struct {
	Username     string
	Token        string
	CreatedAt    time.Time
	LastActivity time.Time // updated on every successful Get() — sliding window expiry
}

// SessionStore manages in-memory sessions with TTL expiration and LRU eviction.
// Pattern follows RateLimiter in middleware/ratelimit.go: map + mutex + cleanup goroutine + Close() + done channel.
type SessionStore struct {
	ttl         time.Duration
	maxSessions int
	mu          sync.Mutex
	sessions    map[string]*Session // keyed by token
	nowFunc     func() time.Time   // injectable for testing
	done        chan struct{}
}

// NewSessionStore creates a session store with the given TTL and max session count.
// Starts a background cleanup goroutine.
func NewSessionStore(ttl time.Duration, maxSessions int) *SessionStore {
	s := &SessionStore{
		ttl:         ttl,
		maxSessions: maxSessions,
		sessions:    make(map[string]*Session),
		nowFunc:     time.Now,
		done:        make(chan struct{}),
	}
	go s.cleanup()
	return s
}

// Create generates a new session for the given username.
// Returns the session token. Evicts the oldest session if at capacity (LRU).
func (s *SessionStore) Create(username string) (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)

	s.mu.Lock()
	defer s.mu.Unlock()

	// LRU eviction: if at capacity, delete oldest session by CreatedAt.
	if len(s.sessions) >= s.maxSessions {
		s.evictOldestLocked()
	}

	s.sessions[token] = &Session{
		Username:     username,
		Token:        token,
		CreatedAt:    s.nowFunc(),
		LastActivity: s.nowFunc(),
	}
	return token, nil
}

// Get retrieves a session by token. Returns nil, false if not found or expired.
// Does NOT update LastActivity — call Touch() explicitly for user-initiated requests.
func (s *SessionStore) Get(token string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[token]
	if !ok {
		return nil, false
	}

	// Check TTL against last activity (sliding window).
	if s.nowFunc().Sub(sess.LastActivity) > s.ttl {
		delete(s.sessions, token)
		return nil, false
	}

	return sess, true
}

// Touch updates the LastActivity timestamp for the given token, extending
// the sliding window. Call this only for user-initiated requests, not for
// background polling (stats, health, connection status).
func (s *SessionStore) Touch(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sess, ok := s.sessions[token]; ok {
		sess.LastActivity = s.nowFunc()
	}
}

// Delete removes a session by token.
func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

// DeleteByUsername removes all sessions for the given username (session rotation).
func (s *SessionStore) DeleteByUsername(username string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, sess := range s.sessions {
		if sess.Username == username {
			delete(s.sessions, token)
		}
	}
}

// Close stops the background cleanup goroutine. Safe to call multiple times.
func (s *SessionStore) Close() {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}

// ExpiresAt returns the expiration time for the given session (sliding window).
func (s *SessionStore) ExpiresAt(sess *Session) time.Time {
	return sess.LastActivity.Add(s.ttl)
}

// TTLSeconds returns the session TTL in seconds.
func (s *SessionStore) TTLSeconds() int {
	return int(s.ttl.Seconds())
}

// cleanup removes expired sessions every 60 seconds.
func (s *SessionStore) cleanup() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.mu.Lock()
			now := s.nowFunc()
			for token, sess := range s.sessions {
				if now.Sub(sess.LastActivity) > s.ttl {
					delete(s.sessions, token)
				}
			}
			s.mu.Unlock()
		}
	}
}

// evictOldestLocked removes the oldest session. Caller must hold s.mu.
func (s *SessionStore) evictOldestLocked() {
	var oldestToken string
	var oldestTime time.Time
	first := true
	for token, sess := range s.sessions {
		if first || sess.CreatedAt.Before(oldestTime) {
			oldestToken = token
			oldestTime = sess.CreatedAt
			first = false
		}
	}
	if oldestToken != "" {
		delete(s.sessions, oldestToken)
	}
}
