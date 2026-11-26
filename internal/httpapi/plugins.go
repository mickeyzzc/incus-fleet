package httpapi

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

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

// getPlugin returns one plugin descriptor.
func (a *API) getPlugin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	for _, entry := range a.pluginEntries() {
		if entry.ID == id {
			writeJSON(w, http.StatusOK, toPluginView(entry))

			return
		}
	}

	writeError(w, http.StatusNotFound, "not_found", "plugin not found")
}

func (a *API) pluginEntries() []config.PluginEntry {
	if !a.pluginsEnabled {
		return nil
	}

	entries := append([]config.PluginEntry{}, a.pluginsConfig.Entries...)
	entries = append(entries, a.installedPluginEntries()...)

	return entries
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
