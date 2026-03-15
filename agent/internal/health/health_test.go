package health

import (
	"fmt"
	"net"
	"testing"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/storage"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/wireguard"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type mockWgClient struct {
	err error
}

func (m *mockWgClient) GetDevice(name string) (*wireguard.Device, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &wireguard.Device{Name: name}, nil
}

func (m *mockWgClient) ConfigurePeers(name string, peers []wireguard.PeerConfig) error { return nil }
func (m *mockWgClient) RemovePeer(name string, pubKey wgtypes.Key) error               { return nil }
func (m *mockWgClient) GenerateKeyPair() (wgtypes.Key, wgtypes.Key, error) {
	k, _ := wgtypes.GeneratePrivateKey()
	return k, k.PublicKey(), nil
}
func (m *mockWgClient) Close() error { return nil }

// Ensure mockWgClient satisfies the interface at compile time.
var _ wireguard.WireGuardClient = (*mockWgClient)(nil)

// Ensure net is used (needed for wireguard.Peer).
var _ = net.ParseIP

func TestCheck_AllHealthy(t *testing.T) {
	db, err := storage.NewDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := NewChecker(&mockWgClient{}, db, "wg0")
	resp := c.Check()

	if resp.Status != "ok" {
		t.Errorf("Status: got %q, want %q", resp.Status, "ok")
	}
	if resp.WireGuard != "ok" {
		t.Errorf("WireGuard: got %q, want %q", resp.WireGuard, "ok")
	}
	if resp.SQLite != "ok" {
		t.Errorf("SQLite: got %q, want %q", resp.SQLite, "ok")
	}
}

func TestCheck_WireGuardDown(t *testing.T) {
	db, err := storage.NewDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := NewChecker(&mockWgClient{err: fmt.Errorf("device not found")}, db, "wg0")
	resp := c.Check()

	if resp.Status != "degraded" {
		t.Errorf("Status: got %q, want %q", resp.Status, "degraded")
	}
	if resp.WireGuard != "error" {
		t.Errorf("WireGuard: got %q, want %q", resp.WireGuard, "error")
	}
	if resp.SQLite != "ok" {
		t.Errorf("SQLite: got %q, want %q", resp.SQLite, "ok")
	}
}

func TestCheck_BothDown(t *testing.T) {
	db, err := storage.NewDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	// Close DB to make SQLite check fail.
	db.Close()

	c := NewChecker(&mockWgClient{err: fmt.Errorf("device not found")}, db, "wg0")
	resp := c.Check()

	if resp.Status != "unavailable" {
		t.Errorf("Status: got %q, want %q", resp.Status, "unavailable")
	}
	if resp.WireGuard != "error" {
		t.Errorf("WireGuard: got %q, want %q", resp.WireGuard, "error")
	}
	if resp.SQLite != "error" {
		t.Errorf("SQLite: got %q, want %q", resp.SQLite, "error")
	}
}

func TestCheck_RecoveredFrom(t *testing.T) {
	db, err := storage.NewDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := NewChecker(&mockWgClient{}, db, "wg0")
	c.SetRecoveredFrom("conf-comments")
	resp := c.Check()

	if resp.SQLiteRecoveredFrom != "conf-comments" {
		t.Errorf("SQLiteRecoveredFrom: got %q, want %q", resp.SQLiteRecoveredFrom, "conf-comments")
	}
}
