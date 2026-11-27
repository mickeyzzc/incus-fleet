package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/migrations"
)

func serveDirect(t *testing.T, handler http.HandlerFunc, method, path, body string, pathValues map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range pathValues {
		req.SetPathValue(key, value)
	}
	rec := httptest.NewRecorder()
	handler(rec, req)

	return rec
}

func TestRemainingHandlerBranches(t *testing.T) {
	a, st, _ := directAPI(t)
	a.migrations = migrations.New(nil, st, a.logger)
	if rec := serveDirect(t, a.renameInstance, http.MethodPost, "/api/v1/clusters/c1/instances/web/rename", "{", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("rename invalid JSON = %d", rec.Code)
	}
	for _, handler := range []http.HandlerFunc{a.cancelMigration, a.retryMigration} {
		rec := serveDirect(t, handler, http.MethodPost, "/api/v1/migrations/missing", "", map[string]string{"id": "missing"})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("migration handler = %d body=%s", rec.Code, rec.Body.String())
		}
	}
	if rec := serveDirect(t, a.removeClusterMember, http.MethodDelete, "/api/v1/clusters/c1/members/node-a?force=bad", "", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("remove member bad force = %d", rec.Code)
	}
	if rec := serveDirect(t, a.updateOverlay, http.MethodPatch, "/api/v1/clusters/c1/overlay/o1", "{", nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("overlay unavailable = %d", rec.Code)
	}

	a.overlay = &fakeOverlay{err: errors.New("example overlay failure")}
	if rec := serveDirect(t, a.updateOverlay, http.MethodPatch, "/api/v1/clusters/c1/overlay/o1", `{"name":"o1"}`,
		map[string]string{"cluster": "c1", "overlay": "o1"}); rec.Code != http.StatusInternalServerError {
		t.Fatalf("overlay update error = %d body=%s", rec.Code, rec.Body.String())
	}
}
