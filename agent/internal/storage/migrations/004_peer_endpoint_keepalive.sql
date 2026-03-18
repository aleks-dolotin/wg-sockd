-- Migration 004: Add endpoint, persistent_keepalive, client_dns, client_mtu
-- to peers and profiles tables for full peer configuration support.
--
-- peers: endpoint/persistent_keepalive → server wg0.conf [Peer] section
--        client_dns/client_mtu → client download conf [Interface] section
-- profiles: same fields as profile-level defaults for peers
--
-- NULL integer = inherit from profile → global default (4-level cascade).
-- Empty string endpoint = not set (omitted from conf).

ALTER TABLE peers ADD COLUMN endpoint TEXT NOT NULL DEFAULT '';
ALTER TABLE peers ADD COLUMN persistent_keepalive INTEGER;
ALTER TABLE peers ADD COLUMN client_dns TEXT NOT NULL DEFAULT '';
ALTER TABLE peers ADD COLUMN client_mtu INTEGER;

ALTER TABLE profiles ADD COLUMN endpoint TEXT NOT NULL DEFAULT '';
ALTER TABLE profiles ADD COLUMN persistent_keepalive INTEGER;
ALTER TABLE profiles ADD COLUMN client_dns TEXT NOT NULL DEFAULT '';
ALTER TABLE profiles ADD COLUMN client_mtu INTEGER;
