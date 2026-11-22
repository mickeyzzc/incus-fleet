package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}

	return path
}

func validConfig(t *testing.T) (string, string, string, string) {
	t.Helper()

	dir := t.TempDir()
	cert := writeFile(t, dir, "client.crt", "cert")
	key := writeFile(t, dir, "client.key", "key")
	serverCert := writeFile(t, dir, "server.crt", "server-cert")
	configPath := writeFile(t, dir, "config.yaml", `
server:
  listen: 127.0.0.1:8080
  auth_token: file-token
  log_level: debug
database:
  driver: sqlite
  dsn: /tmp/incus-fleet.db
clusters:
  - name: c1
    endpoint: https://192.0.2.10:8443/
    tls_cert: `+cert+`
    tls_key: `+key+`
    tls_server_cert: `+serverCert+`
`)

	return configPath, cert, key, serverCert
}

func TestLoadValid(t *testing.T) {
	t.Setenv("INCUS_FLEET_AUTH_TOKEN", "")
	t.Setenv("INCUS_FLEET_LISTEN", "")
	t.Setenv("INCUS_FLEET_LOG_LEVEL", "")
	t.Setenv("INCUS_FLEET_DATABASE_DRIVER", "")
	t.Setenv("INCUS_FLEET_DATABASE_DSN", "")

	path, _, _, _ := validConfig(t)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.AuthToken != "file-token" {
		t.Fatalf("AuthToken = %q", cfg.Server.AuthToken)
	}
	if cfg.Server.LogLevel != "debug" {
		t.Fatalf("LogLevel = %q", cfg.Server.LogLevel)
	}
	if cfg.Database.Driver != "sqlite" {
		t.Fatalf("Driver = %q", cfg.Database.Driver)
	}
	if len(cfg.Clusters) != 1 {
		t.Fatalf("Clusters = %d", len(cfg.Clusters))
	}
	if cfg.Clusters[0].DisplayName != "c1" {
		t.Fatalf("DisplayName = %q", cfg.Clusters[0].DisplayName)
	}
	if cfg.Clusters[0].Project != "default" {
		t.Fatalf("Project = %q", cfg.Clusters[0].Project)
	}
	if cfg.Clusters[0].Endpoint != "https://192.0.2.10:8443" {
		t.Fatalf("Endpoint = %q", cfg.Clusters[0].Endpoint)
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Setenv("INCUS_FLEET_AUTH_TOKEN", "env-token")
	t.Setenv("INCUS_FLEET_LISTEN", "127.0.0.1:9090")
	t.Setenv("INCUS_FLEET_LOG_LEVEL", "warn")
	t.Setenv("INCUS_FLEET_DATABASE_DRIVER", "postgres")
	t.Setenv("INCUS_FLEET_DATABASE_DSN", "postgres://user:pass@localhost/db")

	path, _, _, _ := validConfig(t)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.AuthToken != "env-token" {
		t.Fatalf("AuthToken = %q", cfg.Server.AuthToken)
	}
	if cfg.Server.Listen != "127.0.0.1:9090" {
		t.Fatalf("Listen = %q", cfg.Server.Listen)
	}
	if cfg.Server.LogLevel != "warn" {
		t.Fatalf("LogLevel = %q", cfg.Server.LogLevel)
	}
	if cfg.Database.Driver != "postgres" {
		t.Fatalf("Driver = %q", cfg.Database.Driver)
	}
	if cfg.Database.DSN != "postgres://user:pass@localhost/db" { //nolint:gosec // test fixture
		t.Fatalf("DSN = %q", cfg.Database.DSN)
	}
}

func TestDatabaseIsShared(t *testing.T) {
	cases := []struct {
		driver string
		want   bool
	}{
		{"sqlite", false},
		{"rqlite", true},
		{"postgres", true},
	}
	for _, tc := range cases {
		if got := (Database{Driver: tc.driver}).IsShared(); got != tc.want {
			t.Fatalf("Database{%q}.IsShared() = %v, want %v", tc.driver, got, tc.want)
		}
	}
}

func TestLoadRqliteDriver(t *testing.T) {
	t.Setenv("INCUS_FLEET_DATABASE_DRIVER", "rqlite")
	t.Setenv("INCUS_FLEET_DATABASE_DSN", "http://127.0.0.1:4001")

	path, _, _, _ := validConfig(t)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Database.Driver != "rqlite" {
		t.Fatalf("Driver = %q, want rqlite", cfg.Database.Driver)
	}
	if cfg.Database.DSN != "http://127.0.0.1:4001" {
		t.Fatalf("DSN = %q", cfg.Database.DSN)
	}
	if !cfg.Database.IsShared() {
		t.Fatal("rqlite must be treated as a shared store")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil || !strings.Contains(err.Error(), "read config") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	path := writeFile(t, t.TempDir(), "config.yaml", "server: [")
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "parse config") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestValidateErrors(t *testing.T) {
	base := func() Config {
		return Config{
			Server:   Server{Listen: ":8080", AuthToken: "token", LogLevel: "info"},
			Database: Database{Driver: "sqlite", DSN: "test.db"},
			Clusters: []Cluster{{
				Name:     "c1",
				Endpoint: "https://192.0.2.10:8443",
				TLSCert:  "client.crt",
				TLSKey:   "client.key",
			}},
		}
	}

	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"missing token", func(c *Config) { c.Server.AuthToken = "" }, "auth_token"},
		{"missing listen", func(c *Config) { c.Server.Listen = "" }, "server.listen"},
		{"bad log level", func(c *Config) { c.Server.LogLevel = "trace" }, "log_level"},
		{"bad database driver", func(c *Config) { c.Database.Driver = "mysql" }, "database.driver"},
		{"missing dsn", func(c *Config) { c.Database.DSN = "" }, "database.dsn"},
		{"no clusters", func(c *Config) { c.Clusters = nil }, "at least one cluster"},
		{"bad cluster name", func(c *Config) { c.Clusters[0].Name = "Cluster 62" }, "invalid name"},
		{"duplicate cluster", func(c *Config) { c.Clusters = append(c.Clusters, c.Clusters[0]) }, "duplicate cluster"},
		{"bad endpoint", func(c *Config) { c.Clusters[0].Endpoint = "http://example.com" }, "valid https URL"},
		{"missing cert", func(c *Config) { c.Clusters[0].TLSCert = "" }, "tls_cert and tls_key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base()
			tt.mutate(&cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestValidateFiles(t *testing.T) {
	dir := t.TempDir()
	cert := writeFile(t, dir, "client.crt", "cert")
	key := writeFile(t, dir, "client.key", "key")

	cfg := Config{Clusters: []Cluster{{
		Name:     "c1",
		Endpoint: "https://192.0.2.10:8443",
		TLSCert:  cert,
		TLSKey:   key,
	}}}
	if err := cfg.ValidateFiles(); err != nil {
		t.Fatalf("ValidateFiles() error = %v", err)
	}

	cfg.Clusters[0].TLSCert = filepath.Join(dir, "missing.crt")
	if err := cfg.ValidateFiles(); err == nil {
		t.Fatal("ValidateFiles() expected error")
	}

	cfg.Clusters[0].TLSCert = dir
	if err := cfg.ValidateFiles(); err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("ValidateFiles() error = %v", err)
	}
}

func TestReadPEMOrFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "inline.pem", "from-file")

	got, err := ReadPEMOrFile("-----BEGIN CERTIFICATE-----\nYQ==\n-----END CERTIFICATE-----\n")
	if err != nil {
		t.Fatalf("ReadPEMOrFile(inline) error = %v", err)
	}
	if !strings.Contains(string(got), "BEGIN CERTIFICATE") {
		t.Fatalf("ReadPEMOrFile(inline) = %q", got)
	}

	got, err = ReadPEMOrFile(path)
	if err != nil {
		t.Fatalf("ReadPEMOrFile(path) error = %v", err)
	}
	if string(got) != "from-file" {
		t.Fatalf("ReadPEMOrFile(path) = %q", got)
	}

	if _, err := ReadPEMOrFile(filepath.Join(dir, "missing.pem")); err == nil {
		t.Fatal("ReadPEMOrFile(missing) expected error")
	}
}

func TestValidateFilesSkipsInlinePEM(t *testing.T) {
	cfg := Config{Clusters: []Cluster{{
		Name:     "c1",
		Endpoint: "https://192.0.2.10:8443",
		TLSCert:  "-----BEGIN CERTIFICATE-----\nYQ==\n-----END CERTIFICATE-----\n",
		TLSKey:   "-----BEGIN PRIVATE KEY-----\nYQ==\n-----END PRIVATE KEY-----\n",
	}}}
	if err := cfg.ValidateFiles(); err != nil {
		t.Fatalf("ValidateFiles(inline PEM) error = %v", err)
	}
}

func TestClusterByName(t *testing.T) {
	cfg := Config{Clusters: []Cluster{{Name: "c1"}, {Name: "c2"}}}
	if cl, ok := cfg.ClusterByName("c2"); !ok || cl.Name != "c2" {
		t.Fatalf("ClusterByName(c2) = %#v, %v", cl, ok)
	}
	if _, ok := cfg.ClusterByName("missing"); ok {
		t.Fatal("ClusterByName(missing) found")
	}
}

func TestLoadDefaults(t *testing.T) {
	for _, key := range []string{
		"INCUS_FLEET_AUTH_TOKEN",
		"INCUS_FLEET_LISTEN",
		"INCUS_FLEET_LOG_LEVEL",
		"INCUS_FLEET_DATABASE_DRIVER",
		"INCUS_FLEET_DATABASE_DSN",
	} {
		t.Setenv(key, "")
	}

	dir := t.TempDir()
	cert := writeFile(t, dir, "client.crt", "cert")
	key := writeFile(t, dir, "client.key", "key")
	path := writeFile(t, dir, "config.yaml", `
server:
  auth_token: secret
database:
  dsn: fleet.db
clusters:
  - name: c1
    display_name: Cluster 62
    project: fleet
    endpoint: https://192.0.2.10:8443
    tls_cert: `+cert+`
    tls_key: `+key+`
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Listen != "0.0.0.0:8080" || cfg.Server.LogLevel != "info" {
		t.Fatalf("server defaults = %#v", cfg.Server)
	}
	if cfg.Database.Driver != "sqlite" {
		t.Fatalf("database driver = %q", cfg.Database.Driver)
	}
	if cfg.Clusters[0].DisplayName != "Cluster 62" || cfg.Clusters[0].Project != "fleet" {
		t.Fatalf("cluster defaults = %#v", cfg.Clusters[0])
	}
}

func minimalConfig() Config {
	return Config{
		Server:   Server{Listen: "127.0.0.1:8080", AuthToken: "token", LogLevel: "info"},
		Database: Database{Driver: "sqlite", DSN: "/tmp/fleet.db"},
		Clusters: []Cluster{{
			Name:     "c1",
			Endpoint: "https://192.0.2.10:8443",
			TLSCert:  "cert",
			TLSKey:   "key",
		}},
	}
}

func TestAgentAndMetricsDefaults(t *testing.T) {
	cfg := minimalConfig()
	cfg.applyDefaults()

	if cfg.Agent.Listen != "0.0.0.0:8444" || cfg.Agent.Enabled {
		t.Fatalf("agent defaults = %#v", cfg.Agent)
	}
	if cfg.Metrics.Retention != "1h" || cfg.Metrics.Interval != "10s" || cfg.Metrics.Enabled {
		t.Fatalf("metrics defaults = %#v", cfg.Metrics)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate(defaults) error = %v", err)
	}
}

func TestAgentAndMetricsValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
		err    bool
	}{
		{"bad listen", func(c *Config) { c.Agent.Listen = "nope" }, true},
		{"good listen", func(c *Config) { c.Agent.Listen = "127.0.0.1:9444" }, false},
		{"bad retention", func(c *Config) { c.Metrics.Retention = "2h" }, true},
		{"bad interval", func(c *Config) { c.Metrics.Interval = "500ms" }, true},
		{"good metrics", func(c *Config) { c.Metrics.Retention = "30m"; c.Metrics.Interval = "30s" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := minimalConfig()
			cfg.applyDefaults()
			tc.mutate(&cfg)
			err := cfg.Validate()
			if tc.err && err == nil {
				t.Fatal("Validate() error = nil")
			}
			if !tc.err && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestNodeAccessValidation(t *testing.T) {
	cases := []struct {
		name   string
		access NodeAccess
		err    bool
	}{
		{"disabled", NodeAccess{}, false},
		{"user only", NodeAccess{SSHUser: "node-admin"}, true},
		{"key only", NodeAccess{SSHKey: "/key"}, true},
		{"bad port", NodeAccess{SSHUser: "node-admin", SSHKey: "/key", SSHPort: 70000, SSHInsecureIgnoreHostKey: true}, true},
		{"no host policy", NodeAccess{SSHUser: "node-admin", SSHKey: "/key", SSHPort: 22}, true},
		{"insecure", NodeAccess{SSHUser: "node-admin", SSHKey: "/key", SSHPort: 22, SSHInsecureIgnoreHostKey: true}, false},
		{"known hosts", NodeAccess{SSHUser: "node-admin", SSHKey: "/key", SSHPort: 22, SSHKnownHosts: "/known"}, false},
		{"auto mode", NodeAccess{Mode: "auto"}, false},
		{"agent mode", NodeAccess{Mode: "agent"}, false},
		{"ssh mode", NodeAccess{Mode: "ssh"}, false},
		{"local mode", NodeAccess{Mode: "local"}, false},
		{"bad mode", NodeAccess{Mode: "magic"}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := minimalConfig()
			cfg.NodeAccess = tc.access
			err := cfg.Validate()
			if tc.err && err == nil {
				t.Fatalf("Validate() error = nil")
			}
			if !tc.err && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestNodeAccessFilesAndDefaults(t *testing.T) {
	cfg := minimalConfig()
	cfg.NodeAccess = NodeAccess{
		SSHUser:                  "node-admin",
		SSHKey:                   "/does/not/exist",
		SSHInsecureIgnoreHostKey: true,
	}
	cfg.applyDefaults()
	if cfg.NodeAccess.SSHPort != 22 {
		t.Fatalf("SSHPort default = %d", cfg.NodeAccess.SSHPort)
	}
	if cfg.NodeAccess.Mode != "auto" {
		t.Fatalf("Mode default = %q", cfg.NodeAccess.Mode)
	}
	if !cfg.NodeAccess.SSHWriteFallbackEnabled() {
		t.Fatal("SSH write fallback should default to enabled")
	}
	disabled := false
	cfg.NodeAccess.SSHWriteFallback = &disabled
	if cfg.NodeAccess.SSHWriteFallbackEnabled() {
		t.Fatal("SSH write fallback should honor explicit false")
	}
	if err := cfg.ValidateFiles(); err == nil || !strings.Contains(err.Error(), "node_access.ssh_key") {
		t.Fatalf("ValidateFiles() error = %v", err)
	}

	dir := t.TempDir()
	key := writeFile(t, dir, "id_ed25519", "key")
	known := writeFile(t, dir, "known_hosts", "host key")
	cert := writeFile(t, dir, "client.crt", "cert")
	tlsKey := writeFile(t, dir, "client.key", "key")
	cfg.NodeAccess.SSHKey = key
	cfg.NodeAccess.SSHKnownHosts = known
	cfg.Clusters[0].TLSCert = cert
	cfg.Clusters[0].TLSKey = tlsKey
	if err := cfg.ValidateFiles(); err != nil {
		t.Fatalf("ValidateFiles() error = %v", err)
	}

	cfg.NodeAccess.SSHKnownHosts = filepath.Join(dir, "missing")
	if err := cfg.ValidateFiles(); err == nil || !strings.Contains(err.Error(), "ssh_known_hosts") {
		t.Fatalf("ValidateFiles(known_hosts) error = %v", err)
	}
}

func TestAgentEnabledRequiresTLSFiles(t *testing.T) {
	cfg := minimalConfig()
	cfg.applyDefaults()
	cfg.Agent.Enabled = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() expected error for missing agent TLS settings")
	}
	cfg.Agent.TLSCert = "/cert"
	cfg.Agent.TLSKey = "/key"
	cfg.Agent.ClientCA = "/ca"
	cfg.Agent.CAKey = "/ca-key"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestAgentInstallValidation(t *testing.T) {
	cfg := minimalConfig()
	cfg.Agent = AgentConfig{
		Enabled: true, Listen: "0.0.0.0:8444", TLSCert: "/cert", TLSKey: "/key",
		ClientCA: "/ca", CAKey: "/ca-key",
		Install: AgentInstallConfig{
			Enabled: true, GatewayURL: "https://fleet.example:8444/",
			AssetURL: "https://fleet.example/", AgentBinary: "/agent", HelperBinary: "/helper",
		},
	}
	cfg.applyDefaults()
	if cfg.Agent.Install.GatewayURL != "https://fleet.example:8444" ||
		cfg.Agent.Install.AssetURL != "https://fleet.example" {
		t.Fatalf("install URLs not trimmed: %#v", cfg.Agent.Install)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate(install) error = %v", err)
	}
	multi := cfg
	multi.Agent.Install.AgentBinary = ""
	multi.Agent.Install.HelperBinary = ""
	multi.Agent.Install.AgentBinaries = map[string]string{"amd64": "/agent-amd64", "arm64": "/agent-arm64"}
	multi.Agent.Install.HelperBinaries = map[string]string{"amd64": "/helper-amd64", "arm64": "/helper-arm64"}
	if err := multi.Validate(); err != nil {
		t.Fatalf("Validate(multiarch) error = %v", err)
	}
	cases := []struct {
		name string
		edit func(*Config)
	}{
		{"gateway missing", func(c *Config) { c.Agent.Install.GatewayURL = "" }},
		{"gateway http", func(c *Config) { c.Agent.Install.GatewayURL = "http://fleet.example" }},
		{"gateway invalid", func(c *Config) { c.Agent.Install.GatewayURL = "://bad" }},
		{"asset invalid", func(c *Config) { c.Agent.Install.AssetURL = "ftp://fleet.example" }},
		{"agent binary missing", func(c *Config) { c.Agent.Install.AgentBinary = " " }},
		{"helper binary missing", func(c *Config) { c.Agent.Install.HelperBinary = "" }},
		{"agent disabled", func(c *Config) { c.Agent.Enabled = false }},
		{"multiarch mismatch", func(c *Config) {
			c.Agent.Install.AgentBinary = ""
			c.Agent.Install.HelperBinary = ""
			c.Agent.Install.AgentBinaries = map[string]string{"amd64": "/a", "arm64": "/b"}
			c.Agent.Install.HelperBinaries = map[string]string{"amd64": "/h"}
		}},
		{"multiarch empty", func(c *Config) {
			c.Agent.Install.AgentBinary = ""
			c.Agent.Install.HelperBinary = ""
			c.Agent.Install.AgentBinaries = map[string]string{"": "/a"}
			c.Agent.Install.HelperBinaries = map[string]string{"": "/h"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := minimalConfig()
			cfg.Agent = AgentConfig{
				Enabled: true, Listen: "0.0.0.0:8444", TLSCert: "/cert", TLSKey: "/key",
				ClientCA: "/ca", CAKey: "/ca-key",
				Install: AgentInstallConfig{
					Enabled: true, GatewayURL: "https://fleet.example:8444",
					AgentBinary: "/agent", HelperBinary: "/helper",
				},
			}
			tc.edit(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestAgentValidateFiles(t *testing.T) {
	dir := t.TempDir()
	cert := writeFile(t, dir, "agent-server.crt", "cert")
	key := writeFile(t, dir, "agent-server.key", "key")
	ca := writeFile(t, dir, "agent-ca.crt", "ca")
	caKey := writeFile(t, dir, "agent-ca.key", "ca-key")

	cfg := minimalConfig()
	cfg.Clusters[0].TLSCert = "-----BEGIN CERTIFICATE-----\nYQ==\n-----END CERTIFICATE-----\n"
	cfg.Clusters[0].TLSKey = "-----BEGIN PRIVATE KEY-----\nYQ==\n-----END PRIVATE KEY-----\n"
	cfg.Agent = AgentConfig{
		Enabled: true, Listen: "127.0.0.1:8444",
		TLSCert: cert, TLSKey: key, ClientCA: ca, CAKey: caKey,
	}
	if err := cfg.ValidateFiles(); err != nil {
		t.Fatalf("ValidateFiles() error = %v", err)
	}
	cfg.Agent.CAKey = filepath.Join(dir, "missing")
	if err := cfg.ValidateFiles(); err == nil || !strings.Contains(err.Error(), "agent.ca_key") {
		t.Fatalf("ValidateFiles(missing CA key) error = %v", err)
	}
}

func TestApplyEnvAgentAndMetrics(t *testing.T) {
	t.Setenv("INCUS_FLEET_NODE_ACCESS_MODE", "ssh")
	t.Setenv("INCUS_FLEET_NODE_ACCESS_SSH_WRITE_FALLBACK", "false")
	t.Setenv("INCUS_FLEET_AGENT_ENABLED", "true")
	t.Setenv("INCUS_FLEET_AGENT_LISTEN", "127.0.0.1:9444")
	t.Setenv("INCUS_FLEET_METRICS_ENABLED", "true")
	t.Setenv("INCUS_FLEET_METRICS_RETENTION", "30m")
	t.Setenv("INCUS_FLEET_METRICS_INTERVAL", "30s")
	t.Setenv("INCUS_FLEET_METRICS_REMOTE_WRITE_SPOOL_DIR", "/tmp/fleet-spool-test")
	t.Setenv("INCUS_FLEET_METRICS_REMOTE_WRITE_SPOOL_PARTITION", "app-test")

	cfg := minimalConfig()
	cfg.applyEnv()
	if cfg.NodeAccess.Mode != "ssh" || cfg.NodeAccess.SSHWriteFallbackEnabled() {
		t.Fatalf("node access env = %#v", cfg.NodeAccess)
	}
	if !cfg.Agent.Enabled || cfg.Agent.Listen != "127.0.0.1:9444" {
		t.Fatalf("agent env = %#v", cfg.Agent)
	}
	if !cfg.Metrics.Enabled || cfg.Metrics.Retention != "30m" || cfg.Metrics.Interval != "30s" ||
		cfg.Metrics.RemoteWrite.SpoolDir != "/tmp/fleet-spool-test" ||
		cfg.Metrics.RemoteWrite.SpoolPartition != "app-test" {
		t.Fatalf("metrics env = %#v", cfg.Metrics)
	}
}

func TestMetricsExternalDefaultsAndValidation(t *testing.T) {
	cfg := minimalConfig()
	cfg.applyDefaults()
	if cfg.Metrics.RemoteWrite.Timeout != "10s" || cfg.Metrics.RemoteWrite.QueueSize != 10000 ||
		cfg.Metrics.RemoteWrite.BatchSize != 500 || cfg.Metrics.Query.Type != "prometheus" ||
		cfg.Metrics.HTTPSD.Mode != "proxy" || cfg.Metrics.RemoteWrite.SpoolDir == "" ||
		cfg.Metrics.RemoteWrite.MaxBytes != 64<<20 {
		t.Fatalf("external defaults = %#v %#v %#v", cfg.Metrics.RemoteWrite, cfg.Metrics.Query, cfg.Metrics.HTTPSD)
	}

	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"remote url", func(c *Config) { c.Metrics.RemoteWrite.Enabled = true; c.Metrics.RemoteWrite.URL = "bad" }},
		{"remote timeout", func(c *Config) {
			c.Metrics.RemoteWrite.Enabled = true
			c.Metrics.RemoteWrite.URL = "https://example.com/write"
			c.Metrics.RemoteWrite.Timeout = "bad"
		}},
		{"query url", func(c *Config) { c.Metrics.Query.Enabled = true; c.Metrics.Query.URL = "bad" }},
		{"query type", func(c *Config) {
			c.Metrics.Query.Enabled = true
			c.Metrics.Query.URL = "https://example.com"
			c.Metrics.Query.Type = "magic"
		}},
		{"sd token", func(c *Config) { c.Metrics.HTTPSD.Enabled = true }},
		{"sd mode", func(c *Config) {
			c.Metrics.HTTPSD.Enabled = true
			c.Metrics.HTTPSD.Token = "t"
			c.Metrics.HTTPSD.Mode = "magic"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := minimalConfig()
			cfg.applyDefaults()
			tc.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() expected error")
			}
		})
	}
}

func TestHAConfigDefaultsAndValidation(t *testing.T) {
	cfg := minimalConfig()
	cfg.applyDefaults()
	if cfg.HA.Lease != "30s" || cfg.HA.Interval != "2s" || cfg.HA.Enabled {
		t.Fatalf("ha defaults = %#v", cfg.HA)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate(defaults) error = %v", err)
	}
	cases := []HAConfig{
		{Lease: "1s"},
		{Lease: "2h"},
		{Interval: "10ms"},
		{Interval: "2m"},
		{Enabled: true, WorkerID: "bad worker"},
	}
	for _, ha := range cases {
		cfg := minimalConfig()
		cfg.applyDefaults()
		cfg.HA = ha
		if err := cfg.Validate(); err == nil {
			t.Fatalf("Validate(%#v) expected error", ha)
		}
	}
}

func TestClusterEndpoints(t *testing.T) {
	cfg := minimalConfig()
	cfg.Clusters[0].Endpoint = ""
	cfg.Clusters[0].Endpoints = []string{"https://192.0.2.10:8443", "https://192.0.2.11:8443"}
	cfg.applyDefaults()
	if cfg.Clusters[0].Endpoint != cfg.Clusters[0].Endpoints[0] {
		t.Fatalf("primary endpoint = %q", cfg.Clusters[0].Endpoint)
	}
	if got := ClusterEndpoints(cfg.Clusters[0]); len(got) != 2 {
		t.Fatalf("ClusterEndpoints = %v", got)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate(endpoints) error = %v", err)
	}
	cfg.Clusters[0].Endpoints[1] = "http://192.0.2.11:8443"
	if err := cfg.Validate(); err == nil {
		t.Fatal("invalid endpoint accepted")
	}
}

func TestMeshConfigValidation(t *testing.T) {
	base := minimalConfig()
	base.Agent = AgentConfig{
		Enabled: true, Listen: "0.0.0.0:8444", TLSCert: "/cert", TLSKey: "/key",
		ClientCA: "/ca", CAKey: "/ca-key",
	}
	peers := []MeshPeer{
		{ID: "app-a", Address: "192.0.2.20:9400", ControlAddress: "192.0.2.20:9401"},
		{ID: "app-b", Address: "192.0.2.21:9400", ControlAddress: "192.0.2.21:9401"},
		{ID: "witness", Address: "192.0.2.22:9400", ControlAddress: "192.0.2.22:9401"},
	}
	base.Mesh = MeshConfig{
		Enabled: true, NodeID: "app-a", Address: "192.0.2.20:9400",
		ControlAddress: "192.0.2.20:9401", DataDir: "/tmp/mesh-a", Secret: "mesh-secret", Peers: peers,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("Validate(mesh) error = %v", err)
	}
	witness := base
	witness.Mesh.Witness = true
	witness.Agent.Enabled = false
	if err := witness.Validate(); err != nil {
		t.Fatalf("Validate(witness) error = %v", err)
	}

	cases := []struct {
		name string
		edit func(*Config)
	}{
		{"no peers", func(c *Config) { c.Mesh.Peers = nil }},
		{"even peers", func(c *Config) { c.Mesh.Peers = peers[:2] }},
		{"no node id", func(c *Config) { c.Mesh.NodeID = "" }},
		{"no address", func(c *Config) { c.Mesh.Address = "" }},
		{"bad address", func(c *Config) { c.Mesh.Address = "bad" }},
		{"no control address", func(c *Config) { c.Mesh.ControlAddress = "" }},
		{"bad control address", func(c *Config) { c.Mesh.ControlAddress = "bad" }},
		{"no data dir", func(c *Config) { c.Mesh.DataDir = "" }},
		{"no secret", func(c *Config) { c.Mesh.Secret = "" }},
		{"duplicate peer", func(c *Config) { c.Mesh.Peers = append([]MeshPeer(nil), peers...); c.Mesh.Peers[1].ID = "app-a" }},
		{"empty peer id", func(c *Config) { c.Mesh.Peers = append([]MeshPeer(nil), peers...); c.Mesh.Peers[1].ID = "" }},
		{"bad peer address", func(c *Config) { c.Mesh.Peers = append([]MeshPeer(nil), peers...); c.Mesh.Peers[1].Address = "bad" }},
		{"bad peer control", func(c *Config) {
			c.Mesh.Peers = append([]MeshPeer(nil), peers...)
			c.Mesh.Peers[1].ControlAddress = "bad"
		}},
		{"unknown self", func(c *Config) { c.Mesh.NodeID = "missing" }},
		{"agent disabled", func(c *Config) { c.Agent.Enabled = false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := minimalConfig()
			cfg.Agent = AgentConfig{
				Enabled: true, Listen: "0.0.0.0:8444", TLSCert: "/cert", TLSKey: "/key",
				ClientCA: "/ca", CAKey: "/ca-key",
			}
			cfg.Mesh = MeshConfig{
				Enabled: true, NodeID: "app-a", Address: "192.0.2.20:9400",
				ControlAddress: "192.0.2.20:9401", DataDir: "/tmp/mesh-a",
				Secret: "mesh-secret", Peers: append([]MeshPeer(nil), peers...),
			}
			tc.edit(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestMeshEnvSecret(t *testing.T) {
	t.Setenv("INCUS_FLEET_MESH_SECRET", "from-env")
	cfg := minimalConfig()
	cfg.applyDefaults()
	cfg.applyEnv()
	if cfg.Mesh.Secret != "from-env" {
		t.Fatalf("mesh secret = %q", cfg.Mesh.Secret)
	}
}

func TestValidateRejectsBadLogLevelDriverAndNodeMode(t *testing.T) {
	base := Config{
		Server: Server{Listen: "0.0.0.0:8080", AuthToken: "secret", LogLevel: "info"},
		Database: Database{
			Driver: "sqlite",
			DSN:    "/tmp/fleet.db",
		},
	}

	bad := base
	bad.Server.LogLevel = "verbose"
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "log_level") {
		t.Fatalf("Validate() error = %v, want log_level rejection", err)
	}

	bad = base
	bad.Database.Driver = "mysql"
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "database.driver") {
		t.Fatalf("Validate() error = %v, want driver rejection", err)
	}

	bad = base
	bad.NodeAccess.Mode = "carrier-pigeon"
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "node_access.mode") {
		t.Fatalf("Validate() error = %v, want node_access.mode rejection", err)
	}
}
