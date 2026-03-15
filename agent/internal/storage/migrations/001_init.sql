CREATE TABLE IF NOT EXISTS peers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    public_key TEXT UNIQUE NOT NULL,
    friendly_name TEXT NOT NULL DEFAULT '',
    allowed_ips TEXT NOT NULL DEFAULT '',
    profile TEXT,
    enabled BOOLEAN NOT NULL DEFAULT 1,
    auto_discovered BOOLEAN NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    notes TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_peers_public_key ON peers(public_key);
