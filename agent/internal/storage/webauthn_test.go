package storage

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

func setupTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := NewDB(":memory:")
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestInsertCredential_Success(t *testing.T) {
	db := setupTestDB(t)

	err := db.InsertCredential("cred1", []byte("pubkey"), "none", []byte("aaguid00"), "[]", 0x01, 0, "MacBook Touch ID")
	if err != nil {
		t.Fatalf("InsertCredential: %v", err)
	}
}

func TestInsertCredential_Duplicate(t *testing.T) {
	db := setupTestDB(t)

	_ = db.InsertCredential("cred1", []byte("pubkey"), "none", []byte{}, "[]", 0, 0, "Key 1")
	err := db.InsertCredential("cred1", []byte("pubkey2"), "none", []byte{}, "[]", 0, 0, "Key 2")
	if err == nil {
		t.Fatal("expected error on duplicate insert, got nil")
	}
	if !strings.Contains(err.Error(), "UNIQUE") {
		t.Errorf("expected UNIQUE error, got: %v", err)
	}
}

func TestListCredentials_Empty(t *testing.T) {
	db := setupTestDB(t)

	creds, err := db.ListCredentials()
	if err != nil {
		t.Fatalf("ListCredentials: %v", err)
	}
	if len(creds) != 0 {
		t.Errorf("expected 0 credentials, got %d", len(creds))
	}
}

func TestListCredentials_Multiple(t *testing.T) {
	db := setupTestDB(t)

	_ = db.InsertCredential("cred1", []byte("pk1"), "none", []byte{}, "[]", 0, 0, "First")
	_ = db.InsertCredential("cred2", []byte("pk2"), "none", []byte{}, "[]", 0, 0, "Second")

	creds, err := db.ListCredentials()
	if err != nil {
		t.Fatalf("ListCredentials: %v", err)
	}
	if len(creds) != 2 {
		t.Fatalf("expected 2 credentials, got %d", len(creds))
	}
	// Verify both are returned (ordering is tested via created_at which may have same second).
	ids := map[string]bool{creds[0].ID: true, creds[1].ID: true}
	if !ids["cred1"] || !ids["cred2"] {
		t.Errorf("expected cred1 and cred2 in results, got %v", ids)
	}
}

func TestGetCredentialByID_Found(t *testing.T) {
	db := setupTestDB(t)

	err := db.InsertCredential("cred1", []byte("pubkey"), "none", []byte{}, `["internal"]`, 0x05, 10, "Touch ID")
	if err != nil {
		t.Fatalf("InsertCredential: %v", err)
	}

	c, err := db.GetCredentialByID("cred1")
	if err != nil {
		t.Fatalf("GetCredentialByID: %v", err)
	}
	if c.FriendlyName != "Touch ID" {
		t.Errorf("FriendlyName = %q, want %q", c.FriendlyName, "Touch ID")
	}
	if c.SignCount != 10 {
		t.Errorf("SignCount = %d, want 10", c.SignCount)
	}
	if c.Flags != 0x05 {
		t.Errorf("Flags = %d, want 0x05", c.Flags)
	}
}

func TestGetCredentialByID_NotFound(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.GetCredentialByID("nonexistent")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestDeleteCredential_Success(t *testing.T) {
	db := setupTestDB(t)

	if err := db.InsertCredential("cred1", []byte("pk"), "none", []byte{}, "[]", 0, 0, "Key"); err != nil {
		t.Fatalf("InsertCredential: %v", err)
	}
	if err := db.DeleteCredential("cred1"); err != nil {
		t.Fatalf("DeleteCredential: %v", err)
	}

	_, err := db.GetCredentialByID("cred1")
	if err != sql.ErrNoRows {
		t.Errorf("expected credential to be deleted, GetCredentialByID returned: %v", err)
	}
}

func TestDeleteCredential_NotFound(t *testing.T) {
	db := setupTestDB(t)

	err := db.DeleteCredential("ghost")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestUpdateSignCount(t *testing.T) {
	db := setupTestDB(t)

	if err := db.InsertCredential("cred1", []byte("pk"), "none", []byte{}, "[]", 0, 5, "Key"); err != nil {
		t.Fatalf("InsertCredential: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	if err := db.UpdateSignCount("cred1", 42, now); err != nil {
		t.Fatalf("UpdateSignCount: %v", err)
	}

	c, err := db.GetCredentialByID("cred1")
	if err != nil {
		t.Fatalf("GetCredentialByID after UpdateSignCount: %v", err)
	}
	if c.SignCount != 42 {
		t.Errorf("SignCount = %d, want 42", c.SignCount)
	}
	if c.LastUsedAt == nil {
		t.Fatal("LastUsedAt should be set after UpdateSignCount")
	}
}

func TestCountCredentials(t *testing.T) {
	db := setupTestDB(t)

	n, err := db.CountCredentials()
	if err != nil {
		t.Fatalf("CountCredentials: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0, got %d", n)
	}

	_ = db.InsertCredential("c1", []byte("pk1"), "none", []byte{}, "[]", 0, 0, "A")
	_ = db.InsertCredential("c2", []byte("pk2"), "none", []byte{}, "[]", 0, 0, "B")
	_ = db.InsertCredential("c3", []byte("pk3"), "none", []byte{}, "[]", 0, 0, "C")

	n, err = db.CountCredentials()
	if err != nil {
		t.Fatalf("CountCredentials: %v", err)
	}
	if n != 3 {
		t.Errorf("expected 3, got %d", n)
	}
}


