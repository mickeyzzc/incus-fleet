package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// User is one local account.
type User struct {
	ID                 string
	Username           string
	PasswordHash       string
	Role               string
	Disabled           bool
	MustChangePassword bool
	FailedAttempts     int
	LockedUntil        time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// CreateUser inserts a local account.
func (s *SQLStore) CreateUser(ctx context.Context, user *User) error {
	if user == nil || user.ID == "" || user.Username == "" {
		return errors.New("store: user id and username are required")
	}

	now := time.Now().UTC()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	if user.UpdatedAt.IsZero() {
		user.UpdatedAt = now
	}

	const query = `INSERT INTO users
		(id, username, password_hash, role, disabled, must_change_password,
		 failed_attempts, locked_until, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, s.rebind(query), //nolint:gosec // query is a constant
		user.ID, strings.ToLower(user.Username), user.PasswordHash, user.Role,
		boolToInt(user.Disabled), boolToInt(user.MustChangePassword),
		user.FailedAttempts, unixNanos(user.LockedUntil),
		user.CreatedAt.UnixNano(), user.UpdatedAt.UnixNano(),
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return ErrConflict
		}
		return fmt.Errorf("store: create user: %w", err)
	}

	return nil
}

// GetUser loads one account by id.
func (s *SQLStore) GetUser(ctx context.Context, id string) (*User, error) {
	const query = `SELECT id, username, password_hash, role, disabled, must_change_password,
		failed_attempts, locked_until, created_at, updated_at FROM users WHERE id = ?`

	return s.scanUser(s.db.QueryRowContext(ctx, s.rebind(query), id)) //nolint:gosec // query is a constant
}

// GetUserByUsername loads one account by username. Usernames are stored
// lowercase and compared that way.
func (s *SQLStore) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	const query = `SELECT id, username, password_hash, role, disabled, must_change_password,
		failed_attempts, locked_until, created_at, updated_at FROM users WHERE username = ?`

	return s.scanUser(s.db.QueryRowContext(ctx, s.rebind(query), strings.ToLower(username))) //nolint:gosec // query is a constant
}

// ListUsers returns every account ordered by username.
func (s *SQLStore) ListUsers(ctx context.Context) ([]*User, error) {
	const query = `SELECT id, username, password_hash, role, disabled, must_change_password,
		failed_attempts, locked_until, created_at, updated_at FROM users ORDER BY username`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("store: list users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []*User{}
	for rows.Next() {
		user, err := scanUserRow(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan user: %w", err)
		}
		out = append(out, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list users rows: %w", err)
	}

	return out, nil
}

// UpdateUser writes the mutable account fields.
func (s *SQLStore) UpdateUser(ctx context.Context, user *User) error {
	if user == nil || user.ID == "" {
		return errors.New("store: user id is required")
	}
	user.UpdatedAt = time.Now().UTC()

	const query = `UPDATE users SET username = ?, password_hash = ?, role = ?, disabled = ?,
		must_change_password = ?, failed_attempts = ?, locked_until = ?, updated_at = ?
		WHERE id = ?`
	res, err := s.db.ExecContext(ctx, s.rebind(query), //nolint:gosec // query is a constant
		strings.ToLower(user.Username), user.PasswordHash, user.Role,
		boolToInt(user.Disabled), boolToInt(user.MustChangePassword),
		user.FailedAttempts, unixNanos(user.LockedUntil), user.UpdatedAt.UnixNano(), user.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update user: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update user rows: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}

	return nil
}

// DeleteUser removes one account.
func (s *SQLStore) DeleteUser(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM users WHERE id = ?`), id) //nolint:gosec // query is a constant
	if err != nil {
		return fmt.Errorf("store: delete user: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete user rows: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}

	return nil
}

// CountAdmins reports how many enabled administrator accounts exist.
func (s *SQLStore) CountAdmins(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE role = 'admin' AND disabled = 0`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count admins: %w", err)
	}

	return count, nil
}

func (s *SQLStore) scanUser(row *sql.Row) (*User, error) {
	user, err := scanUserRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	return user, nil
}

func scanUserRow(row rowScanner) (*User, error) {
	var (
		user      User
		disabled  int
		mustReset int
		lockedNS  int64
		createdNS int64
		updatedNS int64
	)
	err := row.Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.Role,
		&disabled, &mustReset, &user.FailedAttempts, &lockedNS,
		&createdNS, &updatedNS,
	)
	if err != nil {
		return nil, err
	}

	user.Disabled = disabled != 0
	user.MustChangePassword = mustReset != 0
	if lockedNS != 0 {
		user.LockedUntil = time.Unix(0, lockedNS).UTC()
	}
	user.CreatedAt = time.Unix(0, createdNS).UTC()
	user.UpdatedAt = time.Unix(0, updatedNS).UTC()

	return &user, nil
}
