package auth

import (
	"encoding/json"
	"net"
	"net/http"
	"time"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/config"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/ctxkeys"
)

// loginRequest is the JSON body for POST /api/auth/login.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// AuthHandlers provides HTTP handlers for authentication endpoints.
type AuthHandlers struct {
	cfg            *config.AuthConfig
	sessionStore   *SessionStore
	basicVerifier  *BasicAuthVerifier // nil if basic auth not enabled
	rateLimiter    *LoginRateLimiter
	credCounter    WebAuthnCredentialCounter
	secureCookies  string // "auto", "true", "false"
}

// NewAuthHandlers creates auth handlers.
// basicVerifier may be nil if basic auth is not enabled.
func NewAuthHandlers(
	cfg *config.AuthConfig,
	sessionStore *SessionStore,
	basicVerifier *BasicAuthVerifier,
	rateLimiter *LoginRateLimiter,
	credCounter WebAuthnCredentialCounter,
) *AuthHandlers {
	return &AuthHandlers{
		cfg:           cfg,
		sessionStore:  sessionStore,
		basicVerifier: basicVerifier,
		rateLimiter:   rateLimiter,
		credCounter:   credCounter,
		secureCookies: cfg.SecureCookies,
	}
}

// Handler returns an http.Handler (ServeMux) with all auth endpoints registered.
func (h *AuthHandlers) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/login", h.login)
	mux.HandleFunc("POST /api/auth/logout", h.logout)
	mux.HandleFunc("DELETE /api/auth/logout", h.logout) // F8: workaround for SameSite=Lax
	mux.HandleFunc("GET /api/auth/session", h.session)
	return mux
}

// login handles POST /api/auth/login.
func (h *AuthHandlers) login(w http.ResponseWriter, r *http.Request) {
	// AC-41: If no auth methods enabled, return 400.
	if !h.cfg.AnyEnabled() {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "auth_not_configured",
			"message": "no authentication methods enabled",
		})
		return
	}

	if !h.cfg.Basic.Enabled {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "basic_auth_disabled",
			"message": "password authentication is not enabled",
		})
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "invalid_request",
			"message": "invalid JSON body",
		})
		return
	}

	// Rate limiting by remote IP (F5). Unix socket bypasses limiter.
	remoteIP := extractIP(r)
	if !ctxkeys.IsUnixSocket(r.Context()) && remoteIP != "" {
		if !h.rateLimiter.Check(remoteIP) {
			w.Header().Set("Retry-After", "60")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"error":   "rate_limit_exceeded",
				"message": "too many failed login attempts, try again later",
			})
			return
		}
	}

	// Verify credentials.
	if !h.basicVerifier.Verify(req.Username, req.Password) {
		if remoteIP != "" {
			h.rateLimiter.RecordFailure(remoteIP)
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error":   "invalid_credentials",
			"message": "invalid username or password",
		})
		return
	}

	// Success — rotate session (delete old, create new).
	h.sessionStore.DeleteByUsername(req.Username)
	token, err := h.sessionStore.Create(req.Username)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error":   "internal_error",
			"message": "failed to create session",
		})
		return
	}

	// Reset rate limiter on success.
	if remoteIP != "" {
		h.rateLimiter.Reset(remoteIP)
	}

	// Set session cookie.
	sess, _ := h.sessionStore.Get(token)
	http.SetCookie(w, h.makeCookie(token, h.sessionStore.ExpiresAt(sess), r))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"username":          req.Username,
		"expires_at":        h.sessionStore.ExpiresAt(sess).Format(time.RFC3339),
		"session_ttl_seconds": h.sessionStore.TTLSeconds(),
	})
}

// logout handles POST/DELETE /api/auth/logout.
func (h *AuthHandlers) logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil && cookie.Value != "" {
		h.sessionStore.Delete(cookie.Value)
	}

	// Clear cookie (Max-Age=0).
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// session handles GET /api/auth/session.
func (h *AuthHandlers) session(w http.ResponseWriter, r *http.Request) {
	webauthnAvailable := h.credCounter.CountWebAuthnCredentials() > 0

	// If no auth enabled, return auth_required: false.
	if !h.cfg.AnyEnabled() {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"auth_required":       false,
			"webauthn_available":  webauthnAvailable,
			"session_ttl_seconds": h.sessionStore.TTLSeconds(),
		})
		return
	}

	// Check session cookie.
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil && cookie.Value != "" {
		if sess, ok := h.sessionStore.Get(cookie.Value); ok {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"username":            sess.Username,
				"expires_at":          h.sessionStore.ExpiresAt(sess).Format(time.RFC3339),
				"auth_required":       true,
				"webauthn_available":  webauthnAvailable,
				"session_ttl_seconds": h.sessionStore.TTLSeconds(),
			})
			return
		}
	}

	// No valid session.
	writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
		"error":               "unauthorized",
		"auth_required":       true,
		"webauthn_available":  webauthnAvailable,
		"session_ttl_seconds": h.sessionStore.TTLSeconds(),
	})
}

// makeCookie creates the session cookie with appropriate Secure flag.
func (h *AuthHandlers) makeCookie(token string, expiresAt time.Time, r *http.Request) *http.Cookie {
	secure := false
	switch h.secureCookies {
	case "true":
		secure = true
	case "false":
		secure = false
	default: // "auto"
		secure = r.Header.Get("X-Forwarded-Proto") == "https" || r.TLS != nil
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

// extractIP extracts the remote IP from the request (F5: keyed by IP, not connID).
func extractIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// writeJSON encodes v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
