# Managed Service Reference

## Keywords
postgresql, mariadb, valkey, keydb, elasticsearch, kafka, nats, meilisearch, clickhouse, qdrant, typesense, object storage, shared storage, database, cache, search, queue, s3, connection string, mode, HA, NON_HA, backup, export, import, debug, mount, pg_dump, mysqldump

## TL;DR
Reference cards for all 13 Zerops managed services. Each card provides type, ports, env vars, connection pattern, HA specifics, and gotchas.

## PostgreSQL
**Type**: `postgresql@{17,16,14}` | **Mode**: optional (default NON_HA), immutable
**Ports**: 5432 (RW), 5433 (read replicas, HA only), 6432 (external TLS via pgBouncer)
**Env**: `hostname`, `port`, `user`, `password`, `connectionString`, `dbName`
**HA**: 1 primary + 2 read replicas, streaming replication (async), auto-failover
**Gotchas**: No internal TLS (only 6432). Don't modify `zps` user. Read replicas have async lag. Some libs need `postgres://` scheme.

## MariaDB
**Type**: `mariadb@10.6` | **Mode**: optional (default NON_HA), immutable
**Ports**: 3306 (fixed, no separate replica port)
**Env**: `hostname`, `port`, `user`, `password`, `connectionString`, `dbName`
**HA**: MaxScale routing, read/write splitting, async replication, auto-failover
**Gotchas**: No separate replica port (MaxScale routes on single port). No internal TLS. Don't modify `zps` user.

## Valkey
**Type**: `valkey@7.2` (MUST be 7.2 — 8 passes validation but fails import) | **Mode**: optional (default NON_HA), immutable
**Ports**: 6379 (RW), 6380 (RW TLS), 7000 (RO, HA only), 7001 (RO TLS, HA only)
**Env**: `hostname`, `port`, `password`, `connectionString`, `user`
**HA**: 1 master + 2 replicas. Zerops-specific: ports 6379/6380 on replicas forward to master (NOT native Valkey). Async replication.
**Gotchas**: Version MUST be 7.2. Port forwarding is Zerops-specific. Use 7000/7001 for direct read scaling. TLS ports for external/VPN only.

## KeyDB
**Type**: `keydb@6` | **Mode**: optional (NON_HA only)
**Ports**: 6379 | **Env**: same as Valkey
**DEPRECATED**: Do NOT use for new projects — use `valkey@7.2` instead. When user requests "Redis" or "cache", always use Valkey. Migration from KeyDB: only hostname changes.

## Elasticsearch
**Type**: `elasticsearch@8.16` | **Mode**: optional (default NON_HA), immutable
**Ports**: 9200 (HTTP only, no native transport)
**Env**: `hostname`, `port`, `password` (user always `elastic`)
**HA**: Multi-node cluster, automatic repair
**Config**: `PLUGINS` (comma-separated, restart required), `HEAP_PERCENT` (default 50%, range 1-100)
**Gotchas**: HTTP only internally. Min RAM 0.25 GB. Default user `elastic`. Tune `HEAP_PERCENT=75` for search-heavy.

## Object Storage
**Type**: `object-storage` or `objectstorage` (both valid, no version) | **Mode**: NOT REQUIRED
**Env**: `apiUrl`, `accessKeyId`, `secretAccessKey`, `bucketName`, `quotaGBytes`, `projectId`, `serviceId`, `hostname`
**Config**: `objectStorageSize: 1-100` GB, `objectStoragePolicy` or `objectStorageRawPolicy`, `priority: 10`
**Infrastructure**: runs on **independent infra** separate from other project services — accessible from any Zerops service or remotely over internet
**Bucket**: one auto-created per service (name = hostname + random prefix, **immutable**). Need multiple buckets? Create multiple object-storage services
**Policies**: `private` | `public-read` (list+get) | `public-objects-read` (get only, no listing) | `public-write` (put only) | `public-read-write` (full). Or use `objectStorageRawPolicy` with IAM Policy JSON (`{{ .BucketName }}` template variable available)
**Gotchas**: MinIO backend. **No Zerops backup**. `forcePathStyle: true` / `AWS_USE_PATH_STYLE_ENDPOINT: true` REQUIRED. Region `us-east-1` (required but ignored). No autoscaling, no verticalAutoscaling. Quota changeable in GUI after creation

## Shared Storage
**Type**: `shared-storage` (no version) | **Mode**: optional (default NON_HA), immutable
**Mount**: `/mnt/{hostname}` — add `mount: [hostname]` to runtime zerops.yml
**HA**: 1:1 replication, auto-failover
**Gotchas**: SeaweedFS backend. Max 60 GB. POSIX only (not S3). NON_HA = data loss on hardware failure.

## Kafka
**Type**: `kafka@3.8` | **Mode**: optional (default NON_HA), immutable
**Ports**: 9092 (SASL PLAIN), 8081 (Schema Registry if enabled)
**Env**: `hostname`, `user`, `password`
**HA**: 3 brokers, 6 partitions, replication factor 3, auto-repair
**Gotchas**: SASL PLAIN only (no anonymous). NON_HA = 1 broker, **no replication**. Indefinite topic retention (implement cleanup). 250 GB cap.

## NATS
**Type**: `nats@2.10` | **Mode**: optional (default NON_HA), immutable
**Ports**: 4222 (client), 8222 (HTTP monitoring)
**Env**: `hostname`, `user` (always `zerops`), `password`, `connectionString`
**Config**: `JET_STREAM_ENABLED` (default 1), `MAX_PAYLOAD` (default 8 MB, max 64 MB)
**Gotchas**: Config changes require restart. JetStream HA sync lag 1 min. Set `JET_STREAM_ENABLED=0` for core pub-sub only.

## Meilisearch
**Type**: `meilisearch@1.10` | **Mode**: optional (NON_HA only)
**Ports**: 7700
**Env**: `hostname`, `masterKey`, `defaultSearchKey`, `defaultAdminKey`
**Gotchas**: **No HA** (single-node only). Never expose `masterKey` to frontend — use `defaultSearchKey`.

## ClickHouse
**Type**: `clickhouse@25.3` | **Mode**: optional (default NON_HA), immutable
**Ports**: 9000 (native), 8123 (HTTP), 9004 (MySQL compat), 9005 (PostgreSQL compat)
**Env**: `hostname`, `port`, `portHttp`, `portMysql`, `portPostgresql`, `portNative`, `password`, `superUserPassword`, `dbName`
**HA**: 3 nodes, replication factor 3, cluster `zerops`
**Gotchas**: HA requires `ReplicatedMergeTree` (standard `MergeTree` won't replicate). Super user for backups.

## Qdrant
**Type**: `qdrant@{1.12,1.10}` | **Mode**: optional (default NON_HA), immutable
**Ports**: 6333 (HTTP), 6334 (gRPC)
**Env**: `hostname`, `port`, `grpcPort`, `apiKey`, `readOnlyApiKey`, `connectionString`, `grpcConnectionString`
**HA**: 3 nodes, `automaticClusterReplication=true` by default
**Gotchas**: No public access (internal only). Use 6334 for gRPC (higher perf for large vectors).

## Typesense
**Type**: `typesense@27.1` | **Mode**: optional (default NON_HA), immutable
**Ports**: 8108
**Env**: `hostname`, `port`, `apiKey` (immutable master key)
**HA**: 3-node Raft consensus, auto leader election, recovery up to 1 min
**Gotchas**: API key immutable. 1-min recovery — expect brief 503s. CORS always on.

## RabbitMQ
**Type**: `rabbitmq@3.9` | **Mode**: optional (default NON_HA), immutable
**Ports**: 5672 (AMQP), 15672 (management UI)
**Env**: `hostname`, `port`, `user`, `password`, `connectionString`
**Gotchas**: Management UI on port 15672. Use AMQP 0-9-1 protocol for client connections.

## Common Patterns

- **Mode**: optional (default NON_HA), immutable after creation (delete+recreate to change)
- **Hostname**: immutable, becomes internal DNS name
- **Internal**: HTTP/plain TCP only (no TLS) — TLS for external/VPN ports
- **Credentials**: auto-generated in env vars
- **System Users**: don't modify `zps`/`zerops`/`super`
- **VPN**: append `.zerops` to hostname
- **Backup**: PostgreSQL, MariaDB, Elasticsearch, Meilisearch, Qdrant, NATS (NOT Valkey/KeyDB, NOT ClickHouse — use SQL BACKUP)
- **Priority**: `priority: 10` for databases/storage to start before apps

## Service Operations

### Database Export/Import
- **PostgreSQL**: `pg_dump` for export, `psql` for import
- **MariaDB**: `mysqldump` for export, `mysql` for import
- Requires Zerops VPN or Adminer (built-in web DB tool)
- No SSL/TLS on internal connections — security provided by VPN tunnel
- Desktop tools (DBeaver, pgAdmin, HeidiSQL) connect via VPN using standard env vars

### Object Storage
- MinIO backend on **independent infrastructure**, one auto-named bucket per service (hostname + random prefix, immutable)
- S3-compatible access: `forcePathStyle: true` REQUIRED, region `us-east-1` (required but ignored by MinIO)
- Accessible from any Zerops service and remotely over internet
- Policies via `objectStoragePolicy`: `private`, `public-read`, `public-objects-read`, `public-write`, `public-read-write`. Custom IAM JSON via `objectStorageRawPolicy` (use `{{ .BucketName }}` template)
- Cross-service env prefix: `${hostname_apiUrl}`, `${hostname_accessKeyId}`, `${hostname_secretAccessKey}`, `${hostname_bucketName}`
- Quota (1-100 GB) changeable in GUI after creation. No autoscaling

### Shared Storage
- Mount in zerops.yml: `mount: [hostname]` — appears at `/mnt/{hostname}`
- SeaweedFS backend, POSIX-only (not S3-compatible)
- Max 60 GB per service

### Backup System
- **Supported**: PostgreSQL, MariaDB, Elasticsearch, Meilisearch, Qdrant, NATS, Shared Storage
- **NOT supported**: Valkey, KeyDB, ClickHouse (use native dump commands), Object Storage
- Default schedule: daily 00:00-01:00 UTC (configurable per service)
- Retention minimums: 7 daily, 4 weekly, 3 monthly; max 50 backups per service
- End-to-end encryption (X25519 per project)
- Deleted services: backups retained for 7-day grace period
- CLI: `zcli backup create <service>`
- Storage limits: 5 GB (Lightweight plan), 25 GB (Serious plan), 1 TiB max per project

### Debug Mode
- Available for build and runtime prepare phases (independent settings)
- Pause points: Disable, Before First Command, On Command Failure, After Last Command
- Commands: `zsc debug continue`, `zsc debug success`, `zsc debug fail`
- Max duration: 60 minutes (auto-cancelled after timeout)
- Use for diagnosing build failures or runtime prepare issues

## See Also
- zerops://foundation/grammar — universal rules
- zerops://foundation/runtimes — runtime deltas
- zerops://foundation/wiring — cross-service wiring templates
