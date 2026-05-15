package config

// GRPCAddrForPeer returns the client-routable gRPC address for nodeID from
// peers, or an empty string when the peer is not listed.
func GRPCAddrForPeer(peers []Peer, nodeID string) string {
	for i := range peers {
		if peers[i].NodeID == nodeID {
			return peers[i].GRPCAddr
		}
	}

	return ""
}
