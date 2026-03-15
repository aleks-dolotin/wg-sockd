// Package sockmon monitors the Unix socket file and re-creates it if
// deleted or replaced by a non-socket file (FM-3: socket self-healing).
package sockmon

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"syscall"
	"time"
)

// Monitor watches a Unix socket file and re-creates the listener when
// the socket disappears or becomes invalid.
type Monitor struct {
	socketPath string
	handler    http.Handler
	connCtx    func(ctx context.Context, c net.Conn) context.Context

	mu       sync.Mutex
	listener net.Listener
	server   *http.Server

	// nowFunc and statFunc are injectable for testing.
	nowFunc  func() time.Time
	statFunc func(string) (os.FileInfo, error)
}

// New creates a new socket Monitor.
func New(socketPath string, handler http.Handler, connCtx func(ctx context.Context, c net.Conn) context.Context) *Monitor {
	return &Monitor{
		socketPath: socketPath,
		handler:    handler,
		connCtx:    connCtx,
		nowFunc:    time.Now,
		statFunc:   os.Stat,
	}
}

// Start creates the initial socket and begins serving. Must be called
// before RunMonitor. The listener is started in a background goroutine.
func (m *Monitor) Start() error {
	listener, err := m.createSocket()
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.listener = listener
	m.server = &http.Server{
		Handler:     m.handler,
		ConnContext: m.connCtx,
	}
	srv := m.server
	m.mu.Unlock()

	go func() {
		if err := srv.Serve(listener); err != http.ErrServerClosed {
			log.Printf("ERROR: socket server error: %v", err)
		}
	}()

	log.Printf("Listening on %s", m.socketPath)
	return nil
}

// RunMonitor checks socket health every 5 seconds and re-creates if needed.
// Blocks until ctx is cancelled.
func (m *Monitor) RunMonitor(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.check()
		}
	}
}

func (m *Monitor) check() {
	fi, err := m.statFunc(m.socketPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("WARN: socket missing, re-creating: %s", m.socketPath)
			m.recreate()
			return
		}
		log.Printf("WARN: socket stat error: %v", err)
		return
	}

	// Verify it's actually a Unix socket.
	if fi.Mode().Type()&os.ModeSocket == 0 {
		log.Printf("WARN: %s is not a socket (mode: %s), re-creating", m.socketPath, fi.Mode())
		m.recreate()
		return
	}

	// Check permissions (AC #3): expected 0660 from umask 0117.
	perm := fi.Mode().Perm()
	expected := os.FileMode(0660)
	if perm != expected {
		log.Printf("WARN: socket permissions changed: got %04o, expected %04o", perm, expected)
	}
}

func (m *Monitor) recreate() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Close existing server — this causes the old Serve goroutine to return.
	if m.server != nil {
		m.server.Close()
	}

	// Remove stale socket file if any remnant exists.
	os.Remove(m.socketPath)

	// Create new socket.
	listener, err := m.createSocket()
	if err != nil {
		log.Printf("ERROR: socket re-creation failed: %v", err)
		return
	}

	m.listener = listener
	m.server = &http.Server{
		Handler:     m.handler,
		ConnContext: m.connCtx,
	}

	srv := m.server
	go func() {
		log.Printf("INFO: socket re-created successfully: %s", m.socketPath)
		if err := srv.Serve(listener); err != http.ErrServerClosed {
			log.Printf("ERROR: server error after socket re-creation: %v", err)
		}
	}()
}

func (m *Monitor) createSocket() (net.Listener, error) {
	oldMask := syscall.Umask(0117)
	listener, err := net.Listen("unix", m.socketPath)
	syscall.Umask(oldMask)
	return listener, err
}

// Shutdown gracefully shuts down the current server.
func (m *Monitor) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.server != nil {
		return m.server.Shutdown(ctx)
	}
	return nil
}

