package httpapi

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/mickeyzzc/incus-fleet/internal/agentpb"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/telemetry"
)

// MetricsSDConfig configures the Prometheus HTTP service discovery endpoint.
type MetricsSDConfig struct {
	Enabled   bool
	Token     string
	PublicURL string
	Mode      string
}

func (a *API) requireSDToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.metricsSD.Enabled || a.metricsSD.Token == "" {
			writeError(w, http.StatusNotFound, "not_found", "metrics service discovery is not enabled")
			return
		}
		header := r.Header.Get("Authorization")
		token := strings.TrimPrefix(header, "Bearer ")
		if len(token) != len(a.metricsSD.Token) || subtle.ConstantTimeCompare([]byte(token), []byte(a.metricsSD.Token)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid service discovery token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type sdTarget struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}

func (a *API) sdAgents(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "agent registry is not enabled")
		return
	}
	agents, err := a.store.ListAgents(r.Context())
	if err != nil {
		a.serviceError(w, r, err)
		return
	}
	scheme, host := a.publicTarget(r)
	targets := make([]sdTarget, 0)
	for _, agent := range agents {
		if agent.Status != store.AgentOnline {
			continue
		}
		target := sdTarget{
			Labels: map[string]string{
				"cluster":  agent.Cluster,
				"member":   agent.Member,
				"agent_id": agent.ID,
			},
		}
		if a.metricsSD.Mode == "direct" {
			address := metricsAddress(agent.Facts)
			if address == "" {
				continue
			}
			target.Targets = []string{address}
			target.Labels["__scheme__"] = "http"
			target.Labels["__metrics_path__"] = "/metrics"
		} else {
			target.Targets = []string{host}
			target.Labels["__scheme__"] = scheme
			target.Labels["__metrics_path__"] = "/api/v1/agents/" + url.PathEscape(agent.ID) + "/metrics/prometheus"
		}
		targets = append(targets, target)
	}

	writeJSON(w, http.StatusOK, targets)
}

func metricsAddress(facts string) string {
	if strings.TrimSpace(facts) == "" || facts == "{}" {
		return ""
	}
	var report agentpb.FactsReport
	if err := protojson.Unmarshal([]byte(facts), &report); err != nil {
		return ""
	}
	return strings.TrimSpace(report.Extra["metrics_address"])
}

func (a *API) publicTarget(r *http.Request) (string, string) {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Host
	if a.metricsSD.PublicURL != "" {
		if parsed, err := url.Parse(a.metricsSD.PublicURL); err == nil && parsed.Host != "" {
			host = parsed.Host
			if parsed.Scheme != "" {
				scheme = parsed.Scheme
			}
		}
	}
	return scheme, host
}

func (a *API) agentPrometheus(w http.ResponseWriter, r *http.Request) {
	if a.store == nil || a.telemetry == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "agent metrics are not enabled")
		return
	}
	agent, err := a.store.GetAgent(r.Context(), r.PathValue("id"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}
	series := a.telemetry.Snapshot(telemetry.Filter{AgentID: agent.ID})
	up := 0.0
	if agent.Status == store.AgentOnline {
		up = 1
	}
	series = append(series, telemetry.Series{
		Name:   "fleet_agent_up",
		Labels: map[string]string{"cluster": agent.Cluster, "member": agent.Member, "agent_id": agent.ID},
		Type:   "gauge",
		Points: []telemetry.Sample{{At: time.Now().UTC(), Value: up}},
	})

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(telemetry.Prometheus(series))) //nolint:gosec // text/plain metrics
}
