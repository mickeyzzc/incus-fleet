package instances

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestRunOpsRunScript(t *testing.T) {
	source := &incusfake.Server{
		ExecOutput: "hello",
		Operation:  &incusfake.Operation{Metadata: map[string]any{"return": float64(0)}},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": source})

	result, err := svc.RunOps(context.Background(), "c1", "web", OpsRequest{Action: "run-script", Script: "echo hello"})
	if err != nil {
		t.Fatalf("RunOps() error = %v", err)
	}
	if result.ExitCode != 0 || result.Stdout != "hello" {
		t.Fatalf("result = %#v", result)
	}
	exec := source.ExecArgs()
	if len(exec) != 1 || exec[0].Command[0] != "sh" || exec[0].Command[1] != "-s" {
		t.Fatalf("exec = %#v", exec)
	}
	if !exec[0].WaitForWS || exec[0].Interactive {
		t.Fatalf("exec flags = %#v", exec[0])
	}

	if _, err = svc.RunOps(context.Background(), "c1", "web", OpsRequest{Action: "run-script", Script: " "}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("RunOps() empty script = %v", err)
	}
	if _, err = svc.RunOps(context.Background(), "c1", "web", OpsRequest{Action: "nope"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("RunOps() unknown action = %v", err)
	}
}

func TestRunOpsPassword(t *testing.T) {
	source := &incusfake.Server{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": source})

	if _, err := svc.RunOps(context.Background(), "c1", "web", OpsRequest{Action: "set-password", User: "root", Password: "s3cret"}); err != nil {
		t.Fatalf("RunOps() error = %v", err)
	}
	exec := source.ExecArgs()
	if len(exec) != 1 || exec[0].Command[0] != "chpasswd" {
		t.Fatalf("exec = %#v", exec)
	}

	// The password must never ride in argv.
	if _, err := svc.RunOps(context.Background(), "c1", "web", OpsRequest{Action: "set-password", User: "", Password: "x"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("RunOps() empty user = %v", err)
	}
	if _, err := svc.RunOps(context.Background(), "c1", "web", OpsRequest{Action: "set-password", User: "root", Password: ""}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("RunOps() empty password = %v", err)
	}
}

func TestRunOpsInjectKey(t *testing.T) {
	source := &incusfake.Server{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": source})

	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample user@example"
	if _, err := svc.RunOps(context.Background(), "c1", "web", OpsRequest{Action: "inject-key", User: "deploy", Key: key}); err != nil {
		t.Fatalf("RunOps() error = %v", err)
	}
	exec := source.ExecArgs()
	if len(exec) != 1 || len(exec[0].Command) != 5 || exec[0].Command[0] != "sh" {
		t.Fatalf("exec = %#v", exec)
	}
	if exec[0].Command[4] != "deploy" || strings.Contains(exec[0].Command[2], key) {
		t.Fatalf("key must travel via stdin, argv = %#v", exec[0].Command)
	}

	if _, err := svc.RunOps(context.Background(), "c1", "web", OpsRequest{Action: "inject-key", User: "deploy", Key: "two\nlines"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("RunOps() multiline key = %v", err)
	}
	if _, err := svc.RunOps(context.Background(), "c1", "web", OpsRequest{Action: "inject-key", User: "deploy", Key: ""}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("RunOps() empty key = %v", err)
	}
}

func TestRebuildInstance(t *testing.T) {
	source := &incusfake.Server{
		Instance: &api.Instance{
			Name:       "web",
			Type:       "container",
			Status:     "Stopped",
			StatusCode: api.Stopped,
		},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": source})
	ctx := context.Background()

	if _, err := svc.RebuildInstance(ctx, "c1", "web", "debian/13"); err != nil {
		t.Fatalf("RebuildInstance() error = %v", err)
	}
	rebuilds := source.RebuildArgs()
	if len(rebuilds) != 1 || rebuilds[0].Instance != "web" || rebuilds[0].Source.Type != "image" || rebuilds[0].Source.Alias != "debian/13" {
		t.Fatalf("rebuild args = %#v", rebuilds)
	}

	// Fingerprints select the image directly.
	if _, err := svc.RebuildInstance(ctx, "c1", "web", "149121184cd4"); err != nil {
		t.Fatalf("RebuildInstance() fingerprint error = %v", err)
	}
	if source.RebuildArgs()[1].Source.Fingerprint != "149121184cd4" {
		t.Fatalf("fingerprint source = %#v", source.RebuildArgs()[1].Source)
	}

	if _, err := svc.RebuildInstance(ctx, "c1", "web", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("RebuildInstance() empty image = %v", err)
	}
	if _, err := svc.RebuildInstance(ctx, "missing", "web", "debian/13"); err == nil {
		t.Fatal("RebuildInstance() expected unknown cluster error")
	}

	// A running instance must be stopped first.
	running := &incusfake.Server{
		Instance: &api.Instance{
			Name:       "web",
			Type:       "container",
			Status:     "Running",
			StatusCode: api.Running,
		},
	}
	runningSvc := newTestService(t, map[string]*incusfake.Server{"c1": running})
	if _, err := runningSvc.RebuildInstance(ctx, "c1", "web", "debian/13"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("RebuildInstance() running = %v", err)
	}

	// Daemon failures propagate.
	fail := &incusfake.Server{
		Instance: &api.Instance{
			Name:       "web",
			Type:       "container",
			Status:     "Stopped",
			StatusCode: api.Stopped,
		},
		RebuildErr: errors.New("rebuild refused"),
	}
	failSvc := newTestService(t, map[string]*incusfake.Server{"c1": fail})
	if _, err := failSvc.RebuildInstance(ctx, "c1", "web", "debian/13"); err == nil {
		t.Fatal("RebuildInstance() expected daemon error")
	}
}

func TestRefreshImage(t *testing.T) {
	source := &incusfake.Server{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": source})
	ctx := context.Background()

	if err := svc.RefreshImage(ctx, "c1", "149121184cd4"); err != nil {
		t.Fatalf("RefreshImage() error = %v", err)
	}
	if len(source.RefreshImageArgs()) != 1 || source.RefreshImageArgs()[0] != "149121184cd4" {
		t.Fatalf("refresh args = %#v", source.RefreshImageArgs())
	}

	if err := svc.RefreshImage(ctx, "c1", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("RefreshImage() empty fingerprint = %v", err)
	}
	if err := svc.RefreshImage(ctx, "missing", "abc"); err == nil {
		t.Fatal("RefreshImage() expected unknown cluster error")
	}
}

func TestCloneInstance(t *testing.T) {
	start := true
	source := &incusfake.Server{
		Instance: &api.Instance{
			Name:       "web",
			Type:       "container",
			Status:     "Running",
			StatusCode: api.Running,
			InstancePut: api.InstancePut{
				Description: "app server",
				Profiles:    []string{"default"},
				Config: api.ConfigMap{
					"limits.cpu":           "2",
					"volatile.eth0.hwaddr": "10:66:6a:ff:1f:39",
				},
				Devices: api.DevicesMap{
					"root": {"type": "disk", "path": "/", "pool": "default"},
				},
			},
		},
		ExtraInstances: []api.Instance{
			{
				Name:       "web-clone",
				Type:       "container",
				Status:     "Stopped",
				StatusCode: api.Stopped,
			},
		},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": source})
	ctx := context.Background()

	detail, err := svc.CloneInstance(ctx, "c1", "web", CloneRequest{Target: "web-clone", Start: &start})
	if err != nil {
		t.Fatalf("CloneInstance() error = %v", err)
	}
	if detail.Name != "web-clone" {
		t.Fatalf("detail = %#v", detail)
	}
	created := source.CreateArgs()
	if len(created) != 1 || created[0].Name != "web-clone" || created[0].Source.Type != "copy" || created[0].Source.Source != "web" {
		t.Fatalf("clone args = %#v", created)
	}
	if created[0].Config["limits.cpu"] != "2" {
		t.Fatalf("clone config = %#v", created[0].Config)
	}
	if _, volatile := created[0].Config["volatile.eth0.hwaddr"]; volatile {
		t.Fatalf("volatile keys must not travel with the clone: %#v", created[0].Config)
	}
	if created[0].Type != api.InstanceTypeContainer {
		t.Fatalf("clone type = %#v", created[0].Type)
	}
	// Copies must not carry Start (the daemon rejects it); the clone is
	// powered on through a separate state call.
	if created[0].Start {
		t.Fatalf("clone start = %#v", created[0].Start)
	}
	states := source.UpdateStateArgs()
	if len(states) != 1 || states[0].Action != "start" {
		t.Fatalf("state args = %#v", states)
	}

	if _, err := svc.CloneInstance(ctx, "c1", "web", CloneRequest{Target: ""}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("CloneInstance() empty target = %v", err)
	}
	if _, err := svc.CloneInstance(ctx, "c1", "missing", CloneRequest{Target: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CloneInstance() missing instance = %v", err)
	}
}

func TestRunOpsExecFailure(t *testing.T) {
	source := &incusfake.Server{ExecErr: errors.New("agent down")}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": source})

	if _, err := svc.RunOps(context.Background(), "c1", "web", OpsRequest{Action: "run-script", Script: "x"}); err == nil {
		t.Fatal("RunOps() expected exec error")
	}
	if _, err := svc.RunOps(context.Background(), "missing", "web", OpsRequest{Action: "run-script", Script: "x"}); err == nil {
		t.Fatal("RunOps() expected unknown cluster error")
	}
}

func TestListISOsErrors(t *testing.T) {
	source := &incusfake.Server{VolumesErr: errors.New("pool gone")}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": source})

	if _, err := svc.ListISOs("c1"); err == nil {
		t.Fatal("ListISOs() expected volume listing error")
	}
}
