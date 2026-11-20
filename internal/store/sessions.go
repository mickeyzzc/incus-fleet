package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Session is a persisted browser session. User identity travels with the row so
// any center can authorize a request without keeping local state.
type Session struct {
	ID        string
	UserID    string
	Username  string
	Role      string
	ExpiresAt time.Time
}

// CreateSession persists a browser session so multiple centers can share it.
func (s *SQLStore) CreateSession(ctx context.Context, session *Session) error {
	if session == nil || session.ID == "" {
		return errors.New("store: session id is required")
	}

	_, err := s.db.ExecContext(ctx, s.rebind(
		`INSERT INTO sessions (id, user_id, username, role, expires_at) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET user_id = excluded.user_id, username = excluded.username,
			role = excluded.role, expires_at = excluded.expires_at`,
	), session.ID, session.UserID, session.Username, session.Role, session.ExpiresAt.UnixNano())

	return err
}

// GetSession returns a session, or ErrNotFound when the id is unknown.
func (s *SQLStore) GetSession(ctx context.Context, id string) (*Session, error) {
	var (
		session   Session
		expiresNS int64
	)
	err := s.db.QueryRowContext(ctx, s.rebind(
		`SELECT id, user_id, username, role, expires_at FROM sessions WHERE id = ?`), //nolint:gosec // query is a constant
		id,
	).Scan(&session.ID, &session.UserID, &session.Username, &session.Role, &expiresNS)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	session.ExpiresAt = time.Unix(0, expiresNS).UTC()

	return &session, nil
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
