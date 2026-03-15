// Package middleware provides input validation functions for API handlers.
package middleware

import (
	"encoding/base64"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

// friendlyNameRe allows alphanumeric, spaces, hyphens, underscores, dots, apostrophes, parentheses.
var friendlyNameRe = regexp.MustCompile(`^[a-zA-Z0-9 \-_.,'()]+$`)

const (
	// MaxFriendlyNameLen is the maximum allowed length for friendly_name.
	MaxFriendlyNameLen = 64
	// WireGuardKeyBase64Len is the expected base64 string length for a 32-byte WireGuard key.
	WireGuardKeyBase64Len = 44
	// WireGuardKeyByteLen is the raw byte length of a WireGuard key.
	WireGuardKeyByteLen = 32
)

// ValidatePublicKey checks that a WireGuard public key is valid base64 encoding of 32 bytes.
func ValidatePublicKey(key string) error {
	if key == "" {
		return fmt.Errorf("public_key is required")
	}
	if len(key) != WireGuardKeyBase64Len {
		return fmt.Errorf("public_key must be %d base64 characters (got %d)", WireGuardKeyBase64Len, len(key))
	}
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return fmt.Errorf("public_key is not valid base64: %w", err)
	}
	if len(decoded) != WireGuardKeyByteLen {
		return fmt.Errorf("public_key must decode to %d bytes (got %d)", WireGuardKeyByteLen, len(decoded))
	}
	return nil
}

// ValidateAllowedIPs checks that each entry is a valid CIDR prefix (IPv4 or IPv6).
func ValidateAllowedIPs(ips []string) error {
	for _, s := range ips {
		s = strings.TrimSpace(s)
		if _, err := netip.ParsePrefix(s); err != nil {
			return fmt.Errorf("invalid CIDR %q: %w", s, err)
		}
	}
	return nil
}

// ValidateFriendlyName checks length and allowed characters.
func ValidateFriendlyName(name string) error {
	if name == "" {
		return nil // friendly_name is optional
	}
	if len(name) > MaxFriendlyNameLen {
		return fmt.Errorf("friendly_name exceeds %d characters (got %d)", MaxFriendlyNameLen, len(name))
	}
	if strings.ContainsAny(name, "\n\r\t") {
		return fmt.Errorf("friendly_name must not contain control characters")
	}
	if !friendlyNameRe.MatchString(name) {
		return fmt.Errorf("friendly_name contains invalid characters")
	}
	return nil
}

// ValidateNotes checks that notes don't contain control characters.
func ValidateNotes(notes string) error {
	if strings.ContainsAny(notes, "\r") {
		return fmt.Errorf("notes must not contain carriage returns")
	}
	return nil
}
