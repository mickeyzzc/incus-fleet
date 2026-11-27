package httpapi

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/instances"
	"github.com/mickeyzzc/incus-fleet/internal/plugin"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

type pluginConfigRequest struct {
	Config map[string]string `json:"config"`
}

// putPluginConfig saves operator-edited template values and re-injects them
// into a running instance when one was created by fleet. Keys must be declared
// by the plugin manifest and values must pass the manifest's type validation.
func (a *API) putPluginConfig(w http.ResponseWriter, r *http.Request) {
	if !a.requirePluginManager(w) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	p, ok := a.registry.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "plugin is not registered")
		return
	}
	var req pluginConfigRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if len(req.Config) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "at least one config value is required")
		return
	}

	manifest := p.Manifest()
	merged := make(map[string]string, len(manifest.Install.Templates))
	for _, template := range manifest.Install.Templates {
		merged[template.Name] = template.Default
	}
	for key, value := range req.Config {
		merged[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	resolved, err := plugin.RenderTemplates(manifest, merged)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_config", err.Error())
		return
	}
	for key, value := range req.Config {
		if err := a.store.UpsertPluginConfig(r.Context(), id, strings.TrimSpace(key), strings.TrimSpace(value)); err != nil {
			a.serviceError(w, r, err)
			return
		}
	}

	// Apply the new values to the installed instance immediately. A connect-only
	// plugin has no instance, so there is nothing to re-inject.
	if entry, err := a.store.GetInstalledPlugin(r.Context(), id); err == nil &&
		entry.Cluster != "" && entry.Instance != "" && a.instances != nil {
		script := plugin.ConfigScript(resolved)
		if err := a.instances.UploadFile(entry.Cluster, entry.Instance, "/tmp/fleet-plugin-config.sh", []byte(script)); err != nil {
			a.logger.Warn("plugin config upload failed", "plugin", id, "error", err)
		} else if result, err := a.instances.RunOps(r.Context(), entry.Cluster, entry.Instance, instances.OpsRequest{
			Action: "run-script", Script: "sh /tmp/fleet-plugin-config.sh",
		}); err != nil || result.ExitCode != 0 {
			a.logger.Warn("plugin config apply failed", "plugin", id, "error", err, "exit", result.ExitCode)
		}
	}

	a.recordPluginActivity(r, id, "plugin.config.update", strings.Join(sortedKeys(req.Config), ","))
	view, ok := a.pluginViewByID(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "plugin not found")
		return
	}
	if err := a.decoratePluginView(r, &view); err != nil {
		a.serviceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// deletePluginWiring restores one approved key to its captured static default,
// keeping the approval record so a reinstall does not need a second approval.
func (a *API) deletePluginWiring(w http.ResponseWriter, r *http.Request) {
	if !a.requirePluginManager(w) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	p, ok := a.registry.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "plugin is not registered")
		return
	}
	key := strings.TrimSpace(r.PathValue("key"))
	if !p.Manifest().Capabilities.DeclaresWiring(key) {
		writeError(w, http.StatusBadRequest, "undeclared_wiring_key", "wiring key is not declared by the plugin manifest")
		return
	}
	row, err := a.store.GetPluginWiring(r.Context(), id, key)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "wiring_not_found", "wiring key has no approved entry")
		return
	}
	if err != nil {
		a.serviceError(w, r, err)
		return
	}
	if !row.Approved {
		writeError(w, http.StatusForbidden, "wiring_not_approved", "wiring key was not approved at install time")
		return
	}
	if err := a.store.RestorePluginWiringDefault(r.Context(), id, key); err != nil {
		a.serviceError(w, r, err)
		return
	}
	if err := a.registry.DisableWiring(id, key); err != nil {
		a.serviceError(w, r, err)
		return
	}
	a.recordPluginActivity(r, id, "plugin.wiring.restore", key)
	a.registry.Publish(r.Context(), pluginEventWiringChanged(id, []string{key}))
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) pluginViewByID(id string) (pluginView, bool) {
	entry, ok := a.pluginEntryByID(id)
	if !ok {
		return pluginView{}, false
	}

	return toPluginView(entry), true
}

func (a *API) pluginEntryByID(id string) (config.PluginEntry, bool) {
	for _, entry := range a.pluginEntries() {
		if entry.ID == id {
			return entry, true
		}
	}

	return config.PluginEntry{}, false
}

func (a *API) recordPluginActivity(r *http.Request, pluginID, action, detail string) {
	session, hasSession := sessionFromContext(r.Context())
	actor := "admin"
	if hasSession {
		actor = session.Username
	}
	_ = a.store.RecordHostAudit(context.Background(), &store.HostAudit{
		ID: store.NewID(), At: time.Now().UTC(), Actor: actor,
		SourceIP: requestIP(r), Action: action, Result: "succeeded", Detail: detail,
	})
	kind := "delivery"
	if strings.HasSuffix(action, "restore") {
		kind = "info"
	}
	_ = a.store.RecordPluginEvent(context.Background(), &store.PluginEvent{
		PluginID: pluginID, Kind: kind, Message: detail,
	})
}

func sortedKeys(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)

	return out
}
