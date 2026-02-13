# Bootstrap: Setting Up a Zerops Project

## Overview

Two phases: generate correct configuration (the hard part), then deploy (the easy part).

---

## Phase 1: Configuration

### Step 1 — Discover current state

```
zerops_discover
```

Note what already exists. If ALL requested services already exist, skip to Phase 2. If some exist, only create the missing ones in Step 4.

### Step 2 — Identify stack components

From the user's request, identify:
- **Runtime services**: type + framework (e.g., nodejs@22 with Next.js, go@1 with Fiber, python@3.12 with FastAPI)
- **Managed services**: type + version (e.g., postgresql@16, valkey@7.2, elasticsearch@8.16)

If the user hasn't specified, ask. Don't guess frameworks — the build config depends on it.

### Step 3 — Load knowledge BEFORE generating YAML

**This step is mandatory.** Do not generate any YAML until you've loaded the relevant docs.

For each **runtime service**, call in parallel:
```
zerops_knowledge query="{runtime} zerops.yml {framework}"
```
Examples:
- `zerops_knowledge query="nodejs nextjs zerops.yml"`
- `zerops_knowledge query="go zerops.yml build"`
- `zerops_knowledge query="python fastapi zerops.yml"`

For each **managed service**, call in parallel:
```
zerops_knowledge query="{service} import connection"
```
Examples:
- `zerops_knowledge query="postgresql import connection"`
- `zerops_knowledge query="valkey import"`

Also load if relevant:
- `zerops_knowledge query="import.yml patterns"` — for env var wiring, priority, preprocessor
- `zerops_knowledge query="deploy patterns"` — if the user has a specific deploy method

You now have concrete examples for the user's exact stack. Use them as the base for YAML generation.

If knowledge returns no relevant results for a component, ask the user for their framework's build/deploy specifics before generating YAML.

### Step 4 — Generate import.yml

Using the loaded knowledge, generate `import.yml`. Follow these rules:

**Structure:**
```yaml
services:
  - hostname: <name>
    type: <runtime>@<version>
    ...
```

**Mandatory rules:**
- `mode: NON_HA` or `mode: HA` for ALL managed services (postgresql, mariadb, valkey, keydb, elasticsearch, shared-storage). Omitting passes dry-run but **fails real import**.
- `priority: 10` for managed services (start first), lower for runtime services.
- `enableSubdomainAccess: true` for web-facing runtime services. This replaces the need to call `zerops_subdomain` later.
- No `project:` section — services are added to the existing project.

**Environment variable wiring:**
- Use `envSecrets` for sensitive values (passwords, keys), `envVariables` for configuration.
- Cross-reference other services with `${hostname_varName}` — underscores, not dashes.
- Common patterns (from loaded knowledge):
  - PostgreSQL: `postgresql://${db_user}:${db_password}@db:5432/${db_dbName}`
  - Valkey/Redis: `redis://cache:6379` (or with password: `redis://${cache_password}@cache:6379`)
  - Object storage: `${storage_apiUrl}`, `${storage_accessKeyId}`, `${storage_secretAccessKey}`
- Use `#yamlPreprocessor=on` (first line) + `<@generateRandomString(<64>)>` for generated secrets.

**Ports:**
- Range 10-65435. Ports 0-9 and 65436+ are reserved.
- Port 80 only for PHP services. All others use custom ports (3000, 8080, etc.).

### Step 5 — Generate zerops.yml

For each runtime service, generate a `zerops.yml` entry. **Use the loaded runtime example as your starting point** — do not write from scratch.

**Key decisions per framework (from loaded knowledge):**

| Section | What to get right |
|---------|------------------|
| `build.base` | Match the runtime version from import.yml |
| `build.buildCommands` | Framework-specific: `pnpm build`, `go build -o app`, `pip install`, etc. |
| `build.deployFiles` | Framework-specific: `dist`, `./app`, `./`, `.next + node_modules`, etc. |
| `build.cache` | Package manager cache: `node_modules`, `target`, `.m2`, etc. |
| `build.addToRunPrepare` | Python needs this (`.`), most others don't |
| `run.base` | Usually same as build. Static SPAs use `run.base: static`. PHP uses `php-nginx@X` |
| `run.start` | Entry point: `node dist/index.js`, `./app`, `python -m uvicorn ...` |
| `run.ports` | Must match what the app actually listens on. Use `httpSupport: true` for HTTP |
| `run.documentRoot` | PHP/Nginx/Static only — subdirectory to serve |

**Common mistakes to avoid:**
- Missing `deployFiles` — build output is NOT auto-deployed
- Wrong `deployFiles` path — check the framework's output directory
- Using `initCommands` for package installation — use `prepareCommands` instead
- Missing `node_modules` in deployFiles for Node.js apps that need it at runtime
- Not setting `httpSupport: true` on HTTP ports
- Using `protocol: HTTP` — only `TCP` and `UDP` are valid for `protocol`

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

### Build failure handling

If zerops_events shows build FAILED:
1. `zerops_logs serviceHostname="{hostname}" severity="error" since="10m"` — runtime errors
2. If insufficient: `bash: zcli service log {hostname} --showBuildLogs --limit 50` — build-specific output (only way to see compilation errors)
3. Common causes: wrong buildCommands, missing dependencies, wrong deployFiles path
4. Fix zerops.yml, redeploy. Max 2 retries before asking user.

### Env var sync for managed services

After import, managed service credentials may not be immediately visible to runtime services. Verify before deploying:

1. `zerops_discover includeEnvs=true` for each runtime service
2. Check that cross-referenced vars have real values — not empty, not literal `${...}`
3. If unresolved: `zerops_manage action="restart" serviceHostname="{runtime}"` → re-verify with `zerops_discover includeEnvs=true`

### For 1-2 services — direct

```
zerops_import content="<import.yml>"          # create services
zerops_process processId="<id>"               # wait for RUNNING
zerops_env action="set" serviceHostname="<runtime>" variables=[...]  # if not in import.yml
# Env var sync: verify managed svc vars resolved (see above)
zerops_deploy targetService="<runtime>"       # returns BUILD_TRIGGERED
# CRITICAL: wait 5s, then poll zerops_events every 10s (max 300s) until build FINISHED
```

Then run the full 7-point verification protocol above.

### For 3+ services — agent orchestration

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

---

## Critical Rules

- `http://` for ALL internal service connections — never `https://`. SSL terminates at the L7 balancer.
- `mode: NON_HA` or `mode: HA` is **mandatory** for managed services — dry-run doesn't catch this but real import fails.
- Environment variable cross-references use underscores: `${api_hostname}`, not `${api-hostname}`.
- Ports in range 10-65435 only.
- `enableSubdomainAccess: true` in import.yml for new services. Do NOT also call `zerops_subdomain` — it only works on ACTIVE services.
- `deployFiles` is mandatory in zerops.yml — build output is not auto-deployed.
- `zeropsSubdomain` is already a full URL — do NOT prepend `https://`.
