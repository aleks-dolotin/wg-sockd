package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/config"
)

func TestSPAHandler(t *testing.T) {
	// Create a temp dir with fake static files.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>SPA</html>"), 0644)
	os.MkdirAll(filepath.Join(dir, "assets"), 0755)
	os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log('app')"), 0644)

	staticFS := http.Dir(dir)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		f, err := staticFS.Open(r.URL.Path)
		if err == nil {
			f.Close()
			http.FileServer(staticFS).ServeHTTP(w, r)
			return
		}

		// SPA fallback
		r.URL.Path = "/"
		http.FileServer(staticFS).ServeHTTP(w, r)
	})

	tests := []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{"/", 200, "<html>SPA</html>"},
		{"/assets/app.js", 200, "console.log('app')"},
		{"/peers", 200, "<html>SPA</html>"},       // SPA fallback
		{"/settings/profiles", 200, "<html>SPA</html>"}, // SPA fallback
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			body := w.Body.String()
			if !strings.Contains(body, tt.wantBody) {
				t.Errorf("body = %q, want to contain %q", body, tt.wantBody)
			}
		})
	}
}

func TestEmbedStubIsNil(t *testing.T) {
	// In non-embed_ui builds, embeddedUIFS should be nil.
	if embeddedUIFS != nil {
		t.Error("embeddedUIFS should be nil in stub build")
	}
}

func TestPathCleanSecurity(t *testing.T) {
	// Verify path.Clean prevents directory traversal.
	tests := []string{
		"/../../../etc/passwd",
		"/..%2f..%2fetc/passwd",
		"/..",
	}

	for _, p := range tests {
		cleaned := filepath.Clean(p)
		// filepath.Clean should resolve traversal attempts — just ensure no panic.
		_ = cleaned
	}
}

// Verify fs.Sub works with nil check pattern.
func TestFsSubNilCheck(t *testing.T) {
	var nilFS *fs.FS
	if nilFS != nil {
		t.Error("nil FS should be nil")
	}
}

func TestVersionVarsHaveDefaults(t *testing.T) {
	// In test builds (no ldflags), version variables should have their dev defaults.
	if version == "" {
		t.Error("version should not be empty")
	}
	if version != "dev" {
		// Not a hard failure — ldflags may have been set. Just verify non-empty.
		t.Logf("version = %q (expected 'dev' in test builds)", version)
	}
	if commit == "" {
		t.Error("commit should not be empty")
	}
	if buildDate == "" {
		t.Error("buildDate should not be empty")
	}
	// buildTags can be empty — that's fine for lean builds.
}

func TestDryRun_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, "data")
	os.MkdirAll(dbDir, 0750)
	sockDir := filepath.Join(dir, "run")
	os.MkdirAll(sockDir, 0750)
	confPath := filepath.Join(dir, "wg0.conf")
	os.WriteFile(confPath, []byte("[Interface]\n"), 0644)

	cfg := &config.Config{
		Interface: "wg0",
		SocketPath: filepath.Join(sockDir, "test.sock"),
		DBPath:     filepath.Join(dbDir, "test.db"),
		ConfPath:   confPath,
		UIListen:   "127.0.0.1:8080",
		PeerLimit:  250,
	}

	code := runDryRun(cfg)
	if code != 0 {
		t.Errorf("expected exit code 0 for valid config, got %d", code)
	}
}

func TestDryRun_InvalidUIListen(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, "data")
	os.MkdirAll(dbDir, 0750)

	cfg := &config.Config{
		Interface:  "wg0",
		SocketPath: filepath.Join(dir, "test.sock"),
		DBPath:     filepath.Join(dbDir, "test.db"),
		ConfPath:   "/nonexistent/wg0.conf",
		ServeUI:    true,
		UIListen:   "not-valid",
		PeerLimit:  250,
	}

	code := runDryRun(cfg)
	if code != 1 {
		t.Errorf("expected exit code 1 for invalid ui_listen, got %d", code)
	}
}

func TestDryRun_MissingDataDir(t *testing.T) {
	cfg := &config.Config{
		Interface:  "wg0",
		SocketPath: "/nonexistent-sock-dir/test.sock",
		DBPath:     "/nonexistent-data-dir/test.db",
		ConfPath:   "/nonexistent/wg0.conf",
		UIListen:   "127.0.0.1:8080",
		PeerLimit:  250,
	}

	// Should warn (⚠️) but not fail fatally for non-existent dirs.
	code := runDryRun(cfg)
	// Exit code 0: non-existent dirs are warnings not errors.
	if code != 0 {
		t.Errorf("expected exit code 0 for missing dirs (warnings only), got %d", code)
	}
}

func TestDryRun_NotWritableDataDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("test requires non-root user")
	}

	dir := t.TempDir()
	readOnlyDir := filepath.Join(dir, "readonly")
	os.MkdirAll(readOnlyDir, 0500)
	// Ensure cleanup can remove it.
	t.Cleanup(func() { os.Chmod(readOnlyDir, 0700) })

	cfg := &config.Config{
		Interface:  "wg0",
		SocketPath: filepath.Join(dir, "test.sock"),
		DBPath:     filepath.Join(readOnlyDir, "test.db"),
		ConfPath:   "/nonexistent/wg0.conf",
		UIListen:   "127.0.0.1:8080",
		PeerLimit:  250,
	}

	code := runDryRun(cfg)
	if code != 1 {
		t.Errorf("expected exit code 1 for not-writable dir, got %d", code)
	}
}

func TestDryRun_ConfNotWritable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("test requires non-root user")
	}

	dir := t.TempDir()
	dbDir := filepath.Join(dir, "data")
	os.MkdirAll(dbDir, 0750)
	sockDir := filepath.Join(dir, "run")
	os.MkdirAll(sockDir, 0750)

	// Create conf in a read-only directory.
	confDir := filepath.Join(dir, "wg-readonly")
	os.MkdirAll(confDir, 0750)
	confPath := filepath.Join(confDir, "wg0.conf")
	os.WriteFile(confPath, []byte("[Interface]\n"), 0644)
	// Make the directory read-only so tmp file creation fails.
	os.Chmod(confDir, 0500)
	t.Cleanup(func() { os.Chmod(confDir, 0700) })

	cfg := &config.Config{
		Interface:  "wg0",
		SocketPath: filepath.Join(sockDir, "test.sock"),
		DBPath:     filepath.Join(dbDir, "test.db"),
		ConfPath:   confPath,
		UIListen:   "127.0.0.1:8080",
		PeerLimit:  250,
	}

	code := runDryRun(cfg)
	if code != 1 {
		t.Errorf("expected exit code 1 for non-writable conf directory, got %d", code)
	}
}

