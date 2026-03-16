//go:build !dev_wg

package main

import "github.com/aleks-dolotin/wg-sockd/agent/internal/wireguard"

func maybeDevWgClient(_ string) (wireguard.WireGuardClient, bool) {
	return nil, false
}
