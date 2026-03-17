// Package main is the entrypoint for wg-sockd-ctl, a thin CLI client for
// the wg-sockd agent API over Unix socket.
//
// Usage:
//
//	wg-sockd-ctl [--socket PATH] [--json] <command> [flags]
//
// Commands: peers list|add|delete|approve|get|update|rotate-keys, profiles list|create|update|delete, health, stats
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

// jsonOutput is a global flag parsed in main() alongside --socket and --version.
var jsonOutput bool

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

type UpdatePeerRequest struct {
	FriendlyName *string  `json:"friendly_name,omitempty"`
	AllowedIPs   []string `json:"allowed_ips,omitempty"`
	Profile      **string `json:"profile,omitempty"`
	Enabled      *bool    `json:"enabled,omitempty"`
	Notes        *string  `json:"notes,omitempty"`
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

type CreateProfileRequest struct {
	Name        string   `json:"name"`
	AllowedIPs  []string `json:"allowed_ips"`
	ExcludeIPs  []string `json:"exclude_ips,omitempty"`
	Description string   `json:"description,omitempty"`
}

type UpdateProfileRequest struct {
	AllowedIPs  []string `json:"allowed_ips,omitempty"`
	ExcludeIPs  []string `json:"exclude_ips,omitempty"`
	Description *string  `json:"description,omitempty"`
}

type HealthResponse struct {
	Status    string `json:"status"`
	WireGuard string `json:"wireguard"`
	SQLite    string `json:"sqlite"`
}

type StatsResponse struct {
	TotalPeers  int   `json:"total_peers"`
	OnlinePeers int   `json:"online_peers"`
	TotalRx     int64 `json:"total_rx"`
	TotalTx     int64 `json:"total_tx"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

func main() {
	socketPath := flag.String("socket", defaultSocket, "path to wg-sockd Unix socket")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.BoolVar(&jsonOutput, "json", false, "output in JSON format")
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
	case "health":
		err = healthCmd(client)
	case "stats":
		err = statsCmd(client)
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

Usage: wg-sockd-ctl [--socket PATH] [--json] <command> [flags]

Commands:
  peers list                                List all peers
  peers get --id ID                         Show peer details
  peers add --name NAME [--profile P]       Create a new peer
  peers update --id ID [--name N] [...]     Update a peer
  peers delete --id ID [--yes]              Delete a peer
  peers approve PUBKEY_PREFIX               Approve an auto-discovered peer
  peers rotate-keys --id ID [--yes]         Rotate peer keys
  profiles list                             List all profiles
  profiles create --name N --allowed-ips A  Create a profile
  profiles update --name N [--allowed-ips]  Update a profile
  profiles delete --name N [--yes]          Delete a profile
  health                                    Show agent health
  stats                                     Show aggregate stats
  version                                   Print version and exit

Flags:
  --socket PATH   Unix socket path (default: %s)
  --json          Output in JSON format
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

// writeJSON encodes v as JSON to stdout.
func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
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

func confirm(prompt string) bool {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes"
}

// --- Peers commands ---

func handlePeers(client *http.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing subcommand: list, get, add, update, delete, approve, rotate-keys")
	}

	switch args[0] {
	case "list":
		return peersList(client)
	case "get":
		return peersGet(client, args[1:])
	case "add":
		return peersAdd(client, args[1:])
	case "update":
		return peersUpdate(client, args[1:])
	case "delete":
		return peersDelete(client, args[1:])
	case "approve":
		return peersApprove(client, args[1:])
	case "rotate-keys":
		return peersRotateKeys(client, args[1:])
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

	if jsonOutput {
		return writeJSON(peers)
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

func peersGet(client *http.Client, args []string) error {
	fs := flag.NewFlagSet("peers get", flag.ExitOnError)
	id := fs.Int("id", 0, "peer ID (required)")
	_ = fs.Parse(args)

	if *id == 0 {
		return fmt.Errorf("--id is required")
	}

	resp, err := doRequest(client, http.MethodGet, fmt.Sprintf("/api/peers/%d", *id), nil)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkError(resp); err != nil {
		return err
	}

	var peer PeerResponse
	if err := json.NewDecoder(resp.Body).Decode(&peer); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	if jsonOutput {
		return writeJSON(peer)
	}

	status := "offline"
	if peer.LatestHandshake != nil && time.Since(*peer.LatestHandshake) < 3*time.Minute {
		status = "online"
	}
	if !peer.Enabled {
		status = "disabled"
	}

	profile := "—"
	if peer.Profile != nil {
		profile = *peer.Profile
	}

	handshake := "never"
	if peer.LatestHandshake != nil {
		handshake = peer.LatestHandshake.Format(time.RFC3339)
	}

	fmt.Printf("Name:           %s\n", peer.FriendlyName)
	fmt.Printf("ID:             %d\n", peer.ID)
	fmt.Printf("Public Key:     %s\n", peer.PublicKey)
	fmt.Printf("Status:         %s\n", status)
	fmt.Printf("Enabled:        %v\n", peer.Enabled)
	fmt.Printf("Profile:        %s\n", profile)
	fmt.Printf("Allowed IPs:    %s\n", strings.Join(peer.AllowedIPs, ", "))
	fmt.Printf("Endpoint:       %s\n", orDash(peer.Endpoint))
	fmt.Printf("Last Handshake: %s\n", handshake)
	fmt.Printf("Transfer:       ↓%s  ↑%s\n", humanBytes(peer.TransferRx), humanBytes(peer.TransferTx))
	if peer.AutoDiscovered {
		fmt.Println("Auto-discovered: yes")
	}

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

	if jsonOutput {
		return writeJSON(result)
	}

	fmt.Printf("✅ Peer created: %s\n\n", result.PublicKey)
	fmt.Println("--- Client Config ---")
	fmt.Println(result.Config)

	return nil
}

func peersUpdate(client *http.Client, args []string) error {
	fs := flag.NewFlagSet("peers update", flag.ExitOnError)
	id := fs.Int("id", 0, "peer ID (required)")
	name := fs.String("name", "", "new friendly name")
	profile := fs.String("profile", "", "new profile")
	allowedIPs := fs.String("allowed-ips", "", "new allowed IPs (comma-separated)")
	notes := fs.String("notes", "", "new notes")
	enable := fs.Bool("enable", false, "enable the peer")
	disable := fs.Bool("disable", false, "disable the peer")
	_ = fs.Parse(args)

	if *id == 0 {
		return fmt.Errorf("--id is required")
	}

	if *enable && *disable {
		return fmt.Errorf("--enable and --disable are mutually exclusive")
	}

	update := make(map[string]interface{})
	if *name != "" {
		update["friendly_name"] = *name
	}
	if *profile != "" {
		update["profile"] = *profile
	}
	if *allowedIPs != "" {
		update["allowed_ips"] = strings.Split(*allowedIPs, ",")
	}
	if *notes != "" {
		update["notes"] = *notes
	}
	if *enable {
		update["enabled"] = true
	}
	if *disable {
		update["enabled"] = false
	}

	if len(update) == 0 {
		return fmt.Errorf("no fields to update — specify at least one of --name, --profile, --allowed-ips, --notes, --enable, --disable")
	}

	body, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	resp, err := doRequest(client, http.MethodPut, fmt.Sprintf("/api/peers/%d", *id), strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkError(resp); err != nil {
		return err
	}

	var peer PeerResponse
	if err := json.NewDecoder(resp.Body).Decode(&peer); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	if jsonOutput {
		return writeJSON(peer)
	}

	fmt.Printf("✅ Peer %d updated\n", *id)
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
		if !confirm(fmt.Sprintf("Delete peer %d? [y/N] ", *id)) {
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

	if jsonOutput {
		return writeJSON(map[string]interface{}{"status": "deleted", "id": *id})
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

	if jsonOutput {
		return writeJSON(peer)
	}

	keyDisplay := peer.PublicKey
	if len(keyDisplay) > 12 {
		keyDisplay = keyDisplay[:12] + "…"
	}
	fmt.Printf("✅ Peer approved: %s (%s)\n", peer.FriendlyName, keyDisplay)
	return nil
}

func peersRotateKeys(client *http.Client, args []string) error {
	fs := flag.NewFlagSet("peers rotate-keys", flag.ExitOnError)
	id := fs.Int("id", 0, "peer ID (required)")
	yes := fs.Bool("yes", false, "skip confirmation prompt")
	_ = fs.Parse(args)

	if *id == 0 {
		return fmt.Errorf("--id is required")
	}

	if !*yes {
		if !confirm(fmt.Sprintf("Rotate keys for peer %d? Old keys will stop working immediately. [y/N] ", *id)) {
			fmt.Println("Cancelled")
			return nil
		}
	}

	resp, err := doRequest(client, http.MethodPost, fmt.Sprintf("/api/peers/%d/rotate-keys", *id), nil)
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

	if jsonOutput {
		return writeJSON(result)
	}

	fmt.Printf("✅ Keys rotated: %s\n\n", result.PublicKey)
	fmt.Println("⚠️  Save this config now — the private key won't be shown again.")
	fmt.Println()
	fmt.Println("--- Client Config ---")
	fmt.Println(result.Config)

	return nil
}

// --- Profiles commands ---

func handleProfiles(client *http.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing subcommand: list, create, update, delete")
	}

	switch args[0] {
	case "list":
		return profilesList(client)
	case "create":
		return profilesCreate(client, args[1:])
	case "update":
		return profilesUpdate(client, args[1:])
	case "delete":
		return profilesDelete(client, args[1:])
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

	if jsonOutput {
		return writeJSON(profiles)
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

func profilesCreate(client *http.Client, args []string) error {
	fs := flag.NewFlagSet("profiles create", flag.ExitOnError)
	name := fs.String("name", "", "profile name (required)")
	allowedIPs := fs.String("allowed-ips", "", "comma-separated allowed IPs (required)")
	excludeIPs := fs.String("exclude-ips", "", "comma-separated exclude IPs")
	description := fs.String("description", "", "profile description")
	_ = fs.Parse(args)

	if *name == "" || *allowedIPs == "" {
		return fmt.Errorf("--name and --allowed-ips are required")
	}

	req := CreateProfileRequest{
		Name:       *name,
		AllowedIPs: splitTrim(*allowedIPs),
		Description: *description,
	}
	if *excludeIPs != "" {
		req.ExcludeIPs = splitTrim(*excludeIPs)
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	resp, err := doRequest(client, http.MethodPost, "/api/profiles", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkError(resp); err != nil {
		return err
	}

	var profile ProfileResponse
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		// Some endpoints return 201 with no body
		if jsonOutput {
			return writeJSON(map[string]string{"status": "created", "name": *name})
		}
		fmt.Printf("✅ Profile %q created\n", *name)
		return nil
	}

	if jsonOutput {
		return writeJSON(profile)
	}

	fmt.Printf("✅ Profile %q created\n", *name)
	return nil
}

func profilesUpdate(client *http.Client, args []string) error {
	fs := flag.NewFlagSet("profiles update", flag.ExitOnError)
	name := fs.String("name", "", "profile name (required)")
	allowedIPs := fs.String("allowed-ips", "", "comma-separated allowed IPs")
	excludeIPs := fs.String("exclude-ips", "", "comma-separated exclude IPs")
	description := fs.String("description", "", "profile description")
	_ = fs.Parse(args)

	if *name == "" {
		return fmt.Errorf("--name is required")
	}

	update := make(map[string]interface{})
	if *allowedIPs != "" {
		update["allowed_ips"] = splitTrim(*allowedIPs)
	}
	if *excludeIPs != "" {
		update["exclude_ips"] = splitTrim(*excludeIPs)
	}
	if *description != "" {
		update["description"] = *description
	}

	if len(update) == 0 {
		return fmt.Errorf("no fields to update")
	}

	body, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	resp, err := doRequest(client, http.MethodPut, fmt.Sprintf("/api/profiles/%s", *name), strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkError(resp); err != nil {
		return err
	}

	var profile ProfileResponse
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		if jsonOutput {
			return writeJSON(map[string]string{"status": "updated", "name": *name})
		}
		fmt.Printf("✅ Profile %q updated\n", *name)
		return nil
	}

	if jsonOutput {
		return writeJSON(profile)
	}

	fmt.Printf("✅ Profile %q updated\n", *name)
	return nil
}

func profilesDelete(client *http.Client, args []string) error {
	fs := flag.NewFlagSet("profiles delete", flag.ExitOnError)
	name := fs.String("name", "", "profile name (required)")
	yes := fs.Bool("yes", false, "skip confirmation prompt")
	_ = fs.Parse(args)

	if *name == "" {
		return fmt.Errorf("--name is required")
	}

	if !*yes {
		if !confirm(fmt.Sprintf("Delete profile %q? [y/N] ", *name)) {
			fmt.Println("Cancelled")
			return nil
		}
	}

	resp, err := doRequest(client, http.MethodDelete, fmt.Sprintf("/api/profiles/%s", *name), nil)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkError(resp); err != nil {
		return err
	}

	if jsonOutput {
		return writeJSON(map[string]string{"status": "deleted", "name": *name})
	}

	fmt.Printf("✅ Profile %q deleted\n", *name)
	return nil
}

// --- Health & Stats ---

func healthCmd(client *http.Client) error {
	resp, err := doRequest(client, http.MethodGet, "/api/health", nil)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkError(resp); err != nil {
		return err
	}

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	if jsonOutput {
		return writeJSON(health)
	}

	fmt.Printf("Status:    %s\n", health.Status)
	fmt.Printf("WireGuard: %s\n", health.WireGuard)
	fmt.Printf("SQLite:    %s\n", health.SQLite)
	return nil
}

func statsCmd(client *http.Client) error {
	resp, err := doRequest(client, http.MethodGet, "/api/stats", nil)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkError(resp); err != nil {
		return err
	}

	var stats StatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	if jsonOutput {
		return writeJSON(stats)
	}

	fmt.Printf("Total Peers: %d\n", stats.TotalPeers)
	fmt.Printf("Online:      %d\n", stats.OnlinePeers)
	fmt.Printf("Total RX:    %s\n", humanBytes(stats.TotalRx))
	fmt.Printf("Total TX:    %s\n", humanBytes(stats.TotalTx))
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

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func splitTrim(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
