package health

import (
	"fmt"
	"log"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// DefaultDiskThreshold is the minimum free disk space before read-only mode (10 MB).
	DefaultDiskThreshold uint64 = 10 * 1024 * 1024
)

// DiskChecker monitors available disk space and sets a read-only flag
// when free space drops below the configured threshold (FM-6).
type DiskChecker struct {
	path      string
	threshold uint64
	readOnly  atomic.Bool

	// statfsFunc is injectable for testing.
	statfsFunc func(path string) (available uint64, err error)
}

// NewDiskChecker creates a disk space monitor for the given path.
func NewDiskChecker(path string, threshold uint64) *DiskChecker {
	if threshold == 0 {
		threshold = DefaultDiskThreshold
	}
	dc := &DiskChecker{
		path:      path,
		threshold: threshold,
	}
	dc.statfsFunc = dc.defaultStatfs
	return dc
}

// IsReadOnly returns true if disk space is below threshold.
func (dc *DiskChecker) IsReadOnly() bool {
	return dc.readOnly.Load()
}

// RunLoop periodically checks disk space every 30s.
// Blocks until ctx is done. Should be called as a goroutine.
func (dc *DiskChecker) RunLoop(done <-chan struct{}) {
	// Initial check.
	dc.check()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Logging ticker — warn every 60s while in read-only mode.
	warnTicker := time.NewTicker(60 * time.Second)
	defer warnTicker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			dc.check()
		case <-warnTicker.C:
			if dc.readOnly.Load() {
				log.Println("WARN: disk full, write operations disabled")
			}
		}
	}
}

func (dc *DiskChecker) check() {
	available, err := dc.statfsFunc(dc.path)
	if err != nil {
		log.Printf("WARN: disk space check failed: %v", err)
		return
	}

	wasReadOnly := dc.readOnly.Load()

	if available < dc.threshold {
		if !wasReadOnly {
			log.Printf("WARN: disk full — switching to read-only mode (available: %d bytes, threshold: %d bytes)", available, dc.threshold)
		}
		dc.readOnly.Store(true)
	} else {
		if wasReadOnly {
			log.Println("INFO: disk space available — resuming normal operations")
		}
		dc.readOnly.Store(false)
	}
}

func (dc *DiskChecker) defaultStatfs(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	if stat.Bsize <= 0 {
		return 0, fmt.Errorf("invalid block size: %d", stat.Bsize)
	}
	// Available blocks * block size.
	return stat.Bavail * uint64(stat.Bsize), nil
}

