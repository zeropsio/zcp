---
id: develop-env-cheatsheet-clickhouse-kafka
priority: 2
phases: [develop-active]
envelopeDeployStates: [never-deployed]
managedTypes: [clickhouse, kafka]
coverageExempt: "per-managed-type env cheatsheet — fires only when that dep type is in project scope (RC-D/F7 design); the canonical fixtures are postgres-focused, so rare types (clickhouse/kafka/object-storage/search) are <1% session frequency and covered by the live showcase eval flow (eval-zcp)"
title: "Env keys — ClickHouse / Kafka"
---

### ClickHouse / Kafka env keys

- **ClickHouse + Kafka** — multi-port; match the driver (`portHttp` /
  `portMysql` / `portNative` / `portPostgresql`; Kafka builds the broker
  URL from `hostname:port`, no `connectionString`).
- **Elevated DDL** — ClickHouse `superUser`/`superUserPassword`, only
  when DDL needs them.
