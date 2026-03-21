package auth

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/config"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/ctxkeys"
)

// ---------------------------------------------------------------------------
// Fix #5: makeCookie — trust X-Forwarded-Proto only via Unix socket
// ---------------------------------------------------------------------------

func newMakeCookieHandler(secureCookies string) *AuthHandlers {
	cfg := &config.AuthConfig{
		SecureCookies: secureCookies,
		SessionTTL:    15 * time.Minute,
		MaxSessions:   100,
	}
	return &AuthHandlers{
		cfg:           cfg,
		secureCookies: secureCookies,
	}
}

func TestMakeCookie_AutoMode_UnixSocket_WithHTTPSHeader(t *testing.T) {
	h := newMakeCookieHandler("auto")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	ctx := context.WithValue(req.Context(), ctxkeys.IsUnixSocketKey{}, true)
	req = req.WithContext(ctx)

	cookie := h.makeCookie("tok", time.Now().Add(time.Hour), req)
	if !cookie.Secure {
		t.Error("expected Secure=true when Unix socket + X-Forwarded-Proto: https")
	}
}

func TestMakeCookie_AutoMode_UnixSocket_NoHeader(t *testing.T) {
	h := newMakeCookieHandler("auto")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(req.Context(), ctxkeys.IsUnixSocketKey{}, true)
	req = req.WithContext(ctx)

	cookie := h.makeCookie("tok", time.Now().Add(time.Hour), req)
	if cookie.Secure {
		t.Error("expected Secure=false when Unix socket without X-Forwarded-Proto header")
	}
}

func TestMakeCookie_AutoMode_TCP_SpoofedHeader(t *testing.T) {
	h := newMakeCookieHandler("auto")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	// No IsUnixSocketKey in context — this is a TCP connection.

	cookie := h.makeCookie("tok", time.Now().Add(time.Hour), req)
	if cookie.Secure {
		t.Error("expected Secure=false when TCP + spoofed X-Forwarded-Proto (header must be ignored)")
	}
}

func TestMakeCookie_AutoMode_TCP_RealTLS(t *testing.T) {
	h := newMakeCookieHandler("auto")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.TLS = &tls.ConnectionState{} // simulate real TLS connection
	// No IsUnixSocketKey — TCP connection.

	cookie := h.makeCookie("tok", time.Now().Add(time.Hour), req)
	if !cookie.Secure {
		t.Error("expected Secure=true when TCP with actual TLS")
	}
}

func TestMakeCookie_ExplicitTrue(t *testing.T) {
	h := newMakeCookieHandler("true")
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	cookie := h.makeCookie("tok", time.Now().Add(time.Hour), req)
	if !cookie.Secure {
		t.Error("expected Secure=true when secure_cookies=true")
	}
}

func TestMakeCookie_ExplicitFalse(t *testing.T) {
	h := newMakeCookieHandler("false")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.TLS = &tls.ConnectionState{}

	cookie := h.makeCookie("tok", time.Now().Add(time.Hour), req)
	if cookie.Secure {
		t.Error("expected Secure=false when secure_cookies=false, even with TLS")
	}
}

// ---------------------------------------------------------------------------
// Fix #10: sanitizeFriendlyName — strip all angle brackets
// ---------------------------------------------------------------------------

func TestSanitizeFriendlyName_CleanText(t *testing.T) {
	got := sanitizeFriendlyName("My YubiKey 5")
	if got != "My YubiKey 5" {
		t.Errorf("got %q, want %q", got, "My YubiKey 5")
	}
}

func TestSanitizeFriendlyName_BalancedTags(t *testing.T) {
	got := sanitizeFriendlyName("<script>alert(1)</script>")
	// New behavior: all < and > stripped, not just matched tags.
	want := "scriptalert(1)/script"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSanitizeFriendlyName_UnbalancedTag(t *testing.T) {
	// This was the bug — old code left "<img src=x onerror=alert(1)" partially intact.
	got := sanitizeFriendlyName("<img src=x onerror=alert(1)")
	want := "img src=x onerror=alert(1)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSanitizeFriendlyName_DoubleAngleBrackets(t *testing.T) {
	got := sanitizeFriendlyName("<<script>alert(1)</script>")
	// All angle brackets removed unconditionally.
	want := "scriptalert(1)/script"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSanitizeFriendlyName_OnlyBrackets(t *testing.T) {
	got := sanitizeFriendlyName("<><><>")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestSanitizeFriendlyName_Truncate64(t *testing.T) {
	long := ""
	for i := 0; i < 100; i++ {
		long += "a"
	}
	got := sanitizeFriendlyName(long)
	if len([]rune(got)) != 64 {
		t.Errorf("expected 64 runes, got %d", len([]rune(got)))
	}
}

func TestSanitizeFriendlyName_Unicode(t *testing.T) {
	got := sanitizeFriendlyName("Мой ключ <тест>")
	if got != "Мой ключ тест" {
		t.Errorf("got %q, want %q", got, "Мой ключ тест")
	}
}

func TestSanitizeFriendlyName_Whitespace(t *testing.T) {
	got := sanitizeFriendlyName("  spaced  ")
	if got != "spaced" {
		t.Errorf("got %q, want %q", got, "spaced")
	}
}
