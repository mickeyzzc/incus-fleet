package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// InstalledPlugin is a plugin connected through the web UI's one-click
// install. It complements (rather than replaces) the static YAML entries.
type InstalledPlugin struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Version   string    `json:"version,omitempty"`
	Upstream  string    `json:"upstream"`
	UIPath    string    `json:"ui_path"`
	Role      string    `json:"role"`
	Cluster   string    `json:"cluster,omitempty"`
	Instance  string    `json:"instance,omitempty"`
	Enabled   bool      `json:"enabled"`
	TokenID   string    `json:"token_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreateInstalledPlugin persists a newly installed plugin.
func (s *SQLStore) CreateInstalledPlugin(ctx context.Context, p *InstalledPlugin) error {
	if p.ID == "" {
		return fmt.Errorf("store: installed plugin id required")
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = now
	}

	const query = `INSERT INTO installed_plugins
		(id, name, version, upstream, ui_path, role, cluster, instance, enabled, token_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, s.rebind(query),
		p.ID, p.Name, p.Version, p.Upstream, p.UIPath, p.Role, p.Cluster, p.Instance,
		boolToInt(p.Enabled), p.TokenID, p.CreatedAt.UnixNano(), p.UpdatedAt.UnixNano()); err != nil {
		return fmt.Errorf("store: create installed plugin: %w", err)
	}

	return nil
}

// ListInstalledPlugins returns every plugin installed through the web UI.
func (s *SQLStore) ListInstalledPlugins(ctx context.Context) ([]*InstalledPlugin, error) {
	const query = `SELECT id, name, version, upstream, ui_path, role, cluster, instance, enabled, token_id, created_at, updated_at
		FROM installed_plugins ORDER BY id`
	rows, err := s.db.QueryContext(ctx, s.rebind(query))
	if err != nil {
		return nil, fmt.Errorf("store: list installed plugins: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only

	var out []*InstalledPlugin
	for rows.Next() {
		var p InstalledPlugin
		var enabled int
		var created, updated int64
		if err := rows.Scan(&p.ID, &p.Name, &p.Version, &p.Upstream, &p.UIPath, &p.Role,
			&p.Cluster, &p.Instance, &enabled, &p.TokenID, &created, &updated); err != nil {
			return nil, fmt.Errorf("store: scan installed plugin: %w", err)
		}
		p.Enabled = enabled != 0
		p.CreatedAt = time.Unix(0, created).UTC()
		p.UpdatedAt = time.Unix(0, updated).UTC()
		out = append(out, &p)
	}

	return out, rows.Err()
}

// GetInstalledPlugin returns one installed plugin by id.
func (s *SQLStore) GetInstalledPlugin(ctx context.Context, id string) (*InstalledPlugin, error) {
	const query = `SELECT id, name, version, upstream, ui_path, role, cluster, instance, enabled, token_id, created_at, updated_at
		FROM installed_plugins WHERE id = ?`
	var p InstalledPlugin
	var enabled int
	var created, updated int64
	err := s.db.QueryRowContext(ctx, s.rebind(query), id).Scan(&p.ID, &p.Name, &p.Version,
		&p.Upstream, &p.UIPath, &p.Role, &p.Cluster, &p.Instance, &enabled, &p.TokenID, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get installed plugin: %w", err)
	}
	p.Enabled = enabled != 0
	p.CreatedAt = time.Unix(0, created).UTC()
	p.UpdatedAt = time.Unix(0, updated).UTC()

	return &p, nil
}

// UpdateInstalledPlugin rewrites the mutable fields of an installed plugin.
func (s *SQLStore) UpdateInstalledPlugin(ctx context.Context, p *InstalledPlugin) error {
	p.UpdatedAt = time.Now().UTC()
	const query = `UPDATE installed_plugins SET name = ?, upstream = ?, enabled = ?,
		token_id = ?, cluster = ?, instance = ?, updated_at = ? WHERE id = ?`
	result, err := s.db.ExecContext(ctx, s.rebind(query),
		p.Name, p.Upstream, boolToInt(p.Enabled), p.TokenID, p.Cluster, p.Instance,
		p.UpdatedAt.UnixNano(), p.ID)
	if err != nil {
		return fmt.Errorf("store: update installed plugin: %w", err)
	}
	if n, err := result.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}

	return nil
}

// DeleteInstalledPlugin removes a plugin installed through the web UI.
func (s *SQLStore) DeleteInstalledPlugin(ctx context.Context, id string) error {
	const query = `DELETE FROM installed_plugins WHERE id = ?`
	result, err := s.db.ExecContext(ctx, s.rebind(query), id)
	if err != nil {
		return fmt.Errorf("store: delete installed plugin: %w", err)
	}
	if n, err := result.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}

	return nil
}
