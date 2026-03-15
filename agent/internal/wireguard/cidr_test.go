package wireguard

import (
	"fmt"
	"strings"
	"testing"
)

func TestComputeAllowedIPs(t *testing.T) {
	tests := []struct {
		name         string
		allowed      []string
		excluded     []string
		wantCountMin int // minimum expected route count (-1 to skip)
		wantCountMax int // maximum expected route count (-1 to skip)
		wantContains []string
		wantExcludes []string
		wantWarning  bool
		wantErr      bool
	}{
		{
			name:         "no exclusions returns allowed as-is",
			allowed:      []string{"10.0.0.0/24", "192.168.1.0/24"},
			excluded:     nil,
			wantCountMin: 2,
			wantCountMax: 2,
			wantContains: []string{"10.0.0.0/24", "192.168.1.0/24"},
		},
		{
			name:         "empty allowed returns empty",
			allowed:      []string{},
			excluded:     []string{"10.0.0.0/8"},
			wantCountMin: 0,
			wantCountMax: 0,
		},
		{
			name:         "IPv4: 0.0.0.0/0 minus 192.168.0.0/16",
			allowed:      []string{"0.0.0.0/0"},
			excluded:     []string{"192.168.0.0/16"},
			wantCountMin: 2,
			wantCountMax: 50,
			wantExcludes: []string{"192.168.0.0/16"},
		},
		{
			name:         "IPv6: ::/0 minus fd00::/8",
			allowed:      []string{"::/0"},
			excluded:     []string{"fd00::/8"},
			wantCountMin: 2,
			wantCountMax: 50,
			wantExcludes: []string{"fd00::/8"},
		},
		{
			name:         "mixed IPv4 and IPv6",
			allowed:      []string{"0.0.0.0/0", "::/0"},
			excluded:     []string{"10.0.0.0/8", "fd00::/8"},
			wantCountMin: 4,
			wantCountMax: 50,
			wantExcludes: []string{"10.0.0.0/8", "fd00::/8"},
		},
		{
			name:         "exclude nothing explicit empty slice",
			allowed:      []string{"10.0.0.0/24"},
			excluded:     []string{},
			wantCountMin: 1,
			wantCountMax: 1,
			wantContains: []string{"10.0.0.0/24"},
		},
		{
			name:         "exclude everything",
			allowed:      []string{"10.0.0.0/24"},
			excluded:     []string{"10.0.0.0/24"},
			wantCountMin: 0,
			wantCountMax: 0,
		},
		{
			name:         "overlapping CIDRs in allowed",
			allowed:      []string{"10.0.0.0/8", "10.0.0.0/24"},
			excluded:     []string{"10.0.0.0/24"},
			wantCountMin: 1,
			wantCountMax: 20,
			wantExcludes: []string{"10.0.0.0/24"},
		},
		{
			name:    "invalid allowed CIDR",
			allowed: []string{"not-a-cidr"},
			wantErr: true,
		},
		{
			name:     "invalid excluded CIDR",
			allowed:  []string{"10.0.0.0/24"},
			excluded: []string{"also-not-a-cidr"},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ComputeAllowedIPs(tt.allowed, tt.excluded)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantCountMin >= 0 && result.RouteCount < tt.wantCountMin {
				t.Errorf("RouteCount %d < expected minimum %d, prefixes: %v", result.RouteCount, tt.wantCountMin, result.Prefixes)
			}
			if tt.wantCountMax >= 0 && result.RouteCount > tt.wantCountMax {
				t.Errorf("RouteCount %d > expected maximum %d", result.RouteCount, tt.wantCountMax)
			}

			if tt.wantWarning && result.Warning == "" {
				t.Error("expected warning, got empty")
			}

			prefixSet := make(map[string]bool)
			for _, p := range result.Prefixes {
				prefixSet[p] = true
			}

			for _, want := range tt.wantContains {
				if !prefixSet[want] {
					t.Errorf("expected result to contain %q, got %v", want, result.Prefixes)
				}
			}

			for _, excl := range tt.wantExcludes {
				if prefixSet[excl] {
					t.Errorf("expected result to NOT contain %q, got %v", excl, result.Prefixes)
				}
			}
		})
	}
}

func TestComputeAllowedIPs_RouteCountExact(t *testing.T) {
	// 0.0.0.0/0 minus one /32 = exactly 32 prefixes.
	result, err := ComputeAllowedIPs([]string{"0.0.0.0/0"}, []string{"192.168.1.1/32"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RouteCount != 32 {
		t.Errorf("expected 32 routes for 0.0.0.0/0 minus one /32, got %d", result.RouteCount)
	}
	if result.Warning != "" {
		t.Errorf("expected no warning for 32 routes, got %q", result.Warning)
	}
}

func TestComputeAllowedIPs_HighRouteCountWarning(t *testing.T) {
	// Exclude many /24s from 0.0.0.0/0 to potentially trigger high route count.
	excluded := make([]string, 0, 60)
	for i := 1; i <= 60; i++ {
		excluded = append(excluded, fmt.Sprintf("10.%d.0.0/24", i))
	}
	result, err := ComputeAllowedIPs([]string{"0.0.0.0/0"}, excluded)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Logf("Route count: %d, warning: %q", result.RouteCount, result.Warning)

	// Verify warning logic: if count > threshold, warning must be set.
	if result.RouteCount > RouteCountWarningThreshold {
		if result.Warning == "" {
			t.Error("expected warning for high route count")
		}
		if !strings.Contains(result.Warning, "exceeds threshold") {
			t.Errorf("warning should mention threshold: %q", result.Warning)
		}
	}
}

func TestComputeAllowedIPs_ExcludeSuperset(t *testing.T) {
	// Excluding a superset of allowed should return empty.
	result, err := ComputeAllowedIPs([]string{"10.0.0.0/24"}, []string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RouteCount != 0 {
		t.Errorf("expected 0 routes when excluded is superset, got %d: %v", result.RouteCount, result.Prefixes)
	}
}

func TestComputeAllowedIPs_IPv4MappedIPv6(t *testing.T) {
	// Ensure plain IPv4 CIDRs work (not IPv4-mapped IPv6).
	result, err := ComputeAllowedIPs([]string{"0.0.0.0/0"}, []string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, p := range result.Prefixes {
		if strings.Contains(p, "::ffff:") {
			t.Errorf("unexpected IPv4-mapped IPv6 prefix: %s", p)
		}
	}
}
