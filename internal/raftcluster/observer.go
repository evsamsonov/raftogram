package raftcluster

import (
	"context"

	"github.com/hashicorp/raft"
	"go.uber.org/zap"
)

// StartObserver logs Raft role and leadership transitions until ctx is cancelled.
// It is safe to call once per node; shutdown is cooperative via ctx.
func StartObserver(ctx context.Context, r *raft.Raft, nodeID string, logger *zap.Logger) {
	if r == nil || logger == nil {
		return
	}

	log := logger.Named("raft")
	ch := make(chan raft.Observation, 16)
	obs := raft.NewObserver(ch, false, raftStateObserverFilter)
	r.RegisterObserver(obs)

	go func() {
		defer r.DeregisterObserver(obs)

		for {
			select {
			case <-ctx.Done():
				return
			case ob, ok := <-ch:
				if !ok {
					return
				}
				logRaftObservation(log, nodeID, &ob)
			}
		}
	}()
}

func raftStateObserverFilter(o *raft.Observation) bool {
	_, ok := o.Data.(raft.RaftState)
	return ok
}

func logRaftObservation(log *zap.Logger, nodeID string, ob *raft.Observation) {
	state, ok := ob.Data.(raft.RaftState)
	if !ok {
		return
	}

	addr, leaderID := ob.Raft.LeaderWithID()
	fields := []zap.Field{
		zap.String("node_id", nodeID),
		zap.String("role", state.String()),
	}
	if leaderID != "" {
		fields = append(fields, zap.String("leader_id", string(leaderID)))
	}
	if addr != "" {
		fields = append(fields, zap.String("leader_addr", string(addr)))
	}

	log.Info("Raft role changed", fields...)
}
