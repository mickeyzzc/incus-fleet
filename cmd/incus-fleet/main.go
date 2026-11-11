// Command incus-fleet runs the multi-cluster Incus management service.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mickeyzzc/incus-fleet/internal/app"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("incus-fleet", flag.ContinueOnError)
	flags.SetOutput(stderr)

	defaultConfig := os.Getenv("INCUS_FLEET_CONFIG")
	if defaultConfig == "" {
		defaultConfig = "/etc/incus-fleet/config.yaml"
	}

	configPath := flags.String("config", defaultConfig, "path to the YAML configuration file")
	showVersion := flags.Bool("version", false, "print version and exit")
	check := flags.Bool("check", false, "validate configuration and exit")

	if err := flags.Parse(args); err != nil {
		return 2
	}

	if *showVersion {
		_, _ = fmt.Fprintf(stdout, "incus-fleet %s (commit %s, built %s)\n", version.Version, version.Commit, version.BuildDate)
		return 0
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}

	if *check {
		_, _ = fmt.Fprintf(stdout, "configuration valid: %d cluster(s), database %s\n", len(cfg.Clusters), cfg.Database.Driver)
		return 0
	}

	logger := slog.New(slog.NewJSONHandler(stdout, &slog.HandlerOptions{Level: parseLevel(cfg.Server.LogLevel)}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	application, err := app.New(ctx, cfg, logger)
	if err != nil {
		logger.Error("failed to initialize", "error", err)
		return 1
	}
	defer func() {
		if err := application.Close(); err != nil {
			logger.Warn("shutdown cleanup failed", "error", err)
		}
	}()

	if err := application.Run(ctx); err != nil {
		logger.Error("server stopped", "error", err)
		return 1
	}

	logger.Info("server stopped cleanly")
	return 0
}

func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
