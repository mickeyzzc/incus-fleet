// Package httpapi exposes the incus-fleet HTTP API and web UI.
package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/agentinstall"
	"github.com/mickeyzzc/incus-fleet/internal/answerprofiles"
	"github.com/mickeyzzc/incus-fleet/internal/assets"
	"github.com/mickeyzzc/incus-fleet/internal/auth"
	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/console"
	"github.com/mickeyzzc/incus-fleet/internal/guestagent"
	"github.com/mickeyzzc/incus-fleet/internal/hostops"
	"github.com/mickeyzzc/incus-fleet/internal/instances"
	"github.com/mickeyzzc/incus-fleet/internal/mesh"
	"github.com/mickeyzzc/incus-fleet/internal/metricsquery"
	"github.com/mickeyzzc/incus-fleet/internal/migrations"
	"github.com/mickeyzzc/incus-fleet/internal/nodeaccess"
	"github.com/mickeyzzc/incus-fleet/internal/nodejoin"
	"github.com/mickeyzzc/incus-fleet/internal/nodenet"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/telemetry"

	"github.com/lxc/incus/v7/shared/api"
)

const (
	sessionCookie = "incus_fleet_session"
	maxBodyBytes  = 1 << 20
)

// Deps are the dependencies of the HTTP API.
type Deps struct {
	Clusters       *clusters.Manager
	Instances      *instances.Service
	Assets         *assets.Service
	AnswerProfiles *answerprofiles.Service
	Migrations     *migrations.Service
	Store          *store.SQLStore
	NodeNet        *nodeaccess.Service
	NodeJoin       *nodejoin.Service
	GuestAgent     *guestagent.Service
	Agent          AgentDispatcher
	AgentInstall   *agentinstall.Handler
	Mesh           *mesh.Mesh
	Bootstrap      BootstrapSigner
	Telemetry      *telemetry.Store
	MetricsSD      MetricsSDConfig
	MetricsQuery   *metricsquery.Proxy
	CertIssuer     CertificateIssuer
	Auth           *auth.Authenticator
	StaticFS       fs.FS
	Logger         *slog.Logger
	Version        string
	// DesktopRunner overrides the host-command backend of the graphical
	// console. Production leaves it nil, which uses the nodeaccess SSH path.
	DesktopRunner DesktopRunnerFunc
}

// DesktopRunnerFunc resolves the host-command runner for one guest's console.
// The returned close function releases the runner's resources.
type DesktopRunnerFunc func(ctx context.Context, cluster, member string) (console.HostCommand, func(), error)

// API is the complete HTTP handler for incus-fleet.
type API struct {
	clusters       *clusters.Manager
	instances      *instances.Service
	assets         *assets.Service
	answerProfiles *answerprofiles.Service
	migrations     *migrations.Service
	store          *store.SQLStore
	nodenet        *nodeaccess.Service
	nodejoin       *nodejoin.Service
	guestAgent     *guestagent.Service
	agent          AgentDispatcher
	agentInstall   *agentinstall.Handler
	mesh           *mesh.Mesh
	bootstrap      BootstrapSigner
	telemetry      *telemetry.Store
	metricsSD      MetricsSDConfig
	metricsQuery   *metricsquery.Proxy
	certIssuer     CertificateIssuer
	auth           *auth.Authenticator
	logger         *slog.Logger
	version        string
	static         fs.FS
	desktopRun     DesktopRunnerFunc
}

// New builds the HTTP handler.
func New(deps Deps) *API {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	api := &API{
		clusters:       deps.Clusters,
		instances:      deps.Instances,
		assets:         deps.Assets,
		answerProfiles: deps.AnswerProfiles,
		migrations:     deps.Migrations,
		store:          deps.Store,
		nodenet:        deps.NodeNet,
		nodejoin:       deps.NodeJoin,
		guestAgent:     deps.GuestAgent,
		agent:          deps.Agent,
		agentInstall:   deps.AgentInstall,
		mesh:           deps.Mesh,
		bootstrap:      deps.Bootstrap,
		telemetry:      deps.Telemetry,
		metricsSD:      deps.MetricsSD,
		metricsQuery:   deps.MetricsQuery,
		certIssuer:     deps.CertIssuer,
		auth:           deps.Auth,
		logger:         logger,
		version:        deps.Version,
		desktopRun:     deps.DesktopRunner,
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
	if a.agentInstall != nil {
		a.agentInstall.Register(mux)
	}
	if a.mesh != nil {
		mux.Handle("POST /internal/mesh/dispatch", a.mesh.DispatchHandler())
		mux.Handle("POST /internal/mesh/owner", a.mesh.OwnerHandler())
		mux.Handle("POST /internal/mesh/admin", a.mesh.AdminHandler())
		mux.Handle("GET /api/v1/mesh/status", a.requireAuth(http.HandlerFunc(a.meshStatus)))
		mux.Handle("POST /api/v1/mesh/force-single", a.requireWriter(http.HandlerFunc(a.meshForceSingle)))
		mux.Handle("POST /api/v1/mesh/join", a.requireWriter(http.HandlerFunc(a.meshJoin)))
		mux.Handle("POST /api/v1/mesh/promote", a.requireWriter(http.HandlerFunc(a.meshPromote)))
		mux.Handle("POST /api/v1/mesh/remove", a.requireWriter(http.HandlerFunc(a.meshRemove)))
	}
	mux.HandleFunc("POST /api/v1/auth/login", a.login)
	mux.HandleFunc("POST /api/v1/auth/logout", a.logout)
	mux.Handle("POST /api/v1/auth/password", a.requireAuth(http.HandlerFunc(a.changePassword)))
	mux.Handle("GET /api/v1/auth/sessions", a.requireAuth(http.HandlerFunc(a.listSessions)))
	mux.Handle("GET /api/v1/users", a.requireAdmin(http.HandlerFunc(a.listUsers)))
	mux.Handle("POST /api/v1/users", a.requireAuth(http.HandlerFunc(a.createUser)))
	mux.Handle("PATCH /api/v1/users/{id}", a.requireAdmin(http.HandlerFunc(a.updateUser)))
	mux.Handle("DELETE /api/v1/users/{id}", a.requireAdmin(http.HandlerFunc(a.deleteUser)))
	mux.Handle("GET /api/v1/auth/session", a.requireAuth(http.HandlerFunc(a.session)))
	mux.Handle("GET /api/v1/clusters", a.requireAuth(http.HandlerFunc(a.listClusters)))
	mux.Handle("GET /api/v1/clusters/{cluster}", a.requireAuth(http.HandlerFunc(a.getCluster)))
	mux.Handle("POST /api/v1/clusters/{cluster}/reconnect", a.requireWriter(http.HandlerFunc(a.reconnectCluster)))
	mux.Handle("GET /api/v1/clusters/{cluster}/instances", a.requireAuth(http.HandlerFunc(a.listClusterInstances)))
	mux.Handle("GET /api/v1/clusters/{cluster}/instances/{instance}", a.requireAuth(http.HandlerFunc(a.getInstance)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances", a.requireWriter(http.HandlerFunc(a.createInstance)))
	mux.Handle("PATCH /api/v1/clusters/{cluster}/instances/{instance}", a.requireWriter(http.HandlerFunc(a.updateInstance)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances/{instance}/state", a.requireWriter(http.HandlerFunc(a.instanceAction)))
	mux.Handle("GET /api/v1/clusters/{cluster}/instances/{instance}/metrics", a.requireAuth(http.HandlerFunc(a.instanceMetrics)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/instances/{instance}", a.requireWriter(http.HandlerFunc(a.deleteInstance)))
	mux.Handle("GET /api/v1/clusters/{cluster}/images", a.requireAuth(http.HandlerFunc(a.listImages)))
	mux.Handle("POST /api/v1/clusters/{cluster}/images/import", a.requireWriter(http.HandlerFunc(a.importImage)))
	mux.Handle("POST /api/v1/clusters/{cluster}/images/{fingerprint}/copy", a.requireWriter(http.HandlerFunc(a.copyImage)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/images/{fingerprint}", a.requireWriter(http.HandlerFunc(a.deleteImage)))
	mux.Handle("GET /api/v1/clusters/{cluster}/resources", a.requireAuth(http.HandlerFunc(a.getResources)))
	mux.Handle("GET /api/v1/clusters/{cluster}/traffic", a.requireAuth(http.HandlerFunc(a.clusterTraffic)))
	mux.Handle("POST /api/v1/clusters", a.requireWriter(http.HandlerFunc(a.registerCluster)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}", a.requireWriter(http.HandlerFunc(a.unregisterCluster)))
	mux.Handle("POST /api/v1/clusters/{cluster}/members", a.requireWriter(http.HandlerFunc(a.addClusterMember)))
	mux.Handle("GET /api/v1/clusters/{cluster}/members/{member}/node", a.requireAuth(http.HandlerFunc(a.nodeSnapshot)))
	mux.Handle("POST /api/v1/clusters/{cluster}/members/{member}/node/networks/{name}/state", a.requireWriter(http.HandlerFunc(a.nodeNICState)))
	mux.Handle("PATCH /api/v1/clusters/{cluster}/members/{member}/node/networks/{name}", a.requireWriter(http.HandlerFunc(a.nodeApplyAddress)))
	mux.Handle("POST /api/v1/clusters/{cluster}/members/{member}/node/networks/{name}/wifi", a.requireWriter(http.HandlerFunc(a.nodeWifiConnect)))
	mux.Handle("GET /api/v1/clusters/{cluster}/members/{member}/node/routes", a.requireAuth(http.HandlerFunc(a.nodeRoutes)))
	mux.Handle("POST /api/v1/clusters/{cluster}/members/{member}/node/routes", a.requireWriter(http.HandlerFunc(a.nodeAddRoute)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/members/{member}/node/routes/{destination}", a.requireWriter(http.HandlerFunc(a.nodeDeleteRoute)))
	mux.Handle("GET /api/v1/clusters/{cluster}/members/{member}/node/firewall", a.requireAuth(http.HandlerFunc(a.nodeFirewall)))
	mux.Handle("POST /api/v1/clusters/{cluster}/members/{member}/node/firewall", a.requireWriter(http.HandlerFunc(a.nodeAddFirewallRule)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/members/{member}/node/firewall/{handle}", a.requireWriter(http.HandlerFunc(a.nodeDeleteFirewallRule)))
	mux.Handle("GET /api/v1/audit", a.requireAuth(http.HandlerFunc(a.listHostAudit)))
	mux.Handle("GET /api/v1/audit/export", a.requireAuth(http.HandlerFunc(a.exportHostAudit)))
	mux.Handle("GET /api/v1/tasks", a.requireAuth(http.HandlerFunc(a.listHostTasks)))
	mux.Handle("POST /api/v1/tasks", a.requireWriter(http.HandlerFunc(a.enqueueHostTask)))
	mux.Handle("GET /api/v1/tasks/{id}", a.requireAuth(http.HandlerFunc(a.getHostTask)))
	mux.Handle("GET /api/v1/agents", a.requireAuth(http.HandlerFunc(a.listAgents)))
	mux.Handle("POST /api/v1/agents/enrollment-tokens", a.requireWriter(http.HandlerFunc(a.createAgentEnrollmentToken)))
	mux.Handle("GET /api/v1/agents/{id}/snapshot", a.requireAuth(http.HandlerFunc(a.agentSnapshot)))
	mux.Handle("POST /api/v1/agents/{id}/network", a.requireWriter(http.HandlerFunc(a.agentNetwork)))
	mux.Handle("POST /api/v1/agents/{id}/bootstrap", a.requireWriter(http.HandlerFunc(a.agentBootstrap)))
	mux.Handle("POST /api/v1/agents/{id}/certificate", a.requireWriter(http.HandlerFunc(a.rotateAgentCertificate)))
	mux.Handle("POST /api/v1/agents/{id}/revoke", a.requireWriter(http.HandlerFunc(a.revokeAgent)))
	mux.Handle("GET /api/v1/metrics/query", a.requireAuth(http.HandlerFunc(a.queryMetrics)))
	mux.Handle("GET /api/v1/metrics/snapshot", a.requireAuth(http.HandlerFunc(a.snapshotMetrics)))
	mux.Handle("GET /api/v1/metrics/export", a.requireAuth(http.HandlerFunc(a.exportMetrics)))
	mux.Handle("GET /api/v1/metrics/query_range", a.requireAuth(http.HandlerFunc(a.queryRange)))
	mux.Handle("GET /api/v1/sd/agents", a.requireSDToken(http.HandlerFunc(a.sdAgents)))
	mux.Handle("GET /api/v1/agents/{id}/metrics/prometheus", a.requireSDToken(http.HandlerFunc(a.agentPrometheus)))
	mux.Handle("GET /api/v1/clusters/{cluster}/instances/{instance}/snapshots", a.requireAuth(http.HandlerFunc(a.listSnapshots)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances/{instance}/snapshots", a.requireWriter(http.HandlerFunc(a.createSnapshot)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances/{instance}/snapshots/{snapshot}/restore", a.requireWriter(http.HandlerFunc(a.restoreSnapshot)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/instances/{instance}/snapshots/{snapshot}", a.requireWriter(http.HandlerFunc(a.deleteSnapshot)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances/{instance}/devices", a.requireWriter(http.HandlerFunc(a.attachDevice)))
	mux.Handle("PATCH /api/v1/clusters/{cluster}/instances/{instance}/devices/{device}", a.requireWriter(http.HandlerFunc(a.updateDevice)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/instances/{instance}/devices/{device}", a.requireWriter(http.HandlerFunc(a.detachDevice)))
	mux.Handle("GET /api/v1/clusters/{cluster}/instances/{instance}/console", a.requireAuth(http.HandlerFunc(a.instanceTerminal)))
	mux.Handle("GET /api/v1/clusters/{cluster}/instances/{instance}/vga", a.requireAuth(http.HandlerFunc(a.instanceVga)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances/{instance}/desktop/prepare", a.requireWriter(http.HandlerFunc(a.desktopPrepare)))
	mux.Handle("GET /api/v1/clusters/{cluster}/instances/{instance}/desktop/screenshot", a.requireAuth(http.HandlerFunc(a.desktopScreenshot)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances/{instance}/desktop/input", a.requireWriter(http.HandlerFunc(a.desktopInput)))
	mux.Handle("GET /api/v1/clusters/{cluster}/instances/{instance}/spice", a.requireAuth(http.HandlerFunc(a.instanceSPICE)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances/{instance}/spice/prepare", a.requireWriter(http.HandlerFunc(a.prepareSPICE)))
	mux.Handle("GET /api/v1/clusters/{cluster}/instances/{instance}/exec", a.requireAuth(http.HandlerFunc(a.instanceExec)))
	mux.Handle("GET /api/v1/clusters/{cluster}/instances/{instance}/files", a.requireAuth(http.HandlerFunc(a.instanceFiles)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances/{instance}/files", a.requireWriter(http.HandlerFunc(a.uploadInstanceFile)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/instances/{instance}/files", a.requireWriter(http.HandlerFunc(a.deleteInstanceFile)))
	mux.Handle("GET /api/v1/keys", a.requireAuth(http.HandlerFunc(a.listKeys)))
	mux.Handle("POST /api/v1/keys", a.requireWriter(http.HandlerFunc(a.createKey)))
	mux.Handle("PATCH /api/v1/keys/{id}", a.requireWriter(http.HandlerFunc(a.updateKey)))
	mux.Handle("DELETE /api/v1/keys/{id}", a.requireWriter(http.HandlerFunc(a.deleteKey)))
	mux.Handle("GET /api/v1/scripts", a.requireAuth(http.HandlerFunc(a.listScripts)))
	mux.Handle("POST /api/v1/scripts", a.requireWriter(http.HandlerFunc(a.createScript)))
	mux.Handle("PATCH /api/v1/scripts/{id}", a.requireWriter(http.HandlerFunc(a.updateScript)))
	mux.Handle("DELETE /api/v1/scripts/{id}", a.requireWriter(http.HandlerFunc(a.deleteScript)))
	mux.Handle("GET /api/v1/answer-profiles", a.requireAuth(http.HandlerFunc(a.listAnswerProfiles)))
	mux.Handle("POST /api/v1/answer-profiles", a.requireWriter(http.HandlerFunc(a.createAnswerProfile)))
	mux.Handle("PATCH /api/v1/answer-profiles/{id}", a.requireWriter(http.HandlerFunc(a.updateAnswerProfile)))
	mux.Handle("DELETE /api/v1/answer-profiles/{id}", a.requireWriter(http.HandlerFunc(a.deleteAnswerProfile)))
	mux.Handle("POST /api/v1/batch/keys", a.requireWriter(http.HandlerFunc(a.batchKeys)))
	mux.Handle("POST /api/v1/batch/scripts", a.requireWriter(http.HandlerFunc(a.batchScripts)))
	mux.Handle("POST /api/v1/batch/power", a.requireWriter(http.HandlerFunc(a.batchPower)))
	mux.Handle("GET /api/v1/clusters/{cluster}/iso", a.requireAuth(http.HandlerFunc(a.listISOs)))
	mux.Handle("POST /api/v1/clusters/{cluster}/iso", a.requireWriter(http.HandlerFunc(a.importISO)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/iso/{pool}/{name}", a.requireWriter(http.HandlerFunc(a.deleteISO)))
	mux.Handle("POST /api/v1/clusters/{cluster}/iso/{pool}/{name}/launch", a.requireWriter(http.HandlerFunc(a.launchFromISO)))
	mux.Handle("POST /api/v1/clusters/{cluster}/iso/{pool}/{name}/launch-unattended", a.requireWriter(http.HandlerFunc(a.launchUnattended)))
	mux.Handle("POST /api/v1/clusters/{cluster}/iso/{pool}/{name}/launch-unattended/preview", a.requireWriter(http.HandlerFunc(a.previewUnattendedAnswer)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances/{instance}/ops", a.requireWriter(http.HandlerFunc(a.runInstanceOps)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances/{instance}/agent/install", a.requireWriter(http.HandlerFunc(a.installGuestAgent)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances/{instance}/rebuild", a.requireWriter(http.HandlerFunc(a.rebuildInstance)))
	mux.Handle("POST /api/v1/clusters/{cluster}/images/{fingerprint}/refresh", a.requireWriter(http.HandlerFunc(a.refreshImage)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances/{instance}/clone", a.requireWriter(http.HandlerFunc(a.cloneInstance)))
	mux.Handle("POST /api/v1/clusters/{cluster}/storage-pools", a.requireWriter(http.HandlerFunc(a.createStoragePool)))
	mux.Handle("PATCH /api/v1/clusters/{cluster}/storage-pools/{pool}", a.requireWriter(http.HandlerFunc(a.updateStoragePool)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/storage-pools/{pool}", a.requireWriter(http.HandlerFunc(a.deleteStoragePool)))
	mux.Handle("GET /api/v1/clusters/{cluster}/storage-pools/{pool}/volumes", a.requireAuth(http.HandlerFunc(a.listVolumes)))
	mux.Handle("POST /api/v1/clusters/{cluster}/storage-pools/{pool}/volumes", a.requireWriter(http.HandlerFunc(a.createVolume)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/storage-pools/{pool}/volumes/{volume}", a.requireWriter(http.HandlerFunc(a.deleteVolume)))
	mux.Handle("POST /api/v1/clusters/{cluster}/networks", a.requireWriter(http.HandlerFunc(a.createNetwork)))
	mux.Handle("PATCH /api/v1/clusters/{cluster}/networks/{network}", a.requireWriter(http.HandlerFunc(a.updateNetwork)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/networks/{network}", a.requireWriter(http.HandlerFunc(a.deleteNetwork)))
	mux.Handle("PATCH /api/v1/clusters/{cluster}/members/{member}", a.requireWriter(http.HandlerFunc(a.updateMember)))
	mux.Handle("GET /api/v1/clusters/{cluster}/profiles", a.requireAuth(http.HandlerFunc(a.listProfiles)))
	mux.Handle("POST /api/v1/clusters/{cluster}/profiles", a.requireWriter(http.HandlerFunc(a.createProfile)))
	mux.Handle("PATCH /api/v1/clusters/{cluster}/profiles/{profile}", a.requireWriter(http.HandlerFunc(a.updateProfile)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/profiles/{profile}", a.requireWriter(http.HandlerFunc(a.deleteProfile)))
	mux.Handle("GET /api/v1/instances", a.requireAuth(http.HandlerFunc(a.listInstances)))
	mux.Handle("GET /api/v1/migrations", a.requireAuth(http.HandlerFunc(a.listMigrations)))
	mux.Handle("POST /api/v1/migrations", a.requireWriter(http.HandlerFunc(a.submitMigration)))
	mux.Handle("GET /api/v1/migrations/{id}", a.requireAuth(http.HandlerFunc(a.getMigration)))

	mux.Handle("/", a.staticHandler())

	return a.recoverer(a.securityHeaders(a.logging(mux)))
}

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

func (a *API) createInstance(w http.ResponseWriter, r *http.Request) {
	var req instances.CreateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	detail, err := a.instances.Create(r.Context(), r.PathValue("cluster"), req)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, detail)
}

func (a *API) updateInstance(w http.ResponseWriter, r *http.Request) {
	var req instances.UpdateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	detail, err := a.instances.Update(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), req)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

func (a *API) instanceAction(w http.ResponseWriter, r *http.Request) {
	var req instances.ActionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.Action(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteInstance(w http.ResponseWriter, r *http.Request) {
	force, err := parseForce(r.URL.Query().Get("force"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	// volumes=true also removes the instance's attached custom volumes;
	// shared ISO installer assets are kept.
	volumes, err := parseForce(r.URL.Query().Get("volumes"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.Delete(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), force, volumes); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listSnapshots(w http.ResponseWriter, r *http.Request) {
	snapshots, err := a.instances.ListSnapshots(r.PathValue("cluster"), r.PathValue("instance"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"snapshots": snapshots})
}

func (a *API) createSnapshot(w http.ResponseWriter, r *http.Request) {
	var req instances.SnapshotRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.CreateSnapshot(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) restoreSnapshot(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.RestoreSnapshot(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), r.PathValue("snapshot")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteSnapshot(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.DeleteSnapshot(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), r.PathValue("snapshot")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listVolumes(w http.ResponseWriter, r *http.Request) {
	volumes, err := a.instances.Volumes(r.PathValue("cluster"), r.PathValue("pool"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"volumes": volumes})
}

func (a *API) createVolume(w http.ResponseWriter, r *http.Request) {
	var req instances.VolumeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.CreateVolume(r.PathValue("cluster"), r.PathValue("pool"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (a *API) deleteVolume(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.DeleteVolume(r.PathValue("cluster"), r.PathValue("pool"), r.PathValue("volume")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) attachDevice(w http.ResponseWriter, r *http.Request) {
	var req instances.DeviceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	detail, err := a.instances.AttachDevice(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), req)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

func (a *API) detachDevice(w http.ResponseWriter, r *http.Request) {
	detail, err := a.instances.DetachDevice(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), r.PathValue("device"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

// updateDevice applies one device edit and returns the refreshed detail.
func (a *API) updateDevice(w http.ResponseWriter, r *http.Request) {
	var req instances.DeviceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	detail, err := a.instances.UpdateDevice(
		r.Context(), r.PathValue("cluster"), r.PathValue("instance"), r.PathValue("device"), req)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

func (a *API) importImage(w http.ResponseWriter, r *http.Request) {
	// Image uploads can be large; the body is bounded well above any
	// realistic image while the multipart parser spills to temp files.
	extendUploadDeadline(w)
	r.Body = http.MaxBytesReader(w, r.Body, 8<<30)
	if err := r.ParseMultipartForm(8 << 20); err != nil { //nolint:gosec // body bounded via MaxBytesReader
		writeError(w, http.StatusBadRequest, "invalid_request", "multipart form required")
		return
	}

	metaFile, header, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "image file required")
		return
	}
	defer metaFile.Close() //nolint:errcheck // upload cleanup

	var rootfs io.Reader
	if rootfsFile, _, rootfsErr := r.FormFile("rootfs"); rootfsErr == nil {
		defer rootfsFile.Close() //nolint:errcheck // upload cleanup
		rootfs = rootfsFile
	}

	query := r.URL.Query()
	fingerprint, err := a.instances.ImportImage(
		r.Context(),
		r.PathValue("cluster"),
		header.Filename,
		query.Get("type"),
		query.Get("alias"),
		metaFile,
		rootfs,
	)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	if fingerprint == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"fingerprint": fingerprint})
}

func (a *API) copyImage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TargetCluster string `json:"target_cluster"`
		Alias         string `json:"alias"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.CopyImageTo(r.Context(), r.PathValue("cluster"), r.PathValue("fingerprint"), req.TargetCluster, req.Alias); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteImage(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.DeleteImage(r.Context(), r.PathValue("cluster"), r.PathValue("fingerprint")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listImages(w http.ResponseWriter, r *http.Request) {
	images, err := a.instances.Images(r.PathValue("cluster"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"images": images})
}

func (a *API) getResources(w http.ResponseWriter, r *http.Request) {
	resources, err := a.instances.Resources(r.PathValue("cluster"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	// Bond slaves cannot serve as macvlan parents while the bond owns them;
	// hide them from the candidate lists. Unmanaged NICs often carry no
	// locations in the Incus listing, so the per-member link inventory also
	// attributes them to the members that expose them (the UI groups
	// physical NICs per member). Node access may be unavailable, in which
	// case the unfiltered list is served as before.
	members, memberErr := a.instances.MemberNames(r.PathValue("cluster"))
	if memberErr != nil {
		members = nil
	}
	resources.Networks = enrichNetworks(resources.Networks, members, a.linkInventoryLookup(r.Context(), r.PathValue("cluster")))

	writeJSON(w, http.StatusOK, resources)
}

// linkInventoryLookup builds a member→interface-master-map resolver over
// node access. Members whose lookup fails (SSH unconfigured, node offline)
// contribute no map; the caller keeps their NICs visible.
func (a *API) linkInventoryLookup(ctx context.Context, cluster string) func(member string) (map[string]string, error) {
	return func(member string) (map[string]string, error) {
		runner, closeNode, err := a.nodenet.HostRunner(ctx, cluster, member)
		if err != nil {
			return nil, err
		}
		defer closeNode()

		return nodenet.LinkMasters(ctx, runner)
	}
}

// enrichNetworks drops bond slaves from the unmanaged NIC list and fills in
// missing locations from the per-member link inventories — a NIC's own
// locations win when Incus reports them; otherwise every member whose link
// table contains the interface is recorded. Managed networks are never
// touched, and a failing lookup degrades to keeping the entry unlocated.
func enrichNetworks(networks []instances.Network, members []string, lookup func(member string) (map[string]string, error)) []instances.Network {
	inventories := make(map[string]map[string]string, len(members))
	for _, member := range members {
		links, err := lookup(member)
		if err != nil || links == nil {
			continue
		}
		inventories[member] = links
	}

	out := make([]instances.Network, 0, len(networks))
	for _, network := range networks {
		if network.Managed {
			out = append(out, network)
			continue
		}

		located := network.Locations
		if len(located) == 0 {
			for _, member := range members {
				if _, seen := inventories[member][network.Name]; seen {
					located = append(located, member)
				}
			}
		}

		slave := false
		for _, member := range located {
			if inventories[member][network.Name] != "" {
				slave = true
				break
			}
		}
		if slave {
			continue
		}

		network.Locations = located
		out = append(out, network)
	}

	return out
}

func (a *API) createStoragePool(w http.ResponseWriter, r *http.Request) {
	var req instances.StoragePoolRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.CreateStoragePool(r.PathValue("cluster"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (a *API) updateStoragePool(w http.ResponseWriter, r *http.Request) {
	var req instances.StoragePoolRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.UpdateStoragePool(r.PathValue("cluster"), r.PathValue("pool"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteStoragePool(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.DeleteStoragePool(r.PathValue("cluster"), r.PathValue("pool")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) createNetwork(w http.ResponseWriter, r *http.Request) {
	var req instances.NetworkRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.CreateNetwork(r.PathValue("cluster"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (a *API) updateNetwork(w http.ResponseWriter, r *http.Request) {
	var req instances.NetworkRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.UpdateNetwork(r.PathValue("cluster"), r.PathValue("network"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteNetwork(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.DeleteNetwork(r.PathValue("cluster"), r.PathValue("network")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listProfiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := a.instances.Profiles(r.PathValue("cluster"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
}

func (a *API) createProfile(w http.ResponseWriter, r *http.Request) {
	var req instances.ProfileRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.CreateProfile(r.PathValue("cluster"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (a *API) updateProfile(w http.ResponseWriter, r *http.Request) {
	var req instances.ProfileRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.UpdateProfile(r.PathValue("cluster"), r.PathValue("profile"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteProfile(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.DeleteProfile(r.PathValue("cluster"), r.PathValue("profile")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) updateMember(w http.ResponseWriter, r *http.Request) {
	var req instances.MemberUpdateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.UpdateMember(r.PathValue("cluster"), r.PathValue("member"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
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
	case errors.Is(err, migrations.ErrInvalidRequest),
		errors.Is(err, instances.ErrInvalidRequest),
		errors.Is(err, nodejoin.ErrInvalidRequest),
		errors.Is(err, guestagent.ErrInvalidRequest),
		errors.Is(err, nodenet.ErrInvalidRequest):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, migrations.ErrConflict),
		errors.Is(err, nodejoin.ErrConflict):
		status, code = http.StatusConflict, "conflict"
	case errors.Is(err, migrations.ErrNotFound),
		errors.Is(err, guestagent.ErrNotFound),
		errors.Is(err, instances.ErrNotFound),
		errors.Is(err, store.ErrNotFound),
		errors.Is(err, clusters.ErrUnknownCluster),
		errors.Is(err, nodeaccess.ErrUnknownMember):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, clusters.ErrUnavailable):
		status, code = http.StatusServiceUnavailable, "cluster_unavailable"
	case errors.Is(err, nodenet.ErrUnavailable),
		errors.Is(err, hostops.ErrSSHWriteDisabled),
		errors.Is(err, hostops.ErrAgentUnavailable):
		status, code = http.StatusServiceUnavailable, "node_unavailable"
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

func parseForce(raw string) (bool, error) {
	if strings.TrimSpace(raw) == "" {
		return false, nil
	}

	force, err := strconv.ParseBool(raw)
	if err != nil {
		return false, errors.New("force must be true or false")
	}

	return force, nil
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
