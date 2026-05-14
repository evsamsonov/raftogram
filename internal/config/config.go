// Package config defines cluster and server configuration with validation.
package config

import (
	"errors"
	"fmt"
	"time"
)

// Peer is a single cluster member's network addresses.
type Peer struct {
	// NodeID is the stable Raft voter identifier (e.g. "node1").
	NodeID string `yaml:"node_id"`
	// RaftAddr is the address used for Raft inter-node traffic (host:port).
	RaftAddr string `yaml:"raft_addr"`
	// GRPCAddr is the address exposed to clients (host:port).
	GRPCAddr string `yaml:"grpc_addr"`
}

// Cluster holds the identity and membership configuration for one node.
type Cluster struct {
	// NodeID is the stable identifier of this node; must match one entry in Peers.
	NodeID string `yaml:"node_id"`
	// RaftBindAddr is the address this node listens on for Raft traffic.
	RaftBindAddr string `yaml:"raft_bind_addr"`
	// GRPCBindAddr is the address this node listens on for client gRPC traffic.
	GRPCBindAddr string `yaml:"grpc_bind_addr"`
	// DataDir is the base directory for BoltDB files and Raft snapshots.
	DataDir string `yaml:"data_dir"`
	// Peers is the full static voting membership including this node.
	Peers []Peer `yaml:"peers"`
}

// RaftTuning holds optional overrides for Raft consensus parameters.
// Zero values use Hashicorp Raft library defaults.
type RaftTuning struct {
	// SnapshotInterval is how often to check whether a new snapshot should be
	// triggered. Default: 120s.
	SnapshotInterval time.Duration `yaml:"snapshot_interval"`
	// SnapshotThreshold is the minimum number of committed log entries since the
	// last snapshot before a new one is triggered. Default: 8192.
	SnapshotThreshold uint64 `yaml:"snapshot_threshold"`
	// TrailingLogs is the number of log entries retained after a snapshot for
	// fast follower catch-up. Default: 10240.
	TrailingLogs uint64 `yaml:"trailing_logs"`
}

// Config is the root configuration for a raftogram server.
type Config struct {
	Cluster Cluster    `yaml:"cluster"`
	Raft    RaftTuning `yaml:"raft"`
}

// Validate returns an error if the configuration is invalid.
//
// Rules enforced here:
//   - NodeID, RaftBindAddr, GRPCBindAddr, DataDir must be non-empty.
//   - At least one peer must be listed.
//   - The number of voting peers must be odd (quorum safety).
//   - NodeID must match exactly one peer entry.
//   - Every peer must have non-empty NodeID, RaftAddr and GRPCAddr.
func (c *Config) Validate() error {
	cl := c.Cluster

	if cl.NodeID == "" {
		return errors.New("cluster.node_id is required")
	}
	if cl.RaftBindAddr == "" {
		return errors.New("cluster.raft_bind_addr is required")
	}
	if cl.GRPCBindAddr == "" {
		return errors.New("cluster.grpc_bind_addr is required")
	}
	if cl.DataDir == "" {
		return errors.New("cluster.data_dir is required")
	}
	if len(cl.Peers) == 0 {
		return errors.New("cluster.peers must contain at least one entry")
	}

	// Odd quorum: reject configurations that cannot form a valid majority.
	if len(cl.Peers)%2 == 0 {
		return fmt.Errorf(
			"cluster.peers has %d voting members; an odd number is required to form a valid quorum",
			len(cl.Peers),
		)
	}

	self := false
	for i, p := range cl.Peers {
		if p.NodeID == "" {
			return fmt.Errorf("cluster.peers[%d]: node_id is required", i)
		}
		if p.RaftAddr == "" {
			return fmt.Errorf("cluster.peers[%d]: raft_addr is required", i)
		}
		if p.GRPCAddr == "" {
			return fmt.Errorf("cluster.peers[%d]: grpc_addr is required", i)
		}
		if p.NodeID == cl.NodeID {
			self = true
		}
	}

	if !self {
		return fmt.Errorf("cluster.node_id %q not found in cluster.peers", cl.NodeID)
	}

	return nil
}
