---
id: bootstrap-recipe-import-local
priority: 2
phases: [bootstrap-active]
routes: [recipe]
environments: [local]
steps: [provision]
title: "Recipe local — provision + clone + first run"
coverageExempt: "recipe+local+provision — covered by recipe-local-flow design (Theme 1) + flow-eval-local scenarios"
---

### Provision shape

ZCP transformed the recipe import.yml for local mode:

- Services declaring `zeropsSetup: dev` were dropped (your CWD replaces them).
- The remaining stage runtime keeps `buildFromGit` — Zerops API requires it whenever `zeropsSetup` is set.

Submit the YAML you see in the guide via `zerops_import` as-is. After Zerops pulls upstream code via `buildFromGit`, the stage runtime is RUNNING with the recipe's reference build. Subdomain becomes live within 1-2 minutes; you can hit it to verify the recipe before any local edits.

### After services are RUNNING

```
1. zerops_env action="get" project=true       # surface the project vault
2. zerops_env action="generate-dotenv" setup="<setup-name>"
                                                # renders .env from project + zerops.yaml + .env.local
3. Add ".env" + ".env.local" to .gitignore if not already there
4. Guide user: zcli vpn up <projectId>          # access managed services from your machine
5. composer install / npm install / framework equivalent
6. First local run: php artisan serve / npm run dev / etc.
```

### Iterate locally; deploy when validating

The cloned tree is your editor surface. Local edits do not affect stage until you deploy:

```
zerops_deploy targetService="<stage-hostname>" workingDir="<cwd>"
```

This rebuilds stage with your local code, replacing the upstream-seeded build. Use it whenever you want to validate the production-shape build pipeline against your changes — it is NOT a mandatory bootstrap-completion step.
