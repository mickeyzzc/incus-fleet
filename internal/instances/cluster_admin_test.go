package instances

import (
	"errors"
	"reflect"
	"testing"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func strPtr(value string) *string { return &value }

func TestCreateStoragePool(t *testing.T) {
	srv := &incusfake.Server{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	req := StoragePoolRequest{
		Name:        "fast",
		Driver:      "zfs",
		Description: strPtr("fast pool"),
		Config:      map[string]string{"source": "tank", "volume.size": "50GiB"},
	}
	if err := svc.CreateStoragePool("c1", req); err != nil {
		t.Fatalf("CreateStoragePool() error = %v", err)
	}

	args := srv.CreatePoolArgs()
	if len(args) != 1 {
		t.Fatalf("CreatePoolArgs() = %#v", args)
	}
	if args[0].Name != "fast" || args[0].Driver != "zfs" || args[0].Description != "fast pool" {
		t.Fatalf("created pool = %#v", args[0])
	}
	if args[0].Config["source"] != "tank" || args[0].Config["volume.size"] != "50GiB" {
		t.Fatalf("created pool config = %#v", args[0].Config)
	}

	for _, invalid := range []StoragePoolRequest{
		{Driver: "dir"},
		{Name: "fast"},
		{Name: "bad name", Driver: "dir"},
	} {
		if err := svc.CreateStoragePool("c1", invalid); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("CreateStoragePool(%#v) error = %v, want ErrInvalidRequest", invalid, err)
		}
	}

	if err := svc.CreateStoragePool("missing", req); err == nil {
		t.Fatal("CreateStoragePool() expected unknown cluster error")
	}

	srv.CreatePoolErr = errors.New("boom")
	if err := svc.CreateStoragePool("c1", req); err == nil {
		t.Fatal("CreateStoragePool() expected server error")
	}
}

func TestUpdateStoragePool(t *testing.T) {
	srv := &incusfake.Server{Pools: []api.StoragePool{{
		Name:        "default",
		Driver:      "dir",
		Description: "old",
		Config:      api.ConfigMap{"source": "/srv/incus", "volume.size": "10GiB"},
	}}}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	req := StoragePoolRequest{
		Description: strPtr("new"),
		Config:      map[string]string{"source": "/srv/new", "volume.size": ""},
	}
	if err := svc.UpdateStoragePool("c1", "default", req); err != nil {
		t.Fatalf("UpdateStoragePool() error = %v", err)
	}

	args := srv.UpdatePoolArgs()
	if len(args) != 1 {
		t.Fatalf("UpdatePoolArgs() = %#v", args)
	}
	if args[0].Name != "default" || args[0].ETag != "pool-etag" || args[0].Pool.Description != "new" {
		t.Fatalf("updated pool = %#v", args[0])
	}
	if args[0].Pool.Config["source"] != "/srv/new" {
		t.Fatalf("source = %q", args[0].Pool.Config["source"])
	}
	if _, ok := args[0].Pool.Config["volume.size"]; ok {
		t.Fatalf("volume.size should be deleted: %#v", args[0].Pool.Config)
	}

	srv.UpdatePoolErr = errors.New("boom")
	if err := svc.UpdateStoragePool("c1", "default", req); err == nil {
		t.Fatal("UpdateStoragePool() expected server error")
	}

	if err := svc.UpdateStoragePool("c1", "", req); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty name error = %v", err)
	}
}

func TestDeleteStoragePool(t *testing.T) {
	srv := &incusfake.Server{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	if err := svc.DeleteStoragePool("c1", "scratch"); err != nil {
		t.Fatalf("DeleteStoragePool() error = %v", err)
	}
	if got := srv.DeletePoolArgs(); !reflect.DeepEqual(got, []string{"scratch"}) {
		t.Fatalf("DeletePoolArgs() = %#v", got)
	}

	if err := svc.DeleteStoragePool("c1", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty name error = %v", err)
	}

	srv.DeletePoolErr = errors.New("in use")
	if err := svc.DeleteStoragePool("c1", "scratch"); err == nil {
		t.Fatal("DeleteStoragePool() expected server error")
	}
}

func TestCreateNetwork(t *testing.T) {
	srv := &incusfake.Server{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	req := NetworkRequest{
		Name:        "br-test",
		Type:        "bridge",
		Description: strPtr("test bridge"),
		Config:      map[string]string{"ipv4.address": "10.99.0.1/24", "ipv6.address": "none"},
	}
	if err := svc.CreateNetwork("c1", req); err != nil {
		t.Fatalf("CreateNetwork() error = %v", err)
	}

	args := srv.CreateNetworkArgs()
	if len(args) != 1 {
		t.Fatalf("CreateNetworkArgs() = %#v", args)
	}
	if args[0].Name != "br-test" || args[0].Type != "bridge" || args[0].Description != "test bridge" {
		t.Fatalf("created network = %#v", args[0])
	}
	if args[0].Config["ipv4.address"] != "10.99.0.1/24" {
		t.Fatalf("created network config = %#v", args[0].Config)
	}

	for _, invalid := range []NetworkRequest{
		{Type: "bridge"},
		{Name: "br-test"},
		{Name: "bad name", Type: "bridge"},
		{Name: "br-test", Type: "bridge", Config: map[string]string{"ipv4.address": "nope"}},
		{Name: "br-test", Type: "bridge", Config: map[string]string{"ipv6.address": "10.0.0.1/24"}},
	} {
		if err := svc.CreateNetwork("c1", invalid); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("CreateNetwork(%#v) error = %v, want ErrInvalidRequest", invalid, err)
		}
	}

	srv.CreateNetworkErr = errors.New("boom")
	if err := svc.CreateNetwork("c1", req); err == nil {
		t.Fatal("CreateNetwork() expected server error")
	}
}

func TestUpdateNetwork(t *testing.T) {
	srv := &incusfake.Server{Networks: []api.Network{{
		Name:        "incusbr0",
		Type:        "bridge",
		Managed:     true,
		Description: "old",
		Config:      api.ConfigMap{"ipv4.address": "10.0.0.1/24", "ipv4.nat": "true"},
	}}}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	req := NetworkRequest{
		Description: strPtr("new"),
		Config:      map[string]string{"ipv4.nat": "", "ipv6.address": "none"},
	}
	if err := svc.UpdateNetwork("c1", "incusbr0", req); err != nil {
		t.Fatalf("UpdateNetwork() error = %v", err)
	}

	args := srv.UpdateNetworkArgs()
	if len(args) != 1 {
		t.Fatalf("UpdateNetworkArgs() = %#v", args)
	}
	if args[0].Name != "incusbr0" || args[0].ETag != "network-etag" || args[0].Network.Description != "new" {
		t.Fatalf("updated network = %#v", args[0])
	}
	if args[0].Network.Config["ipv4.address"] != "10.0.0.1/24" {
		t.Fatalf("ipv4.address = %q", args[0].Network.Config["ipv4.address"])
	}
	if _, ok := args[0].Network.Config["ipv4.nat"]; ok {
		t.Fatalf("ipv4.nat should be deleted: %#v", args[0].Network.Config)
	}
	if args[0].Network.Config["ipv6.address"] != "none" {
		t.Fatalf("ipv6.address = %q", args[0].Network.Config["ipv6.address"])
	}

	srv.Networks[0].Managed = false
	if err := svc.UpdateNetwork("c1", "incusbr0", req); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unmanaged network error = %v", err)
	}

	srv.Networks[0].Managed = true
	srv.UpdateNetworkErr = errors.New("boom")
	if err := svc.UpdateNetwork("c1", "incusbr0", req); err == nil {
		t.Fatal("UpdateNetwork() expected server error")
	}
}

func TestDeleteNetwork(t *testing.T) {
	srv := &incusfake.Server{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	if err := svc.DeleteNetwork("c1", "br-test"); err != nil {
		t.Fatalf("DeleteNetwork() error = %v", err)
	}
	if got := srv.DeleteNetworkArgs(); !reflect.DeepEqual(got, []string{"br-test"}) {
		t.Fatalf("DeleteNetworkArgs() = %#v", got)
	}

	if err := svc.DeleteNetwork("c1", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty name error = %v", err)
	}

	srv.DeleteNetworkErr = errors.New("in use")
	if err := svc.DeleteNetwork("c1", "br-test"); err == nil {
		t.Fatal("DeleteNetwork() expected server error")
	}
}

func TestProfiles(t *testing.T) {
	srv := &incusfake.Server{Profiles: []api.Profile{{
		Name:        "web",
		Description: "web profile",
		Config:      api.ConfigMap{"limits.cpu": "2"},
		Devices:     api.DevicesMap{"eth0": {"type": "nic", "network": "incusbr0"}},
		UsedBy:      []string{"/1.0/instances/web"},
	}}}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	profiles, err := svc.Profiles("c1")
	if err != nil {
		t.Fatalf("Profiles() error = %v", err)
	}
	if len(profiles) != 1 || profiles[0].Name != "web" || profiles[0].Description != "web profile" {
		t.Fatalf("profiles = %#v", profiles)
	}
	if profiles[0].Config["limits.cpu"] != "2" || profiles[0].Devices["eth0"]["network"] != "incusbr0" {
		t.Fatalf("profile = %#v", profiles[0])
	}
	if len(profiles[0].UsedBy) != 1 {
		t.Fatalf("used_by = %#v", profiles[0].UsedBy)
	}

	srv.ProfilesErr = errors.New("boom")
	if _, err := svc.Profiles("c1"); err == nil {
		t.Fatal("Profiles() expected server error")
	}
}

func TestCreateProfile(t *testing.T) {
	srv := &incusfake.Server{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	req := ProfileRequest{
		Name:        "web",
		Description: strPtr("web tier"),
		Config:      map[string]string{"limits.cpu": "2"},
		Devices: map[string]map[string]string{
			"root": {"type": "disk", "pool": "default", "path": "/"},
		},
	}
	if err := svc.CreateProfile("c1", req); err != nil {
		t.Fatalf("CreateProfile() error = %v", err)
	}

	args := srv.CreateProfileArgs()
	if len(args) != 1 {
		t.Fatalf("CreateProfileArgs() = %#v", args)
	}
	if args[0].Name != "web" || args[0].Description != "web tier" || args[0].Config["limits.cpu"] != "2" {
		t.Fatalf("created profile = %#v", args[0])
	}
	if args[0].Devices["root"]["pool"] != "default" {
		t.Fatalf("created profile devices = %#v", args[0].Devices)
	}

	for _, invalid := range []ProfileRequest{
		{},
		{Name: "bad name"},
		{Name: "web", Devices: map[string]map[string]string{"root": {"pool": "default"}}},
		{Name: "web", Devices: map[string]map[string]string{"bad name": {"type": "disk"}}},
	} {
		if err := svc.CreateProfile("c1", invalid); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("CreateProfile(%#v) error = %v, want ErrInvalidRequest", invalid, err)
		}
	}

	srv.CreateProfileErr = errors.New("boom")
	if err := svc.CreateProfile("c1", req); err == nil {
		t.Fatal("CreateProfile() expected server error")
	}
}

func TestUpdateProfile(t *testing.T) {
	srv := &incusfake.Server{Profile: &api.Profile{
		Name:        "web",
		Description: "old",
		Config:      api.ConfigMap{"limits.cpu": "2", "limits.memory": "1GiB"},
		Devices:     api.DevicesMap{"eth0": {"type": "nic", "network": "incusbr0"}},
	}}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	req := ProfileRequest{
		Description: strPtr("new"),
		Config:      map[string]string{"limits.cpu": "4", "limits.memory": ""},
		Devices: map[string]map[string]string{
			"root": {"type": "disk", "pool": "default", "path": "/"},
		},
	}
	if err := svc.UpdateProfile("c1", "web", req); err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}

	args := srv.UpdateProfileArgs()
	if len(args) != 1 {
		t.Fatalf("UpdateProfileArgs() = %#v", args)
	}
	if args[0].Name != "web" || args[0].ETag != "profile-etag" || args[0].Profile.Description != "new" {
		t.Fatalf("updated profile = %#v", args[0])
	}
	if args[0].Profile.Config["limits.cpu"] != "4" {
		t.Fatalf("limits.cpu = %q", args[0].Profile.Config["limits.cpu"])
	}
	if _, ok := args[0].Profile.Config["limits.memory"]; ok {
		t.Fatalf("limits.memory should be deleted: %#v", args[0].Profile.Config)
	}
	if len(args[0].Profile.Devices) != 1 || args[0].Profile.Devices["root"]["pool"] != "default" {
		t.Fatalf("devices = %#v", args[0].Profile.Devices)
	}

	req.Devices = nil
	if err := svc.UpdateProfile("c1", "web", req); err != nil {
		t.Fatalf("UpdateProfile() without devices error = %v", err)
	}
	args = srv.UpdateProfileArgs()
	if args[1].Profile.Devices["eth0"]["network"] != "incusbr0" {
		t.Fatalf("existing devices should be preserved: %#v", args[1].Profile.Devices)
	}

	if err := svc.UpdateProfile("c1", "", req); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty name error = %v", err)
	}

	srv.UpdateProfileErr = errors.New("boom")
	if err := svc.UpdateProfile("c1", "web", req); err == nil {
		t.Fatal("UpdateProfile() expected server error")
	}
}

func TestDeleteProfile(t *testing.T) {
	srv := &incusfake.Server{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	if err := svc.DeleteProfile("c1", "web"); err != nil {
		t.Fatalf("DeleteProfile() error = %v", err)
	}
	if got := srv.DeleteProfileArgs(); !reflect.DeepEqual(got, []string{"web"}) {
		t.Fatalf("DeleteProfileArgs() = %#v", got)
	}

	if err := svc.DeleteProfile("c1", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty name error = %v", err)
	}

	srv.DeleteProfileErr = errors.New("in use")
	if err := svc.DeleteProfile("c1", "web"); err == nil {
		t.Fatal("DeleteProfile() expected server error")
	}
}

func TestUpdateMember(t *testing.T) {
	srv := &incusfake.Server{Member: &api.ClusterMember{
		ServerName:    "node-a",
		Description:   "old",
		FailureDomain: "rack1",
		Roles:         []string{"database"},
		Groups:        []string{"g1"},
		Config:        api.ConfigMap{"scheduler.instance": "all"},
	}}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	groups := []string{"g2", "g3"}
	req := MemberUpdateRequest{
		Description: strPtr("new"),
		Groups:      &groups,
	}
	if err := svc.UpdateMember("c1", "node-a", req); err != nil {
		t.Fatalf("UpdateMember() error = %v", err)
	}

	args := srv.UpdateMemberArgs()
	if len(args) != 1 {
		t.Fatalf("UpdateMemberArgs() = %#v", args)
	}
	if args[0].Name != "node-a" || args[0].ETag != "member-etag" {
		t.Fatalf("updated member = %#v", args[0])
	}
	if args[0].Member.Description != "new" || args[0].Member.FailureDomain != "rack1" {
		t.Fatalf("member metadata = %#v", args[0].Member)
	}
	if !reflect.DeepEqual(args[0].Member.Roles, []string{"database"}) {
		t.Fatalf("roles = %#v", args[0].Member.Roles)
	}
	if !reflect.DeepEqual(args[0].Member.Groups, []string{"g2", "g3"}) {
		t.Fatalf("groups = %#v", args[0].Member.Groups)
	}
	if args[0].Member.Config["scheduler.instance"] != "all" {
		t.Fatalf("config = %#v", args[0].Member.Config)
	}

	if err := svc.UpdateMember("c1", "", req); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty name error = %v", err)
	}

	srv.MemberErr = errors.New("not found")
	if err := svc.UpdateMember("c1", "node-a", req); err == nil {
		t.Fatal("UpdateMember() expected lookup error")
	}
}
