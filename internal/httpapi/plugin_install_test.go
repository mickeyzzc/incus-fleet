package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/auth"
	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/instances"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestPluginOneClickInstall(t *testing.T) {
	st, err := store.Open(context.Background(), "sqlite", ":memory:")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	adapter := auth.NewStoreAdapter(st)
	authn := auth.New("secret", time.Hour, auth.WithUserStore(adapter), auth.WithTokenStore(adapter))

	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, authn, func(d *Deps) {
		d.Plugins = config.PluginsConfig{Enabled: true}
	})
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/api/v1/plugins/catalog", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog status = %d body = %s", rec.Code, rec.Body.String())
	}
	var catalog struct {
		Catalog []pluginCatalogItem `json:"catalog"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if len(catalog.Catalog) != 2 || catalog.Catalog[0].ID != "vm" || catalog.Catalog[1].ID != "vmlogs" {
		t.Fatalf("catalog = %#v", catalog.Catalog)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/plugins/install",
		`{"id":"vm","upstream":"http://127.0.0.1:8428"}`, cookie, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("install status = %d body = %s", rec.Code, rec.Body.String())
	}
	var installed struct {
		Plugin pluginView `json:"plugin"`
		Token  string     `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &installed); err != nil {
		t.Fatalf("decode install: %v", err)
	}
	if installed.Plugin.ID != "vm" || installed.Token == "" {
		t.Fatalf("install result = %#v", installed)
	}

	rec = f.request(t, http.MethodGet, "/api/v1/plugins", nil, cookie, "")
	var list struct {
		Plugins []pluginView `json:"plugins"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Plugins) != 1 || list.Plugins[0].ID != "vm" {
		t.Fatalf("plugins = %#v", list.Plugins)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/plugins/vm/toggle", `{"enabled":false}`, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle status = %d body = %s", rec.Code, rec.Body.String())
	}
	rec = f.request(t, http.MethodDelete, "/api/v1/plugins/vm", nil, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("uninstall status = %d body = %s", rec.Code, rec.Body.String())
	}
	rec = f.request(t, http.MethodGet, "/api/v1/plugins", nil, cookie, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list after uninstall: %v", err)
	}
	if len(list.Plugins) != 0 {
		t.Fatalf("plugins after uninstall = %#v", list.Plugins)
	}
}

func TestPickLeastLoadedMember(t *testing.T) {
	members := []clusters.Member{
		{Name: "busy", Status: "Online", Memory: clusters.ResourceMem{Used: 90, Total: 100}, Storage: clusters.StorageUsage{Used: 90, Total: 100}},
		{Name: "offline", Status: "Offline", Memory: clusters.ResourceMem{Used: 1, Total: 100}},
		{Name: "idle", Status: "Online", Memory: clusters.ResourceMem{Used: 10, Total: 100}, Storage: clusters.StorageUsage{Used: 5, Total: 100}},
	}
	if got := pickLeastLoadedMember(members); got != "idle" {
		t.Fatalf("pickLeastLoadedMember() = %q, want idle", got)
	}
	if got := pickLeastLoadedMember(nil); got != "" {
		t.Fatalf("empty members = %q", got)
	}
}

func TestPluginCatalogLoadsManifests(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"id":"hello","name":"Hello","ui_path":"/plugin/hello","image":"debian/13","default_cpu":"1","default_memory":"512MiB","default_disk":"10GiB","default_network":"incusbr0","port":8080,"install_script_file":"install.sh"}`
	if err := os.WriteFile(filepath.Join(dir, "hello.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "install.sh"), []byte("echo hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := &API{pluginsConfig: config.PluginsConfig{Dir: dir}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	items := a.pluginCatalog()

	var found bool
	for _, item := range items {
		if item.ID == "hello" {
			found = true
			if item.InstallHook != "echo hi\n" {
				t.Fatalf("install hook = %q", item.InstallHook)
			}
		}
	}
	if !found {
		t.Fatalf("custom manifest missing: %#v", items)
	}
}

func TestPluginArtifactDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("artifact"))
	}))
	defer server.Close()

	data, err := downloadPluginArtifact(context.Background(), server.URL)
	if err != nil || string(data) != "artifact" {
		t.Fatalf("download = %q err=%v", data, err)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer bad.Close()
	if _, err := downloadPluginArtifact(context.Background(), bad.URL); err == nil {
		t.Fatal("expected status error")
	}
}

func TestResolvePluginImage(t *testing.T) {
	srv := &incusfake.Server{Images: []api.Image{{
		Fingerprint: "fp123",
		Aliases:     []api.ImageAlias{{Name: "debian/13"}},
	}}}
	f := newAPIFixture(t, srv, &incusfake.Server{})
	a := &API{clusters: f.manager}

	if got, err := a.resolvePluginImage("c1", "debian/13"); err != nil || got != "fp123" {
		t.Fatalf("alias resolve = %q err=%v", got, err)
	}
	if got, err := a.resolvePluginImage("c1", "fp123"); err != nil || got != "fp123" {
		t.Fatalf("fingerprint resolve = %q err=%v", got, err)
	}
	if _, err := a.resolvePluginImage("c1", "missing"); err == nil {
		t.Fatal("expected missing image error")
	}
}

func TestPluginHelpers(t *testing.T) {
	if got := firstNonEmpty("", " ", "x"); got != "x" {
		t.Fatalf("firstNonEmpty = %q", got)
	}
	if got := truncateForLog("12345", 3); got != "123…" {
		t.Fatalf("truncateForLog = %q", got)
	}
}

func TestPluginInstallDeploysInstance(t *testing.T) {
	srv := &incusfake.Server{
		Images: []api.Image{{Fingerprint: "fp-vm", Aliases: []api.ImageAlias{{Name: "vm-oci"}}}},
		Instance: &api.Instance{
			Name: "fleet-vm", Status: "Running", StatusCode: api.Running,
			InstancePut: api.InstancePut{Config: api.ConfigMap{}},
		},
		State: &api.InstanceState{
			Status: "Running", StatusCode: api.Running,
			Network: map[string]api.InstanceStateNetwork{
				"eth0": {Addresses: []api.InstanceStateNetworkAddress{{Family: "inet", Address: "10.63.0.50"}}},
			},
		},
	}
	st, err := store.Open(context.Background(), "sqlite", ":memory:")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	adapter := auth.NewStoreAdapter(st)
	authn := auth.New("secret", time.Hour, auth.WithUserStore(adapter), auth.WithTokenStore(adapter))
	f := newAPIFixtureWithDeps(t, srv, &incusfake.Server{}, authn, func(d *Deps) {
		d.Plugins = config.PluginsConfig{Enabled: true}
	})
	cookie := f.login(t)

	rec := f.request(t, http.MethodPost, "/api/v1/plugins/install",
		`{"id":"vm","cluster":"c1","member":"node-a","name":"fleet-vm"}`, cookie, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("deploy install status = %d body = %s", rec.Code, rec.Body.String())
	}
	if len(srv.CreateArgs()) != 1 {
		t.Fatalf("CreateInstance calls = %d", len(srv.CreateArgs()))
	}
	entry, err := f.store.GetInstalledPlugin(context.Background(), "vm")
	if err != nil {
		t.Fatalf("GetInstalledPlugin() error = %v", err)
	}
	if entry.Instance != "fleet-vm" || !strings.Contains(entry.Upstream, "10.63.0.50") {
		t.Fatalf("entry = %#v", entry)
	}

	rec = f.request(t, http.MethodDelete, "/api/v1/plugins/vm", nil, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("uninstall status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestPluginInstallRejectsDisabledAndDuplicate(t *testing.T) {
	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, nil, func(d *Deps) {
		d.Plugins = config.PluginsConfig{Enabled: false}
	})
	cookie := f.login(t)
	rec := f.request(t, http.MethodPost, "/api/v1/plugins/install", `{"id":"vm"}`, cookie, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled install = %d", rec.Code)
	}

	enabled := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, nil, func(d *Deps) {
		d.Plugins = config.PluginsConfig{Enabled: true}
	})
	cookie = enabled.login(t)
	if err := enabled.store.CreateInstalledPlugin(context.Background(), &store.InstalledPlugin{ID: "vm", Name: "vm", Upstream: "http://127.0.0.1:8428"}); err != nil {
		t.Fatal(err)
	}
	rec = enabled.request(t, http.MethodPost, "/api/v1/plugins/install", `{"id":"vm"}`, cookie, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate install = %d", rec.Code)
	}
}

func TestPluginToggleAndUninstallErrors(t *testing.T) {
	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, nil, func(d *Deps) {
		d.Plugins = config.PluginsConfig{Enabled: true}
	})
	cookie := f.login(t)

	rec := f.request(t, http.MethodPost, "/api/v1/plugins/missing/toggle", `{"enabled":false}`, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("toggle missing = %d", rec.Code)
	}
	rec = f.request(t, http.MethodDelete, "/api/v1/plugins/missing", nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("uninstall missing = %d", rec.Code)
	}

	if err := f.store.CreateInstalledPlugin(context.Background(), &store.InstalledPlugin{
		ID: "vm", Name: "VM", Upstream: "http://127.0.0.1:8428", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	rec = f.request(t, http.MethodPost, "/api/v1/plugins/vm/toggle", `{"enabled":false}`, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle = %d body=%s", rec.Code, rec.Body.String())
	}
	entry, err := f.store.GetInstalledPlugin(context.Background(), "vm")
	if err != nil || entry.Enabled {
		t.Fatalf("entry = %#v err=%v", entry, err)
	}
}

func TestPickPluginMemberUnknownCluster(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	a := &API{clusters: f.manager}
	if got := pickPluginMember(a, "missing"); got != "" {
		t.Fatalf("pickPluginMember = %q", got)
	}
}

func TestProvisionPluginErrors(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	a := &API{
		clusters:  f.manager,
		instances: instances.New(f.manager),
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	if _, _, _, _, err := a.provisionPlugin(context.Background(), pluginCatalogItem{ID: "x", Image: "i"}, pluginInstallRequest{Upstream: "not-a-url"}); err == nil {
		t.Fatal("expected invalid upstream error")
	}
	if _, _, _, _, err := a.provisionPlugin(context.Background(), pluginCatalogItem{ID: "x", Image: "i"}, pluginInstallRequest{}); err == nil {
		t.Fatal("expected missing cluster error")
	}
	if _, _, _, _, err := a.provisionPlugin(context.Background(), pluginCatalogItem{ID: "x", Image: "missing"}, pluginInstallRequest{Cluster: "c1", Member: "node-a"}); err == nil {
		t.Fatal("expected missing image error")
	}
}

func TestProvisionPluginHookAndArtifact(t *testing.T) {
	artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("bundle"))
	}))
	defer artifact.Close()

	srv := &incusfake.Server{
		Images:   []api.Image{{Fingerprint: "fp-custom", Aliases: []api.ImageAlias{{Name: "custom"}}}},
		Instance: &api.Instance{Name: "custom-vm", Status: "Running", StatusCode: api.Running},
		State: &api.InstanceState{
			Status: "Running", StatusCode: api.Running,
			Network: map[string]api.InstanceStateNetwork{"eth0": {Addresses: []api.InstanceStateNetworkAddress{{Family: "inet", Address: "10.63.0.60"}}}},
		},
	}
	f := newAPIFixture(t, srv, &incusfake.Server{})
	a := &API{
		clusters:  f.manager,
		instances: instances.New(f.manager),
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	item := pluginCatalogItem{
		ID: "custom", Name: "Custom", Image: "custom", Port: 8080,
		DefaultCPU: "1", DefaultMem: "512MiB", DefaultDisk: "5GiB", Network: "incusbr0",
		ArtifactURL: artifact.URL, InstallHook: "echo ok\n",
	}
	cluster, _, upstream, instance, err := a.provisionPlugin(context.Background(), item, pluginInstallRequest{Cluster: "c1", Member: "node-a", Name: "custom-vm"})
	if err != nil {
		t.Fatalf("provisionPlugin() error = %v", err)
	}
	if cluster != "c1" || instance != "custom-vm" || !strings.Contains(upstream, "10.63.0.60:8080") {
		t.Fatalf("result = %s %s %s", cluster, instance, upstream)
	}
	if len(srv.ExecArgs()) == 0 {
		t.Fatal("install hook was not executed")
	}
}

func TestPluginCatalogSkipsBadManifests(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{not json"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "noid.json"), []byte(`{"name":"x"}`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "missing-script.json"), []byte(`{"id":"x","name":"x","install_script_file":"absent.sh"}`), 0o600)

	a := &API{pluginsConfig: config.PluginsConfig{Dir: dir}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if items := a.pluginCatalog(); len(items) != len(builtinPluginCatalog) {
		t.Fatalf("bad manifests leaked: %d", len(items))
	}
}

func TestPluginMiscBranches(t *testing.T) {
	if got := firstNonEmpty("", " "); got != "" {
		t.Fatalf("firstNonEmpty empty = %q", got)
	}
	if got := truncateForLog("short", 10); got != "short" {
		t.Fatalf("truncate short = %q", got)
	}
	if _, err := downloadPluginArtifact(context.Background(), "://bad"); err == nil {
		t.Fatal("expected invalid URL error")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	a := &API{clusters: f.manager, instances: instances.New(f.manager), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := a.waitPluginIP(ctx, "c1", "missing", time.Second); err == nil {
		t.Fatal("expected wait cancellation")
	}
	if entries := a.installedPluginEntries(); entries != nil {
		t.Fatalf("disabled entries = %#v", entries)
	}
	a.pluginsEnabled = true
	if entries := a.installedPluginEntries(); entries != nil {
		t.Fatalf("nil store entries = %#v", entries)
	}
}

func TestPluginInstallTokenFailure(t *testing.T) {
	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, nil, func(d *Deps) {
		d.Plugins = config.PluginsConfig{Enabled: true}
	})
	cookie := f.login(t)
	rec := f.request(t, http.MethodPost, "/api/v1/plugins/install",
		`{"id":"vm","upstream":"http://127.0.0.1:8428"}`, cookie, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("token failure status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUninstallPluginDeleteFailure(t *testing.T) {
	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, nil, func(d *Deps) {
		d.Plugins = config.PluginsConfig{Enabled: true}
	})
	cookie := f.login(t)
	if err := f.store.CreateInstalledPlugin(context.Background(), &store.InstalledPlugin{
		ID: "vm", Name: "VM", Upstream: "http://127.0.0.1:8428",
		Cluster: "c1", Instance: "missing", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	rec := f.request(t, http.MethodDelete, "/api/v1/plugins/vm", nil, cookie, "")
	if rec.Code != http.StatusInternalServerError && rec.Code != http.StatusNotFound {
		t.Fatalf("uninstall failure = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPluginInstallRejectsUnknownAndBadUpstream(t *testing.T) {
	st, err := store.Open(context.Background(), "sqlite", ":memory:")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	adapter := auth.NewStoreAdapter(st)
	authn := auth.New("secret", time.Hour, auth.WithUserStore(adapter), auth.WithTokenStore(adapter))
	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, authn, func(d *Deps) {
		d.Plugins = config.PluginsConfig{Enabled: true}
	})
	cookie := f.login(t)

	for _, body := range []string{
		`{"id":"unknown","upstream":"http://127.0.0.1:8428"}`,
		`{"id":"vm","upstream":"not-a-url"}`,
		`{"id":"vm","cluster":"missing","upstream":""}`,
	} {
		rec := f.request(t, http.MethodPost, "/api/v1/plugins/install", body, cookie, "")
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusInternalServerError {
			t.Fatalf("install %s status = %d", body, rec.Code)
		}
	}
}

func TestResolvePluginImageListError(t *testing.T) {
	srv := &incusfake.Server{ImagesErr: errors.New("boom")}
	f := newAPIFixture(t, srv, &incusfake.Server{})
	a := &API{clusters: f.manager}
	if _, err := a.resolvePluginImage("c1", "x"); err == nil {
		t.Fatal("expected image list error")
	}
}
