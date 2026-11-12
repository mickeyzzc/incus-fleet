package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"

	"github.com/gorilla/websocket"

	"github.com/mickeyzzc/incus-fleet/internal/instances"
)

// wsUpgrader upgrades browser connections to websockets. Origin checking is
// left to the session cookie plus the framework's same-origin defaults. The
// SPICE client negotiates the "binary" subprotocol and browsers abort the
// handshake unless the server echoes it back.
var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  32 << 10,
	WriteBufferSize: 32 << 10,
	Subprotocols:    []string{"binary"},
}

// instanceTerminal opens an interactive console websocket for an instance.
func (a *API) instanceTerminal(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	op, err := a.instances.ConsoleOp(r.PathValue("cluster"), r.PathValue("instance"))
	if err != nil {
		closeWSWithError(conn, err)
		return
	}

	_ = relayTerminal(r.Context(), conn, op, terminalDebugf(a, r))
}

// instanceVga opens the graphical (SPICE) console websocket of a VM.
func (a *API) instanceVga(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	op, err := a.instances.VgaOp(r.PathValue("cluster"), r.PathValue("instance"))
	if err != nil {
		closeWSWithError(conn, err)
		return
	}

	_ = relayTerminal(r.Context(), conn, op, terminalDebugf(a, r))
}

// instanceExec opens an interactive exec websocket for an instance. The first
// browser frame carries the JSON start request.
func (a *API) instanceExec(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	_, payload, err := conn.ReadMessage()
	if err != nil {
		_ = conn.Close()
		return
	}

	var req instances.ExecRequest
	if err := decodeJSONBytes(payload, &req); err != nil {
		closeWSWithError(conn, err)
		return
	}

	op, err := a.instances.ExecOp(r.PathValue("cluster"), r.PathValue("instance"), req)
	if err != nil {
		closeWSWithError(conn, err)
		return
	}

	_ = relayTerminal(r.Context(), conn, op, terminalDebugf(a, r))
}

// instanceFiles lists or downloads files from an instance.
func (a *API) instanceFiles(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("action") == "list" {
		files, err := a.instances.ListFiles(r.PathValue("cluster"), r.PathValue("instance"), r.URL.Query().Get("path"))
		if err != nil {
			a.serviceError(w, r, err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"files": files})
		return
	}

	target := r.URL.Query().Get("path")
	reader, err := a.instances.DownloadFile(r.PathValue("cluster"), r.PathValue("instance"), target)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}
	defer reader.Close() //nolint:errcheck // streaming best-effort

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", path.Base(target)))
	_, _ = io.Copy(w, reader)
}

// uploadInstanceFile creates a directory or uploads one file.
func (a *API) uploadInstanceFile(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("action") == "mkdir" {
		if err := a.instances.MkdirDir(r.PathValue("cluster"), r.PathValue("instance"), r.URL.Query().Get("path")); err != nil {
			a.serviceError(w, r, err)
			return
		}

		w.WriteHeader(http.StatusNoContent)
		return
	}

	// The body size is bounded above; the in-memory cap only controls when
	// the parser spills to temporary files.
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil { //nolint:gosec // body already bounded via MaxBytesReader
		writeError(w, http.StatusBadRequest, "invalid_request", "multipart form required")
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "file field required")
		return
	}
	defer file.Close() //nolint:errcheck // upload cleanup

	content, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.UploadFile(r.PathValue("cluster"), r.PathValue("instance"), r.URL.Query().Get("path"), content); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// deleteInstanceFile removes one file from an instance.
func (a *API) deleteInstanceFile(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.DeleteFile(r.PathValue("cluster"), r.PathValue("instance"), r.URL.Query().Get("path")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// decodeJSONBytes decodes a JSON payload already read from a frame.
func decodeJSONBytes(payload []byte, target any) error {
	return json.Unmarshal(payload, target)
}

// terminalDebugf logs relay lifecycle details for one session.
func terminalDebugf(a *API, r *http.Request) func(string, ...any) {
	return func(format string, args ...any) {
		a.logger.Info("terminal relay", "instance", r.PathValue("instance"), "msg", fmt.Sprintf(format, args...))
	}
}

func closeWSWithError(conn *websocket.Conn, err error) {
	_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error()))
	_ = conn.Close()
}
