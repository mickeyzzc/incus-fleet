package plugin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Event kinds published by fleet to interested plugins.
const (
	EventAgentConnected      = "agent.connected"
	EventAgentDisconnected   = "agent.disconnected"
	EventAgentHeartbeat      = "agent.heartbeat"
	EventHostTaskCompleted   = "host.task.completed"
	EventMigrationCompleted  = "migration.completed"
	EventInstanceStateChange = "instance.state_changed"
)

// ErrUnknownPlugin is returned when a plugin id is not registered.
var ErrUnknownPlugin = errors.New("plugin: unknown plugin")

// Host is the subset of fleet services a plugin receives at Init time. It is
// intentionally small: plugins reach fleet through the HTTP API with a scoped
// token, not through Go interfaces.
type Host struct {
	Version string
}

// Plugin is one registered plugin. Implementations are external services, so
// Init/Start/Close only track lifecycle; they do not run plugin code.
type Plugin interface {
	Manifest() Manifest
	Init(Host) error
	Start(context.Context) error
	Close() error
}

// EventSubscriber is implemented by plugins that want fleet events. The
// registry only delivers event kinds declared in the manifest.
type EventSubscriber interface {
	HandleEvent(ctx context.Context, e Event) error
}

// Event is one fleet lifecycle event.
type Event struct {
	Type    string    `json:"type"`
	Subject string    `json:"subject,omitempty"`
	At      time.Time `json:"at"`
	Payload any       `json:"payload,omitempty"`
}

// WiringValue is an active override for one fleet behavior.
type WiringValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Registry tracks registered plugins, their lifecycle, event subscriptions, and
// approved wiring overrides.
type Registry struct {
	mu        sync.Mutex
	host      Host
	plugins   map[string]Plugin
	order     []string
	wiring    map[string]map[string]string
	started   bool
	startedOK []Plugin
}

// New builds an empty registry.
func New(opts ...Option) *Registry {
	r := &Registry{
		plugins: make(map[string]Plugin),
		wiring:  make(map[string]map[string]string),
	}
	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Option configures the registry.
type Option func(*Registry)

// WithHost sets the host information handed to plugins.
func WithHost(host Host) Option {
	return func(r *Registry) { r.host = host }
}

// Register validates and adds a plugin. Duplicate ids are rejected.
func (r *Registry) Register(p Plugin) error {
	if p == nil {
		return fmt.Errorf("%w: nil plugin", ErrInvalidManifest)
	}

	m := p.Manifest()
	if err := m.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, dup := r.plugins[m.ID]; dup {
		return fmt.Errorf("%w: duplicate plugin id %q", ErrInvalidManifest, m.ID)
	}

	r.plugins[m.ID] = p
	r.order = append(r.order, m.ID)
	sort.Strings(r.order)

	return nil
}

// Get returns a registered plugin by id.
func (r *Registry) Get(id string) (Plugin, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, ok := r.plugins[id]

	return p, ok
}

// Manifests returns every manifest sorted by id. The slice is a copy.
func (r *Registry) Manifests() []Manifest {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]Manifest, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.plugins[id].Manifest())
	}

	return out
}

// Start initializes and starts every plugin once. A failure rolls back the
// plugins that already started so a retry begins from a clean state.
func (r *Registry) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return nil
	}
	ids := append([]string(nil), r.order...)
	r.mu.Unlock()

	var started []Plugin

	for _, id := range ids {
		r.mu.Lock()
		p := r.plugins[id]
		r.mu.Unlock()

		if err := p.Init(r.host); err != nil {
			closeAll(started)

			return fmt.Errorf("plugin: init %s: %w", id, err)
		}
		if err := p.Start(ctx); err != nil {
			closeAll(started)

			return fmt.Errorf("plugin: start %s: %w", id, err)
		}

		started = append(started, p)
	}

	r.mu.Lock()
	r.started = true
	r.startedOK = started
	r.mu.Unlock()

	return nil
}

// Close stops every started plugin in reverse order and resets the registry so
// a later Start works again.
func (r *Registry) Close() error {
	r.mu.Lock()
	started := r.startedOK
	r.startedOK = nil
	r.started = false
	r.mu.Unlock()

	return closeAll(started)
}

func closeAll(plugins []Plugin) error {
	var errs []error
	for i := len(plugins) - 1; i >= 0; i-- {
		if err := plugins[i].Close(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// Publish delivers an event to every started plugin that subscribed to its
// type. Delivery is fire-and-forget: a failing subscriber never blocks or fails
// another. Events published before Start are dropped.
func (r *Registry) Publish(ctx context.Context, e Event) {
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}

	r.mu.Lock()
	if !r.started {
		r.mu.Unlock()
		return
	}

	type target struct {
		id string
		p  EventSubscriber
	}

	var targets []target
	for _, id := range r.order {
		p := r.plugins[id]
		sub, ok := p.(EventSubscriber)
		if !ok {
			continue
		}
		m := p.Manifest()
		if !m.Capabilities.EventWebhook.subscribes(e.Type) {
			continue
		}
		targets = append(targets, target{id: id, p: sub})
	}
	r.mu.Unlock()

	for _, t := range targets {
		//nolint:errcheck // delivery failures are isolated by design
		_ = t.p.HandleEvent(ctx, e)
	}
}

func (w EventWebhook) subscribes(eventType string) bool {
	for _, want := range w.Events {
		if want == eventType {
			return true
		}
	}

	return false
}

// SetWiring records an operator-approved override for a plugin. Only keys the
// manifest declared are accepted; values must be absolute http(s) URLs.
func (r *Registry) SetWiring(id, key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, ok := r.plugins[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownPlugin, id)
	}

	if !p.Manifest().Capabilities.declaresWiring(key) {
		return fmt.Errorf("%w: wiring key %q is not declared by plugin %s", ErrInvalidManifest, key, id)
	}

	normalized, err := normalizeWiringURL(value)
	if err != nil {
		return err
	}

	if r.wiring[id] == nil {
		r.wiring[id] = make(map[string]string)
	}
	r.wiring[id][key] = normalized

	return nil
}

func (c Capabilities) declaresWiring(key string) bool {
	for _, w := range c.Wiring {
		if w.Key == key {
			return true
		}
	}

	return false
}

func normalizeWiringURL(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	u, err := url.Parse(trimmed)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("%w: wiring value must be an absolute http(s) URL", ErrInvalidManifest)
	}

	return strings.TrimRight(trimmed, "/"), nil
}

// Wiring returns the active overrides for one plugin, sorted by key.
func (r *Registry) Wiring(id string) []WiringValue {
	r.mu.Lock()
	defer r.mu.Unlock()

	values := r.wiring[id]
	out := make([]WiringValue, 0, len(values))
	for key, value := range values {
		out = append(out, WiringValue{Key: key, Value: value})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })

	return out
}

// WiringValue resolves the active value for a wiring key across all plugins.
// The first plugin declaring the key wins; static config is the fallback.
func (r *Registry) WiringValue(key string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, id := range r.order {
		if value, ok := r.wiring[id][key]; ok {
			return value, true
		}
	}

	return "", false
}

// ClearWiring removes every override for a plugin, restoring static config.
func (r *Registry) ClearWiring(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.plugins[id]; !ok {
		return fmt.Errorf("%w: %s", ErrUnknownPlugin, id)
	}
	delete(r.wiring, id)

	return nil
}
