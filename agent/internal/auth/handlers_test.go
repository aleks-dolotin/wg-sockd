package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/config"
)

func testAuthConfig(basicEnabled bool, passwordHash string) *config.AuthConfig {
	return &config.AuthConfig{
		Basic: config.BasicAuthConfig{
			Enabled:      basicEnabled,
			Username:     "admin",
			PasswordHash: passwordHash,
		},
		Token: config.TokenAuthConfig{
			Enabled: false,
		},
		SessionTTL:     15 * time.Minute,
		SkipUnixSocket: true,
		SecureCookies:  "auto",
		MaxSessions:    100,
	}
}

func setupHandlers(t *testing.T) (*AuthHandlers, *SessionStore, string) {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret123"), 12)
	hashStr := string(hash)
	cfg := testAuthConfig(true, hashStr)
	ss := newTestSessionStore(15*time.Minute, 100)
	bv := NewBasicAuthVerifier("admin", hashStr)
	lr := &LoginRateLimiter{
		maxAttempts: 5,
		window:      60 * time.Second,
		attempts:    make(map[string]*loginAttempt),
		nowFunc:     time.Now,
		done:        make(chan struct{}),
	}
	h := NewAuthHandlers(cfg, ss, bv, lr, NoopCredentialCounter())
	return h, ss, hashStr
}

func TestLogin_Success(t *testing.T) {
	h, _, _ := setupHandlers(t)
	handler := h.Handler()

	body, _ := json.Marshal(loginRequest{Username: "admin", Password: "secret123"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// Check cookie is set.
	cookies := rec.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == sessionCookieName {
			found = true
			if !c.HttpOnly {
				t.Error("expected HttpOnly cookie")
			}
			break
		}
	}
	if !found {
		t.Error("expected session cookie to be set")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	h, _, _ := setupHandlers(t)
	handler := h.Handler()

	body, _ := json.Marshal(loginRequest{Username: "admin", Password: "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	var resp map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp["error"] != "invalid_credentials" {
		t.Errorf("error = %q, want %q", resp["error"], "invalid_credentials")
	}
}

func TestLogin_WrongUsername(t *testing.T) {
	h, _, _ := setupHandlers(t)
	handler := h.Handler()

	body, _ := json.Marshal(loginRequest{Username: "nobody", Password: "secret123"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	var resp map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	// Generic message — same as wrong password (AC-4).
	if resp["message"] != "invalid username or password" {
		t.Errorf("message = %q, want generic message", resp["message"])
	}
}

func TestLogout_ClearsSession(t *testing.T) {
	h, ss, _ := setupHandlers(t)
	handler := h.Handler()

	token, _ := ss.Create("admin")

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Session should be deleted.
	if _, ok := ss.Get(token); ok {
		t.Error("expected session to be deleted after logout")
	}
}

func TestSession_NoAuth(t *testing.T) {
	cfg := &config.AuthConfig{
		SessionTTL:  15 * time.Minute,
		MaxSessions: 100,
	}
	ss := newTestSessionStore(15*time.Minute, 100)
	h := NewAuthHandlers(cfg, ss, nil, nil, NoopCredentialCounter())
	handler := h.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp["auth_required"] != false {
		t.Errorf("auth_required = %v, want false", resp["auth_required"])
	}
}

func TestSession_ValidCookie(t *testing.T) {
	h, ss, _ := setupHandlers(t)
	handler := h.Handler()

	token, _ := ss.Create("admin")

	req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp["username"] != "admin" {
		t.Errorf("username = %v, want admin", resp["username"])
	}
}

func TestSession_NoCookie_Returns401(t *testing.T) {
	h, _, _ := setupHandlers(t)
	handler := h.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestLogin_RateLimited(t *testing.T) {
	h, _, _ := setupHandlers(t)
	handler := h.Handler()

	// Exhaust rate limit with 5 failures.
	for i := 0; i < 5; i++ {
		body, _ := json.Marshal(loginRequest{Username: "admin", Password: "wrong"})
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
		req.RemoteAddr = "10.0.0.1:9999"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// 6th attempt should be rate limited.
	body, _ := json.Marshal(loginRequest{Username: "admin", Password: "secret123"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.RemoteAddr = "10.0.0.1:9999"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

func TestLogin_NoAuthConfigured_Returns400(t *testing.T) {
	cfg := &config.AuthConfig{
		SessionTTL:  15 * time.Minute,
		MaxSessions: 100,
	}
	ss := newTestSessionStore(15*time.Minute, 100)
	h := NewAuthHandlers(cfg, ss, nil, nil, NoopCredentialCounter())
	handler := h.Handler()

	body, _ := json.Marshal(loginRequest{Username: "admin", Password: "secret"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	var resp map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp["error"] != "auth_not_configured" {
		t.Errorf("error = %q, want %q", resp["error"], "auth_not_configured")
	}
}

func TestLogin_SessionRotation(t *testing.T) {
	h, ss, _ := setupHandlers(t)
	handler := h.Handler()

	// First login.
	body, _ := json.Marshal(loginRequest{Username: "admin", Password: "secret123"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var firstCookieValue string
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			firstCookieValue = c.Value
		}
	}

	// Second login — should invalidate first session.
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req2.RemoteAddr = "192.168.1.1:12345"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	// First token should be invalid now.
	if _, ok := ss.Get(firstCookieValue); ok {
		t.Error("expected first session to be rotated out")
	}
}
