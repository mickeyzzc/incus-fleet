package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/hostops"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

const (
	headerBackend = "X-Incus-Fleet-Backend"
	headerTask    = "X-Incus-Fleet-Task"
)

// resolveNode picks the host operation backend for one member and writes the
// backend response header. The caller owns closeNode.
func (a *API) resolveNode(w http.ResponseWriter, r *http.Request, kind hostops.Kind) (hostops.Operator, hostops.Backend, func(), bool) {
	if a.nodenet == nil {
		writeError(w, http.StatusServiceUnavailable, "node_unavailable", "node network management is not enabled")
		return nil, "", nil, false
	}

	operator, backend, closeNode, err := a.nodenet.Resolve(r.Context(), r.PathValue("cluster"), r.PathValue("member"), kind)
	if err != nil {
		a.serviceError(w, r, err)
		return nil, "", nil, false
	}

	w.Header().Set(headerBackend, string(backend))

	return operator, backend, closeNode, true
}

// beginHostTask records a mutating host operation before it starts. A nil
// task means the operation is not persisted (tests or a disabled store).
func (a *API) beginHostTask(w http.ResponseWriter, r *http.Request, backend hostops.Backend, action, plan string) (*store.HostTask, bool) {
	if a.store == nil {
		return nil, true
	}

	now := time.Now().UTC()
	task := &store.HostTask{
		ID:        store.NewID(),
		Cluster:   r.PathValue("cluster"),
		Member:    r.PathValue("member"),
		Type:      action,
		State:     store.HostTaskRunning,
		Plan:      plan,
		PlanHash:  planHash(plan),
		CreatedBy: "admin",
		CreatedAt: now,
		StartedAt: now,
		UpdatedAt: now,
		Attempt:   1,
	}
	if err := a.store.CreateHostTask(r.Context(), task); err != nil {
		a.logger.Error("create host task", "action", action, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to record host operation")
		return nil, false
	}
	_ = a.store.AppendHostTaskEvent(r.Context(), &store.HostTaskEvent{
		ID:      store.NewID(),
		TaskID:  task.ID,
		At:      now,
		State:   store.HostTaskRunning,
		Message: "started",
		Data:    plan,
	})
	w.Header().Set(headerTask, task.ID)

	return task, true
}

// finishHostTask records the terminal state and the audit entry. Store errors
// are logged but do not change the operation result.
func (a *API) finishHostTask(r *http.Request, task *store.HostTask, backend hostops.Backend, action, plan string, opErr error) {
	if task == nil || a.store == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	now := time.Now().UTC()
	state := store.HostTaskSucceeded
	result := "succeeded"
	errorCode := ""
	errorText := ""
	if opErr != nil {
		state = store.HostTaskFailed
		result = "failed"
		errorCode = "operation_failed"
		errorText = opErr.Error()
	}

	task.State = state
	task.Result = result
	task.ErrorCode = errorCode
	task.Error = errorText
	task.FinishedAt = now
	task.UpdatedAt = now
	if err := a.store.UpdateHostTask(ctx, task); err != nil {
		a.logger.Error("update host task", "task", task.ID, "error", err)
	}
	_ = a.store.AppendHostTaskEvent(ctx, &store.HostTaskEvent{
		ID:      store.NewID(),
		TaskID:  task.ID,
		At:      now,
		State:   state,
		Message: result,
	})
	_ = a.store.RecordHostAudit(ctx, &store.HostAudit{
		ID:       store.NewID(),
		At:       now,
		Actor:    "admin",
		SourceIP: requestIP(r),
		Backend:  string(backend),
		Cluster:  task.Cluster,
		Member:   task.Member,
		Action:   action,
		TaskID:   task.ID,
		Result:   result,
		Detail:   plan,
	})
}

func planHash(plan string) string {
	sum := sha256.Sum256([]byte(plan))
	return hex.EncodeToString(sum[:])
}

func requestIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}

	return r.RemoteAddr
}

func hostPlan(parts ...string) string {
	return strings.Join(parts, " ")
}
