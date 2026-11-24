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

// TestPreviewAnswerProfileEndpoint covers the profile-editor preview, which
// renders an unsaved form without a cluster or ISO in the path.
func TestPreviewAnswerProfileEndpoint(t *testing.T) {
	web := &incusfake.Server{Instance: &api.Instance{Name: "vm", Type: "virtual-machine", StatusCode: api.Stopped}}
	f := newAPIFixture(t, web, &incusfake.Server{})
	cookie := f.login(t)
	path := "/api/v1/answer-profiles/preview"

	rec := f.request(t, http.MethodPost, path, map[string]any{
		"profile": "ubuntu-autoinstall",
		"spec": map[string]any{
			"Hostname": "preview-node", "Username": "ops", "Password": "Test@12345",
		},
	}, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Content  string `json:"content"`
		FileName string `json:"file_name"`
		Profile  string `json:"profile"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if body.Profile != "ubuntu-autoinstall" || body.FileName != "user-data" {
		t.Fatalf("preview metadata = %#v", body)
	}
	if !strings.Contains(body.Content, "preview-node") {
		t.Fatalf("preview content = %q", body.Content)
	}

	// Inline answers are echoed unchanged.
	rec = f.request(t, http.MethodPost, path, map[string]any{
		"profile": "debian-preseed", "answer_file": "# custom preseed\n",
	}, cookie, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "# custom preseed") {
		t.Fatalf("inline preview status = %d body = %s", rec.Code, rec.Body.String())
	}

	for _, tc := range []struct {
		name string
		body any
	}{
		{"unknown profile", map[string]any{"profile": "not-a-profile"}},
		{"invalid spec", map[string]any{"profile": "debian-preseed", "spec": map[string]any{"Username": "Bad User"}}},
		{"malformed body", "not-an-object"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := f.request(t, http.MethodPost, path, tc.body, cookie, "")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
			}
		})
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

// TestAnswerProfileErrorBranches covers the malformed-body and missing-row
// paths that the happy-path test does not reach.
func TestAnswerProfileErrorBranches(t *testing.T) {
	web := &incusfake.Server{Instance: &api.Instance{Name: "vm", Type: "virtual-machine", StatusCode: api.Stopped}}
	f := newAPIFixture(t, web, &incusfake.Server{})
	cookie := f.login(t)

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   any
		want   int
	}{
		{"create bad body", http.MethodPost, "/api/v1/answer-profiles", "not-an-object", http.StatusBadRequest},
		{"create unknown field", http.MethodPost, "/api/v1/answer-profiles", map[string]any{"bogus": 1}, http.StatusBadRequest},
		{"update bad body", http.MethodPatch, "/api/v1/answer-profiles/x", "{", http.StatusBadRequest},
		{"update missing", http.MethodPatch, "/api/v1/answer-profiles/missing", map[string]any{
			"name": "x", "profile": "debian-preseed", "username": "ops",
		}, http.StatusNotFound},
		{"delete missing", http.MethodDelete, "/api/v1/answer-profiles/missing", nil, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := f.request(t, tc.method, tc.path, tc.body, cookie, "")
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d body = %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// TestLaunchUnattendedValidation covers the handler's reject paths. The full
// flow needs real ISO media, so this pins the request validation that runs
// before any host work starts.
func TestLaunchUnattendedValidation(t *testing.T) {
	web := &incusfake.Server{Instance: &api.Instance{Name: "vm", Type: "virtual-machine", StatusCode: api.Stopped}}
	f := newAPIFixture(t, web, &incusfake.Server{})
	cookie := f.login(t)
	path := "/api/v1/clusters/c1/iso/default/debian/launch-unattended"

	// Malformed body.
	rec := f.request(t, http.MethodPost, path, "not-an-object", cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Unknown field is rejected by the strict decoder.
	rec = f.request(t, http.MethodPost, path, map[string]any{"bogus": true}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d body = %s", rec.Code, rec.Body.String())
	}

	// A missing answer profile reference is an invalid request.
	rec = f.request(t, http.MethodPost, path, map[string]any{
		"name": "auto-vm", "profile": "debian-preseed", "answer_profile_id": "missing",
	}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing profile status = %d body = %s", rec.Code, rec.Body.String())
	}
}

// TestAnswerProfileStoreFailure covers the service-error branch of the list
// handler by closing the store beneath the running API.
func TestAnswerProfileStoreFailure(t *testing.T) {
	web := &incusfake.Server{Instance: &api.Instance{Name: "vm", Type: "virtual-machine", StatusCode: api.Stopped}}
	f := newAPIFixture(t, web, &incusfake.Server{})
	cookie := f.login(t)

	// A successful read first, so the failure below is attributable.
	if rec := f.request(t, http.MethodGet, "/api/v1/answer-profiles", nil, cookie, ""); rec.Code != http.StatusOK {
		t.Fatalf("baseline list status = %d", rec.Code)
	}

	f.closeStore(t)

	rec := f.request(t, http.MethodGet, "/api/v1/answer-profiles", nil, cookie, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("closed-store list status = %d body = %s", rec.Code, rec.Body.String())
	}
}

// TestApplyAnswerProfileBranches covers the profile-resolution decisions:
// inline answers skip the store, an unknown explicit id is rejected, and a
// stored profile fills the request.
func TestApplyAnswerProfileBranches(t *testing.T) {
	web := &incusfake.Server{Instance: &api.Instance{Name: "vm", Type: "virtual-machine", StatusCode: api.Stopped}}
	f := newAPIFixture(t, web, &incusfake.Server{})
	cookie := f.login(t)

	// Create a stored profile to resolve later.
	rec := f.request(t, http.MethodPost, "/api/v1/answer-profiles", map[string]any{
		"name": "stored", "profile": "debian-preseed", "username": "ops", "is_default": true,
	}, cookie, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode id: %v", err)
	}

	base := "/api/v1/clusters/c1/iso/default/debian/launch-unattended"

	// Inline answers bypass the store entirely (request proceeds to ISO work
	// and fails there, which is past the profile resolution).
	rec = f.request(t, http.MethodPost, base, map[string]any{
		"name": "vm-inline", "profile": "debian-preseed", "answer_file": "d-i passwd/root-password password x",
	}, cookie, "")
	if rec.Code == http.StatusBadRequest && strings.Contains(rec.Body.String(), "answer profile") {
		t.Fatalf("inline answer must not consult profiles: %s", rec.Body.String())
	}

	// Explicit stored profile resolves.
	rec = f.request(t, http.MethodPost, base, map[string]any{
		"name": "vm-stored", "profile": "debian-preseed", "answer_profile_id": created.ID,
	}, cookie, "")
	if rec.Code == http.StatusBadRequest && strings.Contains(rec.Body.String(), "not found") {
		t.Fatalf("stored profile should resolve: %s", rec.Body.String())
	}

	// Explicit but unknown id is rejected as an invalid request.
	rec = f.request(t, http.MethodPost, base, map[string]any{
		"name": "vm-ghost", "profile": "debian-preseed", "answer_profile_id": "nope",
	}, cookie, "")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "not found") {
		t.Fatalf("unknown profile status = %d body = %s", rec.Code, rec.Body.String())
	}
}
