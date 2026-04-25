---
id: bootstrap-env-var-discovery
priority: 3
phases: [bootstrap-active]
routes: [classic, adopt]
steps: [provision]
title: "Discover env vars before generate"
---

### Discover env vars after provision, before generate

After import and before writing any zerops.yaml, discover the actual
env vars available on the project. This is the authoritative list —
do not guess alternative spellings. Unknown cross-service references
resolve to literal strings at runtime and fail silently.

```
zerops_discover includeEnvs=true
```

Record one row per service. Keys alone are enough — cross-service
references use `${hostname_varName}` syntax. Reference these in
`run.envVariables` of the app's zerops.yaml.

Common patterns by service type (key name is **`hostname`**, never `host`):

| Service type | Available env vars | Notes |
|-------------|-------------------|-------|
| PostgreSQL | `connectionString`, `connectionTlsString`, `hostname`, `port`, `portTls`, `user`, `password`, `superUser`, `superUserPassword`, `dbName` | `connectionString` preferred; `superUser` for DDL |
| MariaDB | `connectionString`, `hostname`, `port`, `user`, `password`, `dbName` | `connectionString` preferred |
| Valkey | `connectionString`, `connectionTlsString`, `hostname`, `port`, `portTls` | No auth — private network |
| KeyDB | `connectionString`, `hostname`, `port` | No auth, no TLS port |
| NATS | `connectionString`, `hostname`, `port`, `portManagement`, `user`, `password` | NATS URI in `connectionString` |
| Kafka | `hostname`, `port`, `user`, `password` | No `connectionString` — build broker URL from `hostname:port` |
| ClickHouse | `connectionString`, `hostname`, `port`, `portHttp`, `portMysql`, `portNative`, `portPostgresql`, `user`, `password`, `superUser`, `superUserPassword`, `dbName`, `clusterName` | Multi-protocol; pick port matching driver |
| Elasticsearch | `connectionString`, `hostname`, `port`, `user`, `password` | HTTP basic auth |
| Meilisearch | `connectionString`, `hostname`, `port`, `masterKey`, `defaultAdminKey`, `defaultSearchKey`, `defaultReadOnlyKey`, `defaultChatKey` | Use scoped key by role, not `masterKey` |
| Typesense | `connectionString`, `hostname`, `port`, `apiKey` | Single API key |
| Qdrant | `connectionString`, `grpcConnectionString`, `hostname`, `port`, `grpcPort`, `apiKey`, `readOnlyApiKey` | HTTP + gRPC; pick by client |
| object-storage | `apiUrl`, `apiHost`, `bucketName`, `accessKeyId`, `secretAccessKey`, `quotaGBytes`, `hostname` | S3-compatible; no `region` env var (use bucket region from provider docs) |
| shared-storage | `hostname` only | Mounted via `mount:` in zerops.yaml — not a network service |

**Dev-container caveat**: env vars resolve at deploy time, not OS env
vars on a `startWithoutCode: true` container. A dev container that
never deployed has the env vars in the project catalogue but not on
`process.env`. Deploy first, then references fire.
