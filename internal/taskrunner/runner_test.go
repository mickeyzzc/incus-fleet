package taskrunner

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/store"
)

func openStore(t *testing.T) *store.SQLStore {
	t.Helper()
	st, err := store.Open(context.Background(), "sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func createTask(t *testing.T, st *store.SQLStore, id, taskType string) {
	t.Helper()
	now := time.Now().UTC()
	if err := st.CreateHostTask(context.Background(), &store.HostTask{
		ID: id, Cluster: "c1", Member: "node-a", Type: taskType,
		State: store.HostTaskQueued, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
}

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestRunOnceSuccess(t *testing.T) {
	st := openStore(t)
	createTask(t, st, "task-1", "test.ok")
	runner := New(st, "worker-1", time.Minute, time.Second, testLogger())
	called := false
	runner.Register("test.ok", func(_ context.Context, task *store.HostTask) (string, error) {
		called = true
		if task.State != store.HostTaskRunning || task.LeaseOwner != "worker-1" {
			t.Fatalf("task during handler = %#v", task)
		}
		return "done", nil
	})

	finished, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !called || finished.State != store.HostTaskSucceeded || finished.Progress != "done" {
		t.Fatalf("finished = %#v called=%v", finished, called)
	}
	events, err := st.ListHostTaskEvents(context.Background(), "task-1")
	if err != nil || len(events) != 2 {
		t.Fatalf("events = %#v, %v", events, err)
	}
	if _, err := runner.RunOnce(context.Background()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("empty queue error = %v", err)
	}
}

func TestRunOnceFailures(t *testing.T) {
	st := openStore(t)
	createTask(t, st, "task-fail", "test.fail")
	createTask(t, st, "task-missing", "test.missing")
	createTask(t, st, "task-panic", "test.panic")
	runner := New(st, "worker", time.Minute, time.Second, testLogger())
	runner.Register("test.fail", func(context.Context, *store.HostTask) (string, error) {
		return "", errors.New("boom")
	})
	runner.Register("test.panic", func(context.Context, *store.HostTask) (string, error) {
		panic("bad")
	})

	for _, id := range []string{"task-fail", "task-missing", "task-panic"} {
		finished, err := runner.RunOnce(context.Background())
		if err != nil {
			t.Fatalf("RunOnce(%s) error = %v", id, err)
		}
		if finished.ID != id || finished.State != store.HostTaskFailed || finished.Error == "" {
			t.Fatalf("finished = %#v", finished)
		}
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	st := openStore(t)
	runner := New(st, "worker", time.Minute, 10*time.Millisecond, testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestDefaults(t *testing.T) {
	runner := New(openStore(t), "", 0, 0, nil)
	if runner.Worker() != "worker" || runner.lease != 30*time.Second || runner.interval != 2*time.Second {
		t.Fatalf("defaults = %#v", runner)
	}
	runner.Register("", nil)
}

func TestRunProcessesQueuedTask(t *testing.T) {
	st := openStore(t)
	createTask(t, st, "task-run", "test.run")
	runner := New(st, "worker", time.Minute, 10*time.Millisecond, testLogger())
	runner.Register("test.run", func(context.Context, *store.HostTask) (string, error) {
		return "ok", nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go runner.Run(ctx)
	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		task, err := st.GetHostTask(context.Background(), "task-run")
		if err == nil && task.State == store.HostTaskSucceeded {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, _ := st.GetHostTask(context.Background(), "task-run")
	t.Fatalf("task = %#v", task)
}
