// Package reconciler synchronizes the desired peer state with the actual WireGuard device state.
package reconciler

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/config"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/confwriter"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/storage"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/wireguard"
)

// Reconciler synchronizes WireGuard kernel state with SQLite.
type Reconciler struct {
	wgClient wireguard.WireGuardClient
	store    *storage.DB
	cfg      *config.Config
}

// New creates a new Reconciler.
func New(wgClient wireguard.WireGuardClient, store *storage.DB, cfg *config.Config) *Reconciler {
	return &Reconciler{
		wgClient: wgClient,
		store:    store,
		cfg:      cfg,
	}
}

// ReconcileOnce performs a single reconciliation pass:
// 1. Unknown peers (in kernel, not in DB) → remove from kernel, insert as disabled
// 2. Missing peers (in DB enabled, not in kernel) → re-add to kernel
// 3. Rewrite conf file
func (r *Reconciler) ReconcileOnce(ctx context.Context) error {
	// Get current kernel state.
	dev, err := r.wgClient.GetDevice(r.cfg.Interface)
	if err != nil {
		return fmt.Errorf("getting device: %w", err)
	}

	// Build set of kernel peer public keys.
	kernelPeers := make(map[string]wireguard.Peer, len(dev.Peers))
	for _, p := range dev.Peers {
		kernelPeers[p.PublicKey.String()] = p
	}

	// Get DB state.
	dbPeers, err := r.store.ListPeers()
	if err != nil {
		return fmt.Errorf("listing peers: %w", err)
	}

	dbPeerSet := make(map[string]storage.Peer, len(dbPeers))
	for _, p := range dbPeers {
		dbPeerSet[p.PublicKey] = p
	}

	// Step 1: Find unknown peers (in kernel, not in DB).
	for pubKeyStr, wgPeer := range kernelPeers {
		if _, exists := dbPeerSet[pubKeyStr]; exists {
			continue
		}

		// Unknown peer — F3 strict enforcement: remove from kernel immediately.
		log.Printf("WARN: unknown peer discovered and removed from runtime %s", pubKeyStr)

		if err := r.wgClient.RemovePeer(r.cfg.Interface, wgPeer.PublicKey); err != nil {
			log.Printf("ERROR: failed to remove unknown peer %s: %v", pubKeyStr, err)
		}

		// Insert as disabled audit record.
		shortKey := pubKeyStr
		if len(shortKey) > 8 {
			shortKey = shortKey[:8]
		}
		friendlyName := "unknown-" + shortKey

		if err := r.store.UpsertPeerFromReconcile(pubKeyStr, friendlyName, true); err != nil {
			log.Printf("ERROR: failed to insert unknown peer %s: %v", pubKeyStr, err)
		}

		// Mark as disabled since UpsertPeerFromReconcile uses INSERT OR IGNORE.
		disabled := false
		r.store.UpdatePeer(pubKeyStr, &storage.PeerUpdate{Enabled: &disabled})
	}

	// Step 2: Find missing peers (in DB enabled, not in kernel).
	for pubKeyStr, dbPeer := range dbPeerSet {
		if !dbPeer.Enabled {
			continue
		}
		if _, exists := kernelPeers[pubKeyStr]; exists {
			continue
		}

		// Missing peer — re-add to kernel.
		log.Printf("INFO: re-added missing peer %s to runtime", pubKeyStr)

		var allowedNets []net.IPNet
		if dbPeer.AllowedIPs != "" {
			for _, cidr := range strings.Split(dbPeer.AllowedIPs, ",") {
				cidr = strings.TrimSpace(cidr)
				if _, ipNet, err := net.ParseCIDR(cidr); err == nil {
					allowedNets = append(allowedNets, *ipNet)
				}
			}
		}

		// Parse the public key from base64 string.
		key, err := wireguard.ParseKey(pubKeyStr)
		if err != nil {
			log.Printf("ERROR: invalid public key for peer %s: %v", pubKeyStr, err)
			continue
		}

		err = r.wgClient.ConfigurePeers(r.cfg.Interface, []wireguard.PeerConfig{
			{
				PublicKey:  key,
				AllowedIPs: allowedNets,
			},
		})
		if err != nil {
			log.Printf("ERROR: failed to re-add peer %s: %v", pubKeyStr, err)
		}
	}

	// Step 3: Rewrite conf file.
	if err := r.rewriteConf(); err != nil {
		log.Printf("WARNING: conf rewrite failed during reconcile: %v", err)
	}

	return nil
}

// rewriteConf rewrites the WireGuard conf file from current DB state.
func (r *Reconciler) rewriteConf() error {
	dbPeers, err := r.store.ListPeers()
	if err != nil {
		return err
	}

	peers := make([]confwriter.PeerConf, 0, len(dbPeers))
	for _, p := range dbPeers {
		if !p.Enabled {
			continue // unknown/disabled peers NOT written to conf
		}
		peers = append(peers, confwriter.PeerConf{
			PublicKey:    p.PublicKey,
			AllowedIPs:   p.AllowedIPs,
			FriendlyName: p.FriendlyName,
			CreatedAt:    p.CreatedAt,
			Notes:        p.Notes,
		})
	}

	return confwriter.WriteConf(r.cfg.ConfPath, peers)
}
