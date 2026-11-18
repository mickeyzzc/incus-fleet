// Package mesh provides the center consensus layer. It uses Raft for leader
// election, a fencing epoch, and replication of agent connection ownership so
// that any center can route host operations to the instance holding the
// agent's WebSocket.
package mesh

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb/v2"

	"github.com/mickeyzzc/incus-fleet/internal/agentpb"
)

// Peer is one Raft participant. A witness uses the same shape but does not
// run the application's task runner or agent gateway.
type Peer struct {
	ID             string `yaml:"id"`
	Address        string `yaml:"address"`
	ControlAddress string `yaml:"control_address"`
}

// Config controls the Raft mesh.
type Config struct {
	Enabled        bool   `yaml:"enabled"`
	NodeID         string `yaml:"node_id"`
	Address        string `yaml:"address"`
	ControlAddress string `yaml:"control_address"`
	DataDir        string `yaml:"data_dir"`
	// Peers is the initial voter set. Use three voters, or two voters plus a
	// witness. Bootstrap happens only when the local data directory is empty.
	Peers []Peer `yaml:"peers"`
	// Witness participates in quorum but runs no workload.
	Witness bool `yaml:"witness"`
	// Secret guards the internal dispatch/admin endpoints between centers.
	Secret string `yaml:"secret"`
}

// AgentDispatcher is the subset of the gateway used for relay dispatch.
type AgentDispatcher interface {
	Dispatch(ctx context.Context, agentID string, typ agentpb.TaskType, payload []byte, timeout time.Duration) (*agentpb.TaskResult, error)
}

// Mesh owns the Raft node and the local dispatcher used for relay.
type Mesh struct {
	cfg    Config
	logger *slog.Logger
	fsm    *FSM

	mu         sync.RWMutex
	raft       *raft.Raft
	store      *raftboltdb.BoltStore
	transport  *raft.NetworkTransport
	dispatcher AgentDispatcher
	client     *http.Client
	controls   map[string]string
}

// New builds a mesh from the configuration. Call Start to join/bootstrap.
func New(cfg Config, logger *slog.Logger) (*Mesh, error) {
	if !cfg.Enabled {
		return nil, errors.New("mesh: disabled")
	}
	if cfg.NodeID == "" || cfg.Address == "" || cfg.ControlAddress == "" || cfg.DataDir == "" {
		return nil, errors.New("mesh: node_id, address, control_address, and data_dir are required")
	}
	if cfg.Secret == "" {
		return nil, errors.New("mesh: secret is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("mesh: create data dir: %w", err)
	}

	return &Mesh{
		cfg: cfg, logger: logger, fsm: NewFSM(), controls: make(map[string]string),
		client: &http.Client{},
	}, nil
}

// Start opens persistent Raft stores and joins or bootstraps the cluster.
func (m *Mesh) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.raft != nil {
		return nil
	}
	if err := m.startLocked(); err != nil {
		return err
	}
	return nil
}

func (m *Mesh) startLocked() error {
	advertise, err := net.ResolveTCPAddr("tcp", m.cfg.Address)
	if err != nil {
		return fmt.Errorf("mesh: resolve address: %w", err)
	}
	logOutput := &slogWriter{logger: m.logger}
	transport, err := raft.NewTCPTransport(m.cfg.Address, advertise, 3, 5*time.Second, logOutput)
	if err != nil {
		return fmt.Errorf("mesh: create transport: %w", err)
	}
	store, err := raftboltdb.NewBoltStore(filepath.Join(m.cfg.DataDir, "raft.db"))
	if err != nil {
		_ = transport.Close()
		return fmt.Errorf("mesh: open raft store: %w", err)
	}
	snapshots, err := raft.NewFileSnapshotStore(m.cfg.DataDir, 2, logOutput)
	if err != nil {
		_ = store.Close()
		_ = transport.Close()
		return fmt.Errorf("mesh: open snapshot store: %w", err)
	}

	raftCfg := raft.DefaultConfig()
	raftCfg.LocalID = raft.ServerID(m.cfg.NodeID)
	raftCfg.LogOutput = logOutput
	raftCfg.HeartbeatTimeout = 1000 * time.Millisecond
	raftCfg.ElectionTimeout = 1000 * time.Millisecond
	raftCfg.LeaderLeaseTimeout = 500 * time.Millisecond
	raftCfg.CommitTimeout = 100 * time.Millisecond

	node, err := raft.NewRaft(raftCfg, m.fsm, store, store, snapshots, transport)
	if err != nil {
		_ = store.Close()
		_ = transport.Close()
		return fmt.Errorf("mesh: create raft: %w", err)
	}
	m.raft = node
	m.store = store
	m.transport = transport

	if len(m.cfg.Peers) > 0 {
		hasState, err := raft.HasExistingState(store, store, snapshots)
		if err != nil {
			return fmt.Errorf("mesh: inspect raft state: %w", err)
		}
		if !hasState {
			servers := make([]raft.Server, 0, len(m.cfg.Peers))
			for _, peer := range m.cfg.Peers {
				servers = append(servers, raft.Server{
					ID: raft.ServerID(peer.ID), Address: raft.ServerAddress(peer.Address),
				})
			}
			if err := node.BootstrapCluster(raft.Configuration{Servers: servers}).Error(); err != nil {
				return fmt.Errorf("mesh: bootstrap: %w", err)
			}
			m.logger.Info("raft cluster bootstrapped", "peers", len(servers))
		}
	}

	return nil
}

// SetDispatcher installs the local gateway used for relay.
func (m *Mesh) SetDispatcher(d AgentDispatcher) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dispatcher = d
}

// Close stops the Raft node and releases its stores.
func (m *Mesh) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopLocked()
}

func (m *Mesh) stopLocked() error {
	if m.raft == nil {
		return nil
	}
	if err := m.raft.Shutdown().Error(); err != nil {
		return fmt.Errorf("mesh: shutdown raft: %w", err)
	}
	m.raft = nil
	if m.store != nil {
		_ = m.store.Close()
		m.store = nil
	}
	if m.transport != nil {
		_ = m.transport.Close()
		m.transport = nil
	}
	return nil
}

// IsLeader reports whether this instance is the current Raft leader.
func (m *Mesh) IsLeader() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.raft != nil && m.raft.State() == raft.Leader
}

// Leader returns the leader's node ID when known.
func (m *Mesh) Leader() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.raft == nil {
		return ""
	}
	_, id := m.raft.LeaderWithID()
	return string(id)
}

// Term returns the current Raft term, used as the fencing epoch.
func (m *Mesh) Term() uint64 {
	m.mu.RLock()
	node := m.raft
	m.mu.RUnlock()
	if node == nil {
		return m.fsm.Epoch()
	}
	value, ok := node.Stats()["term"]
	if !ok {
		return m.fsm.Epoch()
	}
	term, err := strconv.ParseUint(strings.TrimSpace(fmt.Sprint(value)), 10, 64)
	if err != nil {
		return m.fsm.Epoch()
	}
	return term
}

// WaitForLeader blocks until a leader is elected or the timeout expires.
func (m *Mesh) WaitForLeader(timeout time.Duration) error {
	m.mu.RLock()
	node := m.raft
	m.mu.RUnlock()
	if node == nil {
		return errors.New("mesh: not started")
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, id := node.LeaderWithID(); id != "" {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("mesh: no leader elected")
}

// SetOwner replicates agent ownership without an expiry.
func (m *Mesh) SetOwner(agentID, owner string) error {
	return m.SetOwnerTTL(agentID, owner, 0)
}

// SetOwnerTTL replicates ownership with an expiry so a crashed center cannot
// leave a permanently stale owner.
func (m *Mesh) SetOwnerTTL(agentID, owner string, ttl time.Duration) error {
	m.mu.RLock()
	node := m.raft
	m.mu.RUnlock()
	if node == nil {
		return errors.New("mesh: not started")
	}
	if node.State() != raft.Leader {
		return raft.ErrNotLeader
	}
	cmd := command{Op: opSetOwner, AgentID: agentID, Owner: owner}
	if ttl > 0 {
		cmd.ExpiresAt = time.Now().Add(ttl).UnixMilli()
	}
	data, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	return node.Apply(data, 5*time.Second).Error()
}

// ClearOwner removes a connection ownership record.
func (m *Mesh) ClearOwner(agentID string) error {
	m.mu.RLock()
	node := m.raft
	m.mu.RUnlock()
	if node == nil || node.State() != raft.Leader {
		return raft.ErrNotLeader
	}
	data, err := json.Marshal(command{Op: opClearOwner, AgentID: agentID})
	if err != nil {
		return err
	}
	return node.Apply(data, 5*time.Second).Error()
}

// SetOwnerAny records ownership on the leader, relaying when this node is a
// follower. Agent WebSockets may land on any center behind the virtual URL.
func (m *Mesh) SetOwnerAny(agentID, owner string, ttl time.Duration) error {
	expires := int64(0)
	if ttl > 0 {
		expires = time.Now().Add(ttl).UnixMilli()
	}
	if m.IsLeader() {
		return m.SetOwnerTTL(agentID, owner, ttl)
	}
	return m.forwardOwner(ownerRequest{
		Op: opSetOwner, AgentID: agentID, Owner: owner, ExpiresAt: expires,
	})
}

// ClearOwnerAny removes ownership on the leader, relaying when needed.
func (m *Mesh) ClearOwnerAny(agentID string) error {
	if m.IsLeader() {
		return m.ClearOwner(agentID)
	}
	return m.forwardOwner(ownerRequest{Op: opClearOwner, AgentID: agentID})
}

type ownerRequest struct {
	Op        string `json:"op"`
	AgentID   string `json:"agent_id"`
	Owner     string `json:"owner,omitempty"`
	ExpiresAt int64  `json:"expires_at,omitempty"`
}

func (m *Mesh) forwardOwner(req ownerRequest) error {
	leader := m.Leader()
	if leader == "" {
		return errors.New("mesh: no leader to record agent owner")
	}
	address := m.peerAddress(leader)
	if address == "" {
		return fmt.Errorf("mesh: unknown leader %q control address", leader)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+address+"/internal/mesh/owner", bytes.NewReader(body)) //nolint:gosec // operator-configured peer
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(AdminSecretHeader, m.cfg.Secret)
	resp, err := m.client.Do(httpReq) //nolint:gosec // operator-configured peer
	if err != nil {
		return fmt.Errorf("mesh: forward owner to %s: %w", leader, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("mesh: forward owner to %s: HTTP %d: %s", leader, resp.StatusCode, message)
	}
	return nil
}

// OwnerHandler applies a forwarded ownership change. It is leader-only.
func (m *Mesh) OwnerHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !constantTimeEqual(r.Header.Get(AdminSecretHeader), m.cfg.Secret) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var req ownerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AgentID == "" {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		var err error
		switch req.Op {
		case opSetOwner:
			ttl := time.Duration(0)
			if req.ExpiresAt > 0 {
				ttl = time.Until(time.UnixMilli(req.ExpiresAt))
			}
			err = m.SetOwnerTTL(req.AgentID, req.Owner, ttl)
		case opClearOwner:
			err = m.ClearOwner(req.AgentID)
		default:
			http.Error(w, "unknown operation", http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// Owner resolves the center that currently holds the agent's WebSocket.
func (m *Mesh) Owner(agentID string) string {
	return m.fsm.Owner(agentID)
}

// Status describes the local Raft state for diagnostics.
func (m *Mesh) Status() map[string]any {
	m.mu.RLock()
	node := m.raft
	m.mu.RUnlock()
	status := map[string]any{
		"node_id":         m.cfg.NodeID,
		"control_address": m.cfg.ControlAddress,
		"witness":         m.cfg.Witness,
		"running":         node != nil,
	}
	if node == nil {
		return status
	}
	_, leaderID := node.LeaderWithID()
	status["leader"] = string(leaderID)
	status["state"] = node.State().String()
	status["term"] = m.Term()
	servers := node.GetConfiguration().Configuration().Servers
	peers := make([]map[string]any, 0, len(servers))
	for _, server := range servers {
		peers = append(peers, map[string]any{
			"id": server.ID, "address": server.Address, "voter": server.Suffrage == raft.Voter,
		})
	}
	status["peers"] = peers
	return status
}

// Join adds a recovered node. A non-voter catches up before promotion.
func (m *Mesh) Join(id, address, controlAddress string, voter bool) error {
	m.mu.RLock()
	node := m.raft
	m.mu.RUnlock()
	if node == nil {
		return errors.New("mesh: not started")
	}
	if node.State() != raft.Leader {
		return raft.ErrNotLeader
	}
	if strings.TrimSpace(controlAddress) != "" {
		m.mu.Lock()
		m.controls[id] = controlAddress
		m.mu.Unlock()
	}
	var future raft.Future
	if voter {
		future = node.AddVoter(raft.ServerID(id), raft.ServerAddress(address), 0, 5*time.Second)
	} else {
		future = node.AddNonvoter(raft.ServerID(id), raft.ServerAddress(address), 0, 5*time.Second)
	}
	return future.Error()
}

// Promote turns a caught-up non-voter into a full voter.
func (m *Mesh) Promote(id string) error {
	m.mu.RLock()
	node := m.raft
	m.mu.RUnlock()
	if node == nil || node.State() != raft.Leader {
		return raft.ErrNotLeader
	}
	return node.AddVoter(raft.ServerID(id), raft.ServerAddress(m.raftAddress(id)), 0, 5*time.Second).Error()
}

// Remove drops a node from the voter set.
func (m *Mesh) Remove(id string) error {
	m.mu.RLock()
	node := m.raft
	m.mu.RUnlock()
	if node == nil || node.State() != raft.Leader {
		return raft.ErrNotLeader
	}
	return node.RemoveServer(raft.ServerID(id), 0, 5*time.Second).Error()
}

// ForceSingle resets this node into a one-voter cluster for disaster recovery.
// The operator must ensure all other nodes are permanently fenced first.
func (m *Mesh) ForceSingle() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.stopLocked(); err != nil {
		return err
	}
	entries, err := os.ReadDir(m.cfg.DataDir)
	if err != nil {
		return fmt.Errorf("mesh: read data dir: %w", err)
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(m.cfg.DataDir, entry.Name())); err != nil {
			return fmt.Errorf("mesh: clear data dir: %w", err)
		}
	}
	original := m.cfg.Peers
	m.cfg.Peers = []Peer{{ID: m.cfg.NodeID, Address: m.cfg.Address}}
	err = m.startLocked()
	m.cfg.Peers = original
	if err != nil {
		return err
	}
	m.fsm.Reset()
	m.logger.Warn("raft forced to single-node mode", "node_id", m.cfg.NodeID)
	return nil
}

func (m *Mesh) raftAddress(id string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, peer := range m.cfg.Peers {
		if peer.ID == id {
			return peer.Address
		}
	}
	if m.raft != nil {
		for _, server := range m.raft.GetConfiguration().Configuration().Servers {
			if string(server.ID) == id {
				return string(server.Address)
			}
		}
	}
	return ""
}

func (m *Mesh) peerAddress(id string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if control := m.controls[id]; control != "" {
		return control
	}
	for _, peer := range m.cfg.Peers {
		if peer.ID == id {
			if peer.ControlAddress != "" {
				return peer.ControlAddress
			}
			return peer.Address
		}
	}
	if m.raft != nil {
		for _, server := range m.raft.GetConfiguration().Configuration().Servers {
			if string(server.ID) == id {
				return string(server.Address)
			}
		}
	}
	return ""
}

type dispatchRequest struct {
	AgentID   string `json:"agent_id"`
	TaskType  int32  `json:"task_type"`
	Payload   []byte `json:"payload"`
	TimeoutMS int64  `json:"timeout_ms"`
}

type dispatchResponse struct {
	State   int32  `json:"state"`
	Error   string `json:"error,omitempty"`
	Payload []byte `json:"payload,omitempty"`
}

// Dispatch sends a task to the center that owns the agent connection.
func (m *Mesh) Dispatch(ctx context.Context, agentID string, typ agentpb.TaskType, payload []byte, timeout time.Duration) (*agentpb.TaskResult, error) {
	owner := m.Owner(agentID)
	m.mu.RLock()
	local := m.cfg.NodeID
	dispatcher := m.dispatcher
	client := m.client
	m.mu.RUnlock()

	if owner == "" || owner == local {
		if dispatcher == nil {
			return nil, errors.New("mesh: no local dispatcher configured")
		}
		return dispatcher.Dispatch(ctx, agentID, typ, payload, timeout)
	}

	address := m.peerAddress(owner)
	if address == "" {
		return nil, fmt.Errorf("mesh: unknown owner %q for agent %s", owner, agentID)
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout+5*time.Second)
		defer cancel()
	}
	request := dispatchRequest{
		AgentID: agentID, TaskType: int32(typ), Payload: payload, TimeoutMS: timeout.Milliseconds(),
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+address+"/internal/mesh/dispatch", bytes.NewReader(body)) //nolint:gosec // address is an operator-configured mesh peer
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fleet-Mesh-Secret", m.cfg.Secret)
	resp, err := client.Do(req) //nolint:gosec // request targets an operator-configured mesh peer
	if err != nil {
		return nil, fmt.Errorf("mesh: relay to %s: %w", owner, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("mesh: relay to %s: HTTP %d: %s", owner, resp.StatusCode, message)
	}
	var result dispatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("mesh: decode relay result: %w", err)
	}
	return &agentpb.TaskResult{
		State:   agentpb.TaskState(result.State),
		Payload: result.Payload,
		Error:   result.Error,
	}, nil
}

// DispatchHandler serves a relayed task for an agent connected locally.
func (m *Mesh) DispatchHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !constantTimeEqual(r.Header.Get("X-Fleet-Mesh-Secret"), m.cfg.Secret) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var req dispatchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AgentID == "" {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		m.mu.RLock()
		dispatcher := m.dispatcher
		m.mu.RUnlock()
		if dispatcher == nil {
			http.Error(w, "no local dispatcher", http.StatusServiceUnavailable)
			return
		}
		timeout := time.Duration(req.TimeoutMS) * time.Millisecond
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		result, err := dispatcher.Dispatch(r.Context(), req.AgentID, agentpb.TaskType(req.TaskType), req.Payload, timeout)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(dispatchResponse{
			State: int32(result.State), Error: result.Error, Payload: result.Payload,
		})
	})
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

type slogWriter struct{ logger *slog.Logger }

func (w *slogWriter) Write(data []byte) (int, error) {
	text := string(bytes.TrimSpace(data))
	if text != "" {
		w.logger.Debug("raft", "message", text)
	}
	return len(data), nil
}

// AdminSecretHeader is the header used for inter-center calls.
const AdminSecretHeader = "X-Fleet-Mesh-Secret" //nolint:gosec // header name, not a credential

// FormatPeerAddress validates and normalizes a raft peer address.
func FormatPeerAddress(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" {
		return "", fmt.Errorf("mesh: invalid peer address %q: expected host:port", address)
	}
	if _, err := strconv.Atoi(port); err != nil {
		return "", fmt.Errorf("mesh: invalid peer port in %q", address)
	}
	return net.JoinHostPort(host, port), nil
}
