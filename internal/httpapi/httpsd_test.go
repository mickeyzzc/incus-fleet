package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/agentpb"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/telemetry"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestMetricsServiceDiscoveryAndProxy(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	now := time.Now().UTC()
	for _, agent := range []*store.Agent{
		{ID: "agent-1", Cluster: "c1", Member: "node-a", Status: store.AgentOnline, CreatedAt: now, UpdatedAt: now},
		{ID: "agent-2", Cluster: "c1", Member: "node-b", Status: store.AgentOffline, CreatedAt: now, UpdatedAt: now},
	} {
		if err := f.store.UpsertAgent(context.Background(), agent); err != nil {
			t.Fatal(err)
		}
	}
	ts := telemetry.New(10)
	ts.Ingest(&agentpb.TelemetryBatch{AgentId: "agent-1", Series: []*agentpb.Series{{
		Name: "fleet_host_load", Samples: []*agentpb.Sample{{AtMs: now.UnixMilli(), Value: 1}},
	}}}, "c1", "node-a")
	f.handler = New(Deps{
		Clusters: f.manager, Store: f.store, Telemetry: ts, Auth: f.auth, Version: "test",
		MetricsSD: MetricsSDConfig{Enabled: true, Token: "sd-token", PublicURL: "https://fleet.example"},
	}).Handler()

	unauthorized := f.request(t, http.MethodGet, "/api/v1/sd/agents", nil, nil, "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized = %d", unauthorized.Code)
	}
	rec := f.request(t, http.MethodGet, "/api/v1/sd/agents", nil, nil, "sd-token")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "agent-1") ||
		!strings.Contains(rec.Body.String(), "fleet.example") ||
		!strings.Contains(rec.Body.String(), "/api/v1/agents/agent-1/metrics/prometheus") {
		t.Fatalf("sd = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "agent-2") {
		t.Fatalf("offline agent included: %s", rec.Body.String())
	}

	metrics := f.request(t, http.MethodGet, "/api/v1/agents/agent-1/metrics/prometheus", nil, nil, "sd-token")
	if metrics.Code != http.StatusOK || !strings.Contains(metrics.Body.String(), "fleet_agent_up") {
		t.Fatalf("metrics = %d body=%s", metrics.Code, metrics.Body.String())
	}
}

func TestMetricsServiceDiscoveryDisabled(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	rec := f.request(t, http.MethodGet, "/api/v1/sd/agents", nil, nil, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMetricsServiceDiscoveryFallbackHostAndErrors(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	now := time.Now().UTC()
	if err := f.store.UpsertAgent(context.Background(), &store.Agent{
		ID: "agent-1", Cluster: "c1", Member: "node-a", Status: store.AgentOnline,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	f.handler = New(Deps{
		Clusters: f.manager, Store: f.store, Auth: f.auth, Version: "test",
		Telemetry: telemetry.New(2),
		MetricsSD: MetricsSDConfig{Enabled: true, Token: "sd"},
	}).Handler()
	rec := f.request(t, http.MethodGet, "/api/v1/sd/agents", nil, nil, "sd")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "example.com") {
		t.Fatalf("fallback sd = %d body=%s", rec.Code, rec.Body.String())
	}
	missing := f.request(t, http.MethodGet, "/api/v1/agents/missing/metrics/prometheus", nil, nil, "sd")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing metrics = %d", missing.Code)
	}
}

func TestMetricsSDUnavailableBranches(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	f.handler = New(Deps{
		Clusters: f.manager, Store: f.store, Auth: f.auth, Version: "test",
		MetricsSD: MetricsSDConfig{Enabled: true, Token: "sd"},
	}).Handler()
	rec := f.request(t, http.MethodGet, "/api/v1/sd/agents", nil, nil, "bad")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token = %d", rec.Code)
	}
	metrics := f.request(t, http.MethodGet, "/api/v1/agents/agent-1/metrics/prometheus", nil, nil, "sd")
	if metrics.Code != http.StatusServiceUnavailable {
		t.Fatalf("no telemetry = %d", metrics.Code)
	}

	f.handler = New(Deps{Auth: f.auth, MetricsSD: MetricsSDConfig{Enabled: true, Token: "sd"}}).Handler()
	sd := f.request(t, http.MethodGet, "/api/v1/sd/agents", nil, nil, "sd")
	if sd.Code != http.StatusServiceUnavailable {
		t.Fatalf("no store = %d", sd.Code)
	}
}

func TestMetricsSDForwardedScheme(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	now := time.Now().UTC()
	if err := f.store.UpsertAgent(context.Background(), &store.Agent{
		ID: "agent-1", Cluster: "c1", Member: "node-a", Status: store.AgentOnline,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	f.handler = New(Deps{
		Clusters: f.manager, Store: f.store, Auth: f.auth, Version: "test",
		MetricsSD: MetricsSDConfig{Enabled: true, Token: "sd"},
	}).Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sd/agents", nil)
	req.Host = "fleet.example:8443"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Authorization", "Bearer sd")
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"__scheme__":"https"`) ||
		!strings.Contains(rec.Body.String(), "fleet.example:8443") {
		t.Fatalf("forwarded sd = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMetricsSDStoreError(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	f.handler = New(Deps{
		Auth: f.auth, Store: f.store, Version: "test",
		MetricsSD: MetricsSDConfig{Enabled: true, Token: "sd"},
	}).Handler()
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	rec := f.request(t, http.MethodGet, "/api/v1/sd/agents", nil, nil, "sd")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("store error = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMetricsServiceDiscoveryDirectMode(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	now := time.Now().UTC()
	for _, agent := range []*store.Agent{
		{ID: "agent-1", Cluster: "c1", Member: "node-a", Status: store.AgentOnline,
			Facts:     `{"hostname":"node-a","extra":{"metrics_address":"198.51.100.10:9100"}}`,
			CreatedAt: now, UpdatedAt: now},
		{ID: "agent-2", Cluster: "c1", Member: "node-b", Status: store.AgentOnline,
			Facts: `{}`, CreatedAt: now, UpdatedAt: now},
	} {
		if err := f.store.UpsertAgent(context.Background(), agent); err != nil {
			t.Fatal(err)
		}
	}
	f.handler = New(Deps{
		Clusters: f.manager, Store: f.store, Auth: f.auth, Version: "test",
		MetricsSD: MetricsSDConfig{Enabled: true, Token: "sd", Mode: "direct"},
	}).Handler()
	rec := f.request(t, http.MethodGet, "/api/v1/sd/agents", nil, nil, "sd")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"198.51.100.10:9100"`) ||
		!strings.Contains(rec.Body.String(), `"__metrics_path__":"/metrics"`) ||
		strings.Contains(rec.Body.String(), "agent-2") {
		t.Fatalf("direct sd = %d body=%s", rec.Code, rec.Body.String())
	}
	if metricsAddress("bad") != "" || metricsAddress(`{"extra":{"metrics_address":"x:1"}}`) != "x:1" {
		t.Fatal("metricsAddress() mismatch")
	}
}
