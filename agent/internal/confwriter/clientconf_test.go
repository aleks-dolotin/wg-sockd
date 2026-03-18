package confwriter

import (
	"strings"
	"testing"
)

func intPtr(v int) *int { return &v }

func TestResolveClientConf_PKA(t *testing.T) {
	tests := []struct {
		name       string
		peer       ClientConfPeerValues
		profile    *ClientConfProfileValues
		defaults   ClientConfDefaults
		wantPKA    int
		wantSource string
	}{
		{
			name:       "peer=30, profile=20, global=25 → peer wins",
			peer:       ClientConfPeerValues{PKA: intPtr(30)},
			profile:    &ClientConfProfileValues{PKA: intPtr(20)},
			defaults:   ClientConfDefaults{PKA: 25},
			wantPKA:    30,
			wantSource: "peer",
		},
		{
			name:       "peer=nil, profile=20, global=25 → profile wins",
			peer:       ClientConfPeerValues{},
			profile:    &ClientConfProfileValues{PKA: intPtr(20)},
			defaults:   ClientConfDefaults{PKA: 25},
			wantPKA:    20,
			wantSource: "profile",
		},
		{
			name:       "peer=nil, profile=nil, global=25 → global wins",
			peer:       ClientConfPeerValues{},
			profile:    &ClientConfProfileValues{},
			defaults:   ClientConfDefaults{PKA: 25},
			wantPKA:    25,
			wantSource: "global",
		},
		{
			name:       "peer=nil, profile=nil, global=0 → default fallback 25",
			peer:       ClientConfPeerValues{},
			profile:    &ClientConfProfileValues{},
			defaults:   ClientConfDefaults{PKA: 0},
			wantPKA:    25,
			wantSource: "default",
		},
		{
			name:       "peer=0 explicit, profile=20, global=25 → peer wins (0 = off)",
			peer:       ClientConfPeerValues{PKA: intPtr(0)},
			profile:    &ClientConfProfileValues{PKA: intPtr(20)},
			defaults:   ClientConfDefaults{PKA: 25},
			wantPKA:    0,
			wantSource: "peer",
		},
		{
			name:       "no profile at all, global=25 → global",
			peer:       ClientConfPeerValues{},
			profile:    nil,
			defaults:   ClientConfDefaults{PKA: 25},
			wantPKA:    25,
			wantSource: "global",
		},
		{
			name:       "all nil/zero → default 25",
			peer:       ClientConfPeerValues{},
			profile:    nil,
			defaults:   ClientConfDefaults{},
			wantPKA:    25,
			wantSource: "default",
		},
		{
			name:       "profile=0 explicit, global=25 → profile wins (0 = off)",
			peer:       ClientConfPeerValues{},
			profile:    &ClientConfProfileValues{PKA: intPtr(0)},
			defaults:   ClientConfDefaults{PKA: 25},
			wantPKA:    0,
			wantSource: "profile",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := ResolveClientConf(tt.peer, tt.profile, tt.defaults)
			if rc.PKA != tt.wantPKA {
				t.Errorf("PKA = %d, want %d", rc.PKA, tt.wantPKA)
			}
			if rc.PKASource != tt.wantSource {
				t.Errorf("PKASource = %q, want %q", rc.PKASource, tt.wantSource)
			}
		})
	}
}

func TestResolveClientConf_DNS(t *testing.T) {
	tests := []struct {
		name       string
		peer       ClientConfPeerValues
		profile    *ClientConfProfileValues
		defaults   ClientConfDefaults
		wantDNS    string
		wantSource string
	}{
		{
			name:       "peer=9.9.9.9, profile=8.8.8.8 → peer",
			peer:       ClientConfPeerValues{DNS: "9.9.9.9"},
			profile:    &ClientConfProfileValues{DNS: "8.8.8.8"},
			defaults:   ClientConfDefaults{DNS: "1.0.0.1"},
			wantDNS:    "9.9.9.9",
			wantSource: "peer",
		},
		{
			name:       "peer empty, profile=8.8.8.8 → profile",
			peer:       ClientConfPeerValues{},
			profile:    &ClientConfProfileValues{DNS: "8.8.8.8"},
			defaults:   ClientConfDefaults{DNS: "1.0.0.1"},
			wantDNS:    "8.8.8.8",
			wantSource: "profile",
		},
		{
			name:       "peer empty, profile empty, global=1.0.0.1 → global",
			peer:       ClientConfPeerValues{},
			profile:    &ClientConfProfileValues{},
			defaults:   ClientConfDefaults{DNS: "1.0.0.1"},
			wantDNS:    "1.0.0.1",
			wantSource: "global",
		},
		{
			name:       "all empty → default (empty = omit)",
			peer:       ClientConfPeerValues{},
			profile:    nil,
			defaults:   ClientConfDefaults{},
			wantDNS:    "",
			wantSource: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := ResolveClientConf(tt.peer, tt.profile, tt.defaults)
			if rc.DNS != tt.wantDNS {
				t.Errorf("DNS = %q, want %q", rc.DNS, tt.wantDNS)
			}
			if rc.DNSSource != tt.wantSource {
				t.Errorf("DNSSource = %q, want %q", rc.DNSSource, tt.wantSource)
			}
		})
	}
}

func TestResolveClientConf_MTU(t *testing.T) {
	tests := []struct {
		name       string
		peer       ClientConfPeerValues
		profile    *ClientConfProfileValues
		defaults   ClientConfDefaults
		wantMTU    int
		wantSource string
	}{
		{
			name:       "peer=1380, profile=1400 → peer",
			peer:       ClientConfPeerValues{MTU: intPtr(1380)},
			profile:    &ClientConfProfileValues{MTU: intPtr(1400)},
			defaults:   ClientConfDefaults{MTU: 1420},
			wantMTU:    1380,
			wantSource: "peer",
		},
		{
			name:       "peer nil, profile=1400 → profile",
			peer:       ClientConfPeerValues{},
			profile:    &ClientConfProfileValues{MTU: intPtr(1400)},
			defaults:   ClientConfDefaults{MTU: 1420},
			wantMTU:    1400,
			wantSource: "profile",
		},
		{
			name:       "peer nil, profile nil, global=1420 → global",
			peer:       ClientConfPeerValues{},
			profile:    &ClientConfProfileValues{},
			defaults:   ClientConfDefaults{MTU: 1420},
			wantMTU:    1420,
			wantSource: "global",
		},
		{
			name:       "all zero → default (0 = omit)",
			peer:       ClientConfPeerValues{},
			profile:    nil,
			defaults:   ClientConfDefaults{},
			wantMTU:    0,
			wantSource: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := ResolveClientConf(tt.peer, tt.profile, tt.defaults)
			if rc.MTU != tt.wantMTU {
				t.Errorf("MTU = %d, want %d", rc.MTU, tt.wantMTU)
			}
			if rc.MTUSource != tt.wantSource {
				t.Errorf("MTUSource = %q, want %q", rc.MTUSource, tt.wantSource)
			}
		})
	}
}

func TestClientConfBuilder_Build(t *testing.T) {
	t.Run("full conf with all fields", func(t *testing.T) {
		conf := NewClientConfBuilder().
			SetPrivateKey("PRIVKEY123").
			SetAddress("10.0.0.2/32").
			SetDNS("1.1.1.1").
			SetMTU(1380).
			SetServerPublicKey("SERVERPUB456").
			SetServerEndpoint("vpn.example.com:51820").
			SetPersistentKeepalive(25).
			Build()

		assertContains(t, conf, "PrivateKey = PRIVKEY123")
		assertContains(t, conf, "Address = 10.0.0.2/32")
		assertContains(t, conf, "DNS = 1.1.1.1")
		assertContains(t, conf, "MTU = 1380")
		assertContains(t, conf, "PublicKey = SERVERPUB456")
		assertContains(t, conf, "Endpoint = vpn.example.com:51820")
		assertContains(t, conf, "PersistentKeepalive = 25")
		assertContains(t, conf, "AllowedIPs = 0.0.0.0/0, ::/0")
	})

	t.Run("no private key → comment placeholder", func(t *testing.T) {
		conf := NewClientConfBuilder().
			SetAddress("10.0.0.2/32").
			SetServerPublicKey("SERVERPUB").
			SetServerEndpoint("1.2.3.4:51820").
			SetPersistentKeepalive(25).
			Build()

		assertContains(t, conf, "# PrivateKey = <insert your private key>")
		assertNotContains(t, conf, "PrivateKey = \n") // no empty PrivateKey line
	})

	t.Run("DNS/MTU omitted when empty/zero", func(t *testing.T) {
		conf := NewClientConfBuilder().
			SetAddress("10.0.0.2/32").
			SetServerPublicKey("SERVERPUB").
			SetServerEndpoint("1.2.3.4:51820").
			SetPersistentKeepalive(25).
			Build()

		assertNotContains(t, conf, "DNS =")
		assertNotContains(t, conf, "MTU =")
	})

	t.Run("PKA=0 → omitted", func(t *testing.T) {
		conf := NewClientConfBuilder().
			SetAddress("10.0.0.2/32").
			SetServerPublicKey("SERVERPUB").
			SetServerEndpoint("1.2.3.4:51820").
			SetPersistentKeepalive(0).
			Build()

		assertNotContains(t, conf, "PersistentKeepalive")
	})

	t.Run("DNS in Interface section, PKA in Peer section", func(t *testing.T) {
		conf := NewClientConfBuilder().
			SetAddress("10.0.0.2/32").
			SetDNS("8.8.8.8").
			SetMTU(1400).
			SetServerPublicKey("SERVERPUB").
			SetServerEndpoint("1.2.3.4:51820").
			SetPersistentKeepalive(30).
			Build()

		// DNS and MTU should appear before [Peer]
		peerIdx := strings.Index(conf, "[Peer]")
		dnsIdx := strings.Index(conf, "DNS = 8.8.8.8")
		mtuIdx := strings.Index(conf, "MTU = 1400")
		pkaIdx := strings.Index(conf, "PersistentKeepalive = 30")

		if dnsIdx < 0 || dnsIdx > peerIdx {
			t.Error("DNS should be in [Interface] section (before [Peer])")
		}
		if mtuIdx < 0 || mtuIdx > peerIdx {
			t.Error("MTU should be in [Interface] section (before [Peer])")
		}
		if pkaIdx < 0 || pkaIdx < peerIdx {
			t.Error("PersistentKeepalive should be in [Peer] section (after [Peer])")
		}
	})
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("expected string to contain %q, got:\n%s", substr, s)
	}
}

func assertNotContains(t *testing.T, s, substr string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Errorf("expected string NOT to contain %q, got:\n%s", substr, s)
	}
}
