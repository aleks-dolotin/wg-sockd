package confwriter

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestDebouncedWriter_CoalesceRapidWrites(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "wg0.conf")

	// Write initial interface section.
	os.WriteFile(confPath, []byte("[Interface]\nListenPort = 51820\n"), 0600)

	sw := NewSharedWriter()
	var writeCount atomic.Int32

	peers := []PeerConf{{PublicKey: "testkey1", AllowedIPs: "10.0.0.1/32", FriendlyName: "peer1"}}
	dw := NewDebouncedWriter(sw, confPath, 100*time.Millisecond, func() []PeerConf {
		writeCount.Add(1)
		return peers
	})
	defer dw.Close()

	// Fire 5 notifications within 50ms — should coalesce into 1 write.
	for i := 0; i < 5; i++ {
		dw.Notify()
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for debounce window to fire.
	time.Sleep(200 * time.Millisecond)

	count := writeCount.Load()
	if count != 1 {
		t.Errorf("expected 1 write call, got %d", count)
	}
}

func TestDebouncedWriter_SeparateWindowsSeparateWrites(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "wg0.conf")
	os.WriteFile(confPath, []byte("[Interface]\nListenPort = 51820\n"), 0600)

	sw := NewSharedWriter()
	var writeCount atomic.Int32

	peers := []PeerConf{{PublicKey: "testkey1", AllowedIPs: "10.0.0.1/32"}}
	dw := NewDebouncedWriter(sw, confPath, 50*time.Millisecond, func() []PeerConf {
		writeCount.Add(1)
		return peers
	})
	defer dw.Close()

	// First notification.
	dw.Notify()
	time.Sleep(100 * time.Millisecond) // Wait for first write.

	// Second notification (after first debounce completed).
	dw.Notify()
	time.Sleep(100 * time.Millisecond) // Wait for second write.

	count := writeCount.Load()
	if count != 2 {
		t.Errorf("expected 2 write calls, got %d", count)
	}
}

func TestDebouncedWriter_DirectWriteBypassesDebounce(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "wg0.conf")
	os.WriteFile(confPath, []byte("[Interface]\nListenPort = 51820\n"), 0600)

	sw := NewSharedWriter()
	var notifyWriteCount atomic.Int32

	peers := []PeerConf{{PublicKey: "testkey1", AllowedIPs: "10.0.0.1/32"}}
	dw := NewDebouncedWriter(sw, confPath, 100*time.Millisecond, func() []PeerConf {
		notifyWriteCount.Add(1)
		return peers
	})
	defer dw.Close()

	// Notify (would trigger debounced write).
	dw.Notify()

	// Immediately call DirectWrite — should cancel the pending debounce.
	batchPeers := []PeerConf{
		{PublicKey: "batch1", AllowedIPs: "10.0.0.2/32"},
		{PublicKey: "batch2", AllowedIPs: "10.0.0.3/32"},
	}
	if err := dw.DirectWrite(batchPeers); err != nil {
		t.Fatalf("DirectWrite: %v", err)
	}

	// Wait longer than debounce window — notify callback should NOT fire.
	time.Sleep(200 * time.Millisecond)

	if notifyWriteCount.Load() != 0 {
		t.Errorf("expected 0 debounced writes after DirectWrite, got %d", notifyWriteCount.Load())
	}

	// Verify the conf file contains batch peers.
	data, _ := os.ReadFile(confPath)
	content := string(data)
	if !contains(content, "batch1") || !contains(content, "batch2") {
		t.Errorf("conf file should contain batch peers, got:\n%s", content)
	}
}

func TestDebouncedWriter_FlushOnShutdown(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "wg0.conf")
	os.WriteFile(confPath, []byte("[Interface]\nListenPort = 51820\n"), 0600)

	sw := NewSharedWriter()
	var writeCount atomic.Int32

	peers := []PeerConf{{PublicKey: "shutdown-peer", AllowedIPs: "10.0.0.5/32", FriendlyName: "shutdown"}}
	dw := NewDebouncedWriter(sw, confPath, 5*time.Second, func() []PeerConf {
		writeCount.Add(1)
		return peers
	})

	// Notify but don't wait for debounce.
	dw.Notify()

	// Close immediately — should flush the pending write.
	dw.Close()

	if writeCount.Load() != 1 {
		t.Errorf("expected 1 write on shutdown flush, got %d", writeCount.Load())
	}

	data, _ := os.ReadFile(confPath)
	if !contains(string(data), "shutdown-peer") {
		t.Error("conf file should contain the pending peer after shutdown flush")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

