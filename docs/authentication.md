# Authentication

wg-sockd supports optional HTTP authentication for the agent API and embedded web UI. By default, authentication is **disabled** — the API is protected only by Unix socket file permissions (group `wg-sockd`).

## Authentication Methods

Two password-based methods are available and can be enabled independently or together. A third method — **Passkeys (WebAuthn)** — can be layered on top of Basic Auth for passwordless login.

| Method | Use case |
|--------|----------|
| **Basic** (username + password) | Web UI login, interactive access |
| **Bearer Token** | Automation, scripts, CI/CD pipelines |
| **Passkeys / WebAuthn** | Passwordless browser login (Touch ID, Face ID, security keys) |

When both Basic and Token methods are enabled, either one grants access. Passkeys are an *additional* method on top of Basic Auth — they cannot replace it.

## How Authentication Works

```mermaid
flowchart TD
    R[Incoming request] --> US{Unix socket and skip_unix_socket?}
    US -->|Yes| OK[Pass through]
    US -->|No| SC{Valid session cookie?}
    SC -->|Yes| OK
    SC -->|No| BT{Valid Bearer token?}
    BT -->|Yes| OK
    BT -->|No| HP{Path is /api/health?}
    HP -->|Yes| OK
    HP -->|No| CN{Accepts text/html?}
    CN -->|Yes| RD[302 redirect to /login]
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

### Passkeys / WebAuthn

Passkeys allow signing in with Touch ID, Face ID, or a hardware security key — no password typed. The browser stores the passkey and uses platform authenticators (macOS/iOS/Android biometrics) or cross-platform authenticators (YubiKey, etc.).

> **Requirement:** `auth.basic.enabled: true` **must** be set alongside WebAuthn. Passkeys cannot be the sole auth method — Basic Auth is always available as a recovery option.

**Step 1** — Enable WebAuthn in config:

```yaml
auth:
  basic:
    enabled: true
    username: admin
    password_hash: "$2a$12$..."
  webauthn:
    enabled: true
    origin: "https://vpn.example.com"    # Must match the browser URL exactly
    display_name: "My VPN"               # Optional — shown in browser passkey prompts
  session_ttl: 15m
  secure_cookies: auto
```

**Step 2** — Restart the agent and open the Web UI.

**Step 3** — Log in with your password, then navigate to **Settings → Passkeys** and click **Add Passkey**. Complete the Touch ID / Face ID / security key prompt.

**Step 4** — On next login, the browser offers the passkey automatically (Conditional UI: passkey suggestion appears in the username field). You can also click **Sign in with Passkey** explicitly.

#### How Passkey Login Works

```mermaid
sequenceDiagram
    participant B as Browser
    participant S as wg-sockd

    Note over B: LoginPage loads
    B->>S: POST /api/auth/webauthn/login/begin
    S-->>B: { publicKey options, token }
    B->>B: navigator.credentials.get() → Touch ID / Face ID
    B->>S: POST /api/auth/webauthn/login/finish { credential, token }
    S->>S: Verify signature, update sign_count
    S-->>B: Session cookie + { username, expires_at }
    B->>B: Redirect to dashboard
```

#### How Passkey Registration Works

```mermaid
sequenceDiagram
    participant B as Browser (Settings page)
    participant S as wg-sockd

    Note over B: Admin is logged in via password
    B->>S: POST /api/auth/webauthn/register/begin { friendly_name? }
    S-->>B: { publicKey options, token }
    B->>B: navigator.credentials.create() → Touch ID
    B->>S: POST /api/auth/webauthn/register/finish { credential, token }
    S->>S: Verify + store in SQLite
    S-->>B: { status: "ok", credential_id, friendly_name }
```

#### Origin Configuration

The `origin` value **must exactly match** the URL shown in the browser's address bar:

| Scenario | Correct origin |
|----------|---------------|
| Direct access on port 8080 | `http://localhost:8080` |
| Behind reverse proxy, HTTPS | `https://vpn.example.com` |
| Custom port with HTTPS | `https://vpn.example.com:8443` |

> **Security note:** WebAuthn requires a secure context. The agent logs a `WARN` if `origin` starts with `http://` (non-HTTPS). For production deployments, always use `https://`.

#### Known Limitations

- Single admin user only — multi-user support is out of scope.
- Passkeys registered while using one browser/device may not be available on others unless platform sync is enabled (e.g. iCloud Keychain, Google Password Manager).
- If the server restarts during a passkey ceremony, the challenge expires and the browser shows an error. Clicking **Sign in with Passkey** again fetches a fresh challenge.
- Conditional UI (automatic passkey suggestion in username field) requires Chrome 108+, Safari 16+, or Firefox 122+.

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

### `auth.webauthn`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable WebAuthn/Passkey authentication |
| `origin` | string | — | **Required when enabled.** Full URL of the web UI as seen in the browser (e.g. `https://vpn.example.com`). Trailing slash is stripped automatically. |
| `display_name` | string | hostname from origin | Human-readable name shown in browser passkey prompts. Falls back to hostname extracted from `origin`. |

> `auth.webauthn.enabled: true` requires `auth.basic.enabled: true`. The agent refuses to start if WebAuthn is enabled without Basic Auth.

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

- **Token length** — tokens shorter than 32 characters cause a fatal startup error. Override with `auth.token.allow_weak: true` (not recommended for production).
- **Password hashing** — bcrypt is used with a cost factor of 12. Do not store the plaintext password anywhere.
- **Login rate limiting** — failed login attempts are limited to 5 per 60 seconds per source IP. Unix socket requests bypass this limiter. WebAuthn `login/begin` is subject to the same rate limit.
- **No auth configured** — the agent logs a `WARN` at startup and the API is accessible to anyone who can reach the socket. For internet-exposed deployments, always enable at least one auth method.
- **Unix socket access** — file permissions (`0660`, group `wg-sockd`) are the primary access control layer when `skip_unix_socket: true`. Only add users to the `wg-sockd` group who should have full API access.
- **WebAuthn challenges** — challenges are one-time use and expire after 60 seconds. The server stores up to 100 pending challenges in memory (LRU eviction). Challenges are stored in memory only and do not survive a server restart.
- **Passkey credential IDs** — the server never returns credential IDs to unauthenticated callers (empty `allowCredentials` in login options). This prevents credential enumeration.
- **Sign count monitoring** — if an authenticator's sign count is ≤ the stored value, the agent logs a `WARN` (possible credential cloning) but does not block authentication. Monitor server logs for these warnings.
- **Last passkey warning** — the Settings UI warns when deleting the only remaining passkey. Passkeys can always be replaced by re-registering after password login.

