// Package main is the entrypoint for wg-sockd-ctl, a thin CLI client for
// the wg-sockd agent API over Unix socket.
//
// Usage:
//
//	wg-sockd-ctl [--socket PATH] <command> [flags]
//
// Commands: peers list, peers add, peers delete, peers approve, profiles list
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

const defaultSocket = "/var/run/wg-sockd/wg-sockd.sock"

// --- API types (standalone, no shared code with agent) ---

type PeerResponse struct {
	ID              int64      `json:"id"`
	PublicKey       string     `json:"public_key"`
	FriendlyName    string     `json:"friendly_name"`
	AllowedIPs      []string   `json:"allowed_ips"`
	Profile         *string    `json:"profile,omitempty"`
	Enabled         bool       `json:"enabled"`
	AutoDiscovered  bool       `json:"auto_discovered"`
	Endpoint        string     `json:"endpoint,omitempty"`
	LatestHandshake *time.Time `json:"latest_handshake,omitempty"`
	TransferRx      int64      `json:"transfer_rx"`
	TransferTx      int64      `json:"transfer_tx"`
}

type CreatePeerRequest struct {
	FriendlyName string   `json:"friendly_name"`
	AllowedIPs   []string `json:"allowed_ips,omitempty"`
	Profile      *string  `json:"profile,omitempty"`
}

type PeerConfResponse struct {
	PublicKey string `json:"public_key"`
	Config   string `json:"config"`
}

type ProfileResponse struct {
	Name               string   `json:"name"`
	AllowedIPs         []string `json:"allowed_ips"`
	ExcludeIPs         []string `json:"exclude_ips"`
	ResolvedAllowedIPs []string `json:"resolved_allowed_ips"`
	Description        string   `json:"description,omitempty"`
	IsDefault          bool     `json:"is_default"`
	PeerCount          int      `json:"peer_count"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

func main() {
	socketPath := flag.String("socket", defaultSocket, "path to wg-sockd Unix socket")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Usage = usage
	flag.Parse()

	if *showVersion {
		printVersion()
		os.Exit(0)
	}

	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(1)
	}

	// Handle "version" subcommand (AC-43: same output as --version).
	if args[0] == "version" {
		printVersion()
		os.Exit(0)
	}

	client := newUnixClient(*socketPath)

	var err error
	switch args[0] {
	case "peers":
		err = handlePeers(client, args[1:])
	case "profiles":
		err = handleProfiles(client, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", args[0])
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `wg-sockd-ctl — CLI for wg-sockd agent

Usage: wg-sockd-ctl [--socket PATH] <command> [flags]

Commands:
  peers list                            List all peers
  peers add --name NAME [--profile P]   Create a new peer
  peers delete --id ID [--yes]          Delete a peer
  peers approve PUBKEY_PREFIX           Approve an auto-discovered peer
  profiles list                         List all profiles
  version                               Print version and exit

Flags:
  --socket PATH   Unix socket path (default: %s)
  --version       Print version and exit

`, defaultSocket)
}

func printVersion() {
	v := version
	if buildTags != "" {
		v += "+" + buildTags
	}
	fmt.Printf("wg-sockd-ctl %s (commit: %s, built: %s)\n", v, commit, buildDate)
}

// --- HTTP client over Unix socket ---

func newUnixClient(socketPath string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: 10 * time.Second,
	}
}

func doRequest(client *http.Client, method, path string, body io.Reader) (*http.Response, error) {
	url := "http://localhost" + path
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return client.Do(req)
}

func checkError(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	var apiErr ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err == nil && apiErr.Error != "" {
		if apiErr.Message != "" {
			return fmt.Errorf("%s: %s (HTTP %d)", apiErr.Error, apiErr.Message, resp.StatusCode)
		}
		return fmt.Errorf("%s (HTTP %d)", apiErr.Error, resp.StatusCode)
	}
	return fmt.Errorf("HTTP %d", resp.StatusCode)
}

// --- Peers commands ---

func handlePeers(client *http.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing subcommand: list, add, delete, approve")
	}

	switch args[0] {
	case "list":
		return peersList(client)
	case "add":
		return peersAdd(client, args[1:])
	case "delete":
		return peersDelete(client, args[1:])
	case "approve":
		return peersApprove(client, args[1:])
	default:
		return fmt.Errorf("unknown peers subcommand: %s", args[0])
	}
}

func peersList(client *http.Client) error {
	resp, err := doRequest(client, http.MethodGet, "/api/peers", nil)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkError(resp); err != nil {
		return err
	}

	var peers []PeerResponse
	if err := json.NewDecoder(resp.Body).Decode(&peers); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tPUBLIC_KEY\tALLOWED_IPS\tSTATUS\tRX\tTX")

	for _, p := range peers {
		name := p.FriendlyName
		if p.AutoDiscovered {
			name += " [auto]"
		}

		key := p.PublicKey
		if len(key) > 12 {
			key = key[:12] + "…"
		}

		ips := strings.Join(p.AllowedIPs, ",")

		status := "offline"
		if p.LatestHandshake != nil && time.Since(*p.LatestHandshake) < 3*time.Minute {
			status = "online"
		}
		if !p.Enabled {
			status = "disabled"
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			name, key, ips, status,
			humanBytes(p.TransferRx), humanBytes(p.TransferTx))
	}

	_ = w.Flush()
	return nil
}

func peersAdd(client *http.Client, args []string) error {
	fs := flag.NewFlagSet("peers add", flag.ExitOnError)
	name := fs.String("name", "", "friendly name for the peer (required)")
	profile := fs.String("profile", "", "profile name")
	allowedIPs := fs.String("allowed-ips", "", "comma-separated allowed IPs (alternative to --profile)")
	_ = fs.Parse(args)

	if *name == "" {
		return fmt.Errorf("--name is required")
	}

	req := CreatePeerRequest{FriendlyName: *name}
	if *profile != "" {
		req.Profile = profile
	} else if *allowedIPs != "" {
		req.AllowedIPs = strings.Split(*allowedIPs, ",")
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}
	resp, err := doRequest(client, http.MethodPost, "/api/peers", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkError(resp); err != nil {
		return err
	}

	var result PeerConfResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	fmt.Printf("✅ Peer created: %s\n\n", result.PublicKey)
	fmt.Println("--- Client Config ---")
	fmt.Println(result.Config)

	return nil
}

func peersDelete(client *http.Client, args []string) error {
	fs := flag.NewFlagSet("peers delete", flag.ExitOnError)
	id := fs.Int("id", 0, "peer ID to delete (required)")
	yes := fs.Bool("yes", false, "skip confirmation prompt")
	_ = fs.Parse(args)

	if *id == 0 {
		return fmt.Errorf("--id is required")
	}

	if !*yes {
		fmt.Printf("Delete peer %d? [y/N] ", *id)
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Cancelled")
			return nil
		}
	}

	resp, err := doRequest(client, http.MethodDelete, fmt.Sprintf("/api/peers/%d", *id), nil)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkError(resp); err != nil {
		return err
	}

	fmt.Printf("✅ Peer %d deleted\n", *id)
	return nil
}

func peersApprove(client *http.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: peers approve <pubkey_prefix>")
	}
	prefix := args[0]

	if len(prefix) < 4 {
		return fmt.Errorf("public key prefix must be at least 4 characters (got %d)", len(prefix))
	}

	// First, find the peer by pubkey prefix
	resp, err := doRequest(client, http.MethodGet, "/api/peers", nil)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkError(resp); err != nil {
		return err
	}

	var peers []PeerResponse
	if err := json.NewDecoder(resp.Body).Decode(&peers); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	var matches []PeerResponse
	for _, p := range peers {
		if strings.HasPrefix(p.PublicKey, prefix) {
			matches = append(matches, p)
		}
	}

	if len(matches) == 0 {
		return fmt.Errorf("no peer found with public key starting with %q", prefix)
	}
	if len(matches) > 1 {
		fmt.Fprintf(os.Stderr, "Multiple peers match prefix %q:\n", prefix)
		for _, m := range matches {
			fmt.Fprintf(os.Stderr, "  ID=%d  key=%s  name=%s\n", m.ID, m.PublicKey, m.FriendlyName)
		}
		return fmt.Errorf("ambiguous prefix, provide more characters")
	}

	peer := matches[0]
	approveResp, err := doRequest(client, http.MethodPost, fmt.Sprintf("/api/peers/%d/approve", peer.ID), nil)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = approveResp.Body.Close() }()

	if err := checkError(approveResp); err != nil {
		return err
	}

	keyDisplay := peer.PublicKey
	if len(keyDisplay) > 12 {
		keyDisplay = keyDisplay[:12] + "…"
	}
	fmt.Printf("✅ Peer approved: %s (%s)\n", peer.FriendlyName, keyDisplay)
	return nil
}

// --- Profiles commands ---

func handleProfiles(client *http.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing subcommand: list")
	}

	switch args[0] {
	case "list":
		return profilesList(client)
	default:
		return fmt.Errorf("unknown profiles subcommand: %s", args[0])
	}
}

func profilesList(client *http.Client) error {
	resp, err := doRequest(client, http.MethodGet, "/api/profiles", nil)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkError(resp); err != nil {
		return err
	}

	var profiles []ProfileResponse
	if err := json.NewDecoder(resp.Body).Decode(&profiles); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tALLOWED_IPS\tPEERS\tDEFAULT")

	for _, p := range profiles {
		ips := strings.Join(p.ResolvedAllowedIPs, ",")
		if len(ips) > 50 {
			ips = ips[:50] + "…"
		}
		def := ""
		if p.IsDefault {
			def = "✓"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%d\t%s\n",
			p.Name, ips, p.PeerCount, def)
	}

	_ = w.Flush()
	return nil
}

// --- Helpers ---

func humanBytes(b int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.1fG", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1fM", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.1fK", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

