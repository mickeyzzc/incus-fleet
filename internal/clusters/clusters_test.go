package clusters

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func testClientPEM(t *testing.T) (string, string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "incus-fleet-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return string(certPEM), string(keyPEM)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testConfig(name string) config.Cluster {
	return config.Cluster{
		Name:        name,
		DisplayName: name + " display",
		Endpoint:    "https://example.invalid:8443",
		Project:     "default",
		TLSCert:     "/tmp/client.crt",
		TLSKey:      "/tmp/client.key",
	}
}

func fakeDialer(servers map[string]InstanceServer, calls *[]string) DialFunc {
	return func(_ context.Context, cfg config.Cluster) (InstanceServer, error) {
		if calls != nil {
			*calls = append(*calls, cfg.Name)
		}
		srv, ok := servers[cfg.Name]
		if !ok {
			return nil, errors.New("connect failed")
		}

		return srv, nil
	}
}

func TestSummaryAndList(t *testing.T) {
	srv := &incusfake.Server{
		Clustered: true,
		Server: &api.Server{Environment: api.ServerEnvironment{
			Server:          "incus",
			ServerVersion:   "7.0.2",
			ServerName:      "node-a",
			ServerClustered: true,
			Architectures:   []string{"x86_64"},
		}},
		Members: []api.ClusterMember{{
			ServerName:   "node-a",
			URL:          "https://192.0.2.10:8443",
			Status:       "Online",
			Message:      "Fully operational",
			Database:     true,
			Architecture: "x86_64",
			ClusterMemberPut: api.ClusterMemberPut{
				Roles: []string{"database-leader"},
			},
		}},
		Instances: []api.InstanceFull{
			{Instance: api.Instance{Name: "web", Type: "container", StatusCode: api.Running}},
			{Instance: api.Instance{Name: "db", Type: "container", StatusCode: api.Frozen}},
			{Instance: api.Instance{Name: "vm", Type: "virtual-machine", StatusCode: api.Stopped}},
		},
		PoolResources: &api.ResourcesStoragePool{Space: api.ResourcesStoragePoolSpace{Used: 50, Total: 100}},
		NetworkNames:  []string{"incusbr0", "eth0"},
		ServerResources: &api.Resources{
			CPU:    api.ResourcesCPU{Architecture: "x86_64", Total: 8},
			Memory: api.ResourcesMemory{Used: 4_000_000_000, Total: 16_000_000_000},
		},
	}

	dial := fakeDialer(map[string]InstanceServer{"c1": srv}, nil)
	m, err := NewWithDialer(context.Background(), []config.Cluster{testConfig("c1")}, dial, testLogger())
	if err != nil {
		t.Fatalf("NewWithDialer() error = %v", err)
	}
	t.Cleanup(m.Close)

	if got, want := m.Names(), []string{"c1"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("Names() = %v", got)
	}

	list := m.List()
	if len(list) != 1 {
		t.Fatalf("List() len = %d", len(list))
	}

	s := list[0]
	if !s.Online || s.Error != "" {
		t.Fatalf("summary online = %v error = %q", s.Online, s.Error)
	}
	if s.Name != "c1" || s.DisplayName != "c1 display" || s.Endpoint != "https://example.invalid:8443" {
		t.Fatalf("summary identity = %#v", s)
	}
	if s.ServerName != "node-a" || s.ServerVersion != "7.0.2" || !s.Clustered {
		t.Fatalf("summary server = %#v", s)
	}
	if s.Instances != (Counts{Total: 3, Containers: 2, VirtualMachines: 1, Running: 2}) {
		t.Fatalf("Counts = %#v", s.Instances)
	}
	if s.Storage.Pool != "default" || s.Storage.Used != 50 || s.Storage.Total != 100 {
		t.Fatalf("Storage = %#v", s.Storage)
	}
	if len(s.Members) != 1 || s.Members[0].Name != "node-a" || !s.Members[0].Database {
		t.Fatalf("Members = %#v", s.Members)
	}
	if len(s.Networks) != 2 || s.Networks[0] != "eth0" {
		t.Fatalf("Networks = %#v", s.Networks)
	}
	if s.CPU.Total != 8 || s.CPU.Architecture != "x86_64" {
		t.Fatalf("CPU = %#v", s.CPU)
	}
	if s.Memory.Used != 4_000_000_000 || s.Memory.Total != 16_000_000_000 {
		t.Fatalf("Memory = %#v", s.Memory)
	}

	got, err := m.Get("c1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Name != "c1" {
		t.Fatalf("Get() = %#v", got)
	}
}

func TestUnknownCluster(t *testing.T) {
	m, err := NewWithDialer(context.Background(), []config.Cluster{testConfig("c1")}, fakeDialer(map[string]InstanceServer{"c1": &incusfake.Server{}}, nil), testLogger())
	if err != nil {
		t.Fatalf("NewWithDialer() error = %v", err)
	}
	t.Cleanup(m.Close)

	if _, err := m.Get("missing"); !errors.Is(err, ErrUnknownCluster) {
		t.Fatalf("Get() error = %v", err)
	}
	if _, err := m.Connection("missing"); !errors.Is(err, ErrUnknownCluster) {
		t.Fatalf("Connection() error = %v", err)
	}
	if _, err := m.Reconnect(context.Background(), "missing"); !errors.Is(err, ErrUnknownCluster) {
		t.Fatalf("Reconnect() error = %v", err)
	}
}

func TestOfflineAndReconnect(t *testing.T) {
	srv := &incusfake.Server{}
	var attempts atomic.Int32
	dial := func(_ context.Context, _ config.Cluster) (InstanceServer, error) {
		if attempts.Add(1) == 1 {
			return nil, errors.New("network down")
		}
		return srv, nil
	}

	m, err := NewWithDialer(context.Background(), []config.Cluster{testConfig("c1")}, dial, testLogger())
	if err != nil {
		t.Fatalf("NewWithDialer() error = %v", err)
	}
	t.Cleanup(m.Close)

	summary := m.List()[0]
	if summary.Online {
		t.Fatal("offline cluster reported online")
	}
	if summary.Error == "" {
		t.Fatal("offline cluster has no error")
	}

	conn, _ := m.Connection("c1")
	if _, err := conn.Client(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Client() error = %v", err)
	}

	got, err := m.Reconnect(context.Background(), "c1")
	if err != nil {
		t.Fatalf("Reconnect() error = %v", err)
	}
	if !got.Online {
		t.Fatalf("Reconnect() summary = %#v", got)
	}
	if _, err := conn.Client(); err != nil {
		t.Fatalf("Client() after reconnect error = %v", err)
	}
}

func TestReconnectFailure(t *testing.T) {
	var attempts atomic.Int32
	dial := func(_ context.Context, _ config.Cluster) (InstanceServer, error) {
		attempts.Add(1)
		if attempts.Load() == 1 {
			return &incusfake.Server{}, nil
		}
		return nil, errors.New("still down")
	}

	m, err := NewWithDialer(context.Background(), []config.Cluster{testConfig("c1")}, dial, testLogger())
	if err != nil {
		t.Fatalf("NewWithDialer() error = %v", err)
	}
	t.Cleanup(m.Close)

	summary, err := m.Reconnect(context.Background(), "c1")
	if err == nil {
		t.Fatal("Reconnect() expected error")
	}
	if summary.Online {
		t.Fatal("summary should be offline")
	}
}

func TestDuplicateAndMissingDialer(t *testing.T) {
	if _, err := NewWithDialer(context.Background(), []config.Cluster{testConfig("c1")}, nil, testLogger()); err == nil {
		t.Fatal("NewWithDialer() expected missing dialer error")
	}

	cfg := testConfig("c1")
	if _, err := NewWithDialer(context.Background(), []config.Cluster{cfg, cfg}, fakeDialer(map[string]InstanceServer{"c1": &incusfake.Server{}}, nil), testLogger()); err == nil {
		t.Fatal("NewWithDialer() expected duplicate error")
	}
}

func TestServerErrors(t *testing.T) {
	srv := &incusfake.Server{ServerErr: errors.New("boom")}
	m, err := NewWithDialer(context.Background(), []config.Cluster{testConfig("c1")}, fakeDialer(map[string]InstanceServer{"c1": srv}, nil), testLogger())
	if err != nil {
		t.Fatalf("NewWithDialer() error = %v", err)
	}
	t.Cleanup(m.Close)

	summary := m.List()[0]
	if summary.Online || summary.Error == "" {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestCloseDisconnects(t *testing.T) {
	srv := &incusfake.Server{}
	m, err := NewWithDialer(context.Background(), []config.Cluster{testConfig("c1")}, fakeDialer(map[string]InstanceServer{"c1": srv}, nil), testLogger())
	if err != nil {
		t.Fatalf("NewWithDialer() error = %v", err)
	}
	m.Close()

	if srv.Disconnects != 1 {
		t.Fatalf("Disconnects = %d", srv.Disconnects)
	}
}

func TestDefaultDialMissingFiles(t *testing.T) {
	if _, err := DefaultDial(context.Background(), testConfig("c1")); err == nil {
		t.Fatal("DefaultDial() expected read error")
	}
}

func TestConnectionAccessors(t *testing.T) {
	cfg := testConfig("c1")
	cfg.Project = "fleet"
	conn := &Connection{cfg: cfg}
	if conn.Name() != "c1" || conn.DisplayName() != "c1 display" || conn.Endpoint() != cfg.Endpoint || conn.Project() != "fleet" {
		t.Fatalf("accessors = %q %q %q %q", conn.Name(), conn.DisplayName(), conn.Endpoint(), conn.Project())
	}
	if conn.ConnectError() != nil {
		t.Fatalf("ConnectError() = %v", conn.ConnectError())
	}
	if _, err := conn.Client(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Client() error = %v", err)
	}
}

func TestNewWithDefaultDialerRecordsErrors(t *testing.T) {
	cfg := testConfig("c1")
	cfg.TLSCert = "/missing/client.crt"
	m, err := New(context.Background(), []config.Cluster{cfg}, testLogger())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(m.Close)

	conn, _ := m.Connection("c1")
	if conn.ConnectError() == nil {
		t.Fatal("ConnectError() is nil")
	}
	if m.List()[0].Online {
		t.Fatal("cluster should be offline")
	}
}

func TestConnectionArgsAcceptInlinePEM(t *testing.T) {
	cert, key := testClientPEM(t)

	cfg := testConfig("c1")
	cfg.TLSCert = cert
	cfg.TLSKey = key
	cfg.TLSServerCert = "-----BEGIN CERTIFICATE-----\nYQ==\n-----END CERTIFICATE-----\n"

	args, err := connectionArgs(cfg)
	if err != nil {
		t.Fatalf("connectionArgs(inline PEM) error = %v", err)
	}
	if strings.TrimSpace(args.TLSClientCert) != strings.TrimSpace(cert) ||
		strings.TrimSpace(args.TLSClientKey) != strings.TrimSpace(key) {
		t.Fatalf("connectionArgs(inline PEM) did not keep PEM contents")
	}
	if args.TLSServerCert == "" {
		t.Fatal("connectionArgs(inline server cert) is empty")
	}
}

func TestConnectionArgsReadFiles(t *testing.T) {
	cert, key := testClientPEM(t)
	dir := t.TempDir()
	certPath := filepath.Join(dir, "client.crt")
	keyPath := filepath.Join(dir, "client.key")
	if err := os.WriteFile(certPath, []byte(cert), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte(key), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig("c1")
	cfg.TLSCert = certPath
	cfg.TLSKey = keyPath

	args, err := connectionArgs(cfg)
	if err != nil {
		t.Fatalf("connectionArgs(paths) error = %v", err)
	}
	if args.TLSClientCert != cert || args.TLSClientKey != key {
		t.Fatal("connectionArgs(paths) did not read file contents")
	}
}

func TestDefaultDialInvalidFiles(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "client.crt")
	key := filepath.Join(dir, "client.key")
	serverCert := filepath.Join(dir, "server.crt")
	if err := os.WriteFile(cert, []byte("cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(serverCert, []byte("server"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig("c1")
	cfg.TLSCert = cert
	cfg.TLSKey = filepath.Join(dir, "missing.key")
	if _, err := DefaultDial(context.Background(), cfg); err == nil {
		t.Fatal("missing key expected error")
	}

	cfg.TLSKey = key
	cfg.TLSServerCert = filepath.Join(dir, "missing-server.crt")
	if _, err := DefaultDial(context.Background(), cfg); err == nil {
		t.Fatal("missing server cert expected error")
	}

	cfg.TLSServerCert = serverCert
	cfg.Endpoint = "https://127.0.0.1:1"
	cfg.Name = "c1"
	if _, err := DefaultDial(context.Background(), cfg); err == nil {
		t.Fatal("invalid TLS material expected connect error")
	}
}

func TestEndpointFailover(t *testing.T) {
	cfg := testConfig("c1")
	cfg.Endpoints = []string{"https://192.0.2.10:8443", "https://192.0.2.11:8443"}
	var attempted []string
	dial := func(_ context.Context, target config.Cluster) (InstanceServer, error) {
		attempted = append(attempted, target.Endpoint)
		if target.Endpoint == cfg.Endpoints[0] {
			return nil, errors.New("primary endpoint down")
		}
		return &incusfake.Server{}, nil
	}
	m, err := NewWithDialer(context.Background(), []config.Cluster{cfg}, dial, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)

	conn, err := m.Connection("c1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Client(); err != nil {
		t.Fatalf("Client() error = %v", err)
	}
	if got := conn.ActiveEndpoint(); got != cfg.Endpoints[1] {
		t.Fatalf("ActiveEndpoint = %q", got)
	}
	if len(attempted) != 2 || attempted[0] != cfg.Endpoints[0] || attempted[1] != cfg.Endpoints[1] {
		t.Fatalf("dial attempts = %v", attempted)
	}

	if _, err := m.Reconnect(context.Background(), "c1"); err != nil {
		t.Fatalf("Reconnect() error = %v", err)
	}
	if got := conn.ActiveEndpoint(); got != cfg.Endpoints[1] {
		t.Fatalf("ActiveEndpoint after reconnect = %q", got)
	}
}

func TestClusterWatchStopsOnCancel(t *testing.T) {
	m, err := NewWithDialer(context.Background(), []config.Cluster{testConfig("c1")},
		fakeDialer(map[string]InstanceServer{"c1": &incusfake.Server{}}, nil), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		m.watch(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch did not stop")
	}
}

func TestClusterWatchRetriesOffline(t *testing.T) {
	var attempts atomic.Int32
	dial := func(context.Context, config.Cluster) (InstanceServer, error) {
		attempts.Add(1)
		return nil, errors.New("offline")
	}
	m, err := NewWithDialer(context.Background(), []config.Cluster{testConfig("c1")}, dial, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	m.watchInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.watch(ctx)
		close(done)
	}()
	time.Sleep(80 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch did not stop")
	}
	if attempts.Load() < 2 {
		t.Fatalf("watch attempts = %d", attempts.Load())
	}
}

func TestClusterWatchSuccessBranch(t *testing.T) {
	var attempts atomic.Int32
	dial := func(context.Context, config.Cluster) (InstanceServer, error) {
		if attempts.Add(1) < 3 {
			return nil, errors.New("offline")
		}
		return &incusfake.Server{}, nil
	}
	m, err := NewWithDialer(context.Background(), []config.Cluster{testConfig("c1")}, dial, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	m.watchInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.watch(ctx)
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := m.Connection("c1")
		if err == nil {
			if _, err := conn.Client(); err == nil {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	conn, err := m.Connection("c1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Client(); err != nil {
		t.Fatalf("watch did not reconnect: %v", err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch did not stop")
	}
}

func TestConnectionWithoutEndpoints(t *testing.T) {
	cfg := testConfig("c1")
	cfg.Endpoint = ""
	cfg.Endpoints = nil
	conn := &Connection{cfg: cfg, endpoints: config.ClusterEndpoints(cfg)}
	if err := conn.connect(context.Background(), func(context.Context, config.Cluster) (InstanceServer, error) {
		t.Fatal("dial should not be called")
		return nil, nil
	}); err == nil {
		t.Fatal("connect without endpoints expected error")
	}
	if got := conn.ActiveEndpoint(); got != "" {
		t.Fatalf("ActiveEndpoint = %q", got)
	}
}
