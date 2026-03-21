package config

import (
	"strings"
	"testing"
	"time"
)

func validBaseConfig() *Config {
	cfg := Defaults()
	cfg.Auth.Basic.Enabled = true
	cfg.Auth.Basic.Username = "admin"
	cfg.Auth.Basic.PasswordHash = "$2a$12$fakehashfakehashfakehashfakehashfakehashfakehashfakeha"
	return cfg
}

func TestValidateAuth_ShortToken_NoAllowWeak_Fatal(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Auth.Token.Enabled = true
	cfg.Auth.Token.Token = "short"
	cfg.Auth.Token.AllowWeak = false

	err := cfg.ValidateAuth()
	if err == nil {
		t.Fatal("expected fatal error for short token without allow_weak")
	}
	if !strings.Contains(err.Error(), "shorter than 32") {
		t.Errorf("error should mention length, got: %v", err)
	}
}

func TestValidateAuth_ShortToken_AllowWeak_OK(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Auth.Token.Enabled = true
	cfg.Auth.Token.Token = "short"
	cfg.Auth.Token.AllowWeak = true

	err := cfg.ValidateAuth()
	if err != nil {
		t.Fatalf("expected no error with allow_weak=true, got: %v", err)
	}
}

func TestValidateAuth_LongToken_OK(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Auth.Token.Enabled = true
	cfg.Auth.Token.Token = "this-is-a-very-long-token-that-exceeds-32-characters"

	err := cfg.ValidateAuth()
	if err != nil {
		t.Fatalf("expected no error for long token, got: %v", err)
	}
}

func TestValidateAuth_TokenDisabled_ShortOK(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Auth.Token.Enabled = false
	cfg.Auth.Token.Token = "abc" // short but disabled — should not matter

	err := cfg.ValidateAuth()
	if err != nil {
		t.Fatalf("expected no error for disabled token auth, got: %v", err)
	}
}

func TestValidateAuth_SessionTTL_TooShort(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Auth.SessionTTL = 1 * time.Minute

	err := cfg.ValidateAuth()
	if err == nil {
		t.Fatal("expected error for session TTL < 5m")
	}
}

func TestValidateAuth_SessionTTL_TooLong(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Auth.SessionTTL = 1000 * time.Hour

	err := cfg.ValidateAuth()
	if err == nil {
		t.Fatal("expected error for session TTL > 720h")
	}
}

func TestValidateAuth_BasicEnabled_NoHash_Fatal(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Auth.Basic.PasswordHash = ""

	err := cfg.ValidateAuth()
	if err == nil {
		t.Fatal("expected fatal error for basic auth without password_hash")
	}
}

func TestValidateAuth_TokenEnabled_Empty_Fatal(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Auth.Token.Enabled = true
	cfg.Auth.Token.Token = ""

	err := cfg.ValidateAuth()
	if err == nil {
		t.Fatal("expected fatal error for empty token")
	}
}

func TestValidateAuth_WebAuthnEnabled_NoOrigin_Fatal(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Auth.WebAuthn.Enabled = true
	cfg.Auth.WebAuthn.Origin = ""

	err := cfg.ValidateAuth()
	if err == nil {
		t.Fatal("expected fatal error for WebAuthn without origin")
	}
}

func TestValidateAuth_WebAuthnEnabled_NoBasic_Fatal(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Auth.Basic.Enabled = false
	cfg.Auth.Basic.PasswordHash = "" // not enabled, hash not required
	cfg.Auth.WebAuthn.Enabled = true
	cfg.Auth.WebAuthn.Origin = "https://vpn.example.com"

	err := cfg.ValidateAuth()
	if err == nil {
		t.Fatal("expected fatal error for WebAuthn without basic auth")
	}
}
