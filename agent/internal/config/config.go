// Package config handles agent configuration loading and validation.
package config

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// PeerProfileConfig represents a peer profile defined in config.yaml for seeding.
type PeerProfileConfig struct {
	Name                string   `yaml:"name"`
	AllowedIPs          []string `yaml:"allowed_ips"`
	ExcludeIPs          []string `yaml:"exclude_ips"`
	Description         string   `yaml:"description"`
	Endpoint            string   `yaml:"endpoint"`
	PersistentKeepalive *int     `yaml:"persistent_keepalive"`
	ClientDNS           string   `yaml:"client_dns"`
	ClientMTU           *int     `yaml:"client_mtu"`
	ClientAllowedIPs    string   `yaml:"client_allowed_ips"`
	UsePresharedKey     bool     `yaml:"use_preshared_key"`
}

// PeerDefaultsConfig holds global defaults for client config generation (4-level cascade).
type PeerDefaultsConfig struct {
	ClientDNS                  string `yaml:"client_dns"`
	ClientMTU                  int    `yaml:"client_mtu"`
	ClientPersistentKeepalive  int    `yaml:"client_persistent_keepalive"`
	ClientAllowedIPs           string `yaml:"client_allowed_ips"`
}

// BasicAuthConfig holds username/password authentication settings.
type BasicAuthConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Username     string `yaml:"username"`
	PasswordHash string `yaml:"password_hash"`
}

// TokenAuthConfig holds bearer token authentication settings.
type TokenAuthConfig struct {
	Enabled bool   `yaml:"enabled"`
	Token   string `yaml:"token"`
}

// WebAuthnConfig holds passkey/WebAuthn authentication settings.
type WebAuthnConfig struct {
	Enabled     bool   `yaml:"enabled"`
	DisplayName string `yaml:"display_name"`
	Origin      string `yaml:"origin"`
}

// AuthConfig holds all authentication configuration.
type AuthConfig struct {
	Basic          BasicAuthConfig  `yaml:"basic"`
	Token          TokenAuthConfig  `yaml:"token"`
	WebAuthn       WebAuthnConfig   `yaml:"webauthn"`
	SessionTTL     time.Duration    `yaml:"session_ttl"`
	SkipUnixSocket bool             `yaml:"skip_unix_socket"`
	SecureCookies  string           `yaml:"secure_cookies"`
	MaxSessions    int              `yaml:"max_sessions"`
}

// AnyEnabled returns true if at least one authentication method is enabled.
func (a *AuthConfig) AnyEnabled() bool {
	return a.Basic.Enabled || a.Token.Enabled
}

// Config holds all agent configuration.
type Config struct {
	Interface          string               `yaml:"interface"`
	SocketPath         string               `yaml:"socket_path"`
	DBPath             string               `yaml:"db_path"`
	ConfPath           string               `yaml:"conf_path"`
	ListenAddr         string               `yaml:"listen_addr"`
	ExternalEndpoint   string               `yaml:"external_endpoint"`
	PeerLimit          int                  `yaml:"peer_limit"`
	ReconcileInterval  time.Duration        `yaml:"reconcile_interval"`
	PeerProfiles       []PeerProfileConfig  `yaml:"peer_profiles"`
	PeerDefaults       PeerDefaultsConfig   `yaml:"peer_defaults"`
	RateLimit          int                  `yaml:"rate_limit"`
	ServeUI            bool                 `yaml:"serve_ui"`
	UIListen           string               `yaml:"ui_listen"`
	Auth               AuthConfig           `yaml:"auth"`
}

// Defaults returns a Config populated with default values.
func Defaults() *Config {
	return &Config{
		Interface:          "wg0",
		SocketPath:         "/run/wg-sockd/wg-sockd.sock",
		DBPath:             "/var/lib/wg-sockd/wg-sockd.db",
		ConfPath:           "/etc/wireguard/wg0.conf",
		ListenAddr:         "",
		PeerLimit:          250,
		ReconcileInterval:  30 * time.Second,
		RateLimit:          10,
		ServeUI:            false,
		UIListen:           "127.0.0.1:8080",
		Auth: AuthConfig{
			SessionTTL:     15 * time.Minute,
			SkipUnixSocket: true,
			SecureCookies:  "auto",
			MaxSessions:    100,
		},
		PeerDefaults: PeerDefaultsConfig{
			ClientPersistentKeepalive: 25, // current hardcoded value preserved as default
		},
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
		{"WG_SOCKD_AUTH_BASIC_ENABLED", func(v string) error {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("invalid boolean value %q", v)
			}
			c.Auth.Basic.Enabled = b
			return nil
		}},
		{"WG_SOCKD_AUTH_BASIC_USERNAME", func(v string) error { c.Auth.Basic.Username = v; return nil }},
		{"WG_SOCKD_AUTH_BASIC_PASSWORD_HASH", func(v string) error { c.Auth.Basic.PasswordHash = v; return nil }},
		{"WG_SOCKD_AUTH_TOKEN_ENABLED", func(v string) error {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("invalid boolean value %q", v)
			}
			c.Auth.Token.Enabled = b
			return nil
		}},
		{"WG_SOCKD_AUTH_TOKEN", func(v string) error { c.Auth.Token.Token = v; return nil }},
		{"WG_SOCKD_AUTH_SESSION_TTL", func(v string) error {
			d, err := time.ParseDuration(v)
			if err != nil {
				return fmt.Errorf("invalid duration value %q", v)
			}
			c.Auth.SessionTTL = d
			return nil
		}},
		{"WG_SOCKD_AUTH_SKIP_UNIX_SOCKET", func(v string) error {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("invalid boolean value %q", v)
			}
			c.Auth.SkipUnixSocket = b
			return nil
		}},
		{"WG_SOCKD_AUTH_SECURE_COOKIES", func(v string) error { c.Auth.SecureCookies = v; return nil }},
		{"WG_SOCKD_AUTH_MAX_SESSIONS", func(v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("invalid integer value %q", v)
			}
			c.Auth.MaxSessions = n
			return nil
		}},
		{"WG_SOCKD_AUTH_WEBAUTHN_ENABLED", func(v string) error {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("invalid boolean value %q", v)
			}
			c.Auth.WebAuthn.Enabled = b
			return nil
		}},
		{"WG_SOCKD_AUTH_WEBAUTHN_ORIGIN", func(v string) error { c.Auth.WebAuthn.Origin = v; return nil }},
		{"WG_SOCKD_CLIENT_DNS", func(v string) error { c.PeerDefaults.ClientDNS = v; return nil }},
		{"WG_SOCKD_CLIENT_MTU", func(v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("invalid integer value %q", v)
			}
			c.PeerDefaults.ClientMTU = n
			return nil
		}},
		{"WG_SOCKD_CLIENT_PERSISTENT_KEEPALIVE", func(v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("invalid integer value %q", v)
			}
			c.PeerDefaults.ClientPersistentKeepalive = n
			return nil
		}},
		{"WG_SOCKD_CLIENT_ALLOWED_IPS", func(v string) error { c.PeerDefaults.ClientAllowedIPs = v; return nil }},
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

// ValidateAuth checks auth configuration for fatal errors and warnings.
// Returns an error if the config is invalid (caller should fatal).
// Logs warnings for non-fatal issues.
func (c *Config) ValidateAuth() error {
	a := &c.Auth

	// Validate session TTL bounds.
	if a.SessionTTL < 5*time.Minute || a.SessionTTL > 720*time.Hour {
		return fmt.Errorf("auth.session_ttl must be between 5m and 720h, got %s", a.SessionTTL)
	}

	// Basic auth: enabled but no password hash → fatal.
	if a.Basic.Enabled && a.Basic.PasswordHash == "" {
		return fmt.Errorf("auth.basic.enabled=true but password_hash is empty — generate one with: wg-sockd-ctl hash-password")
	}

	// Token auth: enabled but no token → fatal.
	if a.Token.Enabled && a.Token.Token == "" {
		return fmt.Errorf("auth.token.enabled=true but token is empty")
	}

	// WebAuthn: enabled but no origin → fatal.
	if a.WebAuthn.Enabled && a.WebAuthn.Origin == "" {
		return fmt.Errorf("auth.webauthn.enabled=true but origin is empty — set auth.webauthn.origin to the public URL (e.g. https://vpn.example.com)")
	}

	// Warnings (non-fatal).
	if a.Token.Enabled && len(a.Token.Token) < 32 {
		log.Printf("WARN: auth.token.token is shorter than 32 characters — consider using a longer token")
	}

	if !a.AnyEnabled() {
		log.Printf("WARN: No authentication methods configured — API is unprotected. Set auth.basic.enabled or auth.token.enabled in config.")
	}

	return nil
}
