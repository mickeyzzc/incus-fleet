// Package nodejoin adds a new host to an existing Incus cluster from the web
// UI. Incus only exposes a join token over its API: installing Incus and
// running the join on the new host are OS operations. The service therefore
// connects to the new node over SSH, provisions it, and submits the join
// request through the node's local Incus socket.
package nodejoin

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/lxc/incus/v7/shared/api"
	"golang.org/x/crypto/ssh"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/remote"
)

// ErrInvalidRequest marks a rejected join request.
var ErrInvalidRequest = errors.New("nodejoin: invalid request")

// ErrConflict marks a member name that already exists in the cluster.
var ErrConflict = errors.New("nodejoin: member already exists")

const (
	// DefaultIncusRepo is the Zabbly stable repository matching the Incus
	// version this release is built against.
	DefaultIncusRepo = "https://pkgs.zabbly.com/incus/lts-7.0"
	// DefaultIncusKey is the signing key for DefaultIncusRepo.
	DefaultIncusKey = "https://pkgs.zabbly.com/key.asc"
	// DefaultStorageSource is the per-member source path for a directory
	// storage pool. Existing lab nodes use the same path.
	DefaultStorageSource = "/srv/incus"
	// clusterHTTPSPort is the Incus API port used for member addresses.
	clusterHTTPSPort = "8443"
)

var (
	memberNameRE  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,62}$`)
	addressRE     = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.:-]{0,253}$`)
	sshUserRE     = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_-]{0,31}$`)
	storagePathRE = regexp.MustCompile(`^/[a-zA-Z0-9._/-]{0,199}$`)
	codenameRE    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	archRE        = regexp.MustCompile(`^(amd64|arm64|armhf|i386|riscv64|s390x)$`)
	repoURLRE     = regexp.MustCompile(`^https://[a-zA-Z0-9._~:/?#\[\]@!$&()*+,;=%-]+$`)
	packageRE     = regexp.MustCompile(`^[a-z0-9][a-z0-9.+-]{0,63}$`)
)

// Request joins one new host to a cluster.
type Request struct {
	Name          string `json:"name,omitempty"`
	Address       string `json:"address"`
	SSHUser       string `json:"ssh_user,omitempty"`
	SSHPort       int    `json:"ssh_port,omitempty"`
	SSHPassword   string `json:"ssh_password,omitempty"`
	SudoPassword  string `json:"sudo_password,omitempty"`
	InstallIncus  bool   `json:"install_incus"`
	InstallVMs    bool   `json:"install_vms"`
	IgnoreHostKey bool   `json:"ignore_host_key"`
	StorageSource string `json:"storage_source,omitempty"`
	IncusRepo     string `json:"incus_repo,omitempty"`
	IncusRepoKey  string `json:"incus_repo_key,omitempty"`
}

// Step records one provisioning stage for the UI.
type Step struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Result is the outcome of a successful join.
type Result struct {
	Member clusters.Member `json:"member"`
	Steps  []Step          `json:"steps"`
}

// FetchFunc downloads a URL. Tests inject a fake.
type FetchFunc func(ctx context.Context, url string) ([]byte, error)

// Option customizes the join service.
type Option func(*Service)

// WithDialer overrides the SSH dialer.
func WithDialer(dialer remote.Dialer) Option {
	return func(s *Service) { s.dialer = dialer }
}

// WithFetch overrides the repository key download.
func WithFetch(fetch FetchFunc) Option {
	return func(s *Service) { s.fetch = fetch }
}

// WithWait overrides the member-online polling window.
func WithWait(timeout, interval time.Duration) Option {
	return func(s *Service) {
		s.waitTimeout = timeout
		s.waitInterval = interval
	}
}

// WithReadyTimeout overrides the Incus daemon readiness window.
func WithReadyTimeout(timeout time.Duration) Option {
	return func(s *Service) { s.readyTimeout = timeout }
}

// Service joins nodes to clusters.
type Service struct {
	clusters     *clusters.Manager
	access       config.NodeAccess
	logger       *slog.Logger
	dialer       remote.Dialer
	fetch        FetchFunc
	waitTimeout  time.Duration
	waitInterval time.Duration
	readyTimeout time.Duration
}

// New builds a node join service.
func New(manager *clusters.Manager, access config.NodeAccess, logger *slog.Logger, opts ...Option) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	service := &Service{
		clusters:     manager,
		access:       access,
		logger:       logger,
		dialer:       remote.SSHDialer{},
		fetch:        httpFetch,
		waitTimeout:  2 * time.Minute,
		waitInterval: 3 * time.Second,
		readyTimeout: 30 * time.Second,
	}

	for _, opt := range opts {
		opt(service)
	}

	return service
}

// Join provisions and joins one host.
func (s *Service) Join(ctx context.Context, clusterName string, req Request) (*Result, error) {
	if s.clusters == nil {
		return nil, fmt.Errorf("%w: cluster manager is not configured", clusters.ErrUnavailable)
	}

	normalized, err := s.normalizeRequest(req)
	if err != nil {
		return nil, err
	}

	summary, err := s.clusters.Get(clusterName)
	if err != nil {
		return nil, err
	}
	if !summary.Online {
		return nil, fmt.Errorf("%w: cluster %s is offline", clusters.ErrUnavailable, clusterName)
	}
	if !summary.Clustered {
		return nil, fmt.Errorf("%w: cluster %s is not clustered", ErrInvalidRequest, clusterName)
	}

	conn, err := s.clusters.Connection(clusterName)
	if err != nil {
		return nil, err
	}

	client, err := conn.Client()
	if err != nil {
		return nil, err
	}

	steps := make([]Step, 0, 8)
	addStep := func(name, status, detail string) {
		steps = append(steps, Step{Name: name, Status: status, Detail: detail})
	}

	for _, member := range summary.Members {
		if member.Name == normalized.Name && normalized.Name != "" {
			return nil, fmt.Errorf("%w: %s already exists in %s", ErrConflict, normalized.Name, clusterName)
		}
	}

	// Connect before requesting a token: this validates the SSH credentials
	// and lets us discover the hostname when the operator leaves the name
	// empty.
	sshClient, err := s.dialer.Dial(ctx, s.sshOptions(normalized))
	if err != nil {
		return nil, fmt.Errorf("%w: connect to %s: %v", ErrInvalidRequest, normalized.Address, err)
	}
	defer sshClient.Close() //nolint:errcheck // best-effort cleanup

	if normalized.Name == "" {
		hostname, err := sshClient.Run(ctx, "hostname -s")
		if err != nil {
			return nil, fmt.Errorf("%w: detect hostname: %v", ErrInvalidRequest, err)
		}
		normalized.Name = strings.TrimSpace(hostname)
		if !memberNameRE.MatchString(normalized.Name) {
			return nil, fmt.Errorf("%w: detected hostname %q is not a valid Incus member name", ErrInvalidRequest, normalized.Name)
		}
	}
	addStep("connect", "ok", "SSH connection established to "+normalized.Address)

	provisioner := &provisioner{
		client:       sshClient,
		user:         normalized.SSHUser,
		sudoPassword: normalized.SudoPassword,
	}

	incusVersion, err := provisioner.incusVersion(ctx)
	if err != nil {
		return nil, err
	}
	if incusVersion == "" {
		if !normalized.InstallIncus {
			return nil, fmt.Errorf("%w: Incus is not installed on %s; enable install_incus", ErrInvalidRequest, normalized.Address)
		}

		if err := s.installIncus(ctx, provisioner, normalized); err != nil {
			return nil, err
		}
		addStep("install", "ok", "Incus installed from "+normalized.IncusRepo)

		incusVersion, err = provisioner.incusVersion(ctx)
		if err != nil {
			return nil, err
		}
		if incusVersion == "" {
			return nil, fmt.Errorf("%w: Incus installation completed but incus is still missing", ErrInvalidRequest)
		}
	} else {
		addStep("install", "skipped", "Incus "+incusVersion+" is already installed")
	}

	if summary.ServerVersion != "" && incusVersion != summary.ServerVersion {
		return nil, fmt.Errorf(
			"%w: node runs Incus %s but cluster %s runs %s; all members must match",
			ErrInvalidRequest, incusVersion, clusterName, summary.ServerVersion,
		)
	}

	if err := provisioner.ensureService(ctx); err != nil {
		return nil, err
	}
	if err := provisioner.waitReady(ctx, s.readyTimeout); err != nil {
		return nil, err
	}
	addStep("service", "ok", "Incus daemon is running")

	clusterInfo, _, err := client.GetCluster()
	if err != nil {
		return nil, fmt.Errorf("nodejoin: read cluster member config: %w", err)
	}

	memberConfig, err := buildMemberConfig(clusterInfo, normalized.StorageSource)
	if err != nil {
		return nil, err
	}

	if normalized.StorageSource != "" {
		if err := provisioner.mkdir(ctx, normalized.StorageSource); err != nil {
			return nil, err
		}
		addStep("storage", "ok", "Storage source "+normalized.StorageSource+" is ready")
	}

	token, err := joinToken(client, normalized.Name)
	if err != nil {
		return nil, err
	}
	addStep("token", "ok", "Join token generated for "+normalized.Name)

	certPEM, err := clusterCertificate(conn)
	if err != nil {
		return nil, err
	}

	clusterAddress, err := endpointAddress(summary.Endpoint)
	if err != nil {
		return nil, err
	}

	put := api.ClusterPut{
		Cluster: api.Cluster{
			ServerName: normalized.Name,
			Enabled:    true,
		},
		ClusterAddress:     clusterAddress,
		ClusterCertificate: string(certPEM),
		ServerAddress:      net.JoinHostPort(normalized.Address, clusterHTTPSPort),
		ClusterToken:       token,
	}
	put.MemberConfig = memberConfig

	payload, err := json.Marshal(put)
	if err != nil {
		return nil, fmt.Errorf("nodejoin: encode join request: %w", err)
	}

	if err := provisioner.join(ctx, payload); err != nil {
		return nil, err
	}
	addStep("join", "ok", "Join request accepted by cluster "+clusterName)

	member, err := s.waitOnline(ctx, client, normalized.Name)
	if err != nil {
		return nil, err
	}
	addStep("online", "ok", "Member "+normalized.Name+" is online")

	s.postSetup(ctx, provisioner, normalized, &steps)

	return &Result{Member: member, Steps: steps}, nil
}

// normalizeRequest validates and fills request defaults.
func (s *Service) normalizeRequest(req Request) (Request, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.Address = strings.TrimSpace(req.Address)
	req.SSHUser = strings.TrimSpace(req.SSHUser)
	req.StorageSource = strings.TrimSpace(req.StorageSource)
	req.IncusRepo = strings.TrimSpace(req.IncusRepo)
	req.IncusRepoKey = strings.TrimSpace(req.IncusRepoKey)

	if req.Address == "" {
		return Request{}, fmt.Errorf("%w: address is required", ErrInvalidRequest)
	}
	if !addressRE.MatchString(req.Address) || strings.Contains(req.Address, "..") {
		return Request{}, fmt.Errorf("%w: address %q is invalid", ErrInvalidRequest, req.Address)
	}
	if host, port, err := net.SplitHostPort(req.Address); err == nil && host != "" && port != "" {
		return Request{}, fmt.Errorf("%w: address %q must not include a port", ErrInvalidRequest, req.Address)
	}
	if req.Name != "" && !memberNameRE.MatchString(req.Name) {
		return Request{}, fmt.Errorf("%w: member name %q is invalid", ErrInvalidRequest, req.Name)
	}

	if req.SSHUser == "" {
		req.SSHUser = s.access.SSHUser
	}
	if req.SSHUser == "" {
		return Request{}, fmt.Errorf("%w: ssh_user is required", ErrInvalidRequest)
	}
	if !sshUserRE.MatchString(req.SSHUser) {
		return Request{}, fmt.Errorf("%w: ssh_user %q is invalid", ErrInvalidRequest, req.SSHUser)
	}

	if req.SSHPort == 0 {
		req.SSHPort = s.access.SSHPort
	}
	if req.SSHPort == 0 {
		req.SSHPort = 22
	}
	if req.SSHPort < 1 || req.SSHPort > 65535 {
		return Request{}, fmt.Errorf("%w: ssh_port %d is invalid", ErrInvalidRequest, req.SSHPort)
	}

	for label, secret := range map[string]string{
		"ssh_password":  req.SSHPassword,
		"sudo_password": req.SudoPassword,
	} {
		if strings.ContainsAny(secret, "\n\r\x00") {
			return Request{}, fmt.Errorf("%w: %s must not contain newlines", ErrInvalidRequest, label)
		}
		if len(secret) > 1024 {
			return Request{}, fmt.Errorf("%w: %s is too long", ErrInvalidRequest, label)
		}
	}

	if req.StorageSource != "" {
		if !storagePathRE.MatchString(req.StorageSource) || strings.Contains(req.StorageSource, "..") {
			return Request{}, fmt.Errorf("%w: storage_source %q must be an absolute path", ErrInvalidRequest, req.StorageSource)
		}
	} else {
		req.StorageSource = DefaultStorageSource
	}

	if req.IncusRepo == "" {
		req.IncusRepo = DefaultIncusRepo
	}
	if !repoURLRE.MatchString(req.IncusRepo) {
		return Request{}, fmt.Errorf("%w: incus_repo %q must be an https URL", ErrInvalidRequest, req.IncusRepo)
	}
	if req.IncusRepoKey == "" {
		req.IncusRepoKey = DefaultIncusKey
	}
	if !repoURLRE.MatchString(req.IncusRepoKey) {
		return Request{}, fmt.Errorf("%w: incus_repo_key %q must be an https URL", ErrInvalidRequest, req.IncusRepoKey)
	}

	if strings.TrimSpace(s.access.SSHKnownHosts) == "" &&
		!s.access.SSHInsecureIgnoreHostKey && !req.IgnoreHostKey {
		return Request{}, fmt.Errorf(
			"%w: host key verification is not configured; set node_access.ssh_known_hosts or allow ignore_host_key for a new node",
			ErrInvalidRequest,
		)
	}

	return req, nil
}

func (s *Service) sshOptions(req Request) remote.Options {
	return remote.Options{
		Address:               remote.JoinHostPort(req.Address, req.SSHPort),
		User:                  req.SSHUser,
		Password:              req.SSHPassword,
		KeyPath:               s.access.SSHKey,
		KnownHostsPath:        s.access.SSHKnownHosts,
		InsecureIgnoreHostKey: s.access.SSHInsecureIgnoreHostKey || req.IgnoreHostKey,
		Timeout:               20 * time.Second,
	}
}

// installIncus installs Incus from the configured repository.
func (s *Service) installIncus(ctx context.Context, p *provisioner, req Request) error {
	codename, err := p.detectCodename(ctx)
	if err != nil {
		return err
	}
	arch, err := p.detectArch(ctx)
	if err != nil {
		return err
	}

	key, err := s.fetch(ctx, req.IncusRepoKey)
	if err != nil {
		return fmt.Errorf("nodejoin: download repository key: %w", err)
	}
	if len(key) == 0 {
		return fmt.Errorf("%w: repository key download returned no data", ErrInvalidRequest)
	}

	if err := p.installRepo(ctx, req.IncusRepo, codename, arch, key); err != nil {
		return err
	}

	if err := p.aptInstall(ctx, "incus"); err != nil {
		return err
	}

	if req.InstallVMs {
		packages := []string{"qemu-system-x86", "ovmf", "swtpm"}
		if arch == "arm64" {
			packages = []string{"qemu-system-arm", "qemu-efi-aarch64", "swtpm"}
		}
		if err := p.aptInstall(ctx, packages...); err != nil {
			return err
		}
	}

	return nil
}

// waitOnline polls cluster membership until the new member reports online.
func (s *Service) waitOnline(ctx context.Context, client clusters.InstanceServer, name string) (clusters.Member, error) {
	deadline := time.Now().Add(s.waitTimeout)
	var lastStatus string

	for {
		members, err := client.GetClusterMembers()
		if err == nil {
			for _, member := range members {
				if member.ServerName != name {
					continue
				}
				lastStatus = member.Status
				if strings.EqualFold(member.Status, "Online") {
					return memberFromAPI(member), nil
				}
			}
		}

		if time.Now().After(deadline) {
			status := lastStatus
			if status == "" {
				status = "not listed"
			}
			return clusters.Member{}, fmt.Errorf("nodejoin: member %s did not come online (last status: %s)", name, status)
		}

		select {
		case <-ctx.Done():
			return clusters.Member{}, ctx.Err()
		case <-time.After(s.waitInterval):
		}
	}
}

// postSetup installs the fleet sudo whitelist and SSH key so the node can be
// managed from the fleet UI after the join. Failures are reported as warning
// steps because the member is already part of the cluster.
func (s *Service) postSetup(ctx context.Context, p *provisioner, req Request, steps *[]Step) {
	if req.SSHUser != "root" {
		if err := p.installSudoers(ctx, req.SSHUser); err != nil {
			*steps = append(*steps, Step{Name: "sudoers", Status: "warning", Detail: err.Error()})
		} else {
			*steps = append(*steps, Step{Name: "sudoers", Status: "ok", Detail: "Passwordless node network sudo rules installed"})
		}
	}

	if s.access.SSHUser == "" || s.access.SSHKey == "" || s.access.SSHUser != req.SSHUser {
		*steps = append(*steps, Step{Name: "fleet_key", Status: "skipped", Detail: "Configure node_access to authorize the fleet SSH key"})
		return
	}

	key, err := publicKey(s.access.SSHKey)
	if err != nil {
		*steps = append(*steps, Step{Name: "fleet_key", Status: "warning", Detail: err.Error()})
		return
	}

	if err := p.authorizeKey(ctx, string(key)); err != nil {
		*steps = append(*steps, Step{Name: "fleet_key", Status: "warning", Detail: err.Error()})
		return
	}

	*steps = append(*steps, Step{Name: "fleet_key", Status: "ok", Detail: "Fleet SSH key authorized for " + req.SSHUser})
}

// joinToken requests a cluster join token and renders it.
func joinToken(client clusters.InstanceServer, name string) (string, error) {
	op, err := client.CreateClusterMember(api.ClusterMembersPost{ServerName: name})
	if err != nil {
		return "", fmt.Errorf("nodejoin: create join token: %w", err)
	}

	tokenAPI := op.Get()
	token, err := tokenAPI.ToClusterJoinToken()
	if err != nil {
		return "", fmt.Errorf("nodejoin: decode join token: %w", err)
	}

	return token.String(), nil
}

// buildMemberConfig fills the per-member configuration values requested by
// the cluster. Directory storage pools need a per-node source path.
func buildMemberConfig(cluster *api.Cluster, storageSource string) ([]api.ClusterMemberConfigKey, error) {
	if cluster == nil || len(cluster.MemberConfig) == 0 {
		return nil, nil
	}

	out := make([]api.ClusterMemberConfigKey, 0, len(cluster.MemberConfig))
	for _, key := range cluster.MemberConfig {
		value := key.Value
		if key.Entity == "storage-pool" && key.Key == "source" && storageSource != "" {
			value = storageSource
		}
		if value == "" {
			return nil, fmt.Errorf(
				"%w: cluster requires member configuration %s/%s/%s",
				ErrInvalidRequest, key.Entity, key.Name, key.Key,
			)
		}

		out = append(out, api.ClusterMemberConfigKey{
			Entity:      key.Entity,
			Name:        key.Name,
			Key:         key.Key,
			Value:       value,
			Description: key.Description,
		})
	}

	return out, nil
}

// clusterCertificate returns the PEM-encoded certificate the joining node must
// trust. The fleet configuration pins it per cluster.
func clusterCertificate(conn *clusters.Connection) ([]byte, error) {
	cfg := conn.Config()
	if cfg.TLSServerCert != "" {
		pemBytes, err := os.ReadFile(cfg.TLSServerCert) //nolint:gosec // operator-supplied certificate path
		if err != nil {
			return nil, fmt.Errorf("nodejoin: read cluster certificate: %w", err)
		}

		return pemBytes, nil
	}

	// No pinned certificate: capture the endpoint certificate.
	endpoint, err := url.Parse(cfg.Endpoint)
	if err != nil || endpoint.Host == "" {
		return nil, fmt.Errorf("nodejoin: invalid cluster endpoint %q", cfg.Endpoint)
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	raw, err := tls.DialWithDialer(dialer, "tcp", endpoint.Host, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // certificate is pinned after the handshake
	if err != nil {
		return nil, fmt.Errorf("nodejoin: inspect cluster certificate: %w", err)
	}
	defer raw.Close() //nolint:errcheck // best-effort cleanup

	certs := raw.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil, errors.New("nodejoin: cluster endpoint presented no certificate")
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certs[0].Raw}), nil
}

// endpointAddress converts an https endpoint into host:port for the join
// request.
func endpointAddress(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("nodejoin: invalid cluster endpoint %q", endpoint)
	}

	host := parsed.Host
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, clusterHTTPSPort)
	}

	return host, nil
}

func memberFromAPI(member api.ClusterMember) clusters.Member {
	return clusters.Member{
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
	}
}

// publicKey renders the public half of an OpenSSH private key in
// authorized_keys format.
func publicKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path) //nolint:gosec // operator-supplied key path
	if err != nil {
		return nil, fmt.Errorf("nodejoin: read fleet ssh key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("nodejoin: parse fleet ssh key: %w", err)
	}

	return ssh.MarshalAuthorizedKey(signer.PublicKey()), nil
}

// httpFetch downloads a URL with a bounded body.
func httpFetch(ctx context.Context, rawURL string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 20 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close() //nolint:errcheck // best-effort cleanup

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %s", response.Status)
	}

	return io.ReadAll(io.LimitReader(response.Body, 1<<20))
}

// provisioner runs privileged setup commands over one SSH connection.
type provisioner struct {
	client       remote.Client
	user         string
	sudoPassword string
}

// priv runs one privileged command. Password mode uses `sudo -k -S` so a
// cached timestamp can never make sudo swallow the password line, and
// commands that need stdin receive it through an uploaded temp file instead.
func (p *provisioner) priv(ctx context.Context, command string) (string, error) {
	if p.user == "root" {
		return p.client.Run(ctx, command)
	}
	if p.sudoPassword != "" {
		err := p.client.RunStdin(ctx, "sudo -k -S -p '' "+command, p.sudoPassword+"\n")
		return "", err
	}

	return p.client.Run(ctx, "sudo -n "+command)
}

// upload writes a user-owned temporary file on the node. The random suffix
// keeps concurrent retries and local symlink games from colliding.
func (p *provisioner) upload(ctx context.Context, name, content, mode string) (string, error) {
	suffix, err := randomSuffix()
	if err != nil {
		return "", err
	}

	path := "/tmp/incus-fleet-" + name + "-" + suffix
	command := "umask 077; cat > " + remote.ShellQuote(path) + " && chmod " + mode + " " + remote.ShellQuote(path)
	if err := p.client.RunStdin(ctx, command, content); err != nil {
		return "", err
	}

	return path, nil
}

func randomSuffix() (string, error) {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("nodejoin: generate temp name: %w", err)
	}

	return hex.EncodeToString(raw), nil
}

// remove deletes a temporary file.
func (p *provisioner) remove(ctx context.Context, path string) {
	_, _ = p.client.Run(ctx, "rm -f "+remote.ShellQuote(path))
}

func (p *provisioner) incusVersion(ctx context.Context) (string, error) {
	out, err := p.client.Run(ctx, "incus --version 2>/dev/null || true")
	if err != nil {
		return "", fmt.Errorf("nodejoin: read Incus version: %w", err)
	}

	return strings.TrimSpace(out), nil
}

func (p *provisioner) detectCodename(ctx context.Context) (string, error) {
	out, err := p.client.Run(ctx, `. /etc/os-release 2>/dev/null; printf '%s' "$VERSION_CODENAME"`)
	if err != nil {
		return "", fmt.Errorf("nodejoin: detect OS codename: %w", err)
	}

	codename := strings.TrimSpace(out)
	if !codenameRE.MatchString(codename) {
		return "", fmt.Errorf("%w: detected OS codename %q is not supported", ErrInvalidRequest, codename)
	}

	return codename, nil
}

func (p *provisioner) detectArch(ctx context.Context) (string, error) {
	out, err := p.client.Run(ctx, "dpkg --print-architecture")
	if err != nil {
		return "", fmt.Errorf("nodejoin: detect architecture: %w", err)
	}

	arch := strings.TrimSpace(out)
	if !archRE.MatchString(arch) {
		return "", fmt.Errorf("%w: detected architecture %q is not supported", ErrInvalidRequest, arch)
	}

	return arch, nil
}

// installRepo writes the apt source and signing key, then refreshes apt.
func (p *provisioner) installRepo(ctx context.Context, repo, codename, arch string, key []byte) error {
	if _, err := p.priv(ctx, "install -d -m 0755 /etc/apt/keyrings"); err != nil {
		return fmt.Errorf("nodejoin: create keyrings directory: %w", err)
	}

	keyPath, err := p.upload(ctx, "zabbly.asc", string(key), "0644")
	if err != nil {
		return fmt.Errorf("nodejoin: upload repository key: %w", err)
	}
	defer p.remove(ctx, keyPath)

	if _, err := p.priv(ctx, "install -m 0644 -o root -g root "+remote.ShellQuote(keyPath)+" /etc/apt/keyrings/zabbly.asc"); err != nil {
		return fmt.Errorf("nodejoin: install repository key: %w", err)
	}

	sources := strings.Join([]string{
		"Enabled: yes",
		"Types: deb",
		"URIs: " + repo,
		"Suites: " + codename,
		"Components: main",
		"Architectures: " + arch,
		"Signed-By: /etc/apt/keyrings/zabbly.asc",
		"",
	}, "\n")

	sourcesPath, err := p.upload(ctx, "incus.sources", sources, "0644")
	if err != nil {
		return fmt.Errorf("nodejoin: upload apt source: %w", err)
	}
	defer p.remove(ctx, sourcesPath)

	if _, err := p.priv(ctx, "install -m 0644 -o root -g root "+remote.ShellQuote(sourcesPath)+" /etc/apt/sources.list.d/zabbly-incus.sources"); err != nil {
		return fmt.Errorf("nodejoin: install apt source: %w", err)
	}

	if err := p.aptUpdate(ctx); err != nil {
		return err
	}

	return nil
}

func (p *provisioner) aptUpdate(ctx context.Context) error {
	if _, err := p.priv(ctx, "env DEBIAN_FRONTEND=noninteractive apt-get update"); err != nil {
		return fmt.Errorf("nodejoin: apt-get update: %w", err)
	}

	return nil
}

func (p *provisioner) aptInstall(ctx context.Context, packages ...string) error {
	quoted := make([]string, 0, len(packages))
	for _, pkg := range packages {
		if !packageRE.MatchString(pkg) {
			return fmt.Errorf("%w: invalid package name %q", ErrInvalidRequest, pkg)
		}
		quoted = append(quoted, remote.ShellQuote(pkg))
	}

	command := "env DEBIAN_FRONTEND=noninteractive apt-get install -y " + strings.Join(quoted, " ")
	// Installations routinely exceed the default command timeout.
	installCtx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()

	if _, err := p.priv(installCtx, command); err != nil {
		return fmt.Errorf("nodejoin: apt-get install %s: %w", strings.Join(packages, " "), err)
	}

	return nil
}

func (p *provisioner) ensureService(ctx context.Context) error {
	if _, err := p.priv(ctx, "systemctl enable --now incus.socket incus"); err != nil {
		return fmt.Errorf("nodejoin: start Incus service: %w", err)
	}

	return nil
}

// waitReady polls the local Incus API until the daemon answers.
func (p *provisioner) waitReady(ctx context.Context, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		if _, err := p.priv(ctx, "incus query /1.0"); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: Incus daemon did not become ready", ErrInvalidRequest)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (p *provisioner) mkdir(ctx context.Context, path string) error {
	if _, err := p.priv(ctx, "mkdir -p "+remote.ShellQuote(path)); err != nil {
		return fmt.Errorf("nodejoin: create storage source %s: %w", path, err)
	}

	return nil
}

// join uploads the ClusterPut payload and submits it through the local socket.
func (p *provisioner) join(ctx context.Context, payload []byte) error {
	path, err := p.upload(ctx, "join.json", string(payload), "0600")
	if err != nil {
		return fmt.Errorf("nodejoin: upload join request: %w", err)
	}
	defer p.remove(ctx, path)

	joinCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	if _, err := p.priv(joinCtx, "incus query -X PUT --data-file "+remote.ShellQuote(path)+" --wait /1.0/cluster"); err != nil {
		return fmt.Errorf("nodejoin: join cluster: %w", err)
	}

	return nil
}

// installSudoers installs the node network whitelist for the SSH account.
func (p *provisioner) installSudoers(ctx context.Context, user string) error {
	content := strings.Join([]string{
		"# Managed by incus-fleet node join.",
		"# Minimal host network operations used by the fleet node page.",
		user + " ALL=(root) NOPASSWD: \\",
		"    /usr/bin/ip, /usr/sbin/ip, \\",
		"    /usr/bin/nft, /usr/sbin/nft, \\",
		"    /usr/bin/iw, /usr/sbin/iw, \\",
		"    /usr/bin/dhcpcd, /usr/sbin/dhcpcd, \\",
		"    /usr/bin/wpa_supplicant, /usr/sbin/wpa_supplicant, \\",
		"    /usr/bin/hostname, /usr/sbin/hostname, \\",
		"    /usr/bin/tee /etc/network/interfaces.d/*, \\",
		"    /usr/bin/tee /etc/wpa_supplicant/fleet.conf",
		"",
	}, "\n")

	path, err := p.upload(ctx, "nodesudoers", content, "0600")
	if err != nil {
		return fmt.Errorf("upload sudoers file: %w", err)
	}
	defer p.remove(ctx, path)

	if _, err := p.priv(ctx, "visudo -cf "+remote.ShellQuote(path)); err != nil {
		return fmt.Errorf("validate sudoers file: %w", err)
	}

	if _, err := p.priv(ctx, "install -m 0440 -o root -g root "+remote.ShellQuote(path)+" /etc/sudoers.d/incus-fleet-nodesudoers"); err != nil {
		return fmt.Errorf("install sudoers file: %w", err)
	}

	return nil
}

// authorizeKey appends the fleet public key to the SSH user's authorized_keys.
func (p *provisioner) authorizeKey(ctx context.Context, key string) error {
	quoted := remote.ShellQuote(key)
	command := strings.Join([]string{
		"umask 077",
		"mkdir -p ~/.ssh",
		"touch ~/.ssh/authorized_keys",
		"grep -qxF " + quoted + " ~/.ssh/authorized_keys || printf '%s\\n' " + quoted + " >> ~/.ssh/authorized_keys",
	}, "; ")

	if _, err := p.client.Run(ctx, command); err != nil {
		return fmt.Errorf("authorize fleet SSH key: %w", err)
	}

	return nil
}
