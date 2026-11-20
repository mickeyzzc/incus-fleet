package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestUserLifecycle(t *testing.T) {
	ctx := context.Background()
	st := openSQLite(t)

	user := &User{
		ID:           "user-1",
		Username:     "Alice",
		PasswordHash: "hash",
		Role:         "admin",
	}
	if err := st.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	// Usernames are stored lowercase and looked up case-insensitively.
	got, err := st.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("GetUserByUsername() error = %v", err)
	}
	if got.Username != "alice" || got.Role != "admin" || got.Disabled {
		t.Fatalf("user = %#v", got)
	}

	if _, err := st.GetUser(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetUser(missing) = %v", err)
	}
	if _, err := st.GetUserByUsername(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetUserByUsername(missing) = %v", err)
	}

	if err := st.CreateUser(ctx, &User{ID: "user-2", Username: "alice", Role: "viewer"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate username = %v, want ErrConflict", err)
	}

	got.Disabled = true
	got.MustChangePassword = true
	if err := st.UpdateUser(ctx, got); err != nil {
		t.Fatalf("UpdateUser() error = %v", err)
	}
	got, _ = st.GetUser(ctx, "user-1")
	if !got.Disabled || !got.MustChangePassword {
		t.Fatalf("update not persisted: %#v", got)
	}

	if err := st.UpdateUser(ctx, &User{ID: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateUser(missing) = %v", err)
	}

	if err := st.DeleteUser(ctx, "user-1"); err != nil {
		t.Fatalf("DeleteUser() error = %v", err)
	}
	if err := st.DeleteUser(ctx, "user-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteUser twice = %v", err)
	}
}

func TestCountAdmins(t *testing.T) {
	ctx := context.Background()
	st := openSQLite(t)

	count, err := st.CountAdmins(ctx)
	if err != nil || count != 0 {
		t.Fatalf("CountAdmins() = %d, %v", count, err)
	}

	if err := st.CreateUser(ctx, &User{ID: "a", Username: "admin1", Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateUser(ctx, &User{ID: "b", Username: "admin2", Role: "admin", Disabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateUser(ctx, &User{ID: "c", Username: "op", Role: "operator"}); err != nil {
		t.Fatal(err)
	}

	count, err = st.CountAdmins(ctx)
	if err != nil || count != 1 {
		t.Fatalf("CountAdmins() = %d, %v; want 1 enabled admin", count, err)
	}
}

func TestUserLockFields(t *testing.T) {
	ctx := context.Background()
	st := openSQLite(t)

	locked := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Nanosecond)
	user := &User{ID: "u", Username: "locked", Role: "operator", FailedAttempts: 2, LockedUntil: locked}
	if err := st.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetUser(ctx, "u")
	if err != nil {
		t.Fatal(err)
	}
	if got.FailedAttempts != 2 || !got.LockedUntil.Equal(locked) {
		t.Fatalf("lock fields = %d, %v", got.FailedAttempts, got.LockedUntil)
	}
}

func TestCreateUserValidation(t *testing.T) {
	ctx := context.Background()
	st := openSQLite(t)

	if err := st.CreateUser(ctx, nil); err == nil {
		t.Fatal("nil user accepted")
	}
	if err := st.CreateUser(ctx, &User{Username: "x"}); err == nil {
		t.Fatal("missing id accepted")
	}
	if err := st.CreateUser(ctx, &User{ID: "x"}); err == nil {
		t.Fatal("missing username accepted")
	}

	// Timestamps default when unset.
	user := &User{ID: "u1", Username: "alice", Role: "viewer"}
	if err := st.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	if user.CreatedAt.IsZero() || user.UpdatedAt.IsZero() {
		t.Fatal("timestamps were not defaulted")
	}

	if err := st.UpdateUser(ctx, nil); err == nil {
		t.Fatal("nil update accepted")
	}
	if err := st.UpdateUser(ctx, &User{}); err == nil {
		t.Fatal("update without id accepted")
	}
}

func TestCountAdminsErrorAfterClose(t *testing.T) {
	st := openSQLite(t)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CountAdmins(context.Background()); err == nil {
		t.Fatal("CountAdmins() on a closed store returned no error")
	}
}
