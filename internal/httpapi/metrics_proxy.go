package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/metricsquery"
)

// metricsQueryProxy resolves the external PromQL backend. Installing the `vm`
// plugin is enough to enable it: no static config or restart is required.
func (a *API) metricsQueryProxy() *metricsquery.Proxy {
	if a.metricsQuery != nil {
		return a.metricsQuery
	}
	if a.store == nil {
		return nil
	}

	entries, err := a.store.ListInstalledPlugins(context.Background())
	if err != nil {
		a.logger.Warn("resolve metrics plugin failed", "error", err)
		return nil
	}
	for _, entry := range entries {
		if entry.ID == "vm" && entry.Enabled && entry.Upstream != "" {
			return metricsquery.New(metricsquery.Config{
				URL:  entry.Upstream,
				Type: "victoriametrics",
			})
		}
	}

	return nil
}

func (a *API) queryRange(w http.ResponseWriter, r *http.Request) {
	queryProxy := a.metricsQueryProxy()
	if queryProxy == nil {
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

	data, err := queryProxy.QueryRange(r.Context(), query, from, to, step)
	if err != nil {
		writeError(w, http.StatusBadGateway, "query_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data) //nolint:gosec // upstream PromQL JSON
}
