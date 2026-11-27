package httpapi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/plugin"
)

func TestCatalogItemFromManifestBranches(t *testing.T) {
	m, ok := plugin.BuiltinManifest("vm")
	if !ok {
		t.Fatal("missing vm manifest")
	}
	m.Capabilities.Wiring = nil
	m.Artifact.Image.Reference = ""
	m.Capabilities.ProxyUI.PathPrefix = ""
	item := catalogItemFromManifest(&m)
	if item.Wiring != "" || item.Image != "" || item.UIPath != "/plugin/vm" {
		t.Fatalf("item = %#v", item)
	}
	m.Artifact.Image.Reference = "example.com/plugins/test:1"
	m.Capabilities.ProxyUI.UpstreamPort = 0
	m.Capabilities.ProxyAPI.UpstreamPort = 9090
	m.Capabilities.ProxyUI.PathPrefix = "/plugin-ui/vm/"
	item = catalogItemFromManifest(&m)
	if item.Port != 9090 || item.UIPath != "/plugin-ui/vm" {
		t.Fatalf("item = %#v", item)
	}
}

func TestPluginCatalogSkipBranches(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"no-manifest", "bad-manifest", "id-mismatch", "vm"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	bad := []byte("not a manifest")
	if err := os.WriteFile(filepath.Join(dir, "bad-manifest", "manifest.yaml"), bad, 0o600); err != nil {
		t.Fatal(err)
	}
	m, _ := plugin.BuiltinManifest("vm")
	if err := os.WriteFile(filepath.Join(dir, "id-mismatch", "manifest.yaml"),
		[]byte("schema: fleet-plugin/v1\nid: other\nname: Other\nversion: 1.0.0\ntier: official\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = m
	if err := os.WriteFile(filepath.Join(dir, "vm", "manifest.yaml"), bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vm.json"), []byte(`{"id":"vm","name":"duplicate"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "read-error.json"), []byte(`{"id":"x","name":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "read-error.json"), 0o000); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &API{pluginsConfig: config.PluginsConfig{Dir: dir}}
	items := a.pluginCatalog()
	if len(items) != len(builtinPluginCatalog) {
		t.Fatalf("catalog leaked skipped entries: %#v", items)
	}
}
