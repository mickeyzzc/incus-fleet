package telemetry

import (
	"strings"
	"testing"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/agentpb"
)

func testBatch(at time.Time) *agentpb.TelemetryBatch {
	return &agentpb.TelemetryBatch{
		AgentId: "agent-1", IntervalMs: 10000,
		Series: []*agentpb.Series{
			{Name: "fleet_host_cpu_usage_ratio", Type: agentpb.MetricType_METRIC_TYPE_GAUGE,
				Labels:  map[string]string{"core": "all"},
				Samples: []*agentpb.Sample{{AtMs: at.UnixMilli(), Value: 0.25}}},
			{Name: "fleet_host_network_bytes_total", Type: agentpb.MetricType_METRIC_TYPE_COUNTER,
				Labels:  map[string]string{"device": "eth0", "direction": "rx"},
				Samples: []*agentpb.Sample{{AtMs: at.UnixMilli(), Value: 100}}},
			{Name: "bad_name", Samples: []*agentpb.Sample{{AtMs: at.UnixMilli(), Value: 1}}},
		},
	}
}

func TestIngestQueryAndSnapshot(t *testing.T) {
	store := New(10)
	now := time.Now().UTC().Truncate(time.Second)
	if added := store.Ingest(testBatch(now), "c1", "node-a"); added != 2 {
		t.Fatalf("Ingest() added = %d", added)
	}
	if store.Len() != 2 {
		t.Fatalf("Len() = %d", store.Len())
	}

	series := store.Query(Filter{Cluster: "c1", Member: "node-a"})
	if len(series) != 2 {
		t.Fatalf("Query() = %#v", series)
	}
	if series[0].Labels["agent_id"] != "agent-1" || series[0].Labels["cluster"] != "c1" {
		t.Fatalf("labels = %#v", series[0].Labels)
	}
	if len(series[0].Points) != 1 || series[0].Points[0].Value != 0.25 {
		t.Fatalf("points = %#v", series[0].Points)
	}

	filtered := store.Query(Filter{Cluster: "c1", Metric: "fleet_host_network"})
	if len(filtered) != 1 || filtered[0].Name != "fleet_host_network_bytes_total" {
		t.Fatalf("filtered = %#v", filtered)
	}
	snapshot := store.Snapshot(Filter{Cluster: "c1"})
	if len(snapshot) != 2 || len(snapshot[0].Points) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}

	// Out-of-order and invalid labels are ignored.
	store.Ingest(&agentpb.TelemetryBatch{AgentId: "agent-1", Series: []*agentpb.Series{{
		Name: "fleet_host_cpu_usage_ratio", Labels: map[string]string{"core": "all"},
		Samples: []*agentpb.Sample{{AtMs: now.Add(-time.Minute).UnixMilli(), Value: 1}},
	}}}, "c1", "node-a")
	if got := store.Query(Filter{Cluster: "c1", Metric: "fleet_host_cpu"}); got[0].Points[0].Value != 0.25 {
		t.Fatalf("out-of-order sample accepted: %#v", got)
	}
}

func TestStoreLimitAndExports(t *testing.T) {
	store := New(1)
	now := time.Now().UTC()
	store.Ingest(testBatch(now), "c1", "node-a")
	store.Ingest(&agentpb.TelemetryBatch{AgentId: "agent-1", Series: []*agentpb.Series{{
		Name: "fleet_host_load", Samples: []*agentpb.Sample{{AtMs: now.UnixMilli(), Value: 1}},
	}}}, "c1", "node-a")
	if store.Len() != 1 {
		t.Fatalf("limit ignored: %d", store.Len())
	}

	series := store.Query(Filter{Cluster: "c1"})
	text := Prometheus(series)
	if !strings.Contains(text, "# TYPE fleet_host_cpu_usage_ratio gauge") ||
		!strings.Contains(text, `cluster="c1"`) {
		t.Fatalf("Prometheus() = %s", text)
	}
	csvText, err := CSV(series)
	if err != nil || !strings.Contains(csvText, "timestamp,metric,cluster,member,agent_id,value") {
		t.Fatalf("CSV() = %q, %v", csvText, err)
	}
}

func TestValidate(t *testing.T) {
	if err := Validate(nil); err == nil {
		t.Fatal("Validate(nil) expected error")
	}
	if err := Validate(&agentpb.TelemetryBatch{AgentId: "a", Series: []*agentpb.Series{{Name: "fleet_x"}}}); err != nil {
		t.Fatalf("Validate(valid) error = %v", err)
	}
	if err := Validate(&agentpb.TelemetryBatch{AgentId: "a", Series: []*agentpb.Series{{Name: "x"}}}); err == nil {
		t.Fatal("Validate(bad name) expected error")
	}
}

func TestRingBoundsAndValidationHelpers(t *testing.T) {
	if New(0).maxSeries != DefaultMaxSeries || New(-1).maxSeries != DefaultMaxSeries {
		t.Fatal("New() default maxSeries not applied")
	}
	if !validName("fleet_ok") || validName("other") || validName("fleet_bad-name") || validName("1fleet_x") {
		t.Fatal("validName() mismatch")
	}
	labels := sanitizeLabels(map[string]string{"ok": "value", "": "bad", "long": strings.Repeat("x", 300)})
	if len(labels) != 1 {
		t.Fatalf("sanitizeLabels() = %#v", labels)
	}

	store := New(10)
	now := time.Now().UTC()
	for i := 0; i < Points+5; i++ {
		store.Ingest(&agentpb.TelemetryBatch{AgentId: "a", Series: []*agentpb.Series{{
			Name:    "fleet_host_load",
			Samples: []*agentpb.Sample{{AtMs: now.Add(time.Duration(i) * time.Second).UnixMilli(), Value: float64(i)}},
		}}}, "c1", "node-a")
	}
	series := store.Query(Filter{Cluster: "c1"})
	if len(series) != 1 || len(series[0].Points) > Points {
		t.Fatalf("ring bounds = %#v", series)
	}
	if len(store.Query(Filter{})) != 1 {
		t.Fatal("empty filter should match")
	}
}
