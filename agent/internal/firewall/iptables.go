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

// NewIptablesFirewall creates an IptablesFirewall and ensures the managed chain exists.
// The jump rule from FORWARD is NOT inserted here — it is inserted lazily on the first
// Sync or ApplyPeer call (via ensureDispatchChain). This prevents a startup window where
// the dispatch chain is active but per-peer chains are not yet populated, which would
// cause packets to traverse an empty chain and fall through to the default FORWARD policy.
func NewIptablesFirewall(cfg config.FirewallConfig, iptablesPath string) (*IptablesFirewall, error) {
	fw := &IptablesFirewall{cfg: cfg, iptablesPath: iptablesPath}
	// Only create the chain — no jump rule yet. Jump is inserted on first Sync.
	_ = fw.run("-N", cfg.ManagedChain)
	return fw, nil
}

// peerChainName derives a stable iptables chain name from a WireGuard public key.
// Takes the first 16 alphanumeric characters from the base64 key, prefixed with WG_PEER_.
// Total chain name length: 8 + 16 = 24 chars, well within the iptables 29-char limit.
// Using 16 chars instead of 8 reduces birthday-paradox collision probability from
// ~1/(36^8) to ~1/(36^16), making silent same-chain collisions practically impossible.
// Base64 WireGuard keys (44 chars) always contain at least 32 alphanumeric chars.
func peerChainName(pubKey string) string {
	var safe []rune
	for _, c := range pubKey {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			safe = append(safe, c)
			if len(safe) == 16 {
				break
			}
		}
	}
	return "WG_PEER_" + string(safe)
}

// sourceCIDR returns client_address as-is for use in iptables -s.
// iptables accepts full CIDR notation (e.g. 10.8.0.5/32), which correctly
// handles both host addresses (/32) and subnet addresses without silently
// matching a network address instead of the intended host.
// Returns "" without panic if client_address is empty.
func sourceCIDR(clientAddress string) string {
	return clientAddress
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
// On error, combined output (stdout+stderr) is included for diagnostics.
func (fw *IptablesFirewall) runWithOutput(args ...string) (string, error) {
	cmd := exec.Command(fw.iptablesPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("iptables %v: %w (output: %s)", args, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// ensureDispatchChain creates the managed chain (if not exists) and ensures
// the jump rule from FORWARD is present.
// Uses -I FORWARD 1 -i <wg-interface> to insert at position 1 scoped to the
// WireGuard interface only. This guarantees our rules are evaluated before any
// RELATED,ESTABLISHED catch-all rules (e.g. KUBE-FORWARD) that would otherwise
// bypass per-peer filtering. Scoping to -i <interface> means non-WG traffic
// is never affected regardless of position.
func (fw *IptablesFirewall) ensureDispatchChain() error {
	chain := fw.cfg.ManagedChain
	iface := fw.cfg.WGInterface

	// Create the chain — "already exists" error is expected and ignored.
	_ = fw.run("-N", chain)

	// Check if the jump rule already exists (scoped to WG interface).
	if err := fw.run("-C", "FORWARD", "-i", iface, "-j", chain); err != nil {
		// Rule not present — insert at position 1 so it runs before ESTABLISHED catch-alls.
		if addErr := fw.run("-I", "FORWARD", "1", "-i", iface, "-j", chain); addErr != nil {
			return fmt.Errorf("adding jump rule to FORWARD: %w", addErr)
		}
	}
	return nil
}

// ApplyPeer creates or replaces the per-peer iptables chain.
// If peer.Enabled is false, delegates to RemovePeer (AC-7).
// If peer.ClientAddress is empty, logs WARN and skips (AC-12).
// Empty client_allowed_ips produces a DROP-only chain (AC-4).
// Calls ensureDispatchChain to guarantee the jump rule exists before adding the peer chain,
// so that the first ApplyPeer (e.g. from CreatePeer) activates filtering atomically.
func (fw *IptablesFirewall) ApplyPeer(peer storage.Peer) error {
	if !peer.Enabled {
		return fw.RemovePeer(peer)
	}

	src := sourceCIDR(peer.ClientAddress)
	if src == "" {
		log.Printf("WARN: firewall: peer %s has empty client_address — skipping ApplyPeer", peer.PublicKey)
		return nil
	}

	chainName := peerChainName(peer.PublicKey)

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

	// Ensure dispatch chain jump exists. Called after peer chain is fully populated so
	// that packets are never routed to an empty/partial chain (atomic activation).
	if err := fw.ensureDispatchChain(); err != nil {
		return fmt.Errorf("firewall: ApplyPeer ensureDispatchChain: %w", err)
	}

	// Ensure jump rule from dispatch chain to per-peer chain exists.
	if err := fw.run("-C", fw.cfg.ManagedChain, "-s", src, "-j", chainName); err != nil {
		if addErr := fw.run("-A", fw.cfg.ManagedChain, "-s", src, "-j", chainName); addErr != nil {
			return fmt.Errorf("firewall: adding jump rule for peer %s: %w", peer.PublicKey, addErr)
		}
	}

	return nil
}

// removeAllDispatchJumpsTo scans the dispatch chain and removes every rule that
// targets chainName as its jump destination. Used as a fallback in RemovePeer
// when ClientAddress is empty and the specific jump rule cannot be identified by
// source CIDR. Errors are logged and not returned (best-effort cleanup).
func (fw *IptablesFirewall) removeAllDispatchJumpsTo(chainName string) {
	dispatchOutput, err := fw.runWithOutput("-S", fw.cfg.ManagedChain)
	if err != nil {
		log.Printf("WARN: firewall: removeAllDispatchJumpsTo %s: listing dispatch chain: %v", chainName, err)
		return
	}
	for _, line := range strings.Split(dispatchOutput, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "-A ") && strings.HasSuffix(line, "-j "+chainName) {
			deleteArgs := strings.Fields(strings.Replace(line, "-A ", "-D ", 1))
			if err := fw.run(deleteArgs...); err != nil {
				log.Printf("WARN: firewall: removeAllDispatchJumpsTo %s: deleting rule: %v", chainName, err)
			}
		}
	}
}

// RemovePeer removes the per-peer chain and its jump rule from the dispatch chain.
// Safe to call if chain does not exist — errors are logged and not returned (AC-5).
// If ClientAddress is empty, falls back to scanning the dispatch chain for any jump
// rules referencing chainName and deletes them (prevents broken references in
// WG_SOCKD_FORWARD after a peer loses its client_address).
func (fw *IptablesFirewall) RemovePeer(peer storage.Peer) error {
	src := sourceCIDR(peer.ClientAddress)
	chainName := peerChainName(peer.PublicKey)

	if src != "" {
		// Fast path: remove the specific jump rule by source CIDR.
		if err := fw.run("-D", fw.cfg.ManagedChain, "-s", src, "-j", chainName); err != nil {
			log.Printf("WARN: firewall: RemovePeer %s: removing jump rule: %v", peer.PublicKey, err)
		}
	} else {
		// Fallback: scan dispatch chain and remove all references to chainName.
		// This handles the case where client_address was cleared before RemovePeer was called.
		fw.removeAllDispatchJumpsTo(chainName)
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
// For each orphan chain, its jump rule in the dispatch chain is removed first
// (required by iptables before -X — chain cannot be deleted while referenced).
func (fw *IptablesFirewall) cleanupOrphanChains(expectedChains map[string]struct{}) {
	output, err := fw.runWithOutput("-S")
	if err != nil {
		log.Printf("WARN: firewall: orphan cleanup: listing rules: %v", err)
		return
	}

	// Build map of orphan chain name → jump rule arguments found in dispatch chain.
	// Jump rules look like: -A WG_SOCKD_FORWARD -s 10.8.0.5/32 -j WG_PEER_Ab3cD4eF
	dispatchOutput, dispatchErr := fw.runWithOutput("-S", fw.cfg.ManagedChain)

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

						// Remove all jump rules referencing this chain from the dispatch
						// chain before flushing+deleting — iptables refuses to delete a
						// chain that is still referenced by another chain.
						if dispatchErr == nil {
							for _, dispatchLine := range strings.Split(dispatchOutput, "\n") {
								dispatchLine = strings.TrimSpace(dispatchLine)
								// Match lines like: -A WG_SOCKD_FORWARD ... -j WG_PEER_xxx
								if strings.HasPrefix(dispatchLine, "-A ") && strings.HasSuffix(dispatchLine, "-j "+chainName) {
									// Convert -A to -D to delete the rule.
									deleteArgs := strings.Fields(strings.Replace(dispatchLine, "-A ", "-D ", 1))
									if err := fw.run(deleteArgs...); err != nil {
										log.Printf("WARN: firewall: orphan jump delete %s: %v", chainName, err)
									}
								}
							}
						}

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
