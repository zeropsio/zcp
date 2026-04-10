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

**1. Hello-world** (platform knowledge): proven zerops.yaml patterns, runtime gotchas, base image details. One exists per runtime — match the base runtime, not the framework name:
```
zerops_knowledge recipe="{runtime-base}-hello-world"
```
Example: for a php-nginx framework, load `php-hello-world`. For a nodejs framework, load `nodejs-hello-world`.

**2. Minimal** (framework knowledge, if building a showcase): if a `{framework}-minimal` recipe exists, load it — it contains framework-specific gotchas, integration steps, and zerops.yaml patterns you should extend:
```
zerops_knowledge recipe="{framework}-minimal"
```
Skip this if building a minimal recipe (you ARE the minimal).

Your job is to extend this accumulated base with the NEW knowledge your tier adds. For minimal: framework-specific additions on top of the hello-world (ORM, migrations, templates). For showcase: additional services on top of minimal (cache, queues, storage, search, mail, workers).

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

**Target fields**: just `hostname` (lowercase alphanumeric, e.g. `app`/`db`/`cache`) and `type` (service type from live catalog — pick the highest available version for each stack). The tool dispatches rendering directly on the type — no role classification needed. For runtime services, if it's a background/queue worker instead of the HTTP-serving primary app, set `isWorker: true`. Workers get a `worker` setup name and no subdomain; the primary app gets a `prod` setup and `enableSubdomainAccess`. For managed/utility services, `isWorker` is ignored.

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
- **Cache library**: Redis client library for the framework
- **Session driver**: Redis-backed session configuration
- **Queue driver**: queue/job system for the framework
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
- **worker**: background job processor (`isWorker: true`) — consumes from queue, no HTTP
- **db**: primary database
- **redis**: cache + sessions + queues (Valkey or KeyDB)
- **storage**: S3-compatible object storage
- **search**: search engine (Meilisearch, Elasticsearch, or Typesense)

**API-first showcase targets** (dual-runtime):
- **app**: lightweight static frontend — Svelte SPA (`role: "app"`, `type: static`)
- **api**: JSON API backend — the showcased framework (`role: "api"`)
- **worker**: background job processor (`isWorker: true`) — shares API codebase (same runtime)
- **db**: primary database
- **redis**: cache + sessions + queues (Valkey or KeyDB)
- **storage**: S3-compatible object storage
- **search**: search engine (Meilisearch, Elasticsearch, or Typesense)

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

Recipes always use **standard mode**: each runtime gets a `{name}dev` + `{name}stage` pair. **Exception**: shared-codebase workers (same runtime as app — one app, two processes like web + queue runner) get only `{name}stage` — the app's dev container runs both processes via SSH. No `workerdev` — it would be a zombie container running the same code with no worker process started. Separate-codebase workers (different runtime/language) get their own dev+stage pair.

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

Write the complete zerops.yaml with ALL setup entries in a single file. Minimal recipes have TWO setups (`dev` + `prod`). Showcase recipes have THREE (`dev` + `prod` + `worker`). The same file is the source of truth for the deploy step AND for the README integration-guide fragment — writing it once eliminates drift between what deploys and what the README documents. The deploy step will verify dev against the live service, then cross-deploy the already-written prod/worker configs to stage.

**Dual-runtime zerops.yaml** (API-first showcase): Each runtime service has its own zerops.yaml in its own codebase root:
- `/var/www/apidev/zerops.yaml` — 3 setups: dev, prod, worker (API + shared-codebase worker)
- `/var/www/appdev/zerops.yaml` — 2 setups: dev, prod (frontend only)

The frontend's `build.envVariables` constructs the API URL from known parts:
```
VITE_API_URL: https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app
```
Components: hostname (`api`, defined in import.yaml) + `zeropsSubdomainHost` (project-level, resolved at build time) + port (from API's `run.ports`). The dev setup uses the dev hostname: `apidev-${zeropsSubdomainHost}-3000...`.

Follow the injected chain recipe (working zerops.yaml from the predecessor) as the primary reference. For hello-world (no predecessor), follow the injected zerops.yaml Schema. Platform rules (lifecycle phases, deploy semantics) were taught at provision — use `zerops_knowledge` if you need to look up a specific rule.

Recipe-specific conventions for each setup (platform rules from provision apply — these are ONLY the recipe-specific additions):

**`setup: dev`** (self-deploy from SSHFS mount — agent iterates here):
- `deployFiles: [.]` — **MANDATORY for self-deploy on dynamic runtimes** (nodejs, python, php-nginx, go, rust, bun, ubuntu, …); anything else destroys the source tree. **Exception for `run.base: static`** — a static container serves only the compiled bundle, there is no runtime evaluation. A static dev setup MUST still run `npm run build` (or equivalent) and set `deployFiles: dist/~` (or the framework's output dir). The only difference between dev and prod for a static target is build-time env vars (e.g. `VITE_API_URL` pointing at the `apidev-…` hostname in dev, `api-…` in prod) — NOT whether the build happens.
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

Whether the worker shares the app's codebase depends on the runtime type:

- **Shared codebase** (same runtime): one app, two processes. The worker is just a different entry point. Write a `setup: worker` block in the SAME zerops.yaml. During development, the agent starts both web server and queue worker as SSH processes from `appdev` — no `workerdev` service.
- **Separate codebase** (different runtime): separate project with its own zerops.yaml (`dev`/`prod`). Written to a separate mount (`/var/www/workerdev/`).

Worker-specific: `start` is mandatory (queue runner command), NO healthCheck/readinessCheck/ports (workers don't serve HTTP). Build and envVariables typically match prod.

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
- **Cache** (if provisioned) — store a value with TTL, show cached vs fresh response (proves cache driver)
- **Object storage** (if provisioned) — upload file, list uploaded files (proves S3 integration)
- **Search engine** (if provisioned) — live search across seeded records (proves search driver + indexing)
- **Queue + worker** (if provisioned) — dispatch-job button, show result (proves queue driver + worker)

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
- Instruction to **test each feature against the live service** after writing it — the sub-agent has SSH access to appdev and all managed services (db, cache, storage, search) are reachable. After writing a controller+view, hit the endpoint via `curl` or the framework's test runner and verify it returns expected data. Fix issues immediately — this is the entire point of deferring to after deploy.

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
- [ ] Both `setup: dev` AND `setup: prod` present (generic names); showcase: `setup: worker` too
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

**Step 1: Deploy appdev (self-deploy)**
```
zerops_deploy targetService="appdev" setup="dev"
```
The `setup="dev"` parameter maps hostname `appdev` to `setup: dev` in zerops.yaml. This triggers a build from files already on the mount. Blocks until complete.

**Step 1-API** (API-first showcase only): Deploy apidev FIRST — the API must be running before the frontend builds (the frontend bakes the API URL at build time):
```
zerops_deploy targetService="apidev" setup="dev"
```
Then deploy appdev after the API is verified (Step 3-API below).

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
- **Shared codebase** (worker same runtime as app): start the queue worker as an SSH process on appdev (or apidev for API-first) — both processes run from the same container:
  ```bash
  ssh appdev "cd /var/www && {queue_worker_command} &"
  ```
- **Separate codebase** (worker different runtime): deploy the separate worker codebase:
  ```
  zerops_deploy targetService="workerdev" setup="dev"
  ```
  Then start the worker process via SSH on workerdev.

**Step 3: Enable subdomain and verify appdev**
```
zerops_subdomain action="enable" serviceHostname="appdev"
zerops_verify serviceHostname="appdev"
```
Check: service RUNNING, subdomain returns 200, health endpoint responds (or page loads for static).

**Step 3-API** (API-first): Enable and verify the API FIRST, then deploy and verify the frontend:
```
zerops_subdomain action="enable" serviceHostname="apidev"
zerops_verify serviceHostname="apidev"
```
Verify `/api/health` returns 200 via curl. Then deploy appdev (Step 1) — the frontend builds with the API URL baked in. After appdev deploys, enable its subdomain and verify the dashboard loads and successfully fetches from the API.

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

Use `agent-browser` (pre-installed in the ZCP container) to open the dashboard URL and walk through each feature section:

```
agent-browser open https://appdev-${subdomainHost}.prg1.zerops.app
```

Then interact with the page as a user would:
- Confirm the connectivity panel shows all services as CONNECTED
- Click into each feature section — confirm data displays, not spinners or placeholders
- Submit a form in each section (create article, set cache key, upload file, dispatch job, run search) — confirm the result appears in the UI, not just a network 200
- Open the browser console — there must be ZERO JavaScript errors and ZERO failed network requests
- For dual-runtime (API-first): confirm the Network panel shows the SPA fetching from the API subdomain with 200 responses, not CORS-blocked preflights

What browser verification catches that curl cannot:
- JavaScript runtime errors (uncaught promise rejections, undefined method calls)
- Broken fetch URLs (wrong port, wrong protocol, missing `/api` prefix)
- CORS failures (API rejects the frontend origin)
- Blank renders (component mounted but never populated)
- Missing CSS (everything works but looks broken)
- Stale build artifacts (user sees a version from before your last fix)

If the browser shows a problem curl missed: fix on mount, redeploy, re-verify with agent-browser (counts toward the 3-iteration limit). Do NOT advance to stage deployment until browser verification passes.

### Stage deployment flow (after all appdev work is complete)

Stage is the final product — deploy it once with the complete codebase (skeleton + features).

**Step 5: Verify prod setup (already written at generate)**
The prod setup block was written to zerops.yaml during the generate step. Before cross-deploying, verify it matches what a real user building from git will need:
- `deployFiles` lists every path the start command and framework need at runtime — run `ls` on the mount and cross-reference. When cherry-picking (not using `.`), missing one path will DEPLOY_FAILED at first request.
- `healthCheck` + `deploy.readinessCheck` are present (required for prod — unresponsive containers get restarted; broken builds are gated from traffic).
- `initCommands` covers framework cache warming + migrations (NEVER in buildCommands — `/build/source/...` paths break at `/var/www/...`).
- Mode flags differ from dev (APP_ENV/NODE_ENV/DEBUG/LOG_LEVEL).

If anything is missing, edit zerops.yaml on the mount now — the change propagates to the README via the integration-guide fragment (which mirrors the file content).

**Step 6: Deploy appstage from appdev (cross-deploy)**
```
zerops_deploy sourceService="appdev" targetService="appstage" setup="prod"
```
The `setup="prod"` maps hostname `appstage` to `setup: prod` in zerops.yaml. Stage builds from dev's source code with the prod config. Server auto-starts via the real `start` command (or Nginx for static).

**Step 6-API** (API-first): Cross-deploy the API first, then the frontend (frontend builds with API URL baked in):
```
zerops_deploy sourceService="apidev" targetService="apistage" setup="prod"
```

**Step 7: Deploy workerstage** (showcase only — skip for minimal)
- **Shared codebase** (full-stack): cross-deploy from appdev with the worker setup:
  ```
  zerops_deploy sourceService="appdev" targetService="workerstage" setup="worker"
  ```
  The `setup="worker"` maps to `setup: worker` in the shared zerops.yaml — same build pipeline, different start command.
- **Shared codebase** (API-first): cross-deploy from apidev (worker shares API codebase):
  ```
  zerops_deploy sourceService="apidev" targetService="workerstage" setup="worker"
  ```
- **Separate codebase**: cross-deploy from workerdev:
  ```
  zerops_deploy sourceService="workerdev" targetService="workerstage" setup="prod"
  ```
  The worker has its own zerops.yaml with `setup: prod`.

**Step 8: Enable stage subdomain**
```
zerops_subdomain action="enable" serviceHostname="appstage"
```
API-first: also enable the API stage subdomain:
```
zerops_subdomain action="enable" serviceHostname="apistage"
```

**Step 9: Verify appstage**
```
zerops_verify serviceHostname="appstage"
```
API-first: also verify the API stage:
```
zerops_verify serviceHostname="apistage"
```
For showcase, also verify the worker is running:
```
zerops_logs serviceHostname="workerstage" limit=20
```

**Step 10: Present URLs**

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

**Showcase service keys — shared-codebase workers (same runtime as app) get only `workerstage` in envs 0-1 (no `workerdev` — appdev runs both processes).** Separate-codebase workers (different runtime) get both `workerdev` and `workerstage`. Omitting a comment key for a service that appears in the import.yaml produces a service with no comment, which degrades quality and risks failing the comment ratio check. Complete key list per env:

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

Recipe creation is complete. The close step has TWO parts with different triggers:

1. **Verification sub-agent — ALWAYS run, regardless of whether publishing is requested.** Recipe creation without expert review produces broken recipes; the sub-agent is the only thing catching framework-specific mistakes before the user inherits them.
2. **Export & publish — ONLY when the user explicitly asks.** If the user did not request publishing, stop after the sub-agent review is complete and any CRITICAL/WRONG fixes are applied.

Do NOT skip the sub-agent to save time. Do NOT publish without an explicit user request.

### 1. Verification Sub-Agent (ALWAYS — mandatory)

Spawn a sub-agent to perform a final review of the entire recipe. The sub-agent should act as a **{framework} expert** who has never seen this recipe before, reviewing it for correctness, completeness, and usability.

**Sub-agent prompt template:**

> You are a {framework} expert reviewing a Zerops recipe. Read ALL files in {outputDir}/ and {appDir}/ and verify:
>
> **App code review:**
> - Does the app actually work? Check routes, views, config, migrations.
> - Are framework conventions followed? (correct env mode flag, proxy trust configured, framework-idiomatic patterns)
> - If `buildCommands` compiles assets, does the primary view load them via the framework's asset helper (not inline CSS/JS)?
> - Is there dead code, unused dependencies, or missing files?
> - Does `.env.example` exist with the right keys?
>
> **zerops.yaml review:**
> - Do `setup: dev` and `setup: prod` entries have correct build/deploy/run config?
> - Does `setup: prod` have `healthCheck` (httpGet on the health endpoint)? Missing healthCheck means unhealthy containers are never restarted.
> - Does `setup: prod` have `deploy.readinessCheck`? Missing readinessCheck means broken builds get traffic.
> - Are deployFiles complete for prod? (run `ls` and verify all dirs/files the start command needs are included)
> - Are env vars correct for the framework? (production mode flags, service connection vars, secret references)
> - If the app uses Object Storage: is a region env var set to `us-east-1`? (Zerops does NOT auto-generate a region, but every S3 SDK requires one — use whichever env var name the framework's S3 client reads)
> - **Build-only vs runtime bases**: some bases exist ONLY in the build category and have no corresponding runtime type (they appear in `build.base` enums in the platform schema but not in `run.base`). A `run.base` must always be a valid runtime type. If a worker's `run.base` differs from its `build.base`, verify the `run.base` is a valid runtime type before flagging — the mismatch is often correct because the build base has no runtime equivalent.
> - Is the comment quality good? (WHY not WHAT, no restating key names, no section-heading decorators like `# -- Section --`)
>
> **import.yaml review (all 6 environments):**
> - Do ALL runtime services have `buildFromGit` + `zeropsSetup`? (dev=`zeropsSetup: dev`, prod/stage=`zeropsSetup: prod`)
> - Is there NO `startWithoutCode` anywhere? (that's workspace-only, never in recipe deliverables)
> - Do ALL runtime services have `buildFromGit`?
> - Are env 0-1 hostnames suffixed correctly? (`appdev`/`appstage`, NOT `appdev`/`app`)
> - Is `corePackage: SERIOUS` at project level (not verticalAutoscaling) in env 5?
> - Are `envSecrets` per-service (not project level)?
> - Is the scaling matrix correct across tiers?
> - Do service type versions match the plan? If the research step chose a specific version, all 6 import.yaml files should use that same version consistently.
>
> **README review:**
> - Does the integration-guide include numbered steps for code changes the agent made that any user would also need? (e.g., trusted proxy config, storage driver wiring). Demo-specific code (custom routes, views) does NOT belong — only changes that apply to any app on Zerops.
> - Does the knowledge-base fragment contain ONLY irreducible content (not repeating zerops.yaml)?
> - Is there clear separation: integration-guide = actionable steps, knowledge-base = awareness/gotchas?
> - Are there exactly 3 extract fragments with proper markers?
>
> **Dual-runtime (API-first) additional checks:**
> - Does the frontend successfully fetch from the API? (no CORS errors, data displays)
> - Does each Svelte component call the correct API endpoint?
> - Are both zerops.yaml files correct? (`/var/www/apidev/zerops.yaml` and `/var/www/appdev/zerops.yaml`)
> - Does the frontend's `VITE_API_URL` build variable correctly construct the API URL?
> - Do all 6 import.yaml files include BOTH `app` and `api` services with correct `buildFromGit` URLs and priority ordering?
>
> Report issues as: `[CRITICAL]` (breaks deploy), `[WRONG]` (incorrect but works), `[STYLE]` (quality improvement).

**Browser verification — MANDATORY for showcase** (skip for minimal): After the code/config review, the verification sub-agent MUST open the live dashboard in `agent-browser` on BOTH `appdev` and `appstage` subdomains and walk through every feature section. This is not optional and not a substitute for curl — it's the only check that catches what the user actually sees. The sub-agent's report MUST state, for each subdomain: connectivity panel status, each feature section's render state, JavaScript console errors (expected: zero), and failed network requests (expected: zero). A "looks fine to me" report without these specific observations is not acceptable.

Apply any CRITICAL or WRONG fixes, then **redeploy** to verify the fixes work:
- If zerops.yaml or app code changed: `zerops_deploy targetService="appdev" setup="dev"` (API-first: also redeploy apidev) then cross-deploy to stage
- If only import.yaml (finalize output) changed: re-run finalize checks
- Do NOT skip redeployment — the verification is meaningless if fixes aren't tested.

### 2. Export & Publish (ONLY when the user asks)

If the user did not explicitly request publishing (e.g. "create recipe" by itself), skip this section entirely and complete the close step. Publishing creates GitHub repos and opens PRs — side effects the user did not request.

**Export archive** (for debugging/sharing):
```
zcp sync recipe export {outputDir} --app-dir /var/www/appdev --include-timeline
```
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
