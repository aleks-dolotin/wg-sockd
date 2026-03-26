package firewall

import (
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/config"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/storage"
)

// IptablesFirewall implements Firewall using iptables subprocess calls.
// Chain model:
//   - One dispatch chain ManagedChain (default: WG_SOCKD_FORWARD) jumped from FORWARD.
//   - Per-peer chains named WG_PEER_<8-alnum-chars-of-pubkey>.
//
// Rules survive wg-sockd shutdown (no cleanup on stop — intentional per AC-11).
type IptablesFirewall struct {
	cfg          config.FirewallConfig
	iptablesPath string
}

// NewIptablesFirewall creates an IptablesFirewall and ensures the dispatch chain exists.
func NewIptablesFirewall(cfg config.FirewallConfig, iptablesPath string) (*IptablesFirewall, error) {
	fw := &IptablesFirewall{cfg: cfg, iptablesPath: iptablesPath}
	if err := fw.ensureDispatchChain(); err != nil {
		return nil, fmt.Errorf("firewall: ensure dispatch chain: %w", err)
	}
	return fw, nil
}

// peerChainName derives a stable iptables chain name from a WireGuard public key.
// Takes the first 8 alphanumeric characters from the base64 key, prefixed with WG_PEER_.
// Base64 WireGuard keys (44 chars) always contain at least 32 alphanumeric chars.
func peerChainName(pubKey string) string {
	var safe []rune
	for _, c := range pubKey {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			safe = append(safe, c)
			if len(safe) == 8 {
				break
			}
		}
	}
	return "WG_PEER_" + string(safe)
}

// sourceIP strips the CIDR suffix from client_address, returning just the IP.
// Returns "" without panic if client_address is empty or malformed.
func sourceIP(clientAddress string) string {
	if clientAddress == "" {
		return ""
	}
	parts := strings.SplitN(clientAddress, "/", 2)
	return parts[0]
}

// run executes an iptables command, discarding stdout, returning stderr on error.
func (fw *IptablesFirewall) run(args ...string) error {
	cmd := exec.Command(fw.iptablesPath, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("iptables %v: %w (output: %s)", args, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runWithOutput executes an iptables command and returns its stdout.
func (fw *IptablesFirewall) runWithOutput(args ...string) (string, error) {
	cmd := exec.Command(fw.iptablesPath, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("iptables %v: %w", args, err)
	}
	return string(out), nil
}

// ensureDispatchChain creates the managed chain (if not exists) and ensures
// the jump rule from FORWARD is present.
// Order: -N first (idempotent via ignore), then -C/-A for the jump rule (AC-14).
func (fw *IptablesFirewall) ensureDispatchChain() error {
	chain := fw.cfg.ManagedChain

	// Create the chain — "already exists" error is expected and ignored.
	_ = fw.run("-N", chain)

	// Check if the jump rule from FORWARD already exists; add if absent.
	if err := fw.run("-C", "FORWARD", "-j", chain); err != nil {
		if addErr := fw.run("-A", "FORWARD", "-j", chain); addErr != nil {
			return fmt.Errorf("adding jump rule to FORWARD: %w", addErr)
		}
	}
	return nil
}

// ApplyPeer creates or replaces the per-peer iptables chain.
// If peer.Enabled is false, delegates to RemovePeer (AC-7).
// If peer.ClientAddress is empty, logs WARN and skips (AC-12).
// Empty client_allowed_ips produces a DROP-only chain (AC-4).
func (fw *IptablesFirewall) ApplyPeer(peer storage.Peer) error {
	if !peer.Enabled {
		return fw.RemovePeer(peer)
	}

	src := sourceIP(peer.ClientAddress)
	if src == "" {
		log.Printf("WARN: firewall: peer %s has empty client_address — skipping ApplyPeer", peer.PublicKey)
		return nil
	}

	chainName := peerChainName(peer.PublicKey)

	// Collision check: if the dispatch chain references chainName with a different
	// source IP, skip to avoid overwriting another peer's rules.
	if dispatchOutput, err := fw.runWithOutput("-S", fw.cfg.ManagedChain); err == nil {
		for _, line := range strings.Split(dispatchOutput, "\n") {
			if strings.Contains(line, chainName) && strings.Contains(line, "-s ") && !strings.Contains(line, "-s "+src) {
				log.Printf("ERROR: firewall: chain %s exists with different source IP — skipping peer %s", chainName, peer.PublicKey)
				return nil
			}
		}
	}

	// Idempotent: create chain (ignore if exists), then flush it.
	_ = fw.run("-N", chainName)
	_ = fw.run("-F", chainName)

	// Add ACCEPT rules for each allowed destination CIDR.
	if peer.ClientAllowedIPs != "" {
		for _, cidr := range strings.Split(peer.ClientAllowedIPs, ",") {
			cidr = strings.TrimSpace(cidr)
			if cidr == "" {
				continue
			}
			if err := fw.run("-A", chainName, "-s", src, "-d", cidr, "-j", "ACCEPT"); err != nil {
				log.Printf("WARN: firewall: ApplyPeer %s: adding ACCEPT rule for %s: %v", peer.PublicKey, cidr, err)
			}
		}
	}

	// Final DROP rule — strict policy, catches all unmatched traffic.
	if err := fw.run("-A", chainName, "-j", "DROP"); err != nil {
		return fmt.Errorf("firewall: adding DROP rule for peer %s: %w", peer.PublicKey, err)
	}

	// Ensure jump rule from dispatch chain to per-peer chain exists.
	if err := fw.run("-C", fw.cfg.ManagedChain, "-s", src, "-j", chainName); err != nil {
		if addErr := fw.run("-A", fw.cfg.ManagedChain, "-s", src, "-j", chainName); addErr != nil {
			return fmt.Errorf("firewall: adding jump rule for peer %s: %w", peer.PublicKey, addErr)
		}
	}

	return nil
}

// RemovePeer removes the per-peer chain and its jump rule from the dispatch chain.
// Safe to call if chain does not exist — errors are logged and not returned (AC-5).
func (fw *IptablesFirewall) RemovePeer(peer storage.Peer) error {
	src := sourceIP(peer.ClientAddress)
	chainName := peerChainName(peer.PublicKey)

	// Remove jump rule from dispatch chain (best-effort).
	if src != "" {
		if err := fw.run("-D", fw.cfg.ManagedChain, "-s", src, "-j", chainName); err != nil {
			log.Printf("WARN: firewall: RemovePeer %s: removing jump rule: %v", peer.PublicKey, err)
		}
	}

	// Flush then delete the per-peer chain.
	if err := fw.run("-F", chainName); err != nil {
		log.Printf("WARN: firewall: RemovePeer %s: flushing chain: %v", peer.PublicKey, err)
	}
	if err := fw.run("-X", chainName); err != nil {
		log.Printf("WARN: firewall: RemovePeer %s: deleting chain: %v", peer.PublicKey, err)
	}

	return nil
}

// Sync applies rules for all enabled peers, removes rules for disabled peers,
// and cleans up orphan WG_PEER_* chains with no matching DB peer.
// Orphan cleanup always runs even if individual Apply/Remove calls fail (AC-19).
func (fw *IptablesFirewall) Sync(peers []storage.Peer) error {
	// Step 1: Ensure dispatch chain exists.
	if err := fw.ensureDispatchChain(); err != nil {
		return fmt.Errorf("firewall: Sync ensureDispatchChain: %w", err)
	}

	// Build expected chain name set.
	expectedChains := make(map[string]struct{}, len(peers))
	for _, p := range peers {
		expectedChains[peerChainName(p.PublicKey)] = struct{}{}
	}

	// Step 2: Apply or remove per-peer rules; accumulate errors but continue.
	var applyErrors []string
	for _, p := range peers {
		var err error
		if p.Enabled {
			err = fw.ApplyPeer(p)
		} else {
			err = fw.RemovePeer(p)
		}
		if err != nil {
			log.Printf("WARN: firewall: Sync: peer %s: %v", p.PublicKey, err)
			applyErrors = append(applyErrors, err.Error())
		}
	}

	// Step 3: Orphan cleanup — always runs regardless of step 2 errors (AC-19).
	fw.cleanupOrphanChains(expectedChains)

	if len(applyErrors) > 0 {
		return fmt.Errorf("firewall: Sync completed with %d error(s): %s", len(applyErrors), strings.Join(applyErrors, "; "))
	}
	return nil
}

// cleanupOrphanChains reads current iptables state and removes WG_PEER_* chains
// that are not in expectedChains. Errors are logged, not returned (AC-17).
func (fw *IptablesFirewall) cleanupOrphanChains(expectedChains map[string]struct{}) {
	output, err := fw.runWithOutput("-S")
	if err != nil {
		log.Printf("WARN: firewall: orphan cleanup: listing rules: %v", err)
		return
	}

	seen := make(map[string]struct{})
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		// Lines like "-N WG_PEER_abcd1234" declare chains.
		if strings.HasPrefix(line, "-N WG_PEER_") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				chainName := parts[1]
				if _, expected := expectedChains[chainName]; !expected {
					if _, alreadySeen := seen[chainName]; !alreadySeen {
						seen[chainName] = struct{}{}
						log.Printf("INFO: firewall: removing orphan chain %s", chainName)
						if err := fw.run("-F", chainName); err != nil {
							log.Printf("WARN: firewall: orphan flush %s: %v", chainName, err)
						}
						if err := fw.run("-X", chainName); err != nil {
							log.Printf("WARN: firewall: orphan delete %s: %v", chainName, err)
						}
					}
				}
			}
		}
	}
}
