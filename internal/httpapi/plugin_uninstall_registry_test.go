package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/plugin"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

func TestUninstallCleansRegistryAndDetailData(t *testing.T) {
	f, registry, st, _ := newPluginFixture(t, config.PluginEventsConfig{})
	cookie := f.login(t)
	ctx := context.Background()
	if err := st.CreateInstalledPlugin(ctx, &store.InstalledPlugin{
		ID: "vm", Name: "vm", Upstream: "http://192.0.2.10:8428", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertPluginWiring(ctx, &store.PluginWiring{
		PluginID: "vm", Key: "metrics.query_url", Value: "http://192.0.2.10",
		DefaultValue: "http://192.0.2.1", Enabled: true, Approved: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertPluginConfig(ctx, "vm", "listen_port", "8428"); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterSubscriber("vm", stubHTTPAPISubscriber{}); err != nil {
		t.Fatal(err)
	}
	rec := f.request(t, http.MethodDelete, "/api/v1/plugins/vm", nil, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("uninstall = %d body=%s", rec.Code, rec.Body.String())
	}
	if values, err := st.ListPluginConfig(ctx, "vm"); err != nil || len(values) != 0 {
		t.Fatalf("config after uninstall = %#v err=%v", values, err)
	}
	row, err := st.GetPluginWiring(ctx, "vm", "metrics.query_url")
	if err != nil || row.Enabled || row.Value != row.DefaultValue {
		t.Fatalf("wiring after uninstall = %#v err=%v", row, err)
	}
}

type stubHTTPAPISubscriber struct{}

func (stubHTTPAPISubscriber) HandleEvent(context.Context, plugin.Event) error { return nil }
