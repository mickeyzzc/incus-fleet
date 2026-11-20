package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestControlFrame(t *testing.T) {
	resize, err := controlFrame(terminalControl{Type: "resize", Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("controlFrame() resize error = %v", err)
	}
	var resizeMap map[string]int
	if err := json.Unmarshal(resize, &resizeMap); err != nil {
		t.Fatalf("decode resize: %v", err)
	}
	if resizeMap["width"] != 120 || resizeMap["height"] != 40 {
		t.Fatalf("resize frame = %s", resize)
	}

	signal, err := controlFrame(terminalControl{Type: "signal", Signal: "SIGINT"})
	if err != nil {
		t.Fatalf("controlFrame() signal error = %v", err)
	}
	var signalMap map[string]string
	if err := json.Unmarshal(signal, &signalMap); err != nil {
		t.Fatalf("decode signal: %v", err)
	}
	if signalMap["signal"] != "SIGINT" {
		t.Fatalf("signal frame = %s", signal)
	}

	// A resize with an unknown type still renders as a resize frame.
	fallback, err := controlFrame(terminalControl{})
	if err != nil {
		t.Fatalf("controlFrame() fallback error = %v", err)
	}
	if fallback == nil {
		t.Fatal("controlFrame() fallback missing")
	}
}

// wsChannel builds one websocket pair: the test dials a local upgrade server
// and keeps the server side open until the relay tears it down.
func wsChannel(t *testing.T) (server, client *websocket.Conn) {
	t.Helper()

	upgrader := websocket.Upgrader{}
	var serverConn *websocket.Conn
	ready := make(chan struct{})
	handlerDone := make(chan struct{})
	t.Cleanup(func() { close(handlerDone) })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConn = conn
		close(ready)
		// The relay owns all reads on the hijacked connection; the handler
		// only keeps it open until the test ends.
		<-handlerDone
	}))
	t.Cleanup(srv.Close)

	clientConn, _, err := (&websocket.Dialer{}).Dial("ws://"+strings.TrimPrefix(srv.URL, "http://"), nil)
	if err != nil {
		t.Fatalf("dial channel: %v", err)
	}
	t.Cleanup(func() { _ = clientConn.Close() })

	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("upgrade never happened")
	}

	return serverConn, clientConn
}

type relayCapture struct {
	mu   sync.Mutex
	code int
	text string
	seen chan struct{}
	once sync.Once
}

func newRelayCapture(browser *websocket.Conn) *relayCapture {
	c := &relayCapture{seen: make(chan struct{})}
	browser.SetCloseHandler(func(code int, text string) error {
		c.mu.Lock()
		c.code = code
		c.text = text
		c.mu.Unlock()
		c.once.Do(func() { close(c.seen) })
		return nil
	})
	return c
}

func (c *relayCapture) wait(t *testing.T) (int, string) {
	t.Helper()
	select {
	case <-c.seen:
	case <-time.After(3 * time.Second):
		t.Fatal("browser never saw the close frame")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.code, c.text
}

// runRelay starts a relay session over local channel pairs and ends it by
// closing the data channel from the test side.
func runRelay(t *testing.T, opErr error) (browserClient *websocket.Conn, capture *relayCapture) {
	t.Helper()

	browserServer, browserClient := wsChannel(t)
	dataServer, dataClient := wsChannel(t)
	controlServer, _ := wsChannel(t)
	t.Cleanup(func() { _ = dataClient.Close() })

	op := &incusfake.Operation{
		Err:       opErr,
		FDSecrets: map[string]string{"0": "data", "control": "control"},
		GetWebsocketFunc: func(fd string) (*websocket.Conn, error) {
			if fd == "data" {
				return dataServer, nil
			}
			return controlServer, nil
		},
	}

	capture = newRelayCapture(browserClient)

	// The close handler fires when the client reads the close frame.
	go func() {
		for {
			if _, _, err := browserClient.ReadMessage(); err != nil {
				return
			}
		}
	}()

	go func() {
		_ = relayTerminal(context.Background(), browserServer, op, func(string, ...any) {})
	}()

	// Ending the incus data channel finishes the relay; the operation's own
	// result decides the close frame the browser receives.
	_ = dataClient.Close()

	return browserClient, capture
}

func TestRelayReportsOperationFailureInCloseFrame(t *testing.T) {
	_, capture := runRelay(t, errors.New("Failed to connect to the incus-agent"))

	code, text := capture.wait(t)
	if code != websocket.CloseInternalServerErr {
		t.Fatalf("close code = %d, want 1011", code)
	}
	if !strings.Contains(text, "incus-agent") {
		t.Fatalf("close reason = %q", text)
	}
}

func TestRelayCloseFrameTruncatesLongReason(t *testing.T) {
	_, capture := runRelay(t, errors.New(strings.Repeat("失败", 200)))

	code, text := capture.wait(t)
	if code != websocket.CloseInternalServerErr {
		t.Fatalf("close code = %d, want 1011", code)
	}
	if len(text) > 120 {
		t.Fatalf("close reason length = %d", len(text))
	}
	// A rune-boundary truncation never leaves broken UTF-8 behind.
	if strings.ToValidUTF8(text, "?") != text {
		t.Fatal("close reason broke a UTF-8 sequence")
	}
	if !strings.HasSuffix(text, "失败") || text == "" {
		t.Fatalf("close reason = %q", text)
	}
}

func TestRelayNormalCloseHasNoReason(t *testing.T) {
	_, capture := runRelay(t, nil)

	code, text := capture.wait(t)
	if code != websocket.CloseNormalClosure || text != "" {
		t.Fatalf("close = %d %q, want 1000 empty", code, text)
	}
}
