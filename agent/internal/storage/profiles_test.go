package storage

import (
	"database/sql"
	"testing"
)

func TestMigration002_ProfilesTableExists(t *testing.T) {
	db := newTestDB(t)

	// Verify profiles table exists and has correct columns.
	_, err := db.Conn().Exec("SELECT name, allowed_ips, exclude_ips, description, is_default, created_at FROM profiles LIMIT 0")
	if err != nil {
		t.Fatalf("profiles table should exist with expected columns: %v", err)
	}

	// Verify migration was recorded.
	var count int
	err = db.Conn().QueryRow("SELECT COUNT(*) FROM schema_version WHERE version = '002_profiles.sql'").Scan(&count)
	if err != nil {
		t.Fatalf("querying schema_version: %v", err)
	}
	if count != 1 {
		t.Errorf("expected migration 002_profiles.sql to be recorded, got count=%d", count)
	}
}

func TestSeedProfiles_EmptyDB(t *testing.T) {
	db := newTestDB(t)

	seeds := []ProfileSeed{
		{
			Name:        "full-access",
			AllowedIPs:  []string{"0.0.0.0/0", "::/0"},
			Description: "Route all traffic through VPN",
		},
		{
			Name:        "nas-only",
			AllowedIPs:  []string{"10.0.0.0/24"},
			Description: "Access NAS network only",
		},
		{
			Name:        "internet-only",
			AllowedIPs:  []string{"0.0.0.0/0", "::/0"},
			ExcludeIPs:  []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
			Description: "Internet through VPN, no local access",
		},
	}

	err := db.SeedProfiles(seeds)
	if err != nil {
		t.Fatalf("SeedProfiles: %v", err)
	}

	profiles, err := db.ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	if len(profiles) != 3 {
		t.Fatalf("expected 3 profiles, got %d", len(profiles))
	}

	// Profiles should be ordered by name ASC.
	if profiles[0].Name != "full-access" {
		t.Errorf("first profile: got %q, want %q", profiles[0].Name, "full-access")
	}
	if profiles[1].Name != "internet-only" {
		t.Errorf("second profile: got %q, want %q", profiles[1].Name, "internet-only")
	}
	if profiles[2].Name != "nas-only" {
		t.Errorf("third profile: got %q, want %q", profiles[2].Name, "nas-only")
	}

	// Verify first profile details.
	p := profiles[0]
	if p.Name != "full-access" {
		t.Errorf("Name: got %q, want %q", p.Name, "full-access")
	}
	if len(p.AllowedIPs) != 2 || p.AllowedIPs[0] != "0.0.0.0/0" || p.AllowedIPs[1] != "::/0" {
		t.Errorf("AllowedIPs: got %v, want [0.0.0.0/0 ::/0]", p.AllowedIPs)
	}
	if !p.IsDefault {
		t.Error("seeded profiles should have IsDefault=true")
	}

	// Verify internet-only has exclude_ips.
	pio := profiles[1] // internet-only (alphabetical)
	if len(pio.ExcludeIPs) != 3 {
		t.Errorf("internet-only ExcludeIPs: got %v, want 3 entries", pio.ExcludeIPs)
	}

	// Verify nas-only has empty exclude_ips (not nil).
	pnas := profiles[2] // nas-only
	if pnas.ExcludeIPs == nil {
		t.Error("nas-only ExcludeIPs should be empty slice, not nil")
	}
	if len(pnas.ExcludeIPs) != 0 {
		t.Errorf("nas-only ExcludeIPs: got %v, want empty", pnas.ExcludeIPs)
	}
}

func TestSeedProfiles_NonEmptyDB_NoChanges(t *testing.T) {
	db := newTestDB(t)

	// Seed initial profiles.
	seeds := []ProfileSeed{
		{Name: "existing", AllowedIPs: []string{"10.0.0.0/24"}, Description: "test"},
	}
	err := db.SeedProfiles(seeds)
	if err != nil {
		t.Fatalf("first SeedProfiles: %v", err)
	}

	// Try to seed again with different data.
	newSeeds := []ProfileSeed{
		{Name: "new-profile", AllowedIPs: []string{"0.0.0.0/0"}, Description: "should not appear"},
	}
	err = db.SeedProfiles(newSeeds)
	if err != nil {
		t.Fatalf("second SeedProfiles: %v", err)
	}

	// Should still have only the original profile.
	profiles, err := db.ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	if profiles[0].Name != "existing" {
		t.Errorf("expected 'existing' profile, got %q", profiles[0].Name)
	}
}

func TestSeedProfiles_EmptySlice(t *testing.T) {
	db := newTestDB(t)

	err := db.SeedProfiles(nil)
	if err != nil {
		t.Fatalf("SeedProfiles with nil: %v", err)
	}
	err = db.SeedProfiles([]ProfileSeed{})
	if err != nil {
		t.Fatalf("SeedProfiles with empty slice: %v", err)
	}
}

func TestCreateProfile_And_GetProfile(t *testing.T) {
	db := newTestDB(t)

	p := &Profile{
		Name:        "custom-profile",
		AllowedIPs:  []string{"10.0.0.0/24", "192.168.1.0/24"},
		ExcludeIPs:  []string{},
		Description: "A custom profile",
		IsDefault:   false,
	}

	err := db.CreateProfile(p)
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	got, err := db.GetProfile("custom-profile")
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if got.Name != "custom-profile" {
		t.Errorf("Name: got %q, want %q", got.Name, "custom-profile")
	}
	if len(got.AllowedIPs) != 2 {
		t.Errorf("AllowedIPs: got %v, want 2 entries", got.AllowedIPs)
	}
	if got.IsDefault {
		t.Error("IsDefault should be false for custom profiles")
	}
}

func TestGetProfile_NotFound(t *testing.T) {
	db := newTestDB(t)

	_, err := db.GetProfile("nonexistent")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestUpdateProfile(t *testing.T) {
	db := newTestDB(t)

	p := &Profile{
		Name:        "update-me",
		AllowedIPs:  []string{"10.0.0.0/24"},
		ExcludeIPs:  []string{},
		Description: "old description",
	}
	if err := db.CreateProfile(p); err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	updated := &Profile{
		AllowedIPs:  []string{"0.0.0.0/0"},
		ExcludeIPs:  []string{"10.0.0.0/8"},
		Description: "new description",
	}
	if err := db.UpdateProfile("update-me", updated); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	got, err := db.GetProfile("update-me")
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if got.Description != "new description" {
		t.Errorf("Description: got %q, want %q", got.Description, "new description")
	}
	if len(got.AllowedIPs) != 1 || got.AllowedIPs[0] != "0.0.0.0/0" {
		t.Errorf("AllowedIPs: got %v", got.AllowedIPs)
	}
	if len(got.ExcludeIPs) != 1 || got.ExcludeIPs[0] != "10.0.0.0/8" {
		t.Errorf("ExcludeIPs: got %v", got.ExcludeIPs)
	}
}

func TestUpdateProfile_NotFound(t *testing.T) {
	db := newTestDB(t)

	p := &Profile{AllowedIPs: []string{}, ExcludeIPs: []string{}}
	err := db.UpdateProfile("nonexistent", p)
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestDeleteProfile_NoPeersReferencing(t *testing.T) {
	db := newTestDB(t)

	p := &Profile{
		Name:       "delete-me",
		AllowedIPs: []string{"10.0.0.0/24"},
		ExcludeIPs: []string{},
	}
	if err := db.CreateProfile(p); err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	if err := db.DeleteProfile("delete-me"); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}

	_, err := db.GetProfile("delete-me")
	if err != sql.ErrNoRows {
		t.Errorf("expected profile to be deleted, got %v", err)
	}
}

func TestDeleteProfile_WithReferencingPeers_Error(t *testing.T) {
	db := newTestDB(t)

	// Create a profile first.
	prof := &Profile{
		Name:       "referenced",
		AllowedIPs: []string{"10.0.0.0/24"},
		ExcludeIPs: []string{},
	}
	if err := db.CreateProfile(prof); err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	// Create a peer referencing the profile.
	profileName := "referenced"
	peer := &Peer{
		PublicKey: "peer-with-profile",
		Profile:  &profileName,
		Enabled:  true,
	}
	_, err := db.CreatePeer(peer)
	if err != nil {
		t.Fatalf("CreatePeer: %v", err)
	}

	// DeleteProfile should fail because a peer references it.
	err = db.DeleteProfile("referenced")
	if err == nil {
		t.Fatal("expected error when deleting profile with referencing peers, got nil")
	}

	// Profile should still exist.
	_, err = db.GetProfile("referenced")
	if err != nil {
		t.Errorf("profile should still exist: %v", err)
	}
}

func TestDeleteProfile_NotFound(t *testing.T) {
	db := newTestDB(t)

	err := db.DeleteProfile("nonexistent")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestFKTrigger_PeerInsert_InvalidProfile(t *testing.T) {
	db := newTestDB(t)

	// Try to create a peer with a non-existent profile.
	badProfile := "nonexistent-profile"
	peer := &Peer{
		PublicKey: "fk-test-peer",
		Profile:  &badProfile,
		Enabled:  true,
	}
	_, err := db.CreatePeer(peer)
	if err == nil {
		t.Fatal("expected FK trigger error when inserting peer with invalid profile, got nil")
	}
}

func TestFKTrigger_PeerInsert_NullProfile_OK(t *testing.T) {
	db := newTestDB(t)

	// Peer with nil profile should be fine ("Custom" = no profile).
	peer := &Peer{
		PublicKey: "null-profile-peer",
		Profile:  nil,
		Enabled:  true,
	}
	_, err := db.CreatePeer(peer)
	if err != nil {
		t.Fatalf("inserting peer with null profile should succeed: %v", err)
	}
}

func TestFKTrigger_PeerInsert_ValidProfile_OK(t *testing.T) {
	db := newTestDB(t)

	prof := &Profile{
		Name:       "valid-profile",
		AllowedIPs: []string{"10.0.0.0/24"},
		ExcludeIPs: []string{},
	}
	if err := db.CreateProfile(prof); err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	profileName := "valid-profile"
	peer := &Peer{
		PublicKey: "valid-profile-peer",
		Profile:  &profileName,
		Enabled:  true,
	}
	_, err := db.CreatePeer(peer)
	if err != nil {
		t.Fatalf("inserting peer with valid profile should succeed: %v", err)
	}
}

func TestFKTrigger_PeerUpdate_InvalidProfile(t *testing.T) {
	db := newTestDB(t)

	// Create a peer with no profile.
	peer := &Peer{
		PublicKey: "update-fk-peer",
		Profile:  nil,
		Enabled:  true,
	}
	_, err := db.CreatePeer(peer)
	if err != nil {
		t.Fatalf("CreatePeer: %v", err)
	}

	// Update to a non-existent profile should fail.
	badProfile := "nonexistent"
	badProfilePtr := &badProfile
	err = db.UpdatePeer("update-fk-peer", &PeerUpdate{
		Profile: &badProfilePtr,
	})
	if err == nil {
		t.Fatal("expected FK trigger error when updating peer to invalid profile, got nil")
	}
}

func TestListProfiles_Empty(t *testing.T) {
	db := newTestDB(t)

	profiles, err := db.ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	if len(profiles) != 0 {
		t.Errorf("expected 0 profiles, got %d", len(profiles))
	}
}
