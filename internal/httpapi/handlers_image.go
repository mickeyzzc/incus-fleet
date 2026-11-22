package httpapi

import (
	"io"
	"net/http"
)

func (a *API) importImage(w http.ResponseWriter, r *http.Request) {
	// Image uploads can be large; the body is bounded well above any
	// realistic image while the multipart parser spills to temp files.
	extendUploadDeadline(w)
	r.Body = http.MaxBytesReader(w, r.Body, 8<<30)
	if err := r.ParseMultipartForm(8 << 20); err != nil { //nolint:gosec // body bounded via MaxBytesReader
		writeError(w, http.StatusBadRequest, "invalid_request", "multipart form required")
		return
	}

	metaFile, header, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "image file required")
		return
	}
	defer metaFile.Close() //nolint:errcheck // upload cleanup

	var rootfs io.Reader
	if rootfsFile, _, rootfsErr := r.FormFile("rootfs"); rootfsErr == nil {
		defer rootfsFile.Close() //nolint:errcheck // upload cleanup
		rootfs = rootfsFile
	}

	query := r.URL.Query()
	fingerprint, err := a.instances.ImportImage(
		r.Context(),
		r.PathValue("cluster"),
		header.Filename,
		query.Get("type"),
		query.Get("alias"),
		metaFile,
		rootfs,
	)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	if fingerprint == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"fingerprint": fingerprint})
}

func (a *API) copyImage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TargetCluster string `json:"target_cluster"`
		Alias         string `json:"alias"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.CopyImageTo(r.Context(), r.PathValue("cluster"), r.PathValue("fingerprint"), req.TargetCluster, req.Alias); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteImage(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.DeleteImage(r.Context(), r.PathValue("cluster"), r.PathValue("fingerprint")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listImages(w http.ResponseWriter, r *http.Request) {
	images, err := a.instances.Images(r.PathValue("cluster"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"images": images})
}
