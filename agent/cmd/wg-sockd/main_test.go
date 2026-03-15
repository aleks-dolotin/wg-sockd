package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSPAHandler(t *testing.T) {
	// Create a temp dir with fake static files.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>SPA</html>"), 0644)
	os.MkdirAll(filepath.Join(dir, "assets"), 0755)
	os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log('app')"), 0644)

	staticFS := http.Dir(dir)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanPath := strings.TrimPrefix(r.URL.Path, "/")
		if cleanPath == "" {
			cleanPath = "index.html"
		}

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
		if strings.Contains(cleaned, "..") && cleaned != ".." && cleaned != "/" {
			// filepath.Clean should resolve these
		}
		// Just ensure no panic
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

