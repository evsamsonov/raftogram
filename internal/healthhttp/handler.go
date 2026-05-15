package healthhttp

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/evsamsonov/raftogram/internal/config"
	"github.com/evsamsonov/raftogram/internal/raftcluster"
)

// StatusSource supplies a point-in-time Raft node status for health responses.
type StatusSource interface {
	Status() raftcluster.NodeStatus
}

// Handler serves GET /health for orchestrator liveness and Raft role visibility.
type Handler struct {
	NodeID string
	Peers  []config.Peer
	Source StatusSource
}

type healthResponse struct {
	NodeID         string `json:"node_id"`
	Role           string `json:"role"`
	IsLeader       bool   `json:"is_leader"`
	LeaderID       string `json:"leader_id,omitempty"`
	LeaderRaftAddr string `json:"leader_raft_addr,omitempty"`
	LeaderGRPCAddr string `json:"leader_grpc_addr,omitempty"`
}

// ServeHTTP implements http.Handler for GET /health only.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := json.Marshal(h.buildResponse())
	if err != nil {
		http.Error(w, "encode health response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (h *Handler) buildResponse() healthResponse {
	st := h.Source.Status()
	resp := healthResponse{
		NodeID:   h.NodeID,
		Role:     strings.ToLower(st.Role),
		IsLeader: st.IsLeader,
	}

	if st.LeaderID != "" {
		resp.LeaderID = st.LeaderID
		resp.LeaderRaftAddr = st.LeaderAddr
		resp.LeaderGRPCAddr = config.GRPCAddrForPeer(h.Peers, st.LeaderID)
	}

	return resp
}

// NewMux returns an http.ServeMux with GET /health registered.
func NewMux(h *Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /health", h)
	return mux
}
