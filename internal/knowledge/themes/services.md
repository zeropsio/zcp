# Managed Service Reference

## TL;DR
Reference cards for all 14 Zerops managed services. Each card provides type, ports, env vars, wiring templates, connection pattern, HA specifics, and gotchas.

## Keywords
postgresql, mariadb, valkey, keydb, elasticsearch, kafka, nats, meilisearch, clickhouse, qdrant, typesense, rabbitmq, object storage, shared storage, database, cache, search, queue, s3, connection string, mode, HA, NON_HA, backup, export, import, debug, mount, pg_dump, mysqldump, wiring, env vars, envSecrets, envVariables, cross-service, reference

## Wiring Syntax

- **Hostname substitution**: In templates below, each service uses a sample hostname (e.g., `db`, `cache`, `search`). Replace it with your actual service hostname. The syntax `${hostname_varname}` is real Zerops cross-service reference syntax — `hostname` must match the target service hostname exactly, with dashes converted to underscores.
- **Reference**: `${hostname_variablename}` — dashes in hostnames become underscores
- **envSecrets**: passwords, tokens, keys (blurred in GUI by default, editable/deletable)
- **import.yml service level**: ONLY `envSecrets` and `dotEnvSecrets` exist. There is NO `envVariables` at service level (only at project level). Put ALL connection vars in `envSecrets`.
- **Hostname = DNS**: use hostname directly for host (`db`, NOT `${db_hostname}`), but use `${db_port}` for port
- **Internal**: ALWAYS `http://` — NEVER `https://` (SSL at L7 balancer)
- **Project vars**: auto-inherited by all services — do NOT re-reference (creates shadow)
- **Password sync**: changing DB password in GUI does NOT update env vars (manual sync)

**CRITICAL: Wire credentials in import.yml** — Managed services auto-generate credentials but they are NOT automatically available to runtime services. You MUST wire them via `envSecrets` on the **runtime service** in import.yml (there is no `envVariables` at service level in import.yml):

```yaml
services:
  - hostname: mydb
    type: mariadb@10.6
    mode: NON_HA
    priority: 10

  - hostname: myapp
    type: nodejs@22
    envSecrets:
      DB_HOST: mydb
      DB_PORT: ${mydb_port}
      DB_NAME: ${mydb_dbName}
      DB_USER: ${mydb_user}
      DB_PASSWORD: ${mydb_password}
```

Without this wiring, the runtime service has no way to connect to managed services.

## Service Wiring Templates

Below, **VARS** = config values, **SECRETS** = credentials. **CRITICAL: In import.yml, put ALL of them (both VARS and SECRETS) in `envSecrets`.** There is no `envVariables` at service level in import.yml — using it will silently drop the values. In zerops.yml, use `run.envVariables` for VARS. Replace sample hostnames (`db`, `cache`, etc.) with your actual service hostname.

## PostgreSQL
**Type**: `postgresql@{18,17,16,14}` | **Mode**: optional (default NON_HA), immutable
**Ports**: 5432 (RW), 5433 (read replicas, HA only), 6432 (external TLS via pgBouncer)
**Env**: `hostname`, `port`, `user`, `password`, `connectionString`, `dbName`
**HA**: 1 primary + 2 read replicas, streaming replication (async), auto-failover
**Gotchas**: No internal TLS (only 6432). Don't modify `zps` user. Read replicas have async lag. Some libs need `postgres://` scheme.
**Wiring** (sample hostname: `db`):
**VARS**: `DB_HOST: db` `DB_PORT: ${db_port}` `DB_NAME: ${db_dbName}`
**SECRETS**: `DATABASE_URL: postgresql://${db_user}:${db_password}@db:${db_port}/${db_dbName}`

## MariaDB
**Type**: `mariadb@10.6` | **Mode**: optional (default NON_HA), immutable
**Ports**: 3306 (fixed, no separate replica port)
**Env**: `hostname`, `port`, `user`, `password`, `connectionString`, `dbName`
**HA**: MaxScale routing, read/write splitting, async replication, auto-failover
**Gotchas**: No separate replica port (MaxScale routes on single port). No internal TLS. Don't modify `zps` user.
**Wiring** (sample hostname: `db`):
**VARS**: `DB_HOST: db` `DB_PORT: ${db_port}` `DB_NAME: ${db_dbName}`
**SECRETS**: `DATABASE_URL: mysql://${db_user}:${db_password}@db:${db_port}/${db_dbName}`

## Valkey
**Type**: `valkey@7.2` (MUST be 7.2 -- 8 passes validation but fails import) | **Mode**: optional (default NON_HA), immutable
**Ports**: 6379 (RW), 6380 (RW TLS), 7000 (RO, HA only), 7001 (RO TLS, HA only)
**Env**: `hostname`, `port`, `connectionString`, `portTls` — NO `user` or `password` (unauthenticated)
**HA**: 1 master + 2 replicas. Zerops-specific: ports 6379/6380 on replicas forward to master (NOT native Valkey). Async replication.
**Gotchas**: Version MUST be 7.2. **No authentication** — connection is `redis://hostname:6379` without credentials. Do NOT reference `${cache_user}` or `${cache_password}` — they don't exist. Port forwarding is Zerops-specific. Use 7000/7001 for direct read scaling. TLS ports for external/VPN only.
**Wiring** (sample hostname: `cache`):
**VARS**: `REDIS_URL: redis://cache:${cache_port}`

## KeyDB
**Type**: `keydb@6` | **Mode**: optional (NON_HA only)
**Ports**: 6379 | **Env**: same as Valkey (no user/password)
**DEPRECATED**: Do NOT use for new projects -- use `valkey@7.2` instead. When user requests "Redis" or "cache", always use Valkey. Migration from KeyDB: only hostname changes.
**Wiring** (sample hostname: `cache`):
**VARS**: `REDIS_URL: redis://cache:${cache_port}`

## Elasticsearch
**Type**: `elasticsearch@{9.2,8.16}` | **Mode**: optional (default NON_HA), immutable
**Ports**: 9200 (HTTP only, no native transport)
**Env**: `hostname`, `port`, `password` (user always `elastic`)
**HA**: Multi-node cluster, automatic repair
**Config**: `PLUGINS` (comma-separated, restart required), `HEAP_PERCENT` (default 50%, range 1-100)
**Gotchas**: HTTP only internally. Min RAM 0.25 GB. Default user `elastic`. Tune `HEAP_PERCENT=75` for search-heavy.
**Wiring** (sample hostname: `search`):
**VARS**: `ES_HOST: search` `ES_PORT: ${search_port}`
**SECRETS**: `ES_PASSWORD: ${search_password}`
**CONN**: `http://search:${search_port}` with `Authorization: Basic elastic:${search_password}`

## Object Storage
**Type**: `object-storage` or `objectstorage` (both valid, no version) | **Mode**: NOT REQUIRED
**Env**: `apiUrl`, `accessKeyId`, `secretAccessKey`, `bucketName`, `quotaGBytes`, `projectId`, `serviceId`, `hostname`
**Config**: `objectStorageSize: 1-100` GB, `objectStoragePolicy` or `objectStorageRawPolicy`, `priority: 10`
**Infrastructure**: runs on **independent infra** separate from other project services -- accessible from any Zerops service or remotely over internet
**Bucket**: one auto-created per service (name = hostname + random prefix, **immutable**). Need multiple buckets? Create multiple object-storage services
**Policies**: `private` | `public-read` (list+get) | `public-objects-read` (get only, no listing) | `public-write` (put only) | `public-read-write` (full). Or use `objectStorageRawPolicy` with IAM Policy JSON (`{{ .BucketName }}` template variable available)
**Gotchas**: MinIO backend. **No Zerops backup**. `forcePathStyle: true` / `AWS_USE_PATH_STYLE_ENDPOINT: true` REQUIRED. Region `us-east-1` (required but ignored). No autoscaling, no verticalAutoscaling. Quota changeable in GUI after creation
**Wiring** (sample hostname: `storage`):
**VARS**: `S3_ENDPOINT: ${storage_apiUrl}` `S3_BUCKET: ${storage_bucketName}` `S3_REGION: us-east-1`
**SECRETS**: `S3_KEY: ${storage_accessKeyId}` `S3_SECRET: ${storage_secretAccessKey}`
**REQUIRED**: `forcePathStyle: true` / `AWS_USE_PATH_STYLE_ENDPOINT: true` (MinIO backend)

## Shared Storage
**Type**: `shared-storage` (no version) | **Mode**: optional (default NON_HA), immutable
**Mount**: `/mnt/{hostname}` -- add `mount: [hostname]` to runtime zerops.yml
**HA**: 1:1 replication, auto-failover
**Gotchas**: SeaweedFS backend. Max 60 GB. POSIX only (not S3). NON_HA = data loss on hardware failure.
**Wiring**: No env vars. Mount via `mount: [{hostname}]` in zerops.yml run section. POSIX filesystem, max 60 GB.

## Kafka
**Type**: `kafka@3.8` | **Mode**: optional (default NON_HA), immutable
**Ports**: 9092 (SASL PLAIN), 8081 (Schema Registry if enabled)
**Env**: `hostname`, `user`, `password`
**HA**: 3 brokers, 6 partitions, replication factor 3, auto-repair
**Gotchas**: SASL PLAIN only (no anonymous). NON_HA = 1 broker, **no replication**. Indefinite topic retention (implement cleanup). 250 GB cap.
**Wiring** (sample hostname: `kafka`):
**VARS**: `KAFKA_BROKERS: kafka:9092`
**SECRETS**: `KAFKA_USER: ${kafka_user}` `KAFKA_PASSWORD: ${kafka_password}`
**CONN**: `security.protocol=SASL_PLAINTEXT`, `sasl.mechanism=PLAIN` (no anonymous)

## NATS
**Type**: `nats@{2.12,2.10}` | **Mode**: optional (default NON_HA), immutable
**Ports**: 4222 (client), 8222 (HTTP monitoring)
**Env**: `hostname`, `user` (always `zerops`), `password`, `connectionString`
**Config**: `JET_STREAM_ENABLED` (default 1), `MAX_PAYLOAD` (default 8 MB, max 64 MB)
**Gotchas**: Config changes require restart. JetStream HA sync lag 1 min. Set `JET_STREAM_ENABLED=0` for core pub-sub only.
**Wiring** (sample hostname: `nats`):
**SECRETS**: `NATS_URL: nats://${nats_user}:${nats_password}@nats:4222` or `NATS_URL: ${nats_connectionString}`

## Meilisearch
**Type**: `meilisearch@{1.20,1.10}` | **Mode**: optional (NON_HA only)
**Ports**: 7700
**Env**: `hostname`, `masterKey`, `defaultSearchKey`, `defaultAdminKey`
**Gotchas**: **No HA** (single-node only). Never expose `masterKey` to frontend -- use `defaultSearchKey`.
**Wiring** (sample hostname: `search`):
**VARS**: `MEILI_HOST: http://search:7700`
**SECRETS**: `MEILI_MASTER_KEY: ${search_masterKey}`

## ClickHouse
**Type**: `clickhouse@25.3` | **Mode**: optional (default NON_HA), immutable
**Ports**: 9000 (native), 8123 (HTTP), 9004 (MySQL compat), 9005 (PostgreSQL compat)
**Env**: `hostname`, `port`, `portHttp`, `portMysql`, `portPostgresql`, `portNative`, `password`, `superUserPassword`, `dbName`
**HA**: 3 nodes, replication factor 3, cluster `zerops`
**Gotchas**: HA requires `ReplicatedMergeTree` (standard `MergeTree` won't replicate). Super user for backups.
**Wiring** (sample hostname: `ch`):
**VARS**: `CH_HOST: ch` `CH_DB: ${ch_dbName}`
**SECRETS**: `CH_PASSWORD: ${ch_password}` `CH_SUPER_PASSWORD: ${ch_superUserPassword}`
**CONN**: Native `clickhouse://ch:${ch_port}`, HTTP `http://ch:${ch_portHttp}`, MySQL `ch:9004`, PostgreSQL `ch:9005`

## Qdrant
**Type**: `qdrant@{1.12,1.10}` | **Mode**: optional (default NON_HA), immutable
**Ports**: 6333 (HTTP), 6334 (gRPC)
**Env**: `hostname`, `port`, `grpcPort`, `apiKey`, `readOnlyApiKey`, `connectionString`, `grpcConnectionString`
**HA**: 3 nodes, `automaticClusterReplication=true` by default
**Gotchas**: No public access (internal only). Use 6334 for gRPC (higher perf for large vectors).
**Wiring** (sample hostname: `qdrant`):
**VARS**: `QDRANT_HOST: qdrant` `QDRANT_PORT: ${qdrant_port}` `QDRANT_GRPC: ${qdrant_grpcPort}`
**SECRETS**: `QDRANT_API_KEY: ${qdrant_apiKey}`
**CONN**: HTTP `http://qdrant:${qdrant_port}` with `api-key` header, gRPC `qdrant:${qdrant_grpcPort}`

## Typesense
**Type**: `typesense@27.1` | **Mode**: optional (default NON_HA), immutable
**Ports**: 8108
**Env**: `hostname`, `port`, `apiKey` (immutable master key)
**HA**: 3-node Raft consensus, auto leader election, recovery up to 1 min
**Gotchas**: API key immutable. 1-min recovery -- expect brief 503s. CORS always on.
**Wiring** (sample hostname: `typesense`):
**VARS**: `TYPESENSE_HOST: typesense` `TYPESENSE_PORT: ${typesense_port}`
**SECRETS**: `TYPESENSE_API_KEY: ${typesense_apiKey}`
**CONN**: `http://typesense:${typesense_port}` with `x-typesense-api-key` header

## RabbitMQ
**Type**: `rabbitmq@3.9` | **Mode**: optional (default NON_HA), immutable
**Ports**: 5672 (AMQP), 15672 (management UI)
**Env**: `hostname`, `port`, `user`, `password`, `connectionString`
**Gotchas**: Management UI on port 15672. Use AMQP 0-9-1 protocol for client connections.
**Wiring** (sample hostname: `rabbitmq`):
**VARS**: `RABBITMQ_HOST: rabbitmq` `RABBITMQ_PORT: ${rabbitmq_port}`
**SECRETS**: `RABBITMQ_URL: amqp://${rabbitmq_user}:${rabbitmq_password}@rabbitmq:${rabbitmq_port}` or `RABBITMQ_URL: ${rabbitmq_connectionString}`

## Common Patterns

- **Mode**: optional (default NON_HA), immutable after creation (delete+recreate to change)
- **Hostname**: immutable, becomes internal DNS name
- **Internal**: HTTP/plain TCP only (no TLS) -- TLS for external/VPN ports
- **Credentials**: auto-generated in env vars
- **System Users**: don't modify `zps`/`zerops`/`super`
- **VPN**: append `.zerops` to hostname
- **Backup**: PostgreSQL, MariaDB, Elasticsearch, Meilisearch, Qdrant, NATS (NOT Valkey/KeyDB, NOT ClickHouse -- use SQL BACKUP)
- **Priority**: `priority: 10` for databases/storage to start before apps

## Service Operations

### Database Export/Import
- **PostgreSQL**: `pg_dump` for export, `psql` for import
- **MariaDB**: `mysqldump` for export, `mysql` for import
- Requires Zerops VPN or Adminer (built-in web DB tool)
- Desktop tools (DBeaver, pgAdmin) connect via VPN using standard env vars

### Backup System
- **Supported**: PostgreSQL, MariaDB, Elasticsearch, Meilisearch, Qdrant, NATS, Shared Storage
- **NOT supported**: Valkey, KeyDB, ClickHouse (use native dump), Object Storage
- Default schedule: daily 00:00-01:00 UTC (configurable)
- Retention: 7 daily, 4 weekly, 3 monthly; max 50 per service
- End-to-end encryption (X25519 per project)
- Storage limits: 5 GB (Lightweight), 25 GB (Serious), 1 TiB max

### Debug Mode
- Available for build and runtime prepare phases
- Pause points: Disable, Before First Command, On Command Failure, After Last Command
- Commands: `zsc debug continue`, `zsc debug success`, `zsc debug fail`
- Max duration: 60 minutes
