package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/auth"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

// tokenFixture wires an authenticator with a database-backed token store, which
// is the production shape.
func tokenFixture(t *testing.T) (*apiFixture, *auth.Authenticator) {
	t.Helper()

	st, err := store.Open(context.Background(), "sqlite", ":memory:")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	adapter := auth.NewStoreAdapter(st)
	authn := auth.New("secret", time.Hour, auth.WithUserStore(adapter), auth.WithTokenStore(adapter))

	f := newAPIFixtureWithAuth(t, &incusfake.Server{}, &incusfake.Server{}, authn)
	// The fixture replaced the store; keep the token store reachable through the
	// same authenticator.
	return f, authn
}

func TestTokenEndpointsRequireAuth(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/tokens"},
		{http.MethodPost, "/api/v1/tokens"},
		{http.MethodDelete, "/api/v1/tokens/x"},
		{http.MethodGet, "/api/v1/tokens/scopes"},
	} {
		rec := f.request(t, tc.method, tc.path, nil, nil, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

func TestTokenLifecycleOverHTTP(t *testing.T) {
	f, _ := tokenFixture(t)
	cookie := f.login(t)

	// The fixture authenticator is backed by a token store, so the endpoint
	// answers with an empty list rather than the disabled state.
	rec := f.request(t, http.MethodGet, "/api/v1/tokens", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tokens = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"tokens"`) {
		t.Fatalf("body = %s, want a tokens envelope", rec.Body.String())
	}
}

func TestTokenScopesEndpoint(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/api/v1/tokens/scopes", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tokens/scopes = %d body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Scopes  []string `json:"scopes"`
		Enabled bool     `json:"enabled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Scopes) == 0 {
		t.Fatal("scopes list is empty")
	}
	found := false
	for _, scope := range body.Scopes {
		if scope == string(auth.ScopeReadPlugins) {
			found = true
		}
	}
	if !found {
		t.Fatalf("scopes missing read:plugins: %v", body.Scopes)
	}
}

// Scope enforcement: a browser session is authorized by the scopes its role
// implies, and an API token is authorized only by the scopes it carries.
func TestScopeMiddlewareEnforcesScopes(t *testing.T) {
	f, authn := tokenFixture(t)
	cookie := f.login(t)

	// The bootstrap session is an administrator, so it may issue a token.
	rec := f.request(t, http.MethodPost, "/api/v1/tokens", map[string]any{
		"name": "read-only", "scopes": []string{"read:clusters"},
	}, cookie, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin POST /tokens = %d body=%s, want 201", rec.Code, rec.Body.String())
	}

	var created struct {
		Token  string   `json:"token"`
		Scopes []string `json:"scopes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Token == "" {
		t.Fatal("creation response omitted the one-time secret")
	}
	if len(created.Scopes) != 1 || created.Scopes[0] != "read:clusters" {
		t.Fatalf("scopes = %v", created.Scopes)
	}

	// The token can read the plugin catalogue (it needs read:plugins, which it
	// does not have) — this must be refused.
	denied := f.request(t, http.MethodGet, "/api/v1/plugins", nil, nil, created.Token)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("token without read:plugins = %d body=%s, want 403", denied.Code, denied.Body.String())
	}

	// A token that does hold the scope is accepted.
	full, fullSecret, err := authn.CreateAPIToken(context.Background(), auth.CreateTokenRequest{
		Name: "browser", UserID: "bootstrap", Scopes: []auth.Scope{auth.ScopeReadPlugins},
	})
	if err != nil {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}
	_ = full
	allowed := f.request(t, http.MethodGet, "/api/v1/plugins", nil, nil, fullSecret)
	if allowed.Code != http.StatusOK {
		t.Fatalf("token with read:plugins = %d body=%s, want 200", allowed.Code, allowed.Body.String())
	}

	// An expired or unknown secret is rejected outright.
	bogus := f.request(t, http.MethodGet, "/api/v1/plugins", nil, nil, "fleet_pat_not_a_real_token")
	if bogus.Code != http.StatusUnauthorized {
		t.Fatalf("bogus token = %d, want 401", bogus.Code)
	}
}

// The plugin manager exposes only registered entries and honours the enabled
// switch.
func TestPluginListDisabled(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/api/v1/plugins", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /plugins = %d body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Plugins []pluginView `json:"plugins"`
		Enabled bool         `json:"enabled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Enabled {
		t.Fatal("enabled = true with no plugin config")
	}
	if len(body.Plugins) != 0 {
		t.Fatalf("plugins = %v, want empty", body.Plugins)
	}
}

func TestPluginListReturnsConfiguredEntries(t *testing.T) {
	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, nil, func(d *Deps) {
		d.Plugins = config.PluginsConfig{
			Enabled: true,
			Dir:     "data/plugins",
			Entries: []config.PluginEntry{{
				ID: "victoria-metrics", Enabled: true,
				Upstream: "http://127.0.0.1:8428", Role: "viewer",
				UI: config.PluginUIManifest{
					Path: "/plugin-ui/victoria-metrics", TitleKey: "plugins.victoriaMetrics",
					Icon: "trend-chart", RequiresRole: "viewer",
				},
			}},
		}
	})
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/api/v1/plugins", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /plugins = %d body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Plugins []pluginView `json:"plugins"`
		Enabled bool         `json:"enabled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Enabled {
		t.Fatal("enabled = false, want true")
	}
	if len(body.Plugins) != 1 {
		t.Fatalf("plugins = %d, want 1", len(body.Plugins))
	}
	if body.Plugins[0].ID != "victoria-metrics" {
		t.Fatalf("id = %q", body.Plugins[0].ID)
	}
	if body.Plugins[0].UI.Path != "/plugin-ui/victoria-metrics" {
		t.Fatalf("ui.path = %q", body.Plugins[0].UI.Path)
	}
	if body.Plugins[0].Enabled != true {
		t.Fatal("plugin enabled flag not surfaced")
	}
}

func TestPluginGetUnknownID(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/api/v1/plugins/nope", nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /plugins/nope = %d, want 404", rec.Code)
	}
}

// The proxy must forward to the plugin upstream with the prefix stripped and
// must not forward the fleet session cookie.
func TestPluginProxyForwardsRequest(t *testing.T) {
	var (
		gotPath   string
		gotCookie string
		gotHost   string
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotCookie = r.Header.Get("Cookie")
		gotHost = r.Host
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, nil, func(d *Deps) {
		d.Plugins = config.PluginsConfig{
			Enabled: true,
			Dir:     "data/plugins",
			Entries: []config.PluginEntry{{
				ID: "vm", Enabled: true, Upstream: upstream.URL, Role: "viewer",
				UI: config.PluginUIManifest{Path: "/plugin-ui/vm", RequiresRole: "viewer"},
			}},
		}
	})
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/plugin-ui/vm/api/v1/query?query=up", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("proxy = %d body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/api/v1/query" {
		t.Fatalf("upstream path = %q, want /api/v1/query", gotPath)
	}
	if gotCookie != "" {
		t.Fatalf("fleet session cookie leaked to plugin: %q", gotCookie)
	}
	if !strings.Contains(gotHost, "127.0.0.1") {
		t.Fatalf("upstream host = %q", gotHost)
	}
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestPluginProxyUnreachableUpstream(t *testing.T) {
	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, nil, func(d *Deps) {
		d.Plugins = config.PluginsConfig{
			Enabled: true,
			Dir:     "data/plugins",
			Entries: []config.PluginEntry{{
				ID: "dead", Enabled: true, Upstream: "http://127.0.0.1:1", Role: "viewer",
				UI: config.PluginUIManifest{Path: "/plugin-ui/dead", RequiresRole: "viewer"},
			}},
		}
	})
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/plugin-ui/dead/health", nil, cookie, "")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("proxy = %d, want 502", rec.Code)
	}
}

func TestPluginProxyDisabled(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/plugin-ui/none/x", nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("proxy with plugins disabled = %d, want 404", rec.Code)
	}
}

func TestPluginProxyUnmatchedPath(t *testing.T) {
	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, nil, func(d *Deps) {
		d.Plugins = config.PluginsConfig{Enabled: true, Dir: "data/plugins"}
	})
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/plugin-ui/absent/x", nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unmatched proxy = %d, want 404", rec.Code)
	}
}

// Disabled entries are not proxied even when their path is registered.
func TestPluginProxySkipsDisabledEntries(t *testing.T) {
	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, nil, func(d *Deps) {
		d.Plugins = config.PluginsConfig{
			Enabled: true,
			Dir:     "data/plugins",
			Entries: []config.PluginEntry{{
				ID: "off", Enabled: false, Upstream: "http://127.0.0.1:1", Role: "viewer",
				UI: config.PluginUIManifest{Path: "/plugin-ui/off"},
			}},
		}
	})
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/plugin-ui/off/x", nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled plugin proxy = %d, want 404", rec.Code)
	}
}

func TestTokenCSPAllowsSameOriginFraming(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})

	rec := f.request(t, http.MethodGet, "/api/v1/healthz", nil, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz = %d", rec.Code)
	}

	// Plugin UI is same-origin proxied, so framing must be allowed from self.
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors 'self'") {
		t.Fatalf("CSP = %q, want frame-ancestors 'self'", csp)
	}
	if strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("CSP still forbids all framing: %q", csp)
	}
}

// Full token lifecycle through the HTTP surface, using the database-backed
// authenticator so the real store path is exercised.
func TestTokenCreateListRevokeOverHTTP(t *testing.T) {
	f, _ := tokenFixture(t)
	cookie := f.login(t)

	create := f.request(t, http.MethodPost, "/api/v1/tokens", map[string]any{
		"name": "hive install", "scopes": []string{"read:clusters", "read:plugins"},
		"plugin_id": "mibeehive", "instance_id": "inst-1",
	}, cookie, "")
	if create.Code != http.StatusCreated {
		t.Fatalf("create = %d body=%s", create.Code, create.Body.String())
	}

	var created struct {
		ID     string   `json:"id"`
		Token  string   `json:"token"`
		Scopes []string `json:"scopes"`
		Plugin string   `json:"plugin_id"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID == "" || created.Token == "" {
		t.Fatalf("create response missing id or token: %s", create.Body.String())
	}
	if created.Plugin != "mibeehive" {
		t.Fatalf("plugin_id = %q", created.Plugin)
	}

	list := f.request(t, http.MethodGet, "/api/v1/tokens", nil, cookie, "")
	if list.Code != http.StatusOK {
		t.Fatalf("list = %d", list.Code)
	}
	if strings.Contains(list.Body.String(), created.Token) {
		t.Fatal("list response exposed the plaintext secret")
	}
	if !strings.Contains(list.Body.String(), "hive install") {
		t.Fatalf("list body = %s", list.Body.String())
	}

	revoke := f.request(t, http.MethodDelete, "/api/v1/tokens/"+created.ID, nil, cookie, "")
	if revoke.Code != http.StatusNoContent {
		t.Fatalf("revoke = %d body=%s", revoke.Code, revoke.Body.String())
	}

	after := f.request(t, http.MethodGet, "/api/v1/plugins", nil, nil, created.Token)
	if after.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token = %d, want 401", after.Code)
	}

	missing := f.request(t, http.MethodDelete, "/api/v1/tokens/does-not-exist", nil, cookie, "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("revoke missing = %d, want 404", missing.Code)
	}
}

func TestTokenCreateRejectsInvalidInput(t *testing.T) {
	f, _ := tokenFixture(t)
	cookie := f.login(t)

	for _, body := range []map[string]any{
		{"name": "", "scopes": []string{"read:clusters"}},
		{"name": "x", "scopes": []string{}},
		{"name": "x", "scopes": []string{"bogus:scope"}},
	} {
		rec := f.request(t, http.MethodPost, "/api/v1/tokens", body, cookie, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %v = %d, want 400", body, rec.Code)
		}
	}
}

func TestTokenCreateForOtherUserRequiresAdmin(t *testing.T) {
	f, _ := tokenFixture(t)
	cookie := f.login(t)

	rec := f.request(t, http.MethodPost, "/api/v1/tokens", map[string]any{
		"name": "for-someone", "user_id": "other-user", "scopes": []string{"read:clusters"},
	}, cookie, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin create for other user = %d body=%s", rec.Code, rec.Body.String())
	}

	var created struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.UserID != "other-user" {
		t.Fatalf("user_id = %q", created.UserID)
	}
}

func TestGetPluginFound(t *testing.T) {
	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, nil, func(d *Deps) {
		d.Plugins = config.PluginsConfig{
			Enabled: true, Dir: "data/plugins",
			Entries: []config.PluginEntry{{
				ID: "vm", Enabled: true, Upstream: "http://127.0.0.1:8428", Role: "viewer",
				UI: config.PluginUIManifest{Path: "/plugin-ui/vm"},
			}},
		}
	})
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/api/v1/plugins/vm", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get plugin = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"id":"vm"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestPluginDisplayNameFallsBackToID(t *testing.T) {
	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, nil, func(d *Deps) {
		d.Plugins = config.PluginsConfig{
			Enabled: true, Dir: "data/plugins",
			Entries: []config.PluginEntry{{
				ID: "bare", Enabled: true, Upstream: "http://127.0.0.1:1", Role: "viewer",
			}},
		}
	})
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/api/v1/plugins", nil, cookie, "")
	if !strings.Contains(rec.Body.String(), `"name":"bare"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestPluginProxyUsesFallbackPrefix(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, nil, func(d *Deps) {
		d.Plugins = config.PluginsConfig{
			Enabled: true, Dir: "data/plugins",
			Entries: []config.PluginEntry{{
				ID: "nopath", Enabled: true, Upstream: upstream.URL, Role: "viewer",
			}},
		}
	})
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/plugin-ui/nopath/dashboard", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("fallback proxy = %d body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/dashboard" {
		t.Fatalf("upstream path = %q", gotPath)
	}
}

func TestPluginProxyPreservesQuery(t *testing.T) {
	var gotQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, nil, func(d *Deps) {
		d.Plugins = config.PluginsConfig{
			Enabled: true, Dir: "data/plugins",
			Entries: []config.PluginEntry{{
				ID: "vm", Enabled: true, Upstream: upstream.URL, Role: "viewer",
				UI: config.PluginUIManifest{Path: "/plugin-ui/vm"},
			}},
		}
	})
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/plugin-ui/vm/query?query=up&step=15", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("proxy = %d", rec.Code)
	}
	if gotQuery != "query=up&step=15" {
		t.Fatalf("upstream query = %q", gotQuery)
	}
}

// A viewer account may list only its own tokens and may not mint tokens for
// anyone else, nor request scopes above its own.
func TestTokenEndpointsForNonAdmin(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, "sqlite", ":memory:")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	adapter := auth.NewStoreAdapter(st)
	authn := auth.New("secret", time.Hour, auth.WithUserStore(adapter), auth.WithTokenStore(adapter))

	// Seed a viewer account and log in as it.
	viewer := &auth.User{ID: "viewer-1", Username: "viewer", Role: auth.RoleViewer}
	hash, err := auth.DefaultPasswordHasher().Hash("password123")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	viewer.PasswordHash = hash
	viewer.CreatedAt = time.Now().UTC()
	viewer.UpdatedAt = viewer.CreatedAt
	if err := adapter.CreateUser(ctx, viewer); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	f := newAPIFixtureWithAuth(t, &incusfake.Server{}, &incusfake.Server{}, authn)
	cookie := f.loginAs(t, "viewer", "password123")

	list := f.request(t, http.MethodGet, "/api/v1/tokens", nil, cookie, "")
	if list.Code != http.StatusOK {
		t.Fatalf("viewer list = %d body=%s", list.Code, list.Body.String())
	}

	other := f.request(t, http.MethodPost, "/api/v1/tokens", map[string]any{
		"name": "n", "user_id": "someone-else", "scopes": []string{"read:clusters"},
	}, cookie, "")
	if other.Code != http.StatusForbidden {
		t.Fatalf("viewer mint for other = %d, want 403", other.Code)
	}

	escalate := f.request(t, http.MethodPost, "/api/v1/tokens", map[string]any{
		"name": "n", "scopes": []string{"write:users"},
	}, cookie, "")
	if escalate.Code != http.StatusForbidden {
		t.Fatalf("viewer scope escalation = %d, want 403", escalate.Code)
	}

	own := f.request(t, http.MethodPost, "/api/v1/tokens", map[string]any{
		"name": "mine", "scopes": []string{"read:clusters"},
	}, cookie, "")
	if own.Code != http.StatusCreated {
		t.Fatalf("viewer own token = %d body=%s", own.Code, own.Body.String())
	}

	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(own.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// A viewer may revoke a token it owns.
	if rec := f.request(t, http.MethodDelete, "/api/v1/tokens/"+created.ID, nil, cookie, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("viewer revoke own = %d body=%s (id=%s)", rec.Code, rec.Body.String(), created.ID)
	}

	// Revoking a token it does not own looks like a 404 so the endpoint does not
	// confirm whether the id exists.
	if rec := f.request(t, http.MethodDelete, "/api/v1/tokens/not-mine", nil, cookie, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("viewer revoke unknown = %d, want 404", rec.Code)
	}
}
