package grpcapi

import (
	"context"
	"testing"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	"github.com/hashicorp/raft"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/evsamsonov/raftogram/internal/config"
	"github.com/evsamsonov/raftogram/internal/gen/raftogrampb"
	"github.com/evsamsonov/raftogram/internal/messenger"
)

func TestReadHistoryLeaderUsesCursorAndLimit(t *testing.T) {
	srv, cleanup := newTestServer(t, true)
	defer cleanup()

	ctx := context.Background()
	_, err := srv.CreateChannel(ctx, &raftogrampb.CreateChannelRequest{
		ChannelId: "general",
		Name:      "General",
	})
	require.NoError(t, err)

	for i, payload := range []string{"m1", "m2", "m3"} {
		_, sendErr := srv.SendMessage(ctx, &raftogrampb.SendMessageRequest{
			ChannelId: "general",
			AuthorId:  "alice",
			Payload:   []byte(payload),
		})
		require.NoError(t, sendErr, "send index %d", i)
	}

	firstPage, err := srv.ReadHistory(ctx, &raftogrampb.ReadHistoryRequest{
		ChannelId:     "general",
		AfterSequence: 1,
		Limit:         1,
	})
	require.NoError(t, err)
	require.Len(t, firstPage.GetMessages(), 1)
	assert.Equal(t, uint64(2), firstPage.GetMessages()[0].GetSequence())
	assert.Equal(t, "m2", string(firstPage.GetMessages()[0].GetPayload()))
	assert.True(t, firstPage.GetHasMore())

	secondPage, err := srv.ReadHistory(ctx, &raftogrampb.ReadHistoryRequest{
		ChannelId:     "general",
		AfterSequence: firstPage.GetMessages()[0].GetSequence(),
		Limit:         10,
	})
	require.NoError(t, err)
	require.Len(t, secondPage.GetMessages(), 1)
	assert.Equal(t, uint64(3), secondPage.GetMessages()[0].GetSequence())
	assert.Equal(t, "m3", string(secondPage.GetMessages()[0].GetPayload()))
	assert.False(t, secondPage.GetHasMore())
}

func TestSubscribeOnLeaderReceivesPostSubscriptionCommits(t *testing.T) {
	srv, cleanup := newTestServer(t, true)
	defer cleanup()

	ctx := context.Background()
	_, err := srv.CreateChannel(ctx, &raftogrampb.CreateChannelRequest{
		ChannelId: "general",
		Name:      "General",
	})
	require.NoError(t, err)

	subCtx, cancelSubscribe := context.WithCancel(ctx)
	defer cancelSubscribe()

	chunks := make(chan *raftogrampb.SubscribeChunk, 8)
	subscribeDone := make(chan error, 1)
	go func() {
		subscribeDone <- srv.Subscribe(&raftogrampb.SubscribeRequest{
			ChannelId:     "general",
			AfterSequence: 0,
		}, &testSubscribeStream{ctx: subCtx, chunks: chunks})
	}()

	require.Eventually(t, func() bool {
		_, sendErr := srv.SendMessage(ctx, &raftogrampb.SendMessageRequest{
			ChannelId: "general",
			AuthorId:  "alice",
			Payload:   []byte("live"),
		})
		if sendErr != nil {
			return false
		}

		select {
		case chunk := <-chunks:
			return len(chunk.GetMessages()) >= 1 &&
				string(chunk.GetMessages()[0].GetPayload()) == "live"
		default:
			return false
		}
	}, 2*time.Second, 20*time.Millisecond)

	cancelSubscribe()
	assert.ErrorIs(t, <-subscribeDone, context.Canceled)
}

func TestSubscribeOnFollowerReturnsNotLeader(t *testing.T) {
	srv, cleanup := newTestServer(t, false)
	defer cleanup()

	err := srv.Subscribe(&raftogrampb.SubscribeRequest{
		ChannelId: "general",
	}, &testSubscribeStream{ctx: context.Background(), chunks: make(chan *raftogrampb.SubscribeChunk)})
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestReadHistoryOnFollowerReturnsNotLeader(t *testing.T) {
	srv, cleanup := newTestServer(t, false)
	defer cleanup()

	_, err := srv.ReadHistory(context.Background(), &raftogrampb.ReadHistoryRequest{
		ChannelId: "general",
		Limit:     10,
	})
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

func newTestServer(t *testing.T, bootstrap bool) (*Server, func()) {
	t.Helper()

	fsm := messenger.NewDefaultFSM(nil)

	raftCfg := raft.DefaultConfig()
	raftCfg.LocalID = raft.ServerID("node1")
	raftCfg.Logger = hclog.NewNullLogger()

	logStore := raft.NewInmemStore()
	stableStore := raft.NewInmemStore()
	snapshotStore := raft.NewInmemSnapshotStore()
	addr, transport := raft.NewInmemTransport("node1")

	r, err := raft.NewRaft(raftCfg, fsm, logStore, stableStore, snapshotStore, transport)
	require.NoError(t, err)

	if bootstrap {
		future := r.BootstrapCluster(raft.Configuration{
			Servers: []raft.Server{
				{
					ID:      raft.ServerID("node1"),
					Address: addr,
				},
			},
		})
		require.NoError(t, future.Error())
		require.NoError(t, waitForLeader(r, 5*time.Second))
	}

	srv := NewServer(r, fsm, []config.Peer{
		{
			NodeID:   "node1",
			GRPCAddr: "127.0.0.1:9001",
		},
	})

	cleanup := func() {
		assert.NoError(t, r.Shutdown().Error())
		assert.NoError(t, transport.Close())
	}

	return srv, cleanup
}

type testSubscribeStream struct {
	ctx    context.Context
	chunks chan *raftogrampb.SubscribeChunk
}

func (s *testSubscribeStream) Context() context.Context {
	return s.ctx
}

func (s *testSubscribeStream) Send(chunk *raftogrampb.SubscribeChunk) error {
	s.chunks <- chunk
	return nil
}

func (s *testSubscribeStream) SetHeader(metadata.MD) error  { return nil }
func (s *testSubscribeStream) SendHeader(metadata.MD) error { return nil }
func (s *testSubscribeStream) SetTrailer(metadata.MD)       {}
func (s *testSubscribeStream) SendMsg(any) error            { return nil }
func (s *testSubscribeStream) RecvMsg(any) error            { return nil }

func waitForLeader(r *raft.Raft, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if r.State() == raft.Leader {
			return nil
		}

		time.Sleep(10 * time.Millisecond)
	}

	return context.DeadlineExceeded
}
