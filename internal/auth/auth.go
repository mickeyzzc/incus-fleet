package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/mickeyzzc/incus-fleet/internal/store"
)

// PasswordHasher hashes and verifies passwords.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, hash string) bool
}

// bcryptHasher is the production password hasher.
type bcryptHasher struct{ cost int }

// DefaultPasswordHasher returns a bcrypt hasher at the default cost.
func DefaultPasswordHasher() PasswordHasher {
	return bcryptHasher{cost: bcrypt.DefaultCost}
}

func (h bcryptHasher) Hash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), h.cost)
	if err != nil {
		return "", err
	}

	return string(hash), nil
}

func (h bcryptHasher) Verify(password, hash string) bool {
	if hash == "" {
		return false
	}

	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// Authenticator validates credentials and manages sessions.
type Authenticator struct {
	// token is the optional shared bootstrap token. It stays usable only while
	// no local account exists, so an operator can create the first admin.
	token []byte
	ttl   time.Duration
	now   func() time.Time

	store         SessionStore
	users         UserStore
	lockoutAfter  int
	lockoutWindow time.Duration
	hasher        PasswordHasher

	mu       sync.Mutex
	sessions map[string]Session
}

// Option customises the authenticator.
type Option func(*Authenticator)

// WithUserStore enables local accounts and role-based authorization.
func WithUserStore(users UserStore) Option {
	return func(a *Authenticator) { a.users = users }
}

// WithSessionStore persists sessions in the shared database.
func WithSessionStore(store SessionStore) Option {
	return func(a *Authenticator) { a.store = store }
}

// WithPasswordHasher overrides the password hasher (tests use a cheap one).
func WithPasswordHasher(hasher PasswordHasher) Option {
	return func(a *Authenticator) { a.hasher = hasher }
}

// WithLockout configures the failed-attempt threshold and lock duration.
func WithLockout(after int, window time.Duration) Option {
	return func(a *Authenticator) {
		if after > 0 {
			a.lockoutAfter = after
		}
		if window > 0 {
			a.lockoutWindow = window
		}
	}
}

// New creates an authenticator. An empty token disables the bootstrap path.
func New(token string, ttl time.Duration, options ...Option) *Authenticator {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}

	a := &Authenticator{
		token:         []byte(token),
		ttl:           ttl,
		now:           time.Now,
		sessions:      make(map[string]Session),
		lockoutAfter:  5,
		lockoutWindow: 15 * time.Minute,
		hasher:        DefaultPasswordHasher(),
	}
	for _, option := range options {
		option(a)
	}

	return a
}

// TTL returns the session lifetime.
func (a *Authenticator) TTL() time.Duration { return a.ttl }

// UsersEnabled reports whether local accounts are configured.
func (a *Authenticator) UsersEnabled() bool { return a.users != nil }

// ValidateToken compares a candidate bootstrap token in constant time.
func (a *Authenticator) ValidateToken(token string) bool {
	if len(a.token) == 0 {
		return false
	}

	return subtle.ConstantTimeCompare(a.token, []byte(token)) == 1
}

// BootstrapAllowed reports whether the shared token may still be used. Once any
// local account exists the token is retired: a platform with user management
// must not stay reachable through one shared secret.
func (a *Authenticator) BootstrapAllowed(ctx context.Context) bool {
	if a.users == nil {
		return true
	}

	users, err := a.users.ListUsers(ctx)
	if err != nil {
		return false
	}

	return len(users) == 0
}

// LoginWithToken accepts the shared bootstrap token while no account exists.
func (a *Authenticator) LoginWithToken(ctx context.Context, token string) (Session, error) {
	if !a.ValidateToken(token) {
		return Session{}, ErrInvalidToken
	}
	if !a.BootstrapAllowed(ctx) {
		return Session{}, ErrInvalidToken
	}

	return a.createSession(ctx, Session{
		UserID:   bootstrapUserID,
		Username: "bootstrap-admin",
		Role:     RoleAdmin,
	})
}

// bootstrapUserID marks sessions created with the shared token.
const bootstrapUserID = "bootstrap"

// Login validates a username and password and creates a session.
func (a *Authenticator) Login(ctx context.Context, username, password string) (Session, error) {
	if a.users == nil {
		return Session{}, ErrInvalidCredentials
	}

	user, err := a.users.GetUserByUsername(ctx, strings.TrimSpace(username))
	if err != nil {
		// Verify against a dummy hash so a missing account is not
		// distinguishable from a wrong password by response time.
		a.hasher.Verify(password, dummyHash)
		if errors.Is(err, ErrUserNotFound) {
			return Session{}, ErrInvalidCredentials
		}

		return Session{}, err
	}

	now := a.now()
	if !user.LockedUntil.IsZero() && now.Before(user.LockedUntil) {
		return Session{}, ErrLocked
	}

	if !a.hasher.Verify(password, user.PasswordHash) {
		a.recordFailure(ctx, user)
		return Session{}, ErrInvalidCredentials
	}

	if user.Disabled {
		return Session{}, ErrUserDisabled
	}

	if user.FailedAttempts != 0 || !user.LockedUntil.IsZero() {
		user.FailedAttempts = 0
		user.LockedUntil = time.Time{}
		user.UpdatedAt = now
		if err := a.users.UpdateUser(ctx, user); err != nil {
			return Session{}, err
		}
	}

	return a.createSession(ctx, Session{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
	})
}

// recordFailure increments the failure counter and locks the account once the
// threshold is reached.
func (a *Authenticator) recordFailure(ctx context.Context, user *User) {
	user.FailedAttempts++
	if a.lockoutAfter > 0 && user.FailedAttempts >= a.lockoutAfter {
		user.LockedUntil = a.now().Add(a.lockoutWindow)
		user.FailedAttempts = 0
	}
	user.UpdatedAt = a.now()
	_ = a.users.UpdateUser(ctx, user)
}

// createSession allocates an opaque session id and records it.
func (a *Authenticator) createSession(ctx context.Context, session Session) (Session, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return Session{}, err
	}

	session.ID = base64.RawURLEncoding.EncodeToString(buf[:])
	session.ExpiresAt = a.now().Add(a.ttl)

	if a.store != nil {
		if err := a.store.CreateSession(ctx, &session); err != nil {
			return Session{}, err
		}
	}

	a.mu.Lock()
	a.sessions[session.ID] = session
	a.cleanupLocked()
	a.mu.Unlock()

	return session, nil
}

// Session returns the session for an id, if it is still valid. With a shared
// store the row carries the identity, so a session created by another center
// resolves correctly here.
func (a *Authenticator) Session(ctx context.Context, id string) (Session, bool) {
	if id == "" {
		return Session{}, false
	}

	// With a shared store the store is authoritative: another center may have
	// logged this session out, so a local cache must never outlive it.
	if a.store == nil {
		a.mu.Lock()
		session, ok := a.sessions[id]
		a.mu.Unlock()

		if !ok {
			return Session{}, false
		}
		if !a.now().Before(session.ExpiresAt) {
			a.Logout(ctx, id)
			return Session{}, false
		}

		return session, true
	}

	stored, err := a.store.GetSession(ctx, id)
	if err != nil {
		return Session{}, false
	}
	if !a.now().Before(stored.ExpiresAt) {
		_ = a.store.DeleteSession(ctx, id)
		return Session{}, false
	}

	session := Session{
		ID:        stored.ID,
		UserID:    stored.UserID,
		Username:  stored.Username,
		Role:      Role(stored.Role),
		ExpiresAt: stored.ExpiresAt,
	}

	a.mu.Lock()
	a.sessions[id] = session
	a.mu.Unlock()

	return session, true
}

// ValidateSession reports whether a session exists and has not expired.
func (a *Authenticator) ValidateSession(id string) bool {
	_, ok := a.Session(context.Background(), id)
	return ok
}

// Logout removes a session.
func (a *Authenticator) Logout(ctx context.Context, id string) {
	a.mu.Lock()
	delete(a.sessions, id)
	a.mu.Unlock()

	if a.store != nil {
		_ = a.store.DeleteSession(ctx, id)
	}
}

// SessionsForUser lists active sessions for an account. With a shared store
// the store is authoritative; the local cache is the fallback.
func (a *Authenticator) SessionsForUser(ctx context.Context, userID string) ([]Session, error) {
	if lister, ok := a.store.(interface {
		ListSessionsByUser(context.Context, string) ([]store.Session, error)
	}); ok {
		rows, err := lister.ListSessionsByUser(ctx, userID)
		if err != nil {
			return nil, err
		}
		sessions := make([]Session, 0, len(rows))
		for _, row := range rows {
			if !a.now().Before(row.ExpiresAt) {
				continue
			}
			sessions = append(sessions, Session{
				ID: row.ID, UserID: row.UserID, Username: row.Username,
				Role: Role(row.Role), ExpiresAt: row.ExpiresAt,
			})
		}
		return sessions, nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	sessions := make([]Session, 0)
	for _, session := range a.sessions {
		if session.UserID == userID && a.now().Before(session.ExpiresAt) {
			sessions = append(sessions, session)
		}
	}
	return sessions, nil
}

// RevokeUserSessions removes every session of an account except keepID.
func (a *Authenticator) RevokeUserSessions(ctx context.Context, userID, keepID string) (int, error) {
	sessions, err := a.SessionsForUser(ctx, userID)
	if err != nil {
		return 0, err
	}

	revoked := 0
	for _, session := range sessions {
		if session.ID == keepID {
			continue
		}
		a.Logout(ctx, session.ID)
		revoked++
	}

	return revoked, nil
}

// Cleanup removes expired sessions.
func (a *Authenticator) Cleanup(ctx context.Context) {
	if a.store != nil {
		_ = a.store.CleanupSessions(ctx, a.now())
	}

	a.mu.Lock()
	a.cleanupLocked()
	a.mu.Unlock()
}

func (a *Authenticator) cleanupLocked() {
	now := a.now()
	for id, session := range a.sessions {
		if !now.Before(session.ExpiresAt) {
			delete(a.sessions, id)
		}
	}
}

// dummyHash is verified against when a username does not exist, so the timing
// of a missing account resembles a wrong password.
var dummyHash = func() string {
	hash, err := DefaultPasswordHasher().Hash("dummy-password-for-timing")
	if err != nil {
		return ""
	}

	return hash
}()

// HashPassword produces a storable hash, enforcing the minimum policy.
func (a *Authenticator) HashPassword(password string) (string, error) {
	if err := CheckPasswordPolicy(password); err != nil {
		return "", err
	}

	return a.hasher.Hash(password)
}

// CheckPasswordPolicy enforces the minimum password requirements.
//
// The policy is deliberately a length floor rather than a character-class
// matrix: operators frequently set generated or dated credentials, and
// rejecting those pushes them to reuse stronger-looking but reused secrets.
// Eight characters is the documented floor.
func CheckPasswordPolicy(password string) error {
	if len(password) < 8 {
		return errors.New("auth: password must be at least 8 characters")
	}

	return nil
}
