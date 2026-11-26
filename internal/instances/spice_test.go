package instances

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestSPICEPortFor(t *testing.T) {
	port := SPICEPortFor("demo-installer")
	if port < 5910 || port >= 5990 {
		t.Fatalf("SPICEPortFor() = %d, out of range", port)
	}
	if SPICEPortFor("demo-installer") != port {
		t.Fatal("SPICEPortFor() must be stable for a name")
	}
	if SPICEPortFor("other-vm") == port {
		t.Log("collision between similar names is possible but unlikely")
	}
}

func TestPrepareSPICE(t *testing.T) {
	running := &incusfake.Server{
		Instance: &api.Instance{
			Name:       "vm-01",
			Type:       "virtual-machine",
			Status:     "Running",
			StatusCode: api.Running,
			InstancePut: api.InstancePut{
				Config: api.ConfigMap{
					"limits.memory": "2GiB",
					"raw.qemu":      "-machine accel=kvm\n-spice port=9999,disable-ticketing=on",
				},
			},
		},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": running})
	ctx := context.Background()

	// Applying on a running VM power cycles it and the managed line is
	// replaced, not duplicated.
	result, err := svc.PrepareSPICE(ctx, "c1", "vm-01", 5991, true)
	if err != nil {
		t.Fatalf("PrepareSPICE() error = %v", err)
	}
	if !result.Applied || result.RestartRequired {
		t.Fatalf("result = %#v", result)
	}

	updates := running.UpdateArgs()
	if len(updates) == 0 {
		t.Fatal("no instance update recorded")
	}
	raw := updates[len(updates)-1].Instance.Config["raw.qemu"]
	if strings.Contains(raw, "port=9999") {
		t.Fatalf("old spice line must be replaced: %q", raw)
	}
	if !strings.Contains(raw, "unix=off,port=5991,disable-ticketing=on") || !strings.Contains(raw, "-machine accel=kvm") {
		t.Fatalf("raw.qemu = %q", raw)
	}
	// Power cycle: stop then start.
	if len(running.UpdateStateArgs()) != 2 {
		t.Fatalf("state args = %#v", running.UpdateStateArgs())
	}

	// apply guarantees a running VM: a stopped instance is booted after the
	// config lands (no stop step needed).
	stopped := &incusfake.Server{
		Instance: &api.Instance{
			Name:       "vm-02",
			Type:       "virtual-machine",
			Status:     "Stopped",
			StatusCode: api.Stopped,
		},
	}
	stoppedSvc := newTestService(t, map[string]*incusfake.Server{"c1": stopped})
	result, err = stoppedSvc.PrepareSPICE(ctx, "c1", "vm-02", 5992, true)
	if err != nil {
		t.Fatalf("PrepareSPICE() stopped error = %v", err)
	}
	if !result.Applied || result.RestartRequired {
		t.Fatalf("stopped result = %#v", result)
	}
	states := stopped.UpdateStateArgs()
	if len(states) != 1 || states[0].Action != "start" {
		t.Fatalf("state args = %#v", states)
	}

	if _, err = svc.PrepareSPICE(ctx, "c1", "vm-01", 80, true); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("PrepareSPICE() bad port = %v", err)
	}
	if _, err = svc.PrepareSPICE(ctx, "missing", "vm-01", 5991, true); err == nil {
		t.Fatal("PrepareSPICE() expected unknown cluster error")
	}
}

func TestPrepareSPICEKeepsRunningVMWithExistingListener(t *testing.T) {
	srv := &incusfake.Server{
		Instance: &api.Instance{
			Name:       "vm-ready",
			Type:       "virtual-machine",
			Status:     "Running",
			StatusCode: api.Running,
			InstancePut: api.InstancePut{
				Config: api.ConfigMap{
					"raw.qemu": "-spice unix=off,port=5993,disable-ticketing=on,addr=127.0.0.1",
				},
			},
		},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	result, err := svc.PrepareSPICE(context.Background(), "c1", "vm-ready", 0, true)
	if err != nil {
		t.Fatalf("PrepareSPICE() error = %v", err)
	}
	if result.Port != 5993 || !result.Applied || result.RestartRequired {
		t.Fatalf("result = %#v", result)
	}
	if len(srv.UpdateStateArgs()) != 0 {
		t.Fatalf("running VM must not be power cycled: %#v", srv.UpdateStateArgs())
	}
	if len(srv.UpdateArgs()) != 0 {
		t.Fatalf("raw.qemu must not be rewritten: %#v", srv.UpdateArgs())
	}
}

func TestSPICEDial(t *testing.T) {
	echo := func(conn net.Conn) {
		defer conn.Close() //nolint:errcheck // test echo
		buf := make([]byte, 64)
		n, _ := conn.Read(buf)
		_, _ = conn.Write(buf[:n])
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close() //nolint:errcheck // test teardown
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go echo(conn)
		}
	}()

	_, port, _ := net.SplitHostPort(listener.Addr().String())
	portNum := 0
	for _, r := range port {
		portNum = portNum*10 + int(r-'0')
	}

	vm := &incusfake.Server{
		Instance: &api.Instance{
			Name:       "vm-01",
			Type:       "virtual-machine",
			Status:     "Running",
			StatusCode: api.Running,
			Location:   "node-b",
		},
		Members: []api.ClusterMember{
			{ServerName: "node-a", URL: "https://192.0.2.10:8443", Status: "Online"},
			{ServerName: "node-b", URL: "https://127.0.0.1:8443", Status: "Online"},
		},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": vm})
	// The production dialer targets the instance-derived port on the hosting
	// member. Point it at the ephemeral test listener instead, and record what
	// it was asked to dial so the port rule is still exercised.
	var dialedAddr string
	svc.dial = func(addr string, _ time.Duration) (net.Conn, error) {
		dialedAddr = addr
		return net.DialTimeout("tcp", listener.Addr().String(), 3*time.Second)
	}

	wantPort := SPICEPortFor("vm-01")

	conn, err := svc.SPICEDial(context.Background(), "c1", "vm-01", wantPort)
	if err != nil {
		t.Fatalf("SPICEDial() error = %v", err)
	}
	if !strings.HasSuffix(dialedAddr, ":"+strconv.Itoa(wantPort)) {
		t.Fatalf("dialed address = %q, want port %d", dialedAddr, wantPort)
	}
	defer conn.Close() //nolint:errcheck // test teardown

	if _, err = conn.Write([]byte("spice-handshake")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 15)
	if _, err = conn.Read(buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "spice-handshake" {
		t.Fatalf("echo = %q", string(buf))
	}

	if _, err = svc.SPICEDial(context.Background(), "c1", "vm-01", 80); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("SPICEDial() bad port = %v", err)
	}
	// A port inside the allowed range but not derived from this instance must be
	// rejected: otherwise the bridge would proxy to arbitrary cluster services.
	wrong := wantPort + 1
	if wrong > spicePortMax {
		wrong = wantPort - 1
	}
	if _, err = svc.SPICEDial(context.Background(), "c1", "vm-01", wrong); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("SPICEDial() mismatched port = %v, want ErrInvalidRequest", err)
	}
	if _, err = svc.SPICEDial(context.Background(), "c1", "missing", SPICEPortFor("missing")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SPICEDial() missing instance = %v", err)
	}
}

func TestSpiceMemberHostFallbacks(t *testing.T) {
	// A client that fails to list members falls back to loopback.
	failing := &failingMemberClient{}
	if host := spiceMemberHost(failing, "node-a"); host != "127.0.0.1" {
		t.Fatalf("host = %q, want loopback", host)
	}

	// An empty member list does the same.
	empty := &staticMemberClient{}
	if host := spiceMemberHost(empty, "node-a"); host != "127.0.0.1" {
		t.Fatalf("host = %q, want loopback", host)
	}

	// An empty location resolves to the first member.
	one := &staticMemberClient{members: []api.ClusterMember{
		{ServerName: "node-a", URL: "https://192.0.2.10:8443"},
	}}
	if host := spiceMemberHost(one, ""); host != "192.0.2.10" {
		t.Fatalf("host = %q, want the first member", host)
	}
}

type failingMemberClient struct{}

func (failingMemberClient) GetClusterMembers() ([]api.ClusterMember, error) {
	return nil, errors.New("boom")
}

type staticMemberClient struct {
	members []api.ClusterMember
}

func (s *staticMemberClient) GetClusterMembers() ([]api.ClusterMember, error) {
	return s.members, nil
}

func TestSetDialerReplacesHook(t *testing.T) {
	svc := &Service{}
	called := false
	svc.SetDialer(func(string, time.Duration) (net.Conn, error) {
		called = true
		return nil, errors.New("not dialing")
	})

	if _, err := svc.dial("127.0.0.1:1", time.Second); err == nil || !called {
		t.Fatalf("dial() = %v, called=%v", err, called)
	}
}
