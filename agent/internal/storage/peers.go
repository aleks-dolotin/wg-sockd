package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// Peer represents a peer record in the database.
type Peer struct {
	ID             int64
	PublicKey      string
	FriendlyName   string
	AllowedIPs     string
	Profile        *string // nullable
	Enabled        bool
	AutoDiscovered bool
	CreatedAt      time.Time
	Notes          string
}

// PeerUpdate holds optional fields for partial peer updates.
type PeerUpdate struct {
	FriendlyName   *string
	AllowedIPs     *string
	Profile        **string // pointer-to-pointer for nullable: nil = don't update, non-nil = set value
	Enabled        *bool
	Notes          *string
}

// ListPeers returns all peers ordered by created_at.
func (db *DB) ListPeers() ([]Peer, error) {
	rows, err := db.conn.Query(`
		SELECT id, public_key, friendly_name, allowed_ips, profile, enabled, auto_discovered, created_at, notes
		FROM peers
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("listing peers: %w", err)
	}
	defer rows.Close()

	var peers []Peer
	for rows.Next() {
		var p Peer
		if err := rows.Scan(&p.ID, &p.PublicKey, &p.FriendlyName, &p.AllowedIPs, &p.Profile, &p.Enabled, &p.AutoDiscovered, &p.CreatedAt, &p.Notes); err != nil {
			return nil, fmt.Errorf("scanning peer: %w", err)
		}
		peers = append(peers, p)
	}
	return peers, rows.Err()
}

// GetPeerByPubKey returns a single peer by public key.
// Returns sql.ErrNoRows if not found.
func (db *DB) GetPeerByPubKey(pubKey string) (*Peer, error) {
	var p Peer
	err := db.conn.QueryRow(`
		SELECT id, public_key, friendly_name, allowed_ips, profile, enabled, auto_discovered, created_at, notes
		FROM peers
		WHERE public_key = ?
	`, pubKey).Scan(&p.ID, &p.PublicKey, &p.FriendlyName, &p.AllowedIPs, &p.Profile, &p.Enabled, &p.AutoDiscovered, &p.CreatedAt, &p.Notes)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetPeerByID returns a single peer by ID.
// Returns sql.ErrNoRows if not found.
func (db *DB) GetPeerByID(id int64) (*Peer, error) {
	var p Peer
	err := db.conn.QueryRow(`
		SELECT id, public_key, friendly_name, allowed_ips, profile, enabled, auto_discovered, created_at, notes
		FROM peers
		WHERE id = ?
	`, id).Scan(&p.ID, &p.PublicKey, &p.FriendlyName, &p.AllowedIPs, &p.Profile, &p.Enabled, &p.AutoDiscovered, &p.CreatedAt, &p.Notes)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// CreatePeer inserts a new peer and returns the generated ID.
func (db *DB) CreatePeer(p *Peer) (int64, error) {
	result, err := db.conn.Exec(`
		INSERT INTO peers (public_key, friendly_name, allowed_ips, profile, enabled, auto_discovered, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, p.PublicKey, p.FriendlyName, p.AllowedIPs, p.Profile, p.Enabled, p.AutoDiscovered, p.Notes)
	if err != nil {
		return 0, fmt.Errorf("creating peer: %w", err)
	}
	return result.LastInsertId()
}

// DeletePeer removes a peer by public key.
func (db *DB) DeletePeer(pubKey string) error {
	result, err := db.conn.Exec("DELETE FROM peers WHERE public_key = ?", pubKey)
	if err != nil {
		return fmt.Errorf("deleting peer: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// UpdatePeer applies a partial update to a peer identified by public key.
func (db *DB) UpdatePeer(pubKey string, u *PeerUpdate) error {
	var sets []string
	var args []any

	if u.FriendlyName != nil {
		sets = append(sets, "friendly_name = ?")
		args = append(args, *u.FriendlyName)
	}
	if u.AllowedIPs != nil {
		sets = append(sets, "allowed_ips = ?")
		args = append(args, *u.AllowedIPs)
	}
	if u.Profile != nil {
		sets = append(sets, "profile = ?")
		args = append(args, *u.Profile)
	}
	if u.Enabled != nil {
		sets = append(sets, "enabled = ?")
		args = append(args, *u.Enabled)
	}
	if u.Notes != nil {
		sets = append(sets, "notes = ?")
		args = append(args, *u.Notes)
	}

	if len(sets) == 0 {
		return nil // nothing to update
	}

	query := "UPDATE peers SET " + joinStrings(sets, ", ") + " WHERE public_key = ?"
	args = append(args, pubKey)

	result, err := db.conn.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("updating peer: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// UpsertPeerFromReconcile inserts a peer if not already present (INSERT OR IGNORE).
// Used by the reconciler to track peers discovered in the kernel.
func (db *DB) UpsertPeerFromReconcile(pubKey, friendlyName string, autoDiscovered bool) error {
	_, err := db.conn.Exec(`
		INSERT OR IGNORE INTO peers (public_key, friendly_name, auto_discovered)
		VALUES (?, ?, ?)
	`, pubKey, friendlyName, autoDiscovered)
	if err != nil {
		return fmt.Errorf("upserting peer from reconcile: %w", err)
	}
	return nil
}

// joinStrings joins a slice of strings with a separator.
func joinStrings(s []string, sep string) string {
	result := ""
	for i, v := range s {
		if i > 0 {
			result += sep
		}
		result += v
	}
	return result
}
