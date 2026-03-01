# Bootstrap: Setting Up a Zerops Project

## Overview

Two phases: generate correct configuration (the hard part), then deploy and verify with iteration (the harder part).

**Default pattern: dev+stage service pairs.** Every runtime service gets `{name}dev` + `{name}stage` hostnames (e.g., "appdev", "apidev", "webdev"). Managed services are shared. User can opt into single-service mode if requested explicitly.

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
| All requested services exist as dev+stage pairs | CONFORMANT | If stack matches request, route to deploy. If different stack requested, ASK user how to proceed. NEVER auto-delete. |
| Services exist but not as dev+stage pairs | NON_CONFORMANT | ASK user how to proceed. Options: (a) add new services with different hostnames alongside existing, (b) user explicitly approves deletion of specific named services, (c) work with existing. NEVER auto-delete. |

**Dev+stage detection:** Look for `{name}dev` + `{name}stage` hostname pairs.

Route:
- FRESH: proceed normally through all steps
- CONFORMANT: if stack matches, skip bootstrap — route to deploy workflow (`zerops_workflow action="start" workflow="deploy"`). If user wants a different stack, ASK before making any changes. Do NOT delete existing services without explicit user approval.
- NON_CONFORMANT: STOP. Present existing services to user with types and status. Ask how to proceed. NEVER delete without explicit user approval naming each service.
</section>

<section name="plan">
### Step 1 — Identify stack components + environment mode

> **mode defaults to NON_HA for managed services** — databases, caches, object-storage, shared-storage.
> Set `HA` explicitly for production.

From the user's request, identify:
- **Runtime services**: type + framework (e.g., nodejs@22 with Next.js, go@1 with Fiber, bun@1.2 with Hono)
- **Managed services**: type + version (e.g., postgresql@16, valkey@7.2, elasticsearch@8.16)

**Verify all types against the `availableStacks` field in the workflow response.**

If the user hasn't specified, ask. Don't guess frameworks — the build config depends on it.

**Environment mode** (ask if not specified):
- **Standard** (default): Creates `{name}dev` + `{name}stage` + shared managed services. NON_HA mode.
- **Simple**: Creates single `{name}` + managed services. Only if user explicitly requests it.

Default = standard (dev+stage). If the user says "just one service" or "simple setup", use simple mode.

Multi-runtime naming: use role or runtime as prefix — `phpdev`/`phpstage` + `bundev`/`bunstage` (or `apidev`/`webdev` by role). Managed services are shared, no dev/stage suffixes: `db`, `cache`, `storage`.

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

Using the loaded knowledge from Steps 2+3, generate import.yml ONLY. Do NOT write zerops.yml or application code — that happens in the generate-code step AFTER env var discovery.

> **mode defaults to NON_HA for managed services** — databases, caches, object-storage, shared-storage.
> Set `HA` explicitly for production.

**Hostname pattern** (from Step 1): Standard mode (default) creates `{name}dev` + `{name}stage` pairs (e.g., "appdev"/"appstage", "apidev"/"apistage", "webdev"/"webstage") with shared managed services. Simple mode creates a single `{name}`. If the user didn't specify, ask before generating.

**Dev vs stage properties** (standard mode):

| Property | Dev (`{name}dev`) | Stage (`{name}stage`) |
|----------|-----------------|----------------------|
| `startWithoutCode` | `true` | omit |
| `maxContainers` | `1` | omit (default) |
| `enableSubdomainAccess` | `true` | `true` |

Dev starts immediately with an empty container (RUNNING). Stage stays in READY_TO_DEPLOY until first deploy from dev — no wasted resources running an empty container.

**Shared storage mount** (if shared-storage is in the stack): Add `mount: [{storage-hostname}]` to both dev and stage service definitions in import.yml. This pre-configures the connection but does NOT make storage available at runtime. You MUST also add `mount: [{storage-hostname}]` in the zerops.yml `run:` section and deploy for the storage to actually mount at `/mnt/{storage-hostname}`.

> **Two kinds of "mount" (disambiguation):** (1) `zerops_mount` — SSHFS tool, mounts service `/var/www` locally for development. (2) Shared storage mount — platform feature, attaches a shared-storage volume at `/mnt/{hostname}` via `mount:` in import.yml + zerops.yml. These are completely unrelated.

> **IMPORTANT**: Import `mount:` only applies to ACTIVE services. Stage services are READY_TO_DEPLOY during import, so the mount pre-configuration silently doesn't apply. After first deploy transitions stage to ACTIVE, connect storage via `zerops_manage action="connect-storage" serviceHostname="{stage}" storageHostname="{storage}"`.

**Validation checklist** (import.yml only):

| Check | What to verify |
|-------|---------------|
| Hostnames | Follow [a-z0-9] pattern, max 25 chars |
| Service types | Match available stacks |
| No duplicates | No duplicate hostnames |
| object-storage | Requires `objectStorageSize` field |
| Preprocessor | `#yamlPreprocessor=on` if using `<@...>` functions |
| Mode present | Managed services default to NON_HA if omitted |

Present import.yml to the user for review before proceeding.
</section>

<section name="generate-code">
### Step 7 — Generate zerops.yml and application code

**Prerequisites**: Dev services mounted (step 5), env vars discovered (step 6). Write files to the mounted dev service filesystem at `/var/www/{devHostname}/`.

**CRITICAL: Dev vs Prod deploy differentiation**

| Property | Dev setup | Prod setup |
|----------|-----------|------------|
| Purpose | Iterate, debug, test | Final validation, production-like |
| deployFiles | `[.]` (entire source directory) | Runtime-specific build output |
| start command | Source-mode start | Binary/compiled start |

**Dev setup rules:**
- `deployFiles: [.]` — ALWAYS, no exceptions. Anything else destroys source files after deploy.
- `start: zsc noop --silent` — container stays alive but idle. No server auto-starts. The agent starts the server manually via SSH, giving full control over the process (see Dev iteration below). **Does NOT apply to PHP runtimes** (php-nginx, php-apache) — they have a built-in web server, omit `start:` entirely.
- `buildCommands:` — dependency installation only (no compilation step). Source runs directly from `/var/www/`.

**Prod/stage setup rules:**
- `deployFiles:` — only the compiled output or production artifacts.
- `start:` — runs the compiled artifact directly.
- `buildCommands:` — full build pipeline: install deps, compile, produce artifacts.

**PHP runtimes (php-nginx, php-apache) are different:** The web server is built into the runtime and serves files automatically. There is no `start:` command — both dev and prod just need correct `deployFiles`.

The zerops.yml MUST have TWO separate `setup:` entries — one for the dev hostname, one for the stage hostname — with their own build/deploy/start pipelines.

> **CRITICAL — dev `deployFiles` MUST be `[.]`:** Dev containers are volatile. After deploy, ONLY `deployFiles` content survives. If dev setup uses `[dist]`, `[app]`, or any build output path, all source files + zerops.yml are DESTROYED. Further iteration becomes impossible. Dev setup MUST ALWAYS use `deployFiles: [.]` regardless of runtime. No exceptions.

#### Application code requirements

Every generated application **MUST** expose these endpoints:

| Endpoint | Response | Purpose |
|----------|----------|---------|
| `GET /` | `"Service: {hostname}"` | Landing page / smoke test |
| `GET /health` | `{"status":"ok"}` (HTTP 200) | Liveness probe |
| `GET /status` | Connectivity JSON (HTTP 200) | **Proves managed service connections** |

##### /status endpoint specification

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

**Do NOT generate hello-world apps that skip service connectivity.** The whole point of bootstrap is proving the infrastructure works end-to-end.

#### Env var injection — how variables reach your app

Zerops injects all `envVariables` and `envSecrets` as **standard OS environment variables** at container start. Cross-service references like `${db_connectionString}` are resolved before injection — your app receives plain values, not template syntax.

**Read env vars using the runtime's native env var API. No `.env` files. No dotenv libraries. No manual env loading.** Every language/framework has a standard way to read OS environment variables — use that.

#### Env var mapping in zerops.yml

In zerops.yml `envVariables`, ONLY use variables discovered in the discover-envs step:
```yaml
envVariables:
  DATABASE_URL: ${db_connectionString}
  REDIS_HOST: ${cache_host}
  REDIS_PORT: ${cache_port}
  # Do NOT add variables that don't exist
```

**MANDATORY PRE-DEPLOY CHECK** (do NOT proceed until all pass):
- [ ] zerops.yml has `setup:` entry for EVERY planned runtime hostname
- [ ] Dev setup uses `deployFiles: [.]` — NO EXCEPTIONS
- [ ] `run.start` is the RUN command (not a build tool like `go build`)
- [ ] `run.ports` port matches what the app listens on
- [ ] `envVariables` ONLY uses variables discovered in discover-envs step
- [ ] App binds to `0.0.0.0:{port}`, NOT localhost

#### Files are already on dev

Since you're writing to an SSHFS mount, every file you create or modify is immediately present on the running dev container — no deploy is needed for that. You can verify or test changes right away. The deploy step exists to test the build pipeline and to ensure persistence (dev containers are volatile — only `deployFiles` content survives a deploy).

> **After formal `zerops_deploy` to dev:** Deploy restarts the container with `zsc noop --silent` — no server runs. You must start it manually via SSH again before `zerops_verify` can succeed. See the deploy step for the full SSH start cycle.

> Consider committing the generated code before proceeding to deploy. This preserves your work if the deploy cycle requires iteration.
</section>

---

## Phase 2: Deployment and Verification

<section name="discover-envs">
### Env var discovery protocol (mandatory before deploy)

After importing services and waiting for them to reach RUNNING, discover the ACTUAL env vars available to each service. This data is critical for writing correct zerops.yml envVariables and for subagent prompts.

**Single call — returns env vars for ALL services:**
```
zerops_discover includeEnvs=true
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

**How these reach your app**: All variables mapped in zerops.yml `envVariables` are injected as standard OS environment variables at container start. Your app reads them with the runtime's native env var API. No `.env` files or dotenv libraries needed.
</section>

<section name="deploy">
### Deploy overview

**Core principle: Dev is for iterating and fixing. Stage is for final validation. Fix errors on dev before deploying to stage.**

**Mandatory dev lifecycle** — `start: zsc noop --silent` means no server auto-starts. The agent MUST:
1. Write zerops.yml + app code to SSHFS mount
2. Start server manually via SSH, test endpoints (/health, /status) — fix until working
3. `zerops_deploy` to dev (build pipeline, persist files) — **this restarts the container, server stops**
4. Start server again via SSH (same kill-then-start as step 2)
5. `zerops_verify` dev — now endpoints respond
6. `zerops_deploy` to stage (stage has real `start:` command — server auto-starts there)
7. `zerops_verify` stage

Steps 3-5 repeat on every iteration. Stage (steps 6-7) only after dev is healthy.

> **Files are already on the dev container** via SSHFS mount — deploy does not "send" files there. Deploy runs the build pipeline (buildCommands, deployFiles) and restarts the process. It also ensures persistence — dev containers are volatile, only `deployFiles` content survives.

> **Bootstrap deploys ALWAYS use SSH mode** (sourceService + targetService). NEVER use local mode (targetService only) — git operations fail on SSHFS mounts.

`zerops_deploy` blocks until the build pipeline completes. It returns the final status (`DEPLOYED` or `BUILD_FAILED`) along with build duration. No manual polling needed.
`zerops_import` blocks until all import processes complete. It returns final statuses (`FINISHED` or `FAILED`) for each process.

### Standard mode (dev+stage) — deploy flow

**Prerequisites**: import done, dev mounted, env vars discovered, code written to mount path (steps 4-7).

1. **Deploy to appdev** (SSH self-deploy): `zerops_deploy sourceService="appdev" targetService="appdev" includeGit=true` — SSHes into dev container, runs `git init` + `zcli push -g` on native FS at `/var/www`. Files got there via SSHFS mount writes. `includeGit=true` preserves `.git` on the target so subsequent deploys work. SSHFS mount auto-reconnects after deploy — no remount needed. Deploy tests the build pipeline and ensures deployFiles artifacts persist.
2. **Start appdev** (deploy restarted container with `zsc noop`): start server via SSH (same kill-then-start pattern from Dev iteration below), verify startup log
3. **Verify appdev**: `zerops_subdomain serviceHostname="appdev" action="enable"` then `zerops_verify serviceHostname="appdev"` — must return status=healthy
4. **Iterate if needed** — if `zerops_verify` returns degraded/unhealthy, enter the iteration loop: diagnose from `checks` array -> fix on mount path -> redeploy -> re-verify (max 3 iterations)
5. **Deploy to appstage from dev**: `zerops_deploy sourceService="appdev" targetService="appstage"` — SSH mode: pushes from dev container to stage. Zerops runs the `setup: appstage` build pipeline. Transitions stage from READY_TO_DEPLOY -> BUILDING -> RUNNING. Stage is never a deploy source — no `.git` needed on target.
5b. **Connect shared storage to stage** (if shared-storage is in the stack): `zerops_manage action="connect-storage" serviceHostname="appstage" storageHostname="storage"` — stage was READY_TO_DEPLOY during import, so the import `mount:` did not apply.
6. **Verify appstage**: `zerops_subdomain serviceHostname="appstage" action="enable"` then `zerops_verify serviceHostname="appstage"` — must return status=healthy
7. **Present both URLs** to user:
    ```
    Dev:   {subdomainUrl from enable}
    Stage: {subdomainUrl from enable}
    ```

### Dev iteration: manual start cycle

**This applies to EVERY dev interaction — not just the first time.** Since dev uses `start: zsc noop --silent`, the container runs but no server process listens. After every `zerops_deploy` to dev, the container restarts with `zsc noop` — the agent must start the server again via SSH. The agent controls the server process entirely via SSH. This enables:
- Seeing startup errors immediately in the log
- Killing and restarting with different flags or commands
- Switching between watch mode and build-and-run
- Testing code changes instantly without redeployment

**Source-mode start commands by runtime:** `go run .` (Go), `bun run index.ts` (Bun), `node index.js` (Node.js), `python3 app.py` (Python), `cargo run` (Rust), `dotnet run` (C#), `mix run --no-halt` (Elixir), `deno run --allow-net --allow-env --allow-read main.ts` (Deno), `javac Server.java && java Server` (Java), `bundle exec ruby app.rb` (Ruby), `gleam run` (Gleam).

**The cycle:**
1. **Edit code** on the mount path — changes appear instantly in the container at `/var/www/`.
2. **Kill any previous process**:
   `ssh {devHostname} "pkill -f '{binary}' 2>/dev/null; fuser -k {port}/tcp 2>/dev/null; true"`
   Process pattern for pkill: use the binary name — `go` for Go, `bun` for Bun, `node` for Node.js, `python` for Python. Also kill by port with `fuser` to prevent EADDRINUSE.
3. **Start server** — use `run_in_background=true` on this Bash call:
   `ssh {devHostname} "cd /var/www && {start_command}"`
   The SSH session stays open and streams server stdout/stderr into the task output. Do NOT use `nohup` or redirect to a file — let output flow to the task.
4. **Check startup** — wait 3-5s, then read the background task output (non-blocking):
   `TaskOutput task_id=<from step 3> block=false` — look for startup message (`listening on`, `server started`, `ready`). If you see `error`, `panic`, `fatal`, or `EADDRINUSE` → fix code and go back to step 2.
5. **Test** endpoints from inside the container:
   `ssh {devHostname} "curl -s localhost:{port}/health"` | jq .
6. **If broken**: read the background task output for error details, fix code on the mount, `TaskStop` the server task, go back to step 2.
7. **When working**: proceed to formal `zerops_deploy`. The background SSH task is stopped automatically when deploy restarts the container.

**Why formal deploy is still needed:** Dev containers are volatile — only `deployFiles` content persists across container restarts. The manual-start cycle is for rapid iteration, but the final state must go through `zerops_deploy` to ensure the build pipeline works and files persist.

**PHP runtimes (php-nginx, php-apache) skip manual start.** The web server runs automatically and serves files from the deploy directory. Editing files on the mount is enough — just test the endpoints directly via SSH curl.

**When to use manual cycle vs. full deploy:**
- Code logic changes, adding endpoints, fixing bugs → manual start cycle
- Changing zerops.yml (build config, env vars, ports) → full `zerops_deploy`
- Before deploying to stage → always full `zerops_deploy`

**Piping rule:** `ssh {dev} "curl -s localhost:{port}/api"` | jq . — pipe OUTSIDE SSH. `jq` is not available inside containers.

### Simple mode — deploy flow

1. **Import services:**
   ```
   zerops_import content="<import.yml>"           # blocks until all processes finish
   zerops_discover                                # verify services reached RUNNING
   ```

   > **Subdomain activation:** `enableSubdomainAccess: true` in import.yml pre-configures routing, but **does NOT activate it**. You MUST call `zerops_subdomain action="enable"` after deploy to activate the L7 balancer route. The enable response contains `subdomainUrls` — this is the **only** source for subdomain URLs. Without the explicit enable call, the subdomain returns 502. The call is idempotent — safe to call even if already active.

2. **Discover env vars:**
   ```
   zerops_discover includeEnvs=true
   ```
   Single call returns env vars for all services. Record available env vars. Use ONLY discovered variable names in zerops.yml.

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
4. **Discover ALL env vars**: `zerops_discover includeEnvs=true` — single call returns all services with env vars. Record exact var names.
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

### How variables reach your app

Zerops injects all `envVariables` as standard OS environment variables at container start. Cross-service references (e.g., `${db_connectionString}`) are resolved before injection — your app receives plain values. Use the runtime's native env var API to read them.

**NO .env files. NO dotenv libraries. NO manual env loading.**

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
        - port: {port}    # From runtime knowledge briefing. Common: Go=8080, Node.js=3000, Python=8000, Bun=3000
          httpSupport: true
      envVariables:
        # Map discovered variables to app-expected names
      start: zsc noop --silent   # Dev: idle container, agent starts server manually via SSH. PHP runtimes: omit start entirely.

  - setup: {stageHostname}
    build:
      base: {runtimeVersion}
      buildCommands: [<from runtime knowledge — may include compilation>]
      deployFiles: [{prodDeployFiles}]
      cache: [<runtime-specific cache dirs>]
    run:
      base: {prodRunBase}
      ports:
        - port: {port}    # Same port as dev
          httpSupport: true
      envVariables:
        # Same mappings as dev
      start: {prodStartCommand}
```

## Application Requirements

**Environment variables**: Zerops injects all `envVariables` as OS env vars at container start. Read them using the runtime's native env var API. Do NOT create `.env` files, use dotenv libraries, or add any env file parsing code.

Your app MUST expose these endpoints on the port defined in zerops.yml `run.ports`:

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
| 2 | Write app code | HTTP server on the port defined in zerops.yml `run.ports` with `/`, `/health`, `/status` | Code references discovered env vars |
| 3 | Write .gitignore | Build artifacts and IDE files only. Do NOT include `.env` — no .env files exist on Zerops | File exists, no `.env` entry |
| 3b | Quick-test (mandatory) | Kill previous process, start server via SSH with `run_in_background=true`, check `TaskOutput` for startup, test /health and /status. Fix issues. | Endpoints return expected responses |
| 4 | Deploy dev | `zerops_deploy sourceService="{devHostname}" targetService="{devHostname}" includeGit=true` | status=DEPLOYED (blocks until complete) |
| 5 | Verify build | Check zerops_deploy return value | Not BUILD_FAILED or timedOut |
| 5b | Start server (post-deploy) | Deploy restarted container — `zsc noop --silent` is running, not your app. Start server via SSH with `run_in_background=true` (kill-then-start pattern from Quick-test). | `TaskOutput` shows startup message |
| 6 | Activate subdomain | `zerops_subdomain serviceHostname="{devHostname}" action="enable"` | Returns `subdomainUrls` |
| 7 | Verify dev | `zerops_verify serviceHostname="{devHostname}"` | status=healthy |
| 8 | Deploy stage | `zerops_deploy sourceService="{devHostname}" targetService="{stageHostname}"` | status=DEPLOYED (blocks until complete) |
| 9 | Verify stage | `zerops_subdomain action="enable"` + `zerops_verify serviceHostname="{stageHostname}"` | status=healthy |
| 10 | Report | Status (pass/fail) + dev URL + stage URL | — |

## Quick-test before deploy (mandatory)

Dev uses `start: zsc noop --silent` — no server runs automatically. You MUST start it manually and verify before formal deploy.

**Source-mode start commands:** `go run .` (Go), `bun run index.ts` (Bun), `node index.js` (Node.js), `python3 app.py` (Python), `cargo run` (Rust), `dotnet run` (C#), `deno run --allow-net --allow-env --allow-read main.ts` (Deno), `javac Server.java && java Server` (Java), `bundle exec ruby app.rb` (Ruby), `gleam run` (Gleam).

1. **Kill any previous process**:
   `ssh {devHostname} "pkill -f '{binary}' 2>/dev/null; fuser -k {port}/tcp 2>/dev/null; true"`
2. **Install dependencies** (if needed):
   `ssh {devHostname} "cd /var/www && {install_command}"`
3. **Start server** — use `run_in_background=true` on this Bash call:
   `ssh {devHostname} "cd /var/www && {dev_start_command}"`
   SSH stays open, server output streams to task. Do NOT use nohup or file redirects.
4. **Check startup** — wait 3-5s, then `TaskOutput task_id=<from step 3> block=false`. Look for startup message. If errors visible → fix and retry.
5. **Test endpoints**:
   `ssh {devHostname} "curl -sf localhost:{port}/health"` | jq .
   `ssh {devHostname} "curl -sf localhost:{port}/status"` | jq .
6. **If broken**: read the background task output for errors. Fix code on mount, `TaskStop` the server task, go back to step 1.
7. **When working**: proceed to formal deploy (task 4).

**PHP runtimes:** Skip steps 1-7 — the web server runs automatically. Just test endpoints directly after writing files.

**Piping rule:** Pipe `| jq .` OUTSIDE the SSH command. `jq` is not available inside containers.

## Iteration Loop (when verification fails)

If `zerops_verify` returns "degraded" or "unhealthy", iterate — do NOT skip ahead to stage:

1. **Diagnose**: Read the `checks` array from the `zerops_verify` response:
   | Check result | Diagnosis action |
   |-------------|-----------------|
   | service_running: fail | Service not running — check deploy status, read error logs: `zerops_logs severity="error" since="10m"` |
   | no_error_logs: info | Advisory — error-severity logs found. Read detail. If SSH/infra noise, ignore. If app errors, investigate with `zerops_logs` |
   | startup_detected: fail | App crashed on start — `zerops_logs severity="error" since="5m"` |
   | no_recent_errors: info | Advisory — same as above. Recent error-severity logs found. Read detail to determine if actionable |
   | http_health: fail | App started but /health endpoint broken — check `detail` for HTTP status |
   | http_status: fail | Managed service connectivity issue — check `detail` for which connection failed. Verify env var mapping matches discovered vars. |

2. **Fix**: Edit files at `{mountPath}/` — fix zerops.yml, app code, or both

3. **Redeploy**: `zerops_deploy sourceService="{devHostname}" targetService="{devHostname}" includeGit=true` — `.git` persists on target when `includeGit=true` was used. SSHFS mount auto-reconnects after deploy.

4. **Start server**: kill previous: `ssh {devHostname} "pkill -f '{binary}' 2>/dev/null; fuser -k {port}/tcp 2>/dev/null; true"`, then start with `run_in_background=true`: `ssh {devHostname} "cd /var/www && {start_command}"` — check startup via `TaskOutput task_id=... block=false` after 3-5s.

5. **Re-verify**: `zerops_verify serviceHostname="{devHostname}"` — check status=healthy

Max 3 iterations. After that, report failure with diagnosis.

## Platform Rules

- **Bootstrap deploys ALWAYS use SSH mode** — `zerops_deploy sourceService="{devHostname}" targetService="{devHostname}" includeGit=true`. Git is initialized automatically if missing. `includeGit=true` preserves `.git` on target so subsequent deploys work. NEVER use local mode (targetService only) — git operations fail on SSHFS mounts.
- **Dev setup MUST use `deployFiles: [.]`** — containers are volatile, only deployFiles content persists. Using `[dist]` or `[app]` in dev destroys source code after deploy.
- NEVER write lock files (go.sum, bun.lock, package-lock.json). Write manifests only (go.mod, package.json). Let build commands generate locks.
- NEVER write dependency dirs (node_modules/, vendor/).
- zerops_deploy blocks until build completes — returns DEPLOYED or BUILD_FAILED with build duration.
- `includeGit=true` requires `deployFiles: [.]` in zerops.yml — individual paths break git repository structure.
- zerops_subdomain MUST be called after deploy (even if enableSubdomainAccess was in import). The enable response contains `subdomainUrls` — the only source for subdomain URLs.
- subdomainUrls from enable response are already full URLs — do NOT prepend https://.
- Internal connections use http://, never https://.
- Env var cross-references use underscores: ${service_hostname}.
- **NO .env files** — Zerops injects all envVariables/envSecrets as OS env vars at container start. Do NOT create `.env` files, use dotenv libraries, or add env-file-loading code.
- 0.0.0.0 binding: app must listen on 0.0.0.0, not localhost or 127.0.0.1.

## Recovery

| Problem | Fix |
|---------|-----|
| Build FAILED: "command not found" | Fix buildCommands — check runtime knowledge |
| Build FAILED: "module not found" | Add dependency install to buildCommands |
| App crashes: "EADDRINUSE" | Port conflict — check run.ports.port matches app. If from SSH-started process: `ssh {devHostname} "pkill -f '{binary}' 2>/dev/null; true"` |
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
- Check 3: `zerops_verify` performs 6 checks for runtime services (service_running, no_error_logs, startup_detected, no_recent_errors, http_health, http_status) and 1 check for managed services (service_running only). The response includes a `checks` array — each entry has `name`, `status` (pass/fail/skip/info), and optional `detail`. Status values: `healthy` (all pass/skip/info), `degraded` (running but some checks fail), `unhealthy` (service not running). Error log checks (no_error_logs, no_recent_errors) return `info` instead of `fail` — they are advisory because SSH deploy logs are often classified as errors.

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
