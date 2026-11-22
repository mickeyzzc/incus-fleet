package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// AnswerProfile is a reusable unattended-install answer configuration. It
// either carries the built-in template fields (spec) or a complete custom
// answer file, and can be marked as the default for its installer profile.
type AnswerProfile struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Profile    string    `json:"profile"`
	Hostname   string    `json:"hostname,omitempty"`
	Username   string    `json:"username,omitempty"`
	Password   string    `json:"password,omitempty"`
	Timezone   string    `json:"timezone,omitempty"`
	Disk       string    `json:"disk,omitempty"`
	Locale     string    `json:"locale,omitempty"`
	Keyboard   string    `json:"keyboard,omitempty"`
	AnswerFile string    `json:"answer_file,omitempty"`
	IsDefault  bool      `json:"is_default"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// CreateAnswerProfile stores a new answer configuration.
func (s *SQLStore) CreateAnswerProfile(ctx context.Context, p *AnswerProfile) error {
	if p.ID == "" {
		return fmt.Errorf("store: answer profile id required")
	}
	now := time.Now().UTC().Truncate(time.Nanosecond)
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = now
	}

	// Clear any previous default in this family before inserting a new one:
	// the partial unique index rejects two defaults even momentarily.
	if p.IsDefault {
		const clearStmt = `UPDATE answer_profiles SET is_default = 0 WHERE profile = ?`
		if _, err := s.db.ExecContext(ctx, s.rebind(clearStmt), p.Profile); err != nil {
			return err
		}
	}

	const query = `INSERT INTO answer_profiles
		(id, name, profile, hostname, username, password, timezone, disk, locale, keyboard, answer_file, is_default, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, s.rebind(query),
		p.ID, p.Name, p.Profile, p.Hostname, p.Username, p.Password, p.Timezone,
		p.Disk, p.Locale, p.Keyboard, p.AnswerFile, boolToInt(p.IsDefault),
		p.CreatedAt.UnixNano(), p.UpdatedAt.UnixNano())
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return fmt.Errorf("%w: answer profile name or default conflict", ErrConflict)
		}
		return fmt.Errorf("store: create answer profile: %w", err)
	}

	return nil
}

// ListAnswerProfiles returns every answer configuration, newest first.
func (s *SQLStore) ListAnswerProfiles(ctx context.Context) ([]*AnswerProfile, error) {
	const query = `SELECT id, name, profile, hostname, username, password, timezone, disk,
		locale, keyboard, answer_file, is_default, created_at, updated_at
		FROM answer_profiles ORDER BY updated_at DESC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // read-only scan

	var profiles []*AnswerProfile
	for rows.Next() {
		var p AnswerProfile
		var isDefault int
		var created, updated int64
		if err := rows.Scan(&p.ID, &p.Name, &p.Profile, &p.Hostname, &p.Username,
			&p.Password, &p.Timezone, &p.Disk, &p.Locale, &p.Keyboard, &p.AnswerFile,
			&isDefault, &created, &updated); err != nil {
			return nil, err
		}
		p.IsDefault = isDefault != 0
		p.CreatedAt = time.Unix(0, created).UTC()
		p.UpdatedAt = time.Unix(0, updated).UTC()
		profiles = append(profiles, &p)
	}

	return profiles, rows.Err()
}

// GetAnswerProfile loads one answer configuration by id.
func (s *SQLStore) GetAnswerProfile(ctx context.Context, id string) (*AnswerProfile, error) {
	const query = `SELECT id, name, profile, hostname, username, password, timezone, disk,
		locale, keyboard, answer_file, is_default, created_at, updated_at
		FROM answer_profiles WHERE id = ?`

	var p AnswerProfile
	var isDefault int
	var created, updated int64
	err := s.db.QueryRowContext(ctx, s.rebind(query), id). //nolint:gosec // constant query
								Scan(&p.ID, &p.Name, &p.Profile, &p.Hostname, &p.Username, &p.Password,
			&p.Timezone, &p.Disk, &p.Locale, &p.Keyboard, &p.AnswerFile, &isDefault,
			&created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.IsDefault = isDefault != 0
	p.CreatedAt = time.Unix(0, created).UTC()
	p.UpdatedAt = time.Unix(0, updated).UTC()

	return &p, nil
}

// GetDefaultAnswerProfile returns the default configuration for one installer
// profile, or ErrNotFound when none is marked default.
func (s *SQLStore) GetDefaultAnswerProfile(ctx context.Context, profile string) (*AnswerProfile, error) {
	const query = `SELECT id, name, profile, hostname, username, password, timezone, disk,
		locale, keyboard, answer_file, is_default, created_at, updated_at
		FROM answer_profiles WHERE profile = ? AND is_default = 1`

	var p AnswerProfile
	var isDefault int
	var created, updated int64
	err := s.db.QueryRowContext(ctx, s.rebind(query), profile). //nolint:gosec // constant query
									Scan(&p.ID, &p.Name, &p.Profile, &p.Hostname, &p.Username, &p.Password,
			&p.Timezone, &p.Disk, &p.Locale, &p.Keyboard, &p.AnswerFile, &isDefault,
			&created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.IsDefault = isDefault != 0
	p.CreatedAt = time.Unix(0, created).UTC()
	p.UpdatedAt = time.Unix(0, updated).UTC()

	return &p, nil
}

// UpdateAnswerProfile rewrites one answer configuration.
func (s *SQLStore) UpdateAnswerProfile(ctx context.Context, p *AnswerProfile) error {
	if p.ID == "" {
		return fmt.Errorf("store: answer profile id required")
	}
	// Clear any previous default in this family before setting the new one:
	// the partial unique index rejects two defaults even momentarily.
	if p.IsDefault {
		const clearStmt = `UPDATE answer_profiles SET is_default = 0 WHERE profile = ? AND id <> ?`
		if _, err := s.db.ExecContext(ctx, s.rebind(clearStmt), p.Profile, p.ID); err != nil {
			return err
		}
	}

	updated := time.Now().UTC().Truncate(time.Nanosecond)
	if !updated.After(p.CreatedAt) {
		updated = p.CreatedAt.Add(time.Nanosecond)
	}
	const query = `UPDATE answer_profiles SET name = ?, profile = ?, hostname = ?, username = ?,
		password = ?, timezone = ?, disk = ?, locale = ?, keyboard = ?, answer_file = ?,
		is_default = ?, updated_at = ? WHERE id = ?`
	res, err := s.db.ExecContext(ctx, s.rebind(query),
		p.Name, p.Profile, p.Hostname, p.Username, p.Password, p.Timezone, p.Disk,
		p.Locale, p.Keyboard, p.AnswerFile, boolToInt(p.IsDefault),
		updated.UnixNano(), p.ID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return fmt.Errorf("%w: answer profile name or default conflict", ErrConflict)
		}
		return fmt.Errorf("store: update answer profile: %w", err)
	}

	return requireRow(res)
}

// DeleteAnswerProfile removes one answer configuration.
func (s *SQLStore) DeleteAnswerProfile(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM answer_profiles WHERE id = ?`), id)
	if err != nil {
		return err
	}

	return requireRow(res)
}

// answerProfileSchema holds reusable unattended answer configurations. The
// partial unique index allows many non-default rows but at most one default
// per installer profile.
func answerProfileSchema() string {
	return strings.TrimSpace(`
CREATE TABLE IF NOT EXISTS answer_profiles (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	profile TEXT NOT NULL,
	hostname TEXT NOT NULL DEFAULT '',
	username TEXT NOT NULL DEFAULT '',
	password TEXT NOT NULL DEFAULT '',
	timezone TEXT NOT NULL DEFAULT '',
	disk TEXT NOT NULL DEFAULT '',
	locale TEXT NOT NULL DEFAULT '',
	keyboard TEXT NOT NULL DEFAULT '',
	answer_file TEXT NOT NULL DEFAULT '',
	is_default INTEGER NOT NULL DEFAULT 0,
	created_at BIGINT NOT NULL,
	updated_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_answer_profiles_updated_at ON answer_profiles(updated_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_answer_profiles_name ON answer_profiles(name);
CREATE UNIQUE INDEX IF NOT EXISTS idx_answer_profiles_default ON answer_profiles(profile) WHERE is_default = 1;
`)
}
