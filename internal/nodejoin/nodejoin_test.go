package nodejoin

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lxc/incus/v7/shared/api"
	"golang.org/x/crypto/ssh"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/remote"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

const testCertPEM = `-----BEGIN CERTIFICATE-----
MIIBszCCAVmgAwIBAgIUTEST
-----END CERTIFICATE-----
`

// scripted maps a command substring to output. The longest matching needle
// wins so broad and specific keys can coexist.
type scripted struct {
	needle string
	output string
}

type fakeSSHClient struct {
	mu       sync.Mutex
	calls    []string
	stdins   []string
	outputs  []scripted
	versions []string
	failOn   string
	onRun    func(command string)
}

func (c *fakeSSHClient) Run(ctx context.Context, command string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	c.mu.Lock()
	c.calls = append(c.calls, command)
	failOn := c.failOn
	onRun := c.onRun
	c.mu.Unlock()

	if onRun != nil {
		onRun(command)
	}
	if failOn != "" && strings.Contains(command, failOn) {
		return "", errors.New("ssh command failed")
	}

	if strings.Contains(command, "incus --version") {
		c.mu.Lock()
		defer c.mu.Unlock()
		if len(c.versions) == 0 {
			return "", nil
		}
		version := c.versions[0]
		c.versions = c.versions[1:]
		return version, nil
	}

	return c.lookup(command), nil
}

func (c *fakeSSHClient) RunStdin(ctx context.Context, command string, stdin string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	c.mu.Lock()
	c.calls = append(c.calls, command)
	c.stdins = append(c.stdins, stdin)
	failOn := c.failOn
	onRun := c.onRun
	c.mu.Unlock()

	if onRun != nil {
		onRun(command)
	}
	if failOn != "" && strings.Contains(command, failOn) {
		return errors.New("ssh command failed")
	}

	return nil
}

func (c *fakeSSHClient) lookup(command string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	best := ""
	output := ""
	for _, item := range c.outputs {
		if strings.Contains(command, item.needle) && len(item.needle) > len(best) {
			best = item.needle
			output = item.output
		}
	}

	return output
}

func (c *fakeSSHClient) Close() error { return nil }

func (c *fakeSSHClient) commandLog() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return strings.Join(c.calls, "\n")
}

func (c *fakeSSHClient) stdinLog() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]string(nil), c.stdins...)
}

type fakeSSHDialer struct {
	client *fakeSSHClient
	err    error
	opts   remote.Options
}

func (d *fakeSSHDialer) Dial(_ context.Context, opts remote.Options) (remote.Client, error) {
	d.opts = opts
	if d.err != nil {
		return nil, d.err
	}

	return d.client, nil
}

func newTestKey(t *testing.T, dir string) string {
	t.Helper()

	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	block, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

type joinFixture struct {
	service *Service
	client  *fakeSSHClient
	server  *incusfake.Server
	manager *clusters.Manager
	joined  *atomic.Bool
	keyPath string
}

func newJoinFixture(t *testing.T, configure func(*incusfake.Server)) *joinFixture {
	t.Helper()

	dir := t.TempDir()
	certPath := filepath.Join(dir, "cluster.crt")
	if err := os.WriteFile(certPath, []byte(testCertPEM), 0o600); err != nil {
		t.Fatal(err)
	}
	keyPath := newTestKey(t, dir)

	joined := &atomic.Bool{}
	server := &incusfake.Server{
		Clustered: true,
		Server: &api.Server{Environment: api.ServerEnvironment{
			ServerName:      "node-b",
			ServerVersion:   "7.0.2",
			ServerClustered: true,
		}},
		Members: []api.ClusterMember{{ServerName: "node-b", Status: "Online", URL: "https://198.51.100.10:8443"}},
	}
	server.MembersFunc = func() ([]api.ClusterMember, error) {
		members := []api.ClusterMember{{ServerName: "node-b", Status: "Online", URL: "https://198.51.100.10:8443"}}
		if joined.Load() {
			members = append(members, api.ClusterMember{
				ServerName:   "node-c",
				Status:       "Online",
				URL:          "https://198.51.100.11:8443",
				Architecture: "x86_64",
			})
		}
		return members, nil
	}
	if configure != nil {
		configure(server)
	}

	cfgs := []config.Cluster{{
		Name:          "c2",
		DisplayName:   "c2",
		Endpoint:      "https://198.51.100.10:8443",
		Project:       "default",
		TLSServerCert: certPath,
	}}
	dial := func(context.Context, config.Cluster) (clusters.InstanceServer, error) { return server, nil }
	manager, err := clusters.NewWithDialer(context.Background(), cfgs, dial, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)

	client := &fakeSSHClient{
		outputs: []scripted{
			{needle: "hostname -s", output: "node-c\n"},
			{needle: "/etc/os-release", output: "trixie"},
			{needle: "dpkg --print-architecture", output: "amd64"},
			{needle: "incus query /1.0", output: ""},
		},
		versions: []string{"", "7.0.2"},
		onRun: func(command string) {
			if strings.Contains(command, "query -X PUT") {
				joined.Store(true)
			}
		},
	}
	dialer := &fakeSSHDialer{client: client}

	access := config.NodeAccess{
		SSHUser:                  "node-admin",
		SSHKey:                   keyPath,
		SSHPort:                  22,
		SSHInsecureIgnoreHostKey: true,
	}
	service := New(manager, access, testLogger(),
		WithDialer(dialer),
		WithFetch(func(context.Context, string) ([]byte, error) { return []byte("KEY"), nil }),
		WithWait(300*time.Millisecond, 10*time.Millisecond),
		WithReadyTimeout(30*time.Millisecond),
	)

	return &joinFixture{
		service: service,
		client:  client,
		server:  server,
		manager: manager,
		joined:  joined,
		keyPath: keyPath,
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestJoinInstallsAndJoinsCluster(t *testing.T) {
	f := newJoinFixture(t, nil)

	result, err := f.service.Join(context.Background(), "c2", Request{
		Address:       "198.51.100.11",
		SSHUser:       "node-admin",
		SSHPassword:   "ssh-secret",
		SudoPassword:  "sudo-secret",
		InstallIncus:  true,
		InstallVMs:    true,
		IgnoreHostKey: true,
		StorageSource: "/srv/incus",
	})
	if err != nil {
		t.Fatalf("Join() error = %v", err)
	}

	if result.Member.Name != "node-c" || result.Member.Status != "Online" {
		t.Fatalf("Join() member = %#v", result.Member)
	}

	steps := map[string]string{}
	for _, step := range result.Steps {
		steps[step.Name] = step.Status
	}
	for _, name := range []string{"connect", "install", "service", "storage", "token", "join", "online", "sudoers", "fleet_key"} {
		if steps[name] != "ok" {
			t.Fatalf("step %q status = %q (steps=%#v)", name, steps[name], result.Steps)
		}
	}

	commands := f.client.commandLog()
	for _, want := range []string{
		"sudo -k -S -p '' install -d -m 0755 /etc/apt/keyrings",
		"sudo -k -S -p '' env DEBIAN_FRONTEND=noninteractive apt-get update",
		"sudo -k -S -p '' env DEBIAN_FRONTEND=noninteractive apt-get install -y 'incus'",
		"'qemu-system-x86' 'ovmf' 'swtpm'",
		"sudo -k -S -p '' systemctl enable --now incus.socket incus",
		"sudo -k -S -p '' mkdir -p '/srv/incus'",
		"sudo -k -S -p '' incus query -X PUT --data-file",
		"sudo -k -S -p '' visudo -cf",
		"install -m 0440 -o root -g root",
		"authorized_keys",
	} {
		if !strings.Contains(commands, want) {
			t.Fatalf("commands missing %q:\n%s", want, commands)
		}
	}

	var payload api.ClusterPut
	found := false
	for _, stdin := range f.client.stdinLog() {
		if !strings.HasPrefix(stdin, "{") {
			continue
		}
		if err := json.Unmarshal([]byte(stdin), &payload); err == nil && payload.ClusterToken != "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("join payload not uploaded: %#v", f.client.stdinLog())
	}
	if payload.ServerName != "node-c" {
		t.Fatalf("payload server_name = %q", payload.ServerName)
	}
	if payload.ServerAddress != "198.51.100.11:8443" {
		t.Fatalf("payload server_address = %q", payload.ServerAddress)
	}
	if payload.ClusterAddress != "198.51.100.10:8443" {
		t.Fatalf("payload cluster_address = %q", payload.ClusterAddress)
	}
	if payload.ClusterCertificate != testCertPEM {
		t.Fatalf("payload certificate = %q", payload.ClusterCertificate)
	}
	if len(payload.MemberConfig) != 0 {
		t.Fatalf("payload member_config = %#v", payload.MemberConfig)
	}

	// The sudo password is fed through stdin exactly once per privileged
	// command and never appears on the command line.
	if strings.Contains(commands, "sudo-secret") {
		t.Fatal("sudo password leaked into a command")
	}
	if !strings.Contains(strings.Join(f.client.stdinLog(), "\n"), "sudo-secret\n") {
		t.Fatal("sudo password was not sent on stdin")
	}

	creates := f.server.CreateMemberArgs()
	if len(creates) != 1 || creates[0].ServerName != "node-c" {
		t.Fatalf("CreateClusterMember arguments = %#v", creates)
	}
}

func TestJoinUsesExistingIncus(t *testing.T) {
	f := newJoinFixture(t, nil)
	f.client.versions = []string{"7.0.2"}

	result, err := f.service.Join(context.Background(), "c2", Request{
		Name:          "node-c",
		Address:       "198.51.100.12",
		SSHUser:       "node-admin",
		IgnoreHostKey: true,
	})
	if err != nil {
		t.Fatalf("Join() error = %v", err)
	}
	if result.Member.Name != "node-c" {
		t.Fatalf("unexpected result = %#v", result)
	}

	commands := f.client.commandLog()
	if strings.Contains(commands, "apt-get") {
		t.Fatalf("already-installed node ran apt:\n%s", commands)
	}
}

func TestJoinFillsStorageMemberConfig(t *testing.T) {
	f := newJoinFixture(t, func(server *incusfake.Server) {
		server.Cluster = &api.Cluster{
			Enabled:    true,
			ServerName: "node-b",
			MemberConfig: []api.ClusterMemberConfigKey{{
				Entity: "storage-pool",
				Name:   "default",
				Key:    "source",
			}},
		}
	})
	f.client.versions = []string{"7.0.2"}

	_, err := f.service.Join(context.Background(), "c2", Request{
		Name:          "node-c",
		Address:       "198.51.100.11",
		SSHUser:       "node-admin",
		IgnoreHostKey: true,
		StorageSource: "/data/incus",
	})
	if err != nil {
		t.Fatalf("Join() error = %v", err)
	}

	var payload api.ClusterPut
	for _, stdin := range f.client.stdinLog() {
		if strings.HasPrefix(stdin, "{") && json.Unmarshal([]byte(stdin), &payload) == nil && payload.ClusterToken != "" {
			break
		}
	}
	if len(payload.MemberConfig) != 1 || payload.MemberConfig[0].Value != "/data/incus" {
		t.Fatalf("member config = %#v", payload.MemberConfig)
	}
}

func TestJoinRejectsDuplicateAndBadCluster(t *testing.T) {
	f := newJoinFixture(t, nil)
	_, err := f.service.Join(context.Background(), "c2", Request{
		Name:          "node-b",
		Address:       "198.51.100.11",
		SSHUser:       "node-admin",
		IgnoreHostKey: true,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate error = %v", err)
	}

	_, err = f.service.Join(context.Background(), "nope", Request{Address: "198.51.100.11"})
	if !errors.Is(err, clusters.ErrUnknownCluster) {
		t.Fatalf("unknown cluster error = %v", err)
	}

	notClustered := newJoinFixture(t, func(server *incusfake.Server) {
		server.Clustered = false
		server.Server = &api.Server{Environment: api.ServerEnvironment{ServerVersion: "7.0.2"}}
	})
	_, err = notClustered.service.Join(context.Background(), "c2", Request{
		Address:       "198.51.100.11",
		SSHUser:       "node-admin",
		IgnoreHostKey: true,
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("not clustered error = %v", err)
	}
}

func TestJoinOfflineCluster(t *testing.T) {
	server := &incusfake.Server{ServerErr: errors.New("down")}
	cfgs := []config.Cluster{{Name: "c2", Endpoint: "https://198.51.100.10:8443"}}
	dial := func(context.Context, config.Cluster) (clusters.InstanceServer, error) { return server, nil }
	manager, err := clusters.NewWithDialer(context.Background(), cfgs, dial, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)

	service := New(manager, config.NodeAccess{}, testLogger())
	_, err = service.Join(context.Background(), "c2", Request{
		Address: "198.51.100.11", SSHUser: "node-admin", IgnoreHostKey: true,
	})
	if !errors.Is(err, clusters.ErrUnavailable) {
		t.Fatalf("offline error = %v", err)
	}
}

func TestJoinValidation(t *testing.T) {
	f := newJoinFixture(t, nil)

	cases := []struct {
		name string
		req  Request
	}{
		{"no address", Request{}},
		{"bad address", Request{Address: "bad host"}},
		{"address with port", Request{Address: "198.51.100.11:22"}},
		{"bad name", Request{Address: "198.51.100.11", Name: "-bad"}},
		{"bad user", Request{Address: "198.51.100.11", Name: "node", SSHUser: "bad user"}},
		{"bad port", Request{Address: "198.51.100.11", Name: "node", SSHUser: "node-admin", SSHPort: 70000}},
		{"newline password", Request{Address: "198.51.100.11", Name: "node", SSHUser: "node-admin", SSHPassword: "a\nb"}},
		{"bad storage", Request{Address: "198.51.100.11", Name: "node", SSHUser: "node-admin", StorageSource: "relative"}},
		{"bad repo", Request{Address: "198.51.100.11", Name: "node", SSHUser: "node-admin", IncusRepo: "http://example.com"}},
		{"bad repo key", Request{Address: "198.51.100.11", Name: "node", SSHUser: "node-admin", IncusRepoKey: "ftp://example.com"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := f.service.Join(context.Background(), "c2", tc.req); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("Join() error = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

func TestJoinRequiresHostKeyPolicy(t *testing.T) {
	f := newJoinFixture(t, nil)
	f.service.access.SSHInsecureIgnoreHostKey = false
	f.service.access.SSHKnownHosts = ""

	_, err := f.service.Join(context.Background(), "c2", Request{
		Address:      "198.51.100.11",
		SSHUser:      "node-admin",
		InstallIncus: false,
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("host key policy error = %v", err)
	}
}

func TestJoinVersionMismatch(t *testing.T) {
	f := newJoinFixture(t, nil)
	f.client.versions = []string{"6.0.4"}

	_, err := f.service.Join(context.Background(), "c2", Request{
		Name:          "node-c",
		Address:       "198.51.100.11",
		SSHUser:       "node-admin",
		IgnoreHostKey: true,
	})
	if !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), "must match") {
		t.Fatalf("version mismatch error = %v", err)
	}
}

func TestJoinInstallFailures(t *testing.T) {
	cases := []struct {
		name string
		fail string
		req  Request
	}{
		{"apt install", "apt-get install", Request{InstallIncus: true}},
		{"apt update", "apt-get update", Request{InstallIncus: true}},
		{"repo key dir", "install -d -m 0755", Request{InstallIncus: true}},
		{"repo key upload", "incus-fleet-zabbly.asc", Request{InstallIncus: true}},
		{"repo key install", "install -m 0644 -o root -g root", Request{InstallIncus: true}},
		{"repo sources upload", "incus-fleet-incus.sources", Request{InstallIncus: true}},
		{"repo sources install", "/etc/apt/sources.list.d/zabbly-incus.sources", Request{InstallIncus: true}},
		{"service", "systemctl", Request{InstallIncus: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newJoinFixture(t, nil)
			f.client.failOn = tc.fail
			tc.req.Address = "198.51.100.11"
			tc.req.SSHUser = "node-admin"
			tc.req.IgnoreHostKey = true

			if _, err := f.service.Join(context.Background(), "c2", tc.req); err == nil {
				t.Fatalf("Join() error = nil")
			}
		})
	}
}

func TestJoinFetchAndDetectFailures(t *testing.T) {
	f := newJoinFixture(t, nil)
	f.service.fetch = func(context.Context, string) ([]byte, error) { return nil, errors.New("offline") }
	_, err := f.service.Join(context.Background(), "c2", Request{
		Address: "198.51.100.11", SSHUser: "node-admin", InstallIncus: true, IgnoreHostKey: true,
	})
	if err == nil || !strings.Contains(err.Error(), "repository key") {
		t.Fatalf("fetch error = %v", err)
	}

	empty := newJoinFixture(t, nil)
	empty.service.fetch = func(context.Context, string) ([]byte, error) { return nil, nil }
	_, err = empty.service.Join(context.Background(), "c2", Request{
		Address: "198.51.100.11", SSHUser: "node-admin", InstallIncus: true, IgnoreHostKey: true,
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty key error = %v", err)
	}

	badOS := newJoinFixture(t, nil)
	badOS.client.outputs = []scripted{
		{needle: "hostname -s", output: "node-c\n"},
		{needle: "/etc/os-release", output: "BAD CODENAME"},
	}
	_, err = badOS.service.Join(context.Background(), "c2", Request{
		Address: "198.51.100.11", SSHUser: "node-admin", InstallIncus: true, IgnoreHostKey: true,
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("bad codename error = %v", err)
	}

	badArch := newJoinFixture(t, nil)
	badArch.client.outputs = []scripted{
		{needle: "hostname -s", output: "node-c\n"},
		{needle: "/etc/os-release", output: "trixie"},
		{needle: "dpkg --print-architecture", output: "weird"},
	}
	_, err = badArch.service.Join(context.Background(), "c2", Request{
		Address: "198.51.100.11", SSHUser: "node-admin", InstallIncus: true, IgnoreHostKey: true,
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("bad arch error = %v", err)
	}
}

func TestJoinRequiresInstallFlag(t *testing.T) {
	f := newJoinFixture(t, nil)
	f.client.versions = []string{""}

	_, err := f.service.Join(context.Background(), "c2", Request{
		Name:          "node-c",
		Address:       "198.51.100.11",
		SSHUser:       "node-admin",
		IgnoreHostKey: true,
	})
	if !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), "install_incus") {
		t.Fatalf("missing install error = %v", err)
	}
}

func TestJoinHostnameDetectionFailure(t *testing.T) {
	f := newJoinFixture(t, nil)
	f.client.outputs = []scripted{{needle: "hostname -s", output: "bad_name\n"}}
	f.client.versions = []string{"7.0.2"}

	_, err := f.service.Join(context.Background(), "c2", Request{
		Address:       "198.51.100.11",
		SSHUser:       "node-admin",
		IgnoreHostKey: true,
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("hostname error = %v", err)
	}
}

func TestJoinSSHDialFailure(t *testing.T) {
	f := newJoinFixture(t, nil)
	f.service.dialer = &fakeSSHDialer{err: errors.New("connection refused")}

	_, err := f.service.Join(context.Background(), "c2", Request{
		Address: "198.51.100.11", SSHUser: "node-admin", IgnoreHostKey: true,
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("dial error = %v", err)
	}
}

func TestJoinWaitAndServiceFailures(t *testing.T) {
	t.Run("wait ready", func(t *testing.T) {
		f := newJoinFixture(t, nil)
		f.client.versions = []string{"7.0.2"}
		f.client.failOn = "incus query /1.0"
		_, err := f.service.Join(context.Background(), "c2", Request{
			Name: "node-c", Address: "198.51.100.11", SSHUser: "node-admin", IgnoreHostKey: true,
		})
		if err == nil || !strings.Contains(err.Error(), "did not become ready") {
			t.Fatalf("wait ready error = %v", err)
		}
	})

	t.Run("member offline", func(t *testing.T) {
		f := newJoinFixture(t, nil)
		f.client.versions = []string{"7.0.2"}
		f.server.MembersFunc = func() ([]api.ClusterMember, error) {
			return nil, errors.New("not ready")
		}
		f.service.waitTimeout = 20 * time.Millisecond
		_, err := f.service.Join(context.Background(), "c2", Request{
			Name: "node-c", Address: "198.51.100.11", SSHUser: "node-admin", IgnoreHostKey: true,
		})
		if err == nil || !strings.Contains(err.Error(), "did not come online") {
			t.Fatalf("offline member error = %v", err)
		}
	})

	t.Run("join command", func(t *testing.T) {
		f := newJoinFixture(t, nil)
		f.client.versions = []string{"7.0.2"}
		f.client.failOn = "query -X PUT"
		_, err := f.service.Join(context.Background(), "c2", Request{
			Name: "node-c", Address: "198.51.100.11", SSHUser: "node-admin", IgnoreHostKey: true,
		})
		if err == nil || !strings.Contains(err.Error(), "join cluster") {
			t.Fatalf("join error = %v", err)
		}
	})
}

func TestJoinPostSetupWarnings(t *testing.T) {
	f := newJoinFixture(t, nil)
	f.client.versions = []string{"7.0.2"}
	f.service.access.SSHUser = "other"
	f.client.failOn = "visudo"

	result, err := f.service.Join(context.Background(), "c2", Request{
		Name: "node-c", Address: "198.51.100.11", SSHUser: "node-admin", IgnoreHostKey: true,
	})
	if err != nil {
		t.Fatalf("Join() error = %v", err)
	}

	statuses := map[string]string{}
	for _, step := range result.Steps {
		statuses[step.Name] = step.Status
	}
	if statuses["sudoers"] != "warning" {
		t.Fatalf("sudoers status = %q", statuses["sudoers"])
	}
	if statuses["fleet_key"] != "skipped" {
		t.Fatalf("fleet_key status = %q", statuses["fleet_key"])
	}
}

func TestJoinRootSSHSkipsSudo(t *testing.T) {
	f := newJoinFixture(t, nil)
	f.client.versions = []string{"7.0.2"}

	result, err := f.service.Join(context.Background(), "c2", Request{
		Name: "node-c", Address: "198.51.100.11", SSHUser: "root", IgnoreHostKey: true,
	})
	if err != nil {
		t.Fatalf("Join() error = %v", err)
	}
	if len(result.Steps) == 0 {
		t.Fatal("Join() returned no steps")
	}

	commands := f.client.commandLog()
	if strings.Contains(commands, "sudo -n") || strings.Contains(commands, "sudo -k") {
		t.Fatalf("root join used sudo:\n%s", commands)
	}
}

func TestJoinSudoersInstallFailure(t *testing.T) {
	f := newJoinFixture(t, nil)
	f.client.versions = []string{"7.0.2"}
	f.client.failOn = "install -m 0440"

	result, err := f.service.Join(context.Background(), "c2", Request{
		Name: "node-c", Address: "198.51.100.11", SSHUser: "node-admin", IgnoreHostKey: true,
	})
	if err != nil {
		t.Fatalf("Join() error = %v", err)
	}

	statuses := map[string]string{}
	for _, step := range result.Steps {
		statuses[step.Name] = step.Status
	}
	if statuses["sudoers"] != "warning" {
		t.Fatalf("sudoers status = %q", statuses["sudoers"])
	}
}

func TestJoinAuthorizeKeyFailure(t *testing.T) {
	f := newJoinFixture(t, nil)
	f.client.versions = []string{"7.0.2"}
	f.client.failOn = "authorized_keys"

	result, err := f.service.Join(context.Background(), "c2", Request{
		Name: "node-c", Address: "198.51.100.11", SSHUser: "node-admin", IgnoreHostKey: true,
	})
	if err != nil {
		t.Fatalf("Join() error = %v", err)
	}
	if result.Steps[len(result.Steps)-1].Status != "warning" {
		t.Fatalf("fleet key step = %#v", result.Steps[len(result.Steps)-1])
	}
}

func TestNewDefaultsLogger(t *testing.T) {
	service := New(nil, config.NodeAccess{}, nil)
	if service == nil || service.logger == nil {
		t.Fatalf("New() = %#v", service)
	}
}

func TestJoinPostSetupBadFleetKey(t *testing.T) {
	f := newJoinFixture(t, nil)
	f.client.versions = []string{"7.0.2"}
	f.service.access.SSHKey = filepath.Join(t.TempDir(), "missing")

	result, err := f.service.Join(context.Background(), "c2", Request{
		Name: "node-c", Address: "198.51.100.11", SSHUser: "node-admin", IgnoreHostKey: true,
	})
	if err != nil {
		t.Fatalf("Join() error = %v", err)
	}
	if result.Steps[len(result.Steps)-1].Status != "warning" {
		t.Fatalf("fleet key step = %#v", result.Steps[len(result.Steps)-1])
	}
}

func TestJoinTokenFailure(t *testing.T) {
	f := newJoinFixture(t, func(server *incusfake.Server) {
		server.CreateMemberErr = errors.New("token failed")
	})
	f.client.versions = []string{"7.0.2"}

	_, err := f.service.Join(context.Background(), "c2", Request{
		Name: "node-c", Address: "198.51.100.11", SSHUser: "node-admin", IgnoreHostKey: true,
	})
	if err == nil || !strings.Contains(err.Error(), "join token") {
		t.Fatalf("token error = %v", err)
	}
}

func TestJoinTokenDecodeFailure(t *testing.T) {
	f := newJoinFixture(t, func(server *incusfake.Server) {
		server.JoinTokenMetadata = map[string]any{"serverName": 7}
	})
	f.client.versions = []string{"7.0.2"}

	_, err := f.service.Join(context.Background(), "c2", Request{
		Name: "node-c", Address: "198.51.100.11", SSHUser: "node-admin", IgnoreHostKey: true,
	})
	if err == nil || !strings.Contains(err.Error(), "decode join token") {
		t.Fatalf("token decode error = %v", err)
	}
}

func TestBuildMemberConfig(t *testing.T) {
	if configs, err := buildMemberConfig(nil, "/srv/incus"); err != nil || configs != nil {
		t.Fatalf("buildMemberConfig(nil) = %#v, %v", configs, err)
	}

	cluster := &api.Cluster{MemberConfig: []api.ClusterMemberConfigKey{{
		Entity: "storage-pool", Name: "default", Key: "source", Value: "/var/lib/incus/default",
	}}}
	configs, err := buildMemberConfig(cluster, "/srv/incus")
	if err != nil || len(configs) != 1 || configs[0].Value != "/srv/incus" {
		t.Fatalf("buildMemberConfig() = %#v, %v", configs, err)
	}

	missing := &api.Cluster{MemberConfig: []api.ClusterMemberConfigKey{{
		Entity: "network", Name: "phys0", Key: "parent",
	}}}
	if _, err := buildMemberConfig(missing, ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing value error = %v", err)
	}
}

func TestEndpointAddress(t *testing.T) {
	if got, err := endpointAddress("https://198.51.100.10:8443"); err != nil || got != "198.51.100.10:8443" {
		t.Fatalf("endpointAddress(port) = %q, %v", got, err)
	}
	if got, err := endpointAddress("https://198.51.100.10"); err != nil || got != "198.51.100.10:8443" {
		t.Fatalf("endpointAddress(no port) = %q, %v", got, err)
	}
	if _, err := endpointAddress("not a url"); err == nil {
		t.Fatal("endpointAddress(invalid) error = nil")
	}
}

func TestClusterCertificate(t *testing.T) {
	f := newJoinFixture(t, nil)
	conn, err := f.manager.Connection("c2")
	if err != nil {
		t.Fatal(err)
	}
	pemBytes, err := clusterCertificate(conn)
	if err != nil || string(pemBytes) != testCertPEM {
		t.Fatalf("clusterCertificate() = %q, %v", pemBytes, err)
	}

	// Without a pinned certificate the endpoint certificate is captured.
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer tlsServer.Close()

	cfgs := []config.Cluster{{Name: "c1", Endpoint: tlsServer.URL}}
	dial := func(context.Context, config.Cluster) (clusters.InstanceServer, error) {
		return &incusfake.Server{}, nil
	}
	manager, err := clusters.NewWithDialer(context.Background(), cfgs, dial, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)

	conn, err = manager.Connection("c1")
	if err != nil {
		t.Fatal(err)
	}
	captured, err := clusterCertificate(conn)
	if err != nil {
		t.Fatalf("clusterCertificate(endpoint) error = %v", err)
	}
	if !strings.Contains(string(captured), "BEGIN CERTIFICATE") {
		t.Fatalf("captured certificate = %q", captured)
	}

	// A pinned certificate path that no longer exists must fail closed.
	cfgs = []config.Cluster{{Name: "c2", Endpoint: "https://192.0.2.10:8443", TLSServerCert: filepath.Join(t.TempDir(), "missing.crt")}}
	manager, err = clusters.NewWithDialer(context.Background(), cfgs, dial, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	conn, err = manager.Connection("c2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clusterCertificate(conn); err == nil {
		t.Fatal("clusterCertificate(missing) error = nil")
	}
}

func TestPublicKey(t *testing.T) {
	dir := t.TempDir()
	keyPath := newTestKey(t, dir)

	key, err := publicKey(keyPath)
	if err != nil {
		t.Fatalf("publicKey() error = %v", err)
	}
	if !strings.HasPrefix(string(key), "ssh-ed25519 ") {
		t.Fatalf("publicKey() = %q", key)
	}

	if _, err := publicKey(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("publicKey(missing) error = nil")
	}

	bad := filepath.Join(dir, "bad")
	if err := os.WriteFile(bad, []byte("bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := publicKey(bad); err == nil {
		t.Fatal("publicKey(bad) error = nil")
	}
}

func TestHTTPFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			http.Error(w, "nope", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("key-data"))
	}))
	defer server.Close()

	data, err := httpFetch(context.Background(), server.URL)
	if err != nil || string(data) != "key-data" {
		t.Fatalf("httpFetch() = %q, %v", data, err)
	}

	if _, err := httpFetch(context.Background(), server.URL+"/fail"); err == nil {
		t.Fatal("httpFetch(fail) error = nil")
	}
	if _, err := httpFetch(context.Background(), "://bad"); err == nil {
		t.Fatal("httpFetch(bad url) error = nil")
	}
}

func TestMemberFromAPI(t *testing.T) {
	member := memberFromAPI(api.ClusterMember{
		ServerName: "node",
		URL:        "https://192.0.2.1:8443",
		Status:     "Online",
		Roles:      []string{"database"},
		Groups:     nil,
	})
	if member.Name != "node" || member.Groups == nil || member.Roles[0] != "database" {
		t.Fatalf("memberFromAPI() = %#v", member)
	}
}

func TestNormalizeRequestDefaults(t *testing.T) {
	f := newJoinFixture(t, nil)
	normalized, err := f.service.normalizeRequest(Request{
		Address: "198.51.100.11",
	})
	if err != nil {
		t.Fatalf("normalizeRequest() error = %v", err)
	}
	if normalized.SSHUser != "node-admin" || normalized.SSHPort != 22 {
		t.Fatalf("normalized = %#v", normalized)
	}
	if normalized.StorageSource != DefaultStorageSource {
		t.Fatalf("storage source = %q", normalized.StorageSource)
	}
	if normalized.IncusRepo != DefaultIncusRepo || normalized.IncusRepoKey != DefaultIncusKey {
		t.Fatalf("repo defaults = %#v", normalized)
	}
}

func TestJoinContextCancelled(t *testing.T) {
	f := newJoinFixture(t, nil)
	f.client.versions = []string{"7.0.2"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := f.service.Join(ctx, "c2", Request{
		Name: "node-c", Address: "198.51.100.11", SSHUser: "node-admin", IgnoreHostKey: true,
	})
	if err == nil {
		t.Fatal("Join(cancelled) error = nil")
	}
}

func TestJoinClusterInfoFailure(t *testing.T) {
	f := newJoinFixture(t, func(server *incusfake.Server) {
		server.ClusterErr = errors.New("cluster info failed")
	})
	f.client.versions = []string{"7.0.2"}

	_, err := f.service.Join(context.Background(), "c2", Request{
		Name: "node-c", Address: "198.51.100.11", SSHUser: "node-admin", IgnoreHostKey: true,
	})
	if err == nil || !strings.Contains(err.Error(), "member config") {
		t.Fatalf("cluster info error = %v", err)
	}
}

func TestJoinStorageFailure(t *testing.T) {
	f := newJoinFixture(t, nil)
	f.client.versions = []string{"7.0.2"}
	f.client.failOn = "mkdir -p"

	_, err := f.service.Join(context.Background(), "c2", Request{
		Name: "node-c", Address: "198.51.100.11", SSHUser: "node-admin", IgnoreHostKey: true,
	})
	if err == nil || !strings.Contains(err.Error(), "storage source") {
		t.Fatalf("storage error = %v", err)
	}
}
