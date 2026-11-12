package httpapi

import (
	"encoding/base64"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/apitypes"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func fileEntry(name string, isDir bool, size int64) apitypes.FileEntry {
	return apitypes.FileEntry{Name: name, IsDir: isDir, Size: size, Mode: 0o644}
}

func base64Of(value string) string {
	return base64.StdEncoding.EncodeToString([]byte(value))
}

// wsServer exposes the fleet handler over a real loopback HTTP server so
// websocket handshakes can complete.
func (f *apiFixture) wsServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(f.handler)
	t.Cleanup(server.Close)

	return server
}

func (f *apiFixture) serve(t *testing.T, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)

	return rec
}

func newMultipartWriter(body *strings.Builder, content string) *multipart.Writer {
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "motd")
	_, _ = part.Write([]byte(content))
	_ = writer.Close()

	return writer
}

func httptestRequest(t *testing.T, method, target, body string, cookie *http.Cookie) *http.Request {
	t.Helper()

	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)

	return req
}

func TestInstanceFileEndpoints(t *testing.T) {
	source := &incusfake.Server{
		Files: map[string][]byte{
			"/etc/hostname": []byte("web\n"),
		},
		ListFiles: []apitypes.FileEntry{
			fileEntry("logs", true, 4096),
			fileEntry("app.log", false, 1234),
		},
	}
	target := &incusfake.Server{}
	f := newAPIFixture(t, source, target)
	cookie := f.login(t)

	// Directory listing.
	rec := f.request(t, http.MethodGet, "/api/v1/clusters/c1/instances/web/files?action=list&path=/var/log", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list files status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"logs"`) || !strings.Contains(rec.Body.String(), `"is_dir":true`) {
		t.Fatalf("list files body = %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"app.log"`) {
		t.Fatalf("list files missing file entry: %s", rec.Body.String())
	}

	// Download.
	rec = f.request(t, http.MethodGet, "/api/v1/clusters/c1/instances/web/files?path=/etc/hostname", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("download status = %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "web" {
		t.Fatalf("download body = %q", rec.Body.String())
	}

	// Upload via multipart.
	upload(t, f, cookie, "/etc/motd", "welcome")

	args := source.CreateFileArgs()
	if len(args) != 1 || args[0].Path != "/etc/motd" || string(args[0].Content) != "welcome" {
		t.Fatalf("create file args = %#v", args)
	}

	// mkdir.
	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/web/files?action=mkdir&path=/opt/app", nil, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("mkdir status = %d body = %s", rec.Code, rec.Body.String())
	}
	mkdirs := source.MkdirArgs()
	if len(mkdirs) != 1 || mkdirs[0] != "/opt/app" {
		t.Fatalf("mkdir args = %#v", mkdirs)
	}

	// Delete.
	rec = f.request(t, http.MethodDelete, "/api/v1/clusters/c1/instances/web/files?path=/etc/motd", nil, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec.Code)
	}
	deleted := source.DeleteFileArgs()
	if len(deleted) != 1 || deleted[0] != "/etc/motd" {
		t.Fatalf("delete args = %#v", deleted)
	}

	// Invalid paths are rejected.
	rec = f.request(t, http.MethodGet, "/api/v1/clusters/c1/instances/web/files?path=relative/path", nil, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("relative path status = %d", rec.Code)
	}

	// Missing files and unknown clusters map to not_found.
	rec = f.request(t, http.MethodGet, "/api/v1/clusters/c1/instances/web/files?path=/missing", nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing file status = %d", rec.Code)
	}
	rec = f.request(t, http.MethodGet, "/api/v1/clusters/missing/instances/web/files?action=list&path=/opt", nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown cluster files status = %d", rec.Code)
	}
	// Traversal segments are normalized by the service before lookup.
	rec = f.request(t, http.MethodGet, "/api/v1/clusters/c1/instances/web/files?action=list&path=/../escape", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("escape path status = %d", rec.Code)
	}
}

func TestConsoleAndExecRelay(t *testing.T) {
	stub := incusfake.NewWebsocketStub()
	defer stub.Close()

	source := &incusfake.Server{
		Instance:  &api.Instance{Name: "web"},
		Operation: stub.Operation(),
	}
	target := &incusfake.Server{}
	f := newAPIFixture(t, source, target)
	cookie := f.login(t)

	server := f.wsServer(t)
	consoleURL := "ws" + strings.TrimPrefix(server.URL, "http") +
		"/api/v1/clusters/c1/instances/web/console"

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	header := http.Header{"Cookie": []string{cookie.Name + "=" + cookie.Value}}
	browser, _, err := dialer.Dial(consoleURL, header)
	if err != nil {
		t.Fatalf("dial console: %v", err)
	}
	defer browser.Close() //nolint:errcheck // test teardown

	incusConn := waitForConn(t, stub.Conns, "0")

	// Browser bytes flow to the Incus data channel.
	if err := browser.WriteMessage(websocket.BinaryMessage, []byte("ls\n")); err != nil {
		t.Fatalf("browser write: %v", err)
	}
	_ = incusConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, payload, err := incusConn.ReadMessage()
	if err != nil {
		t.Fatalf("incus read: %v", err)
	}
	if string(payload) != "ls\n" {
		t.Fatalf("incus payload = %q", payload)
	}

	// Incus text frames (base64) flow back decoded to the browser.
	encoded := base64Of("total 0\n")
	if err := incusConn.WriteMessage(websocket.TextMessage, []byte(encoded)); err != nil {
		t.Fatalf("incus write: %v", err)
	}
	_ = browser.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, reply, err := browser.ReadMessage()
	if err != nil {
		t.Fatalf("browser read: %v", err)
	}
	if string(reply) != "total 0\n" {
		t.Fatalf("browser payload = %q", reply)
	}

	// Non-base64 text from Incus reaches the browser as raw binary.
	if err := incusConn.WriteMessage(websocket.TextMessage, []byte("not base64!!")); err != nil {
		t.Fatalf("incus raw write: %v", err)
	}
	_ = browser.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := browser.ReadMessage()
	if err != nil {
		t.Fatalf("browser raw read: %v", err)
	}
	if string(raw) != "not base64!!" {
		t.Fatalf("browser raw payload = %q", raw)
	}

	// Garbage control frames are ignored; the session keeps flowing.
	if err := browser.WriteMessage(websocket.TextMessage, []byte("{invalid")); err != nil {
		t.Fatalf("garbage write: %v", err)
	}
	if err := browser.WriteMessage(websocket.BinaryMessage, []byte("still alive DONE")); err != nil {
		t.Fatalf("post-garbage write: %v", err)
	}
	_ = incusConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, alive, err := incusConn.ReadMessage()
	if err != nil {
		t.Fatalf("incus post-garbage read: %v", err)
	}
	if string(alive) != "still alive DONE" {
		t.Fatalf("post-garbage payload = %q", alive)
	}

	if source.ConsoleCalls() != 1 {
		t.Fatalf("console calls = %d", source.ConsoleCalls())
	}
}

func TestExecRelay(t *testing.T) {
	stub := incusfake.NewWebsocketStub()
	defer stub.Close()

	source := &incusfake.Server{
		Instance:  &api.Instance{Name: "web"},
		Operation: stub.Operation(),
	}
	target := &incusfake.Server{}
	f := newAPIFixture(t, source, target)
	cookie := f.login(t)

	server := f.wsServer(t)
	execURL := "ws" + strings.TrimPrefix(server.URL, "http") +
		"/api/v1/clusters/c1/instances/web/exec"

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	header := http.Header{"Cookie": []string{cookie.Name + "=" + cookie.Value}}
	browser, _, err := dialer.Dial(execURL, header)
	if err != nil {
		t.Fatalf("dial exec: %v", err)
	}
	defer browser.Close() //nolint:errcheck // test teardown

	start := `{"command":["bash"],"cwd":"/root","width":100,"height":30}`
	if err := browser.WriteMessage(websocket.TextMessage, []byte(start)); err != nil {
		t.Fatalf("start write: %v", err)
	}

	incusConn := waitForConn(t, stub.Conns, "0")

	execCalls := source.ExecArgs()
	if len(execCalls) != 1 {
		t.Fatalf("exec args = %#v", execCalls)
	}
	if got := execCalls[0]; len(got.Command) != 1 || got.Command[0] != "bash" ||
		got.Cwd != "/root" || !got.Interactive || !got.WaitForWS {
		t.Fatalf("exec request = %#v", got)
	}

	if err := browser.WriteMessage(websocket.BinaryMessage, []byte("echo hi\n")); err != nil {
		t.Fatalf("browser write: %v", err)
	}
	_ = incusConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, payload, err := incusConn.ReadMessage()
	if err != nil {
		t.Fatalf("incus read: %v", err)
	}
	if string(payload) != "echo hi\n" {
		t.Fatalf("incus payload = %q", payload)
	}

	// Resize control frames reach the operation control channel as JSON.
	if err := browser.WriteMessage(websocket.TextMessage,
		[]byte(`{"type":"resize","width":120,"height":40}`)); err != nil {
		t.Fatalf("control write: %v", err)
	}
	controlConn := waitForConn(t, stub.Conns, "control")
	_ = controlConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, ctl, err := controlConn.ReadMessage()
	if err != nil {
		t.Fatalf("incus control read: %v", err)
	}
	if string(ctl) != `{"height":40,"width":120}` {
		t.Fatalf("control payload = %q", ctl)
	}
}

// Helpers -------------------------------------------------------------

func waitForConn(t *testing.T, conns chan incusfake.StubConn, fd string) *websocket.Conn {
	t.Helper()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case stub := <-conns:
			if stub.FD == fd {
				return stub.Conn
			}
		case <-deadline:
			t.Fatal("timed out waiting for stub websocket connection")
			return nil
		}
	}
}

func upload(t *testing.T, f *apiFixture, cookie *http.Cookie, path, content string) {
	t.Helper()

	body := &strings.Builder{}
	writer := newMultipartWriter(body, content)
	req := httptestRequest(t, http.MethodPost,
		"/api/v1/clusters/c1/instances/web/files?path="+url.QueryEscape(path),
		body.String(), cookie)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rec := f.serve(t, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d body = %s", rec.Code, rec.Body.String())
	}
}
