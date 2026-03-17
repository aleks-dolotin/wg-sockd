package auth

import (
	"golang.org/x/crypto/bcrypt"
)

// dummyHash is a pre-computed bcrypt hash used for timing-safe comparison
// when the username is wrong. Prevents timing side-channel attacks.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-timing-safe"), 12)

// BasicAuthVerifier verifies username/password credentials against
// a configured username and bcrypt password hash (cost 12).
type BasicAuthVerifier struct {
	username     string
	passwordHash []byte
}

// NewBasicAuthVerifier creates a verifier from config values.
func NewBasicAuthVerifier(username, passwordHash string) *BasicAuthVerifier {
	return &BasicAuthVerifier{
		username:     username,
		passwordHash: []byte(passwordHash),
	}
}

// Verify checks username and password. Returns true if credentials are valid.
// Timing-safe: if username is wrong, compares against dummy hash to prevent
// timing side-channel detection of valid usernames.
func (b *BasicAuthVerifier) Verify(username, password string) bool {
	if username != b.username {
		// Compare against dummy hash to normalize timing.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return false
	}
	err := bcrypt.CompareHashAndPassword(b.passwordHash, []byte(password))
	return err == nil
}
