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

Per-service usage guidance (key name is **`hostname`**, never `host`;
run `zerops_discover includeEnvs=true` for the live key list — this
is the preference/usage layer on top):

- **PostgreSQL / MariaDB / ClickHouse**: prefer `connectionString`
  over building from `hostname:port:user:password:dbName`. PostgreSQL
  + ClickHouse expose `superUser` / `superUserPassword` for DDL.
  ClickHouse is multi-protocol — pick the port matching your driver
  (`portHttp`, `portMysql`, `portNative`, `portPostgresql`).
- **Valkey / KeyDB**: no auth (private network). KeyDB has no TLS port.
- **NATS**: NATS URI is in `connectionString`.
- **Kafka**: no `connectionString` — build broker URL from
  `hostname:port`.
- **Elasticsearch**: HTTP basic auth via `user`/`password`.
- **Meilisearch**: use a scoped key — `defaultAdminKey`,
  `defaultSearchKey`, `defaultReadOnlyKey`, `defaultChatKey` — not
  `masterKey`.
- **Typesense**: single `apiKey`.
- **Qdrant**: HTTP + gRPC (`connectionString` / `grpcConnectionString`);
  pick by client. Read-only access via `readOnlyApiKey`.
- **object-storage**: S3-compatible — `apiUrl`, `accessKeyId`,
  `secretAccessKey`, `bucketName`. No `region` env var (use the
  bucket region from provider docs).
- **shared-storage**: `hostname` only — mounted via `mount:` in
  zerops.yaml, not a network service.

**Dev-container caveat**: env vars resolve at deploy time, not OS env
vars on a `startWithoutCode: true` container. A dev container that
never deployed has the env vars in the project catalogue but not on
`process.env`. Deploy first, then references fire.
