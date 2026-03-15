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
	"sync"
	"time"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/config"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/confwriter"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/storage"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/wireguard"
)

// Handlers holds dependencies for API handlers.
type Handlers struct {
	wgClient wireguard.WireGuardClient
	store    *storage.DB
	cfg      *config.Config
	mu       sync.Mutex // protects rewriteConf from concurrent access
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(wgClient wireguard.WireGuardClient, store *storage.DB, cfg *config.Config) *Handlers {
	return &Handlers{
		wgClient: wgClient,
		store:    store,
		cfg:      cfg,
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
func (h *Handlers) CreatePeer(w http.ResponseWriter, r *http.Request) {
	var req CreatePeerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	if len(req.AllowedIPs) == 0 {
		writeError(w, http.StatusBadRequest, "validation_error", "allowed_ips is required")
		return
	}

	// Validate friendly_name — no newlines or control characters (config injection prevention).
	if strings.ContainsAny(req.FriendlyName, "\n\r") {
		writeError(w, http.StatusBadRequest, "validation_error", "friendly_name must not contain newlines")
		return
	}

	// Enforce peer limit (RT-2 DoS prevention).
	if h.cfg.PeerLimit > 0 {
		peers, err := h.store.ListPeers()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		if len(peers) >= h.cfg.PeerLimit {
			writeError(w, http.StatusForbidden, "quota_exceeded",
				fmt.Sprintf("peer limit reached (%d)", h.cfg.PeerLimit))
			return
		}
	}

	// Validate CIDRs.
	for _, cidr := range req.AllowedIPs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			writeError(w, http.StatusBadRequest, "validation_error", fmt.Sprintf("invalid CIDR: %s", cidr))
			return
		}
	}

	// Generate keypair.
	privKey, pubKey, err := h.wgClient.GenerateKeyPair()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "keygen_error", err.Error())
		return
	}

	// Parse allowed IPs for wgctrl.
	var allowedNets []net.IPNet
	for _, cidr := range req.AllowedIPs {
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
	allowedIPsStr := strings.Join(req.AllowedIPs, ",")
	dbPeer := &storage.Peer{
		PublicKey:     pubKey.String(),
		FriendlyName:  req.FriendlyName,
		AllowedIPs:    allowedIPsStr,
		Enabled:       true,
	}
	if req.Profile != nil {
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
		_ = h.store.DeletePeer(pubKey.String())
		_ = h.wgClient.RemovePeer(h.cfg.Interface, pubKey)
		writeError(w, http.StatusInternalServerError, "conf_write_error", err.Error())
		return
	}
	resp := PeerResponse{
		ID:           id,
		PublicKey:    pubKey.String(),
		FriendlyName: req.FriendlyName,
		AllowedIPs:   req.AllowedIPs,
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

	// Rewrite conf.
	if err := h.rewriteConf(); err != nil {
		log.Printf("WARNING: conf write failed: %v", err)
	}

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
		clientAddress = "10.0.0.2/32"
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
Endpoint = <server_endpoint>:%d
PersistentKeepalive = 25
`, clientAddress, dev.PublicKey.String(), serverAllowedIPs, dev.ListenPort)

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.conf", peer.FriendlyName))
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(conf))
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

// rewriteConf rewrites the WireGuard conf file from current DB state.
// Protected by mutex to prevent concurrent write races.
func (h *Handlers) rewriteConf() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	dbPeers, err := h.store.ListPeers()
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

	return confwriter.WriteConf(h.cfg.ConfPath, peers)
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
