package plugininstall

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestValidateRequiresInstanceForDataDisk(t *testing.T) {
	req := testRequest(t)
	req.Instance = ""
	if err := validateRequest(req, nil); err == nil {
		t.Fatal("expected missing instance error for data disk")
	}
	req.DataDiskGB = 0
	req.Manifest.Install.DataDisk.Required = false
	if err := validateRequest(req, nil); err != nil {
		t.Fatalf("no data disk without instance: %v", err)
	}
}

type recordingImageCopier struct {
	from, fingerprint, to, alias string
}

func (c *recordingImageCopier) CopyImageTo(_ context.Context, from, fingerprint, to, alias string) error {
	c.from, c.fingerprint, c.to, c.alias = from, fingerprint, to, alias
	return nil
}

type importResolver struct{ err error }

func (r importResolver) Resolve(context.Context, string, string) (string, error) {
	return "", errors.New("not local")
}

func (r importResolver) Import(context.Context, string, string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	return "fp-imported", nil
}

func TestInstallImportsMissingImage(t *testing.T) {
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer health.Close()
	i, st, _ := newInstaller(t, &fakeInstances{ip: "192.0.2.50"}, health)
	i.cfg.Images = importResolver{}
	task, err := i.Enqueue(context.Background(), testRequest(t), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := i.RunTask(context.Background(), task.ID); err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}
	cp := checkpointFromTask(t, st, task.ID)
	if cp.Image != "fp-imported" {
		t.Fatalf("checkpoint image = %#v", cp)
	}

	failing, _, _ := newInstaller(t, &fakeInstances{ip: "192.0.2.50"}, health)
	failing.cfg.Images = importResolver{err: errors.New("copy failed")}
	bad, err := failing.Enqueue(context.Background(), testRequest(t), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := failing.RunTask(context.Background(), bad.ID); err == nil {
		t.Fatal("expected image import failure")
	}
}

func TestConnectOnlyInstallSkipsProvisioning(t *testing.T) {
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer health.Close()
	ops := &fakeInstances{ip: "192.0.2.50"}
	i, st, registry := newInstaller(t, ops, health)
	req := testRequest(t)
	req.ConnectOnly = true
	req.Upstream = health.URL
	req.Instance = ""
	req.DataDiskGB = 0
	req.Image = ""
	req.Cluster = ""
	req.Member = ""
	task, err := i.Enqueue(context.Background(), req, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := i.RunTask(context.Background(), task.ID); err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}
	done, err := st.GetHostTask(context.Background(), task.ID)
	if err != nil || done.State != store.HostTaskSucceeded {
		t.Fatalf("task = %#v err=%v", done, err)
	}
	entry, err := st.GetInstalledPlugin(context.Background(), "vm")
	if err != nil || entry.Upstream != health.URL || entry.Instance != "" || entry.Cluster != "" {
		t.Fatalf("entry = %#v err=%v", entry, err)
	}
	if got, ok := registry.WiringValue("metrics.query_url"); !ok || got != health.URL {
		t.Fatalf("wiring = %q %v", got, ok)
	}
	if ops.created != 0 {
		t.Fatalf("connect-only install created %d instances", ops.created)
	}
}

func TestHealthFailureIsRecorded(t *testing.T) {
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer health.Close()
	i, st, _ := newInstaller(t, &fakeInstances{ip: "192.0.2.50"}, health)
	i.cfg.HealthBaseURL = health.URL
	req := testRequest(t)
	req.HealthFailureThreshold = 1
	task, err := i.Enqueue(context.Background(), req, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := i.RunTask(context.Background(), task.ID); err == nil {
		t.Fatal("expected health failure")
	}
	samples, err := st.ListPluginHealth(context.Background(), "vm", 10)
	if err != nil || len(samples) == 0 || samples[0].Status != "failed" {
		t.Fatalf("health samples = %#v err=%v", samples, err)
	}
}

func TestConfiguredWebhookSecretIsUsed(t *testing.T) {
	i, _, _ := newInstaller(t, &fakeInstances{ip: "192.0.2.50"}, httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	i.cfg.WebhookSecret = []byte("operator-secret")
	if got := string(i.webhookSecret()); got != "operator-secret" {
		t.Fatalf("webhook secret = %q", got)
	}
}

func TestImageImportErrorBranches(t *testing.T) {
	ctx := context.Background()
	if _, err := (ClusterImageResolver{}).Import(ctx, "c1", "alias"); err == nil {
		t.Fatal("expected missing copier error")
	}
	source := &incusfake.Server{Images: []api.Image{{
		Fingerprint: "fp-2", Aliases: []api.ImageAlias{{Name: "vm-oci"}},
	}}}
	cfg := []config.Cluster{{Name: "c1", Endpoint: "https://192.0.2.10:8443"}, {Name: "c2", Endpoint: "https://192.0.2.11:8443"}}
	dial := func(_ context.Context, c config.Cluster) (clusters.InstanceServer, error) {
		if c.Name == "c1" {
			return &incusfake.Server{}, nil
		}
		return source, nil
	}
	manager, err := clusters.NewWithDialer(ctx, cfg, dial, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	resolver := ClusterImageResolver{Manager: manager}
	if _, err := resolver.Import(ctx, "c1", "vm-oci"); err == nil {
		t.Fatal("expected missing copier error")
	}
	resolver.Copier = &failingImageCopier{}
	if _, err := resolver.Import(ctx, "c1", "vm-oci"); err == nil {
		t.Fatal("expected copy error")
	}
	if _, err := resolver.Import(ctx, "c1", "absent"); err == nil {
		t.Fatal("expected absent image error")
	}
}

type failingImageCopier struct{}

func (failingImageCopier) CopyImageTo(context.Context, string, string, string, string) error {
	return errors.New("copy failed")
}

func TestClusterImageResolverImportsMissingAlias(t *testing.T) {
	source := &incusfake.Server{}
	target := &incusfake.Server{Images: []api.Image{{
		Fingerprint: "fp-1", Aliases: []api.ImageAlias{{Name: "example-image"}},
	}}}
	cfg := []config.Cluster{{Name: "c1", Endpoint: "https://192.0.2.10:8443"}, {Name: "c2", Endpoint: "https://192.0.2.11:8443"}}
	dial := func(_ context.Context, c config.Cluster) (clusters.InstanceServer, error) {
		if c.Name == "c1" {
			return target, nil
		}
		return source, nil
	}
	manager, err := clusters.NewWithDialer(context.Background(), cfg, dial, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	// The source cluster holds the alias the target cluster is missing. The
	// fake server accepts a copy without mutating its image list, so the fake
	// copier records the call instead.
	source.Images = []api.Image{{Fingerprint: "fp-2", Aliases: []api.ImageAlias{{Name: "vm-oci"}}}}
	copier := &recordingImageCopier{}
	resolver := ClusterImageResolver{Manager: manager, Copier: copier}

	if _, err := resolver.Resolve(context.Background(), "c1", "vm-oci"); err == nil {
		t.Fatal("expected missing alias on target")
	}
	got, err := resolver.Import(context.Background(), "c1", "vm-oci")
	if err != nil || got != "fp-2" {
		t.Fatalf("Import() = %q err=%v", got, err)
	}
	if copier.from != "c2" || copier.fingerprint != "fp-2" || copier.to != "c1" || copier.alias != "vm-oci" {
		t.Fatalf("copy args = %#v", copier)
	}
}
