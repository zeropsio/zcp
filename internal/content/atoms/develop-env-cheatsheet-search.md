---
id: develop-env-cheatsheet-search
priority: 2
phases: [develop-active]
envelopeDeployStates: [never-deployed]
managedTypes: [meilisearch, typesense, qdrant, elasticsearch]
coverageExempt: "per-managed-type env cheatsheet — fires only when that dep type is in project scope (RC-D/F7 design); the canonical fixtures are postgres-focused, so rare types (clickhouse/kafka/object-storage/search) are <1% session frequency and covered by the live showcase eval flow (eval-zcp)"
title: "Env keys — search / vector"
---

### Search / vector env keys

- **Search / vector** (Meilisearch, Typesense, Qdrant, Elasticsearch) —
  scoped API keys; pick the narrow key, never master. Qdrant ships HTTP
  (`connectionString`) + gRPC (`grpcConnectionString`).
