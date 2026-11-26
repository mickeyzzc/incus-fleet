package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/auth"
	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/instances"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

// pluginCatalogItem describes a service that can be deployed with one click.
// Items that carry Image + InstallScript create their own instance; items
// without them fall back to connecting an existing upstream.
type pluginCatalogItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	UIPath       string `json:"ui_path"`
	Wiring       string `json:"wiring"`
	UpstreamHint string `json:"upstream_hint,omitempty"`
	Image        string `json:"image,omitempty"`
	DefaultCPU   string `json:"default_cpu,omitempty"`
	DefaultMem   string `json:"default_memory,omitempty"`
	DefaultDisk  string `json:"default_disk,omitempty"`
	Network      string `json:"default_network,omitempty"`
	Port         int    `json:"port,omitempty"`
	HealthPath   string `json:"health_path,omitempty"`
	// ArtifactURL is downloaded by the Fleet server and pushed into the
	// instance, so guests do not need direct internet access.
	ArtifactURL string `json:"artifact_url,omitempty"`
	// InstallScriptFile is a path relative to the manifest, used by
	// third-party plugin bundles. Built-ins use InstallHook directly.
	InstallScriptFile string `json:"install_script_file,omitempty"`
	InstallHook       string `json:"-"`
}

var builtinPluginCatalog = []pluginCatalogItem{
	{
		ID:          "vm",
		Name:        "VictoriaMetrics",
		Description: "Deploys the official VictoriaMetrics service and wires fleet's metrics queries and charts to it.",
		UIPath:      "/plugin/vm",
		Wiring:      "metrics.query_url",
		Image:       "vm-oci",
		DefaultCPU:  "2",
		DefaultMem:  "2GiB",
		DefaultDisk: "20GiB",
		Network:     "incusbr0",
		Port:        8428,
		HealthPath:  "/health",
	},
	{
		ID:          "vmlogs",
		Name:        "VictoriaLogs",
		Description: "Deploys the official VictoriaLogs service and exposes its query UI in the plugin menu.",
		UIPath:      "/plugin/vmlogs",
		Wiring:      "logs.query_url",
		Image:       "vmlogs-oci",
		DefaultCPU:  "2",
		DefaultMem:  "2GiB",
		DefaultDisk: "20GiB",
		Network:     "incusbr0",
		Port:        9428,
		HealthPath:  "/health",
	},
}

type pluginInstallRequest struct {
	ID       string `json:"id"`
	Cluster  string `json:"cluster,omitempty"`
	Member   string `json:"member,omitempty"`
	Name     string `json:"name,omitempty"`
	CPU      string `json:"cpu,omitempty"`
	Memory   string `json:"memory,omitempty"`
	Disk     string `json:"disk,omitempty"`
	Network  string `json:"network,omitempty"`
	Upstream string `json:"upstream,omitempty"`
}

type pluginToggleRequest struct {
	Enabled bool `json:"enabled"`
}

func (a *API) listPluginCatalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"catalog": a.pluginCatalog()})
}

// pluginCatalog merges the built-in services with third-party manifests placed
// in plugins.dir. Each manifest is a plugin.json plus optional install script.
func (a *API) pluginCatalog() []pluginCatalogItem {
	items := append([]pluginCatalogItem{}, builtinPluginCatalog...)
	dir := strings.TrimSpace(a.pluginsConfig.Dir)
	if dir == "" {
		return items
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return items
	}
	seen := map[string]struct{}{}
	for _, item := range items {
		seen[item.ID] = struct{}{}
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path) //nolint:gosec // operator-provided plugin dir
		if err != nil {
			continue
		}
		var item pluginCatalogItem
		if err := json.Unmarshal(data, &item); err != nil || item.ID == "" || item.Name == "" {
			continue
		}
		if _, exists := seen[item.ID]; exists {
			continue
		}
		if item.InstallScriptFile != "" {
			scriptPath := filepath.Join(filepath.Dir(path), item.InstallScriptFile)
			script, err := os.ReadFile(scriptPath) //nolint:gosec // constrained to manifest dir
			if err != nil {
				continue
			}
			item.InstallHook = string(script)
		}
		seen[item.ID] = struct{}{}
		items = append(items, item)
	}

	return items
}

// installPlugin deploys a catalog service on the least-loaded member (unless a
// member was chosen), runs its install hook, and persists the entry and token.
func (a *API) installPlugin(w http.ResponseWriter, r *http.Request) {
	if !a.pluginsEnabled {
		writeError(w, http.StatusServiceUnavailable, "plugins_disabled", "plugin system is disabled")
		return
	}

	var req pluginInstallRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}

	item, ok := a.findCatalogItem(strings.TrimSpace(req.ID))
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown_plugin", "plugin is not in the catalog")
		return
	}

	if _, err := a.store.GetInstalledPlugin(r.Context(), item.ID); err == nil {
		writeError(w, http.StatusConflict, "already_installed", "plugin is already installed")
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		a.serviceError(w, r, err)
		return
	}

	clusterName, _, upstream, instanceName, err := a.provisionPlugin(r.Context(), item, req)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	session, _ := sessionFromContext(r.Context())
	token, secret, err := a.auth.CreateAPIToken(r.Context(), auth.CreateTokenRequest{
		Name:     "plugin-" + item.ID,
		UserID:   session.UserID,
		Username: session.Username,
		Scopes:   []auth.Scope{auth.ScopeReadPlugins},
		PluginID: item.ID,
		TTL:      0,
	})
	if err != nil {
		_ = a.instances.Delete(context.Background(), clusterName, instanceName, true, true)
		a.serviceError(w, r, err)
		return
	}

	entry := &store.InstalledPlugin{
		ID:       item.ID,
		Name:     item.Name,
		Upstream: upstream,
		UIPath:   item.UIPath,
		Role:     "viewer",
		Cluster:  clusterName,
		Instance: instanceName,
		Enabled:  true,
		TokenID:  token.ID,
	}
	if err := a.store.CreateInstalledPlugin(r.Context(), entry); err != nil {
		_ = a.auth.RevokePluginTokens(context.Background(), item.ID)
		if instanceName != "" {
			_ = a.instances.Delete(context.Background(), clusterName, instanceName, true, true)
		}
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"plugin": toPluginView(installedEntry(entry)),
		"token":  secret,
	})
}

// provisionPlugin either deploys the bundle's instance or, for connect-only
// entries, validates and returns the supplied upstream.
func (a *API) provisionPlugin(
	ctx context.Context,
	item pluginCatalogItem,
	req pluginInstallRequest,
) (clusterName, member, upstream, instanceName string, err error) {
	if item.Image == "" || strings.TrimSpace(req.Upstream) != "" {
		upstream = strings.TrimSpace(req.Upstream)
		target, parseErr := url.Parse(upstream)
		if parseErr != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
			return "", "", "", "", fmt.Errorf("%w: upstream must be an absolute http(s) URL", instances.ErrInvalidRequest)
		}
		return "", "", strings.TrimRight(upstream, "/"), "", nil
	}

	requestedCluster := strings.TrimSpace(req.Cluster)
	clusterName = ""
	for _, summary := range a.clusters.List() {
		if !summary.Online {
			continue
		}
		if requestedCluster == "" || summary.Name == requestedCluster {
			clusterName = summary.Name
			break
		}
	}
	if clusterName == "" {
		return "", "", "", "", fmt.Errorf("%w: no online cluster found", instances.ErrInvalidRequest)
	}

	member = strings.TrimSpace(req.Member)
	if member == "" {
		member = pickPluginMember(a, clusterName)
	}

	cpu := firstNonEmpty(req.CPU, item.DefaultCPU, "2")
	memory := firstNonEmpty(req.Memory, item.DefaultMem, "2GiB")
	disk := firstNonEmpty(req.Disk, item.DefaultDisk, "20GiB")
	network := firstNonEmpty(req.Network, item.Network, "incusbr0")

	instanceName = strings.TrimSpace(req.Name)
	if instanceName == "" {
		instanceName = "fleet-" + item.ID
	}

	fingerprint, err := a.resolvePluginImage(clusterName, item.Image)
	if err != nil {
		return "", "", "", "", err
	}

	start := true
	_, err = a.instances.Create(ctx, clusterName, instances.CreateRequest{
		Name:         instanceName,
		Type:         "container",
		Image:        fingerprint,
		CPU:          cpu,
		Memory:       memory,
		RootDiskSize: disk,
		StoragePool:  "default",
		Network:      network,
		Member:       member,
		Start:        &start,
	})
	if err != nil {
		return "", "", "", "", err
	}

	cleanup := func() {
		_ = a.instances.Delete(context.Background(), clusterName, instanceName, true, true)
	}

	ip, err := a.waitPluginIP(ctx, clusterName, instanceName, 90*time.Second)
	if err != nil {
		cleanup()
		return "", "", "", "", err
	}

	if item.ArtifactURL != "" {
		artifact, err := downloadPluginArtifact(ctx, item.ArtifactURL)
		if err != nil {
			cleanup()
			return "", "", "", "", err
		}
		if err := a.instances.UploadFile(clusterName, instanceName, "/tmp/plugin-artifact.tar.gz", artifact); err != nil {
			cleanup()
			return "", "", "", "", fmt.Errorf("instances: push plugin artifact: %w", err)
		}
	}

	if item.InstallHook != "" {
		result, err := a.instances.RunOps(ctx, clusterName, instanceName, instances.OpsRequest{
			Action: "run-script",
			Script: item.InstallHook,
		})
		if err != nil || result.ExitCode != 0 {
			a.logger.Warn("plugin install hook failed",
				"plugin", item.ID, "instance", instanceName,
				"error", err, "exit", result.ExitCode,
				"stderr", truncateForLog(result.Stderr, 2000))
			cleanup()
			return "", "", "", "", fmt.Errorf("%w: install hook failed: %v %s", instances.ErrInvalidRequest, err, result.Stderr)
		}
	}

	upstream = fmt.Sprintf("http://%s:%d", ip, item.Port)

	return clusterName, member, upstream, instanceName, nil
}

// resolvePluginImage accepts an alias or fingerprint and returns the
// fingerprint Create expects.
func (a *API) resolvePluginImage(clusterName, ref string) (string, error) {
	conn, err := a.clusters.Connection(clusterName)
	if err != nil {
		return "", err
	}
	client, err := conn.Client()
	if err != nil {
		return "", err
	}
	images, err := client.GetImages()
	if err != nil {
		return "", fmt.Errorf("instances: list images for %s: %w", clusterName, err)
	}
	for _, image := range images {
		if image.Fingerprint == ref {
			return image.Fingerprint, nil
		}
		for _, alias := range image.Aliases {
			if alias.Name == ref {
				return image.Fingerprint, nil
			}
		}
	}

	return "", fmt.Errorf("%w: image %q is not available on %s", instances.ErrInvalidRequest, ref, clusterName)
}

func (a *API) waitPluginIP(ctx context.Context, clusterName, instanceName string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		detail, err := a.instances.Get(clusterName, instanceName)
		if err == nil {
			for _, address := range detail.IPv4 {
				if trimmed := strings.TrimSpace(address); trimmed != "" {
					return trimmed, nil
				}
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("%w: instance %s did not get an address", instances.ErrInvalidRequest, instanceName)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// pickPluginMember chooses the online member with the lowest memory/storage
// pressure, a deliberately small scheduler rather than a full k8s one.
func pickPluginMember(a *API, clusterName string) string {
	for _, summary := range a.clusters.List() {
		if summary.Name == clusterName {
			return pickLeastLoadedMember(summary.Members)
		}
	}

	return ""
}

// pickLeastLoadedMember returns the online member with the lowest maximum of
// memory and storage pressure. It is the whole scheduler on purpose.
func pickLeastLoadedMember(members []clusters.Member) string {
	best := ""
	bestScore := math.MaxFloat64
	for _, member := range members {
		if member.Status != "Online" {
			continue
		}
		mem := 0.0
		if member.Memory.Total > 0 {
			mem = float64(member.Memory.Used) / float64(member.Memory.Total)
		}
		storage := 0.0
		if member.Storage.Total > 0 {
			storage = float64(member.Storage.Used) / float64(member.Storage.Total)
		}
		score := math.Max(mem, storage)
		if score < bestScore {
			bestScore = score
			best = member.Name
		}
	}

	return best
}

// downloadPluginArtifact fetches a plugin archive once on the Fleet server.
// The bytes are capped so a malicious manifest cannot exhaust memory.
func downloadPluginArtifact(ctx context.Context, artifactURL string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, artifactURL, nil)
	if err != nil {
		return nil, fmt.Errorf("plugin artifact: invalid URL: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("plugin artifact: download failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("plugin artifact: upstream returned %d", resp.StatusCode)
	}

	const maxArtifactBytes = 128 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxArtifactBytes+1))
	if err != nil {
		return nil, fmt.Errorf("plugin artifact: read failed: %w", err)
	}
	if len(data) > maxArtifactBytes {
		return nil, fmt.Errorf("plugin artifact: larger than %d bytes", maxArtifactBytes)
	}

	return data, nil
}

func truncateForLog(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (a *API) uninstallPlugin(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	entry, err := a.store.GetInstalledPlugin(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "plugin is not installed")
			return
		}
		a.serviceError(w, r, err)
		return
	}

	if entry.Cluster != "" && entry.Instance != "" {
		if err := a.instances.Delete(r.Context(), entry.Cluster, entry.Instance, true, true); err != nil {
			a.serviceError(w, r, err)
			return
		}
	}

	if err := a.store.DeleteInstalledPlugin(r.Context(), id); err != nil {
		a.serviceError(w, r, err)
		return
	}
	if err := a.auth.RevokePluginTokens(r.Context(), id); err != nil {
		a.logger.Warn("revoke plugin token failed", "plugin", id, "error", err)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) togglePlugin(w http.ResponseWriter, r *http.Request) {
	var req pluginToggleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))
	entry, err := a.store.GetInstalledPlugin(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "plugin is not installed")
			return
		}
		a.serviceError(w, r, err)
		return
	}

	entry.Enabled = req.Enabled
	if err := a.store.UpdateInstalledPlugin(r.Context(), entry); err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, toPluginView(installedEntry(entry)))
}

func (a *API) findCatalogItem(id string) (pluginCatalogItem, bool) {
	for _, item := range a.pluginCatalog() {
		if item.ID == id {
			return item, true
		}
	}

	return pluginCatalogItem{}, false
}

func installedEntry(p *store.InstalledPlugin) config.PluginEntry {
	uiPath := "/plugin/" + p.ID

	return config.PluginEntry{
		ID:       p.ID,
		Enabled:  p.Enabled,
		Upstream: p.Upstream,
		Role:     p.Role,
		UI: config.PluginUIManifest{
			Path:         uiPath,
			TitleKey:     p.Name,
			RequiresRole: p.Role,
		},
	}
}

// installedPluginEntries loads web-installed plugins so they appear in the
// manager, navigation and proxy just like YAML-configured ones.
func (a *API) installedPluginEntries() []config.PluginEntry {
	if !a.pluginsEnabled || a.store == nil {
		return nil
	}

	entries, err := a.store.ListInstalledPlugins(context.Background())
	if err != nil {
		a.logger.Warn("list installed plugins failed", "error", err)
		return nil
	}

	out := make([]config.PluginEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, installedEntry(entry))
	}

	return out
}
