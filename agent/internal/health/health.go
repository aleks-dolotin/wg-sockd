// Package health provides the health check logic for wg-sockd.
package health

import (
	"os"
	"path/filepath"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/api"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/storage"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/wireguard"
)

// Checker performs health checks on system dependencies.
type Checker struct {
	wgClient      wireguard.WireGuardClient
	db            *storage.DB
	interfaceName string
	confPath      string       // WireGuard conf path for writability check
	recoveredFrom string       // set if DB was recovered from backup/conf at startup
	diskChecker   *DiskChecker // optional — set via SetDiskChecker
}

// NewChecker creates a new health Checker.
func NewChecker(wgClient wireguard.WireGuardClient, db *storage.DB, interfaceName string) *Checker {
	return &Checker{
		wgClient:      wgClient,
		db:            db,
		interfaceName: interfaceName,
	}
}

// SetConfPath sets the WireGuard config path for writability checks.
func (c *Checker) SetConfPath(path string) {
	c.confPath = path
}

// SetRecoveredFrom records that the DB was recovered from a backup source.
func (c *Checker) SetRecoveredFrom(source string) {
	c.recoveredFrom = source
}

// SetDiskChecker attaches a disk checker for disk_ok health reporting.
func (c *Checker) SetDiskChecker(dc *DiskChecker) {
	c.diskChecker = dc
}

// Check performs health checks and returns the overall status.
func (c *Checker) Check() api.HealthResponse {
	resp := api.HealthResponse{
		Status:    "ok",
		WireGuard: "ok",
		SQLite:    "ok",
	}

	// Check WireGuard.
	if _, err := c.wgClient.GetDevice(c.interfaceName); err != nil {
		resp.WireGuard = "error"
	}

	// Check SQLite with quick_check (faster than integrity_check).
	var result string
	if err := c.db.Conn().QueryRow("PRAGMA quick_check").Scan(&result); err != nil || result != "ok" {
		resp.SQLite = "error"
	}

	// Set recovery info if applicable.
	if c.recoveredFrom != "" {
		resp.SQLiteRecoveredFrom = c.recoveredFrom
	}

	// Check disk space if disk checker is configured.
	if c.diskChecker != nil {
		diskOK := !c.diskChecker.IsReadOnly()
		resp.DiskOK = &diskOK
	}

	// Check conf writability — can the agent write wg0.conf.tmp in the conf directory?
	if c.confPath != "" {
		confDir := filepath.Dir(c.confPath)
		tmpFile := filepath.Join(confDir, ".wg-sockd-health-check")
		writable := false
		if f, err := os.Create(tmpFile); err == nil {
			f.Close()
			os.Remove(tmpFile)
			writable = true
		}
		resp.ConfWritable = &writable
	}

	// Determine overall status.
	wgOK := resp.WireGuard == "ok"
	dbOK := resp.SQLite == "ok"
	diskOK := c.diskChecker == nil || !c.diskChecker.IsReadOnly()
	confOK := c.confPath == "" || (resp.ConfWritable != nil && *resp.ConfWritable)

	switch {
	case wgOK && dbOK && diskOK && confOK:
		resp.Status = "ok"
	case !wgOK && !dbOK:
		resp.Status = "unavailable"
	default:
		resp.Status = "degraded"
	}

	return resp
}
