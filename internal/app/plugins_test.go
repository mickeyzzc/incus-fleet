package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/plugin"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

const fixtureWebhookKey = "webhook-secret"

func TestPluginWebhookSecretPreference(t *testing.T) {
	dir := t.TempDir()
	inbound := filepath.Join(dir, "inbound")
	outbound := filepath.Join(dir, "outbound")
	if err := os.WriteFile(inbound, []byte("inbound-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outbound, []byte("outbound-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.PluginsConfig{Events: config.PluginEventsConfig{
		SecretFile: inbound, WebhookSecretFile: outbound,
	}}
	if got := string(pluginWebhookSecret(cfg, []byte("signing-key"), testLogger())); got != "outbound-secret" {
		t.Fatalf("dedicated secret = %q", got)
	}
	cfg.Events.WebhookSecretFile = ""
	if got := string(pluginWebhookSecret(cfg, []byte("signing-key"), testLogger())); got != "inbound-secret" {
		t.Fatalf("inbound secret = %q", got)
	}
	cfg.Events.SecretFile = filepath.Join(dir, "missing")
	if got := string(pluginWebhookSecret(cfg, []byte("signing-key"), testLogger())); got == "" {
		t.Fatal("derived secret was empty")
	}
	if got := string(pluginWebhookSecret(config.PluginsConfig{}, nil, testLogger())); len(got) != 32 {
		t.Fatalf("random secret length = %d", len(got))
	}
}

func TestRestoreInstalledPluginsSkipsInvalidPackages(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, "sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "unsigned")
	if err := os.MkdirAll(pkgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`schema: fleet-plugin/v1
id: unsigned
name: Unsigned
version: 1.0.0
publisher: community-example
tier: community
description: Unsigned package
homepage: https://example.com/plugins/unsigned
artifact:
  image:
    source: registry
    reference: example.com/plugins/test:1.0.0
    architectures: [amd64]
install:
  defaults:
    cpu: 1
    memory_mb: 512
    root_disk_gb: 10
    network: managed
  data_disk:
    filesystem: ext4
    mount_point: /data
health:
  endpoint: /health
`)
	if err := os.WriteFile(filepath.Join(pkgDir, "manifest.yaml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"vm", "unsigned", "missing"} {
		if err := st.CreateInstalledPlugin(ctx, &store.InstalledPlugin{
			ID: id, Name: id, Upstream: "http://192.0.2.20:8080", Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	registry := plugin.New(plugin.WithWiringStore(st))
	cfg := config.PluginsConfig{Enabled: true, Dir: dir}
	if err := restoreInstalledPlugins(ctx, st, registry, cfg, []byte("key"), []byte("secret"), testLogger()); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Get("vm"); !ok {
		t.Fatal("builtin descriptor was not restored")
	}
	if _, ok := registry.Get("unsigned"); ok {
		t.Fatal("unsigned community package was restored")
	}
	if _, ok := registry.Get("missing"); ok {
		t.Fatal("missing package was restored")
	}
	if err := restoreInstalledPlugins(ctx, st, registry, config.PluginsConfig{Enabled: false}, nil, nil, testLogger()); err != nil {
		t.Fatal(err)
	}
	if err := restoreInstalledPlugins(ctx, nil, registry, cfg, nil, nil, testLogger()); err != nil {
		t.Fatal(err)
	}
	registerRestoredWebhook(registry, "unsigned", "", plugin.Manifest{}, []byte("secret"))
}

func TestRestoreInstalledCommunityPlugin(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, "sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	dir := t.TempDir()
	manifest := []byte(`schema: fleet-plugin/v1
id: community-restore
name: Community Restore
version: 1.0.0
publisher: community-example
tier: community
description: Restore test package
homepage: https://example.com/plugins/community-restore
artifact:
  image:
    source: registry
    reference: example.com/plugins/test:1.0.0
    architectures: [amd64]
install:
  defaults:
    cpu: 1
    memory_mb: 512
    root_disk_gb: 10
    network: managed
  data_disk:
    filesystem: ext4
    mount_point: /data
health:
  endpoint: /health
`)
	pkgDir := filepath.Join(dir, "community-restore")
	if err := os.MkdirAll(pkgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "manifest.yaml"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	key := []byte("test-signing-key")
	if err := os.WriteFile(filepath.Join(pkgDir, "manifest.yaml.sig"),
		[]byte(plugin.SignPayload(key, manifest)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateInstalledPlugin(ctx, &store.InstalledPlugin{
		ID: "community-restore", Name: "Community Restore", Upstream: "http://192.0.2.20:8080", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertPluginWiring(ctx, &store.PluginWiring{
		PluginID: "community-restore", Key: "test.only", Value: "http://192.0.2.30",
		Enabled: true, Approved: true,
	}); err != nil {
		t.Fatal(err)
	}

	registry := plugin.New(plugin.WithWiringStore(st))
	if err := restoreInstalledPlugins(ctx, st, registry,
		config.PluginsConfig{Enabled: true, Dir: dir}, key, []byte("webhook-secret"), testLogger()); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Get("community-restore"); !ok {
		t.Fatal("community descriptor was not restored")
	}
	if err := registry.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if got, ok := registry.WiringValue("test.only"); !ok || got != "http://192.0.2.30" {
		t.Fatalf("restored wiring = %q %v", got, ok)
	}

	events := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		events <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	// Replace the restored subscriber with one pointing at the test server,
	// then publish an event and confirm the community plugin receives it.
	sub, err := plugin.NewWebhookSubscriber(plugin.WebhookConfig{
		ID: "community-restore", BaseURL: server.URL, Path: "/events",
		Events: []string{plugin.EventAgentConnected}, Secret: fixtureWebhookKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterSubscriber("community-restore", sub); err != nil {
		t.Fatal(err)
	}
	registry.Publish(ctx, plugin.Event{Type: plugin.EventAgentConnected})
	select {
	case <-events:
	case <-time.After(2 * time.Second):
		t.Fatal("restored plugin did not receive event")
	}
}
