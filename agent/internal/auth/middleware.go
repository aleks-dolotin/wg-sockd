package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/ctxkeys"
)

const sessionCookieName = "wg_sockd_session"

// Middleware provides HTTP authentication middleware.
// Check order: Unix socket exempt → session cookie → Bearer token → /api/health exempt → reject.
type Middleware struct {
	sessionStore   *SessionStore
	tokenVerifier  *TokenAuthVerifier // nil if token auth not enabled
	skipUnixSocket bool
}

// NewMiddleware creates auth middleware.
// tokenVerifier may be nil if token auth is not enabled.
func NewMiddleware(sessionStore *SessionStore, tokenVerifier *TokenAuthVerifier, skipUnixSocket bool) *Middleware {
	return &Middleware{
		sessionStore:   sessionStore,
		tokenVerifier:  tokenVerifier,
		skipUnixSocket: skipUnixSocket,
	}
}

// Wrap returns an http.Handler that enforces authentication.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// 1. Unix socket exempt (configurable).
		if m.skipUnixSocket && ctxkeys.IsUnixSocket(ctx) {
			next.ServeHTTP(w, r)
			return
		}

		// 2. Session cookie check.
		if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
			if sess, ok := m.sessionStore.Get(cookie.Value); ok {
				ctx = context.WithValue(ctx, ctxkeys.UsernameKey{}, sess.Username)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		// 3. Bearer token check.
		if m.tokenVerifier != nil {
			if username, ok := m.tokenVerifier.Authenticate(r); ok {
				ctx = context.WithValue(ctx, ctxkeys.UsernameKey{}, username)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		// 4. /api/health is always exempt.
		if r.URL.Path == "/api/health" {
			next.ServeHTTP(w, r)
			return
		}

		// 5. Not authenticated — content negotiation.
		// AC-43: expired cookie treated same as absent cookie.
		accept := r.Header.Get("Accept")
		if strings.Contains(accept, "text/html") {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "unauthorized",
		})
	})
}
