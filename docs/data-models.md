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
| friendly_name | TEXT | Human-readable name |
| profile | TEXT | Profile name reference (nullable for custom peers) |
| allowed_ips | TEXT | JSON array of CIDR strings |
| enabled | BOOLEAN DEFAULT true | Whether peer is active in WireGuard kernel |
| auto_discovered | BOOLEAN DEFAULT false | True if detected by reconciler (unknown peer) |
| notes | TEXT | Free-text admin notes |
| last_handshake | DATETIME | Last WireGuard handshake time (updated by reconciler) |
| rx_bytes | INTEGER DEFAULT 0 | Received bytes (updated by reconciler) |
| tx_bytes | INTEGER DEFAULT 0 | Transmitted bytes (updated by reconciler) |
| created_at | DATETIME DEFAULT CURRENT_TIMESTAMP | Record creation time |
| updated_at | DATETIME DEFAULT CURRENT_TIMESTAMP | Last modification time |

### profiles

Reusable network access templates with CIDR exclusion.

| Column | Type | Description |
|--------|------|-------------|
| name | TEXT PK | Unique profile identifier (e.g., "full-tunnel") |
| display_name | TEXT | Human-readable label (e.g., "Full Tunnel") |
| allowed_ips | TEXT | JSON array of allowed CIDRs |
| exclude_ips | TEXT | JSON array of CIDRs to subtract from allowed_ips |
| resolved_allowed_ips | TEXT | Computed result after CIDR exclusion (cached) |
| description | TEXT | Profile description |
| created_at | DATETIME DEFAULT CURRENT_TIMESTAMP | Record creation time |
| updated_at | DATETIME DEFAULT CURRENT_TIMESTAMP | Last modification time |

## Relationships

```
profiles.name ←── peers.profile (many peers can use one profile)
```

Profile deletion is blocked (409 Conflict) if any peers reference it.

## Seeding

On first start, if the profiles table is empty, profiles are seeded from `config.yaml`:

```yaml
peer_profiles:
  - name: full-tunnel
    display_name: "Full Tunnel"
    allowed_ips: ["0.0.0.0/0", "::/0"]
  - name: nas-only
    display_name: "NAS Only"
    allowed_ips: ["192.168.1.0/24"]
    exclude_ips: ["192.168.1.1/32"]
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
AllowedIPs = 0.0.0.0/0, ::/0
```

These comments serve as a secondary recovery source if both the database and backup are corrupted.
