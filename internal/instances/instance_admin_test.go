package instances

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestListSnapshots(t *testing.T) {
	srv := &incusfake.Server{
		Instance: manageInstance("web"),
		Snapshots: []api.InstanceSnapshot{
			{Name: "web/snap0", Stateful: true, Size: 1024, CreatedAt: time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)},
			{Name: "web/snap1"},
		},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	snapshots, err := svc.ListSnapshots("c1", "web")
	if err != nil {
		t.Fatalf("ListSnapshots() error = %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("snapshots = %#v", snapshots)
	}
	if snapshots[0].Name != "snap0" || !snapshots[0].Stateful || snapshots[0].Size != 1024 {
		t.Fatalf("snapshots[0] = %#v", snapshots[0])
	}
	if snapshots[0].CreatedAt != "2026-11-10T08:00:00Z" {
		t.Fatalf("createdAt = %q", snapshots[0].CreatedAt)
	}
	if snapshots[1].Name != "snap1" || snapshots[1].CreatedAt != "" {
		t.Fatalf("snapshots[1] = %#v", snapshots[1])
	}

	if _, err := svc.ListSnapshots("missing", "web"); err == nil {
		t.Fatal("ListSnapshots() expected unknown cluster error")
	}
}

func TestSnapshotLifecycle(t *testing.T) {
	srv := &incusfake.Server{Instance: manageInstance("web")}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	if err := svc.CreateSnapshot(context.Background(), "c1", "web", SnapshotRequest{Name: "backup", Stateful: true}); err != nil {
		t.Fatalf("CreateSnapshot() error = %v", err)
	}
	created := srv.CreateSnapshotArgs()
	if len(created) != 1 || created[0].Name != "backup" || !created[0].Stateful || created[0].Instance != "web" {
		t.Fatalf("create args = %#v", created)
	}

	if err := svc.RestoreSnapshot(context.Background(), "c1", "web", "backup"); err != nil {
		t.Fatalf("RestoreSnapshot() error = %v", err)
	}
	updates := srv.UpdateArgs()
	if len(updates) != 1 || updates[0].Instance.Restore != "backup" {
		t.Fatalf("restore update = %#v", updates)
	}

	if err := svc.DeleteSnapshot(context.Background(), "c1", "web", "backup"); err != nil {
		t.Fatalf("DeleteSnapshot() error = %v", err)
	}
	deleted := srv.DeleteSnapshotArgs()
	if len(deleted) != 1 || deleted[0].Name != "backup" || deleted[0].Instance != "web" {
		t.Fatalf("delete args = %#v", deleted)
	}
}

func TestSnapshotValidation(t *testing.T) {
	srv := &incusfake.Server{Instance: manageInstance("web")}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	if err := svc.CreateSnapshot(context.Background(), "c1", "web", SnapshotRequest{Name: ""}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("CreateSnapshot() expected name validation error, got %v", err)
	}
	if err := svc.CreateSnapshot(context.Background(), "c1", "web", SnapshotRequest{Name: "bad name!"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("CreateSnapshot() expected name validation error, got %v", err)
	}
	if err := svc.RestoreSnapshot(context.Background(), "c1", "web", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("RestoreSnapshot() expected name validation error, got %v", err)
	}

	srv.CreateSnapshotErr = errors.New("stateful not supported")
	if err := svc.CreateSnapshot(context.Background(), "c1", "web", SnapshotRequest{Name: "snap"}); err == nil || strings.Contains(err.Error(), "ErrInvalidRequest") {
		t.Fatalf("CreateSnapshot() expected propagated error, got %v", err)
	}
}

func TestListVolumes(t *testing.T) {
	srv := &incusfake.Server{
		Volumes: []api.StorageVolume{
			{Name: "data", Type: "custom", ContentType: "filesystem", Description: "app data", Config: api.ConfigMap{"size": "10GiB"}, UsedBy: []string{"/1.0/instances/web"}, Location: "node-1"},
			{Name: "web", Type: "container", ContentType: "filesystem"},
		},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	volumes, err := svc.Volumes("c1", "default")
	if err != nil {
		t.Fatalf("Volumes() error = %v", err)
	}
	if len(volumes) != 1 {
		t.Fatalf("expected only custom volumes, got %#v", volumes)
	}
	volume := volumes[0]
	if volume.Name != "data" || volume.Type != "custom" || volume.Description != "app data" || volume.Location != "node-1" {
		t.Fatalf("volume = %#v", volume)
	}
	if volume.Config["size"] != "10GiB" || len(volume.UsedBy) != 1 {
		t.Fatalf("volume detail = %#v", volume)
	}

	if _, err := svc.Volumes("missing", "default"); err == nil {
		t.Fatal("Volumes() expected unknown cluster error")
	}
}

func TestVolumeLifecycle(t *testing.T) {
	srv := &incusfake.Server{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	if err := svc.CreateVolume("c1", "default", VolumeRequest{Name: "data", Size: "10GiB", Description: "app data"}); err != nil {
		t.Fatalf("CreateVolume() error = %v", err)
	}
	created := srv.CreateVolumeArgs()
	if len(created) != 1 {
		t.Fatalf("create args = %#v", created)
	}
	args := created[0]
	if args.Pool != "default" || args.Volume.Name != "data" || args.Volume.Type != "custom" || args.Volume.Description != "app data" {
		t.Fatalf("volume args = %#v", args)
	}
	if args.Volume.Config["size"] != "10GiB" {
		t.Fatalf("volume config = %#v", args.Volume.Config)
	}

	if err := svc.DeleteVolume("c1", "default", "data"); err != nil {
		t.Fatalf("DeleteVolume() error = %v", err)
	}
	deleted := srv.DeleteVolumeArgs()
	if len(deleted) != 1 || deleted[0].Pool != "default" || deleted[0].Type != "custom" || deleted[0].Name != "data" {
		t.Fatalf("delete args = %#v", deleted)
	}
}

func TestVolumeValidation(t *testing.T) {
	srv := &incusfake.Server{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	if err := svc.CreateVolume("c1", "default", VolumeRequest{Name: ""}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("CreateVolume() expected name validation error, got %v", err)
	}
	if err := svc.CreateVolume("c1", "default", VolumeRequest{Name: "data", Size: "huge"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("CreateVolume() expected size validation error, got %v", err)
	}
	srv.CreateVolumeErr = errors.New("pool busy")
	if err := svc.CreateVolume("c1", "default", VolumeRequest{Name: "data"}); err == nil {
		t.Fatal("CreateVolume() expected propagated error")
	}
	srv.CreateVolumeErr = nil
	srv.DeleteVolumeErr = errors.New("volume in use")
	if err := svc.DeleteVolume("c1", "default", "data"); err == nil {
		t.Fatal("DeleteVolume() expected propagated error")
	}
}

func TestAttachDeviceVMProvisionsVolume(t *testing.T) {
	srv := &incusfake.Server{
		Instance:     manageInstance("vm1"),
		NetworkNames: []string{"incusbr0"},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})
	ctx := context.Background()

	// Pool + size without a source: fleet provisions the backing volume.
	detail, err := svc.AttachDevice(ctx, "c1", "vm1", DeviceRequest{
		Name: "data", Type: "disk", Pool: "default", Path: "/mnt/data", Size: "50GiB",
	})
	if err != nil {
		t.Fatalf("AttachDevice() error = %v", err)
	}
	if detail.Name != "vm1" {
		t.Fatalf("detail = %#v", detail)
	}

	created := srv.CreateVolumeArgs()
	if len(created) != 1 {
		t.Fatalf("created volumes = %#v", created)
	}
	volume := created[0].Volume
	if volume.Name != "dev-vm1-data" || volume.Type != "custom" || volume.Config["size"] != "50GiB" {
		t.Fatalf("volume = %#v", volume)
	}

	device := srv.UpdateArgs()[0].Instance.Devices["data"]
	if device["type"] != "disk" || device["pool"] != "default" || device["source"] != "dev-vm1-data" || device["path"] != "/mnt/data" {
		t.Fatalf("disk device = %#v", device)
	}
	if _, hasSize := device["size"]; hasSize {
		t.Fatalf("device should not carry size when backed by a volume: %#v", device)
	}

	// A rerun with the volume already present reuses it.
	reuse := &incusfake.Server{
		Instance: manageInstance("vm1"),
		Volumes: []api.StorageVolume{
			{Name: "dev-vm1-data", Type: "custom", ContentType: "filesystem"},
		},
	}
	reuseSvc := newTestService(t, map[string]*incusfake.Server{"c1": reuse})
	if _, err := reuseSvc.AttachDevice(ctx, "c1", "vm1", DeviceRequest{
		Name: "data", Type: "disk", Pool: "default", Path: "/mnt/data", Size: "50GiB",
	}); err != nil {
		t.Fatalf("reuse AttachDevice() error = %v", err)
	}
	if len(reuse.CreateVolumeArgs()) != 0 {
		t.Fatalf("existing volume was recreated: %#v", reuse.CreateVolumeArgs())
	}
}

func TestAttachDetachDevice(t *testing.T) {
	srv := &incusfake.Server{
		Instance:     manageInstance("web"),
		NetworkNames: []string{"incusbr0"},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})
	ctx := context.Background()

	detail, err := svc.AttachDevice(ctx, "c1", "web", DeviceRequest{
		Name: "data", Type: "disk", Pool: "default", Source: "data", Path: "/mnt/data", Size: "10GiB",
	})
	if err != nil {
		t.Fatalf("AttachDevice() error = %v", err)
	}
	if detail.Name != "web" {
		t.Fatalf("detail = %#v", detail)
	}

	updates := srv.UpdateArgs()
	if len(updates) != 1 {
		t.Fatalf("updates = %#v", updates)
	}
	device := updates[0].Instance.Devices["data"]
	if device["type"] != "disk" || device["pool"] != "default" || device["source"] != "data" || device["path"] != "/mnt/data" || device["size"] != "10GiB" {
		t.Fatalf("disk device = %#v", device)
	}

	detail, err = svc.AttachDevice(ctx, "c1", "web", DeviceRequest{Name: "eth1", Type: "nic", Network: "incusbr0", IPv4: "10.0.0.9"})
	if err != nil {
		t.Fatalf("AttachDevice() nic error = %v", err)
	}
	if detail.Name != "web" {
		t.Fatalf("nic detail = %#v", detail)
	}
	device = srv.UpdateArgs()[1].Instance.Devices["eth1"]
	if device["type"] != "nic" || device["network"] != "incusbr0" || device["ipv4.address"] != "10.0.0.9" {
		t.Fatalf("nic device = %#v", device)
	}

	// The fake is a recording stub: reflect the attached device in the served
	// instance so the detach has something to remove.
	withData := manageInstance("web")
	withData.Devices["data"] = map[string]string{"type": "disk", "pool": "default", "source": "data", "path": "/mnt/data", "size": "10GiB"}
	srv.Instance = withData

	detail, err = svc.DetachDevice(ctx, "c1", "web", "data")
	if err != nil {
		t.Fatalf("DetachDevice() error = %v", err)
	}
	if detail.Name != "web" {
		t.Fatalf("detach detail = %#v", detail)
	}
	last := srv.UpdateArgs()[2].Instance.Devices
	if _, ok := last["data"]; ok {
		t.Fatalf("detached device still present: %#v", last)
	}
}

func TestAttachDeviceValidation(t *testing.T) {
	srv := &incusfake.Server{
		Instance:     manageInstance("web"),
		NetworkNames: []string{"incusbr0"},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})
	ctx := context.Background()

	cases := []struct {
		name string
		req  DeviceRequest
		want string
	}{
		{name: "missing name", req: DeviceRequest{Type: "disk", Path: "/data"}, want: "name"},
		{name: "bad type", req: DeviceRequest{Name: "x", Type: "usb"}, want: "type"},
		{name: "duplicate", req: DeviceRequest{Name: "root", Type: "disk", Path: "/data"}, want: "already"},
		{name: "disk without path", req: DeviceRequest{Name: "data", Type: "disk"}, want: "path"},
		{name: "nic without network", req: DeviceRequest{Name: "eth1", Type: "nic"}, want: "network"},
		{name: "nic unknown network", req: DeviceRequest{Name: "eth1", Type: "nic", Network: "nope"}, want: "does not exist"},
		{name: "bad ipv4", req: DeviceRequest{Name: "eth1", Type: "nic", Network: "incusbr0", IPv4: "999.1.1.1"}, want: "ipv4"},
	}

	for _, tc := range cases {
		_, err := svc.AttachDevice(ctx, "c1", "web", tc.req)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want substring %q", tc.name, err, tc.want)
		}
	}

	srv.UpdateErr = errors.New("update failed")
	if _, err := svc.AttachDevice(ctx, "c1", "web", DeviceRequest{Name: "data", Type: "disk", Path: "/data"}); err == nil {
		t.Fatal("AttachDevice() expected propagated update error")
	}
}

func TestDetachDeviceValidation(t *testing.T) {
	srv := &incusfake.Server{Instance: manageInstance("web")}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	if _, err := svc.DetachDevice(context.Background(), "c1", "web", "missing"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("DetachDevice() expected invalid request, got %v", err)
	}
}

func TestDeleteImage(t *testing.T) {
	srv := &incusfake.Server{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	if err := svc.DeleteImage(context.Background(), "c1", "053059ea4e28"); err != nil {
		t.Fatalf("DeleteImage() error = %v", err)
	}
	deleted := srv.DeleteImageArgs()
	if len(deleted) != 1 || deleted[0] != "053059ea4e28" {
		t.Fatalf("delete args = %#v", deleted)
	}

	srv.DeleteImageErr = errors.New("image in use")
	if err := svc.DeleteImage(context.Background(), "c1", "053059ea4e28"); err == nil {
		t.Fatal("DeleteImage() expected propagated error")
	}
}

func TestSnapshotErrorPaths(t *testing.T) {
	opErr := &incusfake.Operation{Err: errors.New("operation failed")}
	srv := &incusfake.Server{Instance: manageInstance("web"), Operation: opErr}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})
	ctx := context.Background()

	if _, err := svc.ListSnapshots("c1", "other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ListSnapshots() unknown instance = %v", err)
	}

	if err := svc.CreateSnapshot(ctx, "missing", "web", SnapshotRequest{Name: "snap"}); err == nil {
		t.Fatal("CreateSnapshot() expected unknown cluster error")
	}
	if err := svc.CreateSnapshot(ctx, "c1", "web", SnapshotRequest{Name: "snap"}); err == nil {
		t.Fatal("CreateSnapshot() expected operation error")
	}
	srv.SnapshotsErr = errors.New("snapshots down")
	if _, err := svc.ListSnapshots("c1", "web"); err == nil {
		t.Fatal("ListSnapshots() expected snapshot list error")
	}
	srv.SnapshotsErr = nil

	srv.Operation = nil
	srv.InstanceErr = errors.New("get failed")
	if err := svc.RestoreSnapshot(ctx, "c1", "web", "snap"); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("RestoreSnapshot() get error = %v", err)
	}
	if err := svc.RestoreSnapshot(ctx, "missing", "web", "snap"); err == nil {
		t.Fatal("RestoreSnapshot() expected unknown cluster error")
	}
	if err := svc.DeleteSnapshot(ctx, "missing", "web", "snap"); err == nil {
		t.Fatal("DeleteSnapshot() expected unknown cluster error")
	}

	srv.InstanceErr = nil
	srv.Operation = opErr
	if err := svc.RestoreSnapshot(ctx, "c1", "web", "snap"); err == nil {
		t.Fatal("RestoreSnapshot() expected operation error")
	}
	if err := svc.DeleteSnapshot(ctx, "c1", "web", "snap"); err == nil {
		t.Fatal("DeleteSnapshot() expected operation error")
	}
}

func TestVolumeErrorPaths(t *testing.T) {
	srv := &incusfake.Server{VolumesErr: errors.New("volumes down")}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	if _, err := svc.Volumes("c1", "default"); err == nil {
		t.Fatal("Volumes() expected volumes error")
	}
	if err := svc.DeleteVolume("c1", "default", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("DeleteVolume() empty name = %v", err)
	}
	if err := svc.DeleteVolume("missing", "default", "data"); err == nil {
		t.Fatal("DeleteVolume() expected unknown cluster error")
	}
	if err := svc.CreateVolume("missing", "default", VolumeRequest{Name: "data"}); err == nil {
		t.Fatal("CreateVolume() expected unknown cluster error")
	}
}

func TestDeviceAndImageErrorPaths(t *testing.T) {
	opErr := &incusfake.Operation{Err: errors.New("operation failed")}
	srv := &incusfake.Server{Instance: manageInstance("web"), Operation: opErr}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})
	ctx := context.Background()

	if _, err := svc.AttachDevice(ctx, "missing", "web", DeviceRequest{Name: "x", Type: "disk", Path: "/d"}); err == nil {
		t.Fatal("AttachDevice() expected unknown cluster error")
	}

	srv.InstanceErr = api.StatusErrorf(http.StatusNotFound, "Instance %q not found", "web")
	if _, err := svc.AttachDevice(ctx, "c1", "web", DeviceRequest{Name: "x", Type: "disk", Path: "/d"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AttachDevice() missing instance = %v", err)
	}
	if _, err := svc.DetachDevice(ctx, "c1", "web", "data"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DetachDevice() missing instance = %v", err)
	}

	srv.InstanceErr = errors.New("get failed")
	if _, err := svc.AttachDevice(ctx, "c1", "web", DeviceRequest{Name: "x", Type: "disk", Path: "/d"}); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("AttachDevice() get error = %v", err)
	}

	srv.InstanceErr = nil
	srv.UpdateErr = errors.New("update failed")
	if _, err := svc.DetachDevice(ctx, "c1", "web", "root"); err == nil {
		t.Fatal("DetachDevice() expected update error")
	}

	srv.UpdateErr = nil
	srv.Operation = opErr
	if _, err := svc.AttachDevice(ctx, "c1", "web", DeviceRequest{Name: "x", Type: "disk", Path: "/d"}); err == nil {
		t.Fatal("AttachDevice() expected operation error")
	}

	srv.Operation = nil
	if err := svc.DeleteImage(ctx, "missing", "fp"); err == nil {
		t.Fatal("DeleteImage() expected unknown cluster error")
	}
	if err := svc.DeleteImage(ctx, "c1", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("DeleteImage() empty fingerprint = %v", err)
	}
	srv.DeleteImageErr = errors.New("image in use")
	if err := svc.DeleteImage(ctx, "c1", "fp"); err == nil {
		t.Fatal("DeleteImage() expected propagated error")
	}
	srv.DeleteImageErr = nil
	srv.Operation = opErr
	if err := svc.DeleteImage(ctx, "c1", "fp"); err == nil {
		t.Fatal("DeleteImage() expected operation error")
	}
}

func TestImageTransfer(t *testing.T) {
	source := &incusfake.Server{
		Instance: &api.Instance{
			Name:       "web",
			Type:       "container",
			Status:     "Running",
			StatusCode: api.Running,
			InstancePut: api.InstancePut{
				Profiles: []string{"default"},
			},
		},
		Images:    []api.Image{{Fingerprint: "abc123def456", Filename: "debian13.tar", Type: "container"}},
		Operation: &incusfake.Operation{Metadata: map[string]any{"fingerprint": "abc123def456"}},
	}
	target := &incusfake.Server{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": source, "c2": target})
	ctx := context.Background()

	fingerprint, err := svc.ImportImage(ctx, "c1", "debian13.tar", "container", "debian/13", strings.NewReader("tarball-bytes"), nil)
	if err != nil {
		t.Fatalf("ImportImage() error = %v", err)
	}
	if fingerprint != "abc123def456" {
		t.Fatalf("fingerprint = %q", fingerprint)
	}
	created := target.CreateImageArgs()
	if len(created) != 0 {
		t.Fatalf("target create args = %#v", created)
	}
	sourceCreated := source.CreateImageArgs()
	if len(sourceCreated) != 1 || sourceCreated[0].Filename != "debian13.tar" || sourceCreated[0].Type != "container" || string(sourceCreated[0].Content) != "tarball-bytes" {
		t.Fatalf("import args = %#v", sourceCreated[0])
	}
	if len(sourceCreated[0].Aliases) != 1 || sourceCreated[0].Aliases[0].Name != "debian/13" {
		t.Fatalf("aliases = %#v", sourceCreated[0].Aliases)
	}

	if _, err := svc.ImportImage(ctx, "c1", "debian13.tar", "iso", "x", strings.NewReader("x"), nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ImportImage() bad type = %v", err)
	}
	if _, err := svc.ImportImage(ctx, "c1", "", "container", "", strings.NewReader("x"), nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ImportImage() empty filename = %v", err)
	}
	if _, err := svc.ImportImage(ctx, "missing", "f", "container", "", strings.NewReader("x"), nil); err == nil {
		t.Fatal("ImportImage() expected unknown cluster error")
	}

	// Split uploads carry a separate rootfs tarball next to the metadata.
	rootfsImport := &incusfake.Server{
		Operation: &incusfake.Operation{Metadata: map[string]any{"fingerprint": "rootfs9feed"}},
	}
	rootfsSvc := newTestService(t, map[string]*incusfake.Server{"c1": rootfsImport})
	fingerprint, err = rootfsSvc.ImportImage(ctx, "c1", "deb13", "container", "", strings.NewReader("meta"), strings.NewReader("rootfs"))
	if err != nil || fingerprint != "rootfs9feed" {
		t.Fatalf("ImportImage() split = %q, %v", fingerprint, err)
	}
	if len(rootfsImport.CreateImageArgs()) != 1 {
		t.Fatal("ImportImage() split upload not recorded")
	}

	source.CreateImageErr = errors.New("daemon refused upload")
	if _, err := svc.ImportImage(ctx, "c1", "debian13.tar", "container", "", strings.NewReader("x"), nil); err == nil {
		t.Fatal("ImportImage() expected create error")
	}
	source.CreateImageErr = nil

	// An operation that fails while waiting surfaces the daemon error.
	opFail := &incusfake.Server{Operation: &incusfake.Operation{Err: errors.New("unpack failed")}}
	failSvc := newTestService(t, map[string]*incusfake.Server{"c1": opFail})
	if _, err := failSvc.ImportImage(ctx, "c1", "debian13.tar", "container", "", strings.NewReader("x"), nil); err == nil {
		t.Fatal("ImportImage() expected operation error")
	}

	// An operation without fingerprint metadata completes with an empty
	// result (the API layer answers 204).
	opPlain := &incusfake.Server{Operation: &incusfake.Operation{}}
	plainSvc := newTestService(t, map[string]*incusfake.Server{"c1": opPlain})
	if fingerprint, err = plainSvc.ImportImage(ctx, "c1", "debian13.tar", "container", "", strings.NewReader("x"), nil); err != nil || fingerprint != "" {
		t.Fatalf("ImportImage() plain op = %q, %v", fingerprint, err)
	}

	target.CopyImageErr = errors.New("copy refused")
	if err := svc.CopyImageTo(ctx, "c1", "abc123", "c2", ""); err == nil {
		t.Fatal("CopyImageTo() expected propagated error")
	}
	target.CopyImageErr = nil

	if err := svc.CopyImageTo(ctx, "c1", "abc123", "c2", "debian/13"); err != nil {
		t.Fatalf("CopyImageTo() error = %v", err)
	}
	// Two calls were recorded: the refused attempt and the successful one.
	copied := target.CopyImageArgs()
	if len(copied) != 2 || copied[1].Fingerprint != "abc123def456" {
		t.Fatalf("copy args = %#v", copied)
	}
	if len(copied[1].Aliases) != 1 || copied[1].Aliases[0].Name != "debian/13" {
		t.Fatalf("copy aliases = %#v", copied[1].Aliases)
	}

	if err := svc.CopyImageTo(ctx, "c1", "abc123", "c1", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("CopyImageTo() same cluster = %v", err)
	}
	if err := svc.CopyImageTo(ctx, "c1", "", "c2", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("CopyImageTo() empty fingerprint = %v", err)
	}
	if err := svc.CopyImageTo(ctx, "missing", "abc", "c2", ""); err == nil {
		t.Fatal("CopyImageTo() expected unknown source cluster error")
	}

	source.Images = nil
	if err := svc.CopyImageTo(ctx, "c1", "abc123", "c2", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CopyImageTo() missing image = %v", err)
	}
	source.Images = []api.Image{{Fingerprint: "abc123def456", Type: "container"}}

	// Proxy port-forward devices validate listen/connect endpoints.
	if _, err := svc.AttachDevice(ctx, "c1", "web", DeviceRequest{Name: "web80", Type: "proxy", Listen: "tcp:0.0.0.0:8080", Connect: "tcp:127.0.0.1:80"}); err != nil {
		t.Fatalf("AttachDevice() proxy error = %v", err)
	}
	proxy := source.UpdateArgs()[len(source.UpdateArgs())-1].Instance.Devices["web80"]
	if proxy["type"] != "proxy" || proxy["listen"] != "tcp:0.0.0.0:8080" || proxy["connect"] != "tcp:127.0.0.1:80" {
		t.Fatalf("proxy device = %#v", proxy)
	}
	bad := []DeviceRequest{
		{Name: "p", Type: "proxy", Listen: "tcp:0.0.0.0:8080"},
		{Name: "p", Type: "proxy", Connect: "tcp:127.0.0.1:80"},
		{Name: "p", Type: "proxy", Listen: "http://x:80", Connect: "tcp:127.0.0.1:80"},
		{Name: "p", Type: "proxy", Listen: "tcp:0.0.0.0:notaport", Connect: "tcp:127.0.0.1:80"},
	}
	for _, req := range bad {
		if _, err := svc.AttachDevice(ctx, "c1", "web", req); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("AttachDevice() invalid proxy %s = %v", req.Listen, err)
		}
	}

	// An unknown instance surfaces the daemon lookup error.
	source.InstanceErr = errors.New("missing")
	if _, err := svc.AttachDevice(ctx, "c1", "web", DeviceRequest{Name: "x", Type: "proxy", Listen: "tcp:0.0.0.0:8443", Connect: "tcp:127.0.0.1:443"}); err == nil {
		t.Fatal("AttachDevice() expected instance lookup error")
	}
	source.InstanceErr = nil

	// A remote transfer that fails while waiting propagates the daemon error.
	target.RemoteOperation = &incusfake.RemoteOperation{Err: errors.New("transfer aborted")}
	if err := svc.CopyImageTo(ctx, "c1", "abc123", "c2", ""); err == nil {
		t.Fatal("CopyImageTo() expected remote wait error")
	}

	// A transfer that never finishes is cut off by the context deadline.
	target.RemoteOperation = &incusfake.RemoteOperation{WaitFunc: func() error {
		<-ctx.Done()
		return ctx.Err()
	}}
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := svc.CopyImageTo(cancelCtx, "c1", "abc123", "c2", ""); err == nil {
		t.Fatal("CopyImageTo() expected context cancellation error")
	}
	target.RemoteOperation = nil
}
