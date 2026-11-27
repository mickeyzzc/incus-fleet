package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

func TestPluginDetailErrorBranches(t *testing.T) {
	f, _, st, _ := newPluginFixture(t, config.PluginEventsConfig{})
	cookie := f.login(t)

	for _, tc := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodPut, "/api/v1/plugins/missing/config", `{"config":{"listen_port":"8428"}}`, http.StatusNotFound},
		{http.MethodPut, "/api/v1/plugins/vm/config", `{`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/plugins/vm/config", `{"config":{}}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/plugins/vm/config", `{"config":{"listen_port":"not-a-port"}}`, http.StatusBadRequest},
		{http.MethodDelete, "/api/v1/plugins/missing/wiring/metrics.query_url", "", http.StatusNotFound},
		{http.MethodDelete, "/api/v1/plugins/vm/wiring/metrics.query_url", "", http.StatusNotFound},
	} {
		rec := f.request(t, tc.method, tc.path, tc.body, cookie, "")
		if rec.Code != tc.want {
			t.Fatalf("%s %s = %d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	if err := st.UpsertPluginWiring(context.Background(), &store.PluginWiring{
		PluginID: "vm", Key: "metrics.query_url", Value: "http://192.0.2.10",
		DefaultValue: "http://192.0.2.1", Enabled: true, Approved: false,
	}); err != nil {
		t.Fatal(err)
	}
	rec := f.request(t, http.MethodDelete, "/api/v1/plugins/vm/wiring/metrics.query_url", nil, cookie, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unapproved restore = %d", rec.Code)
	}

	// A wiring row whose key is no longer declared still appears with the key
	// as its human-facing capability label.
	if err := st.UpsertPluginWiring(context.Background(), &store.PluginWiring{
		PluginID: "vm", Key: "legacy.key", Value: "http://192.0.2.40", Enabled: true, Approved: true,
	}); err != nil {
		t.Fatal(err)
	}
	rec = f.request(t, http.MethodGet, "/api/v1/plugins/vm", nil, cookie, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "legacy.key") {
		t.Fatalf("legacy wiring descriptor = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = f.request(t, http.MethodGet, "/api/v1/plugins/unknown", nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown descriptor = %d", rec.Code)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	rec = f.request(t, http.MethodGet, "/api/v1/plugins/vm", nil, cookie, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("descriptor with closed store = %d body=%s", rec.Code, rec.Body.String())
	}
}
