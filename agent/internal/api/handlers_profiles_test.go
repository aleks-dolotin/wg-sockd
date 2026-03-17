package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/storage"
)

func TestListProfiles_Empty(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	req := httptest.NewRequest("GET", "/api/profiles", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var profiles []ProfileResponse
	json.NewDecoder(w.Body).Decode(&profiles)
	if len(profiles) != 0 {
		t.Errorf("expected 0 profiles, got %d", len(profiles))
	}
}

func TestListProfiles_WithResolvedAllowedIPs(t *testing.T) {
	h, db := newTestHandlers(t)
	router := NewRouter(h)

	// Create a profile with exclusions.
	p := &storage.Profile{
		Name:        "internet-only",
		AllowedIPs:  []string{"0.0.0.0/0", "::/0"},
		ExcludeIPs:  []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
		Description: "Internet through VPN, no local access",
		IsDefault:   true,
	}
	if err := db.CreateProfile(p); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/api/profiles", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var profiles []ProfileResponse
	json.NewDecoder(w.Body).Decode(&profiles)
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}

	pr := profiles[0]
	if pr.Name != "internet-only" {
		t.Errorf("Name: got %q, want %q", pr.Name, "internet-only")
	}
	// resolved_allowed_ips should NOT contain the excluded ranges.
	for _, r := range pr.ResolvedAllowedIPs {
		if r == "10.0.0.0/8" || r == "172.16.0.0/12" || r == "192.168.0.0/16" {
			t.Errorf("resolved_allowed_ips should not contain %q", r)
		}
	}
	if pr.RouteCount == 0 {
		t.Error("RouteCount should be > 0")
	}
	if !pr.IsDefault {
		t.Error("IsDefault should be true for seeded profile")
	}
}

func TestCreateProfile_Success(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	body := `{"name":"my-profile","allowed_ips":["10.0.0.0/24"],"description":"test"}`
	req := httptest.NewRequest("POST", "/api/profiles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp ProfileResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Name != "my-profile" {
		t.Errorf("Name: got %q", resp.Name)
	}
	if !resp.IsDefault == true {
		// Created via API should NOT be default.
	}
	if resp.IsDefault {
		t.Error("API-created profile should have IsDefault=false")
	}
	if len(resp.ResolvedAllowedIPs) == 0 {
		t.Error("ResolvedAllowedIPs should be populated")
	}
}

func TestCreateProfile_WithExclusions(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	body := `{"name":"with-excl","allowed_ips":["0.0.0.0/0"],"exclude_ips":["10.0.0.0/8"]}`
	req := httptest.NewRequest("POST", "/api/profiles", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp ProfileResponse
	json.NewDecoder(w.Body).Decode(&resp)
	// Resolved should not contain 10.0.0.0/8.
	for _, r := range resp.ResolvedAllowedIPs {
		if r == "10.0.0.0/8" {
			t.Errorf("resolved should not contain excluded range %q", r)
		}
	}
}

func TestCreateProfile_InvalidName(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	tests := []struct {
		name string
		body string
	}{
		{"empty name", `{"name":"","allowed_ips":["10.0.0.0/24"]}`},
		{"too short", `{"name":"a","allowed_ips":["10.0.0.0/24"]}`},
		{"uppercase", `{"name":"MyProfile","allowed_ips":["10.0.0.0/24"]}`},
		{"spaces", `{"name":"my profile","allowed_ips":["10.0.0.0/24"]}`},
		{"no allowed_ips", `{"name":"valid-name"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/profiles", strings.NewReader(tt.body))
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusBadRequest, w.Body.String())
			}
		})
	}
}

func TestCreateProfile_Duplicate(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	body := `{"name":"dup-profile","allowed_ips":["10.0.0.0/24"]}`

	// First create — success.
	req := httptest.NewRequest("POST", "/api/profiles", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("first create: got %d, want %d", w.Code, http.StatusCreated)
	}

	// Second create — conflict.
	req = httptest.NewRequest("POST", "/api/profiles", strings.NewReader(body))
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("duplicate create: got %d, want %d. Body: %s", w.Code, http.StatusConflict, w.Body.String())
	}
}

func TestCreateProfile_InvalidCIDR(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	body := `{"name":"bad-cidr","allowed_ips":["not-a-cidr"]}`
	req := httptest.NewRequest("POST", "/api/profiles", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestUpdateProfile_Success(t *testing.T) {
	h, db := newTestHandlers(t)
	router := NewRouter(h)

	// Create profile.
	if err := db.CreateProfile(&storage.Profile{
		Name:        "update-me",
		AllowedIPs:  []string{"10.0.0.0/24"},
		ExcludeIPs:  []string{},
		Description: "old",
	}); err != nil {
		t.Fatal(err)
	}

	body := `{"description":"new"}`
	req := httptest.NewRequest("PUT", "/api/profiles/update-me", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp ProfileResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Description != "new" {
		t.Errorf("Description: got %q, want %q", resp.Description, "new")
	}
	// AllowedIPs should remain unchanged.
	if len(resp.AllowedIPs) != 1 || resp.AllowedIPs[0] != "10.0.0.0/24" {
		t.Errorf("AllowedIPs should be unchanged: got %v", resp.AllowedIPs)
	}
}

func TestUpdateProfile_NotFound(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	body := `{"description":"X"}`
	req := httptest.NewRequest("PUT", "/api/profiles/nonexistent", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestDeleteProfile_Success(t *testing.T) {
	h, db := newTestHandlers(t)
	router := NewRouter(h)

	if err := db.CreateProfile(&storage.Profile{
		Name:       "delete-me",
		AllowedIPs: []string{"10.0.0.0/24"},
		ExcludeIPs: []string{},
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("DELETE", "/api/profiles/delete-me", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusNoContent, w.Body.String())
	}
}

func TestDeleteProfile_NotFound(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	req := httptest.NewRequest("DELETE", "/api/profiles/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestDeleteProfile_WithReferencingPeers_Conflict(t *testing.T) {
	h, db := newTestHandlers(t)
	router := NewRouter(h)

	// Create profile.
	if err := db.CreateProfile(&storage.Profile{
		Name:       "referenced-prof",
		AllowedIPs: []string{"10.0.0.0/24"},
		ExcludeIPs: []string{},
	}); err != nil {
		t.Fatal(err)
	}

	// Create peer referencing the profile.
	profName := "referenced-prof"
	_, err := db.CreatePeer(&storage.Peer{
		PublicKey: "ref-peer-key",
		Profile:  &profName,
		Enabled:  true,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("DELETE", "/api/profiles/referenced-prof", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusConflict, w.Body.String())
	}
}

func TestListProfiles_PeerCount(t *testing.T) {
	h, db := newTestHandlers(t)
	router := NewRouter(h)

	// Create profile.
	if err := db.CreateProfile(&storage.Profile{
		Name:       "counted-prof",
		AllowedIPs: []string{"10.0.0.0/24"},
		ExcludeIPs: []string{},
	}); err != nil {
		t.Fatal(err)
	}

	// Create 2 peers referencing it.
	profName := "counted-prof"
	for i := 0; i < 2; i++ {
		_, err := db.CreatePeer(&storage.Peer{
			PublicKey: "count-peer-" + itoa(int64(i)),
			Profile:  &profName,
			Enabled:  true,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest("GET", "/api/profiles", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var profiles []ProfileResponse
	json.NewDecoder(w.Body).Decode(&profiles)
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	if profiles[0].PeerCount != 2 {
		t.Errorf("PeerCount: got %d, want 2", profiles[0].PeerCount)
	}
}
