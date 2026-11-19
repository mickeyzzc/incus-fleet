// Package app wires the incus-fleet services and HTTP server together.
package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/agentgw"
	"github.com/mickeyzzc/incus-fleet/internal/agentinstall"
	"github.com/mickeyzzc/incus-fleet/internal/agentops"
	"github.com/mickeyzzc/incus-fleet/internal/agentpb"
	"github.com/mickeyzzc/incus-fleet/internal/assets"
	"github.com/mickeyzzc/incus-fleet/internal/auth"
	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/httpapi"
	"github.com/mickeyzzc/incus-fleet/internal/instances"
	"github.com/mickeyzzc/incus-fleet/internal/mesh"
	"github.com/mickeyzzc/incus-fleet/internal/metricsquery"
	"github.com/mickeyzzc/incus-fleet/internal/migrations"
	"github.com/mickeyzzc/incus-fleet/internal/nodeaccess"
	"github.com/mickeyzzc/incus-fleet/internal/nodejoin"
	"github.com/mickeyzzc/incus-fleet/internal/nodenet"
	"github.com/mickeyzzc/incus-fleet/internal/remotewrite"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/taskrunner"
	"github.com/mickeyzzc/incus-fleet/internal/telemetry"
	"github.com/mickeyzzc/incus-fleet/internal/version"
	"github.com/mickeyzzc/incus-fleet/web"
)

// Option customizes application construction.
type Option func(*options)

type options struct {
	staticFS fs.FS
	dialer   clusters.DialFunc
}

// WithStaticFS overrides the embedded frontend filesystem.
func WithStaticFS(fsys fs.FS) Option {
	return func(o *options) { o.staticFS = fsys }
}

// WithDialer overrides the Incus connection dialer.
func WithDialer(dialer clusters.DialFunc) Option {
	return func(o *options) { o.dialer = dialer }
}

// App is one running incus-fleet instance.
type App struct {
	config       *config.Config
	logger       *slog.Logger
	store        *store.SQLStore
	clusters     *clusters.Manager
	server       *http.Server
	agentGW      *agentgw.Gateway
	agentTLS     *tls.Config
	agentServer  *http.Server
	remoteWrite  *remotewrite.Writer
	metricsQuery *metricsquery.Proxy
	taskRunner   *taskrunner.Runner
	mesh         *mesh.Mesh
}

// agentDispatch is the transport used by both async tasks and sync node ops.
const meshOwnerTTL = 60 * time.Second

type agentDispatch interface {
	Dispatch(ctx context.Context, agentID string, typ agentpb.TaskType, payload []byte, timeout time.Duration) (*agentpb.TaskResult, error)
}

// New builds all services and the HTTP handler.
func New(ctx context.Context, cfg *config.Config, logger *slog.Logger, opts ...Option) (*App, error) {
	if cfg == nil {
		return nil, errors.New("app: config is required")
	}
	if logger == nil {
		logger = slog.Default()
	}

	o := options{staticFS: web.Files}
	for _, opt := range opts {
		opt(&o)
	}

	// instanceOwner identifies this fleet instance across restarts. It must be
	// stable (hostname, or the configured HA worker id) so that a restart can
	// recognise its own interrupted work. A pid would change on every restart.
	instanceOwner := strings.TrimSpace(cfg.HA.WorkerID)
	if instanceOwner == "" {
		hostname, err := os.Hostname()
		if err != nil || strings.TrimSpace(hostname) == "" {
			hostname = "center"
		}
		instanceOwner = hostname
	}

	st, err := store.Open(ctx, cfg.Database.Driver, cfg.Database.DSN)
	if err != nil {
		return nil, err
	}

	if err := st.MarkInterrupted(ctx, instanceOwner); err != nil {
		_ = st.Close()
		return nil, err
	}

	var manager *clusters.Manager
	if o.dialer != nil {
		manager, err = clusters.NewWithDialer(ctx, cfg.Clusters, o.dialer, logger)
	} else {
		manager, err = clusters.New(ctx, cfg.Clusters, logger)
	}
	if err != nil {
		_ = st.Close()
		return nil, err
	}

	instancesService := instances.New(manager)
	migrationsService := migrations.New(manager, st, logger, migrations.WithOwner(instanceOwner))
	assetsService := assets.New(st, instancesService)
	localNodeNet := nodenet.NewService(nodenet.SudoRunner{})
	nodeOptions := []nodeaccess.Option{}
	if cfg.LocalMember != "" {
		nodeOptions = append(nodeOptions, nodeaccess.WithLocalMember(cfg.LocalMember))
	}
	nodeAccess := nodeaccess.New(manager, localNodeNet, cfg.NodeAccess, nodeOptions...)
	nodeJoin := nodejoin.New(manager, cfg.NodeAccess, logger)
	var authenticator *auth.Authenticator
	// Sessions live in the database whenever more than one instance can share
	// state: a mesh deployment, rqlite, or PostgreSQL. Only a single-node
	// SQLite deployment keeps them in process memory.
	if cfg.Mesh.Enabled || cfg.Database.IsShared() {
		authenticator = auth.NewWithStore(cfg.Server.AuthToken, 12*time.Hour, st)
	} else {
		authenticator = auth.New(cfg.Server.AuthToken, 12*time.Hour)
	}

	var (
		agentGW  *agentgw.Gateway
		agentTLS *tls.Config
	)
	remoteToken, err := readSecret(cfg.Metrics.RemoteWrite.BearerToken, cfg.Metrics.RemoteWrite.BearerTokenFile)
	if err != nil {
		manager.Close()
		_ = st.Close()
		return nil, err
	}
	queryToken, err := readSecret(cfg.Metrics.Query.BearerToken, cfg.Metrics.Query.BearerTokenFile)
	if err != nil {
		manager.Close()
		_ = st.Close()
		return nil, err
	}

	var telemetryStore *telemetry.Store
	var remoteWriter *remotewrite.Writer
	if cfg.Metrics.Enabled {
		telemetryStore = telemetry.New(telemetry.DefaultMaxSeries)
		if cfg.Metrics.RemoteWrite.Enabled {
			spoolDir := strings.TrimSpace(cfg.Metrics.RemoteWrite.SpoolDir)
			if spoolDir == "" {
				spoolDir = filepath.Join(os.TempDir(), "incus-fleet-remote-write")
			}
			partition := strings.TrimSpace(cfg.Metrics.RemoteWrite.SpoolPartition)
			if partition == "" {
				partition = strings.TrimSpace(cfg.HA.WorkerID)
			}
			if partition == "" {
				partition = defaultSpoolPartition()
			}
			remoteWriter, err = remotewrite.New(remotewrite.Config{
				URL:            cfg.Metrics.RemoteWrite.URL,
				BearerToken:    remoteToken,
				Timeout:        parseDuration(cfg.Metrics.RemoteWrite.Timeout, 10*time.Second),
				QueueSize:      cfg.Metrics.RemoteWrite.QueueSize,
				BatchSize:      cfg.Metrics.RemoteWrite.BatchSize,
				SpoolDir:       spoolDir,
				SpoolPartition: partition,
				MaxBytes:       cfg.Metrics.RemoteWrite.MaxBytes,
			})
			if err != nil {
				manager.Close()
				_ = st.Close()
				return nil, err
			}
			remoteWriter.Start(ctx)
			telemetryStore.SetSink(func(series []telemetry.Series) { remoteWriter.Enqueue(series) })
		}
	}
	var queryProxy *metricsquery.Proxy
	if cfg.Metrics.Query.Enabled {
		queryProxy = metricsquery.New(metricsquery.Config{
			URL: cfg.Metrics.Query.URL, Type: cfg.Metrics.Query.Type,
			BearerToken: queryToken,
			Timeout:     parseDuration(cfg.Metrics.Query.Timeout, 15*time.Second),
		})
	}
	var agentInstall *agentinstall.Handler
	var centerMesh *mesh.Mesh
	var taskRunner *taskrunner.Runner
	var agentDispatcher agentDispatch
	if cfg.Mesh.Enabled {
		peers := make([]mesh.Peer, 0, len(cfg.Mesh.Peers))
		for _, peer := range cfg.Mesh.Peers {
			peers = append(peers, mesh.Peer{
				ID: peer.ID, Address: peer.Address, ControlAddress: peer.ControlAddress,
			})
		}
		if cfg.Mesh.ForceSingle {
			peers = []mesh.Peer{{
				ID: cfg.Mesh.NodeID, Address: cfg.Mesh.Address, ControlAddress: cfg.Mesh.ControlAddress,
			}}
		}
		centerMesh, err = mesh.New(mesh.Config{
			Enabled: cfg.Mesh.Enabled, NodeID: cfg.Mesh.NodeID, Address: cfg.Mesh.Address,
			ControlAddress: cfg.Mesh.ControlAddress, DataDir: cfg.Mesh.DataDir,
			Peers: peers, Witness: cfg.Mesh.Witness, Secret: cfg.Mesh.Secret,
		}, logger)
		if err != nil {
			manager.Close()
			_ = st.Close()
			return nil, err
		}
		if err := centerMesh.Start(); err != nil {
			_ = centerMesh.Close()
			manager.Close()
			_ = st.Close()
			return nil, err
		}
	}

	if cfg.HA.Enabled && (centerMesh == nil || !cfg.Mesh.Witness) {
		worker := strings.TrimSpace(cfg.HA.WorkerID)
		if worker == "" {
			hostname, _ := os.Hostname()
			worker = fmt.Sprintf("%s-%d", hostname, os.Getpid())
		}
		taskRunner = taskrunner.New(st, worker,
			parseDuration(cfg.HA.Lease, 30*time.Second),
			parseDuration(cfg.HA.Interval, 2*time.Second), logger)
	}

	if cfg.Agent.Enabled {
		ca, err := agentgw.LoadCA(cfg.Agent.ClientCA, cfg.Agent.CAKey)
		if err != nil {
			manager.Close()
			_ = st.Close()
			return nil, err
		}
		serverCert, err := tls.LoadX509KeyPair(cfg.Agent.TLSCert, cfg.Agent.TLSKey)
		if err != nil {
			manager.Close()
			_ = st.Close()
			return nil, fmt.Errorf("app: load agent server certificate: %w", err)
		}
		options := []agentgw.Option{}
		if telemetryStore != nil {
			options = append(options, agentgw.WithTelemetry(telemetryStore))
		}
		if centerMesh != nil {
			options = append(options,
				agentgw.WithConnectHook(func(agentID string) {
					if err := centerMesh.SetOwnerAny(agentID, cfg.Mesh.NodeID, meshOwnerTTL); err != nil {
						logger.Warn("mesh: record agent owner failed", "agent", agentID, "error", err)
					}
				}),
				agentgw.WithDisconnectHook(func(agentID string) {
					if err := centerMesh.ClearOwnerAny(agentID); err != nil {
						logger.Warn("mesh: clear agent owner failed", "agent", agentID, "error", err)
					}
				}),
				agentgw.WithHeartbeatHook(func(agentID string) {
					if err := centerMesh.SetOwnerAny(agentID, cfg.Mesh.NodeID, meshOwnerTTL); err != nil {
						logger.Warn("mesh: refresh agent owner failed", "agent", agentID, "error", err)
					}
				}),
			)
		}
		agentGW = agentgw.New(st, ca, logger, options...)
		agentTLS = agentGW.TLSConfig(serverCert)
		agentDispatcher = agentGW
		if centerMesh != nil {
			centerMesh.SetDispatcher(agentDispatcher)
			agentDispatcher = centerMesh
		}
		if cfg.Agent.Install.Enabled {
			agentInstall, err = agentinstall.New(agentinstall.Config{
				GatewayURL:     cfg.Agent.Install.GatewayURL,
				AssetURL:       cfg.Agent.Install.AssetURL,
				AgentBinary:    cfg.Agent.Install.AgentBinary,
				HelperBinary:   cfg.Agent.Install.HelperBinary,
				AgentBinaries:  cfg.Agent.Install.AgentBinaries,
				HelperBinaries: cfg.Agent.Install.HelperBinaries,
			}, ca.CertPEM(), logger)
			if err != nil {
				manager.Close()
				_ = st.Close()
				return nil, err
			}
		}
		nodeAccess.SetAgentResolver(agentops.New(agentDispatcher, st).Resolve)
		if taskRunner != nil {
			taskRunner.Register(taskrunner.AgentNetworkTask, taskrunner.AgentNetworkHandler(agentDispatcher, st))
			taskRunner.Register(taskrunner.AgentBootstrapTask, taskrunner.AgentBootstrapHandler(agentDispatcher, st))
			taskRunner.Register(taskrunner.AgentSnapshotTask, taskrunner.AgentSnapshotHandler(agentDispatcher, st))
		}
	}

	// Clusters registered at runtime through the web UI persist in the
	// store and rejoin the manager on every startup.
	registered, err := st.ListClusters(ctx)
	if err != nil {
		logger.Warn("registered clusters unavailable", "error", err)
	}
	for _, cluster := range registered {
		if _, exists := manager.Get(cluster.Name); exists == nil {
			continue
		}
		if _, err := manager.Register(ctx, config.Cluster{
			Name:          cluster.Name,
			DisplayName:   cluster.DisplayName,
			Endpoint:      cluster.Endpoint,
			Project:       cluster.Project,
			TLSCert:       cluster.TLSCert,
			TLSKey:        cluster.TLSKey,
			TLSServerCert: cluster.TLSServerCert,
		}); err != nil {
			logger.Warn("registered cluster reconnect failed", "cluster", cluster.Name, "error", err)
		}
	}

	handler := httpapi.New(httpapi.Deps{
		Clusters:     manager,
		Instances:    instancesService,
		Assets:       assetsService,
		Migrations:   migrationsService,
		Store:        st,
		NodeNet:      nodeAccess,
		NodeJoin:     nodeJoin,
		Agent:        agentGW,
		AgentInstall: agentInstall,
		Mesh:         centerMesh,
		Bootstrap:    agentGW,
		CertIssuer:   agentGW,
		Telemetry:    telemetryStore,
		MetricsQuery: queryProxy,
		MetricsSD: httpapi.MetricsSDConfig{
			Enabled: cfg.Metrics.HTTPSD.Enabled, Token: cfg.Metrics.HTTPSD.Token,
			PublicURL: cfg.Metrics.HTTPSD.PublicURL, Mode: cfg.Metrics.HTTPSD.Mode,
		},
		Auth:     authenticator,
		StaticFS: o.staticFS,
		Logger:   logger,
		Version:  version.String(),
	}).Handler()

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	online := 0
	for _, summary := range manager.List() {
		if summary.Online {
			online++
		}
	}
	logger.Info("incus-fleet initialized", "clusters", len(cfg.Clusters), "online", online, "database", cfg.Database.Driver)

	return &App{
		config:       cfg,
		logger:       logger,
		store:        st,
		clusters:     manager,
		server:       server,
		agentGW:      agentGW,
		agentTLS:     agentTLS,
		remoteWrite:  remoteWriter,
		metricsQuery: queryProxy,
		taskRunner:   taskRunner,
		mesh:         centerMesh,
	}, nil
}

// Handler returns the application HTTP handler.
func (a *App) Handler() http.Handler {
	return a.server.Handler
}

// Run starts the HTTP server and blocks until the context is cancelled.
func (a *App) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", a.config.Server.Listen)
	if err != nil {
		return fmt.Errorf("app: listen: %w", err)
	}

	a.logger.Info("http server listening", "address", listener.Addr().String())

	if a.taskRunner != nil {
		go a.taskRunner.Run(ctx)
		a.logger.Info("host task dispatcher running", "worker", a.taskRunner.Worker())
	}

	serverCount := 1
	errCh := make(chan error, 2)
	go func() {
		errCh <- a.server.Serve(listener)
	}()

	if a.agentGW != nil && a.agentTLS != nil {
		agentListener, err := tls.Listen("tcp", a.config.Agent.Listen, a.agentTLS)
		if err != nil {
			_ = listener.Close()
			return fmt.Errorf("app: listen agent gateway: %w", err)
		}
		a.agentServer = &http.Server{
			Handler:           a.agentGW.Handler(),
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       60 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       2 * time.Minute,
		}
		serverCount = 2
		a.logger.Info("agent gateway listening", "address", agentListener.Addr().String())
		go func() {
			errCh <- a.agentServer.Serve(agentListener)
		}()
	}

	shutdown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := a.server.Shutdown(shutdownCtx); err != nil {
			a.logger.Warn("http shutdown failed", "error", err)
		}
		if a.agentServer != nil {
			if err := a.agentServer.Shutdown(shutdownCtx); err != nil {
				a.logger.Warn("agent gateway shutdown failed", "error", err)
			}
		}
	}

	select {
	case <-ctx.Done():
		shutdown()
		for i := 0; i < serverCount; i++ {
			serveErr := <-errCh
			if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
				return fmt.Errorf("app: serve: %w", serveErr)
			}
		}

		return nil
	case serveErr := <-errCh:
		shutdown()
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return fmt.Errorf("app: serve: %w", serveErr)
		}

		return nil
	}
}

// Close releases database and cluster resources.
func (a *App) Close() error {
	if a.remoteWrite != nil {
		a.remoteWrite.Close()
	}
	if a.mesh != nil {
		_ = a.mesh.Close()
	}
	a.clusters.Close()

	return a.store.Close()
}

func defaultSpoolPartition() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "center"
	}
	return fmt.Sprintf("%s-%d", hostname, os.Getpid())
}

func readSecret(value, file string) (string, error) {
	if strings.TrimSpace(file) == "" {
		return strings.TrimSpace(value), nil
	}
	data, err := os.ReadFile(file) //nolint:gosec // operator-configured token file
	if err != nil {
		return "", fmt.Errorf("app: read token file: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func parseDuration(value string, fallback time.Duration) time.Duration {
	if parsed, err := time.ParseDuration(value); err == nil && parsed > 0 {
		return parsed
	}
	return fallback
}
