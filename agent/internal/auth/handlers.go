package auth

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/config"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/ctxkeys"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/storage"
)

// loginRequest is the JSON body for POST /api/auth/login.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// AuthHandlers provides HTTP handlers for authentication endpoints.
type AuthHandlers struct {
	cfg           *config.AuthConfig
	sessionStore  *SessionStore
	basicVerifier *BasicAuthVerifier // nil if basic auth not enabled
	rateLimiter   *LoginRateLimiter
	credCounter   WebAuthnCredentialCounter
	secureCookies string // "auto", "true", "false"

	// WebAuthn (Layer 2) — all nil when WebAuthn is disabled.
	webauthnLib    *webauthn.WebAuthn
	challengeStore *ChallengeStore
	credStore      *storage.DB
	webauthnCfg    *config.WebAuthnConfig
}

// NewAuthHandlers creates auth handlers.
// basicVerifier may be nil if basic auth is not enabled.
// webauthnLib, challengeStore, credStore, webauthnCfg may be nil when WebAuthn is disabled.
func NewAuthHandlers(
	cfg *config.AuthConfig,
	sessionStore *SessionStore,
	basicVerifier *BasicAuthVerifier,
	rateLimiter *LoginRateLimiter,
	credCounter WebAuthnCredentialCounter,
	webauthnLib *webauthn.WebAuthn,
	challengeStore *ChallengeStore,
	credStore *storage.DB,
	webauthnCfg *config.WebAuthnConfig,
) *AuthHandlers {
	return &AuthHandlers{
		cfg:            cfg,
		sessionStore:   sessionStore,
		basicVerifier:  basicVerifier,
		rateLimiter:    rateLimiter,
		credCounter:    credCounter,
		secureCookies:  cfg.SecureCookies,
		webauthnLib:    webauthnLib,
		challengeStore: challengeStore,
		credStore:      credStore,
		webauthnCfg:    webauthnCfg,
	}
}

// Handler returns an http.Handler (ServeMux) with all auth endpoints registered.
func (h *AuthHandlers) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/login", h.login)
	mux.HandleFunc("POST /api/auth/logout", h.logout)
	mux.HandleFunc("DELETE /api/auth/logout", h.logout) // F8: workaround for SameSite=Lax
	mux.HandleFunc("GET /api/auth/session", h.session)

	// WebAuthn endpoints — bypasses auth middleware by design (ADR-3).
	// Protected endpoints call requireSession() internally.
	mux.HandleFunc("POST /api/auth/webauthn/register/begin", h.webauthnRegisterBegin)
	mux.HandleFunc("POST /api/auth/webauthn/register/finish", h.webauthnRegisterFinish)
	mux.HandleFunc("POST /api/auth/webauthn/login/begin", h.webauthnLoginBegin)
	mux.HandleFunc("POST /api/auth/webauthn/login/finish", h.webauthnLoginFinish)
	mux.HandleFunc("GET /api/auth/webauthn/credentials", h.webauthnListCredentials)
	mux.HandleFunc("DELETE /api/auth/webauthn/credentials/{id}", h.webauthnDeleteCredential)

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
	webauthnEnabled := h.webauthnCfg != nil && h.webauthnCfg.Enabled
	webauthnAvailable := webauthnEnabled && h.credCounter.CountWebAuthnCredentials() > 0

	// If no auth enabled, return auth_required: false.
	if !h.cfg.AnyEnabled() {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"auth_required":       false,
			"webauthn_available":  webauthnAvailable,
			"webauthn_enabled":    webauthnEnabled,
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
				"webauthn_enabled":    webauthnEnabled,
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
		"webauthn_enabled":    webauthnEnabled,
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

// ---------------------------------------------------------------------------
// WebAuthn helpers
// ---------------------------------------------------------------------------

// requireSession validates the session cookie from the request.
// On failure it writes a 401 JSON response and returns false.
// Caller pattern: sess, ok := h.requireSession(w, r); if !ok { return }
func (h *AuthHandlers) requireSession(w http.ResponseWriter, r *http.Request) (*Session, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error":   "unauthorized",
			"message": "valid session required",
		})
		return nil, false
	}
	sess, ok := h.sessionStore.Get(cookie.Value)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error":   "unauthorized",
			"message": "session expired or invalid",
		})
		return nil, false
	}
	return sess, true
}

// webauthnDisabledGuard returns false (and writes 400) when WebAuthn is not enabled.
func (h *AuthHandlers) webauthnDisabledGuard(w http.ResponseWriter) bool {
	if h.webauthnCfg == nil || !h.webauthnCfg.Enabled || h.webauthnLib == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "webauthn_disabled",
			"message": "WebAuthn is not enabled in server config",
		})
		return false
	}
	return true
}

// sanitizeFriendlyName enforces max 64 characters and strips HTML tags.
func sanitizeFriendlyName(name string) string {
	// Strip simple HTML tags.
	for strings.Contains(name, "<") {
		start := strings.Index(name, "<")
		end := strings.Index(name[start:], ">")
		if end == -1 {
			break
		}
		name = name[:start] + name[start+end+1:]
	}
	// Truncate to 64 runes.
	runes := []rune(name)
	if len(runes) > 64 {
		name = string(runes[:64])
	}
	// Also ensure valid UTF-8.
	if !utf8.ValidString(name) {
		name = strings.ToValidUTF8(name, "")
	}
	return strings.TrimSpace(name)
}

// userAgentFriendlyName derives a short name from the User-Agent header.
func userAgentFriendlyName(r *http.Request) string {
	ua := r.Header.Get("User-Agent")
	if ua == "" {
		return "Passkey"
	}
	// Very simple heuristic — keep first 48 chars.
	runes := []rune(ua)
	if len(runes) > 48 {
		return string(runes[:48])
	}
	return ua
}

// loadAdminUser loads the admin WebAuthnUser from config and SQLite.
func (h *AuthHandlers) loadAdminUser() (*WebAuthnUser, error) {
	creds, err := h.credStore.ListCredentials()
	if err != nil {
		return nil, fmt.Errorf("loading credentials: %w", err)
	}
	return NewWebAuthnUser(
		h.cfg.Basic.Username,
		h.webauthnCfg.DisplayName,
		h.webauthnCfg.Origin,
		creds,
	), nil
}

// ---------------------------------------------------------------------------
// WebAuthn Registration
// ---------------------------------------------------------------------------

// webauthnRegisterBegin handles POST /api/auth/webauthn/register/begin.
// Requires an active session. Returns publicKey options + challenge token.
func (h *AuthHandlers) webauthnRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if !h.webauthnDisabledGuard(w) {
		return
	}
	if _, ok := h.requireSession(w, r); !ok {
		return
	}

	user, err := h.loadAdminUser()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error":   "internal_error",
			"message": "failed to load user credentials",
		})
		return
	}

	creation, sessionData, err := h.webauthnLib.BeginRegistration(user,
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			UserVerification:   protocol.VerificationPreferred,
		}),
	)
	if err != nil {
		log.Printf("ERROR: webauthn BeginRegistration: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error":   "webauthn_error",
			"message": "failed to begin registration",
		})
		return
	}

	token := h.challengeStore.Store(sessionData)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"publicKey": creation.Response,
		"token":     token,
	})
}

// webauthnRegisterFinish handles POST /api/auth/webauthn/register/finish.
func (h *AuthHandlers) webauthnRegisterFinish(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("PANIC in webauthnRegisterFinish: %v", rec)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error":   "internal_error",
				"message": "unexpected error processing credential",
			})
		}
	}()

	if !h.webauthnDisabledGuard(w) {
		return
	}
	if _, ok := h.requireSession(w, r); !ok {
		return
	}

	var req struct {
		Token        string `json:"token"`
		FriendlyName string `json:"friendly_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "invalid_request",
			"message": "invalid JSON",
		})
		return
	}

	sessionData, ok := h.challengeStore.Get(req.Token)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "invalid_token",
			"message": "challenge token not found or expired",
		})
		return
	}

	user, err := h.loadAdminUser()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "internal_error", "message": "failed to load user",
		})
		return
	}

	credential, err := h.webauthnLib.FinishRegistration(user, *sessionData, r)
	if err != nil {
		log.Printf("ERROR: webauthn FinishRegistration: %v", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "registration_failed",
			"message": err.Error(),
		})
		return
	}

	// Sanitize friendly name; fall back to user-agent.
	name := sanitizeFriendlyName(req.FriendlyName)
	if name == "" {
		name = sanitizeFriendlyName(userAgentFriendlyName(r))
	}

	// Encode transports as JSON array string.
	transportJSON := encodeTransports(credential.Transport)

	// Encode credential ID as base64url for storage.
	credID := base64EncodeID(credential.ID)

	// Flags byte.
	var flagsByte uint8
	if credential.Flags.UserPresent {
		flagsByte |= 0x01
	}
	if credential.Flags.UserVerified {
		flagsByte |= 0x04
	}
	if credential.Flags.BackupEligible {
		flagsByte |= 0x08
	}
	if credential.Flags.BackupState {
		flagsByte |= 0x10
	}

	// AAGUID bytes.
	aaguid := credential.Authenticator.AAGUID

	if err := h.credStore.InsertCredential(
		credID,
		credential.PublicKey,
		credential.AttestationType,
		aaguid,
		transportJSON,
		flagsByte,
		credential.Authenticator.SignCount,
		name,
	); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error":   "credential_exists",
				"message": "this credential is already registered",
			})
			return
		}
		log.Printf("ERROR: inserting webauthn credential: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "db_error", "message": "failed to save credential",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":        "ok",
		"credential_id": credID,
		"friendly_name": name,
	})
}

// ---------------------------------------------------------------------------
// WebAuthn Login
// ---------------------------------------------------------------------------

// webauthnLoginBegin handles POST /api/auth/webauthn/login/begin.
// Rate limited. Returns publicKey assertion options + challenge token.
func (h *AuthHandlers) webauthnLoginBegin(w http.ResponseWriter, r *http.Request) {
	if !h.webauthnDisabledGuard(w) {
		return
	}

	// Rate limiting by IP (same as password login).
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

	// Require at least one credential registered.
	count, err := h.credStore.CountCredentials()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "db_error", "message": "failed to check credentials",
		})
		return
	}
	if count == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "no_credentials",
			"message": "no passkeys registered",
		})
		return
	}

	assertion, sessionData, err := h.webauthnLib.BeginDiscoverableLogin()
	if err != nil {
		log.Printf("ERROR: webauthn BeginDiscoverableLogin: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error":   "webauthn_error",
			"message": "failed to begin login",
		})
		return
	}

	token := h.challengeStore.Store(sessionData)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"publicKey": assertion.Response,
		"token":     token,
	})
}

// webauthnLoginFinish handles POST /api/auth/webauthn/login/finish.
func (h *AuthHandlers) webauthnLoginFinish(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("PANIC in webauthnLoginFinish: %v", rec)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error":   "internal_error",
				"message": "unexpected error processing credential",
			})
		}
	}()

	if !h.webauthnDisabledGuard(w) {
		return
	}

	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid_request", "message": "invalid JSON",
		})
		return
	}

	sessionData, ok := h.challengeStore.Get(req.Token)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "invalid_token",
			"message": "challenge token not found or expired",
		})
		return
	}

	// discoverableUserHandler — for single-admin, any valid userHandle maps to admin.
	discoverableUserHandler := func(rawID, userHandle []byte) (webauthn.User, error) {
		user, err := h.loadAdminUser()
		if err != nil {
			return nil, err
		}
		return user, nil
	}

	credential, err := h.webauthnLib.FinishDiscoverableLogin(discoverableUserHandler, *sessionData, r)
	if err != nil {
		log.Printf("ERROR: webauthn FinishPasskeyLogin: %v", err)
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error":   "authentication_failed",
			"message": "passkey verification failed",
		})
		return
	}

	// Sign count check — log WARNING but do not block (Security #8).
	credID := base64EncodeID(credential.ID)
	stored, dbErr := h.credStore.GetCredentialByID(credID)
	if dbErr == nil {
		if credential.Authenticator.SignCount <= stored.SignCount && credential.Authenticator.SignCount != 0 {
			log.Printf("WARNING: WebAuthn sign count anomaly for credential %q: received=%d stored=%d (possible cloning)",
				credID, credential.Authenticator.SignCount, stored.SignCount)
		}
		// Update sign_count and last_used_at.
		if err := h.credStore.UpdateSignCount(credID, credential.Authenticator.SignCount, time.Now()); err != nil {
			log.Printf("WARNING: failed to update sign count for %q: %v", credID, err)
		}
	}

	// Create session (same mechanism as password login).
	token, err := h.sessionStore.Create(h.cfg.Basic.Username)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "internal_error", "message": "failed to create session",
		})
		return
	}
	sess, _ := h.sessionStore.Get(token)
	http.SetCookie(w, h.makeCookie(token, h.sessionStore.ExpiresAt(sess), r))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"username":            h.cfg.Basic.Username,
		"expires_at":          h.sessionStore.ExpiresAt(sess).Format(time.RFC3339),
		"session_ttl_seconds": h.sessionStore.TTLSeconds(),
	})
}

// ---------------------------------------------------------------------------
// WebAuthn Credential Management (Settings)
// ---------------------------------------------------------------------------

// webauthnListCredentials handles GET /api/auth/webauthn/credentials.
func (h *AuthHandlers) webauthnListCredentials(w http.ResponseWriter, r *http.Request) {
	if !h.webauthnDisabledGuard(w) {
		return
	}
	if _, ok := h.requireSession(w, r); !ok {
		return
	}

	creds, err := h.credStore.ListCredentials()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "db_error", "message": "failed to list credentials",
		})
		return
	}

	type credResponse struct {
		ID           string  `json:"id"`
		FriendlyName string  `json:"friendly_name"`
		CreatedAt    string  `json:"created_at"`
		LastUsedAt   *string `json:"last_used_at"`
	}
	result := make([]credResponse, 0, len(creds))
	for _, c := range creds {
		var lastUsed *string
		if c.LastUsedAt != nil {
			s := c.LastUsedAt.Format(time.RFC3339)
			lastUsed = &s
		}
		result = append(result, credResponse{
			ID:           c.ID,
			FriendlyName: c.FriendlyName,
			CreatedAt:    c.CreatedAt.Format(time.RFC3339),
			LastUsedAt:   lastUsed,
		})
	}
	writeJSON(w, http.StatusOK, result)
}

// webauthnDeleteCredential handles DELETE /api/auth/webauthn/credentials/{id}.
func (h *AuthHandlers) webauthnDeleteCredential(w http.ResponseWriter, r *http.Request) {
	if !h.webauthnDisabledGuard(w) {
		return
	}
	if _, ok := h.requireSession(w, r); !ok {
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "missing_id", "message": "credential id is required",
		})
		return
	}

	if err := h.credStore.DeleteCredential(id); err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "not_found", "message": "credential not found",
			})
			return
		}
		log.Printf("ERROR: deleting webauthn credential %q: %v", id, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "db_error", "message": "failed to delete credential",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// encodeTransports encodes a slice of transport hints as a JSON array string.
func encodeTransports(transports []protocol.AuthenticatorTransport) string {
	if len(transports) == 0 {
		return "[]"
	}
	parts := make([]string, len(transports))
	for i, t := range transports {
		parts[i] = fmt.Sprintf("%q", string(t))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// base64EncodeID encodes a raw credential ID as base64url without padding.
func base64EncodeID(id []byte) string {
	return base64.RawURLEncoding.EncodeToString(id)
}


// writeJSON encodes v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
