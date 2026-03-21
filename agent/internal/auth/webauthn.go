package auth

import (
	"crypto/rand"
	"encoding/base64"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/storage"
)

// Ensure protocol is used (transport hints).
var _ = protocol.AuthenticatorTransport("")

// ---------------------------------------------------------------------------
// ChallengeStore — in-memory temporary store for WebAuthn ceremony challenges.
// Follows SessionStore / LoginRateLimiter pattern: map + mutex + goroutine + Close().
// ---------------------------------------------------------------------------

const (
	challengeTTL      = 60 * time.Second
	challengeMaxCap   = 100
	challengeCleanup  = 30 * time.Second
)

// challengeEntry holds a challenge with its creation timestamp.
type challengeEntry struct {
	sessionData *webauthn.SessionData
	createdAt   time.Time
}

// ChallengeStore stores WebAuthn ceremony challenges keyed by random token.
// Challenges are one-time use: deleted on any Get() call regardless of success.
type ChallengeStore struct {
	mu       sync.Mutex
	entries  map[string]*challengeEntry
	order    []string // insertion order for LRU eviction
	nowFunc  func() time.Time
	done     chan struct{}
}

// NewChallengeStore creates a ChallengeStore and starts the cleanup goroutine.
func NewChallengeStore() *ChallengeStore {
	cs := &ChallengeStore{
		entries: make(map[string]*challengeEntry),
		nowFunc: time.Now,
		done:    make(chan struct{}),
	}
	go cs.cleanup()
	return cs
}

// Store saves a SessionData and returns the opaque token the frontend uses to
// reclaim it. Evicts the oldest entry (LRU) if capacity is exceeded.
func (cs *ChallengeStore) Store(sd *webauthn.SessionData) string {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		// rand.Read should never fail in practice; fall back to empty token.
		log.Printf("WARNING: ChallengeStore.Store: rand.Read error: %v", err)
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)

	cs.mu.Lock()
	defer cs.mu.Unlock()

	// LRU eviction when at capacity.
	for len(cs.entries) >= challengeMaxCap && len(cs.order) > 0 {
		oldest := cs.order[0]
		cs.order = cs.order[1:]
		delete(cs.entries, oldest)
	}

	cs.entries[token] = &challengeEntry{sessionData: sd, createdAt: cs.nowFunc()}
	cs.order = append(cs.order, token)
	return token
}

// Get retrieves and deletes the SessionData for the given token (one-time use).
// Returns nil, false if the token is unknown or has expired.
func (cs *ChallengeStore) Get(token string) (*webauthn.SessionData, bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	entry, ok := cs.entries[token]
	// Always delete — one-time use per security requirement.
	delete(cs.entries, token)

	if !ok {
		return nil, false
	}
	// Check TTL.
	if cs.nowFunc().Sub(entry.createdAt) > challengeTTL {
		return nil, false
	}
	return entry.sessionData, true
}

// Close stops the background cleanup goroutine.
func (cs *ChallengeStore) Close() {
	select {
	case <-cs.done:
	default:
		close(cs.done)
	}
}

// cleanup removes expired entries every 30 seconds.
func (cs *ChallengeStore) cleanup() {
	ticker := time.NewTicker(challengeCleanup)
	defer ticker.Stop()
	for {
		select {
		case <-cs.done:
			return
		case <-ticker.C:
			cs.mu.Lock()
			now := cs.nowFunc()
			var newOrder []string
			for _, token := range cs.order {
				if e, ok := cs.entries[token]; ok {
					if now.Sub(e.createdAt) > challengeTTL {
						delete(cs.entries, token)
					} else {
						newOrder = append(newOrder, token)
					}
				}
			}
			cs.order = newOrder
			cs.mu.Unlock()
		}
	}
}

// ---------------------------------------------------------------------------
// WebAuthnUser — implements webauthn.User interface for the single admin user.
// ---------------------------------------------------------------------------

// WebAuthnUser wraps the admin user identity for use with the go-webauthn library.
type WebAuthnUser struct {
	username    string
	displayName string
	credentials []webauthn.Credential
}

// NewWebAuthnUser creates a WebAuthnUser from config values and stored credentials.
// displayName falls back to origin hostname → username if empty.
func NewWebAuthnUser(username, displayName, origin string, creds []storage.WebAuthnCredential) *WebAuthnUser {
	if displayName == "" {
		if u, err := url.Parse(origin); err == nil && u.Host != "" {
			displayName = u.Hostname()
		} else {
			displayName = username
		}
	}
	waCreds := make([]webauthn.Credential, 0, len(creds))
	for _, c := range creds {
		waCreds = append(waCreds, storageCredToWebAuthn(c))
	}
	return &WebAuthnUser{
		username:    username,
		displayName: displayName,
		credentials: waCreds,
	}
}

// WebAuthnID returns a deterministic byte ID derived from the username.
func (u *WebAuthnUser) WebAuthnID() []byte { return []byte(u.username) }

// WebAuthnName returns the admin username.
func (u *WebAuthnUser) WebAuthnName() string { return u.username }

// WebAuthnDisplayName returns the human-readable display name.
func (u *WebAuthnUser) WebAuthnDisplayName() string { return u.displayName }

// WebAuthnCredentials returns all registered passkey credentials.
func (u *WebAuthnUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

// WebAuthnIcon is deprecated in the spec — return empty string.
func (u *WebAuthnUser) WebAuthnIcon() string { return "" }

// storageCredToWebAuthn converts a storage.WebAuthnCredential to webauthn.Credential.
func storageCredToWebAuthn(c storage.WebAuthnCredential) webauthn.Credential {
	// Parse transport hints from JSON array string.
	var transports []protocol.AuthenticatorTransport
	if c.Transport != "" && c.Transport != "[]" {
		inner := c.Transport
		if len(inner) > 2 {
			inner = inner[1 : len(inner)-1]
			for _, part := range splitTransport(inner) {
				transports = append(transports, protocol.AuthenticatorTransport(part))
			}
		}
	}

	flags := protocol.AuthenticatorFlags(c.Flags)

	// Decode credential ID from base64url storage format to raw bytes.
	credIDBytes, err := base64.RawURLEncoding.DecodeString(c.ID)
	if err != nil {
		// Fallback: use raw string bytes if decode fails (legacy data).
		log.Printf("WARNING: failed to decode credential ID %q: %v", c.ID, err)
		credIDBytes = []byte(c.ID)
	}

	return webauthn.Credential{
		ID:              credIDBytes,
		PublicKey:       c.PublicKey,
		AttestationType: c.AttestationType,
		Transport:       transports,
		Flags: webauthn.CredentialFlags{
			UserPresent:    flags.UserPresent(),
			UserVerified:   flags.UserVerified(),
			BackupEligible: flags.HasBackupEligible(),
			BackupState:    flags.HasBackupState(),
		},
		Authenticator: webauthn.Authenticator{
			AAGUID:    c.AAGUID,
			SignCount: c.SignCount,
		},
	}
}

// splitTransport splits a JSON-quoted comma-separated transport string.
func splitTransport(s string) []string {
	var result []string
	for _, part := range splitComma(s) {
		// Strip surrounding quotes.
		part = trimQuotes(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func splitComma(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func trimQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// ---------------------------------------------------------------------------
// SQLiteCredentialCounter — real implementation of WebAuthnCredentialCounter.
// ---------------------------------------------------------------------------

// SQLiteCredentialCounter wraps storage.DB and implements WebAuthnCredentialCounter.
type SQLiteCredentialCounter struct {
	db *storage.DB
}

// NewSQLiteCredentialCounter creates a counter backed by SQLite.
func NewSQLiteCredentialCounter(db *storage.DB) *SQLiteCredentialCounter {
	return &SQLiteCredentialCounter{db: db}
}

// CountWebAuthnCredentials returns the number of registered passkeys, or 0 on error.
func (c *SQLiteCredentialCounter) CountWebAuthnCredentials() int {
	count, err := c.db.CountCredentials()
	if err != nil {
		log.Printf("WARNING: CountWebAuthnCredentials: %v", err)
		return 0
	}
	return count
}



