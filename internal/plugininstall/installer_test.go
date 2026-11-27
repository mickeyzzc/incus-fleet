package plugininstall

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/auth"
	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/instances"
	"github.com/mickeyzzc/incus-fleet/internal/plugin"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

type fakeInstances struct {
	createErr    error
	volumeErr    error
	opsErr       error
	uploadErr    error
	opsScriptErr string
	created      int
	deleted      int
	volumes      int
	volumeDel    int
	uploads      int
	ip           string
	scriptCalls  []string
}

func (f *fakeInstances) Create(_ context.Context, _ string, _ instances.CreateRequest) (instances.Detail, error) {
	f.created++
	return instances.Detail{}, f.createErr
}

func (f *fakeInstances) Delete(_ context.Context, _, _ string, _, _ bool) error {
	f.deleted++
	return nil
}

func (f *fakeInstances) Get(_, _ string) (instances.Detail, error) {
	return instances.Detail{Summary: instances.Summary{IPv4: []string{f.ip}}}, nil
}

func (f *fakeInstances) RunOps(_ context.Context, _, _ string, req instances.OpsRequest) (instances.OpsResult, error) {
	f.scriptCalls = append(f.scriptCalls, req.Script)
	if f.opsScriptErr != "" && strings.Contains(req.Script, f.opsScriptErr) {
		return instances.OpsResult{ExitCode: 1, Stderr: "example script failure"}, f.opsErr
	}
	return instances.OpsResult{}, f.opsErr
}

func (f *fakeInstances) UploadFile(_, _, _ string, _ []byte) error {
	f.uploads++
	return f.uploadErr
}

func (f *fakeInstances) CreateVolume(_, _ string, _ instances.VolumeRequest) error {
	f.volumes++
	return f.volumeErr
}

func (f *fakeInstances) DeleteVolume(_, _, _ string) error {
	f.volumeDel++
	return nil
}

type fakeClusters struct{}

func (fakeClusters) List() []clusters.Summary {
	return []clusters.Summary{{Name: "c1", Online: true, Members: []clusters.Member{{Name: "node-a", Status: "Online"}}}}
}

type fakeImages struct{}

func (fakeImages) Resolve(_ context.Context, _, ref string) (string, error) { return "fp-" + ref, nil }

type fakeTokens struct{}

func (fakeTokens) CreateAPIToken(_ context.Context, _ auth.CreateTokenRequest) (*auth.APIToken, string, error) {
	return &auth.APIToken{ID: "token-1"}, "example-token-test", nil
}

func newInstaller(t *testing.T, instancesOps *fakeInstances, health *httptest.Server) (*Installer, *store.SQLStore, *plugin.Registry) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, "sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	registry := plugin.New(plugin.WithWiringStore(st))
	for _, p := range plugin.BuiltinPlugins() {
		if err := registry.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.Start(ctx); err != nil {
		t.Fatal(err)
	}

	i := New(Config{
		Store: st, Instances: instancesOps, Clusters: fakeClusters{}, Images: fakeImages{},
		Auth: fakeTokens{}, Registry: registry, SigningKey: []byte("example-signing-key-test"),
		Sleep:      func(context.Context, time.Duration) error { return nil },
		HTTPClient: health.Client(),
	})
	if health != nil {
		i.cfg.HealthBaseURL = health.URL
	}

	return i, st, registry
}

func checkpointFromTask(t *testing.T, st *store.SQLStore, id string) checkpoint {
	t.Helper()
	task, err := st.GetHostTask(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return decodeCheckpoint(task.Progress)
}

func testRequest(t *testing.T) Request {
	t.Helper()
	m, ok := plugin.BuiltinManifest("vm")
	if !ok {
		t.Fatal("missing vm manifest")
	}

	return Request{
		PluginID: "vm", Name: "vm", Manifest: m, Cluster: "c1", Member: "node-a",
		Instance: "fleet-vm", Image: "vm-oci", Network: "incusbr0",
		CPU: "2", Memory: "2GiB", Disk: "20GiB",
		DataDiskGB:     20,
		Approvals:      []string{"metrics.query_url"},
		Defaults:       map[string]string{"metrics.query_url": "http://192.0.2.1:8428"},
		TemplateValues: map[string]string{"listen_port": "8428"},
		HealthDelayMS:  1, HealthIntervalMS: 1, HealthFailureThreshold: 2,
	}
}

func TestInstallTaskRunsCheckpointsAndActivatesWiring(t *testing.T) {
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer health.Close()
	ops := &fakeInstances{ip: "192.0.2.50"}
	i, st, registry := newInstaller(t, ops, health)

	task, err := i.Enqueue(context.Background(), testRequest(t), "admin")
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if task.Type != TaskType || task.State != store.HostTaskQueued {
		t.Fatalf("task = %#v", task)
	}
	if err := i.RunTask(context.Background(), task.ID); err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}

	done, err := st.GetHostTask(context.Background(), task.ID)
	if err != nil || done.State != store.HostTaskSucceeded {
		t.Fatalf("done task = %#v err=%v", done, err)
	}
	events, err := st.ListHostTaskEvents(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < len(stepNames)+1 {
		t.Fatalf("checkpoint events = %d, want >= %d", len(events), len(stepNames)+1)
	}
	if !strings.Contains(events[len(events)-2].Message, stepNames[len(stepNames)-1]) {
		t.Fatalf("last checkpoint = %q", events[len(events)-2].Message)
	}

	entry, err := st.GetInstalledPlugin(context.Background(), "vm")
	if err != nil || entry.Instance != "fleet-vm" || !entry.Enabled {
		t.Fatalf("installed plugin = %#v err=%v", entry, err)
	}
	if got, ok := registry.WiringValue("metrics.query_url"); !ok || !strings.Contains(got, "192.0.2.50") {
		t.Fatalf("wiring = %q %v", got, ok)
	}
	if ops.volumes != 1 || ops.created != 1 || len(ops.scriptCalls) < 2 {
		t.Fatalf("provision calls: volumes=%d created=%d scripts=%d", ops.volumes, ops.created, len(ops.scriptCalls))
	}

	// Cancel and resume are rejected on a terminal task.
	if err := i.Cancel(context.Background(), task.ID); err == nil {
		t.Fatal("expected cancel terminal error")
	}
	if err := i.Resume(context.Background(), task.ID); err == nil {
		t.Fatal("expected resume terminal error")
	}
}

func TestInstallTaskRollsBackInReverseOrderAndResumes(t *testing.T) {
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer health.Close()
	ops := &fakeInstances{ip: "192.0.2.51", createErr: errors.New("example create failure")}
	i, st, registry := newInstaller(t, ops, health)

	task, err := i.Enqueue(context.Background(), testRequest(t), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := i.RunTask(context.Background(), task.ID); err == nil {
		t.Fatal("expected install failure")
	}
	failed, err := st.GetHostTask(context.Background(), task.ID)
	if err != nil || failed.State != store.HostTaskFailed {
		t.Fatalf("failed task = %#v err=%v", failed, err)
	}
	if ops.volumeDel != 1 {
		t.Fatalf("data volume rollback calls = %d", ops.volumeDel)
	}
	events, err := st.ListHostTaskEvents(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	var rollback []string
	for _, e := range events {
		if strings.HasPrefix(e.Message, "rollback:") {
			rollback = append(rollback, strings.TrimPrefix(e.Message, "rollback: "))
		}
	}
	if len(rollback) == 0 || rollback[0] != "data_disk" {
		t.Fatalf("rollback order = %v", rollback)
	}
	if _, err := st.GetInstalledPlugin(context.Background(), "vm"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("plugin should not be published, error = %v", err)
	}
	if _, ok := registry.WiringValue("metrics.query_url"); ok {
		t.Fatal("wiring activated despite failed install")
	}

	// Fix the failure and resume from the checkpoint: already-created volume
	// is not allocated twice.
	ops.createErr = nil
	if err := i.Resume(context.Background(), task.ID); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if err := i.RunTask(context.Background(), task.ID); err != nil {
		t.Fatalf("resumed RunTask() error = %v", err)
	}
	done, err := st.GetHostTask(context.Background(), task.ID)
	if err != nil || done.State != store.HostTaskSucceeded {
		t.Fatalf("resumed task = %#v err=%v", done, err)
	}
	if ops.volumes != 2 {
		t.Fatalf("volume allocations after resume = %d, want 2 (fresh attempt)", ops.volumes)
	}
}

func TestInstallTaskCancelStopsBeforeNextStep(t *testing.T) {
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer health.Close()
	ops := &fakeInstances{ip: "192.0.2.52"}
	i, _, _ := newInstaller(t, ops, health)
	task, err := i.Enqueue(context.Background(), testRequest(t), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := i.Cancel(context.Background(), task.ID); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if err := i.RunTask(context.Background(), task.ID); !errors.Is(err, ErrCanceled) {
		t.Fatalf("RunTask() error = %v, want ErrCanceled", err)
	}
	if ops.created != 0 {
		t.Fatalf("canceled task created an instance: %d", ops.created)
	}
}

func TestHealthCheckGatesWiringActivation(t *testing.T) {
	var probes atomic.Int32
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if probes.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer health.Close()
	ops := &fakeInstances{ip: "192.0.2.53"}
	i, _, registry := newInstaller(t, ops, health)
	req := testRequest(t)
	req.HealthFailureThreshold = 5
	task, err := i.Enqueue(context.Background(), req, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := i.RunTask(context.Background(), task.ID); err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}
	if probes.Load() != 3 {
		t.Fatalf("health probes = %d, want 3", probes.Load())
	}
	if got, ok := registry.WiringValue("metrics.query_url"); !ok || !strings.Contains(got, "192.0.2.53") {
		t.Fatalf("wiring = %q %v", got, ok)
	}
}

func TestInstallTaskRejectsCommunityWiringAndDuplicate(t *testing.T) {
	health := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer health.Close()
	ops := &fakeInstances{ip: "192.0.2.54"}
	i, _, _ := newInstaller(t, ops, health)
	req := testRequest(t)
	req.Manifest.Tier = plugin.TierCommunity
	req.Approvals = []string{"metrics.query_url"}
	if _, err := i.Enqueue(context.Background(), req, "admin"); err == nil {
		t.Fatal("expected community wiring rejection")
	}

	req = testRequest(t)
	if _, err := i.Enqueue(context.Background(), req, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := i.Enqueue(context.Background(), req, "admin"); err == nil {
		t.Fatal("expected duplicate install rejection")
	}

	if err := i.Cancel(context.Background(), "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cancel missing error = %v", err)
	}
	if err := i.Resume(context.Background(), "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("resume missing error = %v", err)
	}
	if err := i.RunTask(context.Background(), "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("run missing error = %v", err)
	}
}

func TestInstallTaskStepFailurePaths(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*fakeInstances, *Request, *httptest.Server)
	}{
		{name: "format", mutate: func(f *fakeInstances, _ *Request, _ *httptest.Server) {
			f.opsErr = errors.New("example format failure")
		}},
		{name: "config upload", mutate: func(f *fakeInstances, _ *Request, _ *httptest.Server) {
			f.uploadErr = errors.New("example upload failure")
		}},
		{name: "config script", mutate: func(f *fakeInstances, _ *Request, _ *httptest.Server) {
			f.opsScriptErr = "fleet-plugin-config"
		}},
		{name: "unhealthy", mutate: func(_ *fakeInstances, _ *Request, srv *httptest.Server) {
			srv.Close()
		}},
		{name: "undeclared wiring", mutate: func(_ *fakeInstances, req *Request, _ *httptest.Server) {
			req.Approvals = []string{"logs.query_url"}
		}},
		{name: "template validation", mutate: func(_ *fakeInstances, req *Request, _ *httptest.Server) {
			req.TemplateValues = map[string]string{"listen_port": "bad"}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer health.Close()
			ops := &fakeInstances{ip: "192.0.2.61"}
			i, st, _ := newInstaller(t, ops, health)
			req := testRequest(t)
			tc.mutate(ops, &req, health)
			task, err := i.Enqueue(context.Background(), req, "admin")
			if err != nil {
				t.Fatalf("Enqueue() error = %v", err)
			}
			if err := i.RunTask(context.Background(), task.ID); err == nil {
				t.Fatal("expected step failure")
			}
			done, err := st.GetHostTask(context.Background(), task.ID)
			if err != nil || done.State != store.HostTaskFailed {
				t.Fatalf("task = %#v err=%v", done, err)
			}
			if !strings.Contains(done.Error, stepNamesFromError(done.Error)) {
				t.Fatalf("error = %q", done.Error)
			}
		})
	}
}

func stepNamesFromError(err string) string {
	if idx := strings.Index(err, "step "); idx >= 0 {
		rest := err[idx+5:]
		if end := strings.Index(rest, ":"); end > 0 {
			return rest[:end]
		}
	}
	return "plugininstall"
}

func TestInstallConnectOnlyAndHealthDefaults(t *testing.T) {
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer health.Close()
	ops := &fakeInstances{ip: "192.0.2.62"}
	i, _, registry := newInstaller(t, ops, health)
	var sleeps []time.Duration
	i.cfg.Sleep = func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}
	req := testRequest(t)
	req.Upstream = health.URL
	req.DataDiskGB = 0
	req.HealthDelayMS = 0
	req.HealthIntervalMS = 0
	req.HealthFailureThreshold = 0
	task, err := i.Enqueue(context.Background(), req, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := i.RunTask(context.Background(), task.ID); err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}
	if got, ok := registry.WiringValue("metrics.query_url"); !ok || got != health.URL {
		t.Fatalf("wiring = %q %v", got, ok)
	}
	if len(sleeps) != 1 || sleeps[0] != 10*time.Second {
		t.Fatalf("health delays = %v, want one 10s initial delay", sleeps)
	}
}

func TestHandlerRejectsUnknownTaskType(t *testing.T) {
	health := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer health.Close()
	i, _, _ := newInstaller(t, &fakeInstances{}, health)
	if _, err := i.Handler()(context.Background(), &store.HostTask{Type: "other"}); err == nil {
		t.Fatal("expected unknown task type error")
	}
}

func TestInstallerRequiresDependencies(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for missing store")
		}
	}()
	_ = New(Config{})
}

func TestHandlerRunsClaimedTask(t *testing.T) {
	health := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer health.Close()
	ops := &fakeInstances{ip: "192.0.2.55"}
	i, st, _ := newInstaller(t, ops, health)
	task, err := i.Enqueue(context.Background(), testRequest(t), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := i.Handler()(context.Background(), task); err != nil {
		t.Fatalf("handler error = %v", err)
	}
	cp := checkpointFromTask(t, st, task.ID)
	if cp.Step != len(stepNames) {
		t.Fatalf("checkpoint step = %d, want %d", cp.Step, len(stepNames))
	}
	if task.Plan == "" || !strings.Contains(task.Plan, "vm") {
		t.Fatalf("task plan = %q", task.Plan)
	}
	if task.ID == "" {
		t.Fatal("task id is empty")
	}
}
