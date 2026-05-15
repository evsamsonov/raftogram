## ADDED Requirements

### Requirement: Leader-aware client routing

The client API SHALL surface whether the contacted node is the Raft leader for client-mutating operations. If a mutating request arrives on a non-leader, the system SHALL respond with a documented error that includes enough information for the client to retry against the current leader when known.

#### Scenario: Send on follower returns not leader

- **WHEN** a client invokes a mutating RPC against a follower that knows the leader address
- **THEN** the system SHALL return a not-leader style error and SHALL include the leader’s client endpoint hint when available

### Requirement: Create channel RPC

The system SHALL expose a documented RPC that creates a channel and returns its identifier for subsequent sends.

#### Scenario: Successful create returns id

- **WHEN** a client calls create channel with valid parameters on the leader
- **THEN** the system SHALL commit the creation command and SHALL return the channel identifier to the client

### Requirement: Send message RPC

The system SHALL expose a documented RPC that submits a message to a channel through Raft commit on the leader. The RPC SHALL accept an optional `client_message_id` and SHALL pass it through to the committed command so idempotency semantics in the state machine apply when the field is set.

#### Scenario: Successful send returns assigned sequence

- **WHEN** a client sends a valid message to an existing channel on the leader
- **THEN** the system SHALL commit the command and SHALL return a monotonic sequence or index for that message within the channel per documented semantics

#### Scenario: Optional idempotency key forwarded

- **WHEN** a client sends a message including a non-empty `client_message_id`
- **THEN** the committed command SHALL carry that identifier and the state machine SHALL apply idempotent-send rules per messenger specification

### Requirement: Read history RPC

The system SHALL expose a documented RPC that returns an ordered slice of messages for a channel up to a limit and cursor as documented.

#### Scenario: Paginated history is stable up to committed index

- **WHEN** a client requests history with a cursor pointing before the latest committed index
- **THEN** the system SHALL return messages in ascending order without gaps within the returned window relative to committed state at the time of the read

### Requirement: Streaming updates

The system SHALL support a server-initiated stream that delivers newly committed messages for a subscribed channel to the client with backpressure behavior documented.

#### Scenario: Stream delivers post-subscription commits

- **WHEN** a client opens a subscription stream for a channel on a serving node per documentation
- **THEN** the system SHALL deliver notifications for messages committed after the subscription start cursor per documented ordering rules
