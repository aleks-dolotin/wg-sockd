package storage

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"
)

// BackupLoop runs hourly database backups. First backup occurs after
// initialDelay (allows WAL to settle). Blocks until ctx is cancelled.
func (db *DB) BackupLoop(ctx context.Context, dbPath string, initialDelay time.Duration) {
	// Wait before first backup.
	select {
	case <-ctx.Done():
		return
	case <-time.After(initialDelay):
	}

	// First backup.
	if err := db.Backup(dbPath); err != nil {
		log.Printf("WARN: database backup failed: %v", err)
	} else {
		log.Println("INFO: database backup completed")
	}

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := db.Backup(dbPath); err != nil {
				log.Printf("WARN: database backup failed: %v", err)
			} else {
				log.Println("INFO: database backup completed")
			}
		}
	}
}

// Backup creates a consistent backup of the database at dbPath → dbPath.bak.
// Performs a WAL checkpoint before copying to ensure consistency.
//
// Design decision: We use TRUNCATE (blocking) rather than PASSIVE (non-blocking)
// because PASSIVE may leave uncheckpointed WAL pages, leading to an inconsistent
// backup. The TRUNCATE checkpoint blocks writers for ~50–100ms once per hour,
// which is acceptable for a single-instance daemon. If concurrent write latency
// becomes a concern, consider PASSIVE + sqlite3_backup API instead.
func (db *DB) Backup(dbPath string) error {
	// WAL checkpoint to flush pending writes.
	if _, err := db.conn.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return fmt.Errorf("WAL checkpoint: %w", err)
	}

	bakPath := dbPath + ".bak"
	return copyFileSync(dbPath, bakPath)
}

// copyFileSync copies src to dst with fsync for durability.
func copyFileSync(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening source: %w", err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("creating destination: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copying data: %w", err)
	}

	if err := out.Sync(); err != nil {
		return fmt.Errorf("fsync: %w", err)
	}

	return nil
}

// RecoverDB attempts to recover a corrupted database using a three-level
// recovery chain. Returns the recovery source for health reporting:
//   - "backup" if recovered from .db.bak
//   - "conf" if recovered from wg0.conf comments
//   - "clean" if a fresh database was created
//   - "" if no recovery was needed
//
// confPath is the path to wg0.conf for Level 2 recovery.
// parseComments extracts peer metadata from conf comments.
func RecoverDB(dbPath string, confPath string, parseComments func(string) (map[string]PeerMeta, error)) (string, error) {
	// Check if DB exists at all — not corruption, just first start.
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return "", nil
	}

	// Try opening and checking integrity.
	if isDBHealthy(dbPath) {
		return "", nil // No recovery needed.
	}

	log.Println("WARN: database corruption detected, starting recovery chain")

	// Level 1: Try backup.
	bakPath := dbPath + ".bak"
	if _, err := os.Stat(bakPath); err == nil {
		log.Println("INFO: attempting recovery from backup...")
		if err := copyFileSync(bakPath, dbPath); err == nil {
			if isDBHealthy(dbPath) {
				log.Println("WARN: recovered from backup (up to 1 hour metadata loss)")
				return "backup", nil
			}
		}
		log.Println("WARN: backup recovery failed, trying conf comments...")
	}

	// Level 2: Parse conf comments and create fresh DB.
	if parseComments != nil {
		meta, err := parseComments(confPath)
		if err == nil && len(meta) > 0 {
			log.Printf("INFO: attempting recovery from conf comments (%d peers found)...", len(meta))
			// Remove corrupted DB.
			os.Remove(dbPath)
			os.Remove(dbPath + "-wal")
			os.Remove(dbPath + "-shm")

			newDB, err := NewDB(dbPath)
			if err != nil {
				log.Printf("WARN: failed to create fresh DB for conf recovery: %v", err)
			} else {
				var recovered int
				for pubKey, pm := range meta {
					name := pm.Name
					if name == "" {
						short := pubKey
						if len(short) > 8 {
							short = short[:8]
						}
						name = "recovered-" + short
					}
					if err := newDB.InsertRecoveredPeer(pubKey, name, pm.Notes); err != nil {
						log.Printf("WARN: failed to recover peer %s: %v", pubKey[:8], err)
					} else {
						recovered++
					}
				}
				newDB.Close()
				log.Printf("INFO: recovered %d of %d peers from conf comments", recovered, len(meta))
				if recovered > 0 {
					log.Println("WARN: recovered from conf comments (profile assignments lost, metadata preserved)")
					return "conf", nil
				}
				// All inserts failed — fall through to Level 3.
				log.Println("WARN: all peer inserts failed during conf recovery, falling through to clean start")
			}
		}
	}

	// Level 3: Clean start.
	log.Println("WARN: clean database created (all metadata lost, peers recovered from kernel state)")
	os.Remove(dbPath)
	os.Remove(dbPath + "-wal")
	os.Remove(dbPath + "-shm")
	return "clean", nil
}

// PeerMeta holds metadata extracted from conf comment lines for recovery.
type PeerMeta struct {
	Name  string
	Notes string
}

// isDBHealthy checks if a SQLite database passes integrity check.
func isDBHealthy(dbPath string) bool {
	db, err := NewDB(dbPath)
	if err != nil {
		return false
	}
	defer db.Close()

	var result string
	if err := db.conn.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil {
		return false
	}
	return result == "ok"
}

// InsertRecoveredPeer inserts a peer recovered from conf comments.
// Uses explicit UTC timestamp to avoid timezone mismatch with Go code.
func (db *DB) InsertRecoveredPeer(pubKey, friendlyName, notes string) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err := db.conn.Exec(
		`INSERT OR IGNORE INTO peers (public_key, friendly_name, allowed_ips, enabled, auto_discovered, notes, created_at)
		 VALUES (?, ?, '[]', 1, 0, ?, ?)`,
		pubKey, friendlyName, notes, now,
	)
	return err
}

