package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/plugin"
	"github.com/mickeyzzc/incus-fleet/internal/plugininstall"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

func directAPI(t *testing.T) (*API, *store.SQLStore, *plugin.Registry) {
	t.Helper()
	st, err := store.Open(context.Background(), "sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	registry := plugin.New(plugin.WithWiringStore(st))
	for _, p := range plugin.BuiltinPlugins() {
		if err := registry.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	return &API{
		store: st, registry: registry, pluginsEnabled: true, taskWorker: true,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, st, registry
}

func directRequest(t *testing.T, handler http.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	// Handlers are invoked directly, so populate the path values ServeMux would
	// normally provide.
	switch {
	case strings.Contains(path, "/plugins/vm/"):
		req.SetPathValue("id", "vm")
	case strings.Contains(path, "/plugin-installs/x"):
		req.SetPathValue("id", "x")
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler(rec, req)

	return rec
}

func TestPluginHandlersDirectErrorBranches(t *testing.T) {
	a, st, registry := directAPI(t)
	a.installer = plugininstall.New(plugininstall.Config{Store: st, Registry: registry})
	if rec := directRequest(t, a.submitPluginInstallTask, http.MethodPost, "/api/v1/plugin-installs", "{"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body = %d", rec.Code)
	}
	if rec := directRequest(t, a.submitPluginInstallTask, http.MethodPost, "/api/v1/plugin-installs", `{"id":"unknown"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown plugin = %d", rec.Code)
	}
	a.pluginsConfig = config.PluginsConfig{Dir: t.TempDir(), SigningKey: "example-signing-key-test"}
	dir := filepath.Join(a.pluginsConfig.Dir, "community-x")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(communityManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if rec := directRequest(t, a.submitPluginInstallTask, http.MethodPost, "/api/v1/plugin-installs",
		`{"id":"community-x","signature":"bad"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("bad signature = %d body=%s", rec.Code, rec.Body.String())
	}
	a.pluginsEnabled = false
	if rec := directRequest(t, a.submitPluginInstallTask, http.MethodPost, "/api/v1/plugin-installs", `{"id":"vm"}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled = %d", rec.Code)
	}
	a.pluginsEnabled = true
	a.registry = nil
	if rec := directRequest(t, a.submitPluginInstallTask, http.MethodPost, "/api/v1/plugin-installs", `{"id":"vm"}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no registry = %d", rec.Code)
	}
	a.registry = registry
	a.installer = nil
	if rec := directRequest(t, a.submitPluginInstallTask, http.MethodPost, "/api/v1/plugin-installs", `{"id":"vm"}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no installer = %d", rec.Code)
	}

	// Store failures map to service errors.
	a.installer = plugininstall.New(plugininstall.Config{Store: st, Registry: registry})
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if rec := directRequest(t, a.listPluginInstallTasks, http.MethodGet, "/api/v1/plugin-installs", ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("list store error = %d", rec.Code)
	}
	if rec := directRequest(t, a.getPluginInstallTask, http.MethodGet, "/api/v1/plugin-installs/x", ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("get store error = %d", rec.Code)
	}
	if rec := directRequest(t, a.getPluginWiring, http.MethodGet, "/api/v1/plugins/vm/wiring", ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("wiring store error = %d", rec.Code)
	}
	if rec := directRequest(t, a.putPluginWiring, http.MethodPut, "/api/v1/plugins/vm/wiring",
		`{"values":[{"key":"metrics.query_url","value":"http://192.0.2.2"}]}`); rec.Code != http.StatusInternalServerError {
		t.Fatalf("wiring put store error = %d", rec.Code)
	}
	if rec := directRequest(t, a.cancelPluginInstallTask, http.MethodPost, "/api/v1/plugin-installs/x/cancel", ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("cancel store error = %d", rec.Code)
	}
	if rec := directRequest(t, a.resumePluginInstallTask, http.MethodPost, "/api/v1/plugin-installs/x/resume", ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("resume store error = %d", rec.Code)
	}
}

func TestEventIngestionWithoutRegistry(t *testing.T) {
	a := &API{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if rec := directRequest(t, a.ingestPluginEvent, http.MethodPost, "/api/v1/plugins/events", "{}"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no registry = %d", rec.Code)
	}
}

const communityManifest = `schema: fleet-plugin/v1
id: community-x
name: Community X
version: 1.0.0
publisher: community-example
tier: community
description: Example community package
homepage: https://example.com/plugins/community-x
artifact:
  image:
    source: registry
    reference: example.com/plugins/community-x:1.0.0
    architectures: [amd64]
install:
  defaults:
    cpu: 1
    memory_mb: 512
    root_disk_gb: 10
    network: managed
  data_disk:
    filesystem: ext4
health:
  endpoint: /health
`
