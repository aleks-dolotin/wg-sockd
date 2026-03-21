package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/ctxkeys"
)

const sessionCookieName = "wg_sockd_session"

// Middleware provides HTTP authentication middleware.
// Check order: Unix socket exempt → session cookie → Bearer token → /api/health exempt → reject.
type Middleware struct {
	sessionStore   *SessionStore
	tokenVerifier  *TokenAuthVerifier // nil if token auth not enabled
	skipUnixSocket bool
	secureCookies  string // "auto", "true", "false" — for refreshing cookie Expires on sliding window
}

// NewMiddleware creates auth middleware.
// tokenVerifier may be nil if token auth is not enabled.
func NewMiddleware(sessionStore *SessionStore, tokenVerifier *TokenAuthVerifier, skipUnixSocket bool, secureCookies string) *Middleware {
	return &Middleware{
		sessionStore:   sessionStore,
		tokenVerifier:  tokenVerifier,
		skipUnixSocket: skipUnixSocket,
		secureCookies:  secureCookies,
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
				// Sliding window: touch session + refresh cookie only for user-initiated requests.
				// Background polling (stats, health, connection status) must NOT extend the session,
				// otherwise an idle tab keeps the session alive forever.
				if !isBackgroundPoll(r) {
					m.sessionStore.Touch(cookie.Value)
					http.SetCookie(w, m.makeSessionCookie(sess.Token, m.sessionStore.ExpiresAt(sess), r))
				}
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

// makeSessionCookie creates a refreshed session cookie with sliding window Expires.
// Mirrors AuthHandlers.makeCookie but lives on Middleware for use in Wrap().
func (m *Middleware) makeSessionCookie(token string, expiresAt time.Time, r *http.Request) *http.Cookie {
	secure := false
	switch m.secureCookies {
	case "true":
		secure = true
	case "false":
		secure = false
	default: // "auto"
		if ctxkeys.IsUnixSocket(r.Context()) {
			secure = r.Header.Get("X-Forwarded-Proto") == "https"
		} else {
			secure = r.TLS != nil
		}
	}

	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
	}
}

// backgroundPollPaths are GET endpoints that the UI polls automatically on a timer.
// These must NOT extend the sliding session window — otherwise an idle browser tab
// keeps the session alive forever.
var backgroundPollPaths = map[string]bool{
	"/api/stats":           true,
	"/api/health":          true,
	"/api/auth/session":    true,
}

// isBackgroundPoll returns true for GET requests to known polling endpoints.
func isBackgroundPoll(r *http.Request) bool {
	return r.Method == http.MethodGet && backgroundPollPaths[r.URL.Path]
}
