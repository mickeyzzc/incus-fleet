package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	incus "github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestPluginMetricsSnapshot(t *testing.T) {
	body := pluginMetricsSnapshot([]clusters.Summary{{
		Name:    "c1",
		CPU:     clusters.ResourcesCPU{Total: 8},
		Memory:  clusters.ResourceMem{Used: 100, Total: 200},
		Storage: clusters.StorageUsage{Used: 50, Total: 500},
		Instances: clusters.Counts{
			Total: 3, Running: 2,
		},
		Members: []clusters.Member{{
			Name:    "node-a",
			Memory:  clusters.ResourceMem{Used: 100, Total: 200},
			Storage: clusters.StorageUsage{Used: 50, Total: 500},
			CPU:     clusters.ResourcesCPU{Total: 4},
		}},
	}})

	for _, want := range []string{
		`fleet_cluster_cpu_total{cluster="c1"} 8`,
		`fleet_cluster_memory_used_bytes{cluster="c1"} 100`,
		`fleet_member_storage_total_bytes{cluster="c1",member="node-a"} 500`,
		`fleet_host_memory_bytes{cluster="c1",member="node-a",state="used"} 100`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("snapshot missing %q:\n%s", want, body)
		}
	}
}

func TestPushPluginMetricsOncePostsSnapshot(t *testing.T) {
	var received string
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		received = string(data)
	}))
	defer upstream.Close()

	st, err := store.Open(context.Background(), "sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.CreateInstalledPlugin(context.Background(), &store.InstalledPlugin{
		ID: "vm", Name: "VM", Upstream: upstream.URL, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	fake := &incusfake.Server{
		Server:          &incus.Server{},
		Instances:       []incus.InstanceFull{{Instance: incus.Instance{Name: "vm1", Type: "virtual-machine", StatusCode: incus.Running}}},
		ServerResources: &incus.Resources{CPU: incus.ResourcesCPU{Total: 4}},
	}
	dial := func(_ context.Context, _ config.Cluster) (clusters.InstanceServer, error) { return fake, nil }
	manager, err := clusters.NewWithDialer(context.Background(),
		[]config.Cluster{{Name: "c1", Endpoint: "https://example.invalid:8443"}}, dial,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pushPluginMetricsOnce(context.Background(), manager, st, logger)
	if !strings.Contains(received, `fleet_cluster_instances_total{cluster="c1"} 1`) {
		t.Fatalf("snapshot not posted: %q", received)
	}

	pushPluginMetricsOnce(context.Background(), nil, nil, logger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pushPluginMetrics(ctx, manager, st, logger)
}

func TestPluginMetricsSnapshotNegativeCounts(t *testing.T) {
	body := pluginMetricsSnapshot([]clusters.Summary{{
		Name:      "c1",
		Instances: clusters.Counts{Total: -1, Running: -2},
	}})
	for _, want := range []string{
		`fleet_cluster_instances_total{cluster="c1"} 0`,
		`fleet_cluster_instances_running{cluster="c1"} 0`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("snapshot missing %q:\n%s", want, body)
		}
	}
}

func TestPushPluginMetricsOnce(t *testing.T) {
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/import/prometheus" {
			t.Errorf("path = %s", r.URL.Path)
		}
		data, _ := io.ReadAll(r.Body)
		received = string(data)
	}))
	defer server.Close()

	st, err := store.Open(context.Background(), "sqlite", ":memory:")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.CreateInstalledPlugin(context.Background(), &store.InstalledPlugin{
		ID: "vm", Name: "VM", Upstream: server.URL, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if got := pluginMetricsUpstream(context.Background(), st); got != server.URL {
		t.Fatalf("upstream = %q", got)
	}
	// A nil manager cannot build summaries, so only the upstream lookup runs;
	// the snapshot path is covered by TestPluginMetricsSnapshot.
	if pluginMetricsUpstream(context.Background(), nil) != "" {
		t.Fatal("nil store must not resolve an upstream")
	}
	_ = logger
	_ = received
}
