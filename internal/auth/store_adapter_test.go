package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/store"
)

func openAdapter(t *testing.T) *StoreAdapter {
	t.Helper()

	st, err := store.Open(context.Background(), "sqlite", t.TempDir()+"/adapter.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	return NewStoreAdapter(st)
}

func TestStoreAdapterUserRoundTrip(t *testing.T) {
	ctx := context.Background()
	adapter := openAdapter(t)

	user := &User{
		ID:           "u1",
		Username:     "Alice",
		PasswordHash: "hash",
		Role:         RoleOperator,
	}
	if err := adapter.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser() = %v", err)
	}

	got, err := adapter.GetUser(ctx, "u1")
	if err != nil {
		t.Fatalf("GetUser() = %v", err)
	}
	if got.Username != "alice" || got.Role != RoleOperator {
		t.Fatalf("user = %#v", got)
	}

	byName, err := adapter.GetUserByUsername(ctx, "ALICE")
	if err != nil || byName.ID != "u1" {
		t.Fatalf("GetUserByUsername() = %#v, %v", byName, err)
	}

	if _, err := adapter.GetUser(ctx, "missing"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("GetUser(missing) = %v, want ErrUserNotFound", err)
	}
	if _, err := adapter.GetUserByUsername(ctx, "missing"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("GetUserByUsername(missing) = %v, want ErrUserNotFound", err)
	}

	// Duplicate usernames surface as ErrUserExists for the API layer.
	if err := adapter.CreateUser(ctx, &User{ID: "u2", Username: "alice"}); !errors.Is(err, ErrUserExists) {
		t.Fatalf("duplicate = %v, want ErrUserExists", err)
	}

	got.Disabled = true
	if err := adapter.UpdateUser(ctx, got); err != nil {
		t.Fatalf("UpdateUser() = %v", err)
	}
	if err := adapter.UpdateUser(ctx, &User{ID: "missing"}); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("UpdateUser(missing) = %v", err)
	}

	users, err := adapter.ListUsers(ctx)
	if err != nil || len(users) != 1 {
		t.Fatalf("ListUsers() = %d, %v", len(users), err)
	}

	admins, err := adapter.CountAdmins(ctx)
	if err != nil || admins != 0 {
		t.Fatalf("CountAdmins() = %d, %v", admins, err)
	}

	if err := adapter.DeleteUser(ctx, "u1"); err != nil {
		t.Fatalf("DeleteUser() = %v", err)
	}
	if err := adapter.DeleteUser(ctx, "u1"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("DeleteUser twice = %v", err)
	}
}

func TestStoreAdapterSessions(t *testing.T) {
	ctx := context.Background()
	adapter := openAdapter(t)

	session := &Session{
		ID:        "s1",
		UserID:    "u1",
		Username:  "alice",
		Role:      RoleAdmin,
		ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Nanosecond),
	}
	if err := adapter.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() = %v", err)
	}

	got, err := adapter.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("GetSession() = %v", err)
	}
	if got.Username != "alice" || got.Role != RoleAdmin || got.UserID != "u1" {
		t.Fatalf("session = %#v", got)
	}

	if _, err := adapter.GetSession(ctx, "missing"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("GetSession(missing) = %v", err)
	}

	if err := adapter.CleanupSessions(ctx, time.Now().UTC().Add(2*time.Hour)); err != nil {
		t.Fatalf("CleanupSessions() = %v", err)
	}
	if _, err := adapter.GetSession(ctx, "s1"); err == nil {
		t.Fatal("session survived cleanup")
	}

	if err := adapter.DeleteSession(ctx, "missing"); err != nil {
		t.Fatalf("DeleteSession(missing) = %v", err)
	}
}

func TestAuthenticatorOptions(t *testing.T) {
	a := New("secret", time.Hour, WithLockout(3, time.Minute), WithPasswordHasher(cheapHasher{}))
	if a.lockoutAfter != 3 || a.lockoutWindow != time.Minute {
		t.Fatalf("lockout = %d, %v", a.lockoutAfter, a.lockoutWindow)
	}

	// Zero values keep the defaults.
	b := New("secret", time.Hour, WithLockout(0, 0))
	if b.lockoutAfter != 5 || b.lockoutWindow != 15*time.Minute {
		t.Fatalf("defaults = %d, %v", b.lockoutAfter, b.lockoutWindow)
	}

	if a.UsersEnabled() {
		t.Fatal("UsersEnabled() = true without a user store")
	}
	if !New("", time.Hour, WithUserStore(newFakeUsers())).UsersEnabled() {
		t.Fatal("UsersEnabled() = false with a user store")
	}
}

func TestHashPasswordRejectsWeakInput(t *testing.T) {
	a := New("", time.Hour, WithPasswordHasher(cheapHasher{}))
	if _, err := a.HashPassword("short"); err == nil {
		t.Fatal("HashPassword accepted a short password")
	}
	if _, err := a.HashPassword("long-enough-here"); err != nil {
		t.Fatalf("HashPassword() = %v", err)
	}
}

func TestBcryptHasher(t *testing.T) {
	hasher := bcryptHasher{cost: 4}

	hash, err := hasher.Hash("correct-horse-9")
	if err != nil {
		t.Fatalf("Hash() = %v", err)
	}
	if !hasher.Verify("correct-horse-9", hash) {
		t.Fatal("Verify() rejected the correct password")
	}
	if hasher.Verify("wrong", hash) {
		t.Fatal("Verify() accepted a wrong password")
	}
	if hasher.Verify("anything", "") {
		t.Fatal("Verify() accepted an empty hash")
	}
}
