// Package messenger implements the deterministic Raft FSM that holds messenger
// state: channels and their ordered message histories.
package messenger

import (
	"io"

	"github.com/hashicorp/raft"
)

// FSM is the Raft finite-state machine for the messenger service.
// Apply, Snapshot, and Restore are implemented in task 3.
type FSM struct{}

var _ raft.FSM = (*FSM)(nil)

// Apply is called by the Raft library once a log entry is committed. It applies
// the command to the in-memory state and returns an application-specific result.
func (f *FSM) Apply(_ *raft.Log) interface{} { return nil }

// Snapshot returns a point-in-time snapshot of the current FSM state.
func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
	return &fsmSnapshot{}, nil
}

// Restore replaces the current FSM state with the contents of the snapshot.
func (f *FSM) Restore(rc io.ReadCloser) error {
	return rc.Close()
}

type fsmSnapshot struct{}

func (s *fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	return sink.Close()
}

func (s *fsmSnapshot) Release() {}
