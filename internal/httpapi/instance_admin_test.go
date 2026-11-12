package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestInstanceAdminEndpoints(t *testing.T) {
	source := &incusfake.Server{
		Instance: &api.Instance{
			Name:       "web",
			Type:       "container",
			Status:     "Running",
			StatusCode: api.Running,
			InstancePut: api.InstancePut{
				Profiles: []string{"default"},
				Devices: api.DevicesMap{
					"root": {"type": "disk", "path": "/", "pool": "default"},
					"eth0": {"type": "nic", "network": "incusbr0"},
				},
			},
		},
		Snapshots: []api.InstanceSnapshot{
			{Name: "web/snap0", Stateful: true, Size: 2048, CreatedAt: time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)},
		},
		Volumes: []api.StorageVolume{
			{Name: "data", Type: "custom", Description: "app data", Config: api.ConfigMap{"size": "10GiB"}},
			{Name: "web", Type: "container"},
		},
		NetworkNames: []string{"incusbr0"},
	}
	target := &incusfake.Server{}
	f := newAPIFixture(t, source, target)
	cookie := f.login(t)

	// Snapshots: list.
	rec := f.request(t, http.MethodGet, "/api/v1/clusters/c1/instances/web/snapshots", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list snapshots status = %d body = %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Snapshots []map[string]any `json:"snapshots"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode snapshots: %v", err)
	}
	if len(list.Snapshots) != 1 || list.Snapshots[0]["name"] != "snap0" || list.Snapshots[0]["stateful"] != true {
		t.Fatalf("snapshots = %#v", list.Snapshots)
	}

	// Snapshots: create, restore, delete.
	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/web/snapshots", map[string]any{
		"name":     "backup",
		"stateful": true,
	}, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("create snapshot status = %d body = %s", rec.Code, rec.Body.String())
	}
	created := source.CreateSnapshotArgs()
	if len(created) != 1 || created[0].Name != "backup" || !created[0].Stateful {
		t.Fatalf("create snapshot args = %#v", created)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/web/snapshots/snap0/restore", nil, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("restore snapshot status = %d body = %s", rec.Code, rec.Body.String())
	}
	updates := source.UpdateArgs()
	if len(updates) != 1 || updates[0].Instance.Restore != "snap0" {
		t.Fatalf("restore args = %#v", updates)
	}

	rec = f.request(t, http.MethodDelete, "/api/v1/clusters/c1/instances/web/snapshots/snap0", nil, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete snapshot status = %d body = %s", rec.Code, rec.Body.String())
	}
	deleted := source.DeleteSnapshotArgs()
	if len(deleted) != 1 || deleted[0].Name != "snap0" {
		t.Fatalf("delete snapshot args = %#v", deleted)
	}

	// Snapshot validation and missing instance.
	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/web/snapshots", map[string]any{"name": ""}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid snapshot status = %d body = %s", rec.Code, rec.Body.String())
	}
	rec = f.request(t, http.MethodGet, "/api/v1/clusters/c1/instances/missing/snapshots", nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing instance snapshots status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Volumes: list custom only.
	rec = f.request(t, http.MethodGet, "/api/v1/clusters/c1/storage-pools/default/volumes", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list volumes status = %d body = %s", rec.Code, rec.Body.String())
	}
	var volumeList struct {
		Volumes []map[string]any `json:"volumes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &volumeList); err != nil {
		t.Fatalf("decode volumes: %v", err)
	}
	if len(volumeList.Volumes) != 1 || volumeList.Volumes[0]["name"] != "data" {
		t.Fatalf("volumes = %#v", volumeList.Volumes)
	}

	// Volumes: create and delete.
	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/storage-pools/default/volumes", map[string]any{
		"name":        "assets",
		"description": "shared assets",
		"size":        "5GiB",
	}, cookie, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create volume status = %d body = %s", rec.Code, rec.Body.String())
	}
	createdVolumes := source.CreateVolumeArgs()
	if len(createdVolumes) != 1 || createdVolumes[0].Volume.Name != "assets" || createdVolumes[0].Volume.Type != "custom" || createdVolumes[0].Volume.Config["size"] != "5GiB" {
		t.Fatalf("create volume args = %#v", createdVolumes)
	}

	rec = f.request(t, http.MethodDelete, "/api/v1/clusters/c1/storage-pools/default/volumes/assets", nil, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete volume status = %d body = %s", rec.Code, rec.Body.String())
	}
	deletedVolumes := source.DeleteVolumeArgs()
	if len(deletedVolumes) != 1 || deletedVolumes[0].Type != "custom" || deletedVolumes[0].Name != "assets" {
		t.Fatalf("delete volume args = %#v", deletedVolumes)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/storage-pools/default/volumes", map[string]any{"name": "", "size": "5GiB"}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid volume status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Devices: attach a disk and a nic, then detach.
	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/web/devices", map[string]any{
		"name": "data", "type": "disk", "pool": "default", "source": "assets", "path": "/mnt/assets", "size": "5GiB",
	}, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("attach disk status = %d body = %s", rec.Code, rec.Body.String())
	}
	disk := source.UpdateArgs()[1].Instance.Devices["data"]
	if disk["type"] != "disk" || disk["path"] != "/mnt/assets" || disk["size"] != "5GiB" {
		t.Fatalf("attached disk = %#v", disk)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/web/devices", map[string]any{
		"name": "eth1", "type": "nic", "network": "incusbr0", "ipv4": "10.0.0.0.9",
	}, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("attach nic status = %d body = %s", rec.Code, rec.Body.String())
	}
	nic := source.UpdateArgs()[2].Instance.Devices["eth1"]
	if nic["network"] != "incusbr0" || nic["ipv4.address"] != "10.0.0.0.9" {
		t.Fatalf("attached nic = %#v", nic)
	}

	// Reflect the attached device in the fake so the detach can remove it.
	withData := *source.Instance
	withData.Devices = api.DevicesMap{
		"root": {"type": "disk", "path": "/", "pool": "default"},
		"eth0": {"type": "nic", "network": "incusbr0"},
		"data": {"type": "disk", "pool": "default", "source": "assets", "path": "/mnt/assets", "size": "5GiB"},
	}
	source.Instance = &withData

	rec = f.request(t, http.MethodDelete, "/api/v1/clusters/c1/instances/web/devices/data", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("detach status = %d body = %s", rec.Code, rec.Body.String())
	}
	detached := source.UpdateArgs()[3].Instance.Devices
	if _, ok := detached["data"]; ok {
		t.Fatalf("detach args = %#v", detached)
	}

	rec = f.request(t, http.MethodDelete, "/api/v1/clusters/c1/instances/web/devices/nope", nil, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("detach missing status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Images: delete.
	rec = f.request(t, http.MethodDelete, "/api/v1/clusters/c1/images/053059ea4e28", nil, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete image status = %d body = %s", rec.Code, rec.Body.String())
	}
	images := source.DeleteImageArgs()
	if len(images) != 1 || images[0] != "053059ea4e28" {
		t.Fatalf("delete image args = %#v", images)
	}
}
