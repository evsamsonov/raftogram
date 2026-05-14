package messenger

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/evsamsonov/raftogram/internal/gen/raftogrampb"
)

const (
	// CurrentWireVersion is the protobuf wire format version stored in Raft log
	// entries. Bump when making incompatible command encoding changes.
	CurrentWireVersion = 1
)

var ErrUnsupportedWireVersion = errors.New("unsupported raft log wire version")

// EncodeRaftLogEntry marshals a command envelope to protobuf bytes for Raft.
func EncodeRaftLogEntry(entry *raftogrampb.RaftLogEntry) ([]byte, error) {
	if entry == nil {
		return nil, errors.New("raft log entry is nil")
	}

	if entry.WireVersion == 0 {
		entry.WireVersion = CurrentWireVersion
	}

	if entry.GetWireVersion() != CurrentWireVersion {
		return nil, fmt.Errorf("%w: got=%d want=%d", ErrUnsupportedWireVersion, entry.GetWireVersion(), CurrentWireVersion)
	}

	encoded, err := proto.Marshal(entry)
	if err != nil {
		return nil, fmt.Errorf("marshal raft log entry: %w", err)
	}

	return encoded, nil
}

// DecodeRaftLogEntry unmarshals protobuf bytes and validates wire version.
func DecodeRaftLogEntry(data []byte) (*raftogrampb.RaftLogEntry, error) {
	entry := &raftogrampb.RaftLogEntry{}
	if err := proto.Unmarshal(data, entry); err != nil {
		return nil, fmt.Errorf("unmarshal raft log entry: %w", err)
	}

	if entry.GetWireVersion() != CurrentWireVersion {
		return nil, fmt.Errorf("%w: got=%d want=%d", ErrUnsupportedWireVersion, entry.GetWireVersion(), CurrentWireVersion)
	}

	return entry, nil
}
