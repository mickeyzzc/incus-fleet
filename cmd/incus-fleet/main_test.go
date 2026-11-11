package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestConfig(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	cert := filepath.Join(dir, "client.crt")
	key := filepath.Join(dir, "client.key")
	if err := os.WriteFile(cert, []byte("cert"), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(key, []byte("key"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	path := filepath.Join(dir, "config.yaml")
	content := `
server:
  listen: 127.0.0.1:8080
  auth_token: secret
database:
  driver: sqlite
  dsn: ` + filepath.Join(dir, "fleet.db") + `
clusters:
  - name: c1
    endpoint: https://192.0.2.10:8443
    tls_cert: ` + cert + `
    tls_key: ` + key + `
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run() code = %d stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "incus-fleet") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunCheck(t *testing.T) {
	path := writeTestConfig(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-config", path, "-check"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run() code = %d stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "configuration valid: 1 cluster(s)") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-unknown"}, &stdout, &stderr); code != 2 {
		t.Fatalf("bad flag code = %d", code)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"-config", filepath.Join(t.TempDir(), "missing.yaml")}, &stdout, &stderr); code != 1 {
		t.Fatalf("missing config code = %d", code)
	}
	if stderr.Len() == 0 {
		t.Fatal("missing config produced no error")
	}
}

func TestParseLevel(t *testing.T) {
	tests := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
		"info":  slog.LevelInfo,
		"other": slog.LevelInfo,
	}

	for input, want := range tests {
		if got := parseLevel(input); got != want {
			t.Fatalf("parseLevel(%q) = %v, want %v", input, got, want)
		}
	}
}
