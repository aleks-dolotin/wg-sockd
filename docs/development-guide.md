# wg-sockd — Development Guide

## Prerequisites

- Go 1.26+ (for agent, UI proxy, CLI)
- Node.js 20+ with npm (for React web UI)
- Linux with WireGuard kernel module (for runtime testing)
- `CAP_NET_ADMIN` capability or root (for wgctrl netlink operations)

## Repository Layout

The project is a multi-part repo with three separate Go modules and one Node.js project:

| Module | Path | go.mod |
|--------|------|--------|
| Agent | `agent/` | `github.com/aleks-dolotin/wg-sockd/agent` |
| UI Proxy | `ui/` | `github.com/aleks-dolotin/wg-sockd/ui` |
| CLI | `cmd/wg-sockd-ctl/` | `github.com/aleks-dolotin/wg-sockd/cmd/wg-sockd-ctl` |
| Web UI | `ui/web/` | Node.js (package.json) |

## Build Commands

All builds are orchestrated via the root `Makefile`:

```bash
# Build lean agent binary (~15MB)
make build
# Output: bin/wg-sockd

# Build React UI
make ui
# Output: ui/web/dist/

# Build agent with embedded UI (~30MB)
make build-full
# Output: bin/wg-sockd-full

# Build CLI (static, no CGO)
make build-ctl
# Output: bin/wg-sockd-ctl

# Build UI Docker image
make docker-build
# Output: wg-sockd-ui:latest
```

## Testing

```bash
# Agent unit tests
make test

# Agent tests (verbose)
make test-v

# UI proxy tests
make test-ui

# CLI tests
make test-ctl

# All tests across all modules
make test-all

# End-to-end smoke tests (requires running agent)
make smoke
```

### Test File Locations

| Module | Test Pattern | Location |
|--------|-------------|----------|
| Agent | `*_test.go` | `agent/internal/*/` |
| UI Proxy | `*_test.go` | `ui/internal/*/` |
| CLI | `*_test.go` | `cmd/wg-sockd-ctl/` |
| Smoke | `smoke.sh` | `test/` |

## Local Development

### Agent

```bash
# Build and run (requires WireGuard + CAP_NET_ADMIN)
make build
sudo ./bin/wg-sockd --config deploy/config.yaml

# Or with embedded UI
make build-full
sudo ./bin/wg-sockd-full --config deploy/config.yaml --serve-ui
```

The agent listens on Unix socket by default. Use `curl --unix-socket` or the CLI to interact:

```bash
curl --unix-socket /var/run/wg-sockd/wg-sockd.sock http://localhost/api/health
./bin/wg-sockd-ctl peers list
```

### Web UI

```bash
cd ui/web
npm ci          # Install dependencies
npm run dev     # Start Vite dev server (HMR)
npm run build   # Production build → dist/
npm run lint    # ESLint
```

The dev server expects the agent API to be available. Configure the Vite proxy in `vite.config.js` to forward `/api/*` to the agent socket.

## Installation (systemd)

```bash
# Full install: create user, install binary, configure systemd
sudo make install

# Or one-command remote install
curl -sSL https://raw.githubusercontent.com/aleks-dolotin/wg-sockd/main/deploy/install.sh | sudo bash
```

Installs to:
- Binary: `/usr/local/bin/wg-sockd`
- Config: `/etc/wg-sockd/config.yaml`
- Service: `wg-sockd.service`
- User: `wg-sockd` (GID 5000)

## Configuration

See `deploy/config.yaml` for all options. Key settings:

- `interface` — WireGuard interface name (default: wg0)
- `socket_path` — Unix socket location
- `db_path` — SQLite database location
- `conf_path` — WireGuard config file to manage
- `auto_approve_unknown` — Auto-approve unknown peers (default: false)
- `peer_limit` — Maximum peer count (default: 250)
- `reconcile_interval` — Kernel sync interval (default: 30s)
- `rate_limit` — Requests per second per connection (default: 10, 0 to disable)

All config fields can be overridden via CLI flags (see `--help`).

## Clean

```bash
make clean       # Remove bin/ directory
```
