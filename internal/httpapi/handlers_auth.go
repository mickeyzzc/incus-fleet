package httpapi

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/auth"
)

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": a.version,
		"time":    time.Now().UTC(),
	})
}

func (a *API) versionHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": a.version})
}

// requestIsSecure reports whether the client reached the service over TLS,
// either directly or through a reverse proxy such as Caddy.
func requestIsSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}

	// Only trust the forwarding header from a local/private reverse proxy;
	// a client on the public internet must not be able to mark its own
	// request as secure and force a Secure cookie on a plain-HTTP session.
	if !strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)

	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	if a.rateLimit && !applyRateLimit(loginLimiter, r) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many sign-in attempts; try again later")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		// Token keeps the bootstrap path working until the first local account
		// is created.
		Token string `json:"token"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	var (
		session auth.Session
		err     error
	)
	switch {
	case strings.TrimSpace(req.Username) != "" || req.Password != "":
		session, err = a.auth.Login(r.Context(), req.Username, req.Password)
	default:
		session, err = a.auth.LoginWithToken(r.Context(), req.Token)
	}
	if err != nil {
		a.writeLoginError(w, err)
		return
	}

	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is set for TLS or X-Forwarded-Proto: https
		Name:     sessionCookie,
		Value:    session.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  session.ExpiresAt,
		MaxAge:   int(time.Until(session.ExpiresAt).Seconds()),
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"expires_at":    session.ExpiresAt,
		"username":      session.Username,
		"role":          session.Role,
	})
}

// writeLoginError maps authentication failures to client errors without
// revealing whether a username exists.
func (a *API) writeLoginError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrLocked):
		writeError(w, http.StatusTooManyRequests, "account_locked", "too many failed attempts; try again later")
	case errors.Is(err, auth.ErrUserDisabled):
		writeError(w, http.StatusForbidden, "account_disabled", "account is disabled")
	case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrInvalidToken):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
	default:
		a.logger.Error("login failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "login failed")
	}
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		a.auth.Logout(r.Context(), cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is set for TLS or X-Forwarded-Proto: https
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) session(w http.ResponseWriter, r *http.Request) {
	session, ok := sessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"username":      session.Username,
		"role":          session.Role,
		"expires_at":    session.ExpiresAt,
	})
}
