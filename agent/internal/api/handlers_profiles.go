package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/storage"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/wireguard"
)

// profileNameRe validates profile names: lowercase letters, digits, hyphens.
var profileNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$`)

// ListProfiles handles GET /api/profiles.
// Returns all profiles with resolved_allowed_ips computed via CIDR calculator.
func (h *Handlers) ListProfiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := h.store.ListProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	// Count peers per profile.
	peerCounts, err := h.store.CountPeersPerProfile()
	if err != nil {
		log.Printf("WARNING: could not count peers per profile: %v", err)
	}

	responses := make([]ProfileResponse, 0, len(profiles))
	for _, p := range profiles {
		resp := profileToResponse(p)
		if peerCounts != nil {
			resp.PeerCount = peerCounts[p.Name]
		}
		responses = append(responses, resp)
	}

	writeJSON(w, http.StatusOK, responses)
}

// CreateProfile handles POST /api/profiles.
func (h *Handlers) CreateProfile(w http.ResponseWriter, r *http.Request) {
	var req CreateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	// Validate name.
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "name is required")
		return
	}
	if len(req.Name) < 2 || len(req.Name) > 64 {
		writeError(w, http.StatusBadRequest, "validation_error", "name must be 2-64 characters")
		return
	}
	if !profileNameRe.MatchString(req.Name) {
		writeError(w, http.StatusBadRequest, "validation_error", "name must be lowercase alphanumeric with hyphens (e.g., 'my-profile')")
		return
	}

	// Validate allowed_ips.
	if len(req.AllowedIPs) == 0 {
		writeError(w, http.StatusBadRequest, "validation_error", "allowed_ips is required")
		return
	}

	// Validate CIDRs via the calculator (also validates format).
	_, err := wireguard.ComputeAllowedIPs(req.AllowedIPs, req.ExcludeIPs)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", fmt.Sprintf("invalid CIDR: %v", err))
		return
	}

	excludeIPs := req.ExcludeIPs
	if excludeIPs == nil {
		excludeIPs = []string{}
	}

	p := &storage.Profile{
		Name:                req.Name,
		AllowedIPs:          req.AllowedIPs,
		ExcludeIPs:          excludeIPs,
		Description:         req.Description,
		IsDefault:           false,
		Endpoint:            req.Endpoint,
		PersistentKeepalive: req.PersistentKeepalive,
		ClientDNS:           req.ClientDNS,
		ClientMTU:           req.ClientMTU,
	}

	if err := h.store.CreateProfile(p); err != nil {
		if storage.IsConflict(err) {
			writeError(w, http.StatusConflict, "conflict", fmt.Sprintf("profile %q already exists", req.Name))
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	// Re-read to get created_at.
	created, err := h.store.GetProfile(req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, profileToResponse(*created))
}

// UpdateProfile handles PUT /api/profiles/{name}.
func (h *Handlers) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	// Check profile exists.
	existing, err := h.store.GetProfile(name)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("profile %q not found", name))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	var req UpdateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	// Merge fields.
	updated := &storage.Profile{
		AllowedIPs:          existing.AllowedIPs,
		ExcludeIPs:          existing.ExcludeIPs,
		Description:         existing.Description,
		Endpoint:            existing.Endpoint,
		PersistentKeepalive: existing.PersistentKeepalive,
		ClientDNS:           existing.ClientDNS,
		ClientMTU:           existing.ClientMTU,
	}

	if req.AllowedIPs != nil {
		updated.AllowedIPs = req.AllowedIPs
	}
	if req.ExcludeIPs != nil {
		updated.ExcludeIPs = req.ExcludeIPs
	}
	if req.Description != nil {
		updated.Description = *req.Description
	}
	if req.Endpoint != nil {
		updated.Endpoint = *req.Endpoint
	}
	if req.PersistentKeepalive != nil {
		updated.PersistentKeepalive = *req.PersistentKeepalive
	}
	if req.ClientDNS != nil {
		updated.ClientDNS = *req.ClientDNS
	}
	if req.ClientMTU != nil {
		updated.ClientMTU = *req.ClientMTU
	}

	// Validate resulting CIDRs.
	_, err = wireguard.ComputeAllowedIPs(updated.AllowedIPs, updated.ExcludeIPs)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", fmt.Sprintf("invalid CIDR: %v", err))
		return
	}

	if err := h.store.UpdateProfile(name, updated); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	// Re-read updated profile.
	result, err := h.store.GetProfile(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, profileToResponse(*result))
}

// DeleteProfile handles DELETE /api/profiles/{name}.
func (h *Handlers) DeleteProfile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	err := h.store.DeleteProfile(name)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("profile %q not found", name))
		return
	}
	if err != nil {
		// DeleteProfile returns a descriptive error if peers reference it.
		writeError(w, http.StatusConflict, "conflict", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// profileToResponse converts a storage Profile to an API ProfileResponse,
// computing resolved_allowed_ips via the CIDR calculator.
func profileToResponse(p storage.Profile) ProfileResponse {
	resp := ProfileResponse{
		Name:                p.Name,
		AllowedIPs:          p.AllowedIPs,
		ExcludeIPs:          p.ExcludeIPs,
		Description:         p.Description,
		IsDefault:           p.IsDefault,
		Endpoint:            p.Endpoint,
		PersistentKeepalive: p.PersistentKeepalive,
		ClientDNS:           p.ClientDNS,
		ClientMTU:           p.ClientMTU,
	}

	if resp.AllowedIPs == nil {
		resp.AllowedIPs = []string{}
	}
	if resp.ExcludeIPs == nil {
		resp.ExcludeIPs = []string{}
	}

	// Compute resolved allowed IPs.
	result, err := wireguard.ComputeAllowedIPs(p.AllowedIPs, p.ExcludeIPs)
	if err != nil {
		log.Printf("WARNING: CIDR calculation failed for profile %q: %v", p.Name, err)
		resp.ResolvedAllowedIPs = resp.AllowedIPs
		resp.RouteCount = len(resp.AllowedIPs)
	} else {
		resp.ResolvedAllowedIPs = result.Prefixes
		resp.RouteCount = result.RouteCount
		resp.RouteWarning = result.Warning
	}

	return resp
}


