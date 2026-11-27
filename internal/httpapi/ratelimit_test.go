package httpapi

import (
	"net/http"
	"testing"
	"time"
)

func TestFixedWindowLimiterAllowsUntilLimit(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	l := newFixedWindowLimiter(2, time.Minute)

	if !l.allow("client", now) {
		t.Fatal("first request should be allowed")
	}
	if !l.allow("client", now.Add(time.Second)) {
		t.Fatal("second request should be allowed")
	}
	if l.allow("client", now.Add(2*time.Second)) {
		t.Fatal("third request should be rejected")
	}
	if !l.allow("other", now) {
		t.Fatal("a different key has its own window")
	}
}

func TestFixedWindowLimiterExpiresOldWindows(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	l := newFixedWindowLimiter(1, time.Minute)
	if !l.allow("client", now) {
		t.Fatal("first request should be allowed")
	}
	if l.allow("client", now.Add(time.Second)) {
		t.Fatal("window should still be active")
	}
	if !l.allow("client", now.Add(time.Minute+time.Second)) {
		t.Fatal("expired window should be discarded")
	}
	if len(l.windows) != 1 {
		t.Fatalf("windows = %d, want 1", len(l.windows))
	}
}

func TestClientIP(t *testing.T) {
	request := func(remote string) *http.Request {
		r := &http.Request{RemoteAddr: remote}
		r.Header = http.Header{}
		return r
	}
	if got := clientIP(request("192.0.2.10:8443")); got != "192.0.2.10" {
		t.Fatalf("clientIP = %q", got)
	}
	if got := clientIP(request("198.51.100.7")); got != "198.51.100.7" {
		t.Fatalf("clientIP fallback = %q", got)
	}
}

func TestApplyRateLimit(t *testing.T) {
	l := newFixedWindowLimiter(1, time.Minute)
	r := &http.Request{RemoteAddr: " 192.0.2.20:1234 ", Header: http.Header{}}
	if !applyRateLimit(l, r) {
		t.Fatal("first request should pass")
	}
	if applyRateLimit(l, r) {
		t.Fatal("second request should be limited")
	}
}
