package httpapi

import (
	"net/http"
	"strings"

	"github.com/mickeyzzc/incus-fleet/internal/instances"
	"github.com/mickeyzzc/incus-fleet/internal/unattended"
)

// listISOs lists installer ISO volumes stored on the cluster.
func (a *API) listISOs(w http.ResponseWriter, r *http.Request) {
	isos, err := a.instances.ListISOs(r.PathValue("cluster"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"isos": isos})
}

// importISO uploads one ISO file as a custom storage volume.
func (a *API) importISO(w http.ResponseWriter, r *http.Request) {
	// ISO files are several GB; the body is bounded well above any
	// realistic installer image while the parser spills to temp files.
	extendUploadDeadline(w)
	r.Body = http.MaxBytesReader(w, r.Body, 12<<30)
	if err := r.ParseMultipartForm(8 << 20); err != nil { //nolint:gosec // body bounded via MaxBytesReader
		writeError(w, http.StatusBadRequest, "invalid_request", "multipart form required")
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "image file required")
		return
	}
	defer file.Close() //nolint:errcheck // upload cleanup

	pool := r.URL.Query().Get("pool")
	name, err := a.instances.ImportISO(r.Context(), r.PathValue("cluster"), pool, header.Filename, file)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"name": name, "pool": pool})
}

// deleteISO removes one ISO volume.
func (a *API) deleteISO(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.DeleteISO(r.PathValue("cluster"), r.PathValue("pool"), r.PathValue("name")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// launchFromISO creates an empty VM that boots the ISO installer.
func (a *API) launchFromISO(w http.ResponseWriter, r *http.Request) {
	var req instances.LaunchISORequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	req.ISOName = r.PathValue("name")

	detail, err := a.instances.LaunchFromISO(r.Context(), r.PathValue("cluster"), req)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, detail)
}

// previewUnattendedAnswer renders the answer file for a profile and spec
// without creating anything. Operator-provided answer content is echoed back
// unchanged so the UI can display exactly what would be shipped.
func (a *API) previewUnattendedAnswer(w http.ResponseWriter, r *http.Request) {
	var req instances.UnattendedRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	profile := unattended.Profile(strings.TrimSpace(req.Profile))
	if err := unattended.Validate(profile); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	content := strings.TrimSpace(req.AnswerFile)
	if content == "" {
		rendered, err := unattended.RenderAnswerFile(profile, req.Spec)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		content = rendered
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"content":   content,
		"file_name": unattended.AnswerFileName(profile),
		"profile":   string(profile),
	})
}

// launchUnattended creates a VM that installs itself from the ISO without
// keyboard input. The installer media is remastered (preseed/kickstart) or
// accompanied by a NoCloud seed (autoinstall/nocloud); installation media is
// detached automatically once the installer finishes.
func (a *API) launchUnattended(w http.ResponseWriter, r *http.Request) {
	// Exporting, remastering and re-importing a multi-GB ISO takes
	// minutes; keep the response deadline out of the way.
	extendUploadDeadline(w)

	var req instances.UnattendedRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	detail, launch, err := a.instances.LaunchUnattended(
		r.Context(),
		r.PathValue("cluster"),
		r.PathValue("pool"),
		r.PathValue("name"),
		req,
	)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"instance": detail,
		"launch":   launch,
	})
}
