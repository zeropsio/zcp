# Wiring Patterns

## Keywords
wiring, connection string, env vars, envSecrets, envVariables, cross-service, reference, postgresql, mariadb, valkey, elasticsearch, object storage, kafka, nats, meilisearch, clickhouse, qdrant, typesense, shared storage, keydb

## TL;DR
Cross-service wiring templates for all 13 Zerops managed services. Variable reference syntax, envSecrets rules, and concrete connection patterns.

## Syntax Rules

- **`{h}` placeholder**: represents the service hostname you are wiring to. In actual YAML, replace `{h}` with the real hostname (e.g., `DB_HOST: mydb`). For variable references, use `${hostname_varname}` syntax
- **Reference**: `${hostname_variablename}` — dashes in hostnames become underscores
- **envSecrets**: passwords, tokens, keys (blurred in GUI by default, editable/deletable)
- **import.yml service level**: ONLY `envSecrets` and `dotEnvSecrets` exist. There is NO `envVariables` at service level (only at project level). Put ALL connection vars in `envSecrets`.
- **Hostname = DNS**: use hostname directly for host (`{h}`, NOT `${h_hostname}`), but use `${h_port}` for port
- **Internal**: ALWAYS `http://` — NEVER `https://` (SSL at L7 balancer)
- **Project vars**: auto-inherited by all services — do NOT re-reference (creates shadow)
- **Password sync**: changing DB password in GUI does NOT update env vars (manual sync)

## CRITICAL: Wire credentials in import.yml

Managed services auto-generate credentials but they are NOT automatically available to runtime services. You MUST wire them via `envSecrets` on the **runtime service** in import.yml:

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

## PostgreSQL
**VARS**: `DB_HOST:{h}` `DB_PORT:${h_port}` `DB_NAME:${h_dbName}`
**SECRETS**: `DATABASE_URL:postgresql://${h_user}:${h_password}@{h}:${h_port}/${h_dbName}`
**NOTE**: Some libs need `postgres://` instead of `postgresql://`. HA read replicas on port 5433.

## MariaDB
**VARS**: `DB_HOST:{h}` `DB_PORT:${h_port}` `DB_NAME:${h_dbName}`
**SECRETS**: `DATABASE_URL:mysql://${h_user}:${h_password}@{h}:${h_port}/${h_dbName}`

## Valkey
**SECRETS**: `REDIS_URL:redis://${h_user}:${h_password}@{h}:${h_port}`
**NOTE**: TLS: `rediss://${h_user}:${h_password}@{h}:6380`. Read replicas: port 7000 (non-TLS), 7001 (TLS).

## KeyDB
**SECRETS**: `REDIS_URL:redis://${h_user}:${h_password}@{h}:${h_port}`
**NOTE**: Same as Valkey. DEPRECATED — migrate to Valkey.

## Elasticsearch
**VARS**: `ES_HOST:{h}` `ES_PORT:${h_port}`
**SECRETS**: `ES_PASSWORD:${h_password}`
**CONN**: `http://{h}:${h_port}` with `Authorization: Basic elastic:${h_password}`

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
**CONN**: Native `clickhouse://{h}:${h_port}`, HTTP `http://{h}:${h_portHttp}`, MySQL `{h}:9004`, PostgreSQL `{h}:9005`

## Qdrant
**VARS**: `QDRANT_HOST:{h}` `QDRANT_PORT:${h_port}` `QDRANT_GRPC:${h_grpcPort}`
**SECRETS**: `QDRANT_API_KEY:${h_apiKey}`
**CONN**: HTTP `http://{h}:${h_port}` with `api-key` header, gRPC `{h}:${h_grpcPort}`

## Typesense
**VARS**: `TYPESENSE_HOST:{h}` `TYPESENSE_PORT:${h_port}`
**SECRETS**: `TYPESENSE_API_KEY:${h_apiKey}`
**CONN**: `http://{h}:${h_port}` with `x-typesense-api-key` header

## RabbitMQ
**VARS**: `RABBITMQ_HOST:{h}` `RABBITMQ_PORT:${h_port}`
**SECRETS**: `RABBITMQ_URL:amqp://${h_user}:${h_password}@{h}:${h_port}` or `RABBITMQ_URL:${h_connectionString}`

## See Also
- zerops://foundation/grammar — variable reference syntax, envSecrets vs envVariables
- zerops://foundation/services — service cards with ports and HA details
