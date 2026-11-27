package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/plugin"
)

// ingestPluginEvent accepts a plugin-originated event over the same-origin
// route. The endpoint is opt-in; when enabled it requires both a bearer token
// and an HMAC signature so a random service on the network cannot publish
// fleet events.
func (a *API) ingestPluginEvent(w http.ResponseWriter, r *http.Request) {
	if a.registry == nil {
		writeError(w, http.StatusServiceUnavailable, "events_unavailable", "plugin registry is not configured")
		return
	}
	cfg := a.pluginsConfig.Events
	if tokenFile := strings.TrimSpace(cfg.BearerTokenFile); tokenFile != "" {
		data, err := os.ReadFile(tokenFile) //nolint:gosec // operator-provided token file
		if err != nil || strings.TrimSpace(string(data)) == "" ||
			r.Header.Get("Authorization") != "Bearer "+strings.TrimSpace(string(data)) {
			writeError(w, http.StatusUnauthorized, "invalid_token", "invalid plugin event credential")
			return
		}
	}

	body := http.MaxBytesReader(w, r.Body, 1<<20)
	var raw json.RawMessage
	if err := json.NewDecoder(body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid event body")
		return
	}
	if secretFile := strings.TrimSpace(cfg.SecretFile); secretFile != "" {
		data, err := os.ReadFile(secretFile) //nolint:gosec // operator-provided secret file
		if err != nil || !plugin.VerifySignature(raw, r.Header.Get("X-Fleet-Signature"), []byte(strings.TrimSpace(string(data)))) {
			writeError(w, http.StatusUnauthorized, "invalid_signature", "invalid event signature")
			return
		}
	}

	var event plugin.Event
	if err := json.Unmarshal(raw, &event); err != nil || strings.TrimSpace(event.Type) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "event type is required")
		return
	}
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	id := strings.TrimSpace(r.Header.Get("X-Fleet-Plugin-ID"))
	if id != "" {
		if _, ok := a.registry.Get(id); !ok {
			writeError(w, http.StatusForbidden, "unknown_plugin", "plugin is not registered")
			return
		}
	}
	a.registry.Publish(r.Context(), event)
	w.WriteHeader(http.StatusNoContent)
}

func pluginEventWiringChanged(id string, changed []string) plugin.Event {
	return plugin.Event{
		Type: plugin.EventWiringChanged, Subject: id,
		Payload: map[string]any{"keys": changed},
	}
}
