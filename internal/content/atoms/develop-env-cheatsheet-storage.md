---
id: develop-env-cheatsheet-storage
priority: 2
phases: [develop-active]
envelopeDeployStates: [never-deployed]
managedTypes: [object-storage, shared-storage]
coverageExempt: "per-managed-type env cheatsheet — fires only when that dep type is in project scope (RC-D/F7 design); the canonical fixtures are postgres-focused, so rare types (clickhouse/kafka/object-storage/search) are <1% session frequency and covered by the live showcase eval flow (eval-zcp)"
title: "Env keys — object / shared storage"
---

### Storage env keys

- **Object storage** — S3-compatible: `apiUrl`, `accessKeyId`,
  `secretAccessKey`, `bucketName`. No `region`.
- **Shared storage** — no env keys; mounted at `/mnt/<hostname>` via
  import.yaml `mount:` (never `zerops.yaml`).
