package plugin

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// These cover the remaining documented branches: option plumbing, wiring value
// resolution precedence, and manifest fields not exercised by the main table.

func TestWithHostIsPassedToPlugins(t *testing.T) {
	r := New(WithHost(Host{Version: "1.2.3"}))

	p := &hostRecorder{fakePlugin: newFake(t, "alpha")}
	if err := r.Register(p); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if p.host.Version != "1.2.3" {
		t.Fatalf("host.Version = %q, want 1.2.3", p.host.Version)
	}
}

type hostRecorder struct {
	*fakePlugin
	host Host
}

func (h *hostRecorder) Init(host Host) error {
	h.host = host

	return h.fakePlugin.Init(host)
}

func TestWiringValueResolvesFirstDeclaringPlugin(t *testing.T) {
	r := New()

	alpha := newFake(t, "alpha")
	bravo := newFake(t, "bravo")
	for _, p := range []*fakePlugin{alpha, bravo} {
		if err := r.Register(p); err != nil {
			t.Fatalf("Register() error = %v", err)
		}
	}

	if _, ok := r.WiringValue("metrics.query_url"); ok {
		t.Fatal("WiringValue() reported a value before any override")
	}

	if err := r.SetWiring("bravo", "metrics.query_url", "http://127.0.0.1:8428"); err != nil {
		t.Fatalf("SetWiring() error = %v", err)
	}

	got, ok := r.WiringValue("metrics.query_url")
	if !ok || got != "http://127.0.0.1:8428" {
		t.Fatalf("WiringValue() = %q, %v", got, ok)
	}

	// alpha sorts first and also declares the key, so its override wins.
	if err := r.SetWiring("alpha", "metrics.query_url", "http://127.0.0.1:9090"); err != nil {
		t.Fatalf("SetWiring(alpha) error = %v", err)
	}
	got, _ = r.WiringValue("metrics.query_url")
	if got != "http://127.0.0.1:9090" {
		t.Fatalf("WiringValue() = %q, want the first declaring plugin's value", got)
	}
}

func TestRegisterNilPlugin(t *testing.T) {
	r := New()
	if err := r.Register(nil); err == nil {
		t.Fatal("Register(nil) = nil, want error")
	}
}

func TestRegisterDefaultOptions(t *testing.T) {
	// New() with no options must still produce a usable registry.
	r := New()
	p := newFake(t, "alpha")
	if err := r.Register(p); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestManifestBranches(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Manifest)
		want string
	}{
		{
			name: "negative resources",
			mut:  func(m *Manifest) { m.Install.Defaults.CPU = -1 },
			want: "negative",
		},
		{
			name: "negative data disk",
			mut:  func(m *Manifest) { m.Install.DataDisk.DefaultGB = -1 },
			want: "negative",
		},
		{
			name: "required data disk without size",
			mut:  func(m *Manifest) { m.Install.DataDisk.DefaultGB = 0 },
			want: "positive",
		},
		{
			name: "proxy ui port out of range",
			mut:  func(m *Manifest) { m.Capabilities.ProxyUI.UpstreamPort = 0 },
			want: "upstream_port",
		},
		{
			name: "proxy api path not absolute",
			mut:  func(m *Manifest) { m.Capabilities.ProxyAPI.Paths = []string{"query"} },
			want: "proxy_api.paths",
		},
		{
			name: "proxy api port out of range",
			mut:  func(m *Manifest) { m.Capabilities.ProxyAPI.UpstreamPort = 70000 },
			want: "upstream_port",
		},
		{
			name: "event webhook relative path",
			mut:  func(m *Manifest) { m.Capabilities.EventWebhook.Path = "events" },
			want: "event_webhook.path",
		},
		{
			name: "event webhook without events",
			mut:  func(m *Manifest) { m.Capabilities.EventWebhook.Events = nil },
			want: "event_webhook.events",
		},
		{
			name: "wiring without key",
			mut: func(m *Manifest) {
				m.Capabilities.Wiring = []Wiring{{Effect: "x"}}
			},
			want: "key",
		},
		{
			name: "bool template with bad default",
			mut: func(m *Manifest) {
				m.Install.Templates = []Template{{Name: "x", Type: TemplateBool, Default: "yes"}}
			},
			want: "boolean",
		},
		{
			name: "text template with default is fine",
			mut: func(m *Manifest) {
				m.Install.Templates = []Template{{Name: "x", Type: TemplateText, Default: "v"}}
			},
			want: "",
		},
		{
			name: "port template with non-numeric default",
			mut: func(m *Manifest) {
				m.Install.Templates = []Template{{Name: "x", Type: TemplatePort, Default: "abc"}}
			},
			want: "valid port",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := mustParse(t, validManifestYAML)
			tc.mut(m)
			err := m.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}

				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() error = %q, want %q", err, tc.want)
			}
		})
	}
}

func TestWiringValueClearedAfterClearWiring(t *testing.T) {
	r := New()
	if err := r.Register(newFake(t, "alpha")); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := r.SetWiring("alpha", "metrics.query_url", "http://127.0.0.1:8428"); err != nil {
		t.Fatalf("SetWiring() error = %v", err)
	}
	if err := r.ClearWiring("alpha"); err != nil {
		t.Fatalf("ClearWiring() error = %v", err)
	}
	if _, ok := r.WiringValue("metrics.query_url"); ok {
		t.Fatal("WiringValue() still reports cleared override")
	}
}

func TestClearWiringUnknownPlugin(t *testing.T) {
	r := New()
	if err := r.ClearWiring("nope"); err == nil {
		t.Fatal("ClearWiring() = nil, want unknown plugin error")
	}
}

func TestGetNilPluginManifest(t *testing.T) {
	// A plugin whose Manifest() panics is a programming error, but a nil
	// interface must be rejected rather than dereferenced.
	r := New()
	var nilPlugin Plugin
	if err := r.Register(nilPlugin); err == nil {
		t.Fatal("Register(nil interface) = nil, want error")
	}
}

// ParseManifest must reject structurally valid YAML that fails validation.
func TestParseManifestRejectsInvalidManifest(t *testing.T) {
	const missingName = `
schema: fleet-plugin/v1
id: ok
version: 1.0.0
tier: official
artifact:
  image:
    source: registry
    reference: example.com/x:1
    architectures: [amd64]
install:
  defaults:
    network: managed
  data_disk:
    filesystem: ext4
    mount_point: /data
health:
  endpoint: /health
`
	if _, err := ParseManifest([]byte(missingName)); err == nil {
		t.Fatal("ParseManifest() = nil, want name validation error")
	}
}

// Start failure during Init rolls back nothing because nothing started yet.
func TestRegistryStartInitFailure(t *testing.T) {
	r := New()
	p := newFake(t, "alpha")
	p.initErr = errors.New("init boom")
	if err := r.Register(p); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	err := r.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "init") {
		t.Fatalf("Start() error = %v, want init failure", err)
	}
}

// A second Start after a failed Start works, because the registry reset state.
func TestRegistryStartRetryAfterFailure(t *testing.T) {
	r := New()
	failing := newFake(t, "alpha")
	failing.startErr = errors.New("first attempt")
	if err := r.Register(failing); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := r.Start(context.Background()); err == nil {
		t.Fatal("first Start() = nil, want error")
	}

	failing.startErr = nil
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("retry Start() error = %v", err)
	}
}

// Wiring returns entries sorted by key.
func TestRegistryWiringSorted(t *testing.T) {
	r := New()
	if err := r.Register(newFake(t, "alpha")); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	for _, key := range []string{"metrics.query_url", "metrics.remote_write_url"} {
		if err := r.SetWiring("alpha", key, "http://127.0.0.1:1"); err != nil {
			t.Fatalf("SetWiring(%s) error = %v", key, err)
		}
	}

	values := r.Wiring("alpha")
	if len(values) != 2 {
		t.Fatalf("Wiring() = %+v", values)
	}
	if values[0].Key != "metrics.query_url" || values[1].Key != "metrics.remote_write_url" {
		t.Fatalf("order = %v, %v", values[0].Key, values[1].Key)
	}
}

// A subscriber that is not an EventSubscriber is skipped silently.
func TestRegistryPublishSkipsNonSubscribers(t *testing.T) {
	r := New()
	p := &noEventPlugin{fakePlugin: newFake(t, "alpha")}
	p.manifest.Capabilities.EventWebhook.Events = []string{EventAgentConnected}
	if err := r.Register(p); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Must not panic when a subscribed plugin lacks the interface.
	r.Publish(context.Background(), Event{Type: EventAgentConnected})
}

type noEventPlugin struct{ *fakePlugin }

// Events with an unsubscribed type are not delivered.
func TestRegistryPublishFiltersByType(t *testing.T) {
	r := New()
	p := newFake(t, "alpha")
	p.manifest.Capabilities.EventWebhook.Events = []string{EventAgentConnected}
	if err := r.Register(p); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	r.Publish(context.Background(), Event{Type: EventMigrationCompleted})

	if _, _, _, events := p.counts(); events != 0 {
		t.Fatalf("events = %d, want 0 for an unsubscribed type", events)
	}
}
