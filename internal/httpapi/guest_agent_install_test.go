package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/guestagent"
	"github.com/mickeyzzc/incus-fleet/internal/remote"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

// installScripted maps a command substring to its canned output.
type installScripted struct {
	needle string
	output string
}

// fakeGuestSSH stands in for the guest's SSH session.
type fakeGuestSSH struct {
	mu     sync.Mutex
	calls  []string
	output []installScripted
	failOn string
}

func (c *fakeGuestSSH) Run(_ context.Context, command string) (string, error) {
	c.mu.Lock()
	c.calls = append(c.calls, command)
	failOn := c.failOn
	output := c.output
	c.mu.Unlock()

	if failOn != "" && strings.Contains(command, failOn) {
		return "", errors.New("ssh command failed")
	}

	best := ""
	result := ""
	for _, item := range output {
		if strings.Contains(command, item.needle) && len(item.needle) > len(best) {
			best = item.needle
			result = item.output
		}
	}

	return result, nil
}

func (c *fakeGuestSSH) RunStdin(_ context.Context, command string, _ string) error {
	c.mu.Lock()
	c.calls = append(c.calls, command)
	c.mu.Unlock()

	return nil
}

func (c *fakeGuestSSH) Close() error { return nil }

func (c *fakeGuestSSH) commands() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]string(nil), c.calls...)
}

// fakeInstallDialer accepts every dial and hands out the scripted client.
type fakeInstallDialer struct {
	guest *fakeGuestSSH
	dials int
}

func (d *fakeInstallDialer) Dial(_ context.Context, _ remote.Options) (remote.Client, error) {
	d.dials++

	return d.guest, nil
}

func defaultInstallVM() *api.Instance {
	return &api.Instance{
		Name:       "vm1",
		Type:       "virtual-machine",
		Status:     "Running",
		StatusCode: api.Running,
		Location:   "node-a",
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
	}
}

func newGuestAgentFixture(t *testing.T, configure func(*incusfake.Server)) (*apiFixture, *fakeGuestSSH, *http.Cookie) {
	t.Helper()

	guest := &fakeGuestSSH{output: []installScripted{
		{needle: "ps -p 1", output: "systemd\n"},
		{needle: "/proc/filesystems", output: "yes\n"},
	}}
	dialer := &fakeInstallDialer{guest: guest}

	vm := &incusfake.Server{
		Clustered: true,
		Server: &api.Server{Environment: api.ServerEnvironment{
			ServerName:      "node-a",
			ServerVersion:   "7.0.2",
			ServerClustered: true,
		}},
		Members: []api.ClusterMember{{ServerName: "node-a", Status: "Online", URL: "https://192.0.2.10:8443"}},
		State: &api.InstanceState{
			OSInfo: &api.InstanceStateOSInfo{OS: "Debian GNU/Linux", OSVersion: "13"},
		},
	}
	if configure != nil {
		configure(vm)
	}

	f := newAPIFixtureWithDeps(t, vm, &incusfake.Server{}, nil, func(deps *Deps) {
		deps.GuestAgent = guestagent.New(deps.Clusters, config.NodeAccess{
			SSHUser:                  "fleet",
			SSHKey:                   "/tmp/id_fleet",
			SSHInsecureIgnoreHostKey: true,
		}, nil,
			guestagent.WithDialer(dialer),
			guestagent.WithVerify(20*time.Millisecond, 3*time.Millisecond),
		)
	})
	cookie := f.login(t)

	return f, guest, cookie
}

func TestGuestAgentInstallEndpoint(t *testing.T) {
	f, guest, cookie := newGuestAgentFixture(t, func(vm *incusfake.Server) {
		vm.Instance = defaultInstallVM()
		vm.NetworkLeases = []api.NetworkLease{
			{Hostname: "vm1", Hwaddr: "00:16:3e:aa:bb:01", Address: "10.0.0.99", Type: "dynamic"},
		}
	})

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/vm1/agent/install",
		map[string]any{"ssh_user": "root", "ssh_password": "guest-secret"}, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("install status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"steps"`) || !strings.Contains(rec.Body.String(), `"install"`) {
		t.Fatalf("install body = %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "guest-secret") {
		t.Fatal("credentials must never appear in the response")
	}

	joined := strings.Join(guest.commands(), "\n")
	for _, want := range []string{"mount -t 9p config", "install.sh", "umount"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("guest commands missing %q: %s", want, joined)
		}
	}
}

func TestGuestAgentInstallDisabled(t *testing.T) {
	vm := &incusfake.Server{Instance: defaultInstallVM()}
	f := newAPIFixtureWithDeps(t, vm, &incusfake.Server{}, nil, nil)
	cookie := f.login(t)

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/vm1/agent/install",
		map[string]any{"ssh_user": "root"}, cookie, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled install status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestGuestAgentInstallRejectsContainer(t *testing.T) {
	f, _, cookie := newGuestAgentFixture(t, func(vm *incusfake.Server) {
		container := defaultInstallVM()
		container.Type = "container"
		vm.Instance = container
	})

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/vm1/agent/install",
		map[string]any{"ssh_user": "root"}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("container install status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestGuestAgentInstallRejectsBadBody(t *testing.T) {
	f, _, cookie := newGuestAgentFixture(t, func(vm *incusfake.Server) {
		vm.Instance = defaultInstallVM()
	})

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/vm1/agent/install",
		map[string]any{"ssh_port": 70000}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/vm1/agent/install",
		"not-json", cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json status = %d", rec.Code)
	}
}

func TestGuestAgentInstallUnknownInstance(t *testing.T) {
	f, _, cookie := newGuestAgentFixture(t, func(vm *incusfake.Server) {
		vm.Instance = defaultInstallVM()
	})

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/missing/agent/install",
		map[string]any{"ssh_user": "root"}, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown instance status = %d body = %s", rec.Code, rec.Body.String())
	}
}
