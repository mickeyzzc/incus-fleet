package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/mickeyzzc/incus-fleet/internal/agentgw"
	"github.com/mickeyzzc/incus-fleet/internal/agentpb"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

// AgentDispatcher is the subset of the agent gateway used by HTTP handlers.
type AgentDispatcher interface {
	Dispatch(ctx context.Context, agentID string, typ agentpb.TaskType, payload []byte, timeout time.Duration) (*agentpb.TaskResult, error)
}

const (
	defaultEnrollmentTTL = 15 * time.Minute
	maxEnrollmentTTL     = time.Hour
)

// createAgentEnrollmentToken issues a single-use token for one cluster member.
func (a *API) createAgentEnrollmentToken(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "agent registry is not enabled")
		return
	}

	var req struct {
		Cluster    string `json:"cluster"`
		Member     string `json:"member"`
		TTLSeconds int    `json:"ttl_seconds"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	req.Cluster = strings.TrimSpace(req.Cluster)
	req.Member = strings.TrimSpace(req.Member)
	if req.Cluster == "" || req.Member == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "cluster and member are required")
		return
	}
	if a.clusters == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "cluster manager is not configured")
		return
	}
	if _, err := a.clusters.Get(req.Cluster); err != nil {
		a.serviceError(w, r, err)
		return
	}

	ttl := defaultEnrollmentTTL
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}
	if ttl > maxEnrollmentTTL {
		writeError(w, http.StatusBadRequest, "invalid_request", "ttl_seconds must be at most 3600")
		return
	}

	token, hash, err := agentgw.NewEnrollmentToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to generate enrollment token")
		return
	}
	now := time.Now().UTC()
	record := &store.EnrollmentToken{
		ID:        store.NewID(),
		TokenHash: hash,
		Cluster:   req.Cluster,
		Member:    req.Member,
		ExpiresAt: now.Add(ttl),
		CreatedBy: "admin",
		CreatedAt: now,
	}
	if err := a.store.CreateEnrollmentToken(r.Context(), record); err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"token":      token,
		"cluster":    record.Cluster,
		"member":     record.Member,
		"expires_at": record.ExpiresAt,
	})
}

// listAgents returns enrolled agents.
func (a *API) listAgents(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "agent registry is not enabled")
		return
	}
	agents, err := a.store.ListAgents(r.Context())
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"agents": agents})
}

// agentSnapshot dispatches a read-only network snapshot to one agent.
func (a *API) agentSnapshot(w http.ResponseWriter, r *http.Request) {
	if a.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "agent gateway is not enabled")
		return
	}

	result, err := a.agent.Dispatch(r.Context(), r.PathValue("id"),
		agentpb.TaskType_TASK_TYPE_NODENET_SNAPSHOT, nil, 15*time.Second)
	if err != nil {
		if errors.Is(err, agentgw.ErrAgentOffline) {
			writeError(w, http.StatusServiceUnavailable, "agent_offline", "agent is not connected")
			return
		}
		writeError(w, http.StatusGatewayTimeout, "agent_timeout", err.Error())
		return
	}
	if result.State != agentpb.TaskState_TASK_STATE_SUCCEEDED {
		message := result.Error
		if message == "" {
			message = "agent task failed"
		}
		writeError(w, http.StatusBadGateway, "agent_error", message)
		return
	}

	var snapshot agentpb.NodenetSnapshot
	if err := protojson.Unmarshal(result.Payload, &snapshot); err != nil {
		writeError(w, http.StatusBadGateway, "agent_error", "agent returned an invalid snapshot")
		return
	}
	payload, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(&snapshot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to encode snapshot")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"snapshot": json.RawMessage(payload)})
}
