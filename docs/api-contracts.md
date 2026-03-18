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
| PUT | `/api/peers/{id}` | Update peer |
| DELETE | `/api/peers/{id}` | Delete peer |
| POST | `/api/peers/{id}/rotate-keys` | Rotate peer keypair |
| POST | `/api/peers/{id}/approve` | Approve auto-discovered peer |
| POST | `/api/peers/batch` | Batch create multiple peers |
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
  "sqlite": "ok"
}
```

## Stats

### GET /api/stats

Returns aggregate peer statistics.

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

### GET /api/peers

List all peers.

**Response 200:** Array of peer objects with fields: id, public_key, friendly_name, profile, allowed_ips, enabled, auto_discovered, last_handshake, rx_bytes, tx_bytes, created_at, updated_at.

### POST /api/peers

Create a new peer. Supports two modes: profile-based or custom IPs.

**Request (profile-based):**
```json
{
  "friendly_name": "alice-phone",
  "profile": "full-tunnel"
}
```

**Request (custom IPs):**
```json
{
  "friendly_name": "custom-peer",
  "allowed_ips": ["10.0.0.0/24"]
}
```

**Response 201:** Peer object including `private_key`, `config` (full WireGuard client conf), and `qr` (base64 PNG) — all one-time, never stored.

### PUT /api/peers/{id}

Update peer metadata (friendly_name, notes, enabled status).

**Request:**
```json
{
  "friendly_name": "bob-laptop-new",
  "notes": "Updated name"
}
```

### DELETE /api/peers/{id}

Remove peer from database and WireGuard kernel.


### POST /api/peers/{id}/rotate-keys

Generate new keypair for peer. Returns `{ public_key, config, qr }` — config and QR are one-time, never stored. Old keys are invalidated atomically.

### POST /api/peers/{id}/approve

Approve an auto-discovered peer (auto_discovered=true → enabled=true).

### POST /api/peers/batch

Create multiple peers in one request.

**Request:**
```json
{
  "peers": [
    {"friendly_name": "peer1", "profile": "nas-only"},
    {"friendly_name": "peer2", "profile": "nas-only"}
  ]
}
```

## Profiles

### GET /api/profiles

List all profiles with resolved_allowed_ips.

### POST /api/profiles

Create a new profile.

**Request:**
```json
{
  "name": "media-only",
  "display_name": "Media Server",
  "allowed_ips": ["192.168.1.0/24"],
  "exclude_ips": ["192.168.1.1/32"],
  "description": "Access media server only"
}
```

### PUT /api/profiles/{name}

Update profile fields.

### DELETE /api/profiles/{name}

Delete profile. Fails with 409 if peers reference this profile.

## Error Responses

| Status | Meaning |
|--------|---------|
| 400 | Bad request — validation error (JSON body details) |
| 404 | Peer or profile not found |
| 409 | Conflict — profile in use (cannot delete) |
| 429 | Rate limit exceeded (Retry-After: 1 header) |
| 503 | Service degraded — disk full (writes blocked, reads OK) |

## Authentication

None. Access is controlled by Unix socket file permissions (0660, group `wg-sockd`). Only processes with the correct group membership can connect.

## Rate Limiting

In-memory token bucket: 10 req/s per connection (configurable). `/api/health` is always exempt.
Set `rate_limit: 0` in config to disable.
