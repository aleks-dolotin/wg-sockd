# Upgrading wg-sockd

## v0.x → v1.0.0 (Server-Side IP Filtering)

### New Feature: Firewall enforcement enabled by default

wg-sockd now enforces per-peer destination filtering via iptables. On startup, the agent creates a dispatch chain `WG_SOCKD_FORWARD` in the `FORWARD` chain and per-peer chains `WG_PEER_<8alnum>` with ACCEPT/DROP rules derived from each peer's `client_allowed_ips`.

**This feature is enabled by default** (`firewall.enabled: true`).

### Action required: Check your WireGuard PostUp rules

If your `wg*.conf` file contains broad ACCEPT rules for the WireGuard interface, they must be removed — they bypass per-peer filtering:

```ini
# ❌ Remove these lines from wg*.conf PostUp/PostDown:
PostUp = iptables -A FORWARD -i %i -j ACCEPT
PostDown = iptables -D FORWARD -i %i -j ACCEPT
```

Keep only the outbound rule (needed for response traffic back to clients):

```ini
# ✅ Keep these:
PostUp = iptables -A FORWARD -o %i -j ACCEPT
PostDown = iptables -D FORWARD -o %i -j ACCEPT
```

After editing `wg*.conf`, restart the WireGuard interface and wg-sockd:

```bash
sudo wg-quick down wg1
sudo wg-quick up wg1
sudo systemctl restart wg-sockd
```

### To disable firewall (not recommended)

If `iptables` is not available on your system, or you want to manage filtering externally, disable the feature in `/etc/wg-sockd/config.yaml`:

```yaml
firewall:
  enabled: false
```

### Kubernetes nodes

No additional action required. wg-sockd inserts its jump rule at position 1 scoped to the WireGuard interface (`-I FORWARD 1 -i <wg-interface>`), which ensures it runs before Kubernetes `KUBE-FORWARD RELATED,ESTABLISHED` rules.

### Verify firewall is working after upgrade

```bash
# Should show -A FORWARD -i wg1 -j WG_SOCKD_FORWARD near the top
sudo iptables -S FORWARD

# Should show per-peer jump rules with non-zero packet counters after some traffic
sudo iptables -L WG_SOCKD_FORWARD -v -n
```

---

## v0.15.x → v0.16.0 (Management Port for Metrics)

### Breaking Change: Prometheus metrics moved to dedicated management port

The `/api/metrics` endpoint has been removed from the main API server. Prometheus metrics are now served on a **dedicated management port** (default `:8090`) at `/management/prometheus`.

This prevents unauthenticated exposure of peer names, public keys, traffic volumes, and online status through the public-facing reverse proxy.

**Action required:**

1. **Config file** — add `management_listen` if you want a non-default address:
   ```yaml
   management_listen: ":8090"   # default; set to "" to disable
   ```

2. **Prometheus scrape config** — update target port and path:
   ```yaml
   # Before
   - targets: ["wg-sockd:8080"]
     metrics_path: /api/metrics
   # After
   - targets: ["wg-sockd:8090"]
     metrics_path: /management/prometheus
   ```

3. **Reverse proxy** — remove any `/api/metrics` proxy rule (the endpoint no longer exists on the main server).

4. **Helm chart** — if using the bundled chart, `values.yaml` defaults have been updated automatically. Verify your overrides match:
   ```yaml
   prometheus:
     enabled: true
     path: /management/prometheus
     port: 8090
   ```

5. **NetworkPolicy (recommended)** — restrict port 8090 to Prometheus namespace only.

| Env var | Description |
|---|---|
| `WG_SOCKD_MANAGEMENT_LISTEN` | Management server listen address (default `:8090`, empty to disable) |

---

## v0.13.x → v0.15.0 (WYSIWYG Peer Config + Profile UX Overhaul)

> **Note:** v0.14.0 was never released. This version combines the Profile UX Overhaul and WYSIWYG Cascade Removal.

### Breaking Change: Configuration cascade removed

The 4-level cascade (peer → profile → global → hardcoded) for generating client .conf files has been removed. Peer fields are now used directly — what you see in the UI is exactly what goes into the generated .conf.

**Action required before upgrading:** Run the one-time migration script to backfill cascade-resolved values into peer records:

```bash
cmd/migrate-cascade/main.go --db-path /var/lib/wg-sockd/wg-sockd.db --config /etc/wg-sockd/config.yaml
```

This script resolves the cascade for every existing peer and writes the final values (DNS, MTU, PKA, ClientAllowedIPs, ClientAddress) directly into the peer's DB record. Run this **before** upgrading the binary.

### Breaking Change: `peer_defaults` config section removed

The `peer_defaults` section in config.yaml is now ignored. If you relied on global defaults for DNS, MTU, PKA, or ClientAllowedIPs, set those values on profiles instead and re-create peers or update them in bulk.

### Breaking Change: Environment variables removed

The following env vars are no longer effective:
- `WG_SOCKD_CLIENT_DNS`
- `WG_SOCKD_CLIENT_MTU`
- `WG_SOCKD_CLIENT_PERSISTENT_KEEPALIVE`
- `WG_SOCKD_CLIENT_ALLOWED_IPS`

### Breaking Change: `client_allowed_ips` and `client_address` now required

`POST /api/peers` and `POST /api/peers/{id}/approve` now require `client_allowed_ips` and `client_address` fields. Requests without them return HTTP 400.

CLI: `wg-sockd-ctl peers add` now requires `--client-allowed-ips` and `--client-address` flags.

### Breaking Change: `endpoint` removed from profiles

The `endpoint` field has been removed from profiles entirely (database column dropped via migration 007).
Profile endpoint was a design mistake — endpoint is unique per site-to-site peer, not a shared profile default.

**Action required:** None. The migration runs automatically. If your config.yaml `peer_profiles` section
contains `endpoint`, it will be silently ignored.

Peer-level endpoint (`peers.endpoint`) is NOT affected — it continues to work as before.

### API Change: `resolved_*` fields removed from GET /api/peers

The following fields are no longer present in peer API responses:
- `resolved_client_dns`, `resolved_client_dns_source`
- `resolved_client_mtu`, `resolved_client_mtu_source`
- `resolved_client_persistent_keepalive`, `resolved_client_persistent_keepalive_source`
- `resolved_client_allowed_ips`, `resolved_client_allowed_ips_source`

Clients that read these fields should use the direct peer fields instead (`client_dns`, `client_mtu`, `persistent_keepalive`, `client_allowed_ips`).

### Behavior Change: PresharedKey no longer auto-generated from profile

PSK generation is now controlled **entirely by the client request**:

- The API generates a PSK only when `preshared_key: "auto"` is sent in the request body
- The profile's `use_preshared_key` flag pre-checks the UI checkbox — the user can override it
- The backend does NOT check the profile's flag

### UI Changes

- Profile create/edit moved from modal dialogs to full pages (`/settings/profiles/new`, `/settings/profiles/:name`)
- Peer create/edit forms restructured with sections (General, Server config, Client download config) and info tooltips
- All profile fields pre-fill peer form when profile is selected — all fields remain editable

### New Documentation

See [Profiles and Configuration Cascade](docs/profiles-and-cascade.md) for the WYSIWYG model reference.

---

## v0.12.x → v0.13.0 (Client Config, PSK, Split-Tunnel)

### Breaking Change: `auto_approve_unknown` removed

The `auto_approve_unknown` config field has been removed. If your `config.yaml` contains it,
the agent will log a warning and ignore the field — it will **not** fail to start.

**Action required:** Remove the field from your `config.yaml`:

```yaml
# Remove this line:
auto_approve_unknown: false
```

All unknown peers discovered in the kernel are now always removed and inserted as
disabled pending admin review via the Approve flow.

### New: `client_address` field

A new required field `client_address` (CIDR format, e.g. `10.0.0.2/32`) is used as
`[Interface] Address` in client download configs. Existing peers without this field
continue to work with the `/32` fallback for legacy single-IP AllowedIPs.

**Recommended:** Set `client_address` on existing peers for correct client config generation:

```bash
wg-sockd-ctl peers update --id 1 --client-address 10.0.0.2/32
```

At startup, the agent logs a warning for each peer with empty `client_address`.

### New: SQLite migrations 005 and 006

Migrations run automatically on startup. Both are backward-compatible (empty-string
defaults for all new columns). No manual action required.

### New: Split-Tunnel `client_allowed_ips`

Add global default in `config.yaml` for split-tunnel client configs (optional):

```yaml
peer_defaults:
  client_allowed_ips: "10.0.0.0/8, 172.16.0.0/12"  # empty = full-tunnel (default)
```

### New environment variables

| Variable | Description |
|---|---|
| `WG_SOCKD_CLIENT_ALLOWED_IPS` | Global default client AllowedIPs (split-tunnel) |

---

## v0.6.x → v0.7.0 (HTTP Authentication)

### What Changed

v0.7.0 adds optional HTTP authentication to the agent API and embedded UI.
By default, **no authentication is configured** and the agent behaves identically
to v0.6.x — this is a non-breaking upgrade.

### New Config Section

Add the `auth` block to your `config.yaml` to enable authentication:

```yaml
auth:
  basic:
    enabled: true
    username: admin
    password_hash: "$2a$12$..."   # generate with: wg-sockd-ctl hash-password
  token:
    enabled: true
    token: "your-random-secret-at-least-32-chars"
  session_ttl: 15m
  skip_unix_socket: true          # default: Unix socket access has no auth
  secure_cookies: auto            # auto-detect from X-Forwarded-Proto
  max_sessions: 100
```

### Generate a Password Hash

```bash
wg-sockd-ctl hash-password
# Enter password (no echo), outputs bcrypt hash to stdout
```

### CLI Authentication

When the agent has token auth enabled, the CLI needs a token:

```bash
# Via flag
wg-sockd-ctl --token "your-secret" peers list

# Via environment variable
export WG_SOCKD_AUTH_TOKEN="your-secret"
wg-sockd-ctl peers list
```

**Unix socket access** is exempt from authentication by default (`skip_unix_socket: true`).
The CLI uses Unix socket by default, so no token is needed for local access unless
you explicitly set `skip_unix_socket: false`.

### Kubernetes / Helm

Add auth configuration to your Helm values:

```yaml
auth:
  basic:
    enabled: true
    username: admin
  token:
    enabled: true
  # Reference an external Secret with password hash and token:
  secretName: "my-wg-sockd-auth"
```

Create the Secret separately (recommended for production):

```bash
kubectl create secret generic my-wg-sockd-auth \
  --from-literal=WG_SOCKD_AUTH_BASIC_PASSWORD_HASH='$2a$12$...' \
  --from-literal=WG_SOCKD_AUTH_TOKEN='your-secret-token'
```

### Environment Variables (12 new)

| Variable | Description |
|---|---|
| `WG_SOCKD_AUTH_BASIC_ENABLED` | Enable basic auth (`true`/`false`) |
| `WG_SOCKD_AUTH_BASIC_USERNAME` | Username for basic auth |
| `WG_SOCKD_AUTH_BASIC_PASSWORD_HASH` | Bcrypt hash of the password |
| `WG_SOCKD_AUTH_TOKEN_ENABLED` | Enable token auth (`true`/`false`) |
| `WG_SOCKD_AUTH_TOKEN` | Bearer token value |
| `WG_SOCKD_AUTH_SESSION_TTL` | Session duration (e.g. `15m`, `1h`) |
| `WG_SOCKD_AUTH_SKIP_UNIX_SOCKET` | Skip auth for Unix socket (`true`/`false`) |
| `WG_SOCKD_AUTH_SECURE_COOKIES` | Cookie Secure flag (`auto`/`true`/`false`) |
| `WG_SOCKD_AUTH_MAX_SESSIONS` | Max concurrent sessions (integer) |
| `WG_SOCKD_AUTH_WEBAUTHN_ENABLED` | Enable WebAuthn/passkeys (`true`/`false`) |
| `WG_SOCKD_AUTH_WEBAUTHN_ORIGIN` | WebAuthn origin URL |

### API Changes

New endpoints (always registered, even without auth):

- `POST /api/auth/login` — username/password login, returns session cookie
- `POST /api/auth/logout` — invalidate session
- `DELETE /api/auth/logout` — alias for logout (SameSite workaround)
- `GET /api/auth/session` — check session status, returns `auth_required` field

Existing endpoints are unchanged. When auth is enabled, all `/api/*` endpoints
(except `/api/health`, `/api/auth/*`) require authentication.

### Reverse Proxy Notes

If running behind a reverse proxy (nginx, Traefik, etc.):
- Ensure the proxy forwards `Authorization` and `Cookie` headers
- Set `X-Forwarded-Proto: https` for automatic Secure cookie detection
- Most proxies do this by default
