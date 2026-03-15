// Package reconciler synchronizes the desired peer state with the actual WireGuard device state.
package reconciler

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/config"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/confwriter"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/storage"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/wireguard"
)

// Reconciler synchronizes WireGuard kernel state with SQLite.
type Reconciler struct {
	wgClient   wireguard.WireGuardClient
	store      *storage.DB
	cfg        *config.Config
	confWriter *confwriter.SharedWriter

	// mu guards ReconcileOnce. Pause acquires a write-lock so that
	// ReconcileOnce (which holds a read-lock) cannot run concurrently
	// with operations that must be atomic (e.g. BatchCreatePeers).
	mu sync.RWMutex
}

// New creates a new Reconciler.
func New(wgClient wireguard.WireGuardClient, store *storage.DB, cfg *config.Config, confWriter *confwriter.SharedWriter) *Reconciler {
	return &Reconciler{
		wgClient:   wgClient,
		store:      store,
		cfg:        cfg,
		confWriter: confWriter,
	}
}

// Pause prevents ReconcileOnce from running until Resume is called.
// Callers must always pair Pause with a deferred Resume.
func (r *Reconciler) Pause() {
	r.mu.Lock()
}

// Resume re-enables reconciliation after a Pause.
func (r *Reconciler) Resume() {
	r.mu.Unlock()
}

// RunLoop runs ReconcileOnce on a timer until ctx is cancelled.
// Errors are logged but do not stop the loop.
func (r *Reconciler) RunLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.ReconcileOnce(ctx); err != nil {
				log.Printf("WARNING: periodic reconciliation failed: %v", err)
			}
		}
	}
}

// ReconcileOnce performs a single reconciliation pass:
// 1. Unknown peers (in kernel, not in DB) → remove from kernel, insert as disabled
// 2. Disabled-in-DB peers present in kernel (zombies) → remove from kernel
// 3. Missing peers (in DB enabled, not in kernel) → re-add to kernel
// 4. Rewrite conf file
func (r *Reconciler) ReconcileOnce(ctx context.Context) error {
	// Hold a read-lock so Pause() (write-lock) blocks us during batch operations.
	r.mu.RLock()
	defer r.mu.RUnlock()

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

	// Step 1: Find unknown peers (in kernel, not in DB) AND zombie peers
	// (disabled in DB but still present in kernel — access-control bypass).
	for pubKeyStr, wgPeer := range kernelPeers {
		dbPeer, exists := dbPeerSet[pubKeyStr]

		if exists {
			// Zombie detection: peer is known but disabled — must not be in kernel.
			if !dbPeer.Enabled {
				log.Printf("WARN: zombie peer detected (disabled in DB, present in kernel) — removing: %s", pubKeyStr)
				if err := r.wgClient.RemovePeer(r.cfg.Interface, wgPeer.PublicKey); err != nil {
					log.Printf("ERROR: failed to remove zombie peer %s: %v", pubKeyStr, err)
				}
			}
			continue
		}

		// Unknown peer: not in DB at all.
		if r.cfg.AutoApproveUnknown {
			// Dev mode: keep unknown peer in kernel, register as enabled + auto_discovered.
			log.Printf("WARN: unknown peer discovered and auto-approved: %s", pubKeyStr)

			shortKey := pubKeyStr
			if len(shortKey) > 8 {
				shortKey = shortKey[:8]
			}
			friendlyName := "unknown-" + shortKey

			if err := r.store.UpsertPeerFromReconcile(pubKeyStr, friendlyName, true, true); err != nil {
				log.Printf("ERROR: failed to insert unknown peer %s: %v", pubKeyStr, err)
			}
		} else {
			// Strict mode (default): remove from kernel, insert as disabled.
			log.Printf("WARN: unknown peer discovered and removed from runtime %s", pubKeyStr)

			if err := r.wgClient.RemovePeer(r.cfg.Interface, wgPeer.PublicKey); err != nil {
				log.Printf("ERROR: failed to remove unknown peer %s: %v", pubKeyStr, err)
			}

			shortKey := pubKeyStr
			if len(shortKey) > 8 {
				shortKey = shortKey[:8]
			}
			friendlyName := "unknown-" + shortKey

			if err := r.store.UpsertPeerFromReconcile(pubKeyStr, friendlyName, true, false); err != nil {
				log.Printf("ERROR: failed to insert unknown peer %s: %v", pubKeyStr, err)
			}
		}
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
// The actual file write is serialised by SharedWriter's mutex.
func (r *Reconciler) rewriteConf() error {
	dbPeers, err := r.store.ListPeers()
	if err != nil {
		return err
	}

	peers := make([]confwriter.PeerConf, 0, len(dbPeers))
	for _, p := range dbPeers {
		if !p.Enabled {
			continue
		}
		peers = append(peers, confwriter.PeerConf{
			PublicKey:    p.PublicKey,
			AllowedIPs:   p.AllowedIPs,
			FriendlyName: p.FriendlyName,
			CreatedAt:    p.CreatedAt,
			Notes:        p.Notes,
		})
	}

	return r.confWriter.WriteConf(r.cfg.ConfPath, peers)
}
