package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PluginWiring is one operator-approved fleet behavior override owned by a
// plugin. DefaultValue is the static configuration value captured when the
// plugin was installed, so uninstall can restore it without a restart.
type PluginWiring struct {
	PluginID     string    `json:"plugin_id"`
	Key          string    `json:"key"`
	Value        string    `json:"value"`
	DefaultValue string    `json:"default_value,omitempty"`
	Enabled      bool      `json:"enabled"`
	Approved     bool      `json:"approved"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// UpsertPluginWiring inserts or replaces one wiring row. The (plugin, key) pair
// is the identity, so an install that rewrites a value keeps its approval.
func (s *SQLStore) UpsertPluginWiring(ctx context.Context, w *PluginWiring) error {
	if w == nil || w.PluginID == "" || w.Key == "" {
		return fmt.Errorf("store: plugin wiring requires plugin id and key")
	}
	updated := w.UpdatedAt.UTC()
	if updated.IsZero() {
		updated = time.Now().UTC()
	}

	const query = `INSERT INTO plugin_wiring
		(plugin_id, key, value, default_value, enabled, approved, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(plugin_id, key) DO UPDATE SET
			value = excluded.value,
			default_value = excluded.default_value,
			enabled = excluded.enabled,
			approved = excluded.approved,
			updated_at = excluded.updated_at`
	if _, err := s.db.ExecContext(ctx, s.rebind(query),
		w.PluginID, w.Key, w.Value, w.DefaultValue, boolToInt(w.Enabled), boolToInt(w.Approved),
		updated.UnixNano()); err != nil {
		return fmt.Errorf("store: upsert plugin wiring: %w", err)
	}
	w.UpdatedAt = updated

	return nil
}

// ListPluginWiring returns every wiring row for one plugin, sorted by key.
func (s *SQLStore) ListPluginWiring(ctx context.Context, pluginID string) ([]*PluginWiring, error) {
	const query = `SELECT plugin_id, key, value, default_value, enabled, approved, updated_at
		FROM plugin_wiring WHERE plugin_id = ? ORDER BY key`
	rows, err := s.db.QueryContext(ctx, s.rebind(query), pluginID)
	if err != nil {
		return nil, fmt.Errorf("store: list plugin wiring: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]*PluginWiring, 0)
	for rows.Next() {
		w, err := scanPluginWiring(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}

	return out, rows.Err()
}

// ListActivePluginWiring returns every enabled row across all plugins. The
// registry uses it to recover overrides after a process restart.
func (s *SQLStore) ListActivePluginWiring(ctx context.Context) ([]*PluginWiring, error) {
	const query = `SELECT plugin_id, key, value, default_value, enabled, approved, updated_at
		FROM plugin_wiring WHERE enabled = 1 ORDER BY plugin_id, key`
	rows, err := s.db.QueryContext(ctx, s.rebind(query))
	if err != nil {
		return nil, fmt.Errorf("store: list active plugin wiring: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]*PluginWiring, 0)
	for rows.Next() {
		w, err := scanPluginWiring(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}

	return out, rows.Err()
}

// GetPluginWiring loads one wiring row.
func (s *SQLStore) GetPluginWiring(ctx context.Context, pluginID, key string) (*PluginWiring, error) {
	const query = `SELECT plugin_id, key, value, default_value, enabled, approved, updated_at
		FROM plugin_wiring WHERE plugin_id = ? AND key = ?`
	w, err := scanPluginWiring(s.db.QueryRowContext(ctx, s.rebind(query), pluginID, key))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get plugin wiring: %w", err)
	}

	return w, nil
}

// SetPluginWiringEnabled flips the active flag of an approved key.
func (s *SQLStore) SetPluginWiringEnabled(ctx context.Context, pluginID, key string, enabled bool) error {
	const query = `UPDATE plugin_wiring SET enabled = ?, updated_at = ? WHERE plugin_id = ? AND key = ?`
	result, err := s.db.ExecContext(ctx, s.rebind(query),
		boolToInt(enabled), time.Now().UTC().UnixNano(), pluginID, key)
	if err != nil {
		return fmt.Errorf("store: enable plugin wiring: %w", err)
	}
	if n, err := result.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}

	return nil
}

// RestorePluginWiringDefaults keeps the approval record but resets each value
// to the static default and deactivates it. Uninstall uses this so a later
// reinstall does not need a fresh approval round trip.
func (s *SQLStore) RestorePluginWiringDefaults(ctx context.Context, pluginID string) error {
	const query = `UPDATE plugin_wiring SET value = default_value, enabled = 0, updated_at = ? WHERE plugin_id = ?`
	if _, err := s.db.ExecContext(ctx, s.rebind(query), time.Now().UTC().UnixNano(), pluginID); err != nil {
		return fmt.Errorf("store: restore plugin wiring defaults: %w", err)
	}

	return nil
}

// RestorePluginWiringDefault puts one approved key back to its captured static
// default but keeps the approval row for a later reinstall.
func (s *SQLStore) RestorePluginWiringDefault(ctx context.Context, pluginID, key string) error {
	const query = `UPDATE plugin_wiring SET value = default_value, enabled = 0, updated_at = ?
		WHERE plugin_id = ? AND key = ?`
	result, err := s.db.ExecContext(ctx, s.rebind(query),
		time.Now().UTC().UnixNano(), pluginID, key)
	if err != nil {
		return fmt.Errorf("store: restore plugin wiring default: %w", err)
	}
	if n, err := result.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}

	return nil
}

// DeletePluginWiring removes one wiring row.
func (s *SQLStore) DeletePluginWiring(ctx context.Context, pluginID, key string) error {
	const query = `DELETE FROM plugin_wiring WHERE plugin_id = ? AND key = ?`
	result, err := s.db.ExecContext(ctx, s.rebind(query), pluginID, key)
	if err != nil {
		return fmt.Errorf("store: delete plugin wiring: %w", err)
	}
	if n, err := result.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}

	return nil
}

type wiringScanner interface {
	Scan(dest ...any) error
}

func scanPluginWiring(row wiringScanner) (*PluginWiring, error) {
	var w PluginWiring
	var enabled, approved int
	var updated int64
	if err := row.Scan(&w.PluginID, &w.Key, &w.Value, &w.DefaultValue, &enabled, &approved, &updated); err != nil {
		return nil, err
	}
	w.Enabled = enabled != 0
	w.Approved = approved != 0
	w.UpdatedAt = time.Unix(0, updated).UTC()

	return &w, nil
}
