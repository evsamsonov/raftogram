## ADDED Requirements

### Requirement: Deterministic command application

The system SHALL apply committed Raft log entries to messenger state using a deterministic finite state machine. Given an identical ordered sequence of committed commands, every node SHALL produce identical messenger state snapshots excluding explicitly allowed nondeterministic fields documented as non-replicated.

#### Scenario: Replayed log yields identical channel message order

- **WHEN** two nodes replay the same committed sequence containing send-message commands for one channel
- **THEN** the resulting message total order for that channel SHALL be identical on both nodes

### Requirement: Channel creation command

The system SHALL support a command that creates a channel with a unique identifier supplied or generated according to documented rules. Creating a channel with an identifier that already exists SHALL be handled as a documented idempotent no-op or explicit error consistent on all nodes.

#### Scenario: Duplicate create with same id is stable

- **WHEN** the same channel creation command with the same identifier is committed twice
- **THEN** all nodes SHALL converge on the same final existence flag for that channel without duplicate logical channels

### Requirement: Send message command

The system SHALL support a command that appends a message to a channel’s ordered history with author metadata, monotonic ordering per channel after commit, and enforced maximum message size.

#### Scenario: Oversized message rejected before commit

- **WHEN** a send-message command exceeds the configured maximum payload size
- **THEN** the system SHALL reject the command without committing a user-visible message and SHALL return a documented client error

#### Scenario: Message appended on valid command

- **WHEN** a valid send-message command is committed for an existing channel
- **THEN** the message SHALL appear exactly once in that channel’s history on all caught-up nodes

### Requirement: Optional client message id for idempotent send

The send-message command SHALL support an optional `client_message_id` field. When the field is present, the state machine SHALL treat the pair of channel identifier and `client_message_id` as an idempotency key: any additional committed send commands for the same channel with the same `client_message_id` SHALL NOT introduce a second distinct visible message in that channel’s history and SHALL converge to the same logical outcome as the first successful application.

#### Scenario: Retry with same client message id does not duplicate

- **WHEN** two send-message commands for the same channel with the same non-empty `client_message_id` are both committed in the log
- **THEN** the channel history SHALL contain exactly one user-visible message for that idempotency key on all caught-up nodes

#### Scenario: Sends without client message id are not deduplicated

- **WHEN** two send-message commands for the same channel omit `client_message_id` (or use an empty value as documented) and carry the same payload
- **THEN** the system SHALL apply both commands as distinct messages unless another documented rule applies

### Requirement: Snapshot and restore of messenger state

The system SHALL serialize messenger state together with the Raft snapshot mechanism so that a restored node can continue applying new commits without re-fetching full history from peers beyond what the snapshot already contains.

#### Scenario: Cold start from snapshot plus tail log

- **WHEN** a node restores from snapshot and then receives AppendEntries for subsequent indices
- **THEN** the node SHALL apply new commands on top of restored state and SHALL match the cluster’s authoritative history
