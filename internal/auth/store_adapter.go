package auth

import (
	"context"
	"errors"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/store"
)

// StoreAdapter adapts the SQL store to the auth interfaces. The two packages
// model users and sessions separately to keep their dependencies acyclic; this
// bridge is the only place that knows both shapes.
type StoreAdapter struct {
	store *store.SQLStore
}

// NewStoreAdapter wraps a SQL store for use by the authenticator.
func NewStoreAdapter(s *store.SQLStore) *StoreAdapter {
	return &StoreAdapter{store: s}
}

// CreateUser implements UserStore.
func (a *StoreAdapter) CreateUser(ctx context.Context, user *User) error {
	return mapStoreError(a.store.CreateUser(ctx, &store.User{
		ID:                 user.ID,
		Username:           user.Username,
		PasswordHash:       user.PasswordHash,
		Role:               string(user.Role),
		Disabled:           user.Disabled,
		MustChangePassword: user.MustChangePassword,
		FailedAttempts:     user.FailedAttempts,
		LockedUntil:        user.LockedUntil,
		CreatedAt:          user.CreatedAt,
		UpdatedAt:          user.UpdatedAt,
	}))
}

// GetUser implements UserStore.
func (a *StoreAdapter) GetUser(ctx context.Context, id string) (*User, error) {
	user, err := a.store.GetUser(ctx, id)
	if err != nil {
		return nil, mapStoreError(err)
	}

	return fromStoreUser(user), nil
}

// GetUserByUsername implements UserStore.
func (a *StoreAdapter) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	user, err := a.store.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, mapStoreError(err)
	}

	return fromStoreUser(user), nil
}

// ListUsers implements UserStore.
func (a *StoreAdapter) ListUsers(ctx context.Context) ([]*User, error) {
	users, err := a.store.ListUsers(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]*User, 0, len(users))
	for _, user := range users {
		out = append(out, fromStoreUser(user))
	}

	return out, nil
}

// UpdateUser implements UserStore.
func (a *StoreAdapter) UpdateUser(ctx context.Context, user *User) error {
	return mapStoreError(a.store.UpdateUser(ctx, &store.User{
		ID:                 user.ID,
		Username:           user.Username,
		PasswordHash:       user.PasswordHash,
		Role:               string(user.Role),
		Disabled:           user.Disabled,
		MustChangePassword: user.MustChangePassword,
		FailedAttempts:     user.FailedAttempts,
		LockedUntil:        user.LockedUntil,
		CreatedAt:          user.CreatedAt,
		UpdatedAt:          user.UpdatedAt,
	}))
}

// DeleteUser implements UserStore.
func (a *StoreAdapter) DeleteUser(ctx context.Context, id string) error {
	return mapStoreError(a.store.DeleteUser(ctx, id))
}

// CountAdmins implements UserStore.
func (a *StoreAdapter) CountAdmins(ctx context.Context) (int, error) {
	return a.store.CountAdmins(ctx)
}

// CreateSession implements SessionStore.
func (a *StoreAdapter) CreateSession(ctx context.Context, session *Session) error {
	return a.store.CreateSession(ctx, &store.Session{
		ID:        session.ID,
		UserID:    session.UserID,
		Username:  session.Username,
		Role:      string(session.Role),
		ExpiresAt: session.ExpiresAt,
	})
}

// GetSession implements SessionStore.
func (a *StoreAdapter) GetSession(ctx context.Context, id string) (*Session, error) {
	session, err := a.store.GetSession(ctx, id)
	if err != nil {
		return nil, mapStoreError(err)
	}

	return &Session{
		ID:        session.ID,
		UserID:    session.UserID,
		Username:  session.Username,
		Role:      Role(session.Role),
		ExpiresAt: session.ExpiresAt,
	}, nil
}

// DeleteSession implements SessionStore.
func (a *StoreAdapter) DeleteSession(ctx context.Context, id string) error {
	return a.store.DeleteSession(ctx, id)
}

// CleanupSessions implements SessionStore.
func (a *StoreAdapter) CleanupSessions(ctx context.Context, now time.Time) error {
	return a.store.CleanupSessions(ctx, now)
}

func fromStoreUser(user *store.User) *User {
	return &User{
		ID:                 user.ID,
		Username:           user.Username,
		PasswordHash:       user.PasswordHash,
		Role:               Role(user.Role),
		Disabled:           user.Disabled,
		MustChangePassword: user.MustChangePassword,
		FailedAttempts:     user.FailedAttempts,
		LockedUntil:        user.LockedUntil,
		CreatedAt:          user.CreatedAt,
		UpdatedAt:          user.UpdatedAt,
	}
}

// mapStoreError translates store sentinels into auth sentinels.
func mapStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, store.ErrNotFound) {
		return ErrUserNotFound
	}
	if errors.Is(err, store.ErrConflict) {
		return ErrUserExists
	}

	return err
}
