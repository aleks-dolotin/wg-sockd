// Package config handles agent configuration loading and validation.
package config

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// PeerProfileConfig represents a peer profile defined in config.yaml for seeding.
type PeerProfileConfig struct {
	Name        string   `yaml:"name"`
	DisplayName string   `yaml:"display_name"`
	AllowedIPs  []string `yaml:"allowed_ips"`
	ExcludeIPs  []string `yaml:"exclude_ips"`
	Description string   `yaml:"description"`
}

// Config holds all agent configuration.
type Config struct {
	Interface          string               `yaml:"interface"`
	SocketPath         string               `yaml:"socket_path"`
	DBPath             string               `yaml:"db_path"`
	ConfPath           string               `yaml:"conf_path"`
	ListenAddr         string               `yaml:"listen_addr"`
	ExternalEndpoint   string               `yaml:"external_endpoint"`
	AutoApproveUnknown bool                 `yaml:"auto_approve_unknown"`
	PeerLimit          int                  `yaml:"peer_limit"`
	ReconcileInterval  time.Duration        `yaml:"reconcile_interval"`
	PeerProfiles       []PeerProfileConfig  `yaml:"peer_profiles"`
	RateLimit          int                  `yaml:"rate_limit"`
	ServeUI            bool                 `yaml:"serve_ui"`
	UIListen           string               `yaml:"ui_listen"`
}

// Defaults returns a Config populated with default values.
func Defaults() *Config {
	return &Config{
		Interface:          "wg0",
		SocketPath:         "/run/wg-sockd/wg-sockd.sock",
		DBPath:             "/var/lib/wg-sockd/wg-sockd.db",
		ConfPath:           "/etc/wireguard/wg0.conf",
		ListenAddr:         "",
		AutoApproveUnknown: false,
		PeerLimit:          250,
		ReconcileInterval:  30 * time.Second,
		RateLimit:          10,
		ServeUI:            false,
		UIListen:           "127.0.0.1:8080",
	}
}

// LoadConfig reads configuration from a YAML file at the given path.
// If the file does not exist, defaults are returned without error.
// If the file exists but is malformed, an error is returned.
func LoadConfig(path string) (*Config, error) {
	cfg := Defaults()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	return cfg, nil
}

// ApplyFlags registers CLI flags and applies them over the loaded config.
// Call flag.Parse() before calling this method.
func (c *Config) ApplyFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.Interface, "interface", c.Interface, "WireGuard interface name")
	fs.StringVar(&c.SocketPath, "socket-path", c.SocketPath, "Unix socket path")
	fs.StringVar(&c.DBPath, "db-path", c.DBPath, "SQLite database path")
	fs.StringVar(&c.ConfPath, "conf-path", c.ConfPath, "WireGuard config file path")
	fs.StringVar(&c.ListenAddr, "listen-addr", c.ListenAddr, "HTTP listen address (for standalone UI mode)")
	fs.BoolVar(&c.AutoApproveUnknown, "auto-approve-unknown", c.AutoApproveUnknown, "Auto-approve unknown peers found in kernel")
	fs.BoolVar(&c.ServeUI, "serve-ui", c.ServeUI, "serve embedded UI on TCP (requires embed_ui build tag)")
	fs.StringVar(&c.UIListen, "ui-listen", c.UIListen, "TCP listen address for UI mode")
}

// ApplyEnv reads environment variables and overrides matching Config fields in-place.
// Returns a map keyed by ENV VAR NAME for each override applied, and an error if
// any env var has an invalid value. Bool validation uses strconv.ParseBool
// (accepts "1"/"t"/"TRUE"/"true"/"True"/"0"/"f"/"FALSE"/"false"/"False").
func (c *Config) ApplyEnv() (map[string]string, error) {
	applied := make(map[string]string)

	type envMapping struct {
		envVar string
		apply  func(string) error
	}

	mappings := []envMapping{
		{"WG_SOCKD_INTERFACE", func(v string) error { c.Interface = v; return nil }},
		{"WG_SOCKD_SOCKET_PATH", func(v string) error { c.SocketPath = v; return nil }},
		{"WG_SOCKD_DB_PATH", func(v string) error { c.DBPath = v; return nil }},
		{"WG_SOCKD_CONF_PATH", func(v string) error { c.ConfPath = v; return nil }},
		{"WG_SOCKD_LISTEN_ADDR", func(v string) error { c.ListenAddr = v; return nil }},
		{"WG_SOCKD_AUTO_APPROVE_UNKNOWN", func(v string) error {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("invalid boolean value %q", v)
			}
			c.AutoApproveUnknown = b
			return nil
		}},
		{"WG_SOCKD_PEER_LIMIT", func(v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("invalid integer value %q", v)
			}
			c.PeerLimit = n
			return nil
		}},
		{"WG_SOCKD_RATE_LIMIT", func(v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("invalid integer value %q", v)
			}
			c.RateLimit = n
			return nil
		}},
		{"WG_SOCKD_SERVE_UI", func(v string) error {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("invalid boolean value %q", v)
			}
			c.ServeUI = b
			return nil
		}},
		{"WG_SOCKD_UI_LISTEN", func(v string) error { c.UIListen = v; return nil }},
	}

	for _, m := range mappings {
		v, ok := os.LookupEnv(m.envVar)
		if !ok {
			continue
		}
		if err := m.apply(v); err != nil {
			return applied, fmt.Errorf("env %s: %w", m.envVar, err)
		}
		applied[m.envVar] = v
	}

	return applied, nil
}
