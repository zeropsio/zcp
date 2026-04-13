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
### Process

1. Read `availableStacks` in this response — these are the ONLY valid service types. Pick from them. Do not guess or invent type names.
2. Call `zerops_discover` to see what already exists in the project.
3. Map the user's request to types from `availableStacks`.
4. If the stack uses a known framework, load its recipe: `zerops_knowledge recipe="{name}"`.
5. Choose mode (standard/dev/simple), present plan to user for confirmation.
6. After user confirms, submit: `zerops_workflow action="complete" step="discover" plan=[...]`

### Classify existing services

Call `zerops_discover` to see what exists. Each service returns `managedByZCP` (has ServiceMeta) and `isInfrastructure` (database, cache, storage) fields. Classify based on facts:

| Discover result | Category | Action |
|----------------|----------|--------|
| No runtime services (empty or infrastructure-only) | Empty project | Full bootstrap |
| All runtime services have `managedByZCP=true` | All managed | If stack matches request, route to develop. If different stack requested, ASK user how to proceed. NEVER auto-delete. |
| Any runtime service has `managedByZCP=false` | Unmanaged runtimes exist | ASK user how to proceed. Options: (a) add new services with different hostnames alongside existing, (b) user explicitly approves deletion of specific named services, (c) work with existing. NEVER auto-delete. |

Route:
- Empty project: proceed normally through all steps
- All managed: if stack matches, skip bootstrap — route to develop workflow (`zerops_workflow action="start" workflow="develop"`). If user wants a different stack, ASK before making any changes. If no ServiceMeta files exist for these services, treat as adoption (see below).
- Unmanaged runtimes exist: present existing services to user with types and status. Options: (a) add new services alongside existing, (b) user explicitly approves deletion of specific named services, (c) **adopt existing services** (recommended — see below). NEVER delete without explicit user approval naming each service.

#### Adopting existing services

When runtime services exist but ZCP doesn't know them (no ServiceMeta files), they need to be adopted before workflows (deploy, CI/CD) can manage them. Adoption registers them in ZCP without recreating or modifying them.

**Adoption is fast:** After the plan is submitted, the provision step verifies services exist, mounts dev runtimes, discovers env vars, and auto-completes the remaining steps. No code generation or deployment happens — existing code and configuration are preserved.

**Steps:**

1. List all runtime services from discover result (exclude managed services — databases, caches, storage).

2. For each runtime service, determine suggested mode. Adoption accepts ANY valid hostname — do not require `dev`/`stage` suffixes:
   - If two services form an obvious dev+stage pair (e.g., `apidev`+`apistage`, `web`+`webstage`, `backend`+`backendprod`) → suggest **standard** mode with explicit `stageHostname` if hostnames don't follow `{name}dev`/`{name}stage` convention
   - If a single service exists and user doesn't need a stage pair → suggest **dev** mode (works with any hostname: `api`, `backend`, `appdev`, etc.)
   - If a single service exists and user wants dev+stage → suggest **standard** with explicit `stageHostname`
   - If user wants the simplest setup → suggest **simple** mode (any hostname)

3. **STOP — MANDATORY USER CONFIRMATION GATE**

   Present to user and WAIT for explicit confirmation before proceeding:

   "These services already exist. I'll register them in ZCP so I can manage deploys, CI/CD, and configuration. I won't recreate or delete anything.

   Services:
   - [hostname] ([type]) — mode: [standard/dev/simple]

   Modes:
   - **standard** — dev + stage pair (e.g., appdev + appstage)
   - **dev** — single service, no stage pair
   - **simple** — single service, auto-start

   Enable public subdomain access? [list services that don't have it enabled]

   OK?"

   Do NOT proceed until user confirms. Do NOT auto-proceed. Do NOT assume consent.

4. After user confirms, submit plan with `isExisting: true` on each adopted runtime target:
   ```
   zerops_workflow action="complete" step="discover" plan=[{
     runtime: {devHostname: "api", type: "go@1", isExisting: true, bootstrapMode: "simple"},
     dependencies: [{hostname: "db", type: "postgresql@16", resolution: "EXISTS"}]
   }]
   ```

   For standard mode with non-`dev` hostnames, provide explicit `stageHostname`:
   ```
   zerops_workflow action="complete" step="discover" plan=[{
     runtime: {devHostname: "zmon", type: "go@1", isExisting: true, bootstrapMode: "standard", stageHostname: "zmonstage"},
     dependencies: [{hostname: "db", type: "postgresql@16", resolution: "EXISTS"}]
   }]
   ```

5. Managed services go as dependencies with `resolution: "EXISTS"` — no special handling needed.

6. After plan submission, complete the provision step. For pure adoption plans (all targets `isExisting: true`, all deps `EXISTS`), the engine auto-completes generate, deploy, and close — adoption is done in 2 calls total.

**Mixed plans**: You CAN combine `isExisting: true` (adopt) and `isExisting: false` (create new) targets in one plan. Each target follows its own path through subsequent steps. Fast path only applies when ALL targets are existing.

#### Identify stack components

> **mode defaults to NON_HA for managed services** — databases, caches, object-storage, shared-storage.
> Set `HA` explicitly for production.

From the user's request, identify:
- **Runtime services**: type + framework (e.g., nodejs@22 with Next.js, go@1 with Fiber, bun@1.2 with Hono)
- **Managed services**: type + version (e.g., postgresql@16, valkey@7.2, elasticsearch@8.16)

**Verify all types against the `availableStacks` field in the workflow response.**

If the user hasn't specified, ask. Don't guess frameworks — the build config depends on it.

#### Choose bootstrap mode

- **Standard** (default): Creates dev + stage pair + shared managed services. Dev for iteration, stage for validation. Convention: `{name}dev` + `{name}stage`, but any valid hostname pair works with explicit `stageHostname`.
- **Dev**: Creates single service + managed services only. No stage pair. Any valid hostname (`api`, `backend`, `appdev`, etc.). When user says "just get it running" or "prototype."
- **Simple**: Creates single service + managed services. Real start command, auto-starts after deploy. Any valid hostname. Only if user explicitly requests simplest setup.

Default = standard (dev+stage). Ask user to confirm the mode before proceeding.

Hostname naming: `{name}dev`/`{name}stage` is a convention, not a requirement. For new services it provides auto-derivation; for adoption, use whatever hostnames already exist. Multi-runtime: use role or runtime as prefix — `phpdev`/`phpstage` or `api`/`apistage` (or explicit `stageHostname` for non-standard names). Managed services are shared, no dev/stage suffixes: `db`, `cache`, `storage`.

#### Load framework-specific knowledge (MANDATORY for known frameworks)

If your stack uses a known framework, you **MUST** load its recipe before proceeding:
```
zerops_knowledge recipe="{recipe-name}"
```
Examples: `laravel`, `django`, `phoenix`, `nextjs`, `ghost`, `symfony`, `rails`

Recipes contain critical patterns that generic runtime guides do not cover:
- **Required secrets** (APP_KEY, SECRET_KEY_BASE, etc.) — what goes in import.yaml `envSecrets`
- **Scaffolding commands** — correct flags to avoid `.env` shadowing, RAM scaling
- **Driver/config defaults** — what breaks without a DB, what needs explicit env vars
- **Common failures** — framework-specific gotchas like "never use artisan serve on php-nginx"

Skip this only if you are certain no recipe exists for your framework. The `## Matching Recipes` section in the generate step guidance lists available recipes for your runtime.

#### Confirm and submit plan

**STOP — MANDATORY USER CONFIRMATION GATE**
You MUST present the plan and wait for explicit user confirmation before calling `zerops_workflow action="complete"`.
Do NOT auto-proceed. Do NOT assume consent. The user MUST reply before you continue.

Present exactly:
"I'll set up: [list services with types]. Mode: [standard/dev/simple]. OK?

Modes ([docs](https://docs-2004.prg1.zerops.app/)):
- **standard** — dev + stage pair, shared managed services. Dev for iteration, stage for validation.
- **dev** — single service + managed services. No stage pair. Quick prototyping.
- **simple** — single service with real start command, auto-starts after deploy."

Then STOP and WAIT for the user's response. Only after the user explicitly confirms, proceed to submit.

**Managed-only projects**: If the user only needs managed services (databases, caches, storage) with no runtime services, submit an empty plan: `plan=[]`. Steps generate, deploy, and close will be skipped automatically.

After user confirms, submit:
```
zerops_workflow action="complete" step="discover" plan=[{runtime: {devHostname, type, bootstrapMode}, dependencies: [{hostname, type, resolution}]}]
```
</section>

<section name="provision">
### Generate import.yaml, provision services, discover env vars

**Adopted services (isExisting: true):** Do NOT generate import.yaml entries for adopted services — they already exist on the platform. If the plan contains ONLY adopted targets with all dependencies as `EXISTS`, skip import entirely and go straight to env var discovery. If the plan mixes new + adopted targets, generate import.yaml for new targets only.

Generate import.yaml ONLY. Do NOT write zerops.yaml or application code — that happens in the generate step AFTER env var discovery. The import.yaml schema rules are included below.

> **CRITICAL — fresh file only.** ALWAYS write a NEW import.yaml from scratch containing ONLY the services for THIS bootstrap session. If an import.yaml already exists at the project root, OVERWRITE it completely — do NOT read, append to, or modify it. Appending to a leftover file re-imports previously created services, causing duplicates and failures. In container mode, ZCP automatically removes import.yaml from the project root after this step (copying it to mount paths for provenance). In local mode, the file stays in the project directory.

> **mode defaults to NON_HA for managed services** — databases, caches, object-storage, shared-storage.
> Set `HA` explicitly for production.

**Hostname pattern** (from Step 1): Standard mode (default) creates `{name}dev` + `{name}stage` pairs (e.g., "appdev"/"appstage", "apidev"/"apistage", "webdev"/"webstage") with shared managed services. Dev mode creates a single `{name}dev` (or any valid hostname). Simple mode creates a single `{name}`. If the user didn't specify, ask before generating.

**Runtime service properties by type** (applies to both container and local environments):

| Property | Dev service | Stage service | Simple service |
|----------|-----------|---------------|----------------|
| `startWithoutCode` | `true` | omit | `true` |
| `maxContainers` | `1` | omit (default) | omit (default) |
| `enableSubdomainAccess` | `true` | `true` | `true` |
| `verticalAutoscaling.minRam` | `1.0` for compiled runtimes (Go, Rust, Java, .NET, Elixir, Gleam) | omit (default) | omit (default) |

**Why `startWithoutCode: true`**: Dev and simple services need to reach RUNNING state before the first deploy — otherwise they stay at READY_TO_DEPLOY, which blocks SSHFS mount and SSH access. Stage services deliberately omit this flag — they wait at READY_TO_DEPLOY until first cross-deploy from dev (no wasted resources on an empty container).

**Expected states after import**: Dev → RUNNING, Simple → RUNNING, Stage → READY_TO_DEPLOY, Managed → RUNNING/ACTIVE.

**Shared storage mount** (if shared-storage is in the stack): Add `mount: [{storage-hostname}]` to both dev and stage service definitions in import.yaml. This pre-configures the connection but does NOT make storage available at runtime. You MUST also add `mount: [{storage-hostname}]` in the zerops.yaml `run:` section and deploy for the storage to actually mount at `/mnt/{storage-hostname}`.

> **Two kinds of "mount" (disambiguation):** (1) `zerops_mount` — SSHFS tool, mounts service `/var/www` locally for development. (2) Shared storage mount — platform feature, attaches a shared-storage volume at `/mnt/{hostname}` via `mount:` in import.yaml + zerops.yaml. These are completely unrelated.

> **IMPORTANT**: Import `mount:` only applies to ACTIVE services. Stage services are READY_TO_DEPLOY during import, so the mount pre-configuration silently doesn't apply. After first deploy transitions stage to ACTIVE, connect storage via `zerops_manage action="connect-storage" serviceHostname="{stage}" storageHostname="{storage}"`.

**Validation checklist** (import.yaml only):

| Check | What to verify |
|-------|---------------|
| Hostnames | Follow [a-z0-9] pattern, max 25 chars |
| Service types | Match available stacks |
| No duplicates | No duplicate hostnames |
| object-storage | Requires `objectStorageSize` field |
| Preprocessor | `#zeropsPreprocessor=on` if using `<@...>` functions |
| Mode present | Managed services default to NON_HA if omitted |
| Framework secrets | If using Laravel/Rails/Django/etc., add `envSecrets` with `<@generateRandomString(...)>` (e.g., `APP_KEY`, `SECRET_KEY_BASE`). These are auto-injected as OS env vars — do NOT re-reference them in zerops.yaml `run.envVariables`. |

**Managed service hostname conventions**: `db` (postgresql/mariadb), `cache` (valkey), `queue` (nats/kafka), `search` (elasticsearch/meilisearch), `storage` (object-storage). Standardizes cross-service references and discovery.

**Priority ordering**: managed services `priority: 10`, runtime services default or `priority: 5`. Databases must be ready before apps that depend on them.

**zeropsSetup**: only set in import.yaml when using `buildFromGit` (API rejects one without the other). For workspace deploys without `buildFromGit`, use `zerops_deploy setup="..."` to map hostname to setup name. zerops.yaml uses two canonical setup names from recipes: `setup: dev` (development workspace — idle start, full source, no healthCheck) and `setup: prod` (production/stage/simple — real start, healthCheck). At deploy time, pass `setup="dev"` or `setup="prod"` to map the hostname to the correct entry.

**File extensions:** Use `import.yaml` and `zerops.yaml` for all new files. The legacy `.yml` extension is also accepted — existing repos may use `zerops.yml`.

Present import.yaml to the user for review before proceeding. Then import:
```
zerops_import filePath="./import.yaml"
```

### Auto-mount

When this step completes, ZCP automatically mounts all dev and simple-mode runtime services via SSHFS. Mount results are returned in the `autoMounts` field of the response. You do NOT need to call `zerops_mount` — it happens automatically. Stage services (READY_TO_DEPLOY) and managed services are not mounted. If auto-mount fails, the error is reported but the step still advances — you can retry manually with `zerops_mount action="mount" serviceHostname="{hostname}"`.

### Env var discovery protocol (mandatory before generate)

After importing services and waiting for them to reach RUNNING, discover the ACTUAL env vars available to each service. This data is critical for writing correct zerops.yaml envVariables and for subagent prompts.

**Single call — returns env var keys for ALL services (keys only — sufficient for `${hostname_varName}` references):**
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

**In zerops.yaml envVariables**, map discovered vars to what the app expects:
```yaml
envVariables:
  DATABASE_URL: ${db_connectionString}
  REDIS_HOST: ${cache_host}
  REDIS_PORT: ${cache_port}
  # Map ONLY variables listed in the discovery response
```

**ONLY use variables that were actually discovered.** Guessing variable names causes runtime failures. If a variable doesn't appear in discovery, it doesn't exist.

**How these reach your app**: All variables mapped in zerops.yaml `envVariables` are injected as standard OS environment variables at container start. Your app reads them with the runtime's native env var API. No `.env` files or dotenv libraries needed.
</section>

<section name="generate">
### Generate zerops.yaml and infrastructure verification server

This step verifies that infrastructure works — nothing more. Regardless of what the user asked for (dashboard, API, blog, e-commerce, anything), you are writing a bare-minimum verification server that proves services are reachable and env vars resolve. The user's actual application is implemented AFTER bootstrap, in the develop workflow. This applies to ALL modes — standard, dev, and simple. Never write application logic in bootstrap.

Write a hello-world server with exactly three endpoints: `GET /`, `GET /health`, `GET /status`. No business logic, no UI, no features, no API beyond these three routes. The server should be under 50 lines of code.

**Adopted services (isExisting: true):** Skip zerops.yaml and code generation entirely for adopted targets. These services already have working code and configuration from their previous deploy. The generate checker automatically skips validation for adopted targets. If ALL targets are adopted, complete this step with attestation "All targets are existing services — no code generation needed."

**Prerequisites**: Services auto-mounted (provision step), env vars discovered.

> **WHERE TO WRITE FILES**: Write ALL files (zerops.yaml, app code, configs) to the SSHFS mount path `/var/www/{hostname}/`. The mount paths are returned in the provision step's `autoMounts` response. `/var/www/` without the hostname suffix is the zcpx orchestrator's own filesystem — writing there has NO effect on the target service. Every file must go under `/var/www/{hostname}/`.

**SSHFS is a file bridge, not an execution environment.** The mount lets you read and write files on the target service's `/var/www/` from the zcp orchestrator — exactly what you want for editing code with Read/Edit/Write. It does NOT transplant the target's runtime, dependencies, or environment over to zcp.

**Two execution surfaces, two roles — pick the right one for the tool.**

| Surface | What lives there | What runs there |
|---|---|---|
| **Target container** (e.g. `apidev`, `appdev`) | The app's base image (correct Node/PHP/Go/… version), the dependency tree installed by `build.buildCommands`, `run.envVariables` including `${hostname_varName}` cross-service refs, private-network reachability to managed services (db, cache, storage, search), the dev container's own resource budget | Anything that IS part of the app's world: compilers, type-checkers, test runners, linters, framework CLIs, package managers, app-level curl/http against the running app or managed services |
| **zcp orchestrator** | The platform API clients (`zerops_*` MCP tools), `agent-browser` (Chrome driver), the recipe output directory, git for recipe deliverables, the workflow state, Read/Edit/Write tools talking to SSHFS mounts | Anything that operates ON the app from outside: platform actions via MCP, browser automation against the app's PUBLIC subdomain URL, authoring/committing recipe deliverable files, filesystem inspection across mounts |

The principle: **where is the tool's world?** If the tool IS the app's toolchain, it belongs on the target (because the target has the right version, deps, env, and reachability). If the tool operates on the app from outside (browser, platform API, deliverable authoring), it belongs on zcp by design — the target container doesn't have Chrome installed and shouldn't.

**For target-side commands, always SSH — never run them on zcp via the mount:**

```
ssh {hostname} "cd /var/www && {command}"    # correct — runs where the app lives
cd /var/www/{hostname} && {command}          # WRONG — runs on zcp against the mount
```

Why "run on zcp via the mount" is wrong even when it seems to work:
- **Wrong runtime version** — zcp has whatever zcp happens to have, not the service's base image version.
- **Wrong dependency tree** — any `node_modules`/`vendor`/`.venv` on zcp is accidental; the real tree was installed by `build.buildCommands` inside the target.
- **Wrong environment** — `${hostname_varName}` cross-service refs are injected into the target at deploy time. zcp sees none of them, so a command that "runs" on zcp silently connects to the wrong DB, skips auth, or fails in subtly different ways.
- **No managed-service reachability** — db/cache/storage/search are on the project's private network. zcp is not.
- **Wrong process budget** — zcp's `nproc`/`pthread_create` budget is sized for orchestration, not compilation. Running compilers/test-runners/package-managers there exhausts it and produces `fork failed: resource temporarily unavailable` cascades that break unrelated later commands. **This is the loudest symptom, not the root problem.**

Target-side commands (run via SSH), non-exhaustive:
- Dependency installs: `npm install`, `npm ci`, `pip install`, `go mod download`, `composer install`, `bundle install`, `cargo fetch`, `pnpm install`, `yarn install`
- Compilers / type-checkers: `npx tsc`, `npm run build`, `tsc --noEmit`, `svelte-check`, `npm run check`, `cargo build`, `go build`, `mvn compile`, `nest build`
- Test runners: `jest`, `vitest`, `pytest`, `phpunit`, `go test`, `cargo test`, `rspec`
- Linters / formatters: `eslint`, `prettier`, `ruff`, `pylint`, `phpstan`, `rubocop`, `golangci-lint`
- Framework CLIs: `artisan`, `rails`, `manage.py`, `nest`, `ng`, `rake`
- App-level `curl`/`node`/`python -c` used to ping the running app or managed services

zcp-side commands (run directly):
- `zerops_*` MCP tools (platform API)
- `agent-browser` (drives Chrome against the target's PUBLIC subdomain URL — the target container doesn't have Chrome and shouldn't)
- Read/Edit/Write against mounts, `ls`/`cat`/`head`/`tail`/`grep`/`rg`/`find` for filesystem inspection
- `git status`/`add`/`commit` against the recipe output directory on zcp, or against the mount (the mount is the target's working tree)

**If the target container doesn't have a tool you need** for an app-level command, the fix is almost always to add it to the service's `build.base` / `build.prepareCommands` / `build.buildCommands` (dev deps installed via `npm install`, not `npm ci --omit=dev`; base packages via `zsc install`; …) and redeploy — NOT to fall back to running the command on zcp. The dev setup is specifically designed to carry the full toolchain for this reason.

**If you see `fork failed: resource temporarily unavailable` or `pthread_create: Resource temporarily unavailable`** from any Bash call, you've been running target commands on zcp via the mount. Stop, re-run them via `ssh {hostname} "…"`, and treat the failure as a wrong-container execution mistake, not a framework or platform bug.

> **CRITICAL — self-deploying services MUST use `deployFiles: [.]`:** Containers are volatile. After deploy, ONLY `deployFiles` content survives. If a self-deploying service uses `[dist]`, `[app]`, or any build output path, all source files + zerops.yaml are DESTROYED. Further iteration becomes impossible. Any service that deploys to itself (dev services, simple mode services) MUST ALWAYS use `deployFiles: [.]`. No exceptions. Cross-deploy targets (stage) can use specific paths for compiled output because their source lives on the dev service.

**PHP runtimes (php-nginx, php-apache) are different:** The web server is built into the runtime and serves files automatically. There is no `start:` command — both dev and prod just need correct `deployFiles`.

#### Verification server endpoints

The verification server **MUST** expose exactly these endpoints and nothing else:

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

zerops.yaml `envVariables` are activated by deploy. After `zerops_deploy`:
- All mapped vars are injected as standard OS environment variables
- Cross-service references (`${db_connectionString}`) resolved to real values
- Vars persist across server restarts within the same container

Write zerops.yaml + app code first, then deploy, then start the server and test. **NEVER hardcode credential values or pass them inline** — always use `${hostname_varName}` references in zerops.yaml and read env vars via the runtime's native API. Do NOT create `.env` files — empty values shadow OS env vars. Tools like `composer create-project` create `.env` files automatically; use `--no-scripts` to prevent this. Check your framework recipe for the correct scaffolding command. Dotenv libraries in existing projects are harmless (they fall back to OS vars when no .env exists).

#### Env var mapping in zerops.yaml

In zerops.yaml `envVariables`, ONLY use variables discovered in the provision step:
```yaml
envVariables:
  DATABASE_URL: ${db_connectionString}
  REDIS_HOST: ${cache_host}
  REDIS_PORT: ${cache_port}
  # Map ONLY variables listed in the discovery response
```

#### Files are already on the container

Since you're writing to an SSHFS mount, every file you create or modify is immediately present on the running container. The deploy step runs the build pipeline and activates zerops.yaml config (envVariables, ports). Containers are volatile — only `deployFiles` content survives a deploy.

> Consider committing the generated code before proceeding to deploy. This preserves your work if the deploy cycle requires iteration.
</section>

<section name="generate-standard">
### Standard mode (dev+stage) — zerops.yaml rules

Infrastructure verification only — write a hello-world server (/, /health, /status), not the user's application.

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
- [ ] zerops.yaml has `setup: dev` entry (canonical recipe name, NOT the hostname). Stage `setup: prod` comes after dev is verified.
- [ ] Dev setup uses `deployFiles: [.]` — NO EXCEPTIONS
- [ ] Dev `run.start` is `zsc noop --silent` (not real start cmd) — implicit-webserver runtimes: omit start entirely
- [ ] `run.ports` port matches what the app listens on — implicit-webserver runtimes: omit ports (port 80 fixed)
- [ ] `envVariables` ONLY uses variables discovered in provision step
- [ ] App binds to `0.0.0.0:{port}`, NOT localhost
</section>

<section name="generate-dev">
### Dev-only mode — zerops.yaml rules

Infrastructure verification only — write a hello-world server (/, /health, /status), not the user's application.

**Write a single dev entry. No stage service exists in this mode.**
All files go to `/var/www/{devHostname}/` (the SSHFS mount path from provision).

**Dev setup rules** (identical to standard dev):
- `deployFiles: [.]` — ALWAYS, no exceptions.
- `start: zsc noop --silent` — container stays alive but idle. The agent starts the server manually via SSH. **Does NOT apply to PHP runtimes** (php-nginx, php-apache) — omit `start:` entirely.
- `buildCommands:` — dependency installation only (no compilation).
- **NO healthCheck** — agent controls lifecycle manually.

**MANDATORY PRE-DEPLOY CHECK** (do NOT proceed until all pass):
- [ ] zerops.yaml has `setup: dev` entry (canonical recipe name, NOT the hostname)
- [ ] Dev setup uses `deployFiles: [.]` — NO EXCEPTIONS
- [ ] `run.start` is `zsc noop --silent` — implicit-webserver runtimes: omit start entirely
- [ ] `run.ports` port matches what the app listens on — implicit-webserver runtimes: omit ports (port 80 fixed)
- [ ] `envVariables` ONLY uses variables discovered in provision step
- [ ] App binds to `0.0.0.0:{port}`, NOT localhost
</section>

<section name="generate-simple">
### Simple mode — zerops.yaml rules

Infrastructure verification only — write a hello-world server (/, /health, /status), not the user's application.

**Write a single entry with a REAL start command.** Unlike dev/standard, simple mode services auto-start after deploy — no manual SSH start needed.
All files go to `/var/www/{hostname}/` (the SSHFS mount path from provision).

**Simple setup rules:**
- `deployFiles: [.]` — ALWAYS (self-deploy, source must survive).
- `start:` — **real start command** (`node index.js`, `bun run src/index.ts`, `./app`, etc.). NOT `zsc noop`.
- `buildCommands:` — dependency installation (compilation for Go/Rust/Java if needed).
- `healthCheck:` — **YES, required.** Zerops monitors the container and restarts on failure.

```yaml
zerops:
  - setup: prod
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
- [ ] zerops.yaml has `setup: prod` entry (canonical recipe name — simple mode uses prod profile)
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

**Adopted services (isExisting: true):** Do NOT call `zerops_deploy` for adopted services — they already have deployed code running. Instead, verify they are healthy: `zerops_verify serviceHostname="{hostname}"`. If verification fails, report the issue to the user — they need to fix their existing service. Only deploy adopted services if the user explicitly requests it or if zerops.yaml changes need activation.

> **`zerops_deploy` handles git internally.** It runs `git init`, `git config`, `git add`, `git commit`, and `zcli push` on the container automatically. Do NOT run git commands manually before or during deploy. The git identity is set to "Zerops Agent &lt;agent@zerops.io&gt;" for internal deploy commits.

> **Files are already on the container** via SSHFS mount — deploy does not "send" files there. Deploy runs the build pipeline (buildCommands, deployFiles), activates envVariables, and restarts the process.

`zerops_deploy` blocks until the build pipeline completes. It returns the final status (`DEPLOYED` or `BUILD_FAILED`) along with build duration. No manual polling needed.
`zerops_import` blocks until all import processes complete. It returns final statuses (`FINISHED` or `FAILED`) for each process.

### Platform rules

- All deploys use SSH — `zerops_deploy targetService="{hostname}" setup="dev"` for dev self-deploy (sourceService auto-inferred, includeGit auto-forced), `sourceService="{dev}" targetService="{stage}" setup="prod"` for cross-deploy to stage.
- For new projects: write manifests only (package.json, go.mod, Gemfile). Do NOT write lock files (go.sum, bun.lock, package-lock.json) — let build commands generate them. For existing projects: preserve committed lock files.
- NEVER write dependency dirs (node_modules/, vendor/).
- zerops_subdomain MUST be called once after the first deploy of each new service (even if enableSubdomainAccess was in import). Re-deploys do NOT deactivate it. Use `zerops_discover` to check status and get URL.
- subdomainUrls from enable response are already full URLs — do NOT prepend https://.
- Internal connections use http://, never https://.
- Env var cross-references: `${hostname_varName}` (e.g. `${db_hostname}`, `${db_port}`). Variable names are platform-defined — ALWAYS use `zerops_discover includeEnvs=true` (keys only) to get actual names, never guess.
- **Never self-reference an env var**: `FOO: ${FOO}` is always a tautology and always wrong. If a service needs a value it can't compute itself, supply it from **outside** the service's zerops.yaml — either at project level (`project.envVariables` in import.yaml — visible to every service) or at service level (the service's `envVariables:` block in import.yaml — visible only to that service). Then the service's zerops.yaml references the platform-injected key normally. Self-reference most often appears when an agent tries to "pipe" a value through zerops.yaml; the correct fix is to define it at the import.yaml layer, not inside zerops.yaml.
- **NO .env files** — Zerops injects all envVariables/envSecrets as OS env vars at container start. Do NOT create `.env` files, use dotenv libraries, or add env-file-loading code.
- 0.0.0.0 binding: app must listen on 0.0.0.0, not localhost or 127.0.0.1.

### Common deployment issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| HTTP 502 despite app running | App binds localhost, not 0.0.0.0 | Bind `0.0.0.0:{port}` |
| HTTP 000 (connection refused) | Server not running on dev service | Start server via SSH first |
| SSH hangs after starting server | Expected — server runs in foreground | Use Bash `run_in_background=true` |
| SSH exit 255 after deploy | Deploy created new container — old SSH sessions die | Open new SSH connection, start server again |
| `jq: command not found` via SSH | jq not in containers | Pipe outside: `ssh dev "curl ..." \| jq .` |
| Empty env variable | Wrong var name or not discovered | Check `zerops_discover includeEnvs=true` (keys only). If keys present but values suspect, add `includeEnvValues=true` to inspect actual values |
| SSHFS stale after deploy | Container replaced | Auto-reconnects — wait ~10s |
| Build FAILED: "command not found" | Wrong buildCommands for runtime | Check recipe, fix build pipeline |
| Build FAILED: "module not found" | Missing dependency init | Add `go mod tidy`, `bun install`, etc. to buildCommands |
| App crashes: "EADDRINUSE" | Port conflict | Check run.ports.port matches app |
| App crashes: "connection refused" | Wrong DB/cache host | Check envVariables mapping matches discovered vars |
| HTTP 502 after deploy | Subdomain not activated | Call `zerops_subdomain action="enable"` |
| HTTP 500 | App error | Check `zerops_logs` + framework log files on mount path |
</section>

<section name="deploy-standard">
### Standard mode (dev+stage) — deploy flow

**Prerequisites**: import done, dev auto-mounted (provision step), env vars discovered, code written to mount path.

> **Path distinction:** SSHFS mount path `/var/www/{devHostname}/` is LOCAL only.
> Inside the container, code lives at `/var/www/`. Never use the mount path as
> `workingDir` in `zerops_deploy` — the default `/var/www` is always correct.

**Core lifecycle** — deploy-first. Dev uses an idle start command (`zsc noop --silent`) so no server auto-starts. The agent MUST:
1. `zerops_deploy` to dev — activates envVariables, runs build pipeline, persists files
2. Start server via SSH — env vars are now available as OS env vars
3. `zerops_verify` dev — endpoints respond with real env var values
4. Generate stage entry in zerops.yaml — dev is proven, now write the production config
5. `zerops_deploy` to stage (stage has real `start:` command — server auto-starts there)
6. `zerops_verify` stage

Steps 1-3 repeat on every iteration. Stage (steps 4-6) only after dev is healthy.

**Detailed steps:**

1. **Deploy to appdev**: `zerops_deploy targetService="appdev" setup="dev"` — self-deploy. **Deploy creates a new container — ALL previous SSH sessions are dead (exit 255).** SSHFS mount auto-reconnects.
2. **Start appdev**: start server via **NEW** SSH connection (Bash tool `run_in_background=true`). Old SSH sessions are dead — always open fresh connection after deploy. **Implicit-webserver runtimes (php-nginx, php-apache, nginx, static): skip this step** — auto-starts.
3. **Enable dev subdomain**: `zerops_subdomain serviceHostname="appdev" action="enable"` — returns `subdomainUrls`
4. **Verify appdev**: `zerops_verify serviceHostname="appdev"` — must return status=healthy
5. **Iterate if needed** — if degraded/unhealthy, enter iteration loop below. Max 3 iterations.
6. **Generate stage entry** in zerops.yaml — dev is proven, now write the `setup: prod` entry. See "Dev → Stage" below.
7. **Deploy to appstage from dev**: `zerops_deploy sourceService="appdev" targetService="appstage" setup="prod"` — pushes from dev to stage. Transitions stage from READY_TO_DEPLOY → BUILDING → RUNNING.
7b. **Connect shared storage to stage** (if shared-storage in stack): `zerops_manage action="connect-storage" serviceHostname="appstage" storageHostname="storage"` — stage was READY_TO_DEPLOY during import, so the import `mount:` did not apply.
8. **Enable stage subdomain**: `zerops_subdomain serviceHostname="appstage" action="enable"`
9. **Verify appstage**: `zerops_verify serviceHostname="appstage"` — must return status=healthy
10. **Present both URLs** to user:
    ```
    Dev:   {subdomainUrl from enable}
    Stage: {subdomainUrl from enable}
    ```

### Dev → Stage: What to know

- **Stage has a real start command — server starts automatically after deploy.** No SSH start needed. Zerops monitors via healthCheck and restarts on failure.
- **Stage runs the full build pipeline.** `buildCommands` may include compilation or asset building that dev didn't need.
- **After deploy, only `deployFiles` content exists.** Anything installed manually via SSH is gone. Use `prepareCommands` or `buildCommands` for runtime deps.
- **Copy envVariables from dev** — already proven via /status.

### Dev iteration: manual start cycle

After `zerops_deploy` to dev, env vars are OS env vars. Container runs `zsc noop --silent` — no server process. Agent starts via SSH.

**Key facts:**
1. **Deploy = new container. All previous SSH sessions die (exit 255).** Always open new SSH after deploy.
2. **After deploy, env vars are OS env vars.** NEVER hardcode values or pass them inline.
3. **Code on SSHFS mount is live on the container** — watch-mode frameworks reload automatically, others need manual restart.
4. **Redeploy only when zerops.yaml itself changes** (envVariables, ports, buildCommands). Code-only changes just need server restart.

**The cycle:**
1. **Edit code** on the mount path — changes appear instantly in the container at `/var/www/`.
2. **Kill previous server and start new one** via SSH (Bash tool `run_in_background=true`). Do NOT use `nohup`, `&`, or file redirects.
3. **Check startup** — read background task output via `TaskOutput`.
4. **Test endpoints** — `ssh {devHostname} "curl -s localhost:{port}/health"` | jq .
5. **If broken**: fix code on mount, stop server task, restart from step 2.

**Implicit-webserver runtimes (php-nginx, php-apache, nginx, static) skip manual start.**

**Piping rule:** `ssh {dev} "curl -s localhost:{port}/api"` | jq . — pipe OUTSIDE SSH. `jq` is not available inside containers.

### Iteration loop (when verification fails)

If `zerops_verify` returns degraded/unhealthy, iterate — do NOT skip ahead to stage:

1. **Diagnose**: Read `checks` array from `zerops_verify`. Check `zerops_logs severity="error" since="5m"` + framework log files on mount path FIRST.
2. **Fix**: Edit files at mount path — fix zerops.yaml, app code, or both.
3. **Redeploy** (only if zerops.yaml changed): `zerops_deploy targetService="{devHostname}" setup="dev"`. Code-only fixes just need server restart.
4. **Start server** via SSH. Check startup via `TaskOutput`.
5. **Re-verify**: `zerops_verify` — check status=healthy.

Max 3 iterations. After that, report failure with diagnosis.
</section>

<section name="deploy-dev">
### Dev-only mode — deploy flow

**Prerequisites**: import done, service auto-mounted (provision step), env vars discovered, code written to mount path.

Same lifecycle as standard mode dev phase, but no stage pair. All verification happens on the single dev service.

> **Path distinction:** SSHFS mount path `/var/www/{hostname}/` is LOCAL only.
> Inside the container, code lives at `/var/www/`.

1. **Deploy**: `zerops_deploy targetService="{hostname}" setup="dev"` — self-deploy. **Deploy creates a new container — ALL previous SSH sessions die (exit 255).**
2. **Start server** via **NEW** SSH connection (Bash tool `run_in_background=true`). **Implicit-webserver runtimes: skip** — auto-starts.
3. **Enable subdomain**: `zerops_subdomain serviceHostname="{hostname}" action="enable"` — returns `subdomainUrls`
4. **Verify**: `zerops_verify serviceHostname="{hostname}"` — must return status=healthy
5. **Iterate if needed** — diagnose → fix → redeploy → start server → re-verify. Max 3 iterations.

After dev is verified, present the URL to the user — no stage deploy needed.

### Dev iteration cycle

Same as standard mode: deploy = new container (old SSH dies), start server via new SSH, env vars are OS env vars after deploy. Redeploy only when zerops.yaml changes — code-only changes just need server restart. See standard mode "Dev iteration: manual start cycle" for the full cycle.
</section>

<section name="deploy-simple">
### Simple mode — deploy flow

**Prerequisites**: import done, service auto-mounted (provision step), env vars discovered, code written to mount path.

Simple mode services use a real `start` command and `healthCheck` — server auto-starts after deploy. **No manual SSH start needed** (unlike dev/standard modes).

> **Subdomain activation:** `enableSubdomainAccess: true` in import.yaml pre-configures routing, but **does NOT activate it**. You MUST call `zerops_subdomain action="enable"` after the first deploy. The call is idempotent.

1. **Deploy**: `zerops_deploy targetService="{hostname}" setup="prod"` — self-deploy. Server auto-starts with env vars injected.
2. **Enable subdomain**: `zerops_subdomain serviceHostname="{hostname}" action="enable"` — returns `subdomainUrls`
3. **Verify**: `zerops_verify serviceHostname="{hostname}"` — must return status=healthy
4. **Iterate if needed** — if verification fails: check `zerops_logs severity="error" since="5m"`, fix code on mount path, redeploy, re-verify. Max 3 iterations.

After verification passes, present the URL to the user.
</section>

<section name="deploy-agents">
### For 2+ runtime service pairs — agent orchestration

Prevents context rot by delegating per-service work to specialist agents with fresh context. **Use this for 2 or more runtime service pairs.** For a single service pair, follow the inline flow above.

**Parent agent steps:**
1. `zerops_import content="<import.yaml>"` — create all services (blocks until all processes finish)
2. `zerops_discover` — verify dev services reached RUNNING
3. **Dev services are auto-mounted** when the provision step completes — check `autoMounts` in the response.
4. **Discover ALL env vars**: `zerops_discover includeEnvs=true` — single call returns all services with env var keys. Record exact var names.
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

| Role | Hostname | zerops.yaml setup |
|------|----------|------------------|
| Dev | {devHostname} | `dev` |
| Stage | {stageHostname} | `prod` |

Managed services in this project: {managedServices}

## Discovered Environment Variables

**CRITICAL**: These are the ONLY variables that exist. Use ONLY these in zerops.yaml envVariables.

{envVarSection}

These vars become OS env vars after `zerops_deploy` activates zerops.yaml envVariables.
Your app reads them via the runtime's native env var API. No `.env` files. No dotenv libraries.
**NEVER hardcode credential values** — always use `${hostname_varName}` references in zerops.yaml.

### Mapping in zerops.yaml

```yaml
envVariables:
  # Format: YOUR_APP_VAR: ${discovered_key}
  # Only use variables listed above — anything else will be empty at runtime
```

## Runtime Knowledge

{runtimeKnowledge}

## zerops.yaml Structure

Write the dev setup entry now. Stage entry is generated after dev is verified (task 9).

```yaml
zerops:
  - setup: dev
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
  # Stage entry (setup: prod): generated after dev is verified (task 10)
```

## Infrastructure Verification Server

This is NOT the user's application — it is a bare-minimum server that proves infrastructure works. Under 50 lines of code. No business logic, no UI, no features.

**Environment variables**: see "Discovered Environment Variables" above. Read via runtime's native env var API.

The server MUST expose exactly these endpoints on the port defined in zerops.yaml `run.ports`:

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
| 1 | Write zerops.yaml (dev entry only) | Write to `{mountPath}/zerops.yaml` with dev setup entry. Stage entry comes later (task 9). | File exists with dev setup name |
| 2 | Write app code | HTTP server on the port defined in zerops.yaml `run.ports` with `/`, `/health`, `/status`. Read env vars via runtime's native API. | Code references discovered env vars |
| 3 | Write .gitignore | Build artifacts and IDE files only. Do NOT include `.env` — no .env files exist on Zerops | File exists, no `.env` entry |
| 4 | Deploy dev | `zerops_deploy targetService="{devHostname}" setup="dev"` — activates envVariables as OS env vars | status=DEPLOYED (blocks until complete) |
| 5 | Verify build | Check zerops_deploy return value | Not BUILD_FAILED or timedOut |
| 6 | Start dev server | Start via SSH (Bash tool `run_in_background=true`). Env vars are available after deploy. Skip for implicit-webserver runtimes (php-nginx, php-apache, nginx, static — auto-starts). | `TaskOutput` shows startup message |
| 7 | Activate subdomain | `zerops_subdomain serviceHostname="{devHostname}" action="enable"` | Returns `subdomainUrls` |
| 8 | Verify dev | `zerops_verify serviceHostname="{devHostname}"` | status=healthy |
| 9 | Generate stage entry | Dev is proven — now write the `setup: prod` entry in zerops.yaml. `start:` must be the production run command. `buildCommands` include the full build pipeline. `deployFiles` are the build output (not `[.]`). Add `healthCheck`. Copy `envVariables` from dev. | Stage entry in zerops.yaml |
| 10 | Review stage entry | Is `start:` a production run command? Do `buildCommands` produce what `deployFiles` expects? `healthCheck` present? | Self-check |
| 11 | Deploy stage | `zerops_deploy sourceService="{devHostname}" targetService="{stageHostname}" setup="prod"` — stage has real start command, server auto-starts. | status=DEPLOYED |
| 12 | Verify stage | `zerops_subdomain action="enable"` + `zerops_verify serviceHostname="{stageHostname}"` | status=healthy |
| 13 | Report | Status (pass/fail) + dev URL + stage URL | — |

Tasks 6→7→8 are gated: subdomain activation (7) and verify (8) WILL FAIL if server not started (6).

## Iteration Loop (when verification fails)

If `zerops_verify` returns "degraded" or "unhealthy", iterate — do NOT skip ahead to stage:

**Debugging priority: ALWAYS check application logs first.** Read `zerops_logs` + framework log files on mount path FIRST.

1. **Diagnose**: Read the `checks` array. Check `zerops_logs severity="error" since="5m"`.
2. **Fix**: Edit files at `{mountPath}/` — fix zerops.yaml, app code, or both.
3. **Redeploy** (only if zerops.yaml changed). Code-only fixes just need server restart.
4. **Start server** via SSH. Check startup via `TaskOutput`.
5. **Re-verify**: `zerops_verify` — check status=healthy.

Max 3 iterations. After that, report failure with diagnosis.

## Recovery

| Problem | Fix |
|---------|-----|
| Build FAILED: "command not found" | Fix buildCommands — check runtime knowledge |
| Build FAILED: "module not found" | Add dependency install to buildCommands |
| App crashes: "EADDRINUSE" | Port conflict — check run.ports.port matches app |
| App crashes: "connection refused" to DB | Wrong env var name — compare with discovered vars |
| HTTP 502 after deploy | Call zerops_subdomain action="enable" |
| HTTP 500 from app | Check `zerops_logs` + framework log files. Do NOT start alternative servers. |
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
</section>

<section name="deploy-recovery">
### Recovery and iteration

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| Build FAILED: "command not found" | Wrong buildCommands for runtime | Check recipe, fix build pipeline |
| Build FAILED: "module not found" | Missing dependency init | Add install step to buildCommands |
| App crashes: "EADDRINUSE" | Port conflict | Ensure app listens on correct port from zerops.yaml |
| App crashes: "connection refused" | Wrong DB/cache host | Check envVariables mapping matches discovered vars |
| /status: "db: error" | Missing or wrong env var | Compare zerops.yaml envVariables with discovered var names |
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
- Check 2: Call `zerops_subdomain action="enable"` once after the first deploy of each new service — even if `enableSubdomainAccess` was set in import. The call is idempotent (returns `already_enabled` if already active). Re-deploys do NOT deactivate it. `zerops_discover` shows current status and URL.
- Check 3: `zerops_verify` performs 6 checks for runtime services (service_running, no_error_logs, startup_detected, no_recent_errors, http_health, http_status) and 1 check for managed services (service_running only). The response includes a `checks` array — each entry has `name`, `status` (pass/fail/skip/info), and optional `detail`. Status values: `healthy` (all pass/skip/info), `degraded` (running but some checks fail), `unhealthy` (service not running). Error log checks (no_error_logs, no_recent_errors) return `info` instead of `fail` — they are advisory because SSH deploy logs are often classified as errors.

**Do NOT deploy to stage until dev passes ALL checks.** Stage is for final validation, not debugging.

**Browser verification for web-facing services:**

If the service has `httpSupport: true` on any port (visible in `zerops_discover` output),
spawn a verify agent for deeper visual verification after `zerops_verify` passes:

Agent(model="sonnet", prompt=<Verify Agent Prompt — see develop workflow guidance>)

The verify agent calls zerops_verify (infrastructure baseline), then uses agent-browser
to check if the page actually renders. Its VERDICT determines pass/fail:
- VERDICT: PASS → service verified
- VERDICT: FAIL → enter iteration loop with agent's evidence
- VERDICT: UNCERTAIN → fallback to zerops_verify result (current behavior)

### After completion — next iteration

If the user asks for changes after initial bootstrap:
1. Reuse discovery data — do not re-discover unless services were added/removed.
2. Make the code/config change on the mount path.
3. Deploy to dev first, verify (with iteration loop if needed), then stage. Same dev-first pattern.
4. For config-only changes (env vars), use the configure workflow. For scaling, use `zerops_scale` directly.
</section>

<section name="close">
### Close Bootstrap

**Administrative step** — no checker validation needed. Marks bootstrap as complete and outputs the transition message with next-step guidance.

The transition message includes:
- **Services list** — all provisioned services with modes and dependencies
- **Deploy strategy options** — push-dev, push-git, or manual (strategy selection happens during the deploy or cicd workflows, not here)
- **Router offerings** — ranked workflow suggestions (deploy, cicd, export, and utilities)

**Complete this step:** use `zerops_workflow action="complete" step="close" attestation="Bootstrap finalized, services operational"`.

**Skip this step** only in impossible edge cases (no services at all). Normal projects always reach this step.

Infrastructure is verified — services running with a verification server only. No application code has been written. To implement the user's application, start the develop workflow: `zerops_workflow action="start" workflow="develop"`
</section>

---

## Local Mode Sections

These sections are injected when ZCP detects it's running on the user's local machine (not a Zerops container).

<section name="discover-local">
### Local Mode — Discovery Addendum

**You are running on the user's local machine.** Their machine IS the development server. Do NOT create a dev service on Zerops.

#### Local mode topology

| Mode | What gets created on Zerops | What stays local |
|------|---------------------------|-----------------|
| Standard (default) | `{name}stage` + managed services | Dev server on user's machine |
| Simple | `{name}` + managed services | Push directly to single service |
| Dev / Managed-only | Managed services ONLY | Everything else local |

**Key rule**: No `{name}dev` service on Zerops in local mode. The user's machine replaces the dev service.

**Plan format is unchanged** — still submit `devHostname` in the plan. The engine handles hostname routing internally (creates stage instead of dev in standard mode).

**VPN required**: User needs `zcli vpn up` to access managed services from their machine. Env vars are NOT available via VPN — a `.env` file bridge is generated in the provision step.

If the user only wants managed services (DB, cache, storage) without any runtime service on Zerops, submit an empty plan: `plan=[]`.
</section>

<section name="provision-local">
### Local Mode — Provision Addendum

**Import rules for local mode:**

| Mode | Runtime services in import.yaml | Managed services |
|------|-------------------------------|-----------------|
| Standard | `{name}stage` ONLY (NO `{name}dev`) | Yes — shared, same as container mode |
| Simple | `{name}` (single service) | Yes |
| Dev / Managed-only | NONE — no runtime services | Yes |

**Stage service properties** (standard mode — in import.yaml):
- Do NOT set `startWithoutCode` — stage waits for first deploy (READY_TO_DEPLOY)
- `enableSubdomainAccess: true`
- No `maxContainers: 1` (use defaults)

**No SSHFS mounts** — `zerops_mount` is not available in local mode. Files live on the user's machine.

**After services reach RUNNING:**

1. **Discover env vars**: `zerops_discover includeEnvs=true` — same protocol as container mode (keys only).

2. **Generate .env for local development**: Use `zerops_env action="generate-dotenv" serviceHostname="{appHostname}"` to generate a `.env` file. It reads the service's zerops.yaml envVariables, resolves `${hostname_varName}` references internally against live service data, writes the `.env` file, and returns a summary of resolved variables. The local app reads `.env` directly via dotenv or framework auto-loading.

3. **Add `.env` to `.gitignore`** — it contains secrets. Never commit it.

4. **Guide VPN setup**: Tell the user: `zcli vpn up <projectId>` to access managed services from their machine. All service hostnames (`db`, `cache`, etc.) resolve over VPN. One project at a time — switching disconnects the current.
</section>

<section name="generate-local">
### Generate — Local Mode

Infrastructure verification only — write a hello-world server (/, /health, /status), not the user's application. Under 50 lines of code.

**Write all files locally** in the current working directory. No SSHFS mounts, no remote paths.

#### zerops.yaml

Write zerops.yaml in the project root. Use canonical recipe setup names:

| Mode | `setup:` name | Notes |
|------|------------|-------|
| Standard | `prod` | Stage is the deploy target. User's machine is dev. |
| Simple | `prod` | Single service — production profile (real start, healthCheck). |
| Managed-only | No zerops.yaml needed | No runtime service on Zerops. |

**Standard mode — zerops.yaml for stage service:**
```yaml
zerops:
  - setup: prod
    build:
      base: {runtimeVersion}
      buildCommands: [<from runtime knowledge>]
      deployFiles: [<runtime-specific build output>]
      cache: [<runtime-specific cache dirs>]
    run:
      base: {runtimeBase}
      ports:
        - port: {port}
          httpSupport: true
      envVariables:
        # Map discovered variables to app-expected names
        DATABASE_URL: ${db_connectionString}
      start: {startCommand}   # REAL start command — NOT zsc noop
      healthCheck:
        httpGet:
          port: {port}
          path: /health
```

**Key differences from container mode:**
- Uses real `start:` command (NOT `zsc noop` — that's for container dev iteration only)
- Has `healthCheck` — server auto-starts after deploy, Zerops monitors it
- `deployFiles` can be build output (not forced to `[.]` — stage is NOT self-deploying from a dev container)
- **PHP runtimes (php-nginx, php-apache)**: omit `start:` and `ports:` — web server is built in

**Simple mode** — same as standard stage entry (also uses `setup: prod`).

#### .env file (credential bridge)

The `.env` file was generated in the provision step with actual credential values. Verify it contains all the variables your app needs. If the framework has its own `.env` format (Laravel, Django), adapt the variable names to match.

**IMPORTANT**: `.env` is for LOCAL development only. On Zerops, env vars come from zerops.yaml `envVariables` via `${hostname_varName}` references — resolved at container runtime. Both coexist: `.env` for local, zerops.yaml for Zerops.

**Do NOT delete existing `.env` files** — they are the local credential bridge. If the framework scaffolded one (e.g., `composer create-project`), merge the discovered credentials into it rather than removing it.

#### Env var mapping in zerops.yaml

In zerops.yaml `envVariables`, ONLY use variables discovered in the provision step:
```yaml
envVariables:
  DATABASE_URL: ${db_connectionString}
  REDIS_HOST: ${cache_host}
  REDIS_PORT: ${cache_port}
  # Map ONLY variables listed in the discovery response
```

`${hostname_varName}` references work regardless of push source (local or container) — Zerops resolves them at container runtime. **NEVER hardcode credential values** — always use `${hostname_varName}` references in zerops.yaml and actual values in `.env`.

#### Verification server endpoints

The verification server **MUST** expose exactly these endpoints and nothing else:

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

**Required verification per service type:**
- **PostgreSQL/MariaDB/MySQL**: Execute `SELECT 1` query
- **Valkey/KeyDB**: Execute `PING` command
- **MongoDB**: Run `db.runCommand({ping: 1})`
- **No managed services**: Return `{"service": "{hostname}", "status": "ok"}`

#### Local development

The user manages their own dev server process. ZCP does not start, stop, or manage it.

**Before testing locally, the user needs:**
1. VPN connected: `zcli vpn up <projectId>`
2. `.env` loaded (most frameworks auto-load from project root)
3. Dev server started with their usual command (`php artisan serve`, `npm run dev`, etc.)

Test locally: `curl localhost:{port}/status` — should show connectivity to managed services over VPN.

#### MANDATORY PRE-DEPLOY CHECK

- [ ] zerops.yaml has `setup: prod` entry (canonical recipe name — both standard stage and simple use prod profile)
- [ ] `run.start` is the REAL start command — NOT `zsc noop`
- [ ] `run.ports` port matches what the app listens on — implicit-webserver runtimes: omit ports
- [ ] `healthCheck` configured with correct port and path
- [ ] `envVariables` ONLY uses variables discovered in provision step
- [ ] `.env` exists with actual values for local development
- [ ] `.env` is in `.gitignore`
- [ ] App binds to `0.0.0.0:{port}`, NOT localhost
</section>

<section name="deploy-local">
### Deploy — Local Mode

**Deploy pushes code from your local machine to Zerops** via `zcli push`. No SSH, no SSHFS, no source service concept.

#### Deploy flow

| # | Task | Action | Verify |
|---|------|--------|--------|
| 1 | Deploy | `zerops_deploy targetService="{hostname}" setup="prod"` — pushes local code, triggers build on Zerops | status=DEPLOYED |
| 2 | Enable subdomain | `zerops_subdomain serviceHostname="{hostname}" action="enable"` | Returns subdomainUrls |
| 3 | Verify | `zerops_verify serviceHostname="{hostname}"` | status=healthy |
| 4 | Report | Present Zerops URL + status to user | — |

`zerops_deploy` blocks until the build pipeline completes. Returns DEPLOYED or BUILD_FAILED with buildLogs.

#### Key facts

- **Deploy = new container on Zerops** — only `deployFiles` content persists
- **Local code is unchanged** — edit locally and re-deploy when ready
- **VPN connections survive deploys** — no reconnect needed
- **Server auto-starts on Zerops** (real `start:` command + `healthCheck`) — no manual SSH start needed
- **Subdomain persists** across re-deploys — no need to re-enable after first activation
- **Local dev server continues running** independently — local and Zerops are separate environments

#### Iteration loop (when verification fails)

If `zerops_verify` returns degraded or unhealthy:

1. **Diagnose**: Check `zerops_logs severity="error" since="5m"` and the `checks` array from zerops_verify response
2. **Fix**: Edit code/config locally
3. **Redeploy**: `zerops_deploy targetService="{hostname}" setup="prod"`
4. **Re-verify**: `zerops_verify serviceHostname="{hostname}"`

Max 3 iterations. After that, present diagnosis to user with what was tried and current error state.

#### Recovery patterns

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| Build FAILED: "command not found" | Wrong buildCommands for runtime | Check recipe, fix build pipeline |
| Build FAILED: "module not found" | Missing dependency install | Add install step to buildCommands |
| App crashes on start | Wrong start command or port mismatch | Fix start/ports in zerops.yaml |
| HTTP 502 | Subdomain not enabled | `zerops_subdomain action="enable"` |
| /status connectivity errors | Wrong env var mapping | Compare zerops.yaml envVariables with discovered vars |
| HTTP 500 | App error | Check `zerops_logs` — log tells exact cause |

#### Platform rules

- `${hostname_varName}` typo = silent literal string — platform provides no error
- Build container ≠ run container — different environment
- For new projects: write manifests only (package.json, go.mod). Do NOT write lock files.
- NEVER write dependency dirs (node_modules/, vendor/)
- Internal connections use http://, never https://
- 0.0.0.0 binding: app must listen on 0.0.0.0, not localhost or 127.0.0.1
- **No `.env` files on Zerops** — env vars come from zerops.yaml envVariables. `.env` is for local development only.
</section>
