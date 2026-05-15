// Package grpcapi implements the client-facing Messenger gRPC service.
package grpcapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hashicorp/raft"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/evsamsonov/raftogram/internal/config"
	"github.com/evsamsonov/raftogram/internal/gen/raftogrampb"
	"github.com/evsamsonov/raftogram/internal/messenger"
)

const defaultRaftApplyTimeout = 30 * time.Second
const (
	defaultReadHistoryLimit uint32 = 100
	maxReadHistoryLimit     uint32 = 1000

	// SubscribeBackpressureBufferSize is the per-subscription queue capacity
	// between Raft commit notifications and gRPC stream.Send. When full,
	// FSM Apply blocks in commitHub.Publish (upstream backpressure). Slow
	// clients are further throttled by gRPC HTTP/2 flow control when Send blocks.
	SubscribeBackpressureBufferSize = 64

	// subscribeSendBatchMax is the maximum messages per SubscribeChunk when
	// additional commits are already waiting in the subscription queue.
	subscribeSendBatchMax = 32
)

// Server implements raftogrampb.MessengerServer. Mutations use Raft Apply on
// the leader. ReadHistory and Subscribe serve from the leader's applied state
// after a Raft barrier. Subscribe streams post-subscription commits with bounded
// buffering documented by SubscribeBackpressureBufferSize.
type Server struct {
	raftogrampb.UnimplementedMessengerServer

	raft         *raft.Raft
	fsm          *messenger.FSM
	peers        []config.Peer
	applyTimeout time.Duration
}

// NewServer returns a Messenger gRPC handler that applies commands through Raft
// when this node is the leader, and returns NotLeader details on followers.
func NewServer(r *raft.Raft, fsm *messenger.FSM, peers []config.Peer) *Server {
	return &Server{
		raft:         r,
		fsm:          fsm,
		peers:        peers,
		applyTimeout: defaultRaftApplyTimeout,
	}
}

func (s *Server) CreateChannel(
	ctx context.Context,
	req *raftogrampb.CreateChannelRequest,
) (*raftogrampb.CreateChannelResponse, error) {
	if err := s.leaderCheck(ctx); err != nil {
		return nil, err
	}

	entry := &raftogrampb.RaftLogEntry{
		Command: &raftogrampb.RaftLogEntry_CreateChannel{
			CreateChannel: &raftogrampb.CreateChannelCommand{
				ChannelId: req.GetChannelId(),
				Name:      req.GetName(),
			},
		},
	}

	data, err := messenger.EncodeRaftLogEntry(entry)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode command: %v", err)
	}

	fut := s.raft.Apply(data, s.applyTimeout)
	if err := fut.Error(); err != nil {
		return nil, raftApplyError(err)
	}

	switch res := fut.Response().(type) {
	case error:
		return nil, fsmErrorToStatus(res)
	case messenger.CreateChannelResult:
		name := req.GetName()
		if !res.Created {
			if n, ok := s.fsm.ChannelName(res.ChannelID); ok {
				name = n
			}
		}

		return &raftogrampb.CreateChannelResponse{
			ChannelId: res.ChannelID,
			Name:      name,
			Created:   res.Created,
		}, nil
	default:
		return nil, status.Errorf(codes.Internal, "unexpected apply response type %T", fut.Response())
	}
}

func (s *Server) SendMessage(
	ctx context.Context,
	req *raftogrampb.SendMessageRequest,
) (*raftogrampb.SendMessageResponse, error) {
	if err := s.leaderCheck(ctx); err != nil {
		return nil, err
	}

	entry := &raftogrampb.RaftLogEntry{
		Command: &raftogrampb.RaftLogEntry_SendMessage{
			SendMessage: &raftogrampb.SendMessageCommand{
				ChannelId:       req.GetChannelId(),
				AuthorId:        req.GetAuthorId(),
				Payload:         req.GetPayload(),
				ClientMessageId: req.GetClientMessageId(),
			},
		},
	}

	data, err := messenger.EncodeRaftLogEntry(entry)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode command: %v", err)
	}

	fut := s.raft.Apply(data, s.applyTimeout)
	if err := fut.Error(); err != nil {
		return nil, raftApplyError(err)
	}

	switch res := fut.Response().(type) {
	case error:
		return nil, fsmErrorToStatus(res)
	case messenger.SendMessageResult:
		return &raftogrampb.SendMessageResponse{
			Sequence:     res.Sequence,
			Deduplicated: res.Deduplicated,
		}, nil
	default:
		return nil, status.Errorf(codes.Internal, "unexpected apply response type %T", fut.Response())
	}
}

func (s *Server) ReadHistory(
	ctx context.Context,
	req *raftogrampb.ReadHistoryRequest,
) (*raftogrampb.ReadHistoryResponse, error) {
	if err := s.leaderCheck(ctx); err != nil {
		return nil, err
	}

	barrierFuture := s.raft.Barrier(s.applyTimeout)
	if err := barrierFuture.Error(); err != nil {
		return nil, raftBarrierError(err, s.notLeaderStatus())
	}

	messages, hasMore, err := s.fsm.ReadHistory(
		req.GetChannelId(),
		req.GetAfterSequence(),
		normalizeReadHistoryLimit(req.GetLimit()),
	)
	if err != nil {
		return nil, fsmErrorToStatus(err)
	}

	return &raftogrampb.ReadHistoryResponse{
		Messages: messages,
		HasMore:  hasMore,
	}, nil
}

func (s *Server) Subscribe(req *raftogrampb.SubscribeRequest, stream raftogrampb.Messenger_SubscribeServer) error {
	ctx := stream.Context()
	if err := s.leaderCheck(ctx); err != nil {
		return err
	}

	channelID := req.GetChannelId()
	if channelID == "" {
		return fsmErrorToStatus(messenger.ErrMissingChannelID)
	}

	barrierFuture := s.raft.Barrier(s.applyTimeout)
	if err := barrierFuture.Error(); err != nil {
		return raftBarrierError(err, s.notLeaderStatus())
	}

	if _, ok := s.fsm.ChannelName(channelID); !ok {
		return fsmErrorToStatus(fmt.Errorf("%w: %s", messenger.ErrChannelNotFound, channelID))
	}

	commits, unregister := s.fsm.SubscribeCommits(channelID, SubscribeBackpressureBufferSize)
	defer unregister()

	afterSequence := req.GetAfterSequence()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-commits:
			if !ok {
				return nil
			}
			if msg.GetSequence() <= afterSequence {
				continue
			}

			batch, err := subscribeBatch(ctx, commits, msg, afterSequence, subscribeSendBatchMax)
			if err != nil {
				return err
			}

			if err := stream.Send(&raftogrampb.SubscribeChunk{Messages: batch}); err != nil {
				return err
			}
		}
	}
}

func subscribeBatch(
	ctx context.Context,
	commits <-chan *raftogrampb.StoredMessage,
	first *raftogrampb.StoredMessage,
	afterSequence uint64,
	maxBatch int,
) ([]*raftogrampb.StoredMessage, error) {
	batch := []*raftogrampb.StoredMessage{first}

	for len(batch) < maxBatch {
		select {
		case <-ctx.Done():
			return batch, ctx.Err()
		case msg, ok := <-commits:
			if !ok {
				return batch, nil
			}
			if msg.GetSequence() > afterSequence {
				batch = append(batch, msg)
			}
		default:
			return batch, nil
		}
	}

	return batch, nil
}

func (s *Server) leaderCheck(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if s.raft.State() == raft.Leader {
		return nil
	}

	return s.notLeaderStatus().Err()
}

func (s *Server) notLeaderStatus() *status.Status {
	_, leaderID := s.raft.LeaderWithID()
	hint := &raftogrampb.NotLeader{
		RaftLeaderId:     string(leaderID),
		LeaderGrpcTarget: config.GRPCAddrForPeer(s.peers, string(leaderID)),
	}

	base := status.New(codes.FailedPrecondition, "not leader: retry on suggested leader client endpoint")
	withDetails, err := base.WithDetails(hint)
	if err != nil {
		return base
	}

	return withDetails
}

func raftApplyError(err error) error {
	if err == nil {
		return nil
	}

	// Leadership loss or transport issues surface as generic failures to clients.
	return status.Error(codes.Unavailable, fmt.Sprintf("raft apply: %v", err))
}

func raftBarrierError(err error, notLeader *status.Status) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, raft.ErrNotLeader) {
		return notLeader.Err()
	}

	return status.Error(codes.Unavailable, fmt.Sprintf("raft barrier: %v", err))
}

func normalizeReadHistoryLimit(limit uint32) uint32 {
	switch {
	case limit == 0:
		return defaultReadHistoryLimit
	case limit > maxReadHistoryLimit:
		return maxReadHistoryLimit
	default:
		return limit
	}
}

func fsmErrorToStatus(err error) error {
	switch {
	case errors.Is(err, messenger.ErrChannelNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, messenger.ErrMissingChannelID):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, messenger.ErrPayloadTooLarge):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, messenger.ErrUnsupportedWireVersion):
		return status.Error(codes.Internal, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}
