// Package console implements a graphical console for virtual machines without
// relying on a VNC backend.
//
// Incus ships QEMU builds that expose neither the VNC display backend nor a
// usable SPICE handshake, so the browser cannot attach a normal remote desktop.
// What is available is QEMU's own QMP monitor: it can dump the framebuffer to
// an image and inject keyboard/pointer events. This package attaches a second
// QMP monitor socket through raw.qemu.conf and drives it over the host's SSH
// path, turning it into a screenshot + input console.
package console

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// qmpProbeCode checks whether the monitor socket answers a QMP handshake. It
// runs on the cluster member so no socket path crosses the network.
const qmpProbeCode = `import socket,sys
try:
 s=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM)
 s.settimeout(1)
 s.connect(sys.argv[1])
 s.recv(4096)
 s.sendall(b'{"execute":"qmp_capabilities"}\n')
 r=s.recv(4096)
 print("ok" if b"return" in r else "bad")
except Exception:
 print("bad")`

// ErrUnavailable reports that the guest has no graphical console to drive.
var ErrUnavailable = errors.New("console: graphical console is unavailable")

// Guest identifies the instance whose console is being driven.
type Guest struct {
	Cluster  string
	Instance string
	Member   string
}

// HostCommand runs one command on a cluster member and returns its stdout.
// The nodeaccess SSH backend satisfies this shape.
type HostCommand interface {
	Run(ctx context.Context, command string) (string, error)
}

// KeyEvent is one keyboard press in the terms the browser understands.
//
// Key uses the browser's KeyboardEvent.key value ("a", "Enter", "ArrowLeft").
// Modifiers are separate so a chord like Ctrl+Alt+F1 can be expressed.
type KeyEvent struct {
	Key       string   `json:"key"`
	Modifiers []string `json:"modifiers,omitempty"`
	Repeat    int      `json:"repeat,omitempty"`
}

// PointerEvent is one absolute pointer motion or button transition. X and Y are
// normalised to 0..1 so the browser can send canvas-relative coordinates without
// knowing the guest resolution.
type PointerEvent struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Button string  `json:"button,omitempty"` // left, middle, right, wheel-up, wheel-down
	Down   bool    `json:"down,omitempty"`
	Move   bool    `json:"move,omitempty"`
}

// Monitor drives one guest's QEMU monitor over the host.
type Monitor struct {
	host HostCommand
}

// NewMonitor builds a monitor driver over the given host command runner.
func NewMonitor(host HostCommand) *Monitor {
	return &Monitor{host: host}
}

// qmpSocketPath is the QMP monitor socket the fleet manages. It lives in /tmp so
// the QEMU process (running as the unprivileged incus user) can bind it given
// the matching security.apparmor rule.
func qmpSocketPath(instance string) string {
	return "/tmp/fleet-qmp-" + instance + ".sock"
}

// ScreenshotPath is where QEMU writes the framebuffer dump. Incus grants the
// QEMU profile write access only to /tmp/incus_screenshot_<instance>, so the dump
// must land there before being shipped back.
func ScreenshotPath(instance string) string {
	return "/tmp/incus_screenshot_" + instance
}

// MonitorConfig is the QEMU configuration that exposes the QMP monitor used by
// the web desktop. It is installed when a VM is created so opening the desktop
// does not have to restart the guest.
func MonitorConfig(instance string) string {
	// QEMU's config parser expects double-quoted strings; single quotes are
	// taken literally and break both the path and the section keys.
	// mode = "control" is the same wiring as -qmp: a QMP monitor.
	return fmt.Sprintf(`[chardev "fleetmon"]
backend = "socket"
path = "%s"
server = "on"
wait = "off"

[mon "fleetmon"]
chardev = "fleetmon"
mode = "control"
`, qmpSocketPath(instance))
}

// MonitorAppArmor grants QEMU access to the monitor socket and screenshot file.
// AppArmor rules are plain paths, not shell words: interposing quotes here
// makes apparmor_parser reject the generated profile.
func MonitorAppArmor(instance string) string {
	return fmt.Sprintf("%s mrw,\n%s mrw,", qmpSocketPath(instance), ScreenshotPath(instance))
}

// EnsureMonitor installs the QEMU configuration that exposes a QMP socket and
// relaxes AppArmor so QEMU may bind it. It is idempotent: a live monitor is left
// untouched, and a running VM is only restarted when the configuration changes
// because QEMU reads raw.qemu.conf at start.
func (m *Monitor) EnsureMonitor(ctx context.Context, guest Guest) (bool, error) {
	conf := MonitorConfig(guest.Instance)
	apparmor := MonitorAppArmor(guest.Instance)

	script := fmt.Sprintf(`set -eu
want_conf=%s
want_apparmor=%s
# Command substitution strips trailing newlines from what incus returns, so the
# wanted values go through printf to make the comparison newline-insensitive;
# otherwise a stored value always looks changed and every prepare restarts a
# running VM.
want_conf=$(printf '%%s' "$want_conf")
want_apparmor=$(printf '%%s' "$want_apparmor")
current_conf=$(incus config get %s raw.qemu.conf 2>/dev/null || true)
current_apparmor=$(incus config get %s raw.apparmor 2>/dev/null || true)
probe=$(python3 -c %s %s 2>/dev/null || true)
if [ "$probe" = "ok" ]; then
  # A live QMP monitor means the framebuffer is already available; rewriting
  # config here would needlessly power cycle the guest on every desktop open.
  echo "changed=0"
  exit 0
fi
conf_diff=0
aa_diff=0
was_running=0
changed=0
if [ "$current_conf" != "$want_conf" ]; then
  conf_diff=1
fi
if [ "$current_apparmor" != "$want_apparmor" ]; then
  aa_diff=1
fi
if [ "$conf_diff" -eq 1 ] || [ "$aa_diff" -eq 1 ]; then
  changed=1
  running=$(incus list %s --format csv -c s 2>/dev/null || true)
  if [ "$running" = "RUNNING" ]; then
    was_running=1
    incus stop %s --force >/dev/null
  fi
fi
if [ "$aa_diff" -eq 1 ]; then
  incus config set %s "raw.apparmor=$want_apparmor"
fi
if [ "$conf_diff" -eq 1 ]; then
  incus config set %s "raw.qemu.conf=$want_conf"
fi
if [ "$was_running" -eq 1 ]; then
  incus start %s >/dev/null
fi
echo "changed=$changed"
`,
		shellQuote(conf), shellQuote(apparmor),
		shellQuote(guest.Instance), shellQuote(guest.Instance),
		shellQuote(qmpProbeCode), shellQuote(qmpSocketPath(guest.Instance)),
		shellQuote(guest.Instance), shellQuote(guest.Instance), shellQuote(guest.Instance),
		shellQuote(guest.Instance), shellQuote(guest.Instance),
	)

	out, err := m.host.Run(ctx, script)
	if err != nil {
		return false, fmt.Errorf("console: prepare monitor for %s:%s: %w", guest.Cluster, guest.Instance, err)
	}

	return strings.Contains(out, "changed=1"), nil
}

// Screenshot asks QEMU to dump the framebuffer and returns PNG bytes.
//
// QEMU 7.1+ can write PNG directly; older builds only write PPM, which is
// converted on the host inline so only the compressed frame crosses SSH. The
// dump lands in the AppArmor-granted scratch path, never in a shared directory.
func (m *Monitor) Screenshot(ctx context.Context, guest Guest) ([]byte, error) {
	// The Python payload contains printf-style percent signs, so the script is
	// built with string replacement instead of Go's formatting verbs.
	script := screenshotScript
	for placeholder, value := range map[string]string{
		"__SOCKET__": shellQuote(qmpSocketPath(guest.Instance)),
		"__PNG__":    shellQuote(ScreenshotPath(guest.Instance) + ".png"),
		"__PPM__":    shellQuote(ScreenshotPath(guest.Instance)),
		"__RAWPPM__": ScreenshotPath(guest.Instance),
		"__RAWPNG__": ScreenshotPath(guest.Instance) + ".png",
	} {
		script = strings.ReplaceAll(script, placeholder, value)
	}

	out, err := m.host.Run(ctx, script)
	if err != nil {
		return nil, fmt.Errorf("console: screendump %s:%s: %w", guest.Cluster, guest.Instance, err)
	}

	encoded := strings.TrimSpace(out)
	// Tolerate incidental banners from the login shell.
	if idx := strings.Index(encoded, "iVBOR"); idx > 0 {
		encoded = encoded[idx:]
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("console: decode screenshot: %w", err)
	}

	return decoded, nil
}

// screenshotScript performs a QMP screendump and returns the PNG as base64.
//
// PNG output is requested first; when the guest's QEMU predates 7.1 and only
// speaks PPM, the error response triggers the inline PPM-to-PNG conversion, so
// both QEMU generations are served by one script.
const screenshotScript = `set -eu
rm -f __RAWPPM__ __RAWPNG__
python3 - __SOCKET__ __PPM__ __PNG__ <<'PY'
import base64, json, socket, struct, sys, zlib

sock, ppm, png = sys.argv[1], sys.argv[2], sys.argv[3]
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.settimeout(10)
s.connect(sock)
f = s.makefile("rw")

def readmsg():
    while True:
        line = f.readline()
        if not line:
            raise SystemExit("monitor closed before response")
        try:
            return json.loads(line)
        except ValueError:
            continue

def cmd(ident, execute, args=None):
    req = {"execute": execute, "id": ident}
    if args is not None:
        req["arguments"] = args
    f.write(json.dumps(req) + "\n")
    f.flush()
    while True:
        msg = readmsg()
        if msg.get("id") == ident:
            if "error" in msg:
                raise SystemExit(msg["error"].get("desc", json.dumps(msg["error"])))
            return msg.get("return")

greet = readmsg()
if "QMP" not in greet:
    raise SystemExit("not a QMP monitor: " + json.dumps(greet))
cmd("cap", "qmp_capabilities")

try:
    cmd("sd", "screendump", {"filename": png, "format": "png"})
except SystemExit as first:
    # Older QEMU: no png screendump. Fall back to PPM and convert inline.
    try:
        cmd("sd2", "screendump", {"filename": ppm})
    except SystemExit:
        raise first from None
    data = open(ppm, "rb").read()
    pos = 0
    def token():
        global pos
        while data[pos:pos+1].isspace():
            pos += 1
        if data[pos:pos+1] == b"#":
            while data[pos:pos+1] not in (b"\n", b""):
                pos += 1
            return token()
        start = pos
        while not data[pos:pos+1].isspace():
            pos += 1
        return data[start:pos]
    if token() != b"P6":
        sys.exit("unsupported ppm magic")
    width = int(token()); height = int(token()); maxval = int(token())
    if maxval != 255:
        sys.exit("unsupported ppm maxval")
    pos += 1
    raw = data[pos:pos + width * height * 3]
    if len(raw) != width * height * 3:
        sys.exit("short ppm payload")
    rows = b"".join(b"\x00" + raw[y * width * 3:(y + 1) * width * 3] for y in range(height))
    def chunk(kind, payload):
        return (struct.pack(">I", len(payload)) + kind + payload +
                struct.pack(">I", zlib.crc32(kind + payload) & 0xffffffff))
    out = (b"\x89PNG\r\n\x1a\n" +
           chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)) +
           chunk(b"IDAT", zlib.compress(rows, 6)) +
           chunk(b"IEND", b""))
    open(png, "wb").write(out)

sys.stdout.write(base64.b64encode(open(png, "rb").read()).decode())
PY
`

// SendKey injects one key press (press + release) through QMP.
//
// Modifiers are sent as held keys around the main key, so Ctrl+Alt+F1 becomes
// down(ctrl) down(alt) down(f1) up(f1) up(alt) up(ctrl).
func (m *Monitor) SendKey(ctx context.Context, guest Guest, event KeyEvent) error {
	events, err := keyEvents(event)
	if err != nil {
		return err
	}

	if err := m.sendQMP(ctx, guest, events); err != nil {
		return fmt.Errorf("console: send key %q: %w", event.Key, err)
	}

	return nil
}

// keyEvents translates one browser key event into QMP input events.
func keyEvents(event KeyEvent) ([]map[string]any, error) {
	if event.Key == "" {
		return nil, fmt.Errorf("%w: key is required", ErrUnavailable)
	}

	// Resolve the main key plus any implicit shift the symbol requires.
	main, shifted, err := qcodeName(event.Key)
	if err != nil {
		return nil, err
	}
	if shifted {
		event.Modifiers = append([]string{"Shift"}, event.Modifiers...)
	}

	modifiers := make([]string, 0, len(event.Modifiers))
	for _, mod := range event.Modifiers {
		name, err := modifierQcode(mod)
		if err != nil {
			return nil, err
		}
		modifiers = append(modifiers, name)
	}

	repeat := event.Repeat
	if repeat <= 0 {
		repeat = 1
	}

	var events []map[string]any
	for i := 0; i < repeat; i++ {
		// Chord: hold modifiers, tap the main key, release in reverse.
		for _, mod := range modifiers {
			events = append(events, keyQMPEvent(mod, true))
		}
		events = append(events, keyQMPEvent(main, true), keyQMPEvent(main, false))
		for j := len(modifiers) - 1; j >= 0; j-- {
			events = append(events, keyQMPEvent(modifiers[j], false))
		}
	}

	return events, nil
}

// keyQMPEvent builds one QMP key transition for a qcode.
func keyQMPEvent(qcode string, down bool) map[string]any {
	return map[string]any{
		"type": "key",
		"data": map[string]any{
			"down": down,
			"key":  map[string]any{"type": "qcode", "data": qcode},
		},
	}
}

// SendPointer injects an absolute pointer event. The usb-tablet QEMU emulates
// has a 0..0x7fff axis range, so normalised coordinates scale directly.
func (m *Monitor) SendPointer(ctx context.Context, guest Guest, event PointerEvent) error {
	if event.X < 0 || event.X > 1 || event.Y < 0 || event.Y > 1 {
		return fmt.Errorf("%w: pointer coordinates must be normalised 0..1", ErrUnavailable)
	}
	if event.Button != "" {
		switch event.Button {
		case "left", "middle", "right", "wheel-up", "wheel-down", "side", "extra", "wheel-left", "wheel-right":
		default:
			return fmt.Errorf("%w: unsupported pointer button %q", ErrUnavailable, event.Button)
		}
	}

	var events []map[string]any
	if event.Move || event.Button == "" || event.Down {
		events = append(events,
			absQMPEvent("x", event.X),
			absQMPEvent("y", event.Y),
		)
	}
	if event.Button != "" && !event.Move {
		events = append(events, btnQMPEvent(event.Button, event.Down))
	}
	if len(events) == 0 {
		return fmt.Errorf("%w: empty pointer event", ErrUnavailable)
	}

	if err := m.sendQMP(ctx, guest, events); err != nil {
		return fmt.Errorf("console: send pointer: %w", err)
	}

	return nil
}

// absQMPEvent builds one absolute-axis event; QMP moves need one per axis.
func absQMPEvent(axis string, normalised float64) map[string]any {
	value := int(normalised*float64(absAxisMax) + 0.5)
	if value < 0 {
		value = 0
	}
	if value > absAxisMax {
		value = absAxisMax
	}
	return map[string]any{
		"type": "abs",
		"data": map[string]any{"axis": axis, "value": value},
	}
}

// btnQMPEvent builds one button transition. Wheel events only exist as
// transitions, which matches how browsers report scrolling.
func btnQMPEvent(button string, down bool) map[string]any {
	return map[string]any{
		"type": "btn",
		"data": map[string]any{"button": button, "down": down},
	}
}

// absAxisMax is the axis range QEMU's emulated tablet exposes.
const absAxisMax = 0x7fff

// sendQMP ships one input-send-event command to the guest's QMP monitor.
// Event construction happens in SendKey/SendPointer, which validate their input.
func (m *Monitor) sendQMP(ctx context.Context, guest Guest, events []map[string]any) error {
	payload, err := json.Marshal(map[string]any{"events": events})
	if err != nil {
		return fmt.Errorf("console: encode input event: %w", err)
	}

	// The Python body contains percent-free JSON only, but the socket path and
	// payload still need quoting for the remote shell.
	script := strings.ReplaceAll(inputScript, "__SOCKET__", shellQuote(qmpSocketPath(guest.Instance)))
	script = strings.ReplaceAll(script, "__PAYLOAD__", shellQuote(string(payload)))

	if _, err := m.host.Run(ctx, script); err != nil {
		return err
	}

	return nil
}

// inputScript ships one input-send-event command to the guest's QMP monitor.
// The payload arrives as a shell-quoted JSON argument so it never passes
// through a shell pipeline.
const inputScript = `set -eu
python3 - __SOCKET__ __PAYLOAD__ <<'PY'
import json, socket, sys

sock, payload = sys.argv[1], sys.argv[2]
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.settimeout(10)
s.connect(sock)
f = s.makefile("rw")

def readmsg():
    while True:
        line = f.readline()
        if not line:
            raise SystemExit("monitor closed before response")
        try:
            return json.loads(line)
        except ValueError:
            continue

def cmd(ident, execute, args=None):
    req = {"execute": execute, "id": ident}
    if args is not None:
        req["arguments"] = args
    f.write(json.dumps(req) + "\n")
    f.flush()
    while True:
        msg = readmsg()
        if msg.get("id") == ident:
            if "error" in msg:
                raise SystemExit(msg["error"].get("desc", json.dumps(msg["error"])))
            return msg.get("return")

greet = readmsg()
if "QMP" not in greet:
    raise SystemExit("not a QMP monitor: " + json.dumps(greet))
cmd("cap", "qmp_capabilities")
cmd("ev", "input-send-event", json.loads(payload))
PY
`

// namedKeys maps browser KeyboardEvent.key values onto QEMU qcodes.
var namedKeys = map[string]string{
	"Enter": "ret", "Escape": "esc", "Backspace": "backspace",
	"Tab": "tab", " ": "spc", "Delete": "delete", "Insert": "insert",
	"Home": "home", "End": "end", "PageUp": "pgup", "PageDown": "pgdn",
	"ArrowUp": "up", "ArrowDown": "down", "ArrowLeft": "left", "ArrowRight": "right",
	"CapsLock": "caps_lock", "PrintScreen": "print", "Pause": "stop",
	"F1": "f1", "F2": "f2", "F3": "f3", "F4": "f4", "F5": "f5", "F6": "f6",
	"F7": "f7", "F8": "f8", "F9": "f9", "F10": "f10", "F11": "f11", "F12": "f12",
}

// modifierQcode maps a browser modifier name onto its qcode.
func modifierQcode(name string) (string, error) {
	switch name {
	case "Control", "Ctrl":
		return "ctrl", nil
	case "Alt", "AltLeft":
		return "alt", nil
	case "AltRight":
		return "alt_r", nil
	case "Shift", "ShiftLeft":
		return "shift", nil
	case "ShiftRight":
		return "shift_r", nil
	case "Meta", "MetaLeft", "Super", "Cmd", "Command":
		return "meta_l", nil
	case "MetaRight", "SuperRight":
		return "meta_r", nil
	default:
		return "", fmt.Errorf("%w: unsupported modifier %q", ErrUnavailable, name)
	}
}

// qcodeName maps one browser key value onto a qcode, reporting whether the
// symbol needs Shift held (punctuation, capital letters).
func qcodeName(key string) (string, bool, error) {
	if key == "" {
		return "", false, fmt.Errorf("%w: key is required", ErrUnavailable)
	}

	// Single characters: letters and digits map directly; punctuation needs the
	// shift-chord treatment except the unshifted symbols below.
	if len(key) == 1 {
		if name, ok := namedKeys[key]; ok {
			return name, false, nil
		}
		c := key[0]
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'z' {
			return string(c), false, nil
		}
		if c >= 'A' && c <= 'Z' {
			return strings.ToLower(key), true, nil
		}
		switch key {
		case "-", "=", "[", "]", ";", "'", ",", ".", "/", "\\", "`":
			return unshiftedSymbols[key], false, nil
		}
		if chord, ok := shiftedSymbols[key]; ok {
			return chord, true, nil
		}
		return "", false, fmt.Errorf("%w: unsupported key %q", ErrUnavailable, key)
	}

	if name, ok := namedKeys[key]; ok {
		return name, false, nil
	}

	return "", false, fmt.Errorf("%w: unsupported key %q", ErrUnavailable, key)
}

// unshiftedSymbols are punctuation keys QEMU names directly.
var unshiftedSymbols = map[string]string{
	"-": "minus", "=": "equal", "[": "bracket_left", "]": "bracket_right",
	";": "semicolon", "'": "apostrophe", ",": "comma", ".": "dot",
	"/": "slash", "\\": "backslash", "`": "grave_accent",
}

// shiftedSymbols are symbols produced with Shift; the caller holds shift and
// taps the base key.
var shiftedSymbols = map[string]string{
	"!": "1", "@": "2", "#": "3", "$": "4", "%": "5", "^": "6",
	"&": "7", "*": "8", "(": "9", ")": "0",
	"_": "minus", "+": "equal", "{": "bracket_left", "}": "bracket_right",
	"|": "backslash", ":": "semicolon", "\"": "apostrophe",
	"<": "comma", ">": "dot", "?": "slash", "~": "grave_accent",
}

// shellQuote wraps a value in single quotes for safe interpolation into a shell
// script sent over SSH.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
