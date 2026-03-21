package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"golang.org/x/crypto/bcrypt"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/config"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/ctxkeys"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/storage"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newTestWebAuthnLib creates a minimal webauthn.WebAuthn for testing.
func newTestWebAuthnLib(t *testing.T) *webauthn.WebAuthn {
	t.Helper()
	w, err := webauthn.New(&webauthn.Config{
		RPID:          "localhost",
		RPDisplayName: "Test",
		RPOrigins:     []string{"https://localhost"},
	})
	if err != nil {
		t.Fatalf("webauthn.New: %v", err)
	}
	return w
}

// setupWebAuthnHandlers creates AuthHandlers with real WebAuthn lib, ChallengeStore, and in-memory DB.
func setupWebAuthnHandlers(t *testing.T) (*AuthHandlers, *ChallengeStore, *SessionStore) {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret123"), 4) // fast for tests
	hashStr := string(hash)
	cfg := &config.AuthConfig{
		Basic: config.BasicAuthConfig{
			Enabled:      true,
			Username:     "admin",
			PasswordHash: hashStr,
		},
		SessionTTL:     15 * time.Minute,
		SkipUnixSocket: true,
		SecureCookies:  "auto",
		MaxSessions:    100,
	}
	waLib := newTestWebAuthnLib(t)
	waCfg := &config.WebAuthnConfig{
		Enabled:     true,
		DisplayName: "Test",
		Origin:      "https://localhost",
	}
	ss := newTestSessionStore(15*time.Minute, 100)
	cs := newTestChallengeStore(time.Now)
	bv := NewBasicAuthVerifier("admin", hashStr)
	lr := &LoginRateLimiter{
		maxAttempts: 5,
		window:      60 * time.Second,
		attempts:    make(map[string]*loginAttempt),
		nowFunc:     time.Now,
		done:        make(chan struct{}),
	}
	db, err := storage.NewDB(":memory:")
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	h := NewAuthHandlers(cfg, ss, bv, lr, NoopCredentialCounter(), waLib, cs, db, waCfg)
	return h, cs, ss
}

// addTestSession creates a session and returns the cookie value.
func addTestSession(t *testing.T, ss *SessionStore) string {
	t.Helper()
	token, err := ss.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	return token
}

// fakeCredentialJSON returns a plausible (but cryptographically invalid) WebAuthn
// credential response JSON. go-webauthn will reject it during verification,
// but body parsing and challenge lookup should succeed first.
func fakeCredentialJSON() json.RawMessage {
	return json.RawMessage(`{
		"id": "dGVzdC1jcmVkLWlk",
		"rawId": "dGVzdC1jcmVkLWlk",
		"response": {
			"attestationObject": "o2NmbXRkbm9uZWdhdHRTdG10oGhhdXRoRGF0YVi3",
			"clientDataJSON": "eyJ0eXBlIjoid2ViYXV0aG4uY3JlYXRlIiwiY2hhbGxlbmdlIjoiZEdWemRBIiwib3JpZ2luIjoiaHR0cHM6Ly9sb2NhbGhvc3QifQ"
		},
		"type": "public-key"
	}`)
}

// fakeAssertionJSON returns a plausible (but invalid) assertion response.
func fakeAssertionJSON() json.RawMessage {
	return json.RawMessage(`{
		"id": "dGVzdC1jcmVkLWlk",
		"rawId": "dGVzdC1jcmVkLWlk",
		"response": {
			"authenticatorData": "dGVzdA",
			"clientDataJSON": "eyJ0eXBlIjoid2ViYXV0aG4uZ2V0IiwiY2hhbGxlbmdlIjoiZEdWemRBIiwib3JpZ2luIjoiaHR0cHM6Ly9sb2NhbGhvc3QifQ",
			"signature": "dGVzdA"
		},
		"type": "public-key"
	}`)
}

// ---------------------------------------------------------------------------
// Tests: webauthnRegisterFinish — body parsing + credential forwarding
// ---------------------------------------------------------------------------

func TestWebauthnRegisterFinish_BodyParsing_ExtractsTokenAndCredential(t *testing.T) {
	h, cs, ss := setupWebAuthnHandlers(t)
	handler := h.Handler()
	sessionToken := addTestSession(t, ss)

	// Store a challenge — we need a valid token for the handler to find.
	challengeToken := cs.Store(&webauthn.SessionData{
		Challenge: "test-challenge-register",
	})

	// Build envelope body: { credential, token, friendly_name }
	envelope := map[string]interface{}{
		"credential":    json.RawMessage(fakeCredentialJSON()),
		"token":         challengeToken,
		"friendly_name": "Test Key",
	}
	body, _ := json.Marshal(envelope)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/webauthn/register/finish", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionToken})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// We expect "registration_failed" (invalid crypto) — NOT "invalid_request" (body parse error)
	// and NOT "invalid_token" (token not found). This proves:
	// 1. Body was parsed successfully (not "invalid_request")
	// 2. Token was extracted and found in ChallengeStore (not "invalid_token")
	// 3. Credential was forwarded to go-webauthn (which rejected it → "registration_failed")
	var resp map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&resp)

	if resp["error"] == "invalid_request" {
		t.Fatalf("got 'invalid_request' — body parsing failed. Response: %s", rec.Body.String())
	}
	if resp["error"] == "invalid_token" {
		t.Fatalf("got 'invalid_token' — token extraction or lookup failed. Response: %s", rec.Body.String())
	}
	// "registration_failed" means body parsing and token lookup succeeded,
	// but the fake credential didn't pass crypto verification — expected.
	if resp["error"] != "registration_failed" {
		t.Errorf("expected error='registration_failed' (crypto reject), got %q. Full: %s", resp["error"], rec.Body.String())
	}
}

func TestWebauthnRegisterFinish_MissingCredentialField(t *testing.T) {
	h, cs, ss := setupWebAuthnHandlers(t)
	handler := h.Handler()
	sessionToken := addTestSession(t, ss)

	challengeToken := cs.Store(&webauthn.SessionData{Challenge: "c"})

	// Envelope without "credential" field.
	envelope := map[string]interface{}{
		"token":         challengeToken,
		"friendly_name": "Test Key",
	}
	body, _ := json.Marshal(envelope)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/webauthn/register/finish", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionToken})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var resp map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&resp)

	// With missing credential, json.RawMessage will be null → go-webauthn gets empty body.
	// Should fail at the library level, not at our parse level.
	if resp["error"] == "invalid_request" {
		t.Log("got invalid_request — acceptable if library parse triggered it")
	}
	// Should not be 200.
	if rec.Code == http.StatusOK {
		t.Fatal("expected error for missing credential field, got 200")
	}
}

func TestWebauthnRegisterFinish_InvalidJSON(t *testing.T) {
	h, _, ss := setupWebAuthnHandlers(t)
	handler := h.Handler()
	sessionToken := addTestSession(t, ss)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/webauthn/register/finish",
		bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionToken})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	var resp map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp["error"] != "invalid_request" {
		t.Errorf("error = %q, want 'invalid_request'", resp["error"])
	}
}

func TestWebauthnRegisterFinish_ExpiredToken(t *testing.T) {
	h, cs, ss := setupWebAuthnHandlers(t)
	handler := h.Handler()
	sessionToken := addTestSession(t, ss)

	// Store token but don't use it — use a different one.
	cs.Store(&webauthn.SessionData{Challenge: "c"})

	envelope := map[string]interface{}{
		"credential": json.RawMessage(fakeCredentialJSON()),
		"token":      "nonexistent-token",
	}
	body, _ := json.Marshal(envelope)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/webauthn/register/finish", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionToken})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var resp map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp["error"] != "invalid_token" {
		t.Errorf("error = %q, want 'invalid_token'", resp["error"])
	}
}

func TestWebauthnRegisterFinish_NoSession(t *testing.T) {
	h, _, _ := setupWebAuthnHandlers(t)
	handler := h.Handler()

	envelope := map[string]interface{}{
		"credential": json.RawMessage(fakeCredentialJSON()),
		"token":      "any-token",
	}
	body, _ := json.Marshal(envelope)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/webauthn/register/finish", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Tests: webauthnLoginFinish — body parsing + credential forwarding
// ---------------------------------------------------------------------------

func TestWebauthnLoginFinish_BodyParsing_ExtractsTokenAndCredential(t *testing.T) {
	h, cs, _ := setupWebAuthnHandlers(t)
	handler := h.Handler()

	// Store a challenge for login flow.
	challengeToken := cs.Store(&webauthn.SessionData{
		Challenge: "test-challenge-login",
	})

	envelope := map[string]interface{}{
		"credential": json.RawMessage(fakeAssertionJSON()),
		"token":      challengeToken,
	}
	body, _ := json.Marshal(envelope)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/webauthn/login/finish", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.168.1.1:12345"
	// Inject IsUnixSocket=false context for rate limiter.
	ctx := context.WithValue(req.Context(), ctxkeys.IsUnixSocketKey{}, false)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var resp map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&resp)

	if resp["error"] == "invalid_request" {
		t.Fatalf("got 'invalid_request' — body parsing failed. Response: %s", rec.Body.String())
	}
	if resp["error"] == "invalid_token" {
		t.Fatalf("got 'invalid_token' — token extraction or lookup failed. Response: %s", rec.Body.String())
	}
	// "authentication_failed" means body parsing + token lookup succeeded,
	// but fake assertion didn't pass crypto → expected.
	if resp["error"] != "authentication_failed" {
		t.Errorf("expected error='authentication_failed', got %q. Full: %s", resp["error"], rec.Body.String())
	}
}

func TestWebauthnLoginFinish_InvalidJSON(t *testing.T) {
	h, _, _ := setupWebAuthnHandlers(t)
	handler := h.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/auth/webauthn/login/finish",
		bytes.NewReader([]byte("{bad json")))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	var resp map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp["error"] != "invalid_request" {
		t.Errorf("error = %q, want 'invalid_request'", resp["error"])
	}
}

func TestWebauthnLoginFinish_ExpiredToken(t *testing.T) {
	h, _, _ := setupWebAuthnHandlers(t)
	handler := h.Handler()

	envelope := map[string]interface{}{
		"credential": json.RawMessage(fakeAssertionJSON()),
		"token":      "nonexistent",
	}
	body, _ := json.Marshal(envelope)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/webauthn/login/finish", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var resp map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp["error"] != "invalid_token" {
		t.Errorf("error = %q, want 'invalid_token'", resp["error"])
	}
}
