# Bootstrap: Setting Up a Zerops Project

## Overview

Two phases: generate correct configuration (the hard part), then deploy and verify with iteration (the harder part).

**Default pattern: dev+stage service pairs.** Every runtime service gets `{app}dev` + `{app}stage` hostnames. Managed services are shared. User can opt into single-service mode if requested explicitly.

---

<!-- STACKS:BEGIN -->
<!-- STACKS:END -->

---

## Phase 1: Configuration

<section name="detect">
### Step 0 — Detect project state and route

Call `zerops_discover` to see what exists. Then classify:

| Discover result | State | Action |
|----------------|-------|--------|
| No runtime services | FRESH | Full bootstrap (Steps 1-7 then Phase 2) |
| All requested services exist as dev+stage pairs | CONFORMANT | Suggest deploy workflow instead — services already exist |
| Services exist but not as dev+stage pairs | NON_CONFORMANT | Warn user about non-standard naming, suggest reset or manual approach |

**Dev+stage detection:** Look for `{name}dev` + `{name}stage` hostname pairs.

Route:
- FRESH: proceed normally through all steps
- CONFORMANT: skip bootstrap — route to deploy workflow (`zerops_workflow action="start" workflow="deploy"`)
- NON_CONFORMANT: warn user about non-standard naming, suggest reset or manual approach
</section>

<section name="plan">
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
- **Full** (default): Configure -> validate -> deploy dev -> verify -> deploy stage -> verify.
- **Dev-only**: Configure -> deploy to dev only, skip stage. When user says "just get it running" or "prototype."
- **Quick**: Skip config, deploy with existing zerops.yml. Only when user says "just deploy" and config already exists -> redirect to deploy workflow.
</section>

<section name="load-knowledge">
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
</section>

<section name="generate-import">
### Step 4 — Generate import.yml

Using the loaded knowledge from Steps 2+3, generate import.yml following the infrastructure rules for structure, priority, mode, env var wiring, and ports.

**Hostname pattern** (from Step 1): Standard mode (default) creates `{app}dev` + `{app}stage` pairs with shared managed services. Simple mode creates a single `{app}`. If the user didn't specify, ask before generating.

**Dev vs stage properties** (standard mode):

| Property | Dev (`{app}dev`) | Stage (`{app}stage`) |
|----------|-----------------|----------------------|
| `startWithoutCode` | `true` | omit |
| `maxContainers` | `1` | omit (default) |
| `enableSubdomainAccess` | `true` | `true` |

Dev starts immediately with an empty container (RUNNING). Stage stays in READY_TO_DEPLOY until first deploy from dev — no wasted resources running an empty container.

**Shared storage mount** (if shared-storage is in the stack): Add `mount: [{storage-hostname}]` to both dev and stage service definitions in import.yml. This pre-configures the connection but does NOT make storage available at runtime. You MUST also add `mount: [{storage-hostname}]` in the zerops.yml `run:` section and deploy for the storage to actually mount at `/mnt/{storage-hostname}`.

> **Two kinds of "mount" (disambiguation):** (1) `zerops_mount` — SSHFS tool, mounts service `/var/www` locally for development. (2) Shared storage mount — platform feature, attaches a shared-storage volume at `/mnt/{hostname}` via `mount:` in import.yml + zerops.yml. These are completely unrelated.

> **IMPORTANT**: Import `mount:` only applies to ACTIVE services. Stage services are READY_TO_DEPLOY during import, so the mount pre-configuration silently doesn't apply. After first deploy transitions stage to ACTIVE, connect storage via `zerops_manage action="connect-storage" serviceHostname="{stage}" storageHostname="{storage}"`.

### Step 5 — Plan zerops.yml and application code

For each runtime service, plan zerops.yml using the loaded runtime example from Step 2 as starting point. The infrastructure knowledge from Step 3 covers the YAML schema rules. Together they provide build pipeline, deployFiles, ports, and framework-specific decisions.

**Note:** zerops.yml and application source files are created on the mounted dev service filesystem at `/var/www/{hostname}/` (e.g., `/var/www/appdev/`). This happens after mounting in Phase 2.

**CRITICAL: Dev vs Prod deploy differentiation**

Dev and prod setups serve different purposes and often need different configurations:

| Property | Dev setup | Prod setup |
|----------|-----------|------------|
| Purpose | Iterate, debug, test | Final validation, production-like |
| deployFiles | `[.]` (entire source directory) | Runtime-specific build output |
| start command | Source-mode start | Binary/compiled start |

**Per-runtime deployFiles and start commands:**

| Runtime | Dev deployFiles | Dev start | Prod deployFiles | Prod start |
|---------|----------------|-----------|------------------|------------|
| Go | `[.]` | `go run .` | `[app]` | `./app` |
| Bun | `[.]` | `bun run index.ts` | `[dist, package.json]` | `bun dist/index.js` |
| Node.js | `[.]` | `node index.js` | `[., node_modules]` | `node index.js` |
| Python | `[.]` | `python app.py` | `[.]` | `gunicorn app:app --bind 0.0.0.0:8000` |
| PHP | `[.]` | (web server) | `[., vendor/]` | (web server) |
| Rust | `[.]` | `cargo run --release` | `[{binary}]` | `./{binary}` |
| .NET | `[.]` | `dotnet run` | `[app/~]` | `dotnet {name}.dll` |
| Java | `[.]` | `mvn compile exec:java` | `[target/app.jar]` | `java -jar target/app.jar` |
| Ruby | `[.]` | `bundle exec ruby app.rb` | `[.]` | `bundle exec puma -b tcp://0.0.0.0:3000` |
| Elixir | `[.]` | `mix run --no-halt` | `[_build/prod/rel/{app}/~]` | `bin/{app} start` |
| Deno | `[.]` | `deno run --allow-net --allow-env main.ts` | `[.]` | same |
| Gleam | `[.]` | `gleam run` | `[build/erlang-shipment/~]` | `./entrypoint.sh run` |

**Go specifically**: Dev setup uses `go run .` as start (compiles + runs source each deploy). Prod setup builds a binary in buildCommands and deploys only the binary. The zerops.yml MUST have TWO separate setup entries with different build/deploy pipelines.

**Dev self-deploy lifecycle note:** After deploy, the run container only contains `deployFiles` content. All other files are gone. `deployFiles: [.]` ensures zerops.yml and source files survive the deploy cycle. Without zerops.yml in deployFiles, subsequent deploys from the container fail.

> **CRITICAL — dev `deployFiles` MUST be `[.]`:** Dev containers are volatile. After deploy, ONLY `deployFiles` content survives. If dev setup uses `[dist]`, `[app]`, or any build output path, all source files + zerops.yml are DESTROYED. Further iteration becomes impossible. Dev setup MUST ALWAYS use `deployFiles: [.]` regardless of runtime. No exceptions.

### Step 6 — Application code requirements

Every generated application **MUST** expose these endpoints:

| Endpoint | Response | Purpose |
|----------|----------|---------|
| `GET /` | `"Service: {hostname}"` | Landing page / smoke test |
| `GET /health` | `{"status":"ok"}` (HTTP 200) | Liveness probe |
| `GET /status` | Connectivity JSON (HTTP 200) | **Proves managed service connections** |

#### /status endpoint specification

The `/status` endpoint **MUST actually connect** to each managed service and report results:

```json
{
  "service": "{hostname}",
  "connections": {
    "db": {"status": "ok", "latency_ms": 5},
    "cache": {"status": "ok", "latency_ms": 1}
  }
}
```

**Required verification per service type:**
- **PostgreSQL/MariaDB/MySQL**: Execute `SELECT 1` query
- **Valkey/KeyDB**: Execute `PING` command
- **MongoDB**: Run `db.runCommand({ping: 1})`
- **Object Storage**: Check endpoint reachability
- **Shared Storage**: Check mount path exists and is writable
- **No managed services**: Return `{"service": "{hostname}", "status": "ok"}`

**The `/status` endpoint is the PRIMARY verification gate.** HTTP 200 with all connections reporting "ok" = app works. Anything else = iterate and fix.

**Do NOT generate hello-world apps that skip service connectivity.** The whole point of bootstrap is proving the infrastructure works end-to-end.

### Step 7 — Validate

**Self-check against common import failures before proceeding:**

| Check | What to verify |
|-------|---------------|
| Ports match | `run.ports.port` = what app actually listens on |
| Deploy files exist | `deployFiles` includes actual build output path |
| **deployFiles/start consistency** | If `deployFiles` uses tilde (`dist/~`), start must NOT reference the stripped dir (use `index.js` not `dist/index.js`). Without tilde, dir is preserved (`dist/index.js` is correct). **#1 bootstrap error.** |
| Start command | `run.start` runs the app, not the build tool |
| Env var refs | Cross-references use underscores: `${db_hostname}` not `${my-db_hostname}` |
| Mode present | Every managed service has `mode: NON_HA` or `mode: HA` |
| Dev vs prod | Dev uses `deployFiles: [.]` + source-mode start. Prod uses appropriate build output. **FAIL if dev deployFiles is anything other than `[.]`** — source files will be lost after deploy. |
| /status endpoint | App code includes /status with actual connectivity checks for each managed service |

Present both import.yml and zerops.yml to the user for review before proceeding to Phase 2.
</section>

---

## Phase 2: Deployment and Verification

<section name="discover-envs">
### Env var discovery protocol (mandatory before deploy)

After importing services and waiting for them to reach RUNNING, discover the ACTUAL env vars available to each service. This data is critical for writing correct zerops.yml envVariables and for subagent prompts.

**For each managed service:**
```
zerops_discover service="{managed_hostname}" includeEnvs=true
```

Record which env vars exist. Common patterns by service type:

| Service type | Available env vars | Notes |
|-------------|-------------------|-------|
| PostgreSQL | `{host}_connectionString`, `{host}_host`, `{host}_port`, `{host}_user`, `{host}_password`, `{host}_dbName` | connectionString preferred |
| Valkey/KeyDB | `{host}_host`, `{host}_port` | **No password** — private network, no auth needed |
| MariaDB/MySQL | `{host}_connectionString`, `{host}_host`, `{host}_port`, `{host}_user`, `{host}_password`, `{host}_dbName` | connectionString preferred |
| MongoDB | `{host}_connectionString`, `{host}_host`, `{host}_port`, `{host}_user`, `{host}_password` | connectionString preferred |
| RabbitMQ | `{host}_connectionString`, `{host}_host`, `{host}_port`, `{host}_user`, `{host}_password` | Use AMQP connection string |

**In zerops.yml envVariables**, map discovered vars to what the app expects:
```yaml
envVariables:
  DATABASE_URL: ${db_connectionString}
  REDIS_HOST: ${cache_host}
  REDIS_PORT: ${cache_port}
  # Do NOT add variables that don't exist (e.g., cache_password for Valkey)
```

**ONLY use variables that were actually discovered.** Guessing variable names causes runtime failures. If a variable doesn't appear in discovery, it doesn't exist.
</section>

<section name="deploy">
### Deploy overview

**Core principle: Dev is for iterating and fixing. Stage is for final validation. Fix errors on dev before deploying to stage.**

`zerops_deploy` blocks until the build pipeline completes. It returns the final status (`DEPLOYED` or `BUILD_FAILED`) along with build duration. No manual polling needed.
`zerops_import` blocks until all import processes complete. It returns final statuses (`FINISHED` or `FAILED`) for each process.

### Standard mode (dev+stage) — deploy flow

1. `zerops_import content="<import.yml>"` — create all services (blocks until all processes finish — returns final statuses). Dev gets `startWithoutCode: true` + `maxContainers: 1`, stage omits both.
2. Verify dev services reached RUNNING: `zerops_discover` — stage may be READY_TO_DEPLOY (expected, no empty container wasted)
3. **Mount dev**: `zerops_mount action="mount" serviceHostname="appdev"` — only dev services are mounted
4. **Discover env vars**: For each managed service, `zerops_discover service="{hostname}" includeEnvs=true`. Record the exact env var names available. See "Env var discovery protocol" above.
5. **Create files on mount path**: Write zerops.yml + application source files + .gitignore to `/var/www/appdev/`. Follow the Application Code Requirements (Step 6) and dev vs prod differentiation (Step 5). The zerops.yml `setup:` entries must match ALL service hostnames (both dev and stage). Use only DISCOVERED env vars in envVariables mappings.
6. **Deploy to appdev**: `zerops_deploy targetService="appdev" workingDir="/var/www/appdev" includeGit=true` — local mode, reads from SSHFS mount. `-g` flag includes `.git` directory on the container. The deploy tool auto-initializes a git repo if missing
7. **Remount after deploy**: `zerops_mount action="mount" serviceHostname="appdev"` — deploy replaces the container, making the previous SSHFS mount stale. The new container only has `deployFiles` content. Remount reconnects to the new container. The mount tool auto-detects stale mounts and re-mounts.
8. **Verify appdev**: `zerops_subdomain serviceHostname="appdev" action="enable"` then `zerops_verify serviceHostname="appdev"` — must return status=healthy
9. **Iterate if needed** — if `zerops_verify` returns degraded/unhealthy, enter the iteration loop: diagnose from `checks` array -> fix on mount path -> redeploy -> remount -> re-verify (max 3 iterations)
10. **Deploy to appstage from dev**: After deploy, `/var/www` only contains `deployFiles` content. Dev services **MUST** use `deployFiles: [.]` for SSH cross-service deploy to work — zerops.yml must be present in the working directory. Run: `zerops_deploy sourceService="appdev" targetService="appstage" freshGit=true` — SSH mode: pushes from dev container to stage. Zerops runs the `setup: appstage` build pipeline (production buildCommands + deployFiles). Transitions stage from READY_TO_DEPLOY -> BUILDING -> RUNNING
10b. **Connect shared storage to stage** (if shared-storage is in the stack): `zerops_manage action="connect-storage" serviceHostname="appstage" storageHostname="storage"` — stage was READY_TO_DEPLOY during import, so the import `mount:` did not apply. Now that stage is ACTIVE, connect storage explicitly.
11. **Verify appstage**: `zerops_subdomain serviceHostname="appstage" action="enable"` then `zerops_verify serviceHostname="appstage"` — must return status=healthy
12. **Present both URLs** to user:
    ```
    Dev:   {subdomainUrl from enable}
    Stage: {subdomainUrl from enable}
    ```

### Simple mode — deploy flow

1. **Import services:**
   ```
   zerops_import content="<import.yml>"           # blocks until all processes finish
   zerops_discover                                # verify services reached RUNNING
   ```

   > **Subdomain activation:** `enableSubdomainAccess: true` in import.yml pre-configures routing, but **does NOT activate it**. You MUST call `zerops_subdomain action="enable"` after deploy to activate the L7 balancer route. The enable response contains `subdomainUrls` — this is the **only** source for subdomain URLs. Without the explicit enable call, the subdomain returns 502. The call is idempotent — safe to call even if already active.

2. **Discover env vars:**
   ```
   zerops_discover service="{managed_hostname}" includeEnvs=true
   ```
   Record available env vars. Use ONLY discovered variable names in zerops.yml.

3. **Create files and deploy:**
   Write zerops.yml + app code following Application Code Requirements. Then:
   ```
   zerops_deploy targetService="<runtime>" workingDir="/path/to/app"
   # Blocks until build completes — returns DEPLOYED or BUILD_FAILED
   ```

4. Run the full verification protocol. If it fails, iterate (diagnose -> fix -> redeploy).

### For 2+ runtime service pairs — agent orchestration

Prevents context rot by delegating per-service work to specialist agents with fresh context. **Use this for 2 or more runtime service pairs.** For a single service pair, follow the inline flow above.

**Parent agent steps:**
1. `zerops_import content="<import.yml>"` — create all services (blocks until all processes finish)
2. `zerops_discover` — verify dev services reached RUNNING
3. **Mount all dev services**: `zerops_mount action="mount" serviceHostname="{devHostname}"` for each
4. **Discover ALL env vars**: `zerops_discover service="{hostname}" includeEnvs=true` for every managed service. Record exact var names.
5. For each **runtime** service pair, spawn a Service Bootstrap Agent (in parallel):
   ```
   Task(subagent_type="general-purpose", model="sonnet", prompt=<Service Bootstrap Agent Prompt below>)
   ```
6. For each **managed** service, spawn a verify agent (in parallel):
   ```
   Task(subagent_type="general-purpose", model="haiku", prompt=<Verify-Managed Agent Prompt below>)
   ```
7. After ALL agents complete: `zerops_discover` — your own final verification (do not trust agent self-reports alone)

**CRITICAL: Before spawning agents, you MUST have:**
- All services imported and dev services RUNNING
- All dev services mounted
- All managed service env vars discovered
- Runtime knowledge loaded (from Steps 2+3)

This context is embedded into each agent's prompt. Without it, agents will guess and fail.

### Service Bootstrap Agent Prompt

**This is the CORE handoff.** The subagent gets NO prior context. Everything it needs MUST be in this prompt.

Replace placeholders with actual values. `{envVarSection}` must contain the formatted output from env var discovery — not placeholders.

````
You bootstrap Zerops service pair "{devHostname}" (dev) / "{stageHostname}" (stage).
Runtime: {runtimeType}. This prompt is self-contained — you have no prior context.

## Environment

Files are accessed via SSHFS mount at `{mountPath}` (e.g., `/var/www/appdev/`).
Write files directly to this path — they appear inside the container at `/var/www/`.

## Services

| Role | Hostname | zerops.yml setup |
|------|----------|------------------|
| Dev | {devHostname} | `{devHostname}` |
| Stage | {stageHostname} | `{stageHostname}` |

Managed services in this project: {managedServices}

## Discovered Environment Variables

**CRITICAL**: These are the ONLY variables that exist. Use ONLY these in zerops.yml envVariables.

{envVarSection}

### Mapping in zerops.yml

```yaml
envVariables:
  # Format: YOUR_APP_VAR: ${discovered_key}
  # Only use variables listed above — anything else will be empty at runtime
```

## Runtime Knowledge

{runtimeKnowledge}

## zerops.yml Structure

The zerops.yml MUST have setup entries matching BOTH service hostnames:

```yaml
zerops:
  - setup: {devHostname}
    build:
      base: {runtimeVersion}
      buildCommands: [<from runtime knowledge>]
      deployFiles: [.]   # CRITICAL: MUST be [.] — anything else destroys source files after deploy
      cache: [<runtime-specific cache dirs>]
    run:
      base: {runtimeVersion}
      ports:
        - port: 8080
          httpSupport: true
      envVariables:
        # Map discovered variables to app-expected names
      start: {devStartCommand}   # Source-mode: go run ., bun run index.ts, etc.

  - setup: {stageHostname}
    build:
      base: {runtimeVersion}
      buildCommands: [<from runtime knowledge — may include compilation>]
      deployFiles: [{prodDeployFiles}]
      cache: [<runtime-specific cache dirs>]
    run:
      base: {prodRunBase}
      ports:
        - port: 8080
          httpSupport: true
      envVariables:
        # Same mappings as dev
      start: {prodStartCommand}
```

## Application Requirements

Your app MUST expose these endpoints on port 8080:

| Endpoint | Response | Purpose |
|----------|----------|---------|
| `GET /` | `"Service: {devHostname}"` | Smoke test |
| `GET /health` | `{"status":"ok"}` (200) | Liveness probe |
| `GET /status` | Connectivity JSON (200) | Proves managed service connections |

### /status specification

MUST actually connect to each managed service and report results:

```json
{
  "service": "{devHostname}",
  "connections": {
    "db": {"status": "ok", "latency_ms": 5},
    "cache": {"status": "ok", "latency_ms": 1}
  }
}
```

- PostgreSQL/MariaDB/MySQL: execute `SELECT 1`
- Valkey/KeyDB: execute `PING`
- No managed services: return `{"service": "{devHostname}", "status": "ok"}`

Do NOT generate hello-world apps. The /status endpoint must PROVE real connectivity.

## Tasks

Execute IN ORDER. Every step has verification — do not skip any.

| # | Task | Action | Verify |
|---|------|--------|--------|
| 1 | Write zerops.yml | Write to `{mountPath}/zerops.yml` with both setup entries | File exists with correct setup names |
| 2 | Write app code | HTTP server on :8080 with `/`, `/health`, `/status` | Code references discovered env vars |
| 3 | Write .gitignore | Appropriate for {runtimeType} | File exists |
| 4 | Deploy dev | `zerops_deploy targetService="{devHostname}" workingDir="{mountPath}" includeGit=true` | status=DEPLOYED (blocks until complete) |
| 5 | Verify build | Check zerops_deploy return value | Not BUILD_FAILED or timedOut |
| 5b | Remount | `zerops_mount action="mount" serviceHostname="{devHostname}"` — deploy replaces container, stale mount auto-cleans on remount | Mount path accessible |
| 6 | Activate subdomain | `zerops_subdomain serviceHostname="{devHostname}" action="enable"` | Returns `subdomainUrls` |
| 7 | Verify dev | `zerops_verify serviceHostname="{devHostname}"` | status=healthy |
| 8 | Deploy stage | `zerops_deploy sourceService="{devHostname}" targetService="{stageHostname}" freshGit=true` | status=DEPLOYED (blocks until complete) |
| 9 | Verify stage | `zerops_subdomain action="enable"` + `zerops_verify serviceHostname="{stageHostname}"` | status=healthy |
| 10 | Report | Status (pass/fail) + dev URL + stage URL | — |

## Iteration Loop (when verification fails)

If `zerops_verify` returns "degraded" or "unhealthy", iterate — do NOT skip ahead to stage:

1. **Diagnose**: Read the `checks` array from the `zerops_verify` response:
   | Failed check | Diagnosis action |
   |-------------|-----------------|
   | service_running: fail | Service not running — check deploy status, read error logs: `zerops_logs severity="error" since="10m"` |
   | no_error_logs: fail | Runtime errors — read the `detail` field for the error message |
   | startup_detected: fail | App crashed on start — `zerops_logs severity="error" since="5m"` |
   | no_recent_errors: fail | Errors after startup — read the `detail` field |
   | http_health: fail | App started but /health endpoint broken — check `detail` for HTTP status |
   | http_status: fail | Managed service connectivity issue — check `detail` for which connection failed. Verify env var mapping matches discovered vars. |

2. **Fix**: Edit files at `{mountPath}/` — fix zerops.yml, app code, or both

3. **Redeploy**: `zerops_deploy targetService="{devHostname}" workingDir="{mountPath}" includeGit=true`

4. **Remount**: `zerops_mount action="mount" serviceHostname="{devHostname}"` — deploy replaces the container

5. **Re-verify**: `zerops_verify serviceHostname="{devHostname}"` — check status=healthy

Max 3 iterations. After that, report failure with diagnosis.

## Platform Rules

- **Dev setup MUST use `deployFiles: [.]`** — containers are volatile, only deployFiles content persists. Using `[dist]` or `[app]` in dev destroys source code after deploy.
- NEVER write lock files (go.sum, bun.lock, package-lock.json). Write manifests only (go.mod, package.json). Let build commands generate locks.
- NEVER write dependency dirs (node_modules/, vendor/).
- zerops_deploy blocks until build completes — returns DEPLOYED or BUILD_FAILED with build duration.
- `includeGit=true` requires `deployFiles: [.]` in zerops.yml — individual paths break git repository structure.
- zerops_subdomain MUST be called after deploy (even if enableSubdomainAccess was in import). The enable response contains `subdomainUrls` — the only source for subdomain URLs.
- subdomainUrls from enable response are already full URLs — do NOT prepend https://.
- Internal connections use http://, never https://.
- Env var cross-references use underscores: ${service_hostname}.
- 0.0.0.0 binding: app must listen on 0.0.0.0, not localhost or 127.0.0.1.

## Recovery

| Problem | Fix |
|---------|-----|
| Build FAILED: "command not found" | Fix buildCommands — check runtime knowledge |
| Build FAILED: "module not found" | Add dependency install to buildCommands |
| App crashes: "EADDRINUSE" | Port conflict — check run.ports.port matches app |
| App crashes: "connection refused" to DB | Wrong env var name — compare with discovered vars |
| HTTP 502 after deploy | Call zerops_subdomain action="enable" |
| Empty response / "Cannot GET" | App not handling route — check app code |
| /status: connectivity errors | Check env var mapping and managed service status |
| Env vars show ${...} in API | Expected — resolved at runtime, not in discover response |
````

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

### Verification iteration loop

When any verification check fails, enter the iteration loop instead of giving up:

**Iteration 1-3 (auto-fix):**

1. **Diagnose** — what check failed?
   | Failed check | Diagnosis action |
   |-------------|-----------------|
   | Build FAILED | `zerops_logs serviceHostname="{hostname}" severity="error" since="10m"`, fallback: `zcli service log {hostname} --showBuildLogs --limit 50` |
   | No startup logs | App crashed on start. Check `zerops_logs severity="error" since="5m"` |
   | Error logs after start | Runtime exception. Read error message. |
   | HTTP check failed | App started but endpoint broken. Capture response: `curl -sfm 10 "{url}" 2>&1` |
   | /status shows errors | Service connectivity issue. Check env var names match discovered vars. Verify managed service is RUNNING. |

2. **Fix** — based on diagnosis:
   - Build error -> fix zerops.yml (buildCommands, deployFiles, start)
   - Runtime error -> fix app code on mount path
   - Env var issue -> fix zerops.yml envVariables mapping, or check for typos in variable names
   - Connection error -> verify managed service is RUNNING (`zerops_discover`), check hostname/port match

3. **Redeploy** — `zerops_deploy` to the SAME service (always dev first)

4. **Re-verify** — run the full verification protocol again

**After 3 failed iterations**: Stop and report to user with:
- What was tried in each iteration
- Current error state
- Suggested next steps

**Common fix patterns:**

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| Build FAILED: "command not found" | Wrong buildCommands for runtime | Check recipe, fix build pipeline |
| Build FAILED: "module not found" | Missing dependency init | Add `go mod tidy`, `bun install`, etc. to buildCommands |
| App crashes: "EADDRINUSE" | Port conflict | Ensure app listens on correct port from zerops.yml |
| App crashes: "connection refused" | Wrong DB/cache host | Check envVariables mapping matches discovered vars |
| /status: "db: error" | Missing or wrong env var | Compare zerops.yml envVariables with discovered var names |
| HTTP 502 | Subdomain not activated | Call `zerops_subdomain action="enable"` |
| curl returns empty | App not listening on 0.0.0.0 | Add HOST=0.0.0.0 to envVariables |
</section>

<section name="verify">
### Verification Protocol

Every deployment must pass this protocol before being considered complete.

| # | Check | Tool / Method | Pass criteria |
|---|-------|---------------|---------------|
| 1 | Build/deploy completed | `zerops_deploy` return value | status=DEPLOYED (not BUILD_FAILED or timedOut) |
| 2 | Activate subdomain | `zerops_subdomain serviceHostname="{hostname}" action="enable"` | Success or already_enabled, response contains `subdomainUrls` |
| 3 | Full verification | `zerops_verify serviceHostname="{hostname}"` | status=healthy. If degraded: read `checks` array for diagnosis |

For managed services (DB, cache, storage): skip step 2 (no subdomain), just steps 1 + 3.

**Notes:**
- Check 1: zerops_deploy blocks until build completes and returns the final status directly. No polling needed.
- Check 2: **ALWAYS** call `zerops_subdomain action="enable"` after deploy — even if `enableSubdomainAccess` was set in import. The enable response contains `subdomainUrls` — this is the **only** source for subdomain URLs. The call is idempotent (returns `already_enabled` if already active).
- Check 3: `zerops_verify` performs 6 checks for runtime services (service_running, no_error_logs, startup_detected, no_recent_errors, http_health, http_status) and 1 check for managed services (service_running only). The response includes a `checks` array — each entry has `name`, `status` (pass/fail/skip), and optional `detail`. Status values: `healthy` (all pass/skip), `degraded` (running but some checks fail), `unhealthy` (service not running).

**Do NOT deploy to stage until dev passes ALL checks.** Stage is for final validation, not debugging.
</section>

<section name="report">
### After completion — next iteration

If the user asks for changes after initial bootstrap:
1. Reuse discovery data — do not re-discover unless services were added/removed.
2. Make the code/config change on the mount path.
3. Deploy to dev first, verify (with iteration loop if needed), then stage. Same dev-first pattern.
4. For config-only changes (env vars, scaling), use configure/scale workflows directly.
</section>
