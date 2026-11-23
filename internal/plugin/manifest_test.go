package plugin

import (
	"strings"
	"testing"
)

// validManifestYAML is the canonical manifest used by tests. It mirrors the
// documented shape in docs/plugins.md and must stay parseable.
const validManifestYAML = `
schema: fleet-plugin/v1
id: victoria-metrics
name: VictoriaMetrics
version: 1.0.0
publisher: fleet-official
tier: official
description: Time-series storage for cluster and agent telemetry
homepage: https://example.com/plugins/victoria-metrics

artifact:
  image:
    source: registry
    reference: example.com/plugins/victoria-metrics:1.0.0
    architectures: [amd64, arm64]
  size_hints:
    min_memory_mb: 2048
    min_disk_gb: 20

install:
  defaults:
    cpu: 2
    memory_mb: 4096
    root_disk_gb: 20
    network: managed
  data_disk:
    required: true
    default_gb: 200
    filesystem: ext4
    mount_point: /data
    storage_pool_selector: true
  templates:
    - name: retention
      label: Data retention
      type: choice
      options: [7d, 30d, 90d]
      default: 30d
    - name: listen_port
      type: port
      default: 8428

capabilities:
  deploy_instance: true
  proxy_ui:
    path_prefix: /plugins/victoria-metrics/
    upstream_port: 8428
  proxy_api:
    path_prefix: /api/v1/plugins/victoria-metrics/
    upstream_port: 8428
    paths: [/api/v1/query, /api/v1/query_range]
  event_webhook:
    path: /events
    events: [agent.connected, agent.disconnected]
  wiring:
    - key: metrics.remote_write_url
      effect: fleet telemetry remote_write targets this plugin
    - key: metrics.query_url
      effect: fleet PromQL proxy queries this plugin

health:
  endpoint: /health
  initial_delay_seconds: 10
  interval_seconds: 15
  failure_threshold: 5

permissions:
  host: [api_proxy, event_delivery]
  fleet: [read:clusters, read:instances, read:agents]
`

func TestParseManifestValid(t *testing.T) {
	m, err := ParseManifest([]byte(validManifestYAML))
	if err != nil {
		t.Fatalf("ParseManifest() error = %v", err)
	}

	if m.ID != "victoria-metrics" {
		t.Errorf("ID = %q, want victoria-metrics", m.ID)
	}
	if m.Schema != SchemaV1 {
		t.Errorf("Schema = %q, want %q", m.Schema, SchemaV1)
	}
	if m.Tier != TierOfficial {
		t.Errorf("Tier = %q, want official", m.Tier)
	}
	if len(m.Capabilities.Wiring) != 2 {
		t.Errorf("wiring entries = %d, want 2", len(m.Capabilities.Wiring))
	}
	if got := m.Capabilities.ProxyUI.PathPrefix; got != "/plugins/victoria-metrics/" {
		t.Errorf("proxy_ui.path_prefix = %q", got)
	}
	if len(m.Capabilities.ProxyAPI.Paths) != 2 {
		t.Errorf("proxy_api.paths = %d, want 2", len(m.Capabilities.ProxyAPI.Paths))
	}
	if !m.Install.DataDisk.Required || m.Install.DataDisk.Filesystem != "ext4" {
		t.Errorf("data disk = %+v", m.Install.DataDisk)
	}
	if len(m.Install.Templates) != 2 {
		t.Errorf("templates = %d, want 2", len(m.Install.Templates))
	}
	if m.Health.Endpoint != "/health" || m.Health.FailureThreshold != 5 {
		t.Errorf("health = %+v", m.Health)
	}
}

func TestParseManifestInvalidYAML(t *testing.T) {
	if _, err := ParseManifest([]byte("schema: [unclosed")); err == nil {
		t.Fatal("ParseManifest() expected error for malformed YAML")
	}
}

func TestValidateRejectsMissingRequiredFields(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Manifest)
		want string
	}{
		{
			name: "missing id",
			mut:  func(m *Manifest) { m.ID = "" },
			want: "id",
		},
		{
			name: "invalid id characters",
			mut:  func(m *Manifest) { m.ID = "Victoria Metrics" },
			want: "id",
		},
		{
			name: "missing name",
			mut:  func(m *Manifest) { m.Name = "" },
			want: "name",
		},
		{
			name: "missing version",
			mut:  func(m *Manifest) { m.Version = "" },
			want: "version",
		},
		{
			name: "unsupported schema",
			mut:  func(m *Manifest) { m.Schema = "fleet-plugin/v99" },
			want: "schema",
		},
		{
			name: "invalid tier",
			mut:  func(m *Manifest) { m.Tier = "gold" },
			want: "tier",
		},
		{
			name: "missing image reference",
			mut:  func(m *Manifest) { m.Artifact.Image.Reference = "" },
			want: "reference",
		},
		{
			name: "unsupported image source",
			mut:  func(m *Manifest) { m.Artifact.Image.Source = "ftp" },
			want: "source",
		},
		{
			name: "no architectures",
			mut:  func(m *Manifest) { m.Artifact.Image.Architectures = nil },
			want: "architectures",
		},
		{
			name: "unsupported filesystem",
			mut:  func(m *Manifest) { m.Install.DataDisk.Filesystem = "ntfs" },
			want: "filesystem",
		},
		{
			name: "invalid mount point",
			mut:  func(m *Manifest) { m.Install.DataDisk.MountPoint = "data" },
			want: "mount_point",
		},
		{
			name: "unsupported network",
			mut:  func(m *Manifest) { m.Install.Defaults.Network = "host" },
			want: "network",
		},
		{
			name: "proxy path without leading slash",
			mut:  func(m *Manifest) { m.Capabilities.ProxyUI.PathPrefix = "plugins/x/" },
			want: "path_prefix",
		},
		{
			name: "proxy path without trailing slash",
			mut:  func(m *Manifest) { m.Capabilities.ProxyUI.PathPrefix = "/plugins/x" },
			want: "path_prefix",
		},
		{
			name: "wiring key without effect",
			mut: func(m *Manifest) {
				m.Capabilities.Wiring = []Wiring{{Key: "metrics.query_url"}}
			},
			want: "effect",
		},
		{
			name: "duplicate wiring key",
			mut: func(m *Manifest) {
				m.Capabilities.Wiring = []Wiring{
					{Key: "metrics.query_url", Effect: "a"},
					{Key: "metrics.query_url", Effect: "b"},
				}
			},
			want: "duplicate",
		},
		{
			name: "health endpoint without leading slash",
			mut:  func(m *Manifest) { m.Health.Endpoint = "health" },
			want: "health",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := mustParse(t, validManifestYAML)
			tc.mut(m)
			err := m.Validate()
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// Community plugins must not be able to change fleet core behaviour.
func TestValidateCommunityRejectsWiring(t *testing.T) {
	m := mustParse(t, validManifestYAML)
	m.Tier = TierCommunity

	err := m.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error for community wiring")
	}
	if !strings.Contains(err.Error(), "wiring") {
		t.Fatalf("Validate() error = %q, want it to mention wiring", err)
	}
}

func TestValidateCommunityAllowsGeneralCapabilities(t *testing.T) {
	const communityYAML = `
schema: fleet-plugin/v1
id: mibee-nvr
name: MiBeeNVR
version: 1.0.0
publisher: mibee-studio
tier: community
description: NVR management UI
artifact:
  image:
    source: registry
    reference: example.com/plugins/mibee-nvr:1.0.0
    architectures: [amd64]
install:
  defaults:
    cpu: 2
    memory_mb: 2048
    root_disk_gb: 20
    network: managed
  data_disk:
    required: false
    default_gb: 0
    filesystem: ext4
    mount_point: /data
  templates: []
capabilities:
  deploy_instance: true
  proxy_ui:
    path_prefix: /plugins/mibee-nvr/
    upstream_port: 8080
health:
  endpoint: /healthz
  initial_delay_seconds: 5
  interval_seconds: 15
  failure_threshold: 5
permissions:
  host: [api_proxy]
  fleet: [read:clusters]
`
	m, err := ParseManifest([]byte(communityYAML))
	if err != nil {
		t.Fatalf("ParseManifest() error = %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateAppliesDefaults(t *testing.T) {
	m := mustParse(t, validManifestYAML)
	m.Health.IntervalSeconds = 0
	m.Health.InitialDelaySeconds = 0
	m.Health.FailureThreshold = 0
	m.Install.DataDisk.MountPoint = ""

	if err := m.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if m.Health.IntervalSeconds != defaultHealthInterval {
		t.Errorf("interval = %d, want %d", m.Health.IntervalSeconds, defaultHealthInterval)
	}
	if m.Health.InitialDelaySeconds != defaultInitialDelay {
		t.Errorf("initial delay = %d, want %d", m.Health.InitialDelaySeconds, defaultInitialDelay)
	}
	if m.Health.FailureThreshold != defaultFailureThreshold {
		t.Errorf("failure threshold = %d, want %d", m.Health.FailureThreshold, defaultFailureThreshold)
	}
	if m.Install.DataDisk.MountPoint != defaultMountPoint {
		t.Errorf("mount point = %q, want %q", m.Install.DataDisk.MountPoint, defaultMountPoint)
	}
}

// Templates must carry the values the install form needs to render them.
func TestValidateTemplateFields(t *testing.T) {
	tests := []struct {
		name string
		tpl  Template
		want string
	}{
		{name: "missing name", tpl: Template{Type: TemplateText}, want: "name"},
		{name: "missing type", tpl: Template{Name: "x"}, want: "type"},
		{name: "unsupported type", tpl: Template{Name: "x", Type: "color"}, want: "type"},
		{name: "choice without options", tpl: Template{Name: "x", Type: TemplateChoice}, want: "options"},
		{name: "port default out of range", tpl: Template{Name: "x", Type: TemplatePort, Default: "70000"}, want: "default"},
		{name: "default not in options", tpl: Template{Name: "x", Type: TemplateChoice, Options: []string{"a"}, Default: "b"}, want: "default"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := mustParse(t, validManifestYAML)
			m.Install.Templates = []Template{tc.tpl}
			err := m.Validate()
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestValidateProxyPathConflicts(t *testing.T) {
	m := mustParse(t, validManifestYAML)
	m.Capabilities.ProxyAPI.PathPrefix = m.Capabilities.ProxyUI.PathPrefix

	err := m.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error for overlapping proxy prefixes")
	}
	if !strings.Contains(err.Error(), "path_prefix") {
		t.Fatalf("Validate() error = %q, want it to mention path_prefix", err)
	}
}

func mustParse(t *testing.T, raw string) *Manifest {
	t.Helper()
	m, err := ParseManifest([]byte(raw))
	if err != nil {
		t.Fatalf("ParseManifest() error = %v", err)
	}

	return m
}
