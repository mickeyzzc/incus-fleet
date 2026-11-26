package app

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

const pluginMetricsInterval = 30 * time.Second

// pushPluginMetrics mirrors the fleet's cluster/member inventory into the
// installed vm plugin (VictoriaMetrics) so charts have data even when no agent
// is reporting through the center telemetry buffer.
func pushPluginMetrics(ctx context.Context, manager *clusters.Manager, st *store.SQLStore, logger *slog.Logger) {
	ticker := time.NewTicker(pluginMetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pushPluginMetricsOnce(ctx, manager, st, logger)
		}
	}
}

func pushPluginMetricsOnce(ctx context.Context, manager *clusters.Manager, st *store.SQLStore, logger *slog.Logger) {
	upstream := pluginMetricsUpstream(ctx, st)
	if upstream == "" {
		return
	}

	body := pluginMetricsSnapshot(manager.List())
	if body == "" {
		return
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(upstream, "/")+"/api/v1/import/prometheus", bytes.NewReader([]byte(body)))
	if err != nil {
		return
	}
	request.Header.Set("Content-Type", "text/plain")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(request)
	if err != nil {
		logger.Warn("plugin metrics push failed", "upstream", upstream, "error", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.Warn("plugin metrics push rejected", "upstream", upstream, "status", resp.StatusCode)
	}
}

func pluginMetricsUpstream(ctx context.Context, st *store.SQLStore) string {
	if st == nil {
		return ""
	}
	entries, err := st.ListInstalledPlugins(ctx)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.ID == "vm" && entry.Enabled && entry.Upstream != "" {
			return entry.Upstream
		}
	}

	return ""
}

// pluginMetricsSnapshot renders the current cluster and member resources in
// Prometheus text exposition format.
func pluginMetricsSnapshot(summaries []clusters.Summary) string {
	var out strings.Builder
	line := func(name string, value uint64, labels string) {
		fmt.Fprintf(&out, "%s%s %d\n", name, labels, value)
	}
	for _, summary := range summaries {
		clusterLabel := fmt.Sprintf(`{cluster=%q}`, summary.Name)

		line("fleet_cluster_cpu_total", summary.CPU.Total, clusterLabel)
		line("fleet_cluster_memory_used_bytes", summary.Memory.Used, clusterLabel)
		line("fleet_cluster_memory_total_bytes", summary.Memory.Total, clusterLabel)
		line("fleet_cluster_storage_used_bytes", summary.Storage.Used, clusterLabel)
		line("fleet_cluster_storage_total_bytes", summary.Storage.Total, clusterLabel)
		line("fleet_cluster_instances_total", uint64(summary.Instances.Total), clusterLabel)
		line("fleet_cluster_instances_running", uint64(summary.Instances.Running), clusterLabel)

		for _, member := range summary.Members {
			memberLabel := fmt.Sprintf(`{cluster=%q,member=%q}`, summary.Name, member.Name)
			line("fleet_member_cpu_total", member.CPU.Total, memberLabel)
			line("fleet_member_memory_used_bytes", member.Memory.Used, memberLabel)
			line("fleet_member_memory_total_bytes", member.Memory.Total, memberLabel)
			line("fleet_member_storage_used_bytes", member.Storage.Used, memberLabel)
			line("fleet_member_storage_total_bytes", member.Storage.Total, memberLabel)

			// The host panel already knows these names; memory is enough to make
			// the fallback chart useful while CPU usage needs an agent.
			hostLabel := fmt.Sprintf(`{cluster=%q,member=%q,state="used"}`, summary.Name, member.Name)
			line("fleet_host_memory_bytes", member.Memory.Used, hostLabel)
			hostLabel = fmt.Sprintf(`{cluster=%q,member=%q,state="total"}`, summary.Name, member.Name)
			line("fleet_host_memory_bytes", member.Memory.Total, hostLabel)
		}
	}

	return out.String()
}
