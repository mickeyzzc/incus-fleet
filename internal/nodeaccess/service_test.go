package nodeaccess

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/hostops"
	"github.com/mickeyzzc/incus-fleet/internal/nodenet"
	"github.com/mickeyzzc/incus-fleet/internal/remote"
)

type fakeLookup struct {
	summaries map[string]clusters.Summary
	err       error
}

func (f fakeLookup) Get(name string) (clusters.Summary, error) {
	if f.err != nil {
		return clusters.Summary{}, f.err
	}

	summary, ok := f.summaries[name]
	if !ok {
		return clusters.Summary{}, clusters.ErrUnknownCluster
	}

	return summary, nil
}

// fakeRunner drives a local nodenet service.
type fakeRunner struct {
	mu      sync.Mutex
	outputs map[string]string
	calls   []string
	err     error
}

func (f *fakeRunner) Run(name string, args ...string) (string, error) {
	call := name + " " + strings.Join(args, " ")
	f.mu.Lock()
	f.calls = append(f.calls, call)
	err := f.err
	f.mu.Unlock()

	if err != nil {
		return "", err
	}
	if out, ok := f.outputs[call]; ok {
		return out, nil
	}

	return "", nil
}

func (f *fakeRunner) RunStdin(name string, args []string, _ string) error {
	f.mu.Lock()
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	err := f.err
	f.mu.Unlock()

	return err
}

// fakeClient is a scripted remote.Client.
type fakeClient struct {
	mu      sync.Mutex
	calls   []string
	stdin   []string
	outputs map[string]string
	failOn  string
	closed  bool
}

func (c *fakeClient) Run(_ context.Context, command string) (string, error) {
	c.mu.Lock()
	c.calls = append(c.calls, command)
	failOn := c.failOn
	c.mu.Unlock()

	if failOn != "" && strings.Contains(command, failOn) {
		return "", errors.New("command failed")
	}

	best := ""
	output := ""
	for needle, candidate := range c.outputs {
		if strings.Contains(command, needle) && len(needle) > len(best) {
			best = needle
			output = candidate
		}
	}
	if best != "" {
		return output, nil
	}

	return "", nil
}

func (c *fakeClient) RunStdin(_ context.Context, command string, stdin string) error {
	c.mu.Lock()
	c.calls = append(c.calls, command)
	c.stdin = append(c.stdin, stdin)
	failOn := c.failOn
	c.mu.Unlock()

	if failOn != "" && strings.Contains(command, failOn) {
		return errors.New("command failed")
	}

	return nil
}

func (c *fakeClient) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()

	return nil
}

type fakeDialer struct {
	mu      sync.Mutex
	options []remote.Options
	client  *fakeClient
	err     error
	calls   int
}

func (d *fakeDialer) Dial(_ context.Context, opts remote.Options) (remote.Client, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	d.options = append(d.options, opts)
	if d.err != nil {
		return nil, d.err
	}
	if d.client == nil {
		d.client = &fakeClient{outputs: map[string]string{}}
	}

	return d.client, nil
}

func testAccess() config.NodeAccess {
	return config.NodeAccess{
		SSHUser:                  "node-admin",
		SSHKey:                   "/home/incus/.ssh/id_ed25519",
		SSHPort:                  22,
		SSHInsecureIgnoreHostKey: true,
	}
}

func testLookup() fakeLookup {
	return fakeLookup{summaries: map[string]clusters.Summary{
		"c1": {
			Name: "c1",
			Members: []clusters.Member{
				{Name: "n1", URL: "https://192.0.2.10:8443", Status: "Online"},
				{Name: "n2", URL: "https://198.51.100.11:8443", Status: "Online"},
			},
		},
	}}
}

func TestLocalMemberUsesLocalRunner(t *testing.T) {
	runner := &fakeRunner{
		outputs: map[string]string{
			"ip -j addr show":                     `[{"ifname":"eth0","operstate":"UP","flags":["UP"],"addr_info":[]}]`,
			"ip -j route show":                    `[]`,
			"nft -a list table inet fleet_filter": "",
			"hostname ":                           "n1",
		},
	}
	lookup := testLookup()
	service := New(lookup, nodenet.NewService(runner), testAccess(), WithLocalMember("n1"), WithDialer(&fakeDialer{}))

	snapshot, err := service.Snapshot(context.Background(), "c1", "n1", "")
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.Host != "n1" || len(snapshot.NICs) != 1 {
		t.Fatalf("Snapshot() = %#v", snapshot)
	}
	if len(runner.calls) == 0 {
		t.Fatal("local runner was not used")
	}
}

func TestRemoteMemberDelegatesAllOperations(t *testing.T) {
	client := &fakeClient{outputs: map[string]string{
		"'-j' 'addr' 'show'":  `[{"ifname":"eth0","operstate":"UP","flags":["UP"],"addr_info":[]}]`,
		"'-j' 'route' 'show'": `[{"dst":"default","gateway":"192.0.2.1","dev":"eth0"}]`,
		"'list'":              "",
		"'hostname'":          "n2",
		"'iw'":                "",
		"'ip'":                "",
		"'nft'":               "",
		"'dhcpcd'":            "",
		"'tee'":               "",
		"'wpa_supplicant'":    "",
	}}
	dialer := &fakeDialer{client: client}
	lookup := testLookup()
	service := New(lookup, nodenet.NewService(&fakeRunner{}), testAccess(), WithLocalMember("n1"), WithDialer(dialer))

	ctx := context.Background()
	if _, err := service.Snapshot(ctx, "c1", "n2", ""); err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if err := service.SetNICState("c1", "n2", "eth0", false); err != nil {
		t.Fatalf("SetNICState() error = %v", err)
	}
	if err := service.ApplyAddress(ctx, "c1", "n2", "eth0", nodenet.AddressRequest{
		Address: "10.0.0.50/24",
		Gateway: "10.0.0.1",
		Persist: true,
	}); err != nil {
		t.Fatalf("ApplyAddress() error = %v", err)
	}
	if err := service.WifiConnect("c1", "n2", "wlan0", "Office", "secret"); err != nil {
		t.Fatalf("WifiConnect() error = %v", err)
	}
	if _, err := service.ListRoutes("c1", "n2"); err != nil {
		t.Fatalf("ListRoutes() error = %v", err)
	}
	if err := service.AddRoute("c1", "n2", "203.0.113.0/24", "192.0.2.1", "eth0"); err != nil {
		t.Fatalf("AddRoute() error = %v", err)
	}
	if err := service.DeleteRoute("c1", "n2", "203.0.113.0/24"); err != nil {
		t.Fatalf("DeleteRoute() error = %v", err)
	}
	if _, err := service.ListFirewall("c1", "n2"); err != nil {
		t.Fatalf("ListFirewall() error = %v", err)
	}
	if err := service.AddFirewallRule("c1", "n2", nodenet.FirewallRequest{Port: 22, Proto: "tcp", Action: "accept"}); err != nil {
		t.Fatalf("AddFirewallRule() error = %v", err)
	}
	if err := service.DeleteFirewallRule("c1", "n2", "7"); err != nil {
		t.Fatalf("DeleteFirewallRule() error = %v", err)
	}

	dialer.mu.Lock()
	options := append([]remote.Options(nil), dialer.options...)
	dialer.mu.Unlock()
	if len(options) == 0 {
		t.Fatal("dialer was not used")
	}
	if options[0].Address != "198.51.100.11:22" || options[0].User != "node-admin" {
		t.Fatalf("dial options = %#v", options[0])
	}

	client.mu.Lock()
	calls := strings.Join(client.calls, "\n")
	client.mu.Unlock()
	for _, want := range []string{
		`command -v 'ip'`,
		`sudo -n "$bin" '-j' 'addr' 'show'`,
		`sudo -n "$bin" 'link' 'set' 'dev' 'eth0' 'down'`,
		`sudo -n "$bin" 'addr' 'add'`,
		`sudo -n "$bin" 'route' 'add' '203.0.113.0/24'`,
		`sudo -n "$bin" 'delete' 'rule'`,
	} {
		if !strings.Contains(calls, want) {
			t.Fatalf("commands missing %q:\n%s", want, calls)
		}
	}
}

func TestServiceResolutionErrors(t *testing.T) {
	lookup := testLookup()
	service := New(lookup, nodenet.NewService(&fakeRunner{}), testAccess(), WithLocalMember("n1"), WithDialer(&fakeDialer{}))

	if _, _, _, err := service.Service("nope", "n1"); !errors.Is(err, clusters.ErrUnknownCluster) {
		t.Fatalf("unknown cluster error = %v", err)
	}
	if _, _, _, err := service.Service("c1", "ghost"); !errors.Is(err, ErrUnknownMember) {
		t.Fatalf("unknown member error = %v", err)
	}

	disabled := New(lookup, nodenet.NewService(&fakeRunner{}), config.NodeAccess{}, WithLocalMember("n1"))
	if _, _, _, err := disabled.Service("c1", "n2"); !errors.Is(err, nodenet.ErrUnavailable) {
		t.Fatalf("disabled access error = %v", err)
	}

	badURL := fakeLookup{summaries: map[string]clusters.Summary{
		"c1": {Members: []clusters.Member{{Name: "n2", URL: "https://host/path"}}},
	}}
	badService := New(badURL, nodenet.NewService(&fakeRunner{}), testAccess(), WithLocalMember("n1"))
	if _, _, _, err := badService.Service("c1", "n2"); !errors.Is(err, nodenet.ErrUnavailable) {
		t.Fatalf("bad URL error = %v", err)
	}

	noLookup := New(nil, nodenet.NewService(&fakeRunner{}), testAccess())
	if _, _, _, err := noLookup.Service("c1", "n1"); !errors.Is(err, nodenet.ErrUnavailable) {
		t.Fatalf("nil lookup error = %v", err)
	}
}

func TestAllOperationsRejectUnknownCluster(t *testing.T) {
	service := New(testLookup(), nodenet.NewService(&fakeRunner{}), testAccess(), WithLocalMember("n1"), WithDialer(&fakeDialer{}))

	ctx := context.Background()
	if _, err := service.Snapshot(ctx, "nope", "n1", ""); !errors.Is(err, clusters.ErrUnknownCluster) {
		t.Fatalf("Snapshot error = %v", err)
	}
	if err := service.SetNICState("nope", "n1", "eth0", true); !errors.Is(err, clusters.ErrUnknownCluster) {
		t.Fatalf("SetNICState error = %v", err)
	}
	if err := service.ApplyAddress(ctx, "nope", "n1", "eth0", nodenet.AddressRequest{DHCP: true}); !errors.Is(err, clusters.ErrUnknownCluster) {
		t.Fatalf("ApplyAddress error = %v", err)
	}
	if err := service.WifiConnect("nope", "n1", "wlan0", "ssid", "pw"); !errors.Is(err, clusters.ErrUnknownCluster) {
		t.Fatalf("WifiConnect error = %v", err)
	}
	if _, err := service.ListRoutes("nope", "n1"); !errors.Is(err, clusters.ErrUnknownCluster) {
		t.Fatalf("ListRoutes error = %v", err)
	}
	if err := service.AddRoute("nope", "n1", "203.0.113.0/24", "192.0.2.1", "eth0"); !errors.Is(err, clusters.ErrUnknownCluster) {
		t.Fatalf("AddRoute error = %v", err)
	}
	if err := service.DeleteRoute("nope", "n1", "203.0.113.0/24"); !errors.Is(err, clusters.ErrUnknownCluster) {
		t.Fatalf("DeleteRoute error = %v", err)
	}
	if _, err := service.ListFirewall("nope", "n1"); !errors.Is(err, clusters.ErrUnknownCluster) {
		t.Fatalf("ListFirewall error = %v", err)
	}
	if err := service.AddFirewallRule("nope", "n1", nodenet.FirewallRequest{Port: 22, Proto: "tcp", Action: "accept"}); !errors.Is(err, clusters.ErrUnknownCluster) {
		t.Fatalf("AddFirewallRule error = %v", err)
	}
	if err := service.DeleteFirewallRule("nope", "n1", "7"); !errors.Is(err, clusters.ErrUnknownCluster) {
		t.Fatalf("DeleteFirewallRule error = %v", err)
	}
}

func TestRemoteRunnerFailureAndReconnect(t *testing.T) {
	client := &fakeClient{outputs: map[string]string{}, failOn: "'addr' 'show'"}
	dialer := &fakeDialer{client: client}
	lookup := testLookup()
	service := New(lookup, nodenet.NewService(&fakeRunner{}), testAccess(), WithLocalMember("n1"), WithDialer(dialer))

	_, err := service.Snapshot(context.Background(), "c1", "n2", "")
	if !errors.Is(err, nodenet.ErrUnavailable) {
		t.Fatalf("Snapshot() error = %v, want ErrUnavailable", err)
	}

	client.mu.Lock()
	closed := client.closed
	client.mu.Unlock()
	if !closed {
		t.Fatal("failed command did not close the SSH client")
	}

	// A second call redials after the failure.
	_, _ = service.ListRoutes("c1", "n2")
	dialer.mu.Lock()
	calls := dialer.calls
	dialer.mu.Unlock()
	if calls < 2 {
		t.Fatalf("dialer calls = %d, want at least 2", calls)
	}
}

func TestRunStdinFailure(t *testing.T) {
	client := &fakeClient{outputs: map[string]string{"'wpa_supplicant'": ""}, failOn: "'tee'"}
	dialer := &fakeDialer{client: client}
	service := New(testLookup(), nodenet.NewService(&fakeRunner{}), testAccess(), WithLocalMember("n1"), WithDialer(dialer))

	if err := service.WifiConnect("c1", "n2", "wlan0", "Office", "secret"); !errors.Is(err, nodenet.ErrUnavailable) {
		t.Fatalf("WifiConnect() error = %v", err)
	}
}

func TestDialFailureSurfacesAsUnavailable(t *testing.T) {
	dialer := &fakeDialer{err: errors.New("network down")}
	lookup := testLookup()
	service := New(lookup, nodenet.NewService(&fakeRunner{}), testAccess(), WithLocalMember("n1"), WithDialer(dialer))

	if _, err := service.ListRoutes("c1", "n2"); !errors.Is(err, nodenet.ErrUnavailable) {
		t.Fatalf("ListRoutes() error = %v", err)
	}
}

func TestRootUserSkipsSudoAndRunStdin(t *testing.T) {
	client := &fakeClient{outputs: map[string]string{"tee": ""}}
	dialer := &fakeDialer{client: client}
	lookup := testLookup()
	access := testAccess()
	access.SSHUser = "root"
	service := New(lookup, nodenet.NewService(&fakeRunner{}), access, WithLocalMember("n1"), WithDialer(dialer))

	if err := service.WifiConnect("c1", "n2", "wlan0", "Office", "secret"); err != nil {
		t.Fatalf("WifiConnect() error = %v", err)
	}

	client.mu.Lock()
	calls := strings.Join(client.calls, "\n")
	stdin := client.stdin
	client.mu.Unlock()
	if strings.Contains(calls, "sudo") {
		t.Fatalf("root command used sudo:\n%s", calls)
	}
	if !strings.Contains(calls, `"$bin" '/etc/wpa_supplicant/fleet.conf'`) {
		t.Fatalf("wifi command missing tee:\n%s", calls)
	}
	if len(stdin) == 0 || !strings.Contains(stdin[0], "ssid=") {
		t.Fatalf("wpa config stdin = %#v", stdin)
	}
}

func TestResolveLocalBackend(t *testing.T) {
	service := New(testLookup(), nodenet.NewService(&fakeRunner{}), testAccess(), WithLocalMember("n1"))

	operator, backend, closeNode, err := service.Resolve(context.Background(), "c1", "n1", hostops.Write)
	if err != nil {
		t.Fatalf("Resolve(local) error = %v", err)
	}
	defer closeNode()
	if operator == nil || backend != hostops.BackendLocal {
		t.Fatalf("Resolve(local) = %v, %q", operator, backend)
	}
}

func TestResolveSSHWriteFallback(t *testing.T) {
	access := testAccess()
	disabled := false
	access.SSHWriteFallback = &disabled

	service := New(testLookup(), nodenet.NewService(&fakeRunner{}), access, WithLocalMember("n1"), WithDialer(&fakeDialer{}))

	_, backend, closeNode, err := service.Resolve(context.Background(), "c1", "n2", hostops.Read)
	if err != nil {
		t.Fatalf("Resolve(read) error = %v", err)
	}
	closeNode()
	if backend != hostops.BackendSSH {
		t.Fatalf("Resolve(read) backend = %q", backend)
	}

	if _, _, _, err := service.Resolve(context.Background(), "c1", "n2", hostops.Write); !errors.Is(err, hostops.ErrSSHWriteDisabled) {
		t.Fatalf("Resolve(write) error = %v", err)
	}
}

func TestResolveExplicitSSHModeAllowsWrite(t *testing.T) {
	access := testAccess()
	access.Mode = "ssh"
	disabled := false
	access.SSHWriteFallback = &disabled

	service := New(testLookup(), nodenet.NewService(&fakeRunner{}), access, WithLocalMember("n1"), WithDialer(&fakeDialer{}))

	_, backend, closeNode, err := service.Resolve(context.Background(), "c1", "n2", hostops.Write)
	if err != nil {
		t.Fatalf("Resolve(ssh write) error = %v", err)
	}
	defer closeNode()
	if backend != hostops.BackendSSH {
		t.Fatalf("Resolve(ssh write) backend = %q", backend)
	}
}

func TestResolveAgentModeUnavailable(t *testing.T) {
	access := testAccess()
	access.Mode = "agent"

	service := New(testLookup(), nodenet.NewService(&fakeRunner{}), access, WithLocalMember("n1"), WithDialer(&fakeDialer{}))

	if _, _, _, err := service.Resolve(context.Background(), "c1", "n2", hostops.Read); !errors.Is(err, hostops.ErrAgentUnavailable) {
		t.Fatalf("Resolve(agent) error = %v", err)
	}
}

func TestResolveLocalModeRejectsRemote(t *testing.T) {
	access := testAccess()
	access.Mode = "local"

	service := New(testLookup(), nodenet.NewService(&fakeRunner{}), access, WithLocalMember("n1"), WithDialer(&fakeDialer{}))

	if _, _, _, err := service.Resolve(context.Background(), "c1", "n2", hostops.Read); !errors.Is(err, nodenet.ErrUnavailable) {
		t.Fatalf("Resolve(local remote) error = %v", err)
	}
}

func TestResolveUnknownMember(t *testing.T) {
	service := New(testLookup(), nodenet.NewService(&fakeRunner{}), testAccess(), WithLocalMember("n1"))

	if _, _, _, err := service.Resolve(context.Background(), "c1", "missing", hostops.Read); !errors.Is(err, ErrUnknownMember) {
		t.Fatalf("Resolve(unknown) error = %v", err)
	}
}
