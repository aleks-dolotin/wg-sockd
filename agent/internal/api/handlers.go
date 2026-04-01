package api

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/config"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/firewall"
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
	firewall   firewall.Firewall           // per-peer iptables enforcement
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(
	wgClient wireguard.WireGuardClient,
	store *storage.DB,
	cfg *config.Config,
	confWriter *confwriter.SharedWriter,
	debouncer *confwriter.DebouncedWriter,
	reconciler ReconcilerPauser,
	fw firewall.Firewall,
) *Handlers {
	return &Handlers{
		wgClient:   wgClient,
		store:      store,
		cfg:        cfg,
		confWriter: confWriter,
		debouncer:  debouncer,
		reconciler: reconciler,
		firewall:   fw,
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

	// Validate endpoint if provided.
	if req.ConfiguredEndpoint != "" {
		if _, _, err := net.SplitHostPort(req.ConfiguredEndpoint); err != nil {
			writeError(w, http.StatusBadRequest, "validation_error",
				fmt.Sprintf("invalid endpoint format %q: must be host:port", req.ConfiguredEndpoint))
			return
		}
	}

	// Validate persistent_keepalive range.
	if req.PersistentKeepalive != nil {
		if *req.PersistentKeepalive < 0 || *req.PersistentKeepalive > 65535 {
			writeError(w, http.StatusBadRequest, "validation_error",
				"persistent_keepalive must be 0-65535")
			return
		}
	}

	// WYSIWYG: client_allowed_ips and client_address are required at creation.
	if req.ClientAllowedIPs == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "client_allowed_ips is required")
		return
	}
	if req.ClientAddress == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "client_address is required")
		return
	}

	// Validate client_address CIDR format.
	if _, _, err := net.ParseCIDR(req.ClientAddress); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error",
			fmt.Sprintf("invalid client_address CIDR format %q", req.ClientAddress))
		return
	}
	// Check uniqueness.
	taken, err := h.store.IsClientAddressTaken(req.ClientAddress, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	if taken {
		writeError(w, http.StatusConflict, "conflict",
			fmt.Sprintf("client_address %q is already assigned to another peer", req.ClientAddress))
		return
	}

	// Generate keypair.
	privKey, pubKey, err := h.wgClient.GenerateKeyPair()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "keygen_error", err.Error())
		return
	}

	// Resolve PSK from request field only. Profile pre-fills the UI checkbox,
	// but the backend respects only the explicit client request.
	var pskStr string
	var pskKey *wgtypes.Key
	shouldGeneratePSK := req.PresharedKey == "auto"
	if shouldGeneratePSK {
		generated, genErr := h.wgClient.GeneratePresharedKey()
		if genErr != nil {
			writeError(w, http.StatusInternalServerError, "psk_error", genErr.Error())
			return
		}
		pskStr = generated.String()
		pskKey = &generated
	} else if req.PresharedKey != "" && req.PresharedKey != "auto" {
		// Explicit base64 PSK provided — validate and use.
		parsed, parseErr := wireguard.ParseKey(req.PresharedKey)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "validation_error",
				fmt.Sprintf("invalid preshared_key: %v", parseErr))
			return
		}
		pskStr = req.PresharedKey
		pskKey = &parsed
	}

	// Parse allowed IPs for wgctrl — server-side is always client_address/32.
	serverAllowedIP := clientAddressTo32(req.ClientAddress)
	_, serverNet, _ := net.ParseCIDR(serverAllowedIP) // already validated client_address above
	allowedNets := []net.IPNet{*serverNet}

	// Resolve client_allowed_ips: from profile CIDR math or from request.
	clientAllowedIPs := req.ClientAllowedIPs
	if hasProfile && clientAllowedIPs == "" {
		// Profile CIDR math result becomes client routing.
		clientAllowedIPs = strings.Join(resolvedAllowedIPs, ", ")
	}

	// Apply firewall rules BEFORE ConfigurePeers — zero exposure window (AC-18).
	// tempPeer is stored and reused in all rollback branches (never reconstructed inline).
	tempPeer := storage.Peer{
		PublicKey:        pubKey.String(),
		ClientAddress:    req.ClientAddress,
		ClientAllowedIPs: clientAllowedIPs,
		Enabled:          true,
	}
	if fwErr := h.firewall.ApplyPeer(tempPeer); fwErr != nil {
		log.Printf("WARN: firewall ApplyPeer failed for new peer %s: %v — continuing (AC-9)", pubKey, fwErr)
	}

	// Add to wgctrl.
	wgPeerCfg := wireguard.PeerConfig{
		PublicKey:    pubKey,
		AllowedIPs:   allowedNets,
		PresharedKey: pskKey,
	}
	// Parse endpoint for wgctrl (best-effort — DNS may not resolve yet).
	if req.ConfiguredEndpoint != "" {
		udpAddr, resolveErr := net.ResolveUDPAddr("udp", req.ConfiguredEndpoint)
		if resolveErr != nil {
			log.Printf("WARN: endpoint DNS resolution failed for %q: %v — stored in DB, wgctrl deferred to reconciler", req.ConfiguredEndpoint, resolveErr)
		} else {
			wgPeerCfg.Endpoint = udpAddr
		}
	}
	if req.PersistentKeepalive != nil {
		d := time.Duration(*req.PersistentKeepalive) * time.Second
		wgPeerCfg.PersistentKeepalive = &d
	}
	err = h.wgClient.ConfigurePeers(h.cfg.Interface, []wireguard.PeerConfig{wgPeerCfg})
	if err != nil {
		// Rollback firewall — peer was never fully created.
		if rbErr := h.firewall.RemovePeer(tempPeer); rbErr != nil {
			log.Printf("WARN: firewall RemovePeer rollback failed for %s: %v", pubKey, rbErr)
		}
		writeError(w, http.StatusInternalServerError, "wireguard_error", err.Error())
		return
	}

	// Insert to SQLite.
	dbPeer := &storage.Peer{
		PublicKey:           pubKey.String(),
		FriendlyName:        req.FriendlyName,
		AllowedIPs:          serverAllowedIP,
		Enabled:             true,
		Endpoint:            req.ConfiguredEndpoint,
		PersistentKeepalive: req.PersistentKeepalive,
		ClientDNS:           req.ClientDNS,
		ClientMTU:           req.ClientMTU,
		ClientAddress:       req.ClientAddress,
		PresharedKey:        pskStr,
		ClientAllowedIPs:    clientAllowedIPs,
	}
	if hasProfile {
		dbPeer.Profile = req.Profile
	}

	id, err := h.store.CreatePeer(dbPeer)
	if err != nil {
		// Rollback firewall + wgctrl.
		if rbErr := h.firewall.RemovePeer(tempPeer); rbErr != nil {
			log.Printf("WARN: firewall RemovePeer rollback failed for %s: %v", pubKey, rbErr)
		}
		_ = h.wgClient.RemovePeer(h.cfg.Interface, pubKey)
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	// Rewrite conf file — if this fails, rollback DB and kernel changes.
	if err := h.rewriteConf(); err != nil {
		log.Printf("ERROR: conf write failed, rolling back: %v", err)
		if rbErr := h.firewall.RemovePeer(tempPeer); rbErr != nil {
			log.Printf("WARN: firewall RemovePeer rollback failed for %s: %v", pubKey, rbErr)
		}
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
		ID:                  id,
		PublicKey:           pubKey.String(),
		FriendlyName:        req.FriendlyName,
		AllowedIPs:          []string{serverAllowedIP},
		Profile:             req.Profile,
		Enabled:             true,
		CreatedAt:           time.Now(),
		ConfiguredEndpoint:  req.ConfiguredEndpoint,
		PersistentKeepalive: req.PersistentKeepalive,
		ClientDNS:           req.ClientDNS,
		ClientMTU:           req.ClientMTU,
		ClientAddress:       req.ClientAddress,
		HasPresharedKey:     pskStr != "",
		ClientAllowedIPs:    clientAllowedIPs,
	}

	// Build full client conf with private key (one-time — never stored).
	dev, devErr := h.wgClient.GetDevice(h.cfg.Interface)
	serverPubKey := ""
	serverPort := 51820
	if devErr == nil {
		serverPubKey = dev.PublicKey.String()
		serverPort = dev.ListenPort
	}
	conf := h.buildClientConf(dbPeer, privKey.String(), serverPubKey, serverPort)

	// Generate QR code as base64 PNG (one-time — never stored).
	qrBase64 := ""
	if qrPNG, qrErr := qrcode.Encode(conf, qrcode.Medium, 1024); qrErr == nil {
		qrBase64 = base64.StdEncoding.EncodeToString(qrPNG)
	}

	// Return response with private key, full config and QR (one-time — never stored).
	type CreatePeerResponse struct {
		PeerResponse
		PrivateKey string `json:"private_key"`
		Config     string `json:"config"`
		QR         string `json:"qr"`
	}

	writeJSON(w, http.StatusCreated, CreatePeerResponse{
		PeerResponse: resp,
		PrivateKey:   privKey.String(),
		Config:       conf,
		QR:           qrBase64,
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

	// Hard cap on batch size to prevent resource exhaustion.
	const maxBatchSize = 250
	if len(req.Peers) > maxBatchSize {
		writeError(w, http.StatusBadRequest, "validation_error",
			fmt.Sprintf("batch size %d exceeds maximum of %d", len(req.Peers), maxBatchSize))
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
		// WYSIWYG: client_allowed_ips and client_address are required at creation.
		if p.ClientAllowedIPs == "" {
			writeError(w, http.StatusBadRequest, "validation_error",
				fmt.Sprintf("peer[%d]: client_allowed_ips is required", i))
			return
		}
		// Validate client_allowed_ips CIDR format.
		if p.ClientAllowedIPs != "" {
			for _, ip := range strings.Split(p.ClientAllowedIPs, ",") {
				ip = strings.TrimSpace(ip)
				if _, _, err := net.ParseCIDR(ip); err != nil {
					writeError(w, http.StatusBadRequest, "validation_error",
						fmt.Sprintf("peer[%d]: invalid client_allowed_ips CIDR format %q", i, ip))
					return
				}
			}
		}

		if p.ClientAddress == "" {
			writeError(w, http.StatusBadRequest, "validation_error",
				fmt.Sprintf("peer[%d]: client_address is required", i))
			return
		}
	}

	// Resolve profiles and generate keypairs.
	type resolvedPeer struct {
		privKey    wgtypes.Key
		pubKey     wgtypes.Key
		allowedIPs []string
		pskStr     string       // base64 PSK for DB storage; empty = no PSK
		pskKey     *wgtypes.Key // parsed PSK for wgctrl; nil = no PSK
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

		// Resolve PSK per peer — explicit request only.
		var pskStr string
		var pskKey *wgtypes.Key
		shouldGenPSK := p.PresharedKey == "auto"
		if shouldGenPSK {
			generated, genErr := h.wgClient.GeneratePresharedKey()
			if genErr != nil {
				writeError(w, http.StatusInternalServerError, "psk_error", genErr.Error())
				return
			}
			pskStr = generated.String()
			pskKey = &generated
		} else if p.PresharedKey != "" && p.PresharedKey != "auto" {
			parsed, parseErr := wireguard.ParseKey(p.PresharedKey)
			if parseErr != nil {
				writeError(w, http.StatusBadRequest, "validation_error",
					fmt.Sprintf("peer[%d]: invalid preshared_key: %v", i, parseErr))
				return
			}
			pskStr = p.PresharedKey
			pskKey = &parsed
		}

		resolved = append(resolved, resolvedPeer{
			privKey:    priv,
			pubKey:     pub,
			allowedIPs: ips,
			pskStr:     pskStr,
			pskKey:     pskKey,
			req:        p,
		})
	}

	// Single wgctrl call — server-side AllowedIPs is always client_address/32.
	wgConfigs := make([]wireguard.PeerConfig, 0, len(resolved))
	for _, rp := range resolved {
		serverIP := clientAddressTo32(rp.req.ClientAddress)
		_, serverNet, _ := net.ParseCIDR(serverIP)
		wgCfg := wireguard.PeerConfig{
			PublicKey:    rp.pubKey,
			AllowedIPs:   []net.IPNet{*serverNet},
			PresharedKey: rp.pskKey,
		}
		// Pass endpoint to wgctrl (best-effort DNS resolution).
		if rp.req.ConfiguredEndpoint != "" {
			udpAddr, resolveErr := net.ResolveUDPAddr("udp", rp.req.ConfiguredEndpoint)
			if resolveErr != nil {
				log.Printf("WARN: batch endpoint DNS resolution failed for %q: %v — deferred to reconciler", rp.req.ConfiguredEndpoint, resolveErr)
			} else {
				wgCfg.Endpoint = udpAddr
			}
		}
		if rp.req.PersistentKeepalive != nil {
			d := time.Duration(*rp.req.PersistentKeepalive) * time.Second
			wgCfg.PersistentKeepalive = &d
		}
		wgConfigs = append(wgConfigs, wgCfg)
	}

	// Pause reconciler BEFORE touching the kernel: the entire sequence
	// (firewall apply → ConfigurePeers → DB commit → conf write) must be atomic
	// with respect to the reconciler. Without this, the reconciler could see
	// peers in the kernel that are not yet in DB and delete them (strict mode).
	h.reconciler.Pause()
	defer h.reconciler.Resume()

	// Apply firewall rules BEFORE ConfigurePeers — zero exposure window (mirrors CreatePeer AC-18).
	// Build tempPeers list here for use in rollback branches below.
	tempPeers := make([]storage.Peer, 0, len(resolved))
	for _, rp := range resolved {
		clientAIPs := rp.req.ClientAllowedIPs
		if clientAIPs == "" {
			clientAIPs = strings.Join(rp.allowedIPs, ", ")
		}
		tp := storage.Peer{
			PublicKey:        rp.pubKey.String(),
			ClientAddress:    rp.req.ClientAddress,
			ClientAllowedIPs: clientAIPs,
			Enabled:          true,
		}
		tempPeers = append(tempPeers, tp)
		if fwErr := h.firewall.ApplyPeer(tp); fwErr != nil {
			log.Printf("WARN: firewall ApplyPeer failed for batch peer %s: %v — continuing (AC-9)", rp.pubKey, fwErr)
		}
	}

	if err := h.wgClient.ConfigurePeers(h.cfg.Interface, wgConfigs); err != nil {
		// Rollback firewall for all peers applied above.
		for _, tp := range tempPeers {
			if rbErr := h.firewall.RemovePeer(tp); rbErr != nil {
				log.Printf("WARN: firewall RemovePeer rollback failed for %s: %v", tp.PublicKey, rbErr)
			}
		}
		writeError(w, http.StatusInternalServerError, "wireguard_error", err.Error())
		return
	}

	// Single SQLite transaction.
	dbPeers := make([]*storage.Peer, 0, len(resolved))
	for _, rp := range resolved {
		// Server AllowedIPs = /32 from client_address.
		serverIP := clientAddressTo32(rp.req.ClientAddress)
		// Client AllowedIPs: from profile CIDR math or from request.
		clientAIPs := rp.req.ClientAllowedIPs
		if clientAIPs == "" {
			clientAIPs = strings.Join(rp.allowedIPs, ", ")
		}
		dbPeer := &storage.Peer{
			PublicKey:           rp.pubKey.String(),
			FriendlyName:        rp.req.FriendlyName,
			AllowedIPs:          serverIP,
			Enabled:             true,
			Endpoint:            rp.req.ConfiguredEndpoint,
			PersistentKeepalive: rp.req.PersistentKeepalive,
			ClientDNS:           rp.req.ClientDNS,
			ClientMTU:           rp.req.ClientMTU,
			ClientAddress:       rp.req.ClientAddress,
			PresharedKey:        rp.pskStr,
			ClientAllowedIPs:    clientAIPs,
		}
		if rp.req.Profile != nil && *rp.req.Profile != "" {
			dbPeer.Profile = rp.req.Profile
		}
		dbPeers = append(dbPeers, dbPeer)
	}

	ids, err := h.store.CreatePeersBatch(dbPeers)
	if err != nil {
		// Rollback firewall and wgctrl: remove all added peers.
		for _, tp := range tempPeers {
			if rbErr := h.firewall.RemovePeer(tp); rbErr != nil {
				log.Printf("WARN: firewall RemovePeer rollback failed for %s: %v", tp.PublicKey, rbErr)
			}
		}
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
		// Rollback firewall and wgctrl: remove all added peers.
		for _, tp := range tempPeers {
			if rbErr := h.firewall.RemovePeer(tp); rbErr != nil {
				log.Printf("WARN: firewall RemovePeer rollback failed for %s: %v", tp.PublicKey, rbErr)
			}
		}
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
	var newClientAllowedIPs string

	// Handle profile change — updates client routing, not server AllowedIPs.
	if req.Profile != nil {
		profilePtr := *req.Profile
		if profilePtr != nil && *profilePtr != "" {
			// Resolve profile → client allowed IPs via CIDR calculator.
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
			newClientAllowedIPs = strings.Join(result.Prefixes, ", ")
		} else {
			// Profile is being detached (set to null).
			// client_allowed_ips must be explicitly provided when detaching a profile.
			if req.ClientAllowedIPs == nil {
				writeError(w, http.StatusBadRequest, "validation_error",
					"client_allowed_ips is required when detaching a profile")
				return
			}
		}
	}

	// Handle client_address change — updates server AllowedIPs (/32).
	// Validate BEFORE wgctrl update to prevent kernel/DB divergence.
	if req.ClientAddress != nil {
		if *req.ClientAddress != "" {
			if _, _, err := net.ParseCIDR(*req.ClientAddress); err != nil {
				writeError(w, http.StatusBadRequest, "validation_error",
					fmt.Sprintf("invalid client_address CIDR format %q", *req.ClientAddress))
				return
			}
			// Check uniqueness (exclude current peer).
			taken, err := h.store.IsClientAddressTaken(*req.ClientAddress, existing.PublicKey)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "db_error", err.Error())
				return
			}
			if taken {
				writeError(w, http.StatusConflict, "conflict",
					fmt.Sprintf("client_address %q is already assigned to another peer", *req.ClientAddress))
				return
			}
		}
		if *req.ClientAddress != existing.ClientAddress {
			needsWgUpdate = true
		}
	}

	// Update wgctrl if client_address changed (server AllowedIPs = /32).
	if needsWgUpdate {
		pubKeyBytes, err := wireguard.ParseKey(existing.PublicKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "key_parse_error", err.Error())
			return
		}

		// Determine the effective client_address (new or existing).
		effectiveAddr := existing.ClientAddress
		if req.ClientAddress != nil {
			effectiveAddr = *req.ClientAddress
		}
		serverIP := clientAddressTo32(effectiveAddr)
		_, serverNet, _ := net.ParseCIDR(serverIP)

		err = h.wgClient.ConfigurePeers(h.cfg.Interface, []wireguard.PeerConfig{
			{
				PublicKey:         pubKeyBytes,
				AllowedIPs:        []net.IPNet{*serverNet},
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
		effectiveAddr := existing.ClientAddress
		if req.ClientAddress != nil {
			effectiveAddr = *req.ClientAddress
		}
		serverIP := clientAddressTo32(effectiveAddr)
		dbUpdate.AllowedIPs = &serverIP
	}
	if newClientAllowedIPs != "" {
		dbUpdate.ClientAllowedIPs = &newClientAllowedIPs
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

	// Handle new fields: endpoint, PKA, client DNS/MTU.
	endpointChanged := false
	pkaChanged := false

	if req.ConfiguredEndpoint != nil {
		// Validate endpoint format.
		if *req.ConfiguredEndpoint != "" {
			if _, _, err := net.SplitHostPort(*req.ConfiguredEndpoint); err != nil {
				writeError(w, http.StatusBadRequest, "validation_error",
					fmt.Sprintf("invalid endpoint format %q: must be host:port", *req.ConfiguredEndpoint))
				return
			}
		}
		dbUpdate.Endpoint = req.ConfiguredEndpoint
		endpointChanged = true
	}
	if req.PersistentKeepalive != nil {
		if *req.PersistentKeepalive != nil {
			v := **req.PersistentKeepalive
			if v < 0 || v > 65535 {
				writeError(w, http.StatusBadRequest, "validation_error",
					"persistent_keepalive must be 0-65535")
				return
			}
		}
		dbUpdate.PersistentKeepalive = req.PersistentKeepalive
		pkaChanged = true
	}
	if req.ClientDNS != nil {
		dbUpdate.ClientDNS = req.ClientDNS
	}
	if req.ClientMTU != nil {
		dbUpdate.ClientMTU = req.ClientMTU
	}
	if req.ClientAddress != nil {
		// Validation already done before wgctrl update.
		dbUpdate.ClientAddress = req.ClientAddress
	}
	if req.ClientAllowedIPs != nil {
		dbUpdate.ClientAllowedIPs = req.ClientAllowedIPs
	}

	// Update wgctrl if endpoint or PKA changed (server-side conf fields).
	if endpointChanged || pkaChanged {
		pubKeyBytes, parseErr := wireguard.ParseKey(existing.PublicKey)
		if parseErr != nil {
			writeError(w, http.StatusInternalServerError, "key_parse_error", parseErr.Error())
			return
		}
		wgCfg := wireguard.PeerConfig{PublicKey: pubKeyBytes}
		if endpointChanged && req.ConfiguredEndpoint != nil && *req.ConfiguredEndpoint != "" {
			udpAddr, resolveErr := net.ResolveUDPAddr("udp", *req.ConfiguredEndpoint)
			if resolveErr != nil {
				log.Printf("WARN: endpoint DNS resolution failed for %q: %v — stored in DB, wgctrl deferred", *req.ConfiguredEndpoint, resolveErr)
			} else {
				wgCfg.Endpoint = udpAddr
			}
		}
		if pkaChanged && req.PersistentKeepalive != nil && *req.PersistentKeepalive != nil {
			d := time.Duration(**req.PersistentKeepalive) * time.Second
			wgCfg.PersistentKeepalive = &d
		}
		if wgCfg.Endpoint != nil || wgCfg.PersistentKeepalive != nil {
			if wgErr := h.wgClient.ConfigurePeers(h.cfg.Interface, []wireguard.PeerConfig{wgCfg}); wgErr != nil {
				log.Printf("WARN: wgctrl reconfigure failed for peer %s: %v — DB update proceeds", existing.PublicKey, wgErr)
			}
		}
		needsWgUpdate = true // trigger conf rewrite
	}

	if err := h.store.UpdatePeer(existing.PublicKey, dbUpdate); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	// Apply firewall when client_allowed_ips, client_address, enabled, or profile changed.
	// Profile change updates client_allowed_ips in DB — firewall must reflect new CIDRs.
	firewallRelevant := req.ClientAllowedIPs != nil || req.ClientAddress != nil || req.Enabled != nil || req.Profile != nil
	if firewallRelevant {
		// If client_address changed, remove the old jump rule from the dispatch chain
		// before applying the new one. Without this, the old "-s oldAddr -j chainName"
		// entry persists in WG_SOCKD_FORWARD and causes incorrect filtering if the old
		// address is reassigned to another peer.
		if req.ClientAddress != nil && *req.ClientAddress != existing.ClientAddress {
			if rmErr := h.firewall.RemovePeer(*existing); rmErr != nil {
				log.Printf("WARN: firewall RemovePeer (old address) failed for peer %s: %v", existing.PublicKey, rmErr)
			}
		}
		// Re-read to get the fully updated peer for firewall.
		if fwPeer, fwErr := h.store.GetPeerByID(id); fwErr == nil {
			if applyErr := h.firewall.ApplyPeer(*fwPeer); applyErr != nil {
				log.Printf("WARN: firewall ApplyPeer failed for updated peer %s: %v", existing.PublicKey, applyErr)
			}
		}
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

	// Save old PSK for rollback.
	oldPSKStr := peer.PresharedKey
	var oldPSKKey *wgtypes.Key
	if oldPSKStr != "" {
		parsed, parseErr := wireguard.ParseKey(oldPSKStr)
		if parseErr != nil {
			log.Printf("WARN: could not parse existing PSK for peer %d: %v — rotating without PSK rollback", peer.ID, parseErr)
		} else {
			oldPSKKey = &parsed
		}
	}

	// Generate new keypair.
	newPrivKey, newPubKey, err := h.wgClient.GenerateKeyPair()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "keygen_error", err.Error())
		return
	}

	// If peer had a PSK, generate a new one.
	var newPSKStr string
	var newPSKKey *wgtypes.Key
	if oldPSKStr != "" {
		generated, genErr := h.wgClient.GeneratePresharedKey()
		if genErr != nil {
			writeError(w, http.StatusInternalServerError, "psk_error", genErr.Error())
			return
		}
		newPSKStr = generated.String()
		newPSKKey = &generated
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

	// Remove old firewall chain before DB update (old key still known here).
	if fwErr := h.firewall.RemovePeer(*peer); fwErr != nil {
		log.Printf("WARN: firewall RemovePeer (old key) failed for peer %d: %v", peer.ID, fwErr)
	}

	// Step 1: Remove old peer from wgctrl.
	if err := h.wgClient.RemovePeer(h.cfg.Interface, oldPubKey); err != nil {
		log.Printf("WARNING: removing old peer from wgctrl: %v", err)
		// Continue — key rotation must succeed even if wgctrl remove fails.
	}

	// Step 2: Add new peer to wgctrl.
	err = h.wgClient.ConfigurePeers(h.cfg.Interface, []wireguard.PeerConfig{
		{
			PublicKey:    newPubKey,
			AllowedIPs:   allowedNets,
			PresharedKey: newPSKKey,
		},
	})
	if err != nil {
		// Attempt to re-add old peer (best-effort rollback).
		log.Printf("ERROR: adding new peer to wgctrl failed: %v — attempting rollback", err)
		rollbackErr := h.wgClient.ConfigurePeers(h.cfg.Interface, []wireguard.PeerConfig{
			{PublicKey: oldPubKey, AllowedIPs: allowedNets, PresharedKey: oldPSKKey},
		})
		if rollbackErr != nil {
			log.Printf("ERROR: rollback failed: %v", rollbackErr)
		}
		writeError(w, http.StatusInternalServerError, "wireguard_error", err.Error())
		return
	}

	// Step 3: Update SQLite — public key first, then PSK.
	if err := h.store.UpdatePeerPublicKey(peer.PublicKey, newPubKey.String()); err != nil {
		// Rollback wgctrl: remove new, re-add old.
		log.Printf("ERROR: DB update failed: %v — rolling back wgctrl", err)
		if rbErr := h.wgClient.RemovePeer(h.cfg.Interface, newPubKey); rbErr != nil {
			log.Printf("CRITICAL: wgctrl remove-new rollback failed: %v", rbErr)
		}
		if rbErr := h.wgClient.ConfigurePeers(h.cfg.Interface, []wireguard.PeerConfig{
			{PublicKey: oldPubKey, AllowedIPs: allowedNets, PresharedKey: oldPSKKey},
		}); rbErr != nil {
			log.Printf("CRITICAL: wgctrl re-add-old rollback failed: %v", rbErr)
		}
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	// Update PSK in DB (peer now has newPubKey after UpdatePeerPublicKey).
	if newPSKStr != "" {
		if pskErr := h.store.UpdatePeer(newPubKey.String(), &storage.PeerUpdate{PresharedKey: &newPSKStr}); pskErr != nil {
			log.Printf("ERROR: PSK DB update failed: %v — rolling back", pskErr)
			// Rollback: restore old pubkey (PSK wasn't successfully updated, DB still has old PSK).
			if rbErr := h.store.UpdatePeerPublicKey(newPubKey.String(), peer.PublicKey); rbErr != nil {
				log.Printf("CRITICAL: DB pubkey rollback failed: %v", rbErr)
			}
			if rbErr := h.wgClient.RemovePeer(h.cfg.Interface, newPubKey); rbErr != nil {
				log.Printf("CRITICAL: wgctrl remove-new rollback failed: %v", rbErr)
			}
			if rbErr := h.wgClient.ConfigurePeers(h.cfg.Interface, []wireguard.PeerConfig{
				{PublicKey: oldPubKey, AllowedIPs: allowedNets, PresharedKey: oldPSKKey},
			}); rbErr != nil {
				log.Printf("CRITICAL: wgctrl re-add-old rollback failed: %v", rbErr)
			}
			writeError(w, http.StatusInternalServerError, "db_error", pskErr.Error())
			return
		}
	}

	// Step 4: Rewrite conf — rollback everything on failure.
	if err := h.rewriteConf(); err != nil {
		log.Printf("ERROR: conf write failed after key rotation, rolling back: %v", err)
		// Rollback DB: restore old public key.
		if rbErr := h.store.UpdatePeerPublicKey(newPubKey.String(), peer.PublicKey); rbErr != nil {
			log.Printf("CRITICAL: DB rollback failed during key rotation: %v", rbErr)
		}
		// Restore old PSK in DB.
		if rbErr := h.store.UpdatePeer(peer.PublicKey, &storage.PeerUpdate{PresharedKey: &oldPSKStr}); rbErr != nil {
			log.Printf("CRITICAL: PSK DB rollback failed: %v", rbErr)
		}
		// Rollback wgctrl: remove new, re-add old.
		if rbErr := h.wgClient.RemovePeer(h.cfg.Interface, newPubKey); rbErr != nil {
			log.Printf("CRITICAL: wgctrl remove-new rollback failed: %v", rbErr)
		}
		if rbErr := h.wgClient.ConfigurePeers(h.cfg.Interface, []wireguard.PeerConfig{
			{PublicKey: oldPubKey, AllowedIPs: allowedNets, PresharedKey: oldPSKKey},
		}); rbErr != nil {
			log.Printf("CRITICAL: wgctrl re-add-old rollback failed: %v", rbErr)
		}
		writeError(w, http.StatusInternalServerError, "conf_write_error", err.Error())
		return
	}

	// Apply firewall rules for new key after DB is updated with new public key.
	updatedPeerForFW := *peer
	updatedPeerForFW.PublicKey = newPubKey.String()
	if fwErr := h.firewall.ApplyPeer(updatedPeerForFW); fwErr != nil {
		log.Printf("WARN: firewall ApplyPeer (new key) failed for peer %d: %v", peer.ID, fwErr)
	}

	// Build new .conf content using ClientConfBuilder.
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

	// Build a synthetic peer with updated public key and PSK for conf generation.
	rotatedPeer := *peer
	rotatedPeer.PublicKey = newPubKey.String()
	rotatedPeer.PresharedKey = newPSKStr

	conf := h.buildClientConf(&rotatedPeer, newPrivKey.String(), serverPubKey, serverPort)

	// Generate QR code as base64 PNG (one-time — never stored).
	qrBase64 := ""
	if qrPNG, qrErr := qrcode.Encode(conf, qrcode.Medium, 1024); qrErr == nil {
		qrBase64 = base64.StdEncoding.EncodeToString(qrPNG)
	}

	type RotateKeysResponse struct {
		PublicKey string `json:"public_key"`
		Config    string `json:"config"`
		QR        string `json:"qr"`
	}

	writeJSON(w, http.StatusOK, RotateKeysResponse{
		PublicKey: newPubKey.String(),
		Config:    conf,
		QR:        qrBase64,
	})
}

// ApprovePeer handles POST /api/peers/{id}/approve.
// Expands auto-discovered peer approval to full onboarding with peer configuration.
// Requires client_address in request body.
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

	// Parse full onboarding request.
	var req ApprovePeerRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
	}

	// client_address is required for approve.
	if req.ClientAddress == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "client_address is required")
		return
	}

	// WYSIWYG: client_allowed_ips is required for approve.
	if req.ClientAllowedIPs == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "client_allowed_ips is required")
		return
	}
	if _, _, err := net.ParseCIDR(req.ClientAddress); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error",
			fmt.Sprintf("invalid client_address CIDR format %q", req.ClientAddress))
		return
	}
	taken, err := h.store.IsClientAddressTaken(req.ClientAddress, peer.PublicKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	if taken {
		writeError(w, http.StatusConflict, "conflict",
			fmt.Sprintf("client_address %q is already assigned to another peer", req.ClientAddress))
		return
	}

	// Resolve client routing: from profile or from request.
	clientAllowedIPsStr := req.ClientAllowedIPs
	hasProfile := req.Profile != nil && *req.Profile != ""

	if hasProfile {
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
		clientAllowedIPsStr = strings.Join(result.Prefixes, ", ")
	}

	// Server AllowedIPs = /32 from client_address.
	serverAllowedIP := clientAddressTo32(req.ClientAddress)

	// Validate friendly_name if provided.
	friendlyName := peer.FriendlyName
	if req.FriendlyName != "" {
		if err := middleware.ValidateFriendlyName(req.FriendlyName); err != nil {
			writeError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		friendlyName = req.FriendlyName
	}

	// Validate endpoint if provided.
	if req.ConfiguredEndpoint != "" {
		if _, _, err := net.SplitHostPort(req.ConfiguredEndpoint); err != nil {
			writeError(w, http.StatusBadRequest, "validation_error",
				fmt.Sprintf("invalid endpoint format %q: must be host:port", req.ConfiguredEndpoint))
			return
		}
	}

	// Validate persistent_keepalive range.
	if req.PersistentKeepalive != nil {
		if *req.PersistentKeepalive < 0 || *req.PersistentKeepalive > 65535 {
			writeError(w, http.StatusBadRequest, "validation_error",
				"persistent_keepalive must be 0-65535")
			return
		}
	}

	// Build DB update with all provided fields.
	dbUpdate := &storage.PeerUpdate{
		FriendlyName:  &friendlyName,
		AllowedIPs:    &serverAllowedIP,
		ClientAddress: &req.ClientAddress,
	}
	enabled := true
	dbUpdate.Enabled = &enabled

	if hasProfile {
		dbUpdate.Profile = &req.Profile
	}
	if req.ConfiguredEndpoint != "" {
		dbUpdate.Endpoint = &req.ConfiguredEndpoint
	}
	if req.ClientDNS != "" {
		dbUpdate.ClientDNS = &req.ClientDNS
	}
	if req.ClientMTU != nil {
		dbUpdate.ClientMTU = &req.ClientMTU
	}
	if req.PersistentKeepalive != nil {
		dbUpdate.PersistentKeepalive = &req.PersistentKeepalive
	}
	if clientAllowedIPsStr != "" {
		dbUpdate.ClientAllowedIPs = &clientAllowedIPsStr
	}

	// Update DB: apply all fields + enabled=true, auto_discovered=false.
	if err := h.store.UpdatePeer(peer.PublicKey, dbUpdate); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	// Clear auto_discovered flag separately (ApprovePeer sets enabled=1, auto_discovered=0).
	if err := h.store.ApprovePeer(peer.PublicKey); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	// Configure wgctrl with server AllowedIPs (/32) + endpoint + PKA.
	pubKeyBytes, err := wireguard.ParseKey(peer.PublicKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "key_parse_error", err.Error())
		return
	}

	_, serverNet, _ := net.ParseCIDR(serverAllowedIP)
	allowedNets := []net.IPNet{*serverNet}

	wgCfg := wireguard.PeerConfig{
		PublicKey:         pubKeyBytes,
		AllowedIPs:        allowedNets,
		ReplaceAllowedIPs: true,
	}
	if req.ConfiguredEndpoint != "" {
		udpAddr, resolveErr := net.ResolveUDPAddr("udp", req.ConfiguredEndpoint)
		if resolveErr != nil {
			log.Printf("WARN: endpoint DNS resolution failed for %q during approve: %v", req.ConfiguredEndpoint, resolveErr)
		} else {
			wgCfg.Endpoint = udpAddr
		}
	}
	if req.PersistentKeepalive != nil {
		d := time.Duration(*req.PersistentKeepalive) * time.Second
		wgCfg.PersistentKeepalive = &d
	}

	if err := h.wgClient.ConfigurePeers(h.cfg.Interface, []wireguard.PeerConfig{wgCfg}); err != nil {
		log.Printf("WARNING: wgctrl configure failed during approve: %v", err)
	}

	// Rewrite conf (debounced — non-critical).
	h.notifyConfChange()

	// Re-read updated peer for firewall apply (has ClientAddress set now).
	updated, err := h.store.GetPeerByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	// Apply firewall rules after peer is fully approved and conf written.
	if fwErr := h.firewall.ApplyPeer(*updated); fwErr != nil {
		log.Printf("WARN: firewall ApplyPeer failed for approved peer %s: %v", updated.PublicKey, fwErr)
	}

	resp := peerToResponse(*updated)

	// Merge live wgctrl data if available.
	dev, devErr := h.wgClient.GetDevice(h.cfg.Interface)
	if devErr == nil {
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

	// Pause reconciler to prevent race: reconciler must not re-add peer while we delete (AC-13).
	h.reconciler.Pause()
	defer h.reconciler.Resume()

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

	// Remove firewall rules after DB delete.
	if fwErr := h.firewall.RemovePeer(*peer); fwErr != nil {
		log.Printf("WARN: firewall RemovePeer failed for deleted peer %s: %v", peer.PublicKey, fwErr)
	}

	// Rewrite conf (debounced — non-critical).
	h.notifyConfChange()

	w.WriteHeader(http.StatusNoContent)
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

	// Check conf writability — can we create a temp file alongside wg0.conf?
	confDir := filepath.Dir(h.cfg.ConfPath)
	tmpFile := filepath.Join(confDir, ".wg-sockd-health-check")
	writable := false
	if f, err := os.Create(tmpFile); err == nil {
		_ = f.Close()
		_ = os.Remove(tmpFile)
		writable = true
	}
	resp.ConfWritable = &writable
	if !writable {
		resp.Status = "degraded"
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
func (h *Handlers) serverEndpoint(listenPort int) string {
	if h.cfg.ExternalEndpoint != "" {
		return h.cfg.ExternalEndpoint
	}
	// Should not happen — Validate() requires ExternalEndpoint at startup.
	log.Printf("BUG: ExternalEndpoint is empty — this should have been caught by config validation")
	return fmt.Sprintf("<server_endpoint>:%d", listenPort)
}

// buildClientConf generates a WireGuard client .conf string for a peer.
// WYSIWYG: reads peer fields directly — no cascade, no profile/global lookups.
// privateKey is empty for template-only confs (GetPeerConf).
func (h *Handlers) buildClientConf(peer *storage.Peer, privateKey string, serverPubKey string, serverPort int) string {
	// client_address is required — use directly.
	clientAddr := peer.ClientAddress
	if clientAddr == "" {
		log.Printf("WARN: peer %s has no client_address — using AllowedIPs as fallback", peer.PublicKey)
		clientAddr = peer.AllowedIPs
	}

	b := confwriter.NewClientConfBuilder()
	b.SetAddress(clientAddr).
		SetServerPublicKey(serverPubKey).
		SetServerEndpoint(h.serverEndpoint(serverPort)).
		SetDNS(peer.ClientDNS).
		SetClientAllowedIPs(peer.ClientAllowedIPs)

	// Handle *int fields: nil = skip, 0 = off (builder omits line).
	if peer.ClientMTU != nil {
		b.SetMTU(*peer.ClientMTU)
	}
	if peer.PersistentKeepalive != nil {
		b.SetPersistentKeepalive(*peer.PersistentKeepalive)
	}

	if peer.PresharedKey != "" {
		b.SetPresharedKey(peer.PresharedKey)
	}
	if privateKey != "" {
		b.SetPrivateKey(privateKey)
	}
	return b.Build()
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
		pc := confwriter.PeerConf{
			PublicKey:    p.PublicKey,
			AllowedIPs:   p.AllowedIPs,
			PresharedKey: p.PresharedKey,
			FriendlyName: p.FriendlyName,
			CreatedAt:    p.CreatedAt,
			Notes:        p.Notes,
			Endpoint:     p.Endpoint,
		}
		if p.PersistentKeepalive != nil {
			pc.PersistentKeepalive = *p.PersistentKeepalive
		}
		peers = append(peers, pc)
	}
	return peers, nil
}

// clientAddressTo32 strips the subnet mask from a client_address CIDR (e.g. "10.0.10.3/24")
// and returns a /32 string (e.g. "10.0.10.3/32") suitable for server [Peer] AllowedIPs.
func clientAddressTo32(clientAddress string) string {
	ip, _, err := net.ParseCIDR(clientAddress)
	if err != nil {
		return clientAddress // fallback: return as-is
	}
	return ip.String() + "/32"
}

// NextAddress handles GET /api/peers/next-address.
// Returns the next available tunnel IP address within the WireGuard interface subnet.
// Uses net.InterfaceByName to read the subnet from the OS. Returns 404 in dev mode
// when the WireGuard interface is not available. Returns 409 if the subnet is exhausted.
func (h *Handlers) NextAddress(w http.ResponseWriter, r *http.Request) {
	// Step 1: get subnet from OS interface.
	iface, err := net.InterfaceByName(h.cfg.Interface)
	if err != nil {
		writeError(w, http.StatusNotFound, "interface_not_found",
			fmt.Sprintf("Interface %q not available: %v", h.cfg.Interface, err))
		return
	}
	addrs, err := iface.Addrs()
	if err != nil || len(addrs) == 0 {
		writeError(w, http.StatusNotFound, "no_address",
			fmt.Sprintf("No addresses on interface %q", h.cfg.Interface))
		return
	}

	var subnet *net.IPNet
	var occupied = make(map[[4]byte]bool)

	for _, addr := range addrs {
		ip, ipNet, err := net.ParseCIDR(addr.String())
		if err != nil {
			continue
		}
		ip4 := ip.To4()
		if ip4 == nil {
			continue
		}
		subnet = ipNet
		var key [4]byte
		copy(key[:], ip4)
		occupied[key] = true // interface address is occupied
		break
	}

	if subnet == nil {
		writeError(w, http.StatusNotFound, "no_ipv4", "No IPv4 address on interface")
		return
	}

	// Step 2: collect occupied addresses from DB.
	peerAddrs, err := h.store.ListClientAddresses()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	for _, addr := range peerAddrs {
		ip, _, err := net.ParseCIDR(addr)
		if err != nil {
			continue
		}
		ip4 := ip.To4()
		if ip4 == nil {
			continue
		}
		var key [4]byte
		copy(key[:], ip4)
		occupied[key] = true
	}

	// Step 3: find next free address.
	ones, bits := subnet.Mask.Size()
	if bits != 32 {
		writeError(w, http.StatusNotImplemented, "ipv6_not_supported", "Only IPv4 subnets are supported")
		return
	}
	netIP := subnet.IP.To4()
	base := uint32(netIP[0])<<24 | uint32(netIP[1])<<16 | uint32(netIP[2])<<8 | uint32(netIP[3])
	totalHosts := (1 << (32 - ones)) - 2

	if totalHosts <= 0 {
		writeError(w, http.StatusConflict, "subnet_full", "Subnet too small for peer allocation")
		return
	}

	// Safety cap: avoid scanning millions of addresses for very large subnets (/8).
	scanLimit := totalHosts
	if scanLimit > 65535 {
		scanLimit = 65535
	}

	for i := 1; i <= scanLimit; i++ {
		n := base + uint32(i)
		candidate := net.IPv4(byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
		var key [4]byte
		copy(key[:], candidate.To4())
		if !occupied[key] {
			result := fmt.Sprintf("%s/%d", candidate.String(), ones)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"next_address": result})
			return
		}
	}

	// Subnet exhausted.
	used := len(occupied)
	writeError(w, http.StatusConflict, "subnet_full",
		fmt.Sprintf("No free addresses in %s (%d/%d used)", subnet.String(), used, totalHosts))
}

// peerToResponse converts a storage Peer to an API PeerResponse.
func peerToResponse(p storage.Peer) PeerResponse {
	var allowedIPs []string
	if p.AllowedIPs != "" {
		allowedIPs = strings.Split(p.AllowedIPs, ",")
	}

	return PeerResponse{
		ID:                  p.ID,
		PublicKey:           p.PublicKey,
		FriendlyName:        p.FriendlyName,
		AllowedIPs:          allowedIPs,
		Profile:             p.Profile,
		Enabled:             p.Enabled,
		AutoDiscovered:      p.AutoDiscovered,
		CreatedAt:           p.CreatedAt,
		Notes:               p.Notes,
		ConfiguredEndpoint:  p.Endpoint,
		PersistentKeepalive: p.PersistentKeepalive,
		ClientDNS:           p.ClientDNS,
		ClientMTU:           p.ClientMTU,
		ClientAddress:       p.ClientAddress,
		LastSeenEndpoint:    p.LastSeenEndpoint,
		// Phase 2: PSK — only expose status (never value); client_allowed_ips is safe to show.
		HasPresharedKey:  p.PresharedKey != "",
		ClientAllowedIPs: p.ClientAllowedIPs,
	}
}
