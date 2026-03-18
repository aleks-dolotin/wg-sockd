package proxy

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// mockState implements StateProvider for testing.
type mockState struct{ state string }

func (m *mockState) State() string { return m.state }

// --- path.Clean tests (RT-4 SSRF prevention) ---

func TestPathClean_TraversalBlocked(t *testing.T) {
	webDir := t.TempDir()
	os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>ok</html>"), 0644)

	// Create a secret file OUTSIDE webDir.
	secretDir := t.TempDir()
	secretFile := filepath.Join(secretDir, "secret.txt")
	os.WriteFile(secretFile, []byte("TOP SECRET"), 0644)

	h := NewHandler("/nonexistent.sock", webDir, &mockState{state: "connected"}, VersionInfo{})

	// Attempt directory traversal.
	traversalPaths := []string{
		"/../../../etc/passwd",
		"/../../" + secretFile,
		"/../secret.txt",
		"/./../../etc/shadow",
	}

	for _, tp := range traversalPaths {
		req := httptest.NewRequest("GET", tp, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		// After path.Clean, these all resolve to paths that should NOT leak secrets.
		body := w.Body.String()
		if body == "TOP SECRET" {
			t.Errorf("path traversal succeeded for %q — leaked secret content", tp)
		}
	}
}

func TestPathClean_NormalizesDoubleDots(t *testing.T) {
	webDir := t.TempDir()
	os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>spa</html>"), 0644)
	subDir := filepath.Join(webDir, "sub")
	os.MkdirAll(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "page.html"), []byte("<html>page</html>"), 0644)

	h := NewHandler("/nonexistent.sock", webDir, &mockState{state: "connected"}, VersionInfo{})

	// path.Clean("/sub/../sub/page.html") → "/sub/page.html"
	req := httptest.NewRequest("GET", "/sub/../sub/page.html", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("normalized path should serve file, got %d", w.Code)
	}
	if !contains(w.Body.String(), "page") {
		t.Error("expected page content after path normalization")
	}
}

// --- /api/ prefix forwarding tests ---

func TestOnlyAPIPrefixForwarded(t *testing.T) {
	webDir := t.TempDir()
	os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>spa</html>"), 0644)

	h := NewHandler("/nonexistent.sock", webDir, &mockState{state: "connected"}, VersionInfo{})

	tests := []struct {
		path      string
		wantProxy bool // true = expect 502 (socket unreachable), false = expect 200 (SPA)
	}{
		{"/api/health", true},       // proxied
		{"/api/peers", true},        // proxied
		{"/api/peers/1", true},      // proxied
		{"/not-api/health", false},  // NOT proxied → SPA fallback
		{"/apikeys", false},         // NOT proxied → /apikeys is not /api/
		{"/favicon.ico", false},     // NOT proxied → SPA fallback (index.html)
	}

	for _, tt := range tests {
		req := httptest.NewRequest("GET", tt.path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if tt.wantProxy {
			// Should attempt to proxy → 502 because socket doesn't exist.
			if w.Code != http.StatusBadGateway {
				t.Errorf("path %q: got %d, want %d (should be proxied)", tt.path, w.Code, http.StatusBadGateway)
			}
		} else {
			// Should NOT proxy → serve SPA (200).
			if w.Code != http.StatusOK {
				t.Errorf("path %q: got %d, want %d (should NOT be proxied)", tt.path, w.Code, http.StatusOK)
			}
		}
	}
}

// --- /ui/status endpoint ---

func TestStatusEndpoint(t *testing.T) {
	webDir := t.TempDir()
	os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>spa</html>"), 0644)

	tests := []struct {
		state string
	}{
		{"connected"},
		{"connecting"},
		{"disconnected"},
	}

	for _, tt := range tests {
		h := NewHandler("/nonexistent.sock", webDir, &mockState{state: tt.state}, VersionInfo{Version: "test", Commit: "abc123"})

		req := httptest.NewRequest("GET", "/ui/status", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("state %q: got %d, want 200", tt.state, w.Code)
		}

		body := w.Body.String()
		if !contains(body, tt.state) {
			t.Errorf("state %q not found in response body: %s", tt.state, body)
		}
	}
}

// --- SPA fallback ---

func TestSPAFallback_ServesIndexForUnknownPaths(t *testing.T) {
	webDir := t.TempDir()
	os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>spa-root</html>"), 0644)

	h := NewHandler("/nonexistent.sock", webDir, &mockState{state: "connected"}, VersionInfo{})

	// Non-existent file → should get index.html (SPA routing).
	req := httptest.NewRequest("GET", "/peers/123", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("SPA fallback: got %d, want 200", w.Code)
	}
	if !contains(w.Body.String(), "spa-root") {
		t.Error("SPA fallback did not serve index.html")
	}
}

func TestSPAFallback_ServesActualFiles(t *testing.T) {
	webDir := t.TempDir()
	os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>spa-root</html>"), 0644)

	// Create a static asset.
	assetsDir := filepath.Join(webDir, "assets")
	os.MkdirAll(assetsDir, 0755)
	os.WriteFile(filepath.Join(assetsDir, "app.js"), []byte("console.log('hello')"), 0644)

	h := NewHandler("/nonexistent.sock", webDir, &mockState{state: "connected"}, VersionInfo{})

	req := httptest.NewRequest("GET", "/assets/app.js", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("static file: got %d, want 200", w.Code)
	}
	if !contains(w.Body.String(), "hello") {
		t.Error("static file content not served")
	}
}

func TestSPAFallback_NoIndexHTML(t *testing.T) {
	webDir := t.TempDir() // empty — no index.html

	h := NewHandler("/nonexistent.sock", webDir, &mockState{state: "connected"}, VersionInfo{})

	req := httptest.NewRequest("GET", "/anything", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("no index.html: got %d, want 404", w.Code)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

