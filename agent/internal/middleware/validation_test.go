package middleware

import (
	"strings"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func TestValidatePublicKey(t *testing.T) {
	// Generate a valid key.
	priv, _ := wgtypes.GeneratePrivateKey()
	validKey := priv.PublicKey().String()

	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"valid key", validKey, false},
		{"empty key", "", true},
		{"too short", "abc", true},
		{"wrong length base64", "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXo=", true},
		{"not base64", strings.Repeat("!", 44), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePublicKey(tt.key)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateAllowedIPs(t *testing.T) {
	tests := []struct {
		name    string
		ips     []string
		wantErr bool
	}{
		{"valid IPv4", []string{"10.0.0.0/24", "192.168.1.0/24"}, false},
		{"valid IPv6", []string{"::/0", "fd00::/8"}, false},
		{"mixed", []string{"0.0.0.0/0", "::/0"}, false},
		{"empty slice", []string{}, false},
		{"invalid CIDR", []string{"not-a-cidr"}, true},
		{"missing prefix length", []string{"10.0.0.0"}, true},
		{"one valid one invalid", []string{"10.0.0.0/24", "bad"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAllowedIPs(tt.ips)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateFriendlyName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty (optional)", "", false},
		{"normal name", "Alice Laptop", false},
		{"with hyphens", "my-device-2024", false},
		{"with dots", "phone.work", false},
		{"with apostrophe", "Alice's Phone", false},
		{"with parens", "Device (backup)", false},
		{"cyrillic", "Телефон Алексея", false},
		{"mixed scripts", "Alice's Телефон", false},
		{"chinese", "小明的手机", false},
		{"exactly 64 chars", strings.Repeat("a", 64), false},
		{"65 chars too long", strings.Repeat("a", 65), true},
		{"newline", "bad\nname", true},
		{"carriage return", "bad\rname", true},
		{"tab", "bad\tname", true},
		{"special chars", "bad<script>", true},
		{"semicolon", "bad;name", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFriendlyName(tt.input)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for %q, got nil", tt.input)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for %q: %v", tt.input, err)
			}
		})
	}
}

func TestValidateNotes(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty", "", false},
		{"normal text", "some notes about the peer", false},
		{"with newlines", "line1\nline2", false},
		{"carriage return", "bad\rnotes", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNotes(tt.input)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
