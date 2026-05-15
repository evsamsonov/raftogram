# raftogram

Distributed messenger built on Raft: a static cluster with an **odd** number of voting nodes, gRPC for clients, and HTTP `/health` to inspect node role and leader.

## Requirements

- [Go](https://go.dev/dl/) **1.26+** (see `go` in `go.mod`)
- `make` (for `make build`, `make test`)

## Build

```bash
make build
# or
go build -o raftogram ./cmd/raftogram
```

The binary is written to `raftogram` in the current directory when using `go build -o raftogram`.

Run flags:

| Flag / variable                                | Description                           |
| ---------------------------------------------- | ------------------------------------- |
| `-config` / `RAFTOGRAM_CONFIG`                 | path to YAML (default: `config.yaml`) |
| `-dev` / `RAFTOGRAM_DEV` (any non-empty value) | human-readable logs                   |

## Local dev cluster

Use an **odd** `cluster.peers` count (example below: 3 nodes). Copy the template once per node and adjust `node_id`, addresses, and `data_dir`:

```bash
cp config.yaml.dist config.node1.yaml
cp config.yaml.dist config.node2.yaml
cp config.yaml.dist config.node3.yaml
```

Per-node values (`peers` stays the same on all three):

| Node  | Raft             | gRPC             | Health           | `data_dir`     |
| ----- | ---------------- | ---------------- | ---------------- | -------------- |
| node1 | `127.0.0.1:7000` | `127.0.0.1:9000` | `127.0.0.1:8080` | `./data/node1` |
| node2 | `127.0.0.1:7001` | `127.0.0.1:9001` | `127.0.0.1:8081` | `./data/node2` |
| node3 | `127.0.0.1:7002` | `127.0.0.1:9002` | `127.0.0.1:8082` | `./data/node3` |

Run **one process per terminal** from the repository root:

```bash
./raftogram -config config.node1.yaml -dev
```

```bash
./raftogram -config config.node2.yaml -dev
```

```bash
./raftogram -config config.node3.yaml -dev
```

### Startup order

**Order does not matter**: membership is defined statically in `peers` in every config.

- On the **first** start with empty `data_dir` directories, the cluster bootstraps.
- Nodes that are already running accept the rest as they connect.
- Wait for leader election (usually a few seconds after all three nodes are up).

Liveness checks use HTTP **GET** `/health` on each node’s `health_bind_addr` (see below).

## Verify the cluster

```bash
curl -s http://127.0.0.1:8080/health | jq .
curl -s http://127.0.0.1:8081/health | jq .
curl -s http://127.0.0.1:8082/health | jq .
```

Expected JSON fields:

| Field                                               | Meaning                              |
| --------------------------------------------------- | ------------------------------------ |
| `node_id`                                           | this node’s id (`node1` … `node3`)   |
| `role`                                              | `leader`, `follower`, or `candidate` |
| `is_leader`                                         | `true` only on the leader node       |
| `leader_id`, `leader_raft_addr`, `leader_grpc_addr` | set when the leader is known         |

In a healthy cluster, exactly **one** node has `"role": "leader"` and `"is_leader": true`; the others have `"role": "follower"` with the same `leader_*` fields pointing at the leader.

Example leader response:

```json
{
  "node_id": "node1",
  "role": "leader",
  "is_leader": true,
  "leader_id": "node1",
  "leader_raft_addr": "127.0.0.1:7000",
  "leader_grpc_addr": "127.0.0.1:9000"
}
```

Client gRPC traffic goes to `leader_grpc_addr` (see design in `openspec/changes/raftogram/design.md`).

## Client API with grpcurl

[gRPCurl](https://github.com/fullstorydev/grpcurl) calls the `Messenger` service from the command line. Reflection is not enabled on the server; pass the proto file on every invocation.

Install (macOS / Linux):

```bash
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest
```

Run from the repository root. Every example uses the same flags; only the host (`host:port`) and `-d` body change.

Mutations (`CreateChannel`, `SendMessage`) must hit the **leader**; `ReadHistory` and `Subscribe` do too in the current implementation. Find it with:

```bash
curl -s http://127.0.0.1:8080/health | jq -r .leader_grpc_addr
# → 127.0.0.1:9000  (example; use the value you get)
```

The examples below use `127.0.0.1:9000` — replace with your `leader_grpc_addr` if the leader is another node.

List methods:

```bash
grpcurl -plaintext -import-path api/proto -proto raftogram/v1/messaging.proto \
  127.0.0.1:9000 list raftogram.v1.Messenger
```

### CreateChannel

Creates a channel on the Raft log. Omit `channel_id` to let the server assign one (`channel-<log index>`).

```bash
grpcurl -plaintext -import-path api/proto -proto raftogram/v1/messaging.proto \
  -d '{"name":"general"}' \
  127.0.0.1:9000 raftogram.v1.Messenger/CreateChannel
```

With an explicit id:

```bash
grpcurl -plaintext -import-path api/proto -proto raftogram/v1/messaging.proto \
  -d '{"channel_id":"general","name":"General"}' \
  127.0.0.1:9000 raftogram.v1.Messenger/CreateChannel
```

Example response:

```json
{
  "channelId": "general",
  "name": "General",
  "created": true
}
```

Save `channelId` for the calls below (or use your own id consistently).

### SendMessage

Appends a message to a channel. `payload` is raw bytes on the wire; in JSON it is **base64** (`SGVsbG8=` is `Hello`).

```bash
grpcurl -plaintext -import-path api/proto -proto raftogram/v1/messaging.proto \
  -d "{
    \"channel_id\": \"general\",
    \"author_id\": \"alice\",
    \"payload\": \"$(echo -n 'Hello' | base64)\",
    \"client_message_id\": \"msg-1\"
  }" \
  127.0.0.1:9000 raftogram.v1.Messenger/SendMessage
```

Example response:

```json
{
  "sequence": "1",
  "deduplicated": false
}
```

Repeating the same `client_message_id` in the same channel returns the original `sequence` with `"deduplicated": true`.

### ReadHistory

Returns committed messages with `sequence > after_sequence`, ascending. Default limit is 100 (server cap 1000).

```bash
grpcurl -plaintext -import-path api/proto -proto raftogram/v1/messaging.proto \
  -d '{"channel_id":"general","after_sequence":"0","limit":50}' \
  127.0.0.1:9000 raftogram.v1.Messenger/ReadHistory
```

Example response:

```json
{
  "messages": [
    {
      "sequence": "1",
      "authorId": "alice",
      "payload": "SGVsbG8=",
      "clientMessageId": "msg-1"
    }
  ],
  "hasMore": false
}
```

In protobuf JSON mapping, `uint64` fields such as `sequence` appear as strings; decode `payload` with `base64 -d`.

### Subscribe

Server-streaming RPC: prints `SubscribeChunk` messages as they are committed. Use the leader address; keep the process running and send more messages from another terminal to see chunks.

```bash
grpcurl -plaintext -import-path api/proto -proto raftogram/v1/messaging.proto \
  -d '{"channel_id":"general","after_sequence":"0"}' \
  127.0.0.1:9000 raftogram.v1.Messenger/Subscribe
```

Stop with Ctrl+C.

### Calling a follower (not leader)

Unary calls against a follower return `FailedPrecondition` with a `NotLeader` status detail (leader hint when the cluster knows the leader):

```bash
grpcurl -plaintext -import-path api/proto -proto raftogram/v1/messaging.proto \
  -d '{"name":"general"}' \
  127.0.0.1:9001 raftogram.v1.Messenger/CreateChannel
```

Example error (grpcurl prints decoded details):

```text
Code: FailedPrecondition
Message: not leader: retry on suggested leader client endpoint
Details:
1)	{
      "@type": "type.googleapis.com/raftogram.v1.NotLeader",
      "leaderGrpcTarget": "127.0.0.1:9000",
      "raftLeaderId": "node1"
    }
```

Retry on `leaderGrpcTarget`, or use `leader_grpc_addr` from `/health`.

## Clean restart (bootstrap from scratch)

Stop all processes and remove data directories:

```bash
rm -rf ./data/node1 ./data/node2 ./data/node3
```

Then start all three nodes again — the cluster will be created from scratch.

## Custom configuration

See [`config.yaml.dist`](config.yaml.dist) for all options.
