// Package config loads and validates the incus-fleet configuration.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var clusterNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// Config is the top-level application configuration.
type Config struct {
	Server      Server        `yaml:"server"`
	Database    Database      `yaml:"database"`
	NodeAccess  NodeAccess    `yaml:"node_access"`
	Agent       AgentConfig   `yaml:"agent"`
	Metrics     MetricsConfig `yaml:"metrics"`
	HA          HAConfig      `yaml:"ha"`
	Mesh        MeshConfig    `yaml:"mesh"`
	LocalMember string        `yaml:"local_member"`
	Clusters    []Cluster     `yaml:"clusters"`
}

// MeshConfig configures the Raft center mesh. A healthy deployment has an odd
// number of voters: three centers, or two centers plus one witness.
type MeshConfig struct {
	Enabled        bool       `yaml:"enabled"`
	NodeID         string     `yaml:"node_id"`
	Address        string     `yaml:"address"`
	ControlAddress string     `yaml:"control_address"`
	DataDir        string     `yaml:"data_dir"`
	Peers          []MeshPeer `yaml:"peers"`
	Witness        bool       `yaml:"witness"`
	ForceSingle    bool       `yaml:"force_single"`
	Secret         string     `yaml:"secret"`
}

// MeshPeer is one voting participant, including witnesses.
type MeshPeer struct {
	ID             string `yaml:"id"`
	Address        string `yaml:"address"`
	ControlAddress string `yaml:"control_address"`
}

// AgentConfig is reserved for the host agent gateway. It is disabled until
// the agent transport and certificate authority are wired in.
type AgentConfig struct {
	Enabled  bool               `yaml:"enabled"`
	Listen   string             `yaml:"listen"`
	TLSCert  string             `yaml:"tls_cert"`
	TLSKey   string             `yaml:"tls_key"`
	ClientCA string             `yaml:"client_ca"`
	CAKey    string             `yaml:"ca_key"`
	Install  AgentInstallConfig `yaml:"install"`
}

// AgentInstallConfig enables the one-command Linux installer served by the
// center. The binaries must match the target host architecture.
type AgentInstallConfig struct {
	Enabled        bool              `yaml:"enabled"`
	GatewayURL     string            `yaml:"gateway_url"`
	AssetURL       string            `yaml:"asset_url"`
	AgentBinary    string            `yaml:"agent_binary"`
	HelperBinary   string            `yaml:"helper_binary"`
	AgentBinaries  map[string]string `yaml:"agent_binaries"`
	HelperBinaries map[string]string `yaml:"helper_binaries"`
}

// HAConfig controls the background host-task dispatcher. Multiple center
// instances can share one PostgreSQL database.
type HAConfig struct {
	Enabled  bool   `yaml:"enabled"`
	WorkerID string `yaml:"worker_id"`
	Lease    string `yaml:"lease"`
	Interval string `yaml:"interval"`
}

// MetricsConfig controls the center-side one-hour telemetry buffer.
type MetricsConfig struct {
	Enabled     bool                `yaml:"enabled"`
	Retention   string              `yaml:"retention"`
	Interval    string              `yaml:"interval"`
	RemoteWrite RemoteWriteConfig   `yaml:"remote_write"`
	Query       MetricsQueryConfig  `yaml:"query"`
	HTTPSD      MetricsHTTPSDConfig `yaml:"http_sd"`
}

// RemoteWriteConfig configures Prometheus/VictoriaMetrics remote write.
type RemoteWriteConfig struct {
	Enabled         bool   `yaml:"enabled"`
	URL             string `yaml:"url"`
	BearerToken     string `yaml:"bearer_token"`
	BearerTokenFile string `yaml:"bearer_token_file"`
	Timeout         string `yaml:"timeout"`
	QueueSize       int    `yaml:"queue_size"`
	BatchSize       int    `yaml:"batch_size"`
	SpoolDir        string `yaml:"spool_dir"`
	SpoolPartition  string `yaml:"spool_partition"`
	MaxBytes        int64  `yaml:"max_bytes"`
}

// MetricsQueryConfig configures the PromQL query proxy.
type MetricsQueryConfig struct {
	Enabled         bool   `yaml:"enabled"`
	URL             string `yaml:"url"`
	Type            string `yaml:"type"`
	BearerToken     string `yaml:"bearer_token"`
	BearerTokenFile string `yaml:"bearer_token_file"`
	Timeout         string `yaml:"timeout"`
}

// MetricsHTTPSDConfig configures Prometheus HTTP service discovery.
type MetricsHTTPSDConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Token     string `yaml:"token"`
	PublicURL string `yaml:"public_url"`
	Mode      string `yaml:"mode"`
}

// NodeAccess configures the SSH identity used to manage network settings on
// cluster members other than the host incus-fleet runs on. The account must
// have passwordless sudo; deploy/nodesudoers provides the minimal whitelist.
type NodeAccess struct {
	Mode                     string `yaml:"mode"`
	SSHUser                  string `yaml:"ssh_user"`
	SSHKey                   string `yaml:"ssh_key"`
	SSHPort                  int    `yaml:"ssh_port"`
	SSHKnownHosts            string `yaml:"ssh_known_hosts"`
	SSHInsecureIgnoreHostKey bool   `yaml:"ssh_insecure_ignore_host_key"`
	SSHWriteFallback         *bool  `yaml:"ssh_write_fallback"`
}

// SSHWriteFallbackEnabled reports whether mutating host operations may fall
// back to SSH when no agent is available. It defaults to true for backward
// compatibility with deployments that predate the agent.
func (n NodeAccess) SSHWriteFallbackEnabled() bool {
	return n.SSHWriteFallback == nil || *n.SSHWriteFallback
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

// IsShared reports whether the driver stores state in a backend that more than
// one fleet instance can read at the same time. Only a single-node SQLite file
// is instance-local; rqlite and PostgreSQL are shared, so sessions and other
// state must live in the database rather than in process memory.
func (d Database) IsShared() bool {
	return d.Driver == "rqlite" || d.Driver == "postgres"
}

// Cluster describes one Incus cluster endpoint.
type Cluster struct {
	Name          string   `yaml:"name"`
	DisplayName   string   `yaml:"display_name"`
	Endpoint      string   `yaml:"endpoint"`
	Endpoints     []string `yaml:"endpoints"`
	Project       string   `yaml:"project"`
	TLSCert       string   `yaml:"tls_cert"`
	TLSKey        string   `yaml:"tls_key"`
	TLSServerCert string   `yaml:"tls_server_cert"`
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
	if v := os.Getenv("INCUS_FLEET_NODE_ACCESS_MODE"); v != "" {
		c.NodeAccess.Mode = v
	}
	if v := os.Getenv("INCUS_FLEET_NODE_ACCESS_SSH_WRITE_FALLBACK"); v != "" {
		enabled := v == "1" || strings.EqualFold(v, "true")
		c.NodeAccess.SSHWriteFallback = &enabled
	}
	if v := os.Getenv("INCUS_FLEET_AGENT_ENABLED"); v != "" {
		c.Agent.Enabled = v == "1" || strings.EqualFold(v, "true")
	}
	if v := os.Getenv("INCUS_FLEET_AGENT_LISTEN"); v != "" {
		c.Agent.Listen = v
	}
	if v := os.Getenv("INCUS_FLEET_METRICS_ENABLED"); v != "" {
		c.Metrics.Enabled = v == "1" || strings.EqualFold(v, "true")
	}
	if v := os.Getenv("INCUS_FLEET_METRICS_RETENTION"); v != "" {
		c.Metrics.Retention = v
	}
	if v := os.Getenv("INCUS_FLEET_METRICS_INTERVAL"); v != "" {
		c.Metrics.Interval = v
	}
	if v := os.Getenv("INCUS_FLEET_METRICS_REMOTE_WRITE_URL"); v != "" {
		c.Metrics.RemoteWrite.URL = v
	}
	if v := os.Getenv("INCUS_FLEET_METRICS_REMOTE_WRITE_BEARER_TOKEN"); v != "" {
		c.Metrics.RemoteWrite.BearerToken = v
	}
	if v := os.Getenv("INCUS_FLEET_METRICS_REMOTE_WRITE_SPOOL_DIR"); v != "" {
		c.Metrics.RemoteWrite.SpoolDir = v
	}
	if v := os.Getenv("INCUS_FLEET_METRICS_REMOTE_WRITE_SPOOL_PARTITION"); v != "" {
		c.Metrics.RemoteWrite.SpoolPartition = v
	}
	if v := os.Getenv("INCUS_FLEET_MESH_SECRET"); v != "" {
		c.Mesh.Secret = v
	}
	if v := os.Getenv("INCUS_FLEET_METRICS_QUERY_URL"); v != "" {
		c.Metrics.Query.URL = v
	}
	if v := os.Getenv("INCUS_FLEET_METRICS_QUERY_BEARER_TOKEN"); v != "" {
		c.Metrics.Query.BearerToken = v
	}
	if v := os.Getenv("INCUS_FLEET_METRICS_HTTP_SD_TOKEN"); v != "" {
		c.Metrics.HTTPSD.Token = v
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
	if strings.TrimSpace(c.NodeAccess.Mode) == "" {
		c.NodeAccess.Mode = "auto"
	}
	if strings.TrimSpace(c.Agent.Listen) == "" {
		c.Agent.Listen = "0.0.0.0:8444"
	}
	c.Agent.Install.GatewayURL = strings.TrimRight(strings.TrimSpace(c.Agent.Install.GatewayURL), "/")
	c.Agent.Install.AssetURL = strings.TrimRight(strings.TrimSpace(c.Agent.Install.AssetURL), "/")
	if strings.TrimSpace(c.Metrics.Retention) == "" {
		c.Metrics.Retention = "1h"
	}
	if strings.TrimSpace(c.Metrics.Interval) == "" {
		c.Metrics.Interval = "10s"
	}
	if strings.TrimSpace(c.Metrics.RemoteWrite.Timeout) == "" {
		c.Metrics.RemoteWrite.Timeout = "10s"
	}
	if c.Metrics.RemoteWrite.MaxBytes <= 0 {
		c.Metrics.RemoteWrite.MaxBytes = 64 << 20
	}
	if strings.TrimSpace(c.Metrics.RemoteWrite.SpoolDir) == "" {
		switch {
		case c.Database.Driver == "sqlite" && c.Database.DSN != "" &&
			c.Database.DSN != ":memory:" && !strings.Contains(c.Database.DSN, "mode=memory"):
			c.Metrics.RemoteWrite.SpoolDir = filepath.Join(filepath.Dir(c.Database.DSN), "remote-write")
		default:
			c.Metrics.RemoteWrite.SpoolDir = filepath.Join(os.TempDir(), "incus-fleet-remote-write")
		}
	}
	if c.Metrics.RemoteWrite.QueueSize <= 0 {
		c.Metrics.RemoteWrite.QueueSize = 10000
	}
	if c.Metrics.RemoteWrite.BatchSize <= 0 {
		c.Metrics.RemoteWrite.BatchSize = 500
	}
	if strings.TrimSpace(c.Metrics.Query.Type) == "" {
		c.Metrics.Query.Type = "prometheus"
	}
	if strings.TrimSpace(c.Metrics.Query.Timeout) == "" {
		c.Metrics.Query.Timeout = "15s"
	}
	if strings.TrimSpace(c.Metrics.HTTPSD.Mode) == "" {
		c.Metrics.HTTPSD.Mode = "proxy"
	}
	if strings.TrimSpace(c.HA.Lease) == "" {
		c.HA.Lease = "30s"
	}
	if strings.TrimSpace(c.HA.Interval) == "" {
		c.HA.Interval = "2s"
	}

	for i := range c.Clusters {
		if strings.TrimSpace(c.Clusters[i].Endpoint) == "" && len(c.Clusters[i].Endpoints) > 0 {
			c.Clusters[i].Endpoint = c.Clusters[i].Endpoints[0]
		}
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
	case "sqlite", "rqlite", "postgres":
	default:
		return fmt.Errorf("config: unsupported database.driver %q", c.Database.Driver)
	}
	if strings.TrimSpace(c.Database.DSN) == "" {
		return errors.New("config: database.dsn is required")
	}

	if err := c.Agent.validate(); err != nil {
		return err
	}
	if err := c.Metrics.validate(); err != nil {
		return err
	}
	if err := c.HA.validate(); err != nil {
		return err
	}
	if err := c.Mesh.validate(); err != nil {
		return err
	}
	if c.Mesh.Enabled && !c.Mesh.Witness && !c.Agent.Enabled {
		return fmt.Errorf("config: mesh requires agent.enabled on non-witness nodes")
	}
	if err := c.Agent.validate(); err != nil {
		return err
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

		for _, endpoint := range ClusterEndpoints(*cl) {
			u, err := url.Parse(endpoint)
			if err != nil || u.Scheme != "https" || u.Host == "" {
				return fmt.Errorf("config: cluster %q endpoint must be a valid https URL", cl.Name)
			}
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

	data, err := os.ReadFile(value) //nolint:gosec // operator-supplied certificate path
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

	for _, p := range []struct {
		label string
		path  string
	}{
		{"metrics.remote_write.bearer_token_file", c.Metrics.RemoteWrite.BearerTokenFile},
		{"metrics.query.bearer_token_file", c.Metrics.Query.BearerTokenFile},
	} {
		if p.path == "" {
			continue
		}
		if info, err := os.Stat(p.path); err != nil {
			return fmt.Errorf("config: %s: %w", p.label, err)
		} else if info.IsDir() {
			return fmt.Errorf("config: %s is a directory", p.label)
		}
	}

	if c.Agent.Enabled {
		for _, p := range []struct {
			label string
			path  string
		}{
			{"agent.tls_cert", c.Agent.TLSCert},
			{"agent.tls_key", c.Agent.TLSKey},
			{"agent.client_ca", c.Agent.ClientCA},
			{"agent.ca_key", c.Agent.CAKey},
		} {
			if p.path == "" || strings.Contains(p.path, "-----BEGIN ") {
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

// ClusterEndpoints returns all configured endpoints in dial order.
func ClusterEndpoints(c Cluster) []string {
	if len(c.Endpoints) > 0 {
		return append([]string(nil), c.Endpoints...)
	}
	if strings.TrimSpace(c.Endpoint) == "" {
		return nil
	}
	return []string{c.Endpoint}
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

func (a AgentConfig) validate() error {
	listen := strings.TrimSpace(a.Listen)
	if listen == "" {
		return nil
	}
	if _, _, err := net.SplitHostPort(listen); err != nil {
		return fmt.Errorf("config: agent.listen %q is invalid", a.Listen)
	}
	if !a.Enabled {
		if a.Install.Enabled {
			return fmt.Errorf("config: agent.install requires agent.enabled")
		}
		return nil
	}
	for label, value := range map[string]string{
		"agent.tls_cert":  a.TLSCert,
		"agent.tls_key":   a.TLSKey,
		"agent.client_ca": a.ClientCA,
		"agent.ca_key":    a.CAKey,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("config: %s is required when agent.enabled is true", label)
		}
	}
	if !a.Install.Enabled {
		return nil
	}
	if strings.TrimSpace(a.Install.GatewayURL) == "" {
		return fmt.Errorf("config: agent.install.gateway_url is required")
	}
	gateway, err := url.Parse(a.Install.GatewayURL)
	if err != nil || gateway.Scheme != "https" || gateway.Host == "" {
		return fmt.Errorf("config: agent.install.gateway_url must be an https URL")
	}
	if a.Install.AssetURL != "" {
		asset, err := url.Parse(a.Install.AssetURL)
		if err != nil || (asset.Scheme != "https" && asset.Scheme != "http") || asset.Host == "" {
			return fmt.Errorf("config: agent.install.asset_url must be an http(s) URL")
		}
	}
	if len(a.Install.AgentBinaries) == 0 && len(a.Install.HelperBinaries) == 0 {
		if strings.TrimSpace(a.Install.AgentBinary) == "" {
			return fmt.Errorf("config: agent.install.agent_binary is required")
		}
		if strings.TrimSpace(a.Install.HelperBinary) == "" {
			return fmt.Errorf("config: agent.install.helper_binary is required")
		}
	} else {
		if len(a.Install.AgentBinaries) != len(a.Install.HelperBinaries) {
			return fmt.Errorf("config: agent.install.agent_binaries and helper_binaries must list the same architectures")
		}
		for arch, path := range a.Install.AgentBinaries {
			if strings.TrimSpace(arch) == "" || strings.TrimSpace(path) == "" {
				return fmt.Errorf("config: agent.install.agent_binaries contains an empty entry")
			}
			if strings.TrimSpace(a.Install.HelperBinaries[arch]) == "" {
				return fmt.Errorf("config: agent.install.helper_binaries is missing %s", arch)
			}
		}
	}

	return nil
}

func (m MeshConfig) validate() error {
	if !m.Enabled {
		return nil
	}
	if strings.TrimSpace(m.NodeID) == "" || strings.TrimSpace(m.Address) == "" ||
		strings.TrimSpace(m.DataDir) == "" || strings.TrimSpace(m.Secret) == "" {
		return fmt.Errorf("config: mesh.node_id, address, data_dir and secret are required")
	}
	if _, _, err := net.SplitHostPort(strings.TrimSpace(m.Address)); err != nil {
		return fmt.Errorf("config: mesh.address %q must be host:port", m.Address)
	}
	if _, _, err := net.SplitHostPort(strings.TrimSpace(m.ControlAddress)); err != nil {
		return fmt.Errorf("config: mesh.control_address %q must be host:port", m.ControlAddress)
	}
	if len(m.Peers) == 0 {
		return fmt.Errorf("config: mesh.peers is required")
	}
	if len(m.Peers)%2 == 0 {
		return fmt.Errorf("config: mesh.peers must contain an odd number of voters (use a witness)")
	}
	seen := make(map[string]bool, len(m.Peers))
	for _, peer := range m.Peers {
		if strings.TrimSpace(peer.ID) == "" {
			return fmt.Errorf("config: mesh peer id is required")
		}
		if seen[peer.ID] {
			return fmt.Errorf("config: duplicate mesh peer %q", peer.ID)
		}
		seen[peer.ID] = true
		if _, _, err := net.SplitHostPort(strings.TrimSpace(peer.Address)); err != nil {
			return fmt.Errorf("config: mesh peer %q address must be host:port", peer.ID)
		}
		if _, _, err := net.SplitHostPort(strings.TrimSpace(peer.ControlAddress)); err != nil {
			return fmt.Errorf("config: mesh peer %q control_address must be host:port", peer.ID)
		}
	}
	if !seen[m.NodeID] {
		return fmt.Errorf("config: mesh.node_id must appear in mesh.peers")
	}
	return nil
}

func (h HAConfig) validate() error {
	if strings.TrimSpace(h.Lease) != "" {
		lease, err := time.ParseDuration(strings.TrimSpace(h.Lease))
		if err != nil || lease < 5*time.Second || lease > time.Hour {
			return fmt.Errorf("config: ha.lease %q must be a duration between 5s and 1h", h.Lease)
		}
	}
	if strings.TrimSpace(h.Interval) != "" {
		interval, err := time.ParseDuration(strings.TrimSpace(h.Interval))
		if err != nil || interval < 100*time.Millisecond || interval > time.Minute {
			return fmt.Errorf("config: ha.interval %q must be a duration between 100ms and 1m", h.Interval)
		}
	}
	if h.Enabled && strings.ContainsAny(h.WorkerID, " \t\n") {
		return fmt.Errorf("config: ha.worker_id must not contain whitespace")
	}

	return nil
}

func (m MetricsConfig) validate() error {
	if strings.TrimSpace(m.Retention) != "" {
		retention, err := time.ParseDuration(strings.TrimSpace(m.Retention))
		if err != nil || retention <= 0 || retention > time.Hour {
			return fmt.Errorf("config: metrics.retention %q must be a duration between 1s and 1h", m.Retention)
		}
	}
	if strings.TrimSpace(m.Interval) != "" {
		interval, err := time.ParseDuration(strings.TrimSpace(m.Interval))
		if err != nil || interval < time.Second || interval > time.Minute {
			return fmt.Errorf("config: metrics.interval %q must be a duration between 1s and 1m", m.Interval)
		}
	}
	if m.RemoteWrite.Enabled {
		if _, err := url.ParseRequestURI(m.RemoteWrite.URL); err != nil {
			return fmt.Errorf("config: metrics.remote_write.url is invalid")
		}
		if _, err := time.ParseDuration(m.RemoteWrite.Timeout); err != nil {
			return fmt.Errorf("config: metrics.remote_write.timeout is invalid")
		}
		if m.RemoteWrite.MaxBytes <= 0 {
			return fmt.Errorf("config: metrics.remote_write.max_bytes must be positive")
		}
		if strings.TrimSpace(m.RemoteWrite.SpoolDir) == "" {
			return fmt.Errorf("config: metrics.remote_write.spool_dir is required")
		}
	}
	if m.Query.Enabled {
		if _, err := url.ParseRequestURI(m.Query.URL); err != nil {
			return fmt.Errorf("config: metrics.query.url is invalid")
		}
		if m.Query.Type != "prometheus" && m.Query.Type != "victoriametrics" {
			return fmt.Errorf("config: metrics.query.type must be prometheus or victoriametrics")
		}
		if _, err := time.ParseDuration(m.Query.Timeout); err != nil {
			return fmt.Errorf("config: metrics.query.timeout is invalid")
		}
	}
	if m.HTTPSD.Enabled {
		if strings.TrimSpace(m.HTTPSD.Token) == "" {
			return fmt.Errorf("config: metrics.http_sd.token is required")
		}
		if m.HTTPSD.Mode != "proxy" && m.HTTPSD.Mode != "direct" {
			return fmt.Errorf("config: metrics.http_sd.mode must be proxy or direct")
		}
		if m.HTTPSD.PublicURL != "" {
			if _, err := url.ParseRequestURI(m.HTTPSD.PublicURL); err != nil {
				return fmt.Errorf("config: metrics.http_sd.public_url is invalid")
			}
		}
	}

	return nil
}

func (n NodeAccess) validate() error {
	switch strings.ToLower(strings.TrimSpace(n.Mode)) {
	case "", "auto", "agent", "ssh", "local":
	default:
		return fmt.Errorf("config: node_access.mode %q is invalid (want auto, agent, ssh, or local)", n.Mode)
	}

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
