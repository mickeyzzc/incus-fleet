// Package config loads and validates the incus-fleet configuration.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var clusterNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// Config is the top-level application configuration.
type Config struct {
	Server      Server     `yaml:"server"`
	Database    Database   `yaml:"database"`
	NodeAccess  NodeAccess `yaml:"node_access"`
	LocalMember string     `yaml:"local_member"`
	Clusters    []Cluster  `yaml:"clusters"`
}

// NodeAccess configures the SSH identity used to manage network settings on
// cluster members other than the host incus-fleet runs on. The account must
// have passwordless sudo; deploy/nodesudoers provides the minimal whitelist.
type NodeAccess struct {
	SSHUser                  string `yaml:"ssh_user"`
	SSHKey                   string `yaml:"ssh_key"`
	SSHPort                  int    `yaml:"ssh_port"`
	SSHKnownHosts            string `yaml:"ssh_known_hosts"`
	SSHInsecureIgnoreHostKey bool   `yaml:"ssh_insecure_ignore_host_key"`
}

// Server contains HTTP server settings.
type Server struct {
	Listen    string `yaml:"listen"`
	AuthToken string `yaml:"auth_token"`
	LogLevel  string `yaml:"log_level"`
}

// Database selects the persistent store.
type Database struct {
	Driver string `yaml:"driver"`
	DSN    string `yaml:"dsn"`
}

// Cluster describes one Incus cluster endpoint.
type Cluster struct {
	Name          string `yaml:"name"`
	DisplayName   string `yaml:"display_name"`
	Endpoint      string `yaml:"endpoint"`
	Project       string `yaml:"project"`
	TLSCert       string `yaml:"tls_cert"`
	TLSKey        string `yaml:"tls_key"`
	TLSServerCert string `yaml:"tls_server_cert"`
}

// Load reads, applies environment overrides, validates, and verifies files.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is an operator-supplied configuration path
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.applyEnv()
	cfg.applyDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if err := cfg.ValidateFiles(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) applyEnv() {
	if v := os.Getenv("INCUS_FLEET_LISTEN"); v != "" {
		c.Server.Listen = v
	}
	if v := os.Getenv("INCUS_FLEET_AUTH_TOKEN"); v != "" {
		c.Server.AuthToken = v
	}
	if v := os.Getenv("INCUS_FLEET_LOG_LEVEL"); v != "" {
		c.Server.LogLevel = v
	}
	if v := os.Getenv("INCUS_FLEET_DATABASE_DRIVER"); v != "" {
		c.Database.Driver = v
	}
	if v := os.Getenv("INCUS_FLEET_DATABASE_DSN"); v != "" {
		c.Database.DSN = v
	}
}

func (c *Config) applyDefaults() {
	if c.Server.Listen == "" {
		c.Server.Listen = "0.0.0.0:8080"
	}
	if c.Server.LogLevel == "" {
		c.Server.LogLevel = "info"
	}
	if c.Database.Driver == "" {
		c.Database.Driver = "sqlite"
	}
	if c.NodeAccess.SSHPort == 0 {
		c.NodeAccess.SSHPort = 22
	}

	for i := range c.Clusters {
		if c.Clusters[i].Project == "" {
			c.Clusters[i].Project = "default"
		}
		if c.Clusters[i].DisplayName == "" {
			c.Clusters[i].DisplayName = c.Clusters[i].Name
		}
		c.Clusters[i].Endpoint = strings.TrimRight(c.Clusters[i].Endpoint, "/")
	}
}

// Validate checks configuration invariants that do not depend on the filesystem.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.Server.AuthToken) == "" {
		return errors.New("config: server.auth_token or INCUS_FLEET_AUTH_TOKEN is required")
	}
	if strings.TrimSpace(c.Server.Listen) == "" {
		return errors.New("config: server.listen is required")
	}
	switch c.Server.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config: unsupported server.log_level %q", c.Server.LogLevel)
	}

	switch c.Database.Driver {
	case "sqlite", "postgres":
	default:
		return fmt.Errorf("config: unsupported database.driver %q", c.Database.Driver)
	}
	if strings.TrimSpace(c.Database.DSN) == "" {
		return errors.New("config: database.dsn is required")
	}

	if err := c.NodeAccess.validate(); err != nil {
		return err
	}

	if len(c.Clusters) == 0 {
		return errors.New("config: at least one cluster is required")
	}

	seen := make(map[string]struct{}, len(c.Clusters))
	for i := range c.Clusters {
		cl := &c.Clusters[i]
		if !clusterNameRE.MatchString(cl.Name) {
			return fmt.Errorf("config: cluster %d has invalid name %q", i, cl.Name)
		}
		if _, ok := seen[cl.Name]; ok {
			return fmt.Errorf("config: duplicate cluster name %q", cl.Name)
		}
		seen[cl.Name] = struct{}{}

		u, err := url.Parse(cl.Endpoint)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("config: cluster %q endpoint must be a valid https URL", cl.Name)
		}
		if strings.TrimSpace(cl.TLSCert) == "" || strings.TrimSpace(cl.TLSKey) == "" {
			return fmt.Errorf("config: cluster %q tls_cert and tls_key are required", cl.Name)
		}
	}

	return nil
}

// ReadPEMOrFile returns PEM bytes from either an inline PEM document or a
// file path. Inline values are detected by a PEM BEGIN marker so dynamically
// registered clusters can store certificate bodies while YAML configs keep
// using paths.
func ReadPEMOrFile(value string) ([]byte, error) {
	trimmed := strings.TrimSpace(value)
	if strings.Contains(trimmed, "-----BEGIN ") {
		return []byte(trimmed), nil
	}

	data, err := os.ReadFile(value)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// ValidateFiles verifies that certificate files referenced by the config exist.
func (c *Config) ValidateFiles() error {
	for _, p := range []struct {
		label string
		path  string
	}{
		{"node_access.ssh_key", c.NodeAccess.SSHKey},
		{"node_access.ssh_known_hosts", c.NodeAccess.SSHKnownHosts},
	} {
		if p.path == "" {
			continue
		}
		info, err := os.Stat(p.path)
		if err != nil {
			return fmt.Errorf("config: %s: %w", p.label, err)
		}
		if info.IsDir() {
			return fmt.Errorf("config: %s is a directory", p.label)
		}
	}

	for i := range c.Clusters {
		cl := &c.Clusters[i]
		for _, p := range []struct {
			label string
			path  string
		}{
			{"tls_cert", cl.TLSCert},
			{"tls_key", cl.TLSKey},
			{"tls_server_cert", cl.TLSServerCert},
		} {
			if p.path == "" || strings.Contains(p.path, "-----BEGIN ") {
				continue
			}
			info, err := os.Stat(p.path)
			if err != nil {
				return fmt.Errorf("config: cluster %q %s: %w", cl.Name, p.label, err)
			}
			if info.IsDir() {
				return fmt.Errorf("config: cluster %q %s is a directory", cl.Name, p.label)
			}
		}
	}

	return nil
}

// ClusterByName returns a cluster by name.
func (c *Config) ClusterByName(name string) (Cluster, bool) {
	for i := range c.Clusters {
		if c.Clusters[i].Name == name {
			return c.Clusters[i], true
		}
	}

	return Cluster{}, false
}

func (n NodeAccess) validate() error {
	user := strings.TrimSpace(n.SSHUser)
	key := strings.TrimSpace(n.SSHKey)

	if user == "" && key == "" {
		return nil
	}
	if user == "" {
		return errors.New("config: node_access.ssh_user is required with node_access.ssh_key")
	}
	if key == "" {
		return errors.New("config: node_access.ssh_key is required with node_access.ssh_user")
	}
	if n.SSHPort < 1 || n.SSHPort > 65535 {
		return fmt.Errorf("config: node_access.ssh_port %d is invalid", n.SSHPort)
	}
	if strings.TrimSpace(n.SSHKnownHosts) == "" && !n.SSHInsecureIgnoreHostKey {
		return errors.New("config: node_access needs ssh_known_hosts or ssh_insecure_ignore_host_key=true")
	}

	return nil
}
