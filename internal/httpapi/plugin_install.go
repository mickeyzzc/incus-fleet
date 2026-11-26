package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/mickeyzzc/incus-fleet/internal/auth"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

// pluginCatalogItem describes a first-party plugin that can be connected with
// one click. The install step only registers an existing upstream; it does not
// deploy a VM or change the operator's services.
type pluginCatalogItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	UpstreamHint string `json:"upstream_hint"`
	UIPath       string `json:"ui_path"`
	Wiring       string `json:"wiring"`
}

var builtinPluginCatalog = []pluginCatalogItem{
	{
		ID:           "vm",
		Name:         "VictoriaMetrics",
		Description:  "Connects the existing VictoriaMetrics UI and query API into the plugin menu.",
		UpstreamHint: "http://127.0.0.1:8428",
		UIPath:       "/plugin-ui/vm/",
		Wiring:       "metrics.query_url",
	},
	{
		ID:           "vmlogs",
		Name:         "VictoriaLogs",
		Description:  "Connects the existing VictoriaLogs query UI into the plugin menu.",
		UpstreamHint: "http://127.0.0.1:9428",
		UIPath:       "/plugin-ui/vmlogs/",
		Wiring:       "logs.query_url",
	},
}

type pluginInstallRequest struct {
	ID       string `json:"id"`
	Upstream string `json:"upstream"`
}

type pluginToggleRequest struct {
	Enabled bool `json:"enabled"`
}

func (a *API) listPluginCatalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"catalog": builtinPluginCatalog})
}

// installPlugin validates the catalog id and upstream, mints a scoped API token
// bound to the plugin, and persists the entry so it survives a restart.
func (a *API) installPlugin(w http.ResponseWriter, r *http.Request) {
	if !a.pluginsEnabled {
		writeError(w, http.StatusServiceUnavailable, "plugins_disabled", "plugin system is disabled")
		return
	}

	var req pluginInstallRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}

	item, ok := findCatalogItem(strings.TrimSpace(req.ID))
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown_plugin", "plugin is not in the catalog")
		return
	}

	upstream := strings.TrimSpace(req.Upstream)
	target, err := url.Parse(upstream)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		writeError(w, http.StatusBadRequest, "invalid_upstream", "upstream must be an absolute http(s) URL")
		return
	}
	upstream = strings.TrimRight(upstream, "/")

	if _, err := a.store.GetInstalledPlugin(r.Context(), item.ID); err == nil {
		writeError(w, http.StatusConflict, "already_installed", "plugin is already installed")
		return
	} else if !errors.Is(err, store.ErrNotFound) {
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
		a.serviceError(w, r, err)
		return
	}

	entry := &store.InstalledPlugin{
		ID:       item.ID,
		Name:     item.Name,
		Upstream: upstream,
		UIPath:   item.UIPath,
		Role:     "viewer",
		Enabled:  true,
		TokenID:  token.ID,
	}
	if err := a.store.CreateInstalledPlugin(r.Context(), entry); err != nil {
		_ = a.auth.RevokePluginTokens(context.Background(), item.ID)
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"plugin": toPluginView(installedEntry(entry)),
		// Shown once: store only the token id, never the secret.
		"token": secret,
	})
}

func (a *API) uninstallPlugin(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if _, err := a.store.GetInstalledPlugin(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "plugin is not installed")
			return
		}
		a.serviceError(w, r, err)
		return
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

func findCatalogItem(id string) (pluginCatalogItem, bool) {
	for _, item := range builtinPluginCatalog {
		if item.ID == id {
			return item, true
		}
	}

	return pluginCatalogItem{}, false
}

func installedEntry(p *store.InstalledPlugin) config.PluginEntry {
	item, _ := findCatalogItem(p.ID)
	return config.PluginEntry{
		ID:       p.ID,
		Enabled:  p.Enabled,
		Upstream: p.Upstream,
		Role:     p.Role,
		UI: config.PluginUIManifest{
			Path:         p.UIPath,
			TitleKey:     item.Name,
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
