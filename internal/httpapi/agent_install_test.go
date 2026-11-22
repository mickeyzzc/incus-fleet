package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/agentinstall"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestAgentInstallRoutes(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "agent")
	helperPath := filepath.Join(dir, "helper")
	if err := os.WriteFile(agentPath, []byte("agent"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helperPath, []byte("helper"), 0o600); err != nil {
		t.Fatal(err)
	}
	install, err := agentinstall.New(agentinstall.Config{
		GatewayURL:   "https://fleet.example:8444",
		AgentBinary:  agentPath,
		HelperBinary: helperPath,
	}, []byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	f.handler = New(Deps{Auth: f.auth, Version: "test", AgentInstall: install}).Handler()

	rec := f.request(t, http.MethodGet, "/api/v1/agent/install.sh", nil, nil, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "CENTER_URL='https://fleet.example:8444'") {
		t.Fatalf("install.sh = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = f.request(t, http.MethodGet, "/api/v1/agent/install/agent", nil, nil, "")
	if rec.Code != http.StatusOK || rec.Body.String() != "agent" {
		t.Fatalf("agent asset = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Checksum-Sha256") == "" {
		t.Fatal("agent checksum header is empty")
	}
}
