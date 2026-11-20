// Package auth provides user accounts, password authentication, role-based
// authorization, and sessions.
package auth

import (
	"context"
	"errors"
	"time"
)

// Errors returned by the authenticator.
var (
	// ErrInvalidToken is returned when the shared bootstrap token is wrong.
	ErrInvalidToken = errors.New("auth: invalid token")
	// ErrInvalidCredentials is returned for a wrong username or password.
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	// ErrUserDisabled is returned when a valid password belongs to a disabled account.
	ErrUserDisabled = errors.New("auth: user disabled")
	// ErrUserNotFound is returned when an account does not exist.
	ErrUserNotFound = errors.New("auth: user not found")
	// ErrUserExists is returned when creating a duplicate username.
	ErrUserExists = errors.New("auth: user already exists")
	// ErrForbidden is returned when a role lacks the required permission.
	ErrForbidden = errors.New("auth: forbidden")
	// ErrLocked is returned while an account is temporarily locked after
	// repeated failed sign-ins.
	ErrLocked = errors.New("auth: account locked")
	// ErrLastAdmin prevents removing the final administrator account.
	ErrLastAdmin = errors.New("auth: cannot remove the last administrator")
	// ErrWeakPassword is returned when a password fails the minimum policy.
	ErrWeakPassword = errors.New("auth: password does not meet the minimum policy")
)

// Role is a user's permission level.
type Role string

// Supported roles, from most to least privileged.
const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

// Valid reports whether the role is one of the supported values.
func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleOperator, RoleViewer:
		return true
	}

	return false
}

// CanWrite reports whether the role may mutate fleet state.
func (r Role) CanWrite() bool {
	return r == RoleAdmin || r == RoleOperator
}

// CanAdmin reports whether the role may manage users and fleet settings.
func (r Role) CanAdmin() bool {
	return r == RoleAdmin
}

// User is one local account.
type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Role      Role      `json:"role"`
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// PasswordHash is the encoded password hash. It is never serialized to
	// clients.
	PasswordHash string `json:"-"`
	// MustChangePassword forces a password reset at next sign-in.
	MustChangePassword bool `json:"must_change_password"`

	// FailedAttempts counts consecutive failed sign-ins for lockout.
	FailedAttempts int       `json:"-"`
	LockedUntil    time.Time `json:"-"`
}

// UserStore persists local accounts.
type UserStore interface {
	CreateUser(ctx context.Context, user *User) error
	GetUser(ctx context.Context, id string) (*User, error)
	GetUserByUsername(ctx context.Context, username string) (*User, error)
	ListUsers(ctx context.Context) ([]*User, error)
	UpdateUser(ctx context.Context, user *User) error
	DeleteUser(ctx context.Context, id string) error
	CountAdmins(ctx context.Context) (int, error)
}

// SessionStore persists sessions across center instances. Identity travels with
// the row so any center can authorize a request without local state.
type SessionStore interface {
	CreateSession(ctx context.Context, session *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	DeleteSession(ctx context.Context, id string) error
	CleanupSessions(ctx context.Context, now time.Time) error
}

// Session is an authenticated browser session.
type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	Role      Role      `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
}
