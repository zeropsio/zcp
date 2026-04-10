# Recipe Workflow

Create a Zerops recipe: a deployable reference implementation with 6 environment tiers and structured documentation.

<section name="research-minimal">
## Research — Recipe Plan

Fill in all research fields by examining the framework's documentation and existing recipes.

### What type of recipe?

| Type | Slug pattern | Example | Key characteristic |
|------|-------------|---------|-------------------|
| **1. Runtime hello world** | `{runtime}-hello-world` | `go-hello-world` | Raw HTTP + SQL, no framework. Simplest possible app. |
| **2a. Frontend static** | `{framework}-hello-world` | `react-hello-world` | Builds to HTML/CSS/JS, `run.base: static`. No DB. |
| **2b. Frontend SSR** | `{framework}-hello-world` | `nextjs-hello-world` | SSR framework (Next.js, Nuxt, etc.) with DB. |
| **3. Backend framework** | `{framework}-minimal` | `laravel-minimal` | Framework with ORM, migrations, templates. |
| **4. Showcase** | `{framework}-showcase` | `laravel-showcase` | Full dashboard, 5+ feature sections, worker, all services. |

### Reference Loading
Load knowledge from lower-tier recipes that already exist for your runtime and framework. Each tier builds on the previous:

**1. Hello-world** (platform knowledge): proven zerops.yaml patterns, runtime gotchas, base image details.

```
zerops_knowledge recipe="{hello-world-slug}"
```

How to pick the slug:

- **Dynamic-runtime frameworks** (backends, SSR): use the **runtime base**, not the framework name. Examples: php-nginx framework → `php-hello-world`; nodejs framework → `nodejs-hello-world`; bun → `bun-hello-world`; go → `go-hello-world`.
- **Static-frontend frameworks** (SPAs, static-site generators): there is **no generic `static-hello-world`** — the runtime is Nginx with no framework context. Static hello-worlds are named by **framework**, typically `{framework}-static-hello-world` (e.g. `react-static-hello-world`, `vue-static-hello-world`, `angular-static-hello-world`, `nextjs-static-hello-world`, `sveltekit-static-hello-world`). A few legacy ones drop the `-static-` segment (e.g. `svelte-hello-world`). Pick the one matching the framework you're building.
- **Dual-runtime (API-first) recipes**: load BOTH — the backend framework's runtime hello-world AND the frontend framework's static hello-world. The frontend static hello-world is what teaches the serve-only prod / toolchain-bearing dev pattern your frontend needs.

**2. Minimal** (framework knowledge, if building a showcase): if a `{framework}-minimal` recipe exists, load it — it contains framework-specific gotchas, integration steps, and zerops.yaml patterns you should extend:
```
zerops_knowledge recipe="{framework}-minimal"
```
Skip this if building a minimal recipe (you ARE the minimal).

Your job is to extend this accumulated base with the NEW knowledge your tier adds. For minimal: framework-specific additions on top of the hello-world (ORM, migrations, templates). For showcase: additional services on top of minimal (cache, NATS broker, storage, search, mail, workers).

**Stop after loading.** Framework-specific discoveries (documentRoot, trusted-proxy, middleware) come from the framework's own docs, not Zerops knowledge. The generate step automatically injects the full predecessor recipe plus earlier ancestors' gotchas — you don't need to memorize everything from the research load.

> **Note**: at the generate step, the system automatically injects knowledge from lower-tier recipes (full content from the direct predecessor, gotchas from earlier tiers). The research load is for filling the plan form — the system handles the rest.

### Framework Identity
- **Service type** (from available stacks): match against live catalog
  - Runtime hello world: the bare runtime (e.g., `go@1`, `bun@1`)
  - Frontend static: `static` for prod, but `nodejs@22` (or similar) for build base
  - Frontend SSR: the SSR runtime (e.g., `nodejs@22`)
  - Backend framework: the framework's runtime (e.g., `php-nginx@8.4`, `nodejs@22`)
- **Package manager**: npm, yarn, pnpm, bun, composer, pip, cargo, go mod
- **HTTP port**: the port the app listens on (not applicable for `run.base: static`)

### Build & Deploy Pipeline
- **Build commands**: ordered list (e.g., `npm install`, `npm run build`)
- **Deploy files**: what to deploy (`.` for dev, build output dir for prod)
  - Static frontend: build output (e.g., `dist/~`, `build/~`, `.next/~`)
  - Runtime/framework: varies by language
- **Start command**: the RUN command (not build).
  - Leave **empty** for implicit webserver types (php-nginx, php-apache, nginx, static) — server auto-starts.
  - Static frontends: empty (nginx serves the files)
  - Runtime hello world: the app binary/entrypoint
- **Cache strategy**: directories to cache between builds (e.g., `node_modules`, `vendor`)

### Database & Migration
- **DB driver**: mysql, postgresql, sqlite, mongodb, **none**
  - Static frontends (type 2a): set `none` — no database
  - All others: typically postgresql
- **Migration command**: framework-specific (e.g., `php artisan migrate`). Raw SQL for runtime hello world.
- **Seed command**: data seeding command (mandatory for recipes with a database — the dashboard must show real data on first deploy, not empty states)

### Environment & Secrets
- **Needs app secret**: does the framework require a generated secret key for encryption/sessions?
- **Logging driver**: stderr (preferred), file, syslog

### Decision Tree Resolution
Resolve these 5 decisions (ZCP provides defaults, you may override with justification):
1. **Web server**: builtin (Node/Go/Rust), nginx-sidecar (PHP), nginx-proxy (static)
2. **Build base**: primary runtime; add `nodejs@22` to buildBases if the framework's scaffold includes a JS asset pipeline (Vite/Webpack/Mix). **The scaffold tells you this** — don't strip the JS pipeline to avoid adding nodejs; keep the scaffold intact and add the second build base.
3. **OS**: ubuntu-22 (default), alpine (Go/Rust static binaries)
4. **Dev tooling**: hot-reload (Node/Bun), watch (Python/PHP), manual (Go/Rust/Java), none (static)
5. **Framework scaffold**: preserve what the framework's official scaffold emits (`composer create-project laravel/laravel`, `npx create-next-app`, `rails new`, `django-admin startproject`). "Minimal" in the recipe slug refers to **external services** (no Redis, no S3, DB-only), NOT to the framework's feature surface. Stripping Vite/Tailwind/ESM from a Laravel/Next.js scaffold makes the recipe non-representative: a user running the same scaffold locally will have those files and will expect them to deploy. Keep them.

### Targets
Define workspace services based on recipe type:
- **Type 1 (runtime hello world)**: app + db
- **Type 2a (frontend static)**: app only (NO database)
- **Type 2b (frontend SSR)**: app + db
- **Type 3 (backend framework)**: app + db

**Target fields**: `hostname` (lowercase alphanumeric, e.g. `app`/`db`/`cache`/`queue`), `type` (service type from live catalog — pick the highest available version for each stack), and optionally `role` (for repo routing in dual-runtime recipes: `app` or `api`). The tool dispatches rendering directly on the type — no role classification needed for template dispatch. For runtime services, if it's a background worker instead of the HTTP-serving primary app, set `isWorker: true`. Workers get a `worker` setup name (shared codebase) or `prod` (separate codebase — the default) and no subdomain; the primary app gets a `prod` setup and `enableSubdomainAccess`. For managed/utility services, `isWorker` is ignored.

**Worker-only field** — `sharesCodebaseWith`: on a worker target, names the non-worker runtime target whose codebase this worker shares (Laravel Horizon-style one-repo-two-processes pattern). Empty (default) = separate codebase with its own repo. See the "Worker codebase decision" block in the showcase research section below. Only meaningful for showcase tier; minimal recipes have no worker.

### Submission
Submit via:
```
zerops_workflow action="complete" step="research" recipePlan={...}
```
</section>

<section name="research-showcase">
## Research — Showcase Recipe (Type 4)

All base research fields (framework identity, build pipeline, database, environment, decision tree) apply — see the base research section below. This section adds showcase-specific fields and **overrides the reference loading**.

**Reference loading — load ONE recipe only (this REPLACES the hello-world + minimal loading in the base section):**
```
zerops_knowledge recipe="{framework}-minimal"
```
This is your direct predecessor and starting point. **Do NOT load the hello-world recipe.** The generate step automatically injects earlier ancestors' gotchas (hello-world tier) into your context — loading it manually wastes context with raw SQL patterns and different base images that don't apply to your framework. If you load it anyway, ignore its zerops.yaml patterns entirely; use only the minimal recipe's patterns as your template.

### Additional Showcase Fields
- **Cache library**: Redis client library for the framework (Valkey/KeyDB, used for cache + sessions ONLY — never queues)
- **Session driver**: Redis-backed session configuration
- **Queue driver**: the framework's client for the NATS broker. Default: a NATS client library for the runtime (`nats` for Node/Bun, `nats-py` for Python, `nats.go` for Go, `Rowem\Nats` or similar for PHP, etc.). **Exception**: frameworks with a first-class Redis-bound queue library where switching to NATS would be unidiomatic — Laravel Horizon, Rails Sidekiq, Django+Celery-with-Redis. In those exceptions the framework's own queue library still consumes from Redis via the framework's scheduler/dispatcher, BUT the showcase still provisions a NATS broker as the `queue` service because every showcase has a `kindMessaging` target. The messaging feature section on the dashboard uses NATS directly (pub/sub from the framework's NATS client) regardless of what the framework's own worker command polls.
- **Storage driver**: object storage integration (S3-compatible)
- **Search library**: search integration (e.g., Meilisearch, Elasticsearch)

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

Every showcase has a worker. The worker is always a separate **service**, but whether it is a separate **codebase** is an explicit research-step decision you must make and declare on the target via `sharesCodebaseWith`.

**SEPARATE codebase (default — leave `sharesCodebaseWith` empty).** The worker is its own repo (`{slug}-worker`) with its own `zerops.yaml` containing `dev` + `prod` setups. The import gets its own `workerdev` + `workerstage` pair in envs 0-1 and a bare `worker` in envs 2-5. This is the right default for: any framework consuming from a standalone broker (NATS, Kafka, RabbitMQ) — that's now the entire showcase tier; Go / Rust / generic Python or Node services where workers are typically their own binaries; any architecture where the worker has its own release cadence, dependency set, or team ownership.

**SHARED codebase (opt-in — set `sharesCodebaseWith: "{api or app hostname}"`).** One repo, two process entry points in one `zerops.yaml`: `dev`, `prod`, and a third `worker` setup. No `workerdev` service — the app's dev container hosts both processes via SSH (start the web server and the queue consumer side-by-side). The worker's base runtime MUST match the host target's (validation enforces this). This is idiomatic for:
- **Laravel + Horizon** — `php artisan horizon` is part of the same app, same Composer tree, same container boundary. Set `sharesCodebaseWith: "app"` on the worker.
- **Rails + Sidekiq** — `bundle exec sidekiq` shares the Rails app. `sharesCodebaseWith: "app"`.
- **Django + Celery (in-process)** — when the Celery worker imports from the Django app directly. `sharesCodebaseWith: "app"`.
- **NestJS + BullMQ (same-repo processor)** — if you're genuinely running the processor from the same NestJS codebase as the API. `sharesCodebaseWith: "api"`.

**Rule of thumb**: if the worker and the app are compiled from the same `package.json` / `composer.json` / `pyproject.toml` / `go.mod` AND the worker is started by a command that loads the app's bootstrap (`artisan`, `rails runner`, `python manage.py`, `nest start`), then SHARED. If the worker has its own dependency manifest or its own entry point with no app bootstrap, then SEPARATE. When in doubt, choose SEPARATE — it's the lower-coupling choice and the default.

**DO NOT** claim shared codebase just because the runtime matches. Cross-runtime sharing is rejected by validation; same-runtime-but-separate-repo is the 3-repo case (e.g. Svelte frontend + NestJS API + NestJS worker, three repos) and is fully supported — leave `sharesCodebaseWith` empty.

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

**Framework secrets**: If `needsAppSecret == true`, determine during research whether the secret is used for encryption/sessions (shared by services hitting the same DB) or is per-service.
- **Shared** (used for encryption, CSRF, session signing — any secret that multiple services must agree on): do NOT add to workspace import (see above — no `project:` allowed). After services reach RUNNING, set the value at project level with `zerops_env` **using the same preprocessor expression the deliverable uses** — zcp expands it locally via the official zParser library before calling the platform API, producing byte-for-byte the same value that the platform's own preprocessor will produce at recipe-import time:
  ```
  zerops_env project=true action=set variables=["{SECRET_KEY_NAME}=<@generateRandomString(<32>)>"]
  ```
  Because zcp uses zParser (the same library the platform uses), the workspace value and the deliverable's `project.envVariables: <@generateRandomString(<32>)>` output values with identical length, alphabet, and byte-per-char encoding. A secret that boots the app at workspace time is guaranteed to boot it at recipe-import time. Services auto-restart so the new value takes effect.

  > **Do NOT prepend `base64:` to the preprocessor expression.** Many frameworks document their shared secret in base64 form (Laravel's `APP_KEY=base64:{44chars}`, etc.) because their `key:generate` outputs that shape. The preprocessor emits a 32-char string from a URL-safe 64-char alphabet (`[a-zA-Z0-9_-.]`), which frameworks accept **directly as the raw key** — Laravel's `Encrypter::supported()` checks `mb_strlen($key, '8bit') === 32`, other AES implementations do the same. Prepending `base64:` tells the framework to DECODE the suffix, turning 32 single-byte chars into ~24 bytes, failing the cipher's fixed-length check. **`zerops_env` rejects `base64:<@...>` and `hex:<@...>` shapes to catch this at set time** — if you see that rejection, drop the prefix.

  `zerops_env set` is **upsert** — calling it with an existing key replaces the value cleanly. No delete+set dance needed if you want to change a secret. The response includes a `stored` list echoing what actually landed on the platform; read it to verify the final value shape matches your expectation (length, prefix, character set).

  For correlated secrets, encoded variants, or key pairs, call `zerops_preprocess` directly — same library, exposes batch + setVar/getVar across keys.
- **Per-service** (unique API tokens, webhook secrets): add as service-level `envSecrets` in import.yaml.

**Dual-runtime URL constants** (API-first recipes only — skip for single-runtime): after provision completes and before the generate step writes any zerops.yaml, set the project-level URL constants that the frontend and API will reference. These are derived from the known hostnames + the platform-provided `${zeropsSubdomainHost}` env var. The workspace is an env 0-1 shape (dev-pair), so set both `DEV_*` and `STAGE_*`:

```
zerops_env project=true action=set variables=[
  "DEV_API_URL=https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app",
  "DEV_FRONTEND_URL=https://appdev-${zeropsSubdomainHost}.prg1.zerops.app",
  "STAGE_API_URL=https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app",
  "STAGE_FRONTEND_URL=https://appstage-${zeropsSubdomainHost}.prg1.zerops.app"
]
```

Substitute the API's actual HTTP port (3000 is NestJS default). Static frontends have no port segment. See the "Dual-runtime URL env-var pattern" section under "zerops.yaml — Write ALL setups at once" for the full consumption pattern (frontend `${RUNTIME_STAGE_API_URL}` in `build.envVariables`, API `${STAGE_FRONTEND_URL}` in `run.envVariables`).

The workspace MUST have these set before zerops.yaml is written — otherwise cross-service refs resolve to literal placeholder strings and every CORS/API-URL reference silently fails. The same values must also be passed as `projectEnvVariables` to `generate-finalize` at finalize time, with the env 0-1 / env 2-5 shape split applied — see the finalize step for details.

Follow the injected **import.yaml Schema** for the three env var levels (project envVariables, service envSecrets, zerops.yaml run.envVariables).

Follow the injected **import.yaml Schema** for all field rules (hostname conventions, priority, mode, preprocessor syntax). Recipe-specific validation:

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

### 4. Discover env vars (mandatory before generate — skip if no managed services)

After services reach RUNNING, discover actual env vars:
```
zerops_discover includeEnvs=true
```

Returns keys and annotations only (keys only — sufficient for validating env var names).

**If the plan has no managed services** (type 2a static frontend): skip this step — there are no env vars to discover.

Record which env vars exist. **ONLY use variables that were actually discovered** — guessing names causes silent runtime failures (`${...}` becomes a literal string, not an error). Service-specific variable names are in the injected service reference cards.

### Completion
```
zerops_workflow action="complete" step="provision" attestation="Services created: {list}. Env vars cataloged for zerops.yaml wiring (not yet active as OS vars — activate after deploy): {list}. Dev mounted at /var/www/appdev/"
```
</section>

<section name="generate">
## Generate — App Code & Configuration

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

### WHERE to write files

**Single-runtime** (full-stack): Write all source code, zerops.yaml, and README to `/var/www/appdev/`.

**Dual-runtime** (API-first showcase): Write API code to `/var/www/apidev/` and frontend code to `/var/www/appdev/`. Each has its own zerops.yaml, package.json, and source tree. The API's README.md contains the integration guide (it documents the showcased framework).

**Use SSHFS for file operations**, SSH for commands that use the **base image's built-in tools** (e.g., `composer create-project` on php-nginx, `git init`).
Files placed on the mount are already on the dev container — deploy doesn't "send" them, it triggers a build from what's already there.

**Scaffold each codebase in its own mount — never cross-contaminate.** Framework scaffolders (`sv create`, `npx create-vite`, `nest new`, `composer create-project`, `django-admin startproject`) write config files (`tsconfig.json`, `package.json`, `.npmrc`, `.vscode/`, `.gitignore`) into whatever directory they run from. Running a scaffold from the wrong container or the wrong working directory overwrites the host codebase's config silently. For dual-runtime:
- `cd /var/www/apidev && nest new .` for the API — runs on the `apidev` service's SSH session
- `cd /var/www/appdev && npm create vite@latest . -- --template svelte` for the frontend — runs on the `appdev` service's SSH session (if the static container lacks Node, scaffold files directly via SSHFS write instead of invoking a scaffolder on the container)

Never scaffold into `/tmp` and copy — the scaffolder's footprint always includes hidden files you'll miss. Never run a frontend scaffolder from an API SSH session targeting the API mount — `sv create` invoked from `apidev` SSH will overwrite apidev's `tsconfig.json` and `package.json` even if you `cd` to a different directory first, because scaffolders trust the process working directory as the project root.

### What to generate per recipe type

**Type 1 (runtime hello world):** Raw HTTP server with a single file. DB connection via standard library. Raw SQL migration for a `greetings` table. No framework, no ORM.

**Type 2a (frontend static):** SPA/static site. Framework project (React/Vue/Svelte) with a simple page showing framework name, greeting, and environment indicator. Build-time env var injection. No DB connection.

**Type 2b (frontend SSR):** SSR framework project (Next.js/Nuxt/SvelteKit). Server-rendered pages with DB connection. Framework's API routes for health endpoint.

**Type 3 (backend framework):** Full framework project. ORM-based migrations, template-rendered dashboard, framework CLI tools. Uses the framework's conventions throughout.

**Type 4 (showcase):** Dashboard **SKELETON only** — feature controllers and views are **NOT** written during generate. Generate produces: layout with empty/placeholder partial slots (using the framework's standard include mechanism — partials, components, sub-templates, or imports) for each planned feature section, all routes (display + action endpoints pre-registered but returning placeholder responses), primary model + migration + factory + seeder with sample data, service connectivity panel, zerops.yaml (all 3 setups: dev + prod + worker), README with fragments, .env.example. **Stop here.** The deploy step dispatches a sub-agent to implement feature controllers and views against live services after appdev is verified. Writing feature code during generate means generating blind against disconnected services — producing code with no error handling, no XSS protection, and untested integrations. See "Showcase dashboard — file architecture" below.

### Two kinds of import.yaml (critical distinction)

1. **Workspace import** (provision step) — creates the agent's dev/stage infrastructure. NO `zeropsSetup`, NO `buildFromGit`. Services use `startWithoutCode` (dev) or wait for deploy (stage).
2. **Recipe import** (finalize step) — the 6 deliverable files for end users. Uses `zeropsSetup: dev`/`zeropsSetup: prod` + `buildFromGit` to map hostnames to setup names.

zerops.yaml ALWAYS uses **generic setup names**: `setup: dev` and `setup: prod`. During workspace deploy, the `zerops_deploy` tool's `setup` parameter maps the service hostname to the correct setup name (e.g. `targetService="appdev" setup="dev"`). In recipe import.yaml, `zeropsSetup: dev`/`zeropsSetup: prod` does the same mapping for `buildFromGit` deploys.

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

#### Dual-runtime URL env-var pattern — the canonical solution

Every service in a Zerops project has a **deterministic public URL from the moment the project exists**, derived from three knowns:

- the service's `${hostname}` (declared in import.yaml)
- the project-scope `${zeropsSubdomainHost}` env var (generated by the platform at project creation — available everywhere, always)
- the service's HTTP port (omitted for static services)

URL format is a platform constant:

```
# dynamic runtime on port N:
https://{hostname}-${zeropsSubdomainHost}-{port}.prg1.zerops.app

# static (Nginx, no port segment):
https://{hostname}-${zeropsSubdomainHost}.prg1.zerops.app
```

Because `${zeropsSubdomainHost}` is already a project-scope env var, the URL can be **declared once as a project-level env var at import time**, used everywhere, and the platform resolves `${zeropsSubdomainHost}` at project import time.

**Canonical pattern — two env var name families per dual-runtime recipe**:

- `STAGE_{ROLE}_URL` — the public URL of the "stage" (end-user-facing) slot. In env 0-1 this resolves to `apistage`/`appstage`; in envs 2-5 this resolves to `api`/`app` (single-container prod slot). Present in **every env** (0-5).
- `DEV_{ROLE}_URL` — the dev-pair slot's URL (`apidev`/`appdev`). Only exists in env 0-1 (dev-pair envs). Omitted in envs 2-5 where there is no dev slot.

Typical roles for dual-runtime recipes: `API`, `FRONTEND`. Add a third role (e.g. `WORKER`) only if the worker has a public surface (usually it doesn't).

**Env 0-1 shape** (dev-pair envs — `STAGE_*` + `DEV_*`):
```yaml
# in import.yaml for env 0 and env 1
project:
  envVariables:
    DEV_API_URL: https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app
    DEV_FRONTEND_URL: https://appdev-${zeropsSubdomainHost}.prg1.zerops.app
    STAGE_API_URL: https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app
    STAGE_FRONTEND_URL: https://appstage-${zeropsSubdomainHost}.prg1.zerops.app
```

**Envs 2-5 shape** (single-slot envs — `STAGE_*` only):
```yaml
# in import.yaml for envs 2, 3, 4, 5
project:
  envVariables:
    STAGE_API_URL: https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app
    STAGE_FRONTEND_URL: https://app-${zeropsSubdomainHost}.prg1.zerops.app
```

Port `3000` is the NestJS default — substitute your API's actual HTTP port (from `run.ports[0].port` in the API's zerops.yaml). Static frontends have no port segment.

**Consuming in frontend zerops.yaml — `build.envVariables` with the `RUNTIME_` lift**:

Project-level env vars are runtime-scope by default (available as OS env vars in every service's runtime container). To use them inside `build.envVariables`, reference with the `RUNTIME_` prefix, which lifts the runtime-scope var into the build container's env:

```yaml
zerops:
  - setup: prod
    build:
      base: nodejs@22
      envVariables:
        VITE_API_URL: ${RUNTIME_STAGE_API_URL}   # bakes stage URL into the cross-deployed bundle
      buildCommands:
        - npm ci
        - npm run build
      deployFiles:
        - dist/~
    run:
      base: static

  - setup: dev
    build:
      base: nodejs@22
      envVariables:
        VITE_API_URL: ${RUNTIME_DEV_API_URL}     # bakes dev URL into the iteration bundle
      buildCommands:
        - npm install
      deployFiles: [.]
    run:
      base: nodejs@22                            # serve-only base override — Fix from the setup:dev rule
```

The same zerops.yaml works in every env: envs 0-1 use `setup: dev` (via `zeropsSetup: dev` on appdev) which reads `DEV_*`; every env uses `setup: prod` (via `zeropsSetup: prod` on appstage/app) which reads `STAGE_*`. The `setup: dev` block is never invoked in envs 2-5 (there's no appdev there), so the `DEV_*` reference is dormant there — safe.

**Consuming in API zerops.yaml — `run.envVariables` direct reference**:

For runtime consumption, project-level env vars are already in the OS env at container start. If the framework needs a specific env var name (e.g. `FRONTEND_URL` for CORS allow-list), forward the project var by name in `run.envVariables` — no `RUNTIME_` prefix:

```yaml
zerops:
  - setup: prod
    run:
      envVariables:
        FRONTEND_URL: ${STAGE_FRONTEND_URL}      # stage's frontend origin
        APP_URL: ${STAGE_API_URL}                # stage's own public URL

  - setup: dev
    run:
      envVariables:
        FRONTEND_URL: ${DEV_FRONTEND_URL}        # dev pair's frontend origin
        APP_URL: ${DEV_API_URL}                  # dev pair's own public URL
```

Do NOT shadow a project var with a same-name service-level var (`FRONTEND_URL: ${FRONTEND_URL}` is a shadow loop). Forward it under a different, framework-conventional name (as above).

**Workspace parity — set the same project env vars on the workspace via `zerops_env`**:

The workspace IS a dev-pair env (env-0 shape — has appdev/apidev/appstage/apistage). The workspace `import.yaml` cannot contain a `project:` section (the platform rejects it — see the Provision step), so the agent sets project env vars with `zerops_env project=true` **immediately after provision completes**, before writing zerops.yaml:

```
zerops_env project=true action=set variables=[
  "DEV_API_URL=https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app",
  "DEV_FRONTEND_URL=https://appdev-${zeropsSubdomainHost}.prg1.zerops.app",
  "STAGE_API_URL=https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app",
  "STAGE_FRONTEND_URL=https://appstage-${zeropsSubdomainHost}.prg1.zerops.app"
]
```

`zerops_env` stores the values verbatim including the `${zeropsSubdomainHost}` interpolation marker. When a service starts, the platform resolves `${zeropsSubdomainHost}` to the project's actual subdomain host before injecting the value as an OS env var. Every service sees the fully-resolved URL at container start. Without this step, cross-service refs in zerops.yaml (`FRONTEND_URL: ${STAGE_FRONTEND_URL}`) resolve to the literal string `${STAGE_FRONTEND_URL}` in the workspace — this was v5's post-close CORS regression.

Single-runtime recipes (non-dual-runtime) can skip this — they don't cross services for URL baking. Dual-runtime recipes MUST set these before the generate step writes zerops.yaml that references them.

**For the 6 deliverable import.yaml files** (generated at finalize): pass `projectEnvVariables` as a first-class input to `zerops_workflow action="generate-finalize"`:

```
zerops_workflow action="generate-finalize" \
  projectEnvVariables={
    "0": {
      "DEV_API_URL": "https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app",
      "DEV_FRONTEND_URL": "https://appdev-${zeropsSubdomainHost}.prg1.zerops.app",
      "STAGE_API_URL": "https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app",
      "STAGE_FRONTEND_URL": "https://appstage-${zeropsSubdomainHost}.prg1.zerops.app"
    },
    "1": { /* identical to env 0 */ },
    "2": {
      "STAGE_API_URL": "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app",
      "STAGE_FRONTEND_URL": "https://app-${zeropsSubdomainHost}.prg1.zerops.app"
    },
    "3": { /* identical to env 2 */ },
    "4": { /* identical to env 2 */ },
    "5": { /* identical to env 2 */ }
  } \
  envComments={...}
```

Do NOT hand-edit the 6 generated import.yaml files to add `project.envVariables` after the fact. A second `generate-finalize` call (for any reason — comment fix, check failure) re-renders from template and wipes manual edits. Always pass `projectEnvVariables` via the tool input; it's idempotent across reruns.

**Deeper reference**: the platform's rules for lifting project-scope env vars into build context (`RUNTIME_` prefix), the full `${zeropsSubdomainHost}` URL format, and the workspace-vs-deliverable parity pattern are documented in the `environment-variables` knowledge guide. Fetch it via `zerops_knowledge scope="guide" query="environment-variables"` when you need the platform-level rules behind this pattern, not just the recipe-level instructions above.

**What NOT to do** (all seen in v4/v5):
- Do NOT invent a `setup: stage` — there is no such thing. Stage uses `setup: prod`.
- Do NOT set `RUNTIME_VITE_API_URL` on a source service (e.g. `appdev`) via `zerops_env` and expect it to propagate through cross-deploy to a different target. Cross-deploys build in the target's context, not the source's.
- Do NOT write `project.envVariables` values that reference another service's `${hostname}_zeropsSubdomain` — use the `${zeropsSubdomainHost}` project-scope var and the constant URL format instead.
- Do NOT create a service-level env var with the same name as a project-level env var — that's a shadow loop.

Follow the injected chain recipe (working zerops.yaml from the predecessor) as the primary reference. For hello-world (no predecessor), follow the injected zerops.yaml Schema. Platform rules (lifecycle phases, deploy semantics) were taught at provision — use `zerops_knowledge` if you need to look up a specific rule.

Recipe-specific conventions for each setup (platform rules from provision apply — these are ONLY the recipe-specific additions):

**`setup: dev`** (self-deploy from SSHFS mount — agent iterates here):
- **`setup: dev` MUST give the agent a container that can host the framework's dev toolchain** — shell, package manager, and the framework's hot-reload process (`npm run dev`, `php artisan serve`, `bun --hot`, `cargo watch`, etc.). This is what makes the dev setup iterable over SSH.
- **Dynamic runtimes** (nodejs, python, php-nginx, go, rust, bun, ubuntu, …): `run.base` is the same as prod and `deployFiles: [.]` preserves source across deploys — **MANDATORY**, anything else destroys the source tree.
- **Serve-only runtimes** (`static`, standalone `nginx`, any future serve-only base): these host no toolchain — `run.base: static` is a **prod-only concern**. For the dev setup, pick a different `run.base` that CAN host the framework's dev toolchain — typically the same base that already exists under `build.base` for that setup (e.g. `nodejs@22` for a Vite/Svelte SPA whose prod is `static`). `run.base` may differ between setups inside the same zerops.yaml; the platform supports this and it's the intended pattern for serve-only prod artifacts. `deployFiles: [.]` still applies on dev regardless of `run.base` choice.
- `start: zsc noop --silent` — exception: omit `start` for implicit-webserver runtimes (php-nginx, php-apache, nginx, static)
- **NO healthCheck, NO readinessCheck** — agent controls lifecycle; checks would restart the container during iteration
- Framework mode flags set to dev values (`APP_ENV: local`, `NODE_ENV: development`, `DEBUG: "true"`, verbose logging)
- Same cross-service refs from `zerops_discover` as prod — only mode flags differ
- **Dev dependency pre-install**: if the build base includes a secondary runtime for an asset pipeline, dev `buildCommands` MUST include the dependency install step for that runtime's package manager. This ensures the dev container ships with dependencies pre-populated — the developer (or agent) can SSH in and immediately run the dev server without a manual install step first. Omit the asset compilation step — that's for prod only; dev uses the live dev server.

**`setup: prod`** (cross-deployed from dev to stage — end-user production target):
- Follow the chain recipe's prod setup as a baseline. Adapt to the current recipe's services and framework version.
- **If a search engine is provisioned**: `initCommands` must include the framework's search index command (e.g., `php artisan scout:import "App\\Models\\Article"`) AFTER `db:seed`. The ORM's auto-index-on-create may work during seeding, but an explicit import is the safety net — if the seeder guard skips creation (records exist from a prior deploy) while the search index is empty, auto-indexing fires zero events and search returns nothing.
- **NO `prepareCommands` installing secondary runtimes** unless the prod START command needs them at runtime (e.g., SSR with Node). If the secondary runtime is only for BUILD, it's in `build.base` — adding it to `run.prepareCommands` wastes 30s+ on every container start. Dev needs `prepareCommands` for the dev server; prod does not.
- Framework mode flags set to prod values. Same cross-service ref keys as dev — **only mode flags differ**.

**`setup: worker`** (showcase only — background job processor):

Whether the worker shares the app's codebase is the research-step decision declared via `sharesCodebaseWith` on the worker target. The two shapes are:

- **Shared codebase** (`sharesCodebaseWith` set to the host target's hostname): one repo, two processes. The worker is just a different entry point. Write a `setup: worker` block in the SAME zerops.yaml as the host target (`appdev/zerops.yaml` for single-app, `apidev/zerops.yaml` for dual-runtime where the API is the host). During development, the agent starts both web server and queue consumer as SSH processes from the host target's dev container — no `workerdev` service exists.
- **Separate codebase** (`sharesCodebaseWith` empty — DEFAULT): worker has its own repo (`{slug}-worker`) with its own zerops.yaml containing `dev` + `prod` setups. Mount path is `/var/www/workerdev/`. This is the default because most showcase workers consume from a standalone broker (NATS) and have no reason to be co-located with the HTTP app. Includes the 3-repo case (app static + api runtime + worker same-runtime-but-separate-repo).

Worker-specific: `start` is mandatory (broker consumer command), NO healthCheck/readinessCheck/ports (workers don't serve HTTP). Build and envVariables typically match prod. For shared workers, the `worker` setup block inherits the host target's `build.base` and cache configuration; only the `start` command differs.

**Shared across all setups:**
- `envVariables:` contains ONLY cross-service references + mode flags. Do NOT re-add envSecrets — platform injects them automatically.
- dev and prod env maps must NOT be bit-identical — a structural check fails if mode flags don't differ.

### .env.example preservation

If the framework scaffolds a `.env.example` file (e.g., `composer create-project`), **keep it** — it documents the expected environment variable keys for local development. Remove `.env` (contains generated secrets), but preserve `.env.example` with empty values as a reference for users running locally.

Update `.env.example` to include ALL environment variables used in zerops.yaml `envVariables`. The scaffolded defaults cover standard framework keys but miss service-specific ones added for the recipe (e.g., `MEILISEARCH_HOST`, `SCOUT_DRIVER`, `AWS_ENDPOINT`). Add missing keys with sensible local defaults (e.g., `MEILISEARCH_HOST=http://localhost:7700`, `AWS_ENDPOINT=http://localhost:9000`). A user running locally with zcli VPN should be able to copy `.env.example` to `.env` and have every key present.

### Framework environment conventions

Use the framework's **standard** environment names — don't invent new ones. If the framework has a "base URL" / "app URL" env var, set it to `${zeropsSubdomain}`. The chain recipe demonstrates the correct env var names for this framework.

### Required endpoints

**Types 1, 2b, 3 (server-side):**
- `GET /` — dashboard (HTML) with interactive feature sections proving each provisioned service works
- `GET /health` or `GET /api/health` — JSON health endpoint
- `GET /status` — JSON status with actual connectivity checks (DB ping, cache ping, latency)

The dashboard is the recipe's proof of work. Each provisioned service gets a feature section that **exercises** the service — not just a connectivity dot, but a visible demonstration of the service doing real work. What to demonstrate derives from the plan targets:
- **Database** — list seeded records, create-record form (proves ORM + migrations + CRUD)
- **Cache** (if provisioned) — store a value with TTL, show cached vs fresh response (proves cache driver). **Cache is for cache + sessions only — the queue uses NATS, a separate broker service.**
- **Object storage** (if provisioned) — upload file, list uploaded files (proves S3 integration)
- **Search engine** (if provisioned) — live search across seeded records (proves search driver + indexing)
- **Messaging broker + worker** (if provisioned) — NATS pub/sub + worker consumer. Dispatch-job button publishes a message to a NATS subject; the worker is subscribed to that subject and writes the processed result to a database table or to a status key the dashboard polls. Show: (a) message sent (timestamp + subject), (b) worker-processed result appearing in the dashboard within a second or two. This proves the full NATS → worker → result round-trip, not just a queue-driver integration test.

A minimal recipe (app + db) has one feature section (database CRUD). A showcase recipe has one section per service. No section for services that aren't in the plan.

The dashboard must work immediately after one-click deploy — **verify explicitly during deploy Step 3**:
- Seeder populates sample records (15-25 items) on first deploy — no empty states. After dev deploy, open the dashboard and confirm seeded records appear in the database section. If the table is empty, the seeder failed silently — diagnose and fix before proceeding. Common cause: `zsc execOnce` marks the command as done even if it failed; check `zerops_logs` for seeder errors.
- Search index is populated (`initCommands` runs the framework's index command after `db:seed`) — search must return results for seeded content immediately, not after a manual reindex
- File storage is accessible on first visit (upload form works, no pre-configuration needed)

**Type 4 (showcase):**
Same endpoints as types 1-3, but during the generate step only the **skeleton** is written — the layout has include slots for each feature section, routes are registered, but feature controllers return placeholder responses. The deploy step's sub-agent fills them in against live services. The additional services (cache, storage, search, worker) each add a feature section to the same dashboard page. The dashboard layout is a vertical stack of feature sections — one page, every service demonstrated.

**Type 2a (static frontend):**
- `GET /` — simple page showing framework name, greeting, timestamp, environment indicator
- No server-side health endpoint (static files only)

### Dashboard style

Minimalistic, functional, demonstrative — but **polished**. Minimalistic does NOT mean unstyled browser defaults. The dashboard proves integrations work, not a marketing page, but it must be professional enough that a developer deploying the recipe isn't embarrassed by the output.

**Quality bar:**
- **Styled form controls** — never raw browser-default `<input>` / `<select>` / `<button>`. Apply the framework's scaffolded CSS (Tailwind if scaffolded) or write clean styles: padding, border-radius, consistent sizing, focus ring, hover state on buttons
- **Visual hierarchy** — section headings clearly delineated, consistent vertical rhythm between sections, data tables with proper headers, cell padding, and alternating row shading or border separators
- **Status feedback** — success/error flash after form submissions (not silent page reload), loading indicator text for async operations, meaningful empty states ("No files uploaded yet" not a blank div)
- **Readable data** — tables with aligned columns and comfortable padding, timestamps in human-readable relative form ("3 minutes ago"), IDs in monospace
- System font stack, generous whitespace, monochrome palette with one accent color for interactive elements and status indicators
- Mobile-responsive via simple CSS (single column on narrow screens), not a grid framework

**What to avoid:**
- Component libraries, icon packs, animations, dark mode toggles
- JavaScript frameworks for interactivity — vanilla JS for live search debounce, form submissions via standard POST (no fetch/XHR unless the feature specifically needs it, like live search)
- Inline `<style>` blocks when a build pipeline (Tailwind/Vite) exists — use the pipeline

**XSS protection (mandatory):** ALL dynamic content rendered in HTML must be escaped. Never inject user-provided or API-returned strings via `innerHTML` or JS template literals without escaping. Use `textContent` for JS-injected text, and the framework's template auto-escaping for server-rendered content (every major framework auto-escapes by default — never use the raw/unescaped output mode). File names from S3, article titles from DB, search results — all untrusted input.

The visual benchmark: a well-formatted diagnostic page — clean, professional, usable. Not a SaaS landing page, but not a raw HTML form dump either.

### Showcase dashboard — file architecture

When the dashboard has more than 2 feature sections (showcase recipes), each section lives in **separate files** — its own controller/handler and its own view/template/partial. The main dashboard layout includes them. This isolation lets the main agent build the skeleton first, deploy and verify the base app, then dispatch a sub-agent for feature implementation.

**Skeleton boundary — what goes where:**

| Generate step (main agent) | Deploy step (sub-agent, after appdev verified) |
|---|---|
| Dashboard layout with empty partial/component slots per feature section | Feature section controllers/handlers (CacheController, StorageController, etc.) |
| Placeholder text in each slot ("Section available after deploy") | Feature section views/templates/partials with interactive UI |
| Primary model + migration + factory + seeder (15-25 records) | Feature-specific JavaScript (search debounce, file upload, polling) |
| DashboardController with index, health, status endpoints | Feature-specific model traits/mixins (e.g., Searchable) |
| Service connectivity panel (CONNECTED/DISCONNECTED per service) | |
| All routes registered (GET + POST for every feature action) | |
| zerops.yaml (all setups), README, .env.example | |

**Deploy step — main agent deploys skeleton first:**
Deploy appdev → start processes → verify. The skeleton (connectivity panel, seeded data, health endpoint) must work before adding feature sections. This catches zerops.yaml errors, missing extensions, env var typos, and migration issues BEFORE the sub-agent adds complexity.

**Deploy step — sub-agent implements features (after appdev verified):**
The main agent dispatches ONE sub-agent with a brief containing:
- Exact file paths to create (framework-conventional locations)
- Installed packages relevant to each feature
- What each section must demonstrate (from the service-to-feature mapping above)
- The **UX quality contract** from "Dashboard style" — styled controls (not browser defaults), visual hierarchy, status feedback after actions, XSS-safe dynamic content (`textContent` not `innerHTML`). Include the CSS approach (Tailwind classes if scaffolded, inline styles otherwise) and layout structure (how partials are included)
- Pre-registered route paths for each feature's actions
- **Where app-level commands run** — a hard rule, not a preference. The `{appDir}` path the sub-agent is given is an SSHFS mount on the zcp orchestrator, not a local directory. File edits against it are fine; **target-side commands (the app's own toolchain) MUST run via `ssh {devHostname} "cd /var/www && …"` on the target container**, never with `cd /var/www/{hostname} && …` on zcp. The principle is which container's world the tool belongs to: if it's the app's compiler / test runner / linter / package manager / framework CLI, it belongs on the target (correct runtime version, correct dependency tree, correct env vars, managed-service reachability, none of which zcp has). If it's `agent-browser`, `zerops_*`, or Read/Edit/Write against the mount, it belongs on zcp. Running `npx tsc`, `jest`, `npm install`, `svelte-check`, `eslint`, etc. from zcp against the mount is wrong even when it seems to work, and it exhausts zcp's fork budget producing `fork failed: resource temporarily unavailable` cascades. When in doubt, SSH.
- Instruction to **test each feature against the live service** after writing it — the sub-agent has SSH access to appdev and all managed services (db, cache, storage, search) are reachable. After writing a controller+view, hit the endpoint via `ssh {devHostname} "curl -s localhost:{port}/…"` or the framework's test runner (also via SSH) and verify it returns expected data. Fix issues immediately — this is the entire point of deferring to after deploy.

The sub-agent writes all feature controllers and views sequentially. One sub-agent, all features. Because the sub-agent runs against live services, it produces tested code with proper error handling — not blind template generation.

**Deploy step — main agent resumes (after sub-agent):**
1. Read back the feature files — verify they exist and aren't empty
2. Git add + commit on the mount
3. Redeploy appdev (self-deploy) → restart processes → verify features work
4. Continue to stage deployment (Step 5+) — stage gets the complete codebase

For minimal recipes (1-2 feature sections), skip the sub-agent — the main agent writes everything directly during generate and deploys once.

### Asset pipeline consistency

**Rule**: if `buildCommands` compiles assets (JS, CSS, or both), the primary view/template MUST load those compiled assets via the framework's standard asset inclusion mechanism. Inline `<style>` or `<script>` blocks that bypass the build output are forbidden when a build pipeline exists.

**Why**: a build step that produces assets nobody loads is dead code. Prod wastes build time on compilation that the template ignores. The dev server started in Step 2b serves nothing. The end user cloning the recipe sees a working build config but a template that doesn't use it — indistinguishable from a broken setup.

**How to verify**: if the zerops.yaml prod `buildCommands` includes an asset compilation step (any command that produces built CSS/JS in an output directory), check that the primary view/template references those outputs through the framework's asset helper. Every framework with a build pipeline has one — it's the mechanism that maps source assets to content-hashed output filenames. If you're writing inline styles instead, you've disconnected the pipeline.

This is the generate-step corollary of research decision 5 (scaffold preservation): keeping the config files but not wiring them into the template is functionally identical to stripping the pipeline.

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

### Code Quality
- Comment ratio in zerops.yaml code blocks must be >= 0.3 — **aim for 35%** to clear the threshold comfortably on the first attempt. Agents consistently underestimate; writing to 30% lands at 25%.
- No `PLACEHOLDER_*`, `<your-...>`, or `TODO` strings
- All env var references must use discovered variable names
- Comments explain WHY, not WHAT (don't restate the key name)
- Max 80 chars per comment line

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

### Completion
```
zerops_workflow action="complete" step="generate" attestation="App code and zerops.yaml written to /var/www/appdev/. README with 3 fragments."
```
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

### Writing Style — Developer to Developer

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

**Metrics:**
- Comment ratio: at least 30% of YAML config lines should have comment lines
- Target ~50-60 chars per comment line, never exceed 70
- Every non-obvious decision should have a reason
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
If `run.prepareCommands` installs a secondary runtime (e.g., `sudo -E zsc install nodejs@22`) and the scaffold defines a dev server (e.g., `npm run dev` for Vite), start it now:
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

**Step 3-API** (API-first only, runs AFTER Step 1-API + Step 2a-API, BEFORE Step 1): Enable and verify the API FIRST — this is a checkpoint before the frontend deploy, not a late verification step:
```
zerops_subdomain action="enable" serviceHostname="apidev"
zerops_verify serviceHostname="apidev"
```
Verify `/api/health` returns 200 via curl. THEN return to Step 1 to deploy appdev — the frontend needs the API running before it can deploy (in build-time-baked configurations) or before it can be verified (in runtime-config configurations). After appdev deploys, Step 2 (processes) → Step 3 (enable appdev subdomain + verify the dashboard loads and successfully fetches from the API) → Step 3a (logs from BOTH containers).

**CORS** (API-first): The API must set CORS headers allowing the frontend subdomain. Use the framework's standard CORS middleware (e.g., `@nestjs/cors`, `cors` for Express, `rs/cors` for Go). Allow the frontend's subdomain origin.

For showcase, also verify the worker is running via logs (no HTTP endpoint):
```
zerops_logs serviceHostname="appdev" limit=20
```

**Redeployment = fresh container.** If you fix code and redeploy during iteration, the platform creates a new container — ALL background processes (asset dev server, queue worker) are gone. Restart them before re-verifying. This applies to every redeploy, not just the first.

**Step 4: Iterate if needed** (max 3 iterations)
If verification fails: check logs (`zerops_logs serviceHostname="appdev"`), fix code on mount, kill previous server, restart via SSH, re-verify. After any redeploy, repeat Step 2 (start ALL processes) before Step 3 (verify).

**Step 4b: Showcase feature sections — MANDATORY for Type 4** (skip for minimal)

After appdev is deployed and verified with the skeleton (connectivity panel, seeded data, health endpoint), dispatch the feature sub-agent. **This step is MANDATORY for Type 4 showcase recipes.** If you wrote feature controllers during the generate step, you skipped the live-service testing that makes showcase features reliable. The generate step produces a skeleton only — feature code is written HERE, against running services.

The sub-agent writes code on the appdev mount and can test against live services — database, cache, storage, search are all reachable. See "Showcase dashboard — file architecture" for the sub-agent brief format.

**API-first**: The sub-agent works on BOTH codebases — API endpoints in `/var/www/apidev/`, Svelte components in `/var/www/appdev/`. Include both mount paths in the sub-agent brief. The sub-agent adds API routes (controllers, services) and corresponding frontend components (Svelte pages that fetch from the API).

After the sub-agent finishes:
1. Read back feature files — verify they exist and aren't empty
2. Git add + commit on the mount(s)
3. Redeploy: `zerops_deploy targetService="appdev" setup="dev"` (API-first: also redeploy apidev)
4. Restart ALL processes (Step 2) — redeployment creates a fresh container
5. HTTP-level feature verification (curl):
   - Each feature endpoint returns the right status code and payload shape
   - POST actions return success (not 500 errors)
   - Seeded data visible in database/search sections (tables populated, search returns results)
   - File upload works and file list populates (S3 connectivity proven)
   - Job dispatch shows processed result (queue + worker connectivity proven)

If features fail: fix on mount, redeploy, re-verify (counts toward the 3-iteration limit).

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

ZCP exposes a `zerops_browser` MCP tool that wraps `agent-browser` with a guaranteed lifecycle. It is the ONLY supported way to drive the browser from the recipe workflow. Raw `agent-browser` Bash calls are forbidden — they caused v4 and v5 to crash with `fork failed: resource temporarily unavailable` during browser verification (v5 crashed TWICE, once in the main agent and once in a sub-agent).

**Why the tool is mandatory** — the raw CLI has two failure modes the tool eliminates:

1. **Lifecycle drift.** `agent-browser` runs a persistent daemon per session holding one Chrome instance (~10 child processes). If the batch is missing `close`, or the Bash call is killed before it runs, Chrome stays alive holding the fork budget. Every subsequent Bash call then crashes with "Resource temporarily unavailable" until pkill recovery. `zerops_browser` auto-wraps `[open url] + your commands + [errors] + [console] + [close]`, so the close is guaranteed — you literally cannot forget it.
2. **Concurrency.** Two `agent-browser` invocations in parallel (or a sub-agent's call overlapping the main agent's) either race the daemon or spawn a second Chrome. The tool serializes all calls through a process-wide mutex, so concurrent MCP calls queue instead of dueling for the daemon.

On top of that, `zerops_browser` auto-runs pkill recovery if it detects fork exhaustion or a timeout, and returns `forkRecoveryAttempted: true` in the result so you know to investigate what burned the budget before retrying.

#### Non-negotiable rules

1. **Stop all background dev processes BEFORE calling `zerops_browser`.** The processes you started in Step 2 (`npm run start:dev`, `ts-node worker`, `nohup` jobs on dev containers) are NOT needed for browser verification — you're verifying STAGE, not dev iteration. Kill them explicitly on every dev container, THEN call the tool. Restart them later only if you need more dev iteration after the walk. This is the single most important rule: the tool can recover from fork exhaustion once, but it cannot make your dev processes disappear.
2. **Use `zerops_browser` — never `agent-browser` as a Bash call.** The tool is the ONLY sanctioned path. Any raw `agent-browser` / `echo ... | agent-browser batch` command in a Bash tool call is a bug.
3. **One `zerops_browser` call per subdomain.** Pass the URL + inner commands; the tool wraps open/errors/console/close. Run it once for appstage, then again for appdev. Do NOT pass multiple URLs or multiple open/close markers.
4. **Do not dispatch a sub-agent that calls `zerops_browser` while the main agent also has one in flight.** The verification sub-agent brief forbids browser usage entirely (the close step is split — see below); the main agent runs the browser walk itself.
5. **If the tool returns `forkRecoveryAttempted: true`** — pkill already ran. Before retrying, find the process that burned the budget. Usually it's a dev process you forgot to kill on a dev container (`ssh {devHostname} "ps -ef | grep -E 'nest|vite|node dist|ts-node'"`). Kill it, then call the tool again.
6. **If a Bash call crashes with `fork failed: resource temporarily unavailable` or `pthread_create: Resource temporarily unavailable`** — something other than `zerops_browser` leaked processes. Recover manually:
   ```
   pkill -9 -f "agent-browser-darwin" ; pkill -9 -f "agent-browser-chrome-"
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

```
# Phase 1 (Bash): stop background dev processes on every dev container.
# API-first recipes: both apidev AND appdev. Single-runtime: just appdev.
ssh apidev "pkill -f 'nest start' || true; pkill -f 'ts-node' || true; pkill -f 'node dist/worker' || true"
ssh appdev "pkill -f 'vite' || true; pkill -f 'npm run dev' || true"
```

Then call `zerops_browser` (MCP tool — NOT a Bash call):

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

The tool will execute `[open url] + your commands + [errors] + [console] + [close]` as one batch and return structured JSON: `steps[]`, `errorsOutput`, `consoleOutput`, `durationMs`, `forkRecoveryAttempted`, `message`.

Repeat with the appdev subdomain URL if dev verification is also required. One tool call per URL — do NOT combine multiple URLs in one call.

**Phase 3 (optional — only if more dev iteration is needed)**: restart the dev processes you killed in Phase 1. If you're advancing straight to stage deployment, skip this — the stage containers run their own processes, the dev containers are done.

**Report shape for a verification pass** (per subdomain walked):
- Connectivity panel state (services connected with latencies)
- Each feature section's render state (populated / empty / errored)
- `errorsOutput` from the result (expected: empty)
- `consoleOutput` from the result (expected: empty or benign info only)
- `forkRecoveryAttempted` from the result (expected: false — true means you forgot to kill dev processes)

**What to avoid** (all were seen in v4 or v5):
- Raw `agent-browser` / `echo ... | agent-browser batch` Bash calls — always use `zerops_browser` MCP tool
- `["eval", "window.onerror = …"]` inside commands — use the auto-appended `["errors"]` / `["console"]` output instead
- Running the browser walk while `nest start --watch` / `vite` / workers are still running on dev containers — guaranteed `forkRecoveryAttempted: true`
- Passing `["open", ...]` or `["close"]` inside `commands` — the tool strips them; if you thought you needed them, you didn't
- Dispatching a sub-agent that calls `zerops_browser` while the main agent also has a call in flight
- Re-running the tool against the same URL repeatedly "just to be sure" — one call per URL per iteration

If the browser walk reveals a problem curl missed: the batch has already closed the browser, so fix on mount, redeploy, and run the batch again (counts toward the 3-iteration limit). Do NOT advance to stage deployment until both appdev AND appstage verification passes show empty errors and populated sections.

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

After 1a completes and any redeployments have settled, the main agent performs the browser verification itself using the batch-mode canonical flow documented in **Step 4c: Browser verification**. Do not delegate this to a sub-agent:

- The sub-agent has no Zerops context — browser-observed symptoms with platform causes (wrong CORS origin, literal `${VAR}` in fetched JSON, missing env var) are harder to diagnose from inside a sub-agent.
- The sub-agent and main agent can't share a `zerops_browser` session (the tool serializes calls through a process-wide mutex, but the real problem is state coordination, not locking); running two browser tracks blows the fork budget.
- The main agent already has the full platform context it needs to act on what the browser shows.

**Procedure** (reference Step 4c for the full `zerops_browser` rules and canonical flow):

1. **Stop background dev processes on every dev container** (`ssh apidev "pkill -f 'nest start' …"` etc). Browser verification targets stage; the dev processes must not be competing for the fork budget.
2. **Call `zerops_browser` for `appstage`** — ONE MCP tool call with the appstage URL and the inner commands that walk every feature section (at least one interactive control per section). The tool auto-wraps open/errors/console/close.
3. **Call `zerops_browser` for `appdev`** (showcase only — confirms both dev and stage versions work; minimal recipes skip). Same shape.
4. **Report the walk results** per subdomain: connectivity state, section render state, `errorsOutput` from the result, `consoleOutput` from the result, `forkRecoveryAttempted` (should be false).

**If the browser walk reveals a problem:**
- The tool has already closed the browser, so there's nothing to clean up.
- Fix on the mount, redeploy (the cross-deploy of any affected stage target), and re-call `zerops_browser` for the affected subdomain. This counts toward the 3-iteration close-step limit.
- Do NOT advance to publish until BOTH subdomains return clean output (`errorsOutput` empty, all sections populated, all interactions return expected output, `forkRecoveryAttempted: false`).

**If `zerops_browser` returns `forkRecoveryAttempted: true`:**
- The tool already ran pkill recovery for you. Do NOT re-run it manually.
- The root cause is almost always a dev process you forgot to kill on a dev container. Explicitly list running processes on every dev container (`ssh {hostname} "ps -ef"`) and kill anything leaking fork budget (`nest start`, `vite`, `ts-node`, `nohup` jobs).
- Re-call `zerops_browser` once the processes are gone. If it comes back with `forkRecoveryAttempted: true` a second time, something outside your control is spawning processes — stop, investigate, and ask the user before retrying.
- Verify on-disk state is intact: `git status` on both mounts should show only the sub-agent's 1a fixes (which are already committed per the 1a redeploy step).

**Close-step advancement gate**: do NOT call `zerops_workflow action="complete" step="close"` until `zerops_browser` has returned clean output for all required subdomains AND any regressions it surfaced have been fixed and re-verified. Advancing while the browser walk was aborted or inconclusive is equivalent to not having done it.

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
```
zcp sync recipe create-repo {slug}
zcp sync recipe push-app {slug} /var/www/appdev
```
Creates `zerops-recipe-apps/{slug}-app` on GitHub, then pushes the app source code.

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
