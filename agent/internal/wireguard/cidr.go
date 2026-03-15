package wireguard

import (
	"fmt"
	"net/netip"
	"sort"

	"go4.org/netipx"
)

// RouteCountWarningThreshold is the number of CIDRs above which a warning is issued.
const RouteCountWarningThreshold = 50

// CIDRResult holds the result of a CIDR exclusion computation.
type CIDRResult struct {
	// Prefixes is the resolved list of CIDR strings after exclusion.
	Prefixes []string
	// RouteCount is the number of resulting CIDR prefixes.
	RouteCount int
	// Warning is set when RouteCount exceeds RouteCountWarningThreshold.
	Warning string
}

// ComputeAllowedIPs calculates the effective allowed IP prefixes by subtracting
// excluded CIDRs from allowed CIDRs. Supports IPv4, IPv6, and mixed inputs.
//
// Returns a CIDRResult with the resolved prefixes, route count, and optional warning.
func ComputeAllowedIPs(allowed, excluded []string) (*CIDRResult, error) {
	if len(allowed) == 0 {
		return &CIDRResult{Prefixes: []string{}, RouteCount: 0}, nil
	}

	// If no exclusions, return allowed as-is (but validate first).
	if len(excluded) == 0 {
		for _, s := range allowed {
			if _, err := netip.ParsePrefix(s); err != nil {
				return nil, fmt.Errorf("parsing allowed CIDR %q: %w", s, err)
			}
		}
		result := &CIDRResult{
			Prefixes:   make([]string, len(allowed)),
			RouteCount: len(allowed),
		}
		copy(result.Prefixes, allowed)
		if result.RouteCount > RouteCountWarningThreshold {
			result.Warning = fmt.Sprintf("route count %d exceeds threshold of %d", result.RouteCount, RouteCountWarningThreshold)
		}
		return result, nil
	}

	// Parse allowed prefixes.
	var allowedPrefixes []netip.Prefix
	for _, s := range allowed {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("parsing allowed CIDR %q: %w", s, err)
		}
		allowedPrefixes = append(allowedPrefixes, p.Masked())
	}

	// Parse excluded prefixes.
	var excludedPrefixes []netip.Prefix
	for _, s := range excluded {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("parsing excluded CIDR %q: %w", s, err)
		}
		excludedPrefixes = append(excludedPrefixes, p.Masked())
	}

	// Build IP set: add allowed, remove excluded.
	var b netipx.IPSetBuilder
	for _, p := range allowedPrefixes {
		b.AddPrefix(p)
	}
	for _, p := range excludedPrefixes {
		b.RemovePrefix(p)
	}

	set, err := b.IPSet()
	if err != nil {
		return nil, fmt.Errorf("building IP set: %w", err)
	}

	prefixes := set.Prefixes()

	// Convert to strings.
	strs := make([]string, len(prefixes))
	for i, p := range prefixes {
		strs[i] = p.String()
	}

	// Sort for deterministic output.
	sort.Strings(strs)

	result := &CIDRResult{
		Prefixes:   strs,
		RouteCount: len(strs),
	}

	if result.RouteCount > RouteCountWarningThreshold {
		result.Warning = fmt.Sprintf("route count %d exceeds threshold of %d", result.RouteCount, RouteCountWarningThreshold)
	}

	return result, nil
}
