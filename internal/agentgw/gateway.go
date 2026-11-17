package agentgw

import (
	"context"
	"crypto"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/mickeyzzc/incus-fleet/internal/agentpb"
	"github.com/mickeyzzc/incus-fleet/internal/bootstrap"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/telemetry"
)

const (
	// DefaultHeartbeatInterval is advertised to agents and used for liveness.
	DefaultHeartbeatInterval = 15 * time.Second
	defaultEnrollmentTTL     = 15 * time.Minute
	maxEnrollBody            = 64 << 10
)

// ErrAgentOffline marks a dispatch to an agent without a live connection.
var ErrAgentOffline = errors.New("agentgw: agent is offline")

// Option customizes the gateway.
type Option func(*Gateway)

// WithHeartbeatInterval overrides the advertised heartbeat interval.
func WithHeartbeatInterval(d time.Duration) Option {
	return func(g *Gateway) {
		if d > 0 {
			g.heartbeatInterval = d
		}
	}
}

// WithCertTTL overrides the agent certificate lifetime.
func WithCertTTL(d time.Duration) Option {
	return func(g *Gateway) {
		if d > 0 {
			g.certTTL = d
		}
	}
}

// WithTelemetry attaches the center-side one-hour ring buffer.
func WithTelemetry(store *telemetry.Store) Option {
	return func(g *Gateway) { g.telemetry = store }
}

// Gateway serves enrollment and mTLS WebSocket connections.
type Gateway struct {
	store             *store.SQLStore
	ca                *CA
	logger            *slog.Logger
	heartbeatInterval time.Duration
	certTTL           time.Duration
	telemetry         *telemetry.Store

	mu      sync.Mutex
	conns   map[string]*agentConn
	waiters map[string]chan *agentpb.TaskResult
}

type agentConn struct {
	id   string
	ws   *websocket.Conn
	send chan *agentpb.Envelope
	done chan struct{}
	once sync.Once
}

// New builds a gateway.
func New(st *store.SQLStore, ca *CA, logger *slog.Logger, opts ...Option) *Gateway {
	if logger == nil {
		logger = slog.Default()
	}
	g := &Gateway{
		store:             st,
		ca:                ca,
		logger:            logger,
		heartbeatInterval: DefaultHeartbeatInterval,
		certTTL:           defaultAgentCertTTL,
		conns:             make(map[string]*agentConn),
		waiters:           make(map[string]chan *agentpb.TaskResult),
	}
	for _, opt := range opts {
		opt(g)
	}

	return g
}

// IssueCertificate signs a new client certificate for an existing agent key.
func (g *Gateway) IssueCertificate(agentID, cluster, member string, publicKey crypto.PublicKey, ttl time.Duration) ([]byte, string, time.Time, error) {
	if g.ca == nil {
		return nil, "", time.Time{}, errors.New("agentgw: CA is not configured")
	}
	return g.ca.IssueAgent(agentID, cluster, member, publicKey, ttl)
}

// SignBootstrap signs a bootstrap spec with the CA key.
func (g *Gateway) SignBootstrap(spec *bootstrap.Spec) error {
	if g.ca == nil {
		return errors.New("agentgw: CA is not configured")
	}
	return g.ca.SignBootstrap(spec)
}

// TLSConfig builds the listener configuration. Enrollment uses the server
// certificate; connect requires a verified client certificate.
func (g *Gateway) TLSConfig(server tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{server},
		ClientAuth:   tls.VerifyClientCertIfGiven,
		ClientCAs:    g.ca.Pool(),
		MinVersion:   tls.VersionTLS12,
	}
}

// Handler returns the agent gateway routes.
func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/agent/enroll", g.enroll)
	mux.HandleFunc("GET /api/v1/agent/connect", g.connect)
	return mux
}

type enrollmentRequest struct {
	Token     string `json:"token"`
	Cluster   string `json:"cluster"`
	Member    string `json:"member"`
	Hostname  string `json:"hostname"`
	Version   string `json:"version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Kernel    string `json:"kernel"`
	PublicKey string `json:"public_key"`
}

type enrollmentResponse struct {
	AgentID     string    `json:"agent_id"`
	Cluster     string    `json:"cluster"`
	Member      string    `json:"member"`
	Certificate string    `json:"certificate"`
	CA          string    `json:"ca"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func (g *Gateway) enroll(w http.ResponseWriter, r *http.Request) {
	if g.ca == nil || g.store == nil {
		writeAgentError(w, http.StatusServiceUnavailable, "unavailable", "agent gateway is not configured")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxEnrollBody)
	var req enrollmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	req.Token = strings.TrimSpace(req.Token)
	req.Cluster = strings.TrimSpace(req.Cluster)
	req.Member = strings.TrimSpace(req.Member)
	if req.Token == "" || req.Cluster == "" || req.Member == "" || strings.TrimSpace(req.PublicKey) == "" {
		writeAgentError(w, http.StatusBadRequest, "invalid_request", "token, cluster, member, and public_key are required")
		return
	}

	pub, err := parsePublicKey(req.PublicKey)
	if err != nil {
		writeAgentError(w, http.StatusBadRequest, "invalid_request", "public_key is not a valid PKIX PEM public key")
		return
	}

	hash := HashToken(req.Token)
	if _, err := g.store.ConsumeEnrollmentToken(r.Context(), hash, req.Cluster, req.Member, time.Now().UTC()); err != nil {
		writeAgentError(w, http.StatusUnauthorized, "invalid_token", "enrollment token is invalid, expired, or already used")
		return
	}

	if _, err := g.store.GetAgentByMember(r.Context(), req.Cluster, req.Member); err == nil {
		writeAgentError(w, http.StatusConflict, "conflict", "an agent is already enrolled for this cluster member")
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		writeAgentError(w, http.StatusInternalServerError, "internal_error", "failed to check agent registration")
		return
	}

	agentID := store.NewID()
	certPEM, fingerprint, expires, err := g.ca.IssueAgent(agentID, req.Cluster, req.Member, pub, g.certTTL)
	if err != nil {
		g.logger.Error("issue agent certificate", "error", err)
		writeAgentError(w, http.StatusInternalServerError, "internal_error", "failed to issue certificate")
		return
	}

	now := time.Now().UTC()
	agent := &store.Agent{
		ID:              agentID,
		Cluster:         req.Cluster,
		Member:          req.Member,
		Hostname:        req.Hostname,
		Status:          store.AgentPending,
		CertFingerprint: fingerprint,
		CertNotAfter:    expires,
		Version:         req.Version,
		OS:              req.OS,
		Kernel:          req.Kernel,
		Arch:            req.Arch,
		Capabilities:    "[]",
		Facts:           "{}",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := g.store.UpsertAgent(r.Context(), agent); err != nil {
		g.logger.Error("store agent registration", "error", err)
		writeAgentError(w, http.StatusInternalServerError, "internal_error", "failed to store agent registration")
		return
	}

	writeAgentJSON(w, http.StatusCreated, enrollmentResponse{
		AgentID:     agentID,
		Cluster:     req.Cluster,
		Member:      req.Member,
		Certificate: string(certPEM),
		CA:          string(g.ca.CertPEM()),
		ExpiresAt:   expires,
	})
}

func (g *Gateway) connect(w http.ResponseWriter, r *http.Request) {
	if g.store == nil {
		writeAgentError(w, http.StatusServiceUnavailable, "unavailable", "agent gateway is not configured")
		return
	}
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		writeAgentError(w, http.StatusUnauthorized, "invalid_certificate", "a client certificate is required")
		return
	}
	cert := r.TLS.PeerCertificates[0]
	agentID := strings.TrimSpace(cert.Subject.CommonName)
	if agentID == "" {
		writeAgentError(w, http.StatusUnauthorized, "invalid_certificate", "certificate has no agent ID")
		return
	}
	agent, err := g.store.GetAgent(r.Context(), agentID)
	if err != nil {
		writeAgentError(w, http.StatusForbidden, "unknown_agent", "agent is not enrolled")
		return
	}
	if agent.Status == store.AgentRevoked {
		writeAgentError(w, http.StatusForbidden, "revoked", "agent is revoked")
		return
	}
	if agent.CertFingerprint != "" && agent.CertFingerprint != Fingerprint(cert) {
		writeAgentError(w, http.StatusForbidden, "invalid_certificate", "certificate does not match the registration")
		return
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     func(*http.Request) bool { return true },
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(1 << 20)

	conn := &agentConn{
		id:   agentID,
		ws:   ws,
		send: make(chan *agentpb.Envelope, 16),
		done: make(chan struct{}),
	}
	g.register(conn)
	defer g.unregister(conn)
	defer conn.close()

	go conn.writeLoop()
	g.logger.Info("agent connected", "agent", agentID, "cluster", agent.Cluster, "member", agent.Member)

	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			break
		}
		var env agentpb.Envelope
		if err := proto.Unmarshal(data, &env); err != nil {
			g.logger.Warn("agent sent invalid envelope", "agent", agentID, "error", err)
			continue
		}
		g.handleEnvelope(r.Context(), agent, &env)
	}

	g.markOffline(agent)
	g.logger.Info("agent disconnected", "agent", agentID)
}

func (c *agentConn) writeLoop() {
	for {
		select {
		case env, ok := <-c.send:
			if !ok {
				return
			}
			data, err := proto.Marshal(env)
			if err != nil {
				continue
			}
			if err := c.ws.WriteMessage(websocket.BinaryMessage, data); err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}

func (c *agentConn) close() {
	c.once.Do(func() {
		close(c.done)
		_ = c.ws.Close()
	})
}

func (c *agentConn) enqueue(env *agentpb.Envelope) bool {
	select {
	case c.send <- env:
		return true
	case <-c.done:
		return false
	default:
		return false
	}
}

func (g *Gateway) handleEnvelope(_ context.Context, agent *store.Agent, env *agentpb.Envelope) {
	switch body := env.Body.(type) {
	case *agentpb.Envelope_Hello:
		if body.Hello.AgentId != "" && body.Hello.AgentId != agent.ID {
			g.logger.Warn("agent hello ID mismatch", "cert", agent.ID, "hello", body.Hello.AgentId)
			return
		}
		agent.Status = store.AgentOnline
		agent.Hostname = body.Hello.Hostname
		agent.Version = body.Hello.Version
		agent.OS = body.Hello.Os
		agent.Arch = body.Hello.Arch
		agent.Kernel = body.Hello.Kernel
		agent.LastSeen = time.Now().UTC()
		agent.UpdatedAt = agent.LastSeen
		caps, _ := json.Marshal(body.Hello.Capabilities)
		agent.Capabilities = string(caps)
		g.saveAgent(agent)
		g.send(agent.ID, &agentpb.Envelope{Body: &agentpb.Envelope_HelloAck{HelloAck: &agentpb.HelloAck{
			ServerTimeMs:        time.Now().UTC().UnixMilli(),
			ProtocolVersion:     1,
			HeartbeatIntervalMs: g.heartbeatInterval.Milliseconds(),
		}}})
	case *agentpb.Envelope_Heartbeat:
		agent.LastSeen = time.Now().UTC()
		agent.UpdatedAt = agent.LastSeen
		g.saveAgent(agent)
		g.send(agent.ID, &agentpb.Envelope{Body: &agentpb.Envelope_HeartbeatAck{HeartbeatAck: &agentpb.HeartbeatAck{
			ServerTimeMs: time.Now().UTC().UnixMilli(),
		}}})
	case *agentpb.Envelope_Facts:
		agent.Facts = marshalFacts(body.Facts)
		agent.LastSeen = time.Now().UTC()
		agent.UpdatedAt = agent.LastSeen
		g.saveAgent(agent)
	case *agentpb.Envelope_Telemetry:
		if g.telemetry != nil {
			g.telemetry.Ingest(body.Telemetry, agent.Cluster, agent.Member)
		}
		agent.LastSeen = time.Now().UTC()
		agent.UpdatedAt = agent.LastSeen
		g.saveAgent(agent)
	case *agentpb.Envelope_Result:
		g.complete(body.Result)
	case *agentpb.Envelope_Error:
		g.logger.Warn("agent error", "agent", agent.ID, "code", body.Error.Code, "message", body.Error.Message)
	}
}

func marshalFacts(facts *agentpb.FactsReport) string {
	if facts == nil {
		return "{}"
	}
	data, err := protojson.Marshal(facts)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func (g *Gateway) saveAgent(agent *store.Agent) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := g.store.UpsertAgent(ctx, agent); err != nil {
		g.logger.Warn("update agent", "agent", agent.ID, "error", err)
	}
}

func (g *Gateway) markOffline(agent *store.Agent) {
	agent.Status = store.AgentOffline
	agent.LastSeen = time.Now().UTC()
	agent.UpdatedAt = agent.LastSeen
	g.saveAgent(agent)
}

func (g *Gateway) register(conn *agentConn) {
	g.mu.Lock()
	if old := g.conns[conn.id]; old != nil {
		old.close()
	}
	g.conns[conn.id] = conn
	g.mu.Unlock()
}

func (g *Gateway) unregister(conn *agentConn) {
	g.mu.Lock()
	if g.conns[conn.id] == conn {
		delete(g.conns, conn.id)
	}
	g.mu.Unlock()
}

func (g *Gateway) send(agentID string, env *agentpb.Envelope) {
	g.mu.Lock()
	conn := g.conns[agentID]
	g.mu.Unlock()
	if conn != nil {
		conn.enqueue(env)
	}
}

// Dispatch sends one task to an agent and waits for its result.
func (g *Gateway) Dispatch(ctx context.Context, agentID string, typ agentpb.TaskType, payload []byte, timeout time.Duration) (*agentpb.TaskResult, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	g.mu.Lock()
	conn := g.conns[agentID]
	g.mu.Unlock()
	if conn == nil {
		return nil, ErrAgentOffline
	}

	taskID := store.NewID()
	ch := make(chan *agentpb.TaskResult, 1)
	g.mu.Lock()
	g.waiters[taskID] = ch
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		delete(g.waiters, taskID)
		g.mu.Unlock()
	}()

	env := &agentpb.Envelope{Body: &agentpb.Envelope_Task{Task: &agentpb.TaskDispatch{
		TaskId:     taskID,
		Type:       typ,
		Payload:    payload,
		DeadlineMs: time.Now().Add(timeout).UnixMilli(),
	}}}
	if !conn.enqueue(env) {
		return nil, ErrAgentOffline
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case result := <-ch:
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, fmt.Errorf("agentgw: task %s timed out", taskID)
	}
}

func (g *Gateway) complete(result *agentpb.TaskResult) {
	if result == nil {
		return
	}
	g.mu.Lock()
	ch := g.waiters[result.TaskId]
	g.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- result:
	default:
	}
}

// ConnectionCount returns the number of live agent connections.
func (g *Gateway) ConnectionCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.conns)
}

// NewEnrollmentToken returns a random token and its stored hash.
func NewEnrollmentToken() (string, string, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", "", err
	}
	return token, HashToken(token), nil
}

// HashToken hashes an enrollment token for storage and comparison.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// DefaultEnrollmentTTL is the default token lifetime.
const DefaultEnrollmentTTL = defaultEnrollmentTTL

func parsePublicKey(value string) (any, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, errors.New("missing PEM block")
	}
	return x509.ParsePKIXPublicKey(block.Bytes)
}

func writeAgentJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeAgentError(w http.ResponseWriter, status int, code, message string) {
	writeAgentJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
