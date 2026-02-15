# Wiring Patterns

## Keywords
wiring, connection string, env vars, envSecrets, envVariables, cross-service, reference, postgresql, mariadb, valkey, elasticsearch, object storage, kafka, nats, meilisearch, clickhouse, qdrant, typesense, shared storage, keydb

## TL;DR
Cross-service wiring templates for all 13 Zerops managed services. Variable reference syntax, envSecrets vs envVariables rules, and concrete connection patterns.

## Syntax Rules

- **`{h}` placeholder**: represents the service hostname you are wiring to. In actual YAML, replace `{h}` with the real hostname (e.g., `DB_HOST: mydb`). For variable references, use `${hostname_varname}` syntax
- **Reference**: `${hostname_variablename}` — dashes in hostnames become underscores
- **envSecrets**: passwords, tokens, keys (blurred in GUI by default, editable/deletable)
- **envVariables**: config, URLs, flags (visible in GUI)
- **Hostname = DNS**: use hostname directly for connections (`db:5432`, NOT `${db_hostname}:5432`)
- **Internal**: ALWAYS `http://` — NEVER `https://` (SSL at L7 balancer)
- **Project vars**: auto-inherited by all services — do NOT re-reference (creates shadow)
- **Password sync**: changing DB password in GUI does NOT update env vars (manual sync)

## PostgreSQL
**VARS**: `DB_HOST:{h}` `DB_PORT:5432` `DB_NAME:${h_dbName}`
**SECRETS**: `DATABASE_URL:postgresql://${h_user}:${h_password}@{h}:5432/${h_dbName}`
**NOTE**: Some libs need `postgres://` instead of `postgresql://`. HA read replicas on port 5433.

## MariaDB
**VARS**: `DB_HOST:{h}` `DB_PORT:3306` `DB_NAME:${h_dbName}`
**SECRETS**: `DATABASE_URL:mysql://${h_user}:${h_password}@{h}:3306/${h_dbName}`

## Valkey
**SECRETS**: `REDIS_URL:redis://${h_user}:${h_password}@{h}:6379`
**NOTE**: TLS: `rediss://...@{h}:6380`. Read replicas: port 7000 (non-TLS), 7001 (TLS).

## KeyDB
**SECRETS**: `REDIS_URL:redis://${h_user}:${h_password}@{h}:6379`
**NOTE**: Same as Valkey. DEPRECATED — migrate to Valkey.

## Elasticsearch
**VARS**: `ES_HOST:{h}` `ES_PORT:9200`
**SECRETS**: `ES_PASSWORD:${h_password}`
**CONN**: `http://{h}:9200` with `Authorization: Basic elastic:${h_password}`

## Object Storage
**VARS**: `S3_ENDPOINT:${h_apiUrl}` `S3_BUCKET:${h_bucketName}` `S3_REGION:us-east-1`
**SECRETS**: `S3_KEY:${h_accessKeyId}` `S3_SECRET:${h_secretAccessKey}`
**REQUIRED**: `forcePathStyle: true` / `AWS_USE_PATH_STYLE_ENDPOINT: true` (MinIO backend)

## Shared Storage
**MOUNT**: `/mnt/{hostname}` — add `mount: [{hostname}]` in zerops.yml run section
**NOTE**: No env vars. POSIX filesystem, max 60 GB.

## Kafka
**VARS**: `KAFKA_BROKERS:{h}:9092`
**SECRETS**: `KAFKA_USER:${h_user}` `KAFKA_PASSWORD:${h_password}`
**CONN**: `security.protocol=SASL_PLAINTEXT`, `sasl.mechanism=PLAIN` (no anonymous)

## NATS
**SECRETS**: `NATS_URL:nats://${h_user}:${h_password}@{h}:4222` or `NATS_URL:${h_connectionString}`
**NOTE**: User is always `zerops`. JetStream enabled by default.

## Meilisearch
**VARS**: `MEILI_HOST:http://{h}:7700`
**SECRETS**: `MEILI_MASTER_KEY:${h_masterKey}`
**NOTE**: Frontend uses `${h_defaultSearchKey}`, backend uses `${h_defaultAdminKey}`. Never expose `masterKey`.

## ClickHouse
**VARS**: `CH_HOST:{h}` `CH_DB:${h_dbName}`
**SECRETS**: `CH_PASSWORD:${h_password}` `CH_SUPER_PASSWORD:${h_superUserPassword}`
**CONN**: Native `clickhouse://{h}:9000`, HTTP `http://{h}:8123`, MySQL `{h}:9004`, PostgreSQL `{h}:9005`

## Qdrant
**VARS**: `QDRANT_HOST:{h}` `QDRANT_PORT:6333` `QDRANT_GRPC:6334`
**SECRETS**: `QDRANT_API_KEY:${h_apiKey}`
**CONN**: HTTP `http://{h}:6333` with `api-key` header, gRPC `{h}:6334`

## Typesense
**VARS**: `TYPESENSE_HOST:{h}` `TYPESENSE_PORT:8108`
**SECRETS**: `TYPESENSE_API_KEY:${h_apiKey}`
**CONN**: `http://{h}:8108` with `x-typesense-api-key` header

## RabbitMQ
**VARS**: `RABBITMQ_HOST:{h}` `RABBITMQ_PORT:5672`
**SECRETS**: `RABBITMQ_URL:amqp://${h_user}:${h_password}@{h}:5672` or `RABBITMQ_URL:${h_connectionString}`

## See Also
- zerops://foundation/grammar — variable reference syntax, envSecrets vs envVariables
- zerops://foundation/services — service cards with ports and HA details
