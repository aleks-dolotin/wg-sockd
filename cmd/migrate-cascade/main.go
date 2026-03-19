// Package main provides a one-time migration script that resolves the 4-level
// configuration cascade for all existing peers and writes the final values
// directly into peer DB records.
//
// Run this BEFORE upgrading to v0.15.0 (which removes the cascade code).
//
// Usage:
//
//  1. Compile the tool (needs agent module deps):
//     cd agent/cmd/migrate-cascade && go build -o ../../../bin/migrate-cascade . && cd ../../..
//
//  2. Run the binary:
//     ./bin/migrate-cascade --db-path /var/lib/wg-sockd/wg-sockd.db [--config /etc/wg-sockd/config.yaml] [--dry-run]
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"go.yaml.in/yaml/v2"
	_ "modernc.org/sqlite"
	"gopkg.in/yaml.v3"
)

// --- Inline cascade logic (copied from pre-v0.15.0 confwriter) ---

type resolvedConf struct {
	DNS              string
	MTU              int
	PKA              int
	ClientAllowedIPs string
}

type peerDefaults struct {
	ClientDNS                 string `yaml:"client_dns"`
	ClientMTU                 int    `yaml:"client_mtu"`
	ClientPersistentKeepalive int    `yaml:"client_persistent_keepalive"`
	ClientAllowedIPs          string `yaml:"client_allowed_ips"`
}

type configFile struct {
	PeerDefaults peerDefaults `yaml:"peer_defaults"`
}

func resolveClientConf(
	peerDNS string, peerMTU *int, peerPKA *int, peerClientAllowedIPs string,
	profDNS string, profMTU *int, profPKA *int, profClientAllowedIPs string,
	hasProfile bool,
	defaults peerDefaults,
) resolvedConf {
	rc := resolvedConf{}

	// DNS
	if peerDNS != "" {
		rc.DNS = peerDNS
	} else if hasProfile && profDNS != "" {
		rc.DNS = profDNS
	} else if defaults.ClientDNS != "" {
		rc.DNS = defaults.ClientDNS
	}

	// MTU
	if peerMTU != nil {
		rc.MTU = *peerMTU
	} else if hasProfile && profMTU != nil {
		rc.MTU = *profMTU
	} else if defaults.ClientMTU != 0 {
		rc.MTU = defaults.ClientMTU
	}

	// PKA
	if peerPKA != nil {
		rc.PKA = *peerPKA
	} else if hasProfile && profPKA != nil {
		rc.PKA = *profPKA
	} else if defaults.ClientPersistentKeepalive != 0 {
		rc.PKA = defaults.ClientPersistentKeepalive
	} else {
		rc.PKA = 25 // hardcoded fallback
	}

	// ClientAllowedIPs
	if peerClientAllowedIPs != "" {
		rc.ClientAllowedIPs = peerClientAllowedIPs
	} else if hasProfile && profClientAllowedIPs != "" {
		rc.ClientAllowedIPs = profClientAllowedIPs
	} else if defaults.ClientAllowedIPs != "" {
		rc.ClientAllowedIPs = defaults.ClientAllowedIPs
	} else {
		rc.ClientAllowedIPs = "0.0.0.0/0, ::/0" // hardcoded fallback
	}

	return rc
}

// resolveClientAddress determines client_address from AllowedIPs /32 fallback.
func resolveClientAddress(clientAddress, allowedIPs string) string {
	if clientAddress != "" {
		return clientAddress
	}
	if allowedIPs == "" {
		return ""
	}
	parts := strings.Split(allowedIPs, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if !strings.HasSuffix(p, "/32") {
			return "" // contains subnets, can't use as client address
		}
	}
	return allowedIPs
}

func main() {
	dbPath := flag.String("db-path", "/var/lib/wg-sockd/wg-sockd.db", "path to wg-sockd SQLite database")
	configPath := flag.String("config", "/etc/wg-sockd/config.yaml", "path to config.yaml (for peer_defaults)")
	dryRun := flag.Bool("dry-run", false, "show what would change without writing to DB")
	flag.Parse()

	// Load global defaults from config.
	defaults := peerDefaults{ClientPersistentKeepalive: 25}
	if data, err := os.ReadFile(*configPath); err == nil {
		var cfg configFile
		if err := yaml.Unmarshal(data, &cfg); err == nil {
			defaults = cfg.PeerDefaults
			if defaults.ClientPersistentKeepalive == 0 {
				defaults.ClientPersistentKeepalive = 25
			}
		}
	} else {
		log.Printf("WARN: could not read config %s: %v — using hardcoded defaults", *configPath, err)
	}

	// Open DB.
	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		log.Fatalf("FATAL: opening database: %v", err)
	}
	defer db.Close()

	// Load profiles into map.
	profileMap := make(map[string]struct {
		DNS              string
		MTU              *int
		PKA              *int
		ClientAllowedIPs string
	})
	profRows, err := db.Query(`SELECT name, client_dns, client_mtu, persistent_keepalive, client_allowed_ips FROM profiles`)
	if err != nil {
		log.Printf("WARN: could not load profiles: %v — cascade will use global defaults only", err)
	} else {
		defer profRows.Close()
		for profRows.Next() {
			var name, dns, clientAIPs string
			var mtu, pka sql.NullInt64
			if err := profRows.Scan(&name, &dns, &mtu, &pka, &clientAIPs); err != nil {
				log.Printf("WARN: scanning profile: %v", err)
				continue
			}
			entry := struct {
				DNS              string
				MTU              *int
				PKA              *int
				ClientAllowedIPs string
			}{DNS: dns, ClientAllowedIPs: clientAIPs}
			if mtu.Valid {
				v := int(mtu.Int64)
				entry.MTU = &v
			}
			if pka.Valid {
				v := int(pka.Int64)
				entry.PKA = &v
			}
			profileMap[name] = entry
		}
	}

	// Load all peers.
	rows, err := db.Query(`SELECT id, public_key, friendly_name, allowed_ips, profile, 
		client_dns, client_mtu, persistent_keepalive, client_allowed_ips, client_address 
		FROM peers`)
	if err != nil {
		log.Fatalf("FATAL: querying peers: %v", err)
	}
	defer rows.Close()

	type peerRecord struct {
		ID               int64
		PublicKey        string
		FriendlyName     string
		AllowedIPs       string
		Profile          *string
		ClientDNS        string
		ClientMTU        *int
		PKA              *int
		ClientAllowedIPs string
		ClientAddress    string
	}

	var peers []peerRecord
	for rows.Next() {
		var p peerRecord
		var profile sql.NullString
		var mtu, pka sql.NullInt64
		if err := rows.Scan(&p.ID, &p.PublicKey, &p.FriendlyName, &p.AllowedIPs, &profile,
			&p.ClientDNS, &mtu, &pka, &p.ClientAllowedIPs, &p.ClientAddress); err != nil {
			log.Fatalf("FATAL: scanning peer: %v", err)
		}
		if profile.Valid {
			p.Profile = &profile.String
		}
		if mtu.Valid {
			v := int(mtu.Int64)
			p.ClientMTU = &v
		}
		if pka.Valid {
			v := int(pka.Int64)
			p.PKA = &v
		}
		peers = append(peers, p)
	}

	log.Printf("Found %d peers, %d profiles", len(peers), len(profileMap))

	// Resolve cascade for each peer and update.
	updated := 0
	for _, p := range peers {
		var profDNS, profClientAIPs string
		var profMTU, profPKA *int
		hasProfile := false
		if p.Profile != nil && *p.Profile != "" {
			if prof, ok := profileMap[*p.Profile]; ok {
				profDNS = prof.DNS
				profMTU = prof.MTU
				profPKA = prof.PKA
				profClientAIPs = prof.ClientAllowedIPs
				hasProfile = true
			}
		}

		rc := resolveClientConf(
			p.ClientDNS, p.ClientMTU, p.PKA, p.ClientAllowedIPs,
			profDNS, profMTU, profPKA, profClientAIPs,
			hasProfile, defaults,
		)

		// Resolve client_address fallback.
		clientAddr := resolveClientAddress(p.ClientAddress, p.AllowedIPs)

		// Check if anything changed.
		oldDNS := p.ClientDNS
		oldMTU := 0
		if p.ClientMTU != nil {
			oldMTU = *p.ClientMTU
		}
		oldPKA := -1 // sentinel for "not set"
		if p.PKA != nil {
			oldPKA = *p.PKA
		}
		oldClientAIPs := p.ClientAllowedIPs
		oldClientAddr := p.ClientAddress

		needsUpdate := false
		if rc.DNS != oldDNS {
			needsUpdate = true
		}
		if rc.MTU != oldMTU {
			needsUpdate = true
		}
		if (oldPKA == -1 && rc.PKA != 0) || (oldPKA != -1 && rc.PKA != oldPKA) {
			needsUpdate = true
		}
		if rc.ClientAllowedIPs != oldClientAIPs {
			needsUpdate = true
		}
		if clientAddr != oldClientAddr {
			needsUpdate = true
		}

		if !needsUpdate {
			continue
		}

		keyShort := p.PublicKey
		if len(keyShort) > 12 {
			keyShort = keyShort[:12] + "…"
		}

		fmt.Printf("  peer %d (%s %q):\n", p.ID, keyShort, p.FriendlyName)
		if rc.DNS != oldDNS {
			fmt.Printf("    client_dns: %q → %q\n", oldDNS, rc.DNS)
		}
		if rc.MTU != oldMTU {
			fmt.Printf("    client_mtu: %d → %d\n", oldMTU, rc.MTU)
		}
		if (oldPKA == -1 && rc.PKA != 0) || (oldPKA != -1 && rc.PKA != oldPKA) {
			fmt.Printf("    persistent_keepalive: %v → %d\n", p.PKA, rc.PKA)
		}
		if rc.ClientAllowedIPs != oldClientAIPs {
			fmt.Printf("    client_allowed_ips: %q → %q\n", oldClientAIPs, rc.ClientAllowedIPs)
		}
		if clientAddr != oldClientAddr {
			fmt.Printf("    client_address: %q → %q\n", oldClientAddr, clientAddr)
		}

		if *dryRun {
			updated++
			continue
		}

		// Write resolved values.
		var mtuVal interface{}
		if rc.MTU != 0 {
			mtuVal = rc.MTU
		}
		var pkaVal interface{}
		pkaVal = rc.PKA

		_, err := db.Exec(`UPDATE peers SET client_dns = ?, client_mtu = ?, persistent_keepalive = ?, 
			client_allowed_ips = ?, client_address = ? WHERE id = ?`,
			rc.DNS, mtuVal, pkaVal, rc.ClientAllowedIPs, clientAddr, p.ID)
		if err != nil {
			log.Printf("ERROR: updating peer %d: %v", p.ID, err)
			continue
		}
		updated++
	}

	if *dryRun {
		fmt.Printf("\n[DRY RUN] Would update %d of %d peers\n", updated, len(peers))
	} else {
		fmt.Printf("\nUpdated %d of %d peers\n", updated, len(peers))
	}
}
