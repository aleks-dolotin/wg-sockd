// Package storage provides the SQLite-based persistence layer for wg-sockd.
package storage

import (
	"database/sql"
	"embed"
	"fmt"
	"log"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB wraps a sql.DB connection with migration support.
type DB struct {
	conn *sql.DB
}

// NewDB opens a SQLite database at the given path, enables WAL mode,
// runs integrity check, and applies pending migrations.
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
		conn.Close()
		return nil, fmt.Errorf("enabling WAL mode: %w", err)
	}

	// Set busy timeout so concurrent operations queue briefly instead of
	// immediately failing with "database is locked" (default is 0ms).
	if _, err := conn.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("setting busy_timeout: %w", err)
	}

	// Enable foreign keys.
	if _, err := conn.Exec("PRAGMA foreign_keys=ON"); err != nil {
		conn.Close()
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

	if err := db.runMigrations(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
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

// runMigrations applies all pending .sql migrations in order.
func (db *DB) runMigrations() error {
	// Create schema_version table if not exists.
	_, err := db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			version TEXT PRIMARY KEY,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("creating schema_version table: %w", err)
	}

	// Read all migration files.
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("reading migrations directory: %w", err)
	}

	// Sort by filename to ensure order.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		name := entry.Name()

		// Check if already applied.
		var count int
		err := db.conn.QueryRow("SELECT COUNT(*) FROM schema_version WHERE version = ?", name).Scan(&count)
		if err != nil {
			return fmt.Errorf("checking migration %s: %w", name, err)
		}
		if count > 0 {
			continue
		}

		// Read and execute migration.
		data, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", name, err)
		}

		if _, err := db.conn.Exec(string(data)); err != nil {
			return fmt.Errorf("executing migration %s: %w", name, err)
		}

		// Record as applied.
		if _, err := db.conn.Exec("INSERT INTO schema_version (version) VALUES (?)", name); err != nil {
			return fmt.Errorf("recording migration %s: %w", name, err)
		}

		log.Printf("Applied migration: %s", name)
	}

	return nil
}
