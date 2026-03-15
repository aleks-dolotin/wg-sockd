package confwriter

import "sync"

// SharedWriter serialises all wg0.conf writes behind a single mutex so that
// concurrent calls from API handlers and the Reconciler cannot interleave.
// A single instance must be created in main and injected into both Handlers
// and Reconciler — that is the contract that guarantees mutual exclusion.
type SharedWriter struct {
	mu sync.Mutex
}

// NewSharedWriter returns a new SharedWriter ready for use.
func NewSharedWriter() *SharedWriter {
	return &SharedWriter{}
}

// WriteConf acquires the write lock and delegates to the package-level WriteConf.
// All callers must use this method instead of calling WriteConf directly so that
// concurrent file writes are serialised.
func (w *SharedWriter) WriteConf(path string, peers []PeerConf) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return WriteConf(path, peers)
}

