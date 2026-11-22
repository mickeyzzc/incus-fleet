package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestAnswerProfileEndpoints(t *testing.T) {
	web := &incusfake.Server{Instance: &api.Instance{Name: "vm", Type: "virtual-machine", StatusCode: api.Stopped}}
	f := newAPIFixture(t, web, &incusfake.Server{})
	cookie := f.login(t)

	// Create.
	rec := f.request(t, http.MethodPost, "/api/v1/answer-profiles", map[string]any{
		"name": "lab", "profile": "debian-preseed", "username": "ops",
		"hostname": "lab-node", "timezone": "Asia/Shanghai", "is_default": true,
	}, cookie, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("decode created profile: %v, body = %s", err, rec.Body.String())
	}

	// List & get through the default endpoint.
	rec = f.request(t, http.MethodGet, "/api/v1/answer-profiles", nil, cookie, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "lab-node") {
		t.Fatalf("list status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Update.
	rec = f.request(t, http.MethodPatch, "/api/v1/answer-profiles/"+created.ID, map[string]any{
		"name": "lab-renamed", "profile": "debian-preseed", "username": "ops2",
	}, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Validation error -> 400 (invalid username).
	rec = f.request(t, http.MethodPost, "/api/v1/answer-profiles", map[string]any{
		"name": "bad", "profile": "debian-preseed", "username": "Bad User",
	}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid profile status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Delete.
	rec = f.request(t, http.MethodDelete, "/api/v1/answer-profiles/"+created.ID, nil, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec.Code)
	}
	rec = f.request(t, http.MethodGet, "/api/v1/answer-profiles", nil, cookie, "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "lab-renamed") {
		t.Fatalf("list after delete = %d %s", rec.Code, rec.Body.String())
	}
}

func TestPreviewUsesStoredAnswerProfile(t *testing.T) {
	web := &incusfake.Server{Instance: &api.Instance{Name: "vm", Type: "virtual-machine", StatusCode: api.Stopped}}
	f := newAPIFixture(t, web, &incusfake.Server{})
	cookie := f.login(t)

	// Store a default debian profile with a distinctive hostname.
	rec := f.request(t, http.MethodPost, "/api/v1/answer-profiles", map[string]any{
		"name": "lab-default", "profile": "debian-preseed", "username": "ops",
		"hostname": "stored-node", "is_default": true,
	}, cookie, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create profile status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Preview with no answer content picks up the profile default.
	rec = f.request(t, http.MethodPost,
		"/api/v1/clusters/c1/iso/default/debian/launch-unattended/preview",
		map[string]any{"name": "vm", "profile": "debian-preseed"}, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "stored-node") {
		t.Fatalf("preview did not use stored default: %s", rec.Body.String())
	}

	// Explicit answer content wins over the default.
	rec = f.request(t, http.MethodPost,
		"/api/v1/clusters/c1/iso/default/debian/launch-unattended/preview",
		map[string]any{"name": "vm", "profile": "debian-preseed", "answer_file": "# custom\nhostname: inline-node"}, cookie, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "inline-node") {
		t.Fatalf("preview did not honor inline answer: %d %s", rec.Code, rec.Body.String())
	}

	// Unknown explicit profile id -> 400.
	rec = f.request(t, http.MethodPost,
		"/api/v1/clusters/c1/iso/default/debian/launch-unattended/preview",
		map[string]any{"name": "vm", "profile": "debian-preseed", "answer_profile_id": "missing"}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("preview with unknown profile status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestAnswerProfileEndpointsRequireWriter(t *testing.T) {
	web := &incusfake.Server{Instance: &api.Instance{Name: "vm", Type: "virtual-machine", StatusCode: api.Stopped}}
	f := newAPIFixture(t, web, &incusfake.Server{})
	// No cookie: mutation endpoints must be gated.
	for _, method := range []string{http.MethodPost, http.MethodPatch, http.MethodDelete} {
		path := "/api/v1/answer-profiles"
		if method != http.MethodPost {
			path += "/x"
		}
		rec := f.request(t, method, path, map[string]any{}, nil, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s without auth = %d, want 401", method, rec.Code)
		}
	}
}
