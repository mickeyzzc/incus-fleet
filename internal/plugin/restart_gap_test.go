package plugin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuiltinVMDeclaresRemoteWriteWiring(t *testing.T) {
	m, ok := BuiltinManifest("vm")
	if !ok {
		t.Fatal("missing vm manifest")
	}
	if !m.Capabilities.DeclaresWiring("metrics.remote_write_url") {
		t.Fatalf("vm wiring = %#v", m.Capabilities.Wiring)
	}
}

func TestRegistryUnregisterRemovesDescriptorAndSubscriber(t *testing.T) {
	r := New()
	if err := r.Register(Descriptor(mustManifest(t))); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterSubscriber("vm", stubSubscriber{}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetWiring("vm", "metrics.query_url", "http://192.0.2.10:8428"); err != nil {
		t.Fatal(err)
	}
	if err := r.Unregister("vm"); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Get("vm"); ok {
		t.Fatal("descriptor still registered")
	}
	if wiring := r.Wiring("vm"); len(wiring) != 0 {
		t.Fatalf("wiring = %#v", wiring)
	}
	if err := r.UnregisterSubscriber("vm"); err != nil {
		t.Fatal(err)
	}
	if err := r.Unregister("missing"); !errors.Is(err, ErrUnknownPlugin) {
		t.Fatalf("unregister missing = %v", err)
	}
}

func TestNotificationSinkUsesWiredURL(t *testing.T) {
	received := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		received <- buf[:n]
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	r := New()
	if err := r.Register(Descriptor(mustManifest(t))); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterSubscriber("sink", NewNotificationSink(func() string {
		if target, ok := r.WiringValue("notifications.sink_url"); ok {
			return target
		}
		return ""
	}, []byte("real-secret"), nil)); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	if r.wiring["vm"] == nil {
		r.wiring["vm"] = make(map[string]string)
	}
	r.wiring["vm"]["notifications.sink_url"] = server.URL
	r.mu.Unlock()

	r.Publish(context.Background(), Event{Type: EventAgentConnected, Subject: "agent-1"})
	select {
	case body := <-received:
		if len(body) == 0 {
			t.Fatal("empty event body")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sink did not receive event")
	}
}

func TestSinkOwnsWebhook(t *testing.T) {
	r := New()
	vm, _ := BuiltinManifest("vm")
	bell, _ := BuiltinManifest("bell")
	if SinkOwnsWebhook(nil, bell) || SinkOwnsWebhook(r, bell) || SinkOwnsWebhook(r, vm) {
		t.Fatal("sink must not own an unwired webhook")
	}
	if err := r.Register(Descriptor(bell)); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	if r.wiring["bell"] == nil {
		r.wiring["bell"] = make(map[string]string)
	}
	r.wiring["bell"]["notifications.sink_url"] = "http://192.0.2.10:9090/events"
	r.mu.Unlock()
	if !SinkOwnsWebhook(r, bell) {
		t.Fatal("wired sink must own its webhook")
	}
}

func TestPackageManifestLoadingAndSignatures(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "community-pkg")
	if err := os.MkdirAll(pkg, 0o700); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`schema: fleet-plugin/v1
id: community-pkg
name: Community Package
version: 1.0.0
publisher: community-example
tier: community
description: Package loader test
homepage: https://example.com/plugins/community-pkg
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
	if err := os.WriteFile(filepath.Join(pkg, "manifest.yaml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	m, loaded, err := LoadPackageManifest(dir, "community-pkg")
	if err != nil || m == nil || m.ID != "community-pkg" || len(loaded) == 0 {
		t.Fatalf("load = %#v err=%v", m, err)
	}
	if got, _, err := LoadPackageManifest(dir, "missing"); got != nil || err != nil {
		t.Fatalf("missing package = %#v err=%v", got, err)
	}
	if _, _, err := LoadPackageManifest(dir, "Bad ID"); err == nil {
		t.Fatal("expected invalid id error")
	}
	key := []byte("package-key")
	if VerifyPackageSignature(m, dir, "community-pkg", raw, key) {
		t.Fatal("unsigned package was accepted")
	}
	if err := os.WriteFile(filepath.Join(pkg, "manifest.yaml.sig"), []byte(SignPayload(key, raw)), 0o600); err != nil {
		t.Fatal(err)
	}
	if !VerifyPackageSignature(m, dir, "community-pkg", raw, key) {
		t.Fatal("signed package was rejected")
	}
	if VerifyPackageSignature(m, dir, "community-pkg", []byte("tampered"), key) {
		t.Fatal("tampered package was accepted")
	}
}

func TestNotificationSinkDeliveryErrors(t *testing.T) {
	sink := NewNotificationSink(func() string { return "" }, []byte("secret"), nil)
	if err := sink.HandleEvent(context.Background(), Event{Type: "unsubscribed"}); err != nil {
		t.Fatal(err)
	}
	if err := sink.HandleEvent(context.Background(), Event{Type: EventAgentConnected}); err != nil {
		t.Fatal(err)
	}
	if err := sink.deliver("://bad", Event{Type: EventAgentConnected}); err == nil {
		t.Fatal("expected bad URL error")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	if err := sink.deliver(server.URL, Event{Type: EventAgentConnected}); err == nil {
		t.Fatal("expected upstream error")
	}
}

type stubSubscriber struct{}

func (stubSubscriber) HandleEvent(context.Context, Event) error { return nil }

func mustManifest(t *testing.T) Manifest {
	t.Helper()
	m, ok := BuiltinManifest("vm")
	if !ok {
		t.Fatal("missing manifest")
	}
	return m
}
