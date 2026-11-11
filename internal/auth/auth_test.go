package auth

import (
	"errors"
	"testing"
	"time"
)

func TestLoginAndSessions(t *testing.T) {
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	a := New("secret", time.Hour)
	a.now = func() time.Time { return now }

	if _, _, err := a.Login("wrong"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Login() error = %v", err)
	}

	id, expires, err := a.Login("secret")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if id == "" || !expires.Equal(now.Add(time.Hour)) {
		t.Fatalf("session = %q %v", id, expires)
	}
	if !a.ValidateSession(id) {
		t.Fatal("ValidateSession() = false")
	}
	if a.ValidateSession("") || a.ValidateSession("missing") {
		t.Fatal("ValidateSession() accepted unknown session")
	}

	a.Logout(id)
	if a.ValidateSession(id) {
		t.Fatal("session survived logout")
	}
}

func TestExpiryAndCleanup(t *testing.T) {
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	a := New("secret", time.Minute)
	a.now = func() time.Time { return now }

	id, _, err := a.Login("secret")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	now = now.Add(2 * time.Minute)
	if a.ValidateSession(id) {
		t.Fatal("expired session valid")
	}

	if _, _, err := a.Login("secret"); err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	a.Cleanup()

	a.mu.Lock()
	count := len(a.sessions)
	a.mu.Unlock()
	if count != 1 {
		t.Fatalf("sessions after cleanup = %d", count)
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
