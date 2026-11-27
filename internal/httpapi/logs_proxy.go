package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/logsquery"
)

// logsProxy resolves the active log-search backend. Log shipping/search is off
// by default: a backend exists only when static config was supplied or the
// logs.query_url wiring key was activated by a plugin install.
func (a *API) logsProxy() *logsquery.Proxy {
	if a.logsQuery != nil && a.logsQuery.URL() != "" {
		return a.logsQuery
	}
	if a.registry == nil {
		return nil
	}
	if raw, ok := a.registry.WiringValue("logs.query_url"); ok && raw != "" {
		proxy := logsquery.New(logsquery.Config{URL: raw})
		proxy.SetURL(raw)

		return proxy
	}

	return nil
}

func (a *API) queryLogs(w http.ResponseWriter, r *http.Request) {
	proxy := a.logsProxy()
	if proxy == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "log search is not enabled")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "query is required")
		return
	}
	var from, to time.Time
	if raw := strings.TrimSpace(r.URL.Query().Get("from")); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "from must be an RFC3339 timestamp")
			return
		}
		from = parsed.UTC()
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("to")); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "to must be an RFC3339 timestamp")
			return
		}
		to = parsed.UTC()
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 1000 {
			writeError(w, http.StatusBadRequest, "invalid_request", "limit must be between 1 and 1000")
			return
		}
		limit = parsed
	}

	data, err := proxy.Query(r.Context(), query, from, to, limit)
	if err != nil {
		writeError(w, http.StatusBadGateway, "query_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data) //nolint:gosec // upstream LogsQL JSON
}
