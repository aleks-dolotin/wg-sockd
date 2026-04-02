# wg-sockd — Server-Side IP Filtering

## Overview

wg-sockd enforces per-peer destination filtering via iptables. Without it, a WireGuard peer can rewrite its local config and route traffic to any network reachable from the server — the kernel only validates the source IP (`AllowedIPs = client_address/32`), not the destination.

The firewall feature restricts each peer to only the destination networks defined in its `client_allowed_ips` field.

## Chain Model

```
FORWARD
  └─► WG_SOCKD_FORWARD          (dispatch chain, position 1, scoped to -i <interface>)
        ├─► WG_PEER_j6ImlvYq    (peer 10.0.10.2)
        │     ├─ ACCEPT -s 10.0.10.2 -d 10.0.0.0/24
        │     ├─ ACCEPT -s 10.0.10.2 -d 192.168.1.0/24
        │     └─ DROP
        ├─► WG_PEER_U7crNLtT    (peer 10.0.10.3)
        │     └─ DROP            (empty client_allowed_ips = deny all)
        └─► ...
```

### Chain Naming

Per-peer chain names are derived from the WireGuard public key: first 8 alphanumeric characters (base64 `+`, `/`, `=` are skipped), prefixed with `WG_PEER_`. Example: public key `j6ImlvYqunT...` → `WG_PEER_j6ImlvYq`.

WireGuard keys (44 base64 chars) always contain at least 32 alphanumeric characters, so 8 are always available.

### Dispatch Chain Insertion

The jump rule is inserted at **position 1** in FORWARD, scoped to the WireGuard interface:

```
-I FORWARD 1 -i wg1 -j WG_SOCKD_FORWARD
```

This is critical. Without position 1 and interface scoping, `RELATED,ESTABLISHED` catch-all rules from Kubernetes (`KUBE-FORWARD`) or Docker would process all established connections before reaching `WG_SOCKD_FORWARD`, bypassing per-peer filtering entirely.

Scoping to `-i wg1` means only traffic arriving from the WireGuard interface enters `WG_SOCKD_FORWARD` — all other traffic (pods, containers, LAN) is completely unaffected.

## Lifecycle Integration

| Event | Firewall action |
|-------|----------------|
| `CreatePeer` | `ApplyPeer` called **before** `ConfigurePeers` (zero exposure window) |
| `ApprovePeer` | `ApplyPeer` called after peer is enabled |
| `UpdatePeer` (client_address / client_allowed_ips / enabled changed) | `ApplyPeer` called |
| `UpdatePeer` (enabled → false) | `ApplyPeer` delegates to `RemovePeer` |
| `DeletePeer` | `RemovePeer` called after DB delete |
| `RotateKeys` | `RemovePeer(old)` before DB update, `ApplyPeer(new)` after |
| Startup | `fw.Sync(allPeers)` — applies enabled, removes disabled, cleans orphans |
| Reconciler zombie | `RemovePeer` after wgctrl removal |
| Reconciler re-add | `ApplyPeer` after wgctrl re-add |

### Zero Exposure Window

`ApplyPeer` is called before `ConfigurePeers` during peer creation. The peer is never live in WireGuard kernel without firewall rules already in place.

### Idempotency

`ApplyPeer` always flushes and recreates the per-peer chain. Calling it twice with different CIDRs produces rules reflecting only the second call — no accumulation, no duplicates.

### Orphan Cleanup

`Sync` scans `iptables -S` for `WG_PEER_*` chains with no corresponding DB peer and removes them. This handles: failed `RemovePeer`, upgrade from version without firewall, failed `RotateKeys` mid-way.

Orphan cleanup always runs even if individual `ApplyPeer` calls fail.

## Configuration

```yaml
firewall:
  enabled: true           # default: true
  driver: iptables        # "iptables" | "none" (alias for disabled)
  managed_chain: WG_SOCKD_FORWARD  # dispatch chain name
```

The `WGInterface` field (the WireGuard interface name for the `-i` scoping rule) is taken from the top-level `interface:` field in config — it is not settable separately.

## Rule Persistence

iptables rules survive wg-sockd restarts but are lost on system reboot (default iptables behavior).

On every startup, `fw.Sync()` re-applies all rules from the database. As long as wg-sockd starts before WireGuard clients connect, there is no exposure gap.

For environments requiring persistence across reboots:

```bash
sudo apt install iptables-persistent
sudo netfilter-persistent save
```

Or ensure `wg-sockd.service` has ordering before `wg-quick@.service`.

No `defer firewall.Teardown()` on SIGTERM — rules are intentionally kept active when the agent stops.

## WireGuard PostUp Compatibility

Broad ACCEPT rules in `PostUp` shadow `WG_SOCKD_FORWARD`:

```ini
# ❌ Incompatible — bypasses per-peer filtering
PostUp = iptables -A FORWARD -i %i -j ACCEPT
PostDown = iptables -D FORWARD -i %i -j ACCEPT
```

Remove the `-i %i` lines. Keep only the outbound rule for return traffic:

```ini
# ✅ Compatible
PostUp = iptables -A FORWARD -o %i -j ACCEPT
PostDown = iptables -D FORWARD -o %i -j ACCEPT
```

## Compatibility with Other Services

| Service | Impact | Notes |
|---------|--------|-------|
| Kubernetes (kube-router, KUBE-FORWARD) | None | `-I FORWARD 1 -i <wg>` runs before KUBE-FORWARD ESTABLISHED rules |
| Docker | None | Non-WireGuard traffic unaffected by `-i <wg>` scoping |
| firewalld | Compatible | firewalld uses nftables or iptables-legacy; chains coexist |
| ufw | Compatible | ufw FORWARD rules are unaffected by WG-scoped chain |
| nftables | Not applicable | iptables driver does not interact with nftables tables |

## Disabling the Firewall

```yaml
firewall:
  enabled: false
```

With `enabled: false`, a `NoopFirewall` is used — all methods are no-ops returning nil. No iptables calls are made. Existing chains from a previous run are not cleaned up on disable.

## Diagnostics

```bash
# Check jump rule position (should be first or near-first for -i wg*)
sudo iptables -S FORWARD

# Check dispatch chain (one rule per enabled peer)
sudo iptables -L WG_SOCKD_FORWARD -v -n

# Check per-peer rules (non-zero pkts = traffic is being filtered)
sudo iptables -L WG_PEER_xxxxxxxx -v -n

# List all WG_PEER chains
sudo iptables -S | grep WG_PEER
```

Non-zero packet counters on `WG_SOCKD_FORWARD` confirm that traffic is reaching the firewall. Zero counters after known traffic indicate the jump rule is being bypassed (check for broad ACCEPT rules earlier in FORWARD).

## IPv6 Leak Prevention

The firewall package manages only IPv4 iptables rules. IPv6 filtering is handled separately via the `ipv6_prefix` config option, which prevents IPv6 traffic from leaking outside the tunnel.

When `ipv6_prefix` is configured, wg-sockd derives a ULA IPv6 address for each peer from their IPv4 `client_address` and includes `::/0` in the client's `AllowedIPs` (if the profile includes it). All IPv6 traffic enters the tunnel, where it is dropped by an `ip6tables` rule on the server.

The `ip6tables` DROP rule is not managed by wg-sockd — it must be configured as infrastructure (e.g. in `wg1.conf` PostUp):

```ini
PostUp = ip6tables -A FORWARD -i %i -j DROP
PostDown = ip6tables -D FORWARD -i %i -j DROP
```

See the [Deployment Guide](deployment-guide.md#ipv6-leak-prevention) for full setup instructions.
