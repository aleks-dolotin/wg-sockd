package api

import (
	"net/http"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/middleware"
)

// NewRouter creates a new HTTP router with all API routes registered.
func NewRouter(h *Handlers) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", h.Health)
	mux.HandleFunc("GET /api/stats", h.Stats)
	mux.HandleFunc("GET /api/peers", h.ListPeers)
	mux.HandleFunc("POST /api/peers", h.CreatePeer)
	mux.HandleFunc("POST /api/peers/batch", h.BatchCreatePeers)
	mux.HandleFunc("DELETE /api/peers/{id}", h.DeletePeer)
	mux.HandleFunc("PUT /api/peers/{id}", h.UpdatePeer)
	mux.HandleFunc("POST /api/peers/{id}/rotate-keys", h.RotateKeys)
	mux.HandleFunc("POST /api/peers/{id}/approve", h.ApprovePeer)
	mux.HandleFunc("GET /api/peers/{id}/conf", h.GetPeerConf)
	mux.HandleFunc("GET /api/peers/{id}/qr", h.GetPeerQR)

	mux.HandleFunc("GET /api/profiles", h.ListProfiles)
	mux.HandleFunc("POST /api/profiles", h.CreateProfile)
	mux.HandleFunc("PUT /api/profiles/{name}", h.UpdateProfile)
	mux.HandleFunc("DELETE /api/profiles/{name}", h.DeleteProfile)

	return mux
}

// NewRateLimitedRouter creates a router wrapped with rate limiting and
// read-only guard middleware. Pass nil for any middleware to skip it.
func NewRateLimitedRouter(h *Handlers, rl *middleware.RateLimiter, ro middleware.ReadOnlyChecker) http.Handler {
	var handler http.Handler = NewRouter(h)

	// Read-only guard: reject writes when disk is full (innermost).
	if ro != nil {
		handler = middleware.ReadOnlyGuard(ro)(handler)
	}

	// Rate limiting: per-connection throttling (outermost).
	if rl != nil {
		handler = rl.Wrap(handler)
	}

	return handler
}

