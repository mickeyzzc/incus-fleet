package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/telemetry"
)

func (a *API) telemetryStore(w http.ResponseWriter) (*telemetry.Store, bool) {
	if a.telemetry == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "telemetry is not enabled")
		return nil, false
	}
	return a.telemetry, true
}

func (a *API) queryMetrics(w http.ResponseWriter, r *http.Request) {
	store, ok := a.telemetryStore(w)
	if !ok {
		return
	}
	filter, ok := metricFilter(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"series": store.Query(filter)})
}

func (a *API) snapshotMetrics(w http.ResponseWriter, r *http.Request) {
	store, ok := a.telemetryStore(w)
	if !ok {
		return
	}
	filter, ok := metricFilter(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"series": store.Snapshot(filter)})
}

func (a *API) exportMetrics(w http.ResponseWriter, r *http.Request) {
	store, ok := a.telemetryStore(w)
	if !ok {
		return
	}
	filter, ok := metricFilter(w, r)
	if !ok {
		return
	}
	series := store.Query(filter)
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format"))) {
	case "", "json":
		writeJSON(w, http.StatusOK, map[string]any{"series": series})
	case "prometheus", "text":
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(telemetry.Prometheus(series))) //nolint:gosec // text/plain metrics export
	case "csv":
		text, err := telemetry.CSV(series)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to encode CSV")
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		_, _ = w.Write([]byte(text)) //nolint:gosec // text/csv metrics export
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "format must be json, prometheus, or csv")
	}
}

func metricFilter(w http.ResponseWriter, r *http.Request) (telemetry.Filter, bool) {
	from, ok := parseMetricTime(w, r, "from", time.Now().UTC().Add(-time.Hour))
	if !ok {
		return telemetry.Filter{}, false
	}
	to, ok := parseMetricTime(w, r, "to", time.Now().UTC())
	if !ok {
		return telemetry.Filter{}, false
	}
	if to.Before(from) {
		writeError(w, http.StatusBadRequest, "invalid_request", "to must be after from")
		return telemetry.Filter{}, false
	}
	return telemetry.Filter{
		Cluster: strings.TrimSpace(r.URL.Query().Get("cluster")),
		Member:  strings.TrimSpace(r.URL.Query().Get("member")),
		AgentID: strings.TrimSpace(r.URL.Query().Get("agent")),
		Metric:  strings.TrimSpace(r.URL.Query().Get("metric")),
		From:    from,
		To:      to,
	}, true
}

func parseMetricTime(w http.ResponseWriter, r *http.Request, key string, fallback time.Time) (time.Time, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback, true
	}
	if value, err := time.Parse(time.RFC3339, raw); err == nil {
		return value.UTC(), true
	}
	if seconds, err := time.ParseDuration(raw); err == nil {
		return time.Now().UTC().Add(-seconds), true
	}
	if unix, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return time.Unix(unix, 0).UTC(), true
	}
	writeError(w, http.StatusBadRequest, "invalid_request", key+" must be RFC3339, a duration, or Unix seconds")
	return time.Time{}, false
}
