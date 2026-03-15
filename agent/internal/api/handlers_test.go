package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/config"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/storage"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/wireguard"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// mockWgClient implements wireguard.WireGuardClient for testing.
type mockWgClient struct {
	device    *wireguard.Device
	deviceErr error
	configErr error
	removeErr error
}

func (m *mockWgClient) GetDevice(name string) (*wireguard.Device, error) {
	if m.deviceErr != nil {
		return nil, m.deviceErr
	}
	if m.device != nil {
		return m.device, nil
	}
	key, _ := wgtypes.GeneratePrivateKey()
	return &wireguard.Device{
		Name:       name,
		PublicKey:  key.PublicKey(),
		ListenPort: 51820,
	}, nil
}

func (m *mockWgClient) ConfigurePeers(name string, peers []wireguard.PeerConfig) error {
	return m.configErr
}

func (m *mockWgClient) RemovePeer(name string, pubKey wgtypes.Key) error {
	return m.removeErr
}

func (m *mockWgClient) GenerateKeyPair() (wgtypes.Key, wgtypes.Key, error) {
	priv, _ := wgtypes.GeneratePrivateKey()
	return priv, priv.PublicKey(), nil
}

func (m *mockWgClient) Close() error { return nil }

func newTestHandlers(t *testing.T) (*Handlers, *storage.DB) {
	t.Helper()
	db, err := storage.NewDB(":memory:")
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	cfg := config.Defaults()
	cfg.ConfPath = t.TempDir() + "/wg0.conf"

	mock := &mockWgClient{}
	h := NewHandlers(mock, db, cfg)
	return h, db
}

func TestHealth_OK(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var resp HealthResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Status != "ok" {
		t.Errorf("status: got %q, want %q", resp.Status, "ok")
	}
	if resp.WireGuard != "ok" {
		t.Errorf("wireguard: got %q, want %q", resp.WireGuard, "ok")
	}
	if resp.SQLite != "ok" {
		t.Errorf("sqlite: got %q, want %q", resp.SQLite, "ok")
	}
}

func TestListPeers_Empty(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	req := httptest.NewRequest("GET", "/api/peers", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var peers []PeerResponse
	json.NewDecoder(w.Body).Decode(&peers)
	if len(peers) != 0 {
		t.Errorf("expected 0 peers, got %d", len(peers))
	}
}

func TestListPeers_WithDBPeers(t *testing.T) {
	h, db := newTestHandlers(t)
	router := NewRouter(h)

	// Insert a peer directly into DB.
	_, err := db.CreatePeer(&storage.Peer{
		PublicKey:     "test-pub-key",
		FriendlyName:  "Test Peer",
		AllowedIPs:    "10.0.0.2/32",
		Enabled:       true,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/api/peers", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var peers []PeerResponse
	json.NewDecoder(w.Body).Decode(&peers)
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}
	if peers[0].FriendlyName != "Test Peer" {
		t.Errorf("friendly_name: got %q, want %q", peers[0].FriendlyName, "Test Peer")
	}
	if len(peers[0].AllowedIPs) != 1 || peers[0].AllowedIPs[0] != "10.0.0.2/32" {
		t.Errorf("allowed_ips: got %v", peers[0].AllowedIPs)
	}
}

func TestListPeers_JoinsLiveData(t *testing.T) {
	db, err := storage.NewDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	peerKey, _ := wgtypes.GeneratePrivateKey()
	pubKey := peerKey.PublicKey()

	_, err = db.CreatePeer(&storage.Peer{
		PublicKey:     pubKey.String(),
		FriendlyName:  "Live Peer",
		AllowedIPs:    "10.0.0.2/32",
		Enabled:       true,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, cidr, _ := net.ParseCIDR("10.0.0.2/32")
	now := time.Now()
	mock := &mockWgClient{
		device: &wireguard.Device{
			Name:       "wg0",
			PublicKey:  wgtypes.Key{},
			ListenPort: 51820,
			Peers: []wireguard.Peer{
				{
					PublicKey:     pubKey,
					Endpoint:      &net.UDPAddr{IP: net.ParseIP("1.2.3.4"), Port: 12345},
					AllowedIPs:    []net.IPNet{*cidr},
					LastHandshake: now,
					ReceiveBytes:  5000,
					TransmitBytes: 3000,
				},
			},
		},
	}

	cfg := config.Defaults()
	cfg.ConfPath = t.TempDir() + "/wg0.conf"
	h := NewHandlers(mock, db, cfg)
	router := NewRouter(h)

	req := httptest.NewRequest("GET", "/api/peers", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var peers []PeerResponse
	json.NewDecoder(w.Body).Decode(&peers)

	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}
	if peers[0].Endpoint != "1.2.3.4:12345" {
		t.Errorf("endpoint: got %q, want %q", peers[0].Endpoint, "1.2.3.4:12345")
	}
	if peers[0].TransferRx != 5000 {
		t.Errorf("transfer_rx: got %d, want 5000", peers[0].TransferRx)
	}
	if peers[0].TransferTx != 3000 {
		t.Errorf("transfer_tx: got %d, want 3000", peers[0].TransferTx)
	}
}

func TestCreatePeer(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	body := `{"friendly_name":"New Phone","allowed_ips":["10.0.0.5/32"]}`
	req := httptest.NewRequest("POST", "/api/peers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["friendly_name"] != "New Phone" {
		t.Errorf("friendly_name: got %v", resp["friendly_name"])
	}
	if resp["private_key"] == nil || resp["private_key"] == "" {
		t.Error("private_key should be returned on create")
	}
	if resp["public_key"] == nil || resp["public_key"] == "" {
		t.Error("public_key should be returned")
	}
}

func TestCreatePeer_InvalidCIDR(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	body := `{"friendly_name":"Bad","allowed_ips":["not-a-cidr"]}`
	req := httptest.NewRequest("POST", "/api/peers", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCreatePeer_EmptyAllowedIPs(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	body := `{"friendly_name":"NoIPs","allowed_ips":[]}`
	req := httptest.NewRequest("POST", "/api/peers", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestDeletePeer(t *testing.T) {
	h, db := newTestHandlers(t)
	router := NewRouter(h)

	// Create a peer with a valid base64 key.
	key, _ := wgtypes.GeneratePrivateKey()
	id, err := db.CreatePeer(&storage.Peer{
		PublicKey:     key.PublicKey().String(),
		FriendlyName:  "To Delete",
		AllowedIPs:    "10.0.0.2/32",
		Enabled:       true,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("DELETE", "/api/peers/"+itoa(id), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusNoContent, w.Body.String())
	}
}

func TestDeletePeer_NotFound(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	req := httptest.NewRequest("DELETE", "/api/peers/99999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestDeletePeer_InvalidID(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	req := httptest.NewRequest("DELETE", "/api/peers/abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestGetPeerConf(t *testing.T) {
	h, db := newTestHandlers(t)
	router := NewRouter(h)

	key, _ := wgtypes.GeneratePrivateKey()
	id, err := db.CreatePeer(&storage.Peer{
		PublicKey:     key.PublicKey().String(),
		FriendlyName:  "ConfPeer",
		AllowedIPs:    "10.0.0.2/32",
		Enabled:       true,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/api/peers/"+itoa(id)+"/conf", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if ct != "text/plain" {
		t.Errorf("Content-Type: got %q, want text/plain", ct)
	}

	body := w.Body.String()
	if !strings.Contains(body, "[Interface]") {
		t.Error("conf should contain [Interface]")
	}
	if !strings.Contains(body, "[Peer]") {
		t.Error("conf should contain [Peer]")
	}
	if !strings.Contains(body, "PersistentKeepalive") {
		t.Error("conf should contain PersistentKeepalive")
	}
}

func itoa(i int64) string {
	return fmt.Sprintf("%d", i)
}
