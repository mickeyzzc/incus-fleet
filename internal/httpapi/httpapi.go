// Package httpapi exposes the incus-fleet HTTP API and web UI.
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/auth"
	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/instances"
	"github.com/mickeyzzc/incus-fleet/internal/migrations"

	"github.com/lxc/incus/v7/shared/api"
)

const (
	sessionCookie = "incus_fleet_session"
	maxBodyBytes  = 1 << 20
)

// Deps are the dependencies of the HTTP API.
type Deps struct {
	Clusters   *clusters.Manager
	Instances  *instances.Service
	Migrations *migrations.Service
	Auth       *auth.Authenticator
	StaticFS   fs.FS
	Logger     *slog.Logger
	Version    string
}

// API is the complete HTTP handler for incus-fleet.
type API struct {
	clusters   *clusters.Manager
	instances  *instances.Service
	migrations *migrations.Service
	auth       *auth.Authenticator
	logger     *slog.Logger
	version    string
	static     fs.FS
}

// New builds the HTTP handler.
func New(deps Deps) *API {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	api := &API{
		clusters:   deps.Clusters,
		instances:  deps.Instances,
		migrations: deps.Migrations,
		auth:       deps.Auth,
		logger:     logger,
		version:    deps.Version,
	}

	if deps.StaticFS != nil {
		if sub, err := fs.Sub(deps.StaticFS, "dist"); err == nil {
			api.static = sub
		}
	}

	return api
}

// Handler returns the fully wrapped HTTP handler.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/healthz", a.health)
	mux.HandleFunc("GET /api/v1/version", a.versionHandler)
	mux.HandleFunc("POST /api/v1/auth/login", a.login)
	mux.HandleFunc("POST /api/v1/auth/logout", a.logout)

	mux.Handle("GET /api/v1/auth/session", a.requireAuth(http.HandlerFunc(a.session)))
	mux.Handle("GET /api/v1/clusters", a.requireAuth(http.HandlerFunc(a.listClusters)))
	mux.Handle("GET /api/v1/clusters/{cluster}", a.requireAuth(http.HandlerFunc(a.getCluster)))
	mux.Handle("POST /api/v1/clusters/{cluster}/reconnect", a.requireAuth(http.HandlerFunc(a.reconnectCluster)))
	mux.Handle("GET /api/v1/clusters/{cluster}/instances", a.requireAuth(http.HandlerFunc(a.listClusterInstances)))
	mux.Handle("GET /api/v1/clusters/{cluster}/instances/{instance}", a.requireAuth(http.HandlerFunc(a.getInstance)))
	mux.Handle("GET /api/v1/instances", a.requireAuth(http.HandlerFunc(a.listInstances)))
	mux.Handle("GET /api/v1/migrations", a.requireAuth(http.HandlerFunc(a.listMigrations)))
	mux.Handle("POST /api/v1/migrations", a.requireAuth(http.HandlerFunc(a.submitMigration)))
	mux.Handle("GET /api/v1/migrations/{id}", a.requireAuth(http.HandlerFunc(a.getMigration)))

	mux.Handle("/", a.staticHandler())

	return a.recoverer(a.securityHeaders(a.logging(mux)))
}

func (a *API) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.authenticated(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (a *API) authenticated(r *http.Request) bool {
	if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		if a.auth.ValidateToken(strings.TrimSpace(token)) {
			return true
		}
	}

	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}

	return a.auth.ValidateSession(cookie.Value)
}

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

	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	sessionID, expires, err := a.auth.Login(req.Token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_token", "invalid admin token")
		return
	}

	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is set for TLS or X-Forwarded-Proto: https
		Name:     sessionCookie,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"expires_at":    expires,
	})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		a.auth.Logout(cookie.Value)
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

func (a *API) session(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
}

func (a *API) listClusters(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"clusters": a.clusters.List()})
}

func (a *API) getCluster(w http.ResponseWriter, r *http.Request) {
	summary, err := a.clusters.Get(r.PathValue("cluster"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, summary)
}

func (a *API) reconnectCluster(w http.ResponseWriter, r *http.Request) {
	summary, err := a.clusters.Reconnect(r.Context(), r.PathValue("cluster"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, summary)
}

func (a *API) listClusterInstances(w http.ResponseWriter, r *http.Request) {
	instanceType, err := parseInstanceType(r.URL.Query().Get("type"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	list, err := a.instances.List(r.PathValue("cluster"), instanceType)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"instances": list})
}

func (a *API) listInstances(w http.ResponseWriter, r *http.Request) {
	instanceType, err := parseInstanceType(r.URL.Query().Get("type"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	list, err := a.instances.ListAll(instanceType)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"instances": list})
}

func (a *API) getInstance(w http.ResponseWriter, r *http.Request) {
	detail, err := a.instances.Get(r.PathValue("cluster"), r.PathValue("instance"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

func (a *API) listMigrations(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "limit must be a positive integer")
			return
		}
		limit = parsed
	}

	list, err := a.migrations.List(r.Context(), limit)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"migrations": list})
}

func (a *API) submitMigration(w http.ResponseWriter, r *http.Request) {
	var req migrations.Request
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	migration, err := a.migrations.Submit(r.Context(), req)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusAccepted, migration)
}

func (a *API) getMigration(w http.ResponseWriter, r *http.Request) {
	migration, err := a.migrations.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, migration)
}

func (a *API) serviceError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	code := "internal_error"

	switch {
	case errors.Is(err, migrations.ErrInvalidRequest):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, migrations.ErrConflict):
		status, code = http.StatusConflict, "conflict"
	case errors.Is(err, migrations.ErrNotFound),
		errors.Is(err, instances.ErrNotFound),
		errors.Is(err, clusters.ErrUnknownCluster):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, clusters.ErrUnavailable):
		status, code = http.StatusServiceUnavailable, "cluster_unavailable"
	}

	if status >= 500 {
		a.logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	}

	writeError(w, status, code, err.Error())
}

func (a *API) staticHandler() http.Handler {
	if a.static == nil {
		return http.NotFoundHandler()
	}

	fileServer := http.FileServer(http.FS(a.static))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if info, err := fs.Stat(a.static, path); err == nil && !info.IsDir() {
			if strings.HasPrefix(path, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		if strings.Contains(path, ".") {
			http.NotFound(w, r)
			return
		}

		index, err := fs.ReadFile(a.static, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
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

func parseInstanceType(raw string) (api.InstanceType, error) {
	switch strings.TrimSpace(raw) {
	case "", "any":
		return api.InstanceTypeAny, nil
	case "container":
		return api.InstanceTypeContainer, nil
	case "virtual-machine":
		return api.InstanceTypeVM, nil
	default:
		return "", fmt.Errorf("type must be one of any, container, virtual-machine")
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("invalid JSON body: multiple values")
	}

	return nil
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
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
