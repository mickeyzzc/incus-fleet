// Package clusters owns connections to the managed Incus clusters.
package clusters

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
	"github.com/pkg/sftp"

	"github.com/mickeyzzc/incus-fleet/internal/apitypes"

	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/version"
)

// ErrUnknownCluster is returned when a cluster name is not configured.
var ErrUnknownCluster = errors.New("clusters: unknown cluster")

// ErrUnavailable is returned when a configured cluster has no healthy client.
var ErrUnavailable = errors.New("clusters: cluster unavailable")

// FileEntry describes one file or directory inside an instance.
type FileEntry = apitypes.FileEntry

// InstanceServer is the narrow slice of the Incus client used by fleet
// services. Tests provide a fake implementation of this interface.
type InstanceServer interface {
	GetServer() (*api.Server, string, error)
	GetCluster() (*api.Cluster, string, error)
	GetClusterMembers() ([]api.ClusterMember, error)
	GetInstancesFull(instanceType api.InstanceType) ([]api.InstanceFull, error)
	GetInstance(name string) (*api.Instance, string, error)
	GetInstanceState(name string) (*api.InstanceState, string, error)
	UpdateInstanceState(name string, state api.InstanceStatePut, etag string) (incus.Operation, error)
	CreateInstance(instance api.InstancesPost) (incus.Operation, error)
	UpdateInstance(name string, instance api.InstancePut, etag string) (incus.Operation, error)
	CopyInstance(source incus.InstanceServer, instance api.Instance, args *incus.InstanceCopyArgs) (incus.RemoteOperation, error)
	DeleteInstance(name string) (incus.Operation, error)
	ConsoleInstanceDynamic(instanceName string, console api.InstanceConsolePost, args *incus.InstanceConsoleArgs) (incus.Operation, func(io.ReadWriteCloser) error, error)
	ExecInstance(instanceName string, exec api.InstanceExecPost, args *incus.InstanceExecArgs) (incus.Operation, error)
	GetInstanceFile(instanceName string, path string) (io.ReadCloser, *incus.InstanceFileResponse, error)
	CreateInstanceFile(instanceName string, path string, args incus.InstanceFileArgs) error
	DeleteInstanceFile(instanceName string, path string) error
	ListInstanceFiles(instanceName string, dir string) ([]apitypes.FileEntry, error)
	MkdirInstanceDir(instanceName string, dir string) error
	GetImages() ([]api.Image, error)
	GetImage(fingerprint string) (*api.Image, string, error)
	CreateImage(image api.ImagesPost, args *incus.ImageCreateArgs) (incus.Operation, error)
	CopyImage(source incus.ImageServer, image api.Image, args *incus.ImageCopyArgs) (incus.RemoteOperation, error)
	DeleteImage(fingerprint string) (incus.Operation, error)
	GetInstanceSnapshots(instanceName string) ([]api.InstanceSnapshot, error)
	CreateInstanceSnapshot(instanceName string, snapshot api.InstanceSnapshotsPost) (incus.Operation, error)
	DeleteInstanceSnapshot(instanceName string, name string) (incus.Operation, error)
	GetStoragePoolVolumes(pool string) ([]api.StorageVolume, error)
	CreateStoragePoolVolume(pool string, volume api.StorageVolumesPost) error
	DeleteStoragePoolVolume(pool string, volType string, name string) error
	CreateStoragePoolVolumeFromISO(pool string, args incus.StorageVolumeBackupArgs) (incus.Operation, error)
	RebuildInstance(instanceName string, req api.InstanceRebuildPost) (incus.Operation, error)
	RefreshImage(fingerprint string) (incus.Operation, error)
	GetStoragePoolNames() ([]string, error)
	GetStoragePools() ([]api.StoragePool, error)
	GetStoragePool(name string) (*api.StoragePool, string, error)
	CreateStoragePool(pool api.StoragePoolsPost) error
	UpdateStoragePool(name string, pool api.StoragePoolPut, etag string) error
	DeleteStoragePool(name string) error
	GetNetworkNames() ([]string, error)
	GetNetworks() ([]api.Network, error)
	GetNetwork(name string) (*api.Network, string, error)
	CreateNetwork(network api.NetworksPost) error
	UpdateNetwork(name string, network api.NetworkPut, etag string) error
	DeleteNetwork(name string) error
	GetStoragePoolResources(name string) (*api.ResourcesStoragePool, error)
	GetServerResources() (*api.Resources, error)
	GetProfiles() ([]api.Profile, error)
	GetProfile(name string) (*api.Profile, string, error)
	CreateProfile(profile api.ProfilesPost) error
	UpdateProfile(name string, profile api.ProfilePut, etag string) error
	DeleteProfile(name string) error
	GetClusterMember(name string) (*api.ClusterMember, string, error)
	UpdateClusterMember(name string, member api.ClusterMemberPut, etag string) error
	IsClustered() bool
}

// DialFunc connects to one configured Incus endpoint. The returned client may
// also implement the full Incus client interface, in which case it is retained
// as a migration source.
type DialFunc func(ctx context.Context, cfg config.Cluster) (InstanceServer, error)

// Connection is one configured cluster and its client.
type Connection struct {
	cfg    config.Cluster
	client InstanceServer
	raw    incus.InstanceServer
	err    error

	mu sync.RWMutex
}

// Name returns the configured short name.
func (c *Connection) Name() string { return c.cfg.Name }

// DisplayName returns the human-facing name.
func (c *Connection) DisplayName() string { return c.cfg.DisplayName }

// Endpoint returns the API endpoint.
func (c *Connection) Endpoint() string { return c.cfg.Endpoint }

// Project returns the Incus project used for this connection.
func (c *Connection) Project() string { return c.cfg.Project }

// Client returns the project-scoped client or the last connection error.
func (c *Connection) Client() (InstanceServer, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		if c.err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrUnavailable, c.cfg.Name, c.err)
		}

		return nil, fmt.Errorf("%w: %s", ErrUnavailable, c.cfg.Name)
	}

	return c.client, nil
}

// RawClient returns the full client used as a migration source.
// It may be nil when the connection is down.
func (c *Connection) RawClient() incus.InstanceServer {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.raw
}

// ConnectError returns the last dial error, if any.
func (c *Connection) ConnectError() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.err
}

// sftpExtensions is implemented by clients that already carry the fleet
// directory helpers (the test fake); real SDK clients need the wrapper.
type sftpExtensions interface {
	ListInstanceFiles(instanceName string, dir string) ([]apitypes.FileEntry, error)
	MkdirInstanceDir(instanceName string, dir string) error
}

// reconnect replaces the client, disconnecting the previous one.
func (c *Connection) reconnect(_ context.Context, srv InstanceServer) {
	full := fullClient(srv)
	client := srv
	if full != nil {
		if c.cfg.Project != "" {
			full = full.UseProject(c.cfg.Project)
		}
		// Keep the narrow surface (including the SFTP helpers) while
		// preserving the configured project scope. Clients that already
		// implement the helpers are used as-is.
		if _, needsWrap := srv.(sftpExtensions); !needsWrap {
			client = &augmentedClient{InstanceServer: full}
		}
	}

	c.mu.Lock()
	old := c.client
	c.client = client
	c.raw = full
	c.err = nil
	c.mu.Unlock()

	if old != nil {
		if closer, ok := old.(interface{ Disconnect() }); ok {
			closer.Disconnect()
		}
	}
}

func fullClient(srv InstanceServer) incus.InstanceServer {
	if wrapped, ok := srv.(*augmentedClient); ok {
		return wrapped.InstanceServer
	}

	if full, ok := srv.(incus.InstanceServer); ok {
		return full
	}

	return nil
}

func (c *Connection) setError(err error) {
	c.mu.Lock()
	c.client = nil
	c.raw = nil
	c.err = err
	c.mu.Unlock()
}

func (c *Connection) disconnect() {
	c.mu.Lock()
	client := c.client
	c.client = nil
	c.raw = nil
	c.mu.Unlock()

	if client != nil {
		if closer, ok := client.(interface{ Disconnect() }); ok {
			closer.Disconnect()
		}
	}
}

// Manager owns all configured cluster connections.
type Manager struct {
	dial   DialFunc
	logger *slog.Logger
	mu     sync.RWMutex
	order  []string
	conns  map[string]*Connection
}

// New connects to every configured cluster. Connection failures are recorded
// per cluster so the application can still start and report the problem.
func New(ctx context.Context, cfgs []config.Cluster, logger *slog.Logger) (*Manager, error) {
	return NewWithDialer(ctx, cfgs, DefaultDial, logger)
}

// NewWithDialer builds a manager with an injectable dialer for tests.
func NewWithDialer(ctx context.Context, cfgs []config.Cluster, dial DialFunc, logger *slog.Logger) (*Manager, error) {
	if dial == nil {
		return nil, errors.New("clusters: dialer is required")
	}
	if logger == nil {
		logger = slog.Default()
	}

	m := &Manager{
		dial:   dial,
		logger: logger,
		order:  make([]string, 0, len(cfgs)),
		conns:  make(map[string]*Connection, len(cfgs)),
	}

	for _, cfg := range cfgs {
		if _, exists := m.conns[cfg.Name]; exists {
			return nil, fmt.Errorf("clusters: duplicate cluster %q", cfg.Name)
		}

		conn := &Connection{cfg: cfg}
		srv, err := dial(ctx, cfg)
		if err != nil {
			conn.err = err
			logger.Warn("cluster connection failed", "cluster", cfg.Name, "error", err)
		} else {
			conn.reconnect(ctx, srv)
		}

		m.order = append(m.order, cfg.Name)
		m.conns[cfg.Name] = conn
	}

	return m, nil
}

// DefaultDial connects to an Incus HTTPS endpoint using pinned client
// certificates. Certificate arguments are PEM contents, not paths.
func DefaultDial(_ context.Context, cfg config.Cluster) (InstanceServer, error) {
	certPEM, err := os.ReadFile(cfg.TLSCert)
	if err != nil {
		return nil, fmt.Errorf("read client certificate: %w", err)
	}
	keyPEM, err := os.ReadFile(cfg.TLSKey)
	if err != nil {
		return nil, fmt.Errorf("read client key: %w", err)
	}

	var serverCert string
	if cfg.TLSServerCert != "" {
		serverPEM, err := os.ReadFile(cfg.TLSServerCert)
		if err != nil {
			return nil, fmt.Errorf("read server certificate: %w", err)
		}
		serverCert = string(serverPEM)
	}

	srv, err := incus.ConnectIncus(cfg.Endpoint, &incus.ConnectionArgs{
		TLSClientCert: string(certPEM),
		TLSClientKey:  string(keyPEM),
		TLSServerCert: serverCert,
		UserAgent:     "incus-fleet/" + version.String(),
		SkipGetEvents: true,
		AuthType:      "tls",
	})
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	return &augmentedClient{InstanceServer: srv}, nil
}

// augmentedClient layers SFTP-backed directory helpers onto a raw SDK client.
type augmentedClient struct {
	incus.InstanceServer

	// sftpFor is injectable so tests can supply an in-process client.
	sftpFor func(instanceName string) (*sftp.Client, error)
}

// sftpClient opens the SFTP channel for one instance.
func (a *augmentedClient) sftpClient(instanceName string) (*sftp.Client, error) {
	if a.sftpFor != nil {
		return a.sftpFor(instanceName)
	}

	return a.GetInstanceFileSFTP(instanceName)
}

// ListInstanceFiles lists one directory inside an instance over SFTP.
func (a *augmentedClient) ListInstanceFiles(instanceName string, dir string) ([]apitypes.FileEntry, error) {
	client, err := a.sftpClient(instanceName)
	if err != nil {
		return nil, fmt.Errorf("open sftp %s: %w", instanceName, err)
	}
	defer client.Close() //nolint:errcheck // best-effort close

	entries, err := client.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}

	return fileEntries(entries), nil
}

// fileEntries converts SFTP directory listings into fleet entries, with
// directories first and names sorted within each group.
func fileEntries(entries []os.FileInfo) []apitypes.FileEntry {
	out := make([]apitypes.FileEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, FileEntry{
			Name:    entry.Name(),
			Size:    entry.Size(),
			Mode:    uint32(entry.Mode().Perm()),
			IsDir:   entry.IsDir(),
			ModTime: entry.ModTime(),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}

		return out[i].Name < out[j].Name
	})

	return out
}

// MkdirInstanceDir creates a directory inside an instance over SFTP.
func (a *augmentedClient) MkdirInstanceDir(instanceName string, dir string) error {
	client, err := a.sftpClient(instanceName)
	if err != nil {
		return fmt.Errorf("open sftp %s: %w", instanceName, err)
	}
	defer client.Close() //nolint:errcheck // best-effort close

	if err := client.MkdirAll(dir); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	return nil
}

// List returns a fresh summary for every configured cluster, in config order.
func (m *Manager) List() []Summary {
	m.mu.RLock()
	conns := make([]*Connection, 0, len(m.order))
	for _, name := range m.order {
		conns = append(conns, m.conns[name])
	}
	m.mu.RUnlock()

	summaries := make([]Summary, len(conns))

	var wg sync.WaitGroup
	wg.Add(len(conns))
	for i, conn := range conns {
		go func(i int, conn *Connection) {
			defer wg.Done()
			summaries[i] = conn.Summary()
		}(i, conn)
	}
	wg.Wait()

	return summaries
}

// Get returns a fresh summary for one cluster.
func (m *Manager) Get(name string) (Summary, error) {
	conn, err := m.Connection(name)
	if err != nil {
		return Summary{}, err
	}

	return conn.Summary(), nil
}

// Names returns configured cluster names in config order.
func (m *Manager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return append([]string(nil), m.order...)
}

// Connection returns one configured connection.
func (m *Manager) Connection(name string) (*Connection, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, ok := m.conns[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownCluster, name)
	}

	return conn, nil
}

// Reconnect re-dials one cluster and returns its fresh summary.
func (m *Manager) Reconnect(ctx context.Context, name string) (Summary, error) {
	conn, err := m.Connection(name)
	if err != nil {
		return Summary{}, err
	}

	srv, err := m.dial(ctx, conn.cfg)
	if err != nil {
		conn.setError(err)
		m.logger.Warn("cluster reconnect failed", "cluster", name, "error", err)
		return conn.Summary(), fmt.Errorf("clusters: reconnect %s: %w", name, err)
	}

	conn.reconnect(ctx, srv)
	m.logger.Info("cluster reconnected", "cluster", name)

	return conn.Summary(), nil
}

// Close disconnects every cluster.
func (m *Manager) Close() {
	m.mu.Lock()
	conns := make([]*Connection, 0, len(m.conns))
	for _, conn := range m.conns {
		conns = append(conns, conn)
	}
	m.mu.Unlock()

	for _, conn := range conns {
		conn.disconnect()
	}
}

// Summary is the API representation of one cluster.
type Summary struct {
	Name          string       `json:"name"`
	DisplayName   string       `json:"display_name"`
	Endpoint      string       `json:"endpoint"`
	Online        bool         `json:"online"`
	Error         string       `json:"error,omitempty"`
	ServerName    string       `json:"server_name,omitempty"`
	ServerVersion string       `json:"server_version,omitempty"`
	Clustered     bool         `json:"clustered"`
	Architectures []string     `json:"architectures,omitempty"`
	Members       []Member     `json:"members"`
	Instances     Counts       `json:"instances"`
	Storage       StorageUsage `json:"storage"`
	Networks      []string     `json:"networks"`
	CPU           ResourcesCPU `json:"cpu"`
	Memory        ResourceMem  `json:"memory"`
}

// ResourcesCPU summarizes the processor capacity of the cluster member.
type ResourcesCPU struct {
	Total        uint64 `json:"total"`
	Architecture string `json:"architecture,omitempty"`
}

// ResourceMem summarizes memory usage on the cluster member in bytes.
type ResourceMem struct {
	Used  uint64 `json:"used"`
	Total uint64 `json:"total"`
}

// Member is one Incus cluster member.
type Member struct {
	Name          string   `json:"name"`
	URL           string   `json:"url,omitempty"`
	Status        string   `json:"status"`
	Message       string   `json:"message,omitempty"`
	Database      bool     `json:"database"`
	Roles         []string `json:"roles"`
	Architecture  string   `json:"architecture,omitempty"`
	Description   string   `json:"description,omitempty"`
	FailureDomain string   `json:"failure_domain,omitempty"`
	Groups        []string `json:"groups"`
}

// Counts summarizes instances by type and state.
type Counts struct {
	Total           int `json:"total"`
	Containers      int `json:"containers"`
	VirtualMachines int `json:"virtual_machines"`
	Running         int `json:"running"`
}

// StorageUsage summarizes the primary storage pool.
type StorageUsage struct {
	Pool  string `json:"pool,omitempty"`
	Used  uint64 `json:"used"`
	Total uint64 `json:"total"`
}

// Summary probes the cluster and builds a status snapshot.
func (c *Connection) Summary() Summary {
	summary := Summary{
		Name:        c.cfg.Name,
		DisplayName: c.cfg.DisplayName,
		Endpoint:    c.cfg.Endpoint,
		Members:     []Member{},
		Networks:    []string{},
	}

	client, err := c.Client()
	if err != nil {
		summary.Error = err.Error()
		return summary
	}

	server, _, err := client.GetServer()
	if err != nil {
		summary.Error = fmt.Sprintf("get server: %v", err)
		return summary
	}

	summary.Online = true
	summary.Clustered = server.Environment.ServerClustered
	summary.ServerName = server.Environment.ServerName
	summary.ServerVersion = server.Environment.ServerVersion
	summary.Architectures = append([]string(nil), server.Environment.Architectures...)

	if members, err := client.GetClusterMembers(); err == nil {
		for _, member := range members {
			summary.Members = append(summary.Members, Member{
				Name:          member.ServerName,
				URL:           member.URL,
				Status:        member.Status,
				Message:       member.Message,
				Database:      member.Database,
				Roles:         append([]string(nil), member.Roles...),
				Architecture:  member.Architecture,
				Description:   member.Description,
				FailureDomain: member.FailureDomain,
				Groups:        append([]string{}, member.Groups...),
			})
		}
	}

	if instances, err := client.GetInstancesFull(api.InstanceTypeAny); err == nil {
		for _, instance := range instances {
			summary.Instances.Total++
			switch api.InstanceType(instance.Type) {
			case api.InstanceTypeContainer:
				summary.Instances.Containers++
			case api.InstanceTypeVM:
				summary.Instances.VirtualMachines++
			}
			if instance.StatusCode == api.Running || instance.StatusCode == api.Frozen {
				summary.Instances.Running++
			}
		}
	}

	if pools, err := client.GetStoragePoolNames(); err == nil && len(pools) > 0 {
		sort.Strings(pools)
		summary.Storage.Pool = pools[0]
		if resources, err := client.GetStoragePoolResources(pools[0]); err == nil {
			summary.Storage.Used = resources.Space.Used
			summary.Storage.Total = resources.Space.Total
		}
	}

	if networks, err := client.GetNetworkNames(); err == nil {
		sort.Strings(networks)
		summary.Networks = networks
	}

	if resources, err := client.GetServerResources(); err == nil {
		summary.CPU = ResourcesCPU{
			Total:        resources.CPU.Total,
			Architecture: resources.CPU.Architecture,
		}
		summary.Memory = ResourceMem{
			Used:  resources.Memory.Used,
			Total: resources.Memory.Total,
		}
	}

	return summary
}

// Register connects to a new cluster at runtime and adds it to the manager.
// The caller persists the cluster configuration.
func (m *Manager) Register(ctx context.Context, cfg config.Cluster) (Summary, error) {
	if strings.TrimSpace(cfg.Name) == "" {
		return Summary{}, fmt.Errorf("clusters: name is required")
	}

	m.mu.Lock()
	if _, exists := m.conns[cfg.Name]; exists {
		m.mu.Unlock()
		return Summary{}, fmt.Errorf("clusters: duplicate cluster %q", cfg.Name)
	}
	m.mu.Unlock()

	conn := &Connection{cfg: cfg}
	srv, err := m.dial(ctx, cfg)
	if err != nil {
		conn.err = err
		m.mu.Lock()
		m.order = append(m.order, cfg.Name)
		m.conns[cfg.Name] = conn
		m.mu.Unlock()
		m.logger.Warn("cluster connection failed", "cluster", cfg.Name, "error", err)
		return conn.Summary(), fmt.Errorf("clusters: register %s: %w", cfg.Name, err)
	}

	conn.reconnect(ctx, srv)

	m.mu.Lock()
	m.order = append(m.order, cfg.Name)
	m.conns[cfg.Name] = conn
	m.mu.Unlock()

	m.logger.Info("cluster registered", "cluster", cfg.Name)
	return conn.Summary(), nil
}

// Unregister removes one cluster from the manager. The caller handles
// persistence.
func (m *Manager) Unregister(name string) error {
	m.mu.Lock()
	conn, exists := m.conns[name]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("clusters: %w: %s", ErrUnknownCluster, name)
	}

	order := make([]string, 0, len(m.order))
	for _, item := range m.order {
		if item != name {
			order = append(order, item)
		}
	}
	m.order = order
	delete(m.conns, name)
	m.mu.Unlock()

	conn.disconnect()
	m.logger.Info("cluster unregistered", "cluster", name)

	return nil
}
