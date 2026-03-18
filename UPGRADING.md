# Upgrading wg-sockd

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
(except `/api/health`, `/api/metrics`, `/api/auth/*`) require authentication.

### Reverse Proxy Notes

If running behind a reverse proxy (nginx, Traefik, etc.):
- Ensure the proxy forwards `Authorization` and `Cookie` headers
- Set `X-Forwarded-Proto: https` for automatic Secure cookie detection
- Most proxies do this by default
