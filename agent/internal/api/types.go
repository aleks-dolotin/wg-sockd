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
	Endpoint        string     `json:"endpoint,omitempty"`
	LatestHandshake *time.Time `json:"latest_handshake,omitempty"`
	TransferRx      int64      `json:"transfer_rx"`
	TransferTx      int64      `json:"transfer_tx"`
}

// CreatePeerRequest is the input for POST /api/peers.
type CreatePeerRequest struct {
	FriendlyName string   `json:"friendly_name"`
	AllowedIPs   []string `json:"allowed_ips"`
	Profile      *string  `json:"profile,omitempty"`
}

// UpdatePeerRequest is the input for PUT /api/peers/{id}.
type UpdatePeerRequest struct {
	FriendlyName *string  `json:"friendly_name,omitempty"`
	AllowedIPs   []string `json:"allowed_ips,omitempty"`
	Profile      **string `json:"profile,omitempty"`
	Enabled      *bool    `json:"enabled,omitempty"`
	Notes        *string  `json:"notes,omitempty"`
}

// PeerConfResponse holds .conf file content for client download.
type PeerConfResponse struct {
	PublicKey string `json:"public_key"`
	Config    string `json:"config"`
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
	DisplayName        string   `json:"display_name"`
	AllowedIPs         []string `json:"allowed_ips"`
	ExcludeIPs         []string `json:"exclude_ips"`
	ResolvedAllowedIPs []string `json:"resolved_allowed_ips"`
	Description        string   `json:"description,omitempty"`
	IsDefault          bool     `json:"is_default"`
	RouteCount         int      `json:"route_count"`
	RouteWarning       string   `json:"route_warning,omitempty"`
	PeerCount          int      `json:"peer_count"`
}

// CreateProfileRequest is the input for POST /api/profiles.
type CreateProfileRequest struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	AllowedIPs  []string `json:"allowed_ips"`
	ExcludeIPs  []string `json:"exclude_ips"`
	Description string   `json:"description,omitempty"`
}

// UpdateProfileRequest is the input for PUT /api/profiles/{name}.
type UpdateProfileRequest struct {
	DisplayName *string  `json:"display_name,omitempty"`
	AllowedIPs  []string `json:"allowed_ips,omitempty"`
	ExcludeIPs  []string `json:"exclude_ips,omitempty"`
	Description *string  `json:"description,omitempty"`
}

// --- Error Types ---

// ErrorResponse is the standard error format for all API errors.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}
