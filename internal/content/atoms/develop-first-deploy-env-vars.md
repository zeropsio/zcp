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
and connect-time fails. Once values are in your context (via
`includeEnvValues=true`), reference them back by `${name}` in any
subsequent commands — don't paste raw secrets into tool calls.

### Per-managed-type cheatsheet

- **Postgres / MariaDB / MySQL** — `connectionString` resolves to
  `protocol://${user}:${password}@${hostname}:${port}` and **omits
  the database name**. For Prisma / Drizzle / sqlx / SQLAlchemy /
  Sequelize, compose explicitly with `/${db_dbName}` appended (see
  worked example in the env-var-model atom).
- **Elevated DDL credentials** — `superUser` / `superUserPassword` on
  Postgres + ClickHouse. Pull from catalog only when DDL is needed.
- **ClickHouse + Kafka** — multiple ports; match the driver
  (`portHttp` / `portMysql` / `portNative` / `portPostgresql` for
  ClickHouse; build broker URL from `hostname:port` for Kafka —
  no `connectionString`).
- **Object storage** — S3-compatible: `apiUrl`, `accessKeyId`,
  `secretAccessKey`, `bucketName`. No `region`.
- **Shared storage** — `hostname`-only mount (`mount:` in
  zerops.yaml, not a network service).
- **Search / vector** (Meilisearch, Typesense, Qdrant) — scoped API
  keys; pick the narrow key for app code, never the master key.
  Qdrant ships both HTTP (`connectionString`) and gRPC
  (`grpcConnectionString`); match the client library.

For exotic types, `zerops_knowledge query="<service>"` returns the
canonical reference page.

The reserved-keys atom lists the few keys that cannot appear in
`envVariables` (`HOSTNAME` in `run.envVariables` is the headline
trap — `BUILD_FAILED` in 4-5s with empty logs).
