package store

import (
	"context"
	"fmt"
	"time"
)

// PluginConfig is one operator-editable template value of an installed plugin.
type PluginConfig struct {
	PluginID  string    `json:"plugin_id"`
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UpsertPluginConfig stores one operator-edited config value.
func (s *SQLStore) UpsertPluginConfig(ctx context.Context, pluginID, key, value string) error {
	if pluginID == "" || key == "" {
		return fmt.Errorf("store: plugin config requires plugin id and key")
	}
	const query = `INSERT INTO plugin_config (plugin_id, key, value, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(plugin_id, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`
	_, err := s.db.ExecContext(ctx, s.rebind(query), pluginID, key, value, time.Now().UTC().UnixNano())
	if err != nil {
		return fmt.Errorf("store: upsert plugin config: %w", err)
	}

	return nil
}

// ListPluginConfig returns all stored config values of one plugin as a map.
func (s *SQLStore) ListPluginConfig(ctx context.Context, pluginID string) (map[string]string, error) {
	const query = `SELECT key, value FROM plugin_config WHERE plugin_id = ? ORDER BY key`
	rows, err := s.db.QueryContext(ctx, s.rebind(query), pluginID)
	if err != nil {
		return nil, fmt.Errorf("store: list plugin config: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("store: scan plugin config: %w", err)
		}
		out[key] = value
	}

	return out, rows.Err()
}

// DeletePluginConfig removes every config value of a plugin on uninstall.
func (s *SQLStore) DeletePluginConfig(ctx context.Context, pluginID string) error {
	const query = `DELETE FROM plugin_config WHERE plugin_id = ?`
	if _, err := s.db.ExecContext(ctx, s.rebind(query), pluginID); err != nil {
		return fmt.Errorf("store: delete plugin config: %w", err)
	}

	return nil
}

// PluginHealthSample is one recorded result of a plugin health probe.
type PluginHealthSample struct {
	ID       string    `json:"id"`
	PluginID string    `json:"plugin_id"`
	At       time.Time `json:"at"`
	Status   string    `json:"status"`
	Detail   string    `json:"detail,omitempty"`
}

// RecordPluginHealth appends one health sample.
func (s *SQLStore) RecordPluginHealth(ctx context.Context, sample *PluginHealthSample) error {
	if sample == nil || sample.PluginID == "" {
		return fmt.Errorf("store: plugin health requires a plugin id")
	}
	if sample.ID == "" {
		sample.ID = NewID()
	}
	if sample.At.IsZero() {
		sample.At = time.Now().UTC()
	}
	const query = `INSERT INTO plugin_health (id, plugin_id, at, status, detail) VALUES (?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, s.rebind(query),
		sample.ID, sample.PluginID, sample.At.UnixNano(), sample.Status, sample.Detail); err != nil {
		return fmt.Errorf("store: record plugin health: %w", err)
	}

	return nil
}

// ListPluginHealth returns the newest health samples of one plugin.
func (s *SQLStore) ListPluginHealth(ctx context.Context, pluginID string, limit int) ([]*PluginHealthSample, error) {
	if limit <= 0 {
		limit = 20
	}
	const query = `SELECT id, plugin_id, at, status, detail FROM plugin_health
		WHERE plugin_id = ? ORDER BY at DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, s.rebind(query), pluginID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list plugin health: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]*PluginHealthSample, 0)
	for rows.Next() {
		var sample PluginHealthSample
		var at int64
		if err := rows.Scan(&sample.ID, &sample.PluginID, &at, &sample.Status, &sample.Detail); err != nil {
			return nil, fmt.Errorf("store: scan plugin health: %w", err)
		}
		sample.At = time.Unix(0, at).UTC()
		out = append(out, &sample)
	}

	return out, rows.Err()
}

// PluginEvent is one entry in a plugin's delivery/activity feed.
type PluginEvent struct {
	ID       string    `json:"id"`
	PluginID string    `json:"plugin_id"`
	At       time.Time `json:"at"`
	Kind     string    `json:"kind"`
	Message  string    `json:"message"`
}

// RecordPluginEvent appends one plugin event.
func (s *SQLStore) RecordPluginEvent(ctx context.Context, event *PluginEvent) error {
	if event == nil || event.PluginID == "" {
		return fmt.Errorf("store: plugin event requires a plugin id")
	}
	if event.ID == "" {
		event.ID = NewID()
	}
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	const query = `INSERT INTO plugin_events (id, plugin_id, at, kind, message) VALUES (?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, s.rebind(query),
		event.ID, event.PluginID, event.At.UnixNano(), event.Kind, event.Message); err != nil {
		return fmt.Errorf("store: record plugin event: %w", err)
	}

	return nil
}

// ListPluginEvents returns the newest events of one plugin.
func (s *SQLStore) ListPluginEvents(ctx context.Context, pluginID string, limit int) ([]*PluginEvent, error) {
	if limit <= 0 {
		limit = 20
	}
	const query = `SELECT id, plugin_id, at, kind, message FROM plugin_events
		WHERE plugin_id = ? ORDER BY at DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, s.rebind(query), pluginID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list plugin events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]*PluginEvent, 0)
	for rows.Next() {
		var event PluginEvent
		var at int64
		if err := rows.Scan(&event.ID, &event.PluginID, &at, &event.Kind, &event.Message); err != nil {
			return nil, fmt.Errorf("store: scan plugin event: %w", err)
		}
		event.At = time.Unix(0, at).UTC()
		out = append(out, &event)
	}

	return out, rows.Err()
}
