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
	if err := st.CreateSession(ctx, "s1", expires); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSession(ctx, "s1")
	if err != nil || !got.Equal(expires) {
		t.Fatalf("GetSession = %v, %v", got, err)
	}
	newExpires := expires.Add(time.Hour)
	if err := st.CreateSession(ctx, "s1", newExpires); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetSession(ctx, "s1")
	if !got.Equal(newExpires) {
		t.Fatalf("updated expiry = %v", got)
	}
	if err := st.CleanupSessions(ctx, time.Now().UTC().Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSession(ctx, "s1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSession after cleanup = %v", err)
	}
	if err := st.CreateSession(ctx, "", expires); err == nil {
		t.Fatal("empty session id accepted")
	}
	if err := st.DeleteSession(ctx, "missing"); err != nil {
		t.Fatalf("DeleteSession(missing) = %v", err)
	}
}
