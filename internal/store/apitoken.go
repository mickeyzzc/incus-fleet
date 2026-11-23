package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// APIToken is a persisted API token. The plaintext secret is never stored;
// TokenHash is the SHA-256 of the issued secret.
type APIToken struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	UserID     string     `json:"user_id"`
	Username   string     `json:"username,omitempty"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	PluginID   string     `json:"plugin_id,omitempty"`
	InstanceID string     `json:"instance_id,omitempty"`
	TokenHash  string     `json:"-"`
}

const apiTokenSchema = `
CREATE TABLE IF NOT EXISTS api_tokens (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	user_id TEXT NOT NULL,
	username TEXT NOT NULL DEFAULT '',
	scopes TEXT NOT NULL,
	created_at BIGINT NOT NULL,
	expires_at BIGINT NOT NULL DEFAULT 0,
	last_used_at BIGINT NOT NULL DEFAULT 0,
	plugin_id TEXT NOT NULL DEFAULT '',
	instance_id TEXT NOT NULL DEFAULT '',
	token_hash TEXT NOT NULL
)`

const apiTokenIndexes = `
CREATE UNIQUE INDEX IF NOT EXISTS idx_api_tokens_hash ON api_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_api_tokens_plugin ON api_tokens(plugin_id)`

// CreateAPIToken inserts a token. A duplicate hash is a conflict.
func (s *SQLStore) CreateAPIToken(ctx context.Context, token *APIToken) error {
	scopes, err := json.Marshal(token.Scopes)
	if err != nil {
		return fmt.Errorf("store: encode token scopes: %w", err)
	}

	_, err = s.db.ExecContext(ctx, s.rebind(`
INSERT INTO api_tokens (id, name, user_id, username, scopes, created_at, expires_at, last_used_at, plugin_id, instance_id, token_hash)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		token.ID, token.Name, token.UserID, token.Username, string(scopes),
		token.CreatedAt.UnixMilli(), timePtrMillis(token.ExpiresAt), timePtrMillis(token.LastUsedAt),
		token.PluginID, token.InstanceID, token.TokenHash,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: api token hash already exists", ErrConflict)
		}

		return fmt.Errorf("store: create api token: %w", err)
	}

	return nil
}

// GetAPITokenByHash loads one token by its hash.
func (s *SQLStore) GetAPITokenByHash(ctx context.Context, hash string) (*APIToken, error) {
	row := s.db.QueryRowContext(ctx, s.rebind(`
SELECT id, name, user_id, username, scopes, created_at, expires_at, last_used_at, plugin_id, instance_id, token_hash
FROM api_tokens WHERE token_hash = ?`), hash)

	token, err := scanAPIToken(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: api token", ErrNotFound)
		}

		return nil, fmt.Errorf("store: get api token: %w", err)
	}

	return token, nil
}

// ListAPITokens returns every token, newest first.
func (s *SQLStore) ListAPITokens(ctx context.Context) ([]*APIToken, error) {
	return s.queryAPITokens(ctx, `
SELECT id, name, user_id, username, scopes, created_at, expires_at, last_used_at, plugin_id, instance_id, token_hash
FROM api_tokens ORDER BY created_at DESC`)
}

// ListAPITokensForUser returns one user's tokens, newest first.
func (s *SQLStore) ListAPITokensForUser(ctx context.Context, userID string) ([]*APIToken, error) {
	return s.queryAPITokens(ctx, `
SELECT id, name, user_id, username, scopes, created_at, expires_at, last_used_at, plugin_id, instance_id, token_hash
FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC`, userID)
}

func (s *SQLStore) queryAPITokens(ctx context.Context, query string, args ...any) ([]*APIToken, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("store: list api tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tokens []*APIToken
	for rows.Next() {
		token, err := scanAPIToken(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("store: scan api token: %w", err)
		}
		tokens = append(tokens, token)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate api tokens: %w", err)
	}

	return tokens, nil
}

// DeleteAPIToken removes one token.
func (s *SQLStore) DeleteAPIToken(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM api_tokens WHERE id = ?`), id)
	if err != nil {
		return fmt.Errorf("store: delete api token: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete api token rows: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: api token", ErrNotFound)
	}

	return nil
}

// TouchAPIToken records the last successful use.
func (s *SQLStore) TouchAPIToken(ctx context.Context, id string, at time.Time) error {
	result, err := s.db.ExecContext(ctx, s.rebind(`UPDATE api_tokens SET last_used_at = ? WHERE id = ?`), at.UnixMilli(), id)
	if err != nil {
		return fmt.Errorf("store: touch api token: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: touch api token rows: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: api token", ErrNotFound)
	}

	return nil
}

func scanAPIToken(scan func(dest ...any) error) (*APIToken, error) {
	var (
		token      APIToken
		scopes     string
		createdAt  int64
		expiresAt  int64
		lastUsedAt int64
	)

	if err := scan(
		&token.ID, &token.Name, &token.UserID, &token.Username, &scopes,
		&createdAt, &expiresAt, &lastUsedAt, &token.PluginID, &token.InstanceID, &token.TokenHash,
	); err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(scopes), &token.Scopes); err != nil {
		return nil, fmt.Errorf("decode scopes: %w", err)
	}
	token.CreatedAt = time.UnixMilli(createdAt).UTC()
	token.ExpiresAt = millisPtr(expiresAt)
	token.LastUsedAt = millisPtr(lastUsedAt)

	return &token, nil
}

func timePtrMillis(t *time.Time) int64 {
	if t == nil {
		return 0
	}

	return t.UnixMilli()
}

func millisPtr(ms int64) *time.Time {
	if ms == 0 {
		return nil
	}
	value := time.UnixMilli(ms).UTC()

	return &value
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())

	return strings.Contains(message, "unique constraint") ||
		strings.Contains(message, "duplicate key") ||
		strings.Contains(message, "unique violation")
}
