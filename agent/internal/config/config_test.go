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
	if cfg.PeerLimit != 250 {
		t.Errorf("PeerLimit: got %d, want 250", cfg.PeerLimit)
	}
	if cfg.ReconcileInterval != 30*time.Second {
		t.Errorf("ReconcileInterval: got %v, want 30s", cfg.ReconcileInterval)
	}
	if cfg.ManagementListen != ":8090" {
		t.Errorf("ManagementListen: got %q, want %q", cfg.ManagementListen, ":8090")
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
management_listen: ":9090"
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
	if cfg.PeerLimit != 100 {
		t.Errorf("PeerLimit: got %d, want 100", cfg.PeerLimit)
	}
	if cfg.ReconcileInterval != 60*time.Second {
		t.Errorf("ReconcileInterval: got %v, want 1m0s", cfg.ReconcileInterval)
	}
	if cfg.ManagementListen != ":9090" {
		t.Errorf("ManagementListen: got %q, want %q", cfg.ManagementListen, ":9090")
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
    allowed_ips: ["0.0.0.0/0", "::/0"]
    description: "Route all traffic through VPN"
  - name: nas-only
    allowed_ips: ["10.0.0.0/24"]
    description: "Access NAS network only"
  - name: internet-only
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
	// Non-overridden fields keep defaults
	if cfg.DBPath != "/var/lib/wg-sockd/wg-sockd.db" {
		t.Errorf("DBPath should keep default, got %q", cfg.DBPath)
	}
}

// --- New tests for ServeUI, UIListen, and ApplyEnv ---

func TestDefaults_ServeUIAndUIListen(t *testing.T) {
	cfg := Defaults()
	if cfg.ServeUI != false {
		t.Errorf("ServeUI: got %v, want false", cfg.ServeUI)
	}
	if cfg.UIListen != "127.0.0.1:8080" {
		t.Errorf("UIListen: got %q, want %q", cfg.UIListen, "127.0.0.1:8080")
	}
}

func TestLoadConfig_ServeUIAndUIListen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	yaml := `interface: wg0
serve_ui: true
ui_listen: "0.0.0.0:9090"
`
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ServeUI != true {
		t.Errorf("ServeUI: got %v, want true", cfg.ServeUI)
	}
	if cfg.UIListen != "0.0.0.0:9090" {
		t.Errorf("UIListen: got %q, want %q", cfg.UIListen, "0.0.0.0:9090")
	}
}

func TestApplyEnv_StringOverrides(t *testing.T) {
	t.Setenv("WG_SOCKD_INTERFACE", "wg2")
	t.Setenv("WG_SOCKD_UI_LISTEN", "10.0.0.1:9090")

	cfg := Defaults()
	applied, err := cfg.ApplyEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Interface != "wg2" {
		t.Errorf("Interface: got %q, want %q", cfg.Interface, "wg2")
	}
	if cfg.UIListen != "10.0.0.1:9090" {
		t.Errorf("UIListen: got %q, want %q", cfg.UIListen, "10.0.0.1:9090")
	}
	if len(applied) != 2 {
		t.Errorf("applied map length: got %d, want 2", len(applied))
	}
}

func TestApplyEnv_BoolOverride(t *testing.T) {
	t.Setenv("WG_SOCKD_SERVE_UI", "true")

	cfg := Defaults()
	applied, err := cfg.ApplyEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ServeUI != true {
		t.Errorf("ServeUI: got %v, want true", cfg.ServeUI)
	}
	if _, ok := applied["WG_SOCKD_SERVE_UI"]; !ok {
		t.Error("expected WG_SOCKD_SERVE_UI in applied map")
	}
}

func TestApplyEnv_BoolParseBoolVariants(t *testing.T) {
	// strconv.ParseBool accepts "1", "t", "T", "TRUE", "true", "True", etc.
	variants := []string{"1", "t", "T", "TRUE", "true", "True"}
	for _, v := range variants {
		t.Run(v, func(t *testing.T) {
			t.Setenv("WG_SOCKD_SERVE_UI", v)
			cfg := Defaults()
			_, err := cfg.ApplyEnv()
			if err != nil {
				t.Fatalf("ParseBool should accept %q: %v", v, err)
			}
			if cfg.ServeUI != true {
				t.Errorf("ServeUI: got %v, want true for input %q", cfg.ServeUI, v)
			}
		})
	}
}

func TestApplyEnv_InvalidBool(t *testing.T) {
	t.Setenv("WG_SOCKD_SERVE_UI", "invalid")

	cfg := Defaults()
	_, err := cfg.ApplyEnv()
	if err == nil {
		t.Fatal("expected error for invalid boolean, got nil")
	}
}

func TestApplyEnv_ManagementListen(t *testing.T) {
	t.Setenv("WG_SOCKD_MANAGEMENT_LISTEN", ":9999")
	cfg := Defaults()
	applied, err := cfg.ApplyEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ManagementListen != ":9999" {
		t.Errorf("ManagementListen: got %q, want %q", cfg.ManagementListen, ":9999")
	}
	if _, ok := applied["WG_SOCKD_MANAGEMENT_LISTEN"]; !ok {
		t.Error("expected WG_SOCKD_MANAGEMENT_LISTEN in applied map")
	}
}

func TestApplyEnv_ManagementListenDisable(t *testing.T) {
	t.Setenv("WG_SOCKD_MANAGEMENT_LISTEN", "")
	cfg := Defaults()
	_, err := cfg.ApplyEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ManagementListen != "" {
		t.Errorf("ManagementListen: got %q, want empty (disabled)", cfg.ManagementListen)
	}
}

func TestApplyEnv_NoEnvVars(t *testing.T) {
	// Ensure none of our env vars are set.
	for _, key := range []string{"WG_SOCKD_INTERFACE", "WG_SOCKD_SOCKET_PATH", "WG_SOCKD_SERVE_UI", "WG_SOCKD_UI_LISTEN", "WG_SOCKD_MANAGEMENT_LISTEN"} {
		os.Unsetenv(key)
	}

	cfg := Defaults()
	applied, err := cfg.ApplyEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("applied map should be empty, got %v", applied)
	}
	// Config should remain at defaults.
	if cfg.Interface != "wg0" {
		t.Errorf("Interface should be default, got %q", cfg.Interface)
	}
}

func TestApplyEnv_MapKeyFormat(t *testing.T) {
	// AC-47: map keys should be ENV VAR NAMES, not config field names.
	t.Setenv("WG_SOCKD_INTERFACE", "wg2")

	cfg := Defaults()
	applied, err := cfg.ApplyEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := applied["WG_SOCKD_INTERFACE"]; !ok {
		t.Errorf("map key should be 'WG_SOCKD_INTERFACE', got keys: %v", applied)
	}
	// Ensure it's not keyed by the config field name.
	if _, ok := applied["interface"]; ok {
		t.Error("map should not use config field name as key")
	}
}
