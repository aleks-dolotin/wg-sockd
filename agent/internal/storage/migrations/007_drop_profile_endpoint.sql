-- Migration 007: Remove endpoint column from profiles table.
-- Profile-level endpoint was a design mistake — endpoint is unique per peer
-- (site-to-site only), not a shared profile default.
-- Peer endpoint field (peers.endpoint) is NOT affected.

ALTER TABLE profiles DROP COLUMN endpoint;
