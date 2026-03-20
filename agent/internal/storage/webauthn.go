package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// WebAuthnCredential represents a registered passkey credential in the database.
type WebAuthnCredential struct {
	ID              string
	PublicKey       []byte
	AttestationType string
	AAGUID          []byte
	Transport       string // JSON array of transport hints
	Flags           uint8
	SignCount        uint32
	CreatedAt       time.Time
	LastUsedAt      *time.Time // nil if never used after initial registration
	FriendlyName    string
}

// InsertCredential inserts a new WebAuthn credential.
// Returns an error that can be checked for UNIQUE constraint violation (duplicate).
func (db *DB) InsertCredential(id string, publicKey []byte, attestationType string, aaguid []byte, transport string, flags uint8, signCount uint32, friendlyName string) error {
	_, err := db.conn.Exec(`
		INSERT INTO webauthn_credentials
			(id, public_key, attestation_type, aaguid, transport, flags, sign_count, friendly_name)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, id, publicKey, attestationType, aaguid, transport, int64(flags), int64(signCount), friendlyName)
	if err != nil {
		return fmt.Errorf("inserting webauthn credential: %w", err)
	}
	return nil
}

// ListCredentials returns all registered credentials ordered by created_at DESC.
func (db *DB) ListCredentials() ([]WebAuthnCredential, error) {
	rows, err := db.conn.Query(`
		SELECT id, public_key, attestation_type, aaguid, transport, flags, sign_count,
		       created_at, last_used_at, friendly_name
		FROM webauthn_credentials
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("listing webauthn credentials: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var creds []WebAuthnCredential
	for rows.Next() {
		c, err := scanCredential(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning webauthn credential: %w", err)
		}
		creds = append(creds, c)
	}
	return creds, rows.Err()
}

// GetCredentialByID returns a single credential by its ID.
// Returns sql.ErrNoRows if not found.
func (db *DB) GetCredentialByID(id string) (*WebAuthnCredential, error) {
	row := db.conn.QueryRow(`
		SELECT id, public_key, attestation_type, aaguid, transport, flags, sign_count,
		       created_at, last_used_at, friendly_name
		FROM webauthn_credentials
		WHERE id = ?
	`, id)
	c, err := scanCredential(row)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// DeleteCredential removes a credential by ID.
// Returns sql.ErrNoRows if the credential was not found.
func (db *DB) DeleteCredential(id string) error {
	result, err := db.conn.Exec("DELETE FROM webauthn_credentials WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting webauthn credential: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// UpdateSignCount updates sign_count and last_used_at for a credential.
func (db *DB) UpdateSignCount(id string, signCount uint32, lastUsedAt time.Time) error {
	_, err := db.conn.Exec(`
		UPDATE webauthn_credentials
		SET sign_count = ?, last_used_at = ?
		WHERE id = ?
	`, int64(signCount), lastUsedAt, id)
	if err != nil {
		return fmt.Errorf("updating sign count for webauthn credential %q: %w", id, err)
	}
	return nil
}

// CountCredentials returns the total number of registered credentials.
func (db *DB) CountCredentials() (int, error) {
	var count int
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM webauthn_credentials").Scan(&count); err != nil {
		return 0, fmt.Errorf("counting webauthn credentials: %w", err)
	}
	return count, nil
}

// scanCredential scans a credential row.
func scanCredential(scanner interface{ Scan(dest ...any) error }) (WebAuthnCredential, error) {
	var c WebAuthnCredential
	var flags int64
	var signCount int64
	var lastUsedAt sql.NullTime
	if err := scanner.Scan(
		&c.ID, &c.PublicKey, &c.AttestationType, &c.AAGUID, &c.Transport,
		&flags, &signCount, &c.CreatedAt, &lastUsedAt, &c.FriendlyName,
	); err != nil {
		return WebAuthnCredential{}, err
	}
	c.Flags = uint8(flags)
	c.SignCount = uint32(signCount)
	if lastUsedAt.Valid {
		t := lastUsedAt.Time
		c.LastUsedAt = &t
	}
	return c, nil
}

