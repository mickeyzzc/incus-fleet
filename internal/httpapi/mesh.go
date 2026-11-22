package httpapi

import (
	"net/http"
	"strings"
)

// meshStatus reports the local Raft state.
func (a *API) meshStatus(w http.ResponseWriter, _ *http.Request) {
	if a.mesh == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "center mesh is not enabled")
		return
	}
	writeJSON(w, http.StatusOK, a.mesh.Status())
}

// meshForceSingle is a destructive disaster-recovery operation. The operator
// must have fenced all other nodes before calling it.
func (a *API) meshForceSingle(w http.ResponseWriter, r *http.Request) {
	if a.mesh == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "center mesh is not enabled")
		return
	}
	var req struct {
		Confirm bool `json:"confirm"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if !req.Confirm {
		writeError(w, http.StatusBadRequest, "invalid_request", "confirm=true is required")
		return
	}
	if err := a.mesh.ForceSingle(); err != nil {
		a.serviceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "single", "term": a.mesh.Term()})
}

func (a *API) meshJoin(w http.ResponseWriter, r *http.Request) {
	if a.mesh == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "center mesh is not enabled")
		return
	}
	var req struct {
		ID             string `json:"id"`
		Address        string `json:"address"`
		ControlAddress string `json:"control_address"`
		Voter          bool   `json:"voter"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.Address) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "id and address are required")
		return
	}
	if err := a.mesh.JoinAny(req.ID, req.Address, req.ControlAddress, req.Voter); err != nil {
		a.serviceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"joined": req.ID, "voter": req.Voter})
}

func (a *API) meshPromote(w http.ResponseWriter, r *http.Request) {
	if a.mesh == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "center mesh is not enabled")
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(req.ID) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "id is required")
		return
	}
	if err := a.mesh.PromoteAny(req.ID); err != nil {
		a.serviceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"promoted": req.ID})
}

func (a *API) meshRemove(w http.ResponseWriter, r *http.Request) {
	if a.mesh == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "center mesh is not enabled")
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(req.ID) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "id is required")
		return
	}
	if err := a.mesh.RemoveAny(req.ID); err != nil {
		a.serviceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": req.ID})
}
