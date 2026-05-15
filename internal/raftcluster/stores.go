package raftcluster

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb"
	"go.uber.org/zap"
)

// PersistentRaftStores wires BoltDB-backed Raft log and stable stores plus a
// local file snapshot store under a single data directory.
type PersistentRaftStores struct {
	LogStore      raft.LogStore
	StableStore   raft.StableStore
	SnapshotStore raft.SnapshotStore

	logDB    *raftboltdb.BoltStore
	stableDB *raftboltdb.BoltStore
}

// OpenPersistentRaftStores creates cluster.data_dir if needed, opens separate
// BoltDB files for the Raft log and stable store, and a FileSnapshotStore in
// data_dir/snapshots. When logger is non-nil, snapshot create/compact events are
// written as structured zap logs.
func OpenPersistentRaftStores(dataDir string, logger *zap.Logger) (*PersistentRaftStores, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	logPath := filepath.Join(dataDir, "raft-log.db")
	stablePath := filepath.Join(dataDir, "raft-stable.db")
	snapDir := filepath.Join(dataDir, "snapshots")

	if err := os.MkdirAll(snapDir, 0o750); err != nil {
		return nil, fmt.Errorf("create snapshot dir: %w", err)
	}

	logDB, err := raftboltdb.NewBoltStore(logPath)
	if err != nil {
		return nil, fmt.Errorf("open raft log store: %w", err)
	}

	stableDB, err := raftboltdb.NewBoltStore(stablePath)
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("open raft stable store: %w", err),
			closeNamed(logDB, "raft log store"),
		)
	}

	snapStore, err := raft.NewFileSnapshotStoreWithLogger(snapDir, 2, newSnapshotHclogLogger(logger))
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("open file snapshot store: %w", err),
			closeNamed(stableDB, "raft stable store"),
			closeNamed(logDB, "raft log store"),
		)
	}

	return &PersistentRaftStores{
		LogStore:      logDB,
		StableStore:   stableDB,
		SnapshotStore: snapStore,
		logDB:         logDB,
		stableDB:      stableDB,
	}, nil
}

// Close releases BoltDB handles. Call after the Raft node has shut down or if
// startup failed after a successful OpenPersistentRaftStores.
func (s *PersistentRaftStores) Close() error {
	if s == nil {
		return nil
	}

	return errors.Join(
		closeNamed(s.logDB, "raft log store"),
		closeNamed(s.stableDB, "raft stable store"),
	)
}

// closeNamed closes c and wraps any error with a descriptive name. Returns nil
// on success, so callers can pass it directly to errors.Join.
func closeNamed(c io.Closer, name string) error {
	if err := c.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}
	return nil
}
