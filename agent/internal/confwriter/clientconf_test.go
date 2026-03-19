package confwriter

import (
	"strings"
	"testing"
)

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
			SetClientAllowedIPs("0.0.0.0/0, ::/0").
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
			SetClientAllowedIPs("0.0.0.0/0").
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
			SetClientAllowedIPs("10.0.0.0/8").
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
			SetClientAllowedIPs("10.0.0.0/8").
			Build()

		assertNotContains(t, conf, "PersistentKeepalive")
	})

	t.Run("AllowedIPs omitted when clientAllowedIPs empty", func(t *testing.T) {
		conf := NewClientConfBuilder().
			SetAddress("10.0.0.2/32").
			SetServerPublicKey("SERVERPUB").
			SetServerEndpoint("1.2.3.4:51820").
			Build()

		assertNotContains(t, conf, "AllowedIPs")
	})

	t.Run("explicit clientAllowedIPs used directly", func(t *testing.T) {
		conf := NewClientConfBuilder().
			SetAddress("10.0.0.2/32").
			SetServerPublicKey("SERVERPUB").
			SetServerEndpoint("1.2.3.4:51820").
			SetClientAllowedIPs("10.0.0.0/8, 192.168.1.0/24").
			Build()

		assertContains(t, conf, "AllowedIPs = 10.0.0.0/8, 192.168.1.0/24")
	})

	t.Run("DNS in Interface section, PKA in Peer section", func(t *testing.T) {
		conf := NewClientConfBuilder().
			SetAddress("10.0.0.2/32").
			SetDNS("8.8.8.8").
			SetMTU(1400).
			SetServerPublicKey("SERVERPUB").
			SetServerEndpoint("1.2.3.4:51820").
			SetPersistentKeepalive(30).
			SetClientAllowedIPs("10.0.0.0/8").
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

	t.Run("preshared key included in Peer section", func(t *testing.T) {
		conf := NewClientConfBuilder().
			SetAddress("10.0.0.2/32").
			SetServerPublicKey("SERVERPUB").
			SetServerEndpoint("1.2.3.4:51820").
			SetClientAllowedIPs("0.0.0.0/0").
			SetPresharedKey("PSKVALUE123").
			Build()

		assertContains(t, conf, "PresharedKey = PSKVALUE123")
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
