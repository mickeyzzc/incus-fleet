package instances

import (
	"context"
	"errors"
	"net"
	"net/http"
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
	if !strings.Contains(raw, "port=5991,disable-ticketing=on") || !strings.Contains(raw, "-machine accel=kvm") {
		t.Fatalf("raw.qemu = %q", raw)
	}
	// Power cycle: stop then start.
	if len(running.UpdateStateArgs()) != 2 {
		t.Fatalf("state args = %#v", running.UpdateStateArgs())
	}

	// An idle VM keeps the config applied without a restart.
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
	if len(stopped.UpdateStateArgs()) != 0 {
		t.Fatalf("stopped VM must not be power cycled: %#v", stopped.UpdateStateArgs())
	}

	if _, err = svc.PrepareSPICE(ctx, "c1", "vm-01", 80, true); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("PrepareSPICE() bad port = %v", err)
	}
	if _, err = svc.PrepareSPICE(ctx, "missing", "vm-01", 5991, true); err == nil {
		t.Fatal("PrepareSPICE() expected unknown cluster error")
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
	defer listener.Close()                         //nolint:errcheck // test teardown
	go http.Serve(listener, http.HandlerFunc(nil)) //nolint:errcheck // keep listener busy
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

	conn, err := svc.SPICEDial(context.Background(), "c1", "vm-01", portNum)
	if err != nil {
		t.Fatalf("SPICEDial() error = %v", err)
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
	if _, err = svc.SPICEDial(context.Background(), "c1", "missing", portNum); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SPICEDial() missing instance = %v", err)
	}
}
