package confwriter

import (
	"log"
	"sync"
	"time"
)

// DebouncedWriter wraps a SharedWriter and coalesces rapid conf writes.
// Mutations within the debounce window (default 100ms) produce a single
// WriteConf call. Thread-safe — multiple goroutines may call Notify().
type DebouncedWriter struct {
	sw       *SharedWriter
	path     string
	getPeers func() []PeerConf // callback to get current peer list
	window   time.Duration

	mu      sync.Mutex
	timer   *time.Timer
	pending bool
	closed  bool
}

// NewDebouncedWriter creates a debounced writer.
// getPeers is called at flush time to get the current peer state.
func NewDebouncedWriter(sw *SharedWriter, path string, window time.Duration, getPeers func() []PeerConf) *DebouncedWriter {
	return &DebouncedWriter{
		sw:       sw,
		path:     path,
		getPeers: getPeers,
		window:   window,
	}
}

// Notify signals that a mutation occurred and a conf write is needed.
// The actual write is deferred until the debounce window elapses with
// no further Notify calls. Returns immediately.
func (d *DebouncedWriter) Notify() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return
	}

	d.pending = true

	// Reset or create the debounce timer.
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(d.window, func() {
		d.flush()
	})
}

// Flush immediately executes any pending write and resets the debounce timer.
// Used by batch operations and graceful shutdown.
func (d *DebouncedWriter) Flush() {
	d.mu.Lock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	hasPending := d.pending
	d.mu.Unlock()

	if hasPending {
		d.flush()
	}
}

func (d *DebouncedWriter) flush() {
	d.mu.Lock()
	if !d.pending {
		d.mu.Unlock()
		return
	}
	d.pending = false
	d.mu.Unlock()

	peers := d.getPeers()
	if err := d.sw.WriteConf(d.path, peers); err != nil {
		log.Printf("ERROR: debounced conf write failed: %v", err)
	}
}

// Close flushes any pending writes and marks the writer as closed.
func (d *DebouncedWriter) Close() {
	d.mu.Lock()
	d.closed = true
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	hasPending := d.pending
	d.mu.Unlock()

	if hasPending {
		log.Println("INFO: flushing pending conf writes at shutdown")
		d.flush()
	}
}

// DirectWrite performs an immediate synchronous write, bypassing debounce.
// Used by batch endpoint and operations that need synchronous confirmation.
// Also resets the debounce timer to prevent a duplicate write.
func (d *DebouncedWriter) DirectWrite(peers []PeerConf) error {
	// Cancel any pending debounced write.
	d.mu.Lock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	d.pending = false
	d.mu.Unlock()

	return d.sw.WriteConf(d.path, peers)
}

