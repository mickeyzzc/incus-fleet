package console

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// fakeHost captures every script it was asked to run and replies with canned
// stdout, so tests can assert on the generated payload without a real QEMU.
type fakeHost struct {
	script   string
	stdout   string
	err      error
	calls    int
	verifier func(script string) error
}

func (f *fakeHost) Run(_ context.Context, script string) (string, error) {
	f.calls++
	f.script = script
	if f.verifier != nil {
		if err := f.verifier(script); err != nil {
			return "", err
		}
	}
	return f.stdout, f.err
}

func TestEnsureMonitorWritesQMPConfig(t *testing.T) {
	host := &fakeHost{stdout: "changed=1"}
	monitor := NewMonitor(host)

	restarted, err := monitor.EnsureMonitor(context.Background(), Guest{Cluster: "c63", Instance: "deb13-vm"})
	if err != nil {
		t.Fatalf("EnsureMonitor() error = %v", err)
	}
	if !restarted {
		t.Fatal("EnsureMonitor() restarted = false, want true when config changed")
	}

	// The QMP monitor section must use control mode and double-quoted values;
	// single quotes break QEMU's config parser.
	for _, want := range []string{
		"[chardev \"fleetmon\"]",
		"path = \"/tmp/fleet-qmp-deb13-vm.sock\"",
		"[mon \"fleetmon\"]",
		"mode = \"control\"",
	} {
		if !strings.Contains(host.script, want) {
			t.Errorf("prepare script missing %q:\n%s", want, host.script)
		}
	}

	// AppArmor rules are plain paths: quotes make apparmor_parser reject them.
	if !strings.Contains(host.script, `/tmp/fleet-qmp-deb13-vm.sock mrw,`) ||
		!strings.Contains(host.script, `/tmp/incus_screenshot_deb13-vm mrw,`) {
		t.Errorf("prepare script missing AppArmor paths:\n%s", host.script)
	}

	// Both the socket and the AppArmor rule must be in place before the VM
	// starts: QEMU binds the socket at startup, and a restart with a stale
	// profile fails with "Permission denied".
	idxStop := strings.Index(host.script, "incus stop")
	idxApparmor := strings.Index(host.script, `incus config set 'deb13-vm' "raw.apparmor=`)
	idxConf := strings.Index(host.script, `incus config set 'deb13-vm' "raw.qemu.conf=`)
	idxStart := strings.LastIndex(host.script, "incus start")
	if idxStop == -1 || idxApparmor == -1 || idxConf == -1 || idxStart == -1 {
		t.Fatalf("prepare script missing a step: stop=%d apparmor=%d conf=%d start=%d", idxStop, idxApparmor, idxConf, idxStart)
	}
	if idxStop >= idxApparmor || idxApparmor >= idxConf || idxConf >= idxStart {
		t.Errorf("prepare script order is stop < apparmor < conf < start, got stop=%d apparmor=%d conf=%d start=%d",
			idxStop, idxApparmor, idxConf, idxStart)
	}

	// The wanted values must be newline-normalised before the comparison:
	// command substitution strips trailing newlines from the stored value, so
	// an unnormalised literal always looks changed and restarts a running VM.
	if !strings.Contains(host.script, `want_conf=$(printf '%s' "$want_conf")`) ||
		!strings.Contains(host.script, `want_apparmor=$(printf '%s' "$want_apparmor")`) {
		t.Errorf("prepare script does not normalise trailing newlines:\n%s", host.script)
	}
}

func TestEnsureMonitorIdempotent(t *testing.T) {
	host := &fakeHost{stdout: "changed=0"}
	monitor := NewMonitor(host)

	restarted, err := monitor.EnsureMonitor(context.Background(), Guest{Cluster: "c63", Instance: "deb13-vm"})
	if err != nil {
		t.Fatalf("EnsureMonitor() error = %v", err)
	}
	if restarted {
		t.Fatal("EnsureMonitor() restarted = true, want false when config matches")
	}
}

func TestScreenshotScriptQuotesSocketOnce(t *testing.T) {
	// The regression this guards against: quoting the socket path twice embeds
	// literal quote characters in argv, and connect() fails with
	// FileNotFoundError on a path that does not exist.
	host := &fakeHost{stdout: "aGVsbG8="}
	monitor := NewMonitor(host)

	png, err := monitor.Screenshot(context.Background(), Guest{Cluster: "c63", Instance: "deb13-vm"})
	if err != nil {
		t.Fatalf("Screenshot() error = %v", err)
	}
	if string(png) != "hello" {
		t.Fatalf("Screenshot() = %q, want decoded base64 payload", png)
	}

	// The heredoc invocation passes the socket path exactly once-quoted.
	if !strings.Contains(host.script, `python3 - '/tmp/fleet-qmp-deb13-vm.sock'`) {
		t.Errorf("screenshot script does not pass a once-quoted socket path:\n%s", host.script)
	}
	if strings.Contains(host.script, `''`) {
		t.Errorf("screenshot script contains doubled quotes:\n%s", host.script)
	}
	// PNG dump is requested; the PPM fallback path must also be present.
	if !strings.Contains(host.script, `"format": "png"`) {
		t.Error("screenshot script does not request png format")
	}
	if !strings.Contains(host.script, `/tmp/incus_screenshot_deb13-vm`) {
		t.Error("screenshot script does not use the AppArmor-granted dump path")
	}
}

func TestScreenshotPropagatesHostError(t *testing.T) {
	host := &fakeHost{err: errors.New("boom")}
	monitor := NewMonitor(host)

	if _, err := monitor.Screenshot(context.Background(), Guest{Cluster: "c63", Instance: "x"}); err == nil {
		t.Fatal("Screenshot() expected an error when the host command fails")
	}
}

// inputPayload extracts the JSON payload the input script was handed.
//
// The python invocation reads: python3 - '<socket>' '<payload>' — two
// single-quoted argv entries after the dash. shellQuote never emits a bare
// single quote inside a value, so splitting on the quote boundaries is exact.
func inputPayload(script string) ([]map[string]any, error) {
	idx := strings.Index(script, `python3 - '`)
	if idx == -1 {
		return nil, errors.New("script does not call the monitor python payload")
	}
	line := script[idx:]
	if end := strings.Index(line, "\n"); end >= 0 {
		line = line[:end]
	}

	fields := strings.Split(line, "'")
	// line = "python3 - " + "'" + sock + "'" + " " + "'" + payload + "'" + " <<'PY'"
	//       fields[0]   |  [1] sock   [2]  [3]  |  [4] payload     [5]   [6]   [7]
	if len(fields) < 6 {
		return nil, errors.New("python invocation is missing quoted arguments")
	}
	payload := fields[3]

	var events struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal([]byte(payload), &events); err != nil {
		return nil, err
	}
	return events.Events, nil
}

func TestSendKeyArrowDown(t *testing.T) {
	host := &fakeHost{}
	monitor := NewMonitor(host)

	if err := monitor.SendKey(context.Background(), Guest{Cluster: "c63", Instance: "deb13-vm"},
		KeyEvent{Key: "ArrowDown"}); err != nil {
		t.Fatalf("SendKey() error = %v", err)
	}

	events, err := inputPayload(host.script)
	if err != nil {
		t.Fatalf("input payload malformed: %v\nscript:\n%s", err, host.script)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want press+release", len(events))
	}
	if events[0]["type"] != "key" || events[1]["type"] != "key" {
		t.Fatalf("event types = %v", events)
	}
	press := events[0]["data"].(map[string]any)
	release := events[1]["data"].(map[string]any)
	if press["down"] != true || release["down"] != false {
		t.Fatalf("down flags = %v / %v", press, release)
	}
	key := press["key"].(map[string]any)
	if key["type"] != "qcode" || key["data"] != "down" {
		t.Fatalf("key = %v, want qcode down", key)
	}
}

func TestSendKeyChordOrder(t *testing.T) {
	host := &fakeHost{}
	monitor := NewMonitor(host)

	if err := monitor.SendKey(context.Background(), Guest{Cluster: "c63", Instance: "deb13-vm"},
		KeyEvent{Key: "F1", Modifiers: []string{"Control", "Alt"}}); err != nil {
		t.Fatalf("SendKey() error = %v", err)
	}

	events, err := inputPayload(host.script)
	if err != nil {
		t.Fatalf("input payload malformed: %v", err)
	}
	if len(events) != 6 {
		t.Fatalf("got %d events, want ctrl+alt hold, f1 tap, release", len(events))
	}
	qcodes := make([]string, 0, len(events))
	downs := make([]bool, 0, len(events))
	for _, ev := range events {
		data := ev["data"].(map[string]any)
		qcodes = append(qcodes, data["key"].(map[string]any)["data"].(string))
		downs = append(downs, data["down"].(bool))
	}
	wantQcodes := []string{"ctrl", "alt", "f1", "f1", "alt", "ctrl"}
	wantDowns := []bool{true, true, true, false, false, false}
	for i := range events {
		if qcodes[i] != wantQcodes[i] || downs[i] != wantDowns[i] {
			t.Fatalf("event %d = %s(down=%v), want %s(down=%v)",
				i, qcodes[i], downs[i], wantQcodes[i], wantDowns[i])
		}
	}
}

func TestSendKeyRepeatAndShiftedSymbol(t *testing.T) {
	host := &fakeHost{}
	monitor := NewMonitor(host)

	// "?" needs shift held around slash.
	if err := monitor.SendKey(context.Background(), Guest{Cluster: "c63", Instance: "deb13-vm"},
		KeyEvent{Key: "?", Repeat: 2}); err != nil {
		t.Fatalf("SendKey() error = %v", err)
	}

	events, err := inputPayload(host.script)
	if err != nil {
		t.Fatalf("input payload malformed: %v", err)
	}
	// Two repeats of: shift down, slash down, slash up, shift up.
	if len(events) != 8 {
		t.Fatalf("got %d events, want two shift+slash taps", len(events))
	}
	first := events[0]["data"].(map[string]any)
	if first["key"].(map[string]any)["data"] != "shift" {
		t.Fatalf("first event = %v, want shift held", first)
	}
	third := events[2]["data"].(map[string]any)
	if third["key"].(map[string]any)["data"] != "slash" {
		t.Fatalf("third event = %v, want slash release", third)
	}
}

func TestSendKeyRejectsUnknown(t *testing.T) {
	monitor := NewMonitor(&fakeHost{})

	if err := monitor.SendKey(context.Background(), Guest{},
		KeyEvent{Key: "MediaPlay"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("SendKey() error = %v, want ErrUnavailable", err)
	}
	if err := monitor.SendKey(context.Background(), Guest{}, KeyEvent{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("SendKey() error = %v, want ErrUnavailable", err)
	}
}

func TestSendPointerAbsolute(t *testing.T) {
	host := &fakeHost{}
	monitor := NewMonitor(host)

	if err := monitor.SendPointer(context.Background(), Guest{Cluster: "c63", Instance: "deb13-vm"},
		PointerEvent{X: 0.5, Y: 0.25, Button: "left", Down: true}); err != nil {
		t.Fatalf("SendPointer() error = %v", err)
	}

	events, err := inputPayload(host.script)
	if err != nil {
		t.Fatalf("input payload malformed: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want abs x, abs y, btn press", len(events))
	}
	x := events[0]["data"].(map[string]any)
	if x["axis"] != "x" || x["value"] != float64(16384) { // 0.5 * 32767 rounds to 16384
		t.Fatalf("x event = %v, want axis x at half range", x)
	}
	y := events[1]["data"].(map[string]any)
	if y["axis"] != "y" || y["value"] != float64(8192) { // 0.25 * 32767 rounds to 8192
		t.Fatalf("y event = %v, want axis y at quarter range", y)
	}
	btn := events[2]["data"].(map[string]any)
	if btn["button"] != "left" || btn["down"] != true {
		t.Fatalf("btn event = %v, want left press", btn)
	}
}

func TestSendPointerMoveOnly(t *testing.T) {
	host := &fakeHost{}
	monitor := NewMonitor(host)

	if err := monitor.SendPointer(context.Background(), Guest{Cluster: "c63", Instance: "deb13-vm"},
		PointerEvent{X: 0.1, Y: 0.9, Move: true}); err != nil {
		t.Fatalf("SendPointer() error = %v", err)
	}

	events, err := inputPayload(host.script)
	if err != nil {
		t.Fatalf("input payload malformed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want two abs events", len(events))
	}
}

func TestSendPointerRejects(t *testing.T) {
	monitor := NewMonitor(&fakeHost{})

	if err := monitor.SendPointer(context.Background(), Guest{},
		PointerEvent{X: 1.5, Y: 0.1, Move: true}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("SendPointer() error = %v, want ErrUnavailable", err)
	}
	if err := monitor.SendPointer(context.Background(), Guest{},
		PointerEvent{X: 0.5, Y: 0.5, Button: "pinky", Move: true}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("SendPointer() error = %v, want ErrUnavailable for unknown button", err)
	}
}

func TestQcodeName(t *testing.T) {
	cases := []struct {
		key     string
		qcode   string
		shifted bool
		fails   bool
	}{
		{key: "a", qcode: "a"}, {key: "5", qcode: "5"},
		{key: "A", qcode: "a", shifted: true},
		{key: "Enter", qcode: "ret"}, {key: "Escape", qcode: "esc"},
		{key: "Delete", qcode: "delete"}, {key: " ", qcode: "spc"},
		{key: "ArrowDown", qcode: "down"}, {key: "PageUp", qcode: "pgup"},
		{key: "-", qcode: "minus"}, {key: "=", qcode: "equal"},
		{key: "?", qcode: "slash", shifted: true},
		{key: "!", qcode: "1", shifted: true},
		{key: "\"", qcode: "apostrophe", shifted: true},
		{key: "", fails: true}, {key: "F13", fails: true}, {key: "MediaPlay", fails: true},
	}
	for _, c := range cases {
		qcode, shifted, err := qcodeName(c.key)
		if c.fails {
			if err == nil {
				t.Errorf("qcodeName(%q) = %q, want error", c.key, qcode)
			}
			continue
		}
		if err != nil {
			t.Errorf("qcodeName(%q) error = %v", c.key, err)
			continue
		}
		if qcode != c.qcode || shifted != c.shifted {
			t.Errorf("qcodeName(%q) = (%q, %v), want (%q, %v)", c.key, qcode, shifted, c.qcode, c.shifted)
		}
	}
}

func TestEnsureMonitorPropagatesHostError(t *testing.T) {
	host := &fakeHost{err: errors.New("ssh refused")}
	monitor := NewMonitor(host)

	if _, err := monitor.EnsureMonitor(context.Background(), Guest{Cluster: "c63", Instance: "x"}); err == nil {
		t.Fatal("EnsureMonitor() expected an error when the host command fails")
	}
}

func TestSendKeyPropagatesHostError(t *testing.T) {
	host := &fakeHost{err: errors.New("socket gone")}
	monitor := NewMonitor(host)

	err := monitor.SendKey(context.Background(), Guest{Cluster: "c63", Instance: "x"}, KeyEvent{Key: "Enter"})
	if err == nil || !strings.Contains(err.Error(), "socket gone") {
		t.Fatalf("SendKey() error = %v, want wrapped host error", err)
	}
}

func TestSendPointerPropagatesHostError(t *testing.T) {
	host := &fakeHost{err: errors.New("socket gone")}
	monitor := NewMonitor(host)

	err := monitor.SendPointer(context.Background(), Guest{Cluster: "c63", Instance: "x"},
		PointerEvent{X: 0.5, Y: 0.5, Move: true})
	if err == nil || !strings.Contains(err.Error(), "socket gone") {
		t.Fatalf("SendPointer() error = %v, want wrapped host error", err)
	}
}

func TestSendPointerClampsCoordinates(t *testing.T) {
	host := &fakeHost{}
	monitor := NewMonitor(host)

	// 0 and 1 are the axis extremes; the clamping keeps malformed floats inside
	// the tablet range.
	if err := monitor.SendPointer(context.Background(), Guest{Cluster: "c63", Instance: "x"},
		PointerEvent{X: 0, Y: 1, Move: true}); err != nil {
		t.Fatalf("SendPointer() error = %v", err)
	}

	events, err := inputPayload(host.script)
	if err != nil {
		t.Fatalf("input payload malformed: %v", err)
	}
	x := events[0]["data"].(map[string]any)["value"].(float64)
	y := events[1]["data"].(map[string]any)["value"].(float64)
	if x != 0 || y != float64(absAxisMax) {
		t.Fatalf("clamped values = %v/%v, want 0/%d", x, y, absAxisMax)
	}
}

func TestModifierQcode(t *testing.T) {
	cases := []struct {
		name  string
		qcode string
		fails bool
	}{
		{name: "Control", qcode: "ctrl"},
		{name: "AltRight", qcode: "alt_r"},
		{name: "ShiftRight", qcode: "shift_r"},
		{name: "Meta", qcode: "meta_l"},
		{name: "MetaRight", qcode: "meta_r"},
		{name: "Cmd", qcode: "meta_l"},
		{name: "Hyper", fails: true},
	}
	for _, c := range cases {
		qcode, err := modifierQcode(c.name)
		if c.fails {
			if err == nil {
				t.Errorf("modifierQcode(%q) = %q, want error", c.name, qcode)
			}
			continue
		}
		if err != nil || qcode != c.qcode {
			t.Errorf("modifierQcode(%q) = (%q, %v), want %q", c.name, qcode, err, c.qcode)
		}
	}
}

func TestSendKeyRejectsUnknownModifier(t *testing.T) {
	monitor := NewMonitor(&fakeHost{})

	if err := monitor.SendKey(context.Background(), Guest{},
		KeyEvent{Key: "a", Modifiers: []string{"Hyper"}}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("SendKey() error = %v, want ErrUnavailable", err)
	}
}

func TestAbsQMPEventClampsOutOfRangeValues(t *testing.T) {
	// Defensive clamp: callers outside SendPointer may pass raw fractions.
	low := absQMPEvent("x", -1)
	high := absQMPEvent("y", 2)
	mid := absQMPEvent("x", 0.5)

	if got := low["data"].(map[string]any)["value"].(int); got != 0 {
		t.Fatalf("negative clamp = %d, want 0", got)
	}
	if got := high["data"].(map[string]any)["value"].(int); got != absAxisMax {
		t.Fatalf("upper clamp = %d, want %d", got, absAxisMax)
	}
	if got := mid["data"].(map[string]any)["value"].(int); got != 16384 {
		t.Fatalf("mid value = %d, want 16384", got)
	}
}
