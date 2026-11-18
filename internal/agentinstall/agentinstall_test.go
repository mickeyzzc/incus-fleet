package agentinstall

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const testCA = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"

func newTestHandler(t *testing.T) (*Handler, string, string) {
	t.Helper()
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "agent")
	helperPath := filepath.Join(dir, "helper")
	if err := os.WriteFile(agentPath, []byte("agent-binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helperPath, []byte("helper-binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := New(Config{
		GatewayURL:   "https://fleet.example:8444/",
		AgentBinary:  agentPath,
		HelperBinary: helperPath,
	}, []byte(testCA), nil)
	if err != nil {
		t.Fatal(err)
	}

	return handler, agentPath, helperPath
}

func TestInstallerScriptAndAssets(t *testing.T) {
	handler, _, _ := newTestHandler(t)
	mux := http.NewServeMux()
	handler.Register(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/install.sh", nil)
	req.Host = "fleet.example"
	req.Header.Set("X-Forwarded-Proto", "https")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("install.sh status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"ASSET_URL='https://fleet.example'",
		"CENTER_URL='https://fleet.example:8444'",
		"sha256sum -c",
		"--token TOKEN --cluster NAME --member NAME",
		testCA,
		"incus-fleet-agent ALL=(root) NOPASSWD: /usr/libexec/incus-fleet-helper",
		"ExecStart=/usr/local/bin/incus-fleet-agent",
		"runuser -u incus-fleet-agent",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("install.sh missing %q\n%s", want, body)
		}
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/x-shellscript") {
		t.Fatalf("content type = %q", got)
	}
	if strings.Contains(body, "{{") {
		t.Fatal("installer contains an unrendered template action")
	}
	cmd := exec.Command("sh", "-n")
	cmd.Stdin = strings.NewReader(body)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("installer shell syntax: %v\n%s", err, output)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/agent/install/agent", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "agent-binary" {
		t.Fatalf("agent asset = %d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Checksum-Sha256") != handler.agent.sha256 {
		t.Fatalf("agent checksum = %q", rec.Header().Get("X-Checksum-Sha256"))
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/agent/install/ca.crt", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != testCA {
		t.Fatalf("ca asset = %d %q", rec.Code, rec.Body.String())
	}
}

func TestInstallerUsesExplicitAssetURLAndForwardedHost(t *testing.T) {
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "agent")
	helperPath := filepath.Join(dir, "helper")
	if err := os.WriteFile(agentPath, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helperPath, []byte("h"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := New(Config{
		GatewayURL:   "https://gw.example:8444",
		AssetURL:     "https://fleet.example/base/",
		AgentBinary:  agentPath,
		HelperBinary: helperPath,
	}, []byte(testCA), nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/install.sh", nil)
	req.Host = "internal:8080"
	req.Header.Set("X-Forwarded-Host", "public.example, proxy")
	handler.installScript(rec, req)
	if !strings.Contains(rec.Body.String(), "ASSET_URL='https://fleet.example/base'") {
		t.Fatalf("explicit asset URL not used: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/agent/install.sh", nil)
	req.Host = "internal:8080"
	req.Header.Set("X-Forwarded-Host", "public.example, proxy")
	req.Header.Set("X-Forwarded-Proto", "https, http")
	rec = httptest.NewRecorder()
	handler.cfg.AssetURL = ""
	handler.installScript(rec, req)
	if !strings.Contains(rec.Body.String(), "ASSET_URL='https://public.example'") {
		t.Fatalf("forwarded URL not used: %s", rec.Body.String())
	}
}

func TestInstallerRejectsInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "agent")
	helperPath := filepath.Join(dir, "helper")
	if err := os.WriteFile(agentPath, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helperPath, []byte("h"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []Config{
		{AgentBinary: agentPath, HelperBinary: helperPath},
		{GatewayURL: "http://fleet.example", AgentBinary: agentPath, HelperBinary: helperPath},
		{GatewayURL: "https://fleet.example", HelperBinary: helperPath},
		{GatewayURL: "https://fleet.example", AgentBinary: agentPath},
		{GatewayURL: "https://fleet.example", AgentBinary: dir, HelperBinary: helperPath},
		{GatewayURL: "https://fleet.example", AgentBinary: agentPath, HelperBinary: helperPath, AssetURL: "ftp://fleet.example"},
	}
	for _, cfg := range cases {
		if _, err := New(cfg, []byte(testCA), nil); err == nil {
			t.Fatalf("New(%#v) expected error", cfg)
		}
	}
	if _, err := New(Config{GatewayURL: "https://fleet.example", AgentBinary: agentPath, HelperBinary: helperPath}, nil, nil); err == nil {
		t.Fatal("empty CA expected error")
	}
	if _, err := New(Config{GatewayURL: "https://fleet.example", AgentBinary: filepath.Join(dir, "missing"), HelperBinary: helperPath}, []byte(testCA), nil); err == nil {
		t.Fatal("missing agent binary expected error")
	}
	if _, err := New(Config{
		GatewayURL:     "https://fleet.example",
		AgentBinaries:  map[string]string{"amd64": agentPath, "arm64": agentPath},
		HelperBinaries: map[string]string{"amd64": helperPath},
	}, []byte(testCA), nil); err == nil {
		t.Fatal("multiarch mismatch expected error")
	}
	if _, err := New(Config{
		GatewayURL:     "https://fleet.example",
		AgentBinaries:  map[string]string{"amd64": agentPath},
		HelperBinaries: map[string]string{"arm64": helperPath},
	}, []byte(testCA), nil); err == nil {
		t.Fatal("missing helper arch expected error")
	}
}

func TestInstallerRequestHelpers(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := requestBaseURL(req); !strings.HasPrefix(got, "http://") {
		t.Fatalf("requestBaseURL = %q", got)
	}
	if got := shQuote("a'b"); got != `'a'\''b'` {
		t.Fatalf("shQuote = %q", got)
	}
	if got := modTime(filepath.Join(t.TempDir(), "missing")); !got.IsZero() {
		t.Fatalf("modTime(missing) = %v", got)
	}
}

func TestEmbeddedAssetsMatchDeploy(t *testing.T) {
	sudoers, err := os.ReadFile(filepath.Join("..", "..", "deploy", "agent-sudoers"))
	if err != nil {
		t.Fatal(err)
	}
	if string(sudoers) != sudoersAsset {
		t.Fatal("embedded agent-sudoers differs from deploy/agent-sudoers")
	}
	unit, err := os.ReadFile(filepath.Join("..", "..", "deploy", "systemd", "incus-fleet-agent.service"))
	if err != nil {
		t.Fatal(err)
	}
	if string(unit) != unitAsset {
		t.Fatal("embedded systemd unit differs from deploy/systemd/incus-fleet-agent.service")
	}
}

func TestServeAssetUnavailable(t *testing.T) {
	handler := &Handler{agent: asset{path: filepath.Join(t.TempDir(), "missing")}}
	rec := httptest.NewRecorder()
	handler.serveAgent(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing asset status = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = ""
	handler.installScript(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing request host status = %d", rec.Code)
	}
}

func TestServeAssetContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent")
	if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := New(Config{
		GatewayURL:   "https://fleet.example",
		AgentBinary:  path,
		HelperBinary: path,
	}, []byte(testCA), nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/install/helper", nil)
	handler.serveHelper(rec, req)
	body, _ := io.ReadAll(rec.Result().Body)
	if string(body) != "payload" || rec.Code != http.StatusOK {
		t.Fatalf("helper = %d %q", rec.Code, body)
	}
}

func TestMultiArchInstaller(t *testing.T) {
	dir := t.TempDir()
	amdAgent := filepath.Join(dir, "agent-amd64")
	armAgent := filepath.Join(dir, "agent-arm64")
	amdHelper := filepath.Join(dir, "helper-amd64")
	armHelper := filepath.Join(dir, "helper-arm64")
	for path, data := range map[string]string{
		amdAgent: "amd-agent", armAgent: "arm-agent", amdHelper: "amd-helper", armHelper: "arm-helper",
	} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	handler, err := New(Config{
		GatewayURL:     "https://fleet.example:8444",
		AgentBinaries:  map[string]string{"amd64": amdAgent, "arm64": armAgent},
		HelperBinaries: map[string]string{"amd64": amdHelper, "arm64": armHelper},
	}, []byte(testCA), nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/install.sh", nil)
	req.Host = "fleet.example"
	handler.installScript(rec, req)
	body := rec.Body.String()
	for _, want := range []string{"amd64) AGENT_SHA=", "arm64) AGENT_SHA=", "?arch=$ARCH"} {
		if !strings.Contains(body, want) {
			t.Fatalf("multiarch script missing %q", want)
		}
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/agent/install/agent?arch=arm64", nil)
	handler.serveAgent(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "arm-agent" {
		t.Fatalf("arm agent = %d %q", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/agent/install/agent?arch=riscv64", nil)
	handler.serveAgent(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unsupported arch = %d", rec.Code)
	}
}
