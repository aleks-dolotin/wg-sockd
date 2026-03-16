package main

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testServer creates a Unix socket HTTP server that responds to API calls.
func testServer(t *testing.T, handler http.Handler) (socketPath string, cleanup func()) {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")

	listener, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	server := &http.Server{Handler: handler}
	go server.Serve(listener)

	return sock, func() {
		server.Close()
		os.Remove(sock)
	}
}

func TestPeersList(t *testing.T) {
	now := time.Now()
	peers := []PeerResponse{
		{
			ID:              1,
			PublicKey:        "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG=",
			FriendlyName:    "alice-phone",
			AllowedIPs:      []string{"10.0.0.2/32"},
			Enabled:         true,
			LatestHandshake: &now,
			TransferRx:      1024 * 1024 * 50,
			TransferTx:      1024 * 1024 * 10,
		},
		{
			ID:             2,
			PublicKey:       "zyxwvutsrqponmlkjihgfedcba9876543210ZYXWVUT=",
			FriendlyName:   "bob-laptop",
			AllowedIPs:     []string{"10.0.0.3/32"},
			Enabled:        true,
			AutoDiscovered: true,
			TransferRx:     512,
			TransferTx:     256,
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/peers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(peers)
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	client := newUnixClient(sock)
	err := peersList(client)
	if err != nil {
		t.Fatalf("peersList: %v", err)
	}
}

func TestPeersAdd(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/peers", func(w http.ResponseWriter, r *http.Request) {
		var req CreatePeerRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.FriendlyName == "" {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "friendly_name required"})
			return
		}

		w.WriteHeader(201)
		json.NewEncoder(w).Encode(PeerConfResponse{
			PublicKey: "newkey123456789012345678901234567890ABCD=",
			Config:   "[Interface]\nPrivateKey = xxx\n\n[Peer]\nEndpoint = vpn:51820\n",
		})
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	client := newUnixClient(sock)

	// Success case
	err := peersAdd(client, []string{"--name", "test-peer", "--profile", "full-tunnel"})
	if err != nil {
		t.Fatalf("peersAdd: %v", err)
	}

	// Missing name
	err = peersAdd(client, []string{})
	if err == nil {
		t.Fatal("expected error for missing --name")
	}
}

func TestPeersDelete(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/peers/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	client := newUnixClient(sock)
	err := peersDelete(client, []string{"--id", "1", "--yes"})
	if err != nil {
		t.Fatalf("peersDelete: %v", err)
	}
}

func TestPeersApprove(t *testing.T) {
	peers := []PeerResponse{
		{ID: 5, PublicKey: "abc123xyz", FriendlyName: "unknown-abc1", AutoDiscovered: true},
		{ID: 6, PublicKey: "def456xyz", FriendlyName: "unknown-def4", AutoDiscovered: true},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/peers", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(peers)
	})
	mux.HandleFunc("POST /api/peers/{id}/approve", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]string{"status": "approved"})
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	client := newUnixClient(sock)

	// Exact prefix match (minimum 4 chars required)
	err := peersApprove(client, []string{"abc1"})
	if err != nil {
		t.Fatalf("peersApprove: %v", err)
	}

	// Ambiguous prefix
	err = peersApprove(client, []string{""})
	if err == nil {
		t.Fatal("expected error for ambiguous prefix")
	}

	// No match
	err = peersApprove(client, []string{"zzz"})
	if err == nil {
		t.Fatal("expected error for no match")
	}
}

func TestProfilesList(t *testing.T) {
	profiles := []ProfileResponse{
		{
			Name:               "full-tunnel",
			DisplayName:        "Full Tunnel",
			AllowedIPs:         []string{"0.0.0.0/0"},
			ResolvedAllowedIPs: []string{"0.0.0.0/0"},
			IsDefault:          true,
			PeerCount:          3,
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/profiles", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(profiles)
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	client := newUnixClient(sock)
	err := profilesList(client)
	if err != nil {
		t.Fatalf("profilesList: %v", err)
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0B"},
		{500, "500B"},
		{1024, "1.0K"},
		{1024 * 1024, "1.0M"},
		{1024 * 1024 * 1024, "1.0G"},
		{1536, "1.5K"},
	}

	for _, tt := range tests {
		result := humanBytes(tt.input)
		if result != tt.expected {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestUnixClientCreation(t *testing.T) {
	client := newUnixClient("/tmp/nonexistent.sock")
	if client == nil {
		t.Fatal("newUnixClient returned nil")
	}
	if client.Timeout != 10*time.Second {
		t.Errorf("unexpected timeout: %v", client.Timeout)
	}
}

func TestCTLVersionVarsHaveDefaults(t *testing.T) {
	if version == "" {
		t.Error("version should not be empty")
	}
	if version != "dev" {
		t.Logf("version = %q (expected 'dev' in test builds)", version)
	}
	if commit == "" {
		t.Error("commit should not be empty")
	}
	if buildDate == "" {
		t.Error("buildDate should not be empty")
	}
}

func TestPrintVersion(t *testing.T) {
	// Just verify it doesn't panic.
	// In a test environment, version vars have dev defaults.
	printVersion()
}

