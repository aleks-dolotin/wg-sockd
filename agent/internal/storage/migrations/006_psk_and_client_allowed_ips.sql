-- Migration 006: PresharedKey lifecycle + split-tunnel client_allowed_ips
-- Phase 2 of client conf improvements.
-- All new columns use empty string defaults — backward compatible with existing peers.

ALTER TABLE peers ADD COLUMN preshared_key TEXT NOT NULL DEFAULT '';
ALTER TABLE peers ADD COLUMN client_allowed_ips TEXT NOT NULL DEFAULT '';

ALTER TABLE profiles ADD COLUMN client_allowed_ips TEXT NOT NULL DEFAULT '';
ALTER TABLE profiles ADD COLUMN use_preshared_key BOOLEAN NOT NULL DEFAULT 0;

