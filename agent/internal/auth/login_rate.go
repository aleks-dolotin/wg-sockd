package auth

import (
	"sync"
	"time"
)

// loginAttempt records a single failed login attempt timestamp.
type loginAttempt struct {
	timestamps []time.Time
}

// LoginRateLimiter implements per-IP rate limiting for login attempts.
// Keyed by remote IP (F5 fix: prevents bypass via TCP reconnect).
// 5 failed attempts per 60 seconds per IP.
type LoginRateLimiter struct {
	maxAttempts int
	window      time.Duration
	mu          sync.Mutex
	attempts    map[string]*loginAttempt // keyed by remote IP
	nowFunc     func() time.Time
	done        chan struct{}
}

// NewLoginRateLimiter creates a login rate limiter.
// maxAttempts: max failed attempts per window. window: time window for counting.
func NewLoginRateLimiter(maxAttempts int, window time.Duration) *LoginRateLimiter {
	lr := &LoginRateLimiter{
		maxAttempts: maxAttempts,
		window:      window,
		attempts:    make(map[string]*loginAttempt),
		nowFunc:     time.Now,
		done:        make(chan struct{}),
	}
	go lr.cleanup()
	return lr
}

// Check returns true if the IP is allowed to attempt login (under limit).
// Returns false if rate limit exceeded.
func (lr *LoginRateLimiter) Check(remoteIP string) bool {
	lr.mu.Lock()
	defer lr.mu.Unlock()

	a, ok := lr.attempts[remoteIP]
	if !ok {
		return true
	}

	now := lr.nowFunc()
	cutoff := now.Add(-lr.window)

	// Count recent failures within the window.
	count := 0
	for _, ts := range a.timestamps {
		if ts.After(cutoff) {
			count++
		}
	}

	return count < lr.maxAttempts
}

// RecordFailure records a failed login attempt for the given IP.
func (lr *LoginRateLimiter) RecordFailure(remoteIP string) {
	lr.mu.Lock()
	defer lr.mu.Unlock()

	now := lr.nowFunc()
	a, ok := lr.attempts[remoteIP]
	if !ok {
		a = &loginAttempt{}
		lr.attempts[remoteIP] = a
	}
	a.timestamps = append(a.timestamps, now)

	// Trim old entries to prevent unbounded growth.
	cutoff := now.Add(-lr.window)
	filtered := a.timestamps[:0]
	for _, ts := range a.timestamps {
		if ts.After(cutoff) {
			filtered = append(filtered, ts)
		}
	}
	a.timestamps = filtered
}

// Reset clears the failure count for the given IP (called on successful login).
func (lr *LoginRateLimiter) Reset(remoteIP string) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	delete(lr.attempts, remoteIP)
}

// Close stops the background cleanup goroutine. Safe to call multiple times.
func (lr *LoginRateLimiter) Close() {
	select {
	case <-lr.done:
	default:
		close(lr.done)
	}
}

// cleanup removes stale entries every 60 seconds.
func (lr *LoginRateLimiter) cleanup() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-lr.done:
			return
		case <-ticker.C:
			lr.mu.Lock()
			now := lr.nowFunc()
			cutoff := now.Add(-lr.window)
			for ip, a := range lr.attempts {
				// Remove entries with no recent attempts.
				hasRecent := false
				for _, ts := range a.timestamps {
					if ts.After(cutoff) {
						hasRecent = true
						break
					}
				}
				if !hasRecent {
					delete(lr.attempts, ip)
				}
			}
			lr.mu.Unlock()
		}
	}
}
