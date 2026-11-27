package plugin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/store"
)

// Event kinds published by fleet to interested plugins.
const (
	EventAgentConnected      = "agent.connected"
	EventAgentDisconnected   = "agent.disconnected"
	EventAgentHeartbeat      = "agent.heartbeat"
	EventHostTaskCompleted   = "host.task.completed"
	EventMigrationCompleted  = "migration.completed"
	EventInstanceStateChange = "instance.state_changed"
	// EventWiringChanged is published when an operator changes an approved
	// wiring value through the management API.
	EventWiringChanged = "plugin.wiring_changed"
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
// registry only delivers event kinds declared in the manifest, or the kinds a
// dynamically registered subscriber explicitly lists.
type EventSubscriber interface {
	HandleEvent(ctx context.Context, e Event) error
}

// EventSubscription is an optional interface for dynamic subscribers. It lets
// a webhook subscriber declare its event kinds without a static manifest entry.
type EventSubscription interface {
	EventSubscriber
	SubscribedEvents() []string
}

// WiringStore is the durable half of the wiring registry. Keeping the interface
// narrow makes the registry testable without a live database.
type WiringStore interface {
	ListActivePluginWiring(ctx context.Context) ([]*store.PluginWiring, error)
	GetPluginWiring(ctx context.Context, pluginID, key string) (*store.PluginWiring, error)
	UpsertPluginWiring(ctx context.Context, w *store.PluginWiring) error
	SetPluginWiringEnabled(ctx context.Context, pluginID, key string, enabled bool) error
	RestorePluginWiringDefaults(ctx context.Context, pluginID string) error
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
	mu           sync.Mutex
	host         Host
	plugins      map[string]Plugin
	order        []string
	wiring       map[string]map[string]string
	wiringLoaded bool
	wiringStore  WiringStore
	listeners    []func()
	subscribers  map[string]EventSubscriber
	logger       *slog.Logger
	started      bool
	startedOK    []Plugin
}

// New builds an empty registry.
func New(opts ...Option) *Registry {
	r := &Registry{
		plugins:     make(map[string]Plugin),
		wiring:      make(map[string]map[string]string),
		subscribers: make(map[string]EventSubscriber),
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

// WithWiringStore makes approved overrides durable across restarts.
func WithWiringStore(st WiringStore) Option {
	return func(r *Registry) { r.wiringStore = st }
}

// WithWiringListener registers a callback invoked after any wiring change.
// Fleet uses it to reconfigure static-config consumers without a restart.
func WithWiringListener(fn func()) Option {
	return func(r *Registry) {
		if fn != nil {
			r.listeners = append(r.listeners, fn)
		}
	}
}

// WithLogger sets the logger used for isolated event-delivery failures.
func WithLogger(logger *slog.Logger) Option {
	return func(r *Registry) { r.logger = logger }
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
	if err := r.loadWiring(ctx); err != nil {
		return err
	}

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
			return errors.Join(fmt.Errorf("plugin: init %s: %w", id, err), closeAll(started))
		}
		if err := p.Start(ctx); err != nil {
			return errors.Join(fmt.Errorf("plugin: start %s: %w", id, err), closeAll(started))
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
	for id, sub := range r.subscribers {
		if subscription, ok := sub.(EventSubscription); ok {
			if !containsEvent(subscription.SubscribedEvents(), e.Type) {
				continue
			}
		} else if p, exists := r.plugins[id]; exists && !p.Manifest().Capabilities.EventWebhook.subscribes(e.Type) {
			continue
		}
		targets = append(targets, target{id: id, p: sub})
	}
	r.mu.Unlock()

	for _, t := range targets {
		if err := t.p.HandleEvent(ctx, e); err != nil && r.logger != nil {
			r.logger.Warn("plugin event delivery failed", "plugin", t.id, "event", e.Type, "error", err)
		}
	}
}

// RegisterSubscriber attaches a dynamic event subscriber, e.g. a webhook for a
// plugin installed at runtime. It is idempotent for a given subscriber id.
func (r *Registry) RegisterSubscriber(id string, sub EventSubscriber) error {
	if strings.TrimSpace(id) == "" || sub == nil {
		return fmt.Errorf("%w: event subscriber requires id and handler", ErrInvalidManifest)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subscribers[id] = sub

	return nil
}

// UnregisterSubscriber removes a dynamic event subscriber. Uninstall calls it
// so a deleted plugin stops receiving fleet events immediately.
func (r *Registry) UnregisterSubscriber(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: event subscriber requires id", ErrInvalidManifest)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.subscribers, id)

	return nil
}

// Unregister removes a descriptor, its active overrides, and its dynamic
// subscriber. Uninstall uses it so a removed community plugin cannot be
// resurrected from persisted wiring after a restart. Built-in descriptors are
// never unregistered by fleet.
func (r *Registry) Unregister(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.plugins[id]; !ok {
		return fmt.Errorf("%w: %s", ErrUnknownPlugin, id)
	}
	delete(r.plugins, id)
	delete(r.wiring, id)
	delete(r.subscribers, id)
	for idx, existing := range r.order {
		if existing == id {
			r.order = append(r.order[:idx], r.order[idx+1:]...)
			break
		}
	}

	return nil
}

func containsEvent(events []string, want string) bool {
	for _, event := range events {
		if event == want {
			return true
		}
	}

	return false
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
// manifest declared are accepted; values must be absolute http(s) URLs. The
// default value is captured on first write so uninstall can restore it.
func (r *Registry) SetWiring(id, key, value string) error {
	r.mu.Lock()
	p, ok := r.plugins[id]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrUnknownPlugin, id)
	}
	if !p.Manifest().Capabilities.DeclaresWiring(key) {
		r.mu.Unlock()
		return fmt.Errorf("%w: wiring key %q is not declared by plugin %s", ErrInvalidManifest, key, id)
	}
	defaultValue := r.wiring[id][key]
	r.mu.Unlock()

	normalized, err := normalizeWiringURL(value)
	if err != nil {
		return err
	}

	if r.wiringStore != nil {
		if existing, err := r.wiringStore.GetPluginWiring(context.Background(), id, key); err == nil {
			defaultValue = existing.DefaultValue
		} else if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("plugin: read wiring %s/%s: %w", id, key, err)
		}
		row := &store.PluginWiring{
			PluginID: id, Key: key, Value: normalized,
			DefaultValue: defaultValue, Enabled: true, Approved: true,
		}
		if err := r.wiringStore.UpsertPluginWiring(context.Background(), row); err != nil {
			return fmt.Errorf("plugin: persist wiring %s/%s: %w", id, key, err)
		}
	}

	r.mu.Lock()
	if r.wiring[id] == nil {
		r.wiring[id] = make(map[string]string)
	}
	r.wiring[id][key] = normalized
	r.mu.Unlock()
	r.notify()

	return nil
}

func (r *Registry) notify() {
	for _, listener := range r.listeners {
		listener()
	}
}

// loadWiring recovers active overrides from the durable store. Rows belonging
// to unknown plugins are ignored so a removed plugin cannot resurrect wiring.
func (r *Registry) loadWiring(ctx context.Context) error {
	if r.wiringStore == nil {
		return nil
	}
	rows, err := r.wiringStore.ListActivePluginWiring(ctx)
	if err != nil {
		return fmt.Errorf("plugin: load wiring: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.wiringLoaded {
		return nil
	}
	for _, row := range rows {
		if _, ok := r.plugins[row.PluginID]; !ok {
			continue
		}
		if r.wiring[row.PluginID] == nil {
			r.wiring[row.PluginID] = make(map[string]string)
		}
		r.wiring[row.PluginID][row.Key] = row.Value
	}
	r.wiringLoaded = true

	return nil
}

// DeclaresWiring reports whether the manifest allows a plugin to take over a
// fleet behavior. Runtime installers call it before activating an override.
func (c Capabilities) DeclaresWiring(key string) bool {
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
	loaded := r.wiringLoaded
	r.mu.Unlock()
	if !loaded {
		// Wiring may be queried before Start (for example during construction of
		// a runtime consumer). Recover it from the store on first use.
		_ = r.loadWiring(context.Background())
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range r.order {
		if value, ok := r.wiring[id][key]; ok {
			return value, true
		}
	}

	return "", false
}

// DisableWiring deactivates one key while keeping the approval and default.
// It is used by the wiring API's "restore default" action.
func (r *Registry) DisableWiring(id, key string) error {
	r.mu.Lock()
	_, ok := r.plugins[id]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownPlugin, id)
	}
	if r.wiringStore != nil {
		if err := r.wiringStore.SetPluginWiringEnabled(context.Background(), id, key, false); err != nil {
			return fmt.Errorf("plugin: disable wiring %s/%s: %w", id, key, err)
		}
	}
	r.mu.Lock()
	delete(r.wiring[id], key)
	r.mu.Unlock()
	r.notify()

	return nil
}

// ClearWiring removes every override for a plugin, restoring static config.
func (r *Registry) ClearWiring(id string) error {
	r.mu.Lock()
	_, ok := r.plugins[id]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownPlugin, id)
	}

	if r.wiringStore != nil {
		if err := r.wiringStore.RestorePluginWiringDefaults(context.Background(), id); err != nil {
			return fmt.Errorf("plugin: restore wiring defaults for %s: %w", id, err)
		}
	}

	r.mu.Lock()
	delete(r.wiring, id)
	r.mu.Unlock()
	r.notify()

	return nil
}
