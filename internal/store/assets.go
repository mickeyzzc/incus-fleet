package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Keystone is a reusable SSH public key kept in the fleet database.
type Keystone struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	PublicKey string    `json:"public_key"`
	Comment   string    `json:"comment,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Script is a reusable shell script kept in the fleet database.
type Script struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Body        string    `json:"body"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateKeystone stores a new SSH public key.
func (s *SQLStore) CreateKeystone(ctx context.Context, key *Keystone) error {
	if key.ID == "" {
		return fmt.Errorf("store: keystone id required")
	}
	if key.CreatedAt.IsZero() {
		key.CreatedAt = time.Now().UTC().Truncate(time.Nanosecond)
	}

	const query = `INSERT INTO keystones (id, name, public_key, comment, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, public_key = excluded.public_key, comment = excluded.comment`
	_, err := s.db.ExecContext(ctx, s.rebind(query),
		key.ID, key.Name, key.PublicKey, key.Comment, key.CreatedAt.UnixNano())

	return err
}

// ListKeystones returns every stored key ordered by creation time.
func (s *SQLStore) ListKeystones(ctx context.Context) ([]*Keystone, error) {
	const query = `SELECT id, name, public_key, comment, created_at FROM keystones ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // read-only scan

	var keys []*Keystone
	for rows.Next() {
		var key Keystone
		var created int64
		if err := rows.Scan(&key.ID, &key.Name, &key.PublicKey, &key.Comment, &created); err != nil {
			return nil, err
		}
		key.CreatedAt = time.Unix(0, created).UTC()
		keys = append(keys, &key)
	}

	return keys, rows.Err()
}

// GetKeystone loads one key by id.
func (s *SQLStore) GetKeystone(ctx context.Context, id string) (*Keystone, error) {
	const query = `SELECT id, name, public_key, comment, created_at FROM keystones WHERE id = ?`

	var key Keystone
	var created int64
	err := s.db.QueryRowContext(ctx, s.rebind(query), id). //nolint:gosec // query is a constant
								Scan(&key.ID, &key.Name, &key.PublicKey, &key.Comment, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	key.CreatedAt = time.Unix(0, created).UTC()

	return &key, nil
}

// UpdateKeystone rewrites name, key material, and comment.
func (s *SQLStore) UpdateKeystone(ctx context.Context, key *Keystone) error {
	if key.ID == "" {
		return fmt.Errorf("store: keystone id required")
	}

	const query = `UPDATE keystones SET name = ?, public_key = ?, comment = ? WHERE id = ?`
	res, err := s.db.ExecContext(ctx, s.rebind(query), key.Name, key.PublicKey, key.Comment, key.ID)
	if err != nil {
		return err
	}

	return requireRow(res)
}

// DeleteKeystone removes one key.
func (s *SQLStore) DeleteKeystone(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM keystones WHERE id = ?`), id)
	if err != nil {
		return err
	}

	return requireRow(res)
}

// CreateScript stores a new reusable script.
func (s *SQLStore) CreateScript(ctx context.Context, script *Script) error {
	if script.ID == "" {
		return fmt.Errorf("store: script id required")
	}
	now := time.Now().UTC().Truncate(time.Nanosecond)
	if script.CreatedAt.IsZero() {
		script.CreatedAt = now
	}
	if script.UpdatedAt.IsZero() {
		script.UpdatedAt = now
	}

	const query = `INSERT INTO scripts (id, name, description, body, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, s.rebind(query),
		script.ID, script.Name, script.Description, script.Body, script.CreatedAt.UnixNano(), script.UpdatedAt.UnixNano())

	return err
}

// ListScripts returns every stored script ordered by update time.
func (s *SQLStore) ListScripts(ctx context.Context) ([]*Script, error) {
	const query = `SELECT id, name, description, body, created_at, updated_at FROM scripts ORDER BY updated_at DESC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // read-only scan

	var scripts []*Script
	for rows.Next() {
		var script Script
		var created, updated int64
		if err := rows.Scan(&script.ID, &script.Name, &script.Description, &script.Body, &created, &updated); err != nil {
			return nil, err
		}
		script.CreatedAt = time.Unix(0, created).UTC()
		script.UpdatedAt = time.Unix(0, updated).UTC()
		scripts = append(scripts, &script)
	}

	return scripts, rows.Err()
}

// GetScript loads one script by id.
func (s *SQLStore) GetScript(ctx context.Context, id string) (*Script, error) {
	const query = `SELECT id, name, description, body, created_at, updated_at FROM scripts WHERE id = ?`

	var script Script
	var created, updated int64
	err := s.db.QueryRowContext(ctx, s.rebind(query), id). //nolint:gosec // query is a constant
								Scan(&script.ID, &script.Name, &script.Description, &script.Body, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	script.CreatedAt = time.Unix(0, created).UTC()
	script.UpdatedAt = time.Unix(0, updated).UTC()

	return &script, nil
}

// UpdateScript rewrites name, description, and body.
func (s *SQLStore) UpdateScript(ctx context.Context, script *Script) error {
	if script.ID == "" {
		return fmt.Errorf("store: script id required")
	}

	const query = `UPDATE scripts SET name = ?, description = ?, body = ?, updated_at = ? WHERE id = ?`
	res, err := s.db.ExecContext(ctx, s.rebind(query),
		script.Name, script.Description, script.Body, time.Now().UTC().UnixNano(), script.ID)
	if err != nil {
		return err
	}

	return requireRow(res)
}

// DeleteScript removes one script.
func (s *SQLStore) DeleteScript(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM scripts WHERE id = ?`), id)
	if err != nil {
		return err
	}

	return requireRow(res)
}

func requireRow(res sql.Result) error {
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}

	return nil
}

// assetSchema is appended to the shared schema bootstrap.
func assetSchema() string {
	return strings.TrimSpace(`
CREATE TABLE IF NOT EXISTS keystones (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	public_key TEXT NOT NULL,
	comment TEXT NOT NULL DEFAULT '',
	created_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_keystones_created_at ON keystones(created_at DESC);
CREATE TABLE IF NOT EXISTS scripts (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	body TEXT NOT NULL,
	created_at BIGINT NOT NULL,
	updated_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_scripts_updated_at ON scripts(updated_at DESC);
`)
}
