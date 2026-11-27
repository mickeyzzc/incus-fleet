package app

import (
	"context"
	"crypto/rand"
	"log/slog"
	"strings"

	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/plugin"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

// pluginWebhookSecret resolves the independent secret used to sign
// fleet-to-plugin webhooks. The inbound events secret is preferred, followed
// by a dedicated outbound file, then the legacy signing-key derivation. When
// nothing is configured a random per-process secret is generated and logged;
// no webhook is ever signed with an empty or predictable secret.
func pluginWebhookSecret(cfg config.PluginsConfig, signingKey []byte, logger *slog.Logger) []byte {
	paths := []string{cfg.Events.WebhookSecretFile, cfg.Events.SecretFile}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		data, err := readSecret("", path)
		if err != nil {
			logger.Warn("plugin webhook secret file unavailable", "path", path, "error", err)
			continue
		}
		if value := strings.TrimSpace(data); value != "" {
			return []byte(value)
		}
	}
	if len(signingKey) > 0 {
		logger.Warn("plugin webhook secret derived from the signing key; configure plugins.events.secret_file or webhook_secret_file")
		return []byte(plugin.SignPayload(signingKey, []byte("webhook:default")))
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		logger.Error("plugin webhook secret entropy failure", "error", err)
		return []byte("insecure-ephemeral-webhook-secret")
	}
	logger.Warn("plugin webhook secret is not configured; using a random per-process secret (set plugins.events.secret_file)")

	return secret
}

// restoreInstalledPlugins registers descriptors and webhook subscribers for
// plugins installed through the API, including community packages. Without
// this, a restart would skip their persisted wiring rows and leave their
// wiring endpoints at 404.
func restoreInstalledPlugins(
	ctx context.Context,
	st *store.SQLStore,
	registry *plugin.Registry,
	cfg config.PluginsConfig,
	signingKey, webhookSecret []byte,
	logger *slog.Logger,
) error {
	if !cfg.Enabled || st == nil || registry == nil {
		return nil
	}
	installed, err := st.ListInstalledPlugins(ctx)
	if err != nil {
		return err
	}
	for _, entry := range installed {
		m, ok := installedManifest(cfg.Dir, entry.ID, signingKey, logger)
		if !ok {
			continue
		}
		if _, exists := registry.Get(entry.ID); !exists {
			if err := registry.Register(plugin.Descriptor(*m)); err != nil {
				logger.Warn("restore plugin descriptor failed", "plugin", entry.ID, "error", err)
				continue
			}
		}
		registerRestoredWebhook(registry, entry.ID, entry.Upstream, *m, webhookSecret)
	}

	return nil
}

func installedManifest(
	dir, id string,
	signingKey []byte,
	logger *slog.Logger,
) (*plugin.Manifest, bool) {
	if m, ok := plugin.BuiltinManifest(id); ok {
		return &m, true
	}
	m, raw, err := plugin.LoadPackageManifest(dir, id)
	if err != nil || m == nil {
		logger.Warn("installed plugin manifest unavailable", "plugin", id, "error", err)
		return nil, false
	}
	if !plugin.VerifyPackageSignature(m, dir, id, raw, signingKey) {
		logger.Warn("installed community plugin failed signature verification", "plugin", id)
		return nil, false
	}
	return m, true
}

func registerRestoredWebhook(
	registry *plugin.Registry,
	id, upstream string,
	m plugin.Manifest,
	secret []byte,
) {
	if upstream == "" || len(m.Capabilities.EventWebhook.Events) == 0 ||
		plugin.SinkOwnsWebhook(registry, m) {
		return
	}
	sub, err := plugin.NewWebhookSubscriber(plugin.WebhookConfig{
		ID: id, BaseURL: upstream, Path: m.Capabilities.EventWebhook.Path,
		Events: m.Capabilities.EventWebhook.Events, Secret: string(secret),
	})
	if err != nil {
		return
	}
	_ = registry.RegisterSubscriber(id, sub)
}
