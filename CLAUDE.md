# CLAUDE.md — Project Knowledge Base

This file is automatically loaded by Claude at the start of every conversation.
It contains essential project context, build, and deploy instructions.

For infrastructure details (network topology, server access, WireGuard keys),
see `.claude/infrastructure.md` (local only, not tracked by git).

## Project: wg-sockd

WireGuard peer management agent with REST API, web UI, and CLI.
Manages VPN peers on Linux hosts via Unix socket, SQLite storage, and wgctrl netlink.

**GitHub:** `aleks-dolotin/wg-sockd`

## Repository Structure

| Part | Path | Description |
|------|------|-------------|
| Agent | `agent/` | Core daemon — REST API over Unix socket, SQLite, wgctrl netlink, reconciler |
| UI Proxy | `ui/` | Go reverse proxy for Kubernetes (routes HTTP → Unix socket) |
| Web UI | `ui/web/` | React 19 SPA (Vite, TailwindCSS 4, shadcn/ui, TanStack Query) |
| CLI | `cmd/wg-sockd-ctl/` | Static Go binary for headless peer management |
| Helm Chart | `chart/` | Kubernetes deployment chart |
| Deploy | `deploy/` | systemd unit, install.sh, uninstall.sh, default config |

## Tech Stack

- **Go 1.26** (agent, ui-proxy, CLI) — pure Go SQLite via `modernc.org/sqlite`, no CGO
- **React 19** + Vite 8 + TailwindCSS 4 + shadcn/ui + React Router DOM 7
- **WireGuard** control via `wgctrl` (netlink)

## Build Commands (Makefile)

```bash
make build          # Build agent (lean, no UI)
make build-full     # Build agent with embedded React UI
make build-ctl      # Build wg-sockd-ctl CLI (static, CGO_ENABLED=0)
make build-dev      # Build with dev_wg tag (in-memory WireGuard, for local dev)
make test           # Test agent module
make test-all       # Test all 3 Go modules (agent, ui, ctl)
make lint           # golangci-lint all Go modules
make lint-all       # Go lint + ESLint for UI
make ui             # Build React SPA (npm ci + npm run build)
make dev            # Build + run dev mode (macOS-friendly, no real WireGuard)
make smoke          # Run smoke tests (test/smoke.sh)
make install        # Install binary + systemd unit (requires root)
make uninstall      # Remove binary + systemd unit (preserves config/data)
make setup-hooks    # Install git hooks (pre-commit + pre-push)
```

## CI/CD (GitHub Actions)

### CI (`.github/workflows/ci.yml`)

Triggers on push to `main` and PRs. Runs Go test + lint + cross-compile check, React UI build + lint.

### Release (`.github/workflows/release.yml`)

Triggers on tag push `v*`. Pipeline: test → build-ui → build-binaries (amd64 + arm64) → GitHub Release → Docker push (GHCR) → Helm push (GHCR OCI).

Release artifacts: `wg-sockd-linux-*` (lean), `wg-sockd-full-linux-*` (with UI), `wg-sockd-ctl-linux-*`, plus SHA256 checksums.

### How to Release

```bash
git tag -a v0.X.0 -m "v0.X.0: description"
git push origin v0.X.0
# GitHub Actions builds, tests, and creates the release automatically
```

## Deployment

### Install from GitHub Release

```bash
curl -sSL https://raw.githubusercontent.com/aleks-dolotin/wg-sockd/main/deploy/install.sh | sudo bash
```

Options: `--agent-only` for lean binary without UI.

### Systemd Service Layout

- Binary: `/usr/local/bin/wg-sockd`
- Config: `/etc/wg-sockd/config.yaml`
- Database: `/var/lib/wg-sockd/wg-sockd.db`
- Socket: `/run/wg-sockd/wg-sockd.sock`
- Service: `wg-sockd.service` (runs as `wg-sockd:wg-sockd`, GID 5000)

### Config Reference

```yaml
interface: wg0               # WireGuard interface to manage
socket_path: /run/wg-sockd/wg-sockd.sock
db_path: /var/lib/wg-sockd/wg-sockd.db
conf_path: /etc/wireguard/wg0.conf
auto_approve_unknown: false
peer_limit: 250
reconcile_interval: 30s
rate_limit: 10
# external_endpoint: "vpn.example.com:51820"
serve_ui: false
ui_listen: "127.0.0.1:8080"
```

## Architecture

The agent follows a socket-mediated pattern: REST API exclusively over Unix domain socket, zero TCP network surface by default. Reconciliation loop (30s) syncs kernel WireGuard state with SQLite database. Unknown peers found in kernel are removed and recorded for admin approval.

Key design decisions: Unix socket only (no network attack surface), pure Go SQLite (no CGO), profile-based peer templates with CIDR exclusion, atomic conf writing with debounce.

For detailed architecture, see `docs/architecture.md`.

## Security Notes

- **Never commit** infrastructure details (IPs, keys, server configs) to this repo
- **Pre-push hook** scans for sensitive patterns (private keys, internal IPs, passwords)
- Use `.claude/infrastructure.md` for private deployment context (gitignored)
- WireGuard private keys live only on servers in `/etc/wireguard/`
