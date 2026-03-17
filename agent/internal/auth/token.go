package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// TokenAuthVerifier verifies Bearer token credentials from the Authorization header.
// Uses constant-time comparison to prevent timing attacks.
type TokenAuthVerifier struct {
	token []byte
}

// NewTokenAuthVerifier creates a verifier for the given secret token.
func NewTokenAuthVerifier(token string) *TokenAuthVerifier {
	return &TokenAuthVerifier{token: []byte(token)}
}

// Authenticate implements Authenticator. Extracts the Bearer token from
// the Authorization header and performs constant-time comparison.
// Returns "api-token" as the username for token-authenticated requests.
func (t *TokenAuthVerifier) Authenticate(r *http.Request) (string, bool) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", false
	}

	// Only accept Bearer scheme — never query params.
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return "", false
	}

	provided := []byte(strings.TrimPrefix(authHeader, "Bearer "))

	if subtle.ConstantTimeCompare(provided, t.token) == 1 {
		return "api-token", true
	}
	return "", false
}
