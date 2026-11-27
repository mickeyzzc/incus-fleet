package httpapi

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/auth"
	"github.com/mickeyzzc/incus-fleet/internal/config"
)

// pluginView is the descriptor the plugin manager UI and router consume.
type pluginView struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Version    string       `json:"version"`
	Enabled    bool         `json:"enabled"`
	Upstream   string       `json:"upstream,omitempty"`
	Role       string       `json:"role"`
	UI         pluginUIView `json:"ui"`
	HealthPath string       `json:"health_path,omitempty"`
	Kind       string       `json:"kind"`
	// The detail view's tabs are populated only by GET /plugins/{id}; the
	// list endpoint stays cheap by omitting them.
	Config        map[string]string  `json:"config,omitempty"`
	Wiring        []pluginWiringView `json:"wiring,omitempty"`
	Events        []pluginEventView  `json:"events,omitempty"`
	HealthHistory []pluginHealthView `json:"health_history,omitempty"`
}

type pluginWiringView struct {
	Capability    string `json:"capability"`
	Target        string `json:"target"`
	DefaultTarget string `json:"default_target"`
	Key           string `json:"key,omitempty"`
}

type pluginEventView struct {
	At      string `json:"at"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type pluginHealthView struct {
	At     string `json:"at"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type pluginUIView struct {
	Path         string `json:"path,omitempty"`
	Icon         string `json:"icon,omitempty"`
	TitleKey     string `json:"title_key,omitempty"`
	RequiresRole string `json:"requires_role,omitempty"`
}

// registerPluginRoutes mounts the plugin manager API and the same-origin proxy.
// Plugin UI is served through fleet rather than framed cross-origin, because the
// CSP forbids external framing.
func (a *API) registerPluginRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/plugins", a.requireScope(auth.ScopeReadPlugins, http.HandlerFunc(a.listPlugins)))
	mux.Handle("GET /api/v1/plugins/catalog", a.requireScope(auth.ScopeReadPlugins, http.HandlerFunc(a.listPluginCatalog)))
	mux.Handle("GET /api/v1/plugins/{id}", a.requireScope(auth.ScopeReadPlugins, http.HandlerFunc(a.getPlugin)))
	mux.Handle("POST /api/v1/plugins/install", a.requireScope(auth.ScopeManagePlugins, http.HandlerFunc(a.installPlugin)))
	mux.Handle("POST /api/v1/plugins/{id}/toggle", a.requireScope(auth.ScopeManagePlugins, http.HandlerFunc(a.togglePlugin)))
	mux.Handle("GET /api/v1/plugins/{id}/wiring", a.requireScope(auth.ScopeReadPlugins, http.HandlerFunc(a.getPluginWiring)))
	mux.Handle("PUT /api/v1/plugins/{id}/wiring", a.requireScope(auth.ScopeManagePlugins, http.HandlerFunc(a.putPluginWiring)))
	mux.Handle("DELETE /api/v1/plugins/{id}/wiring/{key}", a.requireScope(auth.ScopeManagePlugins, http.HandlerFunc(a.deletePluginWiring)))
	mux.Handle("PUT /api/v1/plugins/{id}/config", a.requireScope(auth.ScopeManagePlugins, http.HandlerFunc(a.putPluginConfig)))
	mux.Handle("POST /api/v1/plugin-installs", a.requireScope(auth.ScopeManagePlugins, http.HandlerFunc(a.submitPluginInstallTask)))
	mux.Handle("GET /api/v1/plugin-installs", a.requireScope(auth.ScopeReadPlugins, http.HandlerFunc(a.listPluginInstallTasks)))
	mux.Handle("GET /api/v1/plugin-installs/{id}", a.requireScope(auth.ScopeReadPlugins, http.HandlerFunc(a.getPluginInstallTask)))
	mux.Handle("POST /api/v1/plugin-installs/{id}/cancel", a.requireScope(auth.ScopeManagePlugins, http.HandlerFunc(a.cancelPluginInstallTask)))
	mux.Handle("POST /api/v1/plugin-installs/{id}/resume", a.requireScope(auth.ScopeManagePlugins, http.HandlerFunc(a.resumePluginInstallTask)))
	mux.Handle("DELETE /api/v1/plugins/{id}", a.requireScope(auth.ScopeManagePlugins, http.HandlerFunc(a.uninstallPlugin)))

	// Proxy mount. Plugin UIs live under their own prefix, never under
	// /plugins/ — that path belongs to the manager page itself, and sharing it
	// would make a reload of /plugins/ return a JSON 404 instead of the SPA.
	mux.Handle("/plugin-ui/", a.requireScope(auth.ScopeReadPlugins, http.HandlerFunc(a.proxyPlugin)))
}

// listPlugins returns every configured plugin with its state.
func (a *API) listPlugins(w http.ResponseWriter, _ *http.Request) {
	entries := a.pluginEntries()
	out := make([]pluginView, 0, len(entries))
	for _, entry := range entries {
		out = append(out, toPluginView(entry))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"plugins": out,
		"enabled": a.pluginsEnabled,
	})
}

// getPlugin returns one plugin descriptor, including the data behind the
// detail view's configuration, wiring, events and health-history tabs.
func (a *API) getPlugin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	for _, entry := range a.pluginEntries() {
		if entry.ID == id {
			view := toPluginView(entry)
			if err := a.decoratePluginView(r, &view); err != nil {
				a.serviceError(w, r, err)
				return
			}
			writeJSON(w, http.StatusOK, view)

			return
		}
	}

	writeError(w, http.StatusNotFound, "not_found", "plugin not found")
}

func (a *API) decoratePluginView(r *http.Request, view *pluginView) error {
	if a.store == nil {
		return nil
	}
	ctx := r.Context()
	values, err := a.store.ListPluginConfig(ctx, view.ID)
	if err != nil {
		return err
	}
	if len(values) > 0 {
		view.Config = values
	}
	if a.registry != nil {
		if _, ok := a.registry.Get(view.ID); ok {
			entries, err := a.pluginWiringEntries(view.ID)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				capability := entry.Effect
				if capability == "" {
					capability = entry.Key
				}
				view.Wiring = append(view.Wiring, pluginWiringView{
					Capability: capability, Target: entry.Value,
					DefaultTarget: entry.DefaultValue, Key: entry.Key,
				})
			}
		}
	}
	events, err := a.store.ListPluginEvents(ctx, view.ID, 20)
	if err != nil {
		return err
	}
	for _, event := range events {
		view.Events = append(view.Events, pluginEventView{
			At: event.At.Format(time.RFC3339), Kind: event.Kind, Message: event.Message,
		})
	}
	health, err := a.store.ListPluginHealth(ctx, view.ID, 20)
	if err != nil {
		return err
	}
	for _, sample := range health {
		view.HealthHistory = append(view.HealthHistory, pluginHealthView{
			At: sample.At.Format(time.RFC3339), Status: sample.Status, Detail: sample.Detail,
		})
	}

	return nil
}

func (a *API) pluginEntries() []config.PluginEntry {
	if !a.pluginsEnabled {
		return nil
	}

	entries := append([]config.PluginEntry{}, a.pluginsConfig.Entries...)
	entries = append(entries, a.installedPluginEntries()...)
	entries = append(entries, a.registryPluginEntries(entries)...)

	return entries
}

// registryPluginEntries exposes descriptors that are registered but have no
// static YAML entry — first-party catalog plugins and restored community
// packages. Without this the manager cannot open their detail pages at all.
func (a *API) registryPluginEntries(existing []config.PluginEntry) []config.PluginEntry {
	if a.registry == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(existing))
	for _, entry := range existing {
		seen[entry.ID] = struct{}{}
	}
	out := make([]config.PluginEntry, 0)
	for _, manifest := range a.registry.Manifests() {
		if _, ok := seen[manifest.ID]; ok {
			continue
		}
		entry := config.PluginEntry{
			ID: manifest.ID, Enabled: true,
			Role: "viewer",
			UI: config.PluginUIManifest{
				Path: strings.TrimSuffix(manifest.Capabilities.ProxyUI.PathPrefix, "/"),
				Icon: manifest.ID, TitleKey: manifest.Name, RequiresRole: "viewer",
			},
		}
		out = append(out, entry)
	}

	return out
}

func toPluginView(entry config.PluginEntry) pluginView {
	return pluginView{
		ID:       entry.ID,
		Name:     displayName(entry),
		Version:  "",
		Enabled:  entry.Enabled,
		Upstream: entry.Upstream,
		Role:     entry.Role,
		UI: pluginUIView{
			Path:         entry.UI.Path,
			Icon:         entry.UI.Icon,
			TitleKey:     entry.UI.TitleKey,
			RequiresRole: entry.UI.RequiresRole,
		},
		Kind: "external",
	}
}

func displayName(entry config.PluginEntry) string {
	if entry.UI.TitleKey != "" {
		return entry.UI.TitleKey
	}

	return entry.ID
}

// proxyPlugin forwards a request to the plugin upstream that owns the path.
func (a *API) proxyPlugin(w http.ResponseWriter, r *http.Request) {
	if !a.pluginsEnabled {
		writeError(w, http.StatusNotFound, "not_found", "plugin system is disabled")
		return
	}

	entry, prefix, ok := a.matchPlugin(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no plugin serves this path")
		return
	}

	target, err := url.Parse(entry.Upstream)
	if err != nil || target.Host == "" {
		writeError(w, http.StatusBadGateway, "bad_upstream", "plugin upstream is not a valid URL")
		return
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = target.Scheme
			pr.Out.URL.Host = target.Host
			pr.Out.URL.Path = trimPrefixPath(pr.In.URL.Path, prefix)
			pr.Out.URL.RawQuery = pr.In.URL.RawQuery
			pr.Out.Host = target.Host
			// Do not leak the fleet session cookie to the plugin.
			pr.Out.Header.Del("Cookie")
			pr.SetXForwarded()
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			a.logger.Warn("plugin proxy failed", "plugin", entry.ID, "error", err)
			writeError(w, http.StatusBadGateway, "upstream_unreachable", "plugin did not respond")
		},
		FlushInterval: -1,
	}

	proxy.ServeHTTP(w, r)
}

// matchPlugin finds the longest configured prefix matching path and returns the
// entry plus the matched prefix.
func (a *API) matchPlugin(path string) (config.PluginEntry, string, bool) {
	var (
		best   config.PluginEntry
		prefix string
		found  bool
	)

	for _, entry := range a.pluginEntries() {
		if !entry.Enabled {
			continue
		}
		for _, candidate := range pluginPrefixes(entry) {
			if candidate == "" || !strings.HasPrefix(path, candidate) {
				continue
			}
			if !found || len(candidate) > len(prefix) {
				best, prefix, found = entry, candidate, true
			}
		}
	}

	return best, prefix, found
}

// pluginPrefixes lists the fleet-side path prefixes that map to one plugin.
// Only the dedicated /plugin-ui/<id> namespace is served; the declared UI path
// is a client-side route that renders the proxied content inside the SPA.
func pluginPrefixes(entry config.PluginEntry) []string {
	return []string{"/plugin-ui/" + entry.ID}
}

func trimPrefixPath(path, prefix string) string {
	trimmed := strings.TrimPrefix(path, prefix)
	if trimmed == "" {
		return "/"
	}
	if !strings.HasPrefix(trimmed, "/") {
		return "/" + trimmed
	}

	return trimmed
}
