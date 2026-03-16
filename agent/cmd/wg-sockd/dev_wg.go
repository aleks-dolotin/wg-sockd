//go:build dev_wg

package main

import (
	"flag"
	"log"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/wireguard"
)

var devWg = flag.Bool("dev-wg", false, "use in-memory WireGuard client (development only)")

func maybeDevWgClient(interfaceName string) (wireguard.WireGuardClient, bool) {
	if !*devWg {
		return nil, false
	}
	client, err := wireguard.NewDevClient(interfaceName)
	if err != nil {
		log.Fatalf("FATAL: creating dev WireGuard client: %v", err)
	}
	log.Println("WARNING: using in-memory WireGuard client (--dev-wg) — NOT for production")
	return client, true
}
