// Package app wires the incus-fleet services and HTTP server together.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/assets"
	"github.com/mickeyzzc/incus-fleet/internal/auth"
	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/httpapi"
	"github.com/mickeyzzc/incus-fleet/internal/instances"
	"github.com/mickeyzzc/incus-fleet/internal/migrations"
	"github.com/mickeyzzc/incus-fleet/internal/nodeaccess"
	"github.com/mickeyzzc/incus-fleet/internal/nodejoin"
	"github.com/mickeyzzc/incus-fleet/internal/nodenet"
	"github.com/mickeyzzc/incus-fleet/internal/store"
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
	config   *config.Config
	logger   *slog.Logger
	store    *store.SQLStore
	clusters *clusters.Manager
	server   *http.Server
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

	st, err := store.Open(ctx, cfg.Database.Driver, cfg.Database.DSN)
	if err != nil {
		return nil, err
	}

	if err := st.MarkInterrupted(ctx); err != nil {
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
	migrationsService := migrations.New(manager, st, logger)
	assetsService := assets.New(st, instancesService)
	localNodeNet := nodenet.NewService(nodenet.SudoRunner{})
	nodeOptions := []nodeaccess.Option{}
	if cfg.LocalMember != "" {
		nodeOptions = append(nodeOptions, nodeaccess.WithLocalMember(cfg.LocalMember))
	}
	nodeAccess := nodeaccess.New(manager, localNodeNet, cfg.NodeAccess, nodeOptions...)
	nodeJoin := nodejoin.New(manager, cfg.NodeAccess, logger)
	authenticator := auth.New(cfg.Server.AuthToken, 12*time.Hour)

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
		Clusters:   manager,
		Instances:  instancesService,
		Assets:     assetsService,
		Migrations: migrationsService,
		Store:      st,
		NodeNet:    nodeAccess,
		NodeJoin:   nodeJoin,
		Auth:       authenticator,
		StaticFS:   o.staticFS,
		Logger:     logger,
		Version:    version.String(),
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
		config:   cfg,
		logger:   logger,
		store:    st,
		clusters: manager,
		server:   server,
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

	errCh := make(chan error, 1)
	go func() {
		errCh <- a.server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := a.server.Shutdown(shutdownCtx); err != nil {
			a.logger.Warn("http shutdown failed", "error", err)
		}
		serveErr := <-errCh
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return fmt.Errorf("app: serve: %w", serveErr)
		}

		return nil
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("app: serve: %w", err)
		}

		return nil
	}
}

// Close releases database and cluster resources.
func (a *App) Close() error {
	a.clusters.Close()

	return a.store.Close()
}
