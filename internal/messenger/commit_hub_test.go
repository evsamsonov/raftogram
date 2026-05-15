package messenger

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/evsamsonov/raftogram/internal/gen/raftogrampb"
)

func TestCommitHubPublishBlocksWhenBufferFull(t *testing.T) {
	hub := commitHub{}
	commits, unregister := hub.register("general", 1)
	defer unregister()

	msg := &raftogrampb.StoredMessage{Sequence: 1, Payload: []byte("first")}
	hub.publish("general", msg)

	publishDone := make(chan struct{})
	go func() {
		hub.publish("general", &raftogrampb.StoredMessage{Sequence: 2, Payload: []byte("second")})
		close(publishDone)
	}()

	select {
	case <-publishDone:
		t.Fatal("publish should block until subscriber reads")
	case <-time.After(50 * time.Millisecond):
	}

	<-commits
	<-commits

	select {
	case <-publishDone:
	case <-time.After(time.Second):
		t.Fatal("publish did not unblock after reads")
	}
}

func TestFSMSubscribeCommitsReceivesNewCommits(t *testing.T) {
	fsm := NewDefaultFSM()

	applyEntry(t, fsm, 1, &raftogrampb.RaftLogEntry{
		WireVersion: CurrentWireVersion,
		Command: &raftogrampb.RaftLogEntry_CreateChannel{
			CreateChannel: &raftogrampb.CreateChannelCommand{ChannelId: "general"},
		},
	})

	commits, unregister := fsm.SubscribeCommits("general", 4)
	defer unregister()

	applyEntry(t, fsm, 2, &raftogrampb.RaftLogEntry{
		WireVersion: CurrentWireVersion,
		Command: &raftogrampb.RaftLogEntry_SendMessage{
			SendMessage: &raftogrampb.SendMessageCommand{
				ChannelId: "general",
				AuthorId:  "alice",
				Payload:   []byte("hello"),
			},
		},
	})

	select {
	case msg := <-commits:
		assert.Equal(t, uint64(1), msg.GetSequence())
		assert.Equal(t, "hello", string(msg.GetPayload()))
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for commit notification")
	}
}

func TestFSMSubscribeCommitsSkipsDeduplicatedApply(t *testing.T) {
	fsm := NewDefaultFSM()

	applyEntry(t, fsm, 1, &raftogrampb.RaftLogEntry{
		WireVersion: CurrentWireVersion,
		Command: &raftogrampb.RaftLogEntry_CreateChannel{
			CreateChannel: &raftogrampb.CreateChannelCommand{ChannelId: "general"},
		},
	})

	commits, unregister := fsm.SubscribeCommits("general", 4)
	defer unregister()

	applyEntry(t, fsm, 2, &raftogrampb.RaftLogEntry{
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

	applyEntry(t, fsm, 3, &raftogrampb.RaftLogEntry{
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

	require.Len(t, commits, 1)
	<-commits
}
