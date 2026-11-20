package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)


func TestUnregisterClusterRequiresStore(t *testing.T) {
	// Cluster registration is a runtime feature; without a persistent store the
	// endpoint must say so instead of failing later on a nil pointer.
	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, nil, func(deps *Deps) {
		deps.Store = nil
	})
	cookie := f.login(t)

	rec := f.request(t, http.MethodDelete, "/api/v1/clusters/c1", nil, cookie, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unregister without store status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "store_unavailable") {
		t.Fatalf("unregister without store body = %s", rec.Body.String())
	}
}

func TestRegisterClusterRequiresStoreAndName(t *testing.T) {
	// Registration is a runtime feature; without a persistent store the
	// endpoint must say so instead of silently losing the cluster on restart.
	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, nil, func(deps *Deps) {
		deps.Store = nil
	})
	cookie := f.login(t)

	rec := f.request(t, http.MethodPost, "/api/v1/clusters", map[string]any{"name": "x"}, cookie, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("register without store status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Name and endpoint validation runs on the normal (stored) fixture.
	validated := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	vcookie := validated.login(t)

	rec = validated.request(t, http.MethodPost, "/api/v1/clusters", map[string]any{"name": " "}, vcookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("register without name status = %d", rec.Code)
	}

	rec = validated.request(t, http.MethodPost, "/api/v1/clusters", map[string]any{"name": "x"}, vcookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("register without endpoint status = %d", rec.Code)
	}
}
