package firewall

import "github.com/aleks-dolotin/wg-sockd/agent/internal/storage"

// NoopFirewall is a no-op implementation used when firewall is disabled or driver is "none".
// All methods return nil without performing any system operations.
type NoopFirewall struct{}

func (n *NoopFirewall) Sync(peers []storage.Peer) error    { return nil }
func (n *NoopFirewall) ApplyPeer(peer storage.Peer) error  { return nil }
func (n *NoopFirewall) RemovePeer(peer storage.Peer) error { return nil }
