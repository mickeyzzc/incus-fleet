package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/instances"
	"github.com/mickeyzzc/incus-fleet/internal/plugin"
	"github.com/mickeyzzc/incus-fleet/internal/plugininstall"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestAnswerFileUsesWiredHiveURL(t *testing.T) {
	answer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("wired-answer-file"))
	}))
	defer answer.Close()

	f, registry, _, _ := newPluginFixture(t, config.PluginEventsConfig{})
	if err := registry.SetWiring("hive", "bootstrap.answer_file_url", answer.URL); err != nil {
		t.Fatal(err)
	}
	cookie := f.login(t)
	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/iso/default/debian/launch-unattended/preview",
		`{"profile":"debian-preseed"}`, cookie, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "wired-answer-file") {
		t.Fatalf("preview = %d body=%s", rec.Code, rec.Body.String())
	}

	// An explicit answer file still wins over the wired source.
	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/iso/default/debian/launch-unattended/preview",
		`{"profile":"debian-preseed","answer_file":"operator-answer"}`, cookie, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "operator-answer") {
		t.Fatalf("explicit answer = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLegacyInstallDelegatesToTaskWhenInstallerConfigured(t *testing.T) {
	f, registry, st, _ := newPluginFixture(t, config.PluginEventsConfig{})
	cookie := f.login(t)
	_ = registry
	rec := f.request(t, http.MethodPost, "/api/v1/plugins/install",
		`{"id":"vm","upstream":"http://127.0.0.1:8428"}`, cookie, "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("legacy install = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"task"`) {
		t.Fatalf("legacy response = %s", rec.Body.String())
	}
	tasks, err := st.ListHostTasks(context.Background(), "", "", 0)
	if err != nil || len(tasks) != 1 || tasks[0].Type != plugininstall.TaskType {
		t.Fatalf("tasks = %#v err=%v", tasks, err)
	}
}

func TestLegacyInstallTaskErrorBranches(t *testing.T) {
	f, _, st, _ := newPluginFixture(t, config.PluginEventsConfig{})
	cookie := f.login(t)
	rec := f.request(t, http.MethodPost, "/api/v1/plugins/install", `{"id":"unknown"}`, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown legacy plugin = %d", rec.Code)
	}
	if err := st.CreateInstalledPlugin(context.Background(), &store.InstalledPlugin{
		ID: "vm", Name: "vm", Upstream: "http://192.0.2.10:8428", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	rec = f.request(t, http.MethodPost, "/api/v1/plugins/install", `{"id":"vm"}`, cookie, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate legacy plugin = %d", rec.Code)
	}
}

// Compile-time assertions for the store methods used by the detail API.
var (
	_ = (*store.SQLStore)(nil).ListPluginConfig
	_ = (*store.SQLStore)(nil).RecordPluginEvent
	_ = (*store.SQLStore)(nil).RecordPluginHealth
	_ = plugin.EventAgentConnected
	_ = instances.ErrInvalidRequest
	_ = incusfake.Server{}
)
