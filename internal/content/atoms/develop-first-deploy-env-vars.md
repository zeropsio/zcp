---
id: develop-first-deploy-env-vars
priority: 2
phases: [develop-active]
deployStates: [never-deployed]
title: "Use the discovered env var catalog when wiring the app"
---

### Env var catalog from bootstrap

Bootstrap discovered every managed service's env var keys at provision
and recorded them in the session. When you scaffold `zerops.yaml` and
write the app, reference those keys verbatim — **do not guess
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

**If you need to check keys at any point:** `zerops_discover
service="{hostname}" includeEnvs=true` returns the key list. The values
are redacted — the catalog only tracks names, which is enough for
cross-service wiring.
