package httpapi

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/auth"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/plugin"
	"github.com/mickeyzzc/incus-fleet/internal/plugininstall"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestInstallTaskManifestSignatureAndStoreErrors(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "community-task")
	if err := os.MkdirAll(pkg, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`schema: fleet-plugin/v1
id: community-task
name: Community Task
version: 1.0.0
publisher: community-example
tier: community
description: Task signature test
homepage: https://example.com/plugins/community-task
artifact:
  image:
    source: registry
    reference: example.com/plugins/test:1.0.0
    architectures: [amd64]
install:
  defaults:
    cpu: 1
    memory_mb: 512
    root_disk_gb: 10
    network: managed
  data_disk:
    filesystem: ext4
    mount_point: /data
health:
  endpoint: /health
`)
	if err := os.WriteFile(filepath.Join(pkg, "manifest.yaml"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	st, err := store.Open(ctx, "sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	adapter := auth.NewStoreAdapter(st)
	authn := auth.New("secret", time.Hour, auth.WithUserStore(adapter), auth.WithTokenStore(adapter))
	registry := plugin.New(plugin.WithWiringStore(st))
	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, authn, func(d *Deps) {
		d.Store = st
		d.Plugins = config.PluginsConfig{Enabled: true, Dir: dir, SigningKey: "task-signing-key"}
		d.Registry = registry
		d.Installer = plugininstall.New(plugininstall.Config{Store: st, Registry: registry})
	})
	cookie := f.login(t)
	rec := f.request(t, http.MethodPost, "/api/v1/plugin-installs", `{"id":"community-task"}`, cookie, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unsigned manifest = %d body=%s", rec.Code, rec.Body.String())
	}

	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/v1/plugin-installs"},
		{http.MethodGet, "/api/v1/plugin-installs/missing"},
		{http.MethodPost, "/api/v1/plugin-installs/missing/cancel"},
		{http.MethodPost, "/api/v1/plugin-installs/missing/resume"},
	} {
		rec := f.request(t, tc.method, tc.path, "", cookie, "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s %s = %d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}
