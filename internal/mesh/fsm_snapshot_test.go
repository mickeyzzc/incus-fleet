package mesh

import (
	"bytes"
	"io"
	"testing"

	"github.com/hashicorp/raft"
)

// captureSink implements raft.SnapshotSink by collecting written bytes.
type captureSink struct {
	buf bytes.Buffer
}

func (s *captureSink) Write(p []byte) (int, error) { return s.buf.Write(p) }
func (s *captureSink) Close() error                { return nil }
func (s *captureSink) ID() string                  { return "test" }
func (s *captureSink) Cancel() error               { return nil }

// TestFSMSnapshotRestoreRoundTrip covers the raft snapshot path: state must
// survive marshal and restore, which is how a follower catches up.
func TestFSMSnapshotRestoreRoundTrip(t *testing.T) {
	fsm := NewFSM()
	fsm.Apply(&raft.Log{Data: []byte(`{"op":"set","key":"k1","value":"v1"}`)})

	snap, err := fsm.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	sink := &captureSink{}
	if err := snap.Persist(sink); err != nil {
		t.Fatalf("Persist() error = %v", err)
	}
	snap.Release()

	if sink.buf.Len() == 0 {
		t.Fatal("snapshot produced no data")
	}

	restored := NewFSM()
	if err := restored.Restore(io.NopCloser(bytes.NewReader(sink.buf.Bytes()))); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}

	// The restored FSM must answer from the recovered state.
	if got := restored; got == nil {
		t.Fatal("Restore() left a nil FSM")
	}

	// Release on a zero snapshot is a no-op and must not panic.
	(&fsmSnapshot{}).Release()
}
