package migrations

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

type fixture struct {
	service *Service
	store   *store.SQLStore
	source  *incusfake.Server
	target  *incusfake.Server
	manager *clusters.Manager
}

func newFixture(t *testing.T, source, target *incusfake.Server) *fixture {
	t.Helper()

	cfgs := []config.Cluster{
		{Name: "c1", DisplayName: "c1", Endpoint: "https://192.0.2.10:8443"},
		{Name: "c2", DisplayName: "c2", Endpoint: "https://198.51.100.11:8443"},
	}
	dial := func(_ context.Context, cfg config.Cluster) (clusters.InstanceServer, error) {
		switch cfg.Name {
		case "c1":
			return source, nil
		case "c2":
			return target, nil
		default:
			return nil, errors.New("unknown")
		}
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager, err := clusters.NewWithDialer(context.Background(), cfgs, dial, logger)
	if err != nil {
		t.Fatalf("clusters.NewWithDialer() error = %v", err)
	}

	st, err := store.Open(context.Background(), "sqlite", ":memory:")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}

	svc := New(manager, st, logger, WithRunTimeout(30*time.Second))
	t.Cleanup(func() {
		svc.Wait()
		manager.Close()
		_ = st.Close()
	})

	return &fixture{service: svc, store: st, source: source, target: target, manager: manager}
}

func runningContainer(name string) *api.Instance {
	return &api.Instance{
		Name:       name,
		Type:       "container",
		Status:     "Running",
		StatusCode: api.Running,
		InstancePut: api.InstancePut{
			Architecture: "x86_64",
		},
		ExpandedConfig:  api.ConfigMap{},
		ExpandedDevices: api.DevicesMap{"root": {"pool": "default"}},
	}
}

func runningVM(name string, stateful bool) *api.Instance {
	vm := runningContainer(name)
	vm.Type = "virtual-machine"
	if stateful {
		vm.ExpandedConfig["migration.stateful"] = "true"
	}

	return vm
}

func moveRequest(name string) Request {
	return Request{SourceCluster: "c1", TargetCluster: "c2", Instance: name, Mode: ModeMove}
}

func TestMoveRunningContainer(t *testing.T) {
	source := &incusfake.Server{Instance: runningContainer("web")}
	target := &incusfake.Server{}
	f := newFixture(t, source, target)

	migration, err := f.service.Submit(context.Background(), moveRequest("web"))
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	f.service.Wait()

	got, err := f.service.Get(context.Background(), migration.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != StatusDone {
		t.Fatalf("status = %q error = %q", got.Status, got.Error)
	}
	if got.InstanceType != "container" || got.Mode != ModeMove || got.Live {
		t.Fatalf("migration = %#v", got)
	}

	if !source.HasCall("UpdateInstanceState:stop") || !source.HasCall("DeleteInstance:web") {
		t.Fatalf("source calls = %v", source.Calls())
	}
	args := target.CopyArgs()
	if len(args) != 1 || args[0].Live || !args[0].InstanceOnly || args[0].Mode != "pull" {
		t.Fatalf("copy args = %#v", args)
	}
	if target.HasCall("UpdateInstanceState:start") {
		t.Fatalf("target should already be running: %v", target.Calls())
	}
}

func TestMoveStoppedContainer(t *testing.T) {
	instance := runningContainer("web")
	source := &incusfake.Server{Instance: instance, State: &api.InstanceState{Status: "Stopped", StatusCode: api.Stopped}}
	target := &incusfake.Server{State: &api.InstanceState{Status: "Stopped", StatusCode: api.Stopped}}
	f := newFixture(t, source, target)

	migration, err := f.service.Submit(context.Background(), moveRequest("web"))
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	f.service.Wait()

	got, _ := f.service.Get(context.Background(), migration.ID)
	if got.Status != StatusDone {
		t.Fatalf("status = %q error = %q", got.Status, got.Error)
	}
	if source.HasCall("UpdateInstanceState:stop") || target.HasCall("UpdateInstanceState:start") {
		t.Fatalf("unexpected state changes: source=%v target=%v", source.Calls(), target.Calls())
	}
	if !source.HasCall("DeleteInstance:web") {
		t.Fatalf("source calls = %v", source.Calls())
	}
}

func TestCopyRestoresSourceWithoutAutoStart(t *testing.T) {
	var stateCalls atomic.Int32
	source := &incusfake.Server{Instance: runningContainer("web")}
	source.StateFunc = func(string) (*api.InstanceState, error) {
		if stateCalls.Add(1) == 1 {
			return &api.InstanceState{Status: "Running", StatusCode: api.Running}, nil
		}

		return &api.InstanceState{Status: "Stopped", StatusCode: api.Stopped}, nil
	}
	target := &incusfake.Server{State: &api.InstanceState{Status: "Stopped", StatusCode: api.Stopped}}
	f := newFixture(t, source, target)

	autoStart := false
	req := moveRequest("web")
	req.Mode = ModeCopy
	req.AutoStart = &autoStart

	migration, err := f.service.Submit(context.Background(), req)
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	f.service.Wait()

	got, _ := f.service.Get(context.Background(), migration.ID)
	if got.Status != StatusDone {
		t.Fatalf("status = %q error = %q", got.Status, got.Error)
	}

	actions := sourceStateActions(source)
	if len(actions) != 2 || actions[0] != "stop" || actions[1] != "start" {
		t.Fatalf("source actions = %v", actions)
	}
	if target.HasCall("UpdateInstanceState:start") {
		t.Fatalf("auto_start=false still started target: %v", target.Calls())
	}
	if source.HasCall("DeleteInstance:web") {
		t.Fatalf("copy deleted source: %v", source.Calls())
	}
}

func TestLiveMoveVM(t *testing.T) {
	source := &incusfake.Server{Instance: runningVM("vm1", true)}
	target := &incusfake.Server{State: &api.InstanceState{Status: "Frozen", StatusCode: api.Frozen}}
	f := newFixture(t, source, target)

	req := moveRequest("vm1")
	req.Live = true
	migration, err := f.service.Submit(context.Background(), req)
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	f.service.Wait()

	got, _ := f.service.Get(context.Background(), migration.ID)
	if got.Status != StatusDone {
		t.Fatalf("status = %q error = %q", got.Status, got.Error)
	}

	args := target.CopyArgs()
	if len(args) != 1 || !args[0].Live {
		t.Fatalf("copy args = %#v", args)
	}
	if !target.HasCall("UpdateInstanceState:unfreeze") {
		t.Fatalf("target calls = %v", target.Calls())
	}
	if !source.HasCall("UpdateInstanceState:stop") || !source.HasCall("DeleteInstance:vm1") {
		t.Fatalf("source calls = %v", source.Calls())
	}
}

func TestProgressAndWaitErrorRecovery(t *testing.T) {
	source := &incusfake.Server{Instance: runningContainer("web")}
	target := &incusfake.Server{
		RemoteOperation: &incusfake.RemoteOperation{
			Target:   &api.Operation{StatusCode: api.Success, Status: "Success", Metadata: map[string]any{"block_progress": "web: 1GB (10MB/s)"}},
			WaitFunc: func() error { return errors.New("Lost connection to the event listener") },
		},
	}
	f := newFixture(t, source, target)

	migration, err := f.service.Submit(context.Background(), moveRequest("web"))
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	f.service.Wait()

	got, _ := f.service.Get(context.Background(), migration.ID)
	if got.Status != StatusDone {
		t.Fatalf("status = %q error = %q", got.Status, got.Error)
	}
}

func TestTransferFailureRestartsSource(t *testing.T) {
	var stateCalls atomic.Int32
	source := &incusfake.Server{Instance: runningContainer("web")}
	source.StateFunc = func(string) (*api.InstanceState, error) {
		if stateCalls.Add(1) == 1 {
			return &api.InstanceState{Status: "Running", StatusCode: api.Running}, nil
		}

		return &api.InstanceState{Status: "Stopped", StatusCode: api.Stopped}, nil
	}
	target := &incusfake.Server{CopyErr: errors.New("network exploded")}
	f := newFixture(t, source, target)

	migration, err := f.service.Submit(context.Background(), moveRequest("web"))
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	f.service.Wait()

	got, _ := f.service.Get(context.Background(), migration.ID)
	if got.Status != StatusFailed || !strings.Contains(got.Error, "network exploded") {
		t.Fatalf("migration = %#v", got)
	}

	actions := sourceStateActions(source)
	if len(actions) != 2 || actions[1] != "start" {
		t.Fatalf("source actions = %v", actions)
	}
}

func TestOperationFailure(t *testing.T) {
	source := &incusfake.Server{Instance: runningContainer("web")}
	target := &incusfake.Server{
		RemoteOperation: &incusfake.RemoteOperation{
			Target:   &api.Operation{StatusCode: api.Failure, Status: "Failure", Err: "disk full"},
			WaitFunc: func() error { return errors.New("lost connection") },
		},
	}
	f := newFixture(t, source, target)

	migration, err := f.service.Submit(context.Background(), moveRequest("web"))
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	f.service.Wait()

	got, _ := f.service.Get(context.Background(), migration.ID)
	if got.Status != StatusFailed || !strings.Contains(got.Error, "disk full") {
		t.Fatalf("migration = %#v", got)
	}
}

func TestConcurrentSubmitConflict(t *testing.T) {
	source := &incusfake.Server{Instance: runningContainer("web")}
	release := make(chan struct{})
	target := &incusfake.Server{
		RemoteOperation: &incusfake.RemoteOperation{
			Target:   &api.Operation{StatusCode: api.Success, Status: "Success"},
			WaitFunc: func() error { <-release; return nil },
		},
	}
	f := newFixture(t, source, target)

	if _, err := f.service.Submit(context.Background(), moveRequest("web")); err != nil {
		t.Fatalf("first Submit() error = %v", err)
	}
	if _, err := f.service.Submit(context.Background(), moveRequest("web")); !errors.Is(err, ErrConflict) {
		t.Fatalf("second Submit() error = %v", err)
	}
	close(release)
	f.service.Wait()
}

func TestPreflightValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*fixture, *Request)
		want   error
	}{
		{"missing clusters", func(_ *fixture, r *Request) { r.TargetCluster = "" }, ErrInvalidRequest},
		{"same cluster", func(_ *fixture, r *Request) { r.TargetCluster = "c1" }, ErrInvalidRequest},
		{"missing instance", func(_ *fixture, r *Request) { r.Instance = " " }, ErrInvalidRequest},
		{"bad mode", func(_ *fixture, r *Request) { r.Mode = "teleport" }, ErrInvalidRequest},
		{"live copy", func(_ *fixture, r *Request) { r.Mode = ModeCopy; r.Live = true }, ErrInvalidRequest},
		{"unknown cluster", func(_ *fixture, r *Request) { r.TargetCluster = "c99" }, clusters.ErrUnknownCluster},
		{"instance missing", func(f *fixture, _ *Request) { f.source.Instance = nil }, ErrNotFound},
		{"target exists", func(f *fixture, _ *Request) { f.target.Instance = runningContainer("web") }, ErrConflict},
		{"live container", func(_ *fixture, r *Request) { r.Live = true }, ErrInvalidRequest},
		{"live without stateful", func(f *fixture, r *Request) {
			f.source.Instance = runningVM("web", false)
			r.Live = true
		}, ErrInvalidRequest},
		{"live stopped", func(f *fixture, r *Request) {
			f.source.Instance = runningVM("web", true)
			f.source.State = &api.InstanceState{Status: "Stopped", StatusCode: api.Stopped}
			r.Live = true
		}, ErrInvalidRequest},
		{"pool missing", func(f *fixture, _ *Request) { f.target.PoolNames = []string{"other"} }, ErrInvalidRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, &incusfake.Server{Instance: runningContainer("web")}, &incusfake.Server{})
			req := moveRequest("web")
			tt.mutate(f, &req)

			_, err := f.service.Submit(context.Background(), req)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Submit() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestStateInspectionErrors(t *testing.T) {
	source := &incusfake.Server{Instance: runningContainer("web"), StateErr: errors.New("state boom")}
	f := newFixture(t, source, &incusfake.Server{})
	if _, err := f.service.Submit(context.Background(), moveRequest("web")); err == nil {
		t.Fatal("Submit() expected state error")
	}

	source = &incusfake.Server{Instance: runningContainer("web")}
	target := &incusfake.Server{PoolNamesErr: errors.New("pools boom")}
	f = newFixture(t, source, target)
	if _, err := f.service.Submit(context.Background(), moveRequest("web")); err == nil {
		t.Fatal("Submit() expected pool error")
	}
}

func TestGetAndList(t *testing.T) {
	f := newFixture(t, &incusfake.Server{Instance: runningContainer("web")}, &incusfake.Server{})

	if _, err := f.service.Get(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v", err)
	}

	migration, err := f.service.Submit(context.Background(), moveRequest("web"))
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	f.service.Wait()

	list, err := f.service.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 1 || list[0].ID != migration.ID {
		t.Fatalf("List() = %#v", list)
	}
}

func TestEnsureRunningActions(t *testing.T) {
	ctx := context.Background()

	for _, tt := range []struct {
		name   string
		state  api.StatusCode
		action string
	}{
		{"frozen", api.Frozen, "unfreeze"},
		{"stopped", api.Stopped, "start"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake := &incusfake.Server{State: &api.InstanceState{Status: tt.state.String(), StatusCode: tt.state}}
			if err := ensureRunning(ctx, fake, "web"); err != nil {
				t.Fatalf("ensureRunning() error = %v", err)
			}
			args := fake.UpdateStateArgs()
			if len(args) != 1 || args[0].Action != tt.action {
				t.Fatalf("actions = %#v", args)
			}
		})
	}

	fake := &incusfake.Server{State: &api.InstanceState{Status: "Running", StatusCode: api.Running}}
	if err := ensureRunning(ctx, fake, "web"); err != nil {
		t.Fatalf("ensureRunning() error = %v", err)
	}
	if len(fake.UpdateStateArgs()) != 0 {
		t.Fatalf("running instance changed state: %#v", fake.UpdateStateArgs())
	}

	if err := stopIfRunning(ctx, fake, "web"); err != nil {
		t.Fatalf("stopIfRunning() error = %v", err)
	}
	args := fake.UpdateStateArgs()
	if len(args) != 1 || args[0].Action != "stop" || !args[0].Force {
		t.Fatalf("stop args = %#v", args)
	}
}

func sourceStateActions(source *incusfake.Server) []string {
	args := source.UpdateStateArgs()
	actions := make([]string, len(args))
	for i, arg := range args {
		actions[i] = arg.Action
	}

	return actions
}

type failingStore struct {
	store.Store
	updateErr error
	getErr    error
	listErr   error
}

func (f *failingStore) UpdateMigration(context.Context, *store.Migration) error { return f.updateErr }
func (f *failingStore) GetMigration(context.Context, string) (*store.Migration, error) {
	return nil, f.getErr
}
func (f *failingStore) ListMigrations(context.Context, int) ([]*store.Migration, error) {
	return nil, f.listErr
}

func TestStoreFailurePaths(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("store boom")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	fs := &failingStore{updateErr: boom, getErr: boom, listErr: boom}
	svc := New(nil, fs, logger, WithRunTimeout(0))
	if svc.runTimeout != 6*time.Hour {
		t.Fatalf("runTimeout = %v", svc.runTimeout)
	}

	svc.setStatus(ctx, "m1", StatusDone, "complete", "")
	tracker := &progressTracker{service: svc, id: "m1"}
	op := api.Operation{Metadata: map[string]any{"progress": "half"}}
	tracker.update(ctx, op)
	tracker.update(ctx, op)

	if _, err := svc.Get(ctx, "m1"); !errors.Is(err, boom) {
		t.Fatalf("Get() error = %v", err)
	}
	if _, err := svc.List(ctx, 10); !errors.Is(err, boom) {
		t.Fatalf("List() error = %v", err)
	}
}

func TestWaitRemoteContextCancel(t *testing.T) {
	f := newFixture(t, &incusfake.Server{}, &incusfake.Server{})
	block := make(chan struct{})
	remote := &incusfake.RemoteOperation{WaitFunc: func() error { <-block; return nil }}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := f.service.waitRemote(ctx, "m1", remote)
	close(block)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waitRemote() error = %v", err)
	}
}

func TestRecoverFromWaitError(t *testing.T) {
	f := newFixture(t, &incusfake.Server{}, &incusfake.Server{})
	ctx := context.Background()
	waitErr := errors.New("wait failed")

	if err := f.service.recoverFromWaitError(ctx, &incusfake.RemoteOperation{
		Target: &api.Operation{StatusCode: api.Success, Status: "Success"},
	}, waitErr); err != nil {
		t.Fatalf("success error = %v", err)
	}

	if err := f.service.recoverFromWaitError(ctx, &incusfake.RemoteOperation{Err: errors.New("target gone")}, waitErr); !errors.Is(err, waitErr) {
		t.Fatalf("lookup error = %v", err)
	}
	if err := f.service.recoverFromWaitError(ctx, &incusfake.RemoteOperation{}, waitErr); !errors.Is(err, waitErr) {
		t.Fatalf("nil target error = %v", err)
	}
	if err := f.service.recoverFromWaitError(ctx, &incusfake.RemoteOperation{
		Target: &api.Operation{StatusCode: api.Failure, Status: "Failure"},
	}, waitErr); err == nil || !strings.Contains(err.Error(), "Failure") {
		t.Fatalf("failure error = %v", err)
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if err := f.service.recoverFromWaitError(cancelCtx, &incusfake.RemoteOperation{
		Target: &api.Operation{StatusCode: api.Running, Status: "Running"},
	}, waitErr); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
}

func TestEnsureRunningAndStopErrors(t *testing.T) {
	ctx := context.Background()
	errBoom := errors.New("boom")

	if err := ensureRunning(ctx, &incusfake.Server{StateErr: errBoom}, "web"); !errors.Is(err, errBoom) {
		t.Fatalf("state error = %v", err)
	}
	if err := ensureRunning(ctx, &incusfake.Server{
		State:          &api.InstanceState{StatusCode: api.Stopped},
		UpdateStateErr: errBoom,
	}, "web"); !errors.Is(err, errBoom) {
		t.Fatalf("update error = %v", err)
	}
	if err := ensureRunning(ctx, &incusfake.Server{
		State: &api.InstanceState{StatusCode: api.Frozen},
		Operation: &incusfake.Operation{WaitFunc: func() error {
			return errBoom
		}},
	}, "web"); !errors.Is(err, errBoom) {
		t.Fatalf("wait error = %v", err)
	}

	if err := stopIfRunning(ctx, &incusfake.Server{StateErr: errBoom}, "web"); !errors.Is(err, errBoom) {
		t.Fatalf("stop state error = %v", err)
	}
	if err := stopIfRunning(ctx, &incusfake.Server{State: &api.InstanceState{StatusCode: api.Stopped}}, "web"); err != nil {
		t.Fatalf("stop stopped error = %v", err)
	}
	if err := stopIfRunning(ctx, &incusfake.Server{
		State:          &api.InstanceState{StatusCode: api.Running},
		UpdateStateErr: errBoom,
	}, "web"); !errors.Is(err, errBoom) {
		t.Fatalf("stop update error = %v", err)
	}
	if err := stopIfRunning(ctx, &incusfake.Server{
		State: &api.InstanceState{StatusCode: api.Running},
		Operation: &incusfake.Operation{WaitFunc: func() error {
			return errBoom
		}},
	}, "web"); !errors.Is(err, errBoom) {
		t.Fatalf("stop wait error = %v", err)
	}
}

func TestExecuteFailureBranches(t *testing.T) {
	t.Run("stop fails", func(t *testing.T) {
		source := &incusfake.Server{Instance: runningContainer("web"), UpdateStateErr: errors.New("cannot stop")}
		f := newFixture(t, source, &incusfake.Server{})
		migration, err := f.service.Submit(context.Background(), moveRequest("web"))
		if err != nil {
			t.Fatalf("Submit() error = %v", err)
		}
		f.service.Wait()
		got, _ := f.service.Get(context.Background(), migration.ID)
		if got.Status != StatusFailed || !strings.Contains(got.Error, "cannot stop") {
			t.Fatalf("migration = %#v", got)
		}
	})

	t.Run("stop wait fails", func(t *testing.T) {
		source := &incusfake.Server{
			Instance:  runningContainer("web"),
			Operation: &incusfake.Operation{WaitFunc: func() error { return errors.New("stop wait") }},
		}
		f := newFixture(t, source, &incusfake.Server{})
		migration, err := f.service.Submit(context.Background(), moveRequest("web"))
		if err != nil {
			t.Fatalf("Submit() error = %v", err)
		}
		f.service.Wait()
		got, _ := f.service.Get(context.Background(), migration.ID)
		if got.Status != StatusFailed || !strings.Contains(got.Error, "stop wait") {
			t.Fatalf("migration = %#v", got)
		}
	})

	t.Run("delete fails", func(t *testing.T) {
		source := &incusfake.Server{
			Instance:  runningContainer("web"),
			State:     &api.InstanceState{StatusCode: api.Stopped, Status: "Stopped"},
			DeleteErr: errors.New("delete failed"),
		}
		f := newFixture(t, source, &incusfake.Server{})
		migration, err := f.service.Submit(context.Background(), moveRequest("web"))
		if err != nil {
			t.Fatalf("Submit() error = %v", err)
		}
		f.service.Wait()
		got, _ := f.service.Get(context.Background(), migration.ID)
		if got.Status != StatusFailed || !strings.Contains(got.Error, "delete failed") {
			t.Fatalf("migration = %#v", got)
		}
	})

	t.Run("target start fails", func(t *testing.T) {
		target := &incusfake.Server{
			State:          &api.InstanceState{StatusCode: api.Stopped, Status: "Stopped"},
			UpdateStateErr: errors.New("start failed"),
		}
		f := newFixture(t, &incusfake.Server{Instance: runningContainer("web")}, target)
		migration, err := f.service.Submit(context.Background(), moveRequest("web"))
		if err != nil {
			t.Fatalf("Submit() error = %v", err)
		}
		f.service.Wait()
		got, _ := f.service.Get(context.Background(), migration.ID)
		if got.Status != StatusFailed || !strings.Contains(got.Error, "start failed") {
			t.Fatalf("migration = %#v", got)
		}
	})

	t.Run("live stop state fails", func(t *testing.T) {
		var calls atomic.Int32
		source := &incusfake.Server{Instance: runningVM("vm1", true)}
		source.StateFunc = func(string) (*api.InstanceState, error) {
			if calls.Add(1) == 1 {
				return &api.InstanceState{StatusCode: api.Running, Status: "Running"}, nil
			}
			return nil, errors.New("state gone")
		}
		f := newFixture(t, source, &incusfake.Server{})
		req := moveRequest("vm1")
		req.Live = true
		migration, err := f.service.Submit(context.Background(), req)
		if err != nil {
			t.Fatalf("Submit() error = %v", err)
		}
		f.service.Wait()
		got, _ := f.service.Get(context.Background(), migration.ID)
		if got.Status != StatusFailed || !strings.Contains(got.Error, "state gone") {
			t.Fatalf("migration = %#v", got)
		}
	})

	t.Run("target inspection fails", func(t *testing.T) {
		f := newFixture(t, &incusfake.Server{Instance: runningContainer("web")}, &incusfake.Server{InstanceErr: errors.New("target boom")})
		if _, err := f.service.Submit(context.Background(), moveRequest("web")); err == nil {
			t.Fatal("Submit() expected target error")
		}
	})
}
