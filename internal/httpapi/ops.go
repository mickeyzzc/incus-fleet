package httpapi

import (
	"net/http"

	"github.com/mickeyzzc/incus-fleet/internal/instances"
)

// runInstanceOps performs a guest-side maintenance action (run script,
// set password, inject key) inside an instance.
func (a *API) runInstanceOps(w http.ResponseWriter, r *http.Request) {
	var req instances.OpsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	result, err := a.instances.RunOps(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), req)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// rebuildInstance recreates a stopped instance root disk from another image.
func (a *API) rebuildInstance(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Image string `json:"image"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	detail, err := a.instances.RebuildInstance(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), req.Image)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

// refreshImage re-downloads a cached image from its origin.
func (a *API) refreshImage(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.RefreshImage(r.Context(), r.PathValue("cluster"), r.PathValue("fingerprint")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// cloneInstance copies an instance within its cluster under a new name.
func (a *API) cloneInstance(w http.ResponseWriter, r *http.Request) {
	var req instances.CloneRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	detail, err := a.instances.CloneInstance(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), req)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, detail)
}
