package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/auth"
	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestPluginOneClickInstall(t *testing.T) {
	st, err := store.Open(context.Background(), "sqlite", ":memory:")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	adapter := auth.NewStoreAdapter(st)
	authn := auth.New("secret", time.Hour, auth.WithUserStore(adapter), auth.WithTokenStore(adapter))

	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, authn, func(d *Deps) {
		d.Plugins = config.PluginsConfig{Enabled: true}
	})
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/api/v1/plugins/catalog", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog status = %d body = %s", rec.Code, rec.Body.String())
	}
	var catalog struct {
		Catalog []pluginCatalogItem `json:"catalog"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if len(catalog.Catalog) != 2 || catalog.Catalog[0].ID != "vm" || catalog.Catalog[1].ID != "vmlogs" {
		t.Fatalf("catalog = %#v", catalog.Catalog)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/plugins/install",
		`{"id":"vm","upstream":"http://127.0.0.1:8428"}`, cookie, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("install status = %d body = %s", rec.Code, rec.Body.String())
	}
	var installed struct {
		Plugin pluginView `json:"plugin"`
		Token  string     `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &installed); err != nil {
		t.Fatalf("decode install: %v", err)
	}
	if installed.Plugin.ID != "vm" || installed.Token == "" {
		t.Fatalf("install result = %#v", installed)
	}

	rec = f.request(t, http.MethodGet, "/api/v1/plugins", nil, cookie, "")
	var list struct {
		Plugins []pluginView `json:"plugins"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Plugins) != 1 || list.Plugins[0].ID != "vm" {
		t.Fatalf("plugins = %#v", list.Plugins)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/plugins/vm/toggle", `{"enabled":false}`, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle status = %d body = %s", rec.Code, rec.Body.String())
	}
	rec = f.request(t, http.MethodDelete, "/api/v1/plugins/vm", nil, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("uninstall status = %d body = %s", rec.Code, rec.Body.String())
	}
	rec = f.request(t, http.MethodGet, "/api/v1/plugins", nil, cookie, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list after uninstall: %v", err)
	}
	if len(list.Plugins) != 0 {
		t.Fatalf("plugins after uninstall = %#v", list.Plugins)
	}
}

func TestPickLeastLoadedMember(t *testing.T) {
	members := []clusters.Member{
		{Name: "busy", Status: "Online", Memory: clusters.ResourceMem{Used: 90, Total: 100}, Storage: clusters.StorageUsage{Used: 90, Total: 100}},
		{Name: "offline", Status: "Offline", Memory: clusters.ResourceMem{Used: 1, Total: 100}},
		{Name: "idle", Status: "Online", Memory: clusters.ResourceMem{Used: 10, Total: 100}, Storage: clusters.StorageUsage{Used: 5, Total: 100}},
	}
	if got := pickLeastLoadedMember(members); got != "idle" {
		t.Fatalf("pickLeastLoadedMember() = %q, want idle", got)
	}
	if got := pickLeastLoadedMember(nil); got != "" {
		t.Fatalf("empty members = %q", got)
	}
}

func TestPluginInstallRejectsUnknownAndBadUpstream(t *testing.T) {
	st, err := store.Open(context.Background(), "sqlite", ":memory:")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	adapter := auth.NewStoreAdapter(st)
	authn := auth.New("secret", time.Hour, auth.WithUserStore(adapter), auth.WithTokenStore(adapter))
	f := newAPIFixtureWithDeps(t, &incusfake.Server{}, &incusfake.Server{}, authn, func(d *Deps) {
		d.Plugins = config.PluginsConfig{Enabled: true}
	})
	cookie := f.login(t)

	for _, body := range []string{
		`{"id":"unknown","upstream":"http://127.0.0.1:8428"}`,
		`{"id":"vm","upstream":"not-a-url"}`,
	} {
		rec := f.request(t, http.MethodPost, "/api/v1/plugins/install", body, cookie, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("install %s status = %d", body, rec.Code)
		}
	}
}
