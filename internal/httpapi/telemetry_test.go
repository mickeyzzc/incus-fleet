package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/agentpb"
	"github.com/mickeyzzc/incus-fleet/internal/telemetry"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestTelemetryQuerySnapshotAndExport(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	store := telemetry.New(10)
	now := time.Now().UTC()
	store.Ingest(&agentpb.TelemetryBatch{AgentId: "agent-1", Series: []*agentpb.Series{{
		Name: "fleet_host_load", Type: agentpb.MetricType_METRIC_TYPE_GAUGE,
		Labels:  map[string]string{"window": "1"},
		Samples: []*agentpb.Sample{{AtMs: now.UnixMilli(), Value: 1.25}},
	}}}, "c1", "node-a")
	f.handler = New(Deps{
		Clusters: f.manager, Store: f.store, Telemetry: store, Auth: f.auth, Version: "test",
	}).Handler()
	cookie := f.login(t)

	query := f.request(t, http.MethodGet, "/api/v1/metrics/query?cluster=c1&member=node-a", nil, cookie, "")
	if query.Code != http.StatusOK || !strings.Contains(query.Body.String(), "fleet_host_load") {
		t.Fatalf("query = %d body=%s", query.Code, query.Body.String())
	}
	snapshot := f.request(t, http.MethodGet, "/api/v1/metrics/snapshot?cluster=c1", nil, cookie, "")
	if snapshot.Code != http.StatusOK || !strings.Contains(snapshot.Body.String(), "1.25") {
		t.Fatalf("snapshot = %d body=%s", snapshot.Code, snapshot.Body.String())
	}
	prom := f.request(t, http.MethodGet, "/api/v1/metrics/export?format=prometheus&cluster=c1", nil, cookie, "")
	if prom.Code != http.StatusOK || !strings.Contains(prom.Body.String(), `window="1"`) {
		t.Fatalf("prom export = %d body=%s", prom.Code, prom.Body.String())
	}
	csv := f.request(t, http.MethodGet, "/api/v1/metrics/export?format=csv&cluster=c1", nil, cookie, "")
	if csv.Code != http.StatusOK || !strings.Contains(csv.Body.String(), "timestamp,metric,cluster") {
		t.Fatalf("csv export = %d body=%s", csv.Code, csv.Body.String())
	}
	bad := f.request(t, http.MethodGet, "/api/v1/metrics/query?from=bad", nil, cookie, "")
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad time = %d body=%s", bad.Code, bad.Body.String())
	}
}

func TestTelemetryDisabled(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)
	rec := f.request(t, http.MethodGet, "/api/v1/metrics/query", nil, cookie, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTelemetryValidationAndFormats(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	store := telemetry.New(2)
	now := time.Now().UTC()
	store.Ingest(&agentpb.TelemetryBatch{AgentId: "agent-1", Series: []*agentpb.Series{{
		Name: "fleet_host_load", Samples: []*agentpb.Sample{{AtMs: now.UnixMilli(), Value: 1}},
	}}}, "c1", "node-a")
	f.handler = New(Deps{Clusters: f.manager, Store: f.store, Telemetry: store, Auth: f.auth, Version: "test"}).Handler()
	cookie := f.login(t)

	badFormat := f.request(t, http.MethodGet, "/api/v1/metrics/export?format=yaml", nil, cookie, "")
	if badFormat.Code != http.StatusBadRequest {
		t.Fatalf("bad format = %d", badFormat.Code)
	}
	reversed := f.request(t, http.MethodGet, "/api/v1/metrics/query?from=1h&to=2h", nil, cookie, "")
	if reversed.Code != http.StatusBadRequest {
		t.Fatalf("reversed = %d body=%s", reversed.Code, reversed.Body.String())
	}
	unix := f.request(t, http.MethodGet, "/api/v1/metrics/query?from="+strconv.FormatInt(now.Add(-time.Hour).Unix(), 10), nil, cookie, "")
	if unix.Code != http.StatusOK {
		t.Fatalf("unix time = %d body=%s", unix.Code, unix.Body.String())
	}
	invalid := f.request(t, http.MethodGet, "/api/v1/metrics/snapshot?from=bad", nil, cookie, "")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("bad snapshot time = %d", invalid.Code)
	}
}
