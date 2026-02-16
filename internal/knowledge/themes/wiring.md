# Wiring Patterns

## TL;DR
Cross-service wiring templates for all Zerops managed services. Variable reference syntax, envSecrets vs envVariables rules, and concrete connection patterns.

## Keywords
wiring, connection string, env vars, envSecrets, envVariables, cross-service, reference, postgresql, mariadb, valkey, elasticsearch, object storage, kafka, nats, meilisearch, clickhouse, qdrant, typesense, shared storage, keydb, rabbitmq

## Syntax Rules

- **Hostname substitution**: In templates below, each service uses a sample hostname (e.g., `db`, `cache`, `search`). Replace it with your actual service hostname. The syntax `${hostname_varname}` is real Zerops cross-service reference syntax — `hostname` must match the target service hostname exactly, with dashes converted to underscores.
- **Reference**: `${hostname_variablename}` — dashes in hostnames become underscores
- **envSecrets**: passwords, tokens, keys (blurred in GUI by default, editable/deletable)
- **import.yml service level**: ONLY `envSecrets` and `dotEnvSecrets` exist. There is NO `envVariables` at service level (only at project level). Put ALL connection vars in `envSecrets`.
- **Hostname = DNS**: use hostname directly for connections (`db:5432`, NOT `${db_hostname}:5432`)
- **Internal**: ALWAYS `http://` — NEVER `https://` (SSL at L7 balancer)
- **Project vars**: auto-inherited by all services — do NOT re-reference (creates shadow)
- **Password sync**: changing DB password in GUI does NOT update env vars (manual sync)

## CRITICAL: Wire credentials in import.yml

Managed services auto-generate credentials but they are NOT automatically available to runtime services. You MUST wire them via `envSecrets` on the **runtime service** in import.yml (there is no `envVariables` at service level in import.yml):

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
      DB_PORT: "3306"
      DB_NAME: ${mydb_dbName}
      DB_PASSWORD: ${mydb_password}
      DB_USER: ${mydb_user}
```

Without this wiring, the runtime service has no way to connect to managed services.

## Service Wiring Templates

Below, **VARS** = config values, **SECRETS** = credentials. **CRITICAL: In import.yml, put ALL of them (both VARS and SECRETS) in `envSecrets`.** There is no `envVariables` at service level in import.yml — using it will silently drop the values. In zerops.yml, use `run.envVariables` for VARS. Replace sample hostnames (`db`, `cache`, etc.) with your actual service hostname.

## PostgreSQL
Sample hostname: `db`
**VARS**: `DB_HOST: db` `DB_PORT: "5432"` `DB_NAME: ${db_dbName}`
**SECRETS**: `DATABASE_URL: postgresql://${db_user}:${db_password}@db:5432/${db_dbName}`
**NOTE**: Some libs need `postgres://` instead of `postgresql://`. HA read replicas on port 5433.

## MariaDB
Sample hostname: `db`
**VARS**: `DB_HOST: db` `DB_PORT: "3306"` `DB_NAME: ${db_dbName}`
**SECRETS**: `DATABASE_URL: mysql://${db_user}:${db_password}@db:3306/${db_dbName}`

## Valkey
Sample hostname: `cache`
**SECRETS**: `REDIS_URL: redis://cache:6379`
**NOTE**: No authentication — Valkey has no `user` or `password` env vars. Do NOT use `${cache_user}` or `${cache_password}`. TLS: `rediss://cache:6380`. Read replicas: port 7000 (non-TLS), 7001 (TLS).

## KeyDB
Sample hostname: `cache`
**SECRETS**: `REDIS_URL: redis://cache:6379`
**NOTE**: Same as Valkey (no auth). DEPRECATED — migrate to Valkey.

## Elasticsearch
Sample hostname: `search`
**VARS**: `ES_HOST: search` `ES_PORT: "9200"`
**SECRETS**: `ES_PASSWORD: ${search_password}`
**CONN**: `http://search:9200` with `Authorization: Basic elastic:${search_password}`

## Object Storage
Sample hostname: `storage`
**VARS**: `S3_ENDPOINT: ${storage_apiUrl}` `S3_BUCKET: ${storage_bucketName}` `S3_REGION: us-east-1`
**SECRETS**: `S3_KEY: ${storage_accessKeyId}` `S3_SECRET: ${storage_secretAccessKey}`
**REQUIRED**: `forcePathStyle: true` / `AWS_USE_PATH_STYLE_ENDPOINT: true` (MinIO backend)

## Shared Storage
**MOUNT**: `/mnt/{hostname}` — add `mount: [{hostname}]` in zerops.yml run section
**NOTE**: No env vars. POSIX filesystem, max 60 GB.

## Kafka
Sample hostname: `kafka`
**VARS**: `KAFKA_BROKERS: kafka:9092`
**SECRETS**: `KAFKA_USER: ${kafka_user}` `KAFKA_PASSWORD: ${kafka_password}`
**CONN**: `security.protocol=SASL_PLAINTEXT`, `sasl.mechanism=PLAIN` (no anonymous)

## NATS
Sample hostname: `nats`
**SECRETS**: `NATS_URL: nats://${nats_user}:${nats_password}@nats:4222` or `NATS_URL: ${nats_connectionString}`
**NOTE**: User is always `zerops`. JetStream enabled by default.

## Meilisearch
Sample hostname: `search`
**VARS**: `MEILI_HOST: http://search:7700`
**SECRETS**: `MEILI_MASTER_KEY: ${search_masterKey}`
**NOTE**: Frontend uses `${search_defaultSearchKey}`, backend uses `${search_defaultAdminKey}`. Never expose `masterKey`.

## ClickHouse
Sample hostname: `ch`
**VARS**: `CH_HOST: ch` `CH_DB: ${ch_dbName}`
**SECRETS**: `CH_PASSWORD: ${ch_password}` `CH_SUPER_PASSWORD: ${ch_superUserPassword}`
**CONN**: Native `clickhouse://ch:9000`, HTTP `http://ch:8123`, MySQL `ch:9004`, PostgreSQL `ch:9005`

## Qdrant
Sample hostname: `qdrant`
**VARS**: `QDRANT_HOST: qdrant` `QDRANT_PORT: "6333"` `QDRANT_GRPC: "6334"`
**SECRETS**: `QDRANT_API_KEY: ${qdrant_apiKey}`
**CONN**: HTTP `http://qdrant:6333` with `api-key` header, gRPC `qdrant:6334`

## Typesense
Sample hostname: `typesense`
**VARS**: `TYPESENSE_HOST: typesense` `TYPESENSE_PORT: "8108"`
**SECRETS**: `TYPESENSE_API_KEY: ${typesense_apiKey}`
**CONN**: `http://typesense:8108` with `x-typesense-api-key` header

## RabbitMQ
Sample hostname: `rabbitmq`
**VARS**: `RABBITMQ_HOST: rabbitmq` `RABBITMQ_PORT: "5672"`
**SECRETS**: `RABBITMQ_URL: amqp://${rabbitmq_user}:${rabbitmq_password}@rabbitmq:5672` or `RABBITMQ_URL: ${rabbitmq_connectionString}`
