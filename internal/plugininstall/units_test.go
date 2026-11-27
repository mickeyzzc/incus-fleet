package plugininstall

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/auth"
	"github.com/mickeyzzc/incus-fleet/internal/plugin"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

func TestValidateRequestBranches(t *testing.T) {
	valid := testRequest(t)
	key := []byte("example-signing-key-test")
	valid.SignedManifestBytes = []byte("manifest bytes")
	valid.ManifestSignature = plugin.SignPayload(key, valid.SignedManifestBytes)

	tests := []struct {
		name string
		req  Request
		want bool
	}{
		{name: "valid", req: valid, want: true},
		{name: "empty id", req: Request{}, want: false},
		{name: "mismatched id", req: Request{PluginID: "other", Manifest: valid.Manifest}, want: false},
		{name: "invalid manifest", req: func() Request { r := valid; r.Manifest.Schema = "bad"; return r }(), want: false},
		{name: "community wiring", req: func() Request {
			r := valid
			r.Manifest.Tier = plugin.TierCommunity
			r.Approvals = []string{"metrics.query_url"}
			return r
		}(), want: false},
		{name: "bad signature", req: func() Request { r := valid; r.ManifestSignature = "bad"; return r }(), want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRequest(tc.req, key)
			if (err == nil) != tc.want {
				t.Fatalf("validateRequest() error = %v, want success=%v", err, tc.want)
			}
		})
	}
	if string(valid.ManifestRaw()) != "manifest bytes" {
		t.Fatalf("ManifestRaw() = %q", valid.ManifestRaw())
	}
	empty := testRequest(t)
	if empty.ManifestRaw() != nil {
		t.Fatalf("ManifestRaw() = %q, want nil", empty.ManifestRaw())
	}
}

func TestNewDefaultsAndCheckpointDecoding(t *testing.T) {
	st, err := store.Open(context.Background(), "sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	i := New(Config{Store: st})
	if i.cfg.Sleep == nil || i.cfg.HTTPClient == nil || i.cfg.Logger == nil {
		t.Fatal("defaults were not applied")
	}
	if err := i.cfg.Sleep(context.Background(), 0); err != nil {
		t.Fatalf("Sleep() error = %v", err)
	}

	for _, raw := range []string{"", "not json", `{"step":-1}`, `{"step":999}`} {
		if cp := decodeCheckpoint(raw); cp.Step != 0 {
			t.Fatalf("decodeCheckpoint(%q) = %#v", raw, cp)
		}
	}
	if cp := decodeCheckpoint(`{"step":3,"instance":"x"}`); cp.Step != 3 || cp.Instance != "x" {
		t.Fatalf("decodeCheckpoint() = %#v", cp)
	}
}

func TestHealthPortFallbacks(t *testing.T) {
	req := &Request{
		TemplateValues: map[string]string{"listen_port": "9000"},
		Manifest: plugin.Manifest{
			Install:      plugin.Install{Templates: []plugin.Template{{Name: "other", Type: plugin.TemplatePort, Default: "7000"}}},
			Capabilities: plugin.Capabilities{ProxyUI: plugin.ProxyTarget{UpstreamPort: 8000}},
		},
	}
	if got := healthPort(req); got != 9000 {
		t.Fatalf("healthPort() = %d", got)
	}
	req.TemplateValues = nil
	if got := healthPort(req); got != 7000 {
		t.Fatalf("healthPort() default = %d", got)
	}
	req.Manifest.Install.Templates = nil
	if got := healthPort(req); got != 8000 {
		t.Fatalf("healthPort() proxy = %d", got)
	}
	req.Manifest.Capabilities.ProxyUI.UpstreamPort = 0
	if got := healthPort(req); got != 8080 {
		t.Fatalf("healthPort() fallback = %d", got)
	}
}

func TestRunStepMissingDependencies(t *testing.T) {
	req := testRequest(t)
	req.Cluster = ""
	cp := checkpoint{}
	ctx := context.Background()
	i := New(Config{Store: mustStore(t)})
	if err := i.runStep(ctx, &req, &cp, 1); err == nil {
		t.Fatal("expected placement error without clusters")
	}
	if err := i.runStep(ctx, &req, &cp, 2); err == nil {
		t.Fatal("expected image resolver error")
	}
	if err := i.runStep(ctx, &req, &cp, 5); err == nil {
		t.Fatal("expected instance service error")
	}
	if err := i.runStep(ctx, &req, &cp, 8); err == nil {
		t.Fatal("expected registry error")
	}
}

func TestRunStepUsesManifestDataDiskDefaults(t *testing.T) {
	st := mustStore(t)
	ops := &fakeInstances{ip: "192.0.2.63"}
	i := New(Config{Store: st, Instances: ops, SigningKey: []byte("example-signing-key-test")})
	req := testRequest(t)
	req.DataDiskGB = 0
	req.Manifest.Install.DataDisk.Required = true
	req.Manifest.Install.DataDisk.DefaultGB = 25
	req.Manifest.Install.DataDisk.Filesystem = plugin.FSXFS
	cp := checkpoint{Cluster: "c1", Image: "fp-vm"}
	ctx := context.Background()
	if err := i.runStep(ctx, &req, &cp, 3); err != nil {
		t.Fatalf("data disk step error = %v", err)
	}
	if cp.Volume == "" || cp.StoragePool != "default" || req.Filesystem != plugin.FSXFS {
		t.Fatalf("checkpoint = %#v filesystem=%q", cp, req.Filesystem)
	}
	if ops.volumes != 1 {
		t.Fatalf("volume allocations = %d", ops.volumes)
	}
}

func mustStore(t *testing.T) *store.SQLStore {
	t.Helper()
	st, err := store.Open(context.Background(), "sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	return st
}

type failingTokens struct{}

func (failingTokens) CreateAPIToken(context.Context, auth.CreateTokenRequest) (*auth.APIToken, string, error) {
	return nil, "", errors.New("example token failure")
}

func TestRunStepAdditionalBranches(t *testing.T) {
	ctx := context.Background()
	req := testRequest(t)
	cp := checkpoint{}

	// Placement discovers cluster and member from the lister.
	req.Cluster = ""
	req.Member = ""
	i := New(Config{Store: mustStore(t), Clusters: fakeClusters{}})
	if err := i.runStep(ctx, &req, &cp, 1); err != nil {
		t.Fatalf("placement error = %v", err)
	}
	if cp.Cluster != "c1" || cp.Member != "node-a" {
		t.Fatalf("placement = %#v", cp)
	}

	// Volume allocation failures surface with context.
	ops := &fakeInstances{volumeErr: errors.New("example volume failure")}
	i = New(Config{Store: mustStore(t), Instances: ops})
	req = testRequest(t)
	if err := i.runStep(ctx, &req, &cp, 3); err == nil || !strings.Contains(err.Error(), "data volume") {
		t.Fatalf("volume error = %v", err)
	}

	// Instance creation falls back to manifest and built-in defaults.
	ops = &fakeInstances{}
	i = New(Config{Store: mustStore(t), Instances: ops})
	req = testRequest(t)
	req.CPU = ""
	req.Memory = ""
	req.Disk = ""
	req.Network = ""
	req.Instance = ""
	cp = checkpoint{Cluster: "c1", Image: "fp-vm", Volume: "plugin-vm-data", StoragePool: "default"}
	if err := i.runStep(ctx, &req, &cp, 5); err != nil {
		t.Fatalf("create error = %v", err)
	}
	if cp.Instance != "fleet-vm" {
		t.Fatalf("instance = %q", cp.Instance)
	}

	// Token issuance failure prevents publication.
	i = New(Config{Store: mustStore(t), Auth: failingTokens{}})
	cp = checkpoint{Cluster: "c1", Instance: "fleet-vm", Upstream: "http://192.0.2.30:8428"}
	if err := i.runStep(ctx, &req, &cp, 9); err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("token error = %v", err)
	}

	// An unregistered manifest is registered before wiring is activated.
	st := mustStore(t)
	registry := plugin.New()
	req = testRequest(t)
	req.Manifest.ID = "other-plugin"
	req.Approvals = nil
	i = New(Config{Store: st, Registry: registry})
	if err := i.runStep(ctx, &req, &cp, 8); err != nil {
		t.Fatalf("dynamic registration error = %v", err)
	}
	if _, ok := registry.Get("other-plugin"); !ok {
		t.Fatal("manifest plugin was not registered")
	}
}

func TestClosedStoreFailurePaths(t *testing.T) {
	st, err := store.Open(context.Background(), "sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	i := New(Config{Store: st})
	if _, err := i.Enqueue(context.Background(), testRequest(t), "admin"); err == nil {
		t.Fatal("expected enqueue error")
	}
	if err := i.RunTask(context.Background(), "missing"); err == nil {
		t.Fatal("expected run error")
	}
	if err := i.Cancel(context.Background(), "missing"); err == nil {
		t.Fatal("expected cancel error")
	}
	if err := i.Resume(context.Background(), "missing"); err == nil {
		t.Fatal("expected resume error")
	}
	if err := i.execute(context.Background(), &store.HostTask{ID: "x", Plan: `{"id":"vm"}`}); err == nil {
		t.Fatal("expected execute error")
	}
	if err := i.saveProgress(context.Background(), &store.HostTask{ID: "x"}, checkpoint{}, 0); err == nil {
		t.Fatal("expected saveProgress error")
	}
	i.finish(context.Background(), &store.HostTask{ID: "x", Type: TaskType}, errors.New("example failure"))
}

func TestWaitHealthyStopsOnCanceledSleep(t *testing.T) {
	st := mustStore(t)
	i := New(Config{Store: st, HTTPClient: &http.Client{}, Sleep: func(context.Context, time.Duration) error {
		return errors.New("example cancellation")
	}})
	req := testRequest(t)
	req.HealthDelayMS = 1
	cp := checkpoint{Cluster: "c1", Instance: "fleet-vm"}
	if _, err := i.waitHealthy(context.Background(), &req, &cp); err == nil || !strings.Contains(err.Error(), "cancellation") {
		t.Fatalf("waitHealthy() error = %v", err)
	}
}
