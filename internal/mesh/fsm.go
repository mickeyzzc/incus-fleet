package mesh

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/hashicorp/raft"
)

const (
	opSetOwner   = "set-owner"
	opClearOwner = "clear-owner"
)

type command struct {
	Op        string `json:"op"`
	AgentID   string `json:"agent_id"`
	Owner     string `json:"owner,omitempty"`
	ExpiresAt int64  `json:"expires_at,omitempty"`
}

type ownerRecord struct {
	Owner     string `json:"owner"`
	ExpiresAt int64  `json:"expires_at,omitempty"`
}

type fsmState struct {
	Owners map[string]ownerRecord `json:"owners"`
	Epoch  uint64                 `json:"epoch"`
}

// FSM is the replicated state: which center owns each agent connection.
type FSM struct {
	mu    sync.RWMutex
	state fsmState
}

// NewFSM builds an empty replicated state.
func NewFSM() *FSM {
	return &FSM{state: fsmState{Owners: make(map[string]ownerRecord)}}
}

// Apply implements raft.FSM.
func (f *FSM) Apply(log *raft.Log) any {
	var cmd command
	if err := json.Unmarshal(log.Data, &cmd); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch cmd.Op {
	case opSetOwner:
		if cmd.AgentID == "" || cmd.Owner == "" {
			return fmt.Errorf("mesh: invalid set-owner command")
		}
		f.state.Owners[cmd.AgentID] = ownerRecord{Owner: cmd.Owner, ExpiresAt: cmd.ExpiresAt}
	case opClearOwner:
		delete(f.state.Owners, cmd.AgentID)
	default:
		return fmt.Errorf("mesh: unknown command %q", cmd.Op)
	}
	if log.Index > f.state.Epoch {
		f.state.Epoch = log.Index
	}
	return nil
}

// Owner returns the current owner of an agent.
func (f *FSM) Owner(agentID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	record, ok := f.state.Owners[agentID]
	if !ok {
		return ""
	}
	if record.ExpiresAt > 0 && time.Now().UnixMilli() > record.ExpiresAt {
		delete(f.state.Owners, agentID)
		return ""
	}
	return record.Owner
}

// Epoch returns the highest applied log index, used as a fencing hint.
func (f *FSM) Epoch() uint64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.state.Epoch
}

// Reset clears replicated state after a forced single-node recovery.
func (f *FSM) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = fsmState{Owners: make(map[string]ownerRecord)}
}

type fsmSnapshot struct {
	data []byte
}

// Persist implements raft.FSMSnapshot.
func (s *fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	if _, err := sink.Write(s.data); err != nil {
		_ = sink.Cancel()
		return err
	}
	return sink.Close()
}

// Release implements raft.FSMSnapshot.
func (s *fsmSnapshot) Release() {}

// Snapshot implements raft.FSM.
func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	data, err := json.Marshal(f.state)
	if err != nil {
		return nil, err
	}
	return &fsmSnapshot{data: data}, nil
}

// Restore implements raft.FSM.
func (f *FSM) Restore(reader io.ReadCloser) error {
	defer func() { _ = reader.Close() }()
	var state fsmState
	if err := json.NewDecoder(reader).Decode(&state); err != nil {
		return err
	}
	if state.Owners == nil {
		state.Owners = make(map[string]ownerRecord)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = state
	return nil
}
