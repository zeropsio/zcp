---
id: develop-first-deploy-env-vars
priority: 2
phases: [develop-active]
envelopeDeployStates: [never-deployed]
title: "Use the discovered env var catalog when wiring the app"
references-atoms: [develop-env-var-channels]
---

### Env var catalog from bootstrap

Managed services expose env var keys that your runtime should reference.
Fetch the actual key list with `zerops_discover service="<hostname>"
includeEnvs=true` per managed service and use those keys verbatim — **do
not guess alternatives**. The catalog is the authoritative source; the
host key is **`hostname`** (never `host`), but every other key varies
per service type, so don't hardcode from memory.

Place runtime env vars in `run.envVariables`. Cross-service references
use this form:

```yaml
envVariables:
  DATABASE_URL: ${db_connectionString}
  DB_HOST: ${db_hostname}
```

Zerops rewrites `${db_connectionString}` at deploy time from service
`db`'s `connectionString`; a wrong spelling remains literal and the
app fails at connect time.

**Re-check at any point:** `zerops_discover service="<hostname>"
includeEnvs=true` returns the key list. Values are redacted by default;
names alone are enough for cross-service wiring. Add
`includeEnvValues=true` only for troubleshooting.

### Per-service canonical env-var reference

Patterns for the most common managed-service types when wiring `run.envVariables`. Always verify against `zerops_discover` for the live key list — types update versions and key sets.

| Service | Use |
|---|---|
| PostgreSQL / MariaDB / ClickHouse | Prefer `connectionString` over assembling `hostname:port:user:password:dbName`. PostgreSQL + ClickHouse expose `superUser` / `superUserPassword` for DDL; ClickHouse port must match the driver (`portHttp`, `portMysql`, `portNative`, `portPostgresql`). |
| Valkey / KeyDB | No auth on private network; KeyDB has no TLS port. |
| NATS | URI is `connectionString`. |
| Kafka | No `connectionString`; build broker URL from `hostname:port`. |
| Elasticsearch | HTTP basic auth via `user`/`password`. |
| Meilisearch | Use scoped keys (`defaultAdminKey`, `defaultSearchKey`, `defaultReadOnlyKey`, `defaultChatKey`), not `masterKey`. |
| Typesense | Single `apiKey`. |
| Qdrant | HTTP + gRPC via `connectionString` / `grpcConnectionString`; read-only via `readOnlyApiKey`. |
| object-storage | S3-compatible: `apiUrl`, `accessKeyId`, `secretAccessKey`, `bucketName`; no `region` env var. |
| shared-storage | `hostname` only; mounted via `mount:` in zerops.yaml, not a network service. |
