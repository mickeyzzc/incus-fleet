package httpapi

import (
	"net/http"
	"strings"
	"time"
)

func (a *API) queryRange(w http.ResponseWriter, r *http.Request) {
	if a.metricsQuery == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "external metric queries are not enabled")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "query is required")
		return
	}
	from, ok := parseMetricTime(w, r, "from", time.Now().UTC().Add(-time.Hour))
	if !ok {
		return
	}
	to, ok := parseMetricTime(w, r, "to", time.Now().UTC())
	if !ok {
		return
	}
	step := time.Minute
	if raw := strings.TrimSpace(r.URL.Query().Get("step")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "step must be a positive duration")
			return
		}
		step = parsed
	}
	if to.Before(from) {
		writeError(w, http.StatusBadRequest, "invalid_request", "to must be after from")
		return
	}

	data, err := a.metricsQuery.QueryRange(r.Context(), query, from, to, step)
	if err != nil {
		writeError(w, http.StatusBadGateway, "query_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data) //nolint:gosec // upstream PromQL JSON
}
