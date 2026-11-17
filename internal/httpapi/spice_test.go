package httpapi

import (
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestPrepareSPICEEndpoint(t *testing.T) {
	vm := &incusfake.Server{
		Instance: &api.Instance{
			Name:       "vm-01",
			Type:       "virtual-machine",
			Status:     "Stopped",
			StatusCode: api.Stopped,
		},
	}
	target := &incusfake.Server{}
	f := newAPIFixture(t, vm, target)
	cookie := f.login(t)

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/vm-01/spice/prepare", map[string]any{
		"port": 5993, "apply": false,
	}, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("prepare status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"restart_required":false`) {
		t.Fatalf("prepare body = %s", rec.Body.String())
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/vm-01/spice/prepare", map[string]any{
		"port": 80,
	}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("prepare bad port status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/vm-01/spice/prepare", "not-json", cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("prepare bad json status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/missing/instances/vm-01/spice/prepare", map[string]any{
		"port": 5993,
	}, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("prepare unknown cluster status = %d", rec.Code)
	}
}

func TestSPICEBridge(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close() //nolint:errcheck // test teardown
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close() //nolint:errcheck // echo teardown
				buf := make([]byte, 512)
				for {
					n, readErr := c.Read(buf)
					if readErr != nil {
						return
					}
					if _, writeErr := c.Write(buf[:n]); writeErr != nil {
						return
					}
				}
			}(conn)
		}
	}()

	_, port, _ := net.SplitHostPort(listener.Addr().String())

	vm := &incusfake.Server{
		Instance: &api.Instance{
			Name:       "vm-01",
			Type:       "virtual-machine",
			Status:     "Running",
			StatusCode: api.Running,
			Location:   "node-b",
		},
		Members: []api.ClusterMember{
			{ServerName: "node-b", URL: "https://127.0.0.1:8443", Status: "Online"},
		},
	}
	target := &incusfake.Server{}
	f := newAPIFixture(t, vm, target)
	cookie := f.login(t)
	server := f.wsServer(t)

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	header := http.Header{"Cookie": []string{cookie.Name + "=" + cookie.Value}}
	spiceURL := "ws" + strings.TrimPrefix(server.URL, "http") +
		"/api/v1/clusters/c1/instances/vm-01/spice?port=" + port
	browser, _, dialErr := dialer.Dial(spiceURL, header)
	if dialErr != nil {
		t.Fatalf("dial spice bridge: %v", dialErr)
	}
	defer browser.Close() //nolint:errcheck // test teardown

	payload := []byte("fake-spice-link-mess")
	_ = browser.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := browser.WriteMessage(websocket.BinaryMessage, payload); err != nil {
		t.Fatalf("ws write: %v", err)
	}
	_ = browser.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, reply, err := browser.ReadMessage()
	if err != nil {
		t.Fatalf("ws read: %v", err)
	}
	if string(reply) != string(payload) {
		t.Fatalf("bridge echo = %q", string(reply))
	}
}

func TestInstanceSPICERequiresPort(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)

	// Without the port query parameter the handler rejects before upgrading.
	rec := f.request(t, http.MethodGet, "/api/v1/clusters/c1/instances/vm-01/spice", nil, cookie, "")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "spice port required") {
		t.Fatalf("SPICE without port = %d body=%s", rec.Code, rec.Body.String())
	}
}
