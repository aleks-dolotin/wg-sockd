// Package middleware — security headers for defence-in-depth.
package middleware

import "net/http"

// SecurityHeaders returns middleware that adds standard security response headers.
// These protect against clickjacking, MIME sniffing, and other browser-level attacks
// when the agent serves the embedded UI directly (serve_ui: true).
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

