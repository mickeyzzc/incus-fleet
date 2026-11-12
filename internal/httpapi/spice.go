package httpapi

import (
	"io"
	"net/http"
	"strconv"

	"github.com/gorilla/websocket"

	"github.com/mickeyzzc/incus-fleet/internal/instances"
)

// prepareSPICE arms the VM's qemu SPICE TCP listener so the browser client
// can open one websocket per SPICE channel against the bridge endpoint.
func (a *API) prepareSPICE(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Port  int  `json:"port"`
		Apply bool `json:"apply"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if req.Port == 0 {
		req.Port = instances.SPICEPortFor(r.PathValue("instance"))
	}

	result, err := a.instances.PrepareSPICE(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), req.Port, req.Apply)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// instanceSPICE bridges one websocket onto the VM's SPICE TCP listener; the
// SPICE client opens several of these, one per protocol channel.
func (a *API) instanceSPICE(w http.ResponseWriter, r *http.Request) {
	port, portErr := strconv.Atoi(r.URL.Query().Get("port"))
	if portErr != nil {
		closeWSWithErrorOrPlain(w, "spice port required")
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	tcp, err := a.instances.SPICEDial(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), port)
	if err != nil {
		closeWSWithError(conn, err)
		return
	}
	defer tcp.Close() //nolint:errcheck // bridge teardown

	done := make(chan error, 2)

	go func() {
		_, _ = io.Copy(wsWriter{conn}, tcp)
		done <- nil
	}()

	go func() {
		for {
			_, payload, readErr := conn.ReadMessage()
			if readErr != nil {
				done <- nil
				return
			}
			if _, writeErr := tcp.Write(payload); writeErr != nil {
				done <- writeErr
				return
			}
		}
	}()

	bridgeErr := <-done
	if bridgeErr != nil {
		_ = conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, bridgeErr.Error()))
	}
	_ = conn.Close()
}

// wsWriter adapts websocket binary writes to the io.Writer io.Copy expects.
type wsWriter struct {
	conn *websocket.Conn
}

func (w wsWriter) Write(p []byte) (int, error) {
	if err := w.conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}

	return len(p), nil
}

// closeWSWithErrorOrPlain rejects requests that never became websockets.
func closeWSWithErrorOrPlain(w http.ResponseWriter, message string) {
	writeError(w, http.StatusBadRequest, "invalid_request", message)
}
