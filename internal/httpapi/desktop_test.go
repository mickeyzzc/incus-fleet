package httpapi

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/console"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

// fakeDesktopRunner records the monitor scripts it received and replies with
// canned output, standing in for the SSH host-command path.
type fakeDesktopRunner struct {
	scripts []string
	stdout  string
	err     error
}

func (f *fakeDesktopRunner) Run(_ context.Context, script string) (string, error) {
	f.scripts = append(f.scripts, script)
	if f.err != nil {
		return "", f.err
	}

	return f.stdout, nil
}

func newDesktopFixture(t *testing.T, instance *incusfake.Server) (*apiFixture, *fakeDesktopRunner, *http.Cookie) {
	t.Helper()

	runner := &fakeDesktopRunner{}
	f := newAPIFixtureWithDeps(t, instance, &incusfake.Server{}, nil, func(deps *Deps) {
		deps.DesktopRunner = func(_ context.Context, _ string, _ string) (console.HostCommand, func(), error) {
			return runner, func() {}, nil
		}
	})
	cookie := f.login(t)

	return f, runner, cookie
}

func newDesktopVM(name, location string) *incusfake.Server {
	return &incusfake.Server{
		Instance: &api.Instance{
			Name:       name,
			Type:       "virtual-machine",
			Status:     "Running",
			StatusCode: api.Running,
			Location:   location,
		},
	}
}

// pngPayload is a tiny valid PNG, base64-encoded as the host script returns it.
var pngPayload = base64.StdEncoding.EncodeToString([]byte{
	0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', // signature
	0, 0, 0, 13, 'I', 'H', 'D', 'R', // IHDR
})

func TestDesktopPrepareArmsMonitor(t *testing.T) {
	f, runner, cookie := newDesktopFixture(t, newDesktopVM("vm-01", "node-a"))
	runner.stdout = "changed=1"

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/vm-01/desktop/prepare", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("prepare status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"restarted":true`) {
		t.Fatalf("prepare body = %s", rec.Body.String())
	}
	if len(runner.scripts) != 1 || !strings.Contains(runner.scripts[0], "raw.qemu.conf") {
		t.Fatalf("prepare script = %q", runner.scripts)
	}
}

func TestDesktopScreenshotReturnsPNG(t *testing.T) {
	f, runner, cookie := newDesktopFixture(t, newDesktopVM("vm-01", "node-a"))
	runner.stdout = pngPayload

	rec := f.request(t, http.MethodGet, "/api/v1/clusters/c1/instances/vm-01/desktop/screenshot", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("screenshot status = %d body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("screenshot content type = %q", got)
	}
	if !strings.HasPrefix(rec.Body.String(), "\x89PNG") {
		t.Fatalf("screenshot body is not a PNG: %q", rec.Body.String()[:16])
	}
	if len(runner.scripts) != 1 || !strings.Contains(runner.scripts[0], "screendump") {
		t.Fatalf("screenshot script = %q", runner.scripts)
	}
}

func TestDesktopInputSendsKeyAndPointer(t *testing.T) {
	f, runner, cookie := newDesktopFixture(t, newDesktopVM("vm-01", "node-a"))

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/vm-01/desktop/input",
		map[string]any{"type": "key", "key": "ArrowDown"}, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("key input status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/vm-01/desktop/input",
		map[string]any{"type": "pointer", "x": 0.5, "y": 0.25, "button": "left", "down": true}, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("pointer input status = %d body = %s", rec.Code, rec.Body.String())
	}

	if len(runner.scripts) != 2 {
		t.Fatalf("input scripts = %d, want 2", len(runner.scripts))
	}
	if !strings.Contains(runner.scripts[0], "input-send-event") ||
		!strings.Contains(runner.scripts[0], `"qcode"`) {
		t.Fatalf("key script missing qcode payload:\n%s", runner.scripts[0])
	}
	if !strings.Contains(runner.scripts[1], `"abs"`) || !strings.Contains(runner.scripts[1], `"btn"`) {
		t.Fatalf("pointer script missing abs/btn payload:\n%s", runner.scripts[1])
	}
}

func TestDesktopInputRejectsBadRequests(t *testing.T) {
	f, runner, cookie := newDesktopFixture(t, newDesktopVM("vm-01", "node-a"))

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/vm-01/desktop/input",
		map[string]any{"type": "mouse"}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown type status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/vm-01/desktop/input",
		map[string]any{"type": "key"}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing key status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/vm-01/desktop/input",
		map[string]any{"type": "pointer", "x": 1.5, "y": 0.5, "move": true}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("out-of-range pointer status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/vm-01/desktop/input",
		"not-json", cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json status = %d", rec.Code)
	}

	if len(runner.scripts) != 0 {
		t.Fatalf("rejected inputs must not reach the host, got %d", len(runner.scripts))
	}
}

func TestDesktopRequiresVirtualMachine(t *testing.T) {
	container := &incusfake.Server{
		Instance: &api.Instance{
			Name:       "ct-01",
			Type:       "container",
			Status:     "Running",
			StatusCode: api.Running,
			Location:   "node-a",
		},
	}
	f, runner, cookie := newDesktopFixture(t, container)

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/ct-01/desktop/prepare", nil, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("container prepare status = %d body = %s", rec.Code, rec.Body.String())
	}
	if len(runner.scripts) != 0 {
		t.Fatalf("container must not reach the host, got %d scripts", len(runner.scripts))
	}
}

func TestDesktopSurfacesHostErrors(t *testing.T) {
	f, runner, cookie := newDesktopFixture(t, newDesktopVM("vm-01", "node-a"))
	runner.err = errors.New("ssh refused")

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/vm-01/desktop/prepare", nil, cookie, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("prepare host error status = %d", rec.Code)
	}

	rec = f.request(t, http.MethodGet, "/api/v1/clusters/c1/instances/vm-01/desktop/screenshot", nil, cookie, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("screenshot host error status = %d", rec.Code)
	}
}

func TestDesktopScreenshotRejectsGarbage(t *testing.T) {
	f, runner, cookie := newDesktopFixture(t, newDesktopVM("vm-01", "node-a"))
	runner.stdout = "not-base64!!!"

	rec := f.request(t, http.MethodGet, "/api/v1/clusters/c1/instances/vm-01/desktop/screenshot", nil, cookie, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("screenshot garbage status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestDesktopRequiresKnownMember(t *testing.T) {
	f, _, cookie := newDesktopFixture(t, newDesktopVM("vm-01", ""))

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/vm-01/desktop/prepare", nil, cookie, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unknown member status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestDesktopFallsBackToNodeAccessRunner(t *testing.T) {
	// Without the test override the console uses the nodeaccess SSH path; with
	// no SSH identity configured it must surface as node_unavailable (503).
	vm := newDesktopVM("vm-01", "test-node")
	f := newAPIFixtureWithDeps(t, vm, &incusfake.Server{}, nil, nil)
	cookie := f.login(t)

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/vm-01/desktop/prepare", nil, cookie, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("fallback prepare status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "node_unavailable") {
		t.Fatalf("fallback prepare body = %s", rec.Body.String())
	}
}

func TestDesktopInputSurfacesHostErrors(t *testing.T) {
	f, runner, cookie := newDesktopFixture(t, newDesktopVM("vm-01", "node-a"))
	runner.err = errors.New("socket gone")

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/instances/vm-01/desktop/input",
		map[string]any{"type": "key", "key": "Enter"}, cookie, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("input host error status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "socket gone") {
		t.Fatalf("input host error body = %s", rec.Body.String())
	}
}
