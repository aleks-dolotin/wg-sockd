// Package middleware provides HTTP middleware for wg-sockd API.
// ratelimit.go implements per-connection token-bucket rate limiting (RT-2).
package middleware

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// connIDKey is the context key type for per-connection identification.
type connIDKey struct{}

// connCounter generates unique per-connection IDs.
var connCounter atomic.Int64

// ConnContext is intended for use with http.Server.ConnContext.
// It injects a unique per-connection ID into the request context,
// enabling per-connection rate limiting for Unix sockets where
// RemoteAddr is not meaningful.
func ConnContext(ctx context.Context, c net.Conn) context.Context {
	id := connCounter.Add(1)
	return context.WithValue(ctx, connIDKey{}, id)
}

// connIDFromContext extracts the connection ID injected by ConnContext.
// Returns 0 if no ID is present (falls back to a shared bucket).
func connIDFromContext(ctx context.Context) int64 {
	id, _ := ctx.Value(connIDKey{}).(int64)
	return id
}

// bucket represents a token bucket for a single connection.
type bucket struct {
	tokens    float64
	lastCheck time.Time
}

// RateLimiter implements per-connection token-bucket rate limiting.
// Each connection gets its own bucket that refills at a fixed rate.
type RateLimiter struct {
	rate    float64 // tokens refilled per second
	burst   int     // max token capacity
	mu      sync.Mutex
	buckets map[int64]*bucket
	nowFunc func() time.Time // injectable for testing
}

// NewRateLimiter creates a per-connection rate limiter.
// rate: maximum sustained requests/second per connection.
// burst: maximum burst size (typically equal to rate for simple limiting).
func NewRateLimiter(rate float64, burst int) *RateLimiter {
	rl := &RateLimiter{
		rate:    rate,
		burst:   burst,
		buckets: make(map[int64]*bucket),
		nowFunc: time.Now,
	}
	go rl.cleanup()
	return rl
}

// allow checks if the connection identified by connID may proceed.
func (rl *RateLimiter) allow(connID int64) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.nowFunc()
	b, exists := rl.buckets[connID]
	if !exists {
		b = &bucket{
			tokens:    float64(rl.burst),
			lastCheck: now,
		}
		rl.buckets[connID] = b
	}

	// Refill tokens based on elapsed time.
	elapsed := now.Sub(b.lastCheck).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.lastCheck = now

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}
	return false
}

// cleanup removes stale buckets every 60 seconds to prevent unbounded memory growth.
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		now := rl.nowFunc()
		for id, b := range rl.buckets {
			if now.Sub(b.lastCheck) > 5*time.Minute {
				delete(rl.buckets, id)
			}
		}
		rl.mu.Unlock()
	}
}

// Wrap returns an http.Handler that applies per-connection rate limiting.
// The health endpoint (/api/health) is always exempted to ensure
// monitoring and systemd watchdog checks are never throttled.
func (rl *RateLimiter) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Exempt health endpoint from rate limiting.
		if r.URL.Path == "/api/health" {
			next.ServeHTTP(w, r)
			return
		}

		connID := connIDFromContext(r.Context())
		if !rl.allow(connID) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":   "rate_limit_exceeded",
				"message": "too many requests",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

