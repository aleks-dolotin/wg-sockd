// Package config handles agent configuration loading and validation.
package config

import (
	"flag"
	"fmt"
	"os"
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
}
