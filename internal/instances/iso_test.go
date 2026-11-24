package instances

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestImportISO(t *testing.T) {
	source := &incusfake.Server{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": source})
	ctx := context.Background()

	name, err := svc.ImportISO(ctx, "c1", "default", "debian-13.1-amd64.iso", strings.NewReader("iso-bytes"))
	if err != nil {
		t.Fatalf("ImportISO() error = %v", err)
	}
	if name != "debian-13.1-amd64" {
		t.Fatalf("ImportISO() name = %q", name)
	}
	imports := source.CreateISOArgs()
	if len(imports) != 1 || imports[0].Pool != "default" || imports[0].Name != "debian-13.1-amd64" || !imports[0].Reader {
		t.Fatalf("import args = %#v", imports)
	}

	if _, err = svc.ImportISO(ctx, "c1", "default", "", strings.NewReader("x")); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ImportISO() empty filename = %v", err)
	}
	if _, err = svc.ImportISO(ctx, "c1", "", "a.iso", strings.NewReader("x")); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ImportISO() empty pool = %v", err)
	}
	if _, err = svc.ImportISO(ctx, "missing", "default", "a.iso", strings.NewReader("x")); err == nil {
		t.Fatal("ImportISO() expected unknown cluster error")
	}

	source.CreateISOErr = errors.New("disk full")
	if _, err = svc.ImportISO(ctx, "c1", "default", "a.iso", strings.NewReader("x")); err == nil {
		t.Fatal("ImportISO() expected daemon error")
	}

	// A failed import operation surfaces the daemon error.
	isoFail := &incusfake.Server{Operation: &incusfake.Operation{Err: errors.New("import failed")}}
	failSvc := newTestService(t, map[string]*incusfake.Server{"c1": isoFail})
	if _, err = failSvc.ImportISO(ctx, "c1", "default", "a.iso", strings.NewReader("x")); err == nil {
		t.Fatal("ImportISO() expected operation error")
	}
}

func TestListISOs(t *testing.T) {
	source := &incusfake.Server{
		Volumes: []api.StorageVolume{
			{Name: "debian-13", Type: "custom", ContentType: "iso", Description: "Debian 13 installer"},
			{Name: "data", Type: "custom", ContentType: "filesystem"},
		},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": source})

	isos, err := svc.ListISOs("c1")
	if err != nil {
		t.Fatalf("ListISOs() error = %v", err)
	}
	if len(isos) != 1 || isos[0].Name != "debian-13" || isos[0].Pool != "default" || isos[0].Description != "Debian 13 installer" {
		t.Fatalf("isos = %#v", isos)
	}
}

func TestDeleteISO(t *testing.T) {
	source := &incusfake.Server{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": source})

	if err := svc.DeleteISO("c1", "default", "debian-13"); err != nil {
		t.Fatalf("DeleteISO() error = %v", err)
	}
	deleted := source.DeleteVolumeArgs()
	if len(deleted) != 1 || deleted[0].Pool != "default" || deleted[0].Type != "custom" || deleted[0].Name != "debian-13" {
		t.Fatalf("delete args = %#v", deleted)
	}

	if err := svc.DeleteISO("c1", "default", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("DeleteISO() empty name = %v", err)
	}
	if err := svc.DeleteISO("missing", "default", "x"); err == nil {
		t.Fatal("DeleteISO() expected unknown cluster error")
	}
}

func TestLaunchFromISO(t *testing.T) {
	start := true
	source := &incusfake.Server{
		Instance: &api.Instance{
			Name:       "installer",
			Type:       "virtual-machine",
			Status:     "Running",
			StatusCode: api.Running,
		},
		NetworkNames: []string{"incusbr0"},
		Volumes: []api.StorageVolume{
			{Name: "debian-13", Type: "custom", Location: "node-a", ContentType: "iso"},
		},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": source})
	ctx := context.Background()

	detail, err := svc.LaunchFromISO(ctx, "c1", LaunchISORequest{
		Name:        "installer",
		ISOName:     "debian-13",
		StoragePool: "default",
		Network:     "incusbr0",
		RootDisk:    "20GiB",
		Start:       &start,
	})
	if err != nil {
		t.Fatalf("LaunchFromISO() error = %v", err)
	}
	if detail.Name != "installer" {
		t.Fatalf("detail = %#v", detail)
	}

	created := source.CreateArgs()
	if len(created) != 1 {
		t.Fatalf("create args = %#v", created)
	}
	post := created[0]
	if post.Type != api.InstanceTypeVM || post.Source.Type != "none" || !post.Start {
		t.Fatalf("post = %#v", post)
	}
	if post.Config["security.secureboot"] != "false" {
		t.Fatalf("secure boot = %#v", post.Config)
	}
	iso := post.Devices["iso0"]
	if iso["type"] != "disk" || iso["pool"] != "default" || iso["source"] != "debian-13" || iso["boot.priority"] != "1" {
		t.Fatalf("iso device = %#v", iso)
	}
	// Incus rejects a non-root disk device without a path, replying "Storage
	// volume not found" because it resolves the source as an instance volume.
	// Incus rejects a path on a custom ISO volume ("Custom ISO volumes cannot
	// have a path defined"), and rejects a source on the root disk. The device
	// must therefore carry neither a path nor the root slot.
	if iso["path"] != "" {
		t.Fatalf("iso device must not define a path: %#v", iso)
	}
	root := post.Devices["root"]
	if root["size"] != "20GiB" {
		t.Fatalf("root device = %#v", root)
	}
	if post.Devices["eth0"]["network"] != "incusbr0" {
		t.Fatalf("nic = %#v", post.Devices["eth0"])
	}

	if _, err = svc.LaunchFromISO(ctx, "c1", LaunchISORequest{Name: "", ISOName: "debian-13"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("LaunchFromISO() empty name = %v", err)
	}
	if _, err = svc.LaunchFromISO(ctx, "c1", LaunchISORequest{Name: "x", ISOName: ""}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("LaunchFromISO() empty iso = %v", err)
	}
	if _, err = svc.LaunchFromISO(ctx, "c1", LaunchISORequest{Name: "x", ISOName: "debian-13", StoragePool: "missing"}); err == nil {
		t.Fatal("LaunchFromISO() expected unknown pool error")
	}
}

func TestLaunchFromISOMissingVolume(t *testing.T) {
	source := &incusfake.Server{NetworkNames: []string{"incusbr0"}}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": source})

	_, err := svc.LaunchFromISO(context.Background(), "c1", LaunchISORequest{
		Name:        "installer",
		ISOName:     "does-not-exist",
		StoragePool: "default",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("LaunchFromISO() with a missing volume = %v, want ErrNotFound", err)
	}
}

// TestDeleteISOInUse maps Incus's opaque "still in use" rejection to an
// actionable conflict the API can show without leaking internal detail.
func TestDeleteISOInUse(t *testing.T) {
	srv := &incusfake.Server{DeleteVolumeErr: errors.New("The storage volume is still in use")}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	err := svc.DeleteISO("c1", "default", "attached")
	if !errors.Is(err, ErrInUse) {
		t.Fatalf("DeleteISO() error = %v, want ErrInUse", err)
	}
	if !strings.Contains(err.Error(), "detach") {
		t.Fatalf("DeleteISO() message is not actionable: %v", err)
	}
}

// TestDeleteISOValidationAndMissing covers the guard clauses and the
// not-found mapping on ISO deletion.
func TestDeleteISOValidationAndMissing(t *testing.T) {
	srv := &incusfake.Server{DeleteVolumeErr: api.StatusErrorf(http.StatusNotFound, "no such volume")}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	if err := svc.DeleteISO("c1", "default", "  "); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("DeleteISO(empty name) = %v", err)
	}
	if err := svc.DeleteISO("c1", "", "iso"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("DeleteISO(empty pool) = %v", err)
	}
	if err := svc.DeleteISO("zz", "default", "iso"); err == nil {
		t.Fatal("DeleteISO(unknown cluster) should fail")
	}
	if err := svc.DeleteISO("c1", "default", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteISO(missing volume) = %v", err)
	}
}

// TestISOVolumeMemberErrors covers both failure modes of member resolution:
// a missing volume and a generic lookup error.
func TestISOVolumeMemberErrors(t *testing.T) {
	missing := &incusfake.Server{VolumeErr: api.StatusErrorf(http.StatusNotFound, "missing")}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": missing})
	if _, err := svc.isoVolumeMember(missing, "default", "gone"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("isoVolumeMember(missing) = %v", err)
	}

	broken := &incusfake.Server{VolumeErr: errors.New("boom")}
	svc2 := newTestService(t, map[string]*incusfake.Server{"c1": broken})
	if _, err := svc2.isoVolumeMember(broken, "default", "x"); err == nil {
		t.Fatal("isoVolumeMember(generic error) should fail")
	}
}

// TestCreateISOInstanceCreateFailure covers the error wrap when the cluster
// rejects the VM creation.
func TestCreateISOInstanceCreateFailure(t *testing.T) {
	srv := &incusfake.Server{CreateErr: errors.New("quota exceeded")}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	_, err := svc.createISOInstance(context.Background(), srv, "c1",
		LaunchISORequest{Name: "vm1", ISOName: "iso"}, api.InstancesPost{Name: "vm1", Type: api.InstanceTypeVM})
	if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("createISOInstance() = %v", err)
	}
}

// TestLaunchFromISOValidation covers the required-field guards.
func TestLaunchFromISOValidation(t *testing.T) {
	srv := &incusfake.Server{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	if _, err := svc.LaunchFromISO(context.Background(), "c1", LaunchISORequest{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("LaunchFromISO(empty) = %v", err)
	}
	if _, err := svc.LaunchFromISO(context.Background(), "c1", LaunchISORequest{Name: "vm1"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("LaunchFromISO(no iso) = %v", err)
	}
	if _, err := svc.LaunchFromISO(context.Background(), "zz", LaunchISORequest{Name: "vm1", ISOName: "iso"}); err == nil {
		t.Fatal("LaunchFromISO(unknown cluster) should fail")
	}
}
