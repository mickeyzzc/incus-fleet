package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/agentgw"
	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()

	return &config.Config{
		Server: config.Server{
			Listen:    "127.0.0.1:0",
			AuthToken: "secret",
			LogLevel:  "debug",
		},
		Database: config.Database{
			Driver: "sqlite",
			DSN:    filepath.Join(t.TempDir(), "fleet.db"),
		},
		Clusters: []config.Cluster{
			{Name: "c1", DisplayName: "c1", Endpoint: "https://192.0.2.10:8443"},
			{Name: "c2", DisplayName: "c2", Endpoint: "https://198.51.100.11:8443"},
		},
	}
}

func testDialer(source, target *incusfake.Server) clusters.DialFunc {
	return func(_ context.Context, cfg config.Cluster) (clusters.InstanceServer, error) {
		if cfg.Name == "c1" {
			return source, nil
		}

		return target, nil
	}
}

func staticFS() fstest.MapFS {
	return fstest.MapFS{"dist/index.html": {Data: []byte("<html>fleet</html>")}}
}

func TestNewRequiresConfig(t *testing.T) {
	if _, err := New(context.Background(), nil, testLogger()); err == nil {
		t.Fatal("New() expected error")
	}
}

func TestNewAndHandler(t *testing.T) {
	cfg := testConfig(t)
	application, err := New(context.Background(), cfg, testLogger(),
		WithDialer(testDialer(&incusfake.Server{}, &incusfake.Server{})),
		WithStaticFS(staticFS()),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	handler := application.Handler()
	if handler == nil {
		t.Fatal("Handler() is nil")
	}
}

func TestNewStoreError(t *testing.T) {
	cfg := testConfig(t)
	cfg.Database.Driver = "mysql"

	if _, err := New(context.Background(), cfg, testLogger(), WithDialer(testDialer(&incusfake.Server{}, &incusfake.Server{}))); err == nil {
		t.Fatal("New() expected store error")
	}
}

func TestNewOfflineCluster(t *testing.T) {
	cfg := testConfig(t)
	application, err := New(context.Background(), cfg, testLogger(),
		WithDialer(func(_ context.Context, _ config.Cluster) (clusters.InstanceServer, error) {
			return nil, context.DeadlineExceeded
		}),
		WithStaticFS(staticFS()),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })
}

func TestRunGracefulShutdown(t *testing.T) {
	cfg := testConfig(t)
	cfg.Server.Listen = freeAddress(t)

	application, err := New(context.Background(), cfg, testLogger(),
		WithDialer(testDialer(&incusfake.Server{}, &incusfake.Server{})),
		WithStaticFS(staticFS()),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- application.Run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get("http://" + cfg.Server.Listen + "/api/v1/healthz")
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("server did not start: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not stop")
	}
}

func TestRunListenError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()

	cfg := testConfig(t)
	cfg.Server.Listen = listener.Addr().String()

	application, err := New(context.Background(), cfg, testLogger(), WithDialer(testDialer(&incusfake.Server{}, &incusfake.Server{})))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	if err := application.Run(context.Background()); err == nil {
		t.Fatal("Run() expected listen error")
	}
}

func freeAddress(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := listener.Addr().String()
	_ = listener.Close()

	return address
}

func TestRunWithAgentGateway(t *testing.T) {
	ca, err := agentgw.GenerateCA("app-test-ca")
	if err != nil {
		t.Fatal(err)
	}
	server, err := ca.IssueServer([]string{"127.0.0.1"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writePEM := func(name string, data []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate[0]})
	keyDER, err := x509.MarshalPKCS8PrivateKey(server.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	cfg := testConfig(t)
	cfg.Server.Listen = freeAddress(t)
	cfg.Agent = config.AgentConfig{
		Enabled:  true,
		Listen:   freeAddress(t),
		TLSCert:  writePEM("server.crt", certPEM),
		TLSKey:   writePEM("server.key", keyPEM),
		ClientCA: writePEM("ca.crt", ca.CertPEM()),
		CAKey:    writePEM("ca.key", ca.KeyPEM()),
	}

	application, err := New(context.Background(), cfg, testLogger(),
		WithDialer(testDialer(&incusfake.Server{}, &incusfake.Server{})),
		WithStaticFS(staticFS()),
	)
	if err != nil {
		t.Fatalf("New(agent) error = %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- application.Run(ctx) }()

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} //nolint:gosec // test listener
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := client.Post("https://"+cfg.Agent.Listen+"/api/v1/agent/enroll", "application/json", strings.NewReader("{}"))
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				cancel()
				t.Fatalf("agent enroll status = %d", resp.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("agent gateway did not start: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not stop")
	}
}

func TestNewAgentCAError(t *testing.T) {
	cfg := testConfig(t)
	cfg.Agent = config.AgentConfig{
		Enabled: true, Listen: "127.0.0.1:9444",
		TLSCert: "/missing/server.crt", TLSKey: "/missing/server.key",
		ClientCA: "/missing/ca.crt", CAKey: "/missing/ca.key",
	}
	if _, err := New(context.Background(), cfg, testLogger(),
		WithDialer(testDialer(&incusfake.Server{}, &incusfake.Server{})),
	); err == nil {
		t.Fatal("New(agent missing CA) expected error")
	}
}

func TestParseDuration(t *testing.T) {
	if got := parseDuration("5s", time.Minute); got != 5*time.Second {
		t.Fatalf("parseDuration(5s) = %v", got)
	}
	if got := parseDuration("bad", time.Minute); got != time.Minute {
		t.Fatalf("parseDuration(bad) = %v", got)
	}
}

func TestReadSecret(t *testing.T) {
	value, err := readSecret("inline", "")
	if err != nil || value != "inline" {
		t.Fatalf("readSecret(inline) = %q, %v", value, err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte(" file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err = readSecret("", path)
	if err != nil || value != "file-token" {
		t.Fatalf("readSecret(file) = %q, %v", value, err)
	}
	if _, err := readSecret("", filepath.Join(dir, "missing")); err == nil {
		t.Fatal("readSecret(missing) expected error")
	}
}

func TestNewWithExternalMetrics(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	cfg := testConfig(t)
	cfg.Metrics = config.MetricsConfig{
		Enabled: true,
		RemoteWrite: config.RemoteWriteConfig{
			Enabled: true, URL: backend.URL, Timeout: "1s", QueueSize: 10, BatchSize: 10,
		},
		Query: config.MetricsQueryConfig{
			Enabled: true, URL: backend.URL, Type: "prometheus", Timeout: "1s",
		},
		HTTPSD: config.MetricsHTTPSDConfig{Enabled: true, Token: "sd", Mode: "proxy"},
	}
	application, err := New(context.Background(), cfg, testLogger(),
		WithDialer(testDialer(&incusfake.Server{}, &incusfake.Server{})),
		WithStaticFS(staticFS()),
	)
	if err != nil {
		t.Fatalf("New(external metrics) error = %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })
	if application.remoteWrite == nil || application.metricsQuery == nil {
		t.Fatal("external metrics not wired")
	}
}
