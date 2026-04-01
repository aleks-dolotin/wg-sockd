package firewall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/config"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/storage"
)

// fakeIptables writes a shell script to t.TempDir() that acts as a fake iptables binary.
// It logs all invocations to $IPTABLES_LOG.
// For -S queries, it outputs $IPTABLES_STUB file content (if set).
// Returns the path to the fake binary.
func fakeIptables(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "iptables")
	content := `#!/bin/sh
if [ -n "$IPTABLES_LOG" ]; then
  echo "$@" >> "$IPTABLES_LOG"
fi
# For -S queries, output stub file if set
for arg in "$@"; do
  if [ "$arg" = "-S" ]; then
    if [ -n "$IPTABLES_STUB" ] && [ -f "$IPTABLES_STUB" ]; then
      cat "$IPTABLES_STUB"
    fi
    exit 0
  fi
done
# -C (check) always returns 1 (rule not present) so -A is triggered
for arg in "$@"; do
  if [ "$arg" = "-C" ]; then
    exit 1
  fi
done
exit 0
`
	if err := os.WriteFile(script, []byte(content), 0755); err != nil {
		t.Fatalf("fakeIptables: write script: %v", err)
	}
	return script
}

// setFakeIptablesStub writes stub content to a temp file and sets IPTABLES_STUB env.
func setFakeIptablesStub(t *testing.T, content string) {
	t.Helper()
	stubFile := filepath.Join(t.TempDir(), "stub.txt")
	if err := os.WriteFile(stubFile, []byte(content), 0644); err != nil {
		t.Fatalf("setFakeIptablesStub: %v", err)
	}
	t.Setenv("IPTABLES_STUB", stubFile)
}

// logFile creates a temp log file path and sets IPTABLES_LOG env.
func logFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "iptables.log")
	t.Setenv("IPTABLES_LOG", path)
	return path
}

// readLog reads all logged iptables invocation lines.
func readLog(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("readLog: %v", err)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func testCfg() config.FirewallConfig {
	return config.FirewallConfig{
		Enabled:      true,
		Driver:       "iptables",
		ManagedChain: "WG_SOCKD_FORWARD",
		WGInterface:  "wg0",
	}
}

func makeFW(t *testing.T) (*IptablesFirewall, string) {
	t.Helper()
	bin := fakeIptables(t)
	lf := logFile(t)
	fw := &IptablesFirewall{cfg: testCfg(), iptablesPath: bin}
	return fw, lf
}

// --- Unit tests ---

func TestNoopFirewall_AllMethodsReturnNil(t *testing.T) {
	n := &NoopFirewall{}
	if err := n.Sync(nil); err != nil {
		t.Errorf("Sync: %v", err)
	}
	if err := n.ApplyPeer(storage.Peer{}); err != nil {
		t.Errorf("ApplyPeer: %v", err)
	}
	if err := n.RemovePeer(storage.Peer{}); err != nil {
		t.Errorf("RemovePeer: %v", err)
	}
}

func TestFactory_DisabledReturnsNoop(t *testing.T) {
	fw, err := New(config.FirewallConfig{Enabled: false, Driver: "iptables"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := fw.(*NoopFirewall); !ok {
		t.Errorf("expected NoopFirewall, got %T", fw)
	}
}

func TestFactory_NoneDriverReturnsNoop(t *testing.T) {
	fw, err := New(config.FirewallConfig{Enabled: true, Driver: "none"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := fw.(*NoopFirewall); !ok {
		t.Errorf("expected NoopFirewall, got %T", fw)
	}
}

func TestFactory_UnknownDriverReturnsError(t *testing.T) {
	_, err := New(config.FirewallConfig{Enabled: true, Driver: "nftables"})
	if err == nil {
		t.Fatal("expected error for unknown driver, got nil")
	}
}

func TestPeerChainName_AlphanumericOnly(t *testing.T) {
	// Key with +, /, = characters (typical base64 WireGuard key)
	key := "aB3+cD4/eF5=gH6+iJ7/kL8=mN9+oP0/qR1=sT2+uV3"
	name := peerChainName(key)
	if !strings.HasPrefix(name, "WG_PEER_") {
		t.Errorf("expected WG_PEER_ prefix, got %q", name)
	}
	suffix := name[len("WG_PEER_"):]
	if len(suffix) != 16 {
		t.Errorf("expected 16 char suffix, got %d: %q", len(suffix), suffix)
	}
	for _, c := range suffix {
		isLower := c >= 'a' && c <= 'z'
		isUpper := c >= 'A' && c <= 'Z'
		isDigit := c >= '0' && c <= '9'
		if !isLower && !isUpper && !isDigit {
			t.Errorf("non-alphanumeric char %q in chain name %q", c, name)
		}
	}
}

func TestPeerChainName_Deterministic(t *testing.T) {
	key := "aB3cD4eF5gH6iJ7kL8mN9oP0qR1sT2uV3wX4yZ5AB6C"
	first := peerChainName(key)
	second := peerChainName(key)
	if first != second {
		t.Errorf("peerChainName is not deterministic: %q != %q", first, second)
	}
}

func TestPeerChainName_DifferentKeys(t *testing.T) {
	k1 := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	k2 := "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB="
	if peerChainName(k1) == peerChainName(k2) {
		t.Error("different keys produced same chain name")
	}
}

func TestSourceCIDR(t *testing.T) {
	// sourceCIDR normalises client_address to /32 host CIDR.
	// This prevents iptables from collapsing "10.0.10.3/24" → "10.0.10.0/24"
	// which would make all peers in the subnet share one dispatch rule.
	tests := []struct {
		input string
		want  string
	}{
		{"10.8.0.5/32", "10.8.0.5/32"},
		{"192.168.1.10/24", "192.168.1.10/32"},
		{"10.0.10.3/24", "10.0.10.3/32"},
		{"", ""},
		{"10.0.0.1", "10.0.0.1"},
	}
	for _, tc := range tests {
		got := sourceCIDR(tc.input)
		if got != tc.want {
			t.Errorf("sourceCIDR(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestEnsureDispatchChain_CreatesChainBeforeJumpRule(t *testing.T) {
	// AC-14: -N must appear before -I FORWARD 1 in the log
	fw, lf := makeFW(t)
	if err := fw.ensureDispatchChain(); err != nil {
		t.Fatalf("ensureDispatchChain: %v", err)
	}
	lines := readLog(t, lf)
	nIdx, iIdx := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "-N") && strings.Contains(l, "WG_SOCKD_FORWARD") {
			nIdx = i
		}
		if strings.Contains(l, "-I") && strings.Contains(l, "FORWARD") && strings.Contains(l, "1") && strings.Contains(l, "WG_SOCKD_FORWARD") {
			iIdx = i
		}
	}
	if nIdx == -1 {
		t.Error("expected -N WG_SOCKD_FORWARD call")
	}
	if iIdx == -1 {
		t.Error("expected -I FORWARD 1 -i wg0 -j WG_SOCKD_FORWARD call")
	}
	if nIdx > iIdx && iIdx != -1 {
		t.Errorf("-N (idx %d) must appear before -I (idx %d)", nIdx, iIdx)
	}
}

func TestNewIptablesFirewall_NoJumpRuleOnConstruct(t *testing.T) {
	// MED-2: NewIptablesFirewall must NOT insert the FORWARD jump rule during construction.
	// The jump is deferred to the first ApplyPeer/Sync call so that the dispatch chain
	// is not active (and dropping packets) before per-peer chains are populated.
	bin := fakeIptables(t)
	lf := logFile(t)
	_, err := NewIptablesFirewall(testCfg(), bin)
	if err != nil {
		t.Fatalf("NewIptablesFirewall: %v", err)
	}
	lines := readLog(t, lf)
	for _, l := range lines {
		if strings.Contains(l, "-I") && strings.Contains(l, "FORWARD") {
			t.Errorf("expected NO -I FORWARD jump rule during NewIptablesFirewall, got: %s", l)
		}
	}
}

func TestApplyPeer_InsertJumpRuleAfterChainPopulated(t *testing.T) {
	// MED-2: the FORWARD jump rule must appear AFTER the peer chain rules are added.
	// This ensures that when the dispatch chain becomes active, the per-peer chain
	// is already fully populated (atomic activation, no empty-chain window).
	fw, lf := makeFW(t)
	peer := storage.Peer{
		PublicKey:        "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		ClientAddress:    "10.8.0.5/32",
		ClientAllowedIPs: "10.0.0.0/8",
		Enabled:          true,
	}
	if err := fw.ApplyPeer(peer); err != nil {
		t.Fatalf("ApplyPeer: %v", err)
	}
	lines := readLog(t, lf)

	// Find index of DROP rule (last rule added to peer chain) and -I FORWARD.
	dropIdx, insertIdx := -1, -1
	chainName := peerChainName(peer.PublicKey)
	for i, l := range lines {
		if strings.Contains(l, "-A") && strings.Contains(l, chainName) && strings.Contains(l, "DROP") {
			dropIdx = i
		}
		if strings.Contains(l, "-I") && strings.Contains(l, "FORWARD") && strings.Contains(l, "WG_SOCKD_FORWARD") {
			insertIdx = i
		}
	}
	if dropIdx == -1 {
		t.Error("expected DROP rule for peer chain")
	}
	if insertIdx == -1 {
		t.Error("expected -I FORWARD jump rule after ApplyPeer")
	}
	if dropIdx != -1 && insertIdx != -1 && dropIdx > insertIdx {
		t.Errorf("DROP rule (idx %d) must appear before -I FORWARD (idx %d) — chain must be populated before dispatch is activated", dropIdx, insertIdx)
	}
}

func TestApplyPeer_CreatesRules(t *testing.T) {
	// AC-3: chain contains ACCEPT rules + DROP + jump
	fw, lf := makeFW(t)
	peer := storage.Peer{
		PublicKey:        "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		ClientAddress:    "10.8.0.5/32",
		ClientAllowedIPs: "10.0.0.0/8, 192.168.1.0/24",
		Enabled:          true,
	}
	if err := fw.ApplyPeer(peer); err != nil {
		t.Fatalf("ApplyPeer: %v", err)
	}
	lines := readLog(t, lf)
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "ACCEPT") {
		t.Error("expected ACCEPT rules")
	}
	if !strings.Contains(joined, "DROP") {
		t.Error("expected DROP rule")
	}
	chainName := peerChainName(peer.PublicKey)
	if !strings.Contains(joined, chainName) {
		t.Errorf("expected chain %s in log", chainName)
	}
}

func TestApplyPeer_EmptyAllowedIPs_DropsAll(t *testing.T) {
	// AC-4: empty client_allowed_ips → only DROP rule, no ACCEPT
	fw, lf := makeFW(t)
	peer := storage.Peer{
		PublicKey:        "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		ClientAddress:    "10.8.0.5/32",
		ClientAllowedIPs: "",
		Enabled:          true,
	}
	if err := fw.ApplyPeer(peer); err != nil {
		t.Fatalf("ApplyPeer: %v", err)
	}
	lines := readLog(t, lf)
	joined := strings.Join(lines, "\n")

	if strings.Contains(joined, "ACCEPT") {
		t.Error("expected no ACCEPT rules for empty client_allowed_ips")
	}
	if !strings.Contains(joined, "DROP") {
		t.Error("expected DROP rule")
	}
}

func TestApplyPeer_EmptyClientAddress_Skipped(t *testing.T) {
	// AC-12: empty client_address → no iptables calls, no error
	fw, lf := makeFW(t)
	peer := storage.Peer{
		PublicKey:        "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		ClientAddress:    "",
		ClientAllowedIPs: "10.0.0.0/8",
		Enabled:          true,
	}
	if err := fw.ApplyPeer(peer); err != nil {
		t.Fatalf("ApplyPeer: %v", err)
	}
	lines := readLog(t, lf)
	// No iptables calls should have been made (ensureDispatchChain not called here)
	for _, l := range lines {
		if strings.Contains(l, peerChainName(peer.PublicKey)) {
			t.Errorf("expected no iptables calls for empty client_address, got: %s", l)
		}
	}
}

func TestApplyPeer_DisabledCallsRemove(t *testing.T) {
	// AC-7: disabled peer → RemovePeer path (-D, -F, -X)
	fw, lf := makeFW(t)
	peer := storage.Peer{
		PublicKey:     "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		ClientAddress: "10.8.0.5/32",
		Enabled:       false,
	}
	if err := fw.ApplyPeer(peer); err != nil {
		t.Fatalf("ApplyPeer disabled: %v", err)
	}
	lines := readLog(t, lf)
	joined := strings.Join(lines, "\n")
	// Should see -F and -X (flush + delete chain), no -A
	if !strings.Contains(joined, "-F") {
		t.Error("expected -F (flush) for disabled peer")
	}
	if !strings.Contains(joined, "-X") {
		t.Error("expected -X (delete) for disabled peer")
	}
}

func TestApplyPeer_Idempotent(t *testing.T) {
	// AC-6: calling ApplyPeer twice should not error; second call replaces rules
	fw, _ := makeFW(t)
	peer := storage.Peer{
		PublicKey:        "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		ClientAddress:    "10.8.0.5/32",
		ClientAllowedIPs: "10.0.0.0/8",
		Enabled:          true,
	}
	if err := fw.ApplyPeer(peer); err != nil {
		t.Fatalf("first ApplyPeer: %v", err)
	}
	peer.ClientAllowedIPs = "192.168.0.0/16"
	if err := fw.ApplyPeer(peer); err != nil {
		t.Fatalf("second ApplyPeer: %v", err)
	}
}

func TestRemovePeer_CleansUp(t *testing.T) {
	// AC-5: RemovePeer calls -D, -F, -X
	fw, lf := makeFW(t)
	peer := storage.Peer{
		PublicKey:     "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		ClientAddress: "10.8.0.5/32",
		Enabled:       true,
	}
	if err := fw.RemovePeer(peer); err != nil {
		t.Fatalf("RemovePeer: %v", err)
	}
	lines := readLog(t, lf)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "-D") {
		t.Error("expected -D (delete jump rule)")
	}
	if !strings.Contains(joined, "-F") {
		t.Error("expected -F (flush chain)")
	}
	if !strings.Contains(joined, "-X") {
		t.Error("expected -X (delete chain)")
	}
}

func TestSync_AppliesEnabledRemovesDisabled(t *testing.T) {
	// AC-8: Sync applies enabled, removes disabled
	fw, lf := makeFW(t)
	peers := []storage.Peer{
		{PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", ClientAddress: "10.8.0.2/32", ClientAllowedIPs: "10.0.0.0/8", Enabled: true},
		{PublicKey: "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=", ClientAddress: "10.8.0.3/32", ClientAllowedIPs: "10.0.0.0/8", Enabled: true},
		{PublicKey: "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC=", ClientAddress: "10.8.0.4/32", Enabled: false},
	}
	// Sync errors are non-fatal; the fake binary always exits 0 for most ops
	_ = fw.Sync(peers)

	lines := readLog(t, lf)
	joined := strings.Join(lines, "\n")

	// Enabled peers should have ACCEPT or DROP added
	if !strings.Contains(joined, "-A") {
		t.Error("expected -A calls for enabled peers")
	}
	// Disabled peer should trigger remove (-F, -X)
	if !strings.Contains(joined, "-X") {
		t.Error("expected -X for disabled peer")
	}
}

func TestSync_RemovesOrphanChains(t *testing.T) {
	// AC-17: orphan chain WG_PEER_orphan1 → flushed+deleted
	stubContent := "-N WG_SOCKD_FORWARD\n-N WG_PEER_orphan1\n-A FORWARD -j WG_SOCKD_FORWARD\n"
	setFakeIptablesStub(t, stubContent)

	fw, lf := makeFW(t)
	// Sync with no peers — all WG_PEER_* chains in stub are orphans
	_ = fw.Sync([]storage.Peer{})

	lines := readLog(t, lf)
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "WG_PEER_orphan1") {
		t.Error("expected orphan chain WG_PEER_orphan1 to be processed")
	}
}

func TestSync_OrphanCleanup_DeletesJumpRuleBeforeChain(t *testing.T) {
	// CRIT-1: orphan cleanup must remove the jump rule from dispatch chain before -X.
	// iptables refuses to delete a chain that is still referenced by another chain.
	// Stub: WG_PEER_Ab3cD4eF has a jump rule in WG_SOCKD_FORWARD.
	stubContent := strings.Join([]string{
		"-N WG_SOCKD_FORWARD",
		"-N WG_PEER_Ab3cD4eF",
		"-A WG_SOCKD_FORWARD -s 10.8.0.5/32 -j WG_PEER_Ab3cD4eF",
		"-A FORWARD -i wg0 -j WG_SOCKD_FORWARD",
	}, "\n") + "\n"
	setFakeIptablesStub(t, stubContent)

	fw, lf := makeFW(t)
	_ = fw.Sync([]storage.Peer{})

	lines := readLog(t, lf)
	joined := strings.Join(lines, "\n")

	// Must see -D WG_SOCKD_FORWARD ... -j WG_PEER_Ab3cD4eF before -X WG_PEER_Ab3cD4eF.
	dIdx, xIdx := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "-D") && strings.Contains(l, "WG_SOCKD_FORWARD") && strings.Contains(l, "WG_PEER_Ab3cD4eF") {
			dIdx = i
		}
		if strings.Contains(l, "-X") && strings.Contains(l, "WG_PEER_Ab3cD4eF") {
			xIdx = i
		}
	}
	if dIdx == -1 {
		t.Errorf("expected -D jump rule removal for WG_PEER_Ab3cD4eF, log:\n%s", joined)
	}
	if xIdx == -1 {
		t.Errorf("expected -X deletion of WG_PEER_Ab3cD4eF, log:\n%s", joined)
	}
	if dIdx != -1 && xIdx != -1 && dIdx > xIdx {
		t.Errorf("jump rule -D (idx %d) must appear before chain -X (idx %d)", dIdx, xIdx)
	}
}

func TestSync_OrphanCleanupRunsEvenOnApplyError(t *testing.T) {
	// AC-19: peer with empty client_address triggers skip (not an error that aborts),
	// but orphan cleanup still runs after.
	stubContent := "-N WG_SOCKD_FORWARD\n-N WG_PEER_orphan2\n-A FORWARD -j WG_SOCKD_FORWARD\n"
	setFakeIptablesStub(t, stubContent)

	fw, lf := makeFW(t)
	peers := []storage.Peer{
		// This peer has empty client_address → ApplyPeer skips (logs WARN, no error)
		{PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", ClientAddress: "", ClientAllowedIPs: "10.0.0.0/8", Enabled: true},
	}
	_ = fw.Sync(peers)

	lines := readLog(t, lf)
	joined := strings.Join(lines, "\n")

	// Orphan cleanup must still run
	if !strings.Contains(joined, "WG_PEER_orphan2") {
		t.Error("expected orphan cleanup to run even after ApplyPeer skip")
	}
}

func TestRotateKeys_ChainTransition(t *testing.T) {
	// AC-16: old chain removed, new chain created
	fw, lf := makeFW(t)

	oldPeer := storage.Peer{
		PublicKey:        "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		ClientAddress:    "10.8.0.5/32",
		ClientAllowedIPs: "10.0.0.0/8",
		Enabled:          true,
	}
	newPeer := storage.Peer{
		PublicKey:        "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
		ClientAddress:    "10.8.0.5/32",
		ClientAllowedIPs: "10.0.0.0/8",
		Enabled:          true,
	}

	// Simulate RotateKeys: remove old chain, apply new chain
	if err := fw.RemovePeer(oldPeer); err != nil {
		t.Fatalf("RemovePeer old: %v", err)
	}
	if err := fw.ApplyPeer(newPeer); err != nil {
		t.Fatalf("ApplyPeer new: %v", err)
	}

	lines := readLog(t, lf)
	joined := strings.Join(lines, "\n")

	oldChain := peerChainName(oldPeer.PublicKey)
	newChain := peerChainName(newPeer.PublicKey)

	if !strings.Contains(joined, oldChain) {
		t.Errorf("expected old chain %s to be referenced (remove)", oldChain)
	}
	if !strings.Contains(joined, newChain) {
		t.Errorf("expected new chain %s to be referenced (create)", newChain)
	}
}

func TestPeerChainName_SuffixIs16Chars(t *testing.T) {
	// EC-3: suffix must be exactly 16 alphanumeric chars (collision resistance).
	// Total chain name length = 8 ("WG_PEER_") + 16 = 24, within iptables 29-char limit.
	keys := []string{
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"aB3+cD4/eF5=gH6+iJ7/kL8=mN9+oP0/qR1=sT2+uV3",
		"BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
	}
	for _, key := range keys {
		name := peerChainName(key)
		suffix := name[len("WG_PEER_"):]
		if len(suffix) != 16 {
			t.Errorf("key %q: expected 16-char suffix, got %d: %q", key, len(suffix), suffix)
		}
		if len(name) > 29 {
			t.Errorf("key %q: chain name %q exceeds iptables 29-char limit (%d chars)", key, name, len(name))
		}
	}
}

func TestRemovePeer_EmptyClientAddress_FallbackScanDispatch(t *testing.T) {
	// EC-5: RemovePeer with empty ClientAddress must scan dispatch chain and remove
	// any jump rules referencing the peer's chain (prevents broken references).
	chainName := peerChainName("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	stubContent := strings.Join([]string{
		"-N WG_SOCKD_FORWARD",
		"-N " + chainName,
		"-A WG_SOCKD_FORWARD -s 10.8.0.5/32 -j " + chainName,
		"-A FORWARD -i wg0 -j WG_SOCKD_FORWARD",
	}, "\n") + "\n"
	setFakeIptablesStub(t, stubContent)

	fw, lf := makeFW(t)
	peer := storage.Peer{
		PublicKey:     "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		ClientAddress: "", // empty — triggers fallback scan
		Enabled:       true,
	}
	if err := fw.RemovePeer(peer); err != nil {
		t.Fatalf("RemovePeer: %v", err)
	}
	lines := readLog(t, lf)
	joined := strings.Join(lines, "\n")

	// Fallback must issue -D to remove the stale jump rule found in the dispatch chain.
	if !strings.Contains(joined, "-D") {
		t.Errorf("expected -D (delete stale jump rule) via fallback scan, log:\n%s", joined)
	}
	if !strings.Contains(joined, chainName) {
		t.Errorf("expected chain %s referenced in fallback cleanup, log:\n%s", chainName, joined)
	}
	// Chain must still be flushed and deleted.
	if !strings.Contains(joined, "-F") {
		t.Error("expected -F (flush chain)")
	}
	if !strings.Contains(joined, "-X") {
		t.Error("expected -X (delete chain)")
	}
}

