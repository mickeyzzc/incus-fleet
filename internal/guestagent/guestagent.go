// Package guestagent installs the official incus-agent into agent-less
// virtual machines.
//
// Incus hands every VM a read-only "config" 9p share carrying the official
// incus-agent binary and its install.sh. The installer reaches the guest over
// SSH — jumping through the member host when the bridge network is not
// routable from the center — runs that script, and then verifies that the
// agent started reporting instance state. Guest SSH credentials travel with
// the request and are never persisted or logged.
package guestagent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/remote"
)

// Sentinel errors mapped onto HTTP statuses by the API layer.
var (
	ErrInvalidRequest = errors.New("guestagent: invalid request")
	ErrNotFound       = errors.New("guestagent: instance not found")
)

const (
	// configMount is where the guest's Incus config share gets mounted. It is
	// not /mnt itself so an occupied /mnt never breaks the install.
	configMount = "/mnt/incus-config"

	connectTimeout = 20 * time.Second
	commandTimeout = 2 * time.Minute

	defaultVerifyTimeout  = 90 * time.Second
	defaultVerifyInterval = 3 * time.Second
)

// Request installs the incus-agent into one virtual machine.
type Request struct {
	// Address optionally pins the guest SSH endpoint (host or host:port).
	// When empty it is resolved from the member's DHCP leases by MAC.
	Address      string `json:"address,omitempty"`
	SSHPort      int    `json:"ssh_port,omitempty"`
	SSHUser      string `json:"ssh_user,omitempty"`
	SSHPassword  string `json:"ssh_password,omitempty"`
	SudoPassword string `json:"sudo_password,omitempty"`
}

// Step records one install stage for the UI.
type Step struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Result is the outcome of one install attempt. Steps are always populated;
// a non-nil error means the install did not complete.
type Result struct {
	Steps []Step `json:"steps"`
}

// Option customizes the installer.
type Option func(*Service)

// WithDialer overrides the SSH dialer (tests supply a fake).
func WithDialer(dialer remote.Dialer) Option {
	return func(s *Service) { s.dialer = dialer }
}

// WithVerify overrides the post-install verification window (tests).
func WithVerify(timeout, interval time.Duration) Option {
	return func(s *Service) {
		s.verifyTimeout = timeout
		s.verifyInterval = interval
	}
}

// Service installs the official incus-agent into guests.
type Service struct {
	clusters       *clusters.Manager
	access         config.NodeAccess
	logger         *slog.Logger
	dialer         remote.Dialer
	verifyTimeout  time.Duration
	verifyInterval time.Duration
}

// New builds a guest agent installer.
func New(manager *clusters.Manager, access config.NodeAccess, logger *slog.Logger, opts ...Option) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	service := &Service{
		clusters:       manager,
		access:         access,
		logger:         logger,
		dialer:         remote.SSHDialer{},
		verifyTimeout:  defaultVerifyTimeout,
		verifyInterval: defaultVerifyInterval,
	}

	for _, opt := range opts {
		opt(service)
	}

	return service
}

// Install runs the official agent installer inside one guest VM and reports
// each stage for the UI.
func (s *Service) Install(ctx context.Context, clusterName, instanceName string, req Request) (*Result, error) {
	normalizeRequest(&req)
	if err := validateRequest(req); err != nil {
		return &Result{}, err
	}

	if s.clusters == nil {
		return &Result{}, fmt.Errorf("%w: cluster manager is not configured", clusters.ErrUnavailable)
	}

	summary, err := s.clusters.Get(clusterName)
	if err != nil {
		return &Result{}, err
	}
	if !summary.Online {
		return &Result{}, fmt.Errorf("%w: cluster %s is offline", clusters.ErrUnavailable, clusterName)
	}

	conn, err := s.clusters.Connection(clusterName)
	if err != nil {
		return &Result{}, err
	}
	client, err := conn.Client()
	if err != nil {
		return &Result{}, err
	}

	instance, _, err := client.GetInstance(instanceName)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			return &Result{}, fmt.Errorf("%w: %s:%s", ErrNotFound, clusterName, instanceName)
		}
		return &Result{}, fmt.Errorf("guestagent: get %s:%s: %w", clusterName, instanceName, err)
	}
	if instance.Type != "virtual-machine" {
		return &Result{}, fmt.Errorf("%w: %s is not a virtual machine; containers already run the agent through Incus", ErrInvalidRequest, instanceName)
	}
	if !strings.EqualFold(instance.Status, "Running") {
		return &Result{}, fmt.Errorf("%w: the virtual machine must be running to install the agent", ErrInvalidRequest)
	}

	steps := make([]Step, 0, 8)
	record := func(name, status, detail string) {
		steps = append(steps, Step{Name: name, Status: status, Detail: detail})
	}
	fail := func(name, detail string, err error) (*Result, error) {
		record(name, "failed", detail)
		return &Result{Steps: steps}, err
	}

	record("validate", "ok", "virtual machine "+instanceName+" is running")

	guestAddr, resolveDetail, err := s.resolveAddress(client, instance, req.Address)
	if err != nil {
		return fail("resolve-address", err.Error(), fmt.Errorf("%w: %w", ErrInvalidRequest, err))
	}
	record("resolve-address", "ok", resolveDetail)

	guestClient, closeConn, connectDetail, err := s.connect(ctx, client, instance, req, guestAddr)
	if err != nil {
		return fail("connect", err.Error(), err)
	}
	defer closeConn()
	record("connect", "ok", connectDetail)

	p := &provisioner{client: guestClient, user: req.SSHUser, sudoPassword: req.SudoPassword}

	if err := checkGuest(ctx, p); err != nil {
		return fail("check-guest", err.Error(), err)
	}
	record("check-guest", "ok", "systemd init and 9p support detected")

	if _, err := p.priv(ctx, mountCommand()); err != nil {
		return fail("mount-config", err.Error(), fmt.Errorf("guestagent: mount config share: %w", err))
	}
	record("mount-config", "ok", "Incus config share mounted at "+configMount)

	if _, err := p.priv(ctx, "sh "+remote.ShellQuote(configMount+"/install.sh")); err != nil {
		// The share may hold state from a half-finished run; always unmount.
		_, _ = p.priv(ctx, unmountCommand())
		return fail("install", err.Error(), fmt.Errorf("guestagent: run install.sh: %w", err))
	}
	// install.sh enables the unit; a restart makes sure a stale agent (from a
	// previous manual attempt) does not keep a dead vsock connection.
	_, _ = p.priv(ctx, "systemctl restart incus-agent 2>/dev/null || true")
	record("install", "ok", "official install.sh completed")

	_, _ = p.priv(ctx, unmountCommand())
	record("cleanup", "ok", "config share unmounted")

	if s.verifyAgent(ctx, client, instanceName) {
		record("verify", "ok", "agent started reporting instance state")
		return &Result{Steps: steps}, nil
	}

	// The install itself succeeded; the guest just has not reported yet.
	record("verify", "warn", fmt.Sprintf(
		"agent did not report state within %s; reboot the guest if the terminal stays unavailable",
		s.verifyTimeout))
	return &Result{Steps: steps}, nil
}

// normalizeRequest applies defaults to the raw request.
func normalizeRequest(req *Request) {
	req.Address = strings.TrimSpace(req.Address)
	req.SSHUser = strings.TrimSpace(req.SSHUser)
	if req.SSHUser == "" {
		req.SSHUser = "root"
	}
	if req.SSHPort == 0 {
		req.SSHPort = 22
	}
}

// validateRequest checks shapes; secrets are validated but never logged.
func validateRequest(req Request) error {
	if req.SSHPort < 1 || req.SSHPort > 65535 {
		return fmt.Errorf("%w: ssh_port %d is invalid", ErrInvalidRequest, req.SSHPort)
	}
	if req.Address != "" && strings.ContainsAny(req.Address, " \t\n\r") {
		return fmt.Errorf("%w: address must be host or host:port without spaces", ErrInvalidRequest)
	}

	for label, secret := range map[string]string{
		"ssh_password":  req.SSHPassword,
		"sudo_password": req.SudoPassword,
	} {
		if strings.ContainsAny(secret, "\n\r\x00") {
			return fmt.Errorf("%w: %s must not contain newlines", ErrInvalidRequest, label)
		}
		if len(secret) > 1024 {
			return fmt.Errorf("%w: %s is too long", ErrInvalidRequest, label)
		}
	}

	return nil
}

// resolveAddress returns the guest SSH address: the operator-supplied one when
// present, otherwise the DHCP lease matching one of the VM's NIC MACs.
func (s *Service) resolveAddress(client clusters.InstanceServer, instance *api.Instance, address string) (string, string, error) {
	if address != "" {
		return address, "using operator-supplied address " + address, nil
	}

	macs := volatileMACs(instance)
	if len(macs) == 0 {
		return "", "", errors.New("the VM has no recorded NIC MAC; enter the guest address manually")
	}

	networks := nicNetworks(instance)
	var candidates []string
	for _, network := range networks {
		leases, err := client.GetNetworkLeases(network)
		if err != nil {
			continue
		}
		for _, lease := range leases {
			matched := false
			for _, mac := range macs {
				if strings.EqualFold(lease.Hwaddr, mac) {
					matched = true
					break
				}
			}
			if matched {
				detail := fmt.Sprintf("lease %s for %s on %s", lease.Address, lease.Hwaddr, network)
				return lease.Address, detail, nil
			}
			candidates = append(candidates, lease.Address+" ("+lease.Hwaddr+")")
		}
	}

	if len(candidates) == 0 {
		return "", "", errors.New("no DHCP leases are known for this VM's networks; enter the guest address manually")
	}

	return "", "", fmt.Errorf("no lease matches the VM MACs (%s); known leases: %s",
		strings.Join(macs, ", "), strings.Join(candidates, ", "))
}

// volatileMACs collects the hardware addresses Incus assigned to the VM's
// NICs. They exist even without an agent because incusd records them in the
// instance config.
func volatileMACs(instance *api.Instance) []string {
	var macs []string
	for key, value := range instance.Config {
		if strings.HasSuffix(key, ".hwaddr") && strings.HasPrefix(key, "volatile.") && value != "" {
			macs = append(macs, value)
		}
	}

	return macs
}

// nicNetworks lists the managed networks the VM's NICs attach to, so the
// lease lookup only queries relevant networks.
func nicNetworks(instance *api.Instance) []string {
	seen := map[string]bool{}
	var networks []string

	add := func(device map[string]string) {
		if device["type"] != "nic" {
			return
		}
		name := device["network"]
		if name == "" {
			name = device["parent"]
		}
		if name != "" && !seen[name] {
			seen[name] = true
			networks = append(networks, name)
		}
	}

	for _, device := range instance.Devices {
		add(device)
	}
	for _, device := range instance.ExpandedDevices {
		add(device)
	}

	return networks
}

// memberHost resolves the SSH host of the member hosting the instance, empty
// when the cluster is standalone or the member URL is unusable.
func (s *Service) memberHost(client clusters.InstanceServer, location string) string {
	if location == "" {
		return ""
	}

	members, err := client.GetClusterMembers()
	if err != nil {
		return ""
	}

	for _, member := range members {
		if member.ServerName == location {
			host, err := remote.HostFromURL(member.URL)
			if err != nil {
				return ""
			}
			return host
		}
	}

	return ""
}

// connect opens the guest SSH session. The preferred path jumps through the
// member host with the fleet's node_access identity; a direct dial covers
// co-located centers. The returned close function releases both connections.
func (s *Service) connect(
	ctx context.Context,
	client clusters.InstanceServer,
	instance *api.Instance,
	req Request,
	guestAddr string,
) (remote.Client, func(), string, error) {
	guestOpts := remote.Options{
		Address: remote.JoinHostPort(guestAddr, req.SSHPort),
		User:    req.SSHUser,
		// Guest host keys are never pre-shared; the connection runs over the
		// private bridge or a member jump, so identity is carried by the
		// credentials instead. This matches the lab-scoped opt-out used for
		// node joins.
		InsecureIgnoreHostKey: true,
		Password:              req.SSHPassword,
		Timeout:               connectTimeout,
	}

	var jumpErr error
	if memberHost := s.memberHost(client, instance.Location); memberHost != "" && s.canJump() {
		jumpOpts := remote.Options{
			Address:               remote.JoinHostPort(memberHost, s.access.SSHPort),
			User:                  s.access.SSHUser,
			KeyPath:               s.access.SSHKey,
			KnownHostsPath:        s.access.SSHKnownHosts,
			InsecureIgnoreHostKey: s.access.SSHInsecureIgnoreHostKey,
			Timeout:               connectTimeout,
		}

		memberClient, err := s.dialer.Dial(ctx, jumpOpts)
		if err != nil {
			jumpErr = err
		} else if jumper, ok := memberClient.(remote.Jumper); !ok {
			jumpErr = errors.New("member client cannot open jump channels")
			_ = memberClient.Close()
		} else {
			guestAddress := remote.JoinHostPort(guestAddr, req.SSHPort)
			raw, dialErr := jumper.DialTCP(ctx, guestAddress)
			if dialErr != nil {
				jumpErr = dialErr
				_ = memberClient.Close()
			} else {
				guestOpts.RawConn = raw
				guestClient, guestErr := s.dialer.Dial(ctx, guestOpts)
				if guestErr == nil {
					return guestClient, func() {
						_ = guestClient.Close()
						_ = memberClient.Close()
					}, "via member jump through " + memberHost, nil
				}

				jumpErr = guestErr
				_ = raw.Close()
				_ = memberClient.Close()
			}
		}
	}

	guestClient, err := s.dialer.Dial(ctx, guestOpts)
	if err != nil {
		if jumpErr != nil {
			return nil, func() {}, "", fmt.Errorf("guest ssh failed via member jump (%v) and directly (%w)", jumpErr, err)
		}
		return nil, func() {}, "", fmt.Errorf("guestagent: guest ssh %s: %w", guestOpts.Address, err)
	}

	return guestClient, func() { _ = guestClient.Close() }, "direct connection", nil
}

// canJump reports whether the node_access SSH identity is complete enough to
// reach the member host.
func (s *Service) canJump() bool {
	return s.access.SSHUser != "" && s.access.SSHKey != ""
}

// checkGuest makes sure the guest can actually run the installer.
func checkGuest(ctx context.Context, p *provisioner) error {
	out, err := p.client.Run(ctx, "ps -p 1 -o comm=")
	if err != nil {
		return fmt.Errorf("guestagent: detect init system: %w", err)
	}
	if pid1 := strings.TrimSpace(out); pid1 != "systemd" {
		return fmt.Errorf("guestagent: the guest init system is %q, not systemd; this installer only supports systemd guests", pid1)
	}

	out, err = p.client.Run(ctx, "grep -qw 9p /proc/filesystems && echo yes || echo no")
	if err != nil {
		return fmt.Errorf("guestagent: detect 9p support: %w", err)
	}
	if strings.TrimSpace(out) == "no" {
		return errors.New("guestagent: the guest kernel has no 9p support and cannot mount the agent config share")
	}

	return nil
}

// mountCommand renders the idempotent config-share mount.
func mountCommand() string {
	return "mkdir -p " + remote.ShellQuote(configMount) +
		" && (mountpoint -q " + remote.ShellQuote(configMount) +
		" || mount -t 9p config " + remote.ShellQuote(configMount) + ")"
}

// unmountCommand releases the config share without failing the run.
func unmountCommand() string {
	return "umount " + remote.ShellQuote(configMount) + " 2>/dev/null || true"
}

// verifyAgent waits for the agent to start reporting instance state, which is
// the first thing incusd learns once incus-agent connects.
func (s *Service) verifyAgent(ctx context.Context, client clusters.InstanceServer, name string) bool {
	deadline := time.Now().Add(s.verifyTimeout)

	for {
		state, _, err := client.GetInstanceState(name)
		if err == nil && state != nil && (state.OSInfo != nil || len(state.Network) > 0) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}

		select {
		case <-ctx.Done():
			return false
		case <-time.After(s.verifyInterval):
		}
	}
}

// provisioner runs privileged commands over the guest SSH connection,
// mirroring the node join escalation rules.
type provisioner struct {
	client       remote.Client
	user         string
	sudoPassword string
}

// priv runs one privileged command. Password mode uses `sudo -k -S` so a
// cached timestamp can never make sudo swallow the password line.
func (p *provisioner) priv(ctx context.Context, command string) (string, error) {
	bounded, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	if p.user == "root" {
		return p.client.Run(bounded, command)
	}
	if p.sudoPassword != "" {
		err := p.client.RunStdin(bounded, "sudo -k -S -p '' "+command, p.sudoPassword+"\n")
		return "", err
	}

	return p.client.Run(bounded, "sudo -n "+command)
}
