package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/nodeaccess"
	"github.com/mickeyzzc/incus-fleet/internal/nodenet"
	"github.com/mickeyzzc/incus-fleet/internal/overlay"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

// fakeOverlay is a scripted OverlayService.
type fakeOverlay struct {
	statuses []*overlay.Status
	err      error
	enabled  *overlay.Request
	calls    []string
}

func (f *fakeOverlay) List(context.Context, string) ([]*overlay.Status, error) {
	f.calls = append(f.calls, "list")
	return f.statuses, f.err
}

func (f *fakeOverlay) Get(_ context.Context, _, name string) (*overlay.Status, error) {
	f.calls = append(f.calls, "get:"+name)
	if f.err != nil {
		return nil, f.err
	}
	if len(f.statuses) == 0 {
		return nil, overlay.ErrNotFound
	}
	return f.statuses[0], nil
}

func (f *fakeOverlay) Enable(_ context.Context, _ string, req overlay.Request) (*overlay.Status, error) {
	f.calls = append(f.calls, "enable")
	f.enabled = &req
	if f.err != nil {
		return nil, f.err
	}
	status := &overlay.Status{Name: req.Name, Enabled: true}
	return status, nil
}

func (f *fakeOverlay) Update(_ context.Context, _, name string, req overlay.Request) (*overlay.Status, error) {
	f.calls = append(f.calls, "update:"+name)
	if f.err != nil {
		return nil, f.err
	}
	return &overlay.Status{Name: name, Enabled: true, MTU: req.MTU, DHCPStart: req.DHCPStart, DHCPEnd: req.DHCPEnd}, nil
}

func (f *fakeOverlay) Reconcile(context.Context, string, string) (*overlay.Status, error) {
	f.calls = append(f.calls, "reconcile")
	if f.err != nil {
		return nil, f.err
	}
	return &overlay.Status{Enabled: true}, nil
}

func (f *fakeOverlay) Disable(context.Context, string, string) error {
	f.calls = append(f.calls, "disable")
	return f.err
}

func newOverlayFixture(t *testing.T, service OverlayService) (*apiFixture, *http.Cookie) {
	t.Helper()

	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, nil, func(deps *Deps) {
		deps.Overlay = service
	})

	return f, f.login(t)
}

func TestOverlayEndpointsRequireService(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/api/v1/clusters/c1/overlay", nil, cookie, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestOverlayListAndGet(t *testing.T) {
	service := &fakeOverlay{statuses: []*overlay.Status{{Cluster: "c1", Name: "private", Enabled: true}}}
	f, cookie := newOverlayFixture(t, service)

	rec := f.request(t, http.MethodGet, "/api/v1/clusters/c1/overlay", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"overlays"`) || !strings.Contains(rec.Body.String(), `"private"`) {
		t.Fatalf("list body = %s", rec.Body.String())
	}

	rec = f.request(t, http.MethodGet, "/api/v1/clusters/c1/overlay/private", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d body = %s", rec.Code, rec.Body.String())
	}

	// A missing overlay maps to 404.
	service.statuses = nil
	rec = f.request(t, http.MethodGet, "/api/v1/clusters/c1/overlay/ghost", nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestOverlayEnableValidatesBody(t *testing.T) {
	service := &fakeOverlay{}
	f, cookie := newOverlayFixture(t, service)

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/overlay", "not-an-object", cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body status = %d", rec.Code)
	}

	// Unknown fields are rejected by the strict decoder.
	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/overlay",
		map[string]any{"name": "private", "vni": 100, "subnet": "10.99.0.0/24", "gateway_member": "node-a", "bogus": true}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d", rec.Code)
	}
}

func TestOverlayEnableCreates(t *testing.T) {
	service := &fakeOverlay{}
	f, cookie := newOverlayFixture(t, service)

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/overlay", map[string]any{
		"name": "private", "vni": 6200, "subnet": "198.51.100.0/24", "gateway_member": "node-a",
	}, cookie, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("enable status = %d body = %s", rec.Code, rec.Body.String())
	}
	if service.enabled == nil || service.enabled.Name != "private" || service.enabled.VNI != 6200 {
		t.Fatalf("request = %#v", service.enabled)
	}
}

func TestOverlayReconcileAndDisable(t *testing.T) {
	service := &fakeOverlay{}
	f, cookie := newOverlayFixture(t, service)

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/overlay/private/reconcile", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reconcile status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = f.request(t, http.MethodDelete, "/api/v1/clusters/c1/overlay/private", nil, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("disable status = %d body = %s", rec.Code, rec.Body.String())
	}
	if len(service.calls) != 2 {
		t.Fatalf("calls = %#v", service.calls)
	}
}

func TestOverlayRequiresAuthentication(t *testing.T) {
	service := &fakeOverlay{}
	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, nil, func(deps *Deps) {
		deps.Overlay = service
	})

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/clusters/c1/overlay"},
		{http.MethodPost, "/api/v1/clusters/c1/overlay"},
		{http.MethodDelete, "/api/v1/clusters/c1/overlay/private"},
		{http.MethodPost, "/api/v1/clusters/c1/overlay/private/reconcile"},
	} {
		rec := f.request(t, tc.method, tc.path, nil, nil, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status = %d", tc.method, tc.path, rec.Code)
		}
	}
	if len(service.calls) != 0 {
		t.Fatalf("unauthenticated request reached the service: %#v", service.calls)
	}
}

func TestOverlayServiceErrorsMapToStatus(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"invalid", overlay.ErrInvalidRequest, http.StatusBadRequest},
		{"missing", overlay.ErrNotFound, http.StatusNotFound},
		{"unknown", errors.New("boom"), http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &fakeOverlay{err: tc.err}
			f, cookie := newOverlayFixture(t, service)
			rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/overlay", map[string]any{
				"name": "private", "vni": 1, "subnet": "198.51.100.0/24", "gateway_member": "node-a",
			}, cookie, "")
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d body = %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestOverlayHandlerErrorBranches(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   any
		err    error
		want   int
	}{
		{"list", http.MethodGet, "/api/v1/clusters/c1/overlay", nil, errors.New("boom"), http.StatusInternalServerError},
		{"get", http.MethodGet, "/api/v1/clusters/c1/overlay/x", nil, errors.New("boom"), http.StatusInternalServerError},
		{"reconcile", http.MethodPost, "/api/v1/clusters/c1/overlay/x/reconcile", nil, errors.New("boom"), http.StatusInternalServerError},
		{"disable", http.MethodDelete, "/api/v1/clusters/c1/overlay/x", nil, errors.New("boom"), http.StatusInternalServerError},
		{"reconcile missing", http.MethodPost, "/api/v1/clusters/c1/overlay/x/reconcile", nil, overlay.ErrNotFound, http.StatusNotFound},
		{"disable missing", http.MethodDelete, "/api/v1/clusters/c1/overlay/x", nil, overlay.ErrNotFound, http.StatusNotFound},
		{"reconcile invalid", http.MethodPost, "/api/v1/clusters/c1/overlay/x/reconcile", nil, overlay.ErrInvalidRequest, http.StatusBadRequest},
		{"disable invalid", http.MethodDelete, "/api/v1/clusters/c1/overlay/x", nil, overlay.ErrInvalidRequest, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &fakeOverlay{err: tc.err}
			f, cookie := newOverlayFixture(t, service)
			rec := f.request(t, tc.method, tc.path, tc.body, cookie, "")
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d body = %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// TestSessionUnauthorizedWithoutContext covers the branch where the session
// handler runs without an auth context (e.g. a misconfigured route wrapper).
func TestSessionUnauthorizedWithoutContext(t *testing.T) {
	api := &API{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	rec := httptest.NewRecorder()

	api.session(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestLinkInventoryLookupPropagatesNodeErrors covers the failure path of the
// resolver handed to enrichNetworks: an unconfigured node access must surface
// so the caller degrades to keeping NICs visible.
func TestLinkInventoryLookupPropagatesNodeErrors(t *testing.T) {
	api := &API{
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		nodenet: nodeaccess.New(nil, nodenet.NewService(nil), config.NodeAccess{}),
	}
	lookup := api.linkInventoryLookup(context.Background(), "c1")
	if _, err := lookup("missing-member"); err == nil {
		t.Fatal("expected an error for an unknown member")
	}
}

// TestListSessionsWithoutContext covers the guard on the sessions handler when
// it runs without an auth context.
func TestListSessionsWithoutContext(t *testing.T) {
	api := &API{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sessions", nil)
	rec := httptest.NewRecorder()

	api.listSessions(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
