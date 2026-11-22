package httpapi

import (
	"bufio"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/auth"
)

func (a *API) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := a.authenticate(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxSessionKey{}, session)))
	})
}

// requireWriter rejects read-only roles on mutating endpoints.
func (a *API) requireWriter(next http.Handler) http.Handler {
	return a.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := sessionFromContext(r.Context())
		if !ok || !session.Role.CanWrite() {
			writeError(w, http.StatusForbidden, "forbidden", "this account is read-only")
			return
		}

		next.ServeHTTP(w, r)
	}))
}

// requireAdmin restricts an endpoint to administrators.
func (a *API) requireAdmin(next http.Handler) http.Handler {
	return a.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := sessionFromContext(r.Context())
		if !ok || !session.Role.CanAdmin() {
			writeError(w, http.StatusForbidden, "forbidden", "administrator role required")
			return
		}

		next.ServeHTTP(w, r)
	}))
}

// authenticate resolves the caller identity. It accepts the bootstrap bearer
// token (only while no local account exists) and the session cookie.
func (a *API) authenticate(r *http.Request) (auth.Session, bool) {
	if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		if a.auth.ValidateToken(strings.TrimSpace(token)) && a.auth.BootstrapAllowed(r.Context()) {
			return auth.Session{UserID: "bootstrap", Username: "bootstrap-admin", Role: auth.RoleAdmin}, true
		}
	}

	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return auth.Session{}, false
	}

	return a.auth.Session(r.Context(), cookie.Value)
}

// ctxSessionKey carries the authenticated session for authorization checks.
type ctxSessionKey struct{}

// sessionFromContext returns the session attached by requireAuth.
func sessionFromContext(ctx context.Context) (auth.Session, bool) {
	session, ok := ctx.Value(ctxSessionKey{}).(auth.Session)
	return session, ok
}

func (a *API) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		level := slog.LevelDebug
		if r.Method != http.MethodGet || rec.status >= 400 {
			level = slog.LevelInfo
		}
		a.logger.Log(r.Context(), level, "http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration", time.Since(start).Round(time.Millisecond),
		)
	})
}

func (a *API) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self' data:; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (a *API) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				a.logger.Error("panic serving request", "path", r.URL.Path, "panic", recovered)
				writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
			}
		}()

		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

// Unwrap exposes the wrapped writer so http.ResponseController can reach the
// connection to adjust deadlines. Without it every SetReadDeadline /
// SetWriteDeadline call through this wrapper fails with ErrNotSupported, which
// silently capped long uploads and node joins at the global server timeout.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// Hijack lets websocket upgrades pass through the logging wrapper.
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("statusRecorder: response writer does not support hijacking")
	}

	r.status = http.StatusSwitchingProtocols

	return hijacker.Hijack()
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	n, err := r.ResponseWriter.Write(data)
	r.bytes += n

	return n, err
}
