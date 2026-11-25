package httpapi

import (
	"context"
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

func TestRenameClusterDisplayName(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)

	rec := f.request(t, http.MethodPatch, "/api/v1/clusters/c1", map[string]string{
		"display_name": "  Production  ",
	}, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("rename status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"display_name":"Production"`) {
		t.Fatalf("rename body = %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"c1"`) {
		t.Fatalf("rename changed the identifier: %s", rec.Body.String())
	}

	names, err := f.store.ListClusterDisplayNames(context.Background())
	if err != nil {
		t.Fatalf("ListClusterDisplayNames() error = %v", err)
	}
	if names["c1"] != "Production" {
		t.Fatalf("persisted names = %#v", names)
	}

	empty := f.request(t, http.MethodPatch, "/api/v1/clusters/c1", map[string]string{"display_name": " "}, cookie, "")
	if empty.Code != http.StatusBadRequest {
		t.Fatalf("empty display name status = %d", empty.Code)
	}
	long := f.request(t, http.MethodPatch, "/api/v1/clusters/c1", map[string]string{
		"display_name": strings.Repeat("x", 65),
	}, cookie, "")
	if long.Code != http.StatusBadRequest {
		t.Fatalf("long display name status = %d", long.Code)
	}
	unknown := f.request(t, http.MethodPatch, "/api/v1/clusters/missing", map[string]string{
		"display_name": "Missing",
	}, cookie, "")
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown cluster status = %d body = %s", unknown.Code, unknown.Body.String())
	}
}

func TestRenameClusterWithoutStoreAndStoreErrors(t *testing.T) {
	// Config-file clusters can be renamed without a persistent store; the
	// change is simply not persisted across restarts.
	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, nil, func(deps *Deps) {
		deps.Store = nil
	})
	cookie := f.login(t)

	rec := f.request(t, http.MethodPatch, "/api/v1/clusters/c1", map[string]string{
		"display_name": "Transient",
	}, cookie, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Transient") {
		t.Fatalf("rename without store = %d %s", rec.Code, rec.Body.String())
	}

	// A store failure must leave the runtime name untouched.
	failing := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	fcookie := failing.login(t)
	if err := failing.store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	rec = failing.request(t, http.MethodPatch, "/api/v1/clusters/c1", map[string]string{
		"display_name": "Lost",
	}, fcookie, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("rename with closed store = %d %s", rec.Code, rec.Body.String())
	}

	badJSON := failing.request(t, http.MethodPatch, "/api/v1/clusters/c1", "{not json", fcookie, "")
	if badJSON.Code != http.StatusBadRequest {
		t.Fatalf("rename bad JSON status = %d", badJSON.Code)
	}
}

func TestListClustersAppliesStoredDisplayNames(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)

	if err := f.store.SetClusterDisplayName(context.Background(), "c1", "Shared edge"); err != nil {
		t.Fatalf("SetClusterDisplayName() error = %v", err)
	}

	rec := f.request(t, http.MethodGet, "/api/v1/clusters", nil, cookie, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Shared edge") {
		t.Fatalf("list clusters = %d %s", rec.Code, rec.Body.String())
	}

	detail := f.request(t, http.MethodGet, "/api/v1/clusters/c1", nil, cookie, "")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), "Shared edge") {
		t.Fatalf("cluster detail = %d %s", detail.Code, detail.Body.String())
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
