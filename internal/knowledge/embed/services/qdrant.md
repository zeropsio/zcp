# Qdrant on Zerops

## Keywords
qdrant, vector, vector database, similarity search, embeddings, grpc, ai search, semantic search

## TL;DR
Qdrant on Zerops is **internal-only** (no public access), uses HTTP (6333) and gRPC (6334) ports, and auto-replicates across all HA nodes by default.

## Zerops-Specific Behavior
- Ports:
  - **6333** — HTTP API (`port`)
  - **6334** — gRPC API (`grpcPort`)
- API keys: `apiKey` (full access), `readOnlyApiKey` (search only)
- Connection strings: `connectionString` (HTTP), `grpcConnectionString` (gRPC)
- **Internal access only** — cannot be exposed publicly

## HA Mode (3 nodes)
- `automaticClusterReplication=true` by default — creates replicas on all nodes automatically
- Can disable: `automaticClusterReplication=false`
- Automatic cluster recovery and node replacement

## NON_HA Mode
- Single node, ideal for development
- Simple deployment

## Backup
Native snapshotting: `.snapshot` format (compressed), taken from primary node.

## Configuration
```yaml
# import.yaml
services:
  - hostname: vectors
    type: qdrant@1.12
    mode: HA
```

## Gotchas
1. **No public access**: Qdrant cannot be exposed to the internet — access only through your runtime service
2. **Auto-replication is on by default**: Collections auto-replicate to all nodes — disable if you want manual control
3. **Two API ports**: Use 6333 for HTTP REST, 6334 for gRPC (higher performance for large vector operations)

## See Also
- zerops://decisions/choose-search
- zerops://services/meilisearch
- zerops://platform/backup
