// Package discovery implements poll-based socket discovery and health-check
// reconnection for the wg-sockd agent Unix socket.
//
// State machine:
//
//	disconnected → connecting (socket file not found yet)
//	connecting   → connected  (socket file found, initial health-check passed)
//	connected    → connecting (health-check failed — agent went away)
package discovery

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

const (
	StateDisconnected = "disconnected"
	StateConnecting   = "connecting"
	StateConnected    = "connected"
)

// Manager tracks the connection state of the agent socket.
type Manager struct {
	socketPath string
	state      atomic.Value // string
	httpClient *http.Client

	// Configurable intervals — exported for testing.
	PollInterval   time.Duration
	HealthInterval time.Duration
	HealthTimeout  time.Duration
}

// New creates a new discovery Manager.
func New(socketPath string) *Manager {
	m := &Manager{
		socketPath:     socketPath,
		PollInterval:   2 * time.Second,
		HealthInterval: 5 * time.Second,
		HealthTimeout:  3 * time.Second,
	}
	m.httpClient = &http.Client{
		Timeout: m.HealthTimeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", m.socketPath)
			},
		},
	}
	m.state.Store(StateDisconnected)
	return m
}

// State returns the current connection state.
func (m *Manager) State() string {
	return m.state.Load().(string)
}

// Run drives the state machine until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) {
	m.state.Store(StateConnecting)
	log.Printf("discovery: waiting for socket %s", m.socketPath)

	for {
		select {
		case <-ctx.Done():
			m.state.Store(StateDisconnected)
			return
		default:
		}

		switch m.State() {
		case StateConnecting:
			if m.socketExists() {
				if m.healthCheck() {
					m.state.Store(StateConnected)
					log.Printf("discovery: connected to agent via %s", m.socketPath)
				}
			}
			m.sleep(ctx, m.PollInterval)

		case StateConnected:
			if !m.healthCheck() {
				m.state.Store(StateConnecting)
				log.Printf("discovery: lost connection to agent, reconnecting...")
			}
			m.sleep(ctx, m.HealthInterval)
		}
	}
}

// socketExists checks if the socket file is present on disk.
func (m *Manager) socketExists() bool {
	info, err := os.Stat(m.socketPath)
	if err != nil {
		return false
	}
	// Ensure it's a socket, not a regular file.
	return info.Mode()&os.ModeSocket != 0
}

// healthCheck performs GET /api/health via the Unix socket.
func (m *Manager) healthCheck() bool {
	resp, err := m.httpClient.Get("http://unix/api/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// sleep waits for the given duration or until ctx is cancelled.
func (m *Manager) sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// String returns a human-readable status for logging.
func (m *Manager) String() string {
	return fmt.Sprintf("discovery[%s socket=%s]", m.State(), m.socketPath)
}

