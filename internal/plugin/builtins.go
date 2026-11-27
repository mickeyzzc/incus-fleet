package plugin

import (
	"context"
	"fmt"
	"sort"
)

// builtinManifestYAML is the shared skeleton for the first-party catalog. All
// references use RFC 5737/example.com placeholders: the published repository
// must never contain deployment-specific addresses or hostnames.
const builtinManifestYAML = `
schema: fleet-plugin/v1
id: %s
name: %s
version: 1.0.0
publisher: fleet-official
tier: official
description: %s
homepage: https://example.com/plugins/%s

artifact:
  image:
    source: registry
    reference: example.com/plugins/%s:1.0.0
    architectures: [amd64, arm64]

install:
  defaults:
    cpu: 2
    memory_mb: 4096
    root_disk_gb: 20
    network: managed
  data_disk:
    required: false
    default_gb: 20
    filesystem: ext4
    mount_point: /data
    storage_pool_selector: true
  templates:
    - name: listen_port
      label: Listen port
      type: port
      default: %d

capabilities:
  deploy_instance: true
  proxy_ui:
    path_prefix: /plugin-ui/%s/
    upstream_port: %d
  event_webhook:
    path: /events
    events: [agent.connected, agent.disconnected, agent.heartbeat, host.task.completed, migration.completed, instance.state_changed]
  wiring:
    - key: %s
      effect: %s

health:
  endpoint: /health
  initial_delay_seconds: 10
  interval_seconds: 15
  failure_threshold: 5

permissions:
  host: [api_proxy, event_delivery]
  fleet: [read:clusters, read:instances, read:agents]
`

type builtinSpec struct {
	id, name, description, key, effect string
	port                               int
	// extraWiring declares additional fleet behaviors this plugin may take
	// over. The shared YAML skeleton carries the primary key; these are added
	// after parsing so the catalog table stays a single source of truth.
	extraWiring []Wiring
}

var builtinSpecs = []builtinSpec{
	{
		id: "vm", name: "VictoriaMetrics", description: "Time-series storage for cluster and agent telemetry",
		key: "metrics.query_url", effect: "fleet PromQL proxy queries this plugin", port: 8428,
		extraWiring: []Wiring{{
			Key: "metrics.remote_write_url", Effect: "fleet telemetry remote-write destination",
		}},
	},
	{
		id: "vmlogs", name: "VictoriaLogs", description: "Log storage and search for fleet and agents",
		key: "logs.query_url", effect: "fleet log search proxy queries this plugin", port: 9428,
	},
	{
		id: "hive", name: "MiBeeHive", description: "Network boot, DNS, and answer-file distribution",
		key: "bootstrap.answer_file_url", effect: "fleet unattended installs fetch answer files from this plugin", port: 8080,
	},
	{
		id: "bell", name: "MiBeeBell", description: "Notification center for fleet, hive, and VM events",
		key: "notifications.sink_url", effect: "fleet event webhooks are delivered to this plugin", port: 9090,
	},
}

// builtinPlugin is a descriptor-only plugin. Fleet never executes plugin code;
// lifecycle methods exist so the registry can treat built-ins like any other
// registered plugin.
type builtinPlugin struct{ manifest Manifest }

func (p *builtinPlugin) Manifest() Manifest          { return p.manifest }
func (p *builtinPlugin) Init(Host) error             { return nil }
func (p *builtinPlugin) Start(context.Context) error { return nil }
func (p *builtinPlugin) Close() error                { return nil }

// Descriptor wraps a manifest as a lifecycle-only plugin. Runtime installs use
// it to register a plugin with the registry before activating its wiring, so
// the wiring keys can be validated against the manifest.
func Descriptor(m Manifest) Plugin {
	return &builtinPlugin{manifest: m}
}

// BuiltinPlugins returns the descriptor-only first-party plugins, sorted by id.
func BuiltinPlugins() []Plugin {
	specs := append([]builtinSpec(nil), builtinSpecs...)
	sort.Slice(specs, func(i, j int) bool { return specs[i].id < specs[j].id })

	out := make([]Plugin, 0, len(specs))
	for _, spec := range specs {
		m, err := buildBuiltinManifest(spec)
		if err != nil {
			// The specs are compile-time constants; a failure is a programmer
			// error and must surface during development, not at runtime.
			panic(err)
		}
		out = append(out, &builtinPlugin{manifest: *m})
	}

	return out
}

// BuiltinManifest returns one first-party manifest by catalog id.
func BuiltinManifest(id string) (Manifest, bool) {
	for _, p := range BuiltinPlugins() {
		m := p.Manifest()
		if m.ID == id {
			return m, true
		}
	}

	return Manifest{}, false
}

func buildBuiltinManifest(spec builtinSpec) (*Manifest, error) {
	raw := fmt.Sprintf(builtinManifestYAML, spec.id, spec.name, spec.description, spec.id,
		spec.id, spec.port, spec.id, spec.port, spec.key, spec.effect)

	m, err := ParseManifest([]byte(raw))
	if err != nil {
		return nil, err
	}
	if len(spec.extraWiring) > 0 {
		m.Capabilities.Wiring = append(m.Capabilities.Wiring, spec.extraWiring...)
		if err := m.Validate(); err != nil {
			return nil, err
		}
	}

	return m, nil
}
