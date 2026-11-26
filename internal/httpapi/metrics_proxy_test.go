package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/metricsquery"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestMetricsQueryProxy(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query_range" || !strings.Contains(r.URL.RawQuery, "query=fleet_host_load") {
			t.Errorf("backend request = %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	defer backend.Close()

	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	f.handler = New(Deps{
		Clusters: f.manager, Store: f.store, Auth: f.auth, Version: "test",
		MetricsQuery: metricsquery.New(metricsquery.Config{URL: backend.URL}),
	}).Handler()
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/api/v1/metrics/query_range?query=fleet_host_load&from=2h&to=1h&step=1m", nil, cookie, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "success") {
		t.Fatalf("query range = %d body=%s", rec.Code, rec.Body.String())
	}
	missing := f.request(t, http.MethodGet, "/api/v1/metrics/query_range", nil, cookie, "")
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing query = %d", missing.Code)
	}
}

func TestMetricsQueryUsesInstalledVMPLugin(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// VictoriaMetrics answers PromQL under /prometheus/api/v1/query_range.
		if r.URL.Path != "/prometheus/api/v1/query_range" {
			t.Errorf("backend path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	defer backend.Close()

	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	if err := f.store.CreateInstalledPlugin(context.Background(), &store.InstalledPlugin{
		ID: "vm", Name: "VictoriaMetrics", Upstream: backend.URL,
		UIPath: "/plugin/vm", Role: "viewer", Enabled: true,
	}); err != nil {
		t.Fatalf("CreateInstalledPlugin() error = %v", err)
	}
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/api/v1/metrics/query_range?query=fleet_host_load", nil, cookie, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "success") {
		t.Fatalf("query range via plugin = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMetricsQueryProxyErrors(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)
	disabled := f.request(t, http.MethodGet, "/api/v1/metrics/query_range?query=x", nil, cookie, "")
	if disabled.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled = %d", disabled.Code)
	}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer backend.Close()
	f.handler = New(Deps{
		Clusters: f.manager, Store: f.store, Auth: f.auth, Version: "test",
		MetricsQuery: metricsquery.New(metricsquery.Config{URL: backend.URL}),
	}).Handler()

	badStep := f.request(t, http.MethodGet, "/api/v1/metrics/query_range?query=x&step=bad", nil, cookie, "")
	if badStep.Code != http.StatusBadRequest {
		t.Fatalf("bad step = %d", badStep.Code)
	}
	reversed := f.request(t, http.MethodGet, "/api/v1/metrics/query_range?query=x&from=1h&to=2h", nil, cookie, "")
	if reversed.Code != http.StatusBadRequest {
		t.Fatalf("reversed = %d", reversed.Code)
	}
	upstream := f.request(t, http.MethodGet, "/api/v1/metrics/query_range?query=x&from=2h&to=1h", nil, cookie, "")
	if upstream.Code != http.StatusBadGateway {
		t.Fatalf("upstream = %d body=%s", upstream.Code, upstream.Body.String())
	}
}
