# Bootstrap: Setting Up a Zerops Project

## Overview

Two phases: generate correct configuration (the hard part), then deploy and verify (the easy part).

**Default pattern: dev+stage service pairs.** Every runtime service gets `{app}dev` + `{app}stage` hostnames. Managed services are shared. User can opt into single-service mode if requested explicitly.

---

<!-- STACKS:BEGIN -->
<!-- STACKS:END -->

---

## Phase 1: Configuration

### Step 0 — Detect project state and route

Call `zerops_discover` to see what exists. Then classify:

| Discover result | State | Action |
|----------------|-------|--------|
| No runtime services | FRESH | Full bootstrap (Steps 1–5 → Phase 2) |
| Some requested services exist, others don't | PARTIAL | Generate import.yml for MISSING services only, then Phase 2 |
| All requested services exist as dev+stage pairs | CONFORMANT | Skip to Phase 2 (deploy only) or suggest deploy workflow |
| Services exist but not as dev+stage pairs | EXISTING | Ask user: add dev+stage pairs, or work with existing? |

**Dev+stage detection:** Look for `{name}dev` + `{name}stage` hostname pairs.
**PARTIAL example:** User wants bun + postgresql. Discover shows postgresql exists but no runtime services → create only the runtime services.

### Step 1 — Identify stack components + environment mode

From the user's request, identify:
- **Runtime services**: type + framework (e.g., nodejs@22 with Next.js, go@1 with Fiber, bun@1.2 with Hono)
- **Managed services**: type + version (e.g., postgresql@16, valkey@7.2, elasticsearch@8.16)

**Verify all types against the Available Service Stacks section above.**

If the user hasn't specified, ask. Don't guess frameworks — the build config depends on it.

**Environment mode** (ask if not specified):
- **Standard** (default): Creates `{app}dev` + `{app}stage` + shared managed services. NON_HA mode.
- **Simple**: Creates single `{app}` + managed services. Only if user explicitly requests it.

Default = standard (dev+stage). If the user says "just one service" or "simple setup", use simple mode.

**Workflow scope** (infer from context, do not ask unless ambiguous):
- **Full** (default): Configure → validate → deploy dev → verify → deploy stage → verify.
- **Dev-only**: Configure → deploy to dev only, skip stage. When user says "just get it running" or "prototype."
- **Quick**: Skip config, deploy with existing zerops.yml. Only when user says "just deploy" and config already exists → redirect to deploy workflow.

### Step 2 — Load stack-specific knowledge

**Mandatory.** Call `zerops_knowledge` with the identified runtime and services:
```
zerops_knowledge runtime="{runtime-type}" services=["{service1}", "{service2}", ...]
```

Examples:
- `zerops_knowledge runtime="bun@1.2" services=["postgresql@16"]`
- `zerops_knowledge runtime="nodejs@22" services=["postgresql@16", "valkey@7.2"]`
- `zerops_knowledge runtime="php-nginx@8.4" services=["mariadb@11"]`
- `zerops_knowledge runtime="go@1" services=[]` (runtime only, no managed services)

**What you get back:**
- **Runtime exceptions**: binding rules (0.0.0.0!), deploy patterns, framework-specific gotchas
- **Matching recipes**: pre-built configurations for common stacks (load with `zerops_knowledge recipe="name"`)
- **Service cards**: ports, auto-injected env vars, connection string templates, HA behavior
- **Wiring patterns**: ${hostname_var} system, envSecrets vs envVariables, connection examples
- **Version validation**: checks requested versions against available stacks

**For complex recipes** (multi-base builds, unusual patterns), also check:
```
zerops_knowledge recipe="{recipe-name}"
```
Examples: `bun`, `bun-hono`, `laravel-jetstream`, `ghost`, `django`, `phoenix`

### Step 3 — Load infrastructure knowledge

**Mandatory before generating YAML.** Call:
```
zerops_knowledge scope="infrastructure"
```

**What you get back:**
- **Zerops platform model**: projects, services, containers, routing
- **import.yml schema**: structure, fields, rules, priorities, modes
- **zerops.yml schema**: build/run pipeline, deployFiles, ports, prepareCommands
- **Env var system**: cross-service references (`${hostname_var}`), envSecrets vs envVariables
- **Build/deploy lifecycle**: build and run are SEPARATE containers, cache rules
- **Rules & pitfalls**: common mistakes, validation rules, port ranges

Steps 2 and 3 together provide everything needed for YAML generation — stack-specific knowledge (Step 2) plus platform rules (Step 3).

**After receiving both, verify these before generating YAML:**

1. **Binding**: Briefing specifies 0.0.0.0 — plan HOST/BIND env vars accordingly
2. **Deploy files**: Note the exact deployFiles pattern — wrong path is the #1 error
3. **Build vs run base**: Different bases needed? (PHP: php-nginx, Python: addToRunPrepare, Static: run.base=static)
4. **Cache paths**: Note package manager cache dirs — missing cache = slow rebuilds
5. **Connection strings**: Use the exact pattern from service cards, not generic URLs

If the briefing doesn't cover your stack, call `zerops_knowledge recipe="{name}"` before generating YAML.

### Step 4 — Generate import.yml

Using the loaded knowledge from Steps 2+3, generate import.yml following the infrastructure rules for structure, priority, mode, env var wiring, and ports.

**Hostname pattern** (from Step 1): Standard mode (default) creates `{app}dev` + `{app}stage` pairs with shared managed services. Simple mode creates a single `{app}`. If the user didn't specify, ask before generating.

### Step 5 — Generate zerops.yml

For each runtime service, generate zerops.yml using the loaded runtime example from Step 2 as starting point. The infrastructure knowledge from Step 3 covers the YAML schema rules. Together they provide build pipeline, deployFiles, ports, and framework-specific decisions.

**Health endpoint recommendation:**
When scaffolding new application code, recommend adding `/health` (returns 200 when app is running) and `/status` (returns 200 with managed service connectivity info) endpoints. These enable deeper verification in Phase 2 — but verification adapts based on what's available.

### Step 6 — Validate

**Self-check against common import failures before proceeding:**

| Check | What to verify |
|-------|---------------|
| Ports match | `run.ports.port` = what app actually listens on |
| Deploy files exist | `deployFiles` includes actual build output path |
| **deployFiles/start consistency** | If `deployFiles` uses tilde (`dist/~`), start must NOT reference the stripped dir (use `index.js` not `dist/index.js`). Without tilde, dir is preserved (`dist/index.js` is correct). **#1 bootstrap error.** |
| Start command | `run.start` runs the app, not the build tool |
| Env var refs | Cross-references use underscores: `${db_hostname}` not `${my-db_hostname}` |
| Mode present | Every managed service has `mode: NON_HA` or `mode: HA` |

Present both import.yml and zerops.yml to the user for review before proceeding to Phase 2.

---

## Phase 2: Deployment and Verification

**Core principle: Dev is for iterating and fixing. Stage is for final validation. Fix errors on dev before deploying to stage.**

### Important: zcli push is asynchronous

`zerops_deploy` triggers the build pipeline and returns `status=BUILD_TRIGGERED` BEFORE the build completes.
You MUST poll for completion. Do NOT assume deployment is done when the tool returns.

### Verification Protocol (7-point)

Every deployment must pass this protocol before being considered complete.

| # | Check | Tool / Method | Pass criteria |
|---|-------|---------------|---------------|
| 1 | Build/deploy completed | zerops_events limit=5 (poll 10s, max 300s) | Build event status terminal + not FAILED |
| 2 | Service status | zerops_discover | RUNNING |
| 3 | No error logs | zerops_logs severity="error" since="5m" | Empty |
| 4 | Startup confirmed | zerops_logs search="listening\|started\|ready" since="5m" | At least one match |
| 5 | No post-startup errors | zerops_logs severity="error" since="2m" | Empty |
| 6 | HTTP health check | bash: curl -sfm 10 "{zeropsSubdomain}/health" | HTTP 200 |
| 7 | Managed svc connectivity | bash: curl -sfm 10 "{zeropsSubdomain}/status" OR log search | 200 with svc status / log match |

**Notes:**
- Check 1 is CRITICAL — zerops_deploy returns before build completes. Wait 5s after deploy, then poll zerops_events every 10s until build finishes (max 300s / 30 polls).
- Check 4: framework-dependent — search for `listening on`, `started server`, `ready to accept`.
- Check 6: get `zeropsSubdomain` from `zerops_discover includeEnvs=true`. It is already a full URL — do NOT prepend `https://`.
- Check 7: skip if no managed services. Fallback to log search for `connected|pool|migration`.
- **Graceful degradation:** if the app has no `/health` endpoint, check 4 is the final gate.

**Critical: HTTP 200 does not mean the app works.**
- Check 6: Read the response body. Empty body, "Cannot GET /", or framework error page = NOT healthy.
- Check 7: Read `/status` body. If it shows DB as "disconnected" or "error" = NOT confirmed.
- Always capture response body: `curl -sfm 10 "{url}/health" 2>&1`

**Do NOT deploy to stage until dev passes ALL checks.** Stage is for final validation, not debugging.

### Standard mode (dev+stage) — deploy flow

1. `zerops_import content="<import.yml>"` — create all services
2. `zerops_process processId="<id>"` — wait for all services RUNNING
3. **Env var sync**: `zerops_discover includeEnvs=true` for each runtime service. Verify cross-referenced vars have real values — not empty, not literal `${...}`. If unresolved: `zerops_manage action="restart" serviceHostname="{runtime}"` → re-verify.
4. **Deploy to appdev first**: `zerops_deploy targetService="appdev"`
5. **Verify appdev** — run the full 7-point verification protocol on appdev
6. **Fix any errors on appdev** — iterate until appdev passes all checks
7. **Deploy to appstage**: `zerops_deploy targetService="appstage"`
8. **Verify appstage** — run the 7-point verification protocol on appstage
9. **Present both URLs** to user:
   ```
   Dev:   {appdev zeropsSubdomain}
   Stage: {appstage zeropsSubdomain}
   ```

### Simple mode — deploy flow

1. **Import services:**
   ```
   zerops_import content="<import.yml>"
   zerops_process processId="<id>"               # wait for RUNNING
   ```

   > **CRITICAL — Subdomain:** If your import.yml includes `enableSubdomainAccess: true`, the subdomain is already enabled. Do NOT call `zerops_subdomain action="enable"` after import — it is redundant and wastes a tool call.

2. **Verify environment variables are resolved:**
   ```
   zerops_discover service="<runtime>" includeEnvs=true
   ```
   Check that cross-referenced variables (e.g., `${evaldb_hostname}`) have real values — not empty, not literal `${...}`. If you see unresolved `${...}` literals:
   ```
   zerops_manage action="restart" serviceHostname="<runtime>"
   # Wait 10s, then re-check with zerops_discover includeEnvs=true
   ```

3. **Prepare working directory for deploy:**
   `zerops_deploy` uses `zcli push` which requires a git repository. If your working directory has no `.git`:
   ```bash
   cd /path/to/app && git init && git add -A && git commit -m "deploy"
   ```
   Note: `zerops_deploy` auto-initializes git if the directory exists but has no `.git`, so this step is a safety net.

4. **Deploy and verify:**
   ```
   zerops_deploy targetService="<runtime>" workingDir="/path/to/app"
   # CRITICAL: returns BUILD_TRIGGERED — build is NOT complete yet
   # Wait 5s, then poll zerops_events every 10s (max 300s) until build FINISHED
   ```

Then run the full 7-point verification protocol.

### For 3+ runtime services — agent orchestration

Prevents context rot by delegating per-service work to specialist agents with fresh context.

**Steps:**
1. `zerops_import content="<import.yml>"` — create all services
2. `zerops_process processId="<id>"` — wait until all services reach RUNNING
3. For each **runtime** service, spawn a configure agent (in parallel):
   ```
   Task(subagent_type="general-purpose", model="sonnet", prompt=<configure prompt below>)
   ```
4. For each **managed** service, spawn a verify agent (in parallel):
   ```
   Task(subagent_type="general-purpose", model="haiku", prompt=<verify prompt below>)
   ```
5. After ALL agents complete: `zerops_discover` — your own final verification (do not trust agent self-reports alone)

### Configure-Service Agent Prompt

Replace `{hostname}`, `{type}`, `{env_vars}`, `{managed_services}` with actual discovered values.

```
You configure and deploy Zerops runtime service "{hostname}" ({type}).

Execute IN ORDER. Every step has a verification call — do not skip any.

| # | Action | Tool | Verify |
|---|--------|------|--------|
| 1 | Check state | zerops_discover service="{hostname}" includeEnvs=true | Service exists |
| 2 | Set env vars | zerops_env action="set" serviceHostname="{hostname}" variables=[{env_vars}] | zerops_discover includeEnvs=true — vars present |
| 3 | Verify managed svc env vars | zerops_discover service="{hostname}" includeEnvs=true | Cross-refs resolved (not empty, not literal ${{...}}) |
| 4 | Trigger deploy | zerops_deploy targetService="{hostname}" | status=BUILD_TRIGGERED |
| 5 | Poll build completion | zerops_events serviceHostname="{hostname}" limit=5, every 10s, max 300s | Build event FINISHED |
| 6 | Check error logs | zerops_logs serviceHostname="{hostname}" severity="error" since="5m" | No errors |
| 7 | Confirm startup in logs | zerops_logs serviceHostname="{hostname}" search="listening|started|ready" since="5m" | At least one match |
| 8 | Check post-startup errors | zerops_logs serviceHostname="{hostname}" severity="error" since="2m" | No errors |
| 9 | Get subdomain URL | zerops_discover service="{hostname}" includeEnvs=true | Extract zeropsSubdomain |
| 10 | HTTP health check | bash: curl -sfm 10 "{url}/health" | HTTP 200 (or skip — step 7 = final gate) |
| 11 | Managed svc connectivity | bash: curl -sfm 10 "{url}/status" OR zerops_logs search="connected|pool|migration" | Connectivity confirmed (skip if no managed svcs) |

Pass the env vars from import.yml's envVariables/envSecrets for this service as {env_vars}.
Managed services in the project: {managed_services} (used to decide step 3 and 11).

CRITICAL: zerops_deploy returns BEFORE build completes (status=BUILD_TRIGGERED). You MUST poll
zerops_events for completion. Wait 5s after deploy, then poll every 10s until build event shows
terminal status. Max 300s (30 polls). If build FAILED: check zerops_logs severity="error" since="10m",
then fallback to bash: zcli service log {hostname} --showBuildLogs --limit 50.

Rules:
- Every step requires its verification tool call. Do not self-report success.
- If step 3 finds unresolved vars, call zerops_manage action="restart" serviceHostname="{hostname}" then re-verify.
- If a step fails, retry once. If it fails again, report the failure — do not skip ahead.
- zeropsSubdomain is already a full URL — do NOT prepend https://.
- curl flags: -sfm 10 (silent, fail on error, 10s timeout).
- Internal service connections use http://, never https://.
- Env var cross-references use underscores: ${service_hostname}, not ${service-hostname}.
- Final report format: status (pass/fail) + which checks passed/failed + subdomain URL.
```

### Verify-Managed Agent Prompt

Replace `{hostname}` with actual value.

```
You verify managed Zerops service "{hostname}" is operational.

| # | Action | Tool | Verify |
|---|--------|------|--------|
| 1 | Check status | zerops_discover service="{hostname}" | Status is RUNNING |
| 2 | Check errors | zerops_logs serviceHostname="{hostname}" severity="error" since="1h" | No error logs |

Report status and any errors found. If the service is not RUNNING, report the issue — do not attempt to fix it.
```

### Build failure handling

If zerops_events shows build FAILED:
1. `zerops_logs serviceHostname="{hostname}" severity="error" since="10m"` — runtime errors
2. If insufficient: `bash: zcli service log {hostname} --showBuildLogs --limit 50` — build-specific output (only way to see compilation errors)
3. Common causes: wrong buildCommands, missing dependencies, wrong deployFiles path, app binds to localhost instead of 0.0.0.0
4. Fix zerops.yml, redeploy. Max 2 retries before asking user.

### After completion — next iteration

If the user asks for changes after initial bootstrap:
1. Reuse discovery data — do not re-discover unless services were added/removed.
2. Make the code/config change.
3. Deploy to dev first, verify, then stage. Same dev-first pattern.
4. For config-only changes (env vars, scaling), use configure/scale workflows directly.

