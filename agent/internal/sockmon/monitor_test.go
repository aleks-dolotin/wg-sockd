package sockmon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// shortSockPath creates a Unix socket path short enough for macOS (max 104 chars).
func shortSockPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return fmt.Sprintf("%s/s.sock", dir)
}

func TestMonitor_StartAndServe(t *testing.T) {
	sockPath := shortSockPath(t)

	m := New(sockPath, okHandler(), nil)
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Shutdown(context.Background())

	// Verify socket file exists.
	fi, err := os.Stat(sockPath)
	if err != nil {
		t.Fatalf("socket should exist: %v", err)
	}
	if fi.Mode().Type()&os.ModeSocket == 0 {
		t.Errorf("expected socket file, got mode: %s", fi.Mode())
	}

	// Verify HTTP requests work through the socket.
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", sockPath)
			},
		},
		Timeout: 2 * time.Second,
	}
	resp, err := client.Get("http://localhost/api/health")
	if err != nil {
		t.Fatalf("GET over socket: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestMonitor_DetectDeletedSocket(t *testing.T) {
	sockPath := shortSockPath(t)

	m := New(sockPath, okHandler(), nil)
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Shutdown(context.Background())

	// Delete the socket file.
	if err := os.Remove(sockPath); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Verify it's gone.
	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Fatal("socket should be deleted")
	}

	// Run monitor check — should re-create.
	m.check()

	// Give the goroutine a moment to start serving.
	time.Sleep(100 * time.Millisecond)

	// Verify socket file re-created.
	fi, err := os.Stat(sockPath)
	if err != nil {
		t.Fatalf("socket should be re-created: %v", err)
	}
	if fi.Mode().Type()&os.ModeSocket == 0 {
		t.Errorf("expected socket file after re-creation, got mode: %s", fi.Mode())
	}

	// Verify HTTP requests work on the new socket.
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", sockPath)
			},
		},
		Timeout: 2 * time.Second,
	}
	resp, err := client.Get("http://localhost/api/health")
	if err != nil {
		t.Fatalf("GET after re-creation: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after re-creation, got %d", resp.StatusCode)
	}
}

func TestMonitor_HealthySocketNoAction(t *testing.T) {
	sockPath := shortSockPath(t)

	m := New(sockPath, okHandler(), nil)
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Shutdown(context.Background())

	// Get initial inode.
	fi1, _ := os.Stat(sockPath)

	// Run check — socket is healthy, no action should be taken.
	m.check()

	time.Sleep(50 * time.Millisecond)

	// Verify same socket file (not re-created).
	fi2, err := os.Stat(sockPath)
	if err != nil {
		t.Fatalf("socket should still exist: %v", err)
	}
	// Compare modification times — should be unchanged.
	if !fi1.ModTime().Equal(fi2.ModTime()) {
		t.Error("socket was unexpectedly re-created when healthy")
	}
}

func TestMonitor_ContextCancellation(t *testing.T) {
	sockPath := shortSockPath(t)

	m := New(sockPath, okHandler(), nil)
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Shutdown(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.RunMonitor(ctx)
		close(done)
	}()

	// Cancel context — monitor should stop.
	cancel()

	select {
	case <-done:
		// Success — monitor stopped.
	case <-time.After(2 * time.Second):
		t.Error("monitor did not stop after context cancellation")
	}
}

// Suppress unused import warning for httptest.
var _ = httptest.NewRecorder

