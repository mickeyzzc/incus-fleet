package httpapi

import (
	"net/http"
	"strconv"
	"strings"
)

// listHostAudit returns persisted host operation audit records.
func (a *API) listHostAudit(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "host audit is not enabled")
		return
	}

	limit, ok := queryLimit(w, r)
	if !ok {
		return
	}
	entries, err := a.store.ListHostAudit(r.Context(), strings.TrimSpace(r.URL.Query().Get("cluster")),
		strings.TrimSpace(r.URL.Query().Get("member")), limit)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"audit": entries})
}

// listHostTasks returns recent host tasks.
func (a *API) listHostTasks(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "host tasks are not enabled")
		return
	}

	limit, ok := queryLimit(w, r)
	if !ok {
		return
	}
	tasks, err := a.store.ListHostTasks(r.Context(), strings.TrimSpace(r.URL.Query().Get("cluster")),
		strings.TrimSpace(r.URL.Query().Get("member")), limit)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

// getHostTask returns one task and its event timeline.
func (a *API) getHostTask(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "host tasks are not enabled")
		return
	}

	task, err := a.store.GetHostTask(r.Context(), r.PathValue("id"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}
	events, err := a.store.ListHostTaskEvents(r.Context(), task.ID)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"task": task, "events": events})
}

func queryLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 50, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 200 {
		writeError(w, http.StatusBadRequest, "invalid_request", "limit must be between 1 and 200")
		return 0, false
	}

	return limit, true
}
