package raftcluster

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestSnapshotLogMessage(t *testing.T) {
	assert.Equal(t, "Raft snapshot created", snapshotLogMessage("creating new snapshot"))
	assert.Equal(t, "Raft snapshot compacted", snapshotLogMessage("reaping snapshot"))
	assert.Equal(t, "failed to reap snapshot", snapshotLogMessage("failed to reap snapshot"))
}

func TestHclogKeyvalsToZap(t *testing.T) {
	fields := hclogKeyvalsToZap("path", "/data/snapshots/1", "error", errors.New("snap failed"))
	assert.Len(t, fields, 2)
}

func TestSnapshotHclogLoggerInfo(t *testing.T) {
	core := zaptest.NewLogger(t).Core()
	logger := zap.New(core)
	hclogger := newSnapshotHclogLogger(logger)

	hclogger.Info("creating new snapshot", "path", "/tmp/snap")
}
