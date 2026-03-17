package confwriter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sampleConf = `# Server config
[Interface]
Address = 10.0.0.1/24
ListenPort = 51820
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
PostUp = iptables -A FORWARD -i %i -j ACCEPT; iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE
PostDown = iptables -D FORWARD -i %i -j ACCEPT; iptables -t nat -D POSTROUTING -o eth0 -j MASQUERADE

[Peer]
PublicKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=
AllowedIPs = 10.0.0.2/32
`

func writeTestConf(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "wg0.conf")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseConf_WithPostUpPostDown(t *testing.T) {
	path := writeTestConf(t, sampleConf)
	cf, err := ParseConf(path)
	if err != nil {
		t.Fatalf("ParseConf: %v", err)
	}

	if !strings.Contains(cf.InterfaceRaw, "PostUp") {
		t.Error("InterfaceRaw should contain PostUp")
	}
	if !strings.Contains(cf.InterfaceRaw, "PostDown") {
		t.Error("InterfaceRaw should contain PostDown")
	}
	if !strings.Contains(cf.InterfaceRaw, "PrivateKey") {
		t.Error("InterfaceRaw should contain PrivateKey")
	}
	if len(cf.Peers) != 1 {
		t.Errorf("expected 1 peer, got %d", len(cf.Peers))
	}
}

func TestParseConf_FileNotFound(t *testing.T) {
	cf, err := ParseConf("/nonexistent/wg0.conf")
	if err != nil {
		t.Fatalf("should not error on missing file: %v", err)
	}
	if cf.InterfaceRaw != "" {
		t.Error("InterfaceRaw should be empty for missing file")
	}
}

func TestParseConf_EmptyFile(t *testing.T) {
	path := writeTestConf(t, "")
	cf, err := ParseConf(path)
	if err != nil {
		t.Fatalf("ParseConf: %v", err)
	}
	if cf.InterfaceRaw != "" {
		t.Error("InterfaceRaw should be empty for empty file")
	}
	if len(cf.Peers) != 0 {
		t.Error("Peers should be empty for empty file")
	}
}

func TestParseConf_InterfaceOnly(t *testing.T) {
	conf := `[Interface]
Address = 10.0.0.1/24
ListenPort = 51820
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
`
	path := writeTestConf(t, conf)
	cf, err := ParseConf(path)
	if err != nil {
		t.Fatalf("ParseConf: %v", err)
	}
	if !strings.Contains(cf.InterfaceRaw, "[Interface]") {
		t.Error("InterfaceRaw should contain [Interface]")
	}
	if len(cf.Peers) != 0 {
		t.Error("Peers should be empty for Interface-only conf")
	}
}

func TestWriteConf_PreservesPostUpPostDown(t *testing.T) {
	path := writeTestConf(t, sampleConf)

	created := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	peers := []PeerConf{
		{
			PublicKey:     "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC=",
			AllowedIPs:    "10.0.0.3/32",
			FriendlyName:  "Phone",
			CreatedAt:     created,
			Notes:         "Personal phone",
		},
	}

	err := WriteConf(path, peers)
	if err != nil {
		t.Fatalf("WriteConf: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	// PostUp/PostDown must be preserved.
	if !strings.Contains(content, "PostUp") {
		t.Error("PostUp should be preserved")
	}
	if !strings.Contains(content, "PostDown") {
		t.Error("PostDown should be preserved")
	}
	// Old peer should be gone, new peer present.
	if strings.Contains(content, "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=") {
		t.Error("old peer should be removed")
	}
	if !strings.Contains(content, "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC=") {
		t.Error("new peer should be present")
	}
	// Metadata comments present.
	if !strings.Contains(content, "# wg-sockd:name=Phone") {
		t.Error("name metadata should be present")
	}
	if !strings.Contains(content, "# wg-sockd:created=2026-03-15T") {
		t.Error("created metadata should be present")
	}
	if !strings.Contains(content, "# wg-sockd:notes=Personal phone") {
		t.Error("notes metadata should be present")
	}
}

func TestWriteConf_EmptyConf_CreatesFresh(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wg0.conf")

	peers := []PeerConf{
		{
			PublicKey:    "DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD=",
			AllowedIPs:   "10.0.0.4/32",
			FriendlyName: "Test",
		},
	}

	err := WriteConf(path, peers)
	if err != nil {
		t.Fatalf("WriteConf: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "[Peer]") {
		t.Error("should contain [Peer] section")
	}
	if !strings.Contains(content, "DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD=") {
		t.Error("should contain peer public key")
	}
}

func TestWriteConf_InterfaceOnly_AddFirstPeer(t *testing.T) {
	conf := `[Interface]
Address = 10.0.0.1/24
ListenPort = 51820
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
`
	path := writeTestConf(t, conf)

	peers := []PeerConf{
		{PublicKey: "FIRST_PEER_KEY", AllowedIPs: "10.0.0.2/32", FriendlyName: "First"},
	}

	err := WriteConf(path, peers)
	if err != nil {
		t.Fatalf("WriteConf: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)

	if !strings.Contains(content, "[Interface]") {
		t.Error("Interface section should be preserved")
	}
	if !strings.Contains(content, "FIRST_PEER_KEY") {
		t.Error("first peer should be added")
	}
}

func TestWriteConf_AtomicWrite(t *testing.T) {
	path := writeTestConf(t, sampleConf)

	err := WriteConf(path, []PeerConf{})
	if err != nil {
		t.Fatalf("WriteConf: %v", err)
	}

	// Temp file should not exist after successful write.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp file should be removed after atomic write")
	}

	// File should have 0600 permissions.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("expected permissions 0600, got %04o", perm)
	}
}

func TestParseConfComments_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wg0.conf")

	// Write initial Interface section.
	iface := `[Interface]
Address = 10.0.0.1/24
`
	os.WriteFile(path, []byte(iface), 0600)

	created := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	peers := []PeerConf{
		{PublicKey: "KEY_A", AllowedIPs: "10.0.0.2/32", FriendlyName: "Alice", CreatedAt: created, Notes: "laptop"},
		{PublicKey: "KEY_B", AllowedIPs: "10.0.0.3/32", FriendlyName: "Bob", CreatedAt: created, Notes: "phone"},
	}

	err := WriteConf(path, peers)
	if err != nil {
		t.Fatalf("WriteConf: %v", err)
	}

	// Parse comments back.
	meta, err := ParseConfComments(path)
	if err != nil {
		t.Fatalf("ParseConfComments: %v", err)
	}

	if len(meta) != 2 {
		t.Fatalf("expected 2 peer metadata entries, got %d", len(meta))
	}

	a, ok := meta["KEY_A"]
	if !ok {
		t.Fatal("KEY_A not found in metadata")
	}
	if a.Name != "Alice" {
		t.Errorf("KEY_A name: got %q, want %q", a.Name, "Alice")
	}
	if a.Notes != "laptop" {
		t.Errorf("KEY_A notes: got %q, want %q", a.Notes, "laptop")
	}
	if !strings.HasPrefix(a.CreatedAt, "2026-03-15T") {
		t.Errorf("KEY_A created: got %q, want prefix %q", a.CreatedAt, "2026-03-15T")
	}

	b := meta["KEY_B"]
	if b.Name != "Bob" {
		t.Errorf("KEY_B name: got %q, want %q", b.Name, "Bob")
	}
}

func TestParseConfComments_FileNotFound(t *testing.T) {
	meta, err := ParseConfComments("/nonexistent/wg0.conf")
	if err != nil {
		t.Fatalf("should not error on missing file: %v", err)
	}
	if len(meta) != 0 {
		t.Error("should return empty map for missing file")
	}
}

func TestWriteConf_PresharedKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wg0.conf")

	peers := []PeerConf{
		{PublicKey: "KEY_PSK", AllowedIPs: "10.0.0.5/32", PresharedKey: "PSK_VALUE_HERE"},
	}

	err := WriteConf(path, peers)
	if err != nil {
		t.Fatalf("WriteConf: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "PresharedKey = PSK_VALUE_HERE") {
		t.Error("PresharedKey should be present when provided")
	}
}
