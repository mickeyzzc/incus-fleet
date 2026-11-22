package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/nodejoin"
)

// clusterMemberJoinTimeout bounds the whole provisioning operation. Installing
// Incus and joining a cluster can take several minutes on slow links.
const clusterMemberJoinTimeout = 8 * time.Minute

// addClusterMember provisions a new host and joins it to a cluster.
func (a *API) addClusterMember(w http.ResponseWriter, r *http.Request) {
	if a.nodejoin == nil {
		writeError(w, http.StatusServiceUnavailable, "node_unavailable", "node join is not enabled")
		return
	}

	var req nodejoin.Request
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	// The server's global write timeout is tuned for regular API calls;
	// provisioning needs a longer budget.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(clusterMemberJoinTimeout + 2*time.Minute))

	ctx, cancel := context.WithTimeout(r.Context(), clusterMemberJoinTimeout)
	defer cancel()

	result, err := a.nodejoin.Join(ctx, r.PathValue("cluster"), req)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, result)
}
