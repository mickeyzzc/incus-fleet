package httpapi

import (
	"net/http"

	"github.com/mickeyzzc/incus-fleet/internal/instances"
)

func (a *API) createStoragePool(w http.ResponseWriter, r *http.Request) {
	var req instances.StoragePoolRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.CreateStoragePool(r.PathValue("cluster"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (a *API) updateStoragePool(w http.ResponseWriter, r *http.Request) {
	var req instances.StoragePoolRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.UpdateStoragePool(r.PathValue("cluster"), r.PathValue("pool"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteStoragePool(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.DeleteStoragePool(r.PathValue("cluster"), r.PathValue("pool")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) createNetwork(w http.ResponseWriter, r *http.Request) {
	var req instances.NetworkRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.CreateNetwork(r.PathValue("cluster"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (a *API) updateNetwork(w http.ResponseWriter, r *http.Request) {
	var req instances.NetworkRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.UpdateNetwork(r.PathValue("cluster"), r.PathValue("network"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteNetwork(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.DeleteNetwork(r.PathValue("cluster"), r.PathValue("network")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
