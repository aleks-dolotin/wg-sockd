# wg-sockd — Deployment Guide

## Deployment Modes

wg-sockd supports two deployment architectures:

1. **Standalone** — Agent with embedded UI on a single Linux host (systemd)
2. **Kubernetes** — Agent on the host node + UI proxy pod in the cluster (Helm)

## Standalone Deployment (systemd)

### Quick Install

```bash
# Default mode: full binary (with UI) + CTL
curl -sSL https://raw.githubusercontent.com/aleks-dolotin/wg-sockd/main/deploy/install.sh | sudo bash

# Agent-only mode: lean binary (no UI) + CTL — for K8s / headless
curl -sSL https://raw.githubusercontent.com/aleks-dolotin/wg-sockd/main/deploy/install.sh | sudo bash -s -- --agent-only
```

The install script:
1. Creates system user `wg-sockd` with GID 5000
2. Installs binary to `/usr/local/bin/wg-sockd`
3. Creates config at `/etc/wg-sockd/config.yaml` (if not present)
4. Installs and enables systemd unit
5. Starts the service

### Manual Install

> **Note:** `make install` is a legacy install path. For production deployments, use `deploy/install.sh` which handles user creation, config management, upgrade paths, and SHA256 verification.

```bash
make build
sudo make install
sudo systemctl start wg-sockd
```

### Systemd Unit

File: `deploy/wg-sockd.service`

Key security settings:
- `User=wg-sockd` — runs as unprivileged user
- `AmbientCapabilities=CAP_NET_ADMIN` — only WireGuard kernel operations
- `ProtectSystem=strict` — read-only filesystem except allowed paths
- `NoNewPrivileges=yes` — prevent privilege escalation
- `ReadWritePaths=/var/lib/wg-sockd /var/run/wg-sockd /etc/wireguard` — minimal write access

### Filesystem Layout

| Path | Purpose | Permissions |
|------|---------|-------------|
| `/usr/local/bin/wg-sockd` | Agent binary | 0755 |
| `/etc/wg-sockd/config.yaml` | Configuration | 0640 (root:wg-sockd) |
| `/var/lib/wg-sockd/wg-sockd.db` | SQLite database | 0660 (wg-sockd:wg-sockd) |
| `/var/lib/wg-sockd/wg-sockd.db.bak` | Hourly DB backup | 0660 |
| `/var/run/wg-sockd/wg-sockd.sock` | Unix socket | 0660 (wg-sockd group) |
| `/etc/wireguard/wg0.conf` | WireGuard config | Managed by agent ([Peer] sections) |

### Embedded UI Mode

For standalone deployments with web UI:

```bash
# Option A: Pre-built static files
sudo wg-sockd --config /etc/wg-sockd/config.yaml --serve-ui-dir /opt/wg-sockd/ui/dist

# Option B: Embedded in binary (build with make build-full)
sudo wg-sockd --config /etc/wg-sockd/config.yaml --serve-ui
```

UI is served on TCP `:8080` (configurable via `--ui-listen`).

### Uninstall

```bash
sudo make uninstall
# Or: sudo bash deploy/uninstall.sh
```

Preserves config and data in `/etc/wg-sockd` and `/var/lib/wg-sockd`.

## Kubernetes Deployment (Helm)

### Prerequisites

1. WireGuard running on the target node
2. Agent installed on the node via `install.sh`
3. Node labeled: `kubectl label node <node> wg-sockd=active`

### Install UI Proxy

```bash
helm install wg-sockd-ui ./chart/ \
  --set image.repository=ghcr.io/aleks-dolotin/wg-sockd-ui \
  --set image.tag=latest
```

### Architecture

- Agent runs directly on the host (systemd service)
- UI proxy pod mounts the agent's Unix socket via hostPath
- Pod runs with `supplementalGroups: [5000]` to access the socket
- NodeSelector ensures the pod lands on the WireGuard node

### Helm Values

```yaml
image:
  repository: ghcr.io/aleks-dolotin/wg-sockd-ui
  tag: "0.1.0"

# Pin to specific node (alternative to nodeSelector)
nodeName: my-wg-node

# Security — must match host GID
securityContext:
  runAsGroup: 5000
podSecurityContext:
  supplementalGroups:
    - 5000
```

### Verify

```bash
kubectl port-forward svc/wg-sockd-ui 8080:8080
open http://localhost:8080
```

### Docker Image Build

```bash
make docker-build
# Builds: wg-sockd-ui:latest

# Multi-stage Dockerfile:
# Stage 1: Build React SPA (node:20-alpine)
# Stage 2: Build Go proxy (golang:1.26-alpine)
# Stage 3: Runtime (alpine:latest) — static React + Go proxy binary
```

## Security Considerations

### Socket Access Control

The Unix socket is the only entry point. Permissions:
- Created with `umask(0117)` → mode `0660`
- Owner: `wg-sockd:wg-sockd`
- Only group members can connect
- In K8s: pod needs `supplementalGroups: [5000]`

### Network Surface

- **Default:** Zero TCP exposure — Unix socket only
- **Standalone UI:** TCP `:8080` only when `--serve-ui` is enabled
- **K8s:** TCP `:8080` on the UI proxy pod (behind K8s Service/Ingress)

### Capabilities

Agent needs only `CAP_NET_ADMIN` for WireGuard netlink operations. No root required.

## Troubleshooting

When troubleshooting, always start with `--dry-run` to validate configuration and prerequisites before investigating further:

```bash
sudo wg-sockd --config /etc/wg-sockd/config.yaml --dry-run
```

This validates config parsing, ui_listen format, directory permissions, and WireGuard availability without starting any services.

### Common Issues

**Agent won't start — "loading config" error**
- Check YAML syntax: `cat /etc/wg-sockd/config.yaml | python3 -c 'import yaml,sys; yaml.safe_load(sys.stdin)'`
- Ensure no tabs (YAML requires spaces)

**Agent starts but WireGuard in degraded mode**
- Install `wireguard-tools`: `apt install wireguard-tools` (or equivalent for your distro)
- Verify `wg` is in PATH: `which wg`
- Check WireGuard interface exists: `ip link show wg0`

**UI not accessible**
- Verify `serve_ui: true` in config
- Check `ui_listen` format — must be `host:port` (e.g., `127.0.0.1:8080`)
- Run `--dry-run` to validate ui_listen format
- If using embedded UI, ensure binary was built with `make build-full` (check `wg-sockd --version` for `+ui` tag)
- If `--version` shows no `+ui` tag, you have the lean binary — use `--serve-ui-dir` instead

**Socket permission denied**
- Client must be in the `wg-sockd` group: `sudo usermod -aG wg-sockd $USER`
- Re-login or `newgrp wg-sockd` after adding group

**Upgrade appended duplicate config entries**
- The installer only appends `serve_ui`/`ui_listen` if `^serve_ui:` is not already present
- Comments containing `serve_ui` don't match — only lines starting with `serve_ui:` count
- If duplicated, manually edit `/etc/wg-sockd/config.yaml` to remove the extra entries

**Environment variable override not working**
- Bool values must be: `true`/`false`/`1`/`0`/`t`/`f` (case-insensitive)
- Integer values must be valid numbers
- Check: `WG_SOCKD_SERVE_UI=true wg-sockd --config /etc/wg-sockd/config.yaml --dry-run`

### Diagnostic Commands

```bash
# Check service status
systemctl status wg-sockd

# View logs
journalctl -u wg-sockd -f

# Verify binary version
wg-sockd --version
wg-sockd-ctl --version

# Validate config without starting
wg-sockd --config /etc/wg-sockd/config.yaml --dry-run

# Test API directly
curl --unix-socket /var/run/wg-sockd/wg-sockd.sock http://localhost/api/health
```
