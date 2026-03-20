# wg-sockd — API Contracts

All endpoints are served over Unix domain socket at `/var/run/wg-sockd/wg-sockd.sock`.
No TCP exposure by default. Use `curl --unix-socket` for direct access.

## Endpoints Summary

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/health` | Health check (WireGuard, SQLite, disk) |
| GET | `/api/stats` | Aggregate statistics (peer counts, traffic) |
| GET | `/api/peers` | List all peers |
| POST | `/api/peers` | Create peer (with profile or custom IPs) |
| GET | `/api/peers/{id}` | Get single peer with live wgctrl data |
| PUT | `/api/peers/{id}` | Update peer |
| DELETE | `/api/peers/{id}` | Delete peer |
| POST | `/api/peers/{id}/rotate-keys` | Rotate peer keypair |
| POST | `/api/peers/{id}/approve` | Approve and onboard auto-discovered peer |
| POST | `/api/peers/batch` | Batch create multiple peers |
| GET | `/api/peers/next-address` | Next available tunnel IP address |
| GET | `/api/profiles` | List all profiles |
| POST | `/api/profiles` | Create profile |
| PUT | `/api/profiles/{name}` | Update profile |
| DELETE | `/api/profiles/{name}` | Delete profile (fails if peers use it) |

## Health

### GET /api/health

Returns aggregated system health. Always exempted from rate limiting.

**Response 200:**
```json
{
  "status": "ok",
  "wireguard": "ok",
  "sqlite": "ok",
  "disk_ok": true,
  "conf_writable": true
}
```

| Field | Type | Description |
|-------|------|-------------|
| status | string | Overall status: `"ok"` or `"degraded"` |
| wireguard | string | WireGuard subsystem status |
| sqlite | string | Database subsystem status |
| sqlite_recovered_from | string | Present if DB was recovered on startup (e.g. `"backup"`, `"conf"`) |
| disk_ok | bool | Disk space above threshold |
| conf_writable | bool | WireGuard conf file is writable |

## Stats

### GET /api/stats

Returns aggregate peer statistics from live wgctrl data.

**Response 200:**
```json
{
  "total_peers": 5,
  "online_peers": 2,
  "total_rx": 1048576,
  "total_tx": 524288
}
```

## Peers

### Peer Object

All peer endpoints return or accept a subset of these fields:

| Field | Type | Description |
|-------|------|-------------|
| id | int | Unique peer identifier |
| public_key | string | WireGuard public key (base64) |
| friendly_name | string | Human-readable name |
| allowed_ips | string[] | Server-side AllowedIPs (auto-derived as /32 from client_address) |
| profile | string\|null | Profile name reference (null for custom peers) |
| enabled | bool | Whether peer is active in WireGuard kernel |
| auto_discovered | bool | True if detected by reconciler (unknown peer) |
| created_at | datetime | Record creation time |
| notes | string | Free-text admin notes |
| endpoint | string | Runtime endpoint from `wg show` (read-only) |
| latest_handshake | datetime\|null | Last WireGuard handshake time |
| transfer_rx | int | Received bytes (live from wgctrl) |
| transfer_tx | int | Transmitted bytes (live from wgctrl) |
| configured_endpoint | string | Static endpoint for server-side wg.conf `[Peer]` (host:port) |
| persistent_keepalive | int\|null | Keepalive interval in seconds |
| client_dns | string | DNS servers for client .conf `[Interface] DNS` |
| client_mtu | int\|null | MTU for client .conf `[Interface] MTU` |
| client_address | string | Tunnel IP for this peer (CIDR, e.g. `10.0.10.3/24`) |
| last_seen_endpoint | string | Last known IP:port from WireGuard kernel (read-only) |
| has_preshared_key | bool | Whether a PSK is set (value never exposed in GET) |
| client_allowed_ips | string | Client-side AllowedIPs for client .conf `[Peer] AllowedIPs` |

### GET /api/peers

List all peers with live wgctrl data merged.

**Response 200:** Array of peer objects.

### GET /api/peers/{id}

Get a single peer by ID with live wgctrl data.

**Response 200:** Peer object.

### POST /api/peers

Create a new peer.

**Request:**
```json
{
  "friendly_name": "alice-phone",
  "profile": "full-tunnel",
  "client_address": "10.0.10.3/24",
  "client_allowed_ips": "0.0.0.0/0, ::/0",
  "configured_endpoint": "",
  "persistent_keepalive": 25,
  "client_dns": "1.1.1.1",
  "client_mtu": 1420,
  "preshared_key": "auto"
}
```

| Field | Required | Description |
|-------|----------|-------------|
| friendly_name | yes | Human-readable name |
| profile | no | Profile name — pre-fills client defaults |
| allowed_ips | no | Server-side AllowedIPs (legacy — prefer client_address) |
| client_address | yes | Tunnel IP in CIDR (e.g. `10.0.10.3/24`). Server AllowedIPs auto-derived as /32. |
| client_allowed_ips | yes | Client-side AllowedIPs for .conf download |
| configured_endpoint | no | Static endpoint (host:port) for server wg.conf |
| persistent_keepalive | no | Keepalive interval in seconds |
| client_dns | no | DNS servers for client .conf |
| client_mtu | no | MTU for client .conf |
| preshared_key | no | `"auto"` = generate, base64 = use explicit, `""` = none |

**Response 201:** Peer object including one-time fields `private_key`, `config` (full WireGuard client conf), and `qr` (base64 PNG) — never stored.

### PUT /api/peers/{id}

Update peer fields. All fields are optional — only included fields are updated. Pointer-to-pointer types support setting to `null` (e.g. clearing persistent_keepalive).

**Request:**
```json
{
  "friendly_name": "alice-laptop-new",
  "notes": "Renamed device",
  "enabled": true,
  "profile": "full-tunnel",
  "client_address": "10.0.10.4/24",
  "client_allowed_ips": "10.0.0.0/8",
  "configured_endpoint": "vpn.example.com:51820",
  "persistent_keepalive": 25,
  "client_dns": "1.1.1.1, 8.8.8.8",
  "client_mtu": 1420
}
```

| Field | Type | Description |
|-------|------|-------------|
| friendly_name | string | Update display name |
| allowed_ips | string[] | Update server-side AllowedIPs |
| profile | string\|null | Set profile or `null` to clear |
| enabled | bool | Enable/disable peer |
| notes | string | Update notes |
| configured_endpoint | string | Update static endpoint |
| persistent_keepalive | int\|null | Update keepalive or `null` to clear |
| client_dns | string | Update DNS |
| client_mtu | int\|null | Update MTU or `null` to clear |
| client_address | string | Update tunnel address |
| client_allowed_ips | string | Update client AllowedIPs |

**Response 200:** Updated peer object.

### DELETE /api/peers/{id}

Remove peer from database and WireGuard kernel.

**Response 204:** No content.

### POST /api/peers/{id}/rotate-keys

Generate new keypair for peer. Old keys are invalidated atomically.

**Response 200:** `{ public_key, config, qr }` — config and QR are one-time, never stored.

### POST /api/peers/{id}/approve

Approve and onboard an auto-discovered peer (`auto_discovered=true` → `enabled=true`). Accepts full peer configuration for onboarding.

**Request:**
```json
{
  "friendly_name": "bob-phone",
  "profile": "full-tunnel",
  "client_address": "10.0.10.5/24",
  "client_allowed_ips": "0.0.0.0/0, ::/0",
  "allowed_ips": ["10.0.10.5/32"],
  "configured_endpoint": "",
  "client_dns": "1.1.1.1",
  "client_mtu": 1420,
  "persistent_keepalive": 25
}
```

| Field | Required | Description |
|-------|----------|-------------|
| friendly_name | no | Set display name on approval |
| profile | no | Assign profile |
| client_address | yes | Tunnel IP in CIDR |
| client_allowed_ips | yes | Client-side AllowedIPs |
| allowed_ips | no | Server-side AllowedIPs |
| configured_endpoint | no | Static endpoint |
| client_dns | no | DNS for client .conf |
| client_mtu | no | MTU for client .conf |
| persistent_keepalive | no | Keepalive interval |

**Response 200:** Updated peer object.

### POST /api/peers/batch

Create multiple peers in one request.

**Request:**
```json
{
  "peers": [
    {
      "friendly_name": "peer1",
      "profile": "nas-only",
      "client_address": "10.0.10.10/24",
      "client_allowed_ips": "192.168.1.0/24"
    },
    {
      "friendly_name": "peer2",
      "profile": "nas-only",
      "client_address": "10.0.10.11/24",
      "client_allowed_ips": "192.168.1.0/24"
    }
  ]
}
```

Each item in the `peers` array follows the same schema as `POST /api/peers`.

### GET /api/peers/next-address

Returns the next available tunnel IP address within the WireGuard interface subnet.
Reads the subnet from the OS via `net.InterfaceByName`. Used by the UI to auto-fill
the Tunnel Address field when creating a new peer.

**Response 200:**
```json
{
  "next_address": "10.0.10.6/24"
}
```

**Response 404** — WireGuard interface not available (dev mode):
```json
{
  "error": "interface_not_found",
  "message": "Interface \"wg1\" not available: route ip+net: no such network interface"
}
```

**Response 409** — Subnet exhausted:
```json
{
  "error": "subnet_full",
  "message": "No free addresses in 10.0.10.0/24 (253/253 used)"
}
```

**Response 501** — IPv6-only interface (not implemented):
```json
{
  "error": "ipv6_not_supported",
  "message": "Only IPv4 subnets are supported"
}
```

---

## Profiles

### Profile Object

| Field | Type | Description |
|-------|------|-------------|
| name | string | Unique profile identifier (e.g. `"full-tunnel"`) |
| allowed_ips | string[] | Allowed CIDRs for server-side routing |
| exclude_ips | string[] | CIDRs to subtract from allowed_ips |
| resolved_allowed_ips | string[] | Computed result after CIDR exclusion |
| description | string | Profile description |
| is_default | bool | Whether this is the default profile |
| route_count | int | Number of resolved routes |
| route_warning | string | Warning if route count is high |
| peer_count | int | Number of peers using this profile |
| persistent_keepalive | int\|null | Default keepalive for peers using this profile |
| client_dns | string | Default DNS for peers using this profile |
| client_mtu | int\|null | Default MTU for peers using this profile |
| use_preshared_key | bool | Auto-generate PSK for new peers using this profile |

### GET /api/profiles

List all profiles with resolved_allowed_ips and peer counts.

**Response 200:** Array of profile objects.

### POST /api/profiles

Create a new profile.

**Request:**
```json
{
  "name": "media-only",
  "allowed_ips": ["192.168.1.0/24"],
  "exclude_ips": ["192.168.1.1/32"],
  "description": "Access media server only",
  "persistent_keepalive": 25,
  "client_dns": "1.1.1.1",
  "client_mtu": 1420,
  "use_preshared_key": true
}
```

### PUT /api/profiles/{name}

Update profile fields. All fields are optional.

**Request:**
```json
{
  "allowed_ips": ["10.0.0.0/8"],
  "exclude_ips": ["10.0.1.0/24"],
  "description": "Updated description",
  "persistent_keepalive": 30,
  "client_dns": "8.8.8.8",
  "client_mtu": null,
  "use_preshared_key": false
}
```

| Field | Type | Description |
|-------|------|-------------|
| allowed_ips | string[] | Update allowed CIDRs |
| exclude_ips | string[] | Update excluded CIDRs |
| description | string | Update description |
| persistent_keepalive | int\|null | Update default keepalive or `null` to clear |
| client_dns | string | Update default DNS |
| client_mtu | int\|null | Update default MTU or `null` to clear |
| use_preshared_key | bool | Update PSK auto-generation |

### DELETE /api/profiles/{name}

Delete profile. Fails with 409 if peers reference this profile.

**Response 204:** No content.

## Error Responses

All errors return JSON with `error` and optional `message` fields:

```json
{
  "error": "validation_error",
  "message": "friendly_name is required"
}
```

| Status | Meaning |
|--------|---------|
| 400 | Bad request — validation error (JSON body details) |
| 404 | Peer or profile not found |
| 409 | Conflict — profile in use (cannot delete) |
| 429 | Rate limit exceeded (Retry-After: 1 header) |
| 503 | Service degraded — disk full (writes blocked, reads OK) |

## Authentication

Authentication is optional and disabled by default. When enabled, the agent supports two methods:

- **Basic auth** — username/password login via `POST /api/auth/login`, which issues a session cookie (`wg_sockd_session`).
- **Bearer token** — pass `Authorization: Bearer <token>` on every request. No session required.

All `/api/*` endpoints (except `/api/health` and `/api/auth/*`) require authentication when any auth method is enabled. Unix socket requests are exempt by default (`skip_unix_socket: true`).

See [Authentication](./authentication.md) for setup instructions and configuration reference.

## Auth Endpoints

The following endpoints are always registered, even when authentication is disabled.

### POST /api/auth/login

Authenticate with username and password. Returns a session cookie on success.

**Request:**

```json
{
  "username": "admin",
  "password": "your-password"
}
```

**Response 200:**

```json
{
  "username": "admin",
  "expires_at": "2026-03-18T16:00:00Z",
  "session_ttl_seconds": 900
}
```

Sets `wg_sockd_session` cookie (`HttpOnly`, `SameSite=Lax`).

**Error responses:**

| Status | Error code | Meaning |
|--------|------------|---------|
| 400 | `auth_not_configured` | No auth methods enabled on the server |
| 400 | `basic_auth_disabled` | Basic auth is not enabled |
| 401 | `invalid_credentials` | Wrong username or password |
| 429 | `rate_limit_exceeded` | Too many failed attempts (5 per 60 s per IP). `Retry-After: 60` header included |

### POST /api/auth/logout

Invalidate the current session and clear the session cookie.

**Response 200:**

```json
{ "status": "ok" }
```

`DELETE /api/auth/logout` is an alias for the same operation (SameSite workaround).

### GET /api/auth/session

Check the current session status. Use this to determine whether the server requires authentication and whether the current session is valid.

**Response 200 — authenticated:**

```json
{
  "username": "admin",
  "expires_at": "2026-03-18T16:00:00Z",
  "auth_required": true,
  "webauthn_available": false,
  "session_ttl_seconds": 900
}
```

**Response 200 — no auth configured:**

```json
{
  "auth_required": false,
  "webauthn_available": false,
  "session_ttl_seconds": 900
}
```

**Response 401 — not authenticated:**

```json
{
  "error": "unauthorized",
  "auth_required": true,
  "webauthn_available": false,
  "session_ttl_seconds": 900
}
```

## Rate Limiting

In-memory token bucket: 10 req/s per connection (configurable). `/api/health` is always exempt.
Set `rate_limit: 0` in config to disable.
