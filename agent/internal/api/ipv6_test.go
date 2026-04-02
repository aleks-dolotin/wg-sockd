package api

import (
	"testing"
)

// T5: serverAllowedIPs tests

func TestServerAllowedIPs_WithoutIPv6(t *testing.T) {
	got := serverAllowedIPs("10.0.3.2/24", "")
	want := "10.0.3.2/32"
	if got != want {
		t.Errorf("serverAllowedIPs without prefix: got %q, want %q", got, want)
	}
}

func TestServerAllowedIPs_WithIPv6(t *testing.T) {
	got := serverAllowedIPs("10.0.3.2/24", "fd00:ab01::")
	want := "10.0.3.2/32, fd00:ab01::2/128"
	if got != want {
		t.Errorf("serverAllowedIPs with prefix: got %q, want %q", got, want)
	}
}

func TestServerAllowedIPs_HostAddress(t *testing.T) {
	got := serverAllowedIPs("10.0.3.5/32", "fd00:ab01::")
	want := "10.0.3.5/32, fd00:ab01::5/128"
	if got != want {
		t.Errorf("serverAllowedIPs host /32: got %q, want %q", got, want)
	}
}

func TestServerAllowedIPs_InvalidCIDR(t *testing.T) {
	got := serverAllowedIPs("not-a-cidr", "fd00:ab01::")
	// Fallback: return as-is.
	if got != "not-a-cidr" {
		t.Errorf("serverAllowedIPs invalid CIDR: got %q, want %q", got, "not-a-cidr")
	}
}
