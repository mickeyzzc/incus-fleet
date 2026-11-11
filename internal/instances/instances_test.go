package instances

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func newTestService(t *testing.T, servers map[string]*incusfake.Server) *Service {
	t.Helper()

	cfgs := make([]config.Cluster, 0, len(servers))
	for name := range servers {
		cfgs = append(cfgs, config.Cluster{Name: name, DisplayName: name, Endpoint: "https://example.invalid:8443", Project: "default"})
	}

	dial := func(_ context.Context, cfg config.Cluster) (clusters.InstanceServer, error) {
		srv, ok := servers[cfg.Name]
		if !ok {
			return nil, errors.New("missing fake")
		}

		return srv, nil
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager, err := clusters.NewWithDialer(context.Background(), cfgs, dial, logger)
	if err != nil {
		t.Fatalf("clusters.NewWithDialer() error = %v", err)
	}
	t.Cleanup(manager.Close)

	return New(manager)
}

func startedAt() time.Time {
	return time.Date(2026, 11, 11, 10, 30, 0, 0, time.UTC)
}

func fullInstance(name, instanceType, status string, code api.StatusCode) api.InstanceFull {
	return api.InstanceFull{
		Instance: api.Instance{
			Name:       name,
			Type:       instanceType,
			Status:     status,
			StatusCode: code,
			Location:   "node-a",
			Project:    "default",
			CreatedAt:  startedAt().Add(-time.Hour),
			InstancePut: api.InstancePut{
				Architecture: "x86_64",
				Description:  "test instance",
				Profiles:     []string{"default"},
			},
			ExpandedConfig: api.ConfigMap{
				"migration.stateful": "true",
				"image.os":           "Debian",
				"user.secret":        "should-not-leak",
			},
			ExpandedDevices: api.DevicesMap{
				"root": {"type": "disk", "pool": "default", "path": "/"},
			},
		},
		State: &api.InstanceState{
			Status:     status,
			StatusCode: code,
			Memory:     api.InstanceStateMemory{Usage: 1234},
			Processes:  12,
			StartedAt:  startedAt(),
			Network: map[string]api.InstanceStateNetwork{
				"eth0": {
					Addresses: []api.InstanceStateNetworkAddress{
						{Family: "inet", Address: "10.0.0.0.5"},
						{Family: "inet", Address: "127.0.0.1"},
						{Family: "inet", Address: "10.0.0.0.5"},
						{Family: "inet6", Address: "fd42::5"},
						{Family: "inet6", Address: "fe80::1"},
					},
				},
			},
		},
	}
}

func TestList(t *testing.T) {
	vm := fullInstance("vm1", "virtual-machine", "Running", api.Running)
	container := fullInstance("web", "container", "Stopped", api.Stopped)
	container.State = nil
	container.ExpandedConfig = api.ConfigMap{}
	srv := &incusfake.Server{Instances: []api.InstanceFull{vm, container}}

	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})
	list, err := svc.List("c1", api.InstanceTypeAny)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 2 || list[0].Name != "vm1" || list[1].Name != "web" {
		t.Fatalf("List() = %#v", list)
	}

	got := list[0]
	if got.Cluster != "c1" || got.Type != "virtual-machine" || got.Status != "Running" {
		t.Fatalf("summary = %#v", got)
	}
	if len(got.IPv4) != 1 || got.IPv4[0] != "10.0.0.0.5" || len(got.IPv6) != 1 || got.IPv6[0] != "fd42::5" {
		t.Fatalf("addresses = %#v %#v", got.IPv4, got.IPv6)
	}
	if got.Memory != 1234 || got.Processes != 12 || !got.StartedAt.Equal(startedAt()) {
		t.Fatalf("state summary = %#v", got)
	}
	if got.StoragePool != "default" || !got.MigrationStateful || !got.LiveMigratable {
		t.Fatalf("migration summary = %#v", got)
	}
	if list[1].Status != "Stopped" || list[1].MigrationStateful {
		t.Fatalf("container without state = %#v", list[1])
	}
}

func TestListPassesTypeAndErrors(t *testing.T) {
	srv := &incusfake.Server{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	if _, err := svc.List("c1", api.InstanceTypeVM); err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if !srv.HasCall("GetInstancesFull:virtual-machine") {
		t.Fatalf("calls = %v", srv.Calls())
	}

	srv.InstancesErr = errors.New("boom")
	if _, err := svc.List("c1", api.InstanceTypeAny); err == nil {
		t.Fatal("List() expected error")
	}
	if _, err := svc.List("missing", api.InstanceTypeAny); !errors.Is(err, clusters.ErrUnknownCluster) {
		t.Fatalf("List() unknown error = %v", err)
	}
}

func TestListAll(t *testing.T) {
	svc := newTestService(t, map[string]*incusfake.Server{
		"c1": {Instances: []api.InstanceFull{fullInstance("web", "container", "Running", api.Running)}},
		"c2": {Instances: []api.InstanceFull{fullInstance("db", "container", "Running", api.Running)}},
	})

	all, err := svc.ListAll(api.InstanceTypeAny)
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	if len(all) != 2 || all[0].Cluster != "c1" || all[1].Cluster != "c2" {
		t.Fatalf("ListAll() = %#v", all)
	}
}

func TestGet(t *testing.T) {
	srv := &incusfake.Server{
		Instance: &api.Instance{
			Name:       "vm1",
			Type:       "virtual-machine",
			Status:     "Running",
			StatusCode: api.Running,
			InstancePut: api.InstancePut{
				Architecture: "x86_64",
				Ephemeral:    true,
			},
			ExpandedConfig: api.ConfigMap{
				"image.os":           "Debian",
				"image.release":      "trixie",
				"migration.stateful": "true",
				"user.secret":        "hidden",
			},
			ExpandedDevices: api.DevicesMap{
				"root": {"type": "disk", "pool": "fast", "path": "/"},
			},
		},
		State: &api.InstanceState{
			Status:     "Running",
			StatusCode: api.Running,
			Network:    map[string]api.InstanceStateNetwork{},
			OSInfo: &api.InstanceStateOSInfo{
				OS:            "Debian GNU/Linux",
				OSVersion:     "13",
				KernelVersion: "6.12.0",
				Hostname:      "vm1",
			},
		},
	}

	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})
	detail, err := svc.Get("c1", "vm1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if !detail.Ephemeral || detail.OS != "Debian GNU/Linux" || detail.OSVersion != "13" || detail.Hostname != "vm1" {
		t.Fatalf("Detail = %#v", detail)
	}
	if detail.Config["image.os"] != "Debian" || detail.Config["migration.stateful"] != "true" {
		t.Fatalf("Config = %#v", detail.Config)
	}
	if _, exists := detail.Config["user.secret"]; exists {
		t.Fatalf("Config leaked user.secret: %#v", detail.Config)
	}
	if detail.StoragePool != "fast" || !detail.LiveMigratable {
		t.Fatalf("Detail summary = %#v", detail.Summary)
	}
}

func TestGetErrors(t *testing.T) {
	svc := newTestService(t, map[string]*incusfake.Server{"c1": {}})

	if _, err := svc.Get("c1", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v", err)
	}
	if _, err := svc.Get("missing", "vm"); !errors.Is(err, clusters.ErrUnknownCluster) {
		t.Fatalf("Get() unknown error = %v", err)
	}

	srv := &incusfake.Server{InstanceErr: errors.New("boom")}
	svc = newTestService(t, map[string]*incusfake.Server{"c1": srv})
	if _, err := svc.Get("c1", "vm"); err == nil {
		t.Fatal("Get() expected error")
	}

	srv = &incusfake.Server{
		Instance: &api.Instance{Name: "vm"},
		StateErr: errors.New("state boom"),
	}
	svc = newTestService(t, map[string]*incusfake.Server{"c1": srv})
	if _, err := svc.Get("c1", "vm"); err == nil {
		t.Fatal("Get() expected state error")
	}
}

func TestListAllError(t *testing.T) {
	svc := newTestService(t, map[string]*incusfake.Server{
		"c1": {},
		"c2": {InstancesErr: errors.New("boom")},
	})
	if _, err := svc.ListAll(api.InstanceTypeAny); err == nil {
		t.Fatal("ListAll() expected error")
	}
}
