---
id: develop-first-deploy-env-vars
priority: 2
phases: [develop-active]
deployStates: [never-deployed]
title: "Use the discovered env var catalog when wiring the app"
---

### Env var catalog from bootstrap

Managed services expose env var keys that your runtime should reference.
Fetch the actual key list with `zerops_discover service="{hostname}"
includeEnvs=true` and use those keys verbatim — **do not guess
alternatives**.

Common managed-service keys — the host key is **`hostname`**, never
`host` (verify against the actual catalog for your services):

- PostgreSQL: `connectionString`, `connectionTlsString`, `hostname`,
  `port`, `portTls`, `user`, `password`, `superUser`,
  `superUserPassword`, `dbName`.
- MariaDB: `connectionString`, `hostname`, `port`, `user`, `password`,
  `dbName`.
- Valkey: `connectionString`, `connectionTlsString`, `hostname`, `port`,
  `portTls` (no auth — private network).
- KeyDB: `connectionString`, `hostname`, `port` (no auth, no TLS).
- NATS: `connectionString`, `hostname`, `port`, `portManagement`,
  `user`, `password`.
- Kafka: `hostname`, `port`, `user`, `password` (no
  `connectionString` — build broker URL from `hostname:port`).
- ClickHouse: `connectionString`, `hostname`, `port`, `portHttp`,
  `portMysql`, `portNative`, `portPostgresql`, `user`, `password`,
  `superUser`, `superUserPassword`, `dbName`, `clusterName`.
- Elasticsearch: `connectionString`, `hostname`, `port`, `user`,
  `password`.
- Meilisearch: `connectionString`, `hostname`, `port`, `masterKey`,
  `defaultAdminKey`, `defaultSearchKey`, `defaultReadOnlyKey`,
  `defaultChatKey`.
- Typesense: `connectionString`, `hostname`, `port`, `apiKey`.
- Qdrant: `connectionString`, `grpcConnectionString`, `hostname`,
  `port`, `grpcPort`, `apiKey`, `readOnlyApiKey`.
- Object Storage: `apiUrl`, `apiHost`, `bucketName`, `accessKeyId`,
  `secretAccessKey`, `quotaGBytes`, `hostname`.
- Shared Storage: `hostname` only — mounted via `mount:` in
  zerops.yaml, not a network service.

**Cross-service reference form** — inside `run.envVariables` of a
runtime service:

```yaml
envVariables:
  DATABASE_URL: ${db_connectionString}
  DB_HOST: ${db_hostname}
```

The platform rewrites `${db_connectionString}` at deploy time by
looking up service `db`'s env var named `connectionString`. A wrong
spelling resolves to the literal string `${db_connectionString}` and
the app fails at connect time.

**Re-check at any point:** `zerops_discover service="{hostname}"
includeEnvs=true` returns the key list. Values are redacted by default;
names alone are enough for cross-service wiring. Add
`includeEnvValues=true` only for troubleshooting.
