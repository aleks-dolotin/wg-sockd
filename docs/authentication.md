# Authentication

wg-sockd supports optional HTTP authentication for the agent API and embedded web UI. By default, authentication is **disabled** — the API is protected only by Unix socket file permissions (group `wg-sockd`).

## Authentication Methods

Two methods are available and can be enabled independently or together:

| Method | Use case |
|--------|----------|
| **Basic** (username + password) | Web UI login, interactive access |
| **Bearer Token** | Automation, scripts, CI/CD pipelines |

When both methods are enabled, either one grants access.

## How Authentication Works

```mermaid
flowchart TD
    R[Incoming request] --> US{Unix socket\nskip_unix_socket: true?}
    US -->|Yes| OK[✅ Pass through]
    US -->|No| SC{Valid session\ncookie?}
    SC -->|Yes| OK
    SC -->|No| BT{Valid Bearer\ntoken?}
    BT -->|Yes| OK
    BT -->|No| HP{Path is\n/api/health?}
    HP -->|Yes| OK
    HP -->|No| CN{Accepts\ntext/html?}
    CN -->|Yes| RD[302 → /login]
    CN -->|No| R401[401 JSON]
```

Check order:

1. **Unix socket** — exempt when `skip_unix_socket: true` (default). The CLI uses the Unix socket locally and requires no credentials.
2. **Session cookie** — `wg_sockd_session` cookie issued after a successful login.
3. **Bearer token** — `Authorization: Bearer <token>` header.
4. **`/api/health`** — always exempt, regardless of auth configuration.
5. **Unauthenticated** — browsers receive `302 → /login`; API clients receive `401 JSON`.

## Quick Setup

### Basic Auth (Web UI + CLI)

**Step 1** — Generate a bcrypt password hash:

```bash
wg-sockd-ctl hash-password
# Enter password at the prompt — bcrypt hash is printed to stdout
```

**Step 2** — Add the `auth` block to `/etc/wg-sockd/config.yaml`:

```yaml
auth:
  basic:
    enabled: true
    username: admin
    password_hash: "$2a$12$..."  # output from step 1
  session_ttl: 15m
  skip_unix_socket: true
  secure_cookies: auto
  max_sessions: 100
```

**Step 3** — Restart the agent:

```bash
sudo systemctl restart wg-sockd
```

### Bearer Token Auth (API / Automation)

```yaml
auth:
  token:
    enabled: true
    token: "your-random-secret-at-least-32-chars"
```

Use the token in API requests:

```bash
curl --unix-socket /run/wg-sockd/wg-sockd.sock \
  -H "Authorization: Bearer your-random-secret-at-least-32-chars" \
  http://localhost/api/peers
```

### Both Methods Together

```yaml
auth:
  basic:
    enabled: true
    username: admin
    password_hash: "$2a$12$..."
  token:
    enabled: true
    token: "your-random-secret-at-least-32-chars"
  session_ttl: 15m
  skip_unix_socket: true
  secure_cookies: auto
  max_sessions: 100
```

## Configuration Reference

All fields live under the `auth:` key in `config.yaml`.

### `auth.basic`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable username/password authentication |
| `username` | string | — | Login username |
| `password_hash` | string | — | Bcrypt hash of the password. Generate with `wg-sockd-ctl hash-password` |

### `auth.token`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable Bearer token authentication |
| `token` | string | — | Secret token value. Minimum 32 characters recommended |

### `auth` (session settings)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `session_ttl` | duration | `15m` | Session lifetime. Valid range: `5m` – `720h` |
| `skip_unix_socket` | bool | `true` | Bypass authentication for requests arriving on the Unix socket |
| `secure_cookies` | string | `auto` | Cookie `Secure` flag. `auto` detects HTTPS via `X-Forwarded-Proto`; `true` forces it; `false` disables it |
| `max_sessions` | int | `100` | Maximum number of concurrent active sessions |

## CLI Authentication

The CLI (`wg-sockd-ctl`) communicates over the Unix socket by default. When `skip_unix_socket: true` (the default), no credentials are needed:

```bash
wg-sockd-ctl peers list
```

If you set `skip_unix_socket: false`, or when using the CLI against a remote TCP endpoint with token auth enabled, pass the token:

```bash
# Via flag
wg-sockd-ctl --token "your-secret" peers list

# Via environment variable
export WG_SOCKD_AUTH_TOKEN="your-secret"
wg-sockd-ctl peers list
```

## Environment Variables

All `auth` config fields can be overridden with environment variables. This is the recommended approach for Kubernetes and container deployments.

| Variable | Type | Description |
|----------|------|-------------|
| `WG_SOCKD_AUTH_BASIC_ENABLED` | bool | Enable basic auth (`true`/`false`) |
| `WG_SOCKD_AUTH_BASIC_USERNAME` | string | Username for basic auth |
| `WG_SOCKD_AUTH_BASIC_PASSWORD_HASH` | string | Bcrypt password hash |
| `WG_SOCKD_AUTH_TOKEN_ENABLED` | bool | Enable token auth (`true`/`false`) |
| `WG_SOCKD_AUTH_TOKEN` | string | Bearer token value |
| `WG_SOCKD_AUTH_SESSION_TTL` | duration | Session lifetime (e.g. `15m`, `1h`) |
| `WG_SOCKD_AUTH_SKIP_UNIX_SOCKET` | bool | Skip auth for Unix socket (`true`/`false`) |
| `WG_SOCKD_AUTH_SECURE_COOKIES` | string | Cookie Secure flag (`auto`/`true`/`false`) |
| `WG_SOCKD_AUTH_MAX_SESSIONS` | int | Maximum concurrent sessions |
| `WG_SOCKD_AUTH_WEBAUTHN_ENABLED` | bool | Enable WebAuthn/passkeys (`true`/`false`) |
| `WG_SOCKD_AUTH_WEBAUTHN_ORIGIN` | string | WebAuthn origin URL (e.g. `https://vpn.example.com`) |

Bool values accept: `true`, `false`, `1`, `0`, `t`, `f` (case-insensitive).

## Kubernetes

For Kubernetes deployments, store sensitive values in a Secret and reference it from the Helm values.

**Create the Secret:**

```bash
kubectl create secret generic wg-sockd-auth \
  --from-literal=WG_SOCKD_AUTH_BASIC_PASSWORD_HASH='$2a$12$...' \
  --from-literal=WG_SOCKD_AUTH_TOKEN='your-secret-token'
```

**Helm `values.yaml`:**

```yaml
auth:
  basic:
    enabled: true
    username: admin
  token:
    enabled: true
  secretName: "wg-sockd-auth"
```

## Reverse Proxy

When running behind a reverse proxy (nginx, Traefik, Caddy):

- Ensure `Authorization` and `Cookie` headers are forwarded — most proxies do this by default.
- Set `X-Forwarded-Proto: https` so that `secure_cookies: auto` activates the `Secure` flag on cookies.
- Do not expose the agent's Unix socket path directly; route only through the proxy.

## Security Considerations

- **Token length** — use at least 32 random characters. The agent logs a warning for shorter tokens.
- **Password hashing** — bcrypt is used with a cost factor of 12. Do not store the plaintext password anywhere.
- **Login rate limiting** — failed login attempts are limited to 5 per 60 seconds per source IP. Unix socket requests bypass this limiter.
- **No auth configured** — the agent logs a `WARN` at startup and the API is accessible to anyone who can reach the socket. For internet-exposed deployments, always enable at least one auth method.
- **Unix socket access** — file permissions (`0660`, group `wg-sockd`) are the primary access control layer when `skip_unix_socket: true`. Only add users to the `wg-sockd` group who should have full API access.

