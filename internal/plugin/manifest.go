// Package plugin implements the incus-fleet plugin specification: manifest
// parsing and validation, the plugin registry and lifecycle, event fan-out, and
// the wiring registry that lets an approved plugin take over a documented fleet
// behavior.
//
// Plugins are external services. Nothing in this package executes plugin code;
// the registry only tracks manifests and forwards events.
package plugin

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// SchemaV1 is the only supported manifest schema version.
const SchemaV1 = "fleet-plugin/v1"

// Plugin tiers. Official plugins may declare wiring; community plugins may not.
const (
	TierOfficial  = "official"
	TierCommunity = "community"
)

// Image sources.
const (
	ImageSourceRegistry = "registry"
	ImageSourceTarball  = "local-tarball"
)

// Network modes for a plugin instance.
const (
	NetworkManaged = "managed"
	NetworkOverlay = "overlay"
)

// Supported data-disk filesystems.
const (
	FSExt4 = "ext4"
	FSXFS  = "xfs"
)

// Template input types.
const (
	TemplateText   = "text"
	TemplateChoice = "choice"
	TemplatePort   = "port"
	TemplateBool   = "bool"
)

// Defaults applied during Validate when the manifest omits a value.
const (
	defaultHealthInterval   = 15
	defaultInitialDelay     = 10
	defaultFailureThreshold = 5
	defaultMountPoint       = "/data"
)

// pluginIDRE matches a lowercase DNS label.
var pluginIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ErrInvalidManifest marks manifest problems so the HTTP layer can answer 400.
var ErrInvalidManifest = errors.New("plugin: invalid manifest")

// Manifest is the parsed plugin description. It is the contract between a
// plugin package and fleet.
type Manifest struct {
	Schema       string       `yaml:"schema" json:"schema"`
	ID           string       `yaml:"id" json:"id"`
	Name         string       `yaml:"name" json:"name"`
	Version      string       `yaml:"version" json:"version"`
	Publisher    string       `yaml:"publisher" json:"publisher"`
	Tier         string       `yaml:"tier" json:"tier"`
	Description  string       `yaml:"description" json:"description"`
	Homepage     string       `yaml:"homepage" json:"homepage"`
	Artifact     Artifact     `yaml:"artifact" json:"artifact"`
	Install      Install      `yaml:"install" json:"install"`
	Capabilities Capabilities `yaml:"capabilities" json:"capabilities"`
	Health       Health       `yaml:"health" json:"health"`
	Permissions  Permissions  `yaml:"permissions" json:"permissions"`
}

// Artifact describes how to obtain the plugin image.
type Artifact struct {
	Image ImageArtifact `yaml:"image" json:"image"`
}

// ImageArtifact points at the container image to deploy.
type ImageArtifact struct {
	Source        string   `yaml:"source" json:"source"`
	Reference     string   `yaml:"reference" json:"reference"`
	Architectures []string `yaml:"architectures" json:"architectures"`
}

// Install carries the defaults and templates that render the install form.
type Install struct {
	Defaults  InstallDefaults `yaml:"defaults" json:"defaults"`
	DataDisk  DataDisk        `yaml:"data_disk" json:"data_disk"`
	Templates []Template      `yaml:"templates" json:"templates"`
}

// InstallDefaults are the resource defaults offered by the install form.
type InstallDefaults struct {
	CPU        int    `yaml:"cpu" json:"cpu"`
	MemoryMB   int    `yaml:"memory_mb" json:"memory_mb"`
	RootDiskGB int    `yaml:"root_disk_gb" json:"root_disk_gb"`
	Network    string `yaml:"network" json:"network"`
}

// DataDisk describes the optional data volume.
type DataDisk struct {
	Required            bool   `yaml:"required" json:"required"`
	DefaultGB           int    `yaml:"default_gb" json:"default_gb"`
	Filesystem          string `yaml:"filesystem" json:"filesystem"`
	MountPoint          string `yaml:"mount_point" json:"mount_point"`
	StoragePoolSelector bool   `yaml:"storage_pool_selector" json:"storage_pool_selector"`
}

// Template is one operator-editable install setting. Fleets field names are
// deliberately generic so the UI can render any plugin's form.
type Template struct {
	Name    string   `yaml:"name" json:"name"`
	Label   string   `yaml:"label" json:"label"`
	Type    string   `yaml:"type" json:"type"`
	Options []string `yaml:"options" json:"options"`
	Default string   `yaml:"default" json:"default"`
}

// Capabilities declares what the plugin asks fleet to do for it.
type Capabilities struct {
	DeployInstance bool         `yaml:"deploy_instance" json:"deploy_instance"`
	ProxyUI        ProxyTarget  `yaml:"proxy_ui" json:"proxy_ui"`
	ProxyAPI       ProxyAPI     `yaml:"proxy_api" json:"proxy_api"`
	EventWebhook   EventWebhook `yaml:"event_webhook" json:"event_webhook"`
	Wiring         []Wiring     `yaml:"wiring" json:"wiring"`
}

// ProxyTarget names a same-origin UI mount point.
type ProxyTarget struct {
	PathPrefix   string `yaml:"path_prefix" json:"path_prefix"`
	UpstreamPort int    `yaml:"upstream_port" json:"upstream_port"`
}

// ProxyAPI names a same-origin API mount point and the paths forwarded.
type ProxyAPI struct {
	PathPrefix   string   `yaml:"path_prefix" json:"path_prefix"`
	UpstreamPort int      `yaml:"upstream_port" json:"upstream_port"`
	Paths        []string `yaml:"paths" json:"paths"`
}

// EventWebhook asks for fleet events to be delivered to the plugin.
type EventWebhook struct {
	Path   string   `yaml:"path" json:"path"`
	Events []string `yaml:"events" json:"events"`
}

// Wiring is one fleet behavior a plugin may take over. Only official plugins
// may declare these.
type Wiring struct {
	Key    string `yaml:"key" json:"key"`
	Effect string `yaml:"effect" json:"effect"`
}

// Health describes how fleet probes the plugin.
type Health struct {
	Endpoint            string `yaml:"endpoint" json:"endpoint"`
	InitialDelaySeconds int    `yaml:"initial_delay_seconds" json:"initial_delay_seconds"`
	IntervalSeconds     int    `yaml:"interval_seconds" json:"interval_seconds"`
	FailureThreshold    int    `yaml:"failure_threshold" json:"failure_threshold"`
}

// Permissions are shown verbatim on the install screen.
type Permissions struct {
	Host  []string `yaml:"host" json:"host"`
	Fleet []string `yaml:"fleet" json:"fleet"`
}

// ParseManifest decodes a manifest document and validates it.
func ParseManifest(raw []byte) (*Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%w: parse: %w", ErrInvalidManifest, err)
	}

	if err := m.Validate(); err != nil {
		return nil, err
	}

	return &m, nil
}

// Validate checks the manifest and fills documented defaults in place.
func (m *Manifest) Validate() error {
	if m.Schema != SchemaV1 {
		return fmt.Errorf("%w: schema must be %q, got %q", ErrInvalidManifest, SchemaV1, m.Schema)
	}
	if !pluginIDRE.MatchString(m.ID) {
		return fmt.Errorf("%w: id %q must be a lowercase DNS label", ErrInvalidManifest, m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidManifest)
	}
	if strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("%w: version is required", ErrInvalidManifest)
	}

	switch m.Tier {
	case TierOfficial, TierCommunity:
	default:
		return fmt.Errorf("%w: tier must be %q or %q, got %q", ErrInvalidManifest, TierOfficial, TierCommunity, m.Tier)
	}

	if err := m.Artifact.validate(); err != nil {
		return err
	}
	if err := m.Install.validate(); err != nil {
		return err
	}
	if err := m.Capabilities.validate(m.Tier); err != nil {
		return err
	}
	if err := m.Health.validate(); err != nil {
		return err
	}

	return nil
}

func (a *Artifact) validate() error {
	switch a.Image.Source {
	case ImageSourceRegistry, ImageSourceTarball:
	default:
		return fmt.Errorf("%w: artifact.image.source must be %q or %q, got %q",
			ErrInvalidManifest, ImageSourceRegistry, ImageSourceTarball, a.Image.Source)
	}
	if strings.TrimSpace(a.Image.Reference) == "" {
		return fmt.Errorf("%w: artifact.image.reference is required", ErrInvalidManifest)
	}
	if len(a.Image.Architectures) == 0 {
		return fmt.Errorf("%w: artifact.image.architectures must list at least one architecture", ErrInvalidManifest)
	}

	return nil
}

func (i *Install) validate() error {
	switch i.Defaults.Network {
	case NetworkManaged, NetworkOverlay:
	default:
		return fmt.Errorf("%w: install.defaults.network must be %q or %q, got %q",
			ErrInvalidManifest, NetworkManaged, NetworkOverlay, i.Defaults.Network)
	}
	if i.Defaults.CPU < 0 || i.Defaults.MemoryMB < 0 || i.Defaults.RootDiskGB < 0 {
		return fmt.Errorf("%w: install.defaults resources must not be negative", ErrInvalidManifest)
	}

	switch i.DataDisk.Filesystem {
	case FSExt4, FSXFS:
	default:
		return fmt.Errorf("%w: install.data_disk.filesystem must be %q or %q, got %q",
			ErrInvalidManifest, FSExt4, FSXFS, i.DataDisk.Filesystem)
	}
	if i.DataDisk.DefaultGB < 0 {
		return fmt.Errorf("%w: install.data_disk.default_gb must not be negative", ErrInvalidManifest)
	}
	if strings.TrimSpace(i.DataDisk.MountPoint) == "" {
		i.DataDisk.MountPoint = defaultMountPoint
	}
	if !strings.HasPrefix(i.DataDisk.MountPoint, "/") {
		return fmt.Errorf("%w: install.data_disk.mount_point must be absolute, got %q",
			ErrInvalidManifest, i.DataDisk.MountPoint)
	}
	if i.DataDisk.Required && i.DataDisk.DefaultGB <= 0 {
		return fmt.Errorf("%w: install.data_disk.default_gb must be positive when required", ErrInvalidManifest)
	}

	for idx := range i.Templates {
		if err := i.Templates[idx].validate(); err != nil {
			return err
		}
	}

	return nil
}

func (t *Template) validate() error {
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("%w: install.templates entry is missing name", ErrInvalidManifest)
	}
	switch t.Type {
	case TemplateText, TemplateBool:
	case TemplateChoice:
		if len(t.Options) == 0 {
			return fmt.Errorf("%w: template %q of type choice requires options", ErrInvalidManifest, t.Name)
		}
	case TemplatePort:
	default:
		return fmt.Errorf("%w: template %q has unsupported type %q", ErrInvalidManifest, t.Name, t.Type)
	}
	if strings.TrimSpace(t.Default) == "" {
		return nil
	}

	switch t.Type {
	case TemplateChoice:
		found := false
		for _, opt := range t.Options {
			if opt == t.Default {
				found = true

				break
			}
		}
		if !found {
			return fmt.Errorf("%w: template %q default %q is not one of its options", ErrInvalidManifest, t.Name, t.Default)
		}
	case TemplatePort:
		port, err := strconv.Atoi(t.Default)
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("%w: template %q default %q is not a valid port", ErrInvalidManifest, t.Name, t.Default)
		}
	case TemplateBool:
		if t.Default != "true" && t.Default != "false" {
			return fmt.Errorf("%w: template %q default %q is not a boolean", ErrInvalidManifest, t.Name, t.Default)
		}
	}

	return nil
}

func (c *Capabilities) validate(tier string) error {
	if c.ProxyUI.PathPrefix != "" {
		if err := validatePrefix("proxy_ui.path_prefix", c.ProxyUI.PathPrefix); err != nil {
			return err
		}
		if c.ProxyUI.UpstreamPort <= 0 || c.ProxyUI.UpstreamPort > 65535 {
			return fmt.Errorf("%w: proxy_ui.upstream_port must be 1-65535", ErrInvalidManifest)
		}
	}

	if c.ProxyAPI.PathPrefix != "" {
		if err := validatePrefix("proxy_api.path_prefix", c.ProxyAPI.PathPrefix); err != nil {
			return err
		}
		if c.ProxyAPI.UpstreamPort <= 0 || c.ProxyAPI.UpstreamPort > 65535 {
			return fmt.Errorf("%w: proxy_api.upstream_port must be 1-65535", ErrInvalidManifest)
		}
		for _, p := range c.ProxyAPI.Paths {
			if !strings.HasPrefix(p, "/") {
				return fmt.Errorf("%w: proxy_api.paths entry %q must be absolute", ErrInvalidManifest, p)
			}
		}
	}

	if c.ProxyUI.PathPrefix != "" && c.ProxyUI.PathPrefix == c.ProxyAPI.PathPrefix {
		return fmt.Errorf("%w: proxy_ui.path_prefix and proxy_api.path_prefix must differ", ErrInvalidManifest)
	}

	if c.EventWebhook.Path != "" {
		if !strings.HasPrefix(c.EventWebhook.Path, "/") {
			return fmt.Errorf("%w: event_webhook.path must be absolute, got %q", ErrInvalidManifest, c.EventWebhook.Path)
		}
		if len(c.EventWebhook.Events) == 0 {
			return fmt.Errorf("%w: event_webhook.events must list at least one event", ErrInvalidManifest)
		}
	}

	// A community plugin must never change fleet core behaviour.
	if len(c.Wiring) > 0 && tier != TierOfficial {
		return fmt.Errorf("%w: wiring is only allowed for %s plugins", ErrInvalidManifest, TierOfficial)
	}

	seen := make(map[string]struct{}, len(c.Wiring))
	for _, w := range c.Wiring {
		if strings.TrimSpace(w.Key) == "" {
			return fmt.Errorf("%w: wiring entry is missing key", ErrInvalidManifest)
		}
		if strings.TrimSpace(w.Effect) == "" {
			return fmt.Errorf("%w: wiring key %q must document its effect", ErrInvalidManifest, w.Key)
		}
		if _, dup := seen[w.Key]; dup {
			return fmt.Errorf("%w: duplicate wiring key %q", ErrInvalidManifest, w.Key)
		}
		seen[w.Key] = struct{}{}
	}

	return nil
}

func validatePrefix(field, prefix string) error {
	if !strings.HasPrefix(prefix, "/") {
		return fmt.Errorf("%w: %s %q must start with /", ErrInvalidManifest, field, prefix)
	}
	if !strings.HasSuffix(prefix, "/") {
		return fmt.Errorf("%w: %s %q must end with /", ErrInvalidManifest, field, prefix)
	}

	return nil
}

func (h *Health) validate() error {
	if !strings.HasPrefix(h.Endpoint, "/") {
		return fmt.Errorf("%w: health.endpoint must be absolute, got %q", ErrInvalidManifest, h.Endpoint)
	}
	if h.InitialDelaySeconds <= 0 {
		h.InitialDelaySeconds = defaultInitialDelay
	}
	if h.IntervalSeconds <= 0 {
		h.IntervalSeconds = defaultHealthInterval
	}
	if h.FailureThreshold <= 0 {
		h.FailureThreshold = defaultFailureThreshold
	}

	return nil
}
