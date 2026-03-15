package discovery

import (
	"context"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestNew_InitialState(t *testing.T) {
	m := New("/nonexistent.sock")
	if m.State() != StateDisconnected {
		t.Errorf("initial state: got %q, want %q", m.State(), StateDisconnected)
	}
}

func TestRun_TransitionsToConnecting(t *testing.T) {
	m := New("/nonexistent.sock")
	m.PollInterval = 50 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go m.Run(ctx)

	// Give goroutine a moment to start.
	time.Sleep(100 * time.Millisecond)

	if m.State() != StateConnecting {
		t.Errorf("state after Run start: got %q, want %q", m.State(), StateConnecting)
	}
}

func TestRun_ConnectsWhenSocketAvailable(t *testing.T) {
	// Start a Unix socket server that responds to /api/health.
	// Use /tmp directly — macOS has a 104-byte limit on Unix socket paths
	// and t.TempDir() names are often too long.
	socketPath := "/tmp/wg-sockd-test-" + t.Name() + ".sock"
	os.Remove(socketPath)
	t.Cleanup(func() { os.Remove(socketPath) })

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(listener)
	defer srv.Close()

	m := New(socketPath)
	m.PollInterval = 50 * time.Millisecond
	m.HealthInterval = 50 * time.Millisecond
	m.HealthTimeout = 1 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go m.Run(ctx)

	// Wait for connection.
	deadline := time.After(1 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for connected state, got %q", m.State())
		default:
			if m.State() == StateConnected {
				return // success
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func TestRun_ReconnectsOnHealthFailure(t *testing.T) {
	socketPath := "/tmp/wg-sockd-test-" + t.Name() + ".sock"
	os.Remove(socketPath)
	t.Cleanup(func() { os.Remove(socketPath) })

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(listener)

	m := New(socketPath)
	m.PollInterval = 50 * time.Millisecond
	m.HealthInterval = 50 * time.Millisecond
	m.HealthTimeout = 500 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go m.Run(ctx)

	// Wait for connected.
	waitForState(t, m, StateConnected, 2*time.Second)

	// Kill the server → health check will fail → should go to connecting.
	srv.Close()
	listener.Close()
	os.Remove(socketPath)

	// Wait for reconnecting.
	waitForState(t, m, StateConnecting, 2*time.Second)
}

func TestSocketExists_RegularFile(t *testing.T) {
	// A regular file should NOT be detected as a socket.
	f := t.TempDir() + "/not-a-socket"
	os.WriteFile(f, []byte("hello"), 0644)

	m := New(f)
	if m.socketExists() {
		t.Error("regular file was detected as socket")
	}
}

func TestSocketExists_MissingFile(t *testing.T) {
	m := New("/nonexistent/path/to/socket")
	if m.socketExists() {
		t.Error("missing file was detected as socket")
	}
}

func waitForState(t *testing.T, m *Manager, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for state %q, got %q", want, m.State())
		default:
			if m.State() == want {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

