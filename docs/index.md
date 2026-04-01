# wg-sockd — Project Documentation Index

## Project Overview

- **Type:** Multi-part repository with 6 components
- **Primary Languages:** Go 1.26, JavaScript (React 19)
- **Architecture:** Unix socket-mediated WireGuard management agent
- **Status:** All 5 epics completed (2026-03-15)

## Quick Reference

### Agent (Go backend)
- **Tech:** Go 1.26, SQLite (modernc.org), wgctrl netlink, YAML config
- **Entry Point:** `agent/cmd/wg-sockd/main.go`
- **Pattern:** Layered — api → storage → wireguard → confwriter

### UI Proxy (Go backend)
- **Tech:** Go 1.26, net/http reverse proxy
- **Entry Point:** `ui/cmd/`
- **Purpose:** Kubernetes pod → Unix socket bridge

### Web UI (React SPA)
- **Tech:** React 19, Vite 8, TailwindCSS 4, shadcn/ui, React Query
- **Entry Point:** `ui/web/src/main.jsx`
- **Pattern:** Component-based with server state management

### CLI
- **Tech:** Go (CGO_ENABLED=0, static binary)
- **Entry Point:** `cmd/wg-sockd-ctl/main.go`
- **Subcommands:** peers (list|add|delete|approve), profiles (list)

### Helm Chart
- **Path:** `chart/`
- **Purpose:** Kubernetes deployment of UI proxy pod

### Deploy
- **Path:** `deploy/`
- **Purpose:** Standalone systemd deployment (install.sh, service unit, config)

## Generated Documentation

- [Project Overview](./project-overview.md)
- [Architecture](./architecture.md)
- [Server-Side IP Filtering](./firewall.md)
- [WireGuard Protocol and Kernel API](./wireguard-protocol.md)
- [Authentication](./authentication.md)
- [Profiles and Configuration Cascade](./profiles-and-cascade.md)
- [Source Tree Analysis](./source-tree-analysis.md)
- [API Contracts](./api-contracts.md)
- [Data Models](./data-models.md)
- [Component Inventory](./component-inventory.md)
- [Development Guide](./development-guide.md)
- [Deployment Guide](./deployment-guide.md)

## Existing Documentation

- [README](../README.md) — Comprehensive project documentation with Quick Start, API reference, security, CLI reference
- [UI README](../ui/web/README.md) — React + Vite setup notes
- [Epics](../_bmad-output/planning-artifacts/epics.md) — 5 epics with all user stories
- [Sprint Status](../_bmad-output/implementation-artifacts/sprint-status.yaml) — All epics done

## Getting Started

### Standalone (quickest)
```bash
curl -sSL https://raw.githubusercontent.com/aleks-dolotin/wg-sockd/main/deploy/install.sh | sudo bash
```

### Development
```bash
# Build agent
make build

# Build CLI
make build-ctl

# Build and serve web UI (dev mode)
cd ui/web && npm ci && npm run dev

# Run all tests
make test-all
```

### Kubernetes
```bash
# Install agent on node
sudo bash deploy/install.sh

# Deploy UI proxy
helm install wg-sockd-ui ./chart/
```
