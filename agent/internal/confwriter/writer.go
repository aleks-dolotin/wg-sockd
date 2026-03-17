// Package confwriter manages WireGuard configuration file generation with metadata comments.
// CRITICAL: The [Interface] section is READ-ONLY — agent never modifies Interface-level fields.
package confwriter

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// ConfFile represents a parsed WireGuard configuration file.
type ConfFile struct {
	PreInterfaceComments string     // Comments/blank lines before [Interface]
	InterfaceRaw         string     // Entire [Interface] section verbatim
	Peers                []ConfPeer // Parsed [Peer] sections (for reference only)
}

// ConfPeer represents a parsed [Peer] section from an existing conf.
type ConfPeer struct {
	PublicKey  string
	RawLines  string // Full section text
}

// PeerConf describes a peer to be written to the config file.
type PeerConf struct {
	PublicKey    string
	AllowedIPs  string
	PresharedKey string // optional, empty = omit
	FriendlyName string
	CreatedAt   time.Time
	Notes       string
}

// PeerMeta holds metadata extracted from wg-sockd comment lines.
type PeerMeta struct {
	Name      string
	CreatedAt string
	Notes     string
}

// ParseConf reads a WireGuard configuration file and separates it into
// pre-interface comments, the [Interface] section verbatim, and [Peer] sections.
// Returns an empty ConfFile if the file does not exist.
func ParseConf(path string) (*ConfFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ConfFile{}, nil
		}
		return nil, fmt.Errorf("reading conf: %w", err)
	}

	cf := &ConfFile{}
	content := string(data)
	if content == "" {
		return cf, nil
	}

	lines := strings.Split(content, "\n")

	type section int
	const (
		secPre section = iota
		secInterface
		secPeer
	)

	cur := secPre
	var preBuf, ifaceBuf, peerBuf strings.Builder
	var currentPeerKey string

	flushPeer := func() {
		if peerBuf.Len() > 0 {
			cf.Peers = append(cf.Peers, ConfPeer{
				PublicKey: currentPeerKey,
				RawLines:  peerBuf.String(),
			})
			peerBuf.Reset()
			currentPeerKey = ""
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "[Interface]" {
			if cur == secPeer {
				flushPeer()
			}
			cur = secInterface
			ifaceBuf.WriteString(line + "\n")
			continue
		}

		if trimmed == "[Peer]" {
			if cur == secPeer {
				flushPeer()
			}
			cur = secPeer
			peerBuf.WriteString(line + "\n")
			continue
		}

		switch cur {
		case secPre:
			preBuf.WriteString(line + "\n")
		case secInterface:
			ifaceBuf.WriteString(line + "\n")
		case secPeer:
			peerBuf.WriteString(line + "\n")
			if strings.HasPrefix(trimmed, "PublicKey") {
				parts := strings.SplitN(trimmed, "=", 2)
				if len(parts) == 2 {
					currentPeerKey = strings.TrimSpace(parts[1])
				}
			}
		}
	}

	flushPeer()

	cf.PreInterfaceComments = strings.TrimRight(preBuf.String(), "\n")
	cf.InterfaceRaw = strings.TrimRight(ifaceBuf.String(), "\n")

	return cf, nil
}

// WriteConf writes a WireGuard configuration file, preserving the existing
// [Interface] section and rewriting all [Peer] sections from the provided peers.
// Uses atomic write (temp file + rename) with 0600 permissions.
func WriteConf(path string, peers []PeerConf) error {
	existing, err := ParseConf(path)
	if err != nil {
		return err
	}

	var buf strings.Builder

	// Preserve pre-interface comments.
	if existing.PreInterfaceComments != "" {
		buf.WriteString(existing.PreInterfaceComments + "\n")
	}

	// Preserve [Interface] section verbatim.
	if existing.InterfaceRaw != "" {
		buf.WriteString(existing.InterfaceRaw + "\n")
	} else if len(peers) > 0 {
		// No [Interface] section found — conf file is missing or empty.
		// Agent does NOT generate [Interface] (F4), but warn about invalid state.
		log.Println("WARNING: writing peers to conf without [Interface] section — file will be invalid for wg-quick")
	}

	// Write [Peer] sections with metadata comments.
	for _, p := range peers {
		buf.WriteString("\n")

		// Metadata comments — sanitize to prevent config injection.
		if p.FriendlyName != "" {
			safeName := sanitizeConfValue(p.FriendlyName)
			fmt.Fprintf(&buf, "# wg-sockd:name=%s\n", safeName)
		}
		if !p.CreatedAt.IsZero() {
			fmt.Fprintf(&buf, "# wg-sockd:created=%s\n", p.CreatedAt.Format("2006-01-02"))
		}
		if p.Notes != "" {
			safeNotes := sanitizeConfValue(p.Notes)
			fmt.Fprintf(&buf, "# wg-sockd:notes=%s\n", safeNotes)
		}

		buf.WriteString("[Peer]\n")
		fmt.Fprintf(&buf, "PublicKey = %s\n", p.PublicKey)
		fmt.Fprintf(&buf, "AllowedIPs = %s\n", p.AllowedIPs)
		if p.PresharedKey != "" {
			fmt.Fprintf(&buf, "PresharedKey = %s\n", p.PresharedKey)
		}
	}

	// Atomic write: temp file + rename.
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(buf.String()), 0600); err != nil {
		return fmt.Errorf("writing temp conf: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath) // cleanup on failure
		return fmt.Errorf("renaming conf: %w", err)
	}

	return nil
}

// ParseConfComments scans a WireGuard config file for wg-sockd metadata comments
// and returns a map of PublicKey → PeerMeta.
// This is the disaster recovery mechanism — if SQLite is corrupted, metadata
// can be recovered from conf file comments.
func ParseConfComments(path string) (map[string]PeerMeta, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]PeerMeta{}, nil
		}
		return nil, fmt.Errorf("opening conf for comment parsing: %w", err)
	}
	defer func() { _ = f.Close() }()

	result := make(map[string]PeerMeta)
	var currentMeta PeerMeta
	hasMeta := false

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "# wg-sockd:") {
			kv := strings.TrimPrefix(line, "# wg-sockd:")
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				val := strings.TrimSpace(parts[1])
				switch key {
				case "name":
					currentMeta.Name = val
				case "created":
					currentMeta.CreatedAt = val
				case "notes":
					currentMeta.Notes = val
				}
				hasMeta = true
			}
			continue
		}

		if strings.HasPrefix(line, "PublicKey") && hasMeta {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				pubKey := strings.TrimSpace(parts[1])
				result[pubKey] = currentMeta
			}
			currentMeta = PeerMeta{}
			hasMeta = false
			continue
		}

		if line == "[Peer]" {
			// Reset meta if no wg-sockd comments preceded this peer.
			if !hasMeta {
				currentMeta = PeerMeta{}
			}
			continue
		}

		// Any non-comment, non-section line after [Peer] resets pending meta.
		if !strings.HasPrefix(line, "#") && line != "" && line != "[Interface]" &&
			hasMeta && !strings.HasPrefix(line, "PublicKey") {
			// Inside a peer section, meta already associated or not applicable.
			hasMeta = false
		}
	}

	return result, scanner.Err()
}

// sanitizeConfValue removes newlines and carriage returns from a string
// to prevent config injection via metadata comment values.
func sanitizeConfValue(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}
