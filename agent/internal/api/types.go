// Package api provides the HTTP API router, handlers, and type definitions for wg-sockd.
package api

import "time"

// --- Peer Types ---

// PeerResponse represents a full peer with live wgctrl data merged.
type PeerResponse struct {
	ID              int64      `json:"id"`
	PublicKey       string     `json:"public_key"`
	FriendlyName    string     `json:"friendly_name"`
	AllowedIPs      []string   `json:"allowed_ips"`
	Profile         *string    `json:"profile,omitempty"`
	Enabled         bool       `json:"enabled"`
	AutoDiscovered  bool       `json:"auto_discovered"`
	CreatedAt       time.Time  `json:"created_at"`
	Notes           string     `json:"notes,omitempty"`
	Endpoint        string     `json:"endpoint,omitempty"`          // runtime from wg show
	LatestHandshake *time.Time `json:"latest_handshake,omitempty"`
	TransferRx      int64      `json:"transfer_rx"`
	TransferTx      int64      `json:"transfer_tx"`
	// New: configured (static) fields from DB
	ConfiguredEndpoint  string `json:"configured_endpoint,omitempty"`
	PersistentKeepalive *int   `json:"persistent_keepalive,omitempty"`
	ClientDNS           string `json:"client_dns,omitempty"`
	ClientMTU           *int   `json:"client_mtu,omitempty"`
	// Phase 1: client_address and last_seen_endpoint
	ClientAddress    string `json:"client_address,omitempty"`
	LastSeenEndpoint string `json:"last_seen_endpoint,omitempty"`
	// Phase 2: PSK status (never expose value in GET) + split-tunnel
	HasPresharedKey                bool   `json:"has_preshared_key"`
	ClientAllowedIPs               string `json:"client_allowed_ips,omitempty"`
}

// CreatePeerRequest is the input for POST /api/peers.
type CreatePeerRequest struct {
	FriendlyName        string   `json:"friendly_name"`
	AllowedIPs          []string `json:"allowed_ips"`
	Profile             *string  `json:"profile,omitempty"`
	ConfiguredEndpoint  string   `json:"configured_endpoint,omitempty"`
	PersistentKeepalive *int     `json:"persistent_keepalive,omitempty"`
	ClientDNS           string   `json:"client_dns,omitempty"`
	ClientMTU           *int     `json:"client_mtu,omitempty"`
	ClientAddress       string   `json:"client_address,omitempty"`
	PresharedKey        string   `json:"preshared_key,omitempty"` // "auto" = generate, base64 = use explicit, "" = none
	ClientAllowedIPs    string   `json:"client_allowed_ips,omitempty"`
}

// UpdatePeerRequest is the input for PUT /api/peers/{id}.
type UpdatePeerRequest struct {
	FriendlyName        *string  `json:"friendly_name,omitempty"`
	AllowedIPs          []string `json:"allowed_ips,omitempty"`
	Profile             **string `json:"profile,omitempty"`
	Enabled             *bool    `json:"enabled,omitempty"`
	Notes               *string  `json:"notes,omitempty"`
	ConfiguredEndpoint  *string  `json:"configured_endpoint,omitempty"`
	PersistentKeepalive **int    `json:"persistent_keepalive,omitempty"`
	ClientDNS           *string  `json:"client_dns,omitempty"`
	ClientMTU           **int    `json:"client_mtu,omitempty"`
	ClientAddress       *string  `json:"client_address,omitempty"`
	ClientAllowedIPs    *string  `json:"client_allowed_ips,omitempty"`
}

// PeerConfResponse holds .conf file content for client download.
type PeerConfResponse struct {
	PublicKey string `json:"public_key"`
	Config    string `json:"config"`
}

// ApprovePeerRequest is the input for POST /api/peers/{id}/approve.
// Expands simple approve to full onboarding with peer configuration.
type ApprovePeerRequest struct {
	FriendlyName        string   `json:"friendly_name,omitempty"`
	Profile             *string  `json:"profile,omitempty"`
	AllowedIPs          []string `json:"allowed_ips,omitempty"`
	ClientAddress       string   `json:"client_address"`
	ClientAllowedIPs    string   `json:"client_allowed_ips"`
	ConfiguredEndpoint  string   `json:"configured_endpoint,omitempty"`
	ClientDNS           string   `json:"client_dns,omitempty"`
	ClientMTU           *int     `json:"client_mtu,omitempty"`
	PersistentKeepalive *int     `json:"persistent_keepalive,omitempty"`
}

// BatchCreatePeersRequest wraps multiple peer creation requests.
type BatchCreatePeersRequest struct {
	Peers []CreatePeerRequest `json:"peers"`
}

// --- Health Types ---

// HealthResponse represents the system health status.
type HealthResponse struct {
	Status              string `json:"status"`
	WireGuard           string `json:"wireguard"`
	SQLite              string `json:"sqlite"`
	SQLiteRecoveredFrom string `json:"sqlite_recovered_from,omitempty"`
	DiskOK              *bool  `json:"disk_ok,omitempty"`
	ConfWritable        *bool  `json:"conf_writable,omitempty"`
}

// --- Stats Types ---

// StatsResponse holds aggregate statistics from live wgctrl data.
type StatsResponse struct {
	TotalPeers  int   `json:"total_peers"`
	OnlinePeers int   `json:"online_peers"`
	TotalRx     int64 `json:"total_rx"`
	TotalTx     int64 `json:"total_tx"`
}

// --- Profile Types ---

// ProfileResponse represents a peer profile with resolved allowed IPs.
type ProfileResponse struct {
	Name               string   `json:"name"`
	AllowedIPs         []string `json:"allowed_ips"`
	ExcludeIPs         []string `json:"exclude_ips"`
	ResolvedAllowedIPs []string `json:"resolved_allowed_ips"`
	Description        string   `json:"description,omitempty"`
	IsDefault          bool     `json:"is_default"`
	RouteCount         int      `json:"route_count"`
	RouteWarning       string   `json:"route_warning,omitempty"`
	PeerCount          int      `json:"peer_count"`
	PersistentKeepalive *int    `json:"persistent_keepalive,omitempty"`
	ClientDNS           string  `json:"client_dns,omitempty"`
	ClientMTU           *int    `json:"client_mtu,omitempty"`
	ClientAllowedIPs    string  `json:"client_allowed_ips,omitempty"`
	UsePresharedKey     bool    `json:"use_preshared_key"`
}

// CreateProfileRequest is the input for POST /api/profiles.
type CreateProfileRequest struct {
	Name                string   `json:"name"`
	AllowedIPs          []string `json:"allowed_ips"`
	ExcludeIPs          []string `json:"exclude_ips"`
	Description         string   `json:"description,omitempty"`
	PersistentKeepalive *int     `json:"persistent_keepalive,omitempty"`
	ClientDNS           string   `json:"client_dns,omitempty"`
	ClientMTU           *int     `json:"client_mtu,omitempty"`
	ClientAllowedIPs    string   `json:"client_allowed_ips,omitempty"`
	UsePresharedKey     bool     `json:"use_preshared_key"`
}

// UpdateProfileRequest is the input for PUT /api/profiles/{name}.
type UpdateProfileRequest struct {
	AllowedIPs          []string `json:"allowed_ips,omitempty"`
	ExcludeIPs          []string `json:"exclude_ips,omitempty"`
	Description         *string  `json:"description,omitempty"`
	PersistentKeepalive **int    `json:"persistent_keepalive,omitempty"`
	ClientDNS           *string  `json:"client_dns,omitempty"`
	ClientMTU           **int    `json:"client_mtu,omitempty"`
	ClientAllowedIPs    *string  `json:"client_allowed_ips,omitempty"`
	UsePresharedKey     *bool    `json:"use_preshared_key,omitempty"`
}

// --- Error Types ---

// ErrorResponse is the standard error format for all API errors.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}
