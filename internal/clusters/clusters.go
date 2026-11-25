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
	"time"

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
	GetStoragePoolVolume(pool string, volType string, name string) (*api.StorageVolume, string, error)
	GetCluster() (*api.Cluster, string, error)
	GetClusterMembers() ([]api.ClusterMember, error)
	GetInstancesFull(instanceType api.InstanceType) ([]api.InstanceFull, error)
	GetInstance(name string) (*api.Instance, string, error)
	GetInstanceState(name string) (*api.InstanceState, string, error)
	UpdateInstanceState(name string, state api.InstanceStatePut, etag string) (incus.Operation, error)
	CreateInstance(instance api.InstancesPost) (incus.Operation, error)
	UpdateInstance(name string, instance api.InstancePut, etag string) (incus.Operation, error)
	RenameInstance(name string, instance api.InstancePost) (incus.Operation, error)
	UseTarget(name string) incus.InstanceServer
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
	// Volume backup export slice used to fetch raw ISO bytes for the
	// unattended-install pipeline.
	CreateStorageVolumeBackup(pool string, volName string, backup api.StorageVolumeBackupsPost) (incus.Operation, error)
	GetStorageVolumeBackupFile(pool string, volName string, name string, req *incus.BackupFileRequest) (*incus.BackupFileResponse, error)
	DeleteStorageVolumeBackup(pool string, volName string, name string) (incus.Operation, error)
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
	GetNetworkLeases(network string) ([]api.NetworkLease, error)
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
	CreateClusterMember(member api.ClusterMembersPost) (incus.Operation, error)
	UpdateClusterMember(name string, member api.ClusterMemberPut, etag string) error
	RenameClusterMember(name string, member api.ClusterMemberPost) error
	DeleteClusterMember(name string, force bool) error
	IsClustered() bool
}

// DialFunc connects to one configured Incus endpoint. The returned client may
// also implement the full Incus client interface, in which case it is retained
// as a migration source.
type DialFunc func(ctx context.Context, cfg config.Cluster) (InstanceServer, error)

// Connection is one configured cluster and its client.
type Connection struct {
	cfg           config.Cluster
	client        InstanceServer
	raw           incus.InstanceServer
	err           error
	endpoints     []string
	endpointIndex int
	displayName   string

	mu sync.RWMutex
}

// Name returns the configured short name.
func (c *Connection) Name() string { return c.cfg.Name }

// DisplayName returns the human-facing name. The operator override wins over
// the configured display name and can change without reconnecting.
func (c *Connection) DisplayName() string {
	c.mu.RLock()
	override := c.displayName
	c.mu.RUnlock()
	if override != "" {
		return override
	}

	return c.cfg.DisplayName
}

// SetDisplayName updates the operator-facing name without touching the Incus
// identifier or connection.
func (c *Connection) SetDisplayName(name string) {
	c.mu.Lock()
	c.displayName = strings.TrimSpace(name)
	c.mu.Unlock()
}

// Endpoint returns the configured primary API endpoint.
func (c *Connection) Endpoint() string { return c.cfg.Endpoint }

// ActiveEndpoint returns the endpoint that is currently in use.
func (c *Connection) ActiveEndpoint() string {
	endpoints := config.ClusterEndpoints(c.cfg)
	c.mu.RLock()
	index := c.endpointIndex
	c.mu.RUnlock()
	if len(endpoints) == 0 {
		return c.cfg.Endpoint
	}
	return endpoints[index%len(endpoints)]
}

// connect tries every configured endpoint in order until one dial succeeds.
func (c *Connection) connect(ctx context.Context, dial DialFunc) error {
	endpoints := config.ClusterEndpoints(c.cfg)
	if len(endpoints) == 0 {
		err := errors.New("no endpoints configured")
		c.setError(err)
		return err
	}

	var lastErr error
	for attempt := 0; attempt < len(endpoints); attempt++ {
		c.mu.RLock()
		index := (c.endpointIndex + attempt) % len(endpoints)
		c.mu.RUnlock()

		cfg := c.cfg
		cfg.Endpoint = endpoints[index]
		srv, err := dial(ctx, cfg)
		if srv != nil && err == nil {
			c.mu.Lock()
			c.endpointIndex = index
			c.mu.Unlock()
			c.reconnect(ctx, srv)
			return nil
		}
		if err == nil {
			err = errors.New("dial returned no client")
		}
		lastErr = fmt.Errorf("%s: %w", endpoints[index], err)
	}
	c.setError(lastErr)
	return lastErr
}

// Project returns the Incus project used for this connection.
func (c *Connection) Project() string { return c.cfg.Project }

// Config returns the configured cluster entry.
func (c *Connection) Config() config.Cluster { return c.cfg }

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
	dial          DialFunc
	logger        *slog.Logger
	mu            sync.RWMutex
	order         []string
	conns         map[string]*Connection
	watchCancel   context.CancelFunc
	watchInterval time.Duration
}

// New connects to every configured cluster. Connection failures are recorded
// per cluster so the application can still start and report the problem.
func New(ctx context.Context, cfgs []config.Cluster, logger *slog.Logger) (*Manager, error) {
	return NewWithDialer(ctx, cfgs, DefaultDial, logger)
}

// DialOption customizes the manager before its watch goroutine starts.
type DialOption func(*Manager)

// WithWatchInterval sets the cluster health-check cadence. It exists for
// tests: the watch goroutine reads the interval once at start, so the value
// must be in place before construction returns, never written afterwards.
func WithWatchInterval(d time.Duration) DialOption {
	return func(m *Manager) { m.watchInterval = d }
}

// NewWithDialer builds a manager with an injectable dialer for tests.
func NewWithDialer(ctx context.Context, cfgs []config.Cluster, dial DialFunc, logger *slog.Logger, opts ...DialOption) (*Manager, error) {
	if dial == nil {
		return nil, errors.New("clusters: dialer is required")
	}
	if logger == nil {
		logger = slog.Default()
	}

	m := &Manager{
		dial:          dial,
		logger:        logger,
		order:         make([]string, 0, len(cfgs)),
		conns:         make(map[string]*Connection, len(cfgs)),
		watchInterval: 10 * time.Second,
	}
	for _, opt := range opts {
		opt(m)
	}
	watchCtx, cancel := context.WithCancel(context.Background())
	m.watchCancel = cancel

	for _, cfg := range cfgs {
		if _, exists := m.conns[cfg.Name]; exists {
			cancel()
			return nil, fmt.Errorf("clusters: duplicate cluster %q", cfg.Name)
		}

		conn := &Connection{cfg: cfg, endpoints: config.ClusterEndpoints(cfg)}
		if err := conn.connect(ctx, dial); err != nil {
			logger.Warn("cluster connection failed", "cluster", cfg.Name, "error", err)
		}

		m.order = append(m.order, cfg.Name)
		m.conns[cfg.Name] = conn
	}

	// Start watching only once the connection map is fully populated: the
	// watcher reads the map under a read lock, and these writes are unsynchronized.
	go m.watch(watchCtx)

	return m, nil
}

// DefaultDial connects to an Incus HTTPS endpoint using pinned client
// certificates. Certificate arguments may be PEM contents or file paths.
func DefaultDial(_ context.Context, cfg config.Cluster) (InstanceServer, error) {
	args, err := connectionArgs(cfg)
	if err != nil {
		return nil, err
	}

	srv, err := incus.ConnectIncus(cfg.Endpoint, args)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	return &augmentedClient{InstanceServer: srv}, nil
}

// connectionArgs turns a cluster config into Incus connection arguments.
// Certificate fields may be inline PEM documents or paths on disk.
func connectionArgs(cfg config.Cluster) (*incus.ConnectionArgs, error) {
	certPEM, err := config.ReadPEMOrFile(cfg.TLSCert)
	if err != nil {
		return nil, fmt.Errorf("read client certificate: %w", err)
	}
	keyPEM, err := config.ReadPEMOrFile(cfg.TLSKey)
	if err != nil {
		return nil, fmt.Errorf("read client key: %w", err)
	}

	var serverCert string
	if cfg.TLSServerCert != "" {
		serverPEM, err := config.ReadPEMOrFile(cfg.TLSServerCert)
		if err != nil {
			return nil, fmt.Errorf("read server certificate: %w", err)
		}
		serverCert = string(serverPEM)
	}

	return &incus.ConnectionArgs{
		TLSClientCert: string(certPEM),
		TLSClientKey:  string(keyPEM),
		TLSServerCert: serverCert,
		UserAgent:     "incus-fleet/" + version.String(),
		SkipGetEvents: true,
		AuthType:      "tls",
	}, nil
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

// SetDisplayName changes the human-facing name of one cluster. The Incus
// identifier in routes and API calls is unchanged.
func (m *Manager) SetDisplayName(name, displayName string) error {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return fmt.Errorf("clusters: display name is required")
	}

	conn, err := m.Connection(name)
	if err != nil {
		return err
	}

	conn.SetDisplayName(displayName)

	return nil
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

	if err := conn.connect(ctx, m.dial); err != nil {
		m.logger.Warn("cluster reconnect failed", "cluster", name, "error", err)
		return conn.Summary(), fmt.Errorf("clusters: reconnect %s: %w", name, err)
	}
	m.logger.Info("cluster reconnected", "cluster", name, "endpoint", conn.ActiveEndpoint())

	return conn.Summary(), nil
}

// watch re-dials unavailable clusters using the next healthy endpoint.
func (m *Manager) watch(ctx context.Context) {
	interval := m.watchInterval
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.RLock()
			conns := make([]*Connection, 0, len(m.conns))
			for _, conn := range m.conns {
				conns = append(conns, conn)
			}
			m.mu.RUnlock()
			for _, conn := range conns {
				conn.mu.RLock()
				client := conn.client
				conn.mu.RUnlock()
				if client != nil {
					if _, _, err := client.GetServer(); err != nil {
						m.logger.Warn("cluster health check failed", "cluster", conn.Name(), "error", err)
						conn.setError(err)
						endpoints := config.ClusterEndpoints(conn.cfg)
						if len(endpoints) > 1 {
							conn.mu.Lock()
							conn.endpointIndex = (conn.endpointIndex + 1) % len(endpoints)
							conn.mu.Unlock()
						}
						client = nil
					}
				}
				if client != nil {
					continue
				}
				if err := conn.connect(ctx, m.dial); err != nil {
					m.logger.Debug("cluster endpoint failover pending", "cluster", conn.Name(), "error", err)
					continue
				}
				m.logger.Info("cluster endpoint failover succeeded", "cluster", conn.Name(), "endpoint", conn.ActiveEndpoint())
			}
		}
	}
}

// Close disconnects every cluster.
func (m *Manager) Close() {
	if m.watchCancel != nil {
		m.watchCancel()
	}
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
	Name          string       `json:"name"`
	URL           string       `json:"url,omitempty"`
	Status        string       `json:"status"`
	Message       string       `json:"message,omitempty"`
	Database      bool         `json:"database"`
	Roles         []string     `json:"roles"`
	Architecture  string       `json:"architecture,omitempty"`
	Description   string       `json:"description,omitempty"`
	FailureDomain string       `json:"failure_domain,omitempty"`
	Groups        []string     `json:"groups"`
	CPU           ResourcesCPU `json:"cpu"`
	Memory        ResourceMem  `json:"memory"`
	Storage       StorageUsage `json:"storage"`
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
	Pool   string `json:"pool,omitempty"`
	Used   uint64 `json:"used"`
	Total  uint64 `json:"total"`
	Shared bool   `json:"shared,omitempty"`
}

// memberResourceServer is the subset shared by the narrow test client and the
// member-targeted Incus client returned by UseTarget.
type memberResourceServer interface {
	GetServerResources() (*api.Resources, error)
	GetStoragePoolResources(name string) (*api.ResourcesStoragePool, error)
}

// Summary probes the cluster and builds a status snapshot.
func (c *Connection) Summary() Summary {
	summary := Summary{
		Name:        c.cfg.Name,
		DisplayName: c.DisplayName(),
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

	pool := ""
	driver := ""
	if pools, err := client.GetStoragePoolNames(); err == nil && len(pools) > 0 {
		sort.Strings(pools)
		pool = pools[0]
		summary.Storage.Pool = pool
		if info, _, err := client.GetStoragePool(pool); err == nil {
			driver = info.Driver
		}
	}
	sharedStorage := sharedStorageDriver(driver)

	// Cluster figures are sums over every member, not just the endpoint this
	// request happened to hit. Each member also keeps its own copy for the node
	// detail page.
	var (
		cpuTotal       uint64
		memoryUsed     uint64
		memoryTotal    uint64
		storageUsed    uint64
		storageTotal   uint64
		storageSamples int
		architecture   string
		resourceCount  int
	)
	for i := range summary.Members {
		member := &summary.Members[i]
		var target memberResourceServer = client
		if member.Name != "" {
			target = client.UseTarget(member.Name)
		}

		if resources, err := target.GetServerResources(); err == nil {
			member.CPU = ResourcesCPU{
				Total:        resources.CPU.Total,
				Architecture: resources.CPU.Architecture,
			}
			member.Memory = ResourceMem{
				Used:  resources.Memory.Used,
				Total: resources.Memory.Total,
			}
			cpuTotal += resources.CPU.Total
			memoryUsed += resources.Memory.Used
			memoryTotal += resources.Memory.Total
			if architecture == "" {
				architecture = resources.CPU.Architecture
			}
			resourceCount++
		}

		if pool != "" && !sharedStorage {
			if resources, err := target.GetStoragePoolResources(pool); err == nil {
				member.Storage = StorageUsage{
					Pool:  pool,
					Used:  resources.Space.Used,
					Total: resources.Space.Total,
				}
				storageUsed += resources.Space.Used
				storageTotal += resources.Space.Total
				storageSamples++
			}
		}
	}

	switch {
	case sharedStorage && pool != "":
		if resources, err := client.GetStoragePoolResources(pool); err == nil {
			summary.Storage.Used = resources.Space.Used
			summary.Storage.Total = resources.Space.Total
			summary.Storage.Shared = true
			for i := range summary.Members {
				summary.Members[i].Storage = summary.Storage
			}
		}
	case storageSamples > 0:
		summary.Storage.Used = storageUsed
		summary.Storage.Total = storageTotal
	case pool != "":
		// Non-clustered server, or every per-member query failed: fall back to
		// the endpoint's own pool view.
		if resources, err := client.GetStoragePoolResources(pool); err == nil {
			summary.Storage.Used = resources.Space.Used
			summary.Storage.Total = resources.Space.Total
		}
	}

	// Only managed networks are cluster-level resources. GetNetworkNames also
	// returns unmanaged host NICs, which are node-local detail and belong on the
	// node page rather than in the cluster summary.
	if networks, err := client.GetNetworks(); err == nil {
		managed := make([]string, 0, len(networks))
		for _, network := range networks {
			if network.Managed {
				managed = append(managed, network.Name)
			}
		}
		sort.Strings(managed)
		summary.Networks = managed
	}

	if resourceCount > 0 {
		summary.CPU = ResourcesCPU{Total: cpuTotal, Architecture: architecture}
		summary.Memory = ResourceMem{Used: memoryUsed, Total: memoryTotal}
	} else if resources, err := client.GetServerResources(); err == nil {
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

// sharedStorageDriver reports whether a pool is backed by storage shared by
// every cluster member. Summing those pool views per member would double-count.
func sharedStorageDriver(driver string) bool {
	switch strings.ToLower(driver) {
	case "ceph", "cephfs", "lvmcluster", "powerflex", "pure", "cephobject":
		return true
	default:
		return false
	}
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

	conn := &Connection{cfg: cfg, endpoints: config.ClusterEndpoints(cfg)}
	if err := conn.connect(ctx, m.dial); err != nil {
		m.mu.Lock()
		m.order = append(m.order, cfg.Name)
		m.conns[cfg.Name] = conn
		m.mu.Unlock()
		m.logger.Warn("cluster connection failed", "cluster", cfg.Name, "error", err)
		return conn.Summary(), fmt.Errorf("clusters: register %s: %w", cfg.Name, err)
	}

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
