package mesh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/raft"

	"github.com/mickeyzzc/incus-fleet/internal/agentpb"
)

func applyCommand(t *testing.T, fsm *FSM, cmd command, index uint64) {
	t.Helper()
	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if err := fsm.Apply(&raft.Log{Data: data, Index: index}); err != nil {
		t.Fatalf("Apply(%s) error = %v", cmd.Op, err)
	}
}

func TestFSMOwnership(t *testing.T) {
	fsm := NewFSM()
	applyCommand(t, fsm, command{Op: opSetOwner, AgentID: "a1", Owner: "center-a"}, 1)
	if got := fsm.Owner("a1"); got != "center-a" {
		t.Fatalf("Owner = %q", got)
	}
	if fsm.Epoch() != 1 {
		t.Fatalf("Epoch = %d", fsm.Epoch())
	}
	applyCommand(t, fsm, command{Op: opClearOwner, AgentID: "a1"}, 2)
	if got := fsm.Owner("a1"); got != "" {
		t.Fatalf("Owner after clear = %q", got)
	}
	applyCommand(t, fsm, command{Op: opSetOwner, AgentID: "a2", Owner: "center-b"}, 3)
	snapshot, err := fsm.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	restored := NewFSM()
	sink := &memorySink{}
	if err := snapshot.Persist(sink); err != nil {
		t.Fatal(err)
	}
	if err := restored.Restore(sink.reader()); err != nil {
		t.Fatal(err)
	}
	if restored.Owner("a2") != "center-b" || restored.Epoch() != 3 {
		t.Fatalf("restored state = %q, %d", restored.Owner("a2"), restored.Epoch())
	}
	restored.Reset()
	if restored.Owner("a2") != "" || restored.Epoch() != 0 {
		t.Fatal("Reset did not clear state")
	}
	snapshot.Release()
}

func TestFSMInvalidCommands(t *testing.T) {
	fsm := NewFSM()
	data, _ := json.Marshal(command{Op: "unknown"})
	if err := fsm.Apply(&raft.Log{Data: data, Index: 1}); err == nil {
		t.Fatal("unknown command accepted")
	}
	data, _ = json.Marshal(command{Op: opSetOwner, AgentID: "a"})
	if err := fsm.Apply(&raft.Log{Data: data, Index: 1}); err == nil {
		t.Fatal("set-owner without owner accepted")
	}
	if err := fsm.Apply(&raft.Log{Data: []byte("not-json"), Index: 1}); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}

type memorySink struct {
	data []byte
}

func (s *memorySink) Write(data []byte) (int, error) {
	s.data = append(s.data, data...)
	return len(data), nil
}

func (s *memorySink) ID() string    { return "memory" }
func (s *memorySink) Close() error  { return nil }
func (s *memorySink) Cancel() error { return nil }
func (s *memorySink) reader() *readCloser {
	return &readCloser{Reader: &sliceReader{data: s.data}}
}

type sliceReader struct {
	data []byte
	off  int
}

func (r *sliceReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}

type readCloser struct{ Reader *sliceReader }

func (r *readCloser) Read(p []byte) (int, error) { return r.Reader.Read(p) }
func (r *readCloser) Close() error               { return nil }

type stubDispatcher struct {
	calls int
	agent string
	typ   agentpb.TaskType
}

func (d *stubDispatcher) Dispatch(_ context.Context, agentID string, typ agentpb.TaskType, _ []byte, _ time.Duration) (*agentpb.TaskResult, error) {
	d.calls++
	d.agent = agentID
	d.typ = typ
	return &agentpb.TaskResult{State: agentpb.TaskState_TASK_STATE_SUCCEEDED, Payload: []byte("ok")}, nil
}

func newTestMesh(t *testing.T, localDispatcher AgentDispatcher) *Mesh {
	t.Helper()
	m, err := New(Config{
		Enabled: true, NodeID: "center-a", Address: "127.0.0.1:19400",
		ControlAddress: "127.0.0.1:19401", DataDir: t.TempDir(), Secret: "mesh-secret",
		Peers: []Peer{{ID: "center-a", Address: "127.0.0.1:19400", ControlAddress: "127.0.0.1:19401"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.SetDispatcher(localDispatcher)
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func TestMeshDispatchLocalAndRelay(t *testing.T) {
	remote := &stubDispatcher{}
	remoteMux := http.NewServeMux()
	remoteMesh := newTestMesh(t, remote)
	remoteMux.Handle("POST /internal/mesh/dispatch", remoteMesh.DispatchHandler())
	server := httptest.NewServer(remoteMux)
	defer server.Close()

	local := &stubDispatcher{}
	m := newTestMesh(t, local)
	m.cfg.Peers = append(m.cfg.Peers, Peer{
		ID: "center-b", Address: "127.0.0.1:19410", ControlAddress: server.Listener.Addr().String(),
	})

	// No owner recorded: use the local dispatcher.
	result, err := m.Dispatch(context.Background(), "a1", agentpb.TaskType_TASK_TYPE_NODENET_SNAPSHOT, nil, time.Second)
	if err != nil || result.Payload == nil {
		t.Fatalf("local dispatch = %#v, %v", result, err)
	}
	if local.calls != 1 || remote.calls != 0 {
		t.Fatalf("local=%d remote=%d", local.calls, remote.calls)
	}

	// Explicit local owner still uses the local dispatcher.
	applyCommand(t, m.fsm, command{Op: opSetOwner, AgentID: "a2", Owner: "center-a"}, 1)
	if _, err := m.Dispatch(context.Background(), "a2", agentpb.TaskType_TASK_TYPE_NODENET_APPLY, nil, time.Second); err != nil {
		t.Fatal(err)
	}
	if local.calls != 2 {
		t.Fatalf("local calls = %d", local.calls)
	}

	// Remote owner is relayed over HTTP.
	applyCommand(t, m.fsm, command{Op: opSetOwner, AgentID: "a3", Owner: "center-b"}, 2)
	_, err = m.Dispatch(context.Background(), "a3", agentpb.TaskType_TASK_TYPE_INCUS_BOOTSTRAP, nil, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if remote.calls != 1 || remote.agent != "a3" || remote.typ != agentpb.TaskType_TASK_TYPE_INCUS_BOOTSTRAP {
		t.Fatalf("remote dispatch = calls=%d agent=%q type=%v", remote.calls, remote.agent, remote.typ)
	}
}

func TestMeshDispatchHandlerAuth(t *testing.T) {
	dispatcher := &stubDispatcher{}
	m := newTestMesh(t, dispatcher)
	handler := m.DispatchHandler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/mesh/dispatch", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unauthorized status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/internal/mesh/dispatch", nil)
	req.Header.Set(AdminSecretHeader, "mesh-secret")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad request status = %d", rec.Code)
	}

	body, _ := json.Marshal(dispatchRequest{AgentID: "a1", TaskType: int32(agentpb.TaskType_TASK_TYPE_NODENET_SNAPSHOT)})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/internal/mesh/dispatch", bytes.NewReader(body))
	req.Header.Set(AdminSecretHeader, "mesh-secret")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"payload":"b2s="`) {
		t.Fatalf("dispatch status = %d body=%s", rec.Code, rec.Body.String())
	}
	if dispatcher.calls != 1 {
		t.Fatalf("dispatcher calls = %d", dispatcher.calls)
	}
}

func TestMeshConfigValidationAndHelpers(t *testing.T) {
	if _, err := New(Config{Enabled: false}, nil); err == nil {
		t.Fatal("disabled mesh expected error")
	}
	base := Config{Enabled: true, NodeID: "n1", Address: "127.0.0.1:1", ControlAddress: "127.0.0.1:2", DataDir: t.TempDir(), Secret: "s"}
	for _, edit := range []func(*Config){
		func(c *Config) { c.NodeID = "" },
		func(c *Config) { c.Address = "" },
		func(c *Config) { c.ControlAddress = "" },
		func(c *Config) { c.DataDir = "" },
		func(c *Config) { c.Secret = "" },
	} {
		cfg := base
		edit(&cfg)
		if _, err := New(cfg, nil); err == nil {
			t.Fatalf("New(%#v) expected error", cfg)
		}
	}
	if !constantTimeEqual("abc", "abc") || constantTimeEqual("abc", "abd") || constantTimeEqual("a", "ab") {
		t.Fatal("constantTimeEqual mismatch")
	}
	m := newTestMesh(t, nil)
	if m.IsLeader() || m.Leader() != "" || m.Owner("missing") != "" {
		t.Fatal("unexpected unstarted mesh state")
	}
	if m.peerAddress("unknown") != "" {
		t.Fatal("unknown peer resolved")
	}
	if err := m.SetOwner("a", "center-a"); err == nil {
		t.Fatal("SetOwner without raft expected error")
	}
	if err := m.ClearOwner("a"); err == nil {
		t.Fatal("ClearOwner without raft expected error")
	}
	if err := m.Join("b", "127.0.0.1:2", "127.0.0.1:3", true); err == nil {
		t.Fatal("Join without raft expected error")
	}
	if err := m.Promote("b"); err == nil {
		t.Fatal("Promote without raft expected error")
	}
	if err := m.Remove("b"); err == nil {
		t.Fatal("Remove without raft expected error")
	}
	if _, err := m.Dispatch(context.Background(), "a", agentpb.TaskType_TASK_TYPE_NODENET_SNAPSHOT, nil, time.Second); err == nil {
		t.Fatal("Dispatch without local dispatcher expected error")
	}
	if status := m.Status(); status["running"] != false {
		t.Fatalf("Status = %#v", status)
	}
}

func freePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().String()
}

func TestRaftThreeNodeConsensus(t *testing.T) {
	if testing.Short() {
		t.Skip("raft integration test")
	}
	ids := []string{"center-a", "center-b", "center-c"}
	addresses := []string{freePort(t), freePort(t), freePort(t)}
	peers := make([]Peer, len(ids))
	for i := range ids {
		peers[i] = Peer{ID: ids[i], Address: addresses[i], ControlAddress: addresses[i]}
	}

	nodes := make([]*Mesh, 0, len(ids))
	for i, id := range ids {
		m, err := New(Config{
			Enabled: true, NodeID: id, Address: addresses[i], ControlAddress: addresses[i],
			DataDir: t.TempDir(), Peers: peers, Secret: "mesh-secret",
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Start(); err != nil {
			t.Fatalf("start %s: %v", id, err)
		}
		nodes = append(nodes, m)
		defer func() { _ = m.Close() }()
	}

	leaderIndex := -1
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		for i, m := range nodes {
			if m.IsLeader() {
				leaderIndex = i
				break
			}
		}
		if leaderIndex >= 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if leaderIndex < 0 {
		t.Fatal("no leader elected")
	}
	leaders := 0
	for _, m := range nodes {
		if m.IsLeader() {
			leaders++
		}
	}
	if leaders != 1 {
		t.Fatalf("leaders = %d", leaders)
	}

	leader := nodes[leaderIndex]
	if err := leader.WaitForLeader(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := nodes[(leaderIndex+1)%len(nodes)].WaitForLeader(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := leader.SetOwner("agent-1", ids[leaderIndex]); err != nil {
		t.Fatalf("set owner: %v", err)
	}
	follower := nodes[(leaderIndex+1)%len(nodes)]
	if err := follower.SetOwner("rejected", "wrong"); !errors.Is(err, raft.ErrNotLeader) {
		t.Fatalf("follower set owner error = %v", err)
	}
	if err := follower.ClearOwner("rejected"); !errors.Is(err, raft.ErrNotLeader) {
		t.Fatalf("follower clear owner error = %v", err)
	}
	if err := follower.Join("extra", "127.0.0.1:19898", "127.0.0.1:19899", false); !errors.Is(err, raft.ErrNotLeader) {
		t.Fatalf("follower join error = %v", err)
	}
	if err := follower.Promote("extra"); !errors.Is(err, raft.ErrNotLeader) {
		t.Fatalf("follower promote error = %v", err)
	}
	if err := follower.Remove("extra"); !errors.Is(err, raft.ErrNotLeader) {
		t.Fatalf("follower remove error = %v", err)
	}

	badLeader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "leader down", http.StatusBadGateway)
	}))
	defer badLeader.Close()
	for i := range follower.cfg.Peers {
		if follower.cfg.Peers[i].ID == ids[leaderIndex] {
			follower.cfg.Peers[i].ControlAddress = badLeader.Listener.Addr().String()
		}
	}
	if err := follower.SetOwnerAny("forwarded", "other", time.Minute); err == nil {
		t.Fatal("forward to bad leader expected error")
	}

	goodLeader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer goodLeader.Close()
	for i := range follower.cfg.Peers {
		if follower.cfg.Peers[i].ID == ids[leaderIndex] {
			follower.cfg.Peers[i].ControlAddress = goodLeader.Listener.Addr().String()
		}
	}
	if err := follower.ClearOwnerAny("forwarded"); err != nil {
		t.Fatalf("forward to good leader = %v", err)
	}
	if err := follower.JoinAny("new-node", "127.0.0.1:19894", "", false); err != nil {
		t.Fatalf("forward join = %v", err)
	}
	if err := follower.PromoteAny("new-node"); err != nil {
		t.Fatalf("forward promote = %v", err)
	}
	if err := follower.RemoveAny("new-node"); err != nil {
		t.Fatalf("forward remove = %v", err)
	}

	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ok := true
		for _, m := range nodes {
			if m.Owner("agent-1") != ids[leaderIndex] {
				ok = false
				break
			}
		}
		if ok {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, m := range nodes {
		if got := m.Owner("agent-1"); got != ids[leaderIndex] {
			t.Fatalf("owner not replicated to %s: %q", m.cfg.NodeID, got)
		}
	}

	if err := leader.ClearOwner("agent-1"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if leader.Owner("agent-1") == "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if leader.Owner("agent-1") != "" {
		t.Fatal("owner was not cleared")
	}
	for _, m := range nodes {
		if status := m.Status(); status["running"] != true || status["leader"] == "" {
			t.Fatalf("status = %#v", status)
		}
	}
}

func TestForceSingleRecovery(t *testing.T) {
	address := freePort(t)
	m, err := New(Config{
		Enabled: true, NodeID: "app-a", Address: address, ControlAddress: address,
		DataDir: t.TempDir(), Secret: "mesh-secret",
		Peers: []Peer{{ID: "app-a", Address: address, ControlAddress: address}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Close() }()
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	if err := m.WaitForLeader(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := m.SetOwner("a1", "app-a"); err != nil {
		t.Fatal(err)
	}
	if err := m.ForceSingle(); err != nil {
		t.Fatal(err)
	}
	if err := m.WaitForLeader(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	if !m.IsLeader() || m.Owner("a1") != "" {
		t.Fatalf("post-recovery state leader=%v owner=%q", m.IsLeader(), m.Owner("a1"))
	}
	if status := m.Status(); status["term"] == nil || status["peers"] == nil {
		t.Fatalf("status = %#v", status)
	}
	if err := m.Join("app-b", "127.0.0.1:19991", "127.0.0.1:19992", false); err != nil {
		t.Fatalf("Join(nonvoter) error = %v", err)
	}
	if err := m.Join("app-c", "127.0.0.1:19993", "", false); err != nil {
		t.Fatalf("Join(empty control) error = %v", err)
	}
	if err := m.Promote("app-b"); err != nil {
		t.Fatalf("Promote error = %v", err)
	}
	if err := m.Remove("app-b"); err != nil {
		t.Fatalf("Remove(nonvoter) error = %v", err)
	}
	if err := m.ClearOwner("a1"); err != nil {
		t.Fatalf("ClearOwner error = %v", err)
	}
}

type failingSink struct{}

func (failingSink) ID() string                { return "fail" }
func (failingSink) Write([]byte) (int, error) { return 0, errors.New("write failed") }
func (failingSink) Close() error              { return nil }
func (failingSink) Cancel() error             { return nil }
func (failingSink) Release()                  {}

type errorDispatcher struct{}

func (errorDispatcher) Dispatch(context.Context, string, agentpb.TaskType, []byte, time.Duration) (*agentpb.TaskResult, error) {
	return nil, errors.New("dispatch failed")
}

func TestMeshExtraBranches(t *testing.T) {
	fsm := NewFSM()
	applyCommand(t, fsm, command{Op: opSetOwner, AgentID: "a1", Owner: "center-b"}, 1)
	snapshot, err := fsm.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Persist(failingSink{}); err == nil {
		t.Fatal("Persist to failing sink expected error")
	}

	m := newTestMesh(t, errorDispatcher{})
	handler := m.DispatchHandler()
	body, _ := json.Marshal(dispatchRequest{AgentID: "a1"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/mesh/dispatch", bytes.NewReader(body))
	req.Header.Set(AdminSecretHeader, "mesh-secret")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("dispatcher error status = %d", rec.Code)
	}

	applyCommand(t, m.fsm, command{Op: opSetOwner, AgentID: "a2", Owner: "missing"}, 2)
	if _, err := m.Dispatch(context.Background(), "a2", agentpb.TaskType_TASK_TYPE_NODENET_SNAPSHOT, nil, time.Second); err == nil {
		t.Fatal("unknown owner expected error")
	}

	writer := &slogWriter{logger: slog.Default()}
	if n, err := writer.Write([]byte(" \n")); err != nil || n != 2 {
		t.Fatalf("slogWriter = %d, %v", n, err)
	}
}

func TestMeshStartErrors(t *testing.T) {
	_, err := New(Config{
		Enabled: true, NodeID: "n", Address: "127.0.0.1:19800", ControlAddress: "127.0.0.1:19801",
		DataDir: t.TempDir(), Secret: "s",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	badAddress, err := New(Config{
		Enabled: true, NodeID: "n", Address: "not-an-address", ControlAddress: "127.0.0.1:19801",
		DataDir: t.TempDir(), Secret: "s",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := badAddress.Start(); err == nil {
		t.Fatal("Start with invalid address expected error")
	}

	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{
		Enabled: true, NodeID: "n", Address: "127.0.0.1:19800", ControlAddress: "127.0.0.1:19801",
		DataDir: filepath.Join(blocker, "mesh"), Secret: "s",
	}, nil); err == nil {
		t.Fatal("New with blocked data dir expected error")
	}

	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()
	busyMesh, err := New(Config{
		Enabled: true, NodeID: "n", Address: busy.Addr().String(), ControlAddress: "127.0.0.1:19801",
		DataDir: t.TempDir(), Secret: "s",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := busyMesh.Start(); err == nil {
		t.Fatal("Start on busy port expected error")
	}

	readOnly := t.TempDir()
	if err := os.Chmod(readOnly, 0o500); err != nil { //nolint:gosec // test permissions
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(readOnly, 0o700) }() //nolint:gosec // restore test directory
	blocked, err := New(Config{
		Enabled: true, NodeID: "n", Address: "127.0.0.1:19802", ControlAddress: "127.0.0.1:19803",
		DataDir: readOnly, Secret: "s",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := blocked.Start(); err == nil {
		t.Fatal("Start in read-only dir expected error")
	}

	noPeers := newTestMesh(t, nil)
	noPeers.cfg.Peers = nil
	if err := noPeers.Close(); err != nil {
		t.Fatal(err)
	}
	if err := noPeers.Start(); err != nil {
		t.Fatalf("Start without bootstrap peers: %v", err)
	}
	if err := noPeers.Start(); err != nil {
		t.Fatalf("second Start error = %v", err)
	}
	if err := noPeers.Close(); err != nil {
		t.Fatal(err)
	}
	if err := noPeers.Close(); err != nil {
		t.Fatal(err)
	}
	if noPeers.Leader() == "" {
		if err := noPeers.WaitForLeader(50 * time.Millisecond); err == nil {
			t.Fatal("WaitForLeader without a leader expected error")
		}
	}
}

func TestMeshRelayErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "owner down", http.StatusBadGateway)
	}))
	defer server.Close()
	m := newTestMesh(t, &stubDispatcher{})
	m.cfg.Peers = append(m.cfg.Peers, Peer{ID: "owner", Address: "127.0.0.1:19899", ControlAddress: server.Listener.Addr().String()})
	applyCommand(t, m.fsm, command{Op: opSetOwner, AgentID: "a1", Owner: "owner"}, 1)
	if _, err := m.Dispatch(context.Background(), "a1", agentpb.TaskType_TASK_TYPE_NODENET_SNAPSHOT, nil, time.Second); err == nil {
		t.Fatal("relay HTTP error expected")
	}

	badJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer badJSON.Close()
	applyCommand(t, m.fsm, command{Op: opSetOwner, AgentID: "a2", Owner: "bad-json"}, 2)
	m.cfg.Peers = append(m.cfg.Peers, Peer{ID: "bad-json", Address: "127.0.0.1:19897", ControlAddress: badJSON.Listener.Addr().String()})
	if _, err := m.Dispatch(context.Background(), "a2", agentpb.TaskType_TASK_TYPE_NODENET_SNAPSHOT, nil, time.Second); err == nil {
		t.Fatal("invalid relay JSON expected error")
	}

	applyCommand(t, m.fsm, command{Op: opSetOwner, AgentID: "a3", Owner: "bad-url"}, 3)
	m.cfg.Peers = append(m.cfg.Peers, Peer{ID: "bad-url", Address: "127.0.0.1:19896", ControlAddress: "://bad"})
	if _, err := m.Dispatch(context.Background(), "a3", agentpb.TaskType_TASK_TYPE_NODENET_SNAPSHOT, nil, time.Second); err == nil {
		t.Fatal("invalid relay URL expected error")
	}

	empty := newTestMesh(t, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/mesh/dispatch", strings.NewReader(`{"agent_id":"a1"}`))
	req.Header.Set(AdminSecretHeader, "mesh-secret")
	empty.DispatchHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no dispatcher status = %d", rec.Code)
	}
}

func TestMeshSnapshotRestart(t *testing.T) {
	address := freePort(t)
	dir := t.TempDir()
	cfg := Config{
		Enabled: true, NodeID: "app-a", Address: address, ControlAddress: address,
		DataDir: dir, Secret: "s",
		Peers: []Peer{{ID: "app-a", Address: address, ControlAddress: address}},
	}
	m, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	if err := m.WaitForLeader(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := m.SetOwner("a1", "app-a"); err != nil {
		t.Fatal(err)
	}
	if err := m.raft.Snapshot().Error(); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Close() }()
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	if err := m.WaitForLeader(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	if got := m.Owner("a1"); got != "app-a" {
		t.Fatalf("owner after restart = %q", got)
	}
}

func TestOwnerHandlerAndAny(t *testing.T) {
	m := newTestMesh(t, nil)
	handler := m.OwnerHandler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/mesh/owner", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("owner auth = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/internal/mesh/owner", strings.NewReader("{bad"))
	req.Header.Set(AdminSecretHeader, "mesh-secret")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("owner bad json = %d", rec.Code)
	}
	body, _ := json.Marshal(ownerRequest{Op: "unknown", AgentID: "a1"})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/internal/mesh/owner", bytes.NewReader(body))
	req.Header.Set(AdminSecretHeader, "mesh-secret")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("owner unknown op = %d", rec.Code)
	}
	body, _ = json.Marshal(ownerRequest{Op: opSetOwner, AgentID: "a1", Owner: "center-a"})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/internal/mesh/owner", bytes.NewReader(body))
	req.Header.Set(AdminSecretHeader, "mesh-secret")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("owner set without raft = %d", rec.Code)
	}
	body, _ = json.Marshal(ownerRequest{Op: opClearOwner, AgentID: "a1"})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/internal/mesh/owner", bytes.NewReader(body))
	req.Header.Set(AdminSecretHeader, "mesh-secret")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("owner clear without raft = %d", rec.Code)
	}

	if err := m.SetOwnerAny("a1", "center-a", 0); err == nil {
		t.Fatal("SetOwnerAny without leader expected error")
	}
	if err := m.ClearOwnerAny("a1"); err == nil {
		t.Fatal("ClearOwnerAny without leader expected error")
	}
	applyCommand(t, m.fsm, command{Op: opSetOwner, AgentID: "a2", Owner: "center-a"}, 1)
	if err := m.SetOwnerAny("a2", "center-a", 0); err == nil {
		t.Fatal("SetOwnerAny without leader expected error")
	}
}

func TestOwnerAnyOnLeader(t *testing.T) {
	address := freePort(t)
	m, err := New(Config{
		Enabled: true, NodeID: "app-a", Address: address, ControlAddress: address,
		DataDir: t.TempDir(), Secret: "s",
		Peers: []Peer{{ID: "app-a", Address: address, ControlAddress: address}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Close() }()
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	if err := m.WaitForLeader(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := m.SetOwnerAny("a1", "app-a", time.Minute); err != nil {
		t.Fatal(err)
	}
	if m.Owner("a1") != "app-a" {
		t.Fatal("owner not recorded")
	}
	if err := m.ClearOwnerAny("a1"); err != nil {
		t.Fatal(err)
	}
	if m.Owner("a1") != "" {
		t.Fatal("owner not cleared")
	}
	adminBody, _ := json.Marshal(adminRequest{Op: "join", ID: "app-c", Address: "127.0.0.1:19892"})
	rec := httptest.NewRecorder()
	adminReq := httptest.NewRequest(http.MethodPost, "/internal/mesh/admin", bytes.NewReader(adminBody))
	adminReq.Header.Set(AdminSecretHeader, "s")
	m.AdminHandler().ServeHTTP(rec, adminReq)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("AdminHandler join = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := m.peerAddress("app-c"); got != "127.0.0.1:19892" {
		t.Fatalf("raft peer fallback address = %q", got)
	}
	if err := m.RemoveAny("app-c"); err != nil {
		t.Fatalf("RemoveAny(app-c) = %v", err)
	}
	if err := m.JoinAny("app-b", "127.0.0.1:19893", "", false); err != nil {
		t.Fatalf("JoinAny on leader = %v", err)
	}
	if err := m.PromoteAny("app-b"); err != nil {
		t.Fatalf("PromoteAny on leader = %v", err)
	}
	if err := m.RemoveAny("app-b"); err != nil {
		t.Fatalf("RemoveAny on leader = %v", err)
	}
}

func TestOwnerExpiry(t *testing.T) {
	fsm := NewFSM()
	applyCommand(t, fsm, command{
		Op: opSetOwner, AgentID: "a1", Owner: "center-a", ExpiresAt: time.Now().Add(30 * time.Millisecond).UnixMilli(),
	}, 1)
	if fsm.Owner("a1") != "center-a" {
		t.Fatal("owner missing before expiry")
	}
	time.Sleep(50 * time.Millisecond)
	if got := fsm.Owner("a1"); got != "" {
		t.Fatalf("expired owner = %q", got)
	}
}

func TestRaftLeaderFailover(t *testing.T) {
	if testing.Short() {
		t.Skip("raft integration test")
	}
	ids := []string{"center-a", "center-b", "witness"}
	addresses := []string{freePort(t), freePort(t), freePort(t)}
	peers := make([]Peer, len(ids))
	for i := range ids {
		peers[i] = Peer{ID: ids[i], Address: addresses[i], ControlAddress: addresses[i]}
	}
	nodes := make([]*Mesh, 0, len(ids))
	for i, id := range ids {
		m, err := New(Config{
			Enabled: true, NodeID: id, Address: addresses[i], ControlAddress: addresses[i],
			DataDir: t.TempDir(), Peers: peers, Secret: "mesh-secret", Witness: id == "witness",
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Start(); err != nil {
			t.Fatal(err)
		}
		nodes = append(nodes, m)
	}

	leaderIndex := -1
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		for i, m := range nodes {
			if m.IsLeader() {
				leaderIndex = i
				break
			}
		}
		if leaderIndex >= 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if leaderIndex < 0 {
		t.Fatal("no initial leader")
	}
	if err := nodes[leaderIndex].Close(); err != nil {
		t.Fatal(err)
	}

	remaining := make([]*Mesh, 0, len(nodes)-1)
	for i, m := range nodes {
		if i != leaderIndex {
			remaining = append(remaining, m)
		}
	}
	defer func() {
		for _, m := range remaining {
			_ = m.Close()
		}
	}()

	newLeader := -1
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		for i, m := range remaining {
			if m.IsLeader() {
				newLeader = i
				break
			}
		}
		if newLeader >= 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if newLeader < 0 {
		t.Fatal("no leader after failure")
	}
	if err := remaining[newLeader].SetOwner("agent-failover", ids[0]); err != nil {
		t.Fatalf("set owner after failover: %v", err)
	}
	// The surviving voter set is 2/3, so the owner must replicate to the peer.
	peer := remaining[(newLeader+1)%len(remaining)]
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if peer.Owner("agent-failover") == ids[0] {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("owner not replicated after failover: %q", peer.Owner("agent-failover"))
}

func TestAdminHandlerAndAny(t *testing.T) {
	m := newTestMesh(t, nil)
	handler := m.AdminHandler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/mesh/admin", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin auth = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/internal/mesh/admin", strings.NewReader("{bad"))
	req.Header.Set(AdminSecretHeader, "mesh-secret")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("admin bad json = %d", rec.Code)
	}
	body, _ := json.Marshal(adminRequest{Op: "unknown", ID: "x"})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/internal/mesh/admin", bytes.NewReader(body))
	req.Header.Set(AdminSecretHeader, "mesh-secret")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("admin unknown = %d", rec.Code)
	}
	body, _ = json.Marshal(adminRequest{Op: "remove", ID: "x"})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/internal/mesh/admin", bytes.NewReader(body))
	req.Header.Set(AdminSecretHeader, "mesh-secret")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("admin remove without raft = %d", rec.Code)
	}

	if err := m.JoinAny("x", "127.0.0.1:1", "", false); err == nil {
		t.Fatal("JoinAny without leader expected error")
	}
	if err := m.PromoteAny("x"); err == nil {
		t.Fatal("PromoteAny without leader expected error")
	}
	if err := m.RemoveAny("x"); err == nil {
		t.Fatal("RemoveAny without leader expected error")
	}
}

func TestAdminForwardingToLeader(t *testing.T) {
	leader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer leader.Close()
	m := newTestMesh(t, nil)
	m.controls["the-leader"] = leader.Listener.Addr().String()
	m.cfg.Peers = append(m.cfg.Peers, Peer{ID: "the-leader", Address: "127.0.0.1:19890", ControlAddress: leader.Listener.Addr().String()})
	// Without a raft leader the forwarder must fail fast.
	if err := m.JoinAny("x", "127.0.0.1:1", "", false); err == nil {
		t.Fatal("forward without known leader expected error")
	}
}
