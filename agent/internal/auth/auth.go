// Package auth provides HTTP authentication middleware, session management,
// and credential verification for the wg-sockd agent API.
package auth

import "net/http"

// Authenticator verifies credentials from an HTTP request.
type Authenticator interface {
	Authenticate(r *http.Request) (username string, ok bool)
}

// WebAuthnCredentialCounter returns the count of registered WebAuthn credentials.
// Used by session endpoint to determine webauthn_available field.
// Layer 1 passes a no-op returning 0; Layer 2 wires real SQLite counter.
type WebAuthnCredentialCounter interface {
	CountWebAuthnCredentials() int
}

// noopCredentialCounter always returns 0 (Layer 1 default).
type noopCredentialCounter struct{}

func (noopCredentialCounter) CountWebAuthnCredentials() int { return 0 }

// NoopCredentialCounter returns a WebAuthnCredentialCounter that always returns 0.
func NoopCredentialCounter() WebAuthnCredentialCounter {
	return noopCredentialCounter{}
}
