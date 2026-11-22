package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/overlay"
)

// OverlayService is the private overlay network orchestrator. It is an
// interface so the HTTP layer can be tested without host access.
type OverlayService interface {
	List(ctx context.Context, cluster string) ([]*overlay.Status, error)
	Get(ctx context.Context, cluster, name string) (*overlay.Status, error)
	Enable(ctx context.Context, cluster string, req overlay.Request) (*overlay.Status, error)
	Reconcile(ctx context.Context, cluster, name string) (*overlay.Status, error)
	Disable(ctx context.Context, cluster, name string) error
}

// overlayApplyTimeout bounds a full apply, which touches every member and
// therefore may take longer than an ordinary mutation.
const overlayApplyTimeout = 3 * time.Minute

// overlayService returns the configured service or writes 503.
func (a *API) overlayService(w http.ResponseWriter) bool {
	if a.overlay == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "private overlay networking is not enabled")
		return false
	}
	return true
}

// listOverlay returns every overlay of a cluster.
func (a *API) listOverlay(w http.ResponseWriter, r *http.Request) {
	if !a.overlayService(w) {
		return
	}

	items, err := a.overlay.List(r.Context(), r.PathValue("cluster"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"overlays": items})
}

// getOverlay returns one overlay.
func (a *API) getOverlay(w http.ResponseWriter, r *http.Request) {
	if !a.overlayService(w) {
		return
	}

	status, err := a.overlay.Get(r.Context(), r.PathValue("cluster"), r.PathValue("overlay"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, status)
}

// enableOverlay creates and applies a new overlay.
func (a *API) enableOverlay(w http.ResponseWriter, r *http.Request) {
	if !a.overlayService(w) {
		return
	}

	var req overlay.Request
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	// Applying writes bridges, tunnels, DHCP, and NAT on every member; give
	// the request room without hitting the server write timeout.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(overlayApplyTimeout))

	status, err := a.overlay.Enable(r.Context(), r.PathValue("cluster"), req)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, status)
}

// reconcileOverlay re-applies a stored overlay against the current members.
func (a *API) reconcileOverlay(w http.ResponseWriter, r *http.Request) {
	if !a.overlayService(w) {
		return
	}

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(overlayApplyTimeout))

	status, err := a.overlay.Reconcile(r.Context(), r.PathValue("cluster"), r.PathValue("overlay"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, status)
}

// disableOverlay tears the overlay down and removes its definition.
func (a *API) disableOverlay(w http.ResponseWriter, r *http.Request) {
	if !a.overlayService(w) {
		return
	}

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(overlayApplyTimeout))

	if err := a.overlay.Disable(r.Context(), r.PathValue("cluster"), r.PathValue("overlay")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
