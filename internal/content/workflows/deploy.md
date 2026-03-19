# Deploy: Deploying Code to Zerops Services

## Overview

Two concerns: ensure zerops.yml is correct for the runtime (hard), then deploy and verify with iteration (the harder part).

---

<!-- STACKS:BEGIN -->
<!-- STACKS:END -->

---

<section name="deploy-prepare">
## Prepare: Configuration Check

### Discover target service

```
zerops_discover service="{hostname}" includeEnvs=true
```

Note the service type (nodejs@22, go@1, etc.) and status.
- **RUNNING** → subsequent deploy. zerops.yml likely exists already.
- **READY_TO_DEPLOY** → first deploy. zerops.yml may need creation.

**Route based on result:**
- **RUNNING + zerops.yml exists + no changes** → Skip to deploy step
- **RUNNING + no zerops.yml or needs changes** → Load knowledge, fix config
- **READY_TO_DEPLOY** → First deploy, load knowledge, generate config
- **Service not found** → Wrong hostname or not created. Use bootstrap workflow.

### Check zerops.yml

Does a `zerops.yml` exist with a `setup: {hostname}` entry?

- **YES and user is re-deploying** → skip to deploy step.
- **NO or user wants to create/fix it** → load knowledge and generate.

### Load contextual knowledge

For new or modified zerops.yml, load runtime knowledge:
```
zerops_knowledge runtime="{runtime-type}"
```

For complex recipes (multi-base builds, unusual patterns):
```
zerops_knowledge recipe="{recipe-name}"
```

Platform knowledge (YAML schemas, rules) is included in this guide automatically.

### Prerequisites

Before deploying, verify:

1. **`zerops.yml` must exist** at the working directory root with a `setup:` entry matching the target service hostname.
2. **`includeGit=true` requires `deployFiles: [.]`** — individual paths break git structure.
3. **Environment variables must be resolved.** Run `zerops_discover includeEnvs=true` and verify cross-referenced variables have real values.
4. **NEVER hardcode credential values.** Always use `${hostname_varName}` references.
</section>

<section name="deploy-execute-overview">
## Deploy: Execute

`zerops_deploy` blocks until the build pipeline completes. It returns the final status (`DEPLOYED` or `BUILD_FAILED`) along with build duration. No manual polling needed.

**Git handled automatically.** `zerops_deploy` auto-initializes a git repository if no `.git` directory exists. For self-deploy, `includeGit` is auto-forced.
</section>

<section name="deploy-execute-standard">
### Standard mode: Dev+Stage deploy flow

1. **Deploy to dev**: `zerops_deploy targetService="{devHostname}"` — self-deploy. SSHFS mount auto-reconnects.
2. **Start dev server** (dev uses `zsc noop --silent` — no server after deploy): start via SSH with `run_in_background=true`. **Implicit-webserver runtimes (php-nginx, php-apache, nginx, static): skip — auto-starts.**
3. **Verify dev**: `zerops_subdomain serviceHostname="{devHostname}" action="enable"` then `zerops_verify serviceHostname="{devHostname}"` — must return status=healthy
4. **Fix errors on dev** — iterate until healthy.
5. **Deploy to stage from dev**: `zerops_deploy sourceService="{devHostname}" targetService="{stageHostname}"`
6. **Verify stage**: `zerops_subdomain serviceHostname="{stageHostname}" action="enable"` then `zerops_verify serviceHostname="{stageHostname}"` — must return status=healthy

**Health checks apply to stage only.** Dev entries must NOT have health checks — dev uses `zsc noop` and agent controls lifecycle manually. Stage has real `start` command and auto-restarts via healthCheck.

**Dev iteration:** After `zerops_deploy` to dev, env vars are OS env vars. Container runs `zsc noop`. Agent starts server via SSH. Code changes on SSHFS mount are live — only redeploy when zerops.yml changes.
</section>

<section name="deploy-execute-dev">
### Dev-only mode: Single service deploy

1. **Deploy to dev**: `zerops_deploy targetService="{devHostname}"` — self-deploy.
2. **Start dev server**: via SSH with `run_in_background=true`. **Implicit-webserver runtimes: skip.**
3. **Enable subdomain**: `zerops_subdomain serviceHostname="{devHostname}" action="enable"`
4. **Verify**: `zerops_verify serviceHostname="{devHostname}"` — must return status=healthy
5. **Iterate if needed** — fix errors, restart, re-verify.
</section>

<section name="deploy-execute-simple">
### Simple mode: Direct deploy

1. **Deploy**: `zerops_deploy targetService="{hostname}"` — server auto-starts (real start command + healthCheck).
2. **Enable subdomain**: `zerops_subdomain serviceHostname="{hostname}" action="enable"`
3. **Verify**: `zerops_verify serviceHostname="{hostname}"` — must return status=healthy
4. **If failed** — diagnose, fix, redeploy.
</section>

<section name="deploy-verify">
## Verify: Health Check and Iteration

When `zerops_verify` returns "degraded" or "unhealthy", iterate:

**Diagnosis from checks array:**
- service_running: fail → service not running, check deploy status
- startup_detected: fail → app crashed, check `zerops_logs severity="error" since="5m"`
- http_health: fail → endpoint broken, check `detail` for HTTP status
- http_status: fail → managed service connectivity issue

**Fix based on diagnosis:**
- Build error → fix zerops.yml (buildCommands, deployFiles, start)
- Runtime error → fix app code
- Env var issue → fix zerops.yml envVariables
- Connection error → verify managed service RUNNING, check hostname/port

**Redeploy:**
- Self-deploy: `zerops_deploy targetService="{hostname}"`
- Cross-deploy: `zerops_deploy sourceService="{devHostname}" targetService="{stageHostname}"`

**Re-verify** — `zerops_verify serviceHostname="{hostname}"` — check status=healthy

**Common fix patterns:**

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| "command not found" in build | Wrong buildCommands | Check runtime knowledge |
| "module not found" in build | Missing dependency install | Add install step to buildCommands |
| App crashes: "connection refused" | Wrong DB/cache host env var | Check envVariables vs discovered vars |
| HTTP 502 | Subdomain not activated | Call zerops_subdomain action="enable" |
| Empty response body | App not listening on 0.0.0.0 | Add HOST=0.0.0.0 to envVariables |

**After 3 failed iterations**: Stop and report to user with what was tried and current error state.
</section>

<section name="deploy-push-dev">
### Push-Dev Deploy Strategy

For services bootstrapped with dev+stage pattern using SSH push deployment.
Follow the dev+stage pattern above.
Key commands: zerops_deploy targetService="{devHostname}" (self-deploy),
zerops_deploy sourceService="{devHostname}" targetService="{stageHostname}" (cross-deploy).
</section>

<section name="deploy-ci-cd">
### CI/CD Deploy Strategy

For services connected to a Git repository with automated deployments.
After connecting the repository via Zerops dashboard:
1. Push code to the connected branch
2. Zerops automatically triggers build pipeline
3. Monitor via zerops_process or zerops_events
4. Verify via zerops_verify after deploy completes
</section>

<section name="deploy-manual">
### Manual Deploy Strategy

For services managed via manual zerops_deploy calls without dev+stage pattern.
Direct deploy: zerops_deploy targetService="{hostname}"
Suitable for services where the user manages their own deployment workflow.
</section>
