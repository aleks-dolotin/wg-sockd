package metrics

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/storage"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/wireguard"
)

type mockWgClient struct {
	device    *wireguard.Device
	deviceErr error
}

func (m *mockWgClient) GetDevice(name string) (*wireguard.Device, error) {
	if m.deviceErr != nil {
		return nil, m.deviceErr
	}
	return m.device, nil
}
func (m *mockWgClient) ConfigurePeers(string, []wireguard.PeerConfig) error { return nil }
func (m *mockWgClient) RemovePeer(string, wgtypes.Key) error               { return nil }
func (m *mockWgClient) GenerateKeyPair() (wgtypes.Key, wgtypes.Key, error) {
	k, _ := wgtypes.GeneratePrivateKey()
	return k, k.PublicKey(), nil
}
func (m *mockWgClient) GeneratePresharedKey() (wgtypes.Key, error) { return wgtypes.GenerateKey() }
func (m *mockWgClient) Close() error                               { return nil }

func TestCollector_BasicMetrics(t *testing.T) {
	// Create in-memory DB.
	db, err := storage.NewDB(":memory:")
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	// Insert test peers.
	pubKey1, _ := wgtypes.GeneratePrivateKey()
	pubKey2, _ := wgtypes.GeneratePrivateKey()
	profile := "full-tunnel"

	// Create profile first (FK constraint).
	if err := db.CreateProfile(&storage.Profile{
		Name:       profile,
		AllowedIPs: []string{"0.0.0.0/0"},
	}); err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	if _, err := db.CreatePeer(&storage.Peer{
		PublicKey:     pubKey1.PublicKey().String(),
		FriendlyName: "alice",
		AllowedIPs:   "10.0.0.2/32",
		Profile:       &profile,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("CreatePeer: %v", err)
	}
	if _, err := db.CreatePeer(&storage.Peer{
		PublicKey:     pubKey2.PublicKey().String(),
		FriendlyName: "bob",
		AllowedIPs:   "10.0.0.3/32",
		Enabled:       false,
	}); err != nil {
		t.Fatalf("CreatePeer: %v", err)
	}

	// Mock wgctrl device.
	wgClient := &mockWgClient{
		device: &wireguard.Device{
			Name: "wg0",
			Peers: []wireguard.Peer{
				{
					PublicKey:     pubKey1.PublicKey(),
					ReceiveBytes:  1024,
					TransmitBytes: 2048,
					LastHandshake: time.Now().Add(-1 * time.Minute),
					Endpoint:      &net.UDPAddr{IP: net.ParseIP("1.2.3.4"), Port: 51820},
				},
				{
					PublicKey:     pubKey2.PublicKey(),
					ReceiveBytes:  512,
					TransmitBytes: 256,
					LastHandshake: time.Now().Add(-10 * time.Minute),
				},
			},
		},
	}

	collector := New(wgClient, db, "wg0")
	registry := prometheus.NewPedanticRegistry()
	registry.MustRegister(collector)

	// Gather and verify key metrics exist.
	metrics, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}

	expectedNames := map[string]bool{
		"wireguard_peer_receive_bytes_total":    false,
		"wireguard_peer_transmit_bytes_total":   false,
		"wireguard_peer_last_handshake_seconds": false,
		"wireguard_peer_is_online":              false,
		"wireguard_peer_enabled":                false,
		"wireguard_peers_total":                 false,
		"wireguard_peers_online":                false,
		"wireguard_transfer_receive_bytes_total":  false,
		"wireguard_transfer_transmit_bytes_total": false,
	}

	for _, mf := range metrics {
		if _, ok := expectedNames[mf.GetName()]; ok {
			expectedNames[mf.GetName()] = true
		}
	}

	for name, found := range expectedNames {
		if !found {
			t.Errorf("expected metric %q not found", name)
		}
	}

	// Verify aggregate values.
	expected := `
		# HELP wireguard_peers_total Total number of peers.
		# TYPE wireguard_peers_total gauge
		wireguard_peers_total 2
		# HELP wireguard_peers_online Number of currently online peers.
		# TYPE wireguard_peers_online gauge
		wireguard_peers_online 1
	`
	if err := testutil.CollectAndCompare(collector, strings.NewReader(expected),
		"wireguard_peers_total", "wireguard_peers_online"); err != nil {
		t.Errorf("aggregate metrics mismatch: %v", err)
	}
}

func TestCollector_DegradedMode(t *testing.T) {
	db, err := storage.NewDB(":memory:")
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	pubKey, _ := wgtypes.GeneratePrivateKey()
	if _, err := db.CreatePeer(&storage.Peer{
		PublicKey:     pubKey.PublicKey().String(),
		FriendlyName: "charlie",
		AllowedIPs:   "10.0.0.4/32",
		Enabled:       true,
	}); err != nil {
		t.Fatalf("CreatePeer: %v", err)
	}

	wgClient := &mockWgClient{deviceErr: fmt.Errorf("wgctrl unavailable")}

	collector := New(wgClient, db, "wg0")
	registry := prometheus.NewPedanticRegistry()
	registry.MustRegister(collector)

	// Should not panic — should emit DB-only metrics.
	metrics, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather in degraded: %v", err)
	}

	if len(metrics) == 0 {
		t.Error("expected metrics in degraded mode")
	}
}
