// Package auth provides token login and in-memory sessions.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

// ErrInvalidToken is returned for wrong credentials.
var ErrInvalidToken = errors.New("auth: invalid token")

// Authenticator validates the shared admin token and manages sessions.
type Authenticator struct {
	token []byte
	ttl   time.Duration
	now   func() time.Time

	mu       sync.Mutex
	sessions map[string]time.Time
}

// New creates an authenticator. Empty tokens are rejected by Login.
func New(token string, ttl time.Duration) *Authenticator {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}

	return &Authenticator{
		token:    []byte(token),
		ttl:      ttl,
		now:      time.Now,
		sessions: make(map[string]time.Time),
	}
}

// TTL returns the session lifetime.
func (a *Authenticator) TTL() time.Duration { return a.ttl }

// ValidateToken compares a candidate token in constant time.
func (a *Authenticator) ValidateToken(token string) bool {
	if len(a.token) == 0 {
		return false
	}

	return subtle.ConstantTimeCompare(a.token, []byte(token)) == 1
}

// Login validates the admin token and creates a session.
func (a *Authenticator) Login(token string) (string, time.Time, error) {
	if !a.ValidateToken(token) {
		return "", time.Time{}, ErrInvalidToken
	}

	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", time.Time{}, err
	}

	id := base64.RawURLEncoding.EncodeToString(buf[:])
	expires := a.now().Add(a.ttl)

	a.mu.Lock()
	a.sessions[id] = expires
	a.cleanupLocked()
	a.mu.Unlock()

	return id, expires, nil
}

// ValidateSession reports whether a session exists and has not expired.
func (a *Authenticator) ValidateSession(id string) bool {
	if id == "" {
		return false
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	expires, ok := a.sessions[id]
	if !ok {
		return false
	}
	if !a.now().Before(expires) {
		delete(a.sessions, id)
		return false
	}

	return true
}

// Logout removes a session.
func (a *Authenticator) Logout(id string) {
	a.mu.Lock()
	delete(a.sessions, id)
	a.mu.Unlock()
}

// Cleanup removes expired sessions.
func (a *Authenticator) Cleanup() {
	a.mu.Lock()
	a.cleanupLocked()
	a.mu.Unlock()
}

func (a *Authenticator) cleanupLocked() {
	now := a.now()
	for id, expires := range a.sessions {
		if !now.Before(expires) {
			delete(a.sessions, id)
		}
	}
}
