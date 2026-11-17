// Package taskrunner claims durable host tasks and executes registered
// handlers. Leases let multiple center instances cooperate safely.
package taskrunner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/store"
)

// Store is the durable task queue surface.
type Store interface {
	ClaimHostTask(ctx context.Context, worker string, lease time.Duration) (*store.HostTask, error)
	RenewHostTaskLease(ctx context.Context, id, worker string, until time.Time) error
	UpdateHostTask(ctx context.Context, task *store.HostTask) error
	AppendHostTaskEvent(ctx context.Context, event *store.HostTaskEvent) error
	ReleaseHostTask(ctx context.Context, id, worker string) error
}

// Handler executes one claimed task and returns a short result summary.
type Handler func(ctx context.Context, task *store.HostTask) (string, error)

// Runner polls the queue and runs handlers under a lease.
type Runner struct {
	store    Store
	worker   string
	lease    time.Duration
	interval time.Duration
	logger   *slog.Logger
	handlers map[string]Handler
}

// New builds a runner. Empty worker defaults to "worker"; lease and interval
// fall back to 30s and 2s.
func New(st Store, worker string, lease, interval time.Duration, logger *slog.Logger) *Runner {
	if strings.TrimSpace(worker) == "" {
		worker = "worker"
	}
	if lease <= 0 {
		lease = 30 * time.Second
	}
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{
		store: st, worker: worker, lease: lease, interval: interval,
		logger: logger, handlers: make(map[string]Handler),
	}
}

// Register attaches a handler for a task type.
func (r *Runner) Register(taskType string, handler Handler) {
	if taskType == "" || handler == nil {
		return
	}
	r.handlers[taskType] = handler
}

// Worker returns the lease owner name.
func (r *Runner) Worker() string { return r.worker }

// Run polls until the context is cancelled.
func (r *Runner) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := r.RunOnce(ctx); err != nil && !errors.Is(err, store.ErrNotFound) {
				r.logger.Warn("task runner claim failed", "error", err, "worker", r.worker)
			}
		}
	}
}

// RunOnce claims and executes at most one task. It returns the finished task
// or store.ErrNotFound when the queue is empty.
func (r *Runner) RunOnce(ctx context.Context) (*store.HostTask, error) {
	task, err := r.store.ClaimHostTask(ctx, r.worker, r.lease)
	if err != nil {
		return nil, err
	}

	renewCtx, cancelRenew := context.WithCancel(ctx)
	renewDone := make(chan struct{})
	go func() {
		defer close(renewDone)
		ticker := time.NewTicker(r.lease / 2)
		defer ticker.Stop()
		for {
			select {
			case <-renewCtx.Done():
				return
			case <-ticker.C:
				if err := r.store.RenewHostTaskLease(renewCtx, task.ID, r.worker, time.Now().UTC().Add(r.lease)); err != nil {
					r.logger.Warn("task lease renewal failed", "task", task.ID, "error", err)
				}
			}
		}
	}()

	now := time.Now().UTC()
	task.State = store.HostTaskRunning
	task.StartedAt = now
	task.UpdatedAt = now
	if err := r.store.UpdateHostTask(ctx, task); err != nil {
		cancelRenew()
		<-renewDone
		return task, err
	}
	_ = r.store.AppendHostTaskEvent(ctx, &store.HostTaskEvent{
		ID: store.NewID(), TaskID: task.ID, At: now, State: store.HostTaskRunning, Message: "started",
	})

	handler := r.handlers[task.Type]
	var (
		summary string
		runErr  error
	)
	if handler == nil {
		runErr = fmt.Errorf("taskrunner: no handler registered for %q", task.Type)
	} else {
		summary, runErr = safeRun(ctx, handler, task)
	}

	cancelRenew()
	<-renewDone

	finish := time.Now().UTC()
	task.UpdatedAt = finish
	task.FinishedAt = finish
	if runErr != nil {
		task.State = store.HostTaskFailed
		task.Result = "failed"
		task.ErrorCode = "task_failed"
		task.Error = runErr.Error()
	} else {
		task.State = store.HostTaskSucceeded
		task.Result = "succeeded"
		task.ErrorCode = ""
		task.Error = ""
		if summary != "" {
			task.Progress = summary
		}
	}
	if err := r.store.UpdateHostTask(context.Background(), task); err != nil {
		return task, err
	}
	_ = r.store.AppendHostTaskEvent(context.Background(), &store.HostTaskEvent{
		ID: store.NewID(), TaskID: task.ID, At: finish, State: task.State, Message: task.Result,
	})

	return task, nil
}

func safeRun(ctx context.Context, handler Handler, task *store.HostTask) (summary string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("taskrunner: handler panic: %v", recovered)
		}
	}()
	return handler(ctx, task)
}
