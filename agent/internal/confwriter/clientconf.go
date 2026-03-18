package confwriter

import (
	"fmt"
	"strings"
)

// ResolvedConf holds the resolved values for a client config after 4-level cascade.
type ResolvedConf struct {
	DNS       string // empty = omit
	DNSSource string // "peer", "profile", "global", "default"
	MTU       int    // 0 = omit
	MTUSource string
	PKA       int // 0 = off (explicit), -1 used internally for "not set"
	PKASource string
}

// ClientConfDefaults holds global defaults from config.yaml.
type ClientConfDefaults struct {
	DNS string
	MTU int
	PKA int // 0 = off, 25 = default
}

// ClientConfPeerValues holds peer-level override values.
// Pointer fields: nil = not set (inherit), non-nil = explicit value.
type ClientConfPeerValues struct {
	DNS string // empty = not set
	MTU *int   // nil = not set
	PKA *int   // nil = not set; 0 = explicitly off
}

// ClientConfProfileValues holds profile-level default values.
type ClientConfProfileValues struct {
	DNS string
	MTU *int
	PKA *int
}

// ResolveClientConf resolves DNS, MTU, and PersistentKeepalive using 4-level cascade:
// peer → profile → global → hardcoded fallback.
//
// Hardcoded fallbacks: DNS="" (omit), MTU=0 (omit), PKA=25.
func ResolveClientConf(peer ClientConfPeerValues, profile *ClientConfProfileValues, defaults ClientConfDefaults) ResolvedConf {
	rc := ResolvedConf{}

	// --- DNS ---
	if peer.DNS != "" {
		rc.DNS = peer.DNS
		rc.DNSSource = "peer"
	} else if profile != nil && profile.DNS != "" {
		rc.DNS = profile.DNS
		rc.DNSSource = "profile"
	} else if defaults.DNS != "" {
		rc.DNS = defaults.DNS
		rc.DNSSource = "global"
	} else {
		rc.DNS = ""
		rc.DNSSource = "default"
	}

	// --- MTU ---
	if peer.MTU != nil {
		rc.MTU = *peer.MTU
		rc.MTUSource = "peer"
	} else if profile != nil && profile.MTU != nil {
		rc.MTU = *profile.MTU
		rc.MTUSource = "profile"
	} else if defaults.MTU != 0 {
		rc.MTU = defaults.MTU
		rc.MTUSource = "global"
	} else {
		rc.MTU = 0
		rc.MTUSource = "default"
	}

	// --- PKA ---
	// PKA uses pointer semantics: nil = not set, 0 = explicitly off.
	if peer.PKA != nil {
		rc.PKA = *peer.PKA
		rc.PKASource = "peer"
	} else if profile != nil && profile.PKA != nil {
		rc.PKA = *profile.PKA
		rc.PKASource = "profile"
	} else if defaults.PKA != 0 {
		rc.PKA = defaults.PKA
		rc.PKASource = "global"
	} else {
		// Hardcoded fallback: 25s (backward compat).
		rc.PKA = 25
		rc.PKASource = "default"
	}

	return rc
}

// ClientConfBuilder builds a WireGuard client .conf file string.
type ClientConfBuilder struct {
	address      string
	privateKey   string
	dns          string
	mtu          int
	serverPubKey string
	serverEndpoint string
	serverAllowedIPs string
	pka          int
}

// NewClientConfBuilder creates a new builder.
func NewClientConfBuilder() *ClientConfBuilder {
	return &ClientConfBuilder{
		serverAllowedIPs: "0.0.0.0/0, ::/0",
	}
}

func (b *ClientConfBuilder) SetAddress(v string) *ClientConfBuilder      { b.address = v; return b }
func (b *ClientConfBuilder) SetPrivateKey(v string) *ClientConfBuilder    { b.privateKey = v; return b }
func (b *ClientConfBuilder) SetDNS(v string) *ClientConfBuilder           { b.dns = v; return b }
func (b *ClientConfBuilder) SetMTU(v int) *ClientConfBuilder              { b.mtu = v; return b }
func (b *ClientConfBuilder) SetServerPublicKey(v string) *ClientConfBuilder { b.serverPubKey = v; return b }
func (b *ClientConfBuilder) SetServerEndpoint(v string) *ClientConfBuilder  { b.serverEndpoint = v; return b }
func (b *ClientConfBuilder) SetServerAllowedIPs(v string) *ClientConfBuilder { b.serverAllowedIPs = v; return b }
func (b *ClientConfBuilder) SetPersistentKeepalive(v int) *ClientConfBuilder { b.pka = v; return b }

// Build generates the WireGuard client .conf file content.
func (b *ClientConfBuilder) Build() string {
	var buf strings.Builder

	buf.WriteString("[Interface]\n")
	if b.privateKey != "" {
		fmt.Fprintf(&buf, "PrivateKey = %s\n", b.privateKey)
	} else {
		buf.WriteString("# PrivateKey = <insert your private key>\n")
	}
	fmt.Fprintf(&buf, "Address = %s\n", b.address)
	if b.dns != "" {
		fmt.Fprintf(&buf, "DNS = %s\n", b.dns)
	}
	if b.mtu > 0 {
		fmt.Fprintf(&buf, "MTU = %d\n", b.mtu)
	}

	buf.WriteString("\n[Peer]\n")
	fmt.Fprintf(&buf, "PublicKey = %s\n", b.serverPubKey)
	fmt.Fprintf(&buf, "AllowedIPs = %s\n", b.serverAllowedIPs)
	fmt.Fprintf(&buf, "Endpoint = %s\n", b.serverEndpoint)
	if b.pka > 0 {
		fmt.Fprintf(&buf, "PersistentKeepalive = %d\n", b.pka)
	}

	return buf.String()
}
