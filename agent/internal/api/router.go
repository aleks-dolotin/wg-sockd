package api

import "net/http"

// NewRouter creates a new HTTP router with all API routes registered.
func NewRouter(h *Handlers) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", h.Health)
	mux.HandleFunc("GET /api/peers", h.ListPeers)
	mux.HandleFunc("POST /api/peers", h.CreatePeer)
	mux.HandleFunc("DELETE /api/peers/{id}", h.DeletePeer)
	mux.HandleFunc("GET /api/peers/{id}/conf", h.GetPeerConf)

	return mux
}
