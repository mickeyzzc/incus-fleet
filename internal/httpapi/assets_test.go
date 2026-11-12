package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestKeyAndScriptEndpoints(t *testing.T) {
	web := &incusfake.Server{
		Instance:   &api.Instance{Name: "web", Type: "container", Status: "Running", StatusCode: api.Running},
		ExecOutput: "ok",
		Operation:  &incusfake.Operation{Metadata: map[string]any{"return": float64(0)}},
	}
	target := &incusfake.Server{}
	f := newAPIFixture(t, web, target)
	cookie := f.login(t)

	// Key CRUD.
	rec := f.request(t, http.MethodPost, "/api/v1/keys", map[string]any{
		"name": "desk", "public_key": "ssh-ed25519 AAA desk", "comment": "workstation",
	}, cookie, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create key status = %d body = %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode key: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("created key = %s", rec.Body.String())
	}

	rec = f.request(t, http.MethodGet, "/api/v1/keys", nil, cookie, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "desk") {
		t.Fatalf("list keys status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = f.request(t, http.MethodPatch, "/api/v1/keys/"+created.ID, map[string]any{
		"name": "desk2", "public_key": "ssh-ed25519 BBB desk",
	}, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("patch key status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/keys", map[string]any{
		"name": "bad", "public_key": "garbage",
	}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad key status = %d", rec.Code)
	}

	// Script CRUD.
	rec = f.request(t, http.MethodPost, "/api/v1/scripts", map[string]any{
		"name": "init", "description": "bootstrap", "body": "#!/bin/sh\necho ok\n",
	}, cookie, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create script status = %d body = %s", rec.Code, rec.Body.String())
	}
	var script struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &script); err != nil {
		t.Fatalf("decode script: %v", err)
	}

	rec = f.request(t, http.MethodPatch, "/api/v1/scripts/"+script.ID, map[string]any{
		"name": "init", "body": "#!/bin/sh\necho v2\n",
	}, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("patch script status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodGet, "/api/v1/scripts", nil, cookie, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "v2") {
		t.Fatalf("list scripts status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Batch: inject two keys into one instance.
	rec = f.request(t, http.MethodPost, "/api/v1/batch/keys", map[string]any{
		"targets": []map[string]any{{"cluster": "c1", "instance": "web"}},
		"key_ids": []string{created.ID},
	}, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("batch keys status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("batch keys body = %s", rec.Body.String())
	}

	// Batch: run the stored script.
	rec = f.request(t, http.MethodPost, "/api/v1/batch/scripts", map[string]any{
		"targets":   []map[string]any{{"cluster": "c1", "instance": "web"}},
		"script_id": script.ID,
	}, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("batch scripts status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Batch: power action on an unknown action reports per-item failure.
	rec = f.request(t, http.MethodPost, "/api/v1/batch/power", map[string]any{
		"targets": []map[string]any{{"cluster": "c1", "instance": "web"}},
		"action":  "pause",
	}, cookie, "")
	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("batch power bad action status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Batch against an empty target list yields the validation error.
	rec = f.request(t, http.MethodPost, "/api/v1/batch/power", map[string]any{
		"targets": []map[string]any{},
		"action":  "start",
	}, cookie, "")
	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("batch power empty targets status = %d", rec.Code)
	}

	// Delete both assets.
	rec = f.request(t, http.MethodDelete, "/api/v1/keys/"+created.ID, nil, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete key status = %d", rec.Code)
	}
	rec = f.request(t, http.MethodDelete, "/api/v1/scripts/"+script.ID, nil, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete script status = %d", rec.Code)
	}
	rec = f.request(t, http.MethodDelete, "/api/v1/keys/"+created.ID, nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("double delete key status = %d", rec.Code)
	}
}
