package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/ctxkeys"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username := ctxkeys.UsernameFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"user": username})
	})
}

func TestMiddleware_SessionCookiePassthrough(t *testing.T) {
	ss := newTestSessionStore(15*time.Minute, 100)
	defer ss.Close()
	token, _ := ss.Create("admin")

	mw := NewMiddleware(ss, nil, true, "auto")
	handler := mw.Wrap(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestMiddleware_BearerTokenPassthrough(t *testing.T) {
	ss := newTestSessionStore(15*time.Minute, 100)
	defer ss.Close()
	tv := NewTokenAuthVerifier("test-secret-token-32-chars-long!")

	mw := NewMiddleware(ss, tv, true, "auto")
	handler := mw.Wrap(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
	req.Header.Set("Authorization", "Bearer test-secret-token-32-chars-long!")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestMiddleware_UnixSocketExempt(t *testing.T) {
	ss := newTestSessionStore(15*time.Minute, 100)
	defer ss.Close()

	mw := NewMiddleware(ss, nil, true, "auto")
	handler := mw.Wrap(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxkeys.IsUnixSocketKey{}, true))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestMiddleware_UnixSocketNotExemptWhenDisabled(t *testing.T) {
	ss := newTestSessionStore(15*time.Minute, 100)
	defer ss.Close()

	mw := NewMiddleware(ss, nil, false, "auto") // skipUnixSocket = false
	handler := mw.Wrap(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxkeys.IsUnixSocketKey{}, true))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMiddleware_HealthExempt(t *testing.T) {
	ss := newTestSessionStore(15*time.Minute, 100)
	defer ss.Close()

	mw := NewMiddleware(ss, nil, false, "auto")
	handler := mw.Wrap(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestMiddleware_ContentNegotiation_Browser302(t *testing.T) {
	ss := newTestSessionStore(15*time.Minute, 100)
	defer ss.Close()

	mw := NewMiddleware(ss, nil, false, "auto")
	handler := mw.Wrap(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
	req.Header.Set("Accept", "text/html,application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}
}

func TestMiddleware_ContentNegotiation_API401JSON(t *testing.T) {
	ss := newTestSessionStore(15*time.Minute, 100)
	defer ss.Close()

	mw := NewMiddleware(ss, nil, false, "auto")
	handler := mw.Wrap(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "unauthorized" {
		t.Errorf("error = %q, want %q", body["error"], "unauthorized")
	}
}

func TestMiddleware_ExpiredSessionCookie_Rejects(t *testing.T) {
	now := time.Now()
	ss := newTestSessionStore(15*time.Minute, 100)
	ss.nowFunc = func() time.Time { return now }
	defer ss.Close()

	token, _ := ss.Create("admin")

	// Advance time past TTL.
	ss.nowFunc = func() time.Time { return now.Add(16 * time.Minute) }

	mw := NewMiddleware(ss, nil, false, "auto")
	handler := mw.Wrap(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d for expired session", rec.Code, http.StatusUnauthorized)
	}
}
