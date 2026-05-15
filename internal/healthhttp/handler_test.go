package healthhttp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/evsamsonov/raftogram/internal/config"
	"github.com/evsamsonov/raftogram/internal/healthhttp"
	"github.com/evsamsonov/raftogram/internal/raftcluster"
)

type stubStatus struct {
	status raftcluster.NodeStatus
}

func (s stubStatus) Status() raftcluster.NodeStatus {
	return s.status
}

func testPeers() []config.Peer {
	return []config.Peer{
		{NodeID: "node1", RaftAddr: "127.0.0.1:7000", GRPCAddr: "127.0.0.1:9000"},
		{NodeID: "node2", RaftAddr: "127.0.0.1:7001", GRPCAddr: "127.0.0.1:9001"},
		{NodeID: "node3", RaftAddr: "127.0.0.1:7002", GRPCAddr: "127.0.0.1:9002"},
	}
}

func TestHandler_GET_health(t *testing.T) {
	tests := []struct {
		name       string
		status     raftcluster.NodeStatus
		wantRole   string
		wantLeader map[string]string
	}{
		{
			name: "leader",
			status: raftcluster.NodeStatus{
				Role:       "Leader",
				LeaderAddr: "127.0.0.1:7000",
				LeaderID:   "node1",
				IsLeader:   true,
			},
			wantRole: "leader",
			wantLeader: map[string]string{
				"leader_id":        "node1",
				"leader_raft_addr": "127.0.0.1:7000",
				"leader_grpc_addr": "127.0.0.1:9000",
			},
		},
		{
			name: "follower with known leader",
			status: raftcluster.NodeStatus{
				Role:       "Follower",
				LeaderAddr: "127.0.0.1:7001",
				LeaderID:   "node2",
				IsLeader:   false,
			},
			wantRole: "follower",
			wantLeader: map[string]string{
				"leader_id":        "node2",
				"leader_raft_addr": "127.0.0.1:7001",
				"leader_grpc_addr": "127.0.0.1:9001",
			},
		},
		{
			name: "candidate without leader",
			status: raftcluster.NodeStatus{
				Role:     "Candidate",
				IsLeader: false,
			},
			wantRole:   "candidate",
			wantLeader: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &healthhttp.Handler{
				NodeID: "node1",
				Peers:  testPeers(),
				Source: stubStatus{status: tt.status},
			}
			srv := httptest.NewServer(healthhttp.NewMux(h))
			t.Cleanup(srv.Close)

			resp, err := http.Get(srv.URL + "/health")
			require.NoError(t, err)
			t.Cleanup(func() { _ = resp.Body.Close() })

			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

			var body map[string]any
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

			assert.Equal(t, "node1", body["node_id"])
			assert.Equal(t, tt.wantRole, body["role"])
			assert.Equal(t, tt.status.IsLeader, body["is_leader"])

			if tt.wantLeader == nil {
				_, hasLeaderID := body["leader_id"]
				assert.False(t, hasLeaderID)
				return
			}

			for k, v := range tt.wantLeader {
				assert.Equal(t, v, body[k])
			}
		})
	}
}

func TestHandler_method_not_allowed(t *testing.T) {
	h := &healthhttp.Handler{
		NodeID: "node1",
		Peers:  testPeers(),
		Source: stubStatus{},
	}
	srv := httptest.NewServer(healthhttp.NewMux(h))
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/health", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}
