package instances

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func manageInstance(name string) *api.Instance {
	return &api.Instance{
		Name:       name,
		Type:       "virtual-machine",
		Status:     "Running",
		StatusCode: api.Running,
		InstancePut: api.InstancePut{
			Architecture: "x86_64",
			Description:  "before",
			Profiles:     []string{"default"},
			Config: api.ConfigMap{
				"limits.cpu":    "1",
				"limits.memory": "512MiB",
				"image.os":      "Debian",
			},
			Devices: api.DevicesMap{
				"root": {"type": "disk", "path": "/", "pool": "default", "size": "5GiB"},
				"eth0": {"type": "nic", "network": "incusbr0"},
			},
		},
		ExpandedConfig: api.ConfigMap{
			"limits.cpu":    "1",
			"limits.memory": "512MiB",
			"image.os":      "Debian",
		},
		ExpandedDevices: api.DevicesMap{
			"root": {"type": "disk", "path": "/", "pool": "default", "size": "5GiB"},
			"eth0": {"type": "nic", "network": "incusbr0"},
		},
	}
}

func TestCreate(t *testing.T) {
	srv := &incusfake.Server{
		Instance:     &api.Instance{Name: "vm1"},
		PoolNames:    []string{"default", "fast"},
		Networks: []api.Network{
			{Name: "incusbr0", Type: "bridge", Managed: true},
			{Name: "incusbr1", Type: "bridge", Managed: true},
		},
		Networks: []api.Network{
			{Name: "incusbr0", Type: "bridge", Managed: true},
			{Name: "incusbr1", Type: "bridge", Managed: true},
		},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	start := true
	detail, err := svc.Create(context.Background(), "c1", CreateRequest{
		Name:         "vm1",
		Type:         "virtual-machine",
		Image:        "053059ea4e28",
		Description:  "new vm",
		Profiles:     []string{"default"},
		CPU:          "2",
		Memory:       "2GiB",
		RootDiskSize: "10GiB",
		StoragePool:  "fast",
		Network:      "incusbr1",
		IPv4:         "10.0.0.50",
		IPv6:         "fd42::50",
		Start:        &start,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if detail.Name != "vm1" {
		t.Fatalf("Create() detail = %#v", detail)
	}

	args := srv.CreateArgs()
	if len(args) != 1 {
		t.Fatalf("CreateArgs() = %#v", args)
	}
	post := args[0]
	if post.Name != "vm1" || post.Type != api.InstanceTypeVM || !post.Start {
		t.Fatalf("post = %#v", post)
	}
	if post.Source.Type != "image" || post.Source.Fingerprint != "053059ea4e28" {
		t.Fatalf("source = %#v", post.Source)
	}
	if post.Description != "new vm" || len(post.Profiles) != 1 {
		t.Fatalf("put = %#v", post.InstancePut)
	}
	if post.Config["limits.cpu"] != "2" || post.Config["limits.memory"] != "2GiB" {
		t.Fatalf("config = %#v", post.Config)
	}
	root := post.Devices["root"]
	if root["pool"] != "fast" || root["size"] != "10GiB" || root["path"] != "/" {
		t.Fatalf("root = %#v", root)
	}
	nic := post.Devices["eth0"]
	if nic["network"] != "incusbr1" || nic["ipv4.address"] != "10.0.0.50" || nic["ipv6.address"] != "fd42::50" {
		t.Fatalf("nic = %#v", nic)
	}
}

func TestCreateDefaults(t *testing.T) {
	srv := &incusfake.Server{Instance: &api.Instance{Name: "ct1"}}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	if _, err := svc.Create(context.Background(), "c1", CreateRequest{Name: "ct1", Image: "debian"}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	post := srv.CreateArgs()[0]
	if post.Type != api.InstanceTypeContainer || !post.Start {
		t.Fatalf("post = %#v", post)
	}
	if _, ok := post.Devices["eth0"]; ok {
		t.Fatalf("unexpected nic = %#v", post.Devices)
	}
	if post.Devices["root"]["pool"] != "default" {
		t.Fatalf("root = %#v", post.Devices["root"])
	}
	if post.Config["limits.cpu"] != "" || post.Config["limits.memory"] != "" {
		t.Fatalf("config = %#v", post.Config)
	}
}

func TestCreateNoStart(t *testing.T) {
	srv := &incusfake.Server{Instance: &api.Instance{Name: "ct1"}}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})
	start := false

	if _, err := svc.Create(context.Background(), "c1", CreateRequest{Name: "ct1", Image: "debian", Start: &start}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if srv.CreateArgs()[0].Start {
		t.Fatal("Start = true, want false")
	}
}

func TestCreateValidation(t *testing.T) {
	srv := &incusfake.Server{
		Instance:     &api.Instance{Name: "ok"},
		PoolNames:    []string{"default"},
		Networks: []api.Network{{Name: "incusbr0", Type: "bridge", Managed: true}},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	for _, tt := range []struct {
		name string
		req  CreateRequest
		want string
	}{
		{"missing name", CreateRequest{Image: "debian"}, "name is required"},
		{"bad name", CreateRequest{Name: "bad/name", Image: "debian"}, "name"},
		{"bad type", CreateRequest{Name: "ok", Type: "lxc", Image: "debian"}, "type must be"},
		{"missing image", CreateRequest{Name: "ok"}, "image is required"},
		{"bad cpu", CreateRequest{Name: "ok", Image: "debian", CPU: "two"}, "cpu"},
		{"bad cpu range", CreateRequest{Name: "ok", Image: "debian", CPU: "3-1"}, "cpu"},
		{"bad memory", CreateRequest{Name: "ok", Image: "debian", Memory: "lots"}, "memory"},
		{"zero memory", CreateRequest{Name: "ok", Image: "debian", Memory: "0"}, "memory"},
		{"bad disk", CreateRequest{Name: "ok", Image: "debian", RootDiskSize: "huge"}, "root disk size"},
		{"missing pool", CreateRequest{Name: "ok", Image: "debian", StoragePool: "absent"}, "storage pool"},
		{"missing network", CreateRequest{Name: "ok", Image: "debian", Network: "absent"}, "network"},
		{"ip without network", CreateRequest{Name: "ok", Image: "debian", IPv4: "10.0.0.5"}, "network is required"},
		{"bad ipv4", CreateRequest{Name: "ok", Image: "debian", Network: "incusbr0", IPv4: "999.1.1.1"}, "ipv4"},
		{"ipv6 as ipv4", CreateRequest{Name: "ok", Image: "debian", Network: "incusbr0", IPv4: "fd42::5"}, "ipv4"},
		{"bad ipv6", CreateRequest{Name: "ok", Image: "debian", Network: "incusbr0", IPv6: "not-an-ip"}, "ipv6"},
		{"ipv4 as ipv6", CreateRequest{Name: "ok", Image: "debian", Network: "incusbr0", IPv6: "10.0.0.5"}, "ipv6"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Create(context.Background(), "c1", tt.req)
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("Create() error = %v, want ErrInvalidRequest", err)
			}
			if !strings.Contains(strings.ToLower(err.Error()), tt.want) {
				t.Fatalf("Create() error = %v, want %q", err, tt.want)
			}
		})
	}

	if _, err := svc.Create(context.Background(), "missing", CreateRequest{Name: "ok", Image: "debian"}); !errors.Is(err, clusters.ErrUnknownCluster) {
		t.Fatalf("Create() unknown cluster error = %v", err)
	}
}

func TestCreateErrors(t *testing.T) {
	srv := &incusfake.Server{Instance: &api.Instance{Name: "vm1"}, CreateErr: errors.New("boom")}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})
	if _, err := svc.Create(context.Background(), "c1", CreateRequest{Name: "vm1", Image: "debian"}); err == nil {
		t.Fatal("Create() expected operation error")
	}

	if _, err := svc.Create(context.Background(), "missing", CreateRequest{Name: "vm1", Image: "debian"}); err == nil {
		t.Fatal("Create() expected unknown cluster error")
	}

	srv = &incusfake.Server{Instance: &api.Instance{Name: "vm1"}, PoolNamesErr: errors.New("pool boom")}
	svc = newTestService(t, map[string]*incusfake.Server{"c1": srv})
	if _, err := svc.Create(context.Background(), "c1", CreateRequest{Name: "vm1", Image: "debian"}); err == nil {
		t.Fatal("Create() expected pool error")
	}

	srv = &incusfake.Server{Instance: &api.Instance{Name: "vm1"}, NetworksErr: errors.New("network boom")}
	svc = newTestService(t, map[string]*incusfake.Server{"c1": srv})
	if _, err := svc.Create(context.Background(), "c1", CreateRequest{Name: "vm1", Image: "debian", Network: "incusbr0"}); err == nil {
		t.Fatal("Create() expected network error")
	}
}

func TestUpdate(t *testing.T) {
	srv := &incusfake.Server{Instance: manageInstance("vm1"), Networks: []api.Network{
			{Name: "incusbr0", Type: "bridge", Managed: true},
			{Name: "incusbr1", Type: "bridge", Managed: true},
		}}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	cpu := "4"
	memory := "4GiB"
	size := "20GiB"
	network := "incusbr1"
	ipv4 := "10.0.0.60"
	ipv6 := "fd42::60"
	description := "after"

	if _, err := svc.Update(context.Background(), "c1", "vm1", UpdateRequest{
		CPU:          &cpu,
		Memory:       &memory,
		RootDiskSize: &size,
		Network:      &network,
		IPv4:         &ipv4,
		IPv6:         &ipv6,
		Description:  &description,
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	args := srv.UpdateArgs()
	if len(args) != 1 {
		t.Fatalf("UpdateArgs() = %#v", args)
	}
	put := args[0].Instance
	if args[0].Name != "vm1" {
		t.Fatalf("name = %q", args[0].Name)
	}
	if put.Config["limits.cpu"] != "4" || put.Config["limits.memory"] != "4GiB" {
		t.Fatalf("config = %#v", put.Config)
	}
	if put.Devices["root"]["size"] != "20GiB" || put.Devices["root"]["pool"] != "default" {
		t.Fatalf("root = %#v", put.Devices["root"])
	}
	if put.Devices["eth0"]["network"] != "incusbr1" || put.Devices["eth0"]["ipv4.address"] != "10.0.0.60" || put.Devices["eth0"]["ipv6.address"] != "fd42::60" {
		t.Fatalf("nic = %#v", put.Devices["eth0"])
	}
	if put.Description != "after" {
		t.Fatalf("description = %q", put.Description)
	}
}

func TestUpdateClearsLimitsAndIPs(t *testing.T) {
	instance := manageInstance("vm1")
	instance.Devices["eth0"]["ipv4.address"] = "10.0.0.60"
	instance.Devices["eth0"]["ipv6.address"] = "fd42::60"
	srv := &incusfake.Server{Instance: instance}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	empty := ""
	if _, err := svc.Update(context.Background(), "c1", "vm1", UpdateRequest{CPU: &empty, Memory: &empty, IPv4: &empty, IPv6: &empty}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	put := srv.UpdateArgs()[0].Instance
	if _, ok := put.Config["limits.cpu"]; ok {
		t.Fatalf("limits.cpu = %#v", put.Config)
	}
	if _, ok := put.Config["limits.memory"]; ok {
		t.Fatalf("limits.memory = %#v", put.Config)
	}
	if _, ok := put.Devices["eth0"]["ipv4.address"]; ok {
		t.Fatalf("eth0 = %#v", put.Devices["eth0"])
	}
	if _, ok := put.Devices["eth0"]["ipv6.address"]; ok {
		t.Fatalf("eth0 = %#v", put.Devices["eth0"])
	}
}

func TestUpdateUsesExpandedNIC(t *testing.T) {
	instance := manageInstance("vm1")
	delete(instance.Devices, "eth0")
	instance.ExpandedDevices["eth0"] = map[string]string{"type": "nic", "network": "incusbr0"}
	srv := &incusfake.Server{Instance: instance, Networks: []api.Network{
			{Name: "incusbr0", Type: "bridge", Managed: true},
			{Name: "incusbr1", Type: "bridge", Managed: true},
		}}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	device := "eth0"
	network := "incusbr1"
	if _, err := svc.Update(context.Background(), "c1", "vm1", UpdateRequest{Device: device, Network: &network}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	put := srv.UpdateArgs()[0].Instance
	if put.Devices["eth0"]["network"] != "incusbr1" {
		t.Fatalf("nic = %#v", put.Devices["eth0"])
	}
}

// A legacy nictype+parent NIC cannot carry "network"; switching networks must
// drop the legacy keys or Incus rejects the whole update.
func TestUpdateLegacyNICSwitchDropsNictype(t *testing.T) {
	instance := manageInstance("vm1")
	instance.Devices["eth0"] = map[string]string{"type": "nic", "nictype": "bridged", "parent": "incusbr0"}
	srv := &incusfake.Server{Instance: instance, Networks: []api.Network{
			{Name: "incusbr0", Type: "bridge", Managed: true},
			{Name: "incusbr1", Type: "bridge", Managed: true},
		}}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	network := "incusbr1"
	if _, err := svc.Update(context.Background(), "c1", "vm1", UpdateRequest{Device: "eth0", Network: &network}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	nic := srv.UpdateArgs()[0].Instance.Devices["eth0"]
	if nic["network"] != "incusbr1" {
		t.Fatalf("nic = %#v", nic)
	}
	if _, has := nic["nictype"]; has {
		t.Fatalf("nictype must be dropped on network switch: %#v", nic)
	}
	if _, has := nic["parent"]; has {
		t.Fatalf("parent must be dropped on network switch: %#v", nic)
	}
}

func TestUpdateValidation(t *testing.T) {
	srv := &incusfake.Server{Instance: manageInstance("vm1"), Networks: []api.Network{{Name: "incusbr0", Type: "bridge", Managed: true}}}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	badCPU := "two"
	badMemory := "lots"
	badSize := "huge"
	missingNetwork := "absent"
	badIPv4 := "999.1.1.1"
	badIPv6 := "nope"
	goodNetwork := "incusbr0"

	for _, tt := range []struct {
		name string
		req  UpdateRequest
		want string
	}{
		{"bad cpu", UpdateRequest{CPU: &badCPU}, "cpu"},
		{"bad memory", UpdateRequest{Memory: &badMemory}, "memory"},
		{"bad size", UpdateRequest{RootDiskSize: &badSize}, "root disk size"},
		{"missing network", UpdateRequest{Network: &missingNetwork}, "network"},
		{"bad ipv4", UpdateRequest{Network: &goodNetwork, IPv4: &badIPv4}, "ipv4"},
		{"bad ipv6", UpdateRequest{Network: &goodNetwork, IPv6: &badIPv6}, "ipv6"},
		{"missing device", UpdateRequest{Device: "eth9", Network: &goodNetwork}, "device"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Update(context.Background(), "c1", "vm1", tt.req)
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("Update() error = %v, want ErrInvalidRequest", err)
			}
			if !strings.Contains(strings.ToLower(err.Error()), tt.want) {
				t.Fatalf("Update() error = %v, want %q", err, tt.want)
			}
		})
	}

	if _, err := svc.Update(context.Background(), "c1", "missing", UpdateRequest{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update() missing error = %v", err)
	}
	if _, err := svc.Update(context.Background(), "missing", "vm1", UpdateRequest{}); err == nil {
		t.Fatal("Update() expected unknown cluster error")
	}
}

func TestUpdateOperationError(t *testing.T) {
	srv := &incusfake.Server{Instance: manageInstance("vm1"), UpdateErr: errors.New("boom")}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})
	if _, err := svc.Update(context.Background(), "c1", "vm1", UpdateRequest{}); err == nil {
		t.Fatal("Update() expected error")
	}
}

func TestAction(t *testing.T) {
	srv := &incusfake.Server{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	for _, action := range []string{"start", "stop", "restart"} {
		if err := svc.Action(context.Background(), "c1", "vm1", ActionRequest{Action: action, Force: true}); err != nil {
			t.Fatalf("Action(%q) error = %v", action, err)
		}
	}
	args := srv.UpdateStateArgs()
	if len(args) != 3 {
		t.Fatalf("UpdateStateArgs() = %#v", args)
	}
	for i, action := range []string{"start", "stop", "restart"} {
		if args[i].Action != action || !args[i].Force || args[i].Timeout == 0 {
			t.Fatalf("args[%d] = %#v", i, args[i])
		}
	}

	if err := svc.Action(context.Background(), "c1", "vm1", ActionRequest{Action: "explode"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Action() error = %v", err)
	}
	if err := svc.Action(context.Background(), "missing", "vm1", ActionRequest{Action: "start"}); err == nil {
		t.Fatal("Action() expected unknown cluster error")
	}

	srv.UpdateStateErr = errors.New("boom")
	if err := svc.Action(context.Background(), "c1", "vm1", ActionRequest{Action: "start"}); err == nil {
		t.Fatal("Action() expected operation error")
	}
}

func TestDelete(t *testing.T) {
	srv := &incusfake.Server{
		State: &api.InstanceState{Status: "Running", StatusCode: api.Running},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	if err := svc.Delete(context.Background(), "c1", "vm1", true, false); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	calls := srv.Calls()
	stopIndex, deleteIndex := -1, -1
	for i, call := range calls {
		if call == "UpdateInstanceState:stop" {
			stopIndex = i
		}
		if call == "DeleteInstance:vm1" {
			deleteIndex = i
		}
	}
	if stopIndex < 0 || deleteIndex < 0 || stopIndex > deleteIndex {
		t.Fatalf("calls = %v", calls)
	}
	if !srv.UpdateStateArgs()[0].Force {
		t.Fatal("stop not forced")
	}
}

func TestDeleteStopped(t *testing.T) {
	srv := &incusfake.Server{State: &api.InstanceState{Status: "Stopped", StatusCode: api.Stopped}}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	if err := svc.Delete(context.Background(), "c1", "vm1", false, false); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if srv.HasCall("UpdateInstanceState") {
		t.Fatalf("calls = %v", srv.Calls())
	}
}

func TestDeleteErrors(t *testing.T) {
	srv := &incusfake.Server{StateFunc: func(name string) (*api.InstanceState, error) {
		if name == "missing" {
			return nil, api.StatusErrorf(http.StatusNotFound, "Instance %q not found", name)
		}

		return &api.InstanceState{Status: "Stopped"}, nil
	}}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})
	if err := svc.Delete(context.Background(), "c1", "missing", false, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := svc.Delete(context.Background(), "missing", "vm1", false, false); err == nil {
		t.Fatal("Delete() expected unknown cluster error")
	}

	srv = &incusfake.Server{State: &api.InstanceState{Status: "Stopped"}, DeleteErr: errors.New("boom")}
	svc = newTestService(t, map[string]*incusfake.Server{"c1": srv})
	if err := svc.Delete(context.Background(), "c1", "vm1", false, false); err == nil {
		t.Fatal("Delete() expected delete error")
	}

	srv = &incusfake.Server{State: &api.InstanceState{Status: "Running"}, UpdateStateErr: errors.New("boom")}
	svc = newTestService(t, map[string]*incusfake.Server{"c1": srv})
	if err := svc.Delete(context.Background(), "c1", "vm1", false, false); err == nil {
		t.Fatal("Delete() expected stop error")
	}
}

func TestImages(t *testing.T) {
	srv := &incusfake.Server{Images: []api.Image{
		{
			Fingerprint:  "bbb",
			Aliases:      []api.ImageAlias{{Name: "debian"}},
			Architecture: "x86_64",
			Type:         "container",
			Size:         100,
			CreatedAt:    startedAt(),
			Properties:   map[string]string{"os": "Debian", "release": "trixie", "description": "Debian trixie"},
		},
		{
			Fingerprint:  "aaa",
			Architecture: "x86_64",
			Type:         "virtual-machine",
			Size:         200,
			CreatedAt:    startedAt().Add(time.Hour),
			Properties:   map[string]string{"os": "Debian", "release": "trixie"},
		},
	}}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	images, err := svc.Images("c1")
	if err != nil {
		t.Fatalf("Images() error = %v", err)
	}
	if len(images) != 2 {
		t.Fatalf("Images() = %#v", images)
	}
	if images[0].Fingerprint != "aaa" || images[0].Type != "virtual-machine" {
		t.Fatalf("images[0] = %#v", images[0])
	}
	if len(images[1].Aliases) != 1 || images[1].Aliases[0] != "debian" || images[1].OS != "Debian" || images[1].Description != "Debian trixie" {
		t.Fatalf("images[1] = %#v", images[1])
	}

	if _, err := svc.Images("missing"); err == nil {
		t.Fatal("Images() expected unknown cluster error")
	}

	srv.ImagesErr = errors.New("boom")
	if _, err := svc.Images("c1"); err == nil {
		t.Fatal("Images() expected error")
	}
}

func TestResources(t *testing.T) {
	srv := &incusfake.Server{
		Pools: []api.StoragePool{
			{Name: "fast", Driver: "zfs", Status: "Created", Config: api.ConfigMap{"source": "tank"}, UsedBy: []string{"/1.0/instances/vm1"}},
			{Name: "default", Driver: "dir", Config: api.ConfigMap{"source": "/srv/incus"}},
		},
		Networks: []api.Network{
			{Name: "incusbr0", Type: "bridge", Managed: true, Config: api.ConfigMap{"ipv4.address": "10.0.0.1/24", "ipv6.address": "none"}, UsedBy: []string{"/1.0/instances/vm1"}, Description: "main"},
		},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	resources, err := svc.Resources("c1")
	if err != nil {
		t.Fatalf("Resources() error = %v", err)
	}
	if len(resources.StoragePools) != 2 || resources.StoragePools[0].Name != "default" || resources.StoragePools[1].Name != "fast" {
		t.Fatalf("pools = %#v", resources.StoragePools)
	}
	if resources.StoragePools[0].Source != "/srv/incus" || resources.StoragePools[0].Total == 0 {
		t.Fatalf("default pool = %#v", resources.StoragePools[0])
	}
	if resources.StoragePools[1].Driver != "zfs" || len(resources.StoragePools[1].UsedBy) != 1 {
		t.Fatalf("fast pool = %#v", resources.StoragePools[1])
	}
	if len(resources.Networks) != 1 || resources.Networks[0].IPv4 != "10.0.0.1/24" || resources.Networks[0].IPv6 != "none" {
		t.Fatalf("networks = %#v", resources.Networks)
	}
	if !resources.Networks[0].Managed || resources.Networks[0].Description != "main" {
		t.Fatalf("network = %#v", resources.Networks[0])
	}
	if resources.StoragePools[0].Config["source"] != "/srv/incus" {
		t.Fatalf("default pool config = %#v", resources.StoragePools[0].Config)
	}
	if resources.Networks[0].Config["ipv4.address"] != "10.0.0.1/24" {
		t.Fatalf("network config = %#v", resources.Networks[0].Config)
	}

	if _, err := svc.Resources("missing"); err == nil {
		t.Fatal("Resources() expected unknown cluster error")
	}

	srv.PoolsErr = errors.New("boom")
	if _, err := svc.Resources("c1"); err == nil {
		t.Fatal("Resources() expected pool error")
	}

	srv = &incusfake.Server{NetworksErr: errors.New("boom")}
	svc = newTestService(t, map[string]*incusfake.Server{"c1": srv})
	if _, err := svc.Resources("c1"); err == nil {
		t.Fatal("Resources() expected network error")
	}

	srv = &incusfake.Server{PoolResourcesErr: errors.New("boom")}
	svc = newTestService(t, map[string]*incusfake.Server{"c1": srv})
	if _, err := svc.Resources("c1"); err == nil {
		t.Fatal("Resources() expected usage error")
	}
}

func TestClampUint64ToInt64(t *testing.T) {
	tests := []struct {
		name  string
		input uint64
		want  int64
	}{
		{name: "zero", input: 0, want: 0},
		{name: "fits", input: 1_000_000, want: 1_000_000},
		{name: "max int64", input: math.MaxInt64, want: math.MaxInt64},
		{name: "overflows", input: math.MaxUint64, want: math.MaxInt64},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := clampUint64ToInt64(test.input); got != test.want {
				t.Fatalf("clampUint64ToInt64(%d) = %d, want %d", test.input, got, test.want)
			}
		})
	}
}

func TestUpdateRootDiskFallsBackToDefaultPool(t *testing.T) {
	// The instance has no root device entry; the update must synthesize one
	// and fall back to the default pool (expandedValue fallback branch).
	srv := &incusfake.Server{
		Instance: &api.Instance{
			Name:            "vm1",
			Type:            "virtual-machine",
			Status:          "Stopped",
			StatusCode:      api.Stopped,
			ExpandedDevices: api.DevicesMap{},
		},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	size := "30GiB"
	if _, err := svc.Update(context.Background(), "c1", "vm1", UpdateRequest{RootDiskSize: &size}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	args := srv.UpdateArgs()
	if len(args) != 1 {
		t.Fatalf("UpdateArgs() = %#v", args)
	}
	root := args[0].Instance.Devices["root"]
	if root["size"] != "30GiB" || root["pool"] != "default" || root["path"] != "/" {
		t.Fatalf("root = %#v", root)
	}
}

func TestDeleteWithVolumes(t *testing.T) {
	instance := manageInstance("vm1")
	instance.Devices["data"] = map[string]string{"type": "disk", "pool": "default", "source": "vm1-data"}
	instance.Devices["iso0"] = map[string]string{"type": "disk", "pool": "default", "source": "installer"}

	newServer := func() *incusfake.Server {
		return &incusfake.Server{
			State:    &api.InstanceState{Status: "Stopped", StatusCode: api.Stopped},
			Instance: instance,
			Volumes: []api.StorageVolume{
				{Name: "vm1-data", ContentType: "block"},
				{Name: "installer", ContentType: "iso"},
			},
		}
	}

	t.Run("removes attached custom volumes but keeps ISO assets", func(t *testing.T) {
		srv := newServer()
		svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

		if err := svc.Delete(context.Background(), "c1", "vm1", false, true); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}

		calls := srv.Calls()
		deleted := false
		for _, call := range calls {
			if call == "DeleteStoragePoolVolume:default/custom/vm1-data" {
				deleted = true
			}
			if call == "DeleteStoragePoolVolume:default/custom/installer" {
				t.Fatalf("shared ISO asset must be kept: %#v", calls)
			}
		}
		if !deleted {
			t.Fatalf("attached custom volume not deleted: %#v", calls)
		}
	})

	t.Run("keeps volumes by default", func(t *testing.T) {
		srv := newServer()
		svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

		if err := svc.Delete(context.Background(), "c1", "vm1", false, false); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}

		for _, call := range srv.Calls() {
			if len(call) > 24 && call[:24] == "DeleteStoragePoolVolume:" {
				t.Fatalf("volume deleted without the option: %s", call)
			}
		}
	})

	t.Run("missing volumes are skipped silently", func(t *testing.T) {
		srv := newServer()
		srv.Volumes = nil // every GetStoragePoolVolume 404s
		svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

		if err := svc.Delete(context.Background(), "c1", "vm1", false, true); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
	})
}

// An unmanaged physical parent attaches as macvlan+parent — the create path's
// shape — because Incus rejects unmanaged names in the "network" key.
func TestUpdateUnmanagedNetworkUsesMacvlanParent(t *testing.T) {
	instance := manageInstance("vm1")
	srv := &incusfake.Server{
		Instance: instance,
		Networks: []api.Network{
			{Name: "incusbr0", Type: "bridge", Managed: true},
			{Name: "wan0", Type: "physical", Managed: false},
		},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	network := "wan0"
	if _, err := svc.Update(context.Background(), "c1", "vm1", UpdateRequest{Device: "eth0", Network: &network}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	nic := srv.UpdateArgs()[0].Instance.Devices["eth0"]
	if nic["nictype"] != "macvlan" || nic["parent"] != "wan0" {
		t.Fatalf("nic = %#v", nic)
	}
	if _, has := nic["network"]; has {
		t.Fatalf("unmanaged parent must not use the network key: %#v", nic)
	}
}

func TestUpdateUnmanagedNetworkRejectsStaticIP(t *testing.T) {
	instance := manageInstance("vm1")
	srv := &incusfake.Server{
		Instance: instance,
		Networks: []api.Network{
			{Name: "wan0", Type: "physical", Managed: false},
		},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	network := "wan0"
	ipv4 := "10.0.0.9"
	_, err := svc.Update(context.Background(), "c1", "vm1", UpdateRequest{Device: "eth0", Network: &network, IPv4: &ipv4})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Update() error = %v", err)
	}
	if len(srv.UpdateArgs()) != 0 {
		t.Fatalf("rejected update must not reach Incus")
	}
}
