// Package firewall provides a modular interface for per-peer iptables enforcement.
// Supported drivers: "iptables" (first implementation), "nftables" (deferred), "none".
package firewall

import (
	"fmt"
	"os/exec"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/config"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/storage"
)

// Firewall manages per-peer kernel firewall rules enforcing destination filtering
// on the FORWARD chain. Implementations must be idempotent — calling ApplyPeer
// twice with different CIDRs must produce rules that reflect only the second call.
type Firewall interface {
	// Sync applies rules for all enabled peers and removes rules for disabled
	// peers. It also cleans up orphan chains — chains with no matching DB peer.
	// Orphan cleanup always runs even if individual ApplyPeer calls return errors.
	Sync(peers []storage.Peer) error

	// ApplyPeer creates or replaces the per-peer firewall chain with ACCEPT rules
	// for each CIDR in client_allowed_ips and a final DROP rule.
	// If peer.Enabled is false, RemovePeer logic executes instead.
	ApplyPeer(peer storage.Peer) error

	// RemovePeer removes the per-peer firewall chain and its jump rule from the
	// dispatch chain. Safe to call if the chain does not exist.
	RemovePeer(peer storage.Peer) error
}

// New constructs a Firewall from cfg.
// Returns NoopFirewall when !cfg.Enabled or cfg.Driver == "none".
// Returns IptablesFirewall for cfg.Driver == "iptables".
// Returns an error for unknown drivers when enabled.
func New(cfg config.FirewallConfig) (Firewall, error) {
	if !cfg.Enabled || cfg.Driver == "none" {
		return &NoopFirewall{}, nil
	}
	switch cfg.Driver {
	case "iptables":
		iptablesPath, err := exec.LookPath("iptables")
		if err != nil {
			return nil, fmt.Errorf("firewall: iptables binary not found: %w", err)
		}
		return NewIptablesFirewall(cfg, iptablesPath)
	default:
		return nil, fmt.Errorf("firewall: unknown driver %q", cfg.Driver)
	}
}
