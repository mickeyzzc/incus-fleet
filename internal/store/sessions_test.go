package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSessions(t *testing.T) {
	st, err := Open(context.Background(), "sqlite", t.TempDir()+"/sessions.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()

	expires := time.Now().UTC().Add(time.Hour).Truncate(time.Nanosecond)
	session := &Session{
		ID:        "s1",
		UserID:    "user-1",
		Username:  "alice",
		Role:      "operator",
		ExpiresAt: expires,
	}
	if err := st.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetSession(ctx, "s1")
	if err != nil || !got.ExpiresAt.Equal(expires) {
		t.Fatalf("GetSession = %#v, %v", got, err)
	}
	// Identity must round-trip so another center can authorize the request.
	if got.Username != "alice" || got.UserID != "user-1" || got.Role != "operator" {
		t.Fatalf("session identity = %#v", got)
	}

	newExpires := expires.Add(time.Hour)
	session.ExpiresAt = newExpires
	if err := st.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetSession(ctx, "s1")
	if !got.ExpiresAt.Equal(newExpires) {
		t.Fatalf("updated expiry = %v", got.ExpiresAt)
	}

	if err := st.CleanupSessions(ctx, time.Now().UTC().Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSession(ctx, "s1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSession after cleanup = %v", err)
	}
	if err := st.CreateSession(ctx, &Session{}); err == nil {
		t.Fatal("empty session id accepted")
	}
	if err := st.CreateSession(ctx, nil); err == nil {
		t.Fatal("nil session accepted")
	}
	if err := st.DeleteSession(ctx, "missing"); err != nil {
		t.Fatalf("DeleteSession(missing) = %v", err)
	}
}
