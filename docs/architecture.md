# wg-sockd — Architecture

## System Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                        Linux Host                            │
│                                                              │
│  ┌──────────┐    netlink    ┌──────────────────────────────┐ │
│  │ WireGuard│◄─────────────►│       wg-sockd Agent         │ │
│  │  Kernel  │               │                              │ │
│  │  (wg1)   │               │  ┌─────────┐  ┌──────────┐  │ │
│  └──────────┘               │  │ SQLite  │  │   Conf   │  │ │
│                             │  │   DB    │  │  Writer  │  │ │
│                             │  └─────────┘  └──────────┘  │ │
│  ┌─────────────┐            │                              │ │
│  │  wg1.conf   │◄───────────│  Reconciler (30s loop)       │ │
│  │ [Interface] │            │                              │ │
│  │ [Peer] ...  │            └──────────────┬───────────────┘ │
│  └─────────────┘                           │                 │
│                                            │                 │
│  ┌─────────────────────────────────┐       │                 │
│  │  iptables FORWARD chain         │◄──────┘                 │
│  │  -I FORWARD 1 -i wg1            │   firewall.Sync/Apply   │
│  │    └─► WG_SOCKD_FORWARD         │                         │
│  │          ├─► WG_PEER_j6ImlvYq   │                         │
│  │          ├─► WG_PEER_U7crNLtT   │                         │
│  │          └─► WG_PEER_...        │                         │
│  └─────────────────────────────────┘                         │
│                                                              │
│                   Unix Socket (0660)                         │
│            /run/wg-sockd/wg-sockd.sock                       │
│                         │                                    │
│             ┌───────────┼───────────┐                        │
│             ▼           ▼           ▼                        │
│       ┌──────────┐ ┌────────┐ ┌──────────┐                  │
│       │   CLI    │ │  curl  │ │Embedded  │                  │
│       │wg-sockd- │ │        │ │   UI     │                  │
│       │   ctl    │ │        │ │ TCP:8080 │                  │
│       └──────────┘ └────────┘ └──────────┘                  │
└──────────────────────────────────────────────────────────────┘

Kubernetes Mode:
┌──────────────┐     hostPath     ┌──────────┐
│  UI Proxy    │────mount─────────│  Agent   │
│  Pod (Go)    │  Unix Socket     │  (host)  │
│  TCP:8080    │                  │          │
└──────┬───────┘                  └──────────┘
       │
   Browser
```

## Agent Internal Architecture

The agent (`agent/`) follows a clean layered architecture with Go's `internal/` package convention:

### Package Map

| Package | Path | Responsibility |
|---------|------|----------------|
| `main` | `agent/cmd/wg-sockd/` | Entry point, flag parsing, dependency wiring, Unix socket listener, optional embedded UI serving |
| `config` | `agent/internal/config/` | YAML config loading, CLI flag overrides, env var overrides, validation |
| `api` | `agent/internal/api/` | HTTP router (net/http), request/response types, handlers for peers/profiles/health/stats/QR |
| `storage` | `agent/internal/storage/` | SQLite database operations — peers CRUD, profiles CRUD, migrations, backup/recovery |
| `wireguard` | `agent/internal/wireguard/` | WireGuard kernel interface via wgctrl netlink — get/set device, peer operations |
| `confwriter` | `agent/internal/confwriter/` | Atomic wg1.conf writing — preserves [Interface], manages [Peer] sections with metadata comments, debounced writes |
| `reconciler` | `agent/internal/reconciler/` | Periodic kernel ↔ DB sync — detects unknown peers, removes unauthorized, updates stats, triggers firewall Apply/Remove |
| `firewall` | `agent/internal/firewall/` | Modular per-peer iptables enforcement — `Firewall` interface, `IptablesFirewall` driver, `NoopFirewall` driver |
| `health` | `agent/internal/health/` | Health check aggregator — WireGuard reachability, SQLite connectivity, disk space |
| `middleware` | `agent/internal/middleware/` | HTTP middleware — rate limiting (token bucket), request logging, security headers |
| `sockmon` | `agent/internal/sockmon/` | Socket file monitor — periodic existence check, self-healing re-creation |
| `metrics` | `agent/internal/metrics/` | Prometheus metrics collector — per-peer rx/tx bytes, handshake age, online status |
| `auth` | `agent/internal/auth/` | Authentication — basic auth, bearer token, WebAuthn/passkey, session store |

### Data Flow

1. **Peer Creation (zero exposure window):**
   Client → Unix Socket → `api.Router` → `api.HandleCreatePeer` → generate keypair → **`firewall.ApplyPeer`** (iptables rules before kernel exposure) → `wireguard.ConfigurePeers` (netlink) → `storage.CreatePeer` (SQLite) → `confwriter.Write` (wg1.conf) → Response with peer config + private key + QR

   Rollback order on failure: `firewall.RemovePeer` → `storage.DeletePeer` → `wireguard.RemovePeer` → `confwriter.Write`

2. **Peer Deletion:**
   Client → `api.HandleDeletePeer` → `reconciler.Pause()` → `wireguard.RemovePeer` (netlink) → `storage.DeletePeer` (SQLite) → **`firewall.RemovePeer`** (iptables cleanup) → `confwriter.Write` → `reconciler.Resume()`

   `Pause`/`Resume` prevents the reconciler from re-adding the peer during the delete window.

3. **Key Rotation:**
   Client → `api.HandleRotateKeys` → **`firewall.RemovePeer(oldPeer)`** → `wireguard.RemovePeer(oldKey)` → `wireguard.ConfigurePeers(newKey)` → `storage.UpdatePeerPublicKey` → **`firewall.ApplyPeer(newPeer)`** → `confwriter.Write`

   Chain name is derived from the public key, so rotation always produces a new `WG_PEER_*` chain name.

4. **Reconciliation (every 30s):**
   `reconciler.ReconcileOnce` → `wireguard.GetDevice` (kernel state) → compare with `storage.ListPeers` (DB state):
   - Zombie peer (disabled in DB, present in kernel) → `wireguard.RemovePeer` + **`firewall.RemovePeer`**
   - Missing peer (enabled in DB, absent in kernel) → `wireguard.ConfigurePeers` + **`firewall.ApplyPeer`**
   - Unknown peer (in kernel, not in DB) → `wireguard.RemovePeer` + `storage.UpsertPeer(auto_discovered=true)`
   - `confwriter.Write` to sync wg1.conf

5. **Startup Sync:**
   `main` → `reconciler.ReconcileOnce` → **`firewall.Sync(allPeers)`**:
   - Apply rules for all enabled peers
   - Remove rules for disabled peers
   - Clean up orphan `WG_PEER_*` chains (no matching DB peer)

### Firewall Architecture

The `firewall` package provides server-side per-peer destination filtering via iptables. It enforces `client_allowed_ips` at the kernel level, preventing a peer from reaching networks beyond what the admin has authorized — even if the peer modifies its local WireGuard config.

#### Chain Model

```
iptables FORWARD
  └─► WG_SOCKD_FORWARD          (dispatch chain, one jump per peer)
        ├─► WG_PEER_j6ImlvYq    (peer: 10.0.10.2)
        │     ├─ ACCEPT -s 10.0.10.2 -d 10.0.0.0/24
        │     ├─ ACCEPT -s 10.0.10.2 -d 192.168.1.0/24
        │     └─ DROP
        ├─► WG_PEER_U7crNLtT    (peer: 10.0.10.3)
        │     └─ DROP            (empty client_allowed_ips → deny all)
        └─► WG_PEER_...
```

The jump rule is inserted at position 1 scoped to the WireGuard interface:
```
-I FORWARD 1 -i wg1 -j WG_SOCKD_FORWARD
```

Scoping to `-i wg1` ensures only WireGuard peer traffic is evaluated — all other traffic (Kubernetes pods, Docker containers, etc.) is unaffected regardless of position.

#### Chain Naming

Per-peer chain names are derived from the WireGuard public key:
```
WG_PEER_<first-8-alphanumeric-chars-of-base64-pubkey>
```

Base64 special characters (`+`, `/`, `=`) are skipped. A 44-character WireGuard key always yields at least 32 alphanumeric characters, so 8 are always available. Chain names are 16 characters total — within iptables' 28-character limit.

#### Drivers

| Driver | Behavior |
|--------|----------|
| `iptables` | Full per-peer enforcement via `os/exec` subprocess calls |
| `none` | No-op — all methods return nil, no kernel changes |

The driver is selected via `firewall.driver` in config; `none` is the escape hatch for environments where iptables is unavailable or undesired.

### IPv6 Leak Prevention

Optional feature controlled by `ipv6_prefix` in config. When set, wg-sockd derives a ULA IPv6 address from each peer's IPv4 `client_address` (e.g. `10.0.3.2` → `fd00:ab01::2/128`) and includes it in both the client and server WireGuard configs. Combined with `::/0` in the profile's `allowed_ips`, this captures all IPv6 traffic into the tunnel. An `ip6tables` DROP rule on the server prevents forwarding — effectively blocking IPv6 leaks without requiring end-to-end IPv6 connectivity.

The derivation is deterministic and requires no database changes — IPv6 addresses are computed at runtime from IPv4 + prefix.

#### Lifecycle Guarantees

- **Idempotent `ApplyPeer`:** Always flushes and recreates the per-peer chain — no diff logic, safe to call repeatedly
- **Rules survive shutdown:** No cleanup on SIGTERM — rules persist until the next `Sync` or explicit `RemovePeer`; for persistence across reboots use `iptables-persistent`
- **Orphan cleanup:** `Sync` detects and removes `WG_PEER_*` chains with no matching DB peer (from failed rotations, upgrades, manual edits)
- **Atomic DeletePeer:** `reconciler.Pause()`/`Resume()` wraps the delete sequence to prevent the reconciler from re-adding the peer mid-operation

### Resilience Features

| Feature | Implementation | Trigger |
|---------|---------------|---------|
| Rate Limiting | In-memory token bucket per connection, 10 req/s default | Every API request |
| Socket Self-Healing | 5s periodic check, re-create listener if missing | Socket file deleted or replaced |
| Debounced Conf Writing | 100ms coalesce window for rapid mutations | Multiple peer changes in quick succession |
| Graceful Degradation | Disk space check, 503 on writes, reads continue | Disk full condition |
| SQLite Backup | Hourly .db → .db.bak with fsync | Timer-based |
| SQLite Recovery | 3-level chain: backup → conf comments → clean start | DB corruption detected |
| Firewall Sync | Full rule reconciliation on startup | Agent start |
| Firewall Idempotency | Flush + recreate on every ApplyPeer | Peer create/update/approve/rotate |

## UI Proxy Architecture

The UI proxy (`ui/`) is a minimal Go reverse proxy for Kubernetes deployments:

- `ui/internal/proxy/` — HTTP reverse proxy that forwards requests to the agent's Unix socket
- `ui/internal/discovery/` — Socket file discovery and health checking
- `ui/cmd/` — Entry point with Docker-optimized configuration
- `ui/web/` — React SPA source (built separately, served as static files)

## Web UI Architecture

The React SPA (`ui/web/`) uses a modern component-based architecture:

- **Framework:** React 19 with JSX
- **Routing:** React Router DOM 7 — file-based page components in `src/pages/`
- **State:** TanStack React Query for server state (no client-side global store)
- **Styling:** TailwindCSS 4 + shadcn/ui (Radix primitives)
- **API Layer:** `src/api/` — fetch-based client for agent REST API
- **Components:** `src/components/` — Layout, ConnectionStatus, UnknownPeerAlert, shadcn ui primitives

## CLI Architecture

The CLI (`cmd/wg-sockd-ctl/`) is a single-file Go binary:

- `CGO_ENABLED=0` — fully static, no runtime dependencies
- Communicates via HTTP-over-Unix-socket to the agent
- Subcommands: `peers list|add|delete|approve|rotate-keys|get|update`, `profiles list|create|update|delete`, `health`, `stats`, `version`
- Supports custom socket path via `--socket` flag
- `--json` flag on all commands for machine-readable output

## Deployment Architectures

### Standalone (systemd)

- Binary: `/usr/local/bin/wg-sockd`
- Config: `/etc/wg-sockd/config.yaml`
- Database: `/var/lib/wg-sockd/wg-sockd.db`
- Socket: `/run/wg-sockd/wg-sockd.sock`
- Service: `wg-sockd.service` with `CAP_NET_ADMIN`, `ProtectSystem=strict`
- Optional: `--serve-ui` for embedded web interface on TCP :8080
- Management: separate port `:8090` for Prometheus metrics (`/management/prometheus`)

### Kubernetes (Helm)

- Agent: Installed directly on the node (via `install.sh`)
- UI Proxy: Kubernetes pod with hostPath mount to Unix socket (`/run/wg-sockd/`)
- Node selector: `wg-sockd=active` label
- Security: `runAsGroup: 5000` matching host socket group (`wg-sockd`)
- Ingress: nginx-ingress with TLS termination
