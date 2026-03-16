# wg-sockd — Deployment Guide

## Deployment Modes

wg-sockd supports two deployment architectures:

1. **Standalone** — Agent with embedded UI on a single Linux host (systemd)
2. **Kubernetes** — Agent on the host node + UI proxy pod in the cluster (Helm)

## Standalone Deployment (systemd)

### Quick Install

```bash
curl -sSL https://raw.githubusercontent.com/aleks-dolotin/wg-sockd/main/deploy/install.sh | sudo bash
```

The install script:
1. Creates system user `wg-sockd` with GID 5000
2. Installs binary to `/usr/local/bin/wg-sockd`
3. Creates config at `/etc/wg-sockd/config.yaml` (if not present)
4. Installs and enables systemd unit
5. Starts the service

### Manual Install

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
