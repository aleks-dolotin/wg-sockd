package wireguard

import (
	"fmt"
	"net/netip"
)

// DeriveIPv6 converts an IPv4 client address to a ULA IPv6 address using the given prefix.
// The host part of the IPv4 address is appended to the prefix.
//
// Examples:
//
//	DeriveIPv6("10.0.3.2/24", "fd00:wg1::")  → "fd00:wg1::2/128", nil
//	DeriveIPv6("10.0.3.2/24", "")            → "", nil       (disabled)
//	DeriveIPv6("fe80::1/64",  "fd00:wg1::")  → "", nil       (IPv6 input, skip)
//
// Returns an error only if clientAddress is not a valid CIDR.
func DeriveIPv6(clientAddress, prefix string) (string, error) {
	if prefix == "" {
		return "", nil
	}
	if clientAddress == "" {
		return "", nil
	}

	p, err := netip.ParsePrefix(clientAddress)
	if err != nil {
		return "", fmt.Errorf("deriveIPv6: invalid client address %q: %w", clientAddress, err)
	}

	addr := p.Addr()
	if !addr.Is4() {
		return "", nil // IPv6 input — skip silently
	}

	// Extract host part: IP & ^mask.
	ip4 := addr.As4()
	maskBits := p.Bits()

	// For a /32 (host address), use last octet.
	if maskBits == 32 {
		return fmt.Sprintf("%s%d/128", prefix, ip4[3]), nil
	}

	// Compute host part from masked bits.
	// E.g. for /24: host = last octet; for /16: host = last 2 octets as uint.
	var host uint32
	for i := 0; i < 4; i++ {
		host = host<<8 | uint32(ip4[i])
	}
	// Mask off the network bits.
	hostBits := 32 - maskBits
	host = host & ((1 << hostBits) - 1)

	return fmt.Sprintf("%s%d/128", prefix, host), nil
}
