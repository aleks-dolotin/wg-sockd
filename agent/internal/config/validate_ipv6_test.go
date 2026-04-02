package config

import (
	"os"
	"testing"
)

func TestValidate_IPv6Prefix_Empty(t *testing.T) {
	cfg := Defaults()
	cfg.ExternalEndpoint = "vpn.example.com:51820"
	cfg.IPv6Prefix = ""
	if err := cfg.Validate(); err != nil {
		t.Errorf("empty IPv6Prefix should be valid: %v", err)
	}
}

func TestValidate_IPv6Prefix_Valid(t *testing.T) {
	cfg := Defaults()
	cfg.ExternalEndpoint = "vpn.example.com:51820"
	cfg.IPv6Prefix = "fd00:ab01::"
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid IPv6Prefix should pass: %v", err)
	}
}

func TestValidate_IPv6Prefix_ValidFD(t *testing.T) {
	cfg := Defaults()
	cfg.ExternalEndpoint = "vpn.example.com:51820"
	cfg.IPv6Prefix = "fd12:3456:789a::"
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid ULA prefix should pass: %v", err)
	}
}

func TestValidate_IPv6Prefix_MissingDoubleColon(t *testing.T) {
	cfg := Defaults()
	cfg.ExternalEndpoint = "vpn.example.com:51820"
	cfg.IPv6Prefix = "fd00:wg1"
	if err := cfg.Validate(); err == nil {
		t.Error("IPv6Prefix without :: should fail validation")
	}
}

func TestValidate_IPv6Prefix_Invalid(t *testing.T) {
	cfg := Defaults()
	cfg.ExternalEndpoint = "vpn.example.com:51820"
	cfg.IPv6Prefix = "not-a-prefix::"
	if err := cfg.Validate(); err == nil {
		t.Error("invalid IPv6Prefix should fail validation")
	}
}

func TestApplyEnv_IPv6Prefix(t *testing.T) {
	cfg := Defaults()
	os.Setenv("WG_SOCKD_IPV6_PREFIX", "fd00:ab01::")
	t.Cleanup(func() { os.Unsetenv("WG_SOCKD_IPV6_PREFIX") })

	applied, err := cfg.ApplyEnv()
	if err != nil {
		t.Fatalf("ApplyEnv error: %v", err)
	}
	if cfg.IPv6Prefix != "fd00:ab01::" {
		t.Errorf("IPv6Prefix: got %q, want %q", cfg.IPv6Prefix, "fd00:ab01::")
	}
	if _, ok := applied["WG_SOCKD_IPV6_PREFIX"]; !ok {
		t.Error("WG_SOCKD_IPV6_PREFIX not in applied map")
	}
}

func TestLoadConfig_IPv6Prefix(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	os.WriteFile(path, []byte("ipv6_prefix: \"fd00:ab01::\"\nexternal_endpoint: \"vpn.example.com:51820\"\n"), 0644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if cfg.IPv6Prefix != "fd00:ab01::" {
		t.Errorf("IPv6Prefix from YAML: got %q, want %q", cfg.IPv6Prefix, "fd00:ab01::")
	}
}
