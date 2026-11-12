package httpapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/assets"
	"github.com/mickeyzzc/incus-fleet/internal/auth"
	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/instances"
	"github.com/mickeyzzc/incus-fleet/internal/migrations"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

type apiFixture struct {
	handler    http.Handler
	token      string
	source     *incusfake.Server
	target     *incusfake.Server
	migrations *migrations.Service
}

func newAPIFixture(t *testing.T, source, target *incusfake.Server) *apiFixture {
	t.Helper()

	cfgs := []config.Cluster{
		{Name: "c1", DisplayName: "c1", Endpoint: "https://192.0.2.10:8443"},
		{Name: "c2", DisplayName: "c2", Endpoint: "https://198.51.100.11:8443"},
	}
	dial := func(_ context.Context, cfg config.Cluster) (clusters.InstanceServer, error) {
		switch cfg.Name {
		case "c1":
			return source, nil
		case "c2":
			return target, nil
		default:
			return nil, clusters.ErrUnavailable
		}
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager, err := clusters.NewWithDialer(context.Background(), cfgs, dial, logger)
	if err != nil {
		t.Fatalf("clusters.NewWithDialer() error = %v", err)
	}

	st, err := store.Open(context.Background(), "sqlite", ":memory:")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}

	migrationService := migrations.New(manager, st, logger, migrations.WithRunTimeout(30*time.Second))
	staticFS := fstest.MapFS{
		"dist/index.html":    {Data: []byte("<html>fleet</html>")},
		"dist/assets/app.js": {Data: []byte("console.log('fleet')")},
	}

	handler := New(Deps{
		Clusters:   manager,
		Instances:  instances.New(manager),
		Assets:     assets.New(st, instances.New(manager)),
		Migrations: migrationService,
		Auth:       auth.New("secret", time.Hour),
		StaticFS:   staticFS,
		Logger:     logger,
		Version:    "test",
	}).Handler()

	t.Cleanup(func() {
		migrationService.Wait()
		manager.Close()
		_ = st.Close()
	})

	return &apiFixture{handler: handler, token: "secret", source: source, target: target, migrations: migrationService}
}

func (f *apiFixture) request(t *testing.T, method, path string, body any, cookie *http.Cookie, bearer string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != nil {
		switch value := body.(type) {
		case string:
			reader = bytes.NewBufferString(value)
		default:
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			reader = bytes.NewReader(data)
		}
	}

	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)

	return rec
}

func (f *apiFixture) login(t *testing.T) *http.Cookie {
	t.Helper()

	rec := f.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{"token": f.token}, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d body = %s", rec.Code, rec.Body.String())
	}

	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login returned no cookie")
	}

	return cookies[0]
}

func testContainer(name string) *api.Instance {
	return &api.Instance{
		Name:       name,
		Type:       "container",
		Status:     "Running",
		StatusCode: api.Running,
		InstancePut: api.InstancePut{
			Architecture: "x86_64",
		},
		ExpandedConfig:  api.ConfigMap{},
		ExpandedDevices: api.DevicesMap{"root": {"pool": "default"}},
	}
}

func TestHealthAndVersionArePublic(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})

	rec := f.request(t, http.MethodGet, "/api/v1/healthz", nil, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d", rec.Code)
	}
	var health map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil || health["status"] != "ok" || health["version"] != "test" {
		t.Fatalf("health = %v error = %v", health, err)
	}

	rec = f.request(t, http.MethodGet, "/api/v1/version", nil, nil, "")
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"test"`)) {
		t.Fatalf("version status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestAuthFlow(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})

	rec := f.request(t, http.MethodGet, "/api/v1/clusters", nil, nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{"token": "wrong"}, nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad login status = %d", rec.Code)
	}

	cookie := f.login(t)

	rec = f.request(t, http.MethodGet, "/api/v1/auth/session", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("session status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodGet, "/api/v1/clusters", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("cookie clusters status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = f.request(t, http.MethodGet, "/api/v1/clusters", nil, nil, f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer clusters status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/auth/logout", nil, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d", rec.Code)
	}
	rec = f.request(t, http.MethodGet, "/api/v1/auth/session", nil, cookie, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("session after logout status = %d", rec.Code)
	}
}

func TestSessionCookieSecurity(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})

	for _, tt := range []struct {
		name       string
		forwarded  string
		tls        bool
		wantSecure bool
	}{
		{name: "plain http", wantSecure: false},
		{name: "tls", tls: true, wantSecure: true},
		{name: "forwarded https", forwarded: "https", wantSecure: true},
		{name: "forwarded http", forwarded: "http", wantSecure: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"token":"secret"}`))
			req.Header.Set("Content-Type", "application/json")
			if tt.forwarded != "" {
				req.Header.Set("X-Forwarded-Proto", tt.forwarded)
			}
			if tt.tls {
				req.TLS = &tls.ConnectionState{}
			}

			rec := httptest.NewRecorder()
			f.handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("login status = %d body = %s", rec.Code, rec.Body.String())
			}

			cookies := rec.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("cookies = %#v", cookies)
			}

			cookie := cookies[0]
			if cookie.Name != sessionCookie || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
				t.Fatalf("cookie attributes = %#v", cookie)
			}
			if cookie.Secure != tt.wantSecure {
				t.Fatalf("cookie Secure = %v, want %v", cookie.Secure, tt.wantSecure)
			}
		})
	}
}

func TestLoginValidation(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})

	for _, tt := range []struct {
		name string
		body string
	}{
		{"malformed", "{"},
		{"unknown field", `{"token":"secret","extra":true}`},
		{"multiple values", `{"token":"secret"}{"token":"secret"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := f.request(t, http.MethodPost, "/api/v1/auth/login", tt.body, nil, "")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestClusterEndpoints(t *testing.T) {
	source := &incusfake.Server{
		Instances: []api.InstanceFull{{Instance: *testContainer("web")}},
	}
	target := &incusfake.Server{}
	f := newAPIFixture(t, source, target)
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/api/v1/clusters", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("clusters status = %d", rec.Code)
	}
	var envelope struct {
		Clusters []clusters.Summary `json:"clusters"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode clusters: %v", err)
	}
	if len(envelope.Clusters) != 2 || envelope.Clusters[0].Name != "c1" || !envelope.Clusters[0].Online {
		t.Fatalf("clusters = %#v", envelope.Clusters)
	}
	if envelope.Clusters[0].Instances.Total != 1 {
		t.Fatalf("cluster instances = %#v", envelope.Clusters[0].Instances)
	}

	rec = f.request(t, http.MethodGet, "/api/v1/clusters/c1", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("cluster status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodGet, "/api/v1/clusters/missing", nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing cluster status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/reconnect", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reconnect status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestInstanceEndpoints(t *testing.T) {
	source := &incusfake.Server{Instance: testContainer("web")}
	target := &incusfake.Server{}
	f := newAPIFixture(t, source, target)
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/api/v1/clusters/c1/instances?type=container", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("instances status = %d body = %s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Instances []instances.Summary `json:"instances"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode instances: %v", err)
	}
	if !source.HasCall("GetInstancesFull:container") {
		t.Fatalf("calls = %v", source.Calls())
	}

	rec = f.request(t, http.MethodGet, "/api/v1/clusters/c1/instances/web", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("instance status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = f.request(t, http.MethodGet, "/api/v1/clusters/c1/instances/missing", nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing instance status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodGet, "/api/v1/instances", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("all instances status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = f.request(t, http.MethodGet, "/api/v1/instances?type=banana", nil, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad type status = %d", rec.Code)
	}
}

func TestInstanceManagementEndpoints(t *testing.T) {
	source := &incusfake.Server{
		Instance: testContainer(""),
		Images: []api.Image{{
			Fingerprint:  "053059ea4e28",
			Type:         "container",
			Architecture: "x86_64",
			Properties:   map[string]string{"os": "Debian", "release": "trixie"},
		}},
		Pools: []api.StoragePool{{
			Name:   "default",
			Driver: "dir",
			Config: api.ConfigMap{"source": "/srv/incus"},
		}},
		Networks: []api.Network{{
			Name:    "incusbr0",
			Type:    "bridge",
			Managed: true,
			Config:  api.ConfigMap{"ipv4.address": "10.0.0.0.1/24"},
		}},
	}
	target := &incusfake.Server{}
	f := newAPIFixture(t, source, target)
	cookie := f.login(t)

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances", map[string]any{
		"name":         "newct",
		"type":         "container",
		"image":        "053059ea4e28",
		"cpu":          "2",
		"memory":       "1GiB",
		"network":      "incusbr0",
		"ipv4":         "10.0.0.0.50",
		"storage_pool": "default",
	}, cookie, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", rec.Code, rec.Body.String())
	}
	createArgs := source.CreateArgs()
	if len(createArgs) != 1 || createArgs[0].Name != "newct" || createArgs[0].Source.Fingerprint != "053059ea4e28" {
		t.Fatalf("create args = %#v", createArgs)
	}

	rec = f.request(t, http.MethodPatch, "/api/v1/clusters/c1/instances/web", map[string]any{
		"cpu":         "4",
		"memory":      "",
		"description": "after",
	}, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d body = %s", rec.Code, rec.Body.String())
	}
	updateArgs := source.UpdateArgs()
	if len(updateArgs) != 1 || updateArgs[0].Instance.Config["limits.cpu"] != "4" {
		t.Fatalf("update args = %#v", updateArgs)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/web/state", map[string]any{
		"action": "restart",
		"force":  true,
	}, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("action status = %d body = %s", rec.Code, rec.Body.String())
	}
	states := source.UpdateStateArgs()
	if len(states) != 1 || states[0].Action != "restart" || !states[0].Force {
		t.Fatalf("state args = %#v", states)
	}

	rec = f.request(t, http.MethodDelete, "/api/v1/clusters/c1/instances/web?force=true", nil, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !source.HasCall("DeleteInstance:web") {
		t.Fatalf("calls = %v", source.Calls())
	}

	rec = f.request(t, http.MethodGet, "/api/v1/clusters/c1/images", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("images status = %d body = %s", rec.Code, rec.Body.String())
	}
	var imageEnvelope struct {
		Images []instances.Image `json:"images"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &imageEnvelope); err != nil || len(imageEnvelope.Images) != 1 || imageEnvelope.Images[0].Fingerprint != "053059ea4e28" {
		t.Fatalf("images = %#v error = %v", imageEnvelope.Images, err)
	}

	rec = f.request(t, http.MethodGet, "/api/v1/clusters/c1/resources", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("resources status = %d body = %s", rec.Code, rec.Body.String())
	}
	var resources instances.Resources
	if err := json.Unmarshal(rec.Body.Bytes(), &resources); err != nil {
		t.Fatalf("decode resources: %v", err)
	}
	if len(resources.StoragePools) != 1 || resources.StoragePools[0].Name != "default" || resources.StoragePools[0].Source != "/srv/incus" {
		t.Fatalf("pools = %#v", resources.StoragePools)
	}
	if len(resources.Networks) != 1 || resources.Networks[0].IPv4 != "10.0.0.0.1/24" {
		t.Fatalf("networks = %#v", resources.Networks)
	}
}

func TestInstanceManagementErrors(t *testing.T) {
	source := &incusfake.Server{Instance: testContainer("web")}
	target := &incusfake.Server{}
	f := newAPIFixture(t, source, target)
	cookie := f.login(t)

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances", `{"name":`, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed create status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances", map[string]any{"image": "debian"}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid create status = %d body = %s", rec.Code, rec.Body.String())
	}
	var envelope errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || envelope.Error.Code != "invalid_request" {
		t.Fatalf("error = %#v error = %v", envelope, err)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c99/instances", map[string]any{"name": "ct", "image": "debian"}, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown cluster status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = f.request(t, http.MethodPatch, "/api/v1/clusters/c1/instances/missing", map[string]any{"cpu": "2"}, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing update status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/web/state", map[string]any{"action": "explode"}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid action status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = f.request(t, http.MethodDelete, "/api/v1/clusters/c1/instances/web?force=banana", nil, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid force status = %d body = %s", rec.Code, rec.Body.String())
	}

	source.ImagesErr = errors.New("images boom")
	rec = f.request(t, http.MethodGet, "/api/v1/clusters/c1/images", nil, cookie, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("images error status = %d", rec.Code)
	}

	source.PoolsErr = errors.New("pools boom")
	rec = f.request(t, http.MethodGet, "/api/v1/clusters/c1/resources", nil, cookie, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("resources error status = %d", rec.Code)
	}
}

func TestClusterAdminEndpoints(t *testing.T) {
	source := &incusfake.Server{
		Pools: []api.StoragePool{{
			Name:        "default",
			Driver:      "dir",
			Description: "primary",
			Config:      api.ConfigMap{"source": "/srv/incus"},
		}},
		Networks: []api.Network{{
			Name:    "incusbr0",
			Type:    "bridge",
			Managed: true,
			Config:  api.ConfigMap{"ipv4.address": "10.0.0.0.1/24"},
		}},
		Profiles: []api.Profile{{
			Name:    "web",
			Config:  api.ConfigMap{"limits.cpu": "2"},
			Devices: api.DevicesMap{"root": {"type": "disk", "pool": "default", "path": "/"}},
			UsedBy:  []string{"/1.0/instances/web"},
		}},
	}
	target := &incusfake.Server{}
	f := newAPIFixture(t, source, target)
	cookie := f.login(t)

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/storage-pools", map[string]any{
		"name":        "fast",
		"driver":      "zfs",
		"description": "fast pool",
		"config":      map[string]string{"source": "tank"},
	}, cookie, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create pool status = %d body = %s", rec.Code, rec.Body.String())
	}
	pools := source.CreatePoolArgs()
	if len(pools) != 1 || pools[0].Name != "fast" || pools[0].Driver != "zfs" || pools[0].Config["source"] != "tank" {
		t.Fatalf("create pool args = %#v", pools)
	}

	rec = f.request(t, http.MethodPatch, "/api/v1/clusters/c1/storage-pools/default", map[string]any{
		"description": "updated",
		"config":      map[string]string{"volume.size": "20GiB"},
	}, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("update pool status = %d body = %s", rec.Code, rec.Body.String())
	}
	poolUpdates := source.UpdatePoolArgs()
	if len(poolUpdates) != 1 || poolUpdates[0].Pool.Description != "updated" || poolUpdates[0].Pool.Config["source"] != "/srv/incus" {
		t.Fatalf("update pool args = %#v", poolUpdates)
	}

	rec = f.request(t, http.MethodDelete, "/api/v1/clusters/c1/storage-pools/fast", nil, cookie, "")
	if rec.Code != http.StatusNoContent || len(source.DeletePoolArgs()) != 1 {
		t.Fatalf("delete pool status = %d args = %#v", rec.Code, source.DeletePoolArgs())
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/networks", map[string]any{
		"name":        "br-test",
		"type":        "bridge",
		"description": "test bridge",
		"config":      map[string]string{"ipv4.address": "10.99.0.1/24", "ipv6.address": "none"},
	}, cookie, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create network status = %d body = %s", rec.Code, rec.Body.String())
	}
	networks := source.CreateNetworkArgs()
	if len(networks) != 1 || networks[0].Name != "br-test" || networks[0].Type != "bridge" || networks[0].Config["ipv4.address"] != "10.99.0.1/24" {
		t.Fatalf("create network args = %#v", networks)
	}

	rec = f.request(t, http.MethodPatch, "/api/v1/clusters/c1/networks/incusbr0", map[string]any{
		"config": map[string]string{"ipv4.nat": "false"},
	}, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("update network status = %d body = %s", rec.Code, rec.Body.String())
	}
	networkUpdates := source.UpdateNetworkArgs()
	if len(networkUpdates) != 1 || networkUpdates[0].Network.Config["ipv4.address"] != "10.0.0.0.1/24" || networkUpdates[0].Network.Config["ipv4.nat"] != "false" {
		t.Fatalf("update network args = %#v", networkUpdates)
	}

	rec = f.request(t, http.MethodDelete, "/api/v1/clusters/c1/networks/br-test", nil, cookie, "")
	if rec.Code != http.StatusNoContent || len(source.DeleteNetworkArgs()) != 1 {
		t.Fatalf("delete network status = %d args = %#v", rec.Code, source.DeleteNetworkArgs())
	}

	rec = f.request(t, http.MethodGet, "/api/v1/clusters/c1/profiles", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("profiles status = %d body = %s", rec.Code, rec.Body.String())
	}
	var profileEnvelope struct {
		Profiles []instances.Profile `json:"profiles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &profileEnvelope); err != nil || len(profileEnvelope.Profiles) != 1 || profileEnvelope.Profiles[0].Name != "web" {
		t.Fatalf("profiles = %#v error = %v", profileEnvelope.Profiles, err)
	}
	if profileEnvelope.Profiles[0].Devices["root"]["pool"] != "default" || len(profileEnvelope.Profiles[0].UsedBy) != 1 {
		t.Fatalf("profile = %#v", profileEnvelope.Profiles[0])
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/profiles", map[string]any{
		"name":        "tier",
		"description": "tier profile",
		"config":      map[string]string{"limits.memory": "2GiB"},
		"devices": map[string]any{
			"eth0": map[string]string{"type": "nic", "network": "incusbr0"},
		},
	}, cookie, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create profile status = %d body = %s", rec.Code, rec.Body.String())
	}
	profiles := source.CreateProfileArgs()
	if len(profiles) != 1 || profiles[0].Name != "tier" || profiles[0].Config["limits.memory"] != "2GiB" || profiles[0].Devices["eth0"]["network"] != "incusbr0" {
		t.Fatalf("create profile args = %#v", profiles)
	}

	rec = f.request(t, http.MethodPatch, "/api/v1/clusters/c1/profiles/web", map[string]any{
		"config": map[string]string{"limits.cpu": "4"},
	}, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("update profile status = %d body = %s", rec.Code, rec.Body.String())
	}
	profileUpdates := source.UpdateProfileArgs()
	if len(profileUpdates) != 1 || profileUpdates[0].Profile.Config["limits.cpu"] != "4" {
		t.Fatalf("update profile args = %#v", profileUpdates)
	}

	rec = f.request(t, http.MethodDelete, "/api/v1/clusters/c1/profiles/web", nil, cookie, "")
	if rec.Code != http.StatusNoContent || len(source.DeleteProfileArgs()) != 1 {
		t.Fatalf("delete profile status = %d args = %#v", rec.Code, source.DeleteProfileArgs())
	}

	rec = f.request(t, http.MethodPatch, "/api/v1/clusters/c1/members/test-node", map[string]any{
		"description": "primary node",
		"groups":      []string{"site-a"},
	}, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("update member status = %d body = %s", rec.Code, rec.Body.String())
	}
	memberUpdates := source.UpdateMemberArgs()
	if len(memberUpdates) != 1 || memberUpdates[0].Member.Description != "primary node" || memberUpdates[0].Member.Groups[0] != "site-a" {
		t.Fatalf("update member args = %#v", memberUpdates)
	}
}

func TestClusterAdminErrors(t *testing.T) {
	source := &incusfake.Server{
		Networks: []api.Network{{
			Name:    "physical",
			Type:    "physical",
			Managed: false,
		}},
	}
	target := &incusfake.Server{}
	f := newAPIFixture(t, source, target)
	cookie := f.login(t)

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/storage-pools", `{"name":`, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed pool status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/storage-pools", map[string]any{"name": "fast"}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid pool status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/networks", map[string]any{
		"name": "br-test",
		"type": "bridge",
		"config": map[string]string{
			"ipv4.address": "not-a-cidr",
		},
	}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid network status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = f.request(t, http.MethodPatch, "/api/v1/clusters/c1/networks/physical", map[string]any{
		"description": "nope",
	}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unmanaged network status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/profiles", map[string]any{"name": "tier", "devices": map[string]any{"root": map[string]string{"pool": "default"}}}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid profile status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = f.request(t, http.MethodPatch, "/api/v1/clusters/c99/members/test-node", map[string]any{"description": "x"}, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown cluster status = %d body = %s", rec.Code, rec.Body.String())
	}

	source.CreatePoolErr = errors.New("pool boom")
	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/storage-pools", map[string]any{"name": "fast", "driver": "zfs"}, cookie, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("pool error status = %d", rec.Code)
	}

	source.ProfilesErr = errors.New("profiles boom")
	rec = f.request(t, http.MethodGet, "/api/v1/clusters/c1/profiles", nil, cookie, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("profiles error status = %d", rec.Code)
	}

	source.MemberErr = errors.New("member boom")
	rec = f.request(t, http.MethodPatch, "/api/v1/clusters/c1/members/test-node", map[string]any{"description": "x"}, cookie, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("member error status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestMigrationEndpoints(t *testing.T) {
	source := &incusfake.Server{Instance: testContainer("web")}
	target := &incusfake.Server{}
	f := newAPIFixture(t, source, target)
	cookie := f.login(t)

	rec := f.request(t, http.MethodPost, "/api/v1/migrations", migrations.Request{
		SourceCluster: "c1",
		TargetCluster: "c2",
		Instance:      "web",
		Mode:          "move",
	}, cookie, "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("submit status = %d body = %s", rec.Code, rec.Body.String())
	}

	var migration store.Migration
	if err := json.Unmarshal(rec.Body.Bytes(), &migration); err != nil {
		t.Fatalf("decode migration: %v", err)
	}
	f.migrations.Wait()

	rec = f.request(t, http.MethodGet, "/api/v1/migrations/"+migration.ID, nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get migration status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodGet, "/api/v1/migrations", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list migrations status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodGet, "/api/v1/migrations/missing", nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing migration status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodGet, "/api/v1/migrations?limit=zero", nil, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad limit status = %d", rec.Code)
	}
}

func TestMigrationErrors(t *testing.T) {
	source := &incusfake.Server{Instance: testContainer("web")}
	target := &incusfake.Server{}
	f := newAPIFixture(t, source, target)
	cookie := f.login(t)

	rec := f.request(t, http.MethodPost, "/api/v1/migrations", `{"source_cluster":"c1"}`, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid request status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/migrations", migrations.Request{
		SourceCluster: "c1",
		TargetCluster: "c1",
		Instance:      "web",
	}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("same cluster status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/migrations", migrations.Request{
		SourceCluster: "c1",
		TargetCluster: "c2",
		Instance:      "missing",
	}, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing instance status = %d", rec.Code)
	}

	target.Instance = testContainer("web")
	rec = f.request(t, http.MethodPost, "/api/v1/migrations", migrations.Request{
		SourceCluster: "c1",
		TargetCluster: "c2",
		Instance:      "web",
	}, cookie, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d", rec.Code)
	}
}

func TestStaticServing(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})

	rec := f.request(t, http.MethodGet, "/", nil, nil, "")
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("fleet")) {
		t.Fatalf("index status = %d body = %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("index cache = %q", rec.Header().Get("Cache-Control"))
	}

	rec = f.request(t, http.MethodGet, "/clusters/c1", nil, nil, "")
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("fleet")) {
		t.Fatalf("spa fallback status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodGet, "/assets/app.js", nil, nil, "")
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset status = %d cache = %q", rec.Code, rec.Header().Get("Cache-Control"))
	}

	rec = f.request(t, http.MethodGet, "/missing.js", nil, nil, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing asset status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodPost, "/", nil, nil, "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post static status = %d", rec.Code)
	}
}

func TestNoStaticFS(t *testing.T) {
	handler := New(Deps{
		Clusters:   nil,
		Instances:  nil,
		Migrations: nil,
		Auth:       auth.New("secret", time.Hour),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRecoverer(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	api := &API{logger: logger}
	handler := api.recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
}
