// Package nodeaccess routes per-node host operations to the node that owns
// them. The fleet instance's own host is served by the local nodenet service;
// every other cluster member is reached over SSH using the fleet SSH identity.
package nodeaccess

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/hostops"
	"github.com/mickeyzzc/incus-fleet/internal/nodenet"
	"github.com/mickeyzzc/incus-fleet/internal/remote"
)

// ErrUnknownMember marks a member that is not part of the cluster.
var ErrUnknownMember = errors.New("nodeaccess: unknown cluster member")

// ClusterLookup resolves cluster summaries. clusters.Manager implements it.
type ClusterLookup interface {
	Get(name string) (clusters.Summary, error)
}

// Option customizes a Service.
type Option func(*Service)

// WithLocalMember overrides the local member name (defaults to os.Hostname).
func WithLocalMember(name string) Option {
	return func(s *Service) { s.localMember = name }
}

// WithDialer overrides the SSH dialer.
func WithDialer(dialer remote.Dialer) Option {
	return func(s *Service) { s.dialer = dialer }
}

// WithAgentResolver attaches the optional agent backend.
func WithAgentResolver(resolver AgentResolver) Option {
	return func(s *Service) { s.agentResolver = resolver }
}

// SetAgentResolver attaches the agent backend after construction.
func (s *Service) SetAgentResolver(resolver AgentResolver) { s.agentResolver = resolver }

// AgentResolver resolves an online agent operator for one member.
type AgentResolver func(ctx context.Context, cluster, member string) (hostops.Operator, bool)

// Service exposes node network operations for cluster members.
type Service struct {
	lookup           ClusterLookup
	local            *nodenet.Service
	localMember      string
	access           config.NodeAccess
	dialer           remote.Dialer
	agentResolver    AgentResolver
	mode             hostops.Mode
	sshWriteFallback bool
}

var _ hostops.Resolver = (*Service)(nil)

// New builds a member-aware node network service.
func New(lookup ClusterLookup, local *nodenet.Service, access config.NodeAccess, opts ...Option) *Service {
	if local == nil {
		local = nodenet.NewService(nodenet.SudoRunner{})
	}

	hostname, _ := os.Hostname()
	mode, err := hostops.ParseMode(access.Mode)
	if err != nil {
		mode = hostops.ModeAuto
	}

	service := &Service{
		lookup:           lookup,
		local:            local,
		localMember:      hostname,
		access:           access,
		dialer:           remote.SSHDialer{},
		mode:             mode,
		sshWriteFallback: access.SSHWriteFallbackEnabled(),
	}

	for _, opt := range opts {
		opt(service)
	}

	return service
}

// Service resolves the node network service for one cluster member. It is
// kept for callers that need the concrete nodenet service; new code should
// use Resolve so the backend is explicit.
func (s *Service) Service(clusterName, memberName string) (*nodenet.Service, string, func(), error) {
	node, host, _, closeNode, err := s.resolveNode(clusterName, memberName)
	return node, host, closeNode, err
}

// Resolve implements hostops.Resolver.
func (s *Service) Resolve(ctx context.Context, clusterName, memberName string, kind hostops.Kind) (hostops.Operator, hostops.Backend, func(), error) {
	member, err := s.findMember(clusterName, memberName)
	if err != nil {
		return nil, "", nil, err
	}
	if member.Name == s.localMember {
		return s.local, hostops.BackendLocal, func() {}, nil
	}
	if s.mode != hostops.ModeSSH && s.mode != hostops.ModeLocal && s.agentResolver != nil {
		if operator, ok := s.agentResolver(ctx, clusterName, member.Name); ok {
			return operator, hostops.BackendAgent, func() {}, nil
		}
		if s.mode == hostops.ModeAgent {
			return nil, "", nil, fmt.Errorf("%w: no online agent for %s/%s", hostops.ErrAgentUnavailable, clusterName, member.Name)
		}
	} else if s.mode == hostops.ModeAgent {
		return nil, "", nil, fmt.Errorf("%w: no agent resolver configured", hostops.ErrAgentUnavailable)
	}

	node, _, backend, closeNode, err := s.resolveNode(clusterName, memberName)
	if err != nil {
		return nil, "", nil, err
	}
	if kind == hostops.Write && backend == hostops.BackendSSH && s.mode == hostops.ModeAuto && !s.sshWriteFallback {
		closeNode()
		return nil, "", nil, fmt.Errorf("%w: configure node_access.ssh_write_fallback=true or install the host agent", hostops.ErrSSHWriteDisabled)
	}

	return node, backend, closeNode, nil
}

func (s *Service) findMember(clusterName, memberName string) (*clusters.Member, error) {
	if s.lookup == nil {
		return nil, fmt.Errorf("%w: cluster lookup is not configured", nodenet.ErrUnavailable)
	}
	summary, err := s.lookup.Get(clusterName)
	if err != nil {
		return nil, err
	}
	memberName = strings.TrimSpace(memberName)
	for i := range summary.Members {
		if summary.Members[i].Name == memberName {
			return &summary.Members[i], nil
		}
	}
	return nil, fmt.Errorf("%w: %s/%s", ErrUnknownMember, clusterName, memberName)
}

func (s *Service) resolveNode(clusterName, memberName string) (*nodenet.Service, string, hostops.Backend, func(), error) {
	member, err := s.findMember(clusterName, memberName)
	if err != nil {
		return nil, "", "", nil, err
	}

	if member.Name == s.localMember {
		return s.local, member.Name, hostops.BackendLocal, func() {}, nil
	}

	switch s.mode {
	case hostops.ModeLocal:
		return nil, "", "", nil, fmt.Errorf("%w: node_access.mode=local only manages the fleet host", nodenet.ErrUnavailable)
	case hostops.ModeAgent:
		return nil, "", "", nil, fmt.Errorf("%w: no agent is registered for %s/%s", hostops.ErrAgentUnavailable, clusterName, member.Name)
	}

	if strings.TrimSpace(s.access.SSHUser) == "" || strings.TrimSpace(s.access.SSHKey) == "" {
		return nil, "", "", nil, fmt.Errorf("%w: remote node access is not configured (node_access.ssh_user and node_access.ssh_key)", nodenet.ErrUnavailable)
	}

	host, err := remote.HostFromURL(member.URL)
	if err != nil {
		return nil, "", "", nil, fmt.Errorf("%w: %v", nodenet.ErrUnavailable, err)
	}

	runner := &sshRunner{
		dialer: s.dialer,
		opts: remote.Options{
			Address:               remote.JoinHostPort(host, s.access.SSHPort),
			User:                  s.access.SSHUser,
			KeyPath:               s.access.SSHKey,
			KnownHostsPath:        s.access.SSHKnownHosts,
			InsecureIgnoreHostKey: s.access.SSHInsecureIgnoreHostKey,
		},
		sudo: s.access.SSHUser != "root",
	}

	return nodenet.NewService(runner), host, hostops.BackendSSH, runner.Close, nil
}

// Snapshot renders the physical network state of one cluster member.
func (s *Service) Snapshot(ctx context.Context, clusterName, memberName, wifiDevice string) (*nodenet.Snapshot, error) {
	node, _, closeNode, err := s.Service(clusterName, memberName)
	if err != nil {
		return nil, err
	}
	defer closeNode()

	return node.Snapshot(ctx, wifiDevice)
}

// SetNICState brings one member NIC up or down.
func (s *Service) SetNICState(clusterName, memberName, nic string, up bool) error {
	node, _, closeNode, err := s.Service(clusterName, memberName)
	if err != nil {
		return err
	}
	defer closeNode()

	return node.SetNICState(nic, up)
}

// ApplyAddress switches one member NIC between DHCP and a static address.
func (s *Service) ApplyAddress(ctx context.Context, clusterName, memberName, nic string, req nodenet.AddressRequest) error {
	node, _, closeNode, err := s.Service(clusterName, memberName)
	if err != nil {
		return err
	}
	defer closeNode()

	return node.ApplyAddress(ctx, nic, req)
}

// WifiConnect associates one member Wi-Fi NIC with an access point.
func (s *Service) WifiConnect(clusterName, memberName, nic, ssid, password string) error {
	node, _, closeNode, err := s.Service(clusterName, memberName)
	if err != nil {
		return err
	}
	defer closeNode()

	return node.WifiConnect(nic, ssid, password)
}

// ListRoutes lists the routing table of one cluster member.
func (s *Service) ListRoutes(clusterName, memberName string) ([]nodenet.Route, error) {
	node, _, closeNode, err := s.Service(clusterName, memberName)
	if err != nil {
		return nil, err
	}
	defer closeNode()

	return node.ListRoutes()
}

// AddRoute adds one static route on a cluster member.
func (s *Service) AddRoute(clusterName, memberName, destination, gateway, device string) error {
	node, _, closeNode, err := s.Service(clusterName, memberName)
	if err != nil {
		return err
	}
	defer closeNode()

	return node.AddRoute(destination, gateway, device)
}

// DeleteRoute removes one static route from a cluster member.
func (s *Service) DeleteRoute(clusterName, memberName, destination string) error {
	node, _, closeNode, err := s.Service(clusterName, memberName)
	if err != nil {
		return err
	}
	defer closeNode()

	return node.DeleteRoute(destination)
}

// ListFirewall lists the fleet-managed firewall rules of one cluster member.
func (s *Service) ListFirewall(clusterName, memberName string) ([]nodenet.FirewallRule, error) {
	node, _, closeNode, err := s.Service(clusterName, memberName)
	if err != nil {
		return nil, err
	}
	defer closeNode()

	return node.ListFirewall()
}

// AddFirewallRule adds one input rule on a cluster member.
func (s *Service) AddFirewallRule(clusterName, memberName string, req nodenet.FirewallRequest) error {
	node, _, closeNode, err := s.Service(clusterName, memberName)
	if err != nil {
		return err
	}
	defer closeNode()

	return node.AddFirewallRule(req)
}

// DeleteFirewallRule removes one input rule by handle.
func (s *Service) DeleteFirewallRule(clusterName, memberName, handle string) error {
	node, _, closeNode, err := s.Service(clusterName, memberName)
	if err != nil {
		return err
	}
	defer closeNode()

	return node.DeleteFirewallRule(handle)
}

// sshRunner adapts one SSH target to nodenet.Runner. It keeps a single
// connection alive for the duration of one API operation and redials after a
// failed command.
type sshRunner struct {
	dialer remote.Dialer
	opts   remote.Options
	sudo   bool

	mu     sync.Mutex
	client remote.Client
}

// Run implements nodenet.Runner.
func (r *sshRunner) Run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := r.connection(ctx)
	if err != nil {
		return "", err
	}

	out, err := client.Run(ctx, r.command(name, args))
	if err != nil {
		r.reset(client)
		return out, fmt.Errorf("%w: %v", nodenet.ErrUnavailable, err)
	}

	return out, nil
}

// RunStdin implements nodenet.Runner.
func (r *sshRunner) RunStdin(name string, args []string, stdin string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := r.connection(ctx)
	if err != nil {
		return err
	}

	if err := client.RunStdin(ctx, r.command(name, args), stdin); err != nil {
		r.reset(client)
		return fmt.Errorf("%w: %v", nodenet.ErrUnavailable, err)
	}

	return nil
}

// Close releases the SSH connection.
func (r *sshRunner) Close() {
	r.mu.Lock()
	client := r.client
	r.client = nil
	r.mu.Unlock()

	if client != nil {
		_ = client.Close()
	}
}

func (r *sshRunner) connection(ctx context.Context) (remote.Client, error) {
	r.mu.Lock()
	if r.client != nil {
		client := r.client
		r.mu.Unlock()
		return client, nil
	}
	r.mu.Unlock()

	client, err := r.dialer.Dial(ctx, r.opts)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", nodenet.ErrUnavailable, err)
	}

	r.mu.Lock()
	if r.client != nil {
		// Another call won the race; keep the first connection.
		existing := r.client
		r.mu.Unlock()
		_ = client.Close()
		return existing, nil
	}
	r.client = client
	r.mu.Unlock()

	return client, nil
}

func (r *sshRunner) reset(client remote.Client) {
	r.mu.Lock()
	if r.client == client {
		r.client = nil
	}
	r.mu.Unlock()

	_ = client.Close()
}

// command builds a shell command that resolves the binary with the same
// preference order as the local runner, then escalates through sudo -n.
func (r *sshRunner) command(name string, args []string) string {
	quotedArgs := make([]string, 0, len(args))
	for _, arg := range args {
		quotedArgs = append(quotedArgs, remote.ShellQuote(arg))
	}

	prefix := `bin=$(PATH=/usr/bin:/bin:/usr/sbin:/sbin command -v ` + remote.ShellQuote(name) + `) && `
	runner := `"$bin"`
	if r.sudo {
		runner = `sudo -n "$bin"`
	}

	return prefix + runner + " " + strings.Join(quotedArgs, " ")
}
