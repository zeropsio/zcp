---
id: develop-env-var-model
priority: 1
phases: [develop-active]
envelopeDeployStates: [never-deployed]
title: "Env-var model — the zerops.yaml lists what the app reads"
references-atoms: [develop-first-deploy-env-vars]
---

### Every value the app reads is a line in its `zerops.yaml`

`run.envVariables` is the complete list of what the process reads.
Values live in the **vault** — the project's **Shared** one
(`zerops_env project=true`) and each service's own (`serviceHostname=…`)
— and reach the app only through a line referencing them. Zerops still
injects unreferenced values today; strict isolation stops that, so never
rely on it.

```yaml
run:
  envVariables:
    APP_KEY: ${APP_KEY_SECRET}           # own value, else Shared
    DATABASE_URL: postgresql://${db_user}:${db_password}@${db_hostname}:${db_port}/${db_dbName}
    REDIS_URL: ${cache_connectionString} # another service's value
    API_BASE_URL: http://api:3000        # literal
    NODE_ENV: production                 # literal mode flag
```

- `${KEY}` reads the service's own vault value, else the Shared one.
- `${host_KEY}` reads another service's value (managed-service
  credentials, a sibling's entries) — fetch the keys, never guess them.
- Another runtime's HTTP endpoint has no env ref: use the internal-DNS
  literal `http://<hostname>:<port>`, http never https.
- `build.envVariables` is the build's list; there only Shared values and
  `${host_KEY}` resolve, never the service's own vault value.

A reference nothing provides reaches the app as the literal `${NAME}`
and fails the deploy preflight — store the value first.

### Name the line what the app reads, the value something else

`APP_KEY: ${APP_KEY}` (same name both sides) reaches the app as the
literal `${APP_KEY}`, and so does every other line referencing
`APP_KEY`. Until Zerops resolves same-name references, store
`APP_KEY_SECRET` and write `APP_KEY: ${APP_KEY_SECRET}`.

### Secrets go in the vault as sensitive

Store a secret with `zerops_env action="set" … sensitive=true` (a
secret-shaped key name defaults to sensitive). Reads return it masked.
The person manages values in Mate's Vault panel or the Zerops dashboard
— never ask them to paste a secret into the chat; ask them to add it
there under the name you reference.

A set restarts the services whose deployed lines read the key
(`readers`); nothing reads it → add the line, then deploy.
