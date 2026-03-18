-- Migration 005: Add client_address and last_seen_endpoint columns to peers.
-- client_address: the client's VPN IP (CIDR) used as [Interface] Address in client download conf.
-- last_seen_endpoint: informational runtime endpoint updated by reconciler (not written to wg0.conf).

ALTER TABLE peers ADD COLUMN client_address TEXT NOT NULL DEFAULT '';
ALTER TABLE peers ADD COLUMN last_seen_endpoint TEXT NOT NULL DEFAULT '';

-- Partial unique index: prevents duplicate client_address assignments.
-- Empty string is excluded so legacy peers (pre-migration) don't conflict.
CREATE UNIQUE INDEX IF NOT EXISTS idx_peers_client_address ON peers(client_address) WHERE client_address != '';
