package raftcluster

import (
	"errors"
	"fmt"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	"github.com/hashicorp/raft"
	"go.uber.org/zap"

	"github.com/evsamsonov/raftogram/internal/config"
)

// Node wraps a running Raft instance and its TCP transport.
type Node struct {
	Raft      *raft.Raft
	transport *raft.NetworkTransport
}

// Open creates a TCP transport, optionally bootstraps the cluster from the
// static peer list in cfg, and starts the Raft node. stores must already be
// open. If the data directory contains existing Raft state the bootstrap step
// is skipped automatically.
//
// Snapshot thresholds from cfg.Raft override Raft library defaults when
// non-zero. Raft-internal logs are silenced here; key transitions are surfaced
// through zap via raftcluster.StartObserver and snapshot store logging.
func Open(cfg config.Config, fsm raft.FSM, stores *PersistentRaftStores, logger *zap.Logger) (*Node, error) {
	nullLog := hclog.NewNullLogger()

	transport, err := raft.NewTCPTransportWithLogger(
		cfg.Cluster.RaftBindAddr,
		nil, // advertise inferred from bind address
		3,   // max connection pool per peer
		10*time.Second,
		nullLog,
	)
	if err != nil {
		return nil, fmt.Errorf("create raft transport: %w", err)
	}

	raftCfg := raft.DefaultConfig()
	raftCfg.LocalID = raft.ServerID(cfg.Cluster.NodeID)
	raftCfg.Logger = nullLog

	if cfg.Raft.SnapshotInterval != 0 {
		raftCfg.SnapshotInterval = cfg.Raft.SnapshotInterval
	}
	if cfg.Raft.SnapshotThreshold != 0 {
		raftCfg.SnapshotThreshold = cfg.Raft.SnapshotThreshold
	}
	if cfg.Raft.TrailingLogs != 0 {
		raftCfg.TrailingLogs = cfg.Raft.TrailingLogs
	}

	servers := make([]raft.Server, len(cfg.Cluster.Peers))
	for i, p := range cfg.Cluster.Peers {
		servers[i] = raft.Server{
			ID:      raft.ServerID(p.NodeID),
			Address: raft.ServerAddress(p.RaftAddr),
		}
	}

	// BootstrapCluster writes the initial configuration to an empty log.
	// On every subsequent start it returns ErrCantBootstrap (existing state),
	// which is safe to ignore.
	err = raft.BootstrapCluster(
		raftCfg,
		stores.LogStore,
		stores.StableStore,
		stores.SnapshotStore,
		transport,
		raft.Configuration{Servers: servers},
	)
	if err != nil && !errors.Is(err, raft.ErrCantBootstrap) {
		if terr := transport.Close(); terr != nil {
			logger.Error("Close raft transport after bootstrap failure", zap.Error(terr))
		}
		return nil, fmt.Errorf("bootstrap raft cluster: %w", err)
	}

	r, err := raft.NewRaft(raftCfg, fsm, stores.LogStore, stores.StableStore, stores.SnapshotStore, transport)
	if err != nil {
		if terr := transport.Close(); terr != nil {
			logger.Error("Close raft transport after node init failure", zap.Error(terr))
		}
		return nil, fmt.Errorf("create raft node: %w", err)
	}

	logger.Info("Raft node started",
		zap.String("node_id", cfg.Cluster.NodeID),
		zap.String("raft_bind", cfg.Cluster.RaftBindAddr),
		zap.Int("peers", len(cfg.Cluster.Peers)),
	)

	return &Node{
		Raft:      r,
		transport: transport,
	}, nil
}

// Shutdown gracefully stops the Raft node and then closes the TCP transport.
func (n *Node) Shutdown() error {
	return errors.Join(
		n.Raft.Shutdown().Error(),
		closeNamed(n.transport, "raft transport"),
	)
}

// NodeStatus is a point-in-time snapshot of the observable Raft node state,
// suitable for health checks and operator dashboards.
type NodeStatus struct {
	// Role is the current Raft role: "Follower", "Candidate", or "Leader".
	Role string
	// LeaderAddr is the Raft bind address of the current cluster leader,
	// or an empty string when the leader is unknown.
	LeaderAddr string
	// LeaderID is the stable node identifier of the current leader,
	// or an empty string when the leader is unknown.
	LeaderID string
	// IsLeader reports whether this node is the leader and ready to accept
	// client mutations via Raft Apply.
	IsLeader bool
}

// Status returns a snapshot of the current node state. It is safe to call
// concurrently and from health-check handlers.
func (n *Node) Status() NodeStatus {
	addr, id := n.Raft.LeaderWithID()
	return NodeStatus{
		Role:       n.Raft.State().String(),
		LeaderAddr: string(addr),
		LeaderID:   string(id),
		IsLeader:   n.Raft.State() == raft.Leader,
	}
}
