# wg-sockd — Deployment Guide

## Deployment Modes

wg-sockd supports two deployment architectures:

1. **Standalone** — Agent with embedded UI on a single Linux host (systemd)
2. **Kubernetes** — Agent on the host node + UI proxy pod in the cluster (Helm)

## Standalone Deployment (systemd)

### Quick Install

Default mode — full binary (with UI) + CTL:

```bash
curl -sSL https://raw.githubusercontent.com/aleks-dolotin/wg-sockd/main/deploy/install.sh | sudo bash
```

Agent-only mode — lean binary (no UI) + CTL, for K8s / headless:

```bash
curl -sSL https://raw.githubusercontent.com/aleks-dolotin/wg-sockd/main/deploy/install.sh | sudo bash -s -- --agent-only
```

To install without starting the service automatically (e.g. to review config first):

Full binary, no auto-start:

```bash
curl -sSL https://raw.githubusercontent.com/aleks-dolotin/wg-sockd/main/deploy/install.sh | sudo bash -s -- --no-start
```

Agent-only, no auto-start:

```bash
curl -sSL https://raw.githubusercontent.com/aleks-dolotin/wg-sockd/main/deploy/install.sh | sudo bash -s -- --agent-only --no-start
```

Start manually when ready:

```bash
sudo systemctl enable --now wg-sockd
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
- `RuntimeDirectory=wg-sockd` — creates `/run/wg-sockd/` before the process starts (survives reboots)

### Filesystem Layout

| Path | Purpose | Permissions |
|------|---------|-------------|
| `/usr/local/bin/wg-sockd` | Agent binary | 0755 |
| `/etc/wg-sockd/config.yaml` | Configuration | 0640 (root:wg-sockd) |
| `/var/lib/wg-sockd/wg-sockd.db` | SQLite database | 0660 (wg-sockd:wg-sockd) |
| `/var/lib/wg-sockd/wg-sockd.db.bak` | Hourly DB backup | 0660 |
| `/var/run/wg-sockd/wg-sockd.sock` | Unix socket | 0660 (wg-sockd group) |
| `/etc/wireguard/` | WireGuard config directory | 0770 (root:wg-sockd) |
| `/etc/wireguard/wg0.conf` | WireGuard config | 0660 (root:wg-sockd) — managed by agent ([Peer] sections) |

### WireGuard Directory Permissions

> **Important:** WireGuard installs `/etc/wireguard/` as `700 root:root` by default — only root can access it. Since `wg-sockd` runs as an unprivileged user, the agent needs read/write access to both the directory and the conf file.

The `install.sh` script sets this up automatically. If you installed manually or see `permission denied` errors on peer creation, fix permissions:

```bash
sudo chown root:wg-sockd /etc/wireguard
sudo chmod 770 /etc/wireguard
sudo chown root:wg-sockd /etc/wireguard/wg0.conf
sudo chmod 660 /etc/wireguard/wg0.conf
```

**Why the directory needs write access:** The agent writes `wg0.conf.tmp` alongside `wg0.conf` and performs an atomic `rename()` to prevent partial writes. This requires write permission on the parent directory.

**Note:** If you run `wg-quick save wg0`, it resets `wg0.conf` ownership to `root:root` with mode `0600` (WireGuard's default `umask 077`). You will need to re-apply the permissions above after any `wg-quick save`.

### Embedded UI Mode

For standalone deployments with web UI.

Option A — serve from pre-built static files:

```bash
sudo wg-sockd --config /etc/wg-sockd/config.yaml --serve-ui-dir /opt/wg-sockd/ui/dist
```

Option B — use embedded binary (build with `make build-full`):

```bash
sudo wg-sockd --config /etc/wg-sockd/config.yaml --serve-ui
```

UI is served on TCP `:8080` (configurable via `--ui-listen`).

### Authentication

By default, the API is protected only by Unix socket file permissions. To enable HTTP authentication for the web UI and API:

**Step 1** — Generate a bcrypt password hash:

```bash
wg-sockd-ctl hash-password
```

**Step 2** — Add the `auth` block to `/etc/wg-sockd/config.yaml`:

```yaml
auth:
  basic:
    enabled: true
    username: admin
    password_hash: "$2a$12$..."   # output from step 1
  token:
    enabled: true
    token: "your-random-secret-at-least-32-chars"
  session_ttl: 15m
  skip_unix_socket: true           # local CLI requires no credentials
  secure_cookies: auto             # auto-detects HTTPS via X-Forwarded-Proto
  max_sessions: 100
```

**Step 3** — Restart the agent:

```bash
sudo systemctl restart wg-sockd
```

See [Authentication](./authentication.md) for the full configuration reference, environment variables, Kubernetes setup, and security considerations.

### Uninstall

```bash
sudo make uninstall
```

Alternatively: `sudo bash deploy/uninstall.sh`

Preserves config and data in `/etc/wg-sockd` and `/var/lib/wg-sockd`.

### Upgrade

Re-run the install script — it downloads the latest binary from GitHub Releases, replaces the binary, and restarts the service automatically. Config and database are not modified.

Full binary (with UI):

```bash
curl -sSL https://raw.githubusercontent.com/aleks-dolotin/wg-sockd/main/deploy/install.sh | sudo bash
```

Agent-only (no UI):

```bash
curl -sSL https://raw.githubusercontent.com/aleks-dolotin/wg-sockd/main/deploy/install.sh | sudo bash -s -- --agent-only
```

To upgrade without restarting immediately:

```bash
curl -sSL https://raw.githubusercontent.com/aleks-dolotin/wg-sockd/main/deploy/install.sh | sudo bash -s -- --no-start
```

Review or edit config if needed, then start:

```bash
sudo systemctl restart wg-sockd
```

> **Note:** See [UPGRADING.md](../UPGRADING.md) for version-specific migration notes.

## Kubernetes Deployment (Helm)

### Prerequisites

1. WireGuard running on the target node
2. Node labeled for pod scheduling — find your node name:

```bash
kubectl get nodes
```

Then label the node where WireGuard is running:

```bash
kubectl label node MY_NODE_NAME wg-sockd=active
```

Replace `MY_NODE_NAME` with the actual name from the output above.

Verify the label was applied:

```bash
kubectl get nodes --show-labels | grep wg-sockd
```

3. Agent installed on the node — SSH into it and run:

```bash
curl -sSL https://raw.githubusercontent.com/aleks-dolotin/wg-sockd/main/deploy/install.sh | sudo bash -s -- --agent-only
```

To install without auto-start (e.g. to configure first):

```bash
curl -sSL https://raw.githubusercontent.com/aleks-dolotin/wg-sockd/main/deploy/install.sh | sudo bash -s -- --agent-only --no-start
```

Then review `/etc/wg-sockd/config.yaml` and start when ready:

```bash
sudo systemctl enable --now wg-sockd
```

### Install UI Proxy

Install the chart directly from the registry:

```bash
helm install wg-sockd-ui oci://ghcr.io/aleks-dolotin/charts/wg-sockd-ui --version 0.22.0 -n wg-sockd --create-namespace
```

This creates a `wg-sockd` namespace and deploys the UI proxy pod there.

### Architecture

- Agent runs directly on the host (systemd service)
- UI proxy pod mounts the agent's Unix socket via hostPath
- Pod runs with `supplementalGroups: [5000]` to access the socket
- NodeSelector ensures the pod lands on the WireGuard node

### Helm Values

```yaml
image:
  repository: ghcr.io/aleks-dolotin/wg-sockd-ui
  tag: "0.22.0"

nodeName: my-wg-node

securityContext:
  runAsGroup: 5000
podSecurityContext:
  supplementalGroups:
    - 5000
```

`nodeName` pins the pod to a specific node (alternative to `nodeSelector`). The `runAsGroup` and `supplementalGroups` must match the host GID (5000).

### Verify

```bash
kubectl port-forward -n wg-sockd svc/wg-sockd-ui 8080:8080
```

Then open `http://localhost:8080`.

### Upgrade

```bash
helm upgrade wg-sockd-ui oci://ghcr.io/aleks-dolotin/charts/wg-sockd-ui \
  --version 0.22.0 -n wg-sockd
```

To also upgrade the agent on the host node, re-run the install script as described in the [Standalone Upgrade](#upgrade) section.

### Docker Image Build

```bash
make docker-build
```

This builds `wg-sockd-ui:latest` using a multi-stage Dockerfile: Stage 1 builds the React SPA (node:20-alpine), Stage 2 builds the Go proxy (golang:1.26-alpine), Stage 3 is the runtime (alpine:latest) with the static React bundle and Go proxy binary.

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

**Peer creation fails — "permission denied" on wg0.conf**
- WireGuard installs `/etc/wireguard/` as `700 root:root` — the agent cannot read or write it
- Fix: `sudo chown root:wg-sockd /etc/wireguard && sudo chmod 770 /etc/wireguard`
- Also: `sudo chown root:wg-sockd /etc/wireguard/wg0.conf && sudo chmod 660 /etc/wireguard/wg0.conf`
- Note: `wg-quick save` resets these permissions — re-apply after running it
- The `--dry-run` flag does not currently check conf_path write access (planned)

**Reconciler spams "conf rewrite failed" warnings**
- Same root cause as above — the agent cannot write `wg0.conf.tmp` in `/etc/wireguard/`
- Health endpoint still returns `"status": "ok"` — this is a known false-positive (the health check does not verify conf writability)

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

**UI proxy pod can't connect after agent restart (Kubernetes)**
- After a node reboot, if the agent fails with `NAMESPACE` / exit code 226, check that the systemd unit includes `RuntimeDirectory=wg-sockd`. Re-run the install script to update the unit file.
- If you see `no such file or directory` in UI proxy logs, restart the pod:

```bash
kubectl rollout restart deployment/wg-sockd-ui -n wg-sockd
```

**Environment variable override not working**
- Bool values must be: `true`/`false`/`1`/`0`/`t`/`f` (case-insensitive)
- Integer values must be valid numbers
- Check: `WG_SOCKD_SERVE_UI=true wg-sockd --config /etc/wg-sockd/config.yaml --dry-run`

### Diagnostic Commands

Check service status:

```bash
systemctl status wg-sockd
```

View logs:

```bash
journalctl -u wg-sockd -f
```

Verify binary versions:

```bash
wg-sockd --version
wg-sockd-ctl --version
```

Validate config without starting:

```bash
wg-sockd --config /etc/wg-sockd/config.yaml --dry-run
```

Test API directly:

```bash
sudo curl --unix-socket /var/run/wg-sockd/wg-sockd.sock http://localhost/api/health
```
