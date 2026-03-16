# wg-sockd — Source Tree Analysis

## Annotated Directory Tree

```
wg-sockd/
├── agent/                          # ── Part: Agent (Go backend) ──
│   ├── cmd/
│   │   └── wg-sockd/
│   │       ├── main.go             # ENTRY POINT — flag parsing, dependency wiring, socket listener
│   │       ├── embed.go            # Build tag: embed_ui — serves React SPA from embedded FS
│   │       ├── embed_stub.go       # Build tag: !embed_ui — no-op stub for lean builds
│   │       └── main_test.go        # Integration tests for main
│   ├── internal/
│   │   ├── api/                    # REST API layer
│   │   │   ├── router.go           # HTTP route registration (net/http ServeMux)
│   │   │   ├── types.go            # Request/response JSON structs
│   │   │   ├── handlers.go         # Peer handlers — CRUD, QR, key rotation, batch, approve
│   │   │   ├── handlers_profiles.go # Profile handlers — CRUD
│   │   │   ├── helpers.go          # Shared handler utilities (JSON response, error helpers)
│   │   │   ├── handlers_test.go    # Peer handler tests
│   │   │   └── handlers_profiles_test.go # Profile handler tests
│   │   ├── config/                 # Configuration
│   │   │   ├── config.go           # YAML loading, CLI flag merge, defaults, validation
│   │   │   └── config_test.go      # Config tests
│   │   ├── confwriter/             # WireGuard conf file management
│   │   │   ├── writer.go           # Atomic write — preserves [Interface], manages [Peer] with metadata comments, debounce
│   │   │   └── writer_test.go      # Conf writer tests
│   │   ├── health/                 # Health check system
│   │   │   ├── health.go           # Aggregated health — WireGuard, SQLite, disk space
│   │   │   ├── health_test.go      # Health tests
│   │   │   └── doc.go              # Package documentation
│   │   ├── middleware/             # HTTP middleware
│   │   │   └── doc.go              # Rate limiting (token bucket), request logging
│   │   ├── reconciler/            # Kernel ↔ DB synchronization
│   │   │   ├── reconciler.go       # 30s periodic sync — unknown peer detection, stats update
│   │   │   ├── reconciler_test.go  # Reconciler tests
│   │   │   └── doc.go              # Package documentation
│   │   ├── sockmon/               # Socket health monitor
│   │   │   └── (socket monitoring — 5s check, self-healing re-creation)
│   │   ├── storage/               # SQLite data layer
│   │   │   ├── db.go               # DB initialization, migrations, connection management
│   │   │   ├── errors.go           # Custom error types (ErrNotFound, ErrDiskFull, etc.)
│   │   │   ├── peers.go            # Peer CRUD operations
│   │   │   ├── profiles.go         # Profile CRUD operations
│   │   │   ├── backup.go           # Hourly backup + 3-level recovery chain
│   │   │   ├── backup_test.go      # Backup/recovery tests
│   │   │   ├── storage_test.go     # Storage integration tests
│   │   │   ├── profiles_test.go    # Profile storage tests
│   │   │   └── migrations/         # SQL migration files (embedded)
│   │   └── wireguard/             # WireGuard kernel interface
│   │       ├── client.go           # Interface definition + wgctrl client wrapper
│   │       ├── wgctrl.go           # Netlink-based implementation
│   │       └── client_test.go      # WireGuard client tests
│   ├── go.mod                      # Go module: github.com/aleks-dolotin/wg-sockd/agent
│   ├── go.sum
│   └── wg-sockd                    # Compiled binary (gitignored)
│
├── ui/                             # ── Part: UI Proxy (Go backend) ──
│   ├── cmd/                        # UI proxy entry point
│   ├── internal/
│   │   ├── proxy/                  # HTTP reverse proxy → Unix socket
│   │   └── discovery/              # Socket file discovery and health
│   ├── web/                        # ── Part: Web UI (React SPA) ──
│   │   ├── src/
│   │   │   ├── main.jsx            # ENTRY POINT — React app bootstrap
│   │   │   ├── App.jsx             # Root component with router
│   │   │   ├── api/                # API client layer (fetch to agent REST API)
│   │   │   ├── pages/              # Route-based page components (peers, profiles, stats)
│   │   │   ├── components/         # Shared components
│   │   │   │   ├── Layout.jsx      # App shell — sidebar, header
│   │   │   │   ├── ConnectionStatus.jsx  # Real-time connection indicator
│   │   │   │   ├── ConnectionContext.jsx # React context for connection state
│   │   │   │   ├── UnknownPeerAlert.jsx  # Alert banner for auto-discovered peers
│   │   │   │   └── ui/             # shadcn/ui primitives (Button, Card, Dialog, etc.)
│   │   │   ├── lib/                # Utility functions (cn, formatters)
│   │   │   ├── assets/             # Static assets
│   │   │   └── index.css           # TailwindCSS entry point
│   │   ├── package.json            # Node dependencies
│   │   ├── vite.config.js          # Vite build configuration
│   │   ├── components.json         # shadcn/ui config
│   │   └── dist/                   # Built output (served by agent or proxy)
│   ├── Dockerfile                  # Multi-stage: build React → Go proxy image
│   ├── go.mod                      # Go module: github.com/aleks-dolotin/wg-sockd/ui
│   └── .dockerignore
│
├── cmd/                            # ── Part: CLI ──
│   └── wg-sockd-ctl/
│       ├── main.go                 # ENTRY POINT — CLI subcommands (peers, profiles)
│       ├── main_test.go            # CLI tests
│       └── go.mod                  # Go module (CGO_ENABLED=0, static binary)
│
├── chart/                          # ── Part: Helm Chart ──
│   ├── Chart.yaml                  # Chart metadata
│   ├── values.yaml                 # Default values (image, security, nodeSelector)
│   ├── templates/                  # K8s manifests (Deployment, Service, etc.)
│   └── tests/                      # Helm test hooks
│
├── deploy/                         # ── Part: Deploy (Standalone) ──
│   ├── config.yaml                 # Default agent config template
│   ├── wg-sockd.service            # systemd unit (CAP_NET_ADMIN, ProtectSystem=strict)
│   ├── install.sh                  # One-command installer (user creation, binary, systemd)
│   └── uninstall.sh                # Clean uninstall (preserves config/data)
│
├── bin/                            # Compiled binaries (gitignored)
│   ├── wg-sockd                    # Agent binary
│   └── wg-sockd-ctl               # CLI binary
│
├── test/
│   └── smoke.sh                    # End-to-end smoke test script
│
├── spike/                          # Day-1 validation spike (wgctrl proof-of-concept)
│
├── Makefile                        # Build system — build, test, install, docker, ui
├── README.md                       # Comprehensive project documentation
├── .gitignore
│
├── _bmad-output/                   # BMAD workflow outputs (planning, implementation, tests)
│   ├── planning-artifacts/
│   │   └── epics.md               # 5 epics with all stories
│   └── implementation-artifacts/
│       ├── sprint-status.yaml     # Sprint tracking (all done)
│       └── *.md                   # Story implementation specs
│
└── design-artifacts/               # WDS design framework (structure only)
    ├── A-Product-Brief/
    ├── B-Trigger-Map/
    ├── C-UX-Scenarios/
    ├── D-Design-System/
    ├── E-PRD/
    ├── F-Testing/
    └── G-Product-Development/
```

## Critical Folders Summary

| Folder | Criticality | Purpose |
|--------|------------|---------|
| `agent/internal/api/` | **High** | All REST API endpoints — the primary interface |
| `agent/internal/storage/` | **High** | SQLite data layer — peer and profile state |
| `agent/internal/wireguard/` | **High** | Kernel interface — the core WireGuard integration |
| `agent/internal/reconciler/` | **High** | Data consistency — kernel ↔ DB sync |
| `agent/internal/confwriter/` | **Medium** | Config file management with debounce |
| `agent/internal/config/` | **Medium** | Application configuration |
| `agent/cmd/wg-sockd/` | **Medium** | Entry point and dependency wiring |
| `ui/web/src/` | **Medium** | Frontend application source |
| `deploy/` | **Medium** | Production deployment artifacts |
| `chart/` | **Medium** | Kubernetes deployment |

## Entry Points

| Component | Entry Point | Description |
|-----------|------------|-------------|
| Agent | `agent/cmd/wg-sockd/main.go` | Daemon entry — parses flags, loads config, starts socket listener |
| UI Proxy | `ui/cmd/` | Reverse proxy entry — connects to Unix socket, serves HTTP |
| Web UI | `ui/web/src/main.jsx` | React bootstrap — renders App component |
| CLI | `cmd/wg-sockd-ctl/main.go` | CLI entry — subcommand dispatch |

## Integration Points

| From | To | Mechanism |
|------|----|-----------|
| CLI → Agent | Unix socket | HTTP over Unix domain socket |
| UI Proxy → Agent | Unix socket (hostPath) | HTTP over Unix domain socket |
| Embedded UI → Agent | In-process | Go serves React static files on TCP |
| Agent → WireGuard | Netlink | wgctrl kernel interface |
| Agent → SQLite | In-process | modernc.org/sqlite (pure Go) |
| Agent → wg0.conf | Filesystem | Atomic write with metadata comments |
| Web UI → UI Proxy/Agent | HTTP | REST API (fetch) |
