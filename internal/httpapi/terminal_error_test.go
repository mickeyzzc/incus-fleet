package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

// TestTerminalErrorPaths covers the websocket handlers' failure branches:
// unknown instances and Incus-side errors surface as ws close frames.
func TestTerminalErrorPaths(t *testing.T) {
	source := &incusfake.Server{
		ConsoleErr: api.StatusErrorf(http.StatusConflict, "console busy"),
		ExecErr:    api.StatusErrorf(http.StatusConflict, "exec refused"),
		Instance:   &api.Instance{Name: "web"},
	}
	target := &incusfake.Server{}
	f := newAPIFixture(t, source, target)
	cookie := f.login(t)

	server := f.wsServer(t)
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	header := http.Header{"Cookie": []string{cookie.Name + "=" + cookie.Value}}

	cases := []struct {
		name string
		path string
		body string
	}{
		{"console error", "/api/v1/clusters/c1/instances/web/console", ""},
		{"exec error", "/api/v1/clusters/c1/instances/web/exec", `{"command":["bash"],"width":80,"height":25}`},
		{"exec missing instance", "/api/v1/clusters/c1/instances/gone/exec", `{"command":["bash"],"width":80,"height":25}`},
		{"exec bad start frame", "/api/v1/clusters/c1/instances/web/exec", `{not json`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url := "ws" + strings.TrimPrefix(server.URL, "http") + tc.path
			browser, _, err := dialer.Dial(url, header)
			if err != nil {
				t.Fatalf("dial %s: %v", tc.path, err)
			}
			defer browser.Close() //nolint:errcheck // test teardown

			if tc.body != "" {
				if err := browser.WriteMessage(websocket.TextMessage, []byte(tc.body)); err != nil {
					t.Fatalf("write start frame: %v", err)
				}
			}

			_ = browser.SetReadDeadline(time.Now().Add(5 * time.Second))
			if _, _, err := browser.ReadMessage(); err == nil {
				t.Fatalf("expected close frame for %s", tc.path)
			}
		})
	}
}

// TestListClusterInstancesErrors covers the error branches of the instance
// listing endpoint.
func TestListClusterInstancesErrors(t *testing.T) {
	source := &incusfake.Server{InstancesErr: errBoom()}
	f := newAPIFixture(t, source, &incusfake.Server{})
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/api/v1/clusters/c1/instances", nil, cookie, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("instances error status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = f.request(t, http.MethodGet, "/api/v1/clusters/missing/instances", nil, cookie, "")
	if rec.Code == http.StatusOK {
		t.Fatalf("unknown cluster status = %d", rec.Code)
	}
}

// TestAssetsListErrorPaths covers the service-error branches of the asset
// listing handlers with a store that has been closed underneath the API.
func TestAssetsListErrorPaths(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)
	f.closeStore(t)

	rec := f.request(t, http.MethodGet, "/api/v1/keys", nil, cookie, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("keys error status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = f.request(t, http.MethodGet, "/api/v1/scripts", nil, cookie, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("scripts error status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func errBoom() error { return &boomErr{} }

type boomErr struct{}

func (*boomErr) Error() string { return "boom" }
