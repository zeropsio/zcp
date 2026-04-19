---
id: bootstrap-recipe-verify-deploy
priority: 4
phases: [bootstrap-active]
routes: [recipe]
steps: [deploy]
title: "Recipe — deploy the imported app"
---

### Deploy the recipe's services

The recipe's import YAML created the service skeleton; the recipe body
includes the initial code layout that goes on-disk. Deploy each runtime
service once the platform reports `ACTIVE`:

```
zerops_deploy targetService="{hostname}"
zerops_verify  serviceHostname="{hostname}"
```

Recipe-provided services usually succeed on first deploy because the
recipe was tuned end-to-end. If the initial deploy fails, read
`DEPLOY_FAILED` metadata (not build logs) for the failing `initCommand`
— stderr lives in runtime logs, not build logs.
