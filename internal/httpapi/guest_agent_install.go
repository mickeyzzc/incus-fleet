package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/guestagent"
)

// guestAgentInstallTimeout bounds the whole in-guest install: SSH + official
// install.sh + the agent verification window.
const guestAgentInstallTimeout = 4 * time.Minute

// installGuestAgent runs the official incus-agent installer inside one
// agent-less virtual machine. The guest credentials travel in the request
// body only; they are used for the session and never persisted or logged.
func (a *API) installGuestAgent(w http.ResponseWriter, r *http.Request) {
	if a.guestAgent == nil {
		writeError(w, http.StatusServiceUnavailable, "node_unavailable", "guest agent install is not configured")
		return
	}

	var req guestagent.Request
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	// The global write timeout is tuned for regular API calls; the install
	// needs a longer budget like the node join does.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(guestAgentInstallTimeout + 2*time.Minute))

	ctx, cancel := context.WithTimeout(r.Context(), guestAgentInstallTimeout)
	defer cancel()

	result, err := a.guestAgent.Install(ctx, r.PathValue("cluster"), r.PathValue("instance"), req)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}
