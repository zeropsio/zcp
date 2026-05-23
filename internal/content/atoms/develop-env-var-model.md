---
id: develop-env-var-model
priority: 1
phases: [develop-active]
envelopeDeployStates: [never-deployed]
title: "Env-var model — auto-inject + renames"
references-atoms: [develop-env-var-channels, develop-first-deploy-env-vars, develop-reserved-env-names]
---

### Where values come from

Project envs auto-inject as OS env vars into every container — app
code reads them directly via `process.env.KEY`, no `zerops.yaml` line
required.

`run.envVariables` lines exist for two purposes only:

1. **Rename a cross-service value** — destination on the left,
   `${hostname_varname}` source on the right. Example:
   ```yaml
   run:
     envVariables:
       DATABASE_URL: postgresql://${db_user}:${db_password}@${db_hostname}:${db_port}/${db_dbName}
       REDIS_URL: ${cache_connectionString}
   ```
2. **Mode flag with a per-setup literal** — `NODE_ENV: development`
   in `setup: appdev`, `NODE_ENV: production` in `setup: appstage`.

### Self-shadow — never the same name on both sides

```yaml
db_hostname: ${db_hostname}   # WRONG — destination == source
APP_KEY: ${APP_KEY}           # WRONG — re-declaring a project env
```

Source resolves to the literal string `${db_hostname}` (8 chars
including dollar-brace), reaches `process.env` as that literal, and
the framework crashes when it parses it as a hostname.
