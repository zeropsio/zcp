# Bootstrap: Setting Up a Zerops Project

## Overview

Two phases: generate correct configuration (the hard part), then deploy and verify (the easy part).

**Default pattern: dev+stage service pairs.** Every runtime service gets `{app}dev` + `{app}stage` hostnames. Managed services are shared. User can opt into single-service mode if requested explicitly.

---

<!-- STACKS:BEGIN -->
<!-- STACKS:END -->

---

## Phase 1: Configuration

### Step 1 — Discover current state

```
zerops_discover
```

Note what already exists. If ALL requested services already exist, skip to Phase 2. If some exist, only create the missing ones in Step 4.

### Step 2 — Identify stack components + environment mode

From the user's request, identify:
- **Runtime services**: type + framework (e.g., nodejs@22 with Next.js, go@1 with Fiber, bun@1.2 with Hono)
- **Managed services**: type + version (e.g., postgresql@16, valkey@7.2, elasticsearch@8.16)

**Verify all types against the Available Service Stacks section above.**

If the user hasn't specified, ask. Don't guess frameworks — the build config depends on it.

**Environment mode** (ask if not specified):
- **Standard** (default): Creates `{app}dev` + `{app}stage` + shared managed services. NON_HA mode.
- **Simple**: Creates single `{app}` + managed services. Only if user explicitly requests it.

Default = standard (dev+stage). If the user says "just one service" or "simple setup", use simple mode.

### Step 3 — Load contextual knowledge BEFORE generating YAML

**This step is mandatory.** Do not generate any YAML until you've loaded the relevant knowledge.

Call `zerops_knowledge` with the identified runtime and services:
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
- **Core principles**: zerops.yml/import.yml structure, port rules, env var system, build pipeline
- **Wiring patterns**: ${hostname_var} system, envSecrets vs envVariables, connection examples

This is a **single call** that assembles exactly what you need for the identified stack. Use it as the authoritative base for YAML generation.

**For complex recipes** (multi-base builds, unusual patterns), also check:
```
zerops_knowledge recipe="{recipe-name}"
```
Examples: `bun`, `bun-hono`, `laravel-jetstream`, `ghost`, `django`, `phoenix`

If the briefing doesn't cover the user's framework specifics, ask for build/deploy details before generating YAML.

### Step 4 — Generate import.yml

Using the loaded knowledge from Step 3, generate import.yml following the core principles for structure, priority, mode, env var wiring, and ports. The briefing includes all rules needed.

### Step 5 — Generate zerops.yml

For each runtime service, generate zerops.yml using the loaded runtime example from Step 3 as starting point. The briefing covers build pipeline, deployFiles, ports, and framework-specific decisions.

**Health endpoint recommendation:**
When scaffolding new application code, recommend adding `/health` (returns 200 when app is running) and `/status` (returns 200 with managed service connectivity info) endpoints. These enable deeper verification in Phase 2 — but verification adapts based on what's available.

### Step 6 — Validate

Run dry-run validation:
```
zerops_import content="<generated import.yml>" dryRun=true
```

**If errors:**
1. Read the error message carefully
2. Fix the specific issue in the YAML
3. Re-validate (max 2 retries)
4. If still failing after 2 fixes, show errors to the user and ask for guidance

**If valid:** Present both import.yml and zerops.yml to the user for review before proceeding to Phase 2.

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
- Checks 2-3: mandatory, pure MCP tools.
- Check 4: framework-dependent patterns — search for `listening on`, `started server`, `ready to accept`.
- Check 5: re-check errors after startup — catches runtime config issues that appear after boot.
- Check 6: get `zeropsSubdomain` from `zerops_discover includeEnvs=true`. It is already a full URL — do NOT prepend `https://`.
- Check 7: skip if no managed services. Fallback to log search for `connected|pool|migration`.
- **Graceful degradation:** if the app has no `/health` endpoint, check 4 is the final gate.

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

```
zerops_import content="<import.yml>"          # create services
zerops_process processId="<id>"               # wait for RUNNING
# Env var sync: verify managed svc vars resolved (see above)
zerops_deploy targetService="<runtime>"       # returns BUILD_TRIGGERED
# CRITICAL: wait 5s, then poll zerops_events every 10s (max 300s) until build FINISHED
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

