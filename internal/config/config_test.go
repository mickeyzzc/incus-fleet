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
