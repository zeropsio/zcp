# Managed Service Reference

## Keywords
postgresql, mariadb, valkey, keydb, elasticsearch, kafka, nats, meilisearch, clickhouse, qdrant, typesense, object storage, shared storage, database, cache, search, queue, s3, wiring, connection string, env vars, mode, HA

## TL;DR
Reference cards for all 13 Zerops managed services plus wiring patterns. Each card provides ports, env vars, connection patterns, HA specifics, and critical gotchas.

## PostgreSQL
**Versions**: 17, 16, 14 | **Mode**: MANDATORY (NON_HA or HA), immutable
**Ports**: 5432 (primary RW), 5433 (read replicas, HA only), 6432 (external TLS via pgBouncer)
**Env vars**: `hostname`, `port`, `user`, `password`, `connectionString`, `dbName`
**Connection**: `postgresql://${db_user}:${db_password}@${db_hostname}:5432/${db_dbName}`
**HA**: 1 primary + 2 read replicas, streaming replication (async), automatic failover
**import.yml**: `type: postgresql@16`, `mode: NON_HA`
**Gotchas**: No internal TLS (only port 6432). Don't modify `zps` user. Read replicas have async lag — avoid write-then-read patterns. Some libraries need `postgres://` scheme.

## MariaDB
**Versions**: 10.6 | **Mode**: MANDATORY (NON_HA or HA), immutable
**Ports**: 3306 (fixed, no separate replica port)
**Env vars**: `hostname`, `port`, `user`, `password`, `connectionString`, `dbName`
**Connection**: `mysql://${db_user}:${db_password}@${db_hostname}:3306/${db_dbName}`
**HA**: MaxScale routing layer handles read/write splitting, async replication, automatic failover
**import.yml**: `type: mariadb@10.6`, `mode: HA`
**Gotchas**: No separate replica port (unlike PostgreSQL) — MaxScale routes on single port. No internal TLS. Don't modify `zps` user.

## Valkey
**Versions**: 7.2 (MUST use `valkey@7.2` — valkey@8 passes validation but fails import) | **Mode**: MANDATORY (NON_HA or HA), immutable
**Ports**: 6379 (RW non-TLS), 6380 (RW TLS), 7000 (RO non-TLS, HA only), 7001 (RO TLS, HA only)
**Env vars**: `hostname`, `port`, `password`, `connectionString`, `user`
**Connection**: `redis://${cache_user}:${cache_password}@${cache_hostname}:6379` (non-TLS), `rediss://...@...:6380` (TLS), port 7000 for read replicas
**HA**: 1 master + 2 replicas. Zerops-specific forwarding: ports 6379/6380 on replicas forward to current master (NOT native Valkey). Async replication (brief data loss on failover).
**import.yml**: `type: valkey@7.2`, `mode: HA`
**Gotchas**: Version MUST be 7.2. Ports 6379/6380 forwarding is Zerops-specific (not standard Redis/Valkey). Use ports 7000/7001 for direct read scaling. TLS ports only for external/VPN — not internal.

## KeyDB
**Versions**: 6 | **Mode**: MANDATORY (NON_HA only)
**Ports**: 6379
**Env vars**: `hostname`, `port`, `password`, `connectionString`, `user`
**Connection**: Same as Valkey/Redis
**import.yml**: `type: keydb@6`, `mode: NON_HA`
**Gotchas**: **DEPRECATED** — development stalled, use Valkey for all new projects. Migration to Valkey is straightforward (only hostname changes).

## Elasticsearch
**Versions**: 8.16 | **Mode**: MANDATORY (NON_HA or HA), immutable
**Ports**: 9200 (HTTP only, no native transport)
**Env vars**: `hostname`, `port`, `password` (user is always `elastic`)
**Connection**: `http://${search_hostname}:9200` with `Authorization: Basic elastic:${search_password}`
**HA**: Multiple nodes in cluster, automatic repair on node failure
**import.yml**: `type: elasticsearch@8.16`, `mode: HA`
**Special config**: `PLUGINS` env var (comma-separated, requires restart). `HEAP_PERCENT` env var (default 50%, range 1-100, requires restart).
**Gotchas**: HTTP only (no HTTPS internally). Min RAM 0.25 GB. 50% default heap may need tuning (set `HEAP_PERCENT=75` for search-heavy). Default user `elastic`.

## Object Storage
**Type**: `object-storage` (no version) | **Mode**: NOT REQUIRED
**Ports**: N/A (S3 API via `apiUrl`)
**Env vars**: `apiUrl`, `accessKeyId`, `secretAccessKey`, `bucketName`, `storageCdnUrl`
**Connection**: Endpoint `${storage_apiUrl}`, bucket `${storage_bucketName}`, keys `${storage_accessKeyId}`/`${storage_secretAccessKey}`, region `us-east-1` (required but ignored)
**import.yml**: `type: object-storage`, `objectStorageSize: 10` (1-100 GB), `objectStoragePolicy: public-read` (optional), `priority: 10`
**Gotchas**: Backend MinIO (S3-compatible). One bucket per service. **No Zerops backup**. `forcePathStyle: true` / `AWS_USE_PATH_STYLE_ENDPOINT: true` required for AWS SDKs. Quota 1-100 GB, no autoscaling.

## Shared Storage
**Type**: `shared-storage` (no version) | **Mode**: MANDATORY (NON_HA or HA), immutable
**Ports**: N/A (POSIX filesystem mount)
**Env vars**: None (mount point `/mnt/{hostname}`)
**Connection**: Filesystem mount at `/mnt/{hostname}`, referenced in zerops.yml mount section
**HA**: 1:1 replication across nodes, automatic failover
**import.yml**: `type: shared-storage`, `mode: HA`
**Usage**: Add `mount: [files]` to runtime service zerops.yml run section
**Gotchas**: Backend SeaweedFS. Max capacity 60 GB (use Object Storage for larger). POSIX only (not S3-compatible). NON_HA = data loss on hardware failure.

## Kafka
**Versions**: 3.8 | **Mode**: MANDATORY (NON_HA or HA), immutable
**Ports**: 9092 (data broker, SASL PLAIN), 8081 (Schema Registry if enabled)
**Env vars**: `hostname`, `user`, `password` (bootstrap `${events_hostname}:9092`)
**Connection**: `bootstrap.servers=${events_hostname}:9092`, `security.protocol=SASL_PLAINTEXT`, `sasl.mechanism=PLAIN`, `sasl.jaas.config=...PlainLoginModule required username="${events_user}" password="${events_password}";`
**HA**: 3 brokers, 6 partitions, replication factor 3, automatic repair
**import.yml**: `type: kafka@3.8`, `mode: HA`
**Gotchas**: SASL PLAIN only (no anonymous). NON_HA = 1 broker, **no replication** (never production). **Indefinite topic retention** (implement cleanup). 250GB storage cap.

## NATS
**Versions**: 2.10 | **Mode**: MANDATORY (NON_HA or HA), immutable
**Ports**: 4222 (client), 8222 (HTTP monitoring)
**Env vars**: `hostname`, `user` (always `zerops`), `password` (16-char auto-generated)
**Connection**: `nats://${messaging_user}:${messaging_password}@${messaging_hostname}:4222`
**HA**: Multi-node cluster, automatic route configuration
**import.yml**: `type: nats@2.10`, `mode: HA`
**Special config**: `JET_STREAM_ENABLED` (default 1), `MAX_PAYLOAD` (default 8MB, max 64MB)
**Gotchas**: Config changes require restart (no hot-reload). JetStream HA sync lag 1 minute. JetStream on by default — set `JET_STREAM_ENABLED=0` for core pub-sub only.

## Meilisearch
**Versions**: 1.10 | **Mode**: MANDATORY (NON_HA only)
**Ports**: 7700
**Env vars**: `hostname`, `masterKey` (admin), `defaultSearchKey` (frontend read-only), `defaultAdminKey` (backend full admin)
**Connection**: `http://${search_hostname}:7700` with `Authorization: Bearer ${search_masterKey}`
**HA**: N/A (single-node only — no clustering)
**import.yml**: `type: meilisearch@1.10`, `mode: NON_HA`
**Gotchas**: **No HA** (use Elasticsearch/Typesense for HA search). **Never expose `masterKey` to frontend** — use `defaultSearchKey`. Single-node data risk.

## ClickHouse
**Versions**: 25.3 | **Mode**: MANDATORY (NON_HA or HA), immutable
**Ports**: 9000 (native TCP), 8123 (HTTP), 9004 (MySQL protocol), 9005 (PostgreSQL protocol)
**Env vars**: `hostname`, `port`, `portHttp`, `portMysql`, `portPostgresql`, `portNative`, `password` (zerops user), `superUserPassword` (admin), `dbName`
**Connection**: Native `clickhouse://${analytics_user}:${analytics_password}@${analytics_hostname}:9000/${analytics_dbName}`, HTTP `http://...@...:8123`
**HA**: 3 nodes, replication factor 3, cluster `zerops` (1 shard, 3 replicas)
**import.yml**: `type: clickhouse@25.3`, `mode: HA`
**Gotchas**: HA requires `ReplicatedMergeTree` + `Replicated` database engine (standard `MergeTree` won't replicate). Super user required for backups. Choose right port: 9000 (native), 8123 (REST), 9004/9005 (MySQL/PostgreSQL compat).

## Qdrant
**Versions**: 1.12, 1.10 | **Mode**: MANDATORY (NON_HA or HA), immutable
**Ports**: 6333 (HTTP), 6334 (gRPC)
**Env vars**: `hostname`, `port`, `grpcPort`, `apiKey` (full), `readOnlyApiKey` (search only), `connectionString` (HTTP), `grpcConnectionString` (gRPC)
**Connection**: HTTP `http://${vectors_hostname}:6333` with `api-key: ${vectors_apiKey}`, gRPC `${vectors_hostname}:6334`
**HA**: 3 nodes, `automaticClusterReplication=true` by default
**import.yml**: `type: qdrant@1.12`, `mode: HA`
**Gotchas**: No public access (internal runtime only). Auto-replication on by default. Use 6334 for gRPC (higher performance for large vectors).

## Typesense
**Versions**: 27.1 | **Mode**: MANDATORY (NON_HA or HA), immutable
**Ports**: 8108
**Env vars**: `hostname`, `port`, `apiKey` (immutable master key)
**Connection**: `http://${search_hostname}:8108` with `x-typesense-api-key: ${search_apiKey}`
**HA**: 3-node Raft consensus cluster, automatic leader election, recovery up to 1 minute
**import.yml**: `type: typesense@27.1`, `mode: HA`
**Gotchas**: API key immutable (plan key rotation at app level). 1-minute recovery — expect brief 503s during failover. CORS always on.

## Common Patterns

**HA Mode**: All services with mode field — HA/NON_HA immutable after creation (delete+recreate to change)
**Hostname**: Fixed after creation (cannot change)
**Internal Connections**: HTTP/plain TCP (no TLS) — TLS only for external/VPN ports
**Credentials**: Auto-generated in env vars for all database/cache/queue services
**Cross-Service Reference**: `${<hostname>_<varName>}` in import.yml envSecrets
**System Users**: Don't modify `zps`/`zerops`/`super` system users
**Hostname as DNS**: Service hostname becomes internal DNS name
**VPN Resolution**: Append `.zerops` to hostname when connecting via VPN
**Backup Support**: PostgreSQL, MariaDB, Elasticsearch, Meilisearch, Qdrant, NATS (not Valkey/KeyDB, not ClickHouse — use SQL BACKUP)
**Version Syntax**: `type: <service>@<version>` in import.yml
**Priority**: Use `priority: 10` for databases/storage to start before apps

## Wiring Patterns

### Variable Reference Syntax

`${hostname_variablename}` — Underscores, not dashes (dashes in hostnames become underscores).

**Examples:**
```yaml
# PostgreSQL
DATABASE_URL: postgresql://${db_user}:${db_password}@db:5432/${db_dbName}

# Valkey/KeyDB
REDIS_URL: redis://${cache_password}@cache:6379

# Object Storage
S3_ENDPOINT: ${storage_apiUrl}
S3_KEY: ${storage_accessKeyId}
S3_SECRET: ${storage_secretAccessKey}
```

### envSecrets vs envVariables

- **envSecrets**: Sensitive data (passwords, tokens, certificates) — masked in GUI, cannot view after creation
- **envVariables**: Configuration (URLs, feature flags, public settings) — visible in GUI

### Concrete Wiring Example

```yaml
project:
  name: my-stack

services:
  - hostname: app
    type: nodejs@22
    envVariables:
      DB_HOST: db
      DB_NAME: ${db_dbName}
      REDIS_HOST: cache
    envSecrets:
      DB_PASSWORD: ${db_password}
      REDIS_PASSWORD: ${cache_password}

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10

  - hostname: cache
    type: valkey@7.2
    mode: NON_HA
    priority: 10
```

### Cross-Service Reference Gotchas

- **Hostname = internal DNS** — Service hostname becomes DNS name (`db`, `cache`, `storage`)
- **Project vars auto-inherited** — Do NOT re-reference in service env (shadows with service var)
- **Use hostname directly** — `db:5432`, NOT `${db_hostname}:5432` (both work, direct is simpler)
- **Password sync** — Changing DB password in GUI doesn't update env vars (manual sync needed)
- **NEVER use HTTPS internally** — SSL terminates at L7 balancer (`http://app:3000`, not `https://`)

## See Also
- zerops://foundation/core
- zerops://foundation/runtimes
