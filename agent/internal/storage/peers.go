package storage

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// scanPeerColumns is the canonical column list for all peer SELECT queries.
const scanPeerColumns = `id, public_key, friendly_name, allowed_ips, profile, enabled, auto_discovered, created_at, notes, endpoint, persistent_keepalive, client_dns, client_mtu, client_address, last_seen_endpoint, preshared_key, client_allowed_ips`

// scanPeer scans a peer row into a Peer struct.
func scanPeer(scanner interface{ Scan(dest ...any) error }) (Peer, error) {
	var p Peer
	var pka sql.NullInt64
	var mtu sql.NullInt64
	if err := scanner.Scan(&p.ID, &p.PublicKey, &p.FriendlyName, &p.AllowedIPs, &p.Profile,
		&p.Enabled, &p.AutoDiscovered, &p.CreatedAt, &p.Notes,
		&p.Endpoint, &pka, &p.ClientDNS, &mtu, &p.ClientAddress, &p.LastSeenEndpoint,
		&p.PresharedKey, &p.ClientAllowedIPs); err != nil {
		return Peer{}, err
	}
	if pka.Valid {
		v := int(pka.Int64)
		p.PersistentKeepalive = &v
	}
	if mtu.Valid {
		v := int(mtu.Int64)
		p.ClientMTU = &v
	}
	return p, nil
}

// Peer represents a peer record in the database.
type Peer struct {
	ID                 int64
	PublicKey          string
	FriendlyName       string
	AllowedIPs         string
	Profile            *string // nullable
	Enabled            bool
	AutoDiscovered     bool
	CreatedAt          time.Time
	Notes              string
	Endpoint           string // configured endpoint for server wg0.conf (empty = not set)
	PersistentKeepalive *int  // server-side PKA for wg0.conf; nil = inherit from profile/global
	ClientDNS          string // client download conf DNS (empty = inherit)
	ClientMTU          *int   // client download conf MTU; nil = inherit
	ClientAddress      string // client's VPN IP (CIDR) for [Interface] Address in client conf
	LastSeenEndpoint   string // runtime endpoint from kernel — informational, not in wg0.conf
	PresharedKey       string // base64-encoded 32-byte PSK; empty = no PSK
	ClientAllowedIPs   string // comma-separated CIDRs for client conf [Peer] AllowedIPs; empty = inherit
}

// PeerUpdate holds optional fields for partial peer updates.
//
// Pointer semantics:
//   - nil           → don't update this field
//   - *string/etc   → set to this value
//
// Pointer-to-pointer semantics (for nullable DB columns):
//   - nil           → don't update
//   - non-nil → nil → set DB column to NULL (inherit from profile/global)
//   - non-nil → val → set DB column to val
type PeerUpdate struct {
	FriendlyName       *string
	AllowedIPs         *string
	Profile            **string // pointer-to-pointer for nullable
	Enabled            *bool
	Notes              *string
	Endpoint           *string
	PersistentKeepalive **int  // pointer-to-pointer: nil=skip, *nil=set NULL, **val=set value
	ClientDNS          *string
	ClientMTU          **int  // pointer-to-pointer: nil=skip, *nil=set NULL, **val=set value
	ClientAddress      *string
	PresharedKey       *string // nil=skip, ""=clear, "base64..."=set
	ClientAllowedIPs   *string // nil=skip, ""=clear, "cidr,cidr"=set
}

// ListPeers returns all peers ordered by created_at.
func (db *DB) ListPeers() ([]Peer, error) {
	rows, err := db.conn.Query(`
		SELECT ` + scanPeerColumns + `
		FROM peers
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("listing peers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var peers []Peer
	for rows.Next() {
		p, err := scanPeer(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning peer: %w", err)
		}
		peers = append(peers, p)
	}
	return peers, rows.Err()
}

// GetPeerByPubKey returns a single peer by public key.
// Returns sql.ErrNoRows if not found.
func (db *DB) GetPeerByPubKey(pubKey string) (*Peer, error) {
	row := db.conn.QueryRow(`
		SELECT `+scanPeerColumns+`
		FROM peers
		WHERE public_key = ?
	`, pubKey)
	p, err := scanPeer(row)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetPeerByID returns a single peer by ID.
// Returns sql.ErrNoRows if not found.
func (db *DB) GetPeerByID(id int64) (*Peer, error) {
	row := db.conn.QueryRow(`
		SELECT `+scanPeerColumns+`
		FROM peers
		WHERE id = ?
	`, id)
	p, err := scanPeer(row)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// CreatePeer inserts a new peer and returns the generated ID.
func (db *DB) CreatePeer(p *Peer) (int64, error) {
	result, err := db.conn.Exec(`
		INSERT INTO peers (public_key, friendly_name, allowed_ips, profile, enabled, auto_discovered, notes, endpoint, persistent_keepalive, client_dns, client_mtu, client_address, preshared_key, client_allowed_ips)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, p.PublicKey, p.FriendlyName, p.AllowedIPs, p.Profile, p.Enabled, p.AutoDiscovered, p.Notes,
		p.Endpoint, p.PersistentKeepalive, p.ClientDNS, p.ClientMTU, p.ClientAddress,
		p.PresharedKey, p.ClientAllowedIPs)
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
	if u.Endpoint != nil {
		sets = append(sets, "endpoint = ?")
		args = append(args, *u.Endpoint)
	}
	if u.PersistentKeepalive != nil {
		sets = append(sets, "persistent_keepalive = ?")
		args = append(args, *u.PersistentKeepalive) // *int or nil → NULL
	}
	if u.ClientDNS != nil {
		sets = append(sets, "client_dns = ?")
		args = append(args, *u.ClientDNS)
	}
	if u.ClientMTU != nil {
		sets = append(sets, "client_mtu = ?")
		args = append(args, *u.ClientMTU) // *int or nil → NULL
	}
	if u.ClientAddress != nil {
		sets = append(sets, "client_address = ?")
		args = append(args, *u.ClientAddress)
	}
	if u.PresharedKey != nil {
		sets = append(sets, "preshared_key = ?")
		args = append(args, *u.PresharedKey)
	}
	if u.ClientAllowedIPs != nil {
		sets = append(sets, "client_allowed_ips = ?")
		args = append(args, *u.ClientAllowedIPs)
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
// lastSeenEndpoint stores the runtime endpoint observed in kernel (informational only).
// persistentKeepalive preserves the value observed in the kernel.
func (db *DB) UpsertPeerFromReconcile(pubKey, friendlyName string, autoDiscovered, enabled bool, lastSeenEndpoint string, persistentKeepalive *int) error {
	_, err := db.conn.Exec(`
		INSERT OR IGNORE INTO peers (public_key, friendly_name, auto_discovered, enabled, last_seen_endpoint, persistent_keepalive)
		VALUES (?, ?, ?, ?, ?, ?)
	`, pubKey, friendlyName, autoDiscovered, enabled, lastSeenEndpoint, persistentKeepalive)
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
	defer func() { _ = tx.Rollback() }()

	ids := make([]int64, 0, len(peers))
	for _, p := range peers {
		result, err := tx.Exec(`
			INSERT INTO peers (public_key, friendly_name, allowed_ips, profile, enabled, auto_discovered, notes, endpoint, persistent_keepalive, client_dns, client_mtu, client_address, preshared_key, client_allowed_ips)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, p.PublicKey, p.FriendlyName, p.AllowedIPs, p.Profile, p.Enabled, p.AutoDiscovered, p.Notes,
			p.Endpoint, p.PersistentKeepalive, p.ClientDNS, p.ClientMTU, p.ClientAddress,
			p.PresharedKey, p.ClientAllowedIPs)
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
	defer func() { _ = rows.Close() }()

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

// IsClientAddressTaken checks if a client_address is already assigned to another peer.
// excludePubKey is excluded from the check (used for updates — the peer itself shouldn't conflict).
func (db *DB) IsClientAddressTaken(address string, excludePubKey string) (bool, error) {
	if address == "" {
		return false, nil
	}
	var count int
	err := db.conn.QueryRow(
		"SELECT COUNT(*) FROM peers WHERE client_address = ? AND public_key != ?",
		address, excludePubKey).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("checking client_address uniqueness: %w", err)
	}
	return count > 0, nil
}

// UpdateLastSeenEndpoints batch-updates last_seen_endpoint for multiple peers in a single transaction.
// updates is a map of public_key → new last_seen_endpoint value.
// Only changed values should be passed (delta-only).
func (db *DB) UpdateLastSeenEndpoints(updates map[string]string) error {
	if len(updates) == 0 {
		return nil
	}

	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning last_seen_endpoint batch: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare("UPDATE peers SET last_seen_endpoint = ? WHERE public_key = ?")
	if err != nil {
		return fmt.Errorf("preparing last_seen_endpoint update: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for pubKey, endpoint := range updates {
		if _, err := stmt.Exec(endpoint, pubKey); err != nil {
			return fmt.Errorf("updating last_seen_endpoint for %s: %w", pubKey, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing last_seen_endpoint batch: %w", err)
	}
	return nil
}

// CountPeersWithEmptyClientAddress returns the number of peers with empty client_address.
func (db *DB) CountPeersWithEmptyClientAddress() (int, error) {
	var count int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM peers WHERE client_address = ''").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting peers with empty client_address: %w", err)
	}
	return count, nil
}

