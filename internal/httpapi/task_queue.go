package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/mickeyzzc/incus-fleet/internal/agentops"
	"github.com/mickeyzzc/incus-fleet/internal/agentpb"
	"github.com/mickeyzzc/incus-fleet/internal/bootstrap"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/taskrunner"
)

// enqueueHostTask records an asynchronous task for the HA dispatcher.
func (a *API) enqueueHostTask(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "host tasks are not enabled")
		return
	}
	if a.clusters == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "cluster manager is not configured")
		return
	}
	if !a.taskWorker {
		writeError(w, http.StatusServiceUnavailable, "no_worker",
			"no task dispatcher is running; enable HA or a shared database so queued tasks can be claimed")
		return
	}

	var req struct {
		Cluster string          `json:"cluster"`
		Member  string          `json:"member"`
		Type    string          `json:"type"`
		Plan    json.RawMessage `json:"plan"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	req.Cluster = strings.TrimSpace(req.Cluster)
	req.Member = strings.TrimSpace(req.Member)
	req.Type = strings.TrimSpace(req.Type)
	if req.Type == "" {
		req.Type = taskrunner.AgentNetworkTask
	}
	switch req.Type {
	case taskrunner.AgentNetworkTask, taskrunner.AgentBootstrapTask, taskrunner.AgentSnapshotTask:
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "type must be agent.network, agent.bootstrap, or agent.snapshot")
		return
	}
	if req.Cluster == "" || req.Member == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "cluster and member are required")
		return
	}
	if req.Type != taskrunner.AgentSnapshotTask && len(req.Plan) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "plan is required for this task type")
		return
	}
	summary, err := a.clusters.Get(req.Cluster)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}
	found := false
	for _, member := range summary.Members {
		if member.Name == req.Member {
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "not_found", "unknown cluster member")
		return
	}

	switch req.Type {
	case taskrunner.AgentNetworkTask:
		var spec agentpb.NodenetApplySpec
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(req.Plan, &spec); err != nil || spec.Action == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "plan must be a valid NodenetApplySpec with an action")
			return
		}
		if !agentops.IsNodenetAction(spec.Action) {
			writeError(w, http.StatusBadRequest, "invalid_request", "unsupported network action")
			return
		}
	case taskrunner.AgentBootstrapTask:
		var spec bootstrap.Spec
		if err := json.Unmarshal(req.Plan, &spec); err != nil || spec.Signature == "" || spec.TaskID == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "plan must be a signed bootstrap spec with task_id")
			return
		}
	}

	now := time.Now().UTC()
	task := &store.HostTask{
		ID:        store.NewID(),
		Cluster:   req.Cluster,
		Member:    req.Member,
		Type:      req.Type,
		State:     store.HostTaskQueued,
		Plan:      string(req.Plan),
		PlanHash:  planHash(string(req.Plan)),
		CreatedBy: "admin",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := a.store.CreateHostTask(r.Context(), task); err != nil {
		a.serviceError(w, r, err)
		return
	}
	_ = a.store.AppendHostTaskEvent(r.Context(), &store.HostTaskEvent{
		ID: store.NewID(), TaskID: task.ID, At: now, State: store.HostTaskQueued, Message: "queued",
	})

	writeJSON(w, http.StatusAccepted, map[string]any{"task": task})
}
