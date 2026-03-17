package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestBasicAuthVerifier_CorrectCredentials(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret123"), 12)
	v := NewBasicAuthVerifier("admin", string(hash))

	if !v.Verify("admin", "secret123") {
		t.Error("expected valid credentials to pass")
	}
}

func TestBasicAuthVerifier_WrongPassword(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret123"), 12)
	v := NewBasicAuthVerifier("admin", string(hash))

	if v.Verify("admin", "wrongpass") {
		t.Error("expected wrong password to fail")
	}
}

func TestBasicAuthVerifier_WrongUsername(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret123"), 12)
	v := NewBasicAuthVerifier("admin", string(hash))

	if v.Verify("nobody", "secret123") {
		t.Error("expected wrong username to fail")
	}
}

func TestTokenAuthVerifier_CorrectToken(t *testing.T) {
	v := NewTokenAuthVerifier("my-secret-token-at-least-32-chars!")

	req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
	req.Header.Set("Authorization", "Bearer my-secret-token-at-least-32-chars!")

	username, ok := v.Authenticate(req)
	if !ok {
		t.Fatal("expected valid token to pass")
	}
	if username != "api-token" {
		t.Errorf("username = %q, want %q", username, "api-token")
	}
}

func TestTokenAuthVerifier_WrongToken(t *testing.T) {
	v := NewTokenAuthVerifier("my-secret-token-at-least-32-chars!")

	req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")

	_, ok := v.Authenticate(req)
	if ok {
		t.Error("expected wrong token to fail")
	}
}

func TestTokenAuthVerifier_NoHeader(t *testing.T) {
	v := NewTokenAuthVerifier("my-secret-token")

	req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)

	_, ok := v.Authenticate(req)
	if ok {
		t.Error("expected missing header to fail")
	}
}

func TestTokenAuthVerifier_NonBearerScheme(t *testing.T) {
	v := NewTokenAuthVerifier("my-secret-token")

	req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")

	_, ok := v.Authenticate(req)
	if ok {
		t.Error("expected non-Bearer scheme to fail")
	}
}
