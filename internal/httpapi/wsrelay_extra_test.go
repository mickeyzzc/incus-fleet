package httpapi

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWaitForControlTimesOut(t *testing.T) {
	// A getter that never yields a connection must return nil before hanging.
	start := time.Now()
	if conn := waitForControl(func() *websocket.Conn { return nil }, 100*time.Millisecond); conn != nil {
		t.Fatal("waitForControl() returned a connection that never appeared")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("waitForControl() took %v", elapsed)
	}
}

func TestWaitForControlReturnsImmediately(t *testing.T) {
	conn := &websocket.Conn{}
	if got := waitForControl(func() *websocket.Conn { return conn }, time.Second); got != conn {
		t.Fatal("waitForControl() did not return the available connection")
	}
}
