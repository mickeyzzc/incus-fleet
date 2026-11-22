package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeUsers is an in-memory UserStore for tests.
type fakeUsers struct {
	mu    sync.Mutex
	byID  map[string]*User
	order []string
}

func newFakeUsers() *fakeUsers {
	return &fakeUsers{byID: map[string]*User{}}
}

func (f *fakeUsers) CreateUser(_ context.Context, user *User) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, existing := range f.byID {
		if strings.EqualFold(existing.Username, user.Username) {
			return ErrUserExists
		}
	}
	clone := *user
	f.byID[user.ID] = &clone
	f.order = append(f.order, user.ID)

	return nil
}

func (f *fakeUsers) GetUser(_ context.Context, id string) (*User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	user, ok := f.byID[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	clone := *user

	return &clone, nil
}

func (f *fakeUsers) GetUserByUsername(_ context.Context, username string) (*User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, user := range f.byID {
		if strings.EqualFold(user.Username, username) {
			clone := *user
			return &clone, nil
		}
	}

	return nil, ErrUserNotFound
}

func (f *fakeUsers) ListUsers(context.Context) ([]*User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]*User, 0, len(f.order))
	for _, id := range f.order {
		if user, ok := f.byID[id]; ok {
			clone := *user
			out = append(out, &clone)
		}
	}

	return out, nil
}

func (f *fakeUsers) UpdateUser(_ context.Context, user *User) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.byID[user.ID]; !ok {
		return ErrUserNotFound
	}
	clone := *user
	f.byID[user.ID] = &clone

	return nil
}

func (f *fakeUsers) DeleteUser(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.byID[id]; !ok {
		return ErrUserNotFound
	}
	delete(f.byID, id)

	return nil
}

func (f *fakeUsers) CountAdmins(context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	count := 0
	for _, user := range f.byID {
		if user.Role == RoleAdmin && !user.Disabled {
			count++
		}
	}

	return count, nil
}

// cheapHasher keeps tests fast; bcrypt at the default cost is deliberately slow.
type cheapHasher struct{}

func (cheapHasher) Hash(password string) (string, error) { return "h:" + password, nil }

func (cheapHasher) Verify(password, hash string) bool { return hash == "h:"+password }

func newTestAuth(t *testing.T) (*Authenticator, *fakeUsers) {
	t.Helper()

	users := newFakeUsers()
	a := New("secret", time.Hour, WithUserStore(users), WithPasswordHasher(cheapHasher{}))
	fixed := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return fixed }

	return a, users
}

func seedUser(t *testing.T, users *fakeUsers, username string, role Role, password string, disabled bool) *User {
	t.Helper()

	user := &User{
		ID:           "user-" + username,
		Username:     username,
		Role:         role,
		PasswordHash: cheapHasher{}.mustHash(password),
		Disabled:     disabled,
	}
	if err := users.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	return user
}

func (cheapHasher) mustHash(password string) string {
	hash, err := cheapHasher{}.Hash(password)
	if err != nil {
		panic(err)
	}

	return hash
}

func TestBootstrapTokenOnlyBeforeFirstUser(t *testing.T) {
	ctx := context.Background()
	a, users := newTestAuth(t)

	session, err := a.LoginWithToken(ctx, "secret")
	if err != nil {
		t.Fatalf("bootstrap login before any user = %v", err)
	}
	if session.Role != RoleAdmin {
		t.Fatalf("bootstrap role = %q", session.Role)
	}

	seedUser(t, users, "admin", RoleAdmin, "correct-horse-9", false)

	// Once a local account exists the shared token must stop working.
	if _, err := a.LoginWithToken(ctx, "secret"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("bootstrap login after first user = %v, want ErrInvalidToken", err)
	}
	if a.BootstrapAllowed(ctx) {
		t.Fatal("BootstrapAllowed() = true after an account exists")
	}
}

func TestLoginByUsernameAndPassword(t *testing.T) {
	ctx := context.Background()
	a, users := newTestAuth(t)
	seedUser(t, users, "alice", RoleOperator, "correct-horse-9", false)

	session, err := a.Login(ctx, "alice", "correct-horse-9")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if session.Username != "alice" || session.Role != RoleOperator || session.ID == "" {
		t.Fatalf("session = %#v", session)
	}
	if !a.ValidateSession(session.ID) {
		t.Fatal("ValidateSession() = false for a fresh session")
	}

	if _, err := a.Login(ctx, "alice", "wrong-password-1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password error = %v", err)
	}
	if _, err := a.Login(ctx, "nobody", "correct-horse-9"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown user error = %v", err)
	}
}

func TestDisabledUserCannotSignIn(t *testing.T) {
	ctx := context.Background()
	a, users := newTestAuth(t)
	seedUser(t, users, "bob", RoleViewer, "correct-horse-9", true)

	if _, err := a.Login(ctx, "bob", "correct-horse-9"); !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("disabled login error = %v, want ErrUserDisabled", err)
	}
}

func TestLockoutAfterRepeatedFailures(t *testing.T) {
	ctx := context.Background()
	a, users := newTestAuth(t)
	seedUser(t, users, "carol", RoleOperator, "correct-horse-9", false)

	for i := 0; i < 5; i++ {
		if _, err := a.Login(ctx, "carol", "wrong-password-1"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d error = %v", i+1, err)
		}
	}

	// Even the correct password is refused while locked.
	if _, err := a.Login(ctx, "carol", "correct-horse-9"); !errors.Is(err, ErrLocked) {
		t.Fatalf("locked login error = %v, want ErrLocked", err)
	}
}

func TestSessionExpiryAndCleanup(t *testing.T) {
	ctx := context.Background()
	a, users := newTestAuth(t)
	seedUser(t, users, "dave", RoleViewer, "correct-horse-9", false)

	session, err := a.Login(ctx, "dave", "correct-horse-9")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	base := a.now()
	a.now = func() time.Time { return base.Add(2 * time.Hour) }

	if a.ValidateSession(session.ID) {
		t.Fatal("expired session still valid")
	}

	if _, err := a.Login(ctx, "dave", "correct-horse-9"); err != nil {
		t.Fatalf("Login() after expiry = %v", err)
	}
	a.Cleanup(ctx)

	a.mu.Lock()
	count := len(a.sessions)
	a.mu.Unlock()
	if count != 1 {
		t.Fatalf("sessions after cleanup = %d, want 1", count)
	}
}

func TestLogoutRemovesSession(t *testing.T) {
	ctx := context.Background()
	a, users := newTestAuth(t)
	seedUser(t, users, "erin", RoleViewer, "correct-horse-9", false)

	session, err := a.Login(ctx, "erin", "correct-horse-9")
	if err != nil {
		t.Fatal(err)
	}
	a.Logout(ctx, session.ID)
	if a.ValidateSession(session.ID) {
		t.Fatal("session survived logout")
	}
}

func TestRolePermissions(t *testing.T) {
	cases := []struct {
		role      Role
		wantWrite bool
		wantAdmin bool
	}{
		{RoleAdmin, true, true},
		{RoleOperator, true, false},
		{RoleViewer, false, false},
	}

	for _, tc := range cases {
		if got := tc.role.CanWrite(); got != tc.wantWrite {
			t.Errorf("%s.CanWrite() = %v, want %v", tc.role, got, tc.wantWrite)
		}
		if got := tc.role.CanAdmin(); got != tc.wantAdmin {
			t.Errorf("%s.CanAdmin() = %v, want %v", tc.role, got, tc.wantAdmin)
		}
		if !tc.role.Valid() {
			t.Errorf("%s.Valid() = false", tc.role)
		}
	}

	if Role("superuser").Valid() {
		t.Error("unknown role reported valid")
	}
}

func TestPasswordPolicy(t *testing.T) {
	cases := []struct {
		password string
		wantErr  bool
	}{
		{"short", true},
		{"test-admin-passw0rd", false},
		{"correct-horse-9", false},
	}

	for _, tc := range cases {
		err := CheckPasswordPolicy(tc.password)
		if (err != nil) != tc.wantErr {
			t.Errorf("CheckPasswordPolicy(%q) error = %v, wantErr %v", tc.password, err, tc.wantErr)
		}
	}
}

func TestValidateToken(t *testing.T) {
	a := New("secret", time.Hour)
	if !a.ValidateToken("secret") {
		t.Fatal("valid token rejected")
	}
	if a.ValidateToken("Secret") || a.ValidateToken("") {
		t.Fatal("invalid token accepted")
	}

	empty := New("", time.Hour)
	if empty.ValidateToken("") {
		t.Fatal("empty authenticator accepted empty token")
	}
}

func TestDefaultTTL(t *testing.T) {
	a := New("secret", 0)
	if a.TTL() != 12*time.Hour {
		t.Fatalf("TTL() = %v", a.TTL())
	}
}

func TestSessionWithStoreErrors(t *testing.T) {
	ctx := context.Background()
	shared := newMemorySessionStore()
	a := New("token", time.Hour, WithSessionStore(shared))
	other := New("token", time.Hour, WithSessionStore(shared))

	session, err := a.LoginWithToken(ctx, "token")
	if err != nil {
		t.Fatal(err)
	}

	shared.getErr = errors.New("boom")
	if _, ok := other.Session(ctx, session.ID); ok {
		t.Fatal("Session() succeeded despite a store error")
	}

	shared.getErr = nil
	shared.sessions[session.ID].ExpiresAt = a.now().Add(-time.Minute)
	if _, ok := other.Session(ctx, session.ID); ok {
		t.Fatal("Session() accepted an expired row")
	}
	if _, exists := shared.sessions[session.ID]; exists {
		t.Fatal("expired row was not deleted")
	}
}

func TestCleanupRemovesExpiredLocalSessions(t *testing.T) {
	ctx := context.Background()
	a, users := newTestAuth(t)
	seedUser(t, users, "zoe", RoleViewer, "correct-horse-9", false)

	session, err := a.Login(ctx, "zoe", "correct-horse-9")
	if err != nil {
		t.Fatal(err)
	}

	base := a.now()
	a.now = func() time.Time { return base.Add(2 * time.Hour) }
	a.Cleanup(ctx)

	a.mu.Lock()
	_, exists := a.sessions[session.ID]
	a.mu.Unlock()
	if exists {
		t.Fatal("expired session survived Cleanup")
	}
}

func TestLoginRecordsFailure(t *testing.T) {
	ctx := context.Background()
	a, users := newTestAuth(t)
	user := seedUser(t, users, "frank", RoleViewer, "correct-horse-9", false)

	if _, err := a.Login(ctx, "frank", "wrong-password-1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("error = %v", err)
	}

	stored, err := users.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.FailedAttempts != 1 {
		t.Fatalf("failed attempts = %d, want 1", stored.FailedAttempts)
	}
}

func TestFailedSuccessResetsCounter(t *testing.T) {
	ctx := context.Background()
	a, users := newTestAuth(t)
	user := seedUser(t, users, "grace", RoleViewer, "correct-horse-9", false)

	_, _ = a.Login(ctx, "grace", "wrong-password-1")
	if _, err := a.Login(ctx, "grace", "correct-horse-9"); err != nil {
		t.Fatalf("Login() = %v", err)
	}

	stored, err := users.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.FailedAttempts != 0 || !stored.LockedUntil.IsZero() {
		t.Fatalf("counter not reset: %#v", stored)
	}
}

func TestLoginWithoutUserStore(t *testing.T) {
	a := New("secret", time.Hour)
	if _, err := a.Login(context.Background(), "any", "any-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("error = %v, want ErrInvalidCredentials", err)
	}
}
