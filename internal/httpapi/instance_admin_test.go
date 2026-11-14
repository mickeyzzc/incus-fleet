package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
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
		"name": "eth1", "type": "nic", "network": "incusbr0", "ipv4": "10.0.0.9",
	}, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("attach nic status = %d body = %s", rec.Code, rec.Body.String())
	}
	nic := source.UpdateArgs()[2].Instance.Devices["eth1"]
	if nic["network"] != "incusbr0" || nic["ipv4.address"] != "10.0.0.9" {
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

func TestAdminErrorBranches(t *testing.T) {
	source := &incusfake.Server{}
	target := &incusfake.Server{}
	f := newAPIFixture(t, source, target)
	cookie := f.login(t)

	cases := []struct {
		name   string
		method string
		path   string
		body   any
		status int
	}{
		{name: "create snapshot unknown cluster", method: http.MethodPost, path: "/api/v1/clusters/missing/instances/web/snapshots", body: map[string]any{"name": "s"}, status: http.StatusNotFound},
		{name: "restore snapshot unknown cluster", method: http.MethodPost, path: "/api/v1/clusters/missing/instances/web/snapshots/s/restore", status: http.StatusNotFound},
		{name: "delete snapshot unknown cluster", method: http.MethodDelete, path: "/api/v1/clusters/missing/instances/web/snapshots/s", status: http.StatusNotFound},
		{name: "list volumes unknown cluster", method: http.MethodGet, path: "/api/v1/clusters/missing/storage-pools/default/volumes", status: http.StatusNotFound},
		{name: "create volume unknown cluster", method: http.MethodPost, path: "/api/v1/clusters/missing/storage-pools/default/volumes", body: map[string]any{"name": "v"}, status: http.StatusNotFound},
		{name: "delete volume unknown cluster", method: http.MethodDelete, path: "/api/v1/clusters/missing/storage-pools/default/volumes/v", status: http.StatusNotFound},
		{name: "attach device unknown cluster", method: http.MethodPost, path: "/api/v1/clusters/missing/instances/web/devices", body: map[string]any{"name": "d", "type": "disk", "path": "/d"}, status: http.StatusNotFound},
		{name: "detach device unknown cluster", method: http.MethodDelete, path: "/api/v1/clusters/missing/instances/web/devices/d", status: http.StatusNotFound},
		{name: "delete image unknown cluster", method: http.MethodDelete, path: "/api/v1/clusters/missing/images/abc", status: http.StatusNotFound},
		{name: "reconnect unknown cluster", method: http.MethodPost, path: "/api/v1/clusters/missing/reconnect", status: http.StatusNotFound},
		{name: "delete file relative path", method: http.MethodDelete, path: "/api/v1/clusters/c1/instances/web/files?path=relative", status: http.StatusBadRequest},
		{name: "upload without multipart", method: http.MethodPost, path: "/api/v1/clusters/c1/instances/web/files?path=/etc/motd", body: map[string]any{"x": 1}, status: http.StatusBadRequest},
	}

	for _, tc := range cases {
		rec := f.request(t, tc.method, tc.path, tc.body, cookie, "")
		if rec.Code != tc.status {
			t.Fatalf("%s: status = %d body = %s, want %d", tc.name, rec.Code, rec.Body.String(), tc.status)
		}
	}
}

func TestImageImportAndCopyEndpoints(t *testing.T) {
	source := &incusfake.Server{
		Images:    []api.Image{{Fingerprint: "abc123def456", Type: "container"}},
		Operation: &incusfake.Operation{Metadata: map[string]any{"fingerprint": "abc123def456"}},
	}
	target := &incusfake.Server{}
	f := newAPIFixture(t, source, target)
	cookie := f.login(t)

	importBody := &bytes.Buffer{}
	importForm := multipart.NewWriter(importBody)
	part, _ := importForm.CreateFormFile("image", "debian13.tar")
	_, _ = part.Write([]byte("raw-tarball-bytes"))
	_ = importForm.WriteField("type", "container")
	_ = importForm.Close()
	importReq := httptest.NewRequest(http.MethodPost,
		"/api/v1/clusters/c1/images/import?type=container&alias=debian%2F13", importBody)
	importReq.Header.Set("Content-Type", importForm.FormDataContentType())
	importReq.AddCookie(cookie)
	rec := f.serve(t, importReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode import response: %v", err)
	}
	if resp.Fingerprint != "abc123def456" {
		t.Fatalf("import response = %#v", resp)
	}
	created := source.CreateImageArgs()
	if len(created) != 1 || created[0].Filename != "debian13.tar" || created[0].Type != "container" || string(created[0].Content) != "raw-tarball-bytes" {
		t.Fatalf("import args = %#v", created[0])
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/images/abc123def456/copy", map[string]any{
		"target_cluster": "c2",
		"alias":          "debian/13",
	}, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("copy status = %d body = %s", rec.Code, rec.Body.String())
	}
	if len(target.CopyImageArgs()) != 1 || target.CopyImageArgs()[0].Fingerprint != "abc123def456" {
		t.Fatalf("copy args = %#v", target.CopyImageArgs())
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/images/import?filename=&type=container", "x", cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("import missing filename status = %d", rec.Code)
	}

	// A multipart body without an image part is rejected.
	noImageBody := &bytes.Buffer{}
	noImageForm := multipart.NewWriter(noImageBody)
	_ = noImageForm.WriteField("type", "container")
	_ = noImageForm.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clusters/c1/images/import?type=container", noImageBody)
	req.Header.Set("Content-Type", noImageForm.FormDataContentType())
	req.AddCookie(cookie)
	rec = f.serve(t, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("import without image part status = %d", rec.Code)
	}

	// Split uploads carry a rootfs part alongside the metadata.
	splitBody := &bytes.Buffer{}
	splitForm := multipart.NewWriter(splitBody)
	part, _ = splitForm.CreateFormFile("image", "deb13")
	_, _ = part.Write([]byte("meta-bytes"))
	part, _ = splitForm.CreateFormFile("rootfs", "deb13.root")
	_, _ = part.Write([]byte("rootfs-bytes"))
	_ = splitForm.Close()
	splitReq := httptest.NewRequest(http.MethodPost,
		"/api/v1/clusters/c1/images/import?type=container", splitBody)
	splitReq.Header.Set("Content-Type", splitForm.FormDataContentType())
	splitReq.AddCookie(cookie)
	rec = f.serve(t, splitReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("split import status = %d body = %s", rec.Code, rec.Body.String())
	}

	// An operation without fingerprint metadata answers 204 No Content.
	emptySource := &incusfake.Server{Operation: &incusfake.Operation{}}
	emptyFixture := newAPIFixture(t, emptySource, target)
	emptyCookie := emptyFixture.login(t)
	emptyBody := &bytes.Buffer{}
	emptyForm := multipart.NewWriter(emptyBody)
	part, _ = emptyForm.CreateFormFile("image", "debian13.tar")
	_, _ = part.Write([]byte("raw"))
	_ = emptyForm.Close()
	emptyReq := httptest.NewRequest(http.MethodPost, "/api/v1/clusters/c1/images/import?type=container", emptyBody)
	emptyReq.Header.Set("Content-Type", emptyForm.FormDataContentType())
	emptyReq.AddCookie(emptyCookie)
	rec = emptyFixture.serve(t, emptyReq)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("import without fingerprint status = %d", rec.Code)
	}

	// Malformed copy requests and refused copies surface as 400s.
	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/images/abc123def456/copy", "not-json", cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("copy bad json status = %d", rec.Code)
	}
	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/images/abc123def456/copy", map[string]any{
		"target_cluster": "c1",
	}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("copy same cluster status = %d body = %s", rec.Code, rec.Body.String())
	}
}
