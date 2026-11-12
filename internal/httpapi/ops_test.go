package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestISOEndpoints(t *testing.T) {
	source := &incusfake.Server{
		Volumes: []api.StorageVolume{
			{Name: "debian-13", Type: "custom", ContentType: "iso", Description: "installer"},
		},
		ExtraInstances: []api.Instance{
			{Name: "installer", Type: "virtual-machine", Status: "Stopped", StatusCode: api.Stopped},
		},
		NetworkNames: []string{"incusbr0"},
	}
	target := &incusfake.Server{}
	f := newAPIFixture(t, source, target)
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/api/v1/clusters/c1/iso", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list iso status = %d body = %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Isos []map[string]any `json:"isos"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode iso list: %v", err)
	}
	if len(list.Isos) != 1 || list.Isos[0]["name"] != "debian-13" {
		t.Fatalf("isos = %#v", list.Isos)
	}

	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	part, _ := form.CreateFormFile("image", "alpine.iso")
	_, _ = part.Write([]byte("iso-bytes"))
	_ = form.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clusters/c1/iso?pool=default", body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.AddCookie(cookie)
	rec = f.serve(t, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import iso status = %d body = %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode iso import: %v", err)
	}
	if created.Name != "alpine" {
		t.Fatalf("created = %#v", created)
	}
	if len(source.CreateISOArgs()) != 1 || !source.CreateISOArgs()[0].Reader {
		t.Fatalf("import args = %#v", source.CreateISOArgs())
	}

	// Missing file part is a 400.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/clusters/c1/iso?pool=default", bytes.NewBufferString("x"))
	req.AddCookie(cookie)
	rec = f.serve(t, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("import iso without file status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodDelete, "/api/v1/clusters/c1/iso/default/debian-13", nil, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete iso status = %d", rec.Code)
	}
	if len(source.DeleteVolumeArgs()) != 1 || source.DeleteVolumeArgs()[0].Name != "debian-13" {
		t.Fatalf("delete args = %#v", source.DeleteVolumeArgs())
	}

	// Launch an installer VM from the ISO.
	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/iso/default/alpine/launch", map[string]any{
		"name": "installer", "network": "incusbr0",
	}, cookie, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("launch status = %d body = %s", rec.Code, rec.Body.String())
	}
	launched := source.CreateArgs()
	if len(launched) != 1 || launched[0].Type != api.InstanceTypeVM || launched[0].Source.Type != "none" {
		t.Fatalf("launch create = %#v", launched)
	}
	if launched[0].Devices["iso0"]["boot.priority"] != "1" || launched[0].Devices["iso0"]["source"] != "alpine" {
		t.Fatalf("iso device = %#v", launched[0].Devices["iso0"])
	}
	if launched[0].Devices["eth0"]["network"] != "incusbr0" {
		t.Fatalf("nic = %#v", launched[0].Devices["eth0"])
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/iso/default/alpine/launch", map[string]any{"name": ""}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("launch empty name status = %d", rec.Code)
	}
	rec = f.request(t, http.MethodGet, "/api/v1/clusters/missing/iso", nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("iso unknown cluster status = %d", rec.Code)
	}

	// Import without a pool falls back to validation errors, and deletion
	// validates its parameters.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/clusters/c1/iso", bytes.NewBufferString("x"))
	req.AddCookie(cookie)
	rec = f.serve(t, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("import iso without pool status = %d", rec.Code)
	}
	rec = f.request(t, http.MethodDelete, "/api/v1/clusters/c1/iso/default/%20", nil, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("delete iso empty name status = %d", rec.Code)
	}
	rec = f.request(t, http.MethodDelete, "/api/v1/clusters/missing/iso/default/x", nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete iso unknown cluster status = %d", rec.Code)
	}

	// Launch body validation.
	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/iso/default/alpine/launch", "not-json", cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("launch bad json status = %d", rec.Code)
	}

	// Refresh and rebuild validation branches.
	rec = f.request(t, http.MethodPost, "/api/v1/clusters/missing/images/abc/refresh", nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("refresh unknown cluster status = %d", rec.Code)
	}
	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/web/rebuild", "not-json", cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("rebuild bad json status = %d", rec.Code)
	}
	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/web/clone", "not-json", cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("clone bad json status = %d", rec.Code)
	}
	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/web/ops", map[string]any{
		"action": "set-password", "user": "root", "password": "x",
	}, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("set-password status = %d body = %s", rec.Code, rec.Body.String())
	}
	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/web/ops", map[string]any{
		"action": "inject-key", "user": "deploy", "key": "ssh-ed25519 AAA test",
	}, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("inject-key status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestOpsEndpoints(t *testing.T) {
	stopped := &incusfake.Server{
		Instance: &api.Instance{
			Name:       "web",
			Type:       "container",
			Status:     "Stopped",
			StatusCode: api.Stopped,
		},
		ExecOutput: "script output",
		Operation:  &incusfake.Operation{Metadata: map[string]any{"return": float64(0)}},
		Images:     []api.Image{{Fingerprint: "abc123def456", Type: "container"}},
		ExtraInstances: []api.Instance{
			{Name: "web-clone", Type: "container", Status: "Stopped", StatusCode: api.Stopped},
		},
	}
	running := &incusfake.Server{
		Instance: &api.Instance{
			Name:       "web",
			Type:       "container",
			Status:     "Running",
			StatusCode: api.Running,
		},
	}
	f := newAPIFixture(t, stopped, running)
	cookie := f.login(t)

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/web/ops", map[string]any{
		"action": "run-script", "script": "echo hi",
	}, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("ops status = %d body = %s", rec.Code, rec.Body.String())
	}
	var result struct {
		ExitCode int    `json:"exit_code"`
		Stdout   string `json:"stdout"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode ops result: %v", err)
	}
	if result.ExitCode != 0 || result.Stdout != "script output" {
		t.Fatalf("result = %#v", result)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/web/ops", map[string]any{
		"action": "nope",
	}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown action status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/web/ops", "not-json", cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/web/rebuild", map[string]any{
		"image": "debian/13",
	}, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("rebuild status = %d body = %s", rec.Code, rec.Body.String())
	}
	rebuilds := stopped.RebuildArgs()
	if len(rebuilds) != 1 || rebuilds[0].Source.Alias != "debian/13" {
		t.Fatalf("rebuild args = %#v", rebuilds)
	}

	// A running instance cannot be rebuilt.
	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c2/instances/web/rebuild", map[string]any{
		"image": "debian/13",
	}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("rebuild running status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/images/abc123def456/refresh", nil, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("refresh status = %d", rec.Code)
	}
	if len(stopped.RefreshImageArgs()) != 1 {
		t.Fatalf("refresh args = %#v", stopped.RefreshImageArgs())
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/web/clone", map[string]any{
		"target": "web-clone",
	}, cookie, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("clone status = %d body = %s", rec.Code, rec.Body.String())
	}
	created := stopped.CreateArgs()
	if len(created) != 1 || created[0].Source.Type != "copy" || created[0].Source.Source != "web" {
		t.Fatalf("clone create = %#v", created)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/web/clone", map[string]any{
		"target": "",
	}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("clone empty status = %d", rec.Code)
	}
}

func TestVgaHandshake(t *testing.T) {
	source := &incusfake.Server{
		Instance: &api.Instance{
			Name:       "vm",
			Type:       "virtual-machine",
			Status:     "Running",
			StatusCode: api.Running,
		},
		Operation: &incusfake.Operation{
			FDSecrets: map[string]string{"0": "data-secret", "control": "ctl-secret"},
		},
	}
	target := &incusfake.Server{}
	f := newAPIFixture(t, source, target)
	cookie := f.login(t)
	server := f.wsServer(t)

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	header := http.Header{"Cookie": []string{cookie.Name + "=" + cookie.Value}}
	vgaURL := "ws" + strings.TrimPrefix(server.URL, "http") +
		"/api/v1/clusters/c1/instances/vm/vga"
	browser, _, err := dialer.Dial(vgaURL, header)
	if err != nil {
		t.Fatalf("dial vga: %v", err)
	}
	_ = browser.Close()
}
