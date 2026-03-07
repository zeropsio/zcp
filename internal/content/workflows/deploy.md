# Deploy: Deploying Code to Zerops Services

## Overview

Two concerns: ensure zerops.yml is correct for the runtime (hard), then deploy and verify with iteration (the harder part).

---

<!-- STACKS:BEGIN -->
<!-- STACKS:END -->

---

## Phase 1: Configuration Check

### Step 1 — Discover target service

```
zerops_discover service="{hostname}" includeEnvs=true
```

Note the service type (nodejs@22, go@1, etc.) and status.
- **RUNNING** → subsequent deploy. zerops.yml likely exists already.
- **READY_TO_DEPLOY** → first deploy. zerops.yml may need creation.

**Route based on result:**
- **RUNNING + zerops.yml exists + no changes** → Skip to Phase 2 (re-deploy)
- **RUNNING + no zerops.yml or needs changes** → Continue to Step 3
- **READY_TO_DEPLOY** → First deploy, continue to Step 2
- **Service not found** → Wrong hostname or not created. Use bootstrap workflow.
- **Dev+stage pair detected** → Follow dev-first flow in Phase 2

### Step 2 — Check zerops.yml

Does a `zerops.yml` exist in the user's project with a `setup: {hostname}` entry?

- **YES and user is re-deploying** → skip to Phase 2.
- **NO or user wants to create/fix it** → continue to Step 3.

### Step 3 — Load contextual knowledge for the runtime

**Mandatory before generating or modifying zerops.yml.**

Call `zerops_knowledge` with the discovered runtime type (from Step 1):
```
zerops_knowledge runtime="{runtime-type}"
```

Examples:
- `zerops_knowledge runtime="nodejs@22"` — for Next.js, Express, Nest.js, etc.
- `zerops_knowledge runtime="go@1"` — for any Go framework
- `zerops_knowledge runtime="python@3.12"` — for Django, FastAPI, Flask, etc.
- `zerops_knowledge runtime="php-nginx@8.4"` — for Laravel, Symfony, etc.

**What you get back:**
- **Runtime exceptions**: PHP (build≠run base), Python (addToRunPrepare), Node.js (node_modules in deployFiles), deploy patterns (tilde syntax, multi-base)
- **Common gotchas**: Missing deployFiles, wrong paths, initCommands vs prepareCommands, protocol values

**If generating or modifying zerops.yml**, also load infrastructure knowledge for YAML schema and rules:
```
zerops_knowledge scope="infrastructure"
```
Skip this if just re-deploying existing code with unchanged zerops.yml.

**For complex recipes** (multi-base builds, unusual patterns), also check:
```
zerops_knowledge recipe="{recipe-name}"
```
Examples: `laravel`, `phoenix`, `django`

If the briefing doesn't cover the user's framework specifics, ask for build/deploy details before generating zerops.yml.

### Step 4 — Generate or fix zerops.yml

Use the loaded runtime knowledge as your starting point — it covers build pipeline, deployFiles, ports, and framework-specific patterns.

Present zerops.yml to user for review before deploying.

---

## Prerequisites

Before deploying, ensure these requirements are met:

1. **Git handled automatically.** `zerops_deploy` auto-initializes a git repository if no `.git` directory exists. For self-deploy, `includeGit` is auto-forced — `.git` always persists.

2. **`zerops.yml` must exist** at the working directory root with a `setup:` entry matching the target service hostname. Without it, the build pipeline has no instructions.

3. **`includeGit=true` requires `deployFiles: [.]`** — when deploying with `includeGit=true`, the `.git/` directory is sent alongside code. Listing individual paths in `deployFiles` (e.g., `[src, node_modules]`) breaks because git expects the full repo structure. Always use `deployFiles: [.]` with `includeGit=true`.

3. **Environment variables must be resolved.** Run `zerops_discover service="{hostname}" includeEnvs=true` and verify cross-referenced variables have real values (not `${...}` literals). If unresolved, restart the service and re-check.

---

## Phase 2: Deploy and Monitor

### zerops_deploy blocks until completion

`zerops_deploy` blocks until the build pipeline completes. It returns the final status (`DEPLOYED` or `BUILD_FAILED`) along with build duration. No manual polling needed.

### Dev+stage pattern

If the project has dev+stage service pairs (e.g., `appdev` + `appstage`), follow this order:

1. **Deploy to dev first**: `zerops_deploy targetService="appdev"` — self-deploy (sourceService auto-inferred, includeGit auto-forced). SSHFS mount auto-reconnects after deploy, no remount needed. Files are already on the dev container via SSHFS mount — deploy runs the build pipeline and ensures deployFiles persist.
2. **Start dev server** (dev uses `zsc noop --silent` — no server runs after deploy): `zerops_deploy` blocks until SSH is ready — kill previous process and start via Bash tool with `run_in_background=true` (server in SSH foreground): `ssh {devHostname} "cd /var/www && {start_command}"`. Check startup via `TaskOutput` after 3-5s — look for startup message, not errors. **Implicit-webserver runtimes (php-nginx, php-apache, nginx, static): skip this step** — the web server starts automatically after deploy.
3. **Verify dev**: `zerops_subdomain serviceHostname="appdev" action="enable"` then `zerops_verify serviceHostname="appdev"` — must return status=healthy
4. **Fix any errors on dev** — if `zerops_verify` returns degraded/unhealthy, read the `checks` array for diagnosis. Iterate until status=healthy.

5. **Deploy to stage from dev**: `zerops_deploy sourceService="appdev" targetService="appstage"` — SSH mode: pushes source from dev container, zerops runs the `setup: appstage` build pipeline for production output
6. **Verify stage**: `zerops_subdomain serviceHostname="appstage" action="enable"` then `zerops_verify serviceHostname="appstage"` — must return status=healthy

This is the default flow for projects bootstrapped with the standard dev+stage pattern. Dev is for iterating and fixing. Stage is for final validation.

**Health checks apply to stage only.** The stage zerops.yml entry should include `run.healthCheck` (continuous liveness monitoring) and optionally `deploy.readinessCheck` (deployment-time traffic gating). Dev entries must NOT have health checks — dev uses `start: zsc noop --silent` and the agent starts/stops the server manually via SSH. A healthCheck on dev would cause Zerops to restart the container whenever the agent stops the server for iteration. Exception: implicit-webserver runtimes (php-nginx, php-apache, nginx, static) CAN use healthCheck on dev — the web server auto-starts, no manual lifecycle needed.

For rapid iteration on dev, see the "Dev iteration: manual start cycle" section in the bootstrap workflow. Dev services use `start: zsc noop --silent` — after every deploy to dev, the agent must start the server manually via SSH before `zerops_verify` can succeed. Implicit-webserver runtimes (php-nginx, php-apache, nginx, static) don't need manual start after deploy — the web server restarts automatically.

### Single service — direct

```
zerops_deploy targetService="{hostname}"
zerops_subdomain serviceHostname="{hostname}" action="enable"
zerops_verify serviceHostname="{hostname}"
```

### Verification iteration loop

When `zerops_verify` returns "degraded" or "unhealthy", iterate — do not give up after one failure:

**Iteration 1–3 (auto-fix):**

1. **Diagnose** — read the `checks` array from `zerops_verify` response:
   - service_running: fail → service not running, check deploy status
   - no_error_logs: info → advisory — error-severity logs found. Read detail. If SSH/infra noise, ignore. If app errors, investigate with `zerops_logs`
   - startup_detected: fail → app crashed on start, check `zerops_logs severity="error" since="5m"`
   - no_recent_errors: info → advisory — same as above. Recent error-severity logs found
   - http_health: fail → endpoint broken, check `detail` for HTTP status
   - http_status: fail → managed service connectivity issue, check `detail` for which connection failed

2. **Fix** — based on diagnosis:
   - Build error → fix zerops.yml (buildCommands, deployFiles, start)
   - Runtime error → fix app code
   - Env var issue → fix zerops.yml envVariables
   - Connection error → verify managed service RUNNING, check hostname/port

3. **Redeploy**:
   - Self-deploy: `zerops_deploy targetService="{hostname}"`
   - Cross-deploy: `zerops_deploy sourceService="{devHostname}" targetService="{stageHostname}"`

4. **Re-verify** — `zerops_verify serviceHostname="{hostname}"` — check status=healthy

**After 3 failed iterations**: Stop and report to user with what was tried and current error state.

**Common fix patterns:**

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| "command not found" in build | Wrong buildCommands | Check runtime knowledge |
| "module not found" in build | Missing dependency install | Add install step to buildCommands |
| App crashes: "connection refused" | Wrong DB/cache host env var | Check envVariables vs discovered vars |
| HTTP 502 | Subdomain not activated | Call zerops_subdomain action="enable" |
| Empty response body | App not listening on 0.0.0.0 | Add HOST=0.0.0.0 to envVariables |

### Multiple services — agent orchestration

For deploying 3+ services, spawn deploy agents to prevent context rot:

1. `zerops_discover` — list all runtime services to deploy
2. For each service, spawn in parallel:
   ```
   Task(subagent_type="general-purpose", model="sonnet", prompt=<deploy agent prompt>)
   ```
3. After ALL agents complete: `zerops_discover` — your own final verification

### Deploy-Service Agent Prompt

Replace `{hostname}` with actual value.

```
You deploy code to Zerops service "{hostname}" and verify it works.

Execute IN ORDER. Every step requires verification.

| # | Action | Tool | Verify |
|---|--------|------|--------|
| 1 | Verify exists | zerops_discover service="{hostname}" | RUNNING or READY_TO_DEPLOY |
| 2 | Deploy | zerops_deploy targetService="{hostname}" | status=DEPLOYED (blocks until complete) |
| 3 | Check errors | zerops_logs serviceHostname="{hostname}" severity="error" since="5m" | No errors |
| 4 | Confirm startup | zerops_logs serviceHostname="{hostname}" search="listening|started|ready" since="5m" | At least one match |
| 5 | Verify running | zerops_discover service="{hostname}" | RUNNING |
| 6 | Activate subdomain | zerops_subdomain serviceHostname="{hostname}" action="enable" | Success or already_enabled. Response contains `subdomainUrls` |
| 7 | HTTP health | bash: curl -sfm 10 "{subdomainUrl}/health" (from enable response) | 200 with valid body |
| 8 | Connectivity | bash: curl -sfm 10 "{subdomainUrl}/status" (from enable response) | 200 with connections "ok" (skip if no /status) |

zerops_deploy blocks until the build pipeline completes. It returns DEPLOYED or BUILD_FAILED with
build duration. No manual polling needed.

Step 6: ALWAYS call zerops_subdomain action="enable" after deploy — even if enableSubdomainAccess was
set in import. The enable response contains subdomainUrls — this is the ONLY source for subdomain
URLs. zerops_discover does not include subdomain URLs. The call is idempotent.
subdomainUrls from the enable response are already full URLs — do NOT prepend https://.

If subdomain URL returns 502, verify the app internally first: curl http://{hostname}:{port}/health.
Internal network access uses hostname directly — no subdomain needed.

SSHFS mount auto-reconnects after deploy — no explicit remount needed.

If any check fails, iterate: diagnose (check logs, capture response bodies), fix the issue,
redeploy, re-verify. Max 3 iterations. If build FAILED: zerops_deploy response includes buildLogs
with last 50 lines of build pipeline output. Check for wrong buildCommands, missing deps, wrong base version.

Report: status (pass/fail) + which checks passed/failed + subdomain URL (from enable response).
```