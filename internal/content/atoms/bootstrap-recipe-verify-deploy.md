---
id: bootstrap-recipe-verify-deploy
priority: 4
phases: [bootstrap-active]
routes: [recipe]
steps: [deploy]
title: "Recipe — verify the already-deployed services"
---

### Verify, don't redeploy

Services are already alive from provision-time `buildFromGit`. Calling
`zerops_deploy` here would push your empty working tree on top and break
the recipe's install.

```
zerops_verify serviceHostname="{hostname}"
```

For every runtime service in the plan. Verify confirms HTTP responsiveness,
log health, startup completion, and subdomain exposure (if enabled).

If verification fails, read the runtime logs for the failing service:

```
zerops_logs serviceHostname="{hostname}"
```

Failures in recipe-provisioned services usually trace to:

- a missing project-level env var from step 2 (check `zerops_env` output)
- a managed-service dependency that's slower than `priority:` implied
- a user-added constraint that wasn't in the recipe's import YAML

Do NOT attempt a fresh `zerops_deploy` before confirming the root cause —
it will mask the real failure with an empty-package deploy.
