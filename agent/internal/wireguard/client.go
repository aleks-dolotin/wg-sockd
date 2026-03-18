// Package wireguard provides the WireGuard client interface and implementations.
package wireguard

import (
	"encoding/base64"
	"fmt"
	"net"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

var base64StdEncoding = base64.StdEncoding

// WireGuardClient abstracts WireGuard device control operations.
// This interface enables testing with mocks and allows future fallback
// implementations (e.g., ExecClient that shells out to `wg` CLI).
type WireGuardClient interface {
	// GetDevice retrieves the current state of a WireGuard interface.
	GetDevice(name string) (*Device, error)

	// ConfigurePeers applies a batch of peer configuration changes.
	// Each PeerConfig can add, update, or remove a peer.
	ConfigurePeers(name string, peers []PeerConfig) error

	// RemovePeer removes a single peer by public key.
	RemovePeer(name string, pubKey wgtypes.Key) error

	// GenerateKeyPair generates a new WireGuard private/public key pair.
	GenerateKeyPair() (privateKey, publicKey wgtypes.Key, err error)

	// GeneratePresharedKey generates a new random 32-byte preshared key.
	// Equivalent to `wg genpsk`.
	GeneratePresharedKey() (wgtypes.Key, error)

	// Close releases underlying resources.
	Close() error
}

// Device represents a WireGuard interface's current state.
// This is a project-local type that decouples handlers from wgctrl.
type Device struct {
	Name       string
	PublicKey  wgtypes.Key
	ListenPort int
	Peers      []Peer
}

// Peer represents a WireGuard peer's current state.
type Peer struct {
	PublicKey           wgtypes.Key
	Endpoint            *net.UDPAddr
	AllowedIPs          []net.IPNet
	LastHandshake       time.Time
	ReceiveBytes        int64
	TransmitBytes       int64
	PersistentKeepalive time.Duration // 0 = not set / off
}

// PeerConfig describes a desired peer configuration change.
type PeerConfig struct {
	PublicKey            wgtypes.Key
	AllowedIPs           []net.IPNet
	Endpoint             *net.UDPAddr
	Remove               bool
	ReplaceAllowedIPs    bool
	PersistentKeepalive  *time.Duration // nil = don't change, &0 = off, &25s = enable
	PresharedKey         *wgtypes.Key   // nil = don't change, non-nil = set (use &zeroKey to clear)
}

// ParseKey parses a base64-encoded WireGuard key string into a wgtypes.Key.
func ParseKey(s string) (wgtypes.Key, error) {
	var key wgtypes.Key
	decoded, err := base64Decode(s)
	if err != nil {
		return key, fmt.Errorf("invalid key %q: %w", s, err)
	}
	if len(decoded) != 32 {
		return key, fmt.Errorf("key %q has wrong length: %d", s, len(decoded))
	}
	copy(key[:], decoded)
	return key, nil
}

func base64Decode(s string) ([]byte, error) {
	return base64StdEncoding.DecodeString(s)
}
