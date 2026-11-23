package httpapi

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// fixedWindowLimiter is a small in-process rate limiter for authentication
// endpoints. It is best-effort: behind multiple centers each process keeps its
// own window, while per-account lockout in auth remains shared via the store.
type fixedWindowLimiter struct {
	mu      sync.Mutex
	windows map[string]*rateWindow
	limit   int
	window  time.Duration
}

type rateWindow struct {
	count   int
	resetAt time.Time
}

func newFixedWindowLimiter(limit int, window time.Duration) *fixedWindowLimiter {
	return &fixedWindowLimiter{
		windows: make(map[string]*rateWindow),
		limit:   limit,
		window:  window,
	}
}

func (l *fixedWindowLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	for id, entry := range l.windows {
		if now.After(entry.resetAt) {
			delete(l.windows, id)
		}
	}

	entry := l.windows[key]
	if entry == nil {
		entry = &rateWindow{resetAt: now.Add(l.window)}
		l.windows[key] = entry
	}

	if entry.count >= l.limit {
		return false
	}
	entry.count++

	return true
}

/** clientIP returns the direct peer address, ignoring spoofable forward headers. */
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}

// loginLimiter caps sign-in attempts per client. Ten attempts per minute is
// well above human usage but stops password spraying from a single host.
var loginLimiter = newFixedWindowLimiter(10, time.Minute)

// registrationLimiter slows automated bootstrap probing without blocking the
// legitimate one-off first-account creation.
var registrationLimiter = newFixedWindowLimiter(5, time.Minute)

func applyRateLimit(l *fixedWindowLimiter, r *http.Request) bool {
	return l.allow(strings.TrimSpace(clientIP(r)), time.Now())
}
