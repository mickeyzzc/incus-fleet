package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/auth"
	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/instances"
	"github.com/mickeyzzc/incus-fleet/internal/logsquery"
	"github.com/mickeyzzc/incus-fleet/internal/plugin"
	"github.com/mickeyzzc/incus-fleet/internal/plugininstall"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func newPluginFixture(t *testing.T, events config.PluginEventsConfig) (*apiFixture, *plugin.Registry, *store.SQLStore, *httptest.Server) {
	t.Helper()
	st, err := store.Open(context.Background(), "sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	adapter := auth.NewStoreAdapter(st)
	authn := auth.New("secret", time.Hour, auth.WithUserStore(adapter), auth.WithTokenStore(adapter))
	registry := plugin.New(plugin.WithWiringStore(st))
	for _, p := range plugin.BuiltinPlugins() {
		if err := registry.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(backend.Close)

	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, authn, func(d *Deps) {
		d.Store = st
		d.Plugins = config.PluginsConfig{Enabled: true, Events: events, SigningKey: "example-signing-key-test"}
		d.Registry = registry
		d.Installer = plugininstall.New(plugininstall.Config{
			Store: st, Instances: testInstallInstances{}, Clusters: testInstallClusters{},
			Images: testInstallImages{}, Registry: registry,
		})
		d.LogsQuery = nil
		d.TaskWorker = true
	})

	return f, registry, st, backend
}

func TestPluginWiringRoutes(t *testing.T) {
	f, registry, st, _ := newPluginFixture(t, config.PluginEventsConfig{})
	cookie := f.login(t)
	if err := st.UpsertPluginWiring(context.Background(), &store.PluginWiring{
		PluginID: "vm", Key: "metrics.query_url", Value: "http://192.0.2.10:8428",
		DefaultValue: "http://192.0.2.1:8428", Enabled: true, Approved: true,
	}); err != nil {
		t.Fatal(err)
	}

	rec := f.request(t, http.MethodGet, "/api/v1/plugins/vm/wiring", nil, cookie, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "metrics.query_url") {
		t.Fatalf("GET wiring = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = f.request(t, http.MethodPut, "/api/v1/plugins/vm/wiring",
		`{"values":[{"key":"metrics.unknown","value":"http://192.0.2.2"}]}`, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("undeclared key = %d", rec.Code)
	}
	rec = f.request(t, http.MethodPut, "/api/v1/plugins/vm/wiring",
		`{"values":[{"key":"logs.query_url","value":"http://192.0.2.2"}]}`, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("key owned by other plugin = %d", rec.Code)
	}
	rec = f.request(t, http.MethodPut, "/api/v1/plugins/vm/wiring",
		`{"values":[{"key":"metrics.query_url","value":"not-a-url"}]}`, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad URL = %d", rec.Code)
	}
	rec = f.request(t, http.MethodPut, "/api/v1/plugins/vm/wiring",
		`{"values":[{"key":"metrics.query_url","value":"http://192.0.2.11:8428/"}]}`, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT wiring = %d body=%s", rec.Code, rec.Body.String())
	}
	if got, ok := registry.WiringValue("metrics.query_url"); !ok || got != "http://192.0.2.11:8428" {
		t.Fatalf("wiring = %q %v", got, ok)
	}
	rec = f.request(t, http.MethodPut, "/api/v1/plugins/vm/wiring",
		`{"values":[{"key":"metrics.query_url","enabled":false}]}`, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("restore default = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := registry.WiringValue("metrics.query_url"); ok {
		t.Fatal("wiring still active after restore")
	}
	rec = f.request(t, http.MethodGet, "/api/v1/plugins/unknown/wiring", nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown plugin wiring = %d", rec.Code)
	}
}

func TestLogsQueryUsesWiring(t *testing.T) {
	f, registry, _, backend := newPluginFixture(t, config.PluginEventsConfig{})
	if err := registry.SetWiring("vmlogs", "logs.query_url", backend.URL); err != nil {
		t.Fatal(err)
	}
	cookie := f.login(t)
	rec := f.request(t, http.MethodGet, "/api/v1/logs/query?query=level:error", nil, cookie, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ok") {
		t.Fatalf("log query = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = f.request(t, http.MethodGet, "/api/v1/logs/query", nil, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing query = %d", rec.Code)
	}
}

type testInstallInstances struct{}

func (testInstallInstances) Create(context.Context, string, instances.CreateRequest) (instances.Detail, error) {
	return instances.Detail{}, nil
}
func (testInstallInstances) Delete(context.Context, string, string, bool, bool) error { return nil }
func (testInstallInstances) Get(string, string) (instances.Detail, error) {
	return instances.Detail{Summary: instances.Summary{IPv4: []string{"192.0.2.60"}}}, nil
}
func (testInstallInstances) RunOps(context.Context, string, string, instances.OpsRequest) (instances.OpsResult, error) {
	return instances.OpsResult{}, nil
}
func (testInstallInstances) UploadFile(string, string, string, []byte) error { return nil }
func (testInstallInstances) CreateVolume(string, string, instances.VolumeRequest) error {
	return nil
}
func (testInstallInstances) DeleteVolume(string, string, string) error { return nil }

type testInstallImages struct{}

func (testInstallImages) Resolve(_ context.Context, _, ref string) (string, error) {
	return "fp-" + ref, nil
}

type testInstallClusters struct{}

func (testInstallClusters) List() []clusters.Summary {
	return []clusters.Summary{{Name: "c1", Online: true}}
}

func TestPluginInstallTaskRoutes(t *testing.T) {
	f, _, _, _ := newPluginFixture(t, config.PluginEventsConfig{})
	cookie := f.login(t)

	rec := f.request(t, http.MethodPost, "/api/v1/plugin-installs",
		`{"id":"vm","cluster":"c1","instance":"fleet-vm","approvals":["metrics.query_url"]}`, cookie, "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("submit = %d body=%s", rec.Code, rec.Body.String())
	}
	var submitted struct {
		Task struct {
			ID       string `json:"id"`
			PluginID string `json:"plugin_id"`
			State    string `json:"state"`
		} `json:"task"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &submitted); err != nil {
		t.Fatal(err)
	}
	if submitted.Task.ID == "" || submitted.Task.PluginID != "vm" || submitted.Task.State != store.HostTaskQueued {
		t.Fatalf("task = %#v", submitted.Task)
	}
	rec = f.request(t, http.MethodGet, "/api/v1/plugin-installs", nil, cookie, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), submitted.Task.ID) {
		t.Fatalf("list = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = f.request(t, http.MethodGet, "/api/v1/plugin-installs/"+submitted.Task.ID, nil, cookie, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "queued") {
		t.Fatalf("get = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = f.request(t, http.MethodPost, "/api/v1/plugin-installs/"+submitted.Task.ID+"/cancel", nil, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("cancel = %d", rec.Code)
	}
	rec = f.request(t, http.MethodPost, "/api/v1/plugin-installs/"+submitted.Task.ID+"/resume", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("resume = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = f.request(t, http.MethodPost, "/api/v1/plugin-installs", `{"id":"unknown"}`, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown plugin = %d", rec.Code)
	}
}

func TestLogsQueryValidationAndStaticBackend(t *testing.T) {
	f, registry, _, backend := newPluginFixture(t, config.PluginEventsConfig{})
	if err := registry.SetWiring("vmlogs", "logs.query_url", backend.URL); err != nil {
		t.Fatal(err)
	}
	cookie := f.login(t)
	for _, path := range []string{
		"/api/v1/logs/query?query=x&from=bad",
		"/api/v1/logs/query?query=x&to=bad",
		"/api/v1/logs/query?query=x&limit=0",
	} {
		if rec := f.request(t, http.MethodGet, path, nil, cookie, ""); rec.Code != http.StatusBadRequest {
			t.Fatalf("query %s = %d", path, rec.Code)
		}
	}

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer broken.Close()
	direct := &API{logsQuery: logsquery.New(logsquery.Config{URL: broken.URL})}
	rec := httptest.NewRecorder()
	direct.queryLogs(rec, httptest.NewRequest(http.MethodGet, "/api/v1/logs/query?query=x", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("broken backend = %d", rec.Code)
	}
	_ = backend
}

func TestPluginInstallTaskErrorResponses(t *testing.T) {
	f, _, _, _ := newPluginFixture(t, config.PluginEventsConfig{})
	cookie := f.login(t)
	for _, tc := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodPost, "/api/v1/plugin-installs", "{", http.StatusBadRequest},
		{http.MethodGet, "/api/v1/plugin-installs/missing", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/plugin-installs/missing/cancel", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/plugin-installs/missing/resume", "", http.StatusNotFound},
	} {
		rec := f.request(t, tc.method, tc.path, tc.body, cookie, "")
		if rec.Code != tc.want {
			t.Fatalf("%s %s = %d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

func TestManifestPackageLoadingAndSignatures(t *testing.T) {
	dir := t.TempDir()
	manifest := `schema: fleet-plugin/v1
id: community-nvr
name: Community NVR
version: 1.0.0
publisher: community-example
tier: community
description: Example signed community package
homepage: https://example.com/plugins/community-nvr
artifact:
  image:
    source: registry
    reference: example.com/plugins/community-nvr:1.0.0
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
`
	pkgDir := filepath.Join(dir, "community-nvr")
	if err := os.MkdirAll(pkgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "manifest.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(dir, "signing.key")
	if err := os.WriteFile(keyFile, []byte("example-signing-key-test"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := &API{pluginsConfig: config.PluginsConfig{Dir: dir, SigningKeyFile: keyFile}}
	if key := a.signingKey(); string(key) != "example-signing-key-test" {
		t.Fatalf("signing key = %q", key)
	}
	m, raw, err := a.pluginInstallManifest("community-nvr")
	if err != nil || m == nil || m.ID != "community-nvr" || len(raw) == 0 {
		t.Fatalf("manifest = %#v raw=%d err=%v", m, len(raw), err)
	}
	if a.manifestSignatureValid(m, pkgDir, raw) {
		t.Fatal("unsigned community package was accepted")
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "manifest.yaml.sig"),
		[]byte(plugin.SignPayload(a.signingKey(), raw)), 0o600); err != nil {
		t.Fatal(err)
	}
	if !a.manifestSignatureValid(m, pkgDir, raw) {
		t.Fatal("signed community package was rejected")
	}
	items := a.pluginCatalog()
	found := false
	for _, item := range items {
		if item.ID != "community-nvr" {
			continue
		}
		found = true
		if item.Name != "Community NVR" || item.Image == "" || item.Port != 0 || item.HealthPath != "/health" {
			t.Fatalf("catalog item = %#v", item)
		}
	}
	if !found {
		t.Fatalf("signed package missing from catalog: %#v", items)
	}
	if a.manifestSignatureValid(m, pkgDir, []byte("tampered")) {
		t.Fatal("tampered manifest was accepted")
	}
	if m, _, err := a.pluginInstallManifest("missing"); m != nil || err != nil {
		t.Fatalf("missing manifest = %#v err=%v", m, err)
	}
	broken := filepath.Join(dir, "broken")
	if err := os.MkdirAll(broken, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "manifest.yaml"), []byte("not a manifest"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.pluginInstallManifest("broken"); err == nil {
		t.Fatal("expected invalid manifest error")
	}
	if m, _, err := a.pluginInstallManifest(""); m != nil || err != nil {
		t.Fatalf("empty id = %#v err=%v", m, err)
	}

	// A built-in resolves without reading a package.
	if m, _, err := a.pluginInstallManifest("vm"); err != nil || m == nil || m.ID != "vm" {
		t.Fatalf("builtin manifest = %#v err=%v", m, err)
	}
}

func TestPluginWiringAndTaskErrorBranches(t *testing.T) {
	f, _, st, _ := newPluginFixture(t, config.PluginEventsConfig{})
	cookie := f.login(t)
	rec := f.request(t, http.MethodPut, "/api/v1/plugins/vm/wiring", "{", cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON = %d", rec.Code)
	}
	rec = f.request(t, http.MethodPut, "/api/v1/plugins/vm/wiring", `{"values":[]}`, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty values = %d", rec.Code)
	}
	rec = f.request(t, http.MethodGet, "/api/v1/plugins/unknown/wiring", nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown wiring = %d", rec.Code)
	}

	// A declared but unapproved key cannot be wired in.
	rec = f.request(t, http.MethodPut, "/api/v1/plugins/vm/wiring",
		`{"values":[{"key":"metrics.query_url","value":"http://192.0.2.2"}]}`, cookie, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unapproved key = %d", rec.Code)
	}

	// A finished task cannot be canceled or resumed.
	task := &store.HostTask{
		ID: "finished", Type: plugininstall.TaskType, State: store.HostTaskSucceeded,
		Plan: `{"id":"vm"}`, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := st.CreateHostTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	rec = f.request(t, http.MethodGet, "/api/v1/plugin-installs/finished", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get finished = %d", rec.Code)
	}
	rec = f.request(t, http.MethodPost, "/api/v1/plugin-installs/finished/cancel", nil, cookie, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("cancel finished = %d", rec.Code)
	}
	rec = f.request(t, http.MethodPost, "/api/v1/plugin-installs/finished/resume", nil, cookie, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("resume finished = %d", rec.Code)
	}
	other := &store.HostTask{
		ID: "other", Type: "host.task", State: store.HostTaskQueued,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := st.CreateHostTask(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	rec = f.request(t, http.MethodGet, "/api/v1/plugin-installs/other", nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-install task = %d", rec.Code)
	}

	disabled := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, nil, func(d *Deps) {
		d.Plugins = config.PluginsConfig{Enabled: false}
	})
	cookie = disabled.login(t)
	rec = disabled.request(t, http.MethodPost, "/api/v1/plugin-installs", `{"id":"vm"}`, cookie, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled install = %d", rec.Code)
	}
}

func TestPluginEventIngestionRoute(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "secret")
	tokenPath := filepath.Join(dir, "bearer")
	if err := os.WriteFile(secretPath, []byte("example-event-secret-test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, []byte("example-event-bearer-test"), 0o600); err != nil {
		t.Fatal(err)
	}
	events := config.PluginEventsConfig{Enabled: true, Path: "/api/v1/plugins/events", SecretFile: secretPath, BearerTokenFile: tokenPath}
	f, _, _, _ := newPluginFixture(t, events)
	cookie := f.login(t)
	body := `{"type":"agent.connected","subject":"agent-1"}`
	sig := plugin.SignPayload([]byte("example-event-secret-test"), []byte(body))
	request, err := http.NewRequest(http.MethodPost, "/api/v1/plugins/events", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer example-event-bearer-test")
	request.Header.Set("X-Fleet-Signature", sig)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, request)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("event = %d body=%s", rec.Code, rec.Body.String())
	}
	request, err = http.NewRequest(http.MethodPost, "/api/v1/plugins/events", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer example-event-bearer-test")
	request.Header.Set("X-Fleet-Signature", "bad")
	rec = httptest.NewRecorder()
	f.handler.ServeHTTP(rec, request)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature = %d", rec.Code)
	}

	for _, tc := range []struct {
		name, body, auth, sig, pluginID string
		want                            int
	}{
		{name: "missing credential", body: body, sig: sig, want: http.StatusUnauthorized},
		{name: "bad json", body: "{", auth: "Bearer example-event-bearer-test", sig: plugin.SignPayload([]byte("example-event-secret-test"), []byte("{")), want: http.StatusBadRequest},
		{name: "no event type", body: `{"subject":"agent-1"}`, auth: "Bearer example-event-bearer-test", sig: plugin.SignPayload([]byte("example-event-secret-test"), []byte(`{"subject":"agent-1"}`)), want: http.StatusBadRequest},
		{name: "unknown plugin", body: body, auth: "Bearer example-event-bearer-test", sig: sig, pluginID: "unknown", want: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, "/api/v1/plugins/events", strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			if tc.sig != "" {
				req.Header.Set("X-Fleet-Signature", tc.sig)
			}
			if tc.pluginID != "" {
				req.Header.Set("X-Fleet-Plugin-ID", tc.pluginID)
			}
			rec := httptest.NewRecorder()
			f.handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
	_ = cookie
}
