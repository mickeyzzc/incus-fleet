package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/plugin"
	"github.com/mickeyzzc/incus-fleet/internal/plugininstall"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

type pluginInstallTaskRequest struct {
	ID                     string            `json:"id"`
	Name                   string            `json:"name,omitempty"`
	Cluster                string            `json:"cluster,omitempty"`
	Member                 string            `json:"member,omitempty"`
	Instance               string            `json:"instance,omitempty"`
	Upstream               string            `json:"upstream,omitempty"`
	CPU                    string            `json:"cpu,omitempty"`
	Memory                 string            `json:"memory,omitempty"`
	Disk                   string            `json:"disk,omitempty"`
	Network                string            `json:"network,omitempty"`
	Values                 map[string]string `json:"values,omitempty"`
	DataDiskGB             int               `json:"data_disk_gb,omitempty"`
	Filesystem             string            `json:"filesystem,omitempty"`
	MountPoint             string            `json:"mount_point,omitempty"`
	StoragePool            string            `json:"storage_pool,omitempty"`
	Approvals              []string          `json:"approvals,omitempty"`
	Defaults               map[string]string `json:"defaults,omitempty"`
	HealthDelayMS          int               `json:"health_delay_ms,omitempty"`
	HealthIntervalMS       int               `json:"health_interval_ms,omitempty"`
	HealthFailureThreshold int               `json:"health_failure_threshold,omitempty"`
	Signature              string            `json:"signature,omitempty"`
}

func (a *API) submitPluginInstallTask(w http.ResponseWriter, r *http.Request) {
	if !a.requirePluginManager(w) || a.installer == nil {
		writeError(w, http.StatusServiceUnavailable, "installer_unavailable", "plugin installer is not configured")
		return
	}
	var req pluginInstallTaskRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	manifest, raw, err := a.pluginInstallManifest(strings.TrimSpace(req.ID))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}
	if manifest == nil {
		writeError(w, http.StatusBadRequest, "unknown_plugin", "plugin is not in the catalog")
		return
	}
	if a.signingKey() != nil && len(raw) > 0 && !plugin.VerifySignature(raw, req.Signature, a.signingKey()) {
		writeError(w, http.StatusForbidden, "invalid_signature", "plugin manifest signature is invalid")
		return
	}

	session, hasSession := sessionFromContext(r.Context())
	owner := "admin"
	if hasSession {
		owner = session.Username
	}
	task, err := a.installer.Enqueue(r.Context(), plugininstall.Request{
		PluginID: manifest.ID, Name: firstNonEmpty(req.Name, manifest.Name),
		Manifest: *manifest, Cluster: req.Cluster, Member: req.Member, Instance: req.Instance,
		Image: manifest.Artifact.Image.Reference, Network: req.Network,
		CPU: req.CPU, Memory: req.Memory, Disk: req.Disk, Upstream: req.Upstream,
		TemplateValues: req.Values, DataDiskGB: req.DataDiskGB,
		Filesystem: req.Filesystem, MountPoint: req.MountPoint, StoragePool: req.StoragePool,
		Approvals: req.Approvals, Defaults: req.Defaults,
		HealthDelayMS: req.HealthDelayMS, HealthIntervalMS: req.HealthIntervalMS,
		HealthFailureThreshold: req.HealthFailureThreshold,
		ManifestSignature:      req.Signature, SignedManifestBytes: raw,
	}, owner)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "install_in_progress", err.Error())
			return
		}
		a.serviceError(w, r, err)
		return
	}

	// Single-instance deployments execute the task inline so a queued install
	// does not need a separate HA dispatcher to make progress.
	if !a.taskWorker {
		go func(ctx context.Context) {
			_ = a.installer.RunTask(ctx, task.ID)
		}(context.WithoutCancel(r.Context()))
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"task": toPluginInstallTaskStatus(task)})
}

func (a *API) listPluginInstallTasks(w http.ResponseWriter, r *http.Request) {
	if !a.requirePluginManager(w) {
		return
	}
	tasks, err := a.store.ListHostTasks(r.Context(), "", "", 0)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}
	out := make([]pluginInstallTaskStatus, 0, len(tasks))
	for _, task := range tasks {
		if task.Type == plugininstall.TaskType {
			out = append(out, toPluginInstallTaskStatus(task))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": out})
}

func (a *API) getPluginInstallTask(w http.ResponseWriter, r *http.Request) {
	if !a.requirePluginManager(w) {
		return
	}
	task, err := a.store.GetHostTask(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "install task not found")
			return
		}
		a.serviceError(w, r, err)
		return
	}
	if task.Type != plugininstall.TaskType {
		writeError(w, http.StatusNotFound, "not_found", "install task not found")
		return
	}
	events, err := a.store.ListHostTaskEvents(r.Context(), task.ID)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}
	view := toPluginInstallTaskStatus(task)
	view.Events = events
	writeJSON(w, http.StatusOK, map[string]any{"task": view})
}

func (a *API) cancelPluginInstallTask(w http.ResponseWriter, r *http.Request) {
	if !a.requirePluginManager(w) || a.installer == nil {
		writeError(w, http.StatusServiceUnavailable, "installer_unavailable", "plugin installer is not configured")
		return
	}
	err := a.installer.Cancel(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "install task not found")
		return
	}
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "invalid_state", err.Error())
		return
	}
	if err != nil {
		a.serviceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) resumePluginInstallTask(w http.ResponseWriter, r *http.Request) {
	if !a.requirePluginManager(w) || a.installer == nil {
		writeError(w, http.StatusServiceUnavailable, "installer_unavailable", "plugin installer is not configured")
		return
	}
	err := a.installer.Resume(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "install task not found")
		return
	}
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "invalid_state", err.Error())
		return
	}
	if err != nil {
		a.serviceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": store.HostTaskQueued})
}

type pluginInstallTaskStatus struct {
	ID        string                 `json:"id"`
	PluginID  string                 `json:"plugin_id"`
	State     string                 `json:"state"`
	Progress  string                 `json:"progress,omitempty"`
	Attempt   int                    `json:"attempt"`
	Error     string                 `json:"error,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
	Events    []*store.HostTaskEvent `json:"events,omitempty"`
}

// pluginInstallManifest resolves a catalog id to a validated manifest and the
// exact bytes a signature covers. Built-in plugins carry no signed package.
func (a *API) pluginInstallManifest(id string) (*plugin.Manifest, []byte, error) {
	if id == "" {
		return nil, nil, nil
	}
	if m, ok := plugin.BuiltinManifest(id); ok {
		return &m, nil, nil
	}
	dir := strings.TrimSpace(a.pluginsConfig.Dir)
	if dir == "" {
		return nil, nil, nil
	}
	path := filepath.Join(dir, id, "manifest.yaml")
	raw, err := os.ReadFile(path) //nolint:gosec // operator-provided plugin directory
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("plugin manifest: %w", err)
	}
	m, err := plugin.ParseManifest(raw)
	if err != nil {
		return nil, nil, err
	}

	return m, raw, nil
}

func (a *API) signingKey() []byte {
	if key := strings.TrimSpace(a.pluginsConfig.SigningKey); key != "" {
		return []byte(key)
	}
	path := strings.TrimSpace(a.pluginsConfig.SigningKeyFile)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path) //nolint:gosec // operator-provided signing key path
	if err != nil {
		return nil
	}

	return []byte(strings.TrimSpace(string(data)))
}

func toPluginInstallTaskStatus(task *store.HostTask) pluginInstallTaskStatus {
	var req struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal([]byte(task.Plan), &req)

	return pluginInstallTaskStatus{
		ID: task.ID, PluginID: req.ID, State: task.State, Progress: task.Progress,
		Attempt: task.Attempt, Error: task.Error,
		CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
	}
}
