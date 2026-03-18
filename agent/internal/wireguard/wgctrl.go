package wireguard

import (
	"fmt"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// WgctrlClient implements WireGuardClient using wgctrl-go (netlink).
type WgctrlClient struct {
	client *wgctrl.Client
}

// Compile-time interface check.
var _ WireGuardClient = (*WgctrlClient)(nil)

// NewWgctrlClient creates a new WgctrlClient.
// Requires CAP_NET_ADMIN or root.
func NewWgctrlClient() (*WgctrlClient, error) {
	c, err := wgctrl.New()
	if err != nil {
		return nil, fmt.Errorf("creating wgctrl client: %w", err)
	}
	return &WgctrlClient{client: c}, nil
}

// GetDevice retrieves the current state of a WireGuard interface.
func (w *WgctrlClient) GetDevice(name string) (*Device, error) {
	dev, err := w.client.Device(name)
	if err != nil {
		return nil, fmt.Errorf("getting device %q: %w", name, err)
	}
	return deviceFromWgctrl(dev), nil
}

// ConfigurePeers applies a batch of peer configuration changes.
func (w *WgctrlClient) ConfigurePeers(name string, peers []PeerConfig) error {
	wgPeers := make([]wgtypes.PeerConfig, len(peers))
	for i, p := range peers {
		wgPeers[i] = peerConfigToWgctrl(p)
	}

	err := w.client.ConfigureDevice(name, wgtypes.Config{
		Peers: wgPeers,
	})
	if err != nil {
		return fmt.Errorf("configuring peers on %q: %w", name, err)
	}
	return nil
}

// RemovePeer removes a single peer by public key.
func (w *WgctrlClient) RemovePeer(name string, pubKey wgtypes.Key) error {
	return w.ConfigurePeers(name, []PeerConfig{
		{PublicKey: pubKey, Remove: true},
	})
}

// GenerateKeyPair generates a new WireGuard private/public key pair.
func (w *WgctrlClient) GenerateKeyPair() (privateKey, publicKey wgtypes.Key, err error) {
	priv, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return wgtypes.Key{}, wgtypes.Key{}, fmt.Errorf("generating private key: %w", err)
	}
	return priv, priv.PublicKey(), nil
}

// GeneratePresharedKey generates a new random 32-byte preshared key.
func (w *WgctrlClient) GeneratePresharedKey() (wgtypes.Key, error) {
	psk, err := wgtypes.GenerateKey()
	if err != nil {
		return wgtypes.Key{}, fmt.Errorf("generating preshared key: %w", err)
	}
	return psk, nil
}

// Close releases the underlying wgctrl client.
func (w *WgctrlClient) Close() error {
	return w.client.Close()
}

// deviceFromWgctrl converts a wgctrl Device to a project Device.
func deviceFromWgctrl(dev *wgtypes.Device) *Device {
	peers := make([]Peer, len(dev.Peers))
	for i, p := range dev.Peers {
		peers[i] = peerFromWgctrl(p)
	}
	return &Device{
		Name:       dev.Name,
		PublicKey:  dev.PublicKey,
		ListenPort: dev.ListenPort,
		Peers:      peers,
	}
}

// peerFromWgctrl converts a wgctrl Peer to a project Peer.
func peerFromWgctrl(p wgtypes.Peer) Peer {
	return Peer{
		PublicKey:           p.PublicKey,
		Endpoint:            p.Endpoint,
		AllowedIPs:          p.AllowedIPs,
		LastHandshake:       p.LastHandshakeTime,
		ReceiveBytes:        p.ReceiveBytes,
		TransmitBytes:       p.TransmitBytes,
		PersistentKeepalive: p.PersistentKeepaliveInterval,
	}
}

// peerConfigToWgctrl converts a project PeerConfig to a wgctrl PeerConfig.
func peerConfigToWgctrl(pc PeerConfig) wgtypes.PeerConfig {
	return wgtypes.PeerConfig{
		PublicKey:                   pc.PublicKey,
		Endpoint:                    pc.Endpoint,
		AllowedIPs:                  pc.AllowedIPs,
		Remove:                      pc.Remove,
		ReplaceAllowedIPs:           pc.ReplaceAllowedIPs,
		PersistentKeepaliveInterval: pc.PersistentKeepalive,
		PresharedKey:                pc.PresharedKey,
	}
}
