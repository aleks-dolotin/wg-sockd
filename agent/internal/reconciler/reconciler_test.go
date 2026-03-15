package reconciler

import (
	"context"
	"net"
	"testing"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/config"
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

	r := New(mock, db, cfg)
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
