package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

func TestPluginDescriptorCarriesDetailTabs(t *testing.T) {
	f, _, st, _ := newPluginFixture(t, config.PluginEventsConfig{})
	cookie := f.login(t)
	if err := st.UpsertPluginWiring(context.Background(), &store.PluginWiring{
		PluginID: "vm", Key: "metrics.query_url", Value: "http://192.0.2.10:8428",
		DefaultValue: "http://192.0.2.1:8428", Enabled: true, Approved: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertPluginConfig(context.Background(), "vm", "listen_port", "8428"); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordPluginHealth(context.Background(), &store.PluginHealthSample{
		PluginID: "vm", Status: "healthy", Detail: "probe ok",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordPluginEvent(context.Background(), &store.PluginEvent{
		PluginID: "vm", Kind: "delivery", Message: "agent.connected delivered",
	}); err != nil {
		t.Fatal(err)
	}

	rec := f.request(t, http.MethodGet, "/api/v1/plugins/vm", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET plugin = %d body=%s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`"config"`, `"wiring"`, `"events"`, `"health_history"`, "192.0.2.10:8428", "agent.connected delivered"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("descriptor missing %q: %s", want, rec.Body.String())
		}
	}
}

func TestPluginConfigAndWiringRestoreRoutes(t *testing.T) {
	f, _, st, _ := newPluginFixture(t, config.PluginEventsConfig{})
	cookie := f.login(t)
	if err := st.UpsertPluginWiring(context.Background(), &store.PluginWiring{
		PluginID: "vm", Key: "metrics.query_url", Value: "http://192.0.2.10:8428",
		DefaultValue: "http://192.0.2.1:8428", Enabled: true, Approved: true,
	}); err != nil {
		t.Fatal(err)
	}

	rec := f.request(t, http.MethodPut, "/api/v1/plugins/vm/config", `{"config":{"listen_port":"8429"}}`, cookie, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"listen_port":"8429"`) {
		t.Fatalf("PUT config = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = f.request(t, http.MethodPut, "/api/v1/plugins/vm/config", `{"config":{"unknown":"x"}}`, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown config key = %d", rec.Code)
	}

	rec = f.request(t, http.MethodDelete, "/api/v1/plugins/vm/wiring/metrics.query_url", nil, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE wiring = %d body=%s", rec.Code, rec.Body.String())
	}
	row, err := st.GetPluginWiring(context.Background(), "vm", "metrics.query_url")
	if err != nil || row.Enabled || row.Value != row.DefaultValue {
		t.Fatalf("restored wiring = %#v err=%v", row, err)
	}
	rec = f.request(t, http.MethodDelete, "/api/v1/plugins/vm/wiring/not-declared", nil, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("undeclared restore = %d", rec.Code)
	}
	rec = f.request(t, http.MethodDelete, "/api/v1/plugins/vm/wiring/logs.query_url", nil, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("foreign restore = %d", rec.Code)
	}
	rec = f.request(t, http.MethodDelete, "/api/v1/plugins/unknown/wiring/metrics.query_url", nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown plugin restore = %d", rec.Code)
	}
}
