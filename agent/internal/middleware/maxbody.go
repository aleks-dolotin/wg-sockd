// Package middleware — MaxBytesReader middleware to limit request body size.
package middleware

import (
	"net/http"
)

// MaxBodySize is the default maximum request body size (1 MB).
const MaxBodySize = 1 << 20 // 1 MB

// MaxBodyReader returns middleware that limits the request body to maxBytes.
// Write operations (POST, PUT, DELETE) are limited; GET/HEAD/OPTIONS pass through.
// The /api/health endpoint is always exempt.
func MaxBodyReader(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only limit write operations.
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// Health endpoint exempt.
			if r.URL.Path == "/api/health" {
				next.ServeHTTP(w, r)
				return
			}

			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

