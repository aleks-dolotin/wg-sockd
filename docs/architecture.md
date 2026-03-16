# wg-sockd — Architecture

## System Architecture

```
┌─────────────────────────────────────────────────────────┐
│                     Linux Host                          │
│                                                         │
│  ┌──────────┐    netlink    ┌─────────────────────────┐ │
│  │ WireGuard│◄─────────────►│     wg-sockd Agent      │ │
│  │  Kernel  │               │                         │ │
│  │  (wg0)   │               │  ┌─────────┐ ┌───────┐ │ │
│  └──────────┘               │  │ SQLite  │ │ Conf  │ │ │
│                             │  │   DB    │ │Writer │ │ │
│                             │  └─────────┘ └───────┘ │ │
│  ┌─────────────┐            │                         │ │
│  │  wg0.conf   │◄───────────│  Reconciler (30s loop)  │ │
│  │ [Interface]  │           │                         │ │
│  │ [Peer] ...  │            └────────────┬────────────┘ │
│  └─────────────┘                         │              │
│                                          │              │
│                    Unix Socket (0660)     │              │
│               /var/run/wg-sockd/wg-sockd.sock           │
│                          │                              │
│              ┌───────────┼───────────┐                  │
│              ▼           ▼           ▼                  │
│        ┌──────────┐ ┌────────┐ ┌──────────┐            │
│        │   CLI    │ │  curl  │ │Embedded  │            │
│        │wg-sockd- │ │        │ │  UI      │            │
│        │  ctl     │ │        │ │ TCP:8080 │            │
│        └──────────┘ └────────┘ └──────────┘            │
└─────────────────────────────────────────────────────────┘

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
| `config` | `agent/internal/config/` | YAML config loading, CLI flag overrides, validation |
| `api` | `agent/internal/api/` | HTTP router (net/http), request/response types, handlers for peers/profiles/health/stats/QR |
| `storage` | `agent/internal/storage/` | SQLite database operations — peers CRUD, profiles CRUD, migrations, backup/recovery |
| `wireguard` | `agent/internal/wireguard/` | WireGuard kernel interface via wgctrl netlink — get/set device, peer operations |
| `confwriter` | `agent/internal/confwriter/` | Atomic wg0.conf writing — preserves [Interface], manages [Peer] sections with metadata comments, debounced writes |
| `reconciler` | `agent/internal/reconciler/` | Periodic kernel ↔ DB sync — detects unknown peers, removes unauthorized, updates stats |
| `health` | `agent/internal/health/` | Health check aggregator — WireGuard reachability, SQLite connectivity, disk space |
| `middleware` | `agent/internal/middleware/` | HTTP middleware — rate limiting (token bucket), request logging |
| `sockmon` | `agent/internal/sockmon/` | Socket file monitor — periodic existence check, self-healing re-creation |

### Data Flow

1. **Peer Creation:**
   Client → Unix Socket → `api.Router` → `api.HandleCreatePeer` → `storage.CreatePeer` (SQLite) → `wireguard.AddPeer` (netlink) → `confwriter.Write` (wg0.conf) → Response with peer config + private key

2. **Reconciliation (every 30s):**
   `reconciler.Run` → `wireguard.GetDevice` (kernel state) → compare with `storage.ListPeers` (DB state) → remove unknown peers → update transfer stats → `confwriter.Write` if changed

3. **Unknown Peer Detection:**
   Reconciler finds peer in kernel not in DB → `wireguard.RemovePeer` (immediate kernel removal) → `storage.CreatePeer` with `auto_discovered=true, enabled=false` → admin must approve via API/UI

### Resilience Features

| Feature | Implementation | Trigger |
|---------|---------------|---------|
| Rate Limiting | In-memory token bucket per connection, 10 req/s default | Every API request (except /health) |
| Socket Self-Healing | 5s periodic check, re-create listener if missing | Socket file deleted or replaced |
| Debounced Conf Writing | 100ms coalesce window for rapid mutations | Multiple peer changes in quick succession |
| Graceful Degradation | Disk space check, 503 on writes, reads continue | Disk full condition |
| SQLite Backup | Hourly .db → .db.bak with fsync | Timer-based |
| SQLite Recovery | 3-level chain: backup → conf comments → clean start | DB corruption detected |

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
- Subcommands: `peers list|add|delete|approve`, `profiles list`
- Supports custom socket path via `--socket` flag

## Deployment Architectures

### Standalone (systemd)

- Binary: `/usr/local/bin/wg-sockd`
- Config: `/etc/wg-sockd/config.yaml`
- Database: `/var/lib/wg-sockd/wg-sockd.db`
- Socket: `/var/run/wg-sockd/wg-sockd.sock`
- Service: `wg-sockd.service` with `CAP_NET_ADMIN`, `ProtectSystem=strict`
- Optional: `--serve-ui` for embedded web interface on TCP :8080

### Kubernetes (Helm)

- Agent: Installed directly on the node (via `install.sh`)
- UI Proxy: Kubernetes pod with hostPath mount to Unix socket
- Node selector: `wg-sockd=active` label
- Security: `runAsGroup: 5000` matching host socket group
