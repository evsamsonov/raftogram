## ADDED Requirements

### Requirement: Cluster membership and node identity

The system SHALL assign each server a stable node identifier and persistent Raft address within a configured cluster. The system SHALL reject or isolate configurations that do not form a valid odd-sized voting quorum for production operation.

#### Scenario: Valid odd quorum accepted

- **WHEN** the operator provides a cluster configuration with an odd number of voting members and unique node IDs
- **THEN** the system SHALL start Raft networking and participate in elections according to that configuration

#### Scenario: Even voting members rejected at startup

- **WHEN** the operator provides a voting configuration with an even number of members
- **THEN** the system SHALL fail fast at startup with a clear error and SHALL NOT begin serving client traffic

### Requirement: Leader election and single leader invariant

The system SHALL elect at most one leader for a given term that can commit new log entries. Followers SHALL NOT commit new client-originated commands without forwarding them to the leader according to the client API rules.

#### Scenario: Leader steps down on loss of quorum

- **WHEN** the leader loses connectivity to a majority of voting members for longer than the configured election timeout
- **THEN** the system SHALL transition to follower or candidate as specified by Raft and SHALL stop acknowledging new commits until a leader is re-established

### Requirement: Durable Raft log and snapshots

The system SHALL persist Raft log entries and required metadata to stable storage before acknowledging commits. The system SHALL support installing snapshots to followers that lag beyond the retained log prefix and SHALL recover correctly after process restart.

#### Scenario: Restart preserves committed entries

- **WHEN** a minority of nodes restart after commits were acknowledged to the state machine
- **THEN** after recovery the cluster SHALL preserve all previously committed entries and SHALL NOT diverge on replay

#### Scenario: Lagging follower catches up via snapshot

- **WHEN** a follower’s log prefix is compacted on the leader and the follower is too far behind for AppendEntries
- **THEN** the system SHALL transfer a snapshot to the follower and the follower SHALL resume replication without manual intervention

### Requirement: Observability of cluster role

The system SHALL expose the current Raft role (follower, candidate, leader) and leader address (if known) through a documented health or metrics interface for operators.

#### Scenario: Health reports non-leader

- **WHEN** a node is a follower with a known leader
- **THEN** the health interface SHALL indicate follower role and SHALL include the leader’s reachable identifier expected by clients for routing
