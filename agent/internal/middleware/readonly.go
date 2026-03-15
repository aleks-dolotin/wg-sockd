// Package middleware — read-only middleware for graceful degradation (FM-6).
package middleware

import (
	"encoding/json"
	"net/http"
)

// ReadOnlyChecker is an interface for checking disk space read-only status.
type ReadOnlyChecker interface {
	IsReadOnly() bool
}

// ReadOnlyGuard returns middleware that rejects write operations (POST, PUT,
// DELETE) with HTTP 503 when the system is in read-only mode (disk full).
// GET requests and the health endpoint always pass through.
func ReadOnlyGuard(checker ReadOnlyChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Always allow read operations.
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// Always allow health endpoint.
			if r.URL.Path == "/api/health" {
				next.ServeHTTP(w, r)
				return
			}

			// Check read-only mode for write operations.
			if checker.IsReadOnly() {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":   "storage_unavailable",
					"message": "disk full — read-only mode active",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

