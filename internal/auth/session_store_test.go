package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/store"
)

type memorySessionStore struct {
	sessions   map[string]time.Time
	createErr  error
	getErr     error
	deleteErr  error
	cleanupNow time.Time
}

func newMemorySessionStore() *memorySessionStore {
	return &memorySessionStore{sessions: make(map[string]time.Time)}
}

func (s *memorySessionStore) CreateSession(_ context.Context, id string, expires time.Time) error {
	if s.createErr != nil {
		return s.createErr
	}
	s.sessions[id] = expires
	return nil
}

func (s *memorySessionStore) GetSession(_ context.Context, id string) (time.Time, error) {
	if s.getErr != nil {
		return time.Time{}, s.getErr
	}
	expires, ok := s.sessions[id]
	if !ok {
		return time.Time{}, store.ErrNotFound
	}
	return expires, nil
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
	for id, expires := range s.sessions {
		if !now.Before(expires) {
			delete(s.sessions, id)
		}
	}
	return nil
}

func TestSharedSessionStore(t *testing.T) {
	shared := newMemorySessionStore()
	a := NewWithStore("token", time.Hour, shared)
	other := NewWithStore("token", time.Hour, shared)

	id, expires, err := a.Login("token")
	if err != nil || id == "" || expires.IsZero() {
		t.Fatalf("Login = %q, %v, %v", id, expires, err)
	}
	if !other.ValidateSession(id) {
		t.Fatal("session not visible to the other center")
	}
	other.Logout(id)
	if a.ValidateSession(id) {
		t.Fatal("session not deleted across centers")
	}

	id, _, _ = a.Login("token")
	shared.deleteErr = errors.New("delete failed")
	a.Logout(id)
	a.Cleanup()
	if shared.cleanupNow.IsZero() {
		t.Fatal("Cleanup did not reach shared store")
	}
}

func TestSharedSessionStoreErrors(t *testing.T) {
	shared := newMemorySessionStore()
	shared.createErr = errors.New("create failed")
	a := NewWithStore("token", time.Hour, shared)
	if _, _, err := a.Login("token"); err == nil {
		t.Fatal("Login expected store error")
	}
	shared.createErr = nil
	id, _, err := a.Login("token")
	if err != nil {
		t.Fatal(err)
	}
	shared.getErr = errors.New("get failed")
	if a.ValidateSession(id) {
		t.Fatal("ValidateSession should fail on store error")
	}
}
