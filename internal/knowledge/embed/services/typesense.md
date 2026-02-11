# Typesense on Zerops

## Keywords
typesense, search, full-text, autocomplete, raft, typo tolerance, api key, fast search

## TL;DR
Typesense uses Raft consensus for HA (3 nodes), has an immutable master API key, CORS enabled by default, and data persists at `/var/lib/typesense`.

## Zerops-Specific Behavior
- API key: Auto-generated via `apiKey` env var — **cannot be changed after creation**
- Data directory: `/var/lib/typesense` (auto-persisted to disk)
- CORS: Enabled by default (safe for frontend direct access)
- Defaults: `thread-pool-size: 16`, `num-collections-parallel-load: 8`
- HTTPS access: Via integrated Nginx proxy layer with load balancing

## HA Mode (Raft Consensus)
- 3-node cluster by default
- Built-in data synchronization
- Automatic leader election
- Recovery: Up to 1 minute during failures
- During failover: Temporary 503/500 responses (auto-resolves)

## NON_HA Mode
- Data persistence not guaranteed on node failures
- Lower resource requirements

## Node Access
- Standard: `node{n}.db.{hostname}.zerops`
- Stable DNS: `node-stable-{n}.db.{hostname}.zerops` (maintains IP until node retirement)

## Configuration
```yaml
# import.yaml
services:
  - hostname: search
    type: typesense@27.1
    mode: HA
```

## Gotchas
1. **API key is immutable**: Cannot change `apiKey` after service creation — plan key rotation at application level
2. **1-minute recovery window**: Expect brief 503s during HA failover — implement retry logic in your app
3. **CORS is always on**: No way to disable — if you need to restrict, use your runtime as a proxy

## See Also
- zerops://decisions/choose-search
- zerops://services/meilisearch
- zerops://services/elasticsearch
