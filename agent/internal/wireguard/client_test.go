package wireguard

import (
	"net"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// MockClient implements WireGuardClient for testing.
type MockClient struct {
	GetDeviceFunc           func(name string) (*Device, error)
	ConfigurePeersFunc      func(name string, peers []PeerConfig) error
	RemovePeerFunc          func(name string, pubKey wgtypes.Key) error
	GenerateKeyPairFunc     func() (wgtypes.Key, wgtypes.Key, error)
	GeneratePresharedKeyFunc func() (wgtypes.Key, error)
	CloseFunc               func() error

	// Recorded calls for assertions
	ConfigurePeersCalls []ConfigurePeersCall
	RemovePeerCalls     []RemovePeerCall
}

type ConfigurePeersCall struct {
	Name  string
	Peers []PeerConfig
}

type RemovePeerCall struct {
	Name   string
	PubKey wgtypes.Key
}

var _ WireGuardClient = (*MockClient)(nil)

func (m *MockClient) GetDevice(name string) (*Device, error) {
	if m.GetDeviceFunc != nil {
		return m.GetDeviceFunc(name)
	}
	return &Device{Name: name}, nil
}

func (m *MockClient) ConfigurePeers(name string, peers []PeerConfig) error {
	m.ConfigurePeersCalls = append(m.ConfigurePeersCalls, ConfigurePeersCall{Name: name, Peers: peers})
	if m.ConfigurePeersFunc != nil {
		return m.ConfigurePeersFunc(name, peers)
	}
	return nil
}

func (m *MockClient) RemovePeer(name string, pubKey wgtypes.Key) error {
	m.RemovePeerCalls = append(m.RemovePeerCalls, RemovePeerCall{Name: name, PubKey: pubKey})
	if m.RemovePeerFunc != nil {
		return m.RemovePeerFunc(name, pubKey)
	}
	return nil
}

func (m *MockClient) GenerateKeyPair() (wgtypes.Key, wgtypes.Key, error) {
	if m.GenerateKeyPairFunc != nil {
		return m.GenerateKeyPairFunc()
	}
	priv, _ := wgtypes.GeneratePrivateKey()
	return priv, priv.PublicKey(), nil
}

func (m *MockClient) GeneratePresharedKey() (wgtypes.Key, error) {
	if m.GeneratePresharedKeyFunc != nil {
		return m.GeneratePresharedKeyFunc()
	}
	return wgtypes.GenerateKey()
}

func (m *MockClient) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

// --- Unit Tests ---

func TestMockClient_ImplementsInterface(t *testing.T) {
	var _ WireGuardClient = &MockClient{}
}

func TestGenerateKeyPair_ProducesValidKeys(t *testing.T) {
	mock := &MockClient{}
	priv, pub, err := mock.GenerateKeyPair()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Keys are 32 bytes
	if len(priv) != 32 {
		t.Errorf("private key length: got %d, want 32", len(priv))
	}
	if len(pub) != 32 {
		t.Errorf("public key length: got %d, want 32", len(pub))
	}

	// Private and public keys should differ
	if priv == pub {
		t.Error("private and public keys should be different")
	}

	// Keys should not be zero
	var zeroKey wgtypes.Key
	if priv == zeroKey {
		t.Error("private key should not be zero")
	}
	if pub == zeroKey {
		t.Error("public key should not be zero")
	}
}

func TestMockClient_ConfigurePeersRecordsCalls(t *testing.T) {
	mock := &MockClient{}
	key, _ := wgtypes.GeneratePrivateKey()

	_, cidr, _ := net.ParseCIDR("10.0.0.2/32")
	peers := []PeerConfig{
		{
			PublicKey:   key,
			AllowedIPs:  []net.IPNet{*cidr},
		},
	}

	err := mock.ConfigurePeers("wg0", peers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.ConfigurePeersCalls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.ConfigurePeersCalls))
	}
	call := mock.ConfigurePeersCalls[0]
	if call.Name != "wg0" {
		t.Errorf("name: got %q, want %q", call.Name, "wg0")
	}
	if len(call.Peers) != 1 {
		t.Errorf("peers count: got %d, want 1", len(call.Peers))
	}
	if call.Peers[0].PublicKey != key {
		t.Error("peer public key mismatch")
	}
}

func TestPeerConfigToWgctrl_AddPeer(t *testing.T) {
	key, _ := wgtypes.GeneratePrivateKey()
	_, cidr, _ := net.ParseCIDR("10.0.0.2/32")

	pc := PeerConfig{
		PublicKey:  key,
		AllowedIPs: []net.IPNet{*cidr},
	}

	wgpc := peerConfigToWgctrl(pc)

	if wgpc.PublicKey != key {
		t.Error("public key mismatch")
	}
	if wgpc.Remove {
		t.Error("Remove should be false for add")
	}
	if len(wgpc.AllowedIPs) != 1 {
		t.Errorf("AllowedIPs count: got %d, want 1", len(wgpc.AllowedIPs))
	}
}

func TestPeerConfigToWgctrl_RemovePeer(t *testing.T) {
	key, _ := wgtypes.GeneratePrivateKey()

	pc := PeerConfig{
		PublicKey: key,
		Remove:    true,
	}

	wgpc := peerConfigToWgctrl(pc)

	if !wgpc.Remove {
		t.Error("Remove should be true")
	}
}

func TestPeerConfigToWgctrl_UpdatePeer(t *testing.T) {
	key, _ := wgtypes.GeneratePrivateKey()
	_, cidr, _ := net.ParseCIDR("10.0.0.3/32")

	pc := PeerConfig{
		PublicKey:         key,
		AllowedIPs:        []net.IPNet{*cidr},
		ReplaceAllowedIPs: true,
	}

	wgpc := peerConfigToWgctrl(pc)

	if !wgpc.ReplaceAllowedIPs {
		t.Error("ReplaceAllowedIPs should be true for update")
	}
}

func TestDeviceFromWgctrl(t *testing.T) {
	key, _ := wgtypes.GeneratePrivateKey()
	peerKey, _ := wgtypes.GeneratePrivateKey()
	_, cidr, _ := net.ParseCIDR("10.0.0.2/32")
	now := time.Now()

	wgDev := &wgtypes.Device{
		Name:       "wg0",
		PublicKey:  key.PublicKey(),
		ListenPort: 51820,
		Peers: []wgtypes.Peer{
			{
				PublicKey:          peerKey.PublicKey(),
				Endpoint:           &net.UDPAddr{IP: net.ParseIP("1.2.3.4"), Port: 12345},
				AllowedIPs:         []net.IPNet{*cidr},
				LastHandshakeTime:  now,
				ReceiveBytes:       1000,
				TransmitBytes:      2000,
			},
		},
	}

	dev := deviceFromWgctrl(wgDev)

	if dev.Name != "wg0" {
		t.Errorf("Name: got %q, want %q", dev.Name, "wg0")
	}
	if dev.ListenPort != 51820 {
		t.Errorf("ListenPort: got %d, want 51820", dev.ListenPort)
	}
	if len(dev.Peers) != 1 {
		t.Fatalf("Peers count: got %d, want 1", len(dev.Peers))
	}
	peer := dev.Peers[0]
	if peer.PublicKey != peerKey.PublicKey() {
		t.Error("peer public key mismatch")
	}
	if peer.ReceiveBytes != 1000 {
		t.Errorf("ReceiveBytes: got %d, want 1000", peer.ReceiveBytes)
	}
	if peer.TransmitBytes != 2000 {
		t.Errorf("TransmitBytes: got %d, want 2000", peer.TransmitBytes)
	}
}
