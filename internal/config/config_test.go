package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/evsamsonov/raftogram/internal/config"
)

func validConfig() *config.Config {
	return &config.Config{
		Cluster: config.Cluster{
			NodeID:         "node1",
			RaftBindAddr:   "127.0.0.1:7000",
			GRPCBindAddr:   "127.0.0.1:9000",
			HealthBindAddr: "127.0.0.1:8080",
			DataDir:        "/tmp/node1",
			Peers: []config.Peer{
				{NodeID: "node1", RaftAddr: "127.0.0.1:7000", GRPCAddr: "127.0.0.1:9000"},
				{NodeID: "node2", RaftAddr: "127.0.0.1:7001", GRPCAddr: "127.0.0.1:9001"},
				{NodeID: "node3", RaftAddr: "127.0.0.1:7002", GRPCAddr: "127.0.0.1:9002"},
			},
		},
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*config.Config)
		wantErr bool
	}{
		{
			name:    "valid 3-node odd quorum",
			mutate:  nil,
			wantErr: false,
		},
		{
			name: "even voting members rejected",
			mutate: func(c *config.Config) {
				c.Cluster.Peers = append(c.Cluster.Peers, config.Peer{
					NodeID:   "node4",
					RaftAddr: "127.0.0.1:7003",
					GRPCAddr: "127.0.0.1:9003",
				})
			},
			wantErr: true,
		},
		{
			name:    "missing node_id",
			mutate:  func(c *config.Config) { c.Cluster.NodeID = "" },
			wantErr: true,
		},
		{
			name:    "node_id not in peers",
			mutate:  func(c *config.Config) { c.Cluster.NodeID = "unknown" },
			wantErr: true,
		},
		{
			name:    "peer missing raft_addr",
			mutate:  func(c *config.Config) { c.Cluster.Peers[1].RaftAddr = "" },
			wantErr: true,
		},
		{
			name:    "missing raft_bind_addr",
			mutate:  func(c *config.Config) { c.Cluster.RaftBindAddr = "" },
			wantErr: true,
		},
		{
			name:    "missing grpc_bind_addr",
			mutate:  func(c *config.Config) { c.Cluster.GRPCBindAddr = "" },
			wantErr: true,
		},
		{
			name:    "missing health_bind_addr",
			mutate:  func(c *config.Config) { c.Cluster.HealthBindAddr = "" },
			wantErr: true,
		},
		{
			name:    "missing data_dir",
			mutate:  func(c *config.Config) { c.Cluster.DataDir = "" },
			wantErr: true,
		},
		{
			name:    "no peers",
			mutate:  func(c *config.Config) { c.Cluster.Peers = nil },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			if tt.mutate != nil {
				tt.mutate(cfg)
			}

			err := cfg.Validate()

			if tt.wantErr {
				require.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
