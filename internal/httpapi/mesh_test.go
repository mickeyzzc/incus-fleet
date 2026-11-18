package httpapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/mesh"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestMeshHandlersUnavailable(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)
	f.handler = New(Deps{Auth: f.auth, Version: "test"}).Handler()
	for _, path := range []string{
		"/api/v1/mesh/status", "/api/v1/mesh/force-single", "/api/v1/mesh/join",
		"/api/v1/mesh/promote", "/api/v1/mesh/remove",
	} {
		method := http.MethodGet
		if path != "/api/v1/mesh/status" {
			method = http.MethodPost
		}
		rec := f.request(t, method, path, map[string]any{}, cookie, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s = %d", path, rec.Code)
		}
	}
}

func TestMeshHandlersValidation(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)
	m, err := mesh.New(mesh.Config{
		Enabled: true, NodeID: "app-a", Address: "127.0.0.1:19500",
		ControlAddress: "127.0.0.1:19501", DataDir: t.TempDir(), Secret: "secret",
		Peers: []mesh.Peer{{ID: "app-a", Address: "127.0.0.1:19500", ControlAddress: "127.0.0.1:19501"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	f.handler = New(Deps{Auth: f.auth, Version: "test", Mesh: m}).Handler()

	for _, path := range []string{
		"/api/v1/mesh/force-single", "/api/v1/mesh/join", "/api/v1/mesh/promote", "/api/v1/mesh/remove",
	} {
		rec := f.request(t, http.MethodPost, path, "{bad", cookie, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s bad json = %d", path, rec.Code)
		}
	}
	rec := f.request(t, http.MethodGet, "/api/v1/mesh/status", nil, cookie, "")
	if rec.Code != http.StatusOK || !containsText(rec.Body.String(), `"node_id":"app-a"`) {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = f.request(t, http.MethodPost, "/api/v1/mesh/force-single", map[string]any{}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("force-single = %d", rec.Code)
	}
	rec = f.request(t, http.MethodPost, "/api/v1/mesh/join", map[string]any{}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("join = %d", rec.Code)
	}
	rec = f.request(t, http.MethodPost, "/api/v1/mesh/promote", map[string]any{}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("promote = %d", rec.Code)
	}
	rec = f.request(t, http.MethodPost, "/api/v1/mesh/remove", map[string]any{}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("remove = %d", rec.Code)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/mesh/join",
		map[string]any{"id": "app-b", "address": "192.0.2.21:9400"}, cookie, "")
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("join with fields = %d", rec.Code)
	}
	rec = f.request(t, http.MethodPost, "/api/v1/mesh/promote",
		map[string]any{"id": "app-b"}, cookie, "")
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("promote with id = %d", rec.Code)
	}
	rec = f.request(t, http.MethodPost, "/api/v1/mesh/remove",
		map[string]any{"id": "app-b"}, cookie, "")
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("remove with id = %d", rec.Code)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/mesh/force-single",
		map[string]any{"confirm": true}, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("force-single = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := m.WaitForLeader(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		path string
		body map[string]any
	}{
		{"/api/v1/mesh/join", map[string]any{"id": "app-b", "address": "192.0.2.21:9400", "control_address": "192.0.2.21:9401", "voter": false}},
		{"/api/v1/mesh/promote", map[string]any{"id": "app-b"}},
		{"/api/v1/mesh/remove", map[string]any{"id": "app-b"}},
	} {
		rec = f.request(t, http.MethodPost, item.path, item.body, cookie, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s = %d body=%s", item.path, rec.Code, rec.Body.String())
		}
	}
	rec = f.request(t, http.MethodGet, "/api/v1/mesh/status", nil, cookie, "")
	if rec.Code != http.StatusOK || !containsText(rec.Body.String(), `"running":true`) {
		t.Fatalf("status after recovery = %d body=%s", rec.Code, rec.Body.String())
	}
}

func containsText(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
