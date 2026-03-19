package confwriter

import (
	"fmt"
	"strings"
)

// ClientConfBuilder builds a WireGuard client .conf file string.

type ClientConfBuilder struct {
	address          string
	privateKey       string
	dns              string
	mtu              int
	serverPubKey     string
	serverEndpoint   string
	pka              int
	presharedKey     string // optional PSK for [Peer] section
	clientAllowedIPs string // required: AllowedIPs for client [Peer] section
}

// NewClientConfBuilder creates a new builder.
func NewClientConfBuilder() *ClientConfBuilder {
	return &ClientConfBuilder{}
}

func (b *ClientConfBuilder) SetAddress(v string) *ClientConfBuilder        { b.address = v; return b }
func (b *ClientConfBuilder) SetPrivateKey(v string) *ClientConfBuilder     { b.privateKey = v; return b }
func (b *ClientConfBuilder) SetDNS(v string) *ClientConfBuilder            { b.dns = v; return b }
func (b *ClientConfBuilder) SetMTU(v int) *ClientConfBuilder               { b.mtu = v; return b }
func (b *ClientConfBuilder) SetServerPublicKey(v string) *ClientConfBuilder  { b.serverPubKey = v; return b }
func (b *ClientConfBuilder) SetServerEndpoint(v string) *ClientConfBuilder   { b.serverEndpoint = v; return b }
func (b *ClientConfBuilder) SetPersistentKeepalive(v int) *ClientConfBuilder { b.pka = v; return b }
func (b *ClientConfBuilder) SetPresharedKey(v string) *ClientConfBuilder     { b.presharedKey = v; return b }
func (b *ClientConfBuilder) SetClientAllowedIPs(v string) *ClientConfBuilder { b.clientAllowedIPs = v; return b }

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
	if b.presharedKey != "" {
		fmt.Fprintf(&buf, "PresharedKey = %s\n", b.presharedKey)
	}
	if b.clientAllowedIPs != "" {
		fmt.Fprintf(&buf, "AllowedIPs = %s\n", b.clientAllowedIPs)
	}
	fmt.Fprintf(&buf, "Endpoint = %s\n", b.serverEndpoint)
	if b.pka > 0 {
		fmt.Fprintf(&buf, "PersistentKeepalive = %d\n", b.pka)
	}

	return buf.String()
}
