# wg-sockd — Data Models

## Database

SQLite (modernc.org/sqlite — pure Go, no CGO) stored at `/var/lib/wg-sockd/wg-sockd.db`.

## Tables

### peers

Primary table for WireGuard peer management.

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER PK AUTOINCREMENT | Unique peer identifier |
| public_key | TEXT UNIQUE NOT NULL | WireGuard public key |
| friendly_name | TEXT NOT NULL DEFAULT '' | Human-readable name |
| allowed_ips | TEXT NOT NULL DEFAULT '' | JSON array of server-side CIDR strings |
| profile | TEXT | Profile name reference (nullable for custom peers) |
| enabled | BOOLEAN NOT NULL DEFAULT 1 | Whether peer is active in WireGuard kernel |
| auto_discovered | BOOLEAN NOT NULL DEFAULT 0 | True if detected by reconciler (unknown peer) |
| created_at | DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP | Record creation time |
| notes | TEXT NOT NULL DEFAULT '' | Free-text admin notes |
| endpoint | TEXT NOT NULL DEFAULT '' | Configured static endpoint (host:port) for server wg.conf |
| persistent_keepalive | INTEGER | Keepalive interval in seconds (nullable) |
| client_dns | TEXT NOT NULL DEFAULT '' | DNS servers for client .conf |
| client_mtu | INTEGER | MTU for client .conf (nullable) |
| client_address | TEXT NOT NULL DEFAULT '' | Tunnel IP in CIDR (e.g. 10.0.10.3/24). Unique index (non-empty values). |
| last_seen_endpoint | TEXT NOT NULL DEFAULT '' | Last known IP:port from WireGuard kernel |
| preshared_key | TEXT NOT NULL DEFAULT '' | Encrypted PSK (never exposed via API) |
| client_allowed_ips | TEXT NOT NULL DEFAULT '' | Client-side AllowedIPs for .conf download |

**Indexes:**

- `idx_peers_public_key` — unique on `public_key`
- `idx_peers_client_address` — unique on `client_address` where `client_address != ''`

**Triggers:**

- `fk_peers_profile_insert` — blocks INSERT if profile doesn't exist in profiles table
- `fk_peers_profile_update` — blocks UPDATE if new profile doesn't exist in profiles table

### profiles

Reusable network access templates with CIDR exclusion.

| Column | Type | Description |
|--------|------|-------------|
| name | TEXT PK | Unique profile identifier (e.g. "full-tunnel") |
| allowed_ips | TEXT NOT NULL DEFAULT '[]' | JSON array of allowed CIDRs |
| exclude_ips | TEXT NOT NULL DEFAULT '[]' | JSON array of CIDRs to subtract from allowed_ips |
| description | TEXT NOT NULL DEFAULT '' | Profile description |
| is_default | BOOLEAN NOT NULL DEFAULT 0 | Whether this is the default profile |
| created_at | DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP | Record creation time |
| persistent_keepalive | INTEGER | Default keepalive for peers using this profile (nullable) |
| client_dns | TEXT NOT NULL DEFAULT '' | Default DNS for peers using this profile |
| client_mtu | INTEGER | Default MTU for peers using this profile (nullable) |
| use_preshared_key | BOOLEAN NOT NULL DEFAULT 0 | Auto-generate PSK for new peers using this profile |

**Triggers:**

- `fk_profiles_delete` — blocks DELETE if any peers reference this profile

## Relationships

```
profiles.name ←── peers.profile (many peers can use one profile)
```

Profile deletion is blocked (409 Conflict) if any peers reference it.
Foreign key constraints enforced via triggers (not SQLite FK pragma).

## Seeding

On first start, if the profiles table is empty, profiles are seeded from `config.yaml`:

```yaml
peer_profiles:
  - name: full-tunnel
    allowed_ips: ["0.0.0.0/0", "::/0"]
    description: "Route all traffic through VPN"
    persistent_keepalive: 25
    client_dns: "1.1.1.1"
    use_preshared_key: true
  - name: nas-only
    allowed_ips: ["192.168.1.0/24"]
    exclude_ips: ["192.168.1.1/32"]
    description: "Access NAS only"
```

After initial seed, the database is the source of truth — profiles are managed via API/UI.

## CIDR Exclusion

Profiles use CIDR set math to compute `resolved_allowed_ips`:

```
resolved = allowed_ips - exclude_ips
```

Uses `go4.org/netipx.IPSetBuilder` for IP range subtraction. Example:

- Input: `allowed=["0.0.0.0/0"]`, `exclude=["192.168.0.0/16", "10.0.0.0/8"]`
- Result: Internet-only routing (all traffic except private ranges)

## Server-side AllowedIPs

Server-side `[Peer] AllowedIPs` in wg.conf are auto-derived as `/32` from `client_address` (e.g. `client_address: 10.0.10.3/24` → `AllowedIPs = 10.0.10.3/32`). This prevents a client peer from spoofing source IPs from entire subnets.

## Backup and Recovery

- **Hourly backup:** `.db` → `.db.bak` with fsync
- **Recovery chain (on corruption):**
  1. Restore from `.db.bak`
  2. Parse peer data from wg0.conf metadata comments (`# wg-sockd:` prefix)
  3. Clean start with empty database (profiles re-seeded from config)
- Health endpoint reports recovery source if non-standard start

## Conf File as Secondary Store

The conf writer embeds peer metadata as comments in wg0.conf:

```ini
# wg-sockd:id=1,name=alice-phone,profile=full-tunnel
[Peer]
PublicKey = ...
AllowedIPs = 10.0.10.3/32
```

These comments serve as a secondary recovery source if both the database and backup are corrupted.
