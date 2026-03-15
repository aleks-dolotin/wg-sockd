package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestLimiter creates a RateLimiter with an injectable clock for deterministic tests.
func newTestLimiter(rate float64, burst int, now func() time.Time) *RateLimiter {
	return &RateLimiter{
		rate:    rate,
		burst:   burst,
		buckets: make(map[int64]*bucket),
		nowFunc: now,
	}
}

// okHandler is a trivial handler that writes 200 OK.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestRateLimiter_AllowWithinLimit(t *testing.T) {
	// 10 requests within 1s should all pass with rate=10, burst=10.
	now := time.Now()
	rl := newTestLimiter(10, 10, func() time.Time { return now })
	handler := rl.Wrap(okHandler)

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
		// Inject connection ID = 1 for all requests (same connection).
		req = req.WithContext(context.WithValue(req.Context(), connIDKey{}, int64(1)))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i, rr.Code)
		}
	}
}

func TestRateLimiter_ExceedLimit(t *testing.T) {
	// 15 requests at same instant: first 10 pass, remaining 5 get 429.
	now := time.Now()
	rl := newTestLimiter(10, 10, func() time.Time { return now })
	handler := rl.Wrap(okHandler)

	okCount, rejectedCount := 0, 0
	for i := 0; i < 15; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
		req = req.WithContext(context.WithValue(req.Context(), connIDKey{}, int64(1)))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code == http.StatusOK {
			okCount++
		} else if rr.Code == http.StatusTooManyRequests {
			rejectedCount++
		} else {
			t.Errorf("request %d: unexpected status %d", i, rr.Code)
		}
	}

	if okCount != 10 {
		t.Errorf("expected 10 OK responses, got %d", okCount)
	}
	if rejectedCount != 5 {
		t.Errorf("expected 5 rejected responses, got %d", rejectedCount)
	}
}

func TestRateLimiter_RefillAfterCooldown(t *testing.T) {
	// After exhausting tokens, wait 1 second, requests should pass again.
	now := time.Now()
	rl := newTestLimiter(10, 10, func() time.Time { return now })
	handler := rl.Wrap(okHandler)

	// Exhaust all 10 tokens.
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
		req = req.WithContext(context.WithValue(req.Context(), connIDKey{}, int64(1)))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}

	// Next request should be rejected.
	req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
	req = req.WithContext(context.WithValue(req.Context(), connIDKey{}, int64(1)))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 after exhaustion, got %d", rr.Code)
	}

	// Advance time by 1 second — should refill 10 tokens.
	now = now.Add(1 * time.Second)

	// Now 10 more requests should pass.
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
		req = req.WithContext(context.WithValue(req.Context(), connIDKey{}, int64(1)))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("request %d after cooldown: expected 200, got %d", i, rr.Code)
		}
	}
}

func TestRateLimiter_HealthEndpointExempted(t *testing.T) {
	// Health endpoint should always pass even when limit is exhausted.
	now := time.Now()
	rl := newTestLimiter(10, 10, func() time.Time { return now })
	handler := rl.Wrap(okHandler)

	// Exhaust all tokens on /api/peers.
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
		req = req.WithContext(context.WithValue(req.Context(), connIDKey{}, int64(1)))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}

	// /api/peers should now be rejected.
	req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
	req = req.WithContext(context.WithValue(req.Context(), connIDKey{}, int64(1)))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 for /api/peers, got %d", rr.Code)
	}

	// But /api/health should still pass.
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req = req.WithContext(context.WithValue(req.Context(), connIDKey{}, int64(1)))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("health request %d: expected 200, got %d", i, rr.Code)
		}
	}
}

func TestRateLimiter_PerConnectionIsolation(t *testing.T) {
	// Different connections should have independent rate limits.
	now := time.Now()
	rl := newTestLimiter(10, 10, func() time.Time { return now })
	handler := rl.Wrap(okHandler)

	// Exhaust tokens for connection 1.
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
		req = req.WithContext(context.WithValue(req.Context(), connIDKey{}, int64(1)))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}

	// Connection 1 should be rejected.
	req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
	req = req.WithContext(context.WithValue(req.Context(), connIDKey{}, int64(1)))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("connection 1: expected 429, got %d", rr.Code)
	}

	// Connection 2 should still be allowed (separate bucket).
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
		req = req.WithContext(context.WithValue(req.Context(), connIDKey{}, int64(2)))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("connection 2 request %d: expected 200, got %d", i, rr.Code)
		}
	}
}

func TestRateLimiter_429ResponseBody(t *testing.T) {
	// Verify the 429 response body matches the spec.
	now := time.Now()
	rl := newTestLimiter(1, 1, func() time.Time { return now })
	handler := rl.Wrap(okHandler)

	// First request succeeds.
	req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
	req = req.WithContext(context.WithValue(req.Context(), connIDKey{}, int64(1)))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rr.Code)
	}

	// Second request should be rate limited.
	req = httptest.NewRequest(http.MethodGet, "/api/peers", nil)
	req = req.WithContext(context.WithValue(req.Context(), connIDKey{}, int64(1)))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: expected 429, got %d", rr.Code)
	}

	// Check Content-Type header.
	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	// Check Retry-After header.
	ra := rr.Header().Get("Retry-After")
	if ra != "1" {
		t.Errorf("expected Retry-After 1, got %q", ra)
	}

	// Check JSON body.
	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decoding 429 body: %v", err)
	}
	if body["error"] != "rate_limit_exceeded" {
		t.Errorf("expected error=rate_limit_exceeded, got %q", body["error"])
	}
	if body["message"] != "too many requests" {
		t.Errorf("expected message='too many requests', got %q", body["message"])
	}
}

func TestConnContext(t *testing.T) {
	// Verify ConnContext injects a unique ID per call.
	ctx := context.Background()

	ctx1 := ConnContext(ctx, nil)
	ctx2 := ConnContext(ctx, nil)

	id1 := connIDFromContext(ctx1)
	id2 := connIDFromContext(ctx2)

	if id1 == 0 {
		t.Error("expected non-zero connection ID")
	}
	if id1 == id2 {
		t.Errorf("expected unique IDs, got %d and %d", id1, id2)
	}
}

