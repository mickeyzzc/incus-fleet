package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/config"
)

func TestWiredAnswerFileErrors(t *testing.T) {
	f, registry, _, _ := newPluginFixture(t, config.PluginEventsConfig{})
	cookie := f.login(t)
	path := "/api/v1/clusters/c1/iso/default/debian/launch-unattended/preview"

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer bad.Close()
	if err := registry.SetWiring("hive", "bootstrap.answer_file_url", bad.URL); err != nil {
		t.Fatal(err)
	}
	rec := f.request(t, http.MethodPost, path, `{"profile":"debian-preseed"}`, cookie, "")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "returned 502") {
		t.Fatalf("bad upstream = %d body=%s", rec.Code, rec.Body.String())
	}

	binary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte{0, 1, 2})
	}))
	defer binary.Close()
	if err := registry.SetWiring("hive", "bootstrap.answer_file_url", binary.URL); err != nil {
		t.Fatal(err)
	}
	rec = f.request(t, http.MethodPost, path, `{"profile":"debian-preseed"}`, cookie, "")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "non-empty text") {
		t.Fatalf("binary answer = %d body=%s", rec.Code, rec.Body.String())
	}

	huge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, maxWiredAnswerBytes+2))
	}))
	defer huge.Close()
	if err := registry.SetWiring("hive", "bootstrap.answer_file_url", huge.URL); err != nil {
		t.Fatal(err)
	}
	rec = f.request(t, http.MethodPost, path, `{"profile":"debian-preseed"}`, cookie, "")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "larger than") {
		t.Fatalf("oversized answer = %d body=%s", rec.Code, rec.Body.String())
	}
}
