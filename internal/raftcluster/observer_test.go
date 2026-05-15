package raftcluster

import (
	"testing"

	"github.com/hashicorp/raft"
	"github.com/stretchr/testify/assert"
)

func TestRaftStateObserverFilter(t *testing.T) {
	assert.True(t, raftStateObserverFilter(&raft.Observation{Data: raft.Follower}))
	assert.False(t, raftStateObserverFilter(&raft.Observation{
		Data: raft.LeaderObservation{LeaderID: "node-1"},
	}))
}
