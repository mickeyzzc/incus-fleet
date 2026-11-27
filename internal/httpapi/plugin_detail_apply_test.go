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

func TestPluginConfigReinjectsIntoInstance(t *testing.T) {
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
	srv := &incusfake.Server{}
	f := newAPIFixtureWithDeps(t, srv, &incusfake.Server{}, authn, func(d *Deps) {
		d.Store = st
		d.Plugins = config.PluginsConfig{Enabled: true}
		d.Registry = registry
		d.Installer = plugininstall.New(plugininstall.Config{Store: st, Registry: registry})
	})
	cookie := f.login(t)
	if err := st.CreateInstalledPlugin(ctx, &store.InstalledPlugin{
		ID: "vm", Name: "vm", Upstream: "http://192.0.2.10:8428",
		Cluster: "c1", Instance: "fleet-vm", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	rec := f.request(t, http.MethodPut, "/api/v1/plugins/vm/config",
		`{"config":{"listen_port":"8429"}}`, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("config apply = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(srv.ExecArgs()) == 0 {
		t.Fatal("config script was not executed in the plugin instance")
	}
}
