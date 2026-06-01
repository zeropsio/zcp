---
id: develop-first-deploy-env-vars
priority: 2
phases: [develop-active]
envelopeDeployStates: [never-deployed]
title: "Use the discovered env var catalog when wiring the app"
references-atoms: [develop-env-var-model, develop-env-var-channels, develop-reserved-env-names]
---

### Env var catalog from bootstrap

Managed services expose env var keys your runtime references. Fetch
the live key list per managed service with `zerops_discover
service="<hostname>" includeEnvs=true` and use those keys verbatim —
**do not guess alternatives**. The catalog is the authoritative source;
the host key is `hostname` (never `host`), other keys vary per service
type. Values are redacted by default — names suffice; pass
`includeEnvValues=true` only to troubleshoot.

Cross-service wiring goes in `zerops.yaml` `run.envVariables`. A wrong
spelling on the right-hand side reaches the app as the literal string
and connect-time fails.

### Per-managed-type cheatsheet

- **Postgres / MariaDB / MySQL** — `connectionString` is
  `protocol://${user}:${password}@${hostname}:${port}` and **omits the
  db name**; append `/${db_dbName}` for Prisma / Drizzle / sqlx /
  SQLAlchemy / Sequelize (worked example in the env-var-model atom).
- **Prisma `migrate dev` P3014** — shadow DB needs DDL the regular user
  lacks: use `prisma db push` for fresh schemas, or pass the
  `${db_superUser}:${db_superUserPassword}` URL for that one call.
- **Elevated DDL** — `superUser`/`superUserPassword` (Postgres +
  ClickHouse), only when DDL needs them.
- **ClickHouse + Kafka** — multi-port; match driver (`portHttp` /
  `portMysql` / `portNative` / `portPostgresql`; Kafka builds
  `hostname:port`, no `connectionString`).
- **Object storage** — S3: `apiUrl`, `accessKeyId`, `secretAccessKey`,
  `bucketName`, no `region`. **Shared storage** — `hostname`-only mount.
- **Search / vector** (Meilisearch, Typesense, Qdrant) — scoped API
  keys, never master; Qdrant ships HTTP + gRPC (`grpcConnectionString`).

For exotic types, `zerops_knowledge query="<service>"` returns the
canonical page. Reserved-keys atom covers keys forbidden in
`envVariables` (`HOSTNAME` in run = `BUILD_FAILED` 4-5s, empty logs).
