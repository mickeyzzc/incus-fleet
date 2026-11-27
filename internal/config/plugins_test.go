package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginsConfigDefaults(t *testing.T) {
	cfg := minimalConfig()
	cfg.applyDefaults()

	if cfg.Plugins.Dir != defaultPluginDir {
		t.Errorf("Plugins.Dir = %q, want %q", cfg.Plugins.Dir, defaultPluginDir)
	}
	if cfg.Plugins.HealthInterval != defaultPluginHealthInterval {
		t.Errorf("Plugins.HealthInterval = %q, want %q", cfg.Plugins.HealthInterval, defaultPluginHealthInterval)
	}
}

func TestPluginsConfigDisabledByDefault(t *testing.T) {
	cfg := minimalConfig()
	cfg.applyDefaults()

	if cfg.Plugins.Enabled {
		t.Fatal("Plugins.Enabled defaulted to true, want opt-in")
	}
}

func TestPluginsValidateAcceptsWellFormedEntry(t *testing.T) {
	cfg := minimalConfig()
	cfg.Plugins = PluginsConfig{
		Enabled: true,
		Dir:     "data/plugins",
		Entries: []PluginEntry{{
			ID:       "victoria-metrics",
			Enabled:  true,
			Upstream: "http://127.0.0.1:8428",
			Role:     "admin",
			UI: PluginUIManifest{
				NavGroup: "resources",
				Path:     "/plugin-ui/victoria-metrics",
				TitleKey: "plugins.victoriaMetrics",
			},
		}},
	}
	cfg.applyDefaults()

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestPluginsValidateRejectsBadEntries(t *testing.T) {
	tests := []struct {
		name  string
		entry PluginEntry
		want  string
	}{
		{
			name:  "invalid id",
			entry: PluginEntry{ID: "Not Valid", Upstream: "http://127.0.0.1:1", Role: "viewer"},
			want:  "id",
		},
		{
			name:  "missing upstream",
			entry: PluginEntry{ID: "ok", Role: "viewer"},
			want:  "upstream",
		},
		{
			name:  "upstream not http",
			entry: PluginEntry{ID: "ok", Upstream: "ftp://127.0.0.1", Role: "viewer"},
			want:  "upstream",
		},
		{
			name:  "invalid role",
			entry: PluginEntry{ID: "ok", Upstream: "http://127.0.0.1:1", Role: "root"},
			want:  "role",
		},
		{
			name: "ui path not absolute",
			entry: PluginEntry{
				ID: "ok", Upstream: "http://127.0.0.1:1", Role: "viewer",
				UI: PluginUIManifest{Path: "plugins/ok"},
			},
			want: "path",
		},
		{
			name: "ui path collides with core route",
			entry: PluginEntry{
				ID: "ok", Upstream: "http://127.0.0.1:1", Role: "viewer",
				UI: PluginUIManifest{Path: "/instances"},
			},
			want: "reserved",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := minimalConfig()
			cfg.Plugins = PluginsConfig{Enabled: true, Dir: "data/plugins", Entries: []PluginEntry{tc.entry}}
			cfg.applyDefaults()

			err := cfg.Validate()
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() error = %q, want %q", err, tc.want)
			}
		})
	}
}

func TestPluginsValidateRejectsDuplicateIDs(t *testing.T) {
	cfg := minimalConfig()
	cfg.Plugins = PluginsConfig{
		Enabled: true,
		Dir:     "data/plugins",
		Entries: []PluginEntry{
			{ID: "dup", Upstream: "http://127.0.0.1:1", Role: "viewer"},
			{ID: "dup", Upstream: "http://127.0.0.1:2", Role: "viewer"},
		},
	}
	cfg.applyDefaults()

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want duplicate id error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("Validate() error = %q, want duplicate", err)
	}
}

func TestPluginsValidateIgnoresEntriesWhenDisabled(t *testing.T) {
	cfg := minimalConfig()
	cfg.Plugins = PluginsConfig{
		Enabled: false,
		Entries: []PluginEntry{{ID: "BAD ID"}},
	}
	cfg.applyDefaults()

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want entries ignored while disabled", err)
	}
}

func TestPluginsValidationErrorsNameThePlugin(t *testing.T) {
	cfg := minimalConfig()
	cfg.Plugins = PluginsConfig{
		Enabled: true,
		Dir:     "data/plugins",
		Entries: []PluginEntry{{ID: "broken", Role: "viewer"}},
	}
	cfg.applyDefaults()

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Fatalf("Validate() error = %q, want it to name the plugin", err)
	}
}

func TestPluginsValidateHealthInterval(t *testing.T) {
	cfg := minimalConfig()
	cfg.Plugins = PluginsConfig{Enabled: true, Dir: "data/plugins", HealthInterval: "not-a-duration"}
	cfg.applyDefaults()

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "health_interval") {
		t.Fatalf("Validate() error = %v, want health_interval", err)
	}
}

func TestPluginsValidateMissingDir(t *testing.T) {
	cfg := minimalConfig()
	cfg.Plugins = PluginsConfig{Enabled: true, Dir: "   "}
	cfg.applyDefaults()
	cfg.Plugins.Dir = "   "

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "dir") {
		t.Fatalf("Validate() error = %v, want dir", err)
	}
}

func TestPluginsValidateUIConstraints(t *testing.T) {
	tests := []struct {
		name string
		ui   PluginUIManifest
		want string
	}{
		{name: "missing leading slash", ui: PluginUIManifest{Path: "plugins/x"}, want: "absolute"},
		{name: "outside namespace", ui: PluginUIManifest{Path: "/elsewhere/x"}, want: "must live under"},
		{name: "manager prefix", ui: PluginUIManifest{Path: "/plugins/some-plugin"}, want: "reserved"},
		{name: "manager index", ui: PluginUIManifest{Path: "/plugins"}, want: "reserved"},
		{name: "bad requires role", ui: PluginUIManifest{Path: "/plugin-ui/x", RequiresRole: "root"}, want: "requires_role"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := minimalConfig()
			cfg.Plugins = PluginsConfig{
				Enabled: true, Dir: "data/plugins",
				Entries: []PluginEntry{{
					ID: "ok", Upstream: "http://127.0.0.1:1", Role: "viewer", UI: tc.ui,
				}},
			}
			cfg.applyDefaults()

			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestPluginsValidateDuplicateUIPath(t *testing.T) {
	cfg := minimalConfig()
	cfg.Plugins = PluginsConfig{
		Enabled: true, Dir: "data/plugins",
		Entries: []PluginEntry{
			{ID: "a", Upstream: "http://127.0.0.1:1", Role: "viewer", UI: PluginUIManifest{Path: "/plugin-ui/same"}},
			{ID: "b", Upstream: "http://127.0.0.1:2", Role: "viewer", UI: PluginUIManifest{Path: "/plugin-ui/same"}},
		},
	}
	cfg.applyDefaults()

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("Validate() error = %v, want duplicate", err)
	}
}

func TestPluginsValidateUpstreamPortOnly(t *testing.T) {
	cfg := minimalConfig()
	cfg.Plugins = PluginsConfig{
		Enabled: true, Dir: "data/plugins",
		Entries: []PluginEntry{{ID: "instance", UpstreamPort: 8428, Role: "viewer"}},
	}
	cfg.applyDefaults()

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want upstream_port alone to be valid", err)
	}
}

func TestPluginEventsAndSigningConfig(t *testing.T) {
	cfg := minimalConfig()
	cfg.Plugins = PluginsConfig{Enabled: true, Dir: "data/plugins"}
	cfg.applyDefaults()
	if cfg.Plugins.Events.Path != defaultPluginEventsPath {
		t.Fatalf("events.path = %q, want %q", cfg.Plugins.Events.Path, defaultPluginEventsPath)
	}
	if cfg.Plugins.Events.Enabled {
		t.Fatal("events must be opt-in")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	cfg.Plugins.Events = PluginEventsConfig{
		Enabled: true, Path: "/api/v1/plugins/events",
		SecretFile: filepath.Join(t.TempDir(), "events-secret"),
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate(events) error = %v", err)
	}
	cfg.Plugins.Events.Path = "relative"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "events.path") {
		t.Fatalf("Validate(relative path) = %v", err)
	}
	cfg.Plugins.Events.Path = "/api/v1/plugins/events"
	cfg.Plugins.Events.SecretFile = ""
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "events.secret_file") {
		t.Fatalf("Validate(missing secret) = %v", err)
	}
}

func TestPluginsValidateBadUpstreamPort(t *testing.T) {
	cfg := minimalConfig()
	cfg.Plugins = PluginsConfig{
		Enabled: true, Dir: "data/plugins",
		Entries: []PluginEntry{{
			ID: "bad", Upstream: "http://127.0.0.1:1", UpstreamPort: 99999, Role: "viewer",
		}},
	}
	cfg.applyDefaults()

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "upstream_port") {
		t.Fatalf("Validate() error = %v, want upstream_port", err)
	}
}
