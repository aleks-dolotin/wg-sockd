package reconciler

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/config"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/confwriter"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/storage"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/wireguard"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type mockWgClient struct {
	device         *wireguard.Device
	configCalls    [][]wireguard.PeerConfig
	removedKeys    []wgtypes.Key
}

func (m *mockWgClient) GetDevice(name string) (*wireguard.Device, error) {
	if m.device != nil {
		return m.device, nil
	}
	return &wireguard.Device{Name: name}, nil
}

func (m *mockWgClient) ConfigurePeers(name string, peers []wireguard.PeerConfig) error {
	m.configCalls = append(m.configCalls, peers)
	return nil
}

func (m *mockWgClient) RemovePeer(name string, pubKey wgtypes.Key) error {
	m.removedKeys = append(m.removedKeys, pubKey)
	return nil
}

func (m *mockWgClient) GenerateKeyPair() (wgtypes.Key, wgtypes.Key, error) {
	k, _ := wgtypes.GeneratePrivateKey()
	return k, k.PublicKey(), nil
}

func (m *mockWgClient) GeneratePresharedKey() (wgtypes.Key, error) { return wgtypes.GenerateKey() }

func (m *mockWgClient) Close() error { return nil }

var _ wireguard.WireGuardClient = (*mockWgClient)(nil)

func newTestReconciler(t *testing.T, mock *mockWgClient) (*Reconciler, *storage.DB) {
	t.Helper()
	db, err := storage.NewDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	cfg := config.Defaults()
	cfg.ConfPath = t.TempDir() + "/wg0.conf"

	r := New(mock, db, cfg, confwriter.NewSharedWriter())
	return r, db
}

func TestReconcileOnce_EmptyBoth(t *testing.T) {
	mock := &mockWgClient{}
	r, _ := newTestReconciler(t, mock)

	err := r.ReconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.removedKeys) != 0 {
		t.Errorf("expected 0 removals, got %d", len(mock.removedKeys))
	}
	if len(mock.configCalls) != 0 {
		t.Errorf("expected 0 config calls, got %d", len(mock.configCalls))
	}
}

func TestReconcileOnce_UnknownPeerRemoved(t *testing.T) {
	unknownKey, _ := wgtypes.GeneratePrivateKey()
	unknownPub := unknownKey.PublicKey()
	_, cidr, _ := net.ParseCIDR("10.0.0.99/32")

	mock := &mockWgClient{
		device: &wireguard.Device{
			Name: "wg0",
			Peers: []wireguard.Peer{
				{
					PublicKey:  unknownPub,
					AllowedIPs: []net.IPNet{*cidr},
				},
			},
		},
	}

	r, db := newTestReconciler(t, mock)

	err := r.ReconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have removed the unknown peer from kernel.
	if len(mock.removedKeys) != 1 {
		t.Fatalf("expected 1 removal, got %d", len(mock.removedKeys))
	}
	if mock.removedKeys[0] != unknownPub {
		t.Error("wrong key removed")
	}

	// Should have inserted into DB as disabled.
	dbPeer, err := db.GetPeerByPubKey(unknownPub.String())
	if err != nil {
		t.Fatalf("peer should be in DB: %v", err)
	}
	if dbPeer.Enabled {
		t.Error("unknown peer should be disabled")
	}
	if !dbPeer.AutoDiscovered {
		t.Error("unknown peer should be auto_discovered")
	}
	if dbPeer.FriendlyName == "" {
		t.Error("friendly_name should be set")
	}
}

func TestReconcileOnce_MissingPeerReAdded(t *testing.T) {
	peerKey, _ := wgtypes.GeneratePrivateKey()
	peerPub := peerKey.PublicKey()

	mock := &mockWgClient{
		device: &wireguard.Device{
			Name:  "wg0",
			Peers: []wireguard.Peer{}, // empty kernel
		},
	}

	r, db := newTestReconciler(t, mock)

	// Insert an enabled peer in DB.
	_, err := db.CreatePeer(&storage.Peer{
		PublicKey:    peerPub.String(),
		FriendlyName: "My Peer",
		AllowedIPs:   "10.0.0.2/32",
		Enabled:      true,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = r.ReconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have re-added the peer to kernel.
	if len(mock.configCalls) != 1 {
		t.Fatalf("expected 1 config call, got %d", len(mock.configCalls))
	}
	if len(mock.configCalls[0]) != 1 {
		t.Fatalf("expected 1 peer in config call, got %d", len(mock.configCalls[0]))
	}
	if mock.configCalls[0][0].PublicKey != peerPub {
		t.Error("wrong key re-added")
	}
}

func TestReconcileOnce_PeersInBoth_NoChanges(t *testing.T) {
	peerKey, _ := wgtypes.GeneratePrivateKey()
	peerPub := peerKey.PublicKey()
	_, cidr, _ := net.ParseCIDR("10.0.0.2/32")

	mock := &mockWgClient{
		device: &wireguard.Device{
			Name: "wg0",
			Peers: []wireguard.Peer{
				{
					PublicKey:  peerPub,
					AllowedIPs: []net.IPNet{*cidr},
				},
			},
		},
	}

	r, db := newTestReconciler(t, mock)

	// Insert same peer in DB.
	_, err := db.CreatePeer(&storage.Peer{
		PublicKey:    peerPub.String(),
		FriendlyName: "Existing Peer",
		AllowedIPs:   "10.0.0.2/32",
		Enabled:      true,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = r.ReconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No removals, no re-adds.
	if len(mock.removedKeys) != 0 {
		t.Errorf("expected 0 removals, got %d", len(mock.removedKeys))
	}
	if len(mock.configCalls) != 0 {
		t.Errorf("expected 0 config calls, got %d", len(mock.configCalls))
	}
}

func TestReconcileOnce_UnknownPeerStrictMode(t *testing.T) {
	unknownKey, _ := wgtypes.GeneratePrivateKey()
	unknownPub := unknownKey.PublicKey()
	_, cidr, _ := net.ParseCIDR("10.0.0.99/32")
	ep := &net.UDPAddr{IP: net.ParseIP("203.0.113.50"), Port: 41820}

	mock := &mockWgClient{
		device: &wireguard.Device{
			Name: "wg0",
			Peers: []wireguard.Peer{
				{PublicKey: unknownPub, AllowedIPs: []net.IPNet{*cidr}, Endpoint: ep},
			},
		},
	}

	db, err := storage.NewDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	cfg := config.Defaults()
	cfg.ConfPath = t.TempDir() + "/wg0.conf"

	r := New(mock, db, cfg, confwriter.NewSharedWriter())

	err = r.ReconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Strict mode: always removed from kernel.
	if len(mock.removedKeys) != 1 {
		t.Errorf("expected 1 removal, got %d", len(mock.removedKeys))
	}

	// Stored as disabled, auto_discovered, with last_seen_endpoint populated.
	dbPeer, err := db.GetPeerByPubKey(unknownPub.String())
	if err != nil {
		t.Fatalf("peer should be in DB: %v", err)
	}
	if !dbPeer.AutoDiscovered {
		t.Error("should be auto_discovered")
	}
	if dbPeer.Enabled {
		t.Error("should be disabled pending approval")
	}
	if dbPeer.LastSeenEndpoint != "203.0.113.50:41820" {
		t.Errorf("LastSeenEndpoint: got %q, want %q", dbPeer.LastSeenEndpoint, "203.0.113.50:41820")
	}
	if dbPeer.Endpoint != "" {
		t.Errorf("configured Endpoint should be empty, got %q", dbPeer.Endpoint)
	}
}

func TestRunLoop_StopsOnCancel(t *testing.T) {
	mock := &mockWgClient{}
	r, _ := newTestReconciler(t, mock)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		r.RunLoop(ctx, 10*time.Millisecond)
		close(done)
	}()

	// Let a few ticks happen.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Success — RunLoop returned.
	case <-time.After(2 * time.Second):
		t.Fatal("RunLoop did not stop after context cancellation")
	}
}
