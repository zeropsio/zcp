# Deploy: Deploying Code to Zerops Services

## Overview

Two concerns: ensure zerops.yml is correct for the runtime (hard), then deploy and verify with iteration (the harder part).

---

<!-- STACKS:BEGIN -->
<!-- STACKS:END -->

---

<section name="deploy-prepare">
## Prepare: Configuration Check

### Discover target services

```
zerops_discover includeEnvs=true
```

Note each service type (nodejs@22, go@1, etc.) and status.
- **RUNNING** → subsequent deploy. zerops.yml likely exists already.
- **READY_TO_DEPLOY** → first deploy after bootstrap. zerops.yml may need creation.

**Route based on result:**
- **RUNNING + zerops.yml exists + no changes** → Skip to deploy step
- **RUNNING + no zerops.yml or needs changes** → Load knowledge, fix config
- **READY_TO_DEPLOY** → First deploy, load knowledge, generate config
- **Service not found** → Wrong hostname or not created. Use bootstrap workflow.

### Check zerops.yml

Does a `zerops.yml` exist with `setup:` entries for all target hostnames?

- **YES and user is re-deploying** → skip to deploy step.
- **NO or user wants to create/fix it** → use the runtime + service knowledge included below.

### Prerequisites

Before deploying, verify:

1. **`zerops.yml` must exist** at the working directory root with `setup:` entries matching target service hostnames.
2. **`includeGit=true` requires `deployFiles: [.]`** — individual paths break git structure.
3. **Environment variables must be resolved.** Run `zerops_discover includeEnvs=true` and verify cross-referenced variables have real values (not `${...}` literals).
4. **NEVER hardcode credential values.** Always use `${hostname_varName}` references.

Platform knowledge (YAML schemas, runtime rules) is included in this guide automatically.
For framework-specific patterns: `zerops_knowledge recipe="{name}"`
</section>

<section name="deploy-execute-overview">
## Deploy: Execute

`zerops_deploy` blocks until the build pipeline completes. It returns the final status (`DEPLOYED` or `BUILD_FAILED`) along with build duration. No manual polling needed.

**Git handled automatically.** `zerops_deploy` auto-initializes a git repository if no `.git` directory exists. For self-deploy, `includeGit` is auto-forced.

> **Path distinction:** SSHFS mount path `/var/www/{devHostname}/` is LOCAL to the zcpx container only.
> Inside the target container, code lives at `/var/www/`. Never use the mount path as
> `workingDir` in `zerops_deploy` — the default `/var/www` is always correct.

> **Files are already on the dev container** via SSHFS mount — deploy does not "send" files there. Deploy runs the build pipeline (buildCommands, deployFiles), activates envVariables, and restarts the process.
</section>

<section name="deploy-execute-standard">
### Standard mode (dev+stage) — deploy flow

**Prerequisites**: dev service mounted, zerops.yml with dev entry, code on mount path.

1. **Deploy to dev**: `zerops_deploy targetService="{devHostname}"` — self-deploy (sourceService auto-inferred, includeGit auto-forced). SSHes into dev container, runs `git init` + `zcli push`. SSHFS mount auto-reconnects after deploy.
2. **Start dev server** (deploy activated envVariables — no server runs because `start: zsc noop --silent`): start server via SSH (Bash tool `run_in_background=true`). Env vars are now OS env vars. **Implicit-webserver runtimes (php-nginx, php-apache, nginx, static): skip this step** — web server starts automatically.
3. **Enable dev subdomain**: `zerops_subdomain serviceHostname="{devHostname}" action="enable"` — returns `subdomainUrls`
4. **Verify dev**: `zerops_verify serviceHostname="{devHostname}"` — must return status=healthy
5. **Iterate if needed** — if unhealthy, enter iteration loop (see below): diagnose → fix → redeploy → re-verify (max 3 iterations)
6. **Deploy to stage from dev**: `zerops_deploy sourceService="{devHostname}" targetService="{stageHostname}"` — pushes from dev container to stage. Zerops runs the stage build pipeline. Stage has real `start:` command — server auto-starts.
7. **Connect shared storage to stage** (if applicable): `zerops_manage action="connect-storage" serviceHostname="{stageHostname}" storageHostname="{storageHostname}"`
8. **Enable stage subdomain**: `zerops_subdomain serviceHostname="{stageHostname}" action="enable"`
9. **Verify stage**: `zerops_verify serviceHostname="{stageHostname}"` — must return status=healthy
10. **Present both URLs** to user

### Dev → Stage: What to know

- **Stage has a real start command — server starts automatically after deploy.** No SSH start needed (unlike dev). Zerops monitors the app via healthCheck and restarts on failure.
- **Stage runs the full build pipeline.** `buildCommands` execute in a clean build container — may include compilation (Go, Rust, Java) or asset building (TypeScript, Vite) that dev didn't need.
- **After deploy, only `deployFiles` content exists.** Anything installed manually via SSH is gone. Use `prepareCommands` or `buildCommands` for runtime deps.
- **Health checks apply to stage only.** Dev entries must NOT have health checks — dev uses `zsc noop` and agent controls lifecycle manually.
</section>

<section name="deploy-execute-dev">
### Dev-only mode — deploy flow

**Prerequisites**: dev service mounted, zerops.yml with dev entry, code on mount path.

1. **Deploy to dev**: `zerops_deploy targetService="{devHostname}"` — self-deploy.
2. **Start dev server** (dev uses `zsc noop --silent`): start via SSH with `run_in_background=true`. **Implicit-webserver runtimes: skip — auto-starts.**
3. **Enable subdomain**: `zerops_subdomain serviceHostname="{devHostname}" action="enable"`
4. **Verify**: `zerops_verify serviceHostname="{devHostname}"` — must return status=healthy
5. **Iterate if needed** — diagnose → fix → restart/redeploy → re-verify
</section>

<section name="deploy-execute-simple">
### Simple mode — deploy flow

**Prerequisites**: service mounted, zerops.yml with setup entry (real start command + healthCheck).

1. **Deploy**: `zerops_deploy targetService="{hostname}"` — server auto-starts (real start command + healthCheck).
2. **Enable subdomain**: `zerops_subdomain serviceHostname="{hostname}" action="enable"`
3. **Verify**: `zerops_verify serviceHostname="{hostname}"` — must return status=healthy
4. **If failed** — diagnose, fix, redeploy.
</section>

<section name="deploy-iteration">
### Dev iteration: manual start cycle

After `zerops_deploy` to dev, env vars from zerops.yml are available as OS env vars. The container runs `zsc noop --silent` — no server process. The agent starts the server via SSH.

**Key facts:**
1. **After deploy, env vars are OS env vars.** Available immediately when the server starts. NEVER hardcode values or pass them inline.
2. **Code on SSHFS mount is live on the container** — watch-mode frameworks reload automatically, others need manual restart.
3. **Redeploy only when zerops.yml itself changes** (envVariables, ports, buildCommands). Code-only changes on the mount just need a server restart.

**The cycle:**
1. **Edit code** on the mount path — changes appear instantly in the container at `/var/www/`.
2. **Kill previous server and start new one** via SSH — use the Bash tool with `run_in_background=true`.
3. **Check startup** — read background task output via `TaskOutput`. Look for startup message or errors.
4. **Test endpoints** — `ssh {devHostname} "curl -s localhost:{port}/health"` | jq .
5. **If broken**: fix code on mount, stop server task, restart from step 2.

**Implicit-webserver runtimes (php-nginx, php-apache, nginx, static) skip manual start.** The web server starts automatically after deploy.

**Piping rule:** `ssh {dev} "curl -s localhost:{port}/api"` | jq . — pipe OUTSIDE SSH. `jq` is not available inside containers.
</section>

<section name="deploy-verify">
## Verify: Health Check and Iteration

When `zerops_verify` returns "degraded" or "unhealthy", iterate — do not give up after one failure:

**Diagnosis from checks array:**
- service_running: fail → service not running, check deploy status
- startup_detected: fail → app crashed on start, check `zerops_logs severity="error" since="5m"`
- no_error_logs / no_recent_errors: info → advisory, check detail for real errors vs noise
- http_health: fail → endpoint broken, check `detail` for HTTP status
- http_status: fail → managed service connectivity issue, check `detail` for which connection failed

**Fix based on diagnosis:**
- Build error → fix zerops.yml (buildCommands, deployFiles, start)
- Runtime error → fix app code
- Env var issue → fix zerops.yml envVariables mapping
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
| Timeout on /status | Wrong port or app not binding | Check run.ports vs actual listen port |

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
