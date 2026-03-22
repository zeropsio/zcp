# Bootstrap: Setting Up a Zerops Project

## Overview

Two phases: generate correct configuration (the hard part), then deploy and verify with iteration (the harder part).

**Default pattern: dev+stage service pairs.** Every runtime service gets `{name}dev` + `{name}stage` hostnames (e.g., "appdev", "apidev", "webdev"). Managed services are shared. User can opt into single-service mode if requested explicitly.

---

<!-- STACKS:BEGIN -->
<!-- STACKS:END -->

---

## Phase 1: Configuration

<section name="discover">
### Detect project state, plan services, confirm with user

Call `zerops_discover` to see what exists. Then classify:

| Discover result | State | Action |
|----------------|-------|--------|
| No runtime services | FRESH | Full bootstrap |
| All requested services exist as dev+stage pairs | CONFORMANT | If stack matches request, route to deploy. If different stack requested, ASK user how to proceed. NEVER auto-delete. |
| Services exist but not as dev+stage pairs | NON_CONFORMANT | ASK user how to proceed. Options: (a) add new services with different hostnames alongside existing, (b) user explicitly approves deletion of specific named services, (c) work with existing. NEVER auto-delete. |

**Dev+stage detection:** Look for `{name}dev` + `{name}stage` hostname pairs.

Route:
- FRESH: proceed normally through all steps
- CONFORMANT: if stack matches, skip bootstrap — route to deploy workflow (`zerops_workflow action="start" workflow="deploy"`). If user wants a different stack, ASK before making any changes.
- NON_CONFORMANT: STOP. Present existing services to user with types and status. Ask how to proceed. NEVER delete without explicit user approval naming each service.

#### Identify stack components

> **mode defaults to NON_HA for managed services** — databases, caches, object-storage, shared-storage.
> Set `HA` explicitly for production.

From the user's request, identify:
- **Runtime services**: type + framework (e.g., nodejs@22 with Next.js, go@1 with Fiber, bun@1.2 with Hono)
- **Managed services**: type + version (e.g., postgresql@16, valkey@7.2, elasticsearch@8.16)

**Verify all types against the `availableStacks` field in the workflow response.**

If the user hasn't specified, ask. Don't guess frameworks — the build config depends on it.

#### Choose bootstrap mode

- **Standard** (default): Creates `{name}dev` + `{name}stage` + shared managed services. Dev for iteration, stage for validation.
- **Dev**: Creates `{name}dev` + managed services only. No stage pair. When user says "just get it running" or "prototype."
- **Simple**: Creates single `{name}` + managed services. Real start command, auto-starts after deploy. Only if user explicitly requests simplest setup.

Default = standard (dev+stage). Ask user to confirm the mode before proceeding.

Multi-runtime naming: use role or runtime as prefix — `phpdev`/`phpstage` + `bundev`/`bunstage` (or `apidev`/`webdev` by role). Managed services are shared, no dev/stage suffixes: `db`, `cache`, `storage`.

#### Load framework-specific knowledge (optional)

For known frameworks, load a recipe for pre-built configuration:
```
zerops_knowledge recipe="{recipe-name}"
```
Examples: `bun`, `bun-hono`, `laravel`, `ghost`, `django`, `phoenix`, `nextjs`

This is optional — platform knowledge (YAML schemas, runtime guides, service cards) is delivered automatically with each step guide. Recipes add framework-specific patterns on top.

#### Confirm and submit plan

**PRESENT the plan to user for confirmation before submitting:**
"I'll set up: [list services with types]. Mode: [standard/dev/simple]. OK?"

After user confirms, submit:
```
zerops_workflow action="complete" step="discover" plan=[{runtime: {devHostname, type, bootstrapMode}, dependencies: [{hostname, type, resolution}]}]
```
</section>

<section name="provision">
### Generate import.yml, provision services, discover env vars

Generate import.yml ONLY. Do NOT write zerops.yml or application code — that happens in the generate step AFTER env var discovery. The import.yml schema rules are included below.

> **mode defaults to NON_HA for managed services** — databases, caches, object-storage, shared-storage.
> Set `HA` explicitly for production.

**Hostname pattern** (from Step 1): Standard mode (default) creates `{name}dev` + `{name}stage` pairs (e.g., "appdev"/"appstage", "apidev"/"apistage", "webdev"/"webstage") with shared managed services. Simple mode creates a single `{name}`. If the user didn't specify, ask before generating.

**Dev vs stage properties** (standard mode):

| Property | Dev (`{name}dev`) | Stage (`{name}stage`) |
|----------|-----------------|----------------------|
| `startWithoutCode` | `true` | omit |
| `maxContainers` | `1` | omit (default) |
| `enableSubdomainAccess` | `true` | `true` |
| `verticalAutoscaling.minRam` | `1.0` for compiled runtimes (Go, Rust, Java, .NET, Elixir, Gleam) | omit (default) |

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

### Env var discovery protocol (mandatory before generate)

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
  # Map ONLY variables listed in the discovery response
```

**ONLY use variables that were actually discovered.** Guessing variable names causes runtime failures. If a variable doesn't appear in discovery, it doesn't exist.

**How these reach your app**: All variables mapped in zerops.yml `envVariables` are injected as standard OS environment variables at container start. Your app reads them with the runtime's native env var API. No `.env` files or dotenv libraries needed.
</section>

<section name="generate">
### Generate zerops.yml and application code

**Prerequisites**: Services mounted, env vars discovered.

> **WHERE TO WRITE FILES**: Write ALL files (zerops.yml, app code, configs) to the SSHFS mount path `/var/www/{hostname}/` — this is the path returned by `zerops_mount` in the provision step. `/var/www/` without the hostname suffix is the zcpx orchestrator's own filesystem — writing there has NO effect on the target service. Every file must go under `/var/www/{hostname}/`.

**SSHFS mount is for source code only** — small file reads/writes (editing .go, .ts, .yml files). Commands that generate many files (npm install, pip install, go mod download, composer install, bundle install, cargo build) MUST run via SSH on the container: `ssh {hostname} "cd /var/www && {install_command}"`. Running them locally through the SSHFS network mount is orders of magnitude slower.

> **CRITICAL — self-deploying services MUST use `deployFiles: [.]`:** Containers are volatile. After deploy, ONLY `deployFiles` content survives. If a self-deploying service uses `[dist]`, `[app]`, or any build output path, all source files + zerops.yml are DESTROYED. Further iteration becomes impossible. Any service that deploys to itself (dev services, simple mode services) MUST ALWAYS use `deployFiles: [.]`. No exceptions. Cross-deploy targets (stage) can use specific paths for compiled output because their source lives on the dev service.

**PHP runtimes (php-nginx, php-apache) are different:** The web server is built into the runtime and serves files automatically. There is no `start:` command — both dev and prod just need correct `deployFiles`.

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
  "status": "ok",
  "connections": {
    "db": {"status": "ok", "latency_ms": 5},
    "cache": {"status": "ok", "latency_ms": 1}
  }
}
```

The top-level `"status": "ok"` is ALWAYS required — with or without connections.

**Required verification per service type:**
- **PostgreSQL/MariaDB/MySQL**: Execute `SELECT 1` query
- **Valkey/KeyDB**: Execute `PING` command
- **MongoDB**: Run `db.runCommand({ping: 1})`
- **Object Storage**: Check endpoint reachability
- **Shared Storage**: Check mount path exists and is writable
- **No managed services**: Return `{"service": "{hostname}", "status": "ok"}`

#### How envVariables reach your app

zerops.yml `envVariables` are activated by deploy. After `zerops_deploy`:
- All mapped vars are injected as standard OS environment variables
- Cross-service references (`${db_connectionString}`) resolved to real values
- Vars persist across server restarts within the same container

Write zerops.yml + app code first, then deploy, then start the server and test. **NEVER hardcode credential values or pass them inline** — always use `${hostname_varName}` references in zerops.yml and read env vars via the runtime's native API. Do NOT create `.env` files — empty values shadow OS env vars. Dotenv libraries in existing projects are harmless (they fall back to OS vars when no .env exists).

#### Env var mapping in zerops.yml

In zerops.yml `envVariables`, ONLY use variables discovered in the provision step:
```yaml
envVariables:
  DATABASE_URL: ${db_connectionString}
  REDIS_HOST: ${cache_host}
  REDIS_PORT: ${cache_port}
  # Map ONLY variables listed in the discovery response
```

#### Files are already on the container

Since you're writing to an SSHFS mount, every file you create or modify is immediately present on the running container. The deploy step runs the build pipeline and activates zerops.yml config (envVariables, ports). Containers are volatile — only `deployFiles` content survives a deploy.

> Consider committing the generated code before proceeding to deploy. This preserves your work if the deploy cycle requires iteration.
</section>

<section name="generate-standard">
### Standard mode (dev+stage) — zerops.yml rules

**Write dev entry ONLY now. Stage entry comes after dev is verified (deploy step).**
All files go to `/var/www/{devHostname}/` (the SSHFS mount path from provision).

**Dev setup rules:**
- `deployFiles: [.]` — ALWAYS, no exceptions.
- `start: zsc noop --silent` — container stays alive but idle. No server auto-starts. The agent starts the server manually via SSH, giving full control over the iteration cycle. **Does NOT apply to PHP runtimes** (php-nginx, php-apache) — they have a built-in web server, omit `start:` entirely.
- `buildCommands:` — dependency installation only (no compilation step). Source runs directly from `/var/www/`.
- **NO healthCheck** — agent controls lifecycle manually. A healthCheck would cause unwanted restarts when the agent stops the server for iteration.

**Dev vs Prod reference** (for later — stage entry uses prod rules after dev is proven):

| Property | Dev setup | Prod/Stage setup |
|----------|-----------|------------------|
| Purpose | Iterate, debug, test | Final validation, production-like |
| deployFiles | `[.]` (entire source) | Runtime-specific build output |
| start command | `zsc noop --silent` | Real binary/compiled start |
| healthCheck | **None** | `httpGet` on app port |
| readinessCheck | **None** | Optional |

**MANDATORY PRE-DEPLOY CHECK** (do NOT proceed until all pass):
- [ ] zerops.yml has `setup:` entry for dev hostname ONLY (no stage entry yet)
- [ ] Dev setup uses `deployFiles: [.]` — NO EXCEPTIONS
- [ ] Dev `run.start` is `zsc noop --silent` (not real start cmd) — implicit-webserver runtimes: omit start entirely
- [ ] `run.ports` port matches what the app listens on — implicit-webserver runtimes: omit ports (port 80 fixed)
- [ ] `envVariables` ONLY uses variables discovered in provision step
- [ ] App binds to `0.0.0.0:{port}`, NOT localhost
</section>

<section name="generate-dev">
### Dev-only mode — zerops.yml rules

**Write a single dev entry. No stage service exists in this mode.**
All files go to `/var/www/{devHostname}/` (the SSHFS mount path from provision).

**Dev setup rules** (identical to standard dev):
- `deployFiles: [.]` — ALWAYS, no exceptions.
- `start: zsc noop --silent` — container stays alive but idle. The agent starts the server manually via SSH. **Does NOT apply to PHP runtimes** (php-nginx, php-apache) — omit `start:` entirely.
- `buildCommands:` — dependency installation only (no compilation).
- **NO healthCheck** — agent controls lifecycle manually.

**MANDATORY PRE-DEPLOY CHECK** (do NOT proceed until all pass):
- [ ] zerops.yml has `setup:` entry for dev hostname
- [ ] Dev setup uses `deployFiles: [.]` — NO EXCEPTIONS
- [ ] `run.start` is `zsc noop --silent` — implicit-webserver runtimes: omit start entirely
- [ ] `run.ports` port matches what the app listens on — implicit-webserver runtimes: omit ports (port 80 fixed)
- [ ] `envVariables` ONLY uses variables discovered in provision step
- [ ] App binds to `0.0.0.0:{port}`, NOT localhost
</section>

<section name="generate-simple">
### Simple mode — zerops.yml rules

**Write a single entry with a REAL start command.** Unlike dev/standard, simple mode services auto-start after deploy — no manual SSH start needed.
All files go to `/var/www/{hostname}/` (the SSHFS mount path from provision).

**Simple setup rules:**
- `deployFiles: [.]` — ALWAYS (self-deploy, source must survive).
- `start:` — **real start command** (`node index.js`, `bun run src/index.ts`, `./app`, etc.). NOT `zsc noop`.
- `buildCommands:` — dependency installation (compilation for Go/Rust/Java if needed).
- `healthCheck:` — **YES, required.** Zerops monitors the container and restarts on failure.

```yaml
zerops:
  - setup: {hostname}
    build:
      base: {runtimeVersion}
      buildCommands: [<from runtime knowledge>]
      deployFiles: [.]   # CRITICAL: self-deploy — MUST be [.]
      cache: [<runtime-specific cache dirs>]
    run:
      base: {runtimeBase}   # Omit for compiled langs or use same as build
      ports:
        - port: {port}
          httpSupport: true
      envVariables:
        # Map discovered variables to app-expected names
      start: {startCommand}   # REAL start: node index.js, bun run src/index.ts, ./app, etc.
      healthCheck:
        httpGet:
          port: {port}
          path: /health
```

**Key differences from dev/standard mode:**
- `start:` is the real run command (NOT `zsc noop --silent`)
- `healthCheck` included — app auto-starts and auto-restarts after deploy
- Single `setup:` entry (no dev/stage split)

**MANDATORY PRE-DEPLOY CHECK** (do NOT proceed until all pass):
- [ ] zerops.yml has `setup:` entry for the service hostname
- [ ] Uses `deployFiles: [.]` — NO EXCEPTIONS
- [ ] `run.start` is the REAL start command (NOT `zsc noop`)
- [ ] `run.ports` port matches what the app listens on — implicit-webserver runtimes: omit ports (port 80 fixed)
- [ ] `healthCheck` configured with correct port and path
- [ ] `envVariables` ONLY uses variables discovered in provision step
- [ ] App binds to `0.0.0.0:{port}`, NOT localhost
</section>

---

## Phase 2: Deployment and Verification

<section name="deploy">
### Deploy overview

**Core principle: Deploy first — env vars activate at deploy time. Dev is for iterating and fixing. Stage is for final validation.**

**Mandatory dev lifecycle** — deploy-first. Dev uses an idle start command so no server auto-starts. The agent MUST:
1. Write zerops.yml (dev entry only) + app code to SSHFS mount
2. `zerops_deploy` to dev — activates envVariables, runs build pipeline, persists files. Container restarts with `zsc noop`.
3. Start server via SSH — env vars are now available as OS env vars
4. `zerops_verify` dev — endpoints respond with real env var values
5. Generate stage entry in zerops.yml — dev is proven, now write the production config based on what worked
6. `zerops_deploy` to stage (stage has real `start:` command — server auto-starts there)
7. `zerops_verify` stage

Steps 2-4 repeat on every iteration. Stage (steps 5-7) only after dev is healthy.

> **Files are already on the dev container** via SSHFS mount — deploy does not "send" files there. Deploy runs the build pipeline (buildCommands, deployFiles), activates envVariables, and restarts the process.

> Bootstrap deploys: `zerops_deploy targetService="{devHostname}"` for self-deploy.
> Cross-deploy to stage: `zerops_deploy sourceService="{devHostname}" targetService="{stageHostname}"`.

`zerops_deploy` blocks until the build pipeline completes. It returns the final status (`DEPLOYED` or `BUILD_FAILED`) along with build duration. No manual polling needed.
`zerops_import` blocks until all import processes complete. It returns final statuses (`FINISHED` or `FAILED`) for each process.

### Standard mode (dev+stage) — deploy flow

**Prerequisites**: import done, dev mounted, env vars discovered, code written to mount path (steps 4-7).

> **Path distinction:** SSHFS mount path `/var/www/{devHostname}/` is LOCAL only.
> Inside the container, code lives at `/var/www/`. Never use the mount path as
> `workingDir` in `zerops_deploy` — the default `/var/www` is always correct.

1. **Deploy to appdev**: `zerops_deploy targetService="appdev"` — self-deploy (sourceService auto-inferred, includeGit auto-forced). SSHes into dev container, runs `git init` + `zcli push -g` on native FS at `/var/www`. SSHFS mount auto-reconnects after deploy — no remount needed. Deploy tests the build pipeline and ensures deployFiles artifacts persist.
2. **Start appdev** (deploy activated envVariables — no server runs): start server via SSH (Bash tool `run_in_background=true`). Env vars are now OS env vars. **Implicit-webserver runtimes (php-nginx, php-apache, nginx, static): skip this step** — web server starts automatically after deploy.
3. **Enable dev subdomain**: `zerops_subdomain serviceHostname="appdev" action="enable"` — returns `subdomainUrls`
4. **Verify appdev**: `zerops_verify serviceHostname="appdev"` — must return status=healthy
5. **Iterate if needed** — if `zerops_verify` returns degraded/unhealthy, enter the iteration loop: diagnose from `checks` array -> fix on mount path -> redeploy -> re-verify (max 3 iterations)
6. **Generate stage entry** in zerops.yml — dev is proven, now write the `setup: appstage` entry. See "Dev → Stage: What to know" below.
7. **Deploy to appstage from dev**: `zerops_deploy sourceService="appdev" targetService="appstage"` — SSH mode: pushes from dev container to stage. Zerops runs the `setup: appstage` build pipeline. Transitions stage from READY_TO_DEPLOY -> BUILDING -> RUNNING. Stage is never a deploy source — no `.git` needed on target.
7b. **Connect shared storage to stage** (if shared-storage is in the stack): `zerops_manage action="connect-storage" serviceHostname="appstage" storageHostname="storage"` — stage was READY_TO_DEPLOY during import, so the import `mount:` did not apply.
8. **Enable stage subdomain**: `zerops_subdomain serviceHostname="appstage" action="enable"` — returns `subdomainUrls`
9. **Verify appstage**: `zerops_verify serviceHostname="appstage"` — must return status=healthy
10. **Present both URLs** to user:
    ```
    Dev:   {subdomainUrl from enable}
    Stage: {subdomainUrl from enable}
    ```

### Dev → Stage: What to know

- **Stage has a real start command — server starts automatically after deploy.** No SSH start needed (unlike dev). Zerops monitors the app via healthCheck and restarts on failure. You just deploy and verify.
- **Stage runs the full build pipeline.** `buildCommands` execute in a clean build container — they may include compilation (Go, Rust, Java) or asset building (TypeScript, Vite) that dev didn't need.
- **After deploy, only `deployFiles` content exists.** Anything installed manually via SSH (pip, gems, cargo cache) is gone. If the app needs runtime deps not in `deployFiles`, use `prepareCommands` (runs on each container start) or `buildCommands` (runs during build).
- **Copy envVariables from dev** — already proven via /status.
- **Your runtime knowledge has Prod deploy patterns.** Use as reference, adapt from what you discovered during dev iteration.

### Dev iteration: manual start cycle

After `zerops_deploy` to dev, env vars from zerops.yml are available as OS env vars. The container runs `zsc noop --silent` — no server process. The agent starts the server via SSH.

**Key facts:**
1. **After deploy, env vars are OS env vars.** Available immediately when the server starts. NEVER hardcode values or pass them inline.
2. **Code on SSHFS mount is live on the container** — the dev workflow depends on runtime (watch-mode frameworks reload automatically, others need manual restart).
3. **Redeploy only when zerops.yml itself changes** (envVariables, ports, buildCommands). Code-only changes on the mount just need a server restart.

**The cycle:**
1. **Edit code** on the mount path — changes appear instantly in the container at `/var/www/`.
2. **Kill previous server and start new one** via SSH — use the Bash tool with `run_in_background=true`. The server runs in SSH foreground, output streams to background task. Do NOT use `nohup`, `&`, or file redirects.
3. **Check startup** — read background task output via `TaskOutput`. Look for startup message or errors.
4. **Test endpoints** — `ssh {devHostname} "curl -s localhost:{port}/health"` | jq .
5. **If broken**: fix code on mount, stop server task, restart from step 2.

**Implicit-webserver runtimes (php-nginx, php-apache, nginx, static) skip manual start.** The web server starts automatically after deploy. Before first deploy, the container runs bare nginx/apache — go straight to deploy.

**Piping rule:** `ssh {dev} "curl -s localhost:{port}/api"` | jq . — pipe OUTSIDE SSH. `jq` is not available inside containers.

### Dev-only mode — deploy flow

Same as standard mode steps 1-5, but no stage pair. All verification happens on the dev service directly. After dev is verified, present the URL to the user — no stage deploy needed.

### Simple mode — deploy flow

1. **Import services** with `startWithoutCode: true` so the service starts immediately:
   ```
   zerops_import content="<import.yml>"
   zerops_discover
   ```

   > **Subdomain activation:** `enableSubdomainAccess: true` in import.yml pre-configures routing, but **does NOT activate it**. You MUST call `zerops_subdomain action="enable"` after deploy to activate the L7 balancer route. The enable response contains `subdomainUrls` — this is the **only** source for subdomain URLs. Without the explicit enable call, the subdomain returns 502. The call is idempotent — safe to call even if already active.

2. **Mount and discover:**
   ```
   zerops_mount action="mount" serviceHostname="{hostname}"
   zerops_discover includeEnvs=true
   ```

3. **Write code** to mount path `/var/www/{hostname}/`. Use `${hostname_varName}` references in zerops.yml envVariables — NEVER hardcode credentials. Env vars activate after deploy.

#### Simple mode zerops.yml

Simple mode services self-deploy (same rules as dev services).
Unlike dev, simple mode uses a real `start` command and `healthCheck` — server auto-starts after deploy with env vars injected.

```yaml
zerops:
  - setup: {hostname}
    build:
      base: {runtimeVersion}
      buildCommands: [<from runtime knowledge>]
      deployFiles: [.]   # CRITICAL: self-deploy — MUST be [.] for iteration
      cache: [<runtime-specific cache dirs>]
    run:
      base: {runtimeBase}   # Omit for compiled langs (Go, Rust) or use same as build
      ports:
        - port: {port}
          httpSupport: true
      envVariables:
        # Map discovered variables to app-expected names
      start: {startCommand}   # Real start: ./app, node index.js, bun run src/index.ts, etc.
      healthCheck:
        httpGet:
          port: {port}
          path: /health
```

**Key differences from dev+stage template:**
- Single `setup:` entry (not two)
- `start:` uses real command (not idle start)
- `healthCheck` included — app auto-starts after deploy
- If recipe uses tilde syntax in `deployFiles` (e.g., `.output/~`), adjust `start` to include the directory prefix (e.g., `node .output/server/index.mjs` instead of `node server/index.mjs`)

4. **Deploy:**
   ```
   zerops_deploy targetService="{hostname}"
   ```

5. **Enable subdomain**: `zerops_subdomain serviceHostname="{hostname}" action="enable"` — returns `subdomainUrls`

6. **Verify**: `zerops_verify serviceHostname="{hostname}"` — must return status=healthy

7. If verification fails, iterate (diagnose -> fix -> redeploy).

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

These vars become OS env vars after `zerops_deploy` activates zerops.yml envVariables.
Your app reads them via the runtime's native env var API. No `.env` files. No dotenv libraries.
**NEVER hardcode credential values** — always use `${hostname_varName}` references in zerops.yml.

### Mapping in zerops.yml

```yaml
envVariables:
  # Format: YOUR_APP_VAR: ${discovered_key}
  # Only use variables listed above — anything else will be empty at runtime
```

## Runtime Knowledge

{runtimeKnowledge}

## zerops.yml Structure

Write the dev setup entry now. Stage entry is generated after dev is verified (task 9).

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
      start: zsc noop --silent   # Dev: idle container. Implicit-webserver runtimes (php-nginx, php-apache, nginx, static): omit start AND ports entirely.
      # NO healthCheck — agent controls lifecycle manually
  # Stage entry: generated after dev is verified (task 10)
```

## Application Requirements

**Environment variables**: see "Discovered Environment Variables" above. Read via runtime's native env var API.

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
  "status": "ok",
  "connections": {
    "db": {"status": "ok", "latency_ms": 5},
    "cache": {"status": "ok", "latency_ms": 1}
  }
}
```

The top-level `"status": "ok"` is ALWAYS required — with or without connections.

- PostgreSQL/MariaDB/MySQL: execute `SELECT 1`
- Valkey/KeyDB: execute `PING`
- No managed services: return `{"service": "{devHostname}", "status": "ok"}`

Generate apps with a /status endpoint that proves real managed service connectivity.

## Tasks

**CRITICAL**: `zerops_deploy` to dev restarts the container — your server DIES.
After every deploy to dev, you MUST start the server via SSH before `zerops_verify` can pass.
This applies EVERY time — not just the first deploy. Implicit-webserver runtimes (php-nginx, php-apache, nginx, static): server auto-starts, skip manual start.

Execute IN ORDER. Every step has verification — do not skip any.

| # | Task | Action | Verify |
|---|------|--------|--------|
| 1 | Write zerops.yml (dev entry only) | Write to `{mountPath}/zerops.yml` with dev setup entry. Stage entry comes later (task 9). | File exists with dev setup name |
| 2 | Write app code | HTTP server on the port defined in zerops.yml `run.ports` with `/`, `/health`, `/status`. Read env vars via runtime's native API. | Code references discovered env vars |
| 3 | Write .gitignore | Build artifacts and IDE files only. Do NOT include `.env` — no .env files exist on Zerops | File exists, no `.env` entry |
| 4 | Deploy dev | `zerops_deploy targetService="{devHostname}"` — activates envVariables as OS env vars | status=DEPLOYED (blocks until complete) |
| 5 | Verify build | Check zerops_deploy return value | Not BUILD_FAILED or timedOut |
| 6 | Start dev server | Start via SSH (Bash tool `run_in_background=true`). Env vars are available after deploy. Skip for implicit-webserver runtimes (php-nginx, php-apache, nginx, static — auto-starts). | `TaskOutput` shows startup message |
| 7 | Activate subdomain | `zerops_subdomain serviceHostname="{devHostname}" action="enable"` | Returns `subdomainUrls` |
| 8 | Verify dev | `zerops_verify serviceHostname="{devHostname}"` | status=healthy |
| 9 | Generate stage entry | Dev is proven — now write the stage `setup:` entry in zerops.yml. Stage runs the production build of your app. `start:` must be the production run command appropriate for your runtime and framework (not the dev command from SSH). Your runtime knowledge Prod deploy pattern shows examples — adapt to your specific situation. `buildCommands` include the full build pipeline (deps + compilation/bundling if applicable). `deployFiles` are the build output (not `[.]`). Add `healthCheck`. Copy `envVariables` from dev. | Stage entry in zerops.yml |
| 10 | Review stage entry | Is `start:` a production run command (adapted to your framework, not a generic example)? Do `buildCommands` produce what `deployFiles` expects? `healthCheck` present? | Self-check |
| 11 | Deploy stage | `zerops_deploy sourceService="{devHostname}" targetService="{stageHostname}"` — stage has real start command, server auto-starts. No SSH start needed. | status=DEPLOYED (blocks until complete) |
| 12 | Verify stage | `zerops_subdomain action="enable"` + `zerops_verify serviceHostname="{stageHostname}"` | status=healthy |
| 13 | Report | Status (pass/fail) + dev URL + stage URL | — |

Tasks 6→7→8 are gated: subdomain activation (7) and verify (8) WILL FAIL if server not started (6).

## Iteration Loop (when verification fails)

If `zerops_verify` returns "degraded" or "unhealthy", iterate — do NOT skip ahead to stage:

**Debugging priority: ALWAYS check application logs first.** Read `zerops_logs` + framework log files on mount path (e.g., `{mountPath}/storage/logs/laravel.log` for Laravel) before trying any other fix.

1. **Diagnose**: Read the `checks` array from the `zerops_verify` response:
   | Check result | Diagnosis action |
   |-------------|-----------------|
   | service_running: fail | Service not running — check deploy status, read error logs: `zerops_logs severity="error" since="10m"` |
   | no_error_logs: info | Advisory — error-severity logs found. Read detail. If SSH/infra noise, ignore. If app errors, investigate with `zerops_logs` |
   | startup_detected: fail | App crashed on start — `zerops_logs severity="error" since="5m"` |
   | no_recent_errors: info | Advisory — same as above. Recent error-severity logs found. Read detail to determine if actionable |
   | http_health: fail | App started but endpoint broken — check `zerops_logs` + framework log files on mount path (e.g., `{mountPath}/storage/logs/laravel.log`) FIRST, then check `detail` for HTTP status. Do NOT try alternative servers (`php artisan serve`, `node server.js`, etc.) — fix the underlying error. |
   | http_status: fail | Managed service connectivity issue — check `detail` for which connection failed. Verify env var mapping matches discovered vars. |

2. **Fix**: Edit files at `{mountPath}/` — fix zerops.yml, app code, or both

3. **Redeploy** (only if zerops.yml changed): `zerops_deploy targetService="{devHostname}"`. For code-only fixes on the mount, just restart the server — no redeploy needed.

4. **Start server** via SSH (Bash tool `run_in_background=true`). Env vars are present after deploy. Check startup via `TaskOutput`.

5. **Re-verify**: `zerops_verify serviceHostname="{devHostname}"` — check status=healthy

Max 3 iterations. After that, report failure with diagnosis.

## Platform Rules

- All deploys use SSH — `zerops_deploy targetService="{hostname}"` for self-deploy (sourceService auto-inferred, includeGit auto-forced), `sourceService="{dev}" targetService="{stage}"` for cross-deploy.
- For new projects: write manifests only (package.json, go.mod, Gemfile). Do NOT write lock files (go.sum, bun.lock, package-lock.json) — let build commands generate them. For existing projects: preserve committed lock files.
- NEVER write dependency dirs (node_modules/, vendor/).
- zerops_deploy blocks until build completes — returns DEPLOYED or BUILD_FAILED with build duration.
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
| HTTP 500 from app | Check app logs FIRST (`zerops_logs` + framework log files on mount path). The log tells you the exact cause — do not guess. |
| Agent tries alternative web server | NEVER use alternative servers (`php artisan serve`, `node server.js`, etc.) on implicit-webserver runtimes (php-nginx, php-apache, nginx, static). Fix the underlying error instead. |
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
   | Build FAILED | `zerops_deploy` response includes `buildLogs` with last 50 lines of build pipeline output. Check for: wrong buildCommands, missing deps, wrong base version, missing "type: module". Fix and redeploy. |
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
| HTTP 500 | App error | Check `zerops_logs` + framework log files on mount path — log tells exact cause. Do NOT start alternative servers. |
</section>

<section name="deploy-agents">
### For 2+ runtime service pairs — agent orchestration

**Parent agent steps:**
1. `zerops_import content="<import.yml>"` — create all services
2. `zerops_discover` — verify dev services reached RUNNING
3. Mount all dev services
4. Discover ALL env vars: `zerops_discover includeEnvs=true`
5. Spawn Service Bootstrap Agents (in parallel) for each runtime service pair
6. Spawn Verify-Managed Agents (in parallel) for each managed service
7. After ALL agents complete: `zerops_discover` — final verification

See the main deploy section for the full Service Bootstrap Agent Prompt and Verify-Managed Agent Prompt templates.
</section>

<section name="deploy-recovery">
### Recovery and iteration

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| Build FAILED: "command not found" | Wrong buildCommands for runtime | Check recipe, fix build pipeline |
| Build FAILED: "module not found" | Missing dependency init | Add install step to buildCommands |
| App crashes: "EADDRINUSE" | Port conflict | Ensure app listens on correct port from zerops.yml |
| App crashes: "connection refused" | Wrong DB/cache host | Check envVariables mapping matches discovered vars |
| /status: "db: error" | Missing or wrong env var | Compare zerops.yml envVariables with discovered var names |
| HTTP 502 | Subdomain not activated | Call `zerops_subdomain action="enable"` |
| curl returns empty | App not listening on 0.0.0.0 | Add HOST=0.0.0.0 to envVariables |
| HTTP 500 | App error | Check `zerops_logs` + framework log files on mount path |

Max 3 iterations per service. After that, report failure with diagnosis to user.
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

### After completion — next iteration

If the user asks for changes after initial bootstrap:
1. Reuse discovery data — do not re-discover unless services were added/removed.
2. Make the code/config change on the mount path.
3. Deploy to dev first, verify (with iteration loop if needed), then stage. Same dev-first pattern.
4. For config-only changes (env vars, scaling), use configure/scale workflows directly.
</section>

<section name="close">
### Close Bootstrap

**Administrative step** — no checker validation needed. Marks bootstrap as complete and outputs the transition message with next-step guidance.

The transition message includes:
- **Services list** — all provisioned services with modes and dependencies
- **Deploy strategy options** — push-dev, ci-cd, or manual (strategy selection happens during the deploy or cicd workflows, not here)
- **CI/CD gate requirements** — if the user chooses CI/CD strategy later
- **Router offerings** — ranked workflow suggestions (deploy, cicd, and utilities)

**Complete this step:** use `zerops_workflow action="complete" step="close" attestation="Bootstrap finalized, services operational"`.

**Skip this step** only in impossible edge cases (no services at all). Normal projects always reach this step.
</section>
