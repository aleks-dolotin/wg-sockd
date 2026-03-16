# wg-sockd — Component Inventory

## Agent Go Packages (agent/internal/)

### Core Packages

| Package | Files | Test Files | Purpose |
|---------|-------|------------|---------|
| `api` | router.go, types.go, handlers.go, handlers_profiles.go, helpers.go | handlers_test.go, handlers_profiles_test.go | REST API — HTTP routing and handlers |
| `config` | config.go | config_test.go | YAML configuration with CLI flag overrides |
| `storage` | db.go, errors.go, peers.go, profiles.go, backup.go | storage_test.go, profiles_test.go, backup_test.go | SQLite data layer — CRUD, migrations, backup/recovery |
| `wireguard` | client.go, wgctrl.go | client_test.go | WireGuard kernel interface via wgctrl netlink |
| `confwriter` | writer.go | writer_test.go | Atomic wg0.conf management with debounce |
| `reconciler` | reconciler.go, doc.go | reconciler_test.go | Periodic kernel ↔ DB sync |
| `health` | health.go, doc.go | health_test.go | Aggregated health checks |
| `middleware` | doc.go | — | Rate limiting and request logging |
| `sockmon` | — | — | Socket file monitoring and self-healing |

### Entry Points

| Binary | Path | Build Tag |
|--------|------|-----------|
| `wg-sockd` | agent/cmd/wg-sockd/main.go | default |
| `wg-sockd` (full) | agent/cmd/wg-sockd/main.go + embed.go | `embed_ui` |

## UI Proxy Go Packages (ui/internal/)

| Package | Purpose |
|---------|---------|
| `proxy` | HTTP reverse proxy forwarding to Unix socket |
| `discovery` | Socket file discovery and health checking |

## Web UI React Components (ui/web/src/)

### Application Components

| Component | File | Purpose |
|-----------|------|---------|
| App | App.jsx | Root component — React Router setup |
| Layout | components/Layout.jsx | App shell — sidebar navigation, header |
| ConnectionStatus | components/ConnectionStatus.jsx | Real-time agent connection indicator |
| ConnectionContext | components/ConnectionContext.jsx | React context provider for connection state |
| UnknownPeerAlert | components/UnknownPeerAlert.jsx | Alert banner for auto-discovered peers needing approval |

### shadcn/ui Primitives (ui/web/src/components/ui/)

Radix-based accessible component library, configured via `components.json`. Includes: Button, Card, Dialog, Alert, Input, Select, Table, Badge, and other primitives.

### Pages (ui/web/src/pages/)

Route-based page components for: peers list, peer creation form, profiles management, stats dashboard.

### API Layer (ui/web/src/api/)

Fetch-based HTTP client for communicating with the agent REST API. Used by React Query hooks for data fetching and mutations.

### Libraries

| Library | Purpose |
|---------|---------|
| TanStack React Query | Server state management — caching, refetching, mutations |
| React Router DOM | Client-side routing |
| lucide-react | Icon library |
| clsx + tailwind-merge | Dynamic className composition |
| class-variance-authority | Component variant styling |

## CLI (cmd/wg-sockd-ctl/)

Single Go file with subcommands:

| Subcommand | Action |
|------------|--------|
| `peers list` | List all peers |
| `peers add` | Create peer (--name, --profile or --allowed-ips) |
| `peers delete` | Delete peer (--id, --yes for skip confirmation) |
| `peers approve` | Approve auto-discovered peer (by pubkey prefix) |
| `profiles list` | List all profiles |

## Infrastructure Components

### Helm Chart (chart/)

- Chart.yaml — metadata
- values.yaml — image, security, node targeting
- templates/ — Deployment, Service, hostPath volume

### Deploy Scripts (deploy/)

- install.sh — One-command installer
- uninstall.sh — Clean removal
- wg-sockd.service — systemd unit
- config.yaml — Default configuration template
