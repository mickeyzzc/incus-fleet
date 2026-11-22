package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/store"
)

type memorySessionStore struct {
	sessions   map[string]*Session
	createErr  error
	getErr     error
	deleteErr  error
	cleanupNow time.Time
}

func newMemorySessionStore() *memorySessionStore {
	return &memorySessionStore{sessions: make(map[string]*Session)}
}

func (s *memorySessionStore) CreateSession(_ context.Context, session *Session) error {
	if s.createErr != nil {
		return s.createErr
	}
	clone := *session
	s.sessions[session.ID] = &clone

	return nil
}

func (s *memorySessionStore) GetSession(_ context.Context, id string) (*Session, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	session, ok := s.sessions[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	clone := *session

	return &clone, nil
}

func (s *memorySessionStore) DeleteSession(_ context.Context, id string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.sessions, id)

	return nil
}

func (s *memorySessionStore) CleanupSessions(_ context.Context, now time.Time) error {
	s.cleanupNow = now
	for id, session := range s.sessions {
		if !now.Before(session.ExpiresAt) {
			delete(s.sessions, id)
		}
	}

	return nil
}

func TestSharedSessionStore(t *testing.T) {
	ctx := context.Background()
	shared := newMemorySessionStore()
	a := New("token", time.Hour, WithSessionStore(shared))
	other := New("token", time.Hour, WithSessionStore(shared))

	session, err := a.LoginWithToken(ctx, "token")
	if err != nil || session.ID == "" || session.ExpiresAt.IsZero() {
		t.Fatalf("Login = %#v, %v", session, err)
	}
	if !other.ValidateSession(session.ID) {
		t.Fatal("session not visible to the other center")
	}

	// The identity must survive the trip through the shared store.
	resolved, ok := other.Session(ctx, session.ID)
	if !ok || resolved.Username != session.Username || resolved.Role != session.Role {
		t.Fatalf("cross-center session = %#v, ok=%v", resolved, ok)
	}

	other.Logout(ctx, session.ID)
	if a.ValidateSession(session.ID) {
		t.Fatal("session not deleted across centers")
	}

	renewed, err := a.LoginWithToken(ctx, "token")
	if err != nil {
		t.Fatal(err)
	}
	shared.deleteErr = errors.New("delete failed")
	a.Logout(ctx, renewed.ID)
	a.Cleanup(ctx)
	if shared.cleanupNow.IsZero() {
		t.Fatal("Cleanup did not reach shared store")
	}
}

func TestSharedSessionStoreErrors(t *testing.T) {
	ctx := context.Background()
	shared := newMemorySessionStore()
	shared.createErr = errors.New("create failed")
	a := New("token", time.Hour, WithSessionStore(shared))
	if _, err := a.LoginWithToken(ctx, "token"); err == nil {
		t.Fatal("Login expected store error")
	}

	shared.createErr = nil
	session, err := a.LoginWithToken(ctx, "token")
	if err != nil {
		t.Fatal(err)
	}

	shared.getErr = errors.New("get failed")
	// A fresh local cache still answers; the store error only matters for IDs
	// this process has not seen.
	a.mu.Lock()
	delete(a.sessions, session.ID)
	a.mu.Unlock()
	if a.ValidateSession(session.ID) {
		t.Fatal("ValidateSession should fail on store error")
	}
}
