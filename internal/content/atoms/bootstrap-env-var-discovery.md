---
id: bootstrap-env-var-discovery
priority: 3
phases: [bootstrap-active]
routes: [classic, adopt]
steps: [provision]
title: "Discover env vars before generate"
---

### Discover env vars after provision, before generate

After import and before any zerops.yaml, discover actual project env
vars. This is authoritative — do not guess alternative spellings.
Unknown cross-service references become literal strings at runtime and
fail silently.

```
zerops_discover includeEnvs=true
```

Record one row per service. Keys are enough; cross-service references
use `${hostname_varName}` in `run.envVariables`.

Usage layer (key is **`hostname`**, never `host`; run discovery for the
live list):

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

**Runtime container caveat**: env vars resolve at deploy time, not as OS
env on a never-deployed `startWithoutCode: true` dev container. They are
in the project catalogue, not `process.env`. Deploy first; then
references fire.
