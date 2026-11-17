package agentgw

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"

	"github.com/mickeyzzc/incus-fleet/internal/agentpb"
	"github.com/mickeyzzc/incus-fleet/internal/bootstrap"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/telemetry"
)

func openAgentStore(t *testing.T) *store.SQLStore {
	t.Helper()
	st, err := store.Open(context.Background(), "sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func createToken(t *testing.T, st *store.SQLStore, cluster, member string) string {
	t.Helper()
	token, hash, err := NewEnrollmentToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateEnrollmentToken(context.Background(), &store.EnrollmentToken{
		ID:        store.NewID(),
		TokenHash: hash,
		Cluster:   cluster,
		Member:    member,
		ExpiresAt: time.Now().Add(15 * time.Minute),
		CreatedBy: "test",
	}); err != nil {
		t.Fatal(err)
	}
	return token
}

func TestEnrollmentAndAgentSession(t *testing.T) {
	ca, err := GenerateCA("test-ca")
	if err != nil {
		t.Fatal(err)
	}
	st := openAgentStore(t)
	gw := New(st, ca, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewUnstartedServer(gw.Handler())
	srv.TLS = &tls.Config{ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: ca.Pool()}
	srv.StartTLS()
	defer srv.Close()

	pubPEM, keyPEM := agentKeyPair(t)
	token := createToken(t, st, "c1", "node-a")

	body, _ := json.Marshal(enrollmentRequest{
		Token: token, Cluster: "c1", Member: "node-a", Hostname: "node-a",
		Version: "0.2.0", OS: "debian", Arch: "amd64", Kernel: "6.12",
		PublicKey: string(pubPEM),
	})
	resp, err := srv.Client().Post(srv.URL+"/api/v1/agent/enroll", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("enroll status = %d body=%s", resp.StatusCode, data)
	}
	var enrolled enrollmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&enrolled); err != nil {
		t.Fatal(err)
	}
	if enrolled.AgentID == "" || enrolled.Certificate == "" || enrolled.CA == "" {
		t.Fatalf("enroll response = %#v", enrolled)
	}

	// A token is single-use.
	reuse, err := srv.Client().Post(srv.URL+"/api/v1/agent/enroll", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_ = reuse.Body.Close()
	if reuse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reused token status = %d", reuse.StatusCode)
	}

	// Connect without a client certificate is rejected before upgrade.
	noCert, err := srv.Client().Get(srv.URL + "/api/v1/agent/connect")
	if err != nil {
		t.Fatal(err)
	}
	_ = noCert.Body.Close()
	if noCert.StatusCode != http.StatusUnauthorized {
		t.Fatalf("connect without cert status = %d", noCert.StatusCode)
	}

	clientCert, err := tls.X509KeyPair([]byte(enrolled.Certificate), keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	dialer := websocket.Dialer{TLSClientConfig: &tls.Config{
		RootCAs:      pool,
		Certificates: []tls.Certificate{clientCert},
		MinVersion:   tls.VersionTLS12,
	}}
	ws, _, err := dialer.Dial("wss"+strings.TrimPrefix(srv.URL, "https")+"/api/v1/agent/connect", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ws.Close() }()

	writeEnvelope(t, ws, &agentpb.Envelope{Body: &agentpb.Envelope_Hello{Hello: &agentpb.Hello{
		AgentId: enrolled.AgentID, Version: "0.2.0", Os: "debian", Arch: "amd64",
		Kernel: "6.12", Hostname: "node-a", Capabilities: []string{"nodenet.snapshot"},
	}}})
	helloAck := readEnvelope(t, ws)
	if helloAck.GetHelloAck() == nil || helloAck.GetHelloAck().ProtocolVersion != 1 {
		t.Fatalf("hello ack = %#v", helloAck)
	}

	writeEnvelope(t, ws, &agentpb.Envelope{Body: &agentpb.Envelope_Facts{Facts: &agentpb.FactsReport{
		Hostname: "node-a", Os: "debian", Kernel: "6.12", Arch: "amd64",
	}}})
	writeEnvelope(t, ws, &agentpb.Envelope{Body: &agentpb.Envelope_Heartbeat{Heartbeat: &agentpb.Heartbeat{}}})
	heartbeatAck := readEnvelope(t, ws)
	if heartbeatAck.GetHeartbeatAck() == nil {
		t.Fatalf("heartbeat ack = %#v", heartbeatAck)
	}

	// Dispatch a snapshot task and answer it from the agent side.
	done := make(chan error, 1)
	go func() {
		task := readEnvelope(t, ws)
		if task.GetTask() == nil || task.GetTask().Type != agentpb.TaskType_TASK_TYPE_NODENET_SNAPSHOT {
			done <- io.ErrUnexpectedEOF
			return
		}
		writeEnvelope(t, ws, &agentpb.Envelope{Body: &agentpb.Envelope_Result{Result: &agentpb.TaskResult{
			TaskId:  task.GetTask().TaskId,
			State:   agentpb.TaskState_TASK_STATE_SUCCEEDED,
			Payload: []byte(`{"host":"node-a"}`),
		}}})
		done <- nil
	}()

	result, err := gw.Dispatch(context.Background(), enrolled.AgentID, agentpb.TaskType_TASK_TYPE_NODENET_SNAPSHOT, nil, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != agentpb.TaskState_TASK_STATE_SUCCEEDED || string(result.Payload) != `{"host":"node-a"}` {
		t.Fatalf("dispatch result = %#v", result)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	agent, err := st.GetAgent(context.Background(), enrolled.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if agent.Status != store.AgentOnline || agent.Facts == "{}" {
		t.Fatalf("agent after session = %#v", agent)
	}

	_ = ws.Close()
	waitForAgentStatus(t, st, enrolled.AgentID, store.AgentOffline)
}

func waitForAgentStatus(t *testing.T, st *store.SQLStore, id, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		agent, err := st.GetAgent(context.Background(), id)
		if err == nil && agent.Status == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	agent, _ := st.GetAgent(context.Background(), id)
	t.Fatalf("agent status = %#v, want %q", agent, want)
}

func writeEnvelope(t *testing.T, ws *websocket.Conn, env *agentpb.Envelope) {
	t.Helper()
	data, err := proto.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, data); err != nil {
		t.Fatal(err)
	}
}

func readEnvelope(t *testing.T, ws *websocket.Conn) *agentpb.Envelope {
	t.Helper()
	_, data, err := ws.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var env agentpb.Envelope
	if err := proto.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	return &env
}

func TestEnrollmentRejectsBadPublicKey(t *testing.T) {
	ca, _ := GenerateCA("test-ca")
	st := openAgentStore(t)
	gw := New(st, ca, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewUnstartedServer(gw.Handler())
	srv.TLS = &tls.Config{ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: ca.Pool()}
	srv.StartTLS()
	defer srv.Close()

	token := createToken(t, st, "c1", "node-a")
	body, _ := json.Marshal(enrollmentRequest{Token: token, Cluster: "c1", Member: "node-a", PublicKey: "not-pem"})
	resp, err := srv.Client().Post(srv.URL+"/api/v1/agent/enroll", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestGatewayOptionsAndOfflineDispatch(t *testing.T) {
	ca, err := GenerateCA("test-ca")
	if err != nil {
		t.Fatal(err)
	}
	st := openAgentStore(t)
	gw := New(st, ca, slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithHeartbeatInterval(5*time.Second), WithCertTTL(2*time.Hour))
	if gw.heartbeatInterval != 5*time.Second || gw.certTTL != 2*time.Hour {
		t.Fatalf("options not applied: %#v", gw)
	}
	server, err := ca.IssueServer([]string{"127.0.0.1"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	tlsConfig := gw.TLSConfig(server)
	if tlsConfig.ClientAuth != tls.VerifyClientCertIfGiven || tlsConfig.ClientCAs == nil {
		t.Fatalf("TLS config = %#v", tlsConfig)
	}
	if gw.ConnectionCount() != 0 {
		t.Fatalf("ConnectionCount() = %d", gw.ConnectionCount())
	}
	if _, err := gw.Dispatch(context.Background(), "missing", agentpb.TaskType_TASK_TYPE_NODENET_SNAPSHOT, nil, time.Second); !errors.Is(err, ErrAgentOffline) {
		t.Fatalf("Dispatch(offline) error = %v", err)
	}
	if marshalFacts(nil) != "{}" {
		t.Fatalf("marshalFacts(nil) = %q", marshalFacts(nil))
	}

	conn := &agentConn{send: make(chan *agentpb.Envelope), done: make(chan struct{})}
	if conn.enqueue(&agentpb.Envelope{}) {
		t.Fatal("enqueue on a full channel should fail")
	}
	close(conn.done)
	if conn.enqueue(&agentpb.Envelope{}) {
		t.Fatal("enqueue on a closed connection should fail")
	}
}

func TestConnectRejectsUnknownOrRevokedAgent(t *testing.T) {
	ca, err := GenerateCA("test-ca")
	if err != nil {
		t.Fatal(err)
	}
	st := openAgentStore(t)
	gw := New(st, ca, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewUnstartedServer(gw.Handler())
	srv.TLS = &tls.Config{ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: ca.Pool()}
	srv.StartTLS()
	defer srv.Close()

	pubPEM, keyPEM := agentKeyPair(t)
	block, _ := pem.Decode(pubPEM)
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	certPEM, _, _, err := ca.IssueAgent("ghost", "c1", "node-a", pub, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	clientCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs: pool, Certificates: []tls.Certificate{clientCert}, MinVersion: tls.VersionTLS12,
	}}}
	resp, err := client.Get(srv.URL + "/api/v1/agent/connect")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("unknown agent status = %d", resp.StatusCode)
	}

	agent := &store.Agent{
		ID: "ghost", Cluster: "c1", Member: "node-a", Status: store.AgentRevoked,
		CertFingerprint: Fingerprint(mustParseCert(t, certPEM)),
		CertNotAfter:    time.Now().Add(time.Hour), CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := st.UpsertAgent(context.Background(), agent); err != nil {
		t.Fatal(err)
	}
	resp, err = client.Get(srv.URL + "/api/v1/agent/connect")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("revoked agent status = %d", resp.StatusCode)
	}
}

func mustParseCert(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func TestEnrollErrors(t *testing.T) {
	ca, err := GenerateCA("test-ca")
	if err != nil {
		t.Fatal(err)
	}
	st := openAgentStore(t)
	gw := New(st, ca, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewUnstartedServer(gw.Handler())
	srv.TLS = &tls.Config{ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: ca.Pool()}
	srv.StartTLS()
	defer srv.Close()

	post := func(body enrollmentRequest) *http.Response {
		data, _ := json.Marshal(body)
		resp, err := srv.Client().Post(srv.URL+"/api/v1/agent/enroll", "application/json", bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	resp := post(enrollmentRequest{})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing fields status = %d", resp.StatusCode)
	}

	pubPEM, _ := agentKeyPair(t)
	resp = post(enrollmentRequest{Token: "bad", Cluster: "c1", Member: "node-a", PublicKey: string(pubPEM)})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad token status = %d", resp.StatusCode)
	}

	token := createToken(t, st, "c1", "node-a")
	resp = post(enrollmentRequest{Token: token, Cluster: "c1", Member: "node-a", PublicKey: string(pubPEM)})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first enroll status = %d", resp.StatusCode)
	}

	token = createToken(t, st, "c1", "node-a")
	resp = post(enrollmentRequest{Token: token, Cluster: "c1", Member: "node-a", PublicKey: string(pubPEM)})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate enroll status = %d", resp.StatusCode)
	}
}

func TestGatewayDisabledAndDispatchTimeout(t *testing.T) {
	gw := New(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/enroll", strings.NewReader("{}"))
	gw.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled enroll status = %d", rec.Code)
	}

	gw = New(openAgentStore(t), mustTestCA(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	conn := &agentConn{id: "agent-1", send: make(chan *agentpb.Envelope, 1), done: make(chan struct{})}
	gw.register(conn)
	defer gw.unregister(conn)
	if gw.ConnectionCount() != 1 {
		t.Fatalf("ConnectionCount() = %d", gw.ConnectionCount())
	}
	if _, err := gw.Dispatch(context.Background(), "agent-1", agentpb.TaskType_TASK_TYPE_NODENET_SNAPSHOT, nil, 10*time.Millisecond); err == nil {
		t.Fatal("Dispatch(timeout) expected error")
	}
	gw.complete(nil)
	gw.send("missing", &agentpb.Envelope{})
}

func mustTestCA(t *testing.T) *CA {
	t.Helper()
	ca, err := GenerateCA("test-ca")
	if err != nil {
		t.Fatal(err)
	}
	return ca
}

func TestDispatchContextCanceled(t *testing.T) {
	ca := mustTestCA(t)
	gw := New(openAgentStore(t), ca, slog.New(slog.NewTextHandler(io.Discard, nil)))
	conn := &agentConn{id: "agent-1", send: make(chan *agentpb.Envelope, 1), done: make(chan struct{})}
	gw.register(conn)
	defer gw.unregister(conn)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := gw.Dispatch(ctx, "agent-1", agentpb.TaskType_TASK_TYPE_NODENET_SNAPSHOT, nil, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("Dispatch(canceled) error = %v", err)
	}
}

func TestConnectRejectsFingerprintMismatch(t *testing.T) {
	ca := mustTestCA(t)
	st := openAgentStore(t)
	gw := New(st, ca, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewUnstartedServer(gw.Handler())
	srv.TLS = &tls.Config{ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: ca.Pool()}
	srv.StartTLS()
	defer srv.Close()

	pubPEM, keyPEM := agentKeyPair(t)
	block, _ := pem.Decode(pubPEM)
	pub, _ := x509.ParsePKIXPublicKey(block.Bytes)
	certPEM, _, _, err := ca.IssueAgent("mismatch", "c1", "node-a", pub, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	clientCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	agent := &store.Agent{
		ID: "mismatch", Cluster: "c1", Member: "node-a", Status: store.AgentOnline,
		CertFingerprint: "wrong", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := st.UpsertAgent(context.Background(), agent); err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs: pool, Certificates: []tls.Certificate{clientCert}, MinVersion: tls.VersionTLS12,
	}}}
	resp, err := client.Get(srv.URL + "/api/v1/agent/connect")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("fingerprint mismatch status = %d", resp.StatusCode)
	}
}

func TestSignBootstrap(t *testing.T) {
	ca := mustTestCA(t)
	gw := New(openAgentStore(t), ca, slog.New(slog.NewTextHandler(io.Discard, nil)))
	spec := &bootstrap.Spec{
		Version: 1, Mode: "join", Cluster: "c1", Member: "node-a",
		Address: "192.0.2.10", Preseed: "cluster:\n  enabled: true\n",
		TaskID: "task-1", Nonce: "nonce", ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	if err := gw.SignBootstrap(spec); err != nil {
		t.Fatalf("SignBootstrap() error = %v", err)
	}
	if err := bootstrap.Verify(ca.CertPEM(), spec); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if gw.ca = nil; gw.SignBootstrap(spec) == nil {
		t.Fatal("SignBootstrap(nil CA) expected error")
	}
}

func TestGatewayIngestsTelemetry(t *testing.T) {
	ca := mustTestCA(t)
	st := openAgentStore(t)
	ts := telemetry.New(10)
	gw := New(st, ca, slog.New(slog.NewTextHandler(io.Discard, nil)), WithTelemetry(ts))
	now := time.Now().UTC()
	agent := &store.Agent{ID: "agent-1", Cluster: "c1", Member: "node-a", Status: store.AgentOnline,
		CreatedAt: now, UpdatedAt: now}
	if err := st.UpsertAgent(context.Background(), agent); err != nil {
		t.Fatal(err)
	}
	gw.handleEnvelope(context.Background(), agent, &agentpb.Envelope{Body: &agentpb.Envelope_Telemetry{
		Telemetry: &agentpb.TelemetryBatch{AgentId: "agent-1", Series: []*agentpb.Series{{
			Name: "fleet_host_load", Type: agentpb.MetricType_METRIC_TYPE_GAUGE,
			Samples: []*agentpb.Sample{{AtMs: now.UnixMilli(), Value: 1.5}},
		}}},
	}})
	series := ts.Query(telemetry.Filter{Cluster: "c1", Member: "node-a"})
	if len(series) != 1 || series[0].Points[0].Value != 1.5 {
		t.Fatalf("telemetry = %#v", series)
	}
}

func TestIssueCertificate(t *testing.T) {
	ca := mustTestCA(t)
	gw := New(openAgentStore(t), ca, slog.New(slog.NewTextHandler(io.Discard, nil)))
	pubPEM, _ := agentKeyPair(t)
	block, _ := pem.Decode(pubPEM)
	pub, _ := x509.ParsePKIXPublicKey(block.Bytes)
	certPEM, fingerprint, expires, err := gw.IssueCertificate("agent-1", "c1", "node-a", pub, time.Hour)
	if err != nil || len(certPEM) == 0 || fingerprint == "" || !expires.After(time.Now()) {
		t.Fatalf("IssueCertificate() = %q, %q, %v, %v", certPEM, fingerprint, expires, err)
	}
	gw.ca = nil
	if _, _, _, err := gw.IssueCertificate("agent-1", "c1", "node-a", pub, time.Hour); err == nil {
		t.Fatal("IssueCertificate(nil CA) expected error")
	}
}
