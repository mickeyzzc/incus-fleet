package guestagent

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/remote"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

// scripted maps a command substring to output; the longest matching needle
// wins so broad and specific keys can coexist.
type scripted struct {
	needle string
	output string
}

// fakeSSHClient records commands and returns scripted output. It implements
// remote.Client and the Jumper capability used by the member jump path.
type fakeSSHClient struct {
	mu            sync.Mutex
	calls         []string
	jumps         []string
	output        []scripted
	failOn        string
	jumpErr       error
	withoutJumper bool
}

func (c *fakeSSHClient) Run(_ context.Context, command string) (string, error) {
	c.mu.Lock()
	c.calls = append(c.calls, command)
	failOn := c.failOn
	output := c.output
	c.mu.Unlock()

	if failOn != "" && strings.Contains(command, failOn) {
		return "", errors.New("ssh command failed")
	}

	best := ""
	for _, item := range output {
		if strings.Contains(command, item.needle) && len(item.needle) > len(best) {
			best = item.needle
			for _, candidate := range output {
				if candidate.needle == best {
					return candidate.output, nil
				}
			}
		}
	}

	return "", nil
}

func (c *fakeSSHClient) RunStdin(_ context.Context, command string, stdin string) error {
	c.mu.Lock()
	c.calls = append(c.calls, command+" <<< "+stdin)
	c.mu.Unlock()

	return nil
}

// DialTCP implements remote.Jumper.
func (c *fakeSSHClient) DialTCP(_ context.Context, address string) (net.Conn, error) {
	c.mu.Lock()
	c.jumps = append(c.jumps, address)
	jumpErr := c.jumpErr
	c.mu.Unlock()

	if jumpErr != nil {
		return nil, jumpErr
	}

	client, server := net.Pipe()
	_ = server.Close()

	return client, nil
}

// bareClient wraps a fake without exposing the Jumper capability.
type bareClient struct{ inner *fakeSSHClient }

func (b *bareClient) Run(ctx context.Context, command string) (string, error) {
	return b.inner.Run(ctx, command)
}

func (b *bareClient) RunStdin(ctx context.Context, command string, stdin string) error {
	return b.inner.RunStdin(ctx, command, stdin)
}

func (b *bareClient) Close() error { return b.inner.Close() }

func (c *fakeSSHClient) Close() error { return nil }

func (c *fakeSSHClient) commands() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]string(nil), c.calls...)
}

func (c *fakeSSHClient) jumpTargets() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]string(nil), c.jumps...)
}

// fakeDialer routes dials by shape: key-based dials are the member jump,
// RawConn dials are the jump guest, password dials are direct guests.
type fakeDialer struct {
	mu        sync.Mutex
	dialCalls []remote.Options
	member    *fakeSSHClient
	guest     *fakeSSHClient
	memberErr error
	guestErr  error
}

func (d *fakeDialer) Dial(_ context.Context, opts remote.Options) (remote.Client, error) {
	d.mu.Lock()
	d.dialCalls = append(d.dialCalls, opts)
	memberErr, guestErr := d.memberErr, d.guestErr
	d.mu.Unlock()

	if opts.RawConn != nil {
		if guestErr != nil {
			return nil, guestErr
		}
		return d.guest, nil
	}
	if opts.KeyPath != "" {
		if memberErr != nil {
			return nil, memberErr
		}
		if d.member != nil && d.member.withoutJumper {
			return &bareClient{inner: d.member}, nil
		}
		return d.member, nil
	}

	if guestErr != nil {
		return nil, guestErr
	}
	return d.guest, nil
}

func (d *fakeDialer) calls() []remote.Options {
	d.mu.Lock()
	defer d.mu.Unlock()

	return append([]remote.Options(nil), d.dialCalls...)
}

type fixture struct {
	service *Service
	dialer  *fakeDialer
	guest   *fakeSSHClient
	member  *fakeSSHClient
	server  *incusfake.Server
}

func newFixture(t *testing.T, configure func(*incusfake.Server, *fakeSSHClient)) *fixture {
	t.Helper()

	guest := &fakeSSHClient{output: []scripted{
		{needle: "ps -p 1", output: "systemd\n"},
		{needle: "/proc/filesystems", output: "yes\n"},
	}}
	member := &fakeSSHClient{}
	dialer := &fakeDialer{member: member, guest: guest}

	server := &incusfake.Server{
		Clustered: true,
		Server: &api.Server{Environment: api.ServerEnvironment{
			ServerName:      "node-b",
			ServerVersion:   "7.0.2",
			ServerClustered: true,
		}},
		Members: []api.ClusterMember{{ServerName: "node-b", Status: "Online", URL: "https://198.51.100.10:8443"}},
		Instance: &api.Instance{
			Name:       "vm1",
			Type:       "virtual-machine",
			Status:     "Running",
			StatusCode: api.Running,
			Location:   "node-b",
			InstancePut: api.InstancePut{
				Config: api.ConfigMap{
					"volatile.eth0.hwaddr": "00:16:3e:aa:bb:01",
				},
				Devices: api.DevicesMap{
					"eth0": {"type": "nic", "network": "incusbr0"},
				},
			},
			ExpandedDevices: api.DevicesMap{
				"eth0": {"type": "nic", "network": "incusbr0"},
			},
		},
		NetworkLeases: []api.NetworkLease{
			{Hostname: "other", Hwaddr: "10:66:6a:aa:bb:cc", Address: "10.0.0.50", Type: "dynamic"},
			{Hostname: "vm1", Hwaddr: "00:16:3e:aa:bb:01", Address: "10.0.0.99", Type: "dynamic"},
		},
		State: &api.InstanceState{
			OSInfo: &api.InstanceStateOSInfo{OS: "Debian GNU/Linux", OSVersion: "13"},
			Network: map[string]api.InstanceStateNetwork{
				"eth0": {Addresses: []api.InstanceStateNetworkAddress{{Address: "10.0.0.99", Family: "inet", Scope: "global"}}},
			},
		},
	}
	if configure != nil {
		configure(server, guest)
	}

	cfgs := []config.Cluster{{
		Name:        "c1",
		DisplayName: "c1",
		Endpoint:    "https://198.51.100.10:8443",
	}}
	dial := func(context.Context, config.Cluster) (clusters.InstanceServer, error) { return server, nil }
	manager, err := clusters.NewWithDialer(context.Background(), cfgs, dial, testLogger())
	if err != nil {
		t.Fatal(err)
	}

	access := config.NodeAccess{
		SSHUser:                  "fleet",
		SSHKey:                   "/tmp/id_fleet",
		SSHInsecureIgnoreHostKey: true,
	}

	service := New(manager, access, testLogger(),
		WithDialer(dialer),
		WithVerify(40*time.Millisecond, 5*time.Millisecond),
	)

	return &fixture{service: service, dialer: dialer, guest: guest, member: member, server: server}
}

func TestInstallSudoModes(t *testing.T) {
	// A non-root user with a sudo password escalates through sudo -S.
	f := newFixture(t, nil)
	result, err := f.service.Install(context.Background(), "c1", "vm1", Request{
		SSHUser:      "ubuntu",
		SSHPassword:  "guest-secret",
		SudoPassword: "sudo-secret",
	})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	for _, step := range result.Steps {
		if step.Name == "verify" && step.Status != "ok" {
			t.Fatalf("verify step = %#v", step)
		}
	}
	joined := strings.Join(f.guest.commands(), "\n")
	if !strings.Contains(joined, "sudo -k -S") {
		t.Fatalf("sudo -S escalation missing: %s", joined)
	}
	if !strings.Contains(joined, "sudo-secret") {
		t.Fatal("sudo password must travel through stdin")
	}

	// Without a sudo password the escalation falls back to passwordless sudo.
	f = newFixture(t, nil)
	if _, err := f.service.Install(context.Background(), "c1", "vm1", Request{
		SSHUser:     "ubuntu",
		SSHPassword: "guest-secret",
	}); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if !strings.Contains(strings.Join(f.guest.commands(), "\n"), "sudo -n") {
		t.Fatal("sudo -n escalation missing")
	}
}

func TestInstallJumpWithoutJumper(t *testing.T) {
	f := newFixture(t, nil)
	f.dialer.member.withoutJumper = true

	result, err := f.service.Install(context.Background(), "c1", "vm1", Request{SSHUser: "root", Address: "10.0.0.99"})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	for _, step := range result.Steps {
		if step.Name == "connect" && step.Detail != "direct connection" {
			t.Fatalf("connect detail = %q", step.Detail)
		}
	}
}

func TestInstallJumpChannelFailure(t *testing.T) {
	f := newFixture(t, nil)
	f.dialer.member.jumpErr = errors.New("channel open refused")

	result, err := f.service.Install(context.Background(), "c1", "vm1", Request{SSHUser: "root", Address: "10.0.0.99"})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	for _, step := range result.Steps {
		if step.Name == "connect" && step.Detail != "direct connection" {
			t.Fatalf("connect detail = %q", step.Detail)
		}
	}
}

func TestInstallDirectWithoutMembers(t *testing.T) {
	f := newFixture(t, func(server *incusfake.Server, _ *fakeSSHClient) {
		server.MembersErr = errors.New("standalone")
	})

	result, err := f.service.Install(context.Background(), "c1", "vm1", Request{SSHUser: "root", Address: "10.0.0.99"})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	for _, step := range result.Steps {
		if step.Name == "connect" && step.Detail != "direct connection" {
			t.Fatalf("connect detail = %q", step.Detail)
		}
	}
}

func TestCheckGuestCommandFailures(t *testing.T) {
	f := newFixture(t, func(_ *incusfake.Server, guest *fakeSSHClient) {
		guest.failOn = "ps -p 1"
	})
	result, err := f.service.Install(context.Background(), "c1", "vm1", Request{SSHUser: "root"})
	if err == nil || !strings.Contains(err.Error(), "detect init system") {
		t.Fatalf("Install() error = %v", err)
	}
	last := result.Steps[len(result.Steps)-1]
	if last.Name != "check-guest" || last.Status != "failed" {
		t.Fatalf("last step = %#v", last)
	}

	f = newFixture(t, func(_ *incusfake.Server, guest *fakeSSHClient) {
		guest.failOn = "/proc/filesystems"
	})
	_, err = f.service.Install(context.Background(), "c1", "vm1", Request{SSHUser: "root"})
	if err == nil || !strings.Contains(err.Error(), "detect 9p support") {
		t.Fatalf("Install() error = %v", err)
	}
}

func TestResolveAddressSkipsLeaseErrors(t *testing.T) {
	f := newFixture(t, func(server *incusfake.Server, _ *fakeSSHClient) {
		server.NetworkLeasesErr = errors.New("leases unavailable")
	})

	_, err := f.service.Install(context.Background(), "c1", "vm1", Request{SSHUser: "root"})
	if !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), "no DHCP leases") {
		t.Fatalf("Install() error = %v", err)
	}
}

func TestResolveAddressMatchesParentNetwork(t *testing.T) {
	f := newFixture(t, func(server *incusfake.Server, _ *fakeSSHClient) {
		server.Instance.Devices = api.DevicesMap{
			"eth0": {"type": "nic", "nictype": "bridged", "parent": "incusbr0"},
		}
		server.Instance.ExpandedDevices = api.DevicesMap{
			"eth0": {"type": "nic", "nictype": "bridged", "parent": "incusbr0"},
		}
	})

	result, err := f.service.Install(context.Background(), "c1", "vm1", Request{SSHUser: "root"})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	for _, step := range result.Steps {
		if step.Name == "resolve-address" && !strings.Contains(step.Detail, "10.0.0.99") {
			t.Fatalf("resolve detail = %q", step.Detail)
		}
	}
}

func TestVerifyAgentStopsOnContextCancel(t *testing.T) {
	f := newFixture(t, func(server *incusfake.Server, _ *fakeSSHClient) {
		server.State = &api.InstanceState{Status: "Running"}
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := f.service.Install(ctx, "c1", "vm1", Request{SSHUser: "root", Address: "10.0.0.99"})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	last := result.Steps[len(result.Steps)-1]
	if last.Name != "verify" || last.Status != "warn" {
		t.Fatalf("last step = %#v", last)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestInstallSuccessViaJump(t *testing.T) {
	f := newFixture(t, nil)

	result, err := f.service.Install(context.Background(), "c1", "vm1", Request{
		SSHUser:     "root",
		SSHPassword: "guest-secret",
	})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	var names []string
	for _, step := range result.Steps {
		names = append(names, step.Name+"="+step.Status)
	}
	want := "validate=ok,resolve-address=ok,connect=ok,check-guest=ok,mount-config=ok,install=ok,cleanup=ok,verify=ok"
	if strings.Join(names, ",") != want {
		t.Fatalf("steps = %s", strings.Join(names, ","))
	}

	// The jump went through the member; the guest handshake carried the user
	// credentials over the member channel.
	calls := f.dialer.calls()
	if len(calls) != 2 {
		t.Fatalf("dials = %d, want 2", len(calls))
	}
	if calls[0].KeyPath == "" || calls[0].User != "fleet" {
		t.Fatalf("member dial options = %#v", calls[0])
	}
	if calls[0].Address != "198.51.100.10:22" {
		t.Fatalf("member jump address = %q", calls[0].Address)
	}
	if calls[1].RawConn == nil || calls[1].User != "root" || calls[1].Password != "guest-secret" {
		t.Fatalf("guest dial options = %#v", calls[1])
	}
	if jumps := f.member.jumpTargets(); len(jumps) != 1 || jumps[0] != "10.0.0.99:22" {
		t.Fatalf("jump targets = %#v", jumps)
	}

	// The lease resolver picked the MAC-matched lease, not the first one.
	for _, step := range result.Steps {
		if step.Name == "resolve-address" && !strings.Contains(step.Detail, "10.0.0.99") {
			t.Fatalf("resolve detail = %q", step.Detail)
		}
	}

	joined := strings.Join(f.guest.commands(), "\n")
	for _, want := range []string{"mount -t 9p config", "install.sh", "umount", "systemctl restart incus-agent"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("guest commands missing %q: %s", want, joined)
		}
	}
}

func TestInstallRejectsContainer(t *testing.T) {
	f := newFixture(t, func(server *incusfake.Server, _ *fakeSSHClient) {
		server.Instance.Type = "container"
	})

	_, err := f.service.Install(context.Background(), "c1", "vm1", Request{SSHUser: "root"})
	if !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), "not a virtual machine") {
		t.Fatalf("Install() error = %v", err)
	}
	if len(f.dialer.calls()) != 0 {
		t.Fatal("containers must not reach any host")
	}
}

func TestInstallRequiresRunning(t *testing.T) {
	f := newFixture(t, func(server *incusfake.Server, _ *fakeSSHClient) {
		server.Instance.Status = "Stopped"
		server.Instance.StatusCode = api.Stopped
	})

	_, err := f.service.Install(context.Background(), "c1", "vm1", Request{SSHUser: "root"})
	if !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), "running") {
		t.Fatalf("Install() error = %v", err)
	}
}

func TestInstallRejectsInvalidRequests(t *testing.T) {
	f := newFixture(t, nil)

	cases := []Request{
		{SSHPort: 70000},
		{SSHUser: "root", SSHPassword: "bad\npassword"},
		{SSHUser: "root", SudoPassword: strings.Repeat("x", 2000)},
	}
	for _, req := range cases {
		if _, err := f.service.Install(context.Background(), "c1", "vm1", req); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("Install(%#v) error = %v, want ErrInvalidRequest", req, err)
		}
	}
	if len(f.dialer.calls()) != 0 {
		t.Fatal("invalid requests must not reach any host")
	}
}

func TestInstallRequiresSystemdGuest(t *testing.T) {
	f := newFixture(t, func(_ *incusfake.Server, guest *fakeSSHClient) {
		guest.output = []scripted{
			{needle: "ps -p 1", output: "init\n"},
		}
	})

	result, err := f.service.Install(context.Background(), "c1", "vm1", Request{SSHUser: "root"})
	if err == nil || !strings.Contains(err.Error(), "systemd") {
		t.Fatalf("Install() error = %v", err)
	}
	last := result.Steps[len(result.Steps)-1]
	if last.Name != "check-guest" || last.Status != "failed" {
		t.Fatalf("last step = %#v", last)
	}
}

func TestInstallMountFailure(t *testing.T) {
	f := newFixture(t, func(_ *incusfake.Server, guest *fakeSSHClient) {
		guest.failOn = "mount -t 9p"
	})

	result, err := f.service.Install(context.Background(), "c1", "vm1", Request{SSHUser: "root"})
	if err == nil || !strings.Contains(err.Error(), "mount config share") {
		t.Fatalf("Install() error = %v", err)
	}
	last := result.Steps[len(result.Steps)-1]
	if last.Name != "mount-config" || last.Status != "failed" {
		t.Fatalf("last step = %#v", last)
	}
}

func TestInstallRunsScriptFailureUnmountsShare(t *testing.T) {
	f := newFixture(t, func(_ *incusfake.Server, guest *fakeSSHClient) {
		guest.failOn = "install.sh"
	})

	result, err := f.service.Install(context.Background(), "c1", "vm1", Request{SSHUser: "root"})
	if err == nil || !strings.Contains(err.Error(), "install.sh") {
		t.Fatalf("Install() error = %v", err)
	}
	last := result.Steps[len(result.Steps)-1]
	if last.Name != "install" || last.Status != "failed" {
		t.Fatalf("last step = %#v", last)
	}

	// The cleanup unmount still ran after the failed install.
	joined := strings.Join(f.guest.commands(), "\n")
	if !strings.Contains(joined, "umount") {
		t.Fatal("failed install must still unmount the config share")
	}
}

func TestInstallFallsBackToDirectDial(t *testing.T) {
	f := newFixture(t, nil)
	f.dialer.memberErr = errors.New("member unreachable")

	result, err := f.service.Install(context.Background(), "c1", "vm1", Request{SSHUser: "root", Address: "10.0.0.99"})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	calls := f.dialer.calls()
	if len(calls) != 2 {
		t.Fatalf("dials = %d, want jump then direct", len(calls))
	}
	if calls[1].RawConn != nil || calls[1].KeyPath != "" {
		t.Fatalf("fallback dial options = %#v", calls[1])
	}
	for _, step := range result.Steps {
		if step.Name == "connect" && step.Detail != "direct connection" {
			t.Fatalf("connect detail = %q", step.Detail)
		}
	}
}

func TestInstallReportsBothFailures(t *testing.T) {
	f := newFixture(t, nil)
	f.dialer.memberErr = errors.New("member unreachable")
	f.dialer.guestErr = errors.New("guest refused")

	_, err := f.service.Install(context.Background(), "c1", "vm1", Request{SSHUser: "root", Address: "10.0.0.99"})
	if err == nil || !strings.Contains(err.Error(), "member jump") || !strings.Contains(err.Error(), "guest refused") {
		t.Fatalf("Install() error = %v", err)
	}
}

func TestInstallResolvesLeaseCandidates(t *testing.T) {
	f := newFixture(t, nil)

	// Empty MAC list and empty address: resolution must fail with candidates.
	f.server.Instance.Config = api.ConfigMap{}
	f.server.Instance.ExpandedDevices = api.DevicesMap{
		"eth0": {"type": "nic", "network": "incusbr0"},
	}

	_, err := f.service.Install(context.Background(), "c1", "vm1", Request{SSHUser: "root"})
	if !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), "manually") {
		t.Fatalf("Install() error = %v", err)
	}
}

func TestInstallVerifyWarnsWhenAgentSilent(t *testing.T) {
	f := newFixture(t, func(server *incusfake.Server, _ *fakeSSHClient) {
		server.State = &api.InstanceState{Status: "Running"}
	})

	result, err := f.service.Install(context.Background(), "c1", "vm1", Request{SSHUser: "root"})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	last := result.Steps[len(result.Steps)-1]
	if last.Name != "verify" || last.Status != "warn" {
		t.Fatalf("last step = %#v", last)
	}
}

func TestInstallUnknownInstance(t *testing.T) {
	f := newFixture(t, nil)

	_, err := f.service.Install(context.Background(), "c1", "missing", Request{SSHUser: "root"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Install() error = %v, want ErrNotFound", err)
	}
}
