package main

import (
	"encoding/json"
	"fmt"
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

// --- Existing tests ---

func TestPeersList(t *testing.T) {
	now := time.Now()
	peers := []PeerResponse{
		{
			ID: 1, PublicKey: "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG=",
			FriendlyName: "alice-phone", AllowedIPs: []string{"10.0.0.2/32"},
			Enabled: true, LatestHandshake: &now,
			TransferRx: 1024 * 1024 * 50, TransferTx: 1024 * 1024 * 10,
		},
		{
			ID: 2, PublicKey: "zyxwvutsrqponmlkjihgfedcba9876543210ZYXWVUT=",
			FriendlyName: "bob-laptop", AllowedIPs: []string{"10.0.0.3/32"},
			Enabled: true, AutoDiscovered: true, TransferRx: 512, TransferTx: 256,
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/peers", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(peers)
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	client := newUnixClient(sock)
	if err := peersList(client); err != nil {
		t.Fatalf("peersList: %v", err)
	}
}

func TestPeersList_JSON(t *testing.T) {
	peers := []PeerResponse{{ID: 1, FriendlyName: "test", Enabled: true}}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/peers", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(peers)
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	jsonOutput = true
	defer func() { jsonOutput = false }()

	client := newUnixClient(sock)
	if err := peersList(client); err != nil {
		t.Fatalf("peersList --json: %v", err)
	}
}

func TestPeersGet(t *testing.T) {
	now := time.Now()
	profile := "full-tunnel"
	peer := PeerResponse{
		ID: 5, PublicKey: "testkey123", FriendlyName: "alice",
		AllowedIPs: []string{"10.0.0.2/32"}, Profile: &profile,
		Enabled: true, LatestHandshake: &now,
		TransferRx: 1024, TransferTx: 2048, Endpoint: "1.2.3.4:51820",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/peers/{id}", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(peer)
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	client := newUnixClient(sock)
	if err := peersGet(client, []string{"--id", "5"}); err != nil {
		t.Fatalf("peersGet: %v", err)
	}
}

func TestPeersGet_JSON(t *testing.T) {
	peer := PeerResponse{ID: 5, FriendlyName: "alice", Enabled: true}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/peers/{id}", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(peer)
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	jsonOutput = true
	defer func() { jsonOutput = false }()

	client := newUnixClient(sock)
	if err := peersGet(client, []string{"--id", "5"}); err != nil {
		t.Fatalf("peersGet --json: %v", err)
	}
}

func TestPeersAdd(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/peers", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(PeerConfResponse{
			PublicKey: "newkey123456789012345678901234567890ABCD=",
			Config:   "[Interface]\nPrivateKey = xxx\n\n[Peer]\nEndpoint = vpn:51820\n",
		})
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	client := newUnixClient(sock)
	if err := peersAdd(client, []string{"--name", "test-peer", "--profile", "full-tunnel"}); err != nil {
		t.Fatalf("peersAdd: %v", err)
	}

	if err := peersAdd(client, []string{}); err == nil {
		t.Fatal("expected error for missing --name")
	}
}

func TestPeersAdd_JSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/peers", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(PeerConfResponse{PublicKey: "key123", Config: "[Interface]\n"})
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	jsonOutput = true
	defer func() { jsonOutput = false }()

	client := newUnixClient(sock)
	if err := peersAdd(client, []string{"--name", "test"}); err != nil {
		t.Fatalf("peersAdd --json: %v", err)
	}
}

func TestPeersUpdate(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/peers/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)

		// Verify correct fields were sent
		if name, ok := body["friendly_name"]; ok {
			if name != "new-name" {
				http.Error(w, "unexpected name", 400)
				return
			}
		}
		if enabled, ok := body["enabled"]; ok {
			if enabled != false {
				http.Error(w, "expected enabled=false", 400)
				return
			}
		}

		json.NewEncoder(w).Encode(PeerResponse{ID: 5, FriendlyName: "new-name", Enabled: false})
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	client := newUnixClient(sock)
	if err := peersUpdate(client, []string{"--id", "5", "--name", "new-name", "--disable"}); err != nil {
		t.Fatalf("peersUpdate: %v", err)
	}

	// No fields
	if err := peersUpdate(client, []string{"--id", "5"}); err == nil {
		t.Fatal("expected error when no fields to update")
	}

	// Missing ID
	if err := peersUpdate(client, []string{"--name", "x"}); err == nil {
		t.Fatal("expected error for missing --id")
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
	if err := peersDelete(client, []string{"--id", "1", "--yes"}); err != nil {
		t.Fatalf("peersDelete: %v", err)
	}
}

func TestPeersDelete_JSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/peers/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	jsonOutput = true
	defer func() { jsonOutput = false }()

	client := newUnixClient(sock)
	if err := peersDelete(client, []string{"--id", "1", "--yes"}); err != nil {
		t.Fatalf("peersDelete --json: %v", err)
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
		json.NewEncoder(w).Encode(map[string]string{"status": "approved"})
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	client := newUnixClient(sock)

	if err := peersApprove(client, []string{"--client-address", "10.0.0.5/32", "abc1"}); err != nil {
		t.Fatalf("peersApprove: %v", err)
	}

	if err := peersApprove(client, []string{""}); err == nil {
		t.Fatal("expected error for ambiguous prefix")
	}

	if err := peersApprove(client, []string{"zzz"}); err == nil {
		t.Fatal("expected error for no match")
	}
}

func TestPeersRotateKeys(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/peers/{id}/rotate-keys", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(PeerConfResponse{
			PublicKey: "newrotatedkey123",
			Config:   "[Interface]\nPrivateKey = rotated\n",
		})
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	client := newUnixClient(sock)
	if err := peersRotateKeys(client, []string{"--id", "5", "--yes"}); err != nil {
		t.Fatalf("peersRotateKeys: %v", err)
	}
}

func TestPeersRotateKeys_JSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/peers/{id}/rotate-keys", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(PeerConfResponse{PublicKey: "newkey", Config: "[Interface]\n"})
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	jsonOutput = true
	defer func() { jsonOutput = false }()

	client := newUnixClient(sock)
	if err := peersRotateKeys(client, []string{"--id", "5", "--yes"}); err != nil {
		t.Fatalf("peersRotateKeys --json: %v", err)
	}
}

func TestProfilesList(t *testing.T) {
	profiles := []ProfileResponse{
		{Name: "full-tunnel", AllowedIPs: []string{"0.0.0.0/0"},
			ResolvedAllowedIPs: []string{"0.0.0.0/0"}, IsDefault: true, PeerCount: 3},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/profiles", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(profiles)
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	client := newUnixClient(sock)
	if err := profilesList(client); err != nil {
		t.Fatalf("profilesList: %v", err)
	}
}

func TestProfilesList_JSON(t *testing.T) {
	profiles := []ProfileResponse{{Name: "test"}}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/profiles", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(profiles)
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	jsonOutput = true
	defer func() { jsonOutput = false }()

	client := newUnixClient(sock)
	if err := profilesList(client); err != nil {
		t.Fatalf("profilesList --json: %v", err)
	}
}

func TestProfilesCreate(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/profiles", func(w http.ResponseWriter, r *http.Request) {
		var req CreateProfileRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Name == "" {
			w.WriteHeader(400)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(ProfileResponse{Name: req.Name})
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	client := newUnixClient(sock)
	if err := profilesCreate(client, []string{"--name", "split-tunnel", "--allowed-ips", "10.0.0.0/8"}); err != nil {
		t.Fatalf("profilesCreate: %v", err)
	}

	if err := profilesCreate(client, []string{}); err == nil {
		t.Fatal("expected error for missing --name")
	}
}

func TestProfilesUpdate(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/profiles/{name}", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ProfileResponse{Name: "test"})
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	client := newUnixClient(sock)
	if err := profilesUpdate(client, []string{"--name", "test", "--description", "updated"}); err != nil {
		t.Fatalf("profilesUpdate: %v", err)
	}
}

func TestProfilesDelete(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/profiles/{name}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	client := newUnixClient(sock)
	if err := profilesDelete(client, []string{"--name", "old-profile", "--yes"}); err != nil {
		t.Fatalf("profilesDelete: %v", err)
	}
}

func TestHealth(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(HealthResponse{Status: "ok", WireGuard: "ok", SQLite: "ok"})
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	client := newUnixClient(sock)
	if err := healthCmd(client); err != nil {
		t.Fatalf("health: %v", err)
	}
}

func TestHealth_JSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(HealthResponse{Status: "ok", WireGuard: "ok", SQLite: "ok"})
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	jsonOutput = true
	defer func() { jsonOutput = false }()

	client := newUnixClient(sock)
	if err := healthCmd(client); err != nil {
		t.Fatalf("health --json: %v", err)
	}
}

func TestStats(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(StatsResponse{TotalPeers: 5, OnlinePeers: 2, TotalRx: 1024 * 1024 * 1500, TotalTx: 1024 * 1024 * 500})
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	client := newUnixClient(sock)
	if err := statsCmd(client); err != nil {
		t.Fatalf("stats: %v", err)
	}
}

func TestStats_JSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(StatsResponse{TotalPeers: 5, OnlinePeers: 2, TotalRx: 100, TotalTx: 200})
	})

	sock, cleanup := testServer(t, mux)
	defer cleanup()

	jsonOutput = true
	defer func() { jsonOutput = false }()

	client := newUnixClient(sock)
	if err := statsCmd(client); err != nil {
		t.Fatalf("stats --json: %v", err)
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0B"}, {500, "500B"}, {1024, "1.0K"},
		{1024 * 1024, "1.0M"}, {1024 * 1024 * 1024, "1.0G"}, {1536, "1.5K"},
	}
	for _, tt := range tests {
		if result := humanBytes(tt.input); result != tt.expected {
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
	if commit == "" {
		t.Error("commit should not be empty")
	}
	if buildDate == "" {
		t.Error("buildDate should not be empty")
	}
}

func TestPrintVersion(t *testing.T) {
	printVersion() // just verify no panic
}

func TestWriteJSON(t *testing.T) {
	err := writeJSON(map[string]int{"foo": 42})
	if err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
}

func TestOrDash(t *testing.T) {
	if orDash("") != "—" {
		t.Error("expected em dash for empty string")
	}
	if orDash("hello") != "hello" {
		t.Error("expected hello")
	}
}

func TestSplitTrim(t *testing.T) {
	result := splitTrim(" 10.0.0.0/8 , 172.16.0.0/12 , ")
	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d: %v", len(result), result)
	}
	if result[0] != "10.0.0.0/8" || result[1] != "172.16.0.0/12" {
		t.Errorf("unexpected result: %v", result)
	}
}

// Verify unused import suppression.
var _ = fmt.Sprintf
