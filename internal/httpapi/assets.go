package httpapi

import (
	"net/http"

	"github.com/mickeyzzc/incus-fleet/internal/assets"
)

// listKeys returns every stored SSH public key.
func (a *API) listKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := a.assets.Keys(r.Context())
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

// createKey stores a new public key.
func (a *API) createKey(w http.ResponseWriter, r *http.Request) {
	var key assets.Keystone
	if err := decodeJSON(w, r, &key); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.assets.CreateKey(r.Context(), &key); err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, key)
}

// updateKey rewrites one stored key.
func (a *API) updateKey(w http.ResponseWriter, r *http.Request) {
	var key assets.Keystone
	if err := decodeJSON(w, r, &key); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	key.ID = r.PathValue("id")

	if err := a.assets.UpdateKey(r.Context(), &key); err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, key)
}

// deleteKey removes one stored key.
func (a *API) deleteKey(w http.ResponseWriter, r *http.Request) {
	if err := a.assets.DeleteKey(r.Context(), r.PathValue("id")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// listScripts returns every stored script.
func (a *API) listScripts(w http.ResponseWriter, r *http.Request) {
	scripts, err := a.assets.Scripts(r.Context())
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"scripts": scripts})
}

// createScript stores a new script.
func (a *API) createScript(w http.ResponseWriter, r *http.Request) {
	var script assets.Script
	if err := decodeJSON(w, r, &script); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.assets.CreateScript(r.Context(), &script); err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, script)
}

// updateScript rewrites one stored script.
func (a *API) updateScript(w http.ResponseWriter, r *http.Request) {
	var script assets.Script
	if err := decodeJSON(w, r, &script); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	script.ID = r.PathValue("id")

	if err := a.assets.UpdateScript(r.Context(), &script); err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, script)
}

// deleteScript removes one stored script.
func (a *API) deleteScript(w http.ResponseWriter, r *http.Request) {
	if err := a.assets.DeleteScript(r.Context(), r.PathValue("id")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// batchKeys injects stored keys into every target instance.
func (a *API) batchKeys(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Targets []assets.Target `json:"targets"`
		KeyIDs  []string        `json:"key_ids"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	items := a.assets.BatchInjectKeys(r.Context(), req.Targets, req.KeyIDs)
	status := http.StatusOK
	for _, item := range items {
		if item.Error != "" {
			status = http.StatusMultiStatus
			break
		}
	}

	writeJSON(w, status, map[string]any{"items": items})
}

// batchScripts runs one stored script on every target instance.
func (a *API) batchScripts(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Targets  []assets.Target `json:"targets"`
		ScriptID string          `json:"script_id"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	items := a.assets.BatchRunScript(r.Context(), req.Targets, req.ScriptID)
	status := http.StatusOK
	for _, item := range items {
		if item.Error != "" {
			status = http.StatusMultiStatus
			break
		}
	}

	writeJSON(w, status, map[string]any{"items": items})
}

// batchPower starts, stops, or restarts every target instance.
func (a *API) batchPower(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Targets []assets.Target `json:"targets"`
		Action  string          `json:"action"`
		Force   bool            `json:"force"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	items := a.assets.BatchPower(r.Context(), req.Targets, req.Action, req.Force)
	status := http.StatusOK
	for _, item := range items {
		if item.Error != "" {
			status = http.StatusMultiStatus
			break
		}
	}

	writeJSON(w, status, map[string]any{"items": items})
}
