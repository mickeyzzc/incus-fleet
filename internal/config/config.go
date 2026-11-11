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
	Server   Server    `yaml:"server"`
	Database Database  `yaml:"database"`
	Clusters []Cluster `yaml:"clusters"`
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

// ValidateFiles verifies that certificate files referenced by the config exist.
func (c *Config) ValidateFiles() error {
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
			if p.path == "" {
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
