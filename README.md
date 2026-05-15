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

## Clean restart (bootstrap from scratch)

Stop all processes and remove data directories:

```bash
rm -rf ./data/node1 ./data/node2 ./data/node3
```

Then start all three nodes again — the cluster will be created from scratch.

## Custom configuration

See [`config.yaml.dist`](config.yaml.dist) for all options.
