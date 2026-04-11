# Recipe Workflow

Create a Zerops recipe: a deployable reference implementation with 6 environment tiers and structured documentation.

<section name="research-minimal">
## Research — Recipe Plan

Fill in all research fields by examining the framework's documentation and existing recipes. The `ResearchData` and `RecipeTarget` input schemas on `zerops_workflow` describe every field — read the tool's own schema for the authoritative field list. This section carries only the decisions the schema alone cannot express.

### What type of recipe?

| Type | Slug pattern | Example | Key characteristic |
|------|-------------|---------|-------------------|
| **1. Runtime hello world** | `{runtime}-hello-world` | `go-hello-world` | Raw HTTP + SQL, no framework. Simplest possible app. |
| **2a. Frontend static** | `{framework}-hello-world` | `react-hello-world` | Builds to HTML/CSS/JS, `run.base: static`. No DB. |
| **2b. Frontend SSR** | `{framework}-hello-world` | `nextjs-hello-world` | SSR framework (Next.js, Nuxt, etc.) with DB. |
| **3. Backend framework** | `{framework}-minimal` | `laravel-minimal` | Framework with ORM, migrations, templates. |
| **4. Showcase** | `{framework}-showcase` | `laravel-showcase` | Full dashboard, 5+ feature sections, worker, all services. |

**Scaffold preservation (mandatory).** Preserve what the framework's official scaffold emits (`composer create-project laravel/laravel`, `npx create-next-app`, `rails new`, `django-admin startproject`). "Minimal" in the recipe slug refers to **external services** (no Redis, no S3, DB-only), NOT to the framework's feature surface. Stripping Vite / Tailwind / ESM from a Laravel or Next.js scaffold makes the recipe non-representative — a user running the same scaffold locally will have those files and will expect them to deploy. Keep them.

### Targets

Define workspace services based on recipe type:
- **Type 1 (runtime hello world)**: app + db
- **Type 2a (frontend static)**: app only (NO database)
- **Type 2b (frontend SSR)**: app + db
- **Type 3 (backend framework)**: app + db

**Target fields** — see the `RecipeTarget` input schema on `zerops_workflow` for field-level descriptions (`hostname`, `type`, `isWorker`, `role`, `sharesCodebaseWith`). The decisions you make while filling targets:

- **Hostname** — lowercase alphanumeric only. Use conventional names (`app`, `db`, `cache`, `queue`, `search`, `storage`).
- **Type** — pick the **highest available version** from `availableStacks` for each stack.
- **isWorker: true** — set for background/queue workers (no HTTP). Ignored for managed/utility services.
- **role** — `app` / `api` for dual-runtime repo routing. Empty for managed services.
- **sharesCodebaseWith** — worker-only; see the Worker codebase decision block in the showcase research section. Minimal recipes have no worker.

### Submission
Submit via:
```
zerops_workflow action="complete" step="research" recipePlan={...}
```
</section>

<section name="research-showcase">
## Research — Showcase Recipe (Type 4)

The base recipe-type table and scaffold preservation rule from the research-minimal block apply here too. This section adds the showcase-only fields and the two showcase-specific decisions: full-stack vs API-first classification, and the worker codebase decision.

**Reference loading — load ONE recipe only:**
```
zerops_knowledge recipe="{framework}-minimal"
```
This is your direct predecessor and starting point. **Do NOT load the hello-world recipe.** The generate step automatically injects earlier ancestors' gotchas (hello-world tier) into your context — loading it manually wastes context with raw SQL patterns and different base images that don't apply to your framework. If you load it anyway, ignore its zerops.yaml patterns entirely; use only the minimal recipe's patterns as your template.

### Additional Showcase Fields

Five showcase-only fields on the research plan: `cacheLib`, `sessionDriver`, `queueDriver`, `storageDriver`, `searchLib`. Each is the library the framework uses for that feature — pick whatever is idiomatic for the framework. The `queueDriver` value is the client library the framework uses to talk to the NATS broker (the showcase provisions NATS as the messaging layer regardless of what the framework's own queue library polls).

### Full-Stack vs API-First Classification

Before defining showcase targets, classify the framework:

**Full-stack** (has built-in view/template engine): The framework renders HTML directly. Dashboard uses the built-in engine. Single `app` service.
Examples: Laravel/Blade, Rails/ERB, Django/Jinja2, Phoenix/HEEx.

**API-first** (no built-in templating): The framework serves JSON. Dashboard is a lightweight Svelte SPA in a separate `app` service that calls the API. The API is a separate `api` service. Worker shares codebase with `api`.

Classification rule: if the predecessor hello-world/minimal recipe renders HTML via a framework-integrated template engine, it is full-stack. If the predecessor only returns JSON or plain text, it is API-first.

### Showcase Targets
Define workspace services for showcase recipe. All targets appear in all 6 environment tiers (the finalize step handles per-env scaling and mode differences):

**Full-stack showcase targets:**
- **app**: runtime service — HTTP-serving primary application
- **worker**: background job processor (`isWorker: true`) — consumes from a broker, no HTTP. See "Worker codebase decision" below for `sharesCodebaseWith`.
- **db**: primary database
- **redis**: cache + sessions (Valkey or KeyDB — **NOT** queues; the broker below is the queue)
- **queue**: NATS broker (`type: nats@2.12`) — the messaging/queue layer the worker consumes from. Hostname is literally `queue` so env var references (`${queue_hostname}`, `${queue_port}`, `${queue_user}`, `${queue_password}`) read clean in the app and worker configs.
- **storage**: S3-compatible object storage
- **search**: search engine (Meilisearch, Elasticsearch, or Typesense)

**API-first showcase targets** (dual-runtime):
- **app**: lightweight static frontend — Svelte SPA (`role: "app"`, `type: static`)
- **api**: JSON API backend — the showcased framework (`role: "api"`)
- **worker**: background job processor (`isWorker: true`). See "Worker codebase decision" below — default is SEPARATE codebase (own repo).
- **db**: primary database
- **redis**: cache + sessions (Valkey or KeyDB — **NOT** queues)
- **queue**: NATS broker (`type: nats@2.12`) — same as full-stack, the dedicated messaging layer
- **storage**: S3-compatible object storage
- **search**: search engine (Meilisearch, Elasticsearch, or Typesense)

### Worker codebase decision

Every showcase has a worker. The worker is always a separate **service**; whether it's a separate **codebase** is a research-step decision on the target via `sharesCodebaseWith`.

**SEPARATE codebase (default)** — leave `sharesCodebaseWith` empty. Worker has its own repo, its own `zerops.yaml`, its own dev+stage pair. This is the normal shape for API-first showcases and any worker consuming from a standalone broker.

**SHARED codebase (opt-in)** — set `sharesCodebaseWith: "{host hostname}"`. One repo, two process entry points in one `zerops.yaml` (the host target's zerops.yaml gets a third `setup: worker` block).

Choose SHARED **only when ALL three tests pass**:

1. **The worker command is the framework's own bundled CLI**, not a generic library call. CLIs that ship with the framework and exist to run the framework's bootstrapped process. Custom entry points (`{packageManager} start worker.{ext}`, `{runtime} worker.{ext}`, any script you had to write) do NOT qualify.
2. **No independent dependency manifest.** Separate `package.json` / `composer.json` / `pyproject.toml` / `go.mod` / `Cargo.toml` disqualifies SHARED.
3. **Cannot run without the app's bootstrap.** Job logic references app-level models, ORM bindings, or framework services that need the framework's config graph.

**When in doubt, SEPARATE.** Generic queue libraries (BullMQ, agenda, etc.) fail test 1 and land on SEPARATE. Cross-runtime sharing is rejected by validation. The 3-repo case (frontend + API + worker, all separate repos, worker and API on the same runtime base) is fully supported — leave `sharesCodebaseWith` empty.

Provision and generate will use this decision to shape the import.yaml, the zerops.yaml files, and the deploy flow. You don't need to think about the mechanics now — just make the decision.

### Submission
Submit via:
```
zerops_workflow action="complete" step="research" recipePlan={...}
```
</section>

<section name="provision">
## Provision — Create Workspace Services

Create all workspace services from the recipe plan. This follows the same pattern as bootstrap — dev/stage pairs for the app runtime, with shared managed services.

### 1. Generate import.yaml

Recipes always use **standard mode**: each runtime gets a `{name}dev` + `{name}stage` pair. **Exception**: a worker target whose `sharesCodebaseWith` is set (shared-codebase worker — the research-step decision in the previous section) gets only `{name}stage`. The host target's dev container runs both processes via SSH. No `workerdev` — it would be a zombie container running the same code with no worker process started. Separate-codebase workers (`sharesCodebaseWith` empty — the default, including the 3-repo case where runtime matches but the repo does not) get their own dev+stage pair from their own `{slug}-worker` repo.

**Dev vs stage properties:**

| Property | Dev (`appdev`) | Stage (`appstage`) |
|----------|---------------|-------------------|
| `startWithoutCode` | `true` | omit |
| `maxContainers` | `1` | omit (default) |
| `enableSubdomainAccess` | `true` | `true` |
| `verticalAutoscaling.minRam` | `1.0` for compiled runtimes (compilation needs RAM) | omit (default) |

**DO NOT add `zeropsSetup` or `buildFromGit` to the workspace import.** These fields require each other — `zeropsSetup` without `buildFromGit` causes API errors. The workspace deploys via `zerops_deploy` with the `setup` parameter instead.

Dev starts immediately with an empty container (RUNNING). Stage stays in READY_TO_DEPLOY until first deploy from dev.

**Static frontends (type 2a):** `run.base: static` serves via built-in Nginx — both dev and stage use `type: static`. Dev still gets `startWithoutCode: true` for the build container. The runtime for building is `nodejs@22` (or similar) as `build.base` in zerops.yaml, NOT as the service type.

**If the plan has NO database** (type 2a static frontend): the import.yaml only contains the app dev/stage pair.

**Workspace import MUST NOT contain a `project:` section.** The ZCP project already exists — the API rejects imports that include `project:`. Only `services:` is allowed here. (The 6 recipe **deliverable** imports written in the finalize step DO contain `project:` with `envVariables` + preprocessor — that's a different file for a different use case.)

**Framework secrets**: if `needsAppSecret == true`, decide where the secret lives.

- **Shared** (encryption keys, CSRF secrets, session signing keys — anything multiple services must agree on): set at project level after provision completes:
  ```
  zerops_env project=true action=set variables=["{SECRET_KEY_NAME}=<@generateRandomString(<32>)>"]
  ```
  Do NOT wrap the preprocessor expression in `base64:` / `hex:` — `zerops_env` rejects those shapes because frameworks accept the raw 32-char output directly. If your framework's docs show a `base64:` prefix on the secret, drop it. `zerops_env set` is upsert and auto-restarts affected services so the new value takes effect.

- **Per-service** (unique API tokens, webhook secrets): add to `envSecrets` in the import.yaml under that service.

For correlated secrets, encoded variants, or key pairs, call `zerops_preprocess` directly.

**Dual-runtime URL constants** (API-first recipes only — skip for single-runtime): after services reach RUNNING, set project-level `DEV_*` + `STAGE_*` URL constants with `zerops_env project=true action=set` so the generate step can reference them in zerops.yaml. The full format, consumption pattern, and the `projectEnvVariables` handoff to finalize are documented in the generate step under "Dual-runtime URL env-var pattern" — set the same values now as will be passed there.

Follow the injected **import.yaml Schema** and **Provision Rules** for field rules (hostname conventions, priority, mode, env var levels, preprocessor syntax). Recipe-specific validation:

| Check | What to verify |
|-------|---------------|
| NO zeropsSetup | Workspace import must NOT include zeropsSetup (requires buildFromGit) |
| envSecrets | Per-service on app/worker, NOT at project level |
| Service types | Match available stacks from research |

### 2. Import services

```
zerops_import content="..."
```

Wait for all services to reach RUNNING.

### 3. Mount dev filesystem

Mount the dev service for direct file access:
```
zerops_mount action="mount" serviceHostname="appdev"
```

This gives SSHFS access to `/var/www/appdev/` — all code writes go here.

### 3a. Configure git on the mount (MANDATORY before first commit)

SSHFS mounts surface the container's `/var/www/` to zcp as a root-owned directory. Git treats this as a security risk and refuses to operate on it — both on zcp (where you edit files) and inside the target container (where `zerops_deploy` runs `git push` on first deploy). You must configure both sides BEFORE any commit.

**On zcp (once per mounted service)**:
```
git config --global --add safe.directory /var/www/{hostname}
git config --global user.email "recipe@zerops.io"
git config --global user.name "Zerops Recipe"
```

**On the target container (once per service, before first `zerops_deploy`)**:
```
ssh {hostname} "git config --global --add safe.directory /var/www && git config --global user.email 'recipe@zerops.io' && git config --global user.name 'Zerops Recipe'"
```

Without zcp-side config: `git commit` on the mount fails with `fatal: detected dubious ownership`. Without container-side config: `zerops_deploy` fails with `fatal: not in a git directory`. Both errors are 100% reproducing on first use — do not try to "commit without configuring and see what happens".

For a dual-runtime showcase with 3 codebases (apidev, appdev, workerdev), repeat both commands for each mount.

### 4. Discover env vars (mandatory before generate — skip if no managed services)

Run `zerops_discover includeEnvs=true` AFTER services reach RUNNING. The response contains the real env var keys every managed service exposes. **You MUST use the names from this response, not guess them from training data.** Guessed names (`${search_apiKey}` when the real key is `${search_masterKey}`) fail silently — the platform interpolator treats unknown cross-service refs as literal strings, and your app sees `"${search_apiKey}"` as the value at runtime.

**Catalog the output.** Record the list of env var keys for every managed service in the provision-step attestation so the generate step (which writes the zerops.yaml `run.envVariables` using these references) has the authoritative list. Example attestation shape:

```
Services: apidev, apistage, appdev, appstage, workerdev, workerstage, db, redis, queue, storage, search.
Env var catalog:
  db: hostname, port, user, password, dbName, connectionString
  redis: hostname, port, password, connectionString
  queue: hostname, port, user, password, connectionString
  storage: apiHost, apiUrl, accessKeyId, secretAccessKey, bucketName
  search: hostname, port, masterKey, defaultAdminKey, defaultSearchKey
Dev mounts: apidev, appdev, workerdev
```

If a managed service returns a set that surprises you (no `hostname`, or a `key` name you don't recognize), STOP and investigate — do not proceed with guessed names.

**If the plan has no managed services** (type 2a static frontend): skip this step entirely.

### Completion
```
zerops_workflow action="complete" step="provision" attestation="Services created: {list}. Env vars cataloged for zerops.yaml wiring (not yet active as OS vars — activate after deploy): {list}. Dev mounted at /var/www/appdev/"
```
</section>

<section name="generate">
## Generate — App Code & Configuration

<block name="container-state">

### Container state during generate

The dev service is RUNNING (via `startWithoutCode`) but zerops.yaml has NOT been deployed yet.

| Available | NOT available (activates after `zerops_deploy`) |
|-----------|------------------------------------------------|
| Base image tools (runtime + package manager) | Secondary build bases (added in `buildCommands`) |
| Platform vars (hostname, serviceId) | `run.envVariables` (cross-service references) |
| SSHFS file access to `/var/www/` | Managed-service connectivity |
| Implicit webservers auto-serve from mount | Correct app configuration |

**Only scaffold commands are safe via SSH** — project creation, `git init`, file operations. These use the base image and need no env vars.

**Do NOT run any command that bootstraps the framework** — no migrations, no cache warming, no health checks, no CLI tools that attempt service connections. They WILL fail because `run.envVariables` do not exist as OS env vars yet.

**Connection errors during generate are expected, not code bugs.** If a command fails with "connection refused", "driver not found", or similar: do NOT fix code, do NOT create .env files, do NOT change drivers or hardcode credentials. Continue writing files. The deploy step activates env vars.

</block>

<block name="where-to-write-files-single">

### WHERE to write files

**Single-runtime** (full-stack): Write all source code, zerops.yaml, and README to `/var/www/appdev/`.

**Use SSHFS for file operations**, SSH for commands that use the **base image's built-in tools** (e.g., `composer create-project` on php-nginx, `git init`).
Files placed on the mount are already on the dev container — deploy doesn't "send" them, it triggers a build from what's already there.

</block>

<block name="where-to-write-files-multi">

**Dual-runtime** (API-first showcase): write each codebase to its own mount. For a 3-repo showcase (frontend + API + separate worker), that's three distinct source trees:

- `/var/www/apidev/` — the API framework project (NestJS, Django, Rails, etc.)
- `/var/www/appdev/` — the frontend SPA (Svelte, React, Vue, etc.)
- `/var/www/workerdev/` — the worker project (may be a separate framework project or a minimal runtime script)

**Each codebase needs its own README.md with all 3 extract fragments** (intro, integration-guide, knowledge-base). At publish time, each codebase is part of the recipe app repo, and the README you write is what a user exploring that codebase sees. The integration-guide fragment in each README contains THAT codebase's zerops.yaml, fully commented. The knowledge-base fragment in each README lists the gotchas specific to THAT codebase's role (e.g., the frontend README covers allowedHosts and dev-server runtime env vars; the API README covers CORS and ORM synchronize; the worker README covers NATS connection and job idempotency).

**Scaffold each codebase in its own mount — never cross-contaminate.** Framework scaffolders (`sv create`, `npx create-vite`, `nest new`, `composer create-project`, `django-admin startproject`) write config files (`tsconfig.json`, `package.json`, `.npmrc`, `.vscode/`, `.gitignore`) into whatever directory they run from. Running a scaffold from the wrong container or the wrong working directory overwrites the host codebase's config silently. For dual-runtime:
- `cd /var/www/apidev && nest new .` for the API — runs on the `apidev` service's SSH session
- `cd /var/www/appdev && npm create vite@latest . -- --template svelte` for the frontend — runs on the `appdev` service's SSH session (if the static container lacks Node, scaffold files directly via SSHFS write instead of invoking a scaffolder on the container)

Never scaffold into `/tmp` and copy — the scaffolder's footprint always includes hidden files you'll miss. Never run a frontend scaffolder from an API SSH session targeting the API mount — `sv create` invoked from `apidev` SSH will overwrite apidev's `tsconfig.json` and `package.json` even if you `cd` to a different directory first, because scaffolders trust the process working directory as the project root.

</block>

<block name="what-to-generate-showcase">

### What to generate per recipe type

**Type 1 (runtime hello world):** Raw HTTP server with a single file. DB connection via standard library. Raw SQL migration for a `greetings` table. No framework, no ORM.

**Type 2a (frontend static):** SPA/static site. Framework project (React/Vue/Svelte) with a simple page showing framework name, greeting, and environment indicator. Build-time env var injection. No DB connection.

**Type 2b (frontend SSR):** SSR framework project (Next.js/Nuxt/SvelteKit). Server-rendered pages with DB connection. Framework's API routes for health endpoint.

**Type 3 (backend framework):** Full framework project. ORM-based migrations, template-rendered dashboard, framework CLI tools. Uses the framework's conventions throughout.

**Type 4 (showcase):** Dashboard **SKELETON only** — feature controllers and views are **NOT** written during generate. Generate produces: layout with empty/placeholder partial slots (using the framework's standard include mechanism — partials, components, sub-templates, or imports) for each planned feature section, all routes (display + action endpoints pre-registered but returning placeholder responses), primary model + migration + factory + seeder with sample data, service connectivity panel, zerops.yaml (all 3 setups: dev + prod + worker), README with fragments, .env.example. **Stop here.** The deploy step dispatches a sub-agent to implement feature controllers and views against live services after appdev is verified. Writing feature code during generate means generating blind against disconnected services — producing code with no error handling, no XSS protection, and untested integrations. See "Showcase dashboard — file architecture" below.

</block>

<block name="two-kinds-of-import-yaml">

### Two kinds of import.yaml (critical distinction)

1. **Workspace import** (provision step) — creates the agent's dev/stage infrastructure. NO `zeropsSetup`, NO `buildFromGit`. Services use `startWithoutCode` (dev) or wait for deploy (stage).
2. **Recipe import** (finalize step) — the 6 deliverable files for end users. Uses `zeropsSetup: dev`/`zeropsSetup: prod` + `buildFromGit` to map hostnames to setup names.

zerops.yaml ALWAYS uses **generic setup names**: `setup: dev` and `setup: prod`. During workspace deploy, the `zerops_deploy` tool's `setup` parameter maps the service hostname to the correct setup name (e.g. `targetService="appdev" setup="dev"`). In recipe import.yaml, `zeropsSetup: dev`/`zeropsSetup: prod` does the same mapping for `buildFromGit` deploys.

</block>

<block name="execution-order">

### Execution order — no sub-agents for zerops.yaml or README

**Write zerops.yaml and README yourself (the main agent), sequentially.** Do NOT delegate them to sub-agents. Sub-agents lose the injected guidance (discovered env vars, zerops.yaml schema, comment ratio rules, prepareCommands constraints) and produce wrong output — showcase v1-v4 all failed on sub-agent-written zerops.yaml (wrong prepareCommands, 15% comment ratio, missing env vars) or README (incomplete intro, divergent zerops.yaml copy).

**Correct order:**
1. Scaffold the project (composer create-project, npx create-next-app, etc.)
2. Write zerops.yaml — YOU, not a sub-agent. Use the discovered env vars and schema from this guidance.
3. Write app code:
   - **Types 1-3 (minimal)**: dashboard skeleton with feature sections, model + migration + seeder, routes, config changes. Write everything yourself — with only 1-2 feature sections (database CRUD, maybe cache) there's no benefit to sub-agents.
   - **Type 4 (showcase)**: write the dashboard skeleton yourself (layout with include slots, connectivity panel, model + migration + seeder, all routes). Do NOT dispatch the feature sub-agent yet — that happens in the deploy step after appdev is deployed and verified. See "Showcase dashboard — file architecture" below.
4. Write README with extract fragments — YOU, not a sub-agent. The integration-guide fragment must contain the SAME zerops.yaml you just wrote in step 2 (read it back from disk, don't rewrite from memory). The intro must list ALL services from the plan, not just the database.
5. Git init + commit

**Why this order matters:** zerops.yaml is the single source of truth. The README's integration-guide copies it verbatim. If two sub-agents write them independently, they diverge. If a sub-agent writes zerops.yaml without the injected guidance, it misses rules that only exist in this step's DetailedGuide.

</block>

<block name="zerops-yaml-header">

### zerops.yaml — Write ALL setups at once

Write the complete zerops.yaml with ALL setup entries in a single file. Minimal recipes have TWO setups (`dev` + `prod`). Showcase recipes with a **shared-codebase** worker (`sharesCodebaseWith` set — see research-showcase section) have THREE setups in the host target's zerops.yaml: `dev` + `prod` + `worker`. Showcase recipes with a **separate-codebase** worker (the default) have TWO setups per zerops.yaml (one for the app, one for the worker, each in its own repo). The same file is the source of truth for the deploy step AND for the README integration-guide fragment — writing it once eliminates drift between what deploys and what the README documents. The deploy step will verify dev against the live service, then cross-deploy the already-written prod (and worker, if shared) configs to stage.

**Per-codebase zerops.yaml** (showcase). The number of zerops.yaml files and their setup shape is driven by `sharesCodebaseWith`:

- **Dual-runtime + shared worker** (`worker.sharesCodebaseWith == "api"`):
  - `/var/www/apidev/zerops.yaml` — 3 setups: `dev`, `prod`, `worker` (API + the shared-codebase worker running from the same Node/PHP/Ruby project as the API)
  - `/var/www/appdev/zerops.yaml` — 2 setups: `dev`, `prod` (static frontend only)
- **Dual-runtime + separate worker** (3-repo case, `worker.sharesCodebaseWith == ""`):
  - `/var/www/apidev/zerops.yaml` — 2 setups: `dev`, `prod`
  - `/var/www/appdev/zerops.yaml` — 2 setups: `dev`, `prod`
  - `/var/www/workerdev/zerops.yaml` — 2 setups: `dev`, `prod` (own repo, own dependency set)
- **Single-app + shared worker** (Laravel/Rails/Django idiom, `worker.sharesCodebaseWith == "app"`):
  - `/var/www/appdev/zerops.yaml` — 3 setups: `dev`, `prod`, `worker`
- **Single-app + separate worker**:
  - `/var/www/appdev/zerops.yaml` — 2 setups: `dev`, `prod`
  - `/var/www/workerdev/zerops.yaml` — 2 setups: `dev`, `prod`

</block>

<block name="dual-runtime-url-shapes">

#### Dual-runtime URL env-var pattern — the canonical solution

Every service has a deterministic public URL derived from its `${hostname}`, the project-scope `${zeropsSubdomainHost}` env var (platform-generated, everywhere, always), and its HTTP port (omitted for static services). URL format is a platform constant:

```
https://{hostname}-${zeropsSubdomainHost}-{port}.prg1.zerops.app   # dynamic runtime on port N
https://{hostname}-${zeropsSubdomainHost}.prg1.zerops.app          # static (Nginx, no port segment)
```

Dual-runtime recipes use two env var name families. `STAGE_{ROLE}_URL` is present in **every env** (0-5) and resolves to `{role}stage` in env 0-1 and the bare `{role}` in envs 2-5. `DEV_{ROLE}_URL` exists **only in env 0-1** (dev-pair envs) and resolves to `{role}dev`. Typical roles: `API`, `FRONTEND`. Add `WORKER` only if the worker has a public surface (usually it doesn't).

**Env 0-1 shape** (dev-pair envs — `STAGE_*` + `DEV_*`):
```yaml
# in import.yaml for env 0 and env 1
project:
  envVariables:
    DEV_API_URL: https://apidev-${zeropsSubdomainHost}-{apiPort}.prg1.zerops.app
    DEV_FRONTEND_URL: https://appdev-${zeropsSubdomainHost}.prg1.zerops.app
    STAGE_API_URL: https://apistage-${zeropsSubdomainHost}-{apiPort}.prg1.zerops.app
    STAGE_FRONTEND_URL: https://appstage-${zeropsSubdomainHost}.prg1.zerops.app
```

**Envs 2-5 shape** (single-slot envs — `STAGE_*` only):
```yaml
# in import.yaml for envs 2, 3, 4, 5
project:
  envVariables:
    STAGE_API_URL: https://api-${zeropsSubdomainHost}-{apiPort}.prg1.zerops.app
    STAGE_FRONTEND_URL: https://app-${zeropsSubdomainHost}.prg1.zerops.app
```

Substitute `{apiPort}` with your API's actual HTTP port (from `run.ports[0].port` in the API's zerops.yaml). Static frontends have no port segment.

</block>

<block name="dual-runtime-consumption">

**Dev-server runtime env vars — `setup: dev` needs `run.envVariables`**:

Framework-bundled dev servers (Vite, webpack dev server, Next dev, Nuxt dev) read `process.env.VITE_*` / `process.env.NEXT_PUBLIC_*` / equivalent **at dev server startup**, not at build time. For `setup: dev`, the client-side env vars must be in `run.envVariables` — or they must be passed on the start command line (`VITE_API_URL=$DEV_API_URL npm run dev`). The `build.envVariables` placement is ONLY correct for `setup: prod` because prod builds bake the values into the bundle via a build step that doesn't exist in dev mode.

```yaml
zerops:
  - setup: dev
    run:
      base: nodejs@22
      envVariables:
        # Client-side vars must be in run.envVariables so the Vite/webpack/
        # Next dev server picks them up at startup. build.envVariables is
        # build-time only and dev servers don't have a build step.
        VITE_API_URL: ${DEV_API_URL}
        NODE_ENV: development

  - setup: prod
    build:
      base: nodejs@22
      envVariables:
        # Client-side vars in build.envVariables get baked into the bundle.
        # This is the prod pattern — `npm run build` substitutes at build time.
        VITE_API_URL: ${STAGE_API_URL}
    run:
      base: static
```

Symptom of the wrong placement: the frontend loads in the browser but every `fetch()` call returns HTML (the Vite dev server's own 404 page) instead of JSON. In the browser devtools, `console.log(import.meta.env.VITE_API_URL)` prints `undefined`. This is LOG2's session-breaking bug 15.

**Consumption**: project-level env vars auto-inject into both runtime AND build containers. Reference them directly by name in zerops.yaml — `build.envVariables: VITE_API_URL: ${STAGE_API_URL}` bakes the stage URL into the cross-deployed bundle; `run.envVariables: FRONTEND_URL: ${STAGE_FRONTEND_URL}` forwards the value under a framework-conventional name for CORS. There is **no `RUNTIME_` prefix** on project vars — that prefix is a different feature (lifting a service-level runtime var into build), not applicable here. The full consumption model (including the shell-prefix alternative in `buildCommands`) lives in the `environment-variables` knowledge guide — fetch it via `zerops_knowledge scope="guide" query="environment-variables"` when you need the platform rules behind this pattern.

The `setup: dev` block reads `DEV_*`; `setup: prod` reads `STAGE_*`. The same zerops.yaml works in every env: envs 2-5 never invoke `setup: dev` (there is no `appdev` there), so the `DEV_*` reference is dormant and safe.

**Workspace parity is set at the provision step**, not here — see the provision step's `zerops_env project=true action=set` invocation. By the time you reach generate, the workspace already has `DEV_*` + `STAGE_*` resolved. Single-runtime recipes skip this entirely — they don't cross services for URL baking.

</block>

<block name="project-env-vars-pointer">

**For the 6 deliverable import.yaml files** (generated at finalize): pass `projectEnvVariables` as a first-class input to `zerops_workflow action="generate-finalize"` so the template re-renders the env 0-1 shape for envs 0-1 and the envs 2-5 shape for envs 2-5:

```
zerops_workflow action="generate-finalize" \
  projectEnvVariables={
    "0": {
      "DEV_API_URL": "https://apidev-${zeropsSubdomainHost}-{apiPort}.prg1.zerops.app",
      "DEV_FRONTEND_URL": "https://appdev-${zeropsSubdomainHost}.prg1.zerops.app",
      "STAGE_API_URL": "https://apistage-${zeropsSubdomainHost}-{apiPort}.prg1.zerops.app",
      "STAGE_FRONTEND_URL": "https://appstage-${zeropsSubdomainHost}.prg1.zerops.app"
    },
    "1": { /* identical to env 0 */ },
    "2": {
      "STAGE_API_URL": "https://api-${zeropsSubdomainHost}-{apiPort}.prg1.zerops.app",
      "STAGE_FRONTEND_URL": "https://app-${zeropsSubdomainHost}.prg1.zerops.app"
    },
    "3": { /* identical to env 2 */ },
    "4": { /* identical to env 2 */ },
    "5": { /* identical to env 2 */ }
  } \
  envComments={...}
```

Do NOT hand-edit the 6 generated files to add `project.envVariables` after the fact. A second `generate-finalize` call re-renders from template and wipes manual edits. Always pass `projectEnvVariables` via the tool input; it's idempotent across reruns.

</block>

<block name="dual-runtime-what-not-to-do">

**What NOT to do**:
- Do NOT invent a `setup: stage` — there is no such thing. Stage uses `setup: prod`.
- Do NOT reference another service's `${hostname}_zeropsSubdomain` to build URLs. Use the `${zeropsSubdomainHost}` project-scope var and the constant URL format above.
- Do NOT create a service-level env var with the same name as a project-level env var — that's a shadow loop (the platform interpolator sees the same-name service var first and resolves to the literal `${VAR_NAME}` string). Forward under a DIFFERENT name (e.g. `FRONTEND_URL: ${STAGE_FRONTEND_URL}`); if you want the project var under its own name, just don't write the line — it's already in the OS env.

</block>

<block name="setup-dev-rules">

Follow the injected chain recipe (working zerops.yaml from the predecessor) as the primary reference. For hello-world (no predecessor), follow the injected zerops.yaml Schema. Platform rules (lifecycle phases, deploy semantics) were taught at provision — use `zerops_knowledge` if you need to look up a specific rule.

Recipe-specific conventions for each setup (platform rules from provision apply — these are ONLY the recipe-specific additions):

**`setup: dev`** (self-deploy from SSHFS mount — agent iterates here):
- **`setup: dev` MUST give the agent a container that can host the framework's dev toolchain** — shell, package manager, and the framework's hot-reload process (`npm run dev`, `php artisan serve`, `bun --hot`, `cargo watch`, etc.). This is what makes the dev setup iterable over SSH.
- **Dynamic runtimes** (nodejs, python, php-nginx, go, rust, bun, ubuntu, …): `run.base` is the same as prod and `deployFiles: [.]` preserves source across deploys — **MANDATORY**, anything else destroys the source tree.
- `start: zsc noop --silent` — exception: omit `start` for implicit-webserver runtimes (php-nginx, php-apache, nginx, static)
- **NO healthCheck, NO readinessCheck** — agent controls lifecycle; checks would restart the container during iteration
- Framework mode flags set to dev values (`APP_ENV: local`, `NODE_ENV: development`, `DEBUG: "true"`, verbose logging)
- Same cross-service refs from `zerops_discover` as prod — only mode flags differ

</block>

<block name="serve-only-dev-override">

- **Serve-only runtimes** (`static`, standalone `nginx`, any future serve-only base): these host no toolchain — `run.base: static` is a **prod-only concern**. For the dev setup, pick a different `run.base` that CAN host the framework's dev toolchain — typically the same base that already exists under `build.base` for that setup (e.g. `nodejs@22` for a Vite/Svelte SPA whose prod is `static`). `run.base` may differ between setups inside the same zerops.yaml; the platform supports this and it's the intended pattern for serve-only prod artifacts. `deployFiles: [.]` still applies on dev regardless of `run.base` choice.

</block>

<block name="dev-dep-preinstall">

- **Dev dependency pre-install**: if the build base includes a secondary runtime for an asset pipeline, dev `buildCommands` MUST include the dependency install step for that runtime's package manager. This ensures the dev container ships with dependencies pre-populated — the developer (or agent) can SSH in and immediately run the dev server without a manual install step first. Omit the asset compilation step — that's for prod only; dev uses the live dev server.

</block>

<block name="dev-server-host-check">

- **Dev-server host-check allow-list** — when the framework's dev server enforces an HTTP Host-header allow-list (most modern bundler-based dev servers do), the Zerops public dev subdomain must be on the allow-list or the dev server returns a "Blocked request / Invalid Host header" error to the browser. This is a framework-config concern, not a Zerops platform setting: the right key lives in the framework's dev-server config and has a different name per framework (e.g. one framework calls it `allowedHosts`, another `allowed-hosts`, another `disable-host-check`, etc.). **During research, look up the current host-check config for the framework's dev server in its official docs** and bake the correct setting into whichever config file the dev server reads (`vite.config.ts`, `webpack.config.js`, `angular.json`, `next.config.js`, etc.). Add `.zerops.app` as a wildcard suffix so both the `{hostname}dev-{subdomainHost}-{port}.prg1.zerops.app` URL and the (later) `{hostname}stage-{subdomainHost}.prg1.zerops.app` URL are accepted without per-URL churn. If the dev server has a separate "preview" mode with its own host-check (some Vite-family servers do), configure both. The symptom of a missed allow-list is a 403 or plain-text "Blocked request" response to the public dev subdomain with no HTML rendered.

</block>

<block name="setup-prod-rules">

**`setup: prod`** (cross-deployed from dev to stage — end-user production target):
- Follow the chain recipe's prod setup as a baseline. Adapt to the current recipe's services and framework version.
- **If a search engine is provisioned**: `initCommands` must include the framework's search-index import command AFTER `db:seed`. The ORM's auto-index-on-create may work during seeding, but an explicit import is the safety net — if the seeder guard skips creation (records exist from a prior deploy) while the search index is empty, auto-indexing fires zero events and search returns nothing.
- **NO `prepareCommands` installing secondary runtimes** unless the prod START command needs them at runtime (e.g., SSR with Node). If the secondary runtime is only for BUILD, it's in `build.base` — adding it to `run.prepareCommands` wastes 30s+ on every container start. Dev needs `prepareCommands` for the dev server; prod does not.
- Framework mode flags set to prod values. Same cross-service ref keys as dev — **only mode flags differ**.

</block>

<block name="worker-setup-block">

**`setup: worker`** (showcase only — background job processor). Whether the worker shares the app's codebase is the research-step decision declared via `sharesCodebaseWith`. Two shapes:

- **Shared codebase** (`sharesCodebaseWith` = host target's hostname): write a `setup: worker` block in the SAME zerops.yaml as the host target. No `workerdev` service — the agent starts both web server and queue consumer as SSH processes from the host target's dev container.
- **Separate codebase** (`sharesCodebaseWith` empty — DEFAULT): worker has its own repo with its own zerops.yaml (`dev` + `prod`). Mount path `/var/www/workerdev/`. Covers the 3-repo case.

Worker rules: `start` mandatory (broker consumer command); NO healthCheck/readinessCheck/ports (workers don't serve HTTP); build + envVariables match prod; shared workers inherit the host target's `build.base` and cache — only `start` differs.

</block>

<block name="shared-across-setups">

**Shared across all setups:**
- `envVariables:` contains ONLY cross-service references + mode flags. Do NOT re-add envSecrets — platform injects them automatically.
- dev and prod env maps must NOT be bit-identical — a structural check fails if mode flags don't differ.

</block>

<block name="env-example-preservation">

### .env.example preservation

If the scaffolder produced `.env.example`, **keep it** with empty values. Remove `.env` (contains generated secrets). Update `.env.example` to cover every env var used in zerops.yaml `envVariables` (scaffolded defaults miss recipe-added keys like search host, object-storage endpoint) with sensible local defaults. A user running locally with zcli VPN should be able to copy `.env.example` → `.env` and have every key present.

</block>

<block name="framework-env-conventions">

### Framework environment conventions

Use the framework's **standard** env var names — do not invent new ones. If the framework has a base/app URL env var, set it to `${zeropsSubdomain}`. The chain recipe shows the correct names.

</block>

<block name="dashboard-skeleton">

### Write the dashboard skeleton

What you write now (main agent) vs what the sub-agent writes later (deploy step):

| Now (generate — you write this) | Later (deploy sub-agent — do NOT write this now) |
|---|---|
| Layout template with empty partial/component slots per feature section | Feature-section controllers/handlers |
| Placeholder text in each slot ("Section available after deploy") | Feature-section views with interactive UI |
| Primary model + migration + factory + seeder (15-25 records) | Feature-specific JavaScript |
| DashboardController with `/`, `/health`, `/status` endpoints returning real data | Feature-specific model mixins/traits |
| Service connectivity panel (CONNECTED/DISCONNECTED per provisioned service) | |
| All routes registered — GET + POST for every feature action, returning placeholder responses | |
| `zerops.yaml` (every setup), each repo's `README.md` (all 3 fragments), `.env.example` | |

Feature sections map to the plan's targets:

- **Database** — list seeded records + create-record form route
- **Cache** (if provisioned) — store-value-with-TTL route, cached-vs-fresh demonstration
- **Object storage** — upload-file + list-files routes
- **Search engine** — live search route over seeded records
- **Messaging broker + worker** — dispatch-job POST that publishes to a NATS subject the worker consumes; status poll that reads the worker's result

You write the routes (pre-registered with placeholder handlers) and the layout partials that WILL hold each section. The actual controllers and views that exercise the live services come later, when a framework-expert sub-agent runs at the deploy step against running containers.

Endpoint requirements:

- **Server-side (types 1, 2b, 3, 4)**: `GET /` (HTML dashboard), `GET /health` or `GET /api/health` (JSON), `GET /status` (JSON with connectivity checks — DB ping, cache ping, latency).
- **Static frontend (type 2a)**: single `GET /` page with framework name, greeting, timestamp, environment indicator. No server-side health endpoint.

For a single-feature minimal recipe you skip the skeleton/sub-agent split entirely — write everything inline in this step and move on.

</block>

<block name="asset-pipeline-consistency">

### Asset pipeline consistency

If `buildCommands` compiles assets (JS, CSS, or both), the primary view/template MUST load the compiled outputs via the framework's standard asset inclusion mechanism. Inline `<style>` or `<script>` blocks that bypass the build output are forbidden when a build pipeline exists. A build step that produces assets nobody loads is dead code. To verify: if zerops.yaml prod `buildCommands` produces built CSS/JS, check that the primary view/template references them through the framework's asset helper. This is the generate-step corollary of research decision 5 (scaffold preservation).

</block>

<block name="readme-with-fragments">

### App README with extract fragments

Write `README.md` at `/var/www/appdev/README.md` with three extract fragments. Use `prettyName` from the workflow response for titles (e.g., "Minimal", "Hello World", "Showcase"). **Critical formatting** — match this structure exactly:

```markdown
# {Framework} {PrettyName} Recipe App

<!-- #ZEROPS_EXTRACT_START:intro# -->
A minimal {Framework} application with a {DB} connection,
demonstrating database connectivity, migrations, and a health endpoint.
Used within [{Framework} {PrettyName} recipe](https://app.zerops.io/recipes/{slug}) for [Zerops](https://zerops.io) platform.
<!-- #ZEROPS_EXTRACT_END:intro# -->

⬇️ **Full recipe page and deploy with one-click**

[![Deploy on Zerops](https://github.com/zeropsio/recipe-shared-assets/blob/main/deploy-button/light/deploy-button.svg)](https://app.zerops.io/recipes/{slug}?environment=small-production)

![{framework} cover](https://github.com/zeropsio/recipe-shared-assets/blob/main/covers/svg/cover-{framework}.svg)

## Integration Guide

<!-- #ZEROPS_EXTRACT_START:integration-guide# -->

### 1. Adding `zerops.yaml`
The main configuration file — place at repository root. It tells Zerops how to build, deploy and run your app.

\`\`\`yaml
zerops:
  ... (paste full zerops.yaml with comments)
\`\`\`

### 2. Step Title (if any code changes needed)
Description of why this change is needed.

<!-- #ZEROPS_EXTRACT_END:integration-guide# -->

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->

### Gotchas
- **Gotcha 1** — explanation
- **Gotcha 2** — explanation

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
```

**Rules:**
- Section headings (`## Integration Guide`) go OUTSIDE markers — they're visible in the README but not extracted
- Content INSIDE markers uses **H3** (`###`), not H2
- **All fragments**: blank line required after the start marker (intro, integration-guide, knowledge-base)
- **Intro content**: plain text, no headings, 1-3 lines
- **Step 1** must be `### 1. Adding \`zerops.yaml\`` with a description paragraph before the code block (the API renders it as a section title)

</block>

<block name="code-quality">

### Code Quality
- Comment ratio in zerops.yaml code blocks must be >= 0.3 — **aim for 35%** to clear the threshold comfortably on the first attempt. Agents consistently underestimate; writing to 30% lands at 25%.
- No `PLACEHOLDER_*`, `<your-...>`, or `TODO` strings
- All env var references must use discovered variable names
- Comments explain WHY, not WHAT (don't restate the key name)
- Max 80 chars per comment line

</block>

<block name="pre-deploy-checklist">

### Pre-deploy checklist
- [ ] `.gitignore` exists and covers build artifacts, dependencies, and env files (e.g. `dist/`, `node_modules/`, `vendor/`, `.env`, `*.pyc`). Framework CLIs may skip generating it — always verify before `git add`
- [ ] Both `setup: dev` AND `setup: prod` present (generic names). Showcase with a shared-codebase worker: add `setup: worker` in the host target's zerops.yaml (the target named by `sharesCodebaseWith`). Showcase with a separate-codebase worker (default): the worker's own zerops.yaml has its own `dev` + `prod` setups, nothing extra in the app's zerops.yaml.
- [ ] dev and prod envVariables differ on mode flags — structural check fails if identical
- [ ] All env var refs use names from `zerops_discover`, none guessed
- [ ] If prod `buildCommands` compiles assets, primary view loads them via framework asset helper (not inline CSS/JS)
- [ ] If dev build base includes secondary runtime, dev `buildCommands` includes package manager install
- [ ] README has all 3 extract fragments with proper markers
- [ ] `.env.example` preserved (`.env` removed), updated with ALL env vars from zerops.yaml
- [ ] Dashboard has interactive feature section per provisioned service (no connectivity-only dots)
- [ ] Seeder creates sample data — dashboard shows real records on first deploy
- [ ] If search engine provisioned: `initCommands` includes search index population after `db:seed`

</block>

<block name="completion">

### Completion
```
zerops_workflow action="complete" step="generate" attestation="App code and zerops.yaml written to /var/www/appdev/. README with 3 fragments."
```

</block>
</section>

<section name="generate-fragments">
## Fragment Quality Requirements

### integration-guide Fragment

The integration guide answers: **"What must I change in my existing app to run it on Zerops?"** It targets a developer bringing their own codebase, not someone cloning the demo.

Must contain (all inside the markers, using **H3** headings):
- **`### zerops.yaml`** — complete config with ALL setups (`prod`, `dev`; `worker` if showcase). Setup names are generic (`prod`/`dev`), NOT hostname-specific. Every config line has an inline comment explaining WHY.
- **Numbered integration steps** (if any) — `### 2. Step Title`, `### 3. Step Title`, etc. Code changes the agent made that any user bringing their own codebase would also need.

**What belongs in integration steps:**
- Code-level changes the agent made that are required to work on Zerops (e.g., proxy trust configuration — without it, CSRF/origin validation breaks behind the L7 balancer)
- Framework config file changes for the platform (e.g., wiring S3 credentials, configuring a Redis session/cache driver)
- Any modification to app source that a user bringing their own app would also need to do

**What does NOT belong in integration steps:**
- Demo-specific scaffolding (custom routes, dashboard views, sample controllers) — these exist only in the recipe app, a real user wouldn't replicate them
- Config values already visible in zerops.yaml (the user can read those inline)
- Generic framework setup (how to install the framework, what build tools do)

### knowledge-base Fragment

The knowledge base answers: **"What will bite me that I can't learn from the zerops.yaml comments or platform docs?"** Each item must be **irreducible** — not learnable from the integration-guide, platform docs, or general framework docs.

Must contain:
- `### Gotchas` section with at least 2 framework-specific pitfalls on Zerops
- Zerops-specific behavior that differs from standard expectations (e.g., no .env file, base image contents, pdo extension availability)

**What belongs in knowledge-base vs integration-guide:**
- If it's a **required code change** → integration-guide step (the user needs to do this)
- If it's a **gotcha or quirk** the user should know about → knowledge-base (awareness, not action)
- If both: put the actionable step in integration-guide, put the "why it matters" explanation in knowledge-base. Example: trustProxies config is an integration step (action), but "CSRF fails without it because L7 terminates SSL" is a gotcha (awareness).

Do NOT include:
- Config values already visible in zerops.yaml (don't re-explain what the comments already cover)
- Platform universals (build/run separation, L7 routing, tilde behavior, autoscaling timing)
- Generic framework knowledge (how the framework works, what build tools do)

### intro Fragment
- 1-3 lines only
- No markdown titles (no `#`)
- No deploy buttons or badges
- No images
- Plain text describing what the recipe demonstrates

### Structural rules (hard)

- **H3 headings inside markers, H2 outside.** Section headings like `## Integration Guide` stay OUTSIDE the extract markers; content INSIDE markers uses `###`.
- **Blank line required after the start marker** for every fragment (intro, integration-guide, knowledge-base).
- **Exactly three fragments per README**, in this order: `intro`, `integration-guide`, `knowledge-base`.
- **Comment ratio in zerops.yaml code blocks must be >= 30%** — aim for 35% to clear the threshold comfortably on the first attempt. Agents consistently underestimate; writing to 30% lands at 25%.
- **No placeholders** — `PLACEHOLDER_*`, `<your-...>`, `TODO`, etc.
- **All env var references must use discovered variable names** — never guess.
- **Comments explain WHY, not WHAT** — don't restate the key name.
- **Max 80 chars per comment line**.

Writing-style voice (the "developer to developer" tone, anti-patterns, correct-style example) lives at **finalize** under "Comment style" — read it there when you write `envComments`. The same voice applies to the zerops.yaml comments you write here.
</section>

<section name="deploy">
## Deploy — Build, Start & Verify

`zerops_deploy` processes the zerops.yaml through the platform — this is when `run.envVariables` become OS env vars and cross-service references (`${hostname_varname}`) resolve to real values. Before this step, the dev container had no service connectivity. After this step, the app is fully configured.

### Dev deployment flow

**Execution order by recipe type — read this before following individual step numbers.**

The step numbers below are reference labels, NOT a linear script. For dual-runtime (API-first) recipes the steps interleave because the frontend depends on the API being verified first:

| Recipe type             | Order                                                                                                                            |
| ----------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| Single-runtime          | **Step 1 → Step 2 (2a/2b/2c) → Step 3 → Step 3a → Step 4 → Step 4b → Step 4c**                                                   |
| Dual-runtime (API-first) | **Step 1-API → Step 2a-API → Step 3-API (verify apidev only) → Step 1 → Step 2 (2a/2b/2c) → Step 3 → Step 3a (BOTH containers) → Step 4 → Step 4b → Step 4c** |

API-first teams: the steps labelled `-API` run FIRST; do not try to verify `appdev` (Step 3) before `appdev` has been deployed (Step 1). Step 3a runs once, at the end, reading logs from both `apidev` and `appdev` together.

> **Parameter naming**: the deploy parameter is `targetService` (NOT `serviceHostname`). `serviceHostname` is used by `zerops_mount`, `zerops_subdomain`, `zerops_verify`, `zerops_logs`, and `zerops_env` — deploy is the exception. If you get `unexpected additional properties ["serviceHostname"]`, you used the wrong name.

**Step 1: Deploy appdev (self-deploy)**
```
zerops_deploy targetService="appdev" setup="dev"
```
The `setup="dev"` parameter maps hostname `appdev` to `setup: dev` in zerops.yaml. This triggers a build from files already on the mount. Blocks until complete.

**Step 1-API** (API-first showcase only, runs BEFORE Step 1): Deploy apidev FIRST — the API must be running before the frontend builds (the frontend bakes the API URL at build time):
```
zerops_deploy targetService="apidev" setup="dev"
```
After this completes, run Step 2a-API (start the API process) then Step 3-API (verify apidev); THEN return to Step 1 to deploy appdev.

**Step 2: Start ALL dev processes (before any verification)**

Every process the app needs to serve a page must be running before Step 3 (verify). This includes the primary server, asset dev servers, and worker processes. Start them all now:

**2a. Primary server:**
- **Server-side apps** (types 1, 2b, 3, 4): Start via SSH:
  ```bash
  ssh appdev "cd /var/www && {start_command} &"
  ```
- **Implicit-webserver runtimes** (php-nginx, php-apache, nginx): Skip — auto-starts.
- **Static frontends** (type 2a): Skip — Nginx serves the built files.

**2a-API** (API-first): Start the API server on apidev:
```bash
ssh apidev "cd /var/www && {api_start_command} &"
```

**2b. Asset dev server** (if the build pipeline uses a secondary runtime):
If `run.prepareCommands` installs a secondary runtime (e.g., `sudo -E zsc install nodejs@22`) and the scaffold defines a dev server (e.g., `npm run dev` for Vite), start it now.

**Before starting, check if one is already running.** The deploy framework may have started the dev server on first deploy; launching a second instance via background SSH creates a port collision. The second instance silently falls back to an incremented port (Vite: 5173 → 5174), and the public subdomain doesn't route to the new port — it routes to the original.

```bash
ssh appdev "pgrep -f 'vite' || true"
ssh appdev "pgrep -f 'npm run dev' || true"
```

If a process is already running, skip the start. If you need to restart (after a config change), kill first: `ssh appdev "pkill -f 'vite' || true"` then start once:

```bash
ssh appdev "cd /var/www && {dev_server_command} &"
```

Pass the appropriate host binding flag so it listens on `0.0.0.0` (e.g., `npx vite --host 0.0.0.0`). This applies even when the primary server auto-starts — the primary handles HTTP, but the asset dev server compiles CSS/JS.

**This step is MANDATORY, not optional.** Without it, templates that reference build-pipeline outputs (Vite manifests, Webpack bundles) will 500 on the first page load. Do NOT work around missing assets by running `npm run build` on the dev container — that compiles static assets instead of using HMR, and doesn't prove the dev experience works. Do NOT replace framework asset helpers with inline CSS/JS — that disconnects the build pipeline.

**2c. Worker dev process** (showcase only):
- **Shared codebase** (`worker.sharesCodebaseWith` is set): start the queue consumer as an SSH process on the HOST target's dev container — both processes run from the same container, same code tree:
  ```bash
  ssh {host_hostname}dev "cd /var/www && {queue_worker_command} &"
  ```
  `{host_hostname}` is the target named by `sharesCodebaseWith` — `appdev` for single-app recipes, `apidev` for dual-runtime recipes where the API hosts the worker.
- **Separate codebase** (`worker.sharesCodebaseWith` empty — the default, including the 3-repo case): deploy the separate worker codebase to its own dev container, then start the process there:
  ```
  zerops_deploy targetService="workerdev" setup="dev"
  ssh workerdev "cd /var/www && {queue_worker_command} &"
  ```

**Step 3: Enable subdomain and verify appdev** (single-runtime recipes — API-first recipes run Step 3-API first, see below, then return here)
```
zerops_subdomain action="enable" serviceHostname="appdev"
zerops_verify serviceHostname="appdev"
```
Check: service RUNNING, subdomain returns 200, health endpoint responds (or page loads for static).

**Step 3a: Verify `initCommands` actually ran — check logs, don't assume** (runs AFTER Step 3 and, for API-first, AFTER Step 3-API has verified apidev AND Step 3 has verified appdev)

If `setup: dev` declares `initCommands` (migrate / seed / search-index), those commands ran during deploy activation — the platform invokes them on every fresh deploy, including the first one on an idle-start container. You MUST verify they ran and succeeded by reading the runtime logs, NOT by re-running them manually:

```
zerops_logs serviceHostname="appdev" limit=200 severity=INFO since=10m
```

**API-first recipes must fetch logs from BOTH containers** — the API typically owns the migration/seed commands and the frontend is often a static build with no initCommands at all:

```
zerops_logs serviceHostname="apidev" limit=200 severity=INFO since=10m
zerops_logs serviceHostname="appdev" limit=200 severity=INFO since=10m
```

Look for the framework-specific output each command emits: migration applied rows, "20 articles seeded", "Meilisearch: indexed 20 documents", etc. Expected outcomes:

- **Output present, success line visible** → initCommands ran cleanly. Proceed to dashboard verification.
- **Output present, error logged** → initCommands ran and one of them crashed. The deploy response will usually return `DEPLOY_FAILED` with `error.meta[].metadata.command` identifying which command failed. Fix the command (code or zerops.yaml) and redeploy. Do NOT re-run the failing command manually via SSH — the whole point of `zsc execOnce` is that the next deploy will retry cleanly; re-running manually bypasses the gate and hides the reproduction case.
- **Output completely absent** → something is wrong. Do NOT assume initCommands silently skipped. Check:
  - Does the dev setup actually declare `initCommands`? Read the zerops.yaml back from the mount.
  - Did the deploy transition to ACTIVE? If it's still DEPLOYING, wait and re-read logs.
  - Is the `since` window long enough? Widen to `since=30m` and retry.
  - Is the log severity filter too narrow? Drop `severity=INFO` to see everything.

**Never "work around" missing output by running `npx ts-node migrate.ts && ... seed.ts` over SSH to populate the database manually.** That produces a recipe that appears to work in the workspace but ships broken to end users who never see your manual fix. If the initCommands truly didn't fire (rare — would be a platform bug), report it and stop; don't proceed with a hand-patched dataset.

**The `zsc execOnce` burn-on-failure trap**: `zsc execOnce` keys on `${appVersionId}`, which doesn't change between retries of the same deploy version. If the first attempt runs the seed, the seed crashes mid-insert, and the container dies — the next retry with the same `appVersionId` will NOT re-run the seed. The platform thinks it already ran. Symptom: the seeder output appears in the FIRST deploy's logs, then is absent on every subsequent retry, and the database contains partial data.

Recovery: either (a) modify something that forces a new `appVersionId` (touch a source file, even a whitespace change, then redeploy — the new version ID makes `execOnce` re-fire), or (b) manually run the seed command via SSH once (`ssh {hostname} "cd /var/www && {seed_command}"`) then redeploy to verify the fix lands. Option (a) is preferred because it preserves the "never manually patch workspace state" rule; option (b) is the escape hatch when the seed depends on a schema that only exists after a successful initCommand run.

**Step 3-API** (API-first only, runs AFTER Step 1-API + Step 2a-API, BEFORE Step 1): Enable and verify the API FIRST — this is a checkpoint before the frontend deploy, not a late verification step:
```
zerops_subdomain action="enable" serviceHostname="apidev"
zerops_verify serviceHostname="apidev"
```
Verify `/api/health` returns 200 via curl. THEN return to Step 1 to deploy appdev — the frontend needs the API running before it can deploy (in build-time-baked configurations) or before it can be verified (in runtime-config configurations). After appdev deploys, Step 2 (processes) → Step 3 (enable appdev subdomain + verify the dashboard loads and successfully fetches from the API) → Step 3a (logs from BOTH containers).

**CORS** (API-first): The API must set CORS headers allowing the frontend subdomain. Use the framework's standard CORS middleware and allow the frontend's subdomain origin.

For showcase, also verify the worker is running via logs (no HTTP endpoint):
```
zerops_logs serviceHostname="appdev" limit=20
```

**Redeployment = fresh container.** If you fix code and redeploy during iteration, the platform creates a new container — ALL background processes (asset dev server, queue worker) are gone. Restart them before re-verifying. This applies to every redeploy, not just the first.

**Step 4: Iterate if needed** (max 3 iterations)
If verification fails: check logs (`zerops_logs serviceHostname="appdev"`), fix code on mount, kill previous server, restart via SSH, re-verify. After any redeploy, repeat Step 2 (start ALL processes) before Step 3 (verify).

**Step 4b: Dispatch the feature sub-agent (MANDATORY for Type 4 showcase)**

After appdev is deployed and verified with the skeleton (connectivity panel, seeded data, health endpoint), dispatch ONE framework-expert sub-agent to fill in the feature sections. **This is where feature implementation happens — generate writes the skeleton only.** Writing feature code at generate means writing blind against disconnected services; the sub-agent writes against live services and can test each feature as it goes.

Minimal recipes (1-2 feature sections) skip the sub-agent entirely — the main agent writes features inline during generate.

**Sub-agent brief — required contents**:

- Exact file paths (framework-conventional locations for controllers, views, partials)
- Installed packages relevant to each feature (from the plan's `cacheLib`, `storageDriver`, `searchLib` etc.)
- Service-to-feature mapping (database/cache/storage/search/queue — one feature section per provisioned service, exercising that service)
- **UX quality contract** (see below) — what the rendered dashboard must look like
- Pre-registered route paths the sub-agent must fill (agent wrote them as stubs at generate)
- **Where app-level commands run** (hard rule, see below)
- Instruction to **test each feature against the live service immediately after writing** — the sub-agent has SSH access to appdev and every managed service is reachable. After writing a controller+view, hit the endpoint via `ssh {devHostname} "curl -s localhost:{port}/…"` (or the framework's test runner over SSH) and verify the response. Fix immediately; do not write ahead of verification.

**API-first**: the sub-agent works on BOTH apidev AND appdev mounts (plus workerdev if the worker has a public-facing component). Include every mount path in the brief. The sub-agent adds API routes (controllers, services) and corresponding frontend components that fetch from the API.

**Feature sections must EXERCISE each service, not just check connectivity**:

- **Database** — list seeded records + create-record form (proves ORM + migrations + CRUD)
- **Cache** (if provisioned) — store-a-value-with-TTL + cached-vs-fresh demonstration. Cache is for cache + sessions only; the queue uses NATS, a separate broker
- **Object storage** (if provisioned) — upload-file + list-files routes (proves S3 integration)
- **Search engine** (if provisioned) — live search over seeded records (proves indexing)
- **Messaging broker + worker** (if provisioned) — dispatch-job POST that publishes to a NATS subject; worker consumes, writes result to a DB table the dashboard polls. Show (a) message sent (timestamp + subject), (b) worker-processed result appearing within ~1s. Proves the full NATS → worker → result round-trip

The seeder populates 15-25 sample records so the dashboard shows real data on first deploy, not empty states. Search index is populated via the framework's search-import command in `initCommands` after `db:seed`.

**UX quality contract** (what "dashboard style" means — include verbatim in the sub-agent brief):

The dashboard must be **polished** — minimalistic does NOT mean unstyled browser defaults. A developer deploying this recipe should not be embarrassed.

- **Styled form controls** — never raw browser-default `<input>` / `<select>` / `<button>`. Use scaffolded CSS (Tailwind if present) or clean inline styles: padding, border-radius, consistent sizing, focus ring, button hover
- **Visual hierarchy** — section headings delineated, consistent vertical rhythm, tables with headers + cell padding + alternating row shading
- **Status feedback** — success/error flash after submissions, loading text for async operations, meaningful empty states
- **Readable data** — aligned columns, relative timestamps ("3 minutes ago"), monospace for IDs
- System font stack, generous whitespace, monochrome palette + ONE accent color, mobile-responsive via simple CSS
- **Avoid**: component libraries, icon packs, animations, dark-mode toggles, JS frameworks for interactivity, inline `<style>` alongside a build pipeline
- **XSS protection (mandatory)**: all dynamic content escaped. `textContent` for JS-injected text; framework template auto-escaping for server-rendered content. Never use raw/unescaped output mode.

**Where app-level commands run** (hard rule — include verbatim in the sub-agent brief):

The sub-agent runs on the zcp orchestrator container. `{appDir}` is an SSHFS network mount — a bridge to the target container's `/var/www/`, not a local directory. File reads and edits through the mount are fine. **Target-side commands — anything in the app's own toolchain — MUST run via SSH on the target container**, not on zcp against the mount.

The principle is WHICH CONTAINER'S WORLD the tool belongs to:

- **SSH (target-side)** — compilers (`tsc`, `nest build`, `go build`), type-checkers (`svelte-check`, `tsc --noEmit`), test runners (`jest`, `vitest`, `pytest`, `phpunit`), linters (`eslint`, `prettier`), package managers (`npm install`, `composer install`), framework CLIs (`artisan`, `nest`, `rails`), and any app-level `curl`/`node`/`python -c` that hits the running app or managed services.
- **Direct (zcp-side)** — `zerops_*` MCP tools, `zerops_browser`, Read/Edit/Write against the mount, `ls`/`cat`/`grep`/`find` against the mount, `git status`/`add`/`commit` (with the safe.directory config from provision).

Correct shape:
```
ssh {hostname} "cd /var/www && {command}"   # correct — app's world
cd /var/www/{hostname} && {command}          # WRONG — zcp against the mount
```

Running app-level commands on zcp uses the wrong runtime, the wrong dependencies, the wrong env vars, has no managed-service reachability, AND exhausts zcp's fork budget. Symptom: `fork failed: resource temporarily unavailable` cascades. Recovery is `pkill -9 -f "agent-browser-"` on zcp + waiting for process reaping; the real fix is to stop running target-side commands zcp-side.

**After the sub-agent returns**:
1. Read back feature files — verify they exist and aren't empty
2. Git add + commit on every mount the sub-agent touched (apidev, appdev, workerdev as applicable)
3. Redeploy each affected dev service — fresh container, all SSH processes died, restart them (Step 2)
4. HTTP-level verification via curl on every feature endpoint
5. If anything fails, fix on mount, iterate (counts toward the 3-iteration limit)

**Step 4c: Browser verification — MANDATORY for Type 4 showcase** (skip for minimal)

curl proves the server responds. It does NOT prove the user sees what they should see. A showcase dashboard is a user-facing deliverable — if the feature sub-agent's code has a JS error, a broken fetch, a missing import, or a CORS failure, curl returns 200 while the dashboard renders blank. **Every showcase recipe must be browser-verified before moving to stage.**

What browser verification catches that curl cannot:
- JavaScript runtime errors (uncaught promise rejections, undefined method calls)
- Broken fetch URLs (wrong port, wrong protocol, missing `/api` prefix)
- CORS failures (API rejects the frontend origin)
- Blank renders (component mounted but never populated)
- Missing CSS (everything works but looks broken)
- Stale build artifacts (user sees a version from before your last fix)

#### Browser verification — use `zerops_browser`, never raw agent-browser

**Why `zerops_browser` is mandatory** — raw `agent-browser` CLI calls left Chrome running when a batch didn't close cleanly, exhausting the fork budget (crashed v4 and v5), and two parallel calls raced the persistent daemon. The tool auto-wraps `[open url] + your commands + [errors] + [console] + [close]` so close is guaranteed, serializes all calls through a process-wide mutex, and auto-runs pkill recovery on fork exhaustion. Never call `agent-browser` directly from Bash.

#### Non-negotiable rules

1. **Walk dev FIRST (while dev processes are running), THEN kill dev processes, THEN walk stage.** This is the only order that works. The dev walk verifies the subdomain the dev processes serve — killing them first would take down the very server you're trying to browse (a 502 response is your proof of wrong ordering). After the dev walk completes, kill every background dev process (`npm run dev`, `nest start --watch`, `ts-node worker`, nohup jobs) on every dev container to free the fork budget, then run the stage walk. Stage containers run their own processes and are not affected by the kill. **Do not reverse this order and do not merge the kill into the dev walk's pre-step.**
2. **Use `zerops_browser` — never `agent-browser` as a Bash call.** The tool is the ONLY sanctioned path. Any raw `agent-browser` / `echo ... | agent-browser batch` command in a Bash tool call is a bug.
3. **One `zerops_browser` call per subdomain.** Pass the URL + inner commands; the tool wraps open/errors/console/close. Do NOT pass multiple URLs or multiple open/close markers in one call.
4. **Do not dispatch a sub-agent that calls `zerops_browser` while the main agent also has one in flight.** The verification sub-agent brief forbids browser usage entirely (the close step is split — see below); the main agent runs the browser walk itself.
5. **If the tool returns `forkRecoveryAttempted: true`** — pkill already ran. Before retrying, find the process that burned the budget. For a STAGE walk, usually it's a dev process you forgot to kill after the dev walk (`ssh {devHostname} "ps -ef | grep -E 'nest|vite|node dist|ts-node'"`). For a DEV walk, the budget was already tight before the walk started — the usual cause is lingering subprocess trees from an earlier feature sub-agent or a previous browser session that wasn't reaped cleanly; run the manual pkill below and retry.
6. **If a Bash call crashes with `fork failed: resource temporarily unavailable` or `pthread_create: Resource temporarily unavailable`** — something other than `zerops_browser` leaked processes. Recover manually:
   ```
   pkill -9 -f "agent-browser-"
   ```
   Wait 1-2s for reaping. Never retry in a loop.

#### Efficient command vocabulary (use these INSIDE `commands` — NOT `eval`)

Dedicated commands produce structured output designed for agents. Don't reach for `eval` / JavaScript unless none of these fit.

| Need | Command (inside the `commands` array) | Notes |
|---|---|---|
| Interactive element tree with clickable refs | `["snapshot", "-i", "-c"]` | `-i` = interactive only, `-c` = compact. Yields `@e1`, `@e2` refs usable in `click`, `fill`, `get`. |
| Text content of an element | `["get", "text", "<sel>"]` | Or `["get","text","@e3"]` using a ref from snapshot. |
| Element count | `["get", "count", "<sel>"]` | e.g. verify a table has ≥1 row. |
| Is something visible / enabled / checked? | `["is", "visible", "<sel>"]` | Plus `is enabled`, `is checked`. |
| Find by semantic locator | `["find", "role", "button", "Submit", "click"]` | Locators: `role`, `text`, `label`, `placeholder`, `testid`. Avoid brittle CSS. |
| Click / fill / type | `["click", "@e1"]`, `["fill", "@e2", "text"]`, `["type", "<sel>", "text"]` | Refs from snapshot. |
| Wait for element or milliseconds | `["wait", "<sel>"]` or `["wait", "500"]` | Integer = ms. |
| Capture network traffic | `["network", "har", "start"]` … interact … `["network", "har", "stop", "./net.har"]` | Full HAR. |

Do NOT pass `["open", ...]` or `["close"]` inside `commands` — the tool strips them and re-adds its own wrappers. `["errors"]` and `["console"]` are also auto-appended (you can still add extra `["errors","--clear"]` calls inside your flow if you want to checkpoint mid-walk).

#### Canonical verification flow

Three phases in strict order. **Do not reorder.**

**Phase 1 — Dev walk (dev processes running, NO kill).** The dev subdomain serves whatever the dev processes started in Step 2 serve. Walk it while they're still up. This is the only phase where the dev container renders your dashboard in a browser:

```
zerops_browser(
  url: "https://{appdev-subdomain}.prg1.zerops.app",
  commands: [
    ["snapshot", "-i", "-c"],
    ["get", "text", "[data-connectivity]"],
    ["get", "count", "[data-article-row]"],
    ["find", "role", "button", "Submit", "click"],
    ["get", "text", "[data-result]"]
  ]
)
```

If dev walk returns a 502 or connection failure, your dev processes aren't running (or they died). Diagnose via `ssh {devHostname} "ps -ef | grep -E 'nest|vite|node|ts-node'"` and restart per Step 2 before continuing.

**Phase 2 — Kill dev processes (Bash).** Only now, after the dev walk has passed, free the fork budget. API-first recipes: both apidev AND appdev. Single-runtime: just appdev.

```
ssh apidev "pkill -f 'nest start' || true; pkill -f 'ts-node' || true; pkill -f 'node dist/worker' || true"
ssh appdev "pkill -f 'vite' || true; pkill -f 'npm run dev' || true"
```

**Phase 3 — Stage walk (dev processes dead).** Walk the stage subdomain. Stage containers run their own processes and are completely unaffected by the dev kill:

```
zerops_browser(
  url: "https://{appstage-subdomain}.prg1.zerops.app",
  commands: [
    ["snapshot", "-i", "-c"],
    ["get", "text", "[data-connectivity]"],
    ["get", "count", "[data-article-row]"],
    ["find", "role", "button", "Submit", "click"],
    ["get", "text", "[data-result]"]
  ]
)
```

The tool executes `[open url] + your commands + [errors] + [console] + [close]` as one batch and returns structured JSON: `steps[]`, `errorsOutput`, `consoleOutput`, `durationMs`, `forkRecoveryAttempted`, `message`.

**If you need to re-iterate** after a stage walk found something: fix on the mount, redeploy dev (which needs dev processes — you must restart them via SSH since the kill in Phase 2 took them down), re-verify dev with the curl flow in deploy Step 3, then cross-deploy to stage, then repeat Phase 2 + Phase 3. Phase 1 does NOT need to run again for a re-iteration — one dev browser walk per close step is enough.

**Report shape for a verification pass** (per subdomain walked):
- Connectivity panel state (services connected with latencies)
- Each feature section's render state (populated / empty / errored)
- `errorsOutput` from the result (expected: empty)
- `consoleOutput` from the result (expected: empty or benign info only)
- `forkRecoveryAttempted` from the result (expected: false — if true on the STAGE walk you didn't fully kill the dev processes in Phase 2; if true on the DEV walk something upstream was leaking before the walk started)

**What to avoid** (all were seen in v4, v5, or v6):
- Raw `agent-browser` / `echo ... | agent-browser batch` Bash calls — always use `zerops_browser` MCP tool
- **Killing dev processes BEFORE the dev walk** — the dev subdomain then returns 502 because the dev processes ARE the dev server. This is the v6 regression. Phase 1 before Phase 2, always.
- `["eval", "window.onerror = …"]` inside commands — use the auto-appended `["errors"]` / `["console"]` output instead
- Running the STAGE walk while dev processes are still running on dev containers — guaranteed `forkRecoveryAttempted: true`
- Passing `["open", ...]` or `["close"]` inside `commands` — the tool strips them; if you thought you needed them, you didn't
- Dispatching a sub-agent that calls `zerops_browser` while the main agent also has a call in flight
- Re-running the tool against the same URL repeatedly "just to be sure" — one call per URL per iteration

If a walk reveals a problem curl missed: the batch has already closed the browser, so fix on mount, redeploy, and run the affected phase again (counts toward the 3-iteration limit). Do NOT advance to publish until BOTH appdev AND appstage walks show empty errors and populated sections.

### Stage deployment flow (after all appdev work is complete)

Stage is the final product — deploy it once with the complete codebase (skeleton + features).

**Step 5: Verify prod setup (already written at generate)**
The prod setup block was written to zerops.yaml during the generate step. Before cross-deploying, verify it matches what a real user building from git will need:
- `deployFiles` lists every path the start command and framework need at runtime — run `ls` on the mount and cross-reference. When cherry-picking (not using `.`), missing one path will DEPLOY_FAILED at first request.
- `healthCheck` + `deploy.readinessCheck` are present (required for prod — unresponsive containers get restarted; broken builds are gated from traffic).
- `initCommands` covers framework cache warming + migrations (NEVER in buildCommands — `/build/source/...` paths break at `/var/www/...`).
- Mode flags differ from dev (APP_ENV/NODE_ENV/DEBUG/LOG_LEVEL).

If anything is missing, edit zerops.yaml on the mount now — the change propagates to the README via the integration-guide fragment (which mirrors the file content).

**Step 6: Cross-deploy ALL stage targets IN PARALLEL**

Once dev is verified, every `*stage` target is an independent cross-deploy — each targets a different container, runs a different build pipeline, and shares nothing with its siblings. **Dispatch all stage deploys in a single message as parallel tool calls.** Do NOT wait for one to finish before starting the next — that serializes ~2 minutes of work for zero benefit.

What's independent and can run in parallel:

- **Minimal single-runtime**: `appstage` only (nothing to parallelize).
- **Showcase single-runtime**: `appstage` + `workerstage` (both cross-deploy from `appdev`, different setups). → 2 parallel calls.
- **Minimal dual-runtime (API-first)**: `appstage` + `apistage`. → 2 parallel calls.
- **Showcase dual-runtime (API-first)**: `appstage` + `apistage` + `workerstage`. → 3 parallel calls.

Example call shape (dispatch these as parallel tool calls in ONE message):

```
zerops_deploy sourceService="apidev" targetService="apistage" setup="prod"
zerops_deploy sourceService="apidev" targetService="workerstage" setup="worker"
zerops_deploy sourceService="appdev" targetService="appstage" setup="prod"
```

- `setup="prod"` maps to `setup: prod` in the target's zerops.yaml (server auto-starts via the real `start` command, or Nginx for static).
- `setup="worker"` maps to `setup: worker` in the host target's zerops.yaml — used ONLY for a **shared-codebase worker** (`sharesCodebaseWith` is set). Source and target are the same host target (`appdev` / `apidev`), just a different setup name. Same build pipeline, different start command.
- **Separate-codebase worker** (`sharesCodebaseWith` empty, including the 3-repo same-runtime case): source is `workerdev`, target is `workerstage`, setup is `prod` (its OWN zerops.yaml). Still parallel with the other cross-deploys.

**Why this is safe**: cross-deploys don't mutate their source service, don't share build containers, and the platform schedules them on separate target containers. There is no ordering constraint between sibling stage targets. The only ordering constraints in this whole phase are (a) dev must be healthy before its stage cross-deploys (already satisfied by this point) and (b) the subdomain + verify calls below run AFTER the deploys return.

**Step 7: Enable stage subdomains + verify — also in parallel**

After all stage deploys return ACTIVE, dispatch the subdomain enables and verifies as parallel tool calls too — each targets a different service and has no dependency on the others.

```
zerops_subdomain action="enable" serviceHostname="appstage"
zerops_subdomain action="enable" serviceHostname="apistage"     # API-first only
zerops_verify serviceHostname="appstage"
zerops_verify serviceHostname="apistage"                         # API-first only
zerops_logs serviceHostname="workerstage" limit=20               # showcase only (worker has no HTTP)
```

**Step 8: Present URLs**

### Reading deploy failures — which phase failed, and where to look

`zerops_deploy` returns `status` that tells you WHICH lifecycle phase failed. Each has a different fix location and a different log source:

| status | Phase | Where the stderr lives | What to fix |
|---|---|---|---|
| `ACTIVE` | — | — | Success. |
| `BUILD_FAILED` | Build container (`/build/source/`) | `buildLogs` field in deploy response | `buildCommands` exited non-zero. Fix `zerops.yaml` `build.buildCommands`. |
| `PREPARING_RUNTIME_FAILED` | Runtime prepare (before deploy files arrive) | `buildLogs` field (yes, historical naming) | `run.prepareCommands` exited non-zero. Fix `zerops.yaml` `run.prepareCommands`. Common cause: referencing `/var/www/` paths that don't exist yet — use `addToRunPrepare` + `/home/zerops/` instead. |
| `DEPLOY_FAILED` | Runtime init (container already started, deploy files at `/var/www/`) | **Runtime logs** — `zerops_logs serviceHostname={service} severity=ERROR since=5m`. NOT buildLogs. | An `initCommand` crashed the container. The deploy response's `error.meta[].metadata.command` field shows which command failed. Fix `zerops.yaml` `run.initCommands`. Common cause: a buildCommand baked `/build/source/...` paths into a compiled cache that doesn't resolve at runtime (move `config:cache`/`asset:precompile`-style commands from `buildCommands` to `run.initCommands`). |
| `CANCELED` | — | — | User/system canceled; redeploy. |

**Reading the error metadata on `DEPLOY_FAILED`**: the deploy response includes a structured `error` field:
```json
{"error": {"code": "commandExec", "meta": [{"metadata": {"command": ["php artisan migrate --force"], "containerId": ["..."]}}]}}
```
This identifies *which* initCommand failed. For *why* it failed, fetch runtime logs on the target service — the stderr is there, not in buildLogs.


### Common deployment issues

| Issue | Diagnosis | Fix |
|-------|-----------|-----|
| HTTP 502 | App not binding 0.0.0.0 or wrong port | Check runtime knowledge for bind rules |
| Empty env vars | Deploy hasn't happened yet, or service not restarted after env change | Deploy first — envVariables activate at deploy time. After `zerops_env set`, restart the service (`zerops_manage action="restart"`) — env vars are cached at process start. |
| `BUILD_FAILED` | buildCommands exited non-zero | Check `buildLogs` in deploy response, fix `buildCommands` in zerops.yaml |
| `PREPARING_RUNTIME_FAILED` | run.prepareCommands failed before deploy files arrived | Check `buildLogs`, fix `run.prepareCommands`. Use `addToRunPrepare` instead of referencing `/var/www/`. |
| `DEPLOY_FAILED` | run.initCommands crashed the container at startup | Check deploy response `error.meta` for which command; fetch stderr via `zerops_logs serviceHostname={service} severity=ERROR since=5m` (NOT buildLogs). If /build/source paths mentioned, move cache commands to run.initCommands. |
| Stage deploy fails | zerops.yaml setup name doesn't match --setup param | Ensure `setup: prod` in zerops.yaml and `setup="prod"` in zerops_deploy |
| Health check fails | healthCheck configured on dev entry | Remove healthCheck from dev; agent controls lifecycle |
| Static site 404 | Wrong `documentRoot` | Match to actual build output directory |

### Completion
```
zerops_workflow action="complete" step="deploy" attestation="Dev deployed at {dev_url}, stage deployed at {stage_url}. Both healthy."
```
</section>

<section name="finalize">
## Finalize — Recipe Repository Files

Recipe files were **auto-generated** in the output directory when deploy completed. The output directory (`outputDir` in the response) contains:
- 6 environment folders with import.yaml (correct structure, scaling, buildFromGit) and README.md
- 1 root README with deploy button, cover image, environment links
- 1 app README scaffold at `appdev/README.md` with correct markers and deploy button — compare with your app README at `/var/www/appdev/` to ensure yours has the same structural elements (deploy button, cover, markers)

### Do NOT edit import.yaml files by hand

The template emits YAML structure + scaling values only — all prose commentary comes from your `envComments` input. Editing files by hand means agents rewrite them from scratch and drop the auto-generated `zeropsSetup` + `buildFromGit` fields. **Pass structured per-env comments instead.** One call bakes all 6 files.

### Step 1: Write one tailored comment set per environment

The 6 envs are **not interchangeable** — each exists to describe a different deployment context. Copying one comment block into all 6 defeats the purpose. Tailor each env's prose to what makes THAT env distinct:

| Env | Distinct framing |
|---|---|
| 0 — AI Agent | dev workspace for an AI agent — SSH in, build, verify via subdomain |
| 1 — Remote (CDE) | remote dev workspace for humans — SSH/IDE, full toolchain, live edit |
| 2 — Local | local development + `zcli vpn` connecting to a Zerops-hosted validator |
| 3 — Stage | single-container staging that mirrors production configuration |
| 4 — Small Production | production with `minContainers: 2` for rolling-deploy availability |
| 5 — HA Production | production with `cpuMode: DEDICATED`, `mode: HA`, `corePackage: SERIOUS` |

Pass `envComments` keyed by env index (`"0"`..`"5"`). Each env carries a `service` map (keys match the hostnames that appear in THAT env's file) and an optional `project` comment. **Service key rule**: envs 0-1 carry the dev+stage pair, so keys are `"appdev"` and `"appstage"`; envs 2-5 collapse to a single runtime entry, so the key is the base hostname (`"app"`). Managed services (`"db"` etc.) keep the base hostname everywhere.

**Showcase service keys — the key list depends on the worker's `sharesCodebaseWith`.** A shared-codebase worker (`sharesCodebaseWith` set) gets ONLY `workerstage` in envs 0-1 because the host target's dev container runs both processes. A separate-codebase worker (empty `sharesCodebaseWith` — the default, including the 3-repo case) gets both `workerdev` and `workerstage`. Omitting a comment key for a service that appears in the import.yaml produces a service with no comment, which degrades quality and risks failing the comment ratio check. Complete key list per env:

**Full-stack showcase:**
- **Envs 0-1 (shared-codebase worker)**: `"appdev"`, `"appstage"`, `"workerstage"`, plus all managed services (`"db"`, `"cache"`, `"storage"`, `"search"`, etc.)
- **Envs 0-1 (separate-codebase worker)**: `"appdev"`, `"appstage"`, `"workerdev"`, `"workerstage"`, plus all managed services
- **Envs 2-5**: `"app"`, `"worker"`, plus all managed services

**API-first showcase (dual-runtime):**
- **Envs 0-1**: `"appdev"`, `"appstage"`, `"apidev"`, `"apistage"`, `"workerstage"`, plus all managed services
- **Envs 2-5**: `"app"`, `"api"`, `"worker"`, plus all managed services

Every service that appears in a given env's import.yaml MUST have a comment explaining its role in THAT env.

```
zerops_workflow action="generate-finalize" \
  envComments={
    "0": {
      "service": {
        "appdev": "Development workspace for AI agents. zeropsSetup:dev deploys the full tree so the agent can SSH in and edit source over SSHFS — PHP reinterprets each request, no restart required. Subdomain gives the agent a URL to verify output.",
        "appstage": "Staging slot — agent deploys here with zerops_deploy setup=prod to validate the production build (composer install --no-dev + runtime config:cache) before finishing the task.",
        "db": "PostgreSQL — carries schema, sessions, cache, and queued jobs (all Laravel drivers default to 'database' in the minimal tier). Shared by appdev and appstage. NON_HA fine for dev/staging; priority 10 so db starts before the app containers."
      },
      "project": "APP_KEY is Laravel's AES-256-CBC encryption key (32 bytes). Project-level so session cookies and encrypted DB attributes remain valid when the L7 balancer routes a request to any app container."
    },
    "1": {
      "service": {
        "appdev": "Remote development workspace — SSH or IDE-SSHFS into the dev container and edit source live. zeropsSetup:dev installs the full Composer dependency set so pint/phpunit/pail are available on the container. PHP interprets each request, no restart cycle.",
        "appstage": "Staging for remote developers — zerops_deploy setup=prod mirrors what CI would build for production, letting you validate config:cache + route:cache before merging.",
        "db": "PostgreSQL — same persistence layer as in env 0. NON_HA because remote dev environments are replaceable."
      },
      "project": "APP_KEY shared across containers (same rationale as env 0)."
    },
    "2": {
      "service": {
        "app": "Local-env validator — you develop against localhost on your machine (zcli vpn up to reach this Zerops Postgres), then push with zcli to this app container to verify the production build actually deploys cleanly before tagging a release.",
        "db": "Managed Postgres reachable from your laptop via zcli VPN. Priority 10 so db starts before the app."
      },
      "project": "APP_KEY shared across containers."
    },
    "3": {
      "service": {
        "app": "Staging — mirrors production config (composer install --no-dev + runtime cache warming) but runs on a single container with lower scaling. Public subdomain for QA and stakeholder review. Git integration or zcli push from CI triggers deploys.",
        "db": "Postgres — single-node because staging data is replaceable."
      },
      "project": "APP_KEY shared across containers."
    },
    "4": {
      "service": {
        "app": "Small production — minContainers: 2 guarantees at least two app containers at all times, spreading load and keeping traffic flowing during rolling deploys and container replacement. Zerops autoscales RAM within verticalAutoscaling bounds.",
        "db": "Postgres single-node. Consider HA mode (env 5) for higher durability."
      },
      "project": "APP_KEY shared across containers — critical in production because sessions break if containers disagree on the key."
    },
    "5": {
      "service": {
        "app": "HA production. cpuMode: DEDICATED pins cores to this service so shared-tenant noise doesn't pollute request latency under sustained load. minContainers: 2 + autoscaling handles capacity; minFreeRamGB leaves 50% headroom for traffic spikes.",
        "db": "Postgres HA — replicates data across multiple nodes so a single node failure causes no data loss or downtime. Dedicated CPU ensures DB ops don't compete with co-located workloads."
      },
      "project": "APP_KEY shared across every app container — required for session validity behind the L7 balancer at HA scale. corePackage: SERIOUS moves the project balancer, logging, and metrics onto dedicated infrastructure (no shared-tenant overhead) — essential for consistent latency at production throughput."
    }
  }
```

**What each env's commentary should cover:**
- **Role in the dev lifecycle** (AI agent workspace / remote dev / local validator / staging / small prod / HA prod) — what this env exists for.
- **What `zeropsSetup: dev` / `zeropsSetup: prod` does for THIS framework** (composer install + caches / bundle step / etc.) — where it's relevant.
- **Scaling rationale** for fields only present in this env: `minContainers: 2` (envs 4-5), `cpuMode: DEDICATED` (env 5), `mode: HA` (env 5), `corePackage: SERIOUS` (env 5).
- **Managed service role** — what THIS app uses it for (sessions/cache/queue/etc. in minimal tier collapsing to one DB).
- **Project secret** — what the framework uses it for + why it must be shared across containers.

**Comment style:**
- Explain WHY, not WHAT. Don't restate the field name. Include **contextual platform behavior** that makes the file self-contained — how fields interact, what propagates where, what happens at deploy time. The reader should never have to leave the file to understand it.
- 2-3 sentences per service (aim for the upper end — single-sentence comments consistently fail the 30% ratio on first attempt). Lines auto-wrap at 80 chars.
- No section-heading decorators (`# -- Title --`, `# === Foo ===`).
- Dev-to-dev tone — like explaining your config to a colleague.
- Reference framework commands where they add precision (`bun --hot`, `composer install --no-dev`, `config:cache`).
- **Each env's import.yaml must be self-contained — do NOT reference other envs.** Each env is published as a standalone deploy target on zerops.io/recipes; users land on one env's page, click deploy, and never see the others. Phrases like "same as env 0", "Consider HA (env 5) for higher durability", "zsc execOnce is a no-op here but load-bearing in env 4" are meaningless out of context. Explain what THIS env does and why, without comparing to siblings.

**Refining one env**: call `generate-finalize` again with only that env's entry under `envComments` — other envs are left untouched. Within an env, passing a service key with an empty string deletes its comment. Passing an empty project string leaves the existing project comment.

### Step 1b: Pass `projectEnvVariables` if the recipe needs project-level env vars (dual-runtime or any framework with cross-service URL constants)

`projectEnvVariables` is a first-class input to `generate-finalize` alongside `envComments`. It bakes per-env `project.envVariables` declarations into every deliverable import.yaml. Merge semantics match envComments: atomic per-env replace, omitted env untouched, empty map clears — so the second run of `generate-finalize` (after any fix) is byte-identical. **Do NOT hand-edit the generated import.yaml to add project envVariables; the next render wipes them.** v5 hit this exact bug.

The dual-runtime URL constants live here. Shape:

- **Envs 0-1** (dev-pair): `DEV_*` + `STAGE_*` for every role.
- **Envs 2-5** (single-slot): `STAGE_*` only, with hostnames `api`/`app` instead of `apistage`/`appstage`.

The values MUST match what the agent set on the workspace project via `zerops_env project=true` (see Provision step). Same values, same names, same pattern.

```
zerops_workflow action="generate-finalize" \
  envComments={...} \
  projectEnvVariables={
    "0": {
      "DEV_API_URL": "https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app",
      "DEV_FRONTEND_URL": "https://appdev-${zeropsSubdomainHost}.prg1.zerops.app",
      "STAGE_API_URL": "https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app",
      "STAGE_FRONTEND_URL": "https://appstage-${zeropsSubdomainHost}.prg1.zerops.app"
    },
    "1": {
      "DEV_API_URL": "https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app",
      "DEV_FRONTEND_URL": "https://appdev-${zeropsSubdomainHost}.prg1.zerops.app",
      "STAGE_API_URL": "https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app",
      "STAGE_FRONTEND_URL": "https://appstage-${zeropsSubdomainHost}.prg1.zerops.app"
    },
    "2": {
      "STAGE_API_URL": "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app",
      "STAGE_FRONTEND_URL": "https://app-${zeropsSubdomainHost}.prg1.zerops.app"
    },
    "3": {
      "STAGE_API_URL": "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app",
      "STAGE_FRONTEND_URL": "https://app-${zeropsSubdomainHost}.prg1.zerops.app"
    },
    "4": {
      "STAGE_API_URL": "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app",
      "STAGE_FRONTEND_URL": "https://app-${zeropsSubdomainHost}.prg1.zerops.app"
    },
    "5": {
      "STAGE_API_URL": "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app",
      "STAGE_FRONTEND_URL": "https://app-${zeropsSubdomainHost}.prg1.zerops.app"
    }
  }
```

Values are emitted verbatim; the platform resolves `${zeropsSubdomainHost}` at end-user project-import time. Single-runtime recipes without cross-service URL constants can omit `projectEnvVariables` entirely — the template just renders the shared secret on its own (unchanged behavior).

### Step 2: Review READMEs

- Root README: verify intro text matches what this recipe actually demonstrates
- Env READMEs: descriptions are auto-generated from plan data — verify accuracy

### Comment style (applies to both envComments and zerops.yaml fragments)

Recipes are read by both humans and AI agents. Write like a senior dev explaining their config to a colleague — not documentation, not tutorials.

**Voice — three dimensions of a good comment:**
1. **WHY this choice** + consequence: "CGO_ENABLED=0 produces a fully static binary — no C libraries linked at runtime" (not "Set CGO_ENABLED to 0")
2. **HOW the platform works here** — contextual behavior that makes the file self-contained, so the reader never has to leave to understand what's happening: "project-level — propagates to all containers automatically", "priority 10 — starts before app containers so migrations don't hit an absent database", "buildFromGit clones this repo and runs the matching zeropsSetup's build pipeline". Include this whenever a field's effect isn't obvious from its name alone.
3. **NOT the WHAT** — never restate the field name or its value. The reader can see `base: php@8.4`; they can't see that project envVariables propagate to child services.

- Direct, concise, no filler ("Install production deps only" not "In this step we will install the production dependencies")
- Use dashes for asides — not parentheses, not semicolons
- One thought per comment line, flow naturally with the YAML structure

**Comment shape — match existing recipes exactly:**
- 1-2 lines per comment block, ~50-60 chars wide (natural prose, not compressed)
- Above the key, not inline (exception: short value annotations like `DB_NAME: db  # matches PostgreSQL hostname`)
- Multi-line comments for decisions: explain the choice and its consequence in flowing sentences
- Group a 2-3 line comment block before a logical section, then let the config breathe
- Never exceed 70 chars per comment line (existing recipes peak at 75, average 53)

**Example of correct style** (from go-hello-world):
```yaml
    # CGO_ENABLED=0 produces a fully static binary — no C compiler
    # or system libraries linked at runtime. lib/pq is pure Go
    # so this is safe and results in a portable artifact.
    envVariables:
      CGO_ENABLED: "0"
    buildCommands:
      # Download all module dependencies, then build both the
      # app server and the database migration binary.
      - go mod download
```

**Anti-patterns:**
- Don't restate the key name ("# Set the build base" on `base: php@8.4`)
- Don't write generic descriptions ("# This is the build section")
- Don't add section-heading comments with decorators (`# -- Dev Runtime --`, `# === Database ===`, `# ----------`) — the YAML structure itself provides grouping. Comments explain decisions, not label sections.
- Don't use "we" or "you" excessively
- Don't explain YAML syntax itself
- Don't write single-word comments ("# dependencies", "# port")
- Don't compress to telegraphic style ("# static bin, no C" — write full sentences)

### Step 3: Complete

```
zerops_workflow action="complete" step="finalize" attestation="Comments provided via generate-finalize; all 6 import.yaml files regenerated with comments baked in"
```
</section>

<section name="close">
## Close — Verify (always) & Publish (only when asked)

Recipe creation is complete. The close step has THREE parts, run in order:

1. **1a — Static code review sub-agent (ALWAYS run, regardless of publish request).** A framework-expert sub-agent reviews the code, reports findings, applies fixes. NO browser walk inside this sub-agent — it never calls `zerops_browser` or `agent-browser`.
2. **1b — Main agent browser walk (ALWAYS run for showcase — skip for minimal).** After the sub-agent exits, the main agent performs the browser verification itself by calling the `zerops_browser` MCP tool (see Step 4c). This split is structural: browser work competes with dev processes and the sub-agent's tool calls for the zcp container's fork budget, and v5 proved that fork exhaustion kills everything in flight (the sub-agent's completed static review was nearly lost). Main agent runs single-threaded; `zerops_browser` auto-wraps lifecycle and auto-recovers from fork exhaustion.
3. **2 — Export & publish (ONLY when the user explicitly asks).** If the user did not request publishing, stop after 1a + 1b and any fixes are applied.

Do NOT skip 1a or 1b to save time. Do NOT publish without an explicit user request.

### 1a. Static Code Review Sub-Agent (ALWAYS — mandatory)

Spawn a sub-agent as a **{framework} code expert** — not a Zerops platform expert. The sub-agent has NO Zerops context beyond what's in its brief: no injected guidance, no schema, no platform rules, no predecessor-recipe knowledge. Asking it to review platform config (zerops.yaml, import.yaml, zeropsSetup, envReplace, etc.) invites stale or hallucinated platform knowledge and framework-shaped "fixes" to platform problems. The main agent already owns platform config (injected guidance + live schema validation at finalize); the sub-agent's unique contribution is **framework-level code review** the main agent and automated checks cannot catch.

**The sub-agent does NOT open a browser.** Browser verification (1b below) is the main agent's job. Splitting code review from browser walk is structural: browser work on the zcp container competes with dev processes and the sub-agent's tool calls for the fork budget, and v5 proved that fork exhaustion during browser walk kills the sub-agent mid-run and can cascade to the parent chat. Static review is capability-bounded; browser walk is state-bounded; they belong to different actors.

The brief below is split into three explicit halves: direct-fix scope (framework code), symptom-only scope (observe and report; do NOT propose platform fixes), and out-of-scope (never touch).

**Sub-agent prompt template:**

> You are a {framework} expert reviewing the CODE of a Zerops recipe. You have deep knowledge of {framework} but NO knowledge of the Zerops platform beyond what's in this brief. Do NOT review platform config files (zerops.yaml, import.yaml) — the main agent has platform context and has already validated them against the live schema. Your job is to catch things only a {framework} expert catches.
>
> **CRITICAL — where commands run:** You are operating from the **zcp orchestrator container**, not from inside the app's dev container. The paths `{appDir}/` (and any other `/var/www/{hostname}/` path) are an **SSHFS network mount** — a file bridge to the target container's `/var/www/`, not a local directory. File reads/edits via the mount are fine and expected, but **app-level commands must run on the target container via SSH**, not on zcp against the mount.
>
> The principle is about **which container's world the tool belongs to**, not about how "heavy" the command is:
>
> - **Target-side (SSH)** — anything that IS part of the app's toolchain: compilers (`tsc`, `nest build`, `go build`), type-checkers (`svelte-check`, `tsc --noEmit`), test runners (`jest`, `vitest`, `pytest`, `phpunit`), linters (`eslint`, `prettier`, `phpstan`), package managers (`npm install`, `composer install`, `pip install`), framework CLIs (`artisan`, `nest`, `rails`), and app-level `curl`/`node`/`python -c` used to hit the running app or managed services.
>   - Target-side means: the correct runtime version from the base image, the correct dependency tree installed by `build.buildCommands`, the correct env vars (including `${hostname_varName}` cross-service refs), and private-network reachability to managed services. zcp has none of these — a tool that "works" on zcp against the mount uses the wrong Node, wrong deps, wrong env, and can't reach the DB.
> - **zcp-side (run directly)** — anything that operates ON the app from outside: `zerops_browser` MCP tool (drives Chrome against the target's public subdomain URL — the target container doesn't have Chrome; the tool is only available inside the ZCP container and is NOT accessible to you as a sub-agent anyway), other `zerops_*` MCP tools (platform API), Read/Edit/Write against the mount, `ls`/`cat`/`head`/`tail`/`grep`/`rg`/`find` for filesystem inspection, `git status`/`add`/`commit`.
>
> Correct shape for target-side commands:
>
> ```
> ssh {hostname} "cd /var/www && {command}"   # correct — runs where the app lives
> cd /var/www/{hostname} && {command}          # WRONG — runs on zcp against the mount
> ```
>
> If you see `fork failed: resource temporarily unavailable` or `pthread_create: Resource temporarily unavailable` from any Bash call, you've been running target-side commands on zcp via the mount — zcp's process budget is sized for orchestration, not compilation, and it runs out fast. Stop, re-run them via `ssh {hostname} "…"`, and treat the failure as a wrong-container execution mistake, not a framework or platform bug.
>
> **Read and review (direct fixes allowed):**
> - All source files in {appDir}/ — controllers, services, models, entities, migrations, modules, templates/views, client-side code, routes, middleware, guards, pipes, interceptors, event handlers
> - Framework config: `tsconfig.json`, `nest-cli.json`, `vite.config.*`, `svelte.config.*`, `package.json` dependencies and scripts, lint config (but NOT the Zerops-managed `zerops.yaml`)
> - `.env.example` — are all required keys present with framework-standard names?
> - Test files — do they exercise the feature sections, or are they scaffold leftovers?
> - README **framework sections** only — what the app does, how its code is wired. Do NOT review the README's zerops.yaml integration-guide fragment — that's platform content the main agent owns.
>
> **Framework-expert review checklist:**
> - Does the app actually work? Check routes, views, config, migrations, framework conventions (env mode flag, proxy trust, idiomatic patterns, DI order, middleware ordering, async boundaries, error propagation).
> - Is there dead code, unused dependencies, missing imports, scaffold leftovers?
> - Are framework security patterns followed? (XSS-safe templating, input validation, auth middleware order, secret handling)
> - Does the test suite match what the code does?
> - Are framework asset helpers used correctly (not inline CSS/JS when a build pipeline exists)?
>
> **Do NOT call `zerops_browser` or `agent-browser`.** Browser verification is a separate phase run by the main agent after this static review completes. You have no reason to launch Chrome: you're a code reviewer, not a user-flow tester. If your review of the code raises a question that would require a browser to answer ("does this controller's error envelope actually reach the frontend?", "does the CORS middleware accept the appstage origin at runtime?"), report it as a `[SYMPTOM]` with the specific evidence you'd expect to see and stop — the main agent will verify it in the browser walk.
>
> **Symptom reporting (NO fixes):**
> If anything in the browser walk points to a platform-level cause (wrong service URL, missing env var, CORS failure, container misrouting, deploy-layer issue), STOP and report the symptom. Do NOT propose `zerops.yaml`, `import.yaml`, or platform-config changes. The main agent has full Zerops context and will fix platform issues. Your report on a platform symptom should be shaped like: "appstage's console shows `Failed to fetch https://api-20fe-3000.prg1.zerops.app/status`. This URL appears to target a service named `api` which doesn't exist in the running environment (only `apidev` and `apistage` do). Platform root cause unclear — main agent to investigate."
>
> **Out of scope (do NOT review, do NOT propose fixes for):**
> - `zerops.yaml` fields — `build.base`, `run.base`, `healthCheck`, `readinessCheck`, `deployFiles`, `buildFromGit`, `zeropsSetup`, `envReplace`, `envSecrets`, `cpuMode`, `mode`, `priority`, `verticalAutoscaling`, `minContainers`, `corePackage`, anything prefixed with `zsc`
> - `import.yaml` fields — any of them, in any of the 6 environment files
> - Service hostname naming, suffix conventions, env-tier progression
> - Env var cross-service references (`${hostname_varname}`)
> - Schema-level field validity
> - Comment ratio or comment style in platform files
> - Service type version choice
> - Any Zerops platform primitive you haven't seen before — don't guess, don't invent new ones (e.g., don't suggest a new `setup:` name), don't suggest fixes that would introduce them
>
> Report issues as:
>   `[CRITICAL]` (breaks the app), `[WRONG]` (incorrect code but works), `[STYLE]` (quality improvement), `[SYMPTOM]` (observed behavior that might have a platform cause — main agent to investigate, no fix proposed).

Apply any CRITICAL or WRONG fixes the sub-agent reported, then **redeploy** to verify the fixes work:
- If zerops.yaml or app code changed: `zerops_deploy targetService="appdev" setup="dev"` (API-first: also redeploy apidev) then cross-deploy to stage
- If only import.yaml (finalize output) changed: re-run finalize checks
- Do NOT skip redeployment — the browser walk in 1b is meaningless if fixes aren't tested.

### 1b. Main Agent Browser Walk (showcase only — MANDATORY; skip for minimal)

After 1a completes and any redeployments have settled, run the same 3-phase browser walk you ran at deploy Step 4c: Phase 1 (dev walk while dev processes are running) → Phase 2 (kill dev processes via SSH) → Phase 3 (stage walk after dev processes are dead). See deploy **Step 4c: Browser verification** for the full rules, the `zerops_browser` tool usage, the command vocabulary, and the `forkRecoveryAttempted` recovery procedure — they are unchanged at close.

**Close-specific rules** (on top of the deploy-step rules):

- Do NOT delegate browser work to a sub-agent. The 1a static review sub-agent explicitly forbids `zerops_browser` (v5 proved fork exhaustion during a sub-agent's browser walk kills the parent chat). Main agent runs single-threaded.
- Do NOT call `zerops_workflow action="complete" step="close"` until `zerops_browser` has returned clean output (`errorsOutput` empty, all sections populated, `forkRecoveryAttempted: false`) for BOTH the dev walk AND the stage walk AND any regressions surfaced have been fixed and re-verified.
- If a walk surfaces a problem: the tool has already closed the browser, so fix on mount, redeploy the affected target, re-call `zerops_browser` for the affected subdomain. This counts toward the 3-iteration close-step limit.

### 2. Export & Publish (ONLY when the user asks)

If the user did not explicitly request publishing (e.g. "create recipe" by itself), skip this section entirely and complete the close step. Publishing creates GitHub repos and opens PRs — side effects the user did not request.

**Export archive** (for debugging/sharing):

Single-runtime recipe (one codebase):
```
zcp sync recipe export {outputDir} --app-dir /var/www/appdev --include-timeline
```

Dual-runtime recipe (API-first — repeat `--app-dir` for every distinct codebase). Which directories to include depends on `worker.sharesCodebaseWith`:

- **Dual-runtime + shared worker** (worker shares the API): `apidev` + `appdev` (two `--app-dir`).
- **Dual-runtime + separate worker** (3-repo case, default): `apidev` + `appdev` + `workerdev` (three `--app-dir`).
- **Single-app + separate worker**: `appdev` + `workerdev` (two `--app-dir`).
- **Single-app + shared worker** (Laravel/Rails/Django): `appdev` only (the worker lives in the same zerops.yaml).

```
zcp sync recipe export {outputDir} \
  --app-dir /var/www/apidev \
  --app-dir /var/www/appdev \
  --include-timeline
```

Each `--app-dir` is packed into its own subdirectory inside the archive (named by `basename`), so `apidev/` and `appdev/` land side by side next to the `environments/` folder. If two `--app-dir` values have the same basename, export fails with a duplicate error — rename one or pass a parent path.

If TIMELINE.md is missing, the command returns a prompt — write the TIMELINE documenting the session, then run export again.

**Create app repo and push source**:

Currently the publish CLI creates a single `{slug}-app` repo. For a dual-runtime showcase, users land on one repo containing multiple codebases as top-level subdirectories (`apidev/`, `appdev/`, `workerdev/`):

```
zcp sync recipe create-repo {slug}
zcp sync recipe push-app {slug} /var/www/{primary-mount}
```

Where `{primary-mount}` is the top-level mount that contains all codebases as subdirectories (typically `appdev` for a single-repo layout, or a wrapper directory created explicitly for publishing). Multi-repo publish (one GitHub repo per codebase) is tracked as a future CLI extension — the current scope publishes a single `{slug}-app` repo regardless of codebase count.

**Publish environments** to `zeropsio/recipes`:
```
zcp sync recipe publish {slug} {outputDir}
```
Commits all environment folders as a PR on `zeropsio/recipes/{slug}/`.

**Push knowledge** (README fragments) to the app repo:
```
zcp sync push recipes {slug}
```

**After PR is merged**, clear Strapi cache and pull:
```
zcp sync cache-clear {slug}
zcp sync pull recipes {slug}
```

### 3. Completion
```
zerops_workflow action="complete" step="close" attestation="Recipe verified by {framework} expert sub-agent, {N} issues found and fixed"
```

Or skip if not publishing now:
```
zerops_workflow action="skip" step="close" reason="Will publish later"
```
</section>
