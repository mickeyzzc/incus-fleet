package plugin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/store"
)

func TestWiringStorePersistsAndRestores(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, "sqlite", ":memory:")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	var notifications int32
	r := New(WithWiringStore(st), WithWiringListener(func() { atomic.AddInt32(&notifications, 1) }))
	for _, p := range BuiltinPlugins() {
		if err := r.Register(p); err != nil {
			t.Fatalf("Register(%s) error = %v", p.Manifest().ID, err)
		}
	}
	if err := r.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := r.SetWiring("vm", "metrics.query_url", "http://192.0.2.10:8428/"); err != nil {
		t.Fatalf("SetWiring() error = %v", err)
	}
	if got, ok := r.WiringValue("metrics.query_url"); !ok || got != "http://192.0.2.10:8428" {
		t.Fatalf("WiringValue() = %q %v", got, ok)
	}
	if got := atomic.LoadInt32(&notifications); got != 1 {
		t.Fatalf("listener calls = %d, want 1", got)
	}
	if err := r.SetWiring("vm", "metrics.unknown", "http://192.0.2.10"); err == nil {
		t.Fatal("expected undeclared wiring key error")
	}
	if err := r.SetWiring("vm", "metrics.query_url", "not-a-url"); err == nil {
		t.Fatal("expected invalid URL error")
	}

	// A fresh registry must recover the override from the database.
	r2 := New(WithWiringStore(st))
	for _, p := range BuiltinPlugins() {
		if err := r2.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := r2.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if got, ok := r2.WiringValue("metrics.query_url"); !ok || got != "http://192.0.2.10:8428" {
		t.Fatalf("restored WiringValue() = %q %v", got, ok)
	}
	if err := r2.SetWiring("vm", "metrics.query_url", "http://192.0.2.12:8428"); err != nil {
		t.Fatalf("update wiring error = %v", err)
	}
	rows, err := st.ListPluginWiring(ctx, "vm")
	if err != nil || len(rows) != 1 || !rows[0].Approved || !rows[0].Enabled {
		t.Fatalf("persisted rows = %#v err=%v", rows, err)
	}

	if err := r2.DisableWiring("vm", "metrics.query_url"); err != nil {
		t.Fatalf("DisableWiring() error = %v", err)
	}
	if _, ok := r2.WiringValue("metrics.query_url"); ok {
		t.Fatal("wiring still active after disable")
	}
	if err := r2.SetWiring("vm", "metrics.query_url", "http://192.0.2.10:8428"); err != nil {
		t.Fatalf("re-enable wiring error = %v", err)
	}
	if err := r2.ClearWiring("vm"); err != nil {
		t.Fatalf("ClearWiring() error = %v", err)
	}
	if _, ok := r2.WiringValue("metrics.query_url"); ok {
		t.Fatal("wiring still active after clear")
	}
	rows, err = st.ListPluginWiring(ctx, "vm")
	if err != nil || len(rows) != 1 || rows[0].Enabled || rows[0].Value != rows[0].DefaultValue {
		t.Fatalf("restored rows = %#v err=%v", rows, err)
	}
	if err := r2.ClearWiring("missing"); !errors.Is(err, ErrUnknownPlugin) {
		t.Fatalf("clear missing error = %v", err)
	}
}

func TestWiringLazyLoadAndMissingStore(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, "sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.UpsertPluginWiring(ctx, &store.PluginWiring{
		PluginID: "bell", Key: "notifications.sink_url", Value: "http://192.0.2.20",
		DefaultValue: "http://192.0.2.1", Enabled: true, Approved: true,
	}); err != nil {
		t.Fatal(err)
	}

	r := New(WithWiringStore(st))
	for _, p := range BuiltinPlugins() {
		if err := r.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	if got, ok := r.WiringValue("notifications.sink_url"); !ok || got != "http://192.0.2.20" {
		t.Fatalf("lazy wiring = %q %v", got, ok)
	}
	if got := r.Wiring("bell"); len(got) != 1 || got[0].Value != "http://192.0.2.20" {
		t.Fatalf("in-memory wiring after lazy load = %#v", got)
	}

	plain := New()
	for _, p := range BuiltinPlugins() {
		if p.Manifest().ID == "vm" {
			if err := plain.Register(p); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, ok := plain.WiringValue("metrics.query_url"); ok {
		t.Fatal("unexpected wiring from empty registry")
	}
	if err := plain.SetWiring("vm", "metrics.query_url", "http://192.0.2.10"); err != nil {
		t.Fatalf("SetWiring() error = %v", err)
	}
	if got, ok := plain.WiringValue("metrics.query_url"); !ok || got != "http://192.0.2.10" {
		t.Fatalf("memory-only wiring = %q %v", got, ok)
	}
}

func TestWebhookSubscriberDeliversSignedEvents(t *testing.T) {
	var gotAuth, gotSig, body string
	delivered := make(chan struct{}, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotSig = r.Header.Get("X-Fleet-Signature")
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		body = string(buf)
		delivered <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	sub, err := NewWebhookSubscriber(WebhookConfig{
		ID: "bell", BaseURL: srv.URL, Path: "/events",
		Events: []string{EventHostTaskCompleted}, Secret: "example-test-secret", Bearer: "example-test-bearer",
	})
	if err != nil {
		t.Fatalf("NewWebhookSubscriber() error = %v", err)
	}
	r := New()
	if err := r.Register(&webhookPlugin{manifest: *mustParse(t, validManifestYAML)}); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterSubscriber("alpha", sub); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	r.Publish(context.Background(), Event{Type: EventHostTaskCompleted, Subject: "task-1"})
	select {
	case <-delivered:
	case <-time.After(2 * time.Second):
		t.Fatal("webhook was not delivered")
	}
	if body == "" || !strings.Contains(body, EventHostTaskCompleted) {
		t.Fatalf("webhook body = %q", body)
	}
	if gotAuth != "Bearer example-test-bearer" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if !VerifySignature([]byte(body), gotSig, []byte("example-test-secret")) {
		t.Fatalf("signature %q did not verify", gotSig)
	}

	// A plugin subscribed to a different event must not receive this one.
	r.Publish(context.Background(), Event{Type: EventMigrationCompleted})
}

type webhookPlugin struct{ manifest Manifest }

func (p *webhookPlugin) Manifest() Manifest          { return p.manifest }
func (p *webhookPlugin) Init(Host) error             { return nil }
func (p *webhookPlugin) Start(context.Context) error { return nil }
func (p *webhookPlugin) Close() error                { return nil }

func TestBuiltinManifestsDeclareWiring(t *testing.T) {
	want := map[string]string{
		"vm":     "metrics.query_url",
		"vmlogs": "logs.query_url",
		"hive":   "bootstrap.answer_file_url",
		"bell":   "notifications.sink_url",
	}
	plugins := BuiltinPlugins()
	if len(plugins) != len(want) {
		t.Fatalf("builtin count = %d, want %d", len(plugins), len(want))
	}
	seen := make([]string, 0, len(want))
	for _, p := range plugins {
		m := p.Manifest()
		if err := m.Validate(); err != nil {
			t.Fatalf("%s manifest invalid: %v", m.ID, err)
		}
		seen = append(seen, m.ID)
		if m.Tier != TierOfficial || len(m.Capabilities.Wiring) == 0 {
			t.Fatalf("%s manifest = %#v", m.ID, m)
		}
		if m.Capabilities.Wiring[0].Key != want[m.ID] {
			t.Fatalf("%s wiring = %q, want %q", m.ID, m.Capabilities.Wiring[0].Key, want[m.ID])
		}
	}
	sort.Strings(seen)
	if strings.Join(seen, ",") != "bell,hive,vm,vmlogs" {
		t.Fatalf("builtin ids = %v", seen)
	}

	if m, ok := BuiltinManifest("vm"); !ok || m.ID != "vm" {
		t.Fatalf("BuiltinManifest(vm) = %#v %v", m, ok)
	}
	if _, ok := BuiltinManifest("unknown"); ok {
		t.Fatal("unexpected builtin manifest")
	}
}

func TestWebhookSubscriberValidationAndErrors(t *testing.T) {
	for _, cfg := range []WebhookConfig{
		{ID: "", BaseURL: "http://192.0.2.1", Path: "/events", Events: []string{"agent.connected"}},
		{ID: "x", BaseURL: "http://192.0.2.1", Path: "events", Events: []string{"agent.connected"}},
		{ID: "x", BaseURL: "not-a-url", Path: "/events", Events: []string{"agent.connected"}},
		{ID: "x", BaseURL: "http://192.0.2.1", Path: "/events"},
	} {
		if _, err := NewWebhookSubscriber(cfg); err == nil {
			t.Fatalf("expected config error for %#v", cfg)
		}
	}

	errorsCh := make(chan error, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	sub, err := NewWebhookSubscriber(WebhookConfig{
		ID: "bell", BaseURL: srv.URL, Path: "/events",
		Events:  []string{EventAgentConnected},
		OnError: func(err error) { errorsCh <- err },
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub.ID() != "bell" || len(sub.SubscribedEvents()) != 1 {
		t.Fatalf("subscriber = %#v", sub)
	}
	if err := sub.HandleEvent(context.Background(), Event{Type: EventMigrationCompleted}); err != nil {
		t.Fatalf("unsubscribed event error = %v", err)
	}
	if err := sub.HandleEvent(context.Background(), Event{Type: EventAgentConnected}); err != nil {
		t.Fatalf("HandleEvent() error = %v", err)
	}
	select {
	case err := <-errorsCh:
		if err == nil {
			t.Fatal("delivery error was nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("delivery failure was not reported")
	}

	bad, err := NewWebhookSubscriber(WebhookConfig{
		ID: "bad", BaseURL: srv.URL, Path: "/events", Events: []string{EventAgentConnected},
	})
	if err != nil {
		t.Fatal(err)
	}
	// A canceled context must not make delivery block the producer.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := bad.HandleEvent(ctx, Event{Type: EventAgentConnected}); err != nil {
		t.Fatalf("canceled HandleEvent() error = %v", err)
	}
}

func TestDescriptorLifecycle(t *testing.T) {
	m, ok := BuiltinManifest("hive")
	if !ok {
		t.Fatal("missing hive manifest")
	}
	p := Descriptor(m)
	if p.Manifest().ID != "hive" {
		t.Fatalf("descriptor manifest = %#v", p.Manifest())
	}
	if err := p.Init(Host{}); err != nil {
		t.Fatal(err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSignatureVerification(t *testing.T) {
	key := []byte("example-signing-key")
	payload := []byte("manifest: example\n")
	sig := SignPayload(key, payload)
	if !VerifySignature(payload, sig, key) {
		t.Fatal("valid signature rejected")
	}
	if !VerifySignature(payload, "sha256:"+sig, key) {
		t.Fatal("prefixed signature rejected")
	}
	for _, bad := range []struct {
		sig string
		key []byte
	}{
		{sig: sig, key: []byte("other-key")},
		{sig: "not-base64!!", key: key},
		{sig: SignPayload(key, []byte("other payload")), key: key},
		{sig: "", key: key},
	} {
		if VerifySignature(payload, bad.sig, bad.key) {
			t.Fatalf("invalid signature accepted: %q", bad.sig)
		}
	}
}

func TestRenderTemplatesAndConfigInjection(t *testing.T) {
	m := Manifest{Install: Install{Templates: []Template{
		{Name: "retention", Type: TemplateChoice, Options: []string{"7d", "30d"}, Default: "30d"},
		{Name: "port", Type: TemplatePort, Default: "8428"},
		{Name: "debug", Type: TemplateBool, Default: "false"},
		{Name: "bind", Type: TemplateText, Default: "0.0.0.0"},
	}}}

	values, err := RenderTemplates(m, map[string]string{"port": "9000", "debug": "true"})
	if err != nil {
		t.Fatalf("RenderTemplates() error = %v", err)
	}
	if values["retention"] != "30d" || values["port"] != "9000" || values["debug"] != "true" || values["bind"] != "0.0.0.0" {
		t.Fatalf("rendered values = %#v", values)
	}
	script := ConfigScript(values)
	if !strings.Contains(script, "'port=9000'") || !strings.Contains(script, "'retention=30d'") ||
		strings.Index(script, "bind=") > strings.Index(script, "debug=") {
		t.Fatalf("config script = %q", script)
	}

	for _, tc := range []struct{ key, value string }{
		{"retention", "1y"}, {"port", "70000"}, {"port", "abc"}, {"debug", "maybe"}, {"unknown", "x"},
	} {
		if _, err := RenderTemplates(m, map[string]string{tc.key: tc.value}); err == nil {
			t.Fatalf("expected error for %s=%s", tc.key, tc.value)
		}
	}
}

func TestDataDiskScriptIsSanitized(t *testing.T) {
	m := Manifest{Install: Install{DataDisk: DataDisk{Filesystem: FSExt4, MountPoint: "/data"}}}
	script := DataDiskScript(m, 20)
	for _, want := range []string{"mkfs.ext4", "/data", "/etc/fstab"} {
		if !strings.Contains(script, want) {
			t.Fatalf("script %q missing %q", script, want)
		}
	}
	if strings.Contains(script, "rm -rf") {
		t.Fatalf("unsafe script: %q", script)
	}
}
