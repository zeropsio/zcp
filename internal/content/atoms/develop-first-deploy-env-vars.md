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

Common managed-service keys (verify against the actual catalog for your
services):

- PostgreSQL: `connectionString`, `host`, `port`, `user`, `password`,
  `dbName`.
- MariaDB: same shape.
- KeyDB / Valkey: `connectionString`, `host`, `port`.
- Object Storage: `accessKeyId`, `secretAccessKey`, `apiUrl`,
  `bucketName`.

**Cross-service reference form** — inside `run.envVariables` of a
runtime service:

```yaml
envVariables:
  DATABASE_URL: ${db_connectionString}
  DB_HOST: ${db_host}
```

The platform rewrites `${db_connectionString}` at deploy time by
looking up service `db`'s env var named `connectionString`. A wrong
spelling resolves to the literal string `${db_connectionString}` and
the app fails at connect time.

**Re-check at any point:** `zerops_discover service="{hostname}"
includeEnvs=true` returns the key list. Values are redacted by default;
names alone are enough for cross-service wiring. Add
`includeEnvValues=true` only for troubleshooting.
