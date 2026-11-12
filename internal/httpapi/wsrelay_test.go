package httpapi

import (
	"encoding/json"
	"testing"
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
