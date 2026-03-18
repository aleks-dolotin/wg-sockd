//go:build dev_wg

package wireguard

import (
	"fmt"
	"net"
	"sync"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// DevClient is an in-memory WireGuard client for local development without a real
// WireGuard kernel interface. Activated via --dev-wg flag. NOT for production use.
type DevClient struct {
	mu         sync.RWMutex
	peers      map[wgtypes.Key]Peer
	deviceName string
	privateKey wgtypes.Key
	publicKey  wgtypes.Key
}

// NewDevClient creates a new in-memory WireGuard client with a generated server keypair.
func NewDevClient(deviceName string) (*DevClient, error) {
	priv, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("generating dev server key: %w", err)
	}
	return &DevClient{
		peers:      make(map[wgtypes.Key]Peer),
		deviceName: deviceName,
		privateKey: priv,
		publicKey:  priv.PublicKey(),
	}, nil
}

func (d *DevClient) GetDevice(name string) (*Device, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	peers := make([]Peer, 0, len(d.peers))
	for _, p := range d.peers {
		peers = append(peers, p)
	}

	return &Device{
		Name:       d.deviceName,
		PublicKey:  d.publicKey,
		ListenPort: 51820,
		Peers:      peers,
	}, nil
}

func (d *DevClient) ConfigurePeers(name string, peers []PeerConfig) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, pc := range peers {
		if pc.Remove {
			delete(d.peers, pc.PublicKey)
			continue
		}

		existing, exists := d.peers[pc.PublicKey]
		if exists && pc.ReplaceAllowedIPs {
			existing.AllowedIPs = make([]net.IPNet, len(pc.AllowedIPs))
			copy(existing.AllowedIPs, pc.AllowedIPs)
			d.peers[pc.PublicKey] = existing
		} else if !exists {
			allowedIPs := make([]net.IPNet, len(pc.AllowedIPs))
			copy(allowedIPs, pc.AllowedIPs)
			d.peers[pc.PublicKey] = Peer{
				PublicKey:  pc.PublicKey,
				AllowedIPs: allowedIPs,
			}
		}
	}

	return nil
}

func (d *DevClient) RemovePeer(name string, pubKey wgtypes.Key) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.peers[pubKey]; !exists {
		return fmt.Errorf("peer %s not found", pubKey)
	}
	delete(d.peers, pubKey)
	return nil
}

func (d *DevClient) GenerateKeyPair() (wgtypes.Key, wgtypes.Key, error) {
	priv, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return wgtypes.Key{}, wgtypes.Key{}, err
	}
	return priv, priv.PublicKey(), nil
}

func (d *DevClient) GeneratePresharedKey() (wgtypes.Key, error) {
	return wgtypes.GenerateKey()
}

func (d *DevClient) Close() error {
	return nil
}
