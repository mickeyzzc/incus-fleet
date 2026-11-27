package httpapi

import (
	"net/http"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/config"
)

func TestWiringStoreErrorBranches(t *testing.T) {
	f, _, st, _ := newPluginFixture(t, config.PluginEventsConfig{})
	cookie := f.login(t)
	rec := f.request(t, http.MethodPut, "/api/v1/plugins/vm/wiring",
		`{"values":[{"key":"metrics.query_url","value":"http://192.0.2.10"}]}`, cookie, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unapproved wiring = %d", rec.Code)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path, body string
	}{
		{http.MethodGet, "/api/v1/plugins/vm/wiring", ""},
		{http.MethodPut, "/api/v1/plugins/vm/wiring", `{"values":[{"key":"metrics.query_url","value":"http://192.0.2.10"}]}`},
	} {
		rec := f.request(t, tc.method, tc.path, tc.body, cookie, "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s %s = %d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}
