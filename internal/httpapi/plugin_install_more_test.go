package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/auth"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/plugin"
	"github.com/mickeyzzc/incus-fleet/internal/plugininstall"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestSubmitInstallTaskErrorBranches(t *testing.T) {
	f, _, _, _ := newPluginFixture(t, config.PluginEventsConfig{})
	cookie := f.login(t)
	rec := f.request(t, http.MethodPost, "/api/v1/plugin-installs", `{"id":""}`, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty id = %d", rec.Code)
	}
	rec = f.request(t, http.MethodPost, "/api/v1/plugin-installs", `{"id":"vm"}`, cookie, "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("first submit = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = f.request(t, http.MethodPost, "/api/v1/plugin-installs", `{"id":"vm"}`, cookie, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate queued install = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSubmitInstallTaskRunsInlineWithoutDispatcher(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, "sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	adapter := auth.NewStoreAdapter(st)
	authn := auth.New("secret", time.Hour, auth.WithUserStore(adapter), auth.WithTokenStore(adapter))
	registry := plugin.New(plugin.WithWiringStore(st))
	for _, p := range plugin.BuiltinPlugins() {
		if err := registry.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.Start(ctx); err != nil {
		t.Fatal(err)
	}
	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, authn, func(d *Deps) {
		d.Store = st
		d.Plugins = config.PluginsConfig{Enabled: true}
		d.Registry = registry
		d.Installer = plugininstall.New(plugininstall.Config{Store: st, Registry: registry})
		d.TaskWorker = false
	})
	cookie := f.login(t)
	rec := f.request(t, http.MethodPost, "/api/v1/plugin-installs",
		`{"id":"vm","connect_only":true,"upstream":"http://192.0.2.10:8428"}`, cookie, "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("submit = %d body=%s", rec.Code, rec.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tasks, err := st.ListHostTasks(ctx, "", "", 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) == 1 && tasks[0].State == store.HostTaskSucceeded {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("inline install task did not finish")
}

func TestUninstallPluginStoreErrors(t *testing.T) {
	f, _, st, _ := newPluginFixture(t, config.PluginEventsConfig{})
	cookie := f.login(t)
	if err := st.CreateInstalledPlugin(context.Background(), &store.InstalledPlugin{
		ID: "vm", Name: "vm", Upstream: "http://192.0.2.10:8428", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	rec := f.request(t, http.MethodDelete, "/api/v1/plugins/vm", nil, cookie, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("uninstall with closed store = %d body=%s", rec.Code, rec.Body.String())
	}
}
