# Develop: Build, Deploy, Fix, and Operate Zerops Services

## Overview

This workflow covers all application development, deploying code, investigating issues, and iterating fixes. If you're arriving from bootstrap, the user's original request (their app, feature, dashboard, API, etc.) has NOT been built yet — bootstrap only created infrastructure with a verification server. Implement the user's actual application here, then deploy it.

Start with investigation if something is broken, or skip to prepare/deploy if code is ready.

---

<section name="investigate">
## Investigate (when diagnosing issues)

Two phases: gather ALL data systematically (do not skip steps or jump to conclusions), then diagnose with Zerops domain knowledge.

### Phase 1: Data Gathering

Collect everything before diagnosing. The most common mistake is jumping to conclusions after Step 1.

#### Step 1 — Service status

```
zerops_discover service="{hostname}" includeEnvs=true
```

Note: status, container count, resource usage, env var keys (keys only). If the service doesn't exist, stop here.

#### Step 2 — Recent events

```
zerops_events serviceHostname="{hostname}" limit=10
```

Events give the timeline — what happened and when. Look for: failed deploys, unexpected restarts, scaling events, env var changes.

#### Step 3 — Error logs

```
zerops_logs serviceHostname="{hostname}" severity="error" since="1h"
```

Look for: stack traces, connection errors, missing module errors, port binding failures.

#### Step 4 — Warning logs

```
zerops_logs serviceHostname="{hostname}" severity="warning" since="1h"
```

Warnings often reveal the root cause that errors only show the symptom of.

#### Step 5 — Pattern search

If Steps 3-4 revealed specific error messages, search for recurring patterns:

```
zerops_logs serviceHostname="{hostname}" search="{error pattern}" since="24h"
```

This shows whether the issue is new or recurring.

### Phase 2: Diagnosis

#### Step 6 — Match against common Zerops issues

**Note**: For detailed platform rules, call `zerops_knowledge scope="infrastructure"` for the full reference, or `zerops_knowledge runtime="{type}" services=[...]` for stack-specific context.

| Symptom | Likely cause | Verify / Fix |
|---------|-------------|--------------|
| ECONNREFUSED between services | Using `https://` internally | Use `http://` for all internal connections |
| 502 Bad Gateway | App binds to localhost | Bind `0.0.0.0` (check runtime exceptions) |
| Env vars show literal `${...}` | Wrong reference syntax | `${...}` is ONLY for cross-service refs (`${db_hostname}`). envSecrets are auto-injected — never reference them in envVariables |
| envSecret changes not visible | Container has stale env | envSecrets require service **restart** (not just redeploy) to take effect |
| Build FAILED | Wrong buildCommands or missing deps | Check build logs |
| Service not starting | Port outside range or bad start cmd | Ports 10-65435, verify `start` in zerops.yaml |
| DB connection timeout | Wrong connection string or DB not running | Use `http://hostname:port`, verify DB status |
| Deploy OK but app broken | Missing env vars or wrong format | `zerops_discover includeEnvs=true` (keys only). If keys present but app still broken, add `includeEnvValues=true` to inspect actual values |
| HTTP 000 (connection refused) | Server not running on dev service | Start server via SSH first |
| SSH hangs after starting server | Expected — server runs in foreground | Use Bash `run_in_background=true` |
| SSH exit 255 after deploy | Deploy created new container — old SSH sessions die | Open new SSH connection, start server again |
| `jq: command not found` via SSH | jq not in containers | Pipe outside: `ssh dev "curl ..." \| jq .` |
| SSHFS stale after deploy | Container replaced | Auto-reconnects — wait ~10s |

#### Step 7 — Load knowledge for uncommon issues

```
zerops_knowledge query="{error category or message}"
```

Or with runtime context:
```
zerops_knowledge runtime="{runtime-type}" services=["{service1}", ...]
```

#### Step 8 — Report findings

Structure the diagnosis as:
- **Problem**: What is happening? (one sentence)
- **Evidence**: Which specific logs/events confirm this? (quote the relevant lines)
- **Root cause**: Why is it happening?
- **Recommended fix**: What specific action resolves it?

After diagnosing, if the fix requires code or config changes, proceed to Prepare and Deploy below.

### Multi-Service Investigation

For 3+ services, spawn debug agents to prevent context rot:

1. `zerops_discover` — identify services with issues (non-RUNNING, recent errors)
2. For each problematic service, spawn in parallel:
   ```
   Task(subagent_type="general-purpose", model="sonnet", prompt=<debug agent prompt>)
   ```
3. Collect findings from all agents, produce a summary

#### Debug-Service Agent Prompt

Replace `{hostname}` with actual value.

```
You diagnose issues with Zerops service "{hostname}".

Gather ALL data before diagnosing. Do not jump to conclusions.

| # | Action | Tool | What to look for |
|---|--------|------|-----------------|
| 1 | Check status | zerops_discover service="{hostname}" includeEnvs=true | Status, containers, resources, env var keys (keys only). If keys present but app still broken, add `includeEnvValues=true` to inspect actual values |
| 2 | Recent events | zerops_events serviceHostname="{hostname}" limit=10 | Failed deploys, restarts, scaling |
| 3 | Error logs | zerops_logs serviceHostname="{hostname}" severity="error" since="1h" | Error messages, stack traces |
| 4 | Warning logs | zerops_logs serviceHostname="{hostname}" severity="warning" since="1h" | Connection issues, retries |
| 5 | Pattern search (if step 3 found errors) | zerops_logs serviceHostname="{hostname}" search="{error from step 3}" since="24h" | Recurring vs new issue |

Report as: Problem, Evidence, Root Cause, Recommended Fix.
Common Zerops issues: https:// for internal (use http://), dashes in env refs (use underscores), ports outside 10-65435, app binding localhost instead of 0.0.0.0.
Use zerops_knowledge for Zerops-specific troubleshooting guidance if needed.
```

### Restart as Last Resort

Only after full investigation — if root cause is unclear and service needs immediate recovery:

```
zerops_manage action="restart" serviceHostname="{hostname}"
```

Then monitor: `zerops_logs serviceHostname="{hostname}" severity="error" since="5m"`

A restart without understanding the root cause means the problem will likely recur.
</section>

---

## Deploy Flow

Two concerns: ensure zerops.yaml is correct for the runtime (hard), then deploy and verify with iteration (the harder part).

---

<!-- STACKS:BEGIN -->
<!-- STACKS:END -->

---

<section name="deploy-prepare">
## Prepare: Discover State and Plan Work

This is the development workflow for all code work on Zerops services. First discover what exists, then implement what the user wants.

- **Service has only a verification server** (hello-world from bootstrap with /, /health, /status) → replace it with the user's actual application
- **Service has existing application code** → modify it according to the user's request
- **Code is ready, just needs deploying** → skip to deploy step

### Discover target services

```
zerops_discover includeEnvs=true
```

Returns env var keys only (no values). Note each service type (nodejs@22, go@1, etc.) and status.
- **RUNNING** → subsequent deploy. zerops.yaml likely exists already.
- **READY_TO_DEPLOY** → first deploy after bootstrap. zerops.yaml may need creation.

**Route based on result:**
- **RUNNING + zerops.yaml exists + no changes** → Skip to deploy step
- **RUNNING + no zerops.yaml or needs changes** → Load knowledge, fix config
- **READY_TO_DEPLOY** → First deploy, load knowledge, generate config
- **Service not found** → Wrong hostname or not created. Use bootstrap workflow.

### Check zerops.yaml

Does a `zerops.yaml` exist with `setup:` entries for all target hostnames?

- **YES and user is re-deploying** → skip to deploy step.
- **NO or user wants to create/fix it** → use the runtime + service knowledge included below.

### Prerequisites

Before deploying, verify:

1. **`zerops.yaml` must exist** at the working directory root with `setup:` entries matching target service hostnames.
2. **`includeGit=true` requires `deployFiles: [.]`** — individual paths break git structure.
3. **`deployFiles` completeness check**: When cherry-picking files (not using `.`), verify every listed path exists AND that your `run.start` command and framework can find all required files. Common misses: `app/` (Laravel/PHP), `src/` (many frameworks), lock files, config dirs. Run `ls` to check.
4. **Environment variables must be resolved.** Run `zerops_discover includeEnvs=true` (keys only) and verify cross-referenced variable names exist. If values need inspection, add `includeEnvValues=true`.
5. **NEVER hardcode credential values.** Always use `${hostname_varName}` references.

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

**Prerequisites**: dev service mounted, zerops.yaml with dev entry, code on mount path.

1. **Deploy to dev**: `zerops_deploy targetService="{devHostname}"` — self-deploy (sourceService auto-inferred, includeGit auto-forced). SSHes into dev container, runs `git init` + `zcli push`. **Deploy creates a new container — ALL previous SSH sessions to {devHostname} are dead (exit 255).** SSHFS mount auto-reconnects after deploy.
2. **Start dev server** via **NEW** SSH connection (old sessions dead). Deploy activated envVariables — no server runs because `start: zsc noop --silent`. Start: Bash tool `run_in_background=true`. Env vars are now OS env vars. **Implicit-webserver runtimes (php-nginx, php-apache, nginx, static): skip this step** — web server starts automatically.
3. **Verify dev**: `zerops_verify serviceHostname="{devHostname}"` — must return status=healthy
4. **Iterate if needed** — if unhealthy, enter iteration loop (see below): diagnose → fix → redeploy → re-verify (max 3 iterations)
5. **Deploy to stage from dev**: `zerops_deploy sourceService="{devHostname}" targetService="{stageHostname}"` — pushes from dev container to stage. Zerops runs the stage build pipeline. Stage has real `start:` command — server auto-starts.
6. **Connect shared storage to stage** (if applicable): `zerops_manage action="connect-storage" serviceHostname="{stageHostname}" storageHostname="{storageHostname}"`
7. **Verify stage**: `zerops_verify serviceHostname="{stageHostname}"` — must return status=healthy
8. **Present URLs** from `zerops_discover` (subdomainUrl field) to user

### Dev → Stage: What to know

- **Stage has a real start command — server starts automatically after deploy.** No SSH start needed (unlike dev). Zerops monitors the app via healthCheck and restarts on failure.
- **Stage runs the full build pipeline.** `buildCommands` execute in a clean build container — may include compilation (Go, Rust, Java) or asset building (TypeScript, Vite) that dev didn't need.
- **After deploy, only `deployFiles` content exists.** Anything installed manually via SSH is gone. Use `prepareCommands` or `buildCommands` for runtime deps.
- **Health checks apply to stage only.** Dev entries must NOT have health checks — dev uses `zsc noop` and agent controls lifecycle manually.
</section>

<section name="deploy-execute-dev">
### Dev-only mode — deploy flow

**Prerequisites**: dev service mounted, zerops.yaml with dev entry, code on mount path.

1. **Deploy to dev**: `zerops_deploy targetService="{devHostname}"` — self-deploy. **New container — all SSH sessions to {devHostname} die.**
2. **Start dev server** via **NEW** SSH (old sessions dead, dev uses `zsc noop --silent`): start with `run_in_background=true`. **Implicit-webserver runtimes: skip — auto-starts.**
3. **Verify**: `zerops_verify serviceHostname="{devHostname}"` — must return status=healthy
4. **Iterate if needed** — diagnose → fix → restart/redeploy → re-verify
</section>

<section name="deploy-execute-simple">
### Simple mode — deploy flow

**Prerequisites**: service mounted, zerops.yaml with setup entry (real start command + healthCheck).

1. **Deploy**: `zerops_deploy targetService="{hostname}"` — server auto-starts (real start command + healthCheck).
2. **Verify**: `zerops_verify serviceHostname="{hostname}"` — must return status=healthy
3. **If failed** — diagnose, fix, redeploy.
</section>

<section name="deploy-iteration">
### Dev iteration: manual start cycle

After `zerops_deploy` to dev, env vars from zerops.yaml are available as OS env vars. The container runs `zsc noop --silent` — no server process. The agent starts the server via SSH.

**Key facts:**
1. **Deploy = new container. All previous SSH sessions die (exit 255).** Always open a new SSH connection after deploy. Background tasks running via old SSH are gone.
2. **After deploy, env vars are OS env vars.** Available immediately when the server starts. NEVER hardcode values or pass them inline.
3. **Code on SSHFS mount is live on the container** — watch-mode frameworks reload automatically, others need manual restart.
4. **Redeploy only when zerops.yaml itself changes** (envVariables, ports, buildCommands). Code-only changes on the mount just need a server restart.

**The cycle:**
1. **Edit code** on the mount path — changes appear instantly in the container at `/var/www/`.
2. **Kill previous server and start new one** via SSH — use the Bash tool with `run_in_background=true`.
3. **Check startup** — read background task output via `TaskOutput`. Look for startup message or errors.
4. **Test endpoints** — `ssh {devHostname} "curl -s localhost:{port}/health"` | jq .
5. **If broken**: fix code on mount, stop server task, restart from step 2.

**Implicit-webserver runtimes (php-nginx, php-apache, nginx, static) skip manual start.** The web server starts automatically after deploy.

**Heavy operations (composer install, npm install, cargo build, etc.) may get killed if the container is near its RAM floor.** Temporarily scale up before running: `ssh {devHostname} "zsc scale ram +2GiB 10m"` — auto-reverts after the duration. Adjust size and duration to match the operation.

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
- Build error → fix zerops.yaml (buildCommands, deployFiles, start)
- Runtime error → fix app code
- Env var issue → fix zerops.yaml envVariables mapping
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

| Process killed / OOM during SSH work | Container near RAM floor | `zsc scale ram +2GiB 10m` before heavy ops (auto-reverts) |

**After 3 failed iterations**: Stop and report to user with what was tried and current error state.

**For deeper investigation**: If the issue is unclear after quick diagnosis, use the full investigation protocol in the Investigate section above (Steps 1-8). Gather logs, events, and platform knowledge before attempting another fix.
</section>

<section name="deploy-push-dev">
### Push-Dev Deploy Strategy

For services bootstrapped with dev+stage pattern using SSH push deployment.
Follow the dev+stage pattern above.
Key commands: zerops_deploy targetService="{devHostname}" (self-deploy),
zerops_deploy sourceService="{devHostname}" targetService="{stageHostname}" (cross-deploy).
</section>

<section name="deploy-push-git">
### Push-Git Deploy Strategy

Push committed code from the dev container to an external git repository (GitHub/GitLab).

**First time setup** (once per service):
1. Get a GitHub/GitLab token from the user (Contents: Read and write for GitHub, write_repository for GitLab)
2. Store as project env var: `zerops_env action="set" project=true variables=["GIT_TOKEN={token}"]`
3. Commit: `ssh {devHostname} "cd /var/www && git add -A && git commit -m 'initial commit'"`
4. Push with remote URL: `zerops_deploy targetService="{devHostname}" strategy="git-push" remoteUrl="{url}"`

**Subsequent deploys:**
1. Commit with a descriptive message:
   `ssh {devHostname} "cd /var/www && git add -A && git commit -m '{what changed}'"`
2. Push to remote:
   `zerops_deploy targetService="{devHostname}" strategy="git-push"`
3. If CI/CD is configured: build triggers automatically.
   Monitor: `zerops_events serviceHostname="{stageHostname}"`
4. If no CI/CD: deploy to stage manually:
   `zerops_deploy sourceService="{devHostname}" targetService="{stageHostname}"`

**Set up CI/CD:** `zerops_workflow action="start" workflow="cicd"`
**Export with import.yaml:** `zerops_workflow action="start" workflow="export"`
**Switch strategy:** `zerops_workflow action="strategy" strategies={"{hostname}":"push-dev"}`
</section>

<section name="deploy-manual">
### Manual Deploy Strategy

You control when and what to deploy. ZCP does not start a guided workflow for manual strategy.

**Deploy directly:**
- Dev: `zerops_deploy targetService="{devHostname}"`
- Stage from dev: `zerops_deploy sourceService="{devHostname}" targetService="{stageHostname}"`
- Simple: `zerops_deploy targetService="{hostname}"`

**After deploy:**
- Verify health: `zerops_verify serviceHostname="..."`
- Subdomain persists across re-deploys. Check `zerops_discover` for current status and URL.

**Dev services (zsc noop):** Server does not auto-start after deploy. Start manually via SSH.
**Stage/simple services:** Server auto-starts with healthCheck.

**Code-only changes (no zerops.yaml change):** Edit on mount, restart server via SSH. No redeploy needed.

**Switch to guided deploys:** `zerops_workflow action="strategy" strategies={"hostname":"push-dev"}`
</section>
