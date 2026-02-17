# Deploy: Deploying Code to Zerops Services

## Overview

Two concerns: ensure zerops.yml is correct for the runtime (hard), then deploy and verify (easy).

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
Examples: `laravel-jetstream`, `phoenix`, `django`

If the briefing doesn't cover the user's framework specifics, ask for build/deploy details before generating zerops.yml.

### Step 4 — Generate or fix zerops.yml

Use the loaded runtime knowledge as your starting point — it covers build pipeline, deployFiles, ports, and framework-specific patterns.

Present zerops.yml to user for review before deploying.

---

## Prerequisites

Before deploying, ensure these requirements are met:

1. **Git repository required.** `zerops_deploy` uses `zcli push` internally, which requires a git repository in the working directory. If the directory has no `.git`:
   ```bash
   cd /path/to/app && git init && git add -A && git commit -m "deploy"
   ```
   Note: `zerops_deploy` auto-initializes git for local deploys if the directory exists but has no `.git`.

2. **`zerops.yml` must exist** at the working directory root with a `setup:` entry matching the target service hostname. Without it, the build pipeline has no instructions.

3. **Environment variables must be resolved.** Run `zerops_discover service="{hostname}" includeEnvs=true` and verify cross-referenced variables have real values (not `${...}` literals). If unresolved, restart the service and re-check.

---

## Phase 2: Deploy and Monitor

### Important: zcli push is asynchronous

`zerops_deploy` triggers the build pipeline and returns `status=BUILD_TRIGGERED` BEFORE the build completes.
You MUST poll for completion. Do NOT assume deployment is done when the tool returns.

### Dev+stage pattern

If the project has dev+stage service pairs (e.g., `appdev` + `appstage`), follow this order:

1. **Deploy to dev first**: `zerops_deploy targetService="appdev"`
2. **Verify dev** — run the full verification protocol on the dev service
3. **Fix any errors on dev** — iterate until dev passes all checks

**Verification means more than HTTP 200.** Read the response body from health/status endpoints. If dev returns 200 but the body shows errors, the app is broken — fix before deploying to stage.

4. **Deploy to stage**: `zerops_deploy targetService="appstage"`
5. **Verify stage** — run the verification protocol on stage

This is the default flow for projects bootstrapped with the standard dev+stage pattern. Dev is for iterating and fixing. Stage is for final validation.

### Single service — direct

```
zerops_deploy targetService="{hostname}"                              # → BUILD_TRIGGERED
# Wait 5s, then poll zerops_events every 10s until build FINISHED (max 300s)
zerops_events serviceHostname="{hostname}" limit=5                    # → build event terminal
zerops_logs serviceHostname="{hostname}" severity="error" since="5m"  # → no errors
zerops_logs serviceHostname="{hostname}" search="listening|started|ready" since="5m"  # → startup confirmed
zerops_discover service="{hostname}"                                  # → RUNNING
zerops_events serviceHostname="{hostname}" limit=5                    # → final event check
zerops_logs serviceHostname="{hostname}" severity="error" since="2m"  # → no post-startup errors
# If subdomain enabled:
# bash: curl -sfm 10 "{zeropsSubdomain}/health"                      # → HTTP 200
```

### Build failure handling

If zerops_events shows build FAILED:
1. `zerops_logs serviceHostname="{hostname}" severity="error" since="10m"` — runtime errors
2. If insufficient: `bash: zcli service log {hostname} --showBuildLogs --limit 50` — build-specific output (only way to see compilation errors)
3. Common causes: wrong buildCommands, missing dependencies, wrong deployFiles path
4. Fix zerops.yml, redeploy. Max 2 retries before asking user.

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
| 2 | Trigger deploy | zerops_deploy targetService="{hostname}" | status=BUILD_TRIGGERED |
| 3 | Poll build | zerops_events serviceHostname="{hostname}" limit=5, every 10s, max 300s | Build FINISHED |
| 4 | Check errors | zerops_logs serviceHostname="{hostname}" severity="error" since="5m" | No errors |
| 5 | Confirm startup | zerops_logs serviceHostname="{hostname}" search="listening|started|ready" since="5m" | At least one match |
| 6 | Verify running | zerops_discover service="{hostname}" | RUNNING |
| 7 | HTTP health | bash: curl -sfm 10 "{zeropsSubdomain}/health" | 200 (skip if no endpoint) |

CRITICAL: zerops_deploy returns BEFORE build completes (status=BUILD_TRIGGERED). You MUST poll
zerops_events for completion. Wait 5s after deploy, then poll every 10s until build event shows
terminal status. Max 300s (30 polls).

If build FAILED: check zerops_logs severity="error" since="10m", then fallback to
bash: zcli service log {hostname} --showBuildLogs --limit 50.

If deploy fails, retry once. Report deployment result — URL if subdomain is enabled.
```