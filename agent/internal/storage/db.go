// Package storage provides the SQLite-based persistence layer for wg-sockd.
package storage

import (
	"database/sql"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
)

// schema is the canonical database schema. Applied once on first start.
// No migration system — database is created from scratch.
const schema = `
CREATE TABLE IF NOT EXISTS peers (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	public_key TEXT UNIQUE NOT NULL,
	friendly_name TEXT NOT NULL DEFAULT '',
	allowed_ips TEXT NOT NULL DEFAULT '',
	profile TEXT,
	enabled BOOLEAN NOT NULL DEFAULT 1,
	auto_discovered BOOLEAN NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	notes TEXT NOT NULL DEFAULT '',
	endpoint TEXT NOT NULL DEFAULT '',
	persistent_keepalive INTEGER,
	client_dns TEXT NOT NULL DEFAULT '',
	client_mtu INTEGER,
	client_address TEXT NOT NULL DEFAULT '',
	last_seen_endpoint TEXT NOT NULL DEFAULT '',
	preshared_key TEXT NOT NULL DEFAULT '',
	client_allowed_ips TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_peers_public_key ON peers(public_key);
CREATE UNIQUE INDEX IF NOT EXISTS idx_peers_client_address ON peers(client_address) WHERE client_address != '';

CREATE TABLE IF NOT EXISTS profiles (
	name TEXT PRIMARY KEY,
	allowed_ips TEXT NOT NULL DEFAULT '[]',
	exclude_ips TEXT NOT NULL DEFAULT '[]',
	description TEXT NOT NULL DEFAULT '',
	is_default BOOLEAN NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	persistent_keepalive INTEGER,
	client_dns TEXT NOT NULL DEFAULT '',
	client_mtu INTEGER,
	use_preshared_key BOOLEAN NOT NULL DEFAULT 0
);

CREATE TRIGGER IF NOT EXISTS fk_peers_profile_insert
BEFORE INSERT ON peers
WHEN NEW.profile IS NOT NULL
BEGIN
	SELECT RAISE(ABORT, 'FOREIGN KEY constraint failed: peers.profile references profiles.name')
	WHERE NOT EXISTS (SELECT 1 FROM profiles WHERE name = NEW.profile);
END;

CREATE TRIGGER IF NOT EXISTS fk_peers_profile_update
BEFORE UPDATE OF profile ON peers
WHEN NEW.profile IS NOT NULL
BEGIN
	SELECT RAISE(ABORT, 'FOREIGN KEY constraint failed: peers.profile references profiles.name')
	WHERE NOT EXISTS (SELECT 1 FROM profiles WHERE name = NEW.profile);
END;

CREATE TRIGGER IF NOT EXISTS fk_profiles_delete
BEFORE DELETE ON profiles
BEGIN
	SELECT RAISE(ABORT, 'FOREIGN KEY constraint failed: profiles.name is referenced by peers')
	WHERE EXISTS (SELECT 1 FROM peers WHERE profile = OLD.name);
END;

CREATE TABLE IF NOT EXISTS webauthn_credentials (
	id TEXT PRIMARY KEY,
	public_key BLOB NOT NULL,
	attestation_type TEXT NOT NULL DEFAULT '',
	aaguid BLOB NOT NULL DEFAULT '',
	transport TEXT NOT NULL DEFAULT '',
	flags INTEGER NOT NULL DEFAULT 0,
	sign_count INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	last_used_at DATETIME,
	friendly_name TEXT NOT NULL DEFAULT ''
);
`

// DB wraps a sql.DB connection.
type DB struct {
	conn *sql.DB
}

// NewDB opens a SQLite database at the given path, enables WAL mode,
// runs integrity check, and creates tables if they don't exist.
// Use ":memory:" for in-memory databases (testing).
func NewDB(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Single connection for SQLite — avoids locking issues.
	conn.SetMaxOpenConns(1)

	// Enable WAL mode for better concurrent read performance.
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("enabling WAL mode: %w", err)
	}

	// Set busy timeout so concurrent operations queue briefly instead of
	// immediately failing with "database is locked" (default is 0ms).
	if _, err := conn.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("setting busy_timeout: %w", err)
	}

	// Enable foreign keys.
	if _, err := conn.Exec("PRAGMA foreign_keys=ON"); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	// Integrity check at startup — log warning, don't fail.
	var result string
	if err := conn.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil {
		log.Printf("WARNING: integrity check failed: %v", err)
	} else if result != "ok" {
		log.Printf("WARNING: integrity check returned: %s", result)
	}

	db := &DB{conn: conn}

	if _, err := conn.Exec(schema); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("creating schema: %w", err)
	}

	return db, nil
}

// Close closes the underlying database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// Conn returns the underlying sql.DB for direct queries.
func (db *DB) Conn() *sql.DB {
	return db.conn
}
