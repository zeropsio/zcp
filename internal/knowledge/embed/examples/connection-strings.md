# Connection String Examples

## Keywords
connection string, database url, postgresql url, mariadb url, redis url, valkey url, elasticsearch url, meilisearch url, s3, object storage

## TL;DR
Copy-paste ready connection strings using Zerops env var references for all database and storage services.

## PostgreSQL
```yaml
# Internal (primary, read/write)
DATABASE_URL: postgresql://${db_user}:${db_password}@${db_hostname}:5432/${db_dbname}

# Read replicas (HA only)
DATABASE_REPLICA_URL: postgresql://${db_user}:${db_password}@${db_hostname}:5433/${db_dbname}

# External (TLS via pgBouncer)
DATABASE_EXTERNAL_URL: postgresql://${db_user}:${db_password}@${db_hostname}:6432/${db_dbname}?sslmode=require
```

## MariaDB
```yaml
DATABASE_URL: mysql://${db_user}:${db_password}@${db_hostname}:3306/${db_dbname}
```

## Valkey / Redis
```yaml
# Non-TLS
REDIS_URL: redis://${cache_user}:${cache_password}@${cache_hostname}:6379

# TLS
REDIS_TLS_URL: rediss://${cache_user}:${cache_password}@${cache_hostname}:6380

# Read replicas (HA only, non-TLS)
REDIS_REPLICA_URL: redis://${cache_user}:${cache_password}@${cache_hostname}:7000
```

## Elasticsearch
```yaml
ELASTICSEARCH_URL: http://elastic:${search_password}@${search_hostname}:9200
```

## Meilisearch
```yaml
MEILISEARCH_URL: http://${search_hostname}:7700
MEILISEARCH_KEY: ${search_masterKey}
```

## ClickHouse
```yaml
# Native TCP
CLICKHOUSE_URL: clickhouse://${analytics_user}:${analytics_password}@${analytics_hostname}:9000/${analytics_dbname}

# HTTP
CLICKHOUSE_HTTP_URL: http://${analytics_user}:${analytics_password}@${analytics_hostname}:8123
```

## Kafka
```yaml
KAFKA_BROKERS: ${events_hostname}:9092
KAFKA_USER: ${events_user}
KAFKA_PASSWORD: ${events_password}
```

## NATS
```yaml
NATS_URL: nats://${messaging_user}:${messaging_password}@${messaging_hostname}:4222
```

## Object Storage (S3)
```yaml
S3_ENDPOINT: ${storage_apiUrl}
S3_BUCKET: ${storage_bucketName}
S3_ACCESS_KEY: ${storage_accessKeyId}
S3_SECRET_KEY: ${storage_secretAccessKey}
S3_REGION: us-east-1                       # required but ignored by MinIO
AWS_USE_PATH_STYLE_ENDPOINT: "true"        # REQUIRED for Zerops (MinIO)
```

## Qdrant
```yaml
QDRANT_URL: http://${vectors_hostname}:6333
QDRANT_GRPC_URL: ${vectors_hostname}:6334
QDRANT_API_KEY: ${vectors_apiKey}
```

## Typesense
```yaml
TYPESENSE_URL: http://${search_hostname}:8108
TYPESENSE_API_KEY: ${search_apiKey}
```

## Pattern
All env var references follow the pattern: `${<service-hostname>_<var-name>}`.
The service hostname is the `hostname` field from your `import.yaml` or service creation.

## See Also
- zerops://examples/zerops-yml-runtimes
- zerops://services/_common-database
- zerops://platform/env-variables
- zerops://config/import-yml
