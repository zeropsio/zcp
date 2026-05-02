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

### Per-managed-type guidance (when the envelope has managed services)

If the envelope has zero managed deps (a runtime-only project), this
section is a no-op. Otherwise, run `zerops_discover service="<dep>"
includeEnvs=true` per managed dep — that returns the live key set for
the actual service version. Patterns to remember when wiring:

- Databases / message brokers usually expose `connectionString` —
  prefer it over assembling `hostname:port:user:password:dbName`.
- Some types expose elevated credentials (`superUser` /
  `superUserPassword` on Postgres + ClickHouse) for DDL — pull from
  the catalog only when DDL is actually needed.
- ClickHouse + Kafka have multiple ports; match the driver
  (`portHttp` / `portMysql` / `portNative` / `portPostgresql` for
  ClickHouse; build broker URL from `hostname:port` for Kafka — no
  `connectionString`).
- Object storage is S3-compatible: `apiUrl`, `accessKeyId`,
  `secretAccessKey`, `bucketName` — no `region` env var.
- Shared storage is a `hostname`-only mount (`mount:` in zerops.yaml,
  not a network service).
- Search / vector services (Meilisearch, Typesense, Qdrant) ship
  scoped API keys; pick the narrow key for app code, never the master
  key. Qdrant has both HTTP (`connectionString`) and gRPC
  (`grpcConnectionString`); pick to match the client library.

For exotic types, `zerops_knowledge query="<service>"` returns the
canonical reference page.
