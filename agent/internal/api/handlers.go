package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/config"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/confwriter"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/middleware"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/storage"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/wireguard"
	qrcode "github.com/skip2/go-qrcode"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// ReconcilerPauser is a narrow interface that allows handlers to pause and
// resume the reconciliation loop around operations that must be atomic
// (kernel write + DB commit). Defined here to avoid a circular import.
type ReconcilerPauser interface {
	Pause()
	Resume()
}

// Handlers holds dependencies for API handlers.
type Handlers struct {
	wgClient   wireguard.WireGuardClient
	store      *storage.DB
	cfg        *config.Config
	confWriter *confwriter.SharedWriter    // serialises wg0.conf writes shared with Reconciler
	debouncer  *confwriter.DebouncedWriter // coalesces rapid conf writes (Story 5.3)
	reconciler ReconcilerPauser            // paused during BatchCreatePeers to avoid race
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(
	wgClient wireguard.WireGuardClient,
	store *storage.DB,
	cfg *config.Config,
	confWriter *confwriter.SharedWriter,
	debouncer *confwriter.DebouncedWriter,
	reconciler ReconcilerPauser,
) *Handlers {
	return &Handlers{
		wgClient:   wgClient,
		store:      store,
		cfg:        cfg,
		confWriter: confWriter,
		debouncer:  debouncer,
		reconciler: reconciler,
	}
}

// ListPeers handles GET /api/peers.
// Joins SQLite metadata with live wgctrl data by PublicKey.
func (h *Handlers) ListPeers(w http.ResponseWriter, r *http.Request) {
	dbPeers, err := h.store.ListPeers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	// Get live data from wgctrl.
	var wgPeerMap map[string]wireguard.Peer
	dev, err := h.wgClient.GetDevice(h.cfg.Interface)
	if err != nil {
		log.Printf("WARNING: could not get wgctrl device: %v", err)
	} else {
		wgPeerMap = make(map[string]wireguard.Peer, len(dev.Peers))
		for _, p := range dev.Peers {
			wgPeerMap[p.PublicKey.String()] = p
		}
	}

	responses := make([]PeerResponse, 0, len(dbPeers))
	for _, dbp := range dbPeers {
		resp := peerToResponse(dbp)

		// Merge live wgctrl data if available.
		if wgPeerMap != nil {
			if wgp, ok := wgPeerMap[dbp.PublicKey]; ok {
				if wgp.Endpoint != nil {
					resp.Endpoint = wgp.Endpoint.String()
				}
				if !wgp.LastHandshake.IsZero() {
					resp.LatestHandshake = &wgp.LastHandshake
				}
				resp.TransferRx = wgp.ReceiveBytes
				resp.TransferTx = wgp.TransmitBytes
			}
		}

		responses = append(responses, resp)
	}

	writeJSON(w, http.StatusOK, responses)
}

// CreatePeer handles POST /api/peers.
// Generates keypair, adds to wgctrl + conf + SQLite.
// Accepts EITHER profile (string) OR allowed_ips ([]string), mutually exclusive.
func (h *Handlers) CreatePeer(w http.ResponseWriter, r *http.Request) {
	var req CreatePeerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	// Mutual exclusivity: profile XOR allowed_ips.
	hasProfile := req.Profile != nil && *req.Profile != ""
	hasAllowedIPs := len(req.AllowedIPs) > 0

	if hasProfile && hasAllowedIPs {
		writeError(w, http.StatusBadRequest, "validation_error", "provide either 'profile' or 'allowed_ips', not both")
		return
	}
	if !hasProfile && !hasAllowedIPs {
		writeError(w, http.StatusBadRequest, "validation_error", "either 'profile' or 'allowed_ips' is required")
		return
	}

	var resolvedAllowedIPs []string

	if hasProfile {
		// Resolve profile → allowed IPs via CIDR calculator.
		profile, err := h.store.GetProfile(*req.Profile)
		if err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("profile %q not found", *req.Profile))
			} else {
				writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			}
			return
		}

		result, err := wireguard.ComputeAllowedIPs(profile.AllowedIPs, profile.ExcludeIPs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "cidr_error", err.Error())
			return
		}
		resolvedAllowedIPs = result.Prefixes
		if result.Warning != "" {
			log.Printf("WARNING: profile %q has high route count: %s", *req.Profile, result.Warning)
		}
	} else {
		resolvedAllowedIPs = req.AllowedIPs
	}

	// Validate friendly_name.
	if err := middleware.ValidateFriendlyName(req.FriendlyName); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}

	// Enforce peer limit (RT-2 DoS prevention).
	if h.cfg.PeerLimit > 0 {
		count, err := h.store.CountPeers()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		if count >= h.cfg.PeerLimit {
			writeError(w, http.StatusTooManyRequests, "quota_exceeded",
				fmt.Sprintf("peer limit reached (%d)", h.cfg.PeerLimit))
			return
		}
	}

	// Validate CIDRs.
	if err := middleware.ValidateAllowedIPs(resolvedAllowedIPs); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}

	// Generate keypair.
	privKey, pubKey, err := h.wgClient.GenerateKeyPair()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "keygen_error", err.Error())
		return
	}

	// Parse allowed IPs for wgctrl.
	var allowedNets []net.IPNet
	for _, cidr := range resolvedAllowedIPs {
		_, ipNet, _ := net.ParseCIDR(cidr) // already validated
		allowedNets = append(allowedNets, *ipNet)
	}

	// Add to wgctrl.
	err = h.wgClient.ConfigurePeers(h.cfg.Interface, []wireguard.PeerConfig{
		{
			PublicKey:  pubKey,
			AllowedIPs: allowedNets,
		},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "wireguard_error", err.Error())
		return
	}

	// Insert to SQLite.
	allowedIPsStr := strings.Join(resolvedAllowedIPs, ",")
	dbPeer := &storage.Peer{
		PublicKey:     pubKey.String(),
		FriendlyName:  req.FriendlyName,
		AllowedIPs:    allowedIPsStr,
		Enabled:       true,
	}
	if hasProfile {
		dbPeer.Profile = req.Profile
	}

	id, err := h.store.CreatePeer(dbPeer)
	if err != nil {
		// Rollback wgctrl.
		_ = h.wgClient.RemovePeer(h.cfg.Interface, pubKey)
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	// Rewrite conf file — if this fails, rollback DB and kernel changes.
	if err := h.rewriteConf(); err != nil {
		log.Printf("ERROR: conf write failed, rolling back: %v", err)
		if rbErr := h.store.DeletePeer(pubKey.String()); rbErr != nil {
			log.Printf("CRITICAL: DB rollback failed for peer %s: %v", pubKey, rbErr)
		}
		if rbErr := h.wgClient.RemovePeer(h.cfg.Interface, pubKey); rbErr != nil {
			log.Printf("CRITICAL: wgctrl rollback failed for peer %s: %v", pubKey, rbErr)
		}
		writeError(w, http.StatusInternalServerError, "conf_write_error", err.Error())
		return
	}
	resp := PeerResponse{
		ID:           id,
		PublicKey:    pubKey.String(),
		FriendlyName: req.FriendlyName,
		AllowedIPs:   resolvedAllowedIPs,
		Profile:      req.Profile,
		Enabled:      true,
		CreatedAt:    time.Now(),
	}

	// Return response with private key embedded in a create-specific wrapper.
	type CreatePeerResponse struct {
		PeerResponse
		PrivateKey string `json:"private_key"`
	}

	writeJSON(w, http.StatusCreated, CreatePeerResponse{
		PeerResponse: resp,
		PrivateKey:   privKey.String(),
	})
}

// BatchCreatePeers handles POST /api/peers/batch.
// Creates multiple peers in a single transaction with one wgctrl call and one conf write.
func (h *Handlers) BatchCreatePeers(w http.ResponseWriter, r *http.Request) {
	var req BatchCreatePeersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	if len(req.Peers) == 0 {
		writeError(w, http.StatusBadRequest, "validation_error", "peers array is empty")
		return
	}

	// Check peer limit.
	if h.cfg.PeerLimit > 0 {
		count, err := h.store.CountPeers()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		if count+len(req.Peers) > h.cfg.PeerLimit {
			writeError(w, http.StatusTooManyRequests, "quota_exceeded",
				fmt.Sprintf("batch of %d would exceed peer limit (%d current, %d max)",
					len(req.Peers), count, h.cfg.PeerLimit))
			return
		}
	}

	// Validate all peers first (fail-fast).
	for i, p := range req.Peers {
		if len(p.AllowedIPs) == 0 && (p.Profile == nil || *p.Profile == "") {
			writeError(w, http.StatusBadRequest, "validation_error",
				fmt.Sprintf("peer[%d]: either 'profile' or 'allowed_ips' is required", i))
			return
		}
		if err := middleware.ValidateFriendlyName(p.FriendlyName); err != nil {
			writeError(w, http.StatusBadRequest, "validation_error",
				fmt.Sprintf("peer[%d]: %v", i, err))
			return
		}
	}

	// Resolve profiles and generate keypairs.
	type resolvedPeer struct {
		privKey    wgtypes.Key
		pubKey     wgtypes.Key
		allowedIPs []string
		req        CreatePeerRequest
	}

	resolved := make([]resolvedPeer, 0, len(req.Peers))
	for i, p := range req.Peers {
		var ips []string
		hasProfile := p.Profile != nil && *p.Profile != ""

		if hasProfile {
			profile, err := h.store.GetProfile(*p.Profile)
			if err != nil {
				if err == sql.ErrNoRows {
					writeError(w, http.StatusNotFound, "not_found",
						fmt.Sprintf("peer[%d]: profile %q not found", i, *p.Profile))
				} else {
					writeError(w, http.StatusInternalServerError, "db_error", err.Error())
				}
				return
			}
			result, err := wireguard.ComputeAllowedIPs(profile.AllowedIPs, profile.ExcludeIPs)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "cidr_error", err.Error())
				return
			}
			ips = result.Prefixes
		} else {
			if err := middleware.ValidateAllowedIPs(p.AllowedIPs); err != nil {
				writeError(w, http.StatusBadRequest, "validation_error",
					fmt.Sprintf("peer[%d]: %v", i, err))
				return
			}
			ips = p.AllowedIPs
		}

		priv, pub, err := h.wgClient.GenerateKeyPair()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "keygen_error", err.Error())
			return
		}

		resolved = append(resolved, resolvedPeer{
			privKey:    priv,
			pubKey:     pub,
			allowedIPs: ips,
			req:        p,
		})
	}

	// Single wgctrl call.
	wgConfigs := make([]wireguard.PeerConfig, 0, len(resolved))
	for _, rp := range resolved {
		var nets []net.IPNet
		for _, cidr := range rp.allowedIPs {
			_, ipNet, _ := net.ParseCIDR(cidr)
			nets = append(nets, *ipNet)
		}
		wgConfigs = append(wgConfigs, wireguard.PeerConfig{
			PublicKey:  rp.pubKey,
			AllowedIPs: nets,
		})
	}

	// Pause reconciler BEFORE touching the kernel: the entire sequence
	// (ConfigurePeers → DB commit → conf write) must be atomic with respect
	// to the reconciler. Without this, the reconciler could see peers in the
	// kernel that are not yet in DB and delete them (strict mode).
	h.reconciler.Pause()
	defer h.reconciler.Resume()

	if err := h.wgClient.ConfigurePeers(h.cfg.Interface, wgConfigs); err != nil {
		writeError(w, http.StatusInternalServerError, "wireguard_error", err.Error())
		return
	}


	// Single SQLite transaction.
	dbPeers := make([]*storage.Peer, 0, len(resolved))
	for _, rp := range resolved {
		dbPeer := &storage.Peer{
			PublicKey:    rp.pubKey.String(),
			FriendlyName: rp.req.FriendlyName,
			AllowedIPs:   strings.Join(rp.allowedIPs, ","),
			Enabled:      true,
		}
		if rp.req.Profile != nil && *rp.req.Profile != "" {
			dbPeer.Profile = rp.req.Profile
		}
		dbPeers = append(dbPeers, dbPeer)
	}

	ids, err := h.store.CreatePeersBatch(dbPeers)
	if err != nil {
		// Rollback wgctrl: remove all added peers.
		for _, rp := range resolved {
			if rbErr := h.wgClient.RemovePeer(h.cfg.Interface, rp.pubKey); rbErr != nil {
				log.Printf("CRITICAL: wgctrl rollback failed for %s: %v", rp.pubKey, rbErr)
			}
		}
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	// Single conf write — rollback on failure.
	if err := h.rewriteConf(); err != nil {
		log.Printf("ERROR: conf write failed after batch import, rolling back: %v", err)
		// Rollback DB: delete all just-created peers.
		for _, dbp := range dbPeers {
			if rbErr := h.store.DeletePeer(dbp.PublicKey); rbErr != nil {
				log.Printf("CRITICAL: DB rollback failed for peer %s: %v", dbp.PublicKey, rbErr)
			}
		}
		// Rollback wgctrl: remove all added peers.
		for _, rp := range resolved {
			if rbErr := h.wgClient.RemovePeer(h.cfg.Interface, rp.pubKey); rbErr != nil {
				log.Printf("CRITICAL: wgctrl rollback failed for %s: %v", rp.pubKey, rbErr)
			}
		}
		writeError(w, http.StatusInternalServerError, "conf_write_error", err.Error())
		return
	}

	// Build responses.
	responses := make([]PeerResponse, 0, len(resolved))
	for i, rp := range resolved {
		responses = append(responses, PeerResponse{
			ID:           ids[i],
			PublicKey:    rp.pubKey.String(),
			FriendlyName: rp.req.FriendlyName,
			AllowedIPs:   rp.allowedIPs,
			Profile:      rp.req.Profile,
			Enabled:      true,
		})
	}

	writeJSON(w, http.StatusCreated, responses)
}

// GetPeer handles GET /api/peers/{id}.
// Returns a single peer by ID with live wgctrl data merged.
func (h *Handlers) GetPeer(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return
	}

	peer, err := h.store.GetPeerByID(id)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "not_found", "peer not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	resp := peerToResponse(*peer)

	// Merge live wgctrl data if available.
	dev, err := h.wgClient.GetDevice(h.cfg.Interface)
	if err == nil {
		for _, wgp := range dev.Peers {
			if wgp.PublicKey.String() == peer.PublicKey {
				if wgp.Endpoint != nil {
					resp.Endpoint = wgp.Endpoint.String()
				}
				if !wgp.LastHandshake.IsZero() {
					resp.LatestHandshake = &wgp.LastHandshake
				}
				resp.TransferRx = wgp.ReceiveBytes
				resp.TransferTx = wgp.TransmitBytes
				break
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// UpdatePeer handles PUT /api/peers/{id}.
// Accepts partial updates — only non-nil fields are applied.
func (h *Handlers) UpdatePeer(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return
	}

	// Lookup existing peer.
	existing, err := h.store.GetPeerByID(id)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "not_found", "peer not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	var req UpdatePeerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	// Validate friendly_name if provided.
	if req.FriendlyName != nil {
		if err := middleware.ValidateFriendlyName(*req.FriendlyName); err != nil {
			writeError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
	}

	// Determine if network config changes (requires wgctrl update).
	needsWgUpdate := false
	var newAllowedIPs string

	// Handle profile change.
	if req.Profile != nil {
		profilePtr := *req.Profile
		if profilePtr != nil && *profilePtr != "" {
			// Resolve profile → allowed IPs.
			profile, err := h.store.GetProfile(*profilePtr)
			if err != nil {
				if err == sql.ErrNoRows {
					writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("profile %q not found", *profilePtr))
				} else {
					writeError(w, http.StatusInternalServerError, "db_error", err.Error())
				}
				return
			}
			result, err := wireguard.ComputeAllowedIPs(profile.AllowedIPs, profile.ExcludeIPs)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "cidr_error", err.Error())
				return
			}
			newAllowedIPs = strings.Join(result.Prefixes, ",")
			needsWgUpdate = true
		} else {
			// Profile is being detached (set to null).
			// Require allowed_ips to be explicitly provided (even as []) when detaching
			// a profile, otherwise the peer would keep stale profile-resolved IPs in the kernel.
			// nil means the field was omitted entirely → reject.
			// [] (empty slice) is valid and means "block all traffic for this peer".
			if req.AllowedIPs == nil {
				writeError(w, http.StatusBadRequest, "validation_error",
					"allowed_ips is required when detaching a profile")
				return
			}
		}
	}

	// Handle direct allowed_ips change (without profile).
	if len(req.AllowedIPs) > 0 && !needsWgUpdate {
		if err := middleware.ValidateAllowedIPs(req.AllowedIPs); err != nil {
			writeError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		newAllowedIPs = strings.Join(req.AllowedIPs, ",")
		needsWgUpdate = true
	}

	// Update wgctrl if network config changed.
	if needsWgUpdate {
		pubKeyBytes, err := wireguard.ParseKey(existing.PublicKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "key_parse_error", err.Error())
			return
		}

		var allowedNets []net.IPNet
		for _, cidr := range strings.Split(newAllowedIPs, ",") {
			_, ipNet, _ := net.ParseCIDR(cidr)
			allowedNets = append(allowedNets, *ipNet)
		}

		err = h.wgClient.ConfigurePeers(h.cfg.Interface, []wireguard.PeerConfig{
			{
				PublicKey:         pubKeyBytes,
				AllowedIPs:        allowedNets,
				ReplaceAllowedIPs: true,
			},
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "wireguard_error", err.Error())
			return
		}
	}

	// Build DB update.
	dbUpdate := &storage.PeerUpdate{}
	if req.FriendlyName != nil {
		dbUpdate.FriendlyName = req.FriendlyName
	}
	if needsWgUpdate {
		dbUpdate.AllowedIPs = &newAllowedIPs
	}
	if req.Profile != nil {
		dbUpdate.Profile = req.Profile
	}
	if req.Enabled != nil {
		dbUpdate.Enabled = req.Enabled
	}
	if req.Notes != nil {
		dbUpdate.Notes = req.Notes
	}

	if err := h.store.UpdatePeer(existing.PublicKey, dbUpdate); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	// Rewrite conf if network config changed (debounced — non-critical).
	if needsWgUpdate {
		h.notifyConfChange()
	}

	// Re-read and return updated peer.
	updated, err := h.store.GetPeerByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	resp := peerToResponse(*updated)

	// Merge live wgctrl data if available.
	dev, err := h.wgClient.GetDevice(h.cfg.Interface)
	if err == nil {
		for _, wgp := range dev.Peers {
			if wgp.PublicKey.String() == updated.PublicKey {
				if wgp.Endpoint != nil {
					resp.Endpoint = wgp.Endpoint.String()
				}
				if !wgp.LastHandshake.IsZero() {
					resp.LatestHandshake = &wgp.LastHandshake
				}
				resp.TransferRx = wgp.ReceiveBytes
				resp.TransferTx = wgp.TransmitBytes
				break
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// RotateKeys handles POST /api/peers/{id}/rotate-keys.
// Generates new keypair, atomically replaces old key in wgctrl + DB + conf.
func (h *Handlers) RotateKeys(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return
	}

	// Lookup peer.
	peer, err := h.store.GetPeerByID(id)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "not_found", "peer not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	oldPubKey, err := wireguard.ParseKey(peer.PublicKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "key_parse_error", err.Error())
		return
	}

	// Generate new keypair.
	newPrivKey, newPubKey, err := h.wgClient.GenerateKeyPair()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "keygen_error", err.Error())
		return
	}

	// Parse existing allowed IPs.
	var allowedNets []net.IPNet
	if peer.AllowedIPs != "" {
		for _, cidr := range strings.Split(peer.AllowedIPs, ",") {
			_, ipNet, parseErr := net.ParseCIDR(strings.TrimSpace(cidr))
			if parseErr != nil {
				log.Printf("WARNING: invalid CIDR in peer %d: %s", id, cidr)
				continue
			}
			allowedNets = append(allowedNets, *ipNet)
		}
	}

	// Step 1: Remove old peer from wgctrl.
	if err := h.wgClient.RemovePeer(h.cfg.Interface, oldPubKey); err != nil {
		log.Printf("WARNING: removing old peer from wgctrl: %v", err)
		// Continue — key rotation must succeed even if wgctrl remove fails.
	}

	// Step 2: Add new peer to wgctrl.
	err = h.wgClient.ConfigurePeers(h.cfg.Interface, []wireguard.PeerConfig{
		{
			PublicKey:  newPubKey,
			AllowedIPs: allowedNets,
		},
	})
	if err != nil {
		// Attempt to re-add old peer (best-effort rollback).
		log.Printf("ERROR: adding new peer to wgctrl failed: %v — attempting rollback", err)
		rollbackErr := h.wgClient.ConfigurePeers(h.cfg.Interface, []wireguard.PeerConfig{
			{PublicKey: oldPubKey, AllowedIPs: allowedNets},
		})
		if rollbackErr != nil {
			log.Printf("ERROR: rollback failed: %v", rollbackErr)
		}
		writeError(w, http.StatusInternalServerError, "wireguard_error", err.Error())
		return
	}

	// Step 3: Update SQLite.
	if err := h.store.UpdatePeerPublicKey(peer.PublicKey, newPubKey.String()); err != nil {
		// Rollback wgctrl: remove new, re-add old.
		log.Printf("ERROR: DB update failed: %v — rolling back wgctrl", err)
		if rbErr := h.wgClient.RemovePeer(h.cfg.Interface, newPubKey); rbErr != nil {
			log.Printf("CRITICAL: wgctrl remove-new rollback failed: %v", rbErr)
		}
		if rbErr := h.wgClient.ConfigurePeers(h.cfg.Interface, []wireguard.PeerConfig{
			{PublicKey: oldPubKey, AllowedIPs: allowedNets},
		}); rbErr != nil {
			log.Printf("CRITICAL: wgctrl re-add-old rollback failed: %v", rbErr)
		}
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	// Step 4: Rewrite conf — rollback everything on failure.
	if err := h.rewriteConf(); err != nil {
		log.Printf("ERROR: conf write failed after key rotation, rolling back: %v", err)
		// Rollback DB: restore old public key.
		if rbErr := h.store.UpdatePeerPublicKey(newPubKey.String(), peer.PublicKey); rbErr != nil {
			log.Printf("CRITICAL: DB rollback failed during key rotation: %v", rbErr)
		}
		// Rollback wgctrl: remove new, re-add old.
		if rbErr := h.wgClient.RemovePeer(h.cfg.Interface, newPubKey); rbErr != nil {
			log.Printf("CRITICAL: wgctrl remove-new rollback failed: %v", rbErr)
		}
		if rbErr := h.wgClient.ConfigurePeers(h.cfg.Interface, []wireguard.PeerConfig{
			{PublicKey: oldPubKey, AllowedIPs: allowedNets},
		}); rbErr != nil {
			log.Printf("CRITICAL: wgctrl re-add-old rollback failed: %v", rbErr)
		}
		writeError(w, http.StatusInternalServerError, "conf_write_error", err.Error())
		return
	}

	// Build new .conf content.
	dev, devErr := h.wgClient.GetDevice(h.cfg.Interface)
	serverPubKey := ""
	serverPort := 51820
	if devErr == nil {
		serverPubKey = dev.PublicKey.String()
		serverPort = dev.ListenPort
	}

	clientAddress := peer.AllowedIPs
	if clientAddress == "" {
		writeError(w, http.StatusInternalServerError, "config_error",
			"peer has no allowed_ips — cannot generate client config")
		return
	}
	serverAllowedIPs := "0.0.0.0/0, ::/0"

	conf := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s

[Peer]
PublicKey = %s
AllowedIPs = %s
Endpoint = %s
PersistentKeepalive = 25
`, newPrivKey.String(), clientAddress, serverPubKey, serverAllowedIPs, h.serverEndpoint(serverPort))

	type RotateKeysResponse struct {
		PublicKey  string `json:"public_key"`
		Config    string `json:"config"`
	}

	writeJSON(w, http.StatusOK, RotateKeysResponse{
		PublicKey: newPubKey.String(),
		Config:   conf,
	})
}

// ApprovePeer handles POST /api/peers/{id}/approve.
// Approves an auto-discovered peer that was blocked by the reconciler.
func (h *Handlers) ApprovePeer(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return
	}

	peer, err := h.store.GetPeerByID(id)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "not_found", "peer not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	// Validate: must be auto_discovered and disabled.
	if !peer.AutoDiscovered {
		writeError(w, http.StatusBadRequest, "validation_error", "peer is not auto-discovered")
		return
	}
	if peer.Enabled {
		writeError(w, http.StatusBadRequest, "validation_error", "peer is already enabled")
		return
	}

	// Optionally accept allowed_ips in request body.
	var reqBody struct {
		AllowedIPs []string `json:"allowed_ips,omitempty"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
	}

	allowedIPsStr := peer.AllowedIPs
	if len(reqBody.AllowedIPs) > 0 {
		if err := middleware.ValidateAllowedIPs(reqBody.AllowedIPs); err != nil {
			writeError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		allowedIPsStr = strings.Join(reqBody.AllowedIPs, ",")
		// Update allowed_ips in DB.
		newIPs := allowedIPsStr
		if err := h.store.UpdatePeer(peer.PublicKey, &storage.PeerUpdate{AllowedIPs: &newIPs}); err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
	}

	// Update DB: enabled=true, auto_discovered=false.
	if err := h.store.ApprovePeer(peer.PublicKey); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	// Add to wgctrl.
	pubKeyBytes, err := wireguard.ParseKey(peer.PublicKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "key_parse_error", err.Error())
		return
	}

	var allowedNets []net.IPNet
	if allowedIPsStr != "" {
		for _, cidr := range strings.Split(allowedIPsStr, ",") {
			cidr = strings.TrimSpace(cidr)
			if _, ipNet, parseErr := net.ParseCIDR(cidr); parseErr == nil {
				allowedNets = append(allowedNets, *ipNet)
			}
		}
	}

	if err := h.wgClient.ConfigurePeers(h.cfg.Interface, []wireguard.PeerConfig{
		{PublicKey: pubKeyBytes, AllowedIPs: allowedNets},
	}); err != nil {
		log.Printf("WARNING: wgctrl configure failed during approve: %v", err)
	}

	// Rewrite conf (debounced — non-critical).
	h.notifyConfChange()

	// Re-read and return.
	updated, err := h.store.GetPeerByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, peerToResponse(*updated))
}

// DeletePeer handles DELETE /api/peers/{id}.
func (h *Handlers) DeletePeer(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return
	}

	// Lookup peer in SQLite.
	peer, err := h.store.GetPeerByID(id)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "not_found", "peer not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	// Remove from wgctrl.
	pubKeyBytes, err := wireguard.ParseKey(peer.PublicKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "key_parse_error", err.Error())
		return
	}
	if err := h.wgClient.RemovePeer(h.cfg.Interface, pubKeyBytes); err != nil {
		log.Printf("WARNING: wgctrl remove failed: %v", err)
	}

	// Delete from SQLite.
	if err := h.store.DeletePeer(peer.PublicKey); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	// Rewrite conf (debounced — non-critical).
	h.notifyConfChange()

	w.WriteHeader(http.StatusNoContent)
}

// GetPeerConf handles GET /api/peers/{id}/conf.
// Returns a client WireGuard .conf file.
func (h *Handlers) GetPeerConf(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return
	}

	peer, err := h.store.GetPeerByID(id)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "not_found", "peer not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	// Get server public key.
	dev, err := h.wgClient.GetDevice(h.cfg.Interface)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "wireguard_error", err.Error())
		return
	}

	// Note: This is a simplified version — actual client conf needs the client's private key,
	// which we don't store (RT-3). In practice, the full conf is only available at creation time.
	// This endpoint returns a template that the user must fill in with their private key.
	clientAddress := peer.AllowedIPs
	if clientAddress == "" {
		writeError(w, http.StatusInternalServerError, "config_error",
			"peer has no allowed_ips — cannot generate client config")
		return
	}

	// [Peer].AllowedIPs defines what traffic the CLIENT routes through the tunnel.
	// Default to full tunnel (0.0.0.0/0, ::/0). Profiles can customize this later.
	serverAllowedIPs := "0.0.0.0/0, ::/0"

	conf := fmt.Sprintf(`[Interface]
# PrivateKey = <insert your private key>
Address = %s

[Peer]
PublicKey = %s
AllowedIPs = %s
Endpoint = %s
PersistentKeepalive = 25
`, clientAddress, dev.PublicKey.String(), serverAllowedIPs, h.serverEndpoint(dev.ListenPort))

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.conf", peer.FriendlyName))
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(conf))
}

// GetPeerQR handles GET /api/peers/{id}/qr.
// Returns the peer's .conf as a QR code PNG image (256x256, Medium recovery).
func (h *Handlers) GetPeerQR(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return
	}

	peer, err := h.store.GetPeerByID(id)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "not_found", "peer not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	// Get server public key.
	dev, err := h.wgClient.GetDevice(h.cfg.Interface)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "wireguard_error", err.Error())
		return
	}

	clientAddress := peer.AllowedIPs
	if clientAddress == "" {
		writeError(w, http.StatusInternalServerError, "config_error",
			"peer has no allowed_ips — cannot generate QR code")
		return
	}
	serverAllowedIPs := "0.0.0.0/0, ::/0"

	conf := fmt.Sprintf(`[Interface]
# PrivateKey = <insert your private key>
Address = %s

[Peer]
PublicKey = %s
AllowedIPs = %s
Endpoint = %s
PersistentKeepalive = 25
`, clientAddress, dev.PublicKey.String(), serverAllowedIPs, h.serverEndpoint(dev.ListenPort))

	png, err := qrcode.Encode(conf, qrcode.Medium, 256)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "qr_error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%s-qr.png", peer.FriendlyName))
	w.WriteHeader(http.StatusOK)
	w.Write(png)
}

// Health handles GET /api/health.
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	resp := HealthResponse{
		Status:    "ok",
		WireGuard: "unknown",
		SQLite:    "unknown",
	}

	// Check WireGuard.
	if _, err := h.wgClient.GetDevice(h.cfg.Interface); err != nil {
		resp.WireGuard = "error"
		resp.Status = "degraded"
	} else {
		resp.WireGuard = "ok"
	}

	// Check SQLite.
	var result string
	if err := h.store.Conn().QueryRow("SELECT 1").Scan(&result); err != nil {
		resp.SQLite = "error"
		resp.Status = "degraded"
	} else {
		resp.SQLite = "ok"
	}

	status := http.StatusOK
	if resp.Status != "ok" {
		status = http.StatusServiceUnavailable
	}

	writeJSON(w, status, resp)
}

// Stats handles GET /api/stats.
// Returns aggregate statistics from live wgctrl data — no DB queries.
func (h *Handlers) Stats(w http.ResponseWriter, r *http.Request) {
	dev, err := h.wgClient.GetDevice(h.cfg.Interface)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "wireguard_error", err.Error())
		return
	}

	now := time.Now()
	onlineThreshold := 3 * time.Minute

	resp := StatsResponse{
		TotalPeers: len(dev.Peers),
	}

	for _, p := range dev.Peers {
		if !p.LastHandshake.IsZero() && now.Sub(p.LastHandshake) < onlineThreshold {
			resp.OnlinePeers++
		}
		resp.TotalRx += p.ReceiveBytes
		resp.TotalTx += p.TransmitBytes
	}

	writeJSON(w, http.StatusOK, resp)
}

// serverEndpoint returns the server endpoint string for client configs.
// Uses cfg.ExternalEndpoint if configured, otherwise falls back to a placeholder with the device's listen port.
func (h *Handlers) serverEndpoint(listenPort int) string {
	if h.cfg.ExternalEndpoint != "" {
		return h.cfg.ExternalEndpoint
	}
	return fmt.Sprintf("<server_endpoint>:%d", listenPort)
}

// rewriteConf rewrites the WireGuard conf file from current DB state.
// Uses DirectWrite to bypass debounce — the caller needs a synchronous
// result for rollback logic. Also cancels any pending debounced write.
func (h *Handlers) rewriteConf() error {
	peers, err := h.buildPeerConfs()
	if err != nil {
		return err
	}
	if h.debouncer != nil {
		return h.debouncer.DirectWrite(peers)
	}
	return h.confWriter.WriteConf(h.cfg.ConfPath, peers)
}

// notifyConfChange signals a non-critical conf mutation that can be
// coalesced with other writes within the debounce window (Story 5.3).
// If the debouncer is not configured, falls back to a synchronous write.
func (h *Handlers) notifyConfChange() {
	if h.debouncer != nil {
		h.debouncer.Notify()
		return
	}
	// Fallback: synchronous write (pre-debounce behaviour).
	if err := h.rewriteConf(); err != nil {
		log.Printf("WARNING: conf write failed: %v", err)
	}
}

// buildPeerConfs reads enabled peers from the DB and returns conf entries.
func (h *Handlers) buildPeerConfs() ([]confwriter.PeerConf, error) {
	dbPeers, err := h.store.ListPeers()
	if err != nil {
		return nil, err
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
	return peers, nil
}

// peerToResponse converts a storage Peer to an API PeerResponse.
func peerToResponse(p storage.Peer) PeerResponse {
	var allowedIPs []string
	if p.AllowedIPs != "" {
		allowedIPs = strings.Split(p.AllowedIPs, ",")
	}

	return PeerResponse{
		ID:             p.ID,
		PublicKey:      p.PublicKey,
		FriendlyName:   p.FriendlyName,
		AllowedIPs:     allowedIPs,
		Profile:        p.Profile,
		Enabled:        p.Enabled,
		AutoDiscovered: p.AutoDiscovered,
		CreatedAt:      p.CreatedAt,
		Notes:          p.Notes,
	}
}
