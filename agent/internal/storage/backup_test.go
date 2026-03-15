package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackup_CreatesValidBackup(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}

	// Insert a peer to verify backup contains data.
	_, err = db.conn.Exec(
		`INSERT INTO peers (public_key, friendly_name, allowed_ips, enabled, auto_discovered, created_at)
		 VALUES ('testkey123', 'test-peer', '["10.0.0.1/32"]', 1, 0, datetime('now'))`)
	if err != nil {
		t.Fatalf("insert peer: %v", err)
	}

	// Run backup.
	if err := db.Backup(dbPath); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	db.Close()

	// Verify backup file exists.
	bakPath := dbPath + ".bak"
	if _, err := os.Stat(bakPath); err != nil {
		t.Fatalf("backup file should exist: %v", err)
	}

	// Verify backup is a valid DB with the peer data.
	bakDB, err := NewDB(bakPath)
	if err != nil {
		t.Fatalf("NewDB on backup: %v", err)
	}
	defer bakDB.Close()

	var name string
	err = bakDB.conn.QueryRow("SELECT friendly_name FROM peers WHERE public_key = 'testkey123'").Scan(&name)
	if err != nil {
		t.Fatalf("query backup: %v", err)
	}
	if name != "test-peer" {
		t.Errorf("expected 'test-peer', got %q", name)
	}
}

func TestRecoverDB_HealthyDB_NoRecovery(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	db.Close()

	source, err := RecoverDB(dbPath, "", nil)
	if err != nil {
		t.Fatalf("RecoverDB: %v", err)
	}
	if source != "" {
		t.Errorf("expected no recovery for healthy DB, got %q", source)
	}
}

func TestRecoverDB_CorruptDB_RecoverFromBackup(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Create healthy DB and backup.
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	_, _ = db.conn.Exec(
		`INSERT INTO peers (public_key, friendly_name, allowed_ips, enabled, auto_discovered, created_at)
		 VALUES ('backupkey', 'backup-peer', '["10.0.0.1/32"]', 1, 0, datetime('now'))`)
	if err := db.Backup(dbPath); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	db.Close()

	// Corrupt the main DB.
	os.WriteFile(dbPath, []byte("CORRUPTED DATA GARBAGE"), 0600)

	source, err := RecoverDB(dbPath, "", nil)
	if err != nil {
		t.Fatalf("RecoverDB: %v", err)
	}
	if source != "backup" {
		t.Errorf("expected recovery from 'backup', got %q", source)
	}

	// Verify recovered DB has the peer.
	recovered, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("open recovered: %v", err)
	}
	defer recovered.Close()

	var name string
	err = recovered.conn.QueryRow("SELECT friendly_name FROM peers WHERE public_key = 'backupkey'").Scan(&name)
	if err != nil {
		t.Fatalf("query recovered: %v", err)
	}
	if name != "backup-peer" {
		t.Errorf("expected 'backup-peer', got %q", name)
	}
}

func TestRecoverDB_CorruptBoth_RecoverFromConf(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Create initial DB so the file exists.
	db, _ := NewDB(dbPath)
	db.Close()

	// Corrupt both DB and backup.
	os.WriteFile(dbPath, []byte("CORRUPT"), 0600)
	os.WriteFile(dbPath+".bak", []byte("CORRUPT"), 0600)

	mockParse := func(path string) (map[string]PeerMeta, error) {
		return map[string]PeerMeta{
			"confkey123": {Name: "conf-peer", Notes: "recovered from conf"},
		}, nil
	}

	source, err := RecoverDB(dbPath, "/fake/wg0.conf", mockParse)
	if err != nil {
		t.Fatalf("RecoverDB: %v", err)
	}
	if source != "conf" {
		t.Errorf("expected recovery from 'conf', got %q", source)
	}

	// Verify recovered DB has the conf peer.
	recovered, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("open recovered: %v", err)
	}
	defer recovered.Close()

	var name string
	err = recovered.conn.QueryRow("SELECT friendly_name FROM peers WHERE public_key = 'confkey123'").Scan(&name)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if name != "conf-peer" {
		t.Errorf("expected 'conf-peer', got %q", name)
	}
}

func TestRecoverDB_CorruptAll_CleanStart(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Create and corrupt DB, no backup.
	db, _ := NewDB(dbPath)
	db.Close()
	os.WriteFile(dbPath, []byte("CORRUPT"), 0600)

	source, err := RecoverDB(dbPath, "", nil)
	if err != nil {
		t.Fatalf("RecoverDB: %v", err)
	}
	if source != "clean" {
		t.Errorf("expected 'clean' recovery, got %q", source)
	}

	// DB file should be removed (NewDB will create fresh on next open).
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Error("expected DB file to be removed for clean start")
	}
}

func TestRecoverDB_NoDB_FirstStart(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "nonexistent.db")

	source, err := RecoverDB(dbPath, "", nil)
	if err != nil {
		t.Fatalf("RecoverDB: %v", err)
	}
	if source != "" {
		t.Errorf("expected empty source for first start, got %q", source)
	}
}

