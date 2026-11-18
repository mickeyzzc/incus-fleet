package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// CreateSession persists a browser session so multiple centers can share it.
func (s *SQLStore) CreateSession(ctx context.Context, id string, expires time.Time) error {
	if id == "" {
		return errors.New("store: session id is required")
	}
	_, err := s.db.ExecContext(ctx, s.rebind(
		`INSERT INTO sessions (id, expires_at) VALUES (?, ?)
		 ON CONFLICT(id) DO UPDATE SET expires_at = excluded.expires_at`,
	), id, expires.UnixNano())
	return err
}

// GetSession returns a session expiry, or ErrNotFound when the id is unknown.
func (s *SQLStore) GetSession(ctx context.Context, id string) (time.Time, error) {
	var nanos int64
	err := s.db.QueryRowContext(ctx, s.rebind(`SELECT expires_at FROM sessions WHERE id = ?`), id).Scan(&nanos)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, ErrNotFound
	}
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, nanos).UTC(), nil
}

// DeleteSession removes one session.
func (s *SQLStore) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM sessions WHERE id = ?`), id)
	return err
}

// CleanupSessions removes expired sessions.
func (s *SQLStore) CleanupSessions(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM sessions WHERE expires_at <= ?`), now.UnixNano())
	return err
}
