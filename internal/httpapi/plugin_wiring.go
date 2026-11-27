package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/store"
)

type wiringEntry struct {
	Key          string `json:"key"`
	Value        string `json:"value"`
	DefaultValue string `json:"default_value,omitempty"`
	Effect       string `json:"effect,omitempty"`
	Enabled      bool   `json:"enabled"`
	Approved     bool   `json:"approved"`
}

type wiringUpdateRequest struct {
	Values []struct {
		Key     string `json:"key"`
		Value   string `json:"value"`
		Enabled *bool  `json:"enabled,omitempty"`
	} `json:"values"`
}

// pluginWiringEntries lists the approved overrides of one plugin together with
// the human-readable effect declared in its manifest.
func (a *API) pluginWiringEntries(id string) ([]wiringEntry, error) {
	p, ok := a.registry.Get(id)
	if !ok {
		return nil, store.ErrNotFound
	}
	m := p.Manifest()
	effects := make(map[string]string, len(m.Capabilities.Wiring))
	for _, wiring := range m.Capabilities.Wiring {
		effects[wiring.Key] = wiring.Effect
	}

	rows, err := a.store.ListPluginWiring(context.Background(), id)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	out := make([]wiringEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, wiringEntry{
			Key: row.Key, Value: row.Value, DefaultValue: row.DefaultValue,
			Effect: effects[row.Key], Enabled: row.Enabled, Approved: row.Approved,
		})
	}

	return out, nil
}

func (a *API) getPluginWiring(w http.ResponseWriter, r *http.Request) {
	if a.registry == nil || a.store == nil {
		writeError(w, http.StatusServiceUnavailable, "wiring_unavailable", "wiring registry is not configured")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	entries, err := a.pluginWiringEntries(id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "plugin is not registered")
		return
	}
	if err != nil {
		a.serviceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"wiring": entries})
}

func (a *API) putPluginWiring(w http.ResponseWriter, r *http.Request) {
	if a.registry == nil || a.store == nil {
		writeError(w, http.StatusServiceUnavailable, "wiring_unavailable", "wiring registry is not configured")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	p, ok := a.registry.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "plugin is not registered")
		return
	}

	var req wiringUpdateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if len(req.Values) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "at least one wiring value is required")
		return
	}

	changed := make([]string, 0, len(req.Values))
	for _, value := range req.Values {
		key := strings.TrimSpace(value.Key)
		if !p.Manifest().Capabilities.DeclaresWiring(key) {
			writeError(w, http.StatusBadRequest, "undeclared_wiring_key", "wiring key is not declared by the plugin manifest")
			return
		}
		row, err := a.store.GetPluginWiring(r.Context(), id, key)
		if errors.Is(err, store.ErrNotFound) || (err == nil && !row.Approved) {
			writeError(w, http.StatusForbidden, "wiring_not_approved", "wiring key was not approved at install time")
			return
		}
		if err != nil {
			a.serviceError(w, r, err)
			return
		}
		enabled := true
		if value.Enabled != nil {
			enabled = *value.Enabled
		}
		if !enabled {
			if err := a.registry.DisableWiring(id, key); err != nil {
				a.serviceError(w, r, err)
				return
			}
			changed = append(changed, key+"=default")

			continue
		}
		normalized, err := normalizeWiringURL(value.Value)
		if err != nil || normalized == "" {
			writeError(w, http.StatusBadRequest, "invalid_wiring_value", "wiring value must be an absolute http(s) URL")
			return
		}
		if err := a.store.UpsertPluginWiring(r.Context(), &store.PluginWiring{
			PluginID: id, Key: key, Value: normalized,
			DefaultValue: row.DefaultValue, Enabled: true, Approved: true,
		}); err != nil {
			a.serviceError(w, r, err)
			return
		}
		if err := a.registry.SetWiring(id, key, normalized); err != nil {
			a.serviceError(w, r, err)
			return
		}
		changed = append(changed, key)
	}

	session, hasSession := sessionFromContext(r.Context())
	actor := "admin"
	if hasSession {
		actor = session.Username
	}
	_ = a.store.RecordHostAudit(context.Background(), &store.HostAudit{
		ID: store.NewID(), At: time.Now().UTC(), Actor: actor,
		SourceIP: requestIP(r), Action: "plugin.wiring.update",
		Result: "succeeded", Detail: strings.Join(changed, ","),
	})
	a.registry.Publish(r.Context(), pluginEventWiringChanged(id, changed))

	entries, err := a.pluginWiringEntries(id)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"wiring": entries})
}

func normalizeWiringURL(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	parsed, err := url.Parse(trimmed)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("invalid URL")
	}

	return strings.TrimRight(trimmed, "/"), nil
}

// requirePluginManager is shared by the wiring and install-task handlers.
func (a *API) requirePluginManager(w http.ResponseWriter) bool {
	if !a.pluginsEnabled {
		writeError(w, http.StatusServiceUnavailable, "plugins_disabled", "plugin system is disabled")
		return false
	}
	if a.registry == nil || a.store == nil {
		writeError(w, http.StatusServiceUnavailable, "wiring_unavailable", "wiring registry is not configured")
		return false
	}

	return true
}
