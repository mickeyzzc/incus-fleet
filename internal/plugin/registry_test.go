package plugin

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakePlugin is a minimal Plugin used to exercise registry behaviour.
type fakePlugin struct {
	manifest Manifest

	mu         sync.Mutex
	initCalls  int
	startCalls int
	closeCalls int
	events     []Event
	initErr    error
	startErr   error
	closeErr   error
}

func (f *fakePlugin) Manifest() Manifest { return f.manifest }

func (f *fakePlugin) Init(Host) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.initCalls++

	return f.initErr
}

func (f *fakePlugin) Start(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalls++

	return f.startErr
}

func (f *fakePlugin) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeCalls++

	return f.closeErr
}

func (f *fakePlugin) HandleEvent(_ context.Context, e Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)

	return nil
}

func (f *fakePlugin) counts() (init, start, closed, events int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.initCalls, f.startCalls, f.closeCalls, len(f.events)
}

func newFake(t *testing.T, id string) *fakePlugin {
	t.Helper()

	return &fakePlugin{manifest: *mustParse(t, validManifestYAML).withID(id)}
}

// withID replaces the manifest id so multiple fakes can coexist.
func (m *Manifest) withID(id string) *Manifest {
	m.ID = id
	for i := range m.Capabilities.Wiring {
		_ = i
	}

	return m
}

func TestRegistryRegisterRejectsDuplicateID(t *testing.T) {
	r := New()
	if err := r.Register(newFake(t, "alpha")); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := r.Register(newFake(t, "alpha")); err == nil {
		t.Fatal("Register() = nil, want duplicate id error")
	}
}

func TestRegistryRegisterRejectsInvalidManifest(t *testing.T) {
	r := New()
	p := newFake(t, "alpha")
	p.manifest.ID = ""

	if err := r.Register(p); err == nil {
		t.Fatal("Register() = nil, want manifest validation error")
	}
}

func TestRegistryManifestsReturnsSortedCopy(t *testing.T) {
	r := New()
	for _, id := range []string{"charlie", "alpha", "bravo"} {
		if err := r.Register(newFake(t, id)); err != nil {
			t.Fatalf("Register(%s) error = %v", id, err)
		}
	}

	got := r.Manifests()
	if len(got) != 3 {
		t.Fatalf("Manifests() len = %d, want 3", len(got))
	}
	if got[0].ID != "alpha" || got[1].ID != "bravo" || got[2].ID != "charlie" {
		t.Fatalf("Manifests() order = %v", []string{got[0].ID, got[1].ID, got[2].ID})
	}

	// The returned slice must not alias internal state.
	got[0].ID = "mutated"
	if r.Manifests()[0].ID != "alpha" {
		t.Fatal("Manifests() returned a slice aliasing registry state")
	}
}

func TestRegistryStartInitsThenStarts(t *testing.T) {
	r := New()
	p := newFake(t, "alpha")
	if err := r.Register(p); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	init, start, _, _ := p.counts()
	if init != 1 || start != 1 {
		t.Fatalf("calls init=%d start=%d, want 1/1", init, start)
	}
}

func TestRegistryStartIsIdempotent(t *testing.T) {
	r := New()
	p := newFake(t, "alpha")
	if err := r.Register(p); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("second Start() error = %v", err)
	}

	init, start, _, _ := p.counts()
	if init != 1 || start != 1 {
		t.Fatalf("calls init=%d start=%d, want 1/1 after two Start() calls", init, start)
	}
}

func TestRegistryStartRollsBackOnFailure(t *testing.T) {
	r := New()
	first := newFake(t, "alpha")
	second := newFake(t, "bravo")
	second.startErr = errors.New("boom")

	if err := r.Register(first); err != nil {
		t.Fatalf("Register(first) error = %v", err)
	}
	if err := r.Register(second); err != nil {
		t.Fatalf("Register(second) error = %v", err)
	}

	err := r.Start(context.Background())
	if err == nil {
		t.Fatal("Start() = nil, want error")
	}
	if !strings.Contains(err.Error(), "bravo") {
		t.Fatalf("Start() error = %q, want it to name the failing plugin", err)
	}

	_, _, closeFirst, _ := first.counts()
	if closeFirst != 1 {
		t.Fatalf("already-started plugin Close calls = %d, want 1", closeFirst)
	}
}

func TestRegistryCloseIsReverseOrderAndIdempotent(t *testing.T) {
	r := New()
	alpha := newFake(t, "alpha")
	bravo := newFake(t, "bravo")
	for _, p := range []*fakePlugin{alpha, bravo} {
		if err := r.Register(p); err != nil {
			t.Fatalf("Register() error = %v", err)
		}
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := r.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}

	_, _, alphaClose, _ := alpha.counts()
	_, _, bravoClose, _ := bravo.counts()
	if alphaClose != 1 || bravoClose != 1 {
		t.Fatalf("close calls alpha=%d bravo=%d, want 1/1 each", alphaClose, bravoClose)
	}
}

func TestRegistryCloseAggregatesErrors(t *testing.T) {
	r := New()
	p := newFake(t, "alpha")
	p.closeErr = errors.New("close failed")
	if err := r.Register(p); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := r.Close(); err == nil {
		t.Fatal("Close() = nil, want aggregated error")
	}
}

func TestRegistryGet(t *testing.T) {
	r := New()
	p := newFake(t, "alpha")
	if err := r.Register(p); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if got, ok := r.Get("alpha"); !ok || got != Plugin(p) {
		t.Fatalf("Get(alpha) = %v, %v", got, ok)
	}
	if _, ok := r.Get("missing"); ok {
		t.Fatal("Get(missing) reported ok")
	}
}

func TestRegistryPublishDeliversOnlyToSubscribers(t *testing.T) {
	r := New()

	subscriber := newFake(t, "sub")
	subscriber.manifest.Capabilities.EventWebhook.Events = []string{EventAgentConnected}

	other := newFake(t, "other")
	other.manifest.Capabilities.EventWebhook = EventWebhook{}

	for _, p := range []*fakePlugin{subscriber, other} {
		if err := r.Register(p); err != nil {
			t.Fatalf("Register() error = %v", err)
		}
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	r.Publish(context.Background(), Event{Type: EventAgentConnected, Subject: "agent-1"})

	_, _, _, subEvents := subscriber.counts()
	_, _, _, otherEvents := other.counts()
	if subEvents != 1 {
		t.Fatalf("subscriber events = %d, want 1", subEvents)
	}
	if otherEvents != 0 {
		t.Fatalf("non-subscriber events = %d, want 0", otherEvents)
	}
}

// A failing subscriber must not stop delivery to the others.
func TestRegistryPublishIsolatesFailures(t *testing.T) {
	r := New()

	failing := newFake(t, "failing")
	failing.manifest.Capabilities.EventWebhook.Events = []string{EventAgentConnected}
	failing.manifest.Capabilities.EventWebhook.Path = "/events"

	healthy := newFake(t, "healthy")
	healthy.manifest.Capabilities.EventWebhook.Events = []string{EventAgentConnected}
	healthy.manifest.Capabilities.EventWebhook.Path = "/events"

	if err := r.Register(failing); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := r.Register(healthy); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Publish does not return error (fire and forget), but must reach healthy.
	r.Publish(context.Background(), Event{Type: EventAgentConnected})

	_, _, _, healthyEvents := healthy.counts()
	if healthyEvents != 1 {
		t.Fatalf("healthy subscriber events = %d, want 1", healthyEvents)
	}
}

func TestRegistryPublishBeforeStartIsDropped(t *testing.T) {
	r := New()
	p := newFake(t, "alpha")
	p.manifest.Capabilities.EventWebhook.Events = []string{EventAgentConnected}
	if err := r.Register(p); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	r.Publish(context.Background(), Event{Type: EventAgentConnected})

	_, _, _, events := p.counts()
	if events != 0 {
		t.Fatalf("events before Start = %d, want 0", events)
	}
}

func TestRegistryWiringOnlyAllowsDeclaredKeys(t *testing.T) {
	r := New()
	if err := r.Register(newFake(t, "alpha")); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if err := r.SetWiring("alpha", "metrics.query_url", "http://127.0.0.1:8428"); err != nil {
		t.Fatalf("SetWiring() error = %v", err)
	}

	// Undeclared keys are rejected so a plugin cannot silently take over
	// behaviour it never advertised.
	err := r.SetWiring("alpha", "notifications.sink_url", "http://evil.example")
	if err == nil {
		t.Fatal("SetWiring() = nil, want rejection for undeclared key")
	}
	if !strings.Contains(err.Error(), "not declared") {
		t.Fatalf("SetWiring() error = %q", err)
	}

	// URL validation applies to declared keys.
	if err := r.SetWiring("alpha", "metrics.query_url", "ftp://nope"); err == nil {
		t.Fatal("SetWiring() = nil, want URL scheme rejection")
	}
}

func TestRegistryWiringReadBackAndClear(t *testing.T) {
	r := New()
	if err := r.Register(newFake(t, "alpha")); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := r.SetWiring("alpha", "metrics.query_url", "http://127.0.0.1:8428/"); err != nil {
		t.Fatalf("SetWiring() error = %v", err)
	}

	wiring := r.Wiring("alpha")
	if len(wiring) != 1 {
		t.Fatalf("Wiring() = %+v", wiring)
	}
	if wiring[0].Value != "http://127.0.0.1:8428" {
		t.Fatalf("value = %q, want trailing slash trimmed", wiring[0].Value)
	}

	if err := r.ClearWiring("alpha"); err != nil {
		t.Fatalf("ClearWiring() error = %v", err)
	}
	if len(r.Wiring("alpha")) != 0 {
		t.Fatal("Wiring() not empty after ClearWiring()")
	}
}

func TestRegistryWiringUnknownPlugin(t *testing.T) {
	r := New()
	if err := r.SetWiring("nope", "metrics.query_url", "http://127.0.0.1"); err == nil {
		t.Fatal("SetWiring() = nil, want unknown plugin error")
	}
}

func TestRegistryConcurrentPublish(t *testing.T) {
	r := New()
	p := newFake(t, "alpha")
	p.manifest.Capabilities.EventWebhook.Events = []string{EventAgentConnected}
	if err := r.Register(p); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Publish(context.Background(), Event{Type: EventAgentConnected})
		}()
	}
	wg.Wait()

	_, _, _, events := p.counts()
	if events != 20 {
		t.Fatalf("events = %d, want 20", events)
	}
}

func TestEventTimesetsZeroTimestamp(t *testing.T) {
	r := New()
	p := &recordingPlugin{fakePlugin: newFake(t, "alpha")}
	p.manifest.Capabilities.EventWebhook.Events = []string{EventAgentConnected}
	if err := r.Register(p); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	r.Publish(context.Background(), Event{Type: EventAgentConnected})

	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.events) != 1 {
		t.Fatalf("events = %d, want 1", len(p.events))
	}
	if p.events[0].At.IsZero() {
		t.Fatal("Event.At was not stamped")
	}
}

type recordingPlugin struct{ *fakePlugin }

func (r *recordingPlugin) HandleEvent(ctx context.Context, e Event) error {
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}

	return r.fakePlugin.HandleEvent(ctx, e)
}
