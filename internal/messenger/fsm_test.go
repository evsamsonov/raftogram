package messenger

import (
	"bytes"
	"errors"
	"testing"

	"github.com/hashicorp/raft"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/evsamsonov/raftogram/internal/gen/raftogrampb"
)

func TestFSMDeterministicApplyProducesIdenticalSnapshot(t *testing.T) {
	fsmA, err := NewFSM(1024)
	require.NoError(t, err)

	fsmB, err := NewFSM(1024)
	require.NoError(t, err)

	entries := []*raftogrampb.RaftLogEntry{
		{
			WireVersion: CurrentWireVersion,
			Command: &raftogrampb.RaftLogEntry_CreateChannel{
				CreateChannel: &raftogrampb.CreateChannelCommand{
					ChannelId: "general",
					Name:      "General",
				},
			},
		},
		{
			WireVersion: CurrentWireVersion,
			Command: &raftogrampb.RaftLogEntry_SendMessage{
				SendMessage: &raftogrampb.SendMessageCommand{
					ChannelId: "general",
					AuthorId:  "alice",
					Payload:   []byte("hello"),
				},
			},
		},
		{
			WireVersion: CurrentWireVersion,
			Command: &raftogrampb.RaftLogEntry_SendMessage{
				SendMessage: &raftogrampb.SendMessageCommand{
					ChannelId: "general",
					AuthorId:  "bob",
					Payload:   []byte("world"),
				},
			},
		},
	}

	for i, entry := range entries {
		index := uint64(i + 1)

		resA := applyEntry(t, fsmA, index, entry)
		resB := applyEntry(t, fsmB, index, entry)

		_, isErrA := resA.(error)
		_, isErrB := resB.(error)
		assert.False(t, isErrA)
		assert.False(t, isErrB)
	}

	snapshotA := snapshotState(t, fsmA)
	snapshotB := snapshotState(t, fsmB)
	assert.Equal(t, snapshotA, snapshotB)
}

func TestFSMApplyRejectsOversizedPayload(t *testing.T) {
	fsm, err := NewFSM(5)
	require.NoError(t, err)

	applyEntry(t, fsm, 1, &raftogrampb.RaftLogEntry{
		WireVersion: CurrentWireVersion,
		Command: &raftogrampb.RaftLogEntry_CreateChannel{
			CreateChannel: &raftogrampb.CreateChannelCommand{
				ChannelId: "general",
			},
		},
	})

	res := applyEntry(t, fsm, 2, &raftogrampb.RaftLogEntry{
		WireVersion: CurrentWireVersion,
		Command: &raftogrampb.RaftLogEntry_SendMessage{
			SendMessage: &raftogrampb.SendMessageCommand{
				ChannelId: "general",
				AuthorId:  "alice",
				Payload:   []byte("payload-too-big"),
			},
		},
	})

	err, ok := res.(error)
	require.True(t, ok)
	assert.ErrorIs(t, err, ErrPayloadTooLarge)
}

func TestFSMApplyDeduplicatesByClientMessageID(t *testing.T) {
	fsm, err := NewFSM(1024)
	require.NoError(t, err)

	applyEntry(t, fsm, 1, &raftogrampb.RaftLogEntry{
		WireVersion: CurrentWireVersion,
		Command: &raftogrampb.RaftLogEntry_CreateChannel{
			CreateChannel: &raftogrampb.CreateChannelCommand{
				ChannelId: "general",
			},
		},
	})

	first := applyEntry(t, fsm, 2, &raftogrampb.RaftLogEntry{
		WireVersion: CurrentWireVersion,
		Command: &raftogrampb.RaftLogEntry_SendMessage{
			SendMessage: &raftogrampb.SendMessageCommand{
				ChannelId:       "general",
				AuthorId:        "alice",
				Payload:         []byte("hello"),
				ClientMessageId: "msg-1",
			},
		},
	})

	second := applyEntry(t, fsm, 3, &raftogrampb.RaftLogEntry{
		WireVersion: CurrentWireVersion,
		Command: &raftogrampb.RaftLogEntry_SendMessage{
			SendMessage: &raftogrampb.SendMessageCommand{
				ChannelId:       "general",
				AuthorId:        "alice",
				Payload:         []byte("hello"),
				ClientMessageId: "msg-1",
			},
		},
	})

	firstResult, ok := first.(SendMessageResult)
	require.True(t, ok)
	secondResult, ok := second.(SendMessageResult)
	require.True(t, ok)
	assert.Equal(t, firstResult.Sequence, secondResult.Sequence)
	assert.False(t, firstResult.Deduplicated)
	assert.True(t, secondResult.Deduplicated)

	state := snapshotState(t, fsm)
	require.Len(t, state.GetChannels(), 1)
	require.Len(t, state.GetChannels()[0].GetMessages(), 1)
	assert.Equal(t, uint64(1), state.GetChannels()[0].GetMessages()[0].GetSequence())
	assert.Equal(t, "msg-1", state.GetChannels()[0].GetMessages()[0].GetClientMessageId())
}

func applyEntry(t *testing.T, fsm *FSM, index uint64, entry *raftogrampb.RaftLogEntry) interface{} {
	t.Helper()

	encoded, err := EncodeRaftLogEntry(entry)
	require.NoError(t, err)

	return fsm.Apply(&raft.Log{
		Index: index,
		Term:  1,
		Data:  encoded,
	})
}

func snapshotState(t *testing.T, fsm *FSM) *raftogrampb.SnapshotState {
	t.Helper()

	snapshot, err := fsm.Snapshot()
	require.NoError(t, err)

	sink := &memorySnapshotSink{}
	require.NoError(t, snapshot.Persist(sink))

	state := &raftogrampb.SnapshotState{}
	require.NoError(t, proto.Unmarshal(sink.Bytes(), state))

	return state
}

type memorySnapshotSink struct {
	bytes.Buffer
	cancelled bool
}

func (s *memorySnapshotSink) ID() string {
	return "memory"
}

func (s *memorySnapshotSink) Cancel() error {
	s.cancelled = true
	return nil
}

func (s *memorySnapshotSink) Close() error {
	if s.cancelled {
		return errors.New("snapshot cancelled")
	}
	return nil
}
