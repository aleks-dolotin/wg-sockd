// Package proxy provides the HTTP handler that combines static file serving,
// API reverse-proxying to the agent Unix socket, and a /ui/status endpoint.
package proxy

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strings"
)

// StateProvider returns the current connection state string.
type StateProvider interface {
	State() string
}

// Handler is the top-level HTTP handler for the UI proxy.
type Handler struct {
	mux *http.ServeMux
}

// NewHandler builds the combined handler:
//   - GET /ui/status      → connection state JSON
//   - /api/*               → reverse proxy to Unix socket
//   - everything else      → static SPA with index.html fallback
func NewHandler(socketPath, webDir string, state StateProvider) *Handler {
	mux := http.NewServeMux()

	// Status endpoint.
	mux.HandleFunc("GET /ui/status", statusHandler(state))

	// Reverse proxy for API calls.
	mux.Handle("/api/", apiProxy(socketPath))

	// Static SPA file server (catch-all).
	mux.Handle("/", spaHandler(webDir))

	return &Handler{mux: mux}
}

// ServeHTTP delegates to the internal mux after applying path.Clean (RT-4 SSRF prevention).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Sanitise the request path — prevents directory traversal and SSRF via path manipulation.
	r.URL.Path = path.Clean(r.URL.Path)
	if r.URL.RawPath != "" {
		r.URL.RawPath = path.Clean(r.URL.RawPath)
	}
	h.mux.ServeHTTP(w, r)
}

// --- Status endpoint ---

func statusHandler(state StateProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"state": state.State(),
		})
	}
}

// --- Reverse proxy ---

func apiProxy(socketPath string) http.Handler {
	target, _ := url.Parse("http://unix")
	rp := httputil.NewSingleHostReverseProxy(target)

	// Custom transport: dial via Unix socket instead of TCP.
	rp.Transport = &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", socketPath)
		},
	}

	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("proxy error: %s %s: %v", r.Method, r.URL.Path, err)
		http.Error(w, `{"error":"proxy_error","message":"agent unreachable"}`, http.StatusBadGateway)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only forward /api/ prefix — additional defence-in-depth (RT-4).
		if !strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api" {
			http.NotFound(w, r)
			return
		}
		rp.ServeHTTP(w, r)
	})
}

// --- SPA static file server ---

// spaHandler serves static files from webDir.
// For paths that don't match a file on disk, it falls back to index.html
// so that client-side routing works.
func spaHandler(webDir string) http.Handler {
	fileServer := http.FileServer(http.Dir(webDir))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file directly.
		filePath := webDir + r.URL.Path
		info, err := os.Stat(filePath)
		if err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		// Check if it's a directory with an index.html.
		if err == nil && info.IsDir() {
			indexPath := filePath + "/index.html"
			if _, indexErr := os.Stat(indexPath); indexErr == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// SPA fallback: serve index.html for client-side routing.
		indexPath := webDir + "/index.html"
		if _, err := os.Stat(indexPath); err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "UI not built — run 'npm run build' in ui/web first", http.StatusNotFound)
				return
			}
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		http.ServeFile(w, r, indexPath)
	})
}

// Ensure Handler implements http.Handler at compile time.
var _ http.Handler = (*Handler)(nil)



