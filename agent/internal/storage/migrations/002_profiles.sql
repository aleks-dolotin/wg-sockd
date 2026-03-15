CREATE TABLE IF NOT EXISTS profiles (
    name TEXT PRIMARY KEY,
    display_name TEXT NOT NULL DEFAULT '',
    allowed_ips TEXT NOT NULL DEFAULT '[]',
    exclude_ips TEXT NOT NULL DEFAULT '[]',
    description TEXT NOT NULL DEFAULT '',
    is_default BOOLEAN NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Enforce FK: peers.profile → profiles.name via triggers.
-- SQLite cannot ALTER TABLE ADD FOREIGN KEY on existing columns,
-- and the peers table was created without REFERENCES in 001_init.sql.

CREATE TRIGGER IF NOT EXISTS fk_peers_profile_insert
BEFORE INSERT ON peers
WHEN NEW.profile IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'FOREIGN KEY constraint failed: peers.profile references profiles.name')
    WHERE NOT EXISTS (SELECT 1 FROM profiles WHERE name = NEW.profile);
END;

CREATE TRIGGER IF NOT EXISTS fk_peers_profile_update
BEFORE UPDATE OF profile ON peers
WHEN NEW.profile IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'FOREIGN KEY constraint failed: peers.profile references profiles.name')
    WHERE NOT EXISTS (SELECT 1 FROM profiles WHERE name = NEW.profile);
END;

CREATE TRIGGER IF NOT EXISTS fk_profiles_delete
BEFORE DELETE ON profiles
BEGIN
    SELECT RAISE(ABORT, 'FOREIGN KEY constraint failed: profiles.name is referenced by peers')
    WHERE EXISTS (SELECT 1 FROM peers WHERE profile = OLD.name);
END;

