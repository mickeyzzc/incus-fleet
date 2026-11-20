package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/mickeyzzc/incus-fleet/internal/console"
	"github.com/mickeyzzc/incus-fleet/internal/nodenet"
)

// consoleGuest resolves the monitor for the instance named in the URL.
//
// The member that hosts the instance is looked up through the cluster manager so
// the monitor command runs on the right host.
func (a *API) consoleGuest(w http.ResponseWriter, r *http.Request) (console.Guest, console.HostCommand, func(), bool) {
	clusterName := r.PathValue("cluster")
	instanceName := r.PathValue("instance")

	detail, err := a.instances.Get(clusterName, instanceName)
	if err != nil {
		a.serviceError(w, r, err)
		return console.Guest{}, nil, nil, false
	}
	if detail.Type != "virtual-machine" {
		// Containers have no framebuffer; the online terminal covers them.
		writeError(w, http.StatusBadRequest, "unsupported_instance_type",
			"graphical console is only available for virtual machines")
		return console.Guest{}, nil, nil, false
	}
	if detail.Location == "" {
		writeError(w, http.StatusServiceUnavailable, "member_unknown",
			"cannot determine which member hosts this instance")
		return console.Guest{}, nil, nil, false
	}

	if a.nodenet == nil {
		writeError(w, http.StatusServiceUnavailable, "node_unavailable",
			"host command execution is not configured")
		return console.Guest{}, nil, nil, false
	}

	runner, closeRunner, err := a.desktopHostRunner(r, clusterName, detail.Location)
	if err != nil {
		a.serviceError(w, r, err)
		return console.Guest{}, nil, nil, false
	}

	return console.Guest{Cluster: clusterName, Instance: instanceName, Member: detail.Location}, runner, closeRunner, true
}

// desktopHostRunner resolves the console's host-command runner: the test
// override when set, otherwise the nodeaccess SSH path.
func (a *API) desktopHostRunner(r *http.Request, cluster, member string) (console.HostCommand, func(), error) {
	if a.desktopRun != nil {
		return a.desktopRun(r.Context(), cluster, member)
	}

	return a.nodenet.HostRunner(r.Context(), cluster, member)
}

// desktopPrepare arms the graphical console for one VM.
//
// It installs the extra monitor socket through raw.qemu.conf, which restarts a
// running VM once. Repeated calls are cheap because the configuration is only
// written when it differs.
func (a *API) desktopPrepare(w http.ResponseWriter, r *http.Request) {
	guest, runner, closeRunner, ok := a.consoleGuest(w, r)
	if !ok {
		return
	}
	defer closeRunner()

	monitor := console.NewMonitor(runner)
	restarted, err := monitor.EnsureMonitor(r.Context(), guest)
	if err != nil {
		a.serviceError(w, r, nodeUnavailable(err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ready":     true,
		"restarted": restarted,
	})
}

// desktopScreenshot returns the current framebuffer as a PNG.
//
// QEMU writes a PPM, which is converted on the host so the browser receives a
// format it can put straight into an <img>.
func (a *API) desktopScreenshot(w http.ResponseWriter, r *http.Request) {
	guest, runner, closeRunner, ok := a.consoleGuest(w, r)
	if !ok {
		return
	}
	defer closeRunner()

	monitor := console.NewMonitor(runner)
	png, err := monitor.Screenshot(r.Context(), guest)
	if err != nil {
		a.serviceError(w, r, nodeUnavailable(err))
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	// The bytes are the framebuffer dump fetched over the member's SSH path;
	// they are served with a fixed image content type, never interpolated.
	_, _ = w.Write(png) //nolint:gosec // G705: payload is a trusted screenshot blob
}

// desktopInput injects one keyboard or pointer event.
//
// The body is a small JSON object:
//
//	{"type":"key","key":"Enter","modifiers":["Control"]}
//	{"type":"pointer","x":0.5,"y":0.5,"button":"left","down":true}
func (a *API) desktopInput(w http.ResponseWriter, r *http.Request) {
	guest, runner, closeRunner, ok := a.consoleGuest(w, r)
	if !ok {
		return
	}
	defer closeRunner()

	var req struct {
		Type      string   `json:"type"`
		Key       string   `json:"key"`
		Modifiers []string `json:"modifiers"`
		Repeat    int      `json:"repeat"`
		X         float64  `json:"x"`
		Y         float64  `json:"y"`
		Button    string   `json:"button"`
		Down      bool     `json:"down"`
		Move      bool     `json:"move"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	monitor := console.NewMonitor(runner)

	switch strings.ToLower(req.Type) {
	case "key":
		err := monitor.SendKey(r.Context(), guest, console.KeyEvent{
			Key: req.Key, Modifiers: req.Modifiers, Repeat: req.Repeat,
		})
		if err != nil {
			a.writeConsoleError(w, r, err)
			return
		}
	case "pointer":
		err := monitor.SendPointer(r.Context(), guest, console.PointerEvent{
			X: req.X, Y: req.Y, Button: req.Button, Down: req.Down, Move: req.Move,
		})
		if err != nil {
			a.writeConsoleError(w, r, err)
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "type must be key or pointer")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// nodeUnavailable classifies console host-path failures (SSH, script exit,
// undecodable frame) as a member availability problem, matching how the other
// host-command endpoints report the same condition.
func nodeUnavailable(err error) error {
	return fmt.Errorf("%w: %v", nodenet.ErrUnavailable, err)
}

// writeConsoleError maps console errors onto client errors.
func (a *API) writeConsoleError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, console.ErrUnavailable) {
		writeError(w, http.StatusBadRequest, "console_unavailable", err.Error())
		return
	}

	a.serviceError(w, r, err)
}
