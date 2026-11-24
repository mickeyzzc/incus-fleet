package httpapi

import (
	"net/http"

	"github.com/mickeyzzc/incus-fleet/internal/instances"
)

func (a *API) listClusterInstances(w http.ResponseWriter, r *http.Request) {
	instanceType, err := parseInstanceType(r.URL.Query().Get("type"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	list, err := a.instances.List(r.PathValue("cluster"), instanceType)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"instances": list})
}

func (a *API) listInstances(w http.ResponseWriter, r *http.Request) {
	instanceType, err := parseInstanceType(r.URL.Query().Get("type"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	list, unavailable, err := a.instances.ListAll(instanceType)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"instances":            list,
		"unavailable_clusters": unavailable,
	})
}

func (a *API) getInstance(w http.ResponseWriter, r *http.Request) {
	detail, err := a.instances.Get(r.PathValue("cluster"), r.PathValue("instance"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

func (a *API) createInstance(w http.ResponseWriter, r *http.Request) {
	var req instances.CreateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	detail, err := a.instances.Create(r.Context(), r.PathValue("cluster"), req)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, detail)
}

func (a *API) updateInstance(w http.ResponseWriter, r *http.Request) {
	var req instances.UpdateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	detail, err := a.instances.Update(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), req)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

// renameInstance changes an instance name. The body carries the new name.
func (a *API) renameInstance(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	detail, err := a.instances.Rename(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), req.Name)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

func (a *API) instanceAction(w http.ResponseWriter, r *http.Request) {
	var req instances.ActionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.Action(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteInstance(w http.ResponseWriter, r *http.Request) {
	force, err := parseForce(r.URL.Query().Get("force"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	// volumes=true also removes the instance's attached custom volumes;
	// shared ISO installer assets are kept.
	volumes, err := parseForce(r.URL.Query().Get("volumes"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.Delete(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), force, volumes); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listSnapshots(w http.ResponseWriter, r *http.Request) {
	snapshots, err := a.instances.ListSnapshots(r.PathValue("cluster"), r.PathValue("instance"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"snapshots": snapshots})
}

func (a *API) createSnapshot(w http.ResponseWriter, r *http.Request) {
	var req instances.SnapshotRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.CreateSnapshot(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) restoreSnapshot(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.RestoreSnapshot(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), r.PathValue("snapshot")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteSnapshot(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.DeleteSnapshot(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), r.PathValue("snapshot")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listVolumes(w http.ResponseWriter, r *http.Request) {
	volumes, err := a.instances.Volumes(r.PathValue("cluster"), r.PathValue("pool"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"volumes": volumes})
}

func (a *API) createVolume(w http.ResponseWriter, r *http.Request) {
	var req instances.VolumeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.CreateVolume(r.PathValue("cluster"), r.PathValue("pool"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (a *API) deleteVolume(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.DeleteVolume(r.PathValue("cluster"), r.PathValue("pool"), r.PathValue("volume")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) attachDevice(w http.ResponseWriter, r *http.Request) {
	var req instances.DeviceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	detail, err := a.instances.AttachDevice(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), req)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

func (a *API) detachDevice(w http.ResponseWriter, r *http.Request) {
	detail, err := a.instances.DetachDevice(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), r.PathValue("device"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

// updateDevice applies one device edit and returns the refreshed detail.
func (a *API) updateDevice(w http.ResponseWriter, r *http.Request) {
	var req instances.DeviceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	detail, err := a.instances.UpdateDevice(
		r.Context(), r.PathValue("cluster"), r.PathValue("instance"), r.PathValue("device"), req)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, detail)
}
