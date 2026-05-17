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
type. Values are redacted by default; names are enough for wiring.
Pass `includeEnvValues=true` only for troubleshooting.

Cross-service wiring goes in `zerops.yaml` `run.envVariables`. A wrong
spelling on the right-hand side reaches the app as the literal string
and connect-time fails.

### Per-managed-type cheatsheet

- **Postgres / MariaDB / MySQL** — `connectionString` resolves to
  `protocol://${user}:${password}@${hostname}:${port}` and **omits
  the database name**. For Prisma / Drizzle / sqlx / SQLAlchemy /
  Sequelize, compose explicitly with `/${db_dbName}` appended (see
  worked example in the env-var-model atom).
- **Prisma — `migrate dev` errors with `P3014`** because its shadow
  database needs CREATE DATABASE permission the regular user lacks.
  For fresh schemas use `prisma db push` (no shadow); for migration
  files override DATABASE_URL with `${db_superUser}:${db_superUserPassword}`
  only for the `migrate dev` call.
- **Elevated DDL credentials** — `superUser`/`superUserPassword` on
  Postgres + ClickHouse, only when DDL needs them.
- **ClickHouse + Kafka** — multi-port; match driver (`portHttp` /
  `portMysql` / `portNative` / `portPostgresql` for ClickHouse;
  Kafka builds broker URL from `hostname:port`, no `connectionString`).
- **Object storage** — S3-compatible: `apiUrl`, `accessKeyId`,
  `secretAccessKey`, `bucketName`. No `region`.
- **Shared storage** — `hostname`-only mount in zerops.yaml.
- **Search / vector** (Meilisearch, Typesense, Qdrant) — scoped API
  keys; pick the narrow key, never master. Qdrant ships HTTP
  (`connectionString`) + gRPC (`grpcConnectionString`).

For exotic types, `zerops_knowledge query="<service>"` returns the
canonical page. Reserved-keys atom covers the few keys forbidden in
`envVariables` (`HOSTNAME` in run = `BUILD_FAILED` 4-5s, empty logs).
