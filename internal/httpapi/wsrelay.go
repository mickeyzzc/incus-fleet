package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/gorilla/websocket"
	incus "github.com/lxc/incus/v7/client"
)

// terminalControl is the fleet-side control frame sent by the browser.
type terminalControl struct {
	Type   string `json:"type"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Signal string `json:"signal"`
}

// relayTerminal pipes a browser websocket to an Incus operation's data and
// control channels until either side closes or the operation finishes.
//
// Binary frames carry raw terminal bytes in both directions. Text frames from
// the browser are control messages; text frames from Incus carry base64
// encoded output, which is decoded before it reaches the browser.
func relayTerminal(ctx context.Context, browser *websocket.Conn, op incus.Operation, debugf func(string, ...any)) error {
	// The daemon hands out per-fd secrets in the operation metadata; the
	// GetWebsocket argument is the secret value, not the fd name.
	opAPI := op.Get()
	fds, _ := opAPI.Metadata["fds"].(map[string]any)
	secretFor := func(name string) string {
		value, _ := fds[name].(string)
		return value
	}

	data, err := op.GetWebsocket(secretFor("0"))
	if err != nil {
		debugf("relay: data channel failed: %v", err)
		_ = browser.Close()
		return err
	}
	debugf("relay: data channel connected")

	// The control channel is optional: consoles receive it through the SDK
	// callback, exec through the operation fd; defensive relaying keeps the
	// session alive without it.
	controlConn := waitForControl(func() *websocket.Conn {
		secret := secretFor("control")
		if secret == "" {
			return nil
		}

		conn, err := op.GetWebsocket(secret)
		if err != nil {
			return nil
		}

		return conn
	}, 3*time.Second)
	debugf("relay: control channel present: %v", controlConn != nil)

	done := make(chan error, 4)

	go incusToBrowser(browser, data, done)
	go browserToIncus(browser, data, controlConn, done)
	go func() {
		waitErr := op.WaitContext(ctx)
		debugf("relay: operation finished: %v", waitErr)
		done <- nil
	}()

	first := <-done

	// Best-effort teardown of the remote operation. When a client vanishes
	// the container-side console session can outlive the websocket and leave
	// a forkconsole process behind; that leaked process holds the console
	// tty and makes every future attach fail with "attaching to the
	// container failed". Cancelling the operation kills it.
	if cancelErr := op.Cancel(); cancelErr != nil {
		debugf("relay: operation cancel: %v", cancelErr)
	}

	// Send websocket close frames before dropping the sockets. A bare TCP
	// FIN looks like an abnormal closure (1006) to incusd, and its console
	// handler then leaves the container-side forkconsole process running.
	closeFrame := func(conn *websocket.Conn) {
		deadline := time.Now().Add(time.Second)
		_ = conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), deadline)
	}
	closeFrame(data)
	closeFrame(browser)
	if controlConn != nil {
		closeFrame(controlConn)
	}

	_ = browser.Close()
	_ = data.Close()
	if controlConn != nil {
		_ = controlConn.Close()
	}

	return first
}

// waitForControl polls the control getter; the console control connection is
// delivered asynchronously by the SDK callback.
func waitForControl(get func() *websocket.Conn, limit time.Duration) *websocket.Conn {
	deadline := time.Now().Add(limit)
	for {
		if conn := get(); conn != nil {
			return conn
		}
		if time.Now().After(deadline) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// incusToBrowser forwards terminal output to the browser.
func incusToBrowser(browser, data *websocket.Conn, done chan<- error) {
	for {
		msgType, payload, err := data.ReadMessage()
		if err != nil {
			done <- nil
			return
		}
		_ = msgType
		_ = payload

		if msgType == websocket.TextMessage {
			if decoded, decodeErr := base64.StdEncoding.DecodeString(string(payload)); decodeErr == nil {
				payload = decoded
			}
			msgType = websocket.BinaryMessage
		}

		if err := browser.WriteMessage(msgType, payload); err != nil {
			done <- err
			return
		}
	}
}

// browserToIncus forwards raw input and translates control frames.
func browserToIncus(browser, data, control *websocket.Conn, done chan<- error) {
	for {
		msgType, payload, err := browser.ReadMessage()
		if err != nil {
			done <- nil
			return
		}

		if msgType == websocket.TextMessage {
			if control == nil {
				continue
			}

			var ctl terminalControl
			if jsonErr := json.Unmarshal(payload, &ctl); jsonErr != nil {
				continue
			}

			forwarded, marshalErr := controlFrame(ctl)
			if marshalErr != nil {
				continue
			}
			if err := control.WriteMessage(websocket.TextMessage, forwarded); err != nil {
				done <- err
				return
			}

			continue
		}

		if err := data.WriteMessage(websocket.BinaryMessage, payload); err != nil {
			done <- err
			return
		}
	}
}

// controlFrame renders an Incus control-channel message.
func controlFrame(ctl terminalControl) ([]byte, error) {
	if ctl.Type == "signal" && ctl.Signal != "" {
		return json.Marshal(map[string]string{"signal": ctl.Signal})
	}

	return json.Marshal(map[string]int{"width": ctl.Width, "height": ctl.Height})
}
