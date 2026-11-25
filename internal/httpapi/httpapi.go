// Package httpapi exposes the incus-fleet HTTP API and web UI.
package httpapi

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/mickeyzzc/incus-fleet/internal/agentinstall"
	"github.com/mickeyzzc/incus-fleet/internal/answerprofiles"
	"github.com/mickeyzzc/incus-fleet/internal/assets"
	"github.com/mickeyzzc/incus-fleet/internal/auth"
	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/console"
	"github.com/mickeyzzc/incus-fleet/internal/guestagent"
	"github.com/mickeyzzc/incus-fleet/internal/instances"
	"github.com/mickeyzzc/incus-fleet/internal/mesh"
	"github.com/mickeyzzc/incus-fleet/internal/metricsquery"
	"github.com/mickeyzzc/incus-fleet/internal/migrations"
	"github.com/mickeyzzc/incus-fleet/internal/nodeaccess"
	"github.com/mickeyzzc/incus-fleet/internal/nodejoin"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/telemetry"
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
	Overlay        OverlayService
	Agent          AgentDispatcher
	AgentInstall   *agentinstall.Handler
	Mesh           *mesh.Mesh
	Bootstrap      BootstrapSigner
	Telemetry      *telemetry.Store
	MetricsSD      MetricsSDConfig
	MetricsQuery   *metricsquery.Proxy
	CertIssuer     CertificateIssuer
	Auth           *auth.Authenticator
	Plugins        config.PluginsConfig
	StaticFS       fs.FS
	Logger         *slog.Logger
	Version        string
	// TaskWorker reports whether queued host tasks can actually be picked up:
	// this instance runs a dispatcher, or a shared database lets another
	// instance's dispatcher claim them. Without it POST /tasks would accept
	// work no one will ever execute.
	TaskWorker bool
	// DesktopRunner overrides the host-command backend of the graphical
	// console. Production leaves it nil, which uses the nodeaccess SSH path.
	DesktopRunner DesktopRunnerFunc
	// RateLimit enables per-client throttling on authentication endpoints.
	RateLimit bool
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
	overlay        OverlayService
	agent          AgentDispatcher
	agentInstall   *agentinstall.Handler
	mesh           *mesh.Mesh
	bootstrap      BootstrapSigner
	telemetry      *telemetry.Store
	metricsSD      MetricsSDConfig
	metricsQuery   *metricsquery.Proxy
	certIssuer     CertificateIssuer
	auth           *auth.Authenticator
	pluginsConfig  config.PluginsConfig
	pluginsEnabled bool
	logger         *slog.Logger
	version        string
	static         fs.FS
	taskWorker     bool
	desktopRun     DesktopRunnerFunc
	rateLimit      bool
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
		overlay:        deps.Overlay,
		agent:          deps.Agent,
		agentInstall:   deps.AgentInstall,
		mesh:           deps.Mesh,
		bootstrap:      deps.Bootstrap,
		telemetry:      deps.Telemetry,
		metricsSD:      deps.MetricsSD,
		metricsQuery:   deps.MetricsQuery,
		certIssuer:     deps.CertIssuer,
		auth:           deps.Auth,
		pluginsConfig:  deps.Plugins,
		pluginsEnabled: deps.Plugins.Enabled,
		logger:         logger,
		version:        deps.Version,
		taskWorker:     deps.TaskWorker,
		desktopRun:     deps.DesktopRunner,
		rateLimit:      deps.RateLimit,
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
	mux.Handle("POST /api/v1/users/{id}/sessions/revoke", a.requireAdmin(http.HandlerFunc(a.revokeUserSessions)))
	mux.Handle("GET /api/v1/auth/session", a.requireAuth(http.HandlerFunc(a.session)))
	// Every signed-in account manages its own tokens; the handlers narrow the
	// result to the caller unless the account is an administrator. read:own-tokens
	// is implied by every role, so no one is locked out of their own credentials.
	mux.Handle("GET /api/v1/tokens", a.requireScope(auth.ScopeReadOwnTokens, http.HandlerFunc(a.listAPITokens)))
	mux.Handle("POST /api/v1/tokens", a.requireScope(auth.ScopeReadOwnTokens, http.HandlerFunc(a.createAPIToken)))
	mux.Handle("DELETE /api/v1/tokens/{id}", a.requireScope(auth.ScopeReadOwnTokens, http.HandlerFunc(a.revokeAPIToken)))
	mux.Handle("GET /api/v1/tokens/scopes", a.requireScope(auth.ScopeReadOwnTokens, http.HandlerFunc(a.availableScopes)))
	a.registerPluginRoutes(mux)
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
	mux.Handle("PATCH /api/v1/clusters/{cluster}", a.requireWriter(http.HandlerFunc(a.renameCluster)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}", a.requireWriter(http.HandlerFunc(a.unregisterCluster)))
	mux.Handle("POST /api/v1/clusters/{cluster}/members", a.requireWriter(http.HandlerFunc(a.addClusterMember)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/members/{member}", a.requireWriter(http.HandlerFunc(a.removeClusterMember)))
	mux.Handle("POST /api/v1/clusters/{cluster}/members/{member}/rename", a.requireWriter(http.HandlerFunc(a.renameClusterMember)))
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
	mux.Handle("POST /api/v1/clusters/{cluster}/instances/{instance}/rename", a.requireWriter(http.HandlerFunc(a.renameInstance)))
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
	mux.Handle("POST /api/v1/answer-profiles/preview", a.requireWriter(http.HandlerFunc(a.previewAnswerProfile)))
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
	mux.Handle("GET /api/v1/clusters/{cluster}/overlay", a.requireAuth(http.HandlerFunc(a.listOverlay)))
	mux.Handle("POST /api/v1/clusters/{cluster}/overlay", a.requireWriter(http.HandlerFunc(a.enableOverlay)))
	mux.Handle("GET /api/v1/clusters/{cluster}/overlay/{overlay}", a.requireAuth(http.HandlerFunc(a.getOverlay)))
	mux.Handle("PATCH /api/v1/clusters/{cluster}/overlay/{overlay}", a.requireWriter(http.HandlerFunc(a.updateOverlay)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/overlay/{overlay}", a.requireWriter(http.HandlerFunc(a.disableOverlay)))
	mux.Handle("POST /api/v1/clusters/{cluster}/overlay/{overlay}/reconcile", a.requireWriter(http.HandlerFunc(a.reconcileOverlay)))
	mux.Handle("PATCH /api/v1/clusters/{cluster}/members/{member}", a.requireWriter(http.HandlerFunc(a.updateMember)))
	mux.Handle("GET /api/v1/clusters/{cluster}/profiles", a.requireAuth(http.HandlerFunc(a.listProfiles)))
	mux.Handle("POST /api/v1/clusters/{cluster}/profiles", a.requireWriter(http.HandlerFunc(a.createProfile)))
	mux.Handle("PATCH /api/v1/clusters/{cluster}/profiles/{profile}", a.requireWriter(http.HandlerFunc(a.updateProfile)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/profiles/{profile}", a.requireWriter(http.HandlerFunc(a.deleteProfile)))
	mux.Handle("GET /api/v1/instances", a.requireAuth(http.HandlerFunc(a.listInstances)))
	mux.Handle("GET /api/v1/migrations", a.requireAuth(http.HandlerFunc(a.listMigrations)))
	mux.Handle("POST /api/v1/migrations", a.requireWriter(http.HandlerFunc(a.submitMigration)))
	mux.Handle("GET /api/v1/migrations/{id}", a.requireAuth(http.HandlerFunc(a.getMigration)))
	mux.Handle("POST /api/v1/migrations/{id}/cancel", a.requireWriter(http.HandlerFunc(a.cancelMigration)))
	mux.Handle("POST /api/v1/migrations/{id}/retry", a.requireWriter(http.HandlerFunc(a.retryMigration)))

	mux.Handle("/", a.staticHandler())

	return a.recoverer(a.securityHeaders(a.logging(mux)))
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
