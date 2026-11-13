package httpapi

import (
	"errors"
	"net/http"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

// TestAssetErrorPaths exercises the decode and not-found branches of the
// asset and resource handlers that the happy-path tests do not reach.
func TestAssetErrorPaths(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"create key bad json", http.MethodPost, "/api/v1/keys", "{not json", http.StatusBadRequest},
		{"update key missing", http.MethodPatch, "/api/v1/keys/nope", `{"name":"n","public_key":"ssh-ed25519 AAA"}`, http.StatusNotFound},
		{"delete key missing", http.MethodDelete, "/api/v1/keys/nope", "", http.StatusNotFound},
		{"create script bad json", http.MethodPost, "/api/v1/scripts", "{not json", http.StatusBadRequest},
		{"update script missing", http.MethodPatch, "/api/v1/scripts/nope", `{"name":"n","body":"echo"}`, http.StatusNotFound},
		{"delete script missing", http.MethodDelete, "/api/v1/scripts/nope", "", http.StatusNotFound},
		{"batch keys bad json", http.MethodPost, "/api/v1/batch/keys", "{not json", http.StatusBadRequest},
		{"batch scripts bad json", http.MethodPost, "/api/v1/batch/scripts", "{not json", http.StatusBadRequest},
		{"batch power bad json", http.MethodPost, "/api/v1/batch/power", "{not json", http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body any
			if tc.body != "" {
				body = tc.body
			}
			rec := f.request(t, tc.method, tc.path, body, cookie, "")
			if rec.Code != tc.want {
				t.Fatalf("%s %s status = %d body = %s", tc.method, tc.path, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestResourceAdminErrorPaths covers the service-error branches of the
// storage pool, network, and profile admin handlers.
func TestResourceAdminErrorPaths(t *testing.T) {
	source := &incusfake.Server{
		UpdatePoolErr:     errors.New("pool busy"),
		DeletePoolErr:     errors.New("pool in use"),
		UpdateNetworkErr:  errors.New("network locked"),
		DeleteNetworkErr:  errors.New("network in use"),
		UpdateProfileErr:  errors.New("profile busy"),
		DeleteProfileErr:  errors.New("profile in use"),
	}
	f := newAPIFixture(t, source, &incusfake.Server{})
	cookie := f.login(t)

	cases := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{"update pool error", http.MethodPatch, "/api/v1/clusters/c1/storage-pools/default", http.StatusInternalServerError},
		{"delete pool error", http.MethodDelete, "/api/v1/clusters/c1/storage-pools/default", http.StatusInternalServerError},
		{"update network error", http.MethodPatch, "/api/v1/clusters/c1/networks/incusbr0", http.StatusInternalServerError},
		{"delete network error", http.MethodDelete, "/api/v1/clusters/c1/networks/incusbr0", http.StatusInternalServerError},
		{"update profile error", http.MethodPatch, "/api/v1/clusters/c1/profiles/default", http.StatusInternalServerError},
		{"delete profile error", http.MethodDelete, "/api/v1/clusters/c1/profiles/default", http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := f.request(t, tc.method, tc.path, map[string]any{}, cookie, "")
			if rec.Code != tc.want {
				t.Fatalf("%s %s status = %d body = %s", tc.method, tc.path, rec.Code, rec.Body.String())
			}
		})
	}
}
