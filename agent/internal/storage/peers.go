package storage

import (
	"database/sql"
	"fmt"
	"strings"
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

	query := "UPDATE peers SET " + strings.Join(sets, ", ") + " WHERE public_key = ?"
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
// The enabled parameter controls whether the peer starts enabled or disabled.
func (db *DB) UpsertPeerFromReconcile(pubKey, friendlyName string, autoDiscovered, enabled bool) error {
	_, err := db.conn.Exec(`
		INSERT OR IGNORE INTO peers (public_key, friendly_name, auto_discovered, enabled)
		VALUES (?, ?, ?, ?)
	`, pubKey, friendlyName, autoDiscovered, enabled)
	if err != nil {
		return fmt.Errorf("upserting peer from reconcile: %w", err)
	}
	return nil
}

// ApprovePeer sets a peer as enabled and clears auto_discovered flag.
// Returns sql.ErrNoRows if peer not found.
func (db *DB) ApprovePeer(pubKey string) error {
	result, err := db.conn.Exec(
		"UPDATE peers SET enabled = 1, auto_discovered = 0 WHERE public_key = ?", pubKey)
	if err != nil {
		return fmt.Errorf("approving peer: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// CreatePeersBatch inserts multiple peers in a single transaction.
// Returns the list of generated IDs in order.
func (db *DB) CreatePeersBatch(peers []*Peer) ([]int64, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning batch transaction: %w", err)
	}
	defer tx.Rollback()

	ids := make([]int64, 0, len(peers))
	for _, p := range peers {
		result, err := tx.Exec(`
			INSERT INTO peers (public_key, friendly_name, allowed_ips, profile, enabled, auto_discovered, notes)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, p.PublicKey, p.FriendlyName, p.AllowedIPs, p.Profile, p.Enabled, p.AutoDiscovered, p.Notes)
		if err != nil {
			return nil, fmt.Errorf("inserting peer %s: %w", p.PublicKey, err)
		}
		id, _ := result.LastInsertId()
		ids = append(ids, id)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing batch: %w", err)
	}
	return ids, nil
}

// CountPeers returns the total number of peers.
func (db *DB) CountPeers() (int, error) {
	var count int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM peers").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting peers: %w", err)
	}
	return count, nil
}

// CountPeersPerProfile returns a map of profile name → peer count
// for all peers that have a non-NULL profile assigned.
func (db *DB) CountPeersPerProfile() (map[string]int, error) {
	rows, err := db.conn.Query(
		"SELECT profile, COUNT(*) FROM peers WHERE profile IS NOT NULL GROUP BY profile")
	if err != nil {
		return nil, fmt.Errorf("counting peers per profile: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			return nil, fmt.Errorf("scanning peer profile count: %w", err)
		}
		counts[name] = count
	}
	return counts, rows.Err()
}

// UpdatePeerPublicKey changes a peer's public key. Used for key rotation.
// Returns sql.ErrNoRows if the peer is not found.
func (db *DB) UpdatePeerPublicKey(oldPubKey, newPubKey string) error {
	result, err := db.conn.Exec("UPDATE peers SET public_key = ? WHERE public_key = ?", newPubKey, oldPubKey)
	if err != nil {
		return fmt.Errorf("updating peer public key: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

