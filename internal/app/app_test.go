package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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
			SpoolDir: t.TempDir(), SpoolPartition: "app-a",
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
	if _, err := os.Stat(filepath.Join(cfg.Metrics.RemoteWrite.SpoolDir, "app-a")); err != nil {
		t.Fatalf("spool partition not created: %v", err)
	}
}

func TestNewWithHADispatcher(t *testing.T) {
	cfg := testConfig(t)
	cfg.HA = config.HAConfig{Enabled: true, WorkerID: "test-worker", Lease: "30s", Interval: "1s"}
	application, err := New(context.Background(), cfg, testLogger(),
		WithDialer(testDialer(&incusfake.Server{}, &incusfake.Server{})),
		WithStaticFS(staticFS()),
	)
	if err != nil {
		t.Fatalf("New(ha) error = %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })
	if application.taskRunner == nil || application.taskRunner.Worker() != "test-worker" {
		t.Fatalf("task runner = %#v", application.taskRunner)
	}
}

func TestNewHADefaultWorker(t *testing.T) {
	cfg := testConfig(t)
	cfg.HA = config.HAConfig{Enabled: true}
	application, err := New(context.Background(), cfg, testLogger(),
		WithDialer(testDialer(&incusfake.Server{}, &incusfake.Server{})),
		WithStaticFS(staticFS()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close() })
	if application.taskRunner == nil || application.taskRunner.Worker() == "" {
		t.Fatal("default HA worker is empty")
	}
}

func TestNewRemoteWriteSpoolError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(t)
	cfg.Metrics = config.MetricsConfig{
		Enabled: true,
		RemoteWrite: config.RemoteWriteConfig{
			Enabled: true, URL: "https://example.com/write",
			Timeout: "1s", QueueSize: 10, BatchSize: 10,
			SpoolDir: filepath.Join(blocker, "queue"),
		},
	}
	if _, err := New(context.Background(), cfg, testLogger(),
		WithDialer(testDialer(&incusfake.Server{}, &incusfake.Server{})),
	); err == nil {
		t.Fatal("New(blocked remote-write spool) expected error")
	}
}

func TestRunWithHADispatcher(t *testing.T) {
	cfg := testConfig(t)
	cfg.Server.Listen = freeAddress(t)
	cfg.HA = config.HAConfig{Enabled: true, WorkerID: "test-worker", Lease: "30s", Interval: "1s"}
	application, err := New(context.Background(), cfg, testLogger(),
		WithDialer(testDialer(&incusfake.Server{}, &incusfake.Server{})),
		WithStaticFS(staticFS()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- application.Run(ctx) }()
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
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not stop")
	}
}

func TestDefaultSpoolPartition(t *testing.T) {
	partition := defaultSpoolPartition()
	if !strings.Contains(partition, strconv.Itoa(os.Getpid())) || strings.TrimSpace(partition) == "" {
		t.Fatalf("defaultSpoolPartition() = %q", partition)
	}
}

func writeTestCA(t *testing.T, dir string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fleet-test-ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(dir, "ca.crt")
	keyPath := filepath.Join(dir, "ca.key")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func writeTestServerCert(t *testing.T, dir string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "fleet.example"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		DNSNames: []string{"fleet.example"}, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func TestNewWithMesh(t *testing.T) {
	dir := t.TempDir()
	caCert, caKey := writeTestCA(t, dir)
	serverCert, serverKey := writeTestServerCert(t, dir)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()

	cfg := testConfig(t)
	cfg.Agent = config.AgentConfig{
		Enabled: true, Listen: "127.0.0.1:18444", TLSCert: serverCert, TLSKey: serverKey,
		ClientCA: caCert, CAKey: caKey,
	}
	cfg.HA = config.HAConfig{Enabled: true, WorkerID: "app-a", Lease: "30s", Interval: "1s"}
	cfg.Mesh = config.MeshConfig{
		Enabled: true, NodeID: "app-a", Address: address, ControlAddress: address,
		DataDir: filepath.Join(dir, "mesh"), Secret: "mesh-secret", ForceSingle: true,
		Peers: []config.MeshPeer{{ID: "app-a", Address: address, ControlAddress: address}},
	}
	application, err := New(context.Background(), cfg, testLogger(),
		WithDialer(testDialer(&incusfake.Server{}, &incusfake.Server{})),
		WithStaticFS(staticFS()),
	)
	if err != nil {
		t.Fatalf("New(mesh) error = %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })
	if application.mesh == nil || application.taskRunner == nil {
		t.Fatal("mesh or task runner not wired")
	}
	if application.mesh.Owner("missing") != "" {
		t.Fatal("unexpected owner")
	}
}

func TestNewWithMeshWitness(t *testing.T) {
	dir := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()

	cfg := testConfig(t)
	cfg.Agent.Enabled = false
	cfg.HA = config.HAConfig{Enabled: true, WorkerID: "witness", Lease: "30s", Interval: "1s"}
	cfg.Mesh = config.MeshConfig{
		Enabled: true, NodeID: "witness", Address: address, ControlAddress: address,
		DataDir: filepath.Join(dir, "mesh"), Secret: "mesh-secret", ForceSingle: true,
		Witness: true,
		Peers:   []config.MeshPeer{{ID: "witness", Address: address, ControlAddress: address}},
	}
	application, err := New(context.Background(), cfg, testLogger(),
		WithDialer(testDialer(&incusfake.Server{}, &incusfake.Server{})),
		WithStaticFS(staticFS()),
	)
	if err != nil {
		t.Fatalf("New(witness) error = %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })
	if application.mesh == nil || application.taskRunner != nil {
		t.Fatalf("witness wiring: mesh=%v taskRunner=%v", application.mesh, application.taskRunner)
	}
}

func TestBootstrapAdminCreatesFirstAccount(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t)
	cfg.Server.BootstrapAdminUsername = "admin"
	cfg.Server.BootstrapAdminPassword = "test-admin-passw0rd"

	app, err := New(ctx, cfg, testLogger(),
		WithStaticFS(staticFS()),
		WithDialer(testDialer(&incusfake.Server{}, &incusfake.Server{})),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	users, err := app.store.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers() = %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("users = %d, want 1", len(users))
	}
	if users[0].Username != "admin" || users[0].Role != "admin" {
		t.Fatalf("bootstrap user = %#v", users[0])
	}
}

func TestBootstrapAdminIsIdempotent(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t)
	cfg.Server.BootstrapAdminUsername = "admin"
	cfg.Server.BootstrapAdminPassword = "test-admin-passw0rd"

	restart := func() {
		app, err := New(ctx, cfg, testLogger(),
			WithStaticFS(staticFS()),
			WithDialer(testDialer(&incusfake.Server{}, &incusfake.Server{})),
		)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		_ = app.Close()
	}

	restart()
	restart()

	// Reopen once more and confirm there is exactly one account.
	app, err := New(ctx, cfg, testLogger(),
		WithStaticFS(staticFS()),
		WithDialer(testDialer(&incusfake.Server{}, &incusfake.Server{})),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	users, err := app.store.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers() = %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("users after restarts = %d, want 1", len(users))
	}
}

func TestBootstrapAdminRejectsWeakPassword(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t)
	cfg.Server.BootstrapAdminUsername = "admin"
	cfg.Server.BootstrapAdminPassword = "short"

	if _, err := New(ctx, cfg, testLogger(),
		WithStaticFS(staticFS()),
		WithDialer(testDialer(&incusfake.Server{}, &incusfake.Server{})),
	); err == nil {
		t.Fatal("New() accepted a weak bootstrap password")
	}
}

func TestNoBootstrapWithoutCredentials(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t)

	app, err := New(ctx, cfg, testLogger(),
		WithStaticFS(staticFS()),
		WithDialer(testDialer(&incusfake.Server{}, &incusfake.Server{})),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	users, err := app.store.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers() = %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("users = %d, want 0", len(users))
	}
}
