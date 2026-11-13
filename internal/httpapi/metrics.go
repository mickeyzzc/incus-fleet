package httpapi

import (
	"net/http"
)

// instanceMetrics returns a live resource snapshot of one instance. The UI
// polls this endpoint and derives rates from consecutive samples.
func (a *API) instanceMetrics(w http.ResponseWriter, r *http.Request) {
	metrics, err := a.instances.InstanceMetrics(r.PathValue("cluster"), r.PathValue("instance"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"metrics": metrics})
}
