# wg-sockd — Project Overview

## Executive Summary

wg-sockd is a WireGuard peer management agent that provides a REST API, web UI, and CLI for managing VPN peers on Linux hosts. It supports profile-based access control, auto-discovery of unknown peers, QR code generation, and flexible deployment (standalone with embedded UI or Kubernetes with Helm).

## Repository Structure

- **Type:** Multi-part (6 components in one repository)
- **Primary Languages:** Go 1.26 (backend), JavaScript/JSX (frontend)
- **Architecture:** Unix socket-based agent with separate UI layer

### Parts

| Part | Path | Type | Description |
|------|------|------|-------------|
| Agent | `agent/` | Go backend | Core WireGuard management daemon — REST API over Unix socket, SQLite storage, wgctrl netlink, conf writer, reconciler |
| UI Proxy | `ui/` | Go backend | Lightweight reverse proxy for Kubernetes — routes HTTP to agent's Unix socket via hostPath |
| Web UI | `ui/web/` | React SPA | Responsive management interface — peer CRUD, profiles, stats dashboard, QR codes |
| CLI | `cmd/wg-sockd-ctl/` | Go CLI | Standalone command-line tool for scripting and headless peer management |
| Helm Chart | `chart/` | Infra | Kubernetes deployment chart for UI proxy pod with hostPath socket mount |
| Deploy | `deploy/` | Infra | Systemd service unit, install/uninstall scripts, default config template |

## Technology Stack

| Category | Technology | Version | Purpose |
|----------|-----------|---------|---------|
| Language (backend) | Go | 1.26.1 | Agent, UI proxy, CLI |
| Language (frontend) | JavaScript/JSX | ES2024 | Web UI |
| Database | SQLite (modernc.org/sqlite) | 1.46.1 | Peer and profile storage (pure Go, no CGO) |
| WireGuard Control | wgctrl | 2024-12 | Kernel netlink interface for peer management |
| Frontend Framework | React | 19.2.4 | SPA component framework |
| Build Tool | Vite | 8.0.0 | Frontend bundler and dev server |
| CSS Framework | TailwindCSS | 4.2.1 | Utility-first styling |
| UI Components | shadcn/ui + Radix UI | latest | Accessible component primitives |
| Data Fetching | TanStack React Query | 5.90.21 | Server state management and caching |
| Routing | React Router DOM | 7.13.1 | Client-side routing |
| QR Generation | go-qrcode | — | PNG QR code generation for peer configs |
| CIDR Math | go4.org/netipx | — | IP set operations for profile exclusion |
| Container | Docker (multi-stage) | — | UI proxy container image |
| Orchestration | Helm | — | Kubernetes deployment |
| Service Manager | systemd | — | Standalone daemon management |

## Architecture Pattern

The system follows a **socket-mediated agent** pattern:

1. **Agent** runs on the Linux host with `CAP_NET_ADMIN`, manages WireGuard kernel interface via netlink, stores state in SQLite, and exposes a REST API exclusively over a Unix domain socket.
2. **Consumers** (CLI, UI proxy, direct curl) connect to the Unix socket — there is zero TCP network surface by default.
3. **Standalone mode:** Agent serves the embedded React SPA on TCP `:8080` via `--serve-ui` flag.
4. **Kubernetes mode:** A separate UI proxy pod mounts the socket via hostPath and proxies HTTP traffic.

## Key Design Decisions

- **Unix socket only** — eliminates network attack surface; socket permissions (0660, group-restricted) provide access control
- **Pure Go SQLite** — modernc.org/sqlite requires no CGO, simplifying cross-compilation
- **Profile-based peers** — reusable network access templates with CIDR exclusion math
- **Unknown peer blocking** — peers found in kernel but not in database are immediately removed and recorded for admin review
- **Conf preservation** — agent never modifies the `[Interface]` section of wg0.conf, only manages `[Peer]` sections with metadata comments
- **Reconciliation loop** — 30-second kernel ↔ database sync ensures consistency

## Project Status

All 5 epics completed (as of 2026-03-15):

1. **Epic 1: Agent Core** — Go module, config, wgctrl client, SQLite storage, conf writer, API router, health endpoint, reconciliation, Unix socket listener, systemd unit, smoke tests
2. **Epic 2: Agent Complete** — Profiles, CIDR exclusion, profiles CRUD, profile-based peer creation, peer update, key rotation, QR codes, stats, periodic reconciliation, input validation, batch import, peer approval
3. **Epic 3: Web UI** — Go reverse proxy, React SPA scaffold, peers list, peer creation form, profiles management, stats dashboard, connection status, unknown peer alerts, multi-stage Dockerfile
4. **Epic 4: Deployment** — Helm chart, install script, wg-sockd-ctl CLI, embedded UI mode, README and documentation
5. **Epic 5: Hardening** — Rate limiting, socket self-healing, debounced conf writing, graceful degradation, SQLite backup and recovery
