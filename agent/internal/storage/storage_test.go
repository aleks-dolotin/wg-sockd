package storage

import (
	"database/sql"
	"testing"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := NewDB(":memory:")
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestNewDB_InMemory(t *testing.T) {
	db := newTestDB(t)

	// Verify schema_version table exists and has migration recorded.
	var count int
	err := db.Conn().QueryRow("SELECT COUNT(*) FROM schema_version").Scan(&count)
	if err != nil {
		t.Fatalf("querying schema_version: %v", err)
	}
	if count != 6 {
		t.Errorf("expected 6 migrations recorded, got %d", count)
	}

	// Verify peers table exists.
	_, err = db.Conn().Exec("SELECT COUNT(*) FROM peers")
	if err != nil {
		t.Fatalf("peers table should exist: %v", err)
	}
}

func TestCreatePeer_And_GetByPubKey(t *testing.T) {
	db := newTestDB(t)

	p := &Peer{
		PublicKey:     "abc123pubkey",
		FriendlyName:  "Alice Laptop",
		AllowedIPs:    "10.0.0.2/32",
		Enabled:       true,
	}

	id, err := db.CreatePeer(p)
	if err != nil {
		t.Fatalf("CreatePeer: %v", err)
	}
	if id < 1 {
		t.Errorf("expected positive ID, got %d", id)
	}

	got, err := db.GetPeerByPubKey("abc123pubkey")
	if err != nil {
		t.Fatalf("GetPeerByPubKey: %v", err)
	}
	if got.FriendlyName != "Alice Laptop" {
		t.Errorf("FriendlyName: got %q, want %q", got.FriendlyName, "Alice Laptop")
	}
	if got.AllowedIPs != "10.0.0.2/32" {
		t.Errorf("AllowedIPs: got %q, want %q", got.AllowedIPs, "10.0.0.2/32")
	}
	if !got.Enabled {
		t.Error("Enabled should be true")
	}
	if got.AutoDiscovered {
		t.Error("AutoDiscovered should be false")
	}
}

func TestCreatePeer_UniqueConstraint(t *testing.T) {
	db := newTestDB(t)

	p := &Peer{PublicKey: "duplicate-key", Enabled: true}
	_, err := db.CreatePeer(p)
	if err != nil {
		t.Fatalf("first CreatePeer: %v", err)
	}

	_, err = db.CreatePeer(p)
	if err == nil {
		t.Fatal("expected error on duplicate public_key, got nil")
	}
}

func TestDeletePeer(t *testing.T) {
	db := newTestDB(t)

	p := &Peer{PublicKey: "to-delete", Enabled: true}
	_, err := db.CreatePeer(p)
	if err != nil {
		t.Fatalf("CreatePeer: %v", err)
	}

	err = db.DeletePeer("to-delete")
	if err != nil {
		t.Fatalf("DeletePeer: %v", err)
	}

	_, err = db.GetPeerByPubKey("to-delete")
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows after delete, got %v", err)
	}
}

func TestDeletePeer_NotFound(t *testing.T) {
	db := newTestDB(t)

	err := db.DeletePeer("nonexistent")
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows, got %v", err)
	}
}

func TestListPeers_OrderByCreatedAt(t *testing.T) {
	db := newTestDB(t)

	peers := []Peer{
		{PublicKey: "peer-a", FriendlyName: "A", Enabled: true},
		{PublicKey: "peer-b", FriendlyName: "B", Enabled: true},
		{PublicKey: "peer-c", FriendlyName: "C", Enabled: true},
	}
	for i := range peers {
		_, err := db.CreatePeer(&peers[i])
		if err != nil {
			t.Fatalf("CreatePeer %d: %v", i, err)
		}
	}

	list, err := db.ListPeers()
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 peers, got %d", len(list))
	}
	if list[0].FriendlyName != "A" || list[1].FriendlyName != "B" || list[2].FriendlyName != "C" {
		t.Errorf("unexpected order: %s, %s, %s", list[0].FriendlyName, list[1].FriendlyName, list[2].FriendlyName)
	}
}

func TestUpsertPeerFromReconcile_NewPeer(t *testing.T) {
	db := newTestDB(t)

	err := db.UpsertPeerFromReconcile("reconciled-key", "Auto Peer", true, true, "", nil)
	if err != nil {
		t.Fatalf("UpsertPeerFromReconcile: %v", err)
	}

	got, err := db.GetPeerByPubKey("reconciled-key")
	if err != nil {
		t.Fatalf("GetPeerByPubKey: %v", err)
	}
	if got.FriendlyName != "Auto Peer" {
		t.Errorf("FriendlyName: got %q, want %q", got.FriendlyName, "Auto Peer")
	}
	if !got.AutoDiscovered {
		t.Error("AutoDiscovered should be true")
	}
}

func TestUpsertPeerFromReconcile_ExistingPeerIgnored(t *testing.T) {
	db := newTestDB(t)

	// Create a peer manually.
	p := &Peer{PublicKey: "existing-key", FriendlyName: "Original Name", Enabled: true}
	_, err := db.CreatePeer(p)
	if err != nil {
		t.Fatalf("CreatePeer: %v", err)
	}

	// Upsert should not overwrite.
	err = db.UpsertPeerFromReconcile("existing-key", "New Name", true, true, "", nil)
	if err != nil {
		t.Fatalf("UpsertPeerFromReconcile: %v", err)
	}

	got, err := db.GetPeerByPubKey("existing-key")
	if err != nil {
		t.Fatalf("GetPeerByPubKey: %v", err)
	}
	// Name should be unchanged — INSERT OR IGNORE leaves existing rows alone.
	if got.FriendlyName != "Original Name" {
		t.Errorf("FriendlyName should not change: got %q, want %q", got.FriendlyName, "Original Name")
	}
}

func TestUpsertPeerFromReconcile_PreservesEndpointAndPKA(t *testing.T) {
	db := newTestDB(t)

	pka := 25
	err := db.UpsertPeerFromReconcile("kernel-peer", "unknown-kernelp", true, false, "10.0.0.2:51820", &pka)
	if err != nil {
		t.Fatalf("UpsertPeerFromReconcile: %v", err)
	}

	got, err := db.GetPeerByPubKey("kernel-peer")
	if err != nil {
		t.Fatalf("GetPeerByPubKey: %v", err)
	}
	if got.LastSeenEndpoint != "10.0.0.2:51820" {
		t.Errorf("LastSeenEndpoint: got %q, want %q", got.LastSeenEndpoint, "10.0.0.2:51820")
	}
	if got.PersistentKeepalive == nil || *got.PersistentKeepalive != 25 {
		t.Errorf("PersistentKeepalive: got %v, want 25", got.PersistentKeepalive)
	}
}


func TestUpdatePeer(t *testing.T) {
	db := newTestDB(t)

	p := &Peer{PublicKey: "update-key", FriendlyName: "Old", AllowedIPs: "10.0.0.1/32", Enabled: true}
	_, err := db.CreatePeer(p)
	if err != nil {
		t.Fatalf("CreatePeer: %v", err)
	}

	newName := "New Name"
	disabled := false
	err = db.UpdatePeer("update-key", &PeerUpdate{
		FriendlyName: &newName,
		Enabled:      &disabled,
	})
	if err != nil {
		t.Fatalf("UpdatePeer: %v", err)
	}

	got, err := db.GetPeerByPubKey("update-key")
	if err != nil {
		t.Fatalf("GetPeerByPubKey: %v", err)
	}
	if got.FriendlyName != "New Name" {
		t.Errorf("FriendlyName: got %q, want %q", got.FriendlyName, "New Name")
	}
	if got.Enabled {
		t.Error("Enabled should be false after update")
	}
	// AllowedIPs should be unchanged.
	if got.AllowedIPs != "10.0.0.1/32" {
		t.Errorf("AllowedIPs should be unchanged: got %q", got.AllowedIPs)
	}
}

func TestUpdatePeer_NotFound(t *testing.T) {
	db := newTestDB(t)

	name := "test"
	err := db.UpdatePeer("nonexistent", &PeerUpdate{FriendlyName: &name})
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows, got %v", err)
	}
}

func TestCreatePeer_WithEndpointAndPKA(t *testing.T) {
	db := newTestDB(t)

	pka := 25
	mtu := 1380
	p := &Peer{
		PublicKey:           "ep-key-1",
		FriendlyName:        "Site-to-Site",
		AllowedIPs:          "10.0.0.2/32",
		Enabled:             true,
		Endpoint:            "10.0.0.2:51820",
		PersistentKeepalive: &pka,
		ClientDNS:           "1.1.1.1",
		ClientMTU:           &mtu,
	}

	id, err := db.CreatePeer(p)
	if err != nil {
		t.Fatalf("CreatePeer: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero ID")
	}

	got, err := db.GetPeerByPubKey("ep-key-1")
	if err != nil {
		t.Fatalf("GetPeerByPubKey: %v", err)
	}
	if got.Endpoint != "10.0.0.2:51820" {
		t.Errorf("Endpoint: got %q, want %q", got.Endpoint, "10.0.0.2:51820")
	}
	if got.PersistentKeepalive == nil || *got.PersistentKeepalive != 25 {
		t.Errorf("PersistentKeepalive: got %v, want 25", got.PersistentKeepalive)
	}
	if got.ClientDNS != "1.1.1.1" {
		t.Errorf("ClientDNS: got %q, want %q", got.ClientDNS, "1.1.1.1")
	}
	if got.ClientMTU == nil || *got.ClientMTU != 1380 {
		t.Errorf("ClientMTU: got %v, want 1380", got.ClientMTU)
	}
}

func TestCreatePeer_WithoutEndpointAndPKA_Defaults(t *testing.T) {
	db := newTestDB(t)

	p := &Peer{
		PublicKey:     "ep-key-default",
		FriendlyName:  "BasicPeer",
		AllowedIPs:    "10.0.0.3/32",
		Enabled:       true,
	}

	_, err := db.CreatePeer(p)
	if err != nil {
		t.Fatalf("CreatePeer: %v", err)
	}

	got, err := db.GetPeerByPubKey("ep-key-default")
	if err != nil {
		t.Fatalf("GetPeerByPubKey: %v", err)
	}
	if got.Endpoint != "" {
		t.Errorf("Endpoint: got %q, want empty", got.Endpoint)
	}
	if got.PersistentKeepalive != nil {
		t.Errorf("PersistentKeepalive: got %v, want nil", got.PersistentKeepalive)
	}
	if got.ClientDNS != "" {
		t.Errorf("ClientDNS: got %q, want empty", got.ClientDNS)
	}
	if got.ClientMTU != nil {
		t.Errorf("ClientMTU: got %v, want nil", got.ClientMTU)
	}
}

func TestUpdatePeer_NullablePKA(t *testing.T) {
	db := newTestDB(t)

	pka := 30
	p := &Peer{
		PublicKey:           "pka-test-key",
		FriendlyName:        "PKATest",
		AllowedIPs:          "10.0.0.4/32",
		Enabled:             true,
		PersistentKeepalive: &pka,
	}
	_, err := db.CreatePeer(p)
	if err != nil {
		t.Fatalf("CreatePeer: %v", err)
	}

	// Case 1: Field omitted (nil) → no change.
	err = db.UpdatePeer("pka-test-key", &PeerUpdate{})
	if err != nil {
		t.Fatalf("UpdatePeer (no change): %v", err)
	}
	got, _ := db.GetPeerByPubKey("pka-test-key")
	if got.PersistentKeepalive == nil || *got.PersistentKeepalive != 30 {
		t.Errorf("Case 1: PKA should still be 30, got %v", got.PersistentKeepalive)
	}

	// Case 2: Explicit null (set NULL = inherit).
	var nilInt *int
	err = db.UpdatePeer("pka-test-key", &PeerUpdate{PersistentKeepalive: &nilInt})
	if err != nil {
		t.Fatalf("UpdatePeer (set null): %v", err)
	}
	got, _ = db.GetPeerByPubKey("pka-test-key")
	if got.PersistentKeepalive != nil {
		t.Errorf("Case 2: PKA should be nil (inherit), got %v", got.PersistentKeepalive)
	}

	// Case 3: Explicit value (set 45).
	v45 := 45
	pv45 := &v45
	err = db.UpdatePeer("pka-test-key", &PeerUpdate{PersistentKeepalive: &pv45})
	if err != nil {
		t.Fatalf("UpdatePeer (set 45): %v", err)
	}
	got, _ = db.GetPeerByPubKey("pka-test-key")
	if got.PersistentKeepalive == nil || *got.PersistentKeepalive != 45 {
		t.Errorf("Case 3: PKA should be 45, got %v", got.PersistentKeepalive)
	}
}

func TestListPeers_IncludesNewFields(t *testing.T) {
	db := newTestDB(t)

	pka := 15
	p := &Peer{
		PublicKey:           "list-ep-key",
		FriendlyName:        "ListTest",
		AllowedIPs:          "10.0.0.5/32",
		Enabled:             true,
		Endpoint:            "1.2.3.4:51820",
		PersistentKeepalive: &pka,
		ClientDNS:           "8.8.8.8",
	}
	_, err := db.CreatePeer(p)
	if err != nil {
		t.Fatalf("CreatePeer: %v", err)
	}

	peers, err := db.ListPeers()
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}
	if peers[0].Endpoint != "1.2.3.4:51820" {
		t.Errorf("Endpoint: got %q, want %q", peers[0].Endpoint, "1.2.3.4:51820")
	}
	if peers[0].PersistentKeepalive == nil || *peers[0].PersistentKeepalive != 15 {
		t.Errorf("PKA: got %v, want 15", peers[0].PersistentKeepalive)
	}
	if peers[0].ClientDNS != "8.8.8.8" {
		t.Errorf("ClientDNS: got %q, want %q", peers[0].ClientDNS, "8.8.8.8")
	}
}
