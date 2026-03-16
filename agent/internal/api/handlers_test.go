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
	"github.com/aleks-dolotin/wg-sockd/agent/internal/confwriter"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/storage"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/wireguard"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// noopReconcilerPauser satisfies ReconcilerPauser for unit tests —
// there is no background reconciler goroutine to pause.
type noopReconcilerPauser struct{}

func (noopReconcilerPauser) Pause()  {}
func (noopReconcilerPauser) Resume() {}

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
	h := NewHandlers(mock, db, cfg, confwriter.NewSharedWriter(), nil, noopReconcilerPauser{})
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
	h := NewHandlers(mock, db, cfg, confwriter.NewSharedWriter(), nil, noopReconcilerPauser{})
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

func TestCreatePeer_WithProfile(t *testing.T) {
	h, db := newTestHandlers(t)
	router := NewRouter(h)

	// Create a profile first.
	if err := db.CreateProfile(&storage.Profile{
		Name:        "nas-only",
		DisplayName: "NAS Only",
		AllowedIPs:  []string{"10.0.0.0/24"},
		ExcludeIPs:  []string{},
		Description: "NAS access",
	}); err != nil {
		t.Fatal(err)
	}

	body := `{"friendly_name":"NAS Phone","profile":"nas-only"}`
	req := httptest.NewRequest("POST", "/api/peers", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["profile"] != "nas-only" {
		t.Errorf("profile: got %v, want 'nas-only'", resp["profile"])
	}
	// Should have resolved allowed_ips from profile.
	ips, ok := resp["allowed_ips"].([]any)
	if !ok || len(ips) == 0 {
		t.Errorf("allowed_ips should be populated from profile, got %v", resp["allowed_ips"])
	}
}

func TestCreatePeer_ProfileAndAllowedIPs_Error(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	body := `{"friendly_name":"Both","profile":"some-profile","allowed_ips":["10.0.0.0/24"]}`
	req := httptest.NewRequest("POST", "/api/peers", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestCreatePeer_InvalidProfile_NotFound(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	body := `{"friendly_name":"Bad Profile","profile":"nonexistent"}`
	req := httptest.NewRequest("POST", "/api/peers", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestCreatePeer_NeitherProfileNorAllowedIPs_Error(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	body := `{"friendly_name":"Nothing"}`
	req := httptest.NewRequest("POST", "/api/peers", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusBadRequest, w.Body.String())
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

func TestUpdatePeer_MetadataOnly(t *testing.T) {
	h, db := newTestHandlers(t)
	router := NewRouter(h)

	key, _ := wgtypes.GeneratePrivateKey()
	id, err := db.CreatePeer(&storage.Peer{
		PublicKey:    key.PublicKey().String(),
		FriendlyName: "Old Name",
		AllowedIPs:   "10.0.0.2/32",
		Enabled:      true,
		Notes:        "old notes",
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"friendly_name":"New Name","notes":"new notes"}`
	req := httptest.NewRequest("PUT", "/api/peers/"+itoa(id), strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp PeerResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.FriendlyName != "New Name" {
		t.Errorf("FriendlyName: got %q, want %q", resp.FriendlyName, "New Name")
	}
	if resp.Notes != "new notes" {
		t.Errorf("Notes: got %q, want %q", resp.Notes, "new notes")
	}
	// AllowedIPs should be unchanged.
	if len(resp.AllowedIPs) != 1 || resp.AllowedIPs[0] != "10.0.0.2/32" {
		t.Errorf("AllowedIPs should be unchanged: got %v", resp.AllowedIPs)
	}
}

func TestUpdatePeer_AllowedIPsChange(t *testing.T) {
	h, db := newTestHandlers(t)
	router := NewRouter(h)

	key, _ := wgtypes.GeneratePrivateKey()
	id, err := db.CreatePeer(&storage.Peer{
		PublicKey:    key.PublicKey().String(),
		FriendlyName: "IP Change",
		AllowedIPs:   "10.0.0.2/32",
		Enabled:      true,
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"allowed_ips":["10.0.0.0/24","192.168.1.0/24"]}`
	req := httptest.NewRequest("PUT", "/api/peers/"+itoa(id), strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp PeerResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.AllowedIPs) != 2 {
		t.Errorf("AllowedIPs: got %v, want 2 entries", resp.AllowedIPs)
	}
}

func TestUpdatePeer_ProfileChange(t *testing.T) {
	h, db := newTestHandlers(t)
	router := NewRouter(h)

	// Create profile.
	if err := db.CreateProfile(&storage.Profile{
		Name:       "new-profile",
		AllowedIPs: []string{"10.0.0.0/24"},
		ExcludeIPs: []string{},
	}); err != nil {
		t.Fatal(err)
	}

	key, _ := wgtypes.GeneratePrivateKey()
	id, err := db.CreatePeer(&storage.Peer{
		PublicKey:    key.PublicKey().String(),
		FriendlyName: "Profile Peer",
		AllowedIPs:   "10.0.0.2/32",
		Enabled:      true,
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"profile":"new-profile"}`
	req := httptest.NewRequest("PUT", "/api/peers/"+itoa(id), strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp PeerResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Profile == nil || *resp.Profile != "new-profile" {
		t.Errorf("Profile: got %v, want 'new-profile'", resp.Profile)
	}
}

func TestUpdatePeer_NotFound(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	body := `{"friendly_name":"X"}`
	req := httptest.NewRequest("PUT", "/api/peers/99999", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestRotateKeys_Success(t *testing.T) {
	h, db := newTestHandlers(t)
	router := NewRouter(h)

	key, _ := wgtypes.GeneratePrivateKey()
	oldPubKey := key.PublicKey().String()
	id, err := db.CreatePeer(&storage.Peer{
		PublicKey:    oldPubKey,
		FriendlyName: "Rotate Me",
		AllowedIPs:   "10.0.0.2/32",
		Enabled:      true,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/peers/"+itoa(id)+"/rotate-keys", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)

	newPubKey, ok := resp["public_key"].(string)
	if !ok || newPubKey == "" {
		t.Fatal("expected non-empty public_key in response")
	}
	if newPubKey == oldPubKey {
		t.Error("new public_key should differ from old")
	}

	conf, ok := resp["config"].(string)
	if !ok || conf == "" {
		t.Fatal("expected non-empty config in response")
	}
	if !strings.Contains(conf, "[Interface]") || !strings.Contains(conf, "PrivateKey") {
		t.Error("config should contain [Interface] and PrivateKey")
	}

	// Verify DB was updated.
	updated, err := db.GetPeerByID(id)
	if err != nil {
		t.Fatalf("GetPeerByID: %v", err)
	}
	if updated.PublicKey != newPubKey {
		t.Errorf("DB public_key: got %q, want %q", updated.PublicKey, newPubKey)
	}
	// ID should be the same.
	if updated.ID != id {
		t.Errorf("peer ID changed: got %d, want %d", updated.ID, id)
	}
}

func TestGetPeerQR_Success(t *testing.T) {
	h, db := newTestHandlers(t)
	router := NewRouter(h)

	key, _ := wgtypes.GeneratePrivateKey()
	id, err := db.CreatePeer(&storage.Peer{
		PublicKey:    key.PublicKey().String(),
		FriendlyName: "QR Peer",
		AllowedIPs:   "10.0.0.2/32",
		Enabled:      true,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/api/peers/"+itoa(id)+"/qr", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if ct != "image/png" {
		t.Errorf("Content-Type: got %q, want image/png", ct)
	}

	// Verify it's a valid PNG (starts with PNG magic bytes).
	body := w.Body.Bytes()
	if len(body) < 8 {
		t.Fatal("response too short to be a PNG")
	}
	pngMagic := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	for i, b := range pngMagic {
		if body[i] != b {
			t.Fatalf("not a valid PNG: byte %d is %x, want %x", i, body[i], b)
		}
	}
}

func TestStats_WithPeers(t *testing.T) {
	db, err := storage.NewDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	now := time.Now()
	peerKey1, _ := wgtypes.GeneratePrivateKey()
	peerKey2, _ := wgtypes.GeneratePrivateKey()
	peerKey3, _ := wgtypes.GeneratePrivateKey()

	_, cidr, _ := net.ParseCIDR("10.0.0.2/32")
	mock := &mockWgClient{
		device: &wireguard.Device{
			Name:       "wg0",
			PublicKey:  wgtypes.Key{},
			ListenPort: 51820,
			Peers: []wireguard.Peer{
				{PublicKey: peerKey1.PublicKey(), AllowedIPs: []net.IPNet{*cidr},
					LastHandshake: now.Add(-1 * time.Minute), ReceiveBytes: 1000, TransmitBytes: 500},
				{PublicKey: peerKey2.PublicKey(), AllowedIPs: []net.IPNet{*cidr},
					LastHandshake: now.Add(-2 * time.Minute), ReceiveBytes: 2000, TransmitBytes: 1000},
				{PublicKey: peerKey3.PublicKey(), AllowedIPs: []net.IPNet{*cidr},
					LastHandshake: now.Add(-10 * time.Minute), ReceiveBytes: 500, TransmitBytes: 200},
			},
		},
	}

	cfg := config.Defaults()
	cfg.ConfPath = t.TempDir() + "/wg0.conf"
	h := NewHandlers(mock, db, cfg, confwriter.NewSharedWriter(), nil, noopReconcilerPauser{})
	router := NewRouter(h)

	req := httptest.NewRequest("GET", "/api/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var resp StatsResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.TotalPeers != 3 {
		t.Errorf("TotalPeers: got %d, want 3", resp.TotalPeers)
	}
	if resp.OnlinePeers != 2 {
		t.Errorf("OnlinePeers: got %d, want 2 (peers with handshake < 3min)", resp.OnlinePeers)
	}
	if resp.TotalRx != 3500 {
		t.Errorf("TotalRx: got %d, want 3500", resp.TotalRx)
	}
	if resp.TotalTx != 1700 {
		t.Errorf("TotalTx: got %d, want 1700", resp.TotalTx)
	}
}

func TestStats_NoPeers(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	req := httptest.NewRequest("GET", "/api/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var resp StatsResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.TotalPeers != 0 {
		t.Errorf("TotalPeers: got %d, want 0", resp.TotalPeers)
	}
}

func TestApprovePeer_Success(t *testing.T) {
	h, db := newTestHandlers(t)
	router := NewRouter(h)

	// Create an auto-discovered, disabled peer (simulating reconciler).
	key, _ := wgtypes.GeneratePrivateKey()
	id, err := db.CreatePeer(&storage.Peer{
		PublicKey:      key.PublicKey().String(),
		FriendlyName:   "unknown-abcd1234",
		AllowedIPs:     "10.0.0.99/32",
		Enabled:        false,
		AutoDiscovered: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/peers/"+itoa(id)+"/approve", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp PeerResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Enabled {
		t.Error("peer should be enabled after approval")
	}
	if resp.AutoDiscovered {
		t.Error("auto_discovered should be false after approval")
	}
}

func TestApprovePeer_AlreadyEnabled(t *testing.T) {
	h, db := newTestHandlers(t)
	router := NewRouter(h)

	key, _ := wgtypes.GeneratePrivateKey()
	id, err := db.CreatePeer(&storage.Peer{
		PublicKey:      key.PublicKey().String(),
		FriendlyName:   "already-enabled",
		AllowedIPs:     "10.0.0.2/32",
		Enabled:        true,
		AutoDiscovered: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/peers/"+itoa(id)+"/approve", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestApprovePeer_NotAutoDiscovered(t *testing.T) {
	h, db := newTestHandlers(t)
	router := NewRouter(h)

	key, _ := wgtypes.GeneratePrivateKey()
	id, err := db.CreatePeer(&storage.Peer{
		PublicKey:      key.PublicKey().String(),
		FriendlyName:   "manual-peer",
		AllowedIPs:     "10.0.0.2/32",
		Enabled:        false,
		AutoDiscovered: false,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/peers/"+itoa(id)+"/approve", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestApprovePeer_NotFound(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	req := httptest.NewRequest("POST", "/api/peers/99999/approve", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestGetPeerQR_NotFound(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	req := httptest.NewRequest("GET", "/api/peers/99999/qr", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestRotateKeys_NotFound(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	req := httptest.NewRequest("POST", "/api/peers/99999/rotate-keys", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestBatchCreatePeers_Success(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	body := `{"peers":[
		{"friendly_name":"Batch1","allowed_ips":["10.0.0.1/32"]},
		{"friendly_name":"Batch2","allowed_ips":["10.0.0.2/32"]},
		{"friendly_name":"Batch3","allowed_ips":["10.0.0.3/32"]}
	]}`
	req := httptest.NewRequest("POST", "/api/peers/batch", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp []PeerResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp) != 3 {
		t.Fatalf("expected 3 peers, got %d", len(resp))
	}
	for i, p := range resp {
		if p.PublicKey == "" {
			t.Errorf("peer[%d]: missing public_key", i)
		}
	}
}

func TestBatchCreatePeers_InvalidPeer_Rejected(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	// Second peer has no allowed_ips and no profile — should fail.
	body := `{"peers":[
		{"friendly_name":"Good","allowed_ips":["10.0.0.1/32"]},
		{"friendly_name":"Bad"}
	]}`
	req := httptest.NewRequest("POST", "/api/peers/batch", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestBatchCreatePeers_Empty(t *testing.T) {
	h, _ := newTestHandlers(t)
	router := NewRouter(h)

	body := `{"peers":[]}`
	req := httptest.NewRequest("POST", "/api/peers/batch", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestUpdatePeer_Disable(t *testing.T) {
	h, db := newTestHandlers(t)
	router := NewRouter(h)

	key, _ := wgtypes.GeneratePrivateKey()
	id, err := db.CreatePeer(&storage.Peer{
		PublicKey:    key.PublicKey().String(),
		FriendlyName: "Disable Me",
		AllowedIPs:   "10.0.0.2/32",
		Enabled:      true,
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"enabled":false}`
	req := httptest.NewRequest("PUT", "/api/peers/"+itoa(id), strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp PeerResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Enabled {
		t.Error("Enabled should be false after update")
	}
}
