package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Profile represents a peer profile record in the database.
type Profile struct {
	Name        string
	AllowedIPs  []string
	ExcludeIPs  []string
	Description string
	IsDefault   bool
	CreatedAt   time.Time
}

// ProfileSeed represents a profile to seed from config.yaml.
type ProfileSeed struct {
	Name        string   `yaml:"name"`
	AllowedIPs  []string `yaml:"allowed_ips"`
	ExcludeIPs  []string `yaml:"exclude_ips"`
	Description string   `yaml:"description"`
}

// ListProfiles returns all profiles ordered by name.
func (db *DB) ListProfiles() ([]Profile, error) {
	rows, err := db.conn.Query(`
		SELECT name, allowed_ips, exclude_ips, description, is_default, created_at
		FROM profiles
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("listing profiles: %w", err)
	}
	defer rows.Close()

	var profiles []Profile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, p)
	}
	return profiles, rows.Err()
}

// GetProfile returns a single profile by name.
// Returns sql.ErrNoRows if not found.
func (db *DB) GetProfile(name string) (*Profile, error) {
	var p Profile
	var allowedJSON, excludeJSON string
	err := db.conn.QueryRow(`
		SELECT name, allowed_ips, exclude_ips, description, is_default, created_at
		FROM profiles
		WHERE name = ?
	`, name).Scan(&p.Name, &allowedJSON, &excludeJSON, &p.Description, &p.IsDefault, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(allowedJSON), &p.AllowedIPs); err != nil {
		return nil, fmt.Errorf("parsing allowed_ips JSON for %q: %w", name, err)
	}
	if err := json.Unmarshal([]byte(excludeJSON), &p.ExcludeIPs); err != nil {
		return nil, fmt.Errorf("parsing exclude_ips JSON for %q: %w", name, err)
	}
	return &p, nil
}

// CreateProfile inserts a new profile.
func (db *DB) CreateProfile(p *Profile) error {
	allowedJSON, err := json.Marshal(p.AllowedIPs)
	if err != nil {
		return fmt.Errorf("marshaling allowed_ips: %w", err)
	}
	excludeJSON, err := json.Marshal(p.ExcludeIPs)
	if err != nil {
		return fmt.Errorf("marshaling exclude_ips: %w", err)
	}

	_, err = db.conn.Exec(`
		INSERT INTO profiles (name, allowed_ips, exclude_ips, description, is_default)
		VALUES (?, ?, ?, ?, ?)
	`, p.Name, string(allowedJSON), string(excludeJSON), p.Description, p.IsDefault)
	if err != nil {
		return fmt.Errorf("creating profile: %w", err)
	}
	return nil
}

// UpdateProfile updates an existing profile by name.
// Returns sql.ErrNoRows if the profile does not exist.
func (db *DB) UpdateProfile(name string, p *Profile) error {
	allowedJSON, err := json.Marshal(p.AllowedIPs)
	if err != nil {
		return fmt.Errorf("marshaling allowed_ips: %w", err)
	}
	excludeJSON, err := json.Marshal(p.ExcludeIPs)
	if err != nil {
		return fmt.Errorf("marshaling exclude_ips: %w", err)
	}

	result, err := db.conn.Exec(`
		UPDATE profiles SET allowed_ips = ?, exclude_ips = ?, description = ?
		WHERE name = ?
	`, string(allowedJSON), string(excludeJSON), p.Description, name)
	if err != nil {
		return fmt.Errorf("updating profile: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteProfile removes a profile by name.
// Returns an error if any peers reference this profile (enforced by SQLite trigger).
// Returns sql.ErrNoRows if the profile does not exist.
func (db *DB) DeleteProfile(name string) error {
	result, err := db.conn.Exec("DELETE FROM profiles WHERE name = ?", name)
	if err != nil {
		// The BEFORE DELETE trigger raises "FOREIGN KEY constraint failed: profiles.name is referenced by peers"
		// when peers still reference this profile. Provide a user-friendly message.
		if strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
			return fmt.Errorf("cannot delete profile %q: peers still reference it", name)
		}
		return fmt.Errorf("deleting profile: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SeedProfiles inserts seed profiles only if the profiles table is empty.
// This ensures config.yaml seeds are applied on first start, but SQLite
// is the source of truth on subsequent starts.
func (db *DB) SeedProfiles(seeds []ProfileSeed) error {
	if len(seeds) == 0 {
		return nil
	}

	// Use BEGIN IMMEDIATE to acquire a write lock immediately,
	// preventing a race where two concurrent seeds both see count=0.
	_, err := db.conn.Exec("BEGIN IMMEDIATE")
	if err != nil {
		return fmt.Errorf("beginning seed transaction: %w", err)
	}
	// Manual transaction management since we used raw BEGIN IMMEDIATE.
	committed := false
	defer func() {
		if !committed {
			db.conn.Exec("ROLLBACK")
		}
	}()

	// Check if profiles table already has data (inside transaction for atomicity).
	var count int
	err = db.conn.QueryRow("SELECT COUNT(*) FROM profiles").Scan(&count)
	if err != nil {
		return fmt.Errorf("checking profiles count: %w", err)
	}
	if count > 0 {
		return nil // table not empty, skip seeding
	}

	for _, s := range seeds {
		allowedJSON, err := json.Marshal(s.AllowedIPs)
		if err != nil {
			return fmt.Errorf("marshaling allowed_ips for %q: %w", s.Name, err)
		}
		excludeIPs := s.ExcludeIPs
		if excludeIPs == nil {
			excludeIPs = []string{}
		}
		excludeJSON, err := json.Marshal(excludeIPs)
		if err != nil {
			return fmt.Errorf("marshaling exclude_ips for %q: %w", s.Name, err)
		}

		_, err = db.conn.Exec(`
			INSERT INTO profiles (name, allowed_ips, exclude_ips, description, is_default)
			VALUES (?, ?, ?, ?, 1)
		`, s.Name, string(allowedJSON), string(excludeJSON), s.Description)
		if err != nil {
			return fmt.Errorf("seeding profile %q: %w", s.Name, err)
		}
	}

	_, err = db.conn.Exec("COMMIT")
	if err != nil {
		return fmt.Errorf("committing seed transaction: %w", err)
	}
	committed = true
	return nil
}

// scanProfile scans a profile row from sql.Rows.
func scanProfile(rows *sql.Rows) (Profile, error) {
	var p Profile
	var allowedJSON, excludeJSON string
	if err := rows.Scan(&p.Name, &allowedJSON, &excludeJSON, &p.Description, &p.IsDefault, &p.CreatedAt); err != nil {
		return Profile{}, fmt.Errorf("scanning profile: %w", err)
	}
	if err := json.Unmarshal([]byte(allowedJSON), &p.AllowedIPs); err != nil {
		return Profile{}, fmt.Errorf("parsing allowed_ips JSON: %w", err)
	}
	if err := json.Unmarshal([]byte(excludeJSON), &p.ExcludeIPs); err != nil {
		return Profile{}, fmt.Errorf("parsing exclude_ips JSON: %w", err)
	}
	return p, nil
}
