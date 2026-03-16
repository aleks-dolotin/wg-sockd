package config

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()

	if cfg.Interface != "wg0" {
		t.Errorf("Interface: got %q, want %q", cfg.Interface, "wg0")
	}
	if cfg.SocketPath != "/run/wg-sockd/wg-sockd.sock" {
		t.Errorf("SocketPath: got %q, want %q", cfg.SocketPath, "/run/wg-sockd/wg-sockd.sock")
	}
	if cfg.DBPath != "/var/lib/wg-sockd/wg-sockd.db" {
		t.Errorf("DBPath: got %q, want %q", cfg.DBPath, "/var/lib/wg-sockd/wg-sockd.db")
	}
	if cfg.ConfPath != "/etc/wireguard/wg0.conf" {
		t.Errorf("ConfPath: got %q, want %q", cfg.ConfPath, "/etc/wireguard/wg0.conf")
	}
	if cfg.ListenAddr != "" {
		t.Errorf("ListenAddr: got %q, want %q", cfg.ListenAddr, "")
	}
	if cfg.AutoApproveUnknown != false {
		t.Errorf("AutoApproveUnknown: got %v, want false", cfg.AutoApproveUnknown)
	}
	if cfg.PeerLimit != 250 {
		t.Errorf("PeerLimit: got %d, want 250", cfg.PeerLimit)
	}
	if cfg.ReconcileInterval != 30*time.Second {
		t.Errorf("ReconcileInterval: got %v, want 30s", cfg.ReconcileInterval)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	cfg, err := LoadConfig("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}
	// Should return defaults
	if cfg.Interface != "wg0" {
		t.Errorf("expected defaults, got Interface=%q", cfg.Interface)
	}
}

func TestLoadConfig_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	yaml := `interface: wg1
socket_path: /tmp/test.sock
db_path: /tmp/test.db
conf_path: /tmp/wg1.conf
listen_addr: "127.0.0.1:8080"
auto_approve_unknown: true
peer_limit: 100
reconcile_interval: 60s
`
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Interface != "wg1" {
		t.Errorf("Interface: got %q, want %q", cfg.Interface, "wg1")
	}
	if cfg.SocketPath != "/tmp/test.sock" {
		t.Errorf("SocketPath: got %q, want %q", cfg.SocketPath, "/tmp/test.sock")
	}
	if cfg.DBPath != "/tmp/test.db" {
		t.Errorf("DBPath: got %q, want %q", cfg.DBPath, "/tmp/test.db")
	}
	if cfg.ConfPath != "/tmp/wg1.conf" {
		t.Errorf("ConfPath: got %q, want %q", cfg.ConfPath, "/tmp/wg1.conf")
	}
	if cfg.ListenAddr != "127.0.0.1:8080" {
		t.Errorf("ListenAddr: got %q, want %q", cfg.ListenAddr, "127.0.0.1:8080")
	}
	if cfg.AutoApproveUnknown != true {
		t.Errorf("AutoApproveUnknown: got %v, want true", cfg.AutoApproveUnknown)
	}
	if cfg.PeerLimit != 100 {
		t.Errorf("PeerLimit: got %d, want 100", cfg.PeerLimit)
	}
	if cfg.ReconcileInterval != 60*time.Second {
		t.Errorf("ReconcileInterval: got %v, want 1m0s", cfg.ReconcileInterval)
	}
}

func TestLoadConfig_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(path, []byte("{{{{not yaml"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
}

func TestLoadConfig_PartialYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	yaml := `interface: wg2
`
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Overridden value
	if cfg.Interface != "wg2" {
		t.Errorf("Interface: got %q, want %q", cfg.Interface, "wg2")
	}
	// Defaults preserved for unspecified fields
	if cfg.SocketPath != "/run/wg-sockd/wg-sockd.sock" {
		t.Errorf("SocketPath should keep default, got %q", cfg.SocketPath)
	}
	if cfg.PeerLimit != 250 {
		t.Errorf("PeerLimit should keep default, got %d", cfg.PeerLimit)
	}
}

func TestLoadConfig_PeerProfiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	yaml := `interface: wg0
peer_profiles:
  - name: full-access
    display_name: "Full Access"
    allowed_ips: ["0.0.0.0/0", "::/0"]
    description: "Route all traffic through VPN"
  - name: nas-only
    display_name: "NAS Only"
    allowed_ips: ["10.0.0.0/24"]
    description: "Access NAS network only"
  - name: internet-only
    display_name: "Internet Only"
    allowed_ips: ["0.0.0.0/0", "::/0"]
    exclude_ips: ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"]
    description: "Internet through VPN, no local access"
`
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.PeerProfiles) != 3 {
		t.Fatalf("expected 3 peer profiles, got %d", len(cfg.PeerProfiles))
	}

	// Check first profile.
	p := cfg.PeerProfiles[0]
	if p.Name != "full-access" {
		t.Errorf("first profile Name: got %q, want %q", p.Name, "full-access")
	}
	if p.DisplayName != "Full Access" {
		t.Errorf("first profile DisplayName: got %q, want %q", p.DisplayName, "Full Access")
	}
	if len(p.AllowedIPs) != 2 {
		t.Errorf("first profile AllowedIPs: got %v, want 2 entries", p.AllowedIPs)
	}

	// Check internet-only has exclude_ips.
	pio := cfg.PeerProfiles[2]
	if len(pio.ExcludeIPs) != 3 {
		t.Errorf("internet-only ExcludeIPs: got %v, want 3 entries", pio.ExcludeIPs)
	}
	if pio.ExcludeIPs[0] != "10.0.0.0/8" {
		t.Errorf("internet-only ExcludeIPs[0]: got %q, want %q", pio.ExcludeIPs[0], "10.0.0.0/8")
	}
}

func TestLoadConfig_NoPeerProfiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	yaml := `interface: wg0
`
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.PeerProfiles != nil {
		t.Errorf("expected nil PeerProfiles, got %v", cfg.PeerProfiles)
	}
}

func TestApplyFlags(t *testing.T) {
	cfg := Defaults()

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg.ApplyFlags(fs)

	err := fs.Parse([]string{
		"--interface", "wg3",
		"--socket-path", "/tmp/override.sock",
		"--auto-approve-unknown",
	})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	if cfg.Interface != "wg3" {
		t.Errorf("Interface: got %q, want %q", cfg.Interface, "wg3")
	}
	if cfg.SocketPath != "/tmp/override.sock" {
		t.Errorf("SocketPath: got %q, want %q", cfg.SocketPath, "/tmp/override.sock")
	}
	if cfg.AutoApproveUnknown != true {
		t.Errorf("AutoApproveUnknown: got %v, want true", cfg.AutoApproveUnknown)
	}
	// Non-overridden fields keep defaults
	if cfg.DBPath != "/var/lib/wg-sockd/wg-sockd.db" {
		t.Errorf("DBPath should keep default, got %q", cfg.DBPath)
	}
}
