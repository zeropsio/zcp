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
- **Type** — pick the **highest available version** from `availableStacks` for each stack. Must include the `@version` suffix (e.g. `nodejs@22`, not bare `nodejs`). The same versioned form is required for the top-level `runtimeType` field on the plan. **For managed services (postgresql, valkey, nats, meilisearch, kafka, mariadb, ...) "highest available" is enforced at validation time** — submitting an older version is rejected unless you set `typePinReason` on that target with one sentence explaining the compatibility constraint that requires the older version (framework lag, library doesn't yet support the newer version, etc). Default rule: pick the latest. Pin reason is the documented escape hatch, not the default. Runtimes (nodejs, php-nginx, ...) are exempt — their version comes from framework compat which you negotiate separately during research.
- **isWorker: true** — set for background/queue workers (no HTTP). Ignored for managed/utility services.
- **role** — `app` / `api` for dual-runtime repo routing. Empty for managed services.
- **sharesCodebaseWith** — worker-only; see the Worker codebase decision block in the showcase research section. Minimal recipes have no worker.

**`research.dbDriver` is the DATABASE TYPE, not the ORM.** This field feeds the root README generator directly — whatever you write here lands on zerops.io/recipes as "connected to {dbDriver}". Valid values: `postgresql`, `mariadb`, `mysql`, `mongodb`, `sqlite`, `cockroachdb`, `clickhouse`, or `none` for recipes without a database. ORM / client-library names (`typeorm`, `prisma`, `sequelize`, `mongoose`, `eloquent`, `sqlalchemy`, `gorm`, `drizzle`, `kysely`, `knex`, ...) are rejected at research-complete time — v16's nestjs-showcase shipped with `dbDriver: "typeorm"` which leaked into the published recipe page as "A NestJS application connected to typeorm, Valkey, ...". The field name is misleading (it suggests an ORM) but the value is always a database name. Keep the ORM choice separate — it goes in the per-codebase README integration guide or CLAUDE.md.

### Features — the declaration/verification contract

`plan.features` lists every user-observable capability the recipe demonstrates. Generate scaffolds them, deploy curls each `healthCheck` and browser-walks each UI surface, close re-runs both. A feature not on the list cannot be verified; a feature on the list cannot be skipped.

Each `RecipeFeature` carries `id` (lowercase slug, unique), `description` (≥10 chars), `surface` (one or more of `api`, `ui`, `worker`, `db`, `cache`, `storage`, `search`, `queue`, `mail`). Features with `api` surface require `healthCheck` (path starting with `/`). Features with `ui` surface require `uiTestId` (the scaffold's `data-feature` value), `interaction` (how the browser walk exercises it), and `mustObserve` (state change proving success — "no results" is a failure by default).

Hello-world recipes declare one feature covering their single capability. Minimal recipes usually declare 1–3. Showcase recipes MUST cover every managed service in the plan — see the showcase research section for the coverage mandate.

```json
{"id":"greeting","description":"Fetch a greeting from the DB and render it.","surface":["api","ui","db"],"healthCheck":"/api/greeting","uiTestId":"greeting","interaction":"Open page; observe [data-feature=\"greeting\"] populate.","mustObserve":"[data-feature=\"greeting\"] [data-value] text is non-empty."}
```

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

**API-first** (no built-in templating): The framework serves JSON. Dashboard is a lightweight Svelte SPA in a separate `app` service that calls the API. The API is a separate `api` service. Worker codebase (shared vs separate) is decided in the Worker codebase decision block below, independent of this classification.

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

### Showcase Features — coverage mandate

See `research-minimal` for the `RecipeFeature` schema. Showcase adds a coverage mandate: the validator requires at least one feature per managed-service kind in the plan (`db`, `cache`, `storage`, `search`, `queue`, `mail` — whichever apply), plus the always-required `api` + `ui` surfaces, plus `worker` when a worker target exists. A standard showcase declares 5–6 features whose `surface` union covers `{api, ui, worker, db, cache, storage, search, queue}`. Typical entries:

- `items-crud` — surface `[api, ui, db]`, healthCheck `/api/items`, interaction "fill title, click Submit, row count +1", mustObserve `[data-feature="items-crud"] [data-row] count increases by 1`
- `cache-demo` — surface `[api, ui, cache]`, healthCheck `/api/cache`, interaction "click Write then Read", mustObserve `[data-result] text equals written value`
- `storage-upload` — surface `[api, ui, storage]`, healthCheck `/api/files`, interaction "upload sample file", mustObserve `[data-file] count increases`
- `search-items` — surface `[api, ui, search]`, healthCheck `/api/search`, interaction "type matching query, debounce 400ms", mustObserve `[data-hit] count > 0 for a known-matching query`
- `jobs-dispatch` — surface `[api, ui, queue, worker]`, healthCheck `/api/jobs`, interaction "click Dispatch, poll result", mustObserve `[data-processed-at] non-empty within 5s`

Each entry gets full fields (id, description, surface, healthCheck, uiTestId, interaction, mustObserve) in the submitted JSON — these bullets are a scaffold, not the submission format.

**The validator rejects incomplete coverage** — missing `search` when meilisearch is in targets, missing `worker` when a worker target exists, missing `queue` when nats is in targets. Fix the gap at research; downstream layers consume this list verbatim.

### Submission
Submit via:
```
zerops_workflow action="complete" step="research" recipePlan={...}
```
</section>

<section name="provision">
## Provision — Create Workspace Services

<block name="provision-framing">

Create all workspace services from the recipe plan. This follows the same pattern as bootstrap — dev/stage pairs for the app runtime, with shared managed services.

</block>

<block name="import-yaml-standard-mode">

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

**Serve-only targets need a toolchain-capable service type for dev.** If the plan's
target type is a serve-only base (static, nginx), the `{name}dev` service must use a
different type that can host the framework's dev toolchain — typically the same runtime
the zerops.yaml's `build.base` will use (e.g. `nodejs@22` for a Vite/Svelte SPA). The
serve-only base is a prod-only concern (the zerops.yaml's `setup: prod` uses
`run.base: static`); the dev container needs a shell, a package manager, and the dev
server binary. The `{name}stage` service keeps the plan's target type because stage
runs the prod setup via cross-deploy. Example: plan target `type: static` →
`appdev type: nodejs@22` + `appstage type: static`.

Dev starts immediately with an empty container (RUNNING). Stage stays in READY_TO_DEPLOY until first deploy from dev.

</block>

<block name="import-yaml-static-frontend">

**Static frontends (type 2a):** `run.base: static` serves via built-in Nginx — stage uses `type: static`. Dev still gets `startWithoutCode: true` for the build container. The runtime for building is `nodejs@22` (or similar) as `build.base` in zerops.yaml, NOT as the service type.

**Service type for dev**: use the toolchain runtime (typically `nodejs@22` or `bun@1`)
as the service type for `{name}dev`, not `static`. The dev container must host the
framework's dev server (Vite, webpack, etc.) over SSH — a static/Nginx container
has no shell, no package manager, and no Node. The `{name}stage` service keeps
`type: static` because it runs `setup: prod` (cross-deploy from dev builds the
bundle, Nginx serves it).

**If the plan has NO database** (type 2a static frontend): the import.yaml only contains the app dev/stage pair.

</block>

<block name="import-yaml-workspace-restrictions">

**Workspace import MUST NOT contain a `project:` section.** The ZCP project already exists — the API rejects imports that include `project:`. Only `services:` is allowed here. (The 6 recipe **deliverable** imports written in the finalize step DO contain `project:` with `envVariables` + preprocessor — that's a different file for a different use case.)

</block>

<block name="import-yaml-framework-secrets">

**Framework secrets**: if `needsAppSecret == true`, decide where the secret lives.

- **Shared** (encryption keys, CSRF secrets, session signing keys — anything multiple services must agree on): set at project level after provision completes:
  ```
  zerops_env project=true action=set variables=["{SECRET_KEY_NAME}=<@generateRandomString(<32>)>"]
  ```
  Do NOT wrap the preprocessor expression in `base64:` / `hex:` — `zerops_env` rejects those shapes because frameworks accept the raw 32-char output directly. If your framework's docs show a `base64:` prefix on the secret, drop it. `zerops_env set` is upsert and auto-restarts affected services so the new value takes effect.

- **Per-service** (unique API tokens, webhook secrets): add to `envSecrets` in the import.yaml under that service.

For correlated secrets, encoded variants, or key pairs, call `zerops_preprocess` directly.

</block>

<block name="import-yaml-dual-runtime">

**Dual-runtime URL constants** (API-first recipes only — skip for single-runtime): after services reach RUNNING, set project-level `DEV_*` + `STAGE_*` URL constants with `zerops_env project=true action=set` so the generate step can reference them in zerops.yaml. The full format, consumption pattern, and the `projectEnvVariables` handoff to finalize are documented in the generate step under "Dual-runtime URL env-var pattern" — set the same values now as will be passed there.

**URL shape — port suffix rule.** Dynamic runtime services (nodejs, php-nginx, go, etc.)
include the port in their subdomain URL: `https://{hostname}-${zeropsSubdomainHost}-{port}.prg1.zerops.app`.
Serve-only/static services omit the port segment: `https://{hostname}-${zeropsSubdomainHost}.prg1.zerops.app`.
The port comes from the target's `run.ports[0].port` in zerops.yaml — you're writing
zerops.yaml at the next step but you already know the port from the plan's `httpPort`
research field (e.g. 3000 for NestJS, 5173 for Vite dev, 80 for static). Set the URL
constants with the correct port suffix NOW, at provision, to avoid costly re-sets later
(each `zerops_env set` restarts all affected containers, killing any SSH-launched
processes). Static frontends in dev mode (Vite on port 5173) use the dev server port,
not port 80 — the dev setup overrides the static base with a toolchain runtime.

The generate step's "Dual-runtime URL env-var pattern" section has the full 6-env
breakdown (DEV_* + STAGE_* for envs 0-1, STAGE_* only for envs 2-5). At provision
you only need the workspace pair: set DEV_* and STAGE_* with the correct port suffixes.

**Batch all project-level env vars into a single `zerops_env set` call.** Each call
restarts every container that reads project-level vars. Multiple calls in sequence
trigger multiple cascading restarts, each killing any SSH-launched processes. Set
JWT_SECRET, all DEV_* URLs, and all STAGE_* URLs in one invocation.

</block>

<block name="provision-schema-inline">

### Workspace import.yaml fields you actually write here

The workspace import creates service shells inside an existing project. These are the fields that apply:

- `hostname` (string, max 40, [a-z0-9] only, immutable)
- `type` (`<runtime>@<version>`, pick highest from `availableStacks`)
- `mode` (`HA` | `NON_HA`, managed services only, immutable)
- `priority` (int; db/storage: `10` so they start first)
- `enableSubdomainAccess` (bool, true for publicly reachable dev services)
- `startWithoutCode` (bool, dev services only — container starts RUNNING without a deploy)
- `minContainers` (int, dev services = 1 — SSHFS needs single container)
- `objectStorageSize` (int, GB, object-storage services only)
- `verticalAutoscaling` (runtime + managed DB/cache; compiled runtimes need higher dev `minRam`)

**Not at provision**:
- `project:` block — the project already exists; API rejects.
- Project-level `envVariables` — cannot be added via workspace import. Set them via `zerops_env set` when you know what keys the app needs, or bake them into the deliverable `import.yaml` files at finalize.
- Service-level `envSecrets` / `dotEnvSecrets` — same reason. During iteration use `zerops_env set`; for the deliverable imports, finalize has the full set and writes them there.
- `zeropsSetup` / `buildFromGit` — deliverable-only fields, not workspace.
- Preprocessor functions (`<@generateRandomString>` etc.) — belong at finalize where the deliverable import is generated.

**Need more**: `zerops_knowledge scope="theme" query="import.yaml Schema"` returns the full reference with every exotic field.

Recipe-specific validation:

| Check | What to verify |
|-------|---------------|
| NO zeropsSetup | Workspace import must NOT include zeropsSetup (requires buildFromGit) |
| envSecrets | Per-service on app/worker, NOT at project level |
| Service types | Match available stacks from research |

</block>

<block name="import-services-step">

### 2. Import services

```
zerops_import content="..."
```

Wait for all services to reach RUNNING.

</block>

<block name="mount-dev-filesystem">

### 3. Mount dev filesystem

Mount the dev service for direct file access:
```
zerops_mount action="mount" serviceHostname="appdev"
```

This gives SSHFS access to `/var/www/appdev/` — all code writes go here.

</block>

<block name="git-config-mount">

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

**Ownership fix after git init.** SSHFS-created files are owned by `root` (the MCP
agent's user). The deploy process runs as `zerops` (uid 2023) inside the container and
must be able to lock `.git/config`. After `git init` + first commit on the SSHFS mount,
run `sudo chown -R zerops:zerops /var/www/.git` on each dev container via SSH. Without
this, the first `zerops_deploy` fails with `fatal: could not lock config file
.git/config: Permission denied`. Do this once per mount, immediately after the initial
commit — subsequent SSHFS writes to tracked files don't touch `.git/` internals.

</block>

<block name="git-init-per-codebase">

**Multi-codebase plans**: repeat both git configuration commands (zcp-side `safe.directory` and container-side `safe.directory`) for **every** provisioned dev mount. One codebase = one mount = one invocation of each command. The number of mounts is driven by your `sharesCodebaseWith` decisions at research — the authoritative shape table lives under "zerops.yaml — Write ALL setups at once" in the generate section. Do not assume a specific mount count; iterate over the mounts your plan actually created.

</block>

<block name="env-var-discovery">

### 4. Discover env vars (mandatory before generate — skip if no managed services)

Run `zerops_discover includeEnvs=true` AFTER services reach RUNNING. The response contains the real env var keys every managed service exposes. **You MUST use the names from this response, not guess them from training data.** Guessed names (`${search_apiKey}` when the real key is `${search_masterKey}`) fail silently — the platform interpolator treats unknown cross-service refs as literal strings, and your app sees `"${search_apiKey}"` as the value at runtime.

**Catalog the output.** Record the list of env var keys for every managed service in the provision-step attestation so the generate step (which writes the zerops.yaml `run.envVariables` using these references) has the authoritative list. Example attestation shape (fill placeholders from your actual plan):

```
Services: {list every runtime dev/stage pair your plan provisioned}, {list every managed service hostname}.
Env var catalog:
  {managedServiceHostname}: {env var keys returned by zerops_discover for this service}
  …
Dev mounts: {list every dev mount path from your plan}
```

Do not copy the placeholder names verbatim — they are intentionally abstract. List the exact hostnames and keys your workspace reported. The shape of the list (number of dev/stage pairs, number of dev mounts) follows from your `sharesCodebaseWith` decisions: single-codebase plans have one dev mount, multi-codebase plans have one per `sharesCodebaseWith` group. The authoritative shape table lives under "zerops.yaml — Write ALL setups at once" in the generate section.

If a managed service returns a set that surprises you (no `hostname`, or a `key` name you don't recognize), STOP and investigate — do not proceed with guessed names.

**If the plan has no managed services** (type 2a static frontend): skip this step entirely.

</block>

<block name="provision-attestation">

### Completion
```
zerops_workflow action="complete" step="provision" attestation="Services created: {list}. Env vars cataloged for zerops.yaml wiring (not yet active as OS vars — activate after deploy): {list}. Dev mounts: {list every dev mount path — one per codebase in your plan}"
```

</block>
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

### WHERE to write files

**Multi-codebase plans** (any plan where more than one dev mount exists): each codebase is an independent source tree with its own mount, its own zerops.yaml, its own README. The **number** of mounts, and what each one contains, follows from `sharesCodebaseWith`:

- Two dev mounts when: (a) dual-runtime with a shared-codebase worker (the worker rides inside its host codebase), or (b) single-runtime with a separate-codebase worker (the worker owns its own repo).
- Three dev mounts when: dual-runtime with a separate-codebase worker.

The authoritative enumeration — which zerops.yaml files exist, how many setups each contains, and which `sharesCodebaseWith` pattern produces each shape — lives under "zerops.yaml — Write ALL setups at once" later in this generate section. Do not re-derive it here; read the shape table, identify your row, and act on it.

**Each codebase gets its own README.md with all 3 extract fragments** (intro, integration-guide, knowledge-base). At publish time every codebase becomes a standalone recipe repo, and its README is the entry point for any user exploring it. The integration-guide fragment in each README contains **that codebase's** zerops.yaml, fully commented. The knowledge-base fragment lists the gotchas specific to **that codebase's** role — dev-server host-check lives in the frontend README, CORS and ORM-sync live in the API README, broker-connection and job-idempotency live in a separate-worker README, and so on.

**Use SSHFS for file operations, SSH for commands that need the base image's built-in tools** (scaffolders, `git init`, compiled binaries). Files placed on a mount are already present in the dev container; deploy doesn't "send" them, it triggers a build from what is already on disk.

**Scaffold each codebase inside its own mount — never cross-contaminate.** Framework scaffolders write config files (`tsconfig.json`, `package.json`, `.npmrc`, `.vscode/`, `.gitignore`, framework-specific dotfiles) into whatever directory they run from, and they trust the process working directory as the project root. Running a scaffolder from the wrong SSH session — or pointing it at a path that belongs to a different service — silently overwrites the other codebase's config. Rules that apply to every multi-codebase plan:

1. SSH into the dev service whose codebase you are scaffolding. Scaffolding the API codebase means SSH to the API dev service; scaffolding the frontend means SSH to the frontend dev service; scaffolding a separate-codebase worker means SSH to the worker dev service. **Every scaffolder / install / build invocation happens inside that ssh session** — not on zcp against the SSHFS mount. From inside the container, the codebase lives at `/var/www` (the container's own path), not at `/var/www/{hostname}/` (which is the zcp-side mount path). So the correct shape is `ssh {hostname} "cd /var/www && <scaffolder>"`, NOT `cd /var/www/{hostname} && <scaffolder>` from zcp.
2. If the target dev service's base image does not ship the scaffolder's runtime (common example: a static-base frontend service has no Node interpreter), write the scaffold files directly via Write/Edit against the SSHFS mount at `/var/www/{hostname}/` instead of invoking the scaffolder on the container. This is the ONLY safe zcp-side path — file writes via the mount, never execution.
3. Never scaffold into `/tmp` and copy — scaffolder footprints always include hidden files you will miss.
4. Never invoke a scaffolder from one service's SSH session while targeting a path that belongs to another service's codebase. The process working directory wins, and the "wrong" codebase's config files will be overwritten even though the shell prompt looks correct.

</block>

<block name="what-to-generate-showcase">

### What to generate per recipe type

**Type 1 (runtime hello world):** Raw HTTP server with a single file. DB connection via standard library. Raw SQL migration for a `greetings` table. No framework, no ORM.

**Type 2a (frontend static):** SPA/static site. Framework project (React/Vue/Svelte) with a simple page showing framework name, greeting, and environment indicator. Build-time env var injection. No DB connection.

**Type 2b (frontend SSR):** SSR framework project (Next.js/Nuxt/SvelteKit). Server-rendered pages with DB connection. Framework's API routes for health endpoint.

**Type 3 (backend framework):** Full framework project. ORM-based migrations, template-rendered dashboard, framework CLI tools. Uses the framework's conventions throughout.

**Type 4 (showcase):** Dashboard **SKELETON only** — feature controllers and views are **NOT** written during generate. Generate produces: layout with empty/placeholder partial slots (using the framework's standard include mechanism — partials, components, sub-templates, or imports) for each planned feature section, all routes (display + action endpoints pre-registered but returning placeholder responses), primary model + migration + factory + seeder with sample data, service connectivity panel, zerops.yaml (setups depend on worker shape — shared-codebase worker adds `setup: worker` in the host target's file; separate-codebase worker has its own zerops.yaml with `dev` + `prod` — see "zerops.yaml — Write ALL setups at once" below for the full enumeration), README with fragments, .env.example. **Stop here.** The deploy step dispatches a sub-agent to implement feature controllers and views against live services after appdev is verified. Writing feature code during generate means generating blind against disconnected services — producing code with no error handling, no XSS protection, and untested integrations. See "Showcase dashboard — file architecture" below.

</block>

<block name="two-kinds-of-import-yaml">

### Two kinds of import.yaml (critical distinction)

1. **Workspace import** (provision step) — creates the agent's dev/stage infrastructure. NO `zeropsSetup`, NO `buildFromGit`. Services use `startWithoutCode` (dev) or wait for deploy (stage).
2. **Recipe import** (finalize step) — the 6 deliverable files for end users. Uses `zeropsSetup: dev`/`zeropsSetup: prod` + `buildFromGit` to map hostnames to setup names.

zerops.yaml ALWAYS uses **generic setup names**: `setup: dev` and `setup: prod`. During workspace deploy, the `zerops_deploy` tool's `setup` parameter maps the service hostname to the correct setup name (e.g. `targetService="appdev" setup="dev"`). In recipe import.yaml, `zeropsSetup: dev`/`zeropsSetup: prod` does the same mapping for `buildFromGit` deploys.

</block>

<block name="execution-order">

### Execution order — zerops.yaml written LAST, README deferred to post-deploy

**Generate step writes four things in this order. README is NOT one of them any more — it moves to after stage verification, when you have actual debug experience to narrate.**

**Correct order:**
1. Scaffold the project (composer create-project, npx create-next-app, framework init, etc.) — for showcase multi-codebase plans dispatch scaffold sub-agents in parallel per the scaffold-subagent-brief topic; for everything else write yourself.
2. Write app code:
   - **Type 1 (runtime hello world)**: single-file HTTP server + DB migration, no framework, no dashboard. Write a minimal handler (e.g. `/` returns `"Hello from <framework>"`, `/greetings` returns SELECT-all from the `greetings` table) and the raw SQL migration. No routes table, no seeder beyond a migration INSERT, no feature sections.
   - **Types 2b/3 (minimal with framework)**: dashboard skeleton with feature sections, model + migration + seeder, routes, config changes. Write everything yourself — with only 1-2 feature sections (database CRUD, maybe cache) there's no benefit to sub-agents.
   - **Type 4 (showcase)**: write the dashboard skeleton yourself OR via the scaffold sub-agents (layout with connectivity panel, model + migration + seeder, /api/health, /api/status, client init per managed service). Do NOT write feature sections yet — that is the feature sub-agent's job at deploy step 4b. See "Showcase dashboard — file architecture" below.
3. On-container smoke test — run the framework's install + validate loop under each dev mount to prove the scaffold compiles and the install flow actually works. This happens BEFORE you commit to a zerops.yaml because step 4 derives `buildCommands`, `cache`, and `deployFiles` from what you observed here. Previous ordering had zerops.yaml written from research-time assumptions, then discovered at deploy-time that the real install flow needed different steps.
4. Write zerops.yaml — YOU, not a sub-agent. Use the discovered env vars, schema, and the install flow you just validated in smoke-test. Sub-agents lose the injected guidance (discovered env vars, zerops.yaml schema, comment ratio rules, `prepareCommands` constraints) and produce wrong output — showcase v1-v4 all failed on sub-agent-written zerops.yaml.
5. Git init + commit

**README is NOT written here.** It moves to the post-deploy `readmes` sub-step, after `verify-stage`. That is the only place the gotchas section can be written honestly, because by then the agent has actually hit the framework's quirks. v11 and v12 wrote generate-time READMEs full of speculative gotchas that failed the authenticity check; the fix is narrating from lived experience, not moving the check.

**Why this order matters:** zerops.yaml is the single source of truth for the integration-guide README fragment. Smoke-test-first means the buildCommands you commit to are the ones that actually worked. README-last means the knowledge-base fragment is authentic.

</block>

<block name="generate-schema-pointer">

### zerops.yaml field reference

The injected chain recipe's `## zerops.yaml template` section is the primary source: it's the same shape you're writing, for a recipe in the same framework family. For hello-world tiers (no chain predecessor) or exotic fields (buildFromGit, cache layers, per-environment overrides) not in the template, fetch the schema on demand:

```
zerops_knowledge scope="theme" query="zerops.yaml Schema"
```

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

**Dual-runtime URL baking has TWO halves. Both must be correct or the stage frontend silently breaks.**

### Half 1 — YAML half (env vars → bundle)

Framework-bundled dev servers (Vite, webpack dev server, Next dev, Nuxt dev) read `process.env.VITE_*` / `process.env.NEXT_PUBLIC_*` / equivalent **at dev server startup**, not at build time. For `setup: dev`, the client-side env vars must be in `run.envVariables`. For `setup: prod` they belong in `build.envVariables` because prod builds bake the values into the bundle.

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

### Half 2 — SOURCE CODE half (bundle actually reads the baked value)

**Baking an env var into the bundle is useless if no file reads it.** v18 shipped a dual-runtime showcase that was YAML-perfect and source-code-broken: every Svelte component hardcoded `fetch('/api/items')` and the `VITE_API_URL` value was baked into a bundle variable nobody imported. Dev was rescued by Vite's proxy; stage served nginx's SPA `index.html` fallback for every `/api/*` request, returning HTTP 200 with `Content-Type: text/html`. The dashboard rendered, the API calls "succeeded" with HTML, and every consumer rendered as an empty state. No error surfaced anywhere.

Every dual-runtime scaffold MUST include a **single API helper module** that reads the baked env var and prefixes every API call. Never `fetch('/api/...')` directly from a component.

```ts
// src/lib/api.ts (or equivalent for your framework)
// Single helper: reads the baked env var, defaults to empty string
// (so dev's Vite proxy handles the relative path unchanged).
const BASE = (import.meta.env.VITE_API_URL ?? "").replace(/\/$/, "");

export async function api(path: string, init?: RequestInit): Promise<Response> {
  const url = `${BASE}${path}`;
  const res = await fetch(url, init);
  if (!res.ok) {
    const body = await res.text().catch(() => "");
    throw new Error(`API ${res.status} ${res.statusText} ${path}: ${body.slice(0, 200)}`);
  }
  const ct = res.headers.get("content-type") ?? "";
  if (!ct.toLowerCase().includes("application/json")) {
    throw new Error(`API ${path} returned non-JSON content-type ${ct} — likely SPA fallback, check VITE_API_URL baking`);
  }
  return res;
}

// Usage — components NEVER call fetch() directly:
// const res = await api("/api/items");
// const items = await res.json();
```

**Why the content-type check is mandatory.** nginx's `try_files ... /index.html` SPA fallback returns 200 with `text/html` for any unknown path. A bare `fetch('/api/items').then(r => r.json())` in an appstage container throws a silent `SyntaxError: Unexpected token '<'` that most frameworks catch into an empty-state render. The user sees a dashboard with zero items. The helper above surfaces the error visibly instead.

**Anti-pattern — forbidden in every scaffolded client codebase**:

```ts
// WRONG — v18's exact bug:
const res = await fetch("/api/items");
const data = await res.json();
items = data.items;             // undefined when res was HTML
```

- No `import.meta.env.VITE_API_URL` reader — env var is baked into the bundle and never used.
- No `res.ok` check — 500 with a valid JSON error body slides past `try/catch` and `data.items` is undefined.
- No content-type check — HTML fallback parses as "falsy JSON" or throws silently.
- Template consumes `items.length` → Svelte crashes with `Cannot read properties of undefined`.

The scaffold subagent's brief lists this as a mandatory structural rule — see `client-code-observable-failure` for the general form.

**Consumption**: project-level env vars auto-inject into both runtime AND build containers. Reference them directly by name in zerops.yaml — `build.envVariables: VITE_API_URL: ${STAGE_API_URL}` bakes the stage URL into the cross-deployed bundle; `run.envVariables: FRONTEND_URL: ${STAGE_FRONTEND_URL}` forwards the value under a framework-conventional name for CORS. There is **no `RUNTIME_` prefix** on project vars — that prefix is a different feature (lifting a service-level runtime var into build), not applicable here. The full consumption model (including the shell-prefix alternative in `buildCommands`) lives in the `environment-variables` knowledge guide — fetch it via `zerops_knowledge scope="guide" query="environment-variables"` when you need the platform rules behind this pattern.

The `setup: dev` block reads `DEV_*`; `setup: prod` reads `STAGE_*`. The same zerops.yaml works in every env: envs 2-5 never invoke `setup: dev` (there is no `appdev` there), so the `DEV_*` reference is dormant and safe.

**Workspace parity is set at the provision step**, not here — see the provision step's `zerops_env project=true action=set` invocation. By the time you reach generate, the workspace already has `DEV_*` + `STAGE_*` resolved. Single-runtime recipes skip this entirely — they don't cross services for URL baking.

**The deploy step enforces both halves.** The `feature-sweep-dev` and `feature-sweep-stage` sub-steps run a curl against every api-surface feature's HealthCheck and reject any response with `text/html` content-type — the exact symptom of a missing source-code half. A YAML-perfect recipe with the source-code half wrong will fail the sweep before it ever reaches the browser walk.

</block>

<block name="project-env-vars-pointer">

**For the 6 deliverable import.yaml files**: pass `projectEnvVariables` as a first-class input to `zerops_workflow action="generate-finalize"` at finalize — the full per-env shape lives in finalize Step 1b. Do NOT hand-edit the generated files; re-running `generate-finalize` re-renders from template.

</block>

<block name="dual-runtime-what-not-to-do">

**What NOT to do**:
- Do NOT invent a `setup: stage` — there is no such thing. Stage uses `setup: prod`.
- Do NOT reference another service's `${hostname}_zeropsSubdomain` to build URLs. Use the `${zeropsSubdomainHost}` project-scope var and the constant URL format above.
- Do NOT create a service-level env var with the same name as ANY auto-injected var — that's a self-shadow loop. Applies to project-level (`${STAGE_API_URL}`, `${APP_SECRET}`) AND cross-service (`${db_hostname}`, `${queue_user}`). The platform interpolator sees the same-name service var first and resolves to the literal `${VAR_NAME}` string. Forward under a DIFFERENT name if the framework needs a rename (e.g. `FRONTEND_URL: ${STAGE_FRONTEND_URL}`, `DB_HOST: ${db_hostname}`); if you want the var under its own name, just don't write the line — it's already in the container's OS env. Full rule set in the `env-var-model` block.

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
- **No `run.os` override by default** — the agent operates from zcp (which has full Ubuntu tooling via SSH proxy). The dev container needs only the runtime and its package manager — both are in the base image already. Omitting `run.os` means build and run use the same default OS, eliminating native binding mismatches entirely. Exception: if the on-container smoke test reveals a hard glibc-only dependency (the dev server crashes on the default OS), THEN add `run.os` with an appropriate OS AND add the package manager's rebuild command to `initCommands`. This is a reactive exception discovered during validation, not a proactive default.
- **Dev ports for dev-server targets** — if `setup: dev` runs a dev server process (any framework with a bundler, HMR server, or dev-mode HTTP server on a non-standard port), `ports` MUST be declared with the dev server's port (from the plan's research data) and `httpSupport: true`. Without it, subdomain access cannot be enabled (`serviceStackIsNotHttp` error). This applies specifically when the dev setup's runtime differs from the prod setup's (e.g., prod is serve-only, dev runs a dev server on an explicit port).

</block>

<block name="serve-only-dev-override">

- **Serve-only runtimes** (`static`, standalone `nginx`, any future serve-only base): these host no toolchain — `run.base: static` is a **prod-only concern**. For the dev setup, pick a different `run.base` that CAN host the framework's dev toolchain — typically the same base that already exists under `build.base` for that setup (e.g. `nodejs@22` for a Vite/Svelte SPA whose prod is `static`). `run.base` may differ between setups inside the same zerops.yaml; the platform supports this and it's the intended pattern for serve-only prod artifacts. `deployFiles: [.]` still applies on dev regardless of `run.base` choice.

- **Serve-only prod `deployFiles` — use the tilde suffix.** When `setup: prod` uses
  `run.base: static` (or `nginx`), the build step compiles assets into a subdirectory
  (e.g. `./dist/`). Nginx serves from `/var/www/`, so `deployFiles: ./dist` ships the
  directory wrapper and files land at `/var/www/dist/index.html` — a 404 at root. The
  tilde suffix (`./dist/~`) strips the parent directory prefix: files land directly at
  `/var/www/index.html`. This is a platform convention, not documented in framework
  guides. Always use `./dist/~` (or the equivalent output path) for static-base prod
  setups.

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

Worker rules: `start` mandatory (broker consumer command); NO healthCheck/readinessCheck/ports (workers don't serve HTTP); `build` matches prod; `envVariables` = mode flags only — cross-service vars (`${db_hostname}`, `${queue_user}`, etc.) auto-inject into worker containers too, no declaration needed; shared workers inherit the host target's `build.base` and cache — only `start` differs.

</block>

<block name="shared-across-setups">

**Shared across all setups:**
- `envVariables:` contains **mode flags** (`NODE_ENV`, `APP_ENV`) + **framework-convention renames only** (`DB_HOST: ${db_hostname}` when the framework expects `DB_HOST`). Cross-service vars are already injected as OS env vars in every container — **don't re-declare them under their own name**. See the `env-var-model` block for the full rule.
- Do NOT re-add envSecrets — platform injects them automatically.
- dev and prod env maps must NOT be bit-identical — a structural check fails if mode flags don't differ.

</block>

<block name="env-var-model">

### `run.envVariables` — what to write, what NOT to write

Before you write any `envVariables:` block, internalise this rule set. Violating it caused multiple recipe runs to self-shadow every DB/queue credential and spend 30+ minutes diagnosing `${db_hostname}` literal strings in worker logs.

**What the platform auto-injects into every container (OS env vars, no declaration needed):**

- **Cross-service vars** — every service's variables are visible from every other service's containers as `{source_hostname}_{varname}`. A worker sees `db_hostname`, `db_password`, `db_port`, `queue_user`, `queue_password`, `storage_apiUrl`, `storage_accessKeyId`, `search_masterKey`, `redis_hostname`, etc. — all auto-injected with real values. Read them directly: `process.env.db_hostname`, `getenv('db_hostname')`. Zero zerops.yaml declaration required.
- **Project-level vars** — everything set via `zerops_env project=true action=set` auto-propagates into every service's container. Read directly: `process.env.STAGE_API_URL`.

**What `run.envVariables` (and `build.envVariables`) is legitimately for — only two things:**

1. **Mode flags** — values that don't come from another service:
   ```yaml
   envVariables:
     NODE_ENV: production
     APP_ENV: local
     LOG_LEVEL: debug
   ```

2. **Framework-convention renames** — forward a platform var under a DIFFERENT key because the framework's config expects that name. Key on the left MUST DIFFER from the source var name on the right:
   ```yaml
   envVariables:
     DB_HOST: ${db_hostname}          # TypeORM DataSource expects uppercase DB_HOST
     DATABASE_URL: ${db_connectionString}
     FRONTEND_URL: ${STAGE_FRONTEND_URL}   # app code uses FRONTEND_URL not STAGE_FRONTEND_URL
   ```

**Do NOT write any of these — all are self-shadows:**

```yaml
envVariables:
  db_hostname: ${db_hostname}        # cross-service self-shadow — worker connects to "${db_hostname}:5432"
  db_password: ${db_password}        # cross-service self-shadow — literal `${db_password}` leaks into logs
  queue_user: ${queue_user}          # cross-service self-shadow
  storage_apiUrl: ${storage_apiUrl}  # cross-service self-shadow
  STAGE_API_URL: ${STAGE_API_URL}    # project-level self-shadow
  APP_SECRET: ${APP_SECRET}          # project-level self-shadow
```

The platform's template interpolator sees the service-level variable of that name first, cannot recurse back to the auto-injected value, and resolves the OS env var to the literal string `${varname}`. The framework then tries to connect to `"${db_hostname}:5432"` at runtime and crashes with a cryptic DNS or auth error.

**Decision flow — for every var your app reads:**

1. Does the app read `process.env.X` where `X` is a platform-provided name (`db_hostname`, `STAGE_API_URL`, etc.)? → **Don't declare it.** It's already there.
2. Does the app read `process.env.X` where `X` is a framework-convention name (`DB_HOST`, `DATABASE_URL`) that differs from the platform name? → Declare `X: ${platform_name}` as a rename. Ensure keys differ.
3. Is `X` a mode flag (`NODE_ENV`, `APP_ENV`, `LOG_LEVEL`) with a value that isn't sourced from another service? → Declare with a literal value.
4. Is `X` a secret you want the worker/API to read? → Declare via `envSecrets` in import.yaml at provision, NOT in zerops.yaml. Platform auto-injects them too.

Full platform rules for env scopes, isolation modes, build/runtime separation, and `envReplace` live in the `environment-variables` knowledge guide — fetch via `zerops_knowledge scope="guide" query="environment-variables"` when you need the mechanics behind this rule.

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

### Write the dashboard skeleton — health page ONLY

The generate step ships an intentionally bare dashboard: **one page, one component, one job** — prove every managed service in the plan is reachable. No feature sections, no forms, no tables, no demos. The feature sub-agent at deploy step 4b owns everything else as a single coherent author.

**What the scaffold writes:**

| Area | Contents |
|---|---|
| Frontend entry | `App.svelte` / equivalent — renders `<StatusPanel />` and literally nothing else |
| Status panel component | Polls `GET /api/status` every 5s, renders one row per managed service (db, redis, nats, storage, search, …) with a colored dot (green/red/yellow) and the service name. No buttons, no forms, no other state |
| API health route | `GET /api/health` — liveness probe, returns `{ ok: true }` |
| API status route | `GET /api/status` — deep connectivity check, returns `{ db: "ok", redis: "ok", nats: "ok", storage: "ok", search: "ok" }` (one key per service in the plan). Implementation pings each service |
| Service clients | Client initialization for every managed service in the plan: TypeORM datasource, Redis client, NATS connect, S3 client, Meilisearch client. Import and configure from env vars — do NOT build demo routes against them |
| Migrations | Full schema for the primary data model — the feature sub-agent will add endpoints that query it |
| Seed data | 3-5 rows of sample data so the feature sub-agent has something to render. NOT 15-25 — the feature sub-agent expands the seed when it builds the features that need more |
| Worker (if separate codebase) | NATS connect + one no-op subscriber that logs the received message. No processing, no DB writes, no result storage |
| `zerops.yaml`, `README.md`, `.env.example` | Per existing rules |

**What the scaffold does NOT write:** item CRUD routes, cache-demo routes, search routes, jobs-dispatch routes, storage upload routes, and the frontend components that consume them. Every one of these belongs to the feature sub-agent, which will author API + frontend + worker changes as a single unit so the contracts stay consistent.

**Why this is narrower than it feels:** v11 and v12 both shipped with the scaffold overshooting into feature code and the main agent concluding step 4b was "already done." The only reliable way to prevent that is to ship a visibly empty dashboard — one green-dot panel — so there is nothing for the main agent to rationalize away. The health page also makes the deploy browser walk meaningful: either every dot is green or it isn't.

**Minimal / hello-world tiers** skip the dashboard entirely — they follow their existing flow (inline feature, no sub-agent split).

</block>

<block name="scaffold-subagent-brief">

### Scaffolding sub-agent brief (multi-codebase recipes only)

For dual-runtime and multi-codebase recipes (showcase Type 4 with separate appdev/apidev/workerdev mounts, or any recipe with more than one codebase), writing every codebase sequentially in the main agent is slow. Dispatch scaffolding sub-agents in parallel, one per codebase — **with a strict brief that ships an intentionally bare health-dashboard-only skeleton.** Feature code is owned by a single feature sub-agent at deploy step 4b who writes API + frontend + worker as one unit so the contracts stay consistent. v10, v11, and v12 all shipped recurring API/frontend contract-mismatch bugs because parallel scaffold agents authored their halves of each feature independently; the single-author rule at step 4b eliminates the entire class.

**Order of operations — scaffolds FIRST, main-agent work AFTER. This is the v14 order; do not fall back to v13.**

1. **Dispatch scaffolding sub-agents in parallel, one per codebase**, each with the brief template below. Each sub-agent writes only its codebase's language-level scaffolding: framework init, health dashboard skeleton, service client wiring, migrations + 3-5 row seed. **No zerops.yaml. No README. No feature code.**
2. **Main agent writes app code after scaffolds return** — this is the `app-code` sub-step. For showcase this is mostly ensuring cross-codebase shape is coherent (import paths, env var names, shared type stubs). The health dashboard itself came from the scaffold sub-agents.
3. **Main agent runs the on-container smoke test** — `smoke-test` sub-step. Install dependencies, run the framework's compile/validate command. The point is to prove the install flow works **before** you commit the build commands to zerops.yaml.
4. **Main agent writes zerops.yaml LAST** — `zerops-yaml` sub-step. Use the install flow you just validated under smoke-test as the source of truth for `buildCommands`, `cache`, and `deployFiles`. Earlier v13 ordering had you writing zerops.yaml from research-time assumptions, then discovering at deploy time that the real build needed different steps.
5. **README is NOT written during generate.** The scaffold sub-agent brief below explicitly says DO NOT write README.md. Any README content on the mounts at generate-complete time is wrong — delete it before completing. The `readmes` sub-step at the end of deploy is the only place READMEs are written, and by then the agent has lived through the debug rounds that make the gotchas section honest.

**Scaffold sub-agent brief — include verbatim (edit only the codebase-specific names and service list from the plan):**

> **Verify every import, decorator, and module-wiring call against the installed package, not against memory.** Before committing an `import` line, an adapter registration, or any language-level symbol binding, open the package's on-disk manifest (`node_modules/<pkg>/package.json`, `vendor/<pkg>/composer.json`, `go.sum` + the module's `go.mod`, the gem's `*.gemspec`, etc.) and confirm the subpath / symbol you're about to reference is exported by the version actually installed. Training-data memory for library APIs is version-frozen and is the single biggest source of stale-path compile errors the code-review sub-agent has to reject at close time. The verification is mechanical and takes one file read — always cheaper than a close-step round-trip. **When in doubt, run the tool's own scaffolder against a scratch directory and copy its import shapes verbatim.** The installed version's own scaffolder is the authoritative source of current-major idioms.
>
> You are scaffolding a health-dashboard-only skeleton. **You write infrastructure. You do NOT write features.** A feature sub-agent runs later with SSH access to live services and authors every feature section end-to-end (API routes + frontend components + worker payloads as a single unit). Your job is to give that sub-agent a healthy, deployable, empty canvas to build on.
>
> **⚠ CRITICAL: where commands run (read this FIRST, before writing any files)**
>
> You are running on the **zcp orchestrator container**, not on the target dev container. The path `/var/www/{hostname}/` on zcp is an **SSHFS network mount** — a bridge into the target container's `/var/www/`. It is a write surface, not an execution surface.
>
> **File writes** via Write/Edit/Read against `/var/www/{hostname}/` work correctly — you are editing the target container's filesystem through the mount. Do this for every source file, config, `package.json`, etc.
>
> **Executable commands** MUST run via SSH into the target container. Every `npm install`, `npm run build`, `tsc`, `vite build`, `nest build`, `npx`, `pnpm`, `yarn`, `composer`, `bundle`, `cargo`, `go build`, and `git init/add/commit` goes through:
>
> ```
> ssh {hostname} "cd /var/www && <command>"
> ```
>
> NOT through:
>
> ```
> cd /var/www/{hostname} && <command>    # WRONG — runs on zcp against the mount
> ```
>
> The reason this matters: zcp's uid/gid differs from the container's `zerops` user, the PATH is different, the node binary has a different ABI, and the mount is not a real filesystem for `.bin/` symlink resolution. Running `npm install` on zcp produces:
>
> 1. `node_modules/` owned by zcp-root instead of the container's `zerops` user — later operations on the container hit EACCES and need `sudo chown -R`
> 2. Broken absolute-path symlinks in `node_modules/.bin/` that don't resolve inside the container — `sh: svelte-check: not found`, `sh: vite: not found`, `npx nest` returns ENOENT even though `node_modules/@nestjs/cli/` exists
> 3. Native modules compiled against zcp's node binary that won't load on the container — mysterious `Error: Cannot find module` errors at runtime
> 4. `.git/` owned by zcp-root so subsequent container-side git operations need `sudo chown` to work
>
> If your install or build logs show EACCES, "not found" for packages that are clearly installed, or ownership surprises, you are running commands on the wrong side of the boundary. Stop, re-read this section, and redo the failing step via `ssh {hostname}`.
>
> **Your normal workflow:**
>
> 1. Use Write/Edit to place files under `/var/www/{hostname}/` (the mount). No SSH needed.
> 2. Use Bash with `ssh {hostname} "cd /var/www && <command>"` for every build / install / test / type-check.
> 3. Do NOT `cd /var/www/{hostname}` in a Bash call. Ever.
>
> That's the complete rule — every deviation from it causes the symptoms above.
>
> **Transcribe-from-scratch protocol (every codebase, BEFORE authoring from memory):**
>
> Run the framework's own scaffolder into a scratch directory on the target container and copy files from it. The installed scaffolder is authoritative for current-major idioms, hygiene file contents, and package wiring:
>
> ```
> ssh {hostname} "cd /tmp && rm -rf scratch && npx @nestjs/cli new scratch --skip-git --package-manager npm"
> # (or: npm create vite@latest scratch --yes / rails new scratch / composer create-project laravel/laravel scratch — pick the framework's own invocation)
> ```
>
> Files to transcribe from `scratch/` (do NOT author from memory):
> - Code-config: `tsconfig.json`, `tsconfig.build.json`, `nest-cli.json`, `vite.config.ts`, `svelte.config.js` (whichever the scaffolder emits for this framework)
> - Hygiene: **`.gitignore`** (the scaffolder's ignore list is authoritative), **`.env.example`** (copy if emitted; otherwise write one from the codebase's `process.env` / `os.environ` reads)
> - Lint config (if emitted): `.eslintrc.*`, `.prettierrc`, `.rubocop.yml`, etc.
>
> After transcription: `ssh {hostname} "rm -rf /tmp/scratch"`.
>
> v21 apidev + workerdev ran `nest new scratch` but copied only code-config files, leaving `.gitignore` behind — the apidev mount then swept up 208 MB of `node_modules` during `git add -A`. This explicit transcription list prevents that class of omission.
>
> **⚠ Recurrence-class service-client traps (read BEFORE you write any managed-service client):**
>
> These are platform × framework intersections that v21 AND v22 both hit as runtime CRITs despite the gotchas being documented in the prior run's published README. The gotcha-in-README is a post-mortem, not a preventative — YOU are the preventative. Build the client correctly the first time by following these rules verbatim.
>
> 1. **NATS credentials MUST be passed as separate `ConnectionOptions` fields, NEVER URL-embedded.** The platform auto-generates `${queue_password}` with URL-reserved characters (`@`, `#`, `/`, `?`) 80%+ of the time, which makes `nats://${user}:${pass}@${host}:${port}` throw `TypeError: Invalid URL` at `connect()`. Even when the URL happens to parse, `nats.js@2` silently drops URL-embedded creds. Correct shape:
>    ```
>    await connect({
>      servers: `${process.env.queue_hostname}:${process.env.queue_port}`,  // NO creds in URL
>      user:    process.env.queue_user,
>      pass:    process.env.queue_password,
>    })
>    ```
>    Wrong shape (do not emit):
>    ```
>    servers: `nats://${queue_user}:${queue_password}@${queue_hostname}:${queue_port}`  // FAILS
>    ```
>    Applies to both API and worker codebases whenever they call NATS.
>
> 2. **Object-Storage (S3) endpoint MUST come from `process.env.storage_apiUrl`, NEVER built as `http://${storage_apiHost}`.** The platform's object-storage proxy returns `301 → https://...` on plain HTTP; the AWS SDK does not follow 301 redirects, so `HeadBucketCommand.send()` throws `NotFound` and `/api/status` flips `{"storage":"error"}`. `${storage_apiUrl}` is already the correct `https://...` form. Correct shape:
>    ```
>    new S3Client({
>      endpoint:       process.env.storage_apiUrl,   // https://... with scheme baked in
>      region:         'us-east-1',                  // MinIO-backed; any region string works
>      forcePathStyle: true,                         // MinIO style
>      credentials: { accessKeyId: process.env.storage_accessKeyId, secretAccessKey: process.env.storage_secretAccessKey },
>    })
>    ```
>    Wrong shape:
>    ```
>    endpoint: `http://${process.env.storage_apiHost}`  // 301 loop, SDK drops it
>    ```
>    Applies to every codebase that speaks S3/MinIO.
>
> 3. **Worker dev-start must use `ts-node src/main.ts` (or the framework's dev-mode equivalent), NEVER `node dist/main.js`.** Dev `buildCommands` runs dependency install only — `npm run build` is a PROD-only step. A `run.start: node dist/main.js` in the dev setup tries to execute a file the dev build never produced, and `zerops_dev_server action=start` returns `post_spawn_exit` with `Cannot find module '/var/www/dist/main.js'`. Correct dev shape: `run.start: npx ts-node -r tsconfig-paths/register src/main.ts` for headless workers, `nest start --watch` for HTTP services. Prod shape (unchanged): `run.start: node dist/main.js` after `npm run build` in prod `buildCommands`. The contract violation is caught at `zerops_dev_server start` time — prevent it at the zerops.yaml scaffold by cross-checking `run.start` against the setup's `buildCommands`.
>
> These three traps accounted for ~15 min of wall-clock cost in v22 (rebuild → redeploy → reverify per CRIT). Get them right on the first scaffold; the deploy step will be 3 CRITs lighter.
>
> **WRITE (frontend codebase):**
>
> - `package.json` — production dependencies for the framework and any CSS tooling the scaffold would normally include
> - Framework config (`vite.config.ts`, `tsconfig.json`, `.env.example`)
> - `src/lib/api.ts` (or equivalent) — **the single HTTP helper every component uses**. Reads `import.meta.env.VITE_API_URL` (framework equivalent), defaults to empty string so dev proxy works, enforces `res.ok` + content-type `application/json` on every call, throws with a descriptive error on failure. Components call `api("/api/status")` — NEVER `fetch("/api/status")` directly. This is the source-code half of the dual-runtime URL pattern (see `dual-runtime-consumption`) and is MANDATORY even when the scaffold only renders StatusPanel. Copy the pattern verbatim from `dual-runtime-consumption` — do not invent your own shape.
> - `App.svelte` (or equivalent entry) that renders `<StatusPanel />` **and nothing else** — no routing, no layout with empty slots, no tabs, no nav. One component mounted. The outer wrapper carries `data-feature="status"` (or whatever `uiTestId` the plan's status feature declares) so the browser walk can locate it.
> - `StatusPanel.svelte` — calls `api("/api/status")` via the helper (NOT `fetch()` directly) every 5s, renders one row per managed service in the plan with a colored dot (green = "ok", yellow = "degraded", red = missing/error) and the service name. Every row carries a `data-service="{name}"` hook. Three explicit render states: loading, error (visible red banner using `data-error`), populated. The outer element carries `data-feature="status"` (or the status feature's `uiTestId`). That's the whole UI. No forms, no buttons, no tables, no tabs.
> - `main.ts` / `main.js` — framework bootstrap
>
> **WRITE (API codebase):**
>
> - `package.json` with production dependencies for the framework, ORM, and every managed-service client in the plan (Redis, NATS, S3, Meilisearch, etc.)
> - `GET /api/health` — liveness probe returning `{ ok: true }` with `Content-Type: application/json`. No service calls.
> - `GET /api/status` — deep connectivity check. Returns a flat object with one key per service in the plan: `{ db: "ok", redis: "ok", nats: "ok", storage: "ok", search: "ok" }` with `Content-Type: application/json`. Each value is `"ok"` on successful ping, `"error"` otherwise. Exactly these keys; exactly these values.
> - Service client initialization for **every** managed service in the plan, from env vars. Import and configure the client library, expose the client for later use.
> - Migrations for the primary data model. Full schema — the feature sub-agent will add read/write endpoints against it.
> - **Seed script obeying the loud-failure rule** (see `init-script-loud-failure`). Seed 3-5 rows of primary-model data. If the plan provisions a search engine and the scaffold pre-wires a client for it, the seed must sync the seeded rows to the search index AND **`await` the completion signal** (e.g., Meilisearch `waitForTask`) before the script exits. No broad `try/catch` that logs and returns — seed failures must exit non-zero so `execOnce` records failure and the deploy sweep catches it. The feature sub-agent expands seeds as it implements features that need more.
> - **No other routes.** No item CRUD. No cache-demo. No search. No jobs dispatch. No storage upload. If you are about to write any of these, stop and re-read this brief.
>
> **WRITE (worker codebase, if separate):**
>
> - `package.json` with production dependencies for NATS and the database client
> - NATS connection + one subject subscriber that logs the received message and returns. No job processing, no DB writes, no Redis writes, no result tables.
> - Worker framework bootstrap (`NestFactory.createApplicationContext()` for NestJS, equivalent for other frameworks)
> - Entity / model imports from the API codebase when the worker shares the database. Never invent worker-only column sets — v11 shipped phantom columns this way.
>
> **WRITE (every codebase):**
>
> - **`.gitignore` — mandatory.** Minimum content: `node_modules/`, the framework's build output directory (`dist/`, `build/`, `.next/`, `target/`, `public/build/`, etc.), `.env`, `.env.local`, `.DS_Store`. Copy the exact contents from the framework's own scaffolder (e.g. `nest new`, `npm create vite@latest`, `rails new`, `composer create-project laravel/laravel`) when one exists — that file is authoritative, don't hand-author from memory.
> - **`.env.example` — mandatory.** List every environment variable the codebase reads from `process.env` / `os.environ` / `ENV[]` / equivalent, with a short comment per line explaining the shape. Blank file is acceptable ONLY when the codebase reads no env vars.
> - Framework lint config (`.eslintrc.*`, `.rubocop.yml`, `.php-cs-fixer.php`, etc.) only if the framework's scaffolder normally emits one.
>
> **DO NOT WRITE (any codebase):**
>
> - **`README.md`. Do not create it. Do not scaffold one.** Delete any README the framework scaffolder emits. The main agent writes READMEs at the very end of deploy, narrating real debug experience. If a README exists at generate-complete time, the checker fails and retries.
> - **`zerops.yaml`. Do not create it.** The main agent writes it AFTER your scaffold returns, AFTER the on-container smoke test proves the install flow. If zerops.yaml exists at scaffold-return time the main agent will flag it and rewrite it.
> - Item CRUD endpoints, item list components, create-item forms, item detail views
> - Cache-demo routes, cached-vs-fresh components
> - Search endpoints or search UI
> - Jobs-dispatch endpoints, jobs UI, jobs history tables, worker job processors
> - Storage upload endpoints, file list components, upload forms
> - Anything that calls a managed service beyond the one connectivity ping in `/api/status`
> - Rich UX: feature-level forms, tables with headers, submit-state badges, contextual hints, `$effect` hooks that auto-load feature data, inline section-level styles. (The `api.ts` helper, the `data-feature="status"` wrapper on StatusPanel, the `data-error` slot, and the three-state render pattern from `client-code-observable-failure` ARE part of the scaffold — they are structural correctness, not "rich UX".)
> - Routing, tabs, layouts with multiple sections, nav components, pagination
> - CORS config, proxy rules, `types.ts` shared between codebases — the main agent resolves cross-codebase integration during verification
>
> **The dashboard you ship is one green-dot panel.** A reader looking at the deployed page should see five rows: `db • green`, `redis • green`, `nats • green`, `storage • green`, `search • green` (with the service names from the plan). That is the correct, expected, final output of the scaffold phase. The feature sub-agent at deploy step 4b builds every showcase section on top of this — owning API routes, frontend components, and worker payloads as a single coherent author — so the dashboard at close time is rich and feature-complete. If you are tempted to add a "small demo" or "minimal example" of any managed service, stop: that is the feature sub-agent's job.
>
> **Reporting back:** return a bulleted list of the files you wrote and the env var names you wired for each managed service. Do not claim you implemented any features. You didn't. If your return value makes the main agent think step 4b is already done, the brief was not followed.
>
> **Self-review before reporting back (v8.78).** Before you return your file list, re-read your own output against the rules in this brief and flag any deviations. Specifically:
>
> 1. **Imports + decorators verified against installed packages?** Every `import` line you committed should map to a path that exists in `node_modules/<pkg>/package.json` exports (or the equivalent manifest for non-Node stacks). The stale-major class (NestJS 8 `CacheModule` import path used in a NestJS 10 project) cost v19 a close-step CRITICAL — verify, don't trust memory.
> 2. **All commands ran via SSH, not zcp-side?** Scan your bash history for any `cd /var/www/{hostname}` followed by an executable command (npm/npx/nest/vite/tsc/...). If found, that step ran on zcp instead of the container. Re-do it via `ssh {hostname} "cd /var/www && <command>"` so the install/build artifacts have the right uid + ABI.
> 3. **Did you write README.md or zerops.yaml?** Either is a brief violation. Delete and report only the language-level scaffolding you wrote.
> 4. **Is the dashboard ONE panel?** If you shipped multiple feature sections, you exceeded the brief. The feature sub-agent owns features.
> 5. **`.gitignore` + `.env.example` present?** Run `ssh {hostname} "ls -la /var/www/.gitignore /var/www/.env.example"`. Both must exist. `.gitignore` must list `node_modules/`, the framework's build output dir, and `.env`. If either is missing, write it before returning — the `scaffold_hygiene` deploy-step check rejects anything without these files, and the v21 apidev scaffold shipped 208 MB of `node_modules` into the published recipe because this check didn't exist.
> 6. **No `node_modules/`, `dist/`, or `.DS_Store` on the mount at return time?** Scan with `ssh {hostname} "find /var/www -maxdepth 2 -name node_modules -o -name dist -o -name .DS_Store | head"`. The `node_modules/` inside the dev container is fine — `.gitignore` excludes it from any subsequent publish. Delete `.DS_Store` files; they have no legitimate place in the tree.
>
> List any deviations explicitly in your report so the main agent can validate. Silent self-correction is fine; surfacing the deviation lets the main agent learn whether the brief needs tightening.

</block>

<block name="asset-pipeline-consistency">

### Asset pipeline consistency

If `buildCommands` compiles assets (JS, CSS, or both), the primary view/template MUST load the compiled outputs via the framework's standard asset inclusion mechanism. Inline `<style>` or `<script>` blocks that bypass the build output are forbidden when a build pipeline exists. A build step that produces assets nobody loads is dead code. To verify: if zerops.yaml prod `buildCommands` produces built CSS/JS, check that the primary view/template references them through the framework's asset helper. This is the generate-step corollary of research decision 5 (scaffold preservation).

</block>

<!-- v14: the readme-with-fragments block moved to the deploy section so
     READMEs are written during the post-verify readmes sub-step, where
     the gotchas section narrates real debug experience instead of
     research-time speculation. Block content lives with its step. -->

<block name="code-quality">

### Code Quality
- Comment ratio in zerops.yaml code blocks must be >= 0.3 — **aim for 35%** to clear the threshold comfortably on the first attempt. Agents consistently underestimate; writing to 30% lands at 25%.
- No `PLACEHOLDER_*`, `<your-...>`, or `TODO` strings
- All env var references must use discovered variable names
- Comments explain WHY, not WHAT (don't restate the key name)
- Max 80 chars per comment line

</block>

<block name="init-script-loud-failure">

### Init-phase scripts must fail loudly — no silent swallow

**Init-phase scripts** are any executable run during container start from `initCommands` or the framework's equivalent boot hook: migrations, seeders, cache warmers, search-index syncers, one-shot provisioners. They run inside `execOnce` gates and their exit code is the deploy's proof of "infrastructure is ready before we serve traffic." Silent swallowing in these scripts turns deploy verification into a lie.

**The rule (three parts):**

1. **No broad `try/catch` that logs and continues.** If the catch block's only action is `console.error("… failed (non-fatal):")` followed by a return, delete it. Either the error is recoverable — in which case name the recovery inline — or it's fatal — in which case `throw` / `exit 1` / `panic`. "Non-fatal" labels on production-path init code are a bug pattern that v18 shipped: a Meilisearch sync failure was swallowed into a console.error, the seed exited 0, `execOnce` recorded success, the container served traffic, and the search feature was permanently broken because the index was never materialized.

2. **Async-durable writes must block until side effects are observable.** If the SDK returns a task/future/promise with deferred completion semantics (Meilisearch `TaskInfo`, Elasticsearch bulk operations, Kafka producer `flush()`, S3 multipart `CompleteMultipartUpload`, Postgres `NOTIFY` handshake), the script must await the completion signal before exit. "The library returned a success object" is not the same as "the side effect is durable." Applied instances the scaffold subagent identifies during research:
   - **Meilisearch**: `await client.index(...).addDocuments(docs)` returns a `TaskInfo` — follow with `await client.index(...).waitForTask(task.taskUid)` (or `client.waitForTask`, depending on SDK version). Same pattern for `updateSearchableAttributes` and `updateFilterableAttributes`.
   - **Elasticsearch / OpenSearch**: bulk `refresh: "wait_for"` or explicit `indices.refresh()`.
   - **Kafka / Pulsar producers**: `producer.flush()` before close.
   - **Message brokers** requiring handshake (NATS JetStream ack, RabbitMQ confirms): await the ack / confirm before proceeding.

3. **Lazy client libraries must be warmed.** If a client library connects lazily on first request, the init script must force the connection via a trivial round-trip (e.g. a ping, a tiny query, a zero-byte write with cleanup). Otherwise the first real request pays the connect cost AND surfaces connection errors to a user instead of to the script.

**Script exit is the deploy's proof.** A seed or migration script that exits 0 is a promise that every side effect it attempts to produce has actually been produced. If the script cannot make that promise, it must exit non-zero. This is how the `feature-sweep-dev` sub-step becomes a meaningful gate: any script that silently skipped work will surface as a `text/html` response or 500 when the sweep hits that feature's endpoint.

**Recovery belongs in runtime, not init.** If a managed service is genuinely optional (Meilisearch can be unavailable and the app should still boot), the recovery is a runtime health-check-gated re-sync triggered on first request, not a silent swallow in the init path. Init path commits to full correctness; runtime path carries the resilience.

</block>

<block name="client-code-observable-failure">

### Scaffolded client code must surface failures visibly

**Every `fetch` / `axios` / `request` / framework HTTP client call the scaffold writes** must treat a non-success response as a user-visible error state, not a silent empty render. A showcase is a demonstration of correct patterns — code that happens to work on the happy path and silently breaks on the sad path teaches users to write fragile code. Three rules the scaffold subagent enforces on every file it writes.

1. **`res.ok` before `res.json()`.** Every fetch wrapper checks status before parsing the body. A 500 with a valid JSON error body does NOT trigger the outer `catch`; it slides past and pollutes the consumer store with undefined fields. Explicit check, explicit throw, explicit visible error.

   ```ts
   const res = await fetch(url);
   if (!res.ok) throw new Error(`${url}: ${res.status} ${res.statusText}`);
   ```

2. **Content-type verification on JSON endpoints.** Every fetch that expects JSON must verify the response's content-type before calling `.json()`. `text/html` on a `/api/*` path means nginx SPA fallback (the v18 trap); any non-JSON content-type is a bug, not an empty result.

   ```ts
   const ct = res.headers.get("content-type") ?? "";
   if (!ct.toLowerCase().includes("application/json")) {
     throw new Error(`${url}: non-JSON content-type ${ct}`);
   }
   ```

3. **Array-consuming stores default to `[]`, never `undefined`.** TypeScript does not save you: `data.hits` reads as `any` under common framework types and binds to a reactive store without a type error. The downstream template calls `.length` on undefined and the whole component crashes. Every store declaration names its default:

   ```ts
   let items: Item[] = $state([]);       // Svelte 5 runes
   const [items, setItems] = useState<Item[]>([]);  // React
   items: ref<Item[]>([]),               // Vue
   ```

   After the fetch, assign the exact shape from the API contract:

   ```ts
   const data = await res.json();
   items = Array.isArray(data.items) ? data.items : [];  // defensive parse
   ```

4. **Three render states per async section, not two.** Every dashboard section that fetches data must explicitly handle:
   - **Loading** (request in flight — spinner or skeleton)
   - **Error** (request failed — visible red banner / toast with the error message)
   - **Empty** (request succeeded but returned zero rows — "no results yet" text)
   - **Populated** (normal render)

   A scaffold that conflates "error" and "empty" into the same render path hides broken features. The error state must be explicit and visible so `zerops_browser` can observe it during the walk and the main agent can react.

5. **`data-feature` test hooks on every feature section.** Every section the scaffold emits for a feature declared in `plan.Features` must carry a `data-feature="{feature.uiTestId}"` attribute on its outer wrapper. The browser walk uses this to locate the section deterministically. Without it, the walk either matches nothing or matches the wrong element and reports a false positive.

   ```svelte
   <!-- Svelte 5 example — use a div wrapper (not an HTML section tag) -->
   <div data-feature="items-crud" class="feature-section">
     <h2>Items</h2>
     {#if loading}
       <p>Loading…</p>
     {:else if error}
       <p class="error" data-error>{error}</p>
     {:else if items.length === 0}
       <p>No items yet.</p>
     {:else}
      <ul>
        {#each items as item (item.id)}
          <li data-row>{item.title}</li>
        {/each}
      </ul>
     {/if}
   </div>
   ```

The four states + the test hook are not optional polish — they are the observable surface the deploy feature sweep and browser walk target. A scaffold that omits them defeats verification.

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
- [ ] **(showcase only)** Dashboard is the health-dashboard skeleton ONLY — `<StatusPanel />` rendering one dot per managed service, plus `/api/health` and `/api/status`. No item CRUD, no cache-demo, no search UI, no jobs dispatch, no storage upload. **Feature sections belong to the feature sub-agent at deploy step 4b.** If you find yourself writing ItemsList or JobsDemo at generate, stop — re-read `scaffold-subagent-brief`.
- [ ] **(showcase only)** Seeder populates 3-5 rows of primary-model sample data. The feature sub-agent at deploy step 4b will expand seeds when it builds the features that need more.
- [ ] **(showcase only)** Search index population goes into `initCommands` — but the feature sub-agent at deploy step 4b adds it, not the scaffold. The scaffold leaves search wiring initialized but no sync step.

</block>

<block name="on-container-smoke-test">

### On-container smoke test

The dev containers are live development environments — validate code ON the container before deploying. `zerops_deploy` triggers a full build cycle (30s–3min); catching dependency errors, type errors, and startup crashes on the container takes seconds.

**Three validation steps** (derive every command from the plan's research data — package manager, compile command, start command, HTTP port):

1. **Install dependencies on the container** — run the plan's package manager install command via SSH on each dev container. This catches hallucinated packages, version conflicts, and peer dependency mismatches in seconds instead of after a build cycle.
   ```
   ssh {hostname}dev "cd /var/www && {packageManagerInstallCommand}"
   ```

2. **Compile/check** (if the framework has a compilation step) — run the relevant compile or type-check command from the plan's research data. This catches type errors, syntax errors, and missing imports.
   ```
   ssh {hostname}dev "cd /var/www && {compileOrTypeCheckCommand}"
   ```

3. **Start the dev server** — start the dev process and verify it binds to the expected port from the plan's research data. Connection errors to managed services are EXPECTED (env vars are not active yet). The goal is "process starts and binds to the port", not "app serves requests." If the process crashes immediately, this catches native binding mismatches, missing modules, and config errors.
   - **Implicit-webserver runtimes** (php-nginx, php-apache, nginx): Skip the start command — the webserver auto-starts when the container is in RUNNING state. Verify by curling the port directly:
     ```
     ssh {hostname}dev "curl -s -o /dev/null -w '%{http_code}' http://localhost:{httpPort}/"
     ```
   - **All other runtimes**: start the dev process explicitly:
     ```
     ssh {hostname}dev "cd /var/www && {startCommand} &"
     sleep 3
     ssh {hostname}dev "curl -s -o /dev/null -w '%{http_code}' http://localhost:{httpPort}/ || echo 'port not bound'"
     ```

**What's available vs what's not**: these commands use only the base image's tools (runtime + package manager). `run.envVariables` are NOT available yet — that's fine, the smoke test doesn't need them. The constraint "do not run commands that bootstrap the framework" means "don't connect to databases", NOT "don't validate your code compiles."

**Failure handling**: if the smoke test catches an error, fix it on the mount and re-run the failing step. Only proceed to `zerops_deploy` when all three steps pass. Do NOT commit and deploy hoping the build container will produce a different result — it won't.

**Multi-codebase**: for plans with multiple dev mounts, run the smoke test on each container independently.

</block>

<block name="comment-anti-patterns">

### Comment formatting anti-patterns

These produce section-heading comments and decorators that label structure rather than explain decisions. The YAML structure itself provides grouping — comments explain WHY.

- Don't add section-heading comments with decorators (`# -- Dev Runtime --`, `# === Database ===`, `# ----------`)
- Don't restate the key name ("# Set the build base" on `base: php@8.4`)
- Don't write generic descriptions ("# This is the build section")
- Don't write single-word comments ("# dependencies", "# port")
- Don't compress to telegraphic style ("# static bin, no C" — write full sentences)
- Don't explain YAML syntax itself

</block>

<block name="completion">

### Completion
```
zerops_workflow action="complete" step="generate" attestation="App code and zerops.yaml written to dev mounts. On-container smoke test passed on all dev mounts. README with 3 fragments."
```

</block>
</section>

<section name="generate-fragments">
## Fragment Quality Requirements

**Two separate outputs per codebase, two separate audiences — never collapse them.**

| File | Audience | Published? | Scope |
|------|----------|------------|-------|
| `README.md` (fragments) | Integrator bringing their own codebase | ✅ extracted to zerops.io/recipes | "What must I change in MY app?" + "What Zerops trap will bite me?" |
| `CLAUDE.md` | Anyone (human or Claude Code) cloning THIS repo | ❌ repo-local only | "How do I operate THIS dev container?" |

Every container-trap, every "npx tsc resolves wrong", every "SSH into the dev container and run X" goes in CLAUDE.md. Every "Zerops L7 balancer terminates SSL" or "add trust-proxy to Express" goes in README.md. If you find yourself wondering which file a fact belongs in, ask: *"Would a reader who never touches this specific repo still care?"* Yes → README.md. No → CLAUDE.md.

The fragment format below applies ONLY to README.md. CLAUDE.md has no fragments, no extraction rules, no authenticity scoring — see the "CLAUDE.md" section below for its guidance.

### integration-guide Fragment

The integration guide answers: **"What must I change in my existing app to run it on Zerops?"** It targets a developer bringing their own codebase, not someone cloning the demo. Platform-level truth only — no repo-operations trivia.

Must contain (all inside the markers, using **H3** headings):
- **`### 1. Adding \`zerops.yaml\``** — complete config with ALL setups (`prod`, `dev`; `worker` if the target hosts a shared-codebase worker). Setup names are generic (`prod`/`dev`), NOT hostname-specific. Every config line has an inline comment explaining WHY.
- **Numbered integration steps** (`### 2. Step Title`, `### 3. Step Title`, ...) — each is a concrete code or config change the reader must apply to their own codebase. Each step MUST include a fenced code block (typescript, js, python, php, yaml...) showing the minimal diff.

**What belongs in integration steps:**
- Platform-forced code changes: bind `0.0.0.0`, trust-proxy for Express, Vite `allowedHosts: ['.zerops.app']`, NATS client options, S3 `forcePathStyle`, etc.
- Framework-config wiring for platform credentials (ORM env vars, cache adapter setup, object-storage client init).
- Any single-line change a user would need to copy-paste when porting their own app.

**What does NOT belong in integration steps:**
- **Repo-operations content** — "how to SSH into the dev container", "how to restart the dev server after a crash", "sudo chown to fix SSHFS uid", "fuser -k to free a stuck port". That is CLAUDE.md territory, not the published recipe page.
- Demo-specific scaffolding (custom routes, dashboard views, sample controllers) — these exist only in this recipe, a real integrator wouldn't replicate them.
- Config values already visible in the zerops.yaml comments above.
- Generic framework tutorials (how to install the framework, what build tools do).

**Upper bound: 6 numbered steps per README.** Beyond 6 and you are either mixing repo-ops in (move them to CLAUDE.md) or not choosing ruthlessly (cut the least-impactful step). v15 appdev hit the sweet spot at 4.

**Each IG item must stand alone (v8.78 enforcement — `<codebase>_ig_per_item_standalone`).** Aggregate IG-fragment floors invite leaning on neighbors — v20 apidev IG #2 ("Binding to `0.0.0.0`") was 3 sentences plus 2 lines of code; the explanation lived in the comment block of the zerops.yaml shown in IG #1. The reader of IG #2 had to back up to IG #1 to understand the why.

Per-item rule:
- ≥1 fenced code block (an IG item without code is prose narration, not an integration step)
- The first prose paragraph (before the first code block) must contain at least one platform-anchor token — a Zerops actor (`Zerops`, `L7 balancer`, `runtime container`, `static base`, `SSHFS mount`), a Zerops mechanism (`zsc`, `execOnce`, `healthCheck`, `readinessCheck`, `subdomain`, `initCommands`, `deployFiles`, `httpSupport`, `enableSubdomainAccess`), or a service-discovery env-var pattern (`${X_hostname}`, `${X_password}`, etc.)

If the why for this step is in another section, **copy the relevant sentence here** — items must teach independently.

### knowledge-base Fragment

The knowledge base answers: **"What symptom will I observe when this breaks, and what's the one-line cause?"** Each gotcha is a distinct failure-mode narration — NOT a second telling of the integration-guide items above.

Must contain:
- `### Gotchas` section with 3–6 bullets (hard floor: 3 authentic, 3 net-new vs predecessor; hard ceiling: 6 — pick ruthlessly)
- Each bullet: `- **<stem>** — <body>` where `<stem>` names the symptom or the surprising behavior, `<body>` explains WHY in 1–3 sentences

**Two hard dedup rules enforced by the checker:**

1. **A gotcha must NOT restate an integration-guide heading in the same README.** If your gotcha stem normalizes to the same tokens as an IG heading (67%+ overlap after stopword strip), the `<codebase>_gotcha_distinct_from_guide` check fails. A gotcha that tells the reader what the guide already said is wasted publication surface.
   - ❌ IG: "Add `.zerops.app` to Vite `allowedHosts`" + Gotcha: "Vite `allowedHosts` blocks Zerops subdomain" — fails.
   - ✅ IG: "Add `.zerops.app` to Vite `allowedHosts`" + Gotcha: "Blocked subdomain returns plain-text HTTP 200 — health checks pass while the browser shows a blank page" — passes (the symptom-framed stem carries distinct tokens).

2. **A gotcha must NOT appear in more than one codebase's README.** If NATS credentials need separate `user`/`pass` options, that fact lives in ONE README (api by convention) and the others say "See apidev/README.md §Gotchas for NATS credential format." The `cross_readme_gotcha_uniqueness` check fails when any normalized stem appears in two+ READMEs.

**Good sources for genuine gotchas:**
- Managed-service platform quirks: "Meilisearch connects over `http://` not `https://`", "Valkey has no auth, passing `password: ''` triggers NOAUTH handshake rejection".
- Symptom-specific framework × platform intersections: "NATS `AUTHORIZATION_VIOLATION` with URL-embedded creds — the client silently ignores them".
- `@nestjs/microservices` + Node `exports` map + missing `package.json` in `deployFiles` → `MODULE_NOT_FOUND` even though the package IS in `node_modules`.
- Any symptom observable in the browser/logs that is not obvious from reading the guide.

**The injected predecessor recipe's gotchas are a starting inventory.** Re-evaluate each against this recipe's library choices — keep the ones that still apply (predecessor overlap is fine — recipes are read in isolation), drop the ones that don't (swap TypeORM for Prisma → drop the `synchronize: true` gotcha), and add gotchas narrated from THIS build for the services and platform behaviors the predecessor doesn't cover. The v8.78 `service_coverage` check is the new authoritative gate; the `exceeds_predecessor` check is now informational only.

**Do NOT include:**
- Config values already visible in the zerops.yaml comments (readers see them inline).
- Platform universals (build/run separation, L7 routing, tilde behavior, autoscaling) — these live in Zerops docs.
- **Repo-operations trivia** (npx tsc wrong-package, SSHFS uid, fuser -k, how to restart dev after crash) — **move these to CLAUDE.md** where they actually help the reader working in the cloned repo.
- Generic framework knowledge (how the framework works, what build tools do).

#### v8.78 enforcement — every gotcha must be load-bearing

The v8.67–v8.77 reforms enforced presence (terms exist, fragments well-formed). v8.78 enforces **load-bearing-ness** per gotcha. A load-bearing gotcha is one where, if you removed it or replaced it with generic Node/PHP/Ruby advice, *something the reader needs to know would disappear*. Three structural rules, all enforced per-codebase:

**1. Causal-anchor (`<codebase>_gotcha_causal_anchor`).** Every gotcha must satisfy BOTH halves:
- (a) **Specific Zerops mechanism** — names a token from a curated narrow list (`L7 balancer`, `execOnce`, `readinessCheck`, `subdomain`, `${X_hostname}`, `httpSupport`, `serviceStackIsNotHttp`, `SSHFS mount`, `initCommands`, `deployFiles`, `minContainers`, `corePackage`, `mode: HA`/`NON_HA`, `static base`, plus managed-service brand names: `Valkey`, `PostgreSQL`, `NATS`, `Meilisearch`, `Object Storage`, `MinIO`). NOT generic terms like `container`, `service`, or bare `envVariables`.
- (b) **Concrete failure mode** — an HTTP status code, a quoted error name in backticks (`AUTHORIZATION_VIOLATION`, `serviceStackIsNotHttp`, `QueryFailedError`), a named exception, or a strong symptom verb (`rejects`, `deadlocks`, `drops`, `crashes`, `times out`, `breaks`, `hangs`, `silently`, `forever`).

A gotcha that names only generic platform terms (e.g. ".env file overrides Zerops-managed values" — which describes no platform-caused failure mechanism, since Zerops doesn't read `.env` files at runtime) is generic Node advice mis-anchored, not a Zerops gotcha.

**2. Service coverage (`<codebase>_service_coverage`).** Each managed service the codebase exercises must have at least one gotcha that names it. Mention by brand (`PostgreSQL`/`Postgres`, `Valkey`/`Redis`, `NATS`, `Object Storage`/`MinIO`/`S3`, `Meilisearch`) OR by service-discovery env-var prefix (`${db_hostname}`, `${redis_password}`, etc.). For the API codebase: every managed-service category in the plan. For workers: db + queue. For static frontends: no requirement.

**3. Reality (`<codebase>_content_reality`).** Every file path and every top-level code symbol named in a gotcha or IG bullet must EITHER exist in the codebase OR be framed as advisory in the surrounding prose. v20 had two violations of this:
- appdev gotcha cited `_nginx.json` with `proxy_pass` as a fix, but `_nginx.json` was never shipped — a reader trying to apply the fix found nothing to edit.
- workerdev gotcha imperatively said "Implement an internal watchdog" with full `setInterval` code declaring `lastActivity`, but no watchdog symbol existed in `src/`.

If the artifact is implemented, name the file (`ships in src/watchdog.ts`). If it's a pattern the reader could adopt but the recipe doesn't ship, frame the prose as advisory: `Pattern to add if…`, `If your worker has long-running handlers…`, `Consider adding…`, `One approach…`. Either path passes; declarative-without-implementation fails.

The `.env` gotcha class is the canonical decorative case — it scored under the v8.67 platform-terms classifier (mentions `envVariables` + `container`) but fails causal-anchor (no specific mechanism, no concrete failure). Generic Node advice ("don't commit `.env`") belongs in framework documentation, not in a Zerops gotcha — Zerops never reads `.env` at runtime regardless.

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
- **Numeric claims in comments must match the adjacent YAML value.** If a comment says "10 GB quota" and the next line is `objectStorageSize: 1`, the comment is lying — it lies to the reader, it lies to the agent who reads the recipe later as a predecessor, and the finalize check will reject it. The enforced patterns today are storage quota (`N GB` vs `objectStorageSize`) and container count (`minContainers N` vs `minContainers:`). Either write the real number, drop the number from the comment, or phrase it aspirationally ("1 GB default — bump via the GUI when usage grows"). Aspirational phrasing skips the check because it names a future value, not the current one. Non-numeric facts (`mode: HA`, `cpuMode: DEDICATED`, `corePackage: SERIOUS`) aren't linted yet but the same discipline applies — don't narrate a fact you haven't actually configured.

Writing-style voice (the "developer to developer" tone, anti-patterns, correct-style example) lives at **finalize** under "Comment style" — read it there when you write `envComments`. The same voice applies to the zerops.yaml comments you write here.
</section>

<section name="deploy">
## Deploy — Build, Start & Verify

<block name="deploy-framing">

`zerops_deploy` processes the zerops.yaml through the platform — this is when `run.envVariables` become OS env vars and cross-service references (`${hostname_varname}`) resolve to real values. Before this step, the dev container had no service connectivity. After this step, the app is fully configured.

**Always pass `targetService` AND `setup` together.** They are two distinct coordinates: `targetService` is the service hostname (`apidev`, `apistage`, `workerdev`, ...), `setup` is the zerops.yaml setup block name (recipes use `dev`/`prod`, shared-codebase worker recipes add `worker`). One zerops.yaml's `setup: prod` block serves both `apistage` (as the target) and a cross-deploy from `apidev` → `apistage`. A missing `setup` is the most common first-deploy failure — the tool can resolve it via role fallback for standard hostnames, but be explicit: `targetService=apidev setup=dev`, `targetService=apistage setup=prod`, `targetService=workerdev setup=dev` (separate-codebase worker) / `setup=worker` (shared-codebase worker inside the host target's zerops.yaml).

</block>

<block name="deploy-execution-order">

### Deploy execution order by recipe type

**Read this before following individual step numbers.**

The step numbers below are reference labels, NOT a linear script. For dual-runtime (API-first) recipes the steps interleave because the frontend depends on the API being verified first:

| Recipe type             | Order                                                                                                                            |
| ----------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| Single-runtime (non-showcase) | **Step 1 → Step 2 (2a/2b/2c) → Step 3 → Step 3a → Step 4**                                                                |
| Single-runtime (showcase) | **Step 1 → Step 2 (2a/2b/2c) → Step 3 → Step 3a → Step 4 → Step 4b → Step 4c**                                              |
| Dual-runtime (API-first) | **Step 1-API → Step 2a-API → Step 3-API (verify apidev only) → Step 1 → Step 2 (2a/2b/2c) → Step 3 → Step 3a (BOTH containers) → Step 4 → Step 4b → Step 4c** |

API-first teams: the steps labelled `-API` run FIRST; do not try to verify `appdev` (Step 3) before `appdev` has been deployed (Step 1). Step 3a runs once, at the end, reading logs from both `apidev` and `appdev` together.

> **Parameter naming**: the deploy parameter is `targetService` (NOT `serviceHostname`). `serviceHostname` is used by `zerops_mount`, `zerops_subdomain`, `zerops_verify`, `zerops_logs`, and `zerops_env` — deploy is the exception. If you get `unexpected additional properties ["serviceHostname"]`, you used the wrong name.

</block>

<block name="deploy-core-universal">

### Dev deployment flow

**Step 1: Deploy appdev (self-deploy)**
```
zerops_deploy targetService="appdev" setup="dev"
```
The `setup="dev"` parameter maps hostname `appdev` to `setup: dev` in zerops.yaml. This triggers a build from files already on the mount. Blocks until complete.

**Step 2: Start ALL dev processes (before any verification)**

Every process the app needs to serve a page must be running before Step 3 (verify). This includes the primary server, asset dev servers, and worker processes. Start them all now:

**2a. Primary server:**
- **Server-side apps** (types 1, 2b, 3, 4): Start via SSH:
  ```bash
  ssh appdev "cd /var/www && {start_command} &"
  ```
- **Implicit-webserver runtimes** (php-nginx, php-apache, nginx): Skip — auto-starts.
- **Static frontends** (type 2a): Skip — Nginx serves the built files.

**Step 3: Enable subdomain and verify appdev**
```
zerops_subdomain action="enable" serviceHostname="appdev"
zerops_verify serviceHostname="appdev"
```
Check: service RUNNING, subdomain returns 200, health endpoint responds (or page loads for static).

**Step 3a: Verify `initCommands` actually ran — check logs, don't assume** (runs AFTER Step 3)

If `setup: dev` declares `initCommands` (migrate / seed / search-index), those commands ran during deploy activation — the platform invokes them on every fresh deploy, including the first one on an idle-start container. You MUST verify they ran and succeeded by reading the runtime logs, NOT by re-running them manually:

```
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

**Post-deploy data verification**: after a successful deploy, verify the expected data actually exists — don't assume initCommands ran just because the deploy returned ACTIVE. If prior failed deploys burned the `execOnce` key, the successful deploy may skip those commands silently. Check: query the database for seeded records, verify the search index contains documents, confirm the cache is populated. If the data is missing, the `execOnce` key was burned — use recovery option (a) or (b) above.

**Redeployment = fresh container.** If you fix code and redeploy during iteration, the platform creates a new container — ALL background processes (asset dev server, queue worker) are gone. Restart them before re-verifying. This applies to every redeploy, not just the first.

**Step 4: Iterate if needed** (max 3 iterations)
If verification fails: check logs (`zerops_logs serviceHostname="appdev"`), fix code on mount, kill previous server, restart via SSH, re-verify. After any redeploy, repeat Step 2 (start ALL processes) before Step 3 (verify).

</block>

<block name="deploy-api-first">

**Step 1-API** (API-first showcase only, runs BEFORE Step 1): Deploy apidev FIRST — the API must be running before the frontend builds (the frontend bakes the API URL at build time):
```
zerops_deploy targetService="apidev" setup="dev"
```
After this completes, run Step 2a-API (start the API process) then Step 3-API (verify apidev); THEN return to Step 1 to deploy appdev.

**2a-API** (API-first): Start the API server on apidev:
```bash
ssh apidev "cd /var/www && {api_start_command} &"
```

**Step 3-API** (API-first only, runs AFTER Step 1-API + Step 2a-API, BEFORE Step 1): Enable and verify the API FIRST — this is a checkpoint before the frontend deploy, not a late verification step:
```
zerops_subdomain action="enable" serviceHostname="apidev"
zerops_verify serviceHostname="apidev"
```
Verify `/api/health` returns 200 via curl. THEN return to Step 1 to deploy appdev — the frontend needs the API running before it can deploy (in build-time-baked configurations) or before it can be verified (in runtime-config configurations). After appdev deploys, Step 2 (processes) → Step 3 (enable appdev subdomain + verify the dashboard loads and successfully fetches from the API) → Step 3a (logs from BOTH containers).

**API-first log reading**: API-first recipes must fetch logs from BOTH containers at Step 3a — the API typically owns the migration/seed commands and the frontend is often a static build with no initCommands at all:

```
zerops_logs serviceHostname="apidev" limit=200 severity=INFO since=10m
zerops_logs serviceHostname="appdev" limit=200 severity=INFO since=10m
```

**CORS** (API-first): The API must set CORS headers allowing the frontend subdomain. Use the framework's standard CORS middleware and allow the frontend's subdomain origin.

</block>

<block name="deploy-asset-dev-server">

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

</block>

<block name="deploy-worker-process">

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

</block>

<block name="deploy-target-verification">

**Verify ALL runtime targets — not just the primary app.** After completing dev deploys, every runtime target must be verified. HTTP targets use `zerops_verify` + `zerops_subdomain`; non-HTTP targets (workers) use `zerops_logs` to confirm the process started. Enumerate by plan shape:

- **Single-runtime minimal**: `appdev` (HTTP — verify + subdomain)
- **Single-runtime showcase (shared worker)**: `appdev` (HTTP — verify + subdomain; worker logs also live in `appdev` since the worker runs as a background process on the host target's container)
- **Single-runtime showcase (separate worker)**: `appdev` (HTTP) + `workerdev` (logs only — no HTTP endpoint)
- **Dual-runtime minimal**: `appdev` (HTTP) + `apidev` (HTTP)
- **Dual-runtime showcase**: `appdev` (HTTP) + `apidev` (HTTP) + `workerdev` (logs only)

Do not skip any target. A skipped verification means a broken target ships to stage undetected.

For showcase, also verify the worker is running via logs (no HTTP endpoint). The worker's log hostname depends on the recipe's `sharesCodebaseWith` shape:

- **Shared-codebase worker** (`sharesCodebaseWith: app` or `sharesCodebaseWith: api`) — the worker runs in the HOST target's container, so its logs live there. Use `zerops_logs serviceHostname="appdev"` for an app-shared worker, `serviceHostname="apidev"` for an api-shared worker.
- **Separate-codebase worker** (`sharesCodebaseWith: ""`, the default for dual-runtime API-first recipes) — the worker owns its own container. Use `zerops_logs serviceHostname="workerdev"`.

```
zerops_logs serviceHostname="{worker_hostname}" limit=20
```

</block>

<block name="dev-deploy-subagent-brief">

**Step 4b: Dispatch the feature sub-agent — enforced sub-step for Type 4 showcase**

After the scaffold's health dashboard is deployed and every service dot is green, dispatch **ONE** framework-expert sub-agent as a single author that owns every feature section end-to-end: API routes, worker payloads, and frontend components as one coherent unit. This is where feature implementation happens. The scaffold at generate writes the health-dashboard-only skeleton (see `scaffold-subagent-brief`); the feature sub-agent writes **everything else**.

**Single author, not parallel authors.** v10, v11, and v12 all shipped the same recurring class of contract-mismatch bugs (frontend reading `.hits` while API returns `{ hits: [] }`, worker reading `payload.jobType` while API publishes `type`, etc.) because scaffold agents ran in parallel and each owned one slice of the contract. A single feature sub-agent authoring both sides of every API/worker/frontend contract eliminates the class entirely. Do NOT split this into multiple parallel feature sub-agents.

**This is an enforced sub-step** — not a prose "MANDATORY" label. The deploy step's full-step complete is gated on `zerops_workflow action="complete" step="deploy" substep="subagent" attestation="<description of what the feature sub-agent produced>"`. The validator rejects empty and boilerplate attestations; the attestation must name the files the sub-agent wrote and the feature sections it implemented.

**Do NOT read the existing scaffold code to decide whether this is needed.** The scaffold is deliberately bare — a single `StatusPanel` component showing five green dots, `/api/health`, `/api/status`, service client initializers, schema + small seed, and nothing else. If the deployed app looks feature-complete, the scaffold brief was not followed at generate; re-audit the scaffold output and dispatch the feature sub-agent to bring the dashboard up to quality.

**Before dispatching the subagent**: kill ALL dev server processes on every dev container. The subagent starts fresh — leftover processes holding ports cause contention (`fuser -k {port}/tcp` retry loops waste minutes). Run:
```
ssh {hostname}dev "pkill -f '{dev_server_process}' || true; fuser -k {httpPort}/tcp 2>/dev/null || true"
```
Do this for every dev container (appdev, apidev if dual-runtime).

Minimal recipes (1-2 feature sections) skip the sub-agent entirely — the main agent writes features inline during generate.

**Sub-agent brief — required contents**:

- **Installed-package verification rule (FIRST line of the dispatch prompt).** Open the prompt verbatim: *"Before writing any import, decorator, adapter registration, or module-wiring call, verify the symbol / subpath against the installed package on disk — read `node_modules/<pkg>/package.json` (Node), `vendor/<pkg>/composer.json` (PHP), the module's `go.mod` (Go), the `*.gemspec` (Ruby), or the equivalent manifest for this stack. Training-data memory for library APIs is version-frozen and will surface stale paths that compiled under prior majors but don't exist in the version installed here. The verification is one file read per package and is ALWAYS cheaper than a close-step review round-trip. When uncertain, run the installed tool's own scaffolder against a scratch directory and copy its import shapes verbatim — the installed version's scaffolder is authoritative."* The rule is framework-agnostic by design: no list of specific moves, no version table to maintain. The agent verifies against what's on disk every time.
- **The full `plan.Features` list, verbatim.** Every feature's `ID`, `Description`, `Surface`, `HealthCheck`, `UITestID`, `Interaction`, and `MustObserve` go into the dispatch prompt. The sub-agent is implementing exactly these features, no more, no less. The feature list is the contract the deploy sub-steps (feature-sweep-dev, browser-walk, feature-sweep-stage) and the close-step review all iterate against — if a feature is not on the list, the sub-agent MUST NOT invent it; if a feature IS on the list, the sub-agent MUST implement it end-to-end (API route + frontend consumer + worker consumer where the surface includes `worker`).
- **Feature implementation rule**: for each feature `F`:
  - If `F.surface` includes `api`: implement an endpoint at `F.healthCheck` that returns 200 with `Content-Type: application/json`. For features that read existing data the endpoint is GET and returns a JSON array/object; for write features it accepts POST with a JSON body. The feature-sweep-dev sub-step WILL curl this path and WILL reject any `text/html` response.
  - If `F.surface` includes `ui`: emit a dashboard section wrapped in `<element data-feature="{F.uiTestId}">` with the four render states required by `client-code-observable-failure` (loading / error / empty / populated). The section's innards must contain whatever selectors the feature's `MustObserve` references (e.g., `[data-row]`, `[data-hit]`, `[data-result]`, `[data-error]`). All fetches go through `src/lib/api.ts` (the scaffold's helper) — the sub-agent NEVER calls `fetch()` directly from a component.
  - If `F.surface` includes `worker`: implement the worker consumer, the publishing endpoint on the API, and the result write-back (DB row, cache key, whatever the feature's `MustObserve` poll reads). Worker code obeys the loud-failure rule: no swallow-and-continue around JetStream ack or database writes.
  - If `F.surface` includes `search`: the search-sync step goes in `initCommands` (after `db:seed`) AND awaits task completion (Meilisearch `waitForTask` or equivalent). The scaffold intentionally left this out — the sub-agent adds it, not the scaffold.
- Every mount path the sub-agent owns — **apidev AND appdev AND workerdev** (when a separate-codebase worker exists). The sub-agent writes to all three as a single unit so API routes, worker payloads, and frontend consumers stay in contract lock-step. This is non-negotiable and is the single biggest reason v10/v11/v12 shipped contract-mismatch bugs — parallel authors cannot keep contracts consistent.
- **Contract-first rule**: for every feature section, the sub-agent defines the API response shape FIRST, the worker payload shape FIRST (if a worker is involved), then implements the backend, then consumes the same exact shape on the frontend via the `api.ts` helper. Frontend and backend for the same feature are written as adjacent edits, not as separate passes.
- **Seed expansion**: the scaffold left 3-5 rows. The sub-agent expands the seed to 15-25 records as part of implementing the features that need them. Seed script still obeys `init-script-loud-failure`: no broad try/catch, async writes (search index sync, cache warmups) must await durability before the script exits.
- **UX quality contract** (see below)
- **Where app-level commands run** (hard rule, see below) — include verbatim
- **Port hygiene**: before starting any dev server, kill any existing holder of the port first: `ssh {hostname}dev "fuser -k {httpPort}/tcp 2>/dev/null || true"`
- **Verify each feature as you write it** — the sub-agent has SSH access to every dev container and every managed service is reachable. After each controller + frontend pair, hit the endpoint via `ssh {hostname}dev "curl -sS -o /dev/null -w '%{http_code} %{content_type}\n' http://localhost:{port}{F.healthCheck}"` and verify it returns `200 application/json`. If it returns `200 text/html`, the frontend hit the SPA fallback — check the `api.ts` helper is being used, not a bare `fetch()`. Fix immediately; do not write ahead of verification.

**Managed service connection patterns** — before writing the sub-agent brief, use `zerops_knowledge query="connection pattern {serviceType}"` for every managed service in the plan. Include auth format, connection string construction, and known client-library pitfalls directly in the brief. Key pitfalls to inject:
- **Valkey/KeyDB (cache)**: no authentication — use `redis://hostname:port` without credentials. Do NOT reference `${cache_user}` or `${cache_password}`.
- **NATS (queue)**: credentials must be passed as separate connection options (`user`, `pass`), NOT embedded in the URL. URL-embedded credentials are silently ignored by most NATS client libraries.
- **Object Storage (S3)**: requires `apiUrl`, `accessKeyId`, `secretAccessKey`, `bucketName` — NOT the `connectionString` format used by databases.

**Dependency hygiene**: when adding packages, check the existing lockfile for the major version of the framework's core package. Pin new packages from the same framework family to the same major version. Run the install command after each batch of package additions to catch peer-dependency conflicts immediately.

**Feature sections the sub-agent owns end-to-end** — iterate `plan.Features` and for each feature, author the API route, the backing logic, the worker payload (if applicable), AND the frontend component that consumes the response, as a **single edit session** (not as separate passes). The feature list is authoritative — the guidance below is a reference for the typical feature shapes a showcase plan declares, but the sub-agent implements whatever the plan declares and nothing else:

- **Database feature** (surface includes `db`) — list seeded records + create-record form. Typed response interface, paginated table with headers and row shading, submit-state feedback. `data-feature="{uiTestId}"` wrapper, `data-row` on each row.
- **Cache feature** (surface includes `cache`) — store-a-value-with-TTL route + cached-vs-fresh demonstration showing timing. Cache is for cache + sessions only; the queue uses NATS, a separate broker.
- **Object storage feature** (surface includes `storage`) — upload-file (multipart) + list-files routes. Frontend form shows upload progress and a list of previously-uploaded files. `data-file` on each entry.
- **Search feature** (surface includes `search`) — live search over seeded records. Frontend debounces input and renders the result array. `data-hit` on each result. Seed must `await` the search-index task completion (see `init-script-loud-failure`).
- **Messaging broker + worker feature** (surface includes `queue` and/or `worker`) — dispatch-job POST publishes to a NATS subject; the worker consumes, does simulated work, writes the result to a DB table or Redis key; the frontend polls the result endpoint and renders (a) dispatched timestamp, (b) processed timestamp, (c) result payload. `data-processed-at`, `data-result`. This exercises the full NATS → worker → result round-trip.

**Every feature section must satisfy `client-code-observable-failure`** — loading / error / empty / populated render states, `data-feature` wrapper, `data-error` slot, fetches via `api.ts` helper. These are not optional polish; they are the observable surface the deploy feature sweep and browser walk target.

**Contract discipline — required in the sub-agent's dispatch prompt:**

> Before you write each feature section:
> 1. Decide the exact JSON shape the API returns and write it as a TypeScript interface (or language equivalent) FIRST, above the controller.
> 2. For sections with a worker: decide the exact payload shape the API publishes to NATS AND the exact result shape the worker writes back. Write both as shared types.
> 3. Implement the API route using the interface as the return type.
> 4. Implement the frontend consumer in the SAME edit session, referencing the same interface. Do not assume a response shape — read the interface you just wrote.
> 5. Smoke-test over SSH immediately: `curl` the endpoint and compare the actual response against the interface. Fix any mismatch before moving to the next feature.
>
> Shared types live in the API codebase (`apidev/src/types/` or equivalent). The frontend imports them if both codebases share TypeScript, or copy-pastes the interface into the frontend codebase if not. Either way, the shape is written ONCE, at the top, and both sides consume it.
>
> Contract-mismatch bugs (frontend reads `.hits`, API returns `{ hits: [] }`; worker reads `payload.jobType`, API publishes `type`) are the single biggest reason recipe close-step reviews flag bugs. Writing the interface first and reading both sides through it eliminates this entire class.

**UX quality contract** (what "dashboard style" means — include verbatim in the sub-agent brief):

The dashboard must be **polished** — minimalistic does NOT mean unstyled browser defaults. A developer deploying this recipe should not be embarrassed.

- **Styled form controls** — never raw browser-default `<input>` / `<select>` / `<button>`. Use scaffolded CSS (Tailwind if present) or clean inline styles: padding, border-radius, consistent sizing, focus ring, button hover
- **Visual hierarchy** — section headings delineated, consistent vertical rhythm, tables with headers + cell padding + alternating row shading
- **Status feedback** — success/error flash after submissions, loading text for async operations, meaningful empty states
- **Readable data** — aligned columns, relative timestamps ("3 minutes ago"), monospace for IDs
- System font stack, generous whitespace, monochrome palette + ONE accent color, mobile-responsive via simple CSS
- **Avoid**: component libraries, icon packs, animations, dark-mode toggles, JS frameworks for interactivity, inline `<style>` alongside a build pipeline
- **XSS protection (mandatory)**: all dynamic content escaped. `textContent` for JS-injected text; framework template auto-escaping for server-rendered content. Never use raw/unescaped output mode.

For the full "where commands run" principle (SSH vs zcp-side), see the `where-commands-run` block below. Include it verbatim in the sub-agent brief.

**After the sub-agent returns**:
1. Read back feature files — verify they exist and aren't empty
2. Git add + commit on every mount the sub-agent touched (apidev, appdev, workerdev as applicable)
3. Redeploy each affected dev service — fresh container, all SSH processes died, restart them (Step 2)
4. HTTP-level verification via curl on every feature endpoint
5. If anything fails, fix on mount, iterate (counts toward the 3-iteration limit)

</block>

<block name="where-commands-run">

### ⚠ Where app-level commands run — applies to main agent AND every sub-agent

This rule governs the main agent's bash calls AND every sub-agent's bash calls. Include this block verbatim in every sub-agent brief; the main agent reads it here. The cost of a violation is identical in both roles (v21: 3 parallel 120 s git-add hangs from main-agent-side `cd /var/www/X && git add -A`).

The zcp orchestrator container runs the main agent and every sub-agent. `/var/www/{hostname}/` on zcp is an **SSHFS network mount** — a bridge to the target container's own `/var/www/`, not a local directory. File reads and edits through the mount are correct (Write/Edit for source files, configs, `package.json`, etc., no SSH needed). **Executable commands — anything in the app's toolchain OR anything that traverses the tree (`git add -A`, `find`, build steps) — MUST run via SSH on the target container**, not on zcp against the mount.

The principle is WHICH CONTAINER'S WORLD the tool belongs to:

- **SSH (target-side)** — compilers (`tsc`, `nest build`, `go build`), type-checkers (`svelte-check`, `tsc --noEmit`), test runners (`jest`, `vitest`, `pytest`, `phpunit`), linters (`eslint`, `prettier`), package managers (`npm install`, `composer install`, `pnpm install`, `yarn`, `bundle install`, `pip install`), framework CLIs (`artisan`, `nest`, `rails`, `rake`), app-level `curl`/`node`/`python -c` hits against the running app or managed services, AND every `git init`/`git add -A`/`git commit` that traverses the codebase tree (large trees over SSHFS hit the 120 s bash timeout — v21 lost 360 CPU-seconds to this exact pattern).
- **Direct (zcp-side)** — `zerops_*` MCP tools, `zerops_browser`, Read/Edit/Write against the mount, `ls`/`cat`/`grep` against the mount for small reads. Whole-tree `git status` is fine when `node_modules/` is properly gitignored AND the `safe.directory` config from provision is in place; `git add -A` is not (traversal cost over SSHFS).

Correct shape:
```
ssh {hostname} "cd /var/www && {command}"   # correct — app's world
cd /var/www/{hostname} && {command}          # WRONG — zcp against the mount
```

Running app-level or tree-traversing commands zcp-side uses the wrong runtime, the wrong dependencies, the wrong env vars, has no managed-service reachability, AND exhausts zcp's fork budget. Symptoms you'll hit on the wrong side of the boundary:

1. **EACCES / root-owned `.git/` or `node_modules/`.** zcp runs as root; the container runs as `zerops` (uid 2023). Files created zcp-side are root-owned; subsequent container operations fail and need `sudo chown -R`.
2. **Broken `.bin/` symlinks.** zcp-side `npm install` writes absolute-path symlinks in `node_modules/.bin/` that don't resolve inside the container — `sh: vite: not found`, `sh: svelte-check: not found`.
3. **ABI mismatch.** Native modules compiled against zcp's node binary don't load on the container runtime.
4. **120 s bash timeouts on `git add -A`.** SSHFS is network-bound; traversing a tree with `node_modules/` (tens of thousands of files, hundreds of MB) takes 2+ minutes over SSHFS while running in seconds natively inside the container.
5. **Fork budget exhaustion.** `fork failed: resource temporarily unavailable` cascades when zcp hosts processes that should be on the container. Recovery: `pkill -9 -f "agent-browser-"` on zcp; the real fix is to stop running target-side commands zcp-side.

If you see any of these symptoms, you are on the wrong side of the boundary. Stop, re-do the failing step via `ssh {hostname} "cd /var/www && ..."`.

**Dev-server lifecycle is special — use `zerops_dev_server`, NOT raw SSH + `&`.**

Starting a long-running dev server on a target container via `ssh host "cmd > log 2>&1 &"` holds the SSH channel open until Bash's 120s timeout fires, because the backgrounded child still owns the channel's stdout/stderr/stdin. Every prior recipe run hit this — v11 lost 358s on a single worker start, v15 lost 9 minutes across five calls, v16's feature sub-agent still burned 6 minutes on two starts that hit the 120s wall. v17 tried a `nohup ... & disown` variant and hung for the full 300s ssh deadline before the tool timed out.

The Tier 1 fix is a dedicated MCP tool: `zerops_dev_server`. Every dev-server start, stop, status probe, log tail, or restart goes through it. It launches the process via `ssh -T -n` + `setsid` with redirected stdio (all three are load-bearing: `-T` disables pty, `-n` closes client-side stdin, `setsid` moves the child into its own session/pgroup so sshd can close the channel the instant the outer shell exits). Every phase is bounded by a tight per-step budget — spawn 8s, probe waitSeconds+5s, tail 5s — so any future regression costs seconds not minutes. It polls the health endpoint server-side in a single SSH round-trip and returns structured `{running, startMillis, healthStatus, logTail, reason}` with a specific reason code on failure (`spawn_timeout`, `spawn_error`, `health_probe_timeout`, `health_probe_connection_refused`, `health_probe_http_<code>`) so failures are diagnosable without a second call.

Correct shape:
```
zerops_dev_server action=start hostname=apidev command="npm run start:dev" port=3000 healthPath="/api/health"
zerops_dev_server action=status hostname=apidev port=3000 healthPath="/api/health"
zerops_dev_server action=logs   hostname=apidev lines=40
zerops_dev_server action=stop   hostname=apidev port=3000
zerops_dev_server action=restart hostname=apidev command="npm run start:dev" port=3000 healthPath="/api/health"
```

Anti-pattern to avoid (hits 120s timeout):
```
ssh apidev "cd /var/www && npm run start:dev > /tmp/nest.log 2>&1 &"    # WRONG — channel stays open
ssh apidev "cd /var/www && npm run start:dev > /tmp/nest.log 2>&1 &" && sleep 8 && ssh apidev "curl -s http://localhost:3000/api/health"  # partial workaround; still leaves orphan on failure
```

The tool also eliminates the port-stuck / process-stuck recovery spirals that cost 5–10 SSH calls per run in v11/v13/v15 (pkill + fuser + retry dance). `action=stop` takes the same `port` + `command`/`processMatch` and tolerates "nothing to kill" as success.

</block>

<block name="writer-subagent-brief">

### Writer sub-agent brief — README + CLAUDE.md composition (deploy step, `readmes` sub-step)

When a recipe has ≥2 codebases each needing README.md + CLAUDE.md (showcase Type 4 and every dual-runtime/separate-worker plan), dispatch a dedicated writer sub-agent rather than composing inline. Rationale: by the `readmes` sub-step the main agent's context is already loaded with deploy debug history; packing 4–6 × (README + CLAUDE.md) writes plus 3–4 iteration rounds into main context burns the budget that matters for close-step critical-fix dispatch. v21's main agent absorbed this work inline and grew its `tool_use` input bytes by 190 KB over v20 — the writer-subagent pattern restores v18–v20's delegation shape.

**Dispatch criterion**: multi-codebase recipe (showcase OR any recipe with ≥2 codebases).

**Brief template** — include verbatim, substituting `{recipe_name}`, `{plan}`, `{debug_narrative}`:

> You are the README + CLAUDE.md writer for the `{recipe_name}` recipe. Every codebase in `{plan.Codebases}` gets a README.md AND a CLAUDE.md, following the fragment/template rules in the deploy-step `readmes` brief. The main agent has deployed all services, run the browser walks, and survived the debug rounds — you have the debug narrative to draw on.
>
> **Input context**:
> - Debug narrative (what broke, how it was fixed): `{debug_narrative}`
> - Per-codebase gotcha pre-classification (so you don't re-dedup across READMEs): `{gotcha_classification}`
> - Each codebase's zerops.yaml (for the YAML block in each README integration-guide)
>
> **Output**: 2 × `{len(codebases)}` files written via the Write tool. No Bash — the main agent handles git ops after you return. Return a bulleted list of files you wrote.
>
> **Rules**: see the full checker list in the deploy-step `readmes` brief. The checkers run automatically after you return; the main agent dispatches you again with failure details if iteration is needed. Do NOT gate your own return on checker pass — report what you wrote and the main agent reconciles against the checker output.
>
> **Scope hygiene**: write README.md + CLAUDE.md files ONLY. Do not touch zerops.yaml, package.json, src/*, or any infrastructure file — those are owned by earlier sub-agents.

</block>

<block name="fix-subagent-brief">

### Fix sub-agent brief — scoped fix dispatch (any step with iterable checks)

When a check failure's `detail` field is ≥2 KB of prose OR a cluster of ≥3 checks on the same artifact all fail in one iteration round, dispatch a scoped fix sub-agent rather than absorbing the fix into main context. Rationale: multi-KB failure details carry verbose recovery instructions; absorbing them into main context inflates the surviving budget for every subsequent step. v20 dispatched `Fix README knowledge-base format` + `Fix gotcha restatements and specificity` as emergent fix sub-agents; v21's main agent inlined both, paying the context cost through close step.

**Dispatch criterion**: single check failure detail ≥2 KB, OR ≥3 checks on the same artifact in one iteration round.

**Brief template**:

> You are a fix sub-agent for the check failure(s) below. The recipe is partially complete; you are iterating on a specific artifact. Your scope:
>
> - Files you MAY edit: `{file_allowlist}`
> - Files you MUST NOT edit: everything else
> - Checks to pass: `{failed_check_names}`
> - Failure details (verbatim from the checker): `{failure_details}`
>
> Read the current state of each file in the allowlist. Make the edits needed to pass the listed checks. Return only: (a) list of files edited, (b) one-sentence summary per file.
>
> Do NOT run any bash/git/deploy commands — the main agent dispatches you and handles re-verification on return. If you think a fix requires commands, describe what you'd run; the main agent decides whether to execute.

</block>

<block name="content-fix-subagent-brief">

### Content-fix sub-agent brief — post-writer content-quality repair (v8.81)

When `complete step=deploy` fails with content-quality checks (any of `<host>_content_reality`, `<host>_gotcha_causal_anchor`, `<host>_gotcha_distinct_from_guide`, `<host>_claude_readme_consistency`, `<host>_scaffold_hygiene`, `<host>_service_coverage`, `<host>_ig_per_item_standalone`, `<host>_knowledge_base_authenticity`, `cross_readme_gotcha_uniqueness`, `recipe_architecture_narrative`), the retry's attestation must reference a content-fix sub-agent dispatch — OR include `inline-fix acknowledged` for a principled deviation.

**Why**: v22's main agent absorbed the content-check iteration cycle directly, producing 11 Edits on workerdev/README.md + 8 on apidev/README.md + 5 on workerdev/CLAUDE.md — ~15 min of wall-clock spent rewriting content inside main context. The v8.80 writer-subagent dispatch gate forces a writer at the `readmes` sub-step, but content checks fire LATER (at full `complete step=deploy`), so the rewrite work leaked past the gate. The v8.81 content-fix sub-agent absorbs that rewrite cycle.

**Dispatch trigger**: the content-fix-dispatch gate surfaces on the retry of `complete step=deploy` whenever a prior attempt emitted ≥1 content-check failure. You get a structured `checkResult` with `content_fix_dispatch_required` — the failing check list is already extracted for you.

**Brief template**:

> You are a content-fix sub-agent dispatched because `complete step=deploy` rejected the previous attempt on content-quality checks. The main agent is blocked on the retry gate until you return with the content repaired.
>
> **Files you MAY edit** (exhaustive — no other files):
> - `/var/www/{hostname}/README.md` for each hostname that failed
> - `/var/www/{hostname}/CLAUDE.md` for each hostname that failed
> - Root-level `README.md` if the failing check is `recipe_architecture_narrative` or `cross_readme_gotcha_uniqueness`
>
> **Files you MUST NOT touch**: source code, zerops.yaml, env configs, anything outside the README / CLAUDE.md surface.
>
> **Checks to pass** (verbatim from the gate's `priorFails` list): `{failing_check_names}`
>
> **Failure details** (the checker's full prose, verbatim — it names the offending bullets and explains WHAT would satisfy each check): `{failure_details_verbatim}`
>
> **What to do**:
>
> 1. **Re-read the affected README.md + CLAUDE.md files as they stand now.** The previous `complete` attempt may have partially fixed some issues; only the still-failing checks land in your brief.
> 2. **Treat each failing check as a rubric.** The check detail names the rule (e.g., "gotcha_causal_anchor: every gotcha must name a specific Zerops mechanism AND a concrete failure mode"). Apply the rule to every offending bullet; do not half-fix.
> 3. **For `gotcha_causal_anchor` / `gotcha_distinct_from_guide` fails**: rewrite the flagged gotcha to lead with the SYMPTOM (exact error message, HTTP status, observable misbehavior) and name a specific Zerops mechanism. If the gotcha restates an adjacent IG item, replace it with a different class of trap — consult the codebase source for an authentic framework × platform intersection.
> 4. **For `content_reality` fails**: either (a) make the claim true by adding the referenced symbol/file to the codebase (rare), or (b) reframe the bullet as advisory ("Pattern to add if…", "Consider adding…") so the reader knows it's a proposal, not a shipped fact.
> 5. **For `claude_readme_consistency` fails**: every production-hazardous pattern in CLAUDE.md (TypeORM synchronize, sync migrations in dev loop, etc.) must carry an explicit `dev only — see README gotcha against X in production` marker, OR be replaced with the production-equivalent procedure.
> 6. **For `recipe_architecture_narrative` fails**: add an `## Architecture` section to the root README naming every runtime codebase by hostname + role + at least one inter-codebase contract verb (publish/consume/subscribe/proxy/call/route-to).
>
> **What NOT to do**:
>
> - Do NOT run bash / git / deploy / MCP tool commands. The main agent re-runs `complete step=deploy` after you return; it owns verification.
> - Do NOT edit source code to make content claims true. The content must match the codebase, not vice versa.
> - Do NOT delete a gotcha to silence a check. The `gotcha_depth_floor` check will catch the deletion; you'll have traded one fail for another. Rewrite, don't remove.
>
> **Return shape**: a bulleted list of files edited (one bullet per file), with a one-sentence summary of what changed per file. No commentary on WHY the original content failed the check — the detail already explained that.

**Main-agent retry pattern** (do not skip the attestation step):

1. Fetch this brief: `zerops_guidance topic="content-fix-subagent-brief"`.
2. Dispatch the sub-agent with the Agent tool, including the brief, the file allowlist, the failing check names, and the verbatim failure details from the prior `complete step=deploy` rejection.
3. After the sub-agent returns, retry `complete step=deploy` with an attestation that names the dispatch — e.g., `"Dispatched content-fix sub-agent for workerdev_gotcha_causal_anchor + workerdev_content_reality; sub-agent rewrote 3 gotchas to load-bearing shape. Re-running deploy checks."`

If the gate still rejects on the retry, the attestation didn't match — include one of the accepted phrasings: `content-fix sub-agent`, `content-fix subagent`, `dispatched a fix sub-agent to fix the <readme/content/gotcha> ...`, or `inline-fix acknowledged` for a principled single-line deviation.

</block>

<block name="feature-subagent-mcp-schemas">

### MCP tool schemas — inline for the feature sub-agent

The feature sub-agent is dispatched with memory-frozen knowledge of MCP tool shapes that often lag the current schema. v21's feature sub-agent hit 6 `-32602 invalid params` errors across one 22-minute run because its brief didn't carry the exact parameter names + types. Include this block verbatim in every feature-subagent dispatch prompt:

- `zerops_dev_server action=start|stop|restart|status|logs hostname={host} command="..." port={int} healthPath="/..." waitSeconds={int} noHttpProbe={bool} processMatch="..."`
- `zerops_logs serviceHostname={host} lines={int}` — NOT `hostname` / `logLines`
- `zerops_scale serviceHostname={host} minRam={float-GB} maxRam={float-GB} minFreeRamGB={float}` — NOT `ramGB`
- `zerops_discover serviceHostname={host}` — returns the full env map
- `zerops_subdomain action=enable|status|verify serviceHostname={host}`
- `zerops_verify serviceHostname={host} port={int} path="/..."`
- `zerops_browser action=snapshot|text|click|fill url="..." selector="..."`

Parameter types are strict: `port` must be an integer (not a string), `noHttpProbe` a boolean, `waitSeconds`/`minRam` numeric. The MCP validator rejects type mismatches with `-32602 invalid params`. When a call is rejected, the expected shape is in the error message — apply the rename (`hostname` → `serviceHostname`, `logLines` → `lines`, `ramGB` → `minRam`) on the first retry; do NOT guess further.

</block>

<block name="feature-sweep-dev">

**Step 4c-pre: Feature sweep (dev) — MANDATORY gate, iterate `plan.Features`**

Before running the browser walk, the deploy sub-step `feature-sweep-dev` enforces a curl-level contract over every feature the plan declared at research time. This is the single gate that catches the v18 nginx-SPA-fallback class of bug (`/api/*` returns 200 + `text/html` because the frontend hardcoded `fetch('/api/items')` and the `VITE_API_URL` baking was dead): the sweep runs `curl -w '%{http_code} %{content_type}'` against every api-surface feature's `HealthCheck` path and rejects any response whose content-type is not `application/json`.

**How to run the sweep (iterate plan.Features):**

```
# For every feature F in plan.Features where F.surface contains "api":
ssh {F.host}dev "curl -sS -o /dev/null -w '%{http_code} %{content_type}\n' http://localhost:{F.port}{F.healthCheck}"
```

- `{F.host}` is the apidev hostname for api-role features; for single-runtime recipes it's `appdev`.
- `{F.port}` is `plan.Research.HTTPPort` (the API's HTTP port).
- `{F.healthCheck}` is the path string as declared — e.g. `/api/items`, `/api/search`.

Capture the status and content-type per feature and **submit the attestation as one line per feature using the format `<featureId>: <status> <content-type>`**:

```
items-crud: 200 application/json
cache-demo: 200 application/json
storage-upload: 200 application/json
search-items: 200 application/json
jobs-dispatch: 200 application/json
```

**The validator enforces:** every api-surface feature ID from `plan.Features` appears on its own line; every matching line contains a 2xx status token; every matching line contains `application/json` (case-insensitive); and NO line contains `text/html` or any 4xx/5xx status. Any violation fails the sub-step — the agent must fix the failing feature and re-run the sweep before completing.

**If a feature returns `text/html` under 200**: the frontend is hitting the SPA fallback. Check the source-code half of the dual-runtime URL pattern (`dual-runtime-consumption`): every `fetch()` must go through an `api()` helper that reads `import.meta.env.VITE_API_URL` (or framework equivalent). Do NOT attest success on a HTML response — the validator will reject it, and even if it didn't, the browser walk would render an empty dashboard and the recipe would ship broken.

**If a feature returns 4xx/5xx**: the backend is broken. Check runtime logs (`zerops_logs serviceHostname={host}dev severity=ERROR since=5m`), fix the source, redeploy if needed, re-run the sweep. The sub-step gate is firm — "4 of 5 features pass" is not an acceptable attestation.

**UI-only features** (surface contains `ui` but not `api`) are NOT part of the sweep — they are exercised in the browser walk below. Worker-only features (`worker` surface without `api`) are observed via the browser walk's result check or `zerops_logs` on the worker container.

**Minimal recipes run this sub-step too.** The rule is tier-independent — every declared api-surface feature must sweep-green before cross-deploy. Minimal recipes usually have 1–2 features which makes the sweep trivially short.

Submit:
```
zerops_workflow action="complete" step="deploy" substep="feature-sweep-dev" attestation="<one line per api-surface feature, as shown above>"
```

</block>

<block name="dev-deploy-browser-walk">

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

#### Canonical verification flow — iterate `plan.Features`

Three phases in strict order. **Do not reorder.** Within each walk, the commands array is **built from the plan's feature list**, not from a hardcoded template. Every feature in `plan.Features` where `surface` contains `"ui"` must be exercised — no feature is optional, no walk is "generic."

**Phase 1 — Dev walk (dev processes running, NO kill).** The dev subdomain serves whatever the dev processes started in Step 2 serve. Walk it while they're still up. This is the only phase where the dev container renders your dashboard in a browser.

**Build the commands array by iterating `plan.Features`.** For each UI-surface feature, translate its `Interaction` into one or more `zerops_browser` commands and assert against `MustObserve`. A minimal per-feature sequence is:

1. Locate the feature's section via its `UITestID`: `["get", "count", "[data-feature=\"{F.uiTestId}\"]"]` — must equal 1.
2. Observe the initial state.
3. Perform the `Interaction` — `fill`, `click`, `find`+`click` with `role` / `text`, `type` — whatever the interaction string prescribes.
4. Assert `MustObserve` — use `get text`, `get count`, or `is visible` against the selector the feature declares.
5. Capture any error banner: `["get", "text", "[data-feature=\"{F.uiTestId}\"] [data-error]"]` — must be empty string.

Example for a feature `{id: "items-crud", uiTestId: "items-crud", interaction: "Fill title, click Submit, row count +1", mustObserve: "[data-feature=\"items-crud\"] [data-row] count increases by 1"}`:

```
zerops_browser(
  url: "https://{appdev-subdomain}.prg1.zerops.app",
  commands: [
    ["snapshot", "-i", "-c"],
    # Locate the feature section
    ["get", "count", "[data-feature=\"items-crud\"]"],
    # Before state — row count
    ["get", "count", "[data-feature=\"items-crud\"] [data-row]"],
    # Interaction — fill title, click Submit
    ["fill", "[data-feature=\"items-crud\"] input[name=\"title\"]", "browser walk test row"],
    ["find", "role", "button", "Submit", "click"],
    ["wait", "500"],
    # After state — row count (MustObserve: increased by 1)
    ["get", "count", "[data-feature=\"items-crud\"] [data-row]"],
    # Error state — must be empty
    ["get", "text", "[data-feature=\"items-crud\"] [data-error]"]
  ]
)
```

Repeat one `zerops_browser` call per URL (dashboard subdomain). If the walk needs to span multiple URLs (rare — dual-runtime with separate frontend SPA routes) the rule is **one zerops_browser call per URL**; serialize if needed, do not batch multiple URLs in one call.

If dev walk returns a 502 or connection failure, your dev processes aren't running (or they died). Diagnose via `ssh {devHostname} "ps -ef | grep -E 'nest|vite|node|ts-node'"` and restart per Step 2 before continuing.

**Phase 2 — Kill dev processes (Bash).** Only now, after the dev walk has passed, free the fork budget. API-first recipes: both apidev AND appdev. Single-runtime: just appdev.

```
ssh apidev "pkill -f 'nest start' || true; pkill -f 'ts-node' || true; pkill -f 'node dist/worker' || true"
ssh appdev "pkill -f 'vite' || true; pkill -f 'npm run dev' || true"
```

**Phase 3 — Stage walk (dev processes dead).** Walk the stage subdomain with the **same feature iteration** as Phase 1. Stage containers run their own processes and are completely unaffected by the dev kill. The commands array is re-generated from the same `plan.Features` — identical feature coverage, different URL.

The tool executes `[open url] + your commands + [errors] + [console] + [close]` as one batch and returns structured JSON: `steps[]`, `errorsOutput`, `consoleOutput`, `durationMs`, `forkRecoveryAttempted`, `message`.

#### Per-feature pass criteria

For each feature the walk iterated, **every** criterion below must hold. A walk only passes when all features pass:

1. **Section located** — `[data-feature="{uiTestId}"]` count equals 1. Zero = scaffold didn't emit the test hook; multiple = ambiguous selector.
2. **MustObserve satisfied** — the state change the feature declared is visible. If `MustObserve` names a count increase, the after-count must be strictly greater than the before-count. If it names a text pattern, the element's text must match. "Zero hits" / "empty state" is a **failure** unless the feature's `MustObserve` string explicitly permits it.
3. **No `[data-error]` text** — the error banner (mandatory output of the `client-code-observable-failure` rule) must be empty after the interaction. A non-empty banner means the feature's fetch or logic raised an error the walk must surface.
4. **No JS runtime error in `consoleOutput`** — the auto-appended `["console"]` output must contain no `Uncaught`, `TypeError`, `SyntaxError`, or `Unexpected token '<'`. The last one is the specific signal that a `res.json()` parsed HTML — same family as the v18 bug.
5. **No network failure in `errorsOutput`** — no `net::ERR_*`, no failed-request lines targeting the feature's API path.
6. **`forkRecoveryAttempted` is false** — any recovery firing means orphaned processes are leaking. See rule 5 in the non-negotiable list above.

If ANY criterion fails for ANY feature, the walk fails. Fix the source on the mount, redeploy (which needs dev processes restarted via SSH since the kill in Phase 2 took them down), re-verify dev with the curl flow in deploy Step 3, re-run the feature-sweep-dev sub-step, then cross-deploy and repeat Phase 2 + Phase 3. This counts toward the 3-iteration limit.

**Report shape for a verification pass** (per subdomain walked):
- **Per feature**: ID, before-state, interaction performed, after-state, MustObserve result (PASS/FAIL), error banner text (expected empty)
- `errorsOutput` from the result (expected: empty)
- `consoleOutput` from the result (expected: empty or benign info only)
- `forkRecoveryAttempted` from the result (expected: false)

Do NOT advance to publish until BOTH appdev AND appstage walks show every feature PASS, empty errors, and no console noise.

**Features with `surface` but no `ui` are NOT part of this walk.** Worker-only features are observed via a post-interaction check on their MustObserve selector (usually a result element populated by a polling frontend consumer). API-only features were swept at `feature-sweep-dev` / `feature-sweep-stage`. Every feature gets verified exactly once at the layer appropriate to its surface.

</block>

<block name="browser-command-reference">

#### Browser command vocabulary (use these INSIDE `commands` — NOT `eval`)

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

#### What to avoid (all were seen in v4, v5, or v6)

- Raw `agent-browser` / `echo ... | agent-browser batch` Bash calls — always use `zerops_browser` MCP tool
- **Killing dev processes BEFORE the dev walk** — the dev subdomain then returns 502 because the dev processes ARE the dev server. This is the v6 regression. Phase 1 before Phase 2, always.
- `["eval", "window.onerror = …"]` inside commands — use the auto-appended `["errors"]` / `["console"]` output instead
- Running the STAGE walk while dev processes are still running on dev containers — guaranteed `forkRecoveryAttempted: true`
- Passing `["open", ...]` or `["close"]` inside `commands` — the tool strips them; if you thought you needed them, you didn't
- Dispatching a sub-agent that calls `zerops_browser` while the main agent also has a call in flight
- Re-running the tool against the same URL repeatedly "just to be sure" — one call per URL per iteration

</block>

<block name="stage-deployment-flow">

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

**`zerops_verify` is mandatory for every runtime target after every deploy — dev and stage.**
It runs a standardized check suite that catches readiness-probe misconfiguration, env-var
binding failures, and container state inconsistencies that `curl` alone misses. Call it
for every `{name}dev` after self-deploy, and for every `{name}stage` after cross-deploy.
Worker targets without HTTP: skip `zerops_verify` (it checks HTTP endpoints), verify via
`zerops_logs` instead.

**Stage verification completeness by plan shape** (every target below must be verified):

- **Single-runtime minimal**: `appstage` (verify + subdomain)
- **Single-runtime showcase**: `appstage` (verify + subdomain) + `workerstage` (logs)
- **Dual-runtime minimal**: `appstage` (verify + subdomain) + `apistage` (verify + subdomain)
- **Dual-runtime showcase**: `appstage` + `apistage` (both verify + subdomain) + `workerstage` (logs)

**Step 8: Present URLs**

</block>

<block name="reading-deploy-failures">

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

</block>

<block name="feature-sweep-stage">

**Step 7b: Feature sweep (stage) — MANDATORY gate after every cross-deploy**

After `verify-stage` passes and every stage service is healthy, re-run the feature sweep against the **stage** endpoints. This is the second and final content-type gate — the stage bundle is built from the dev source (via cross-deploy), and the v18 bug class manifests specifically at stage because the `build.envVariables: VITE_API_URL: ${STAGE_API_URL}` bake is STAGE-specific. A dev-green sweep with a broken source-code half will still flip to `text/html` on stage.

**How to run the stage sweep:**

```
# For every feature F in plan.Features where F.surface contains "api":
curl -sS -o /dev/null -w '%{http_code} %{content_type}\n' https://{F.host}stage-{subdomainHost}-{F.port}.prg1.zerops.app{F.healthCheck}
```

For static-base stage services (where the API is a DIFFERENT service), curl the API's subdomain — `apistage`, not `appstage`. The sweep targets the URL the frontend's bundle actually calls, which is whichever service's origin the baked `VITE_API_URL` (or equivalent) points at.

Static-base appstage services still get swept for their UI-surface features (e.g., the dashboard returns the SPA index) but the api-surface features always route to the API service's origin — that's the whole point of `VITE_API_URL`. The sweep's feature list is unchanged between dev and stage; only the host+port change.

**Submit the attestation in the same per-feature format as `feature-sweep-dev`**:

```
items-crud: 200 application/json
cache-demo: 200 application/json
storage-upload: 200 application/json
search-items: 200 application/json
jobs-dispatch: 200 application/json
```

Same validator, same contract — every declared api-surface feature ID must appear with a 2xx status and `application/json`. **Any `text/html` on a stage sweep is the v18 bug** — the frontend bundle is hitting the local SPA fallback instead of the API. Fix the source code's fetch helper (`dual-runtime-consumption`), redeploy the frontend, re-run the sweep.

Submit:
```
zerops_workflow action="complete" step="deploy" substep="feature-sweep-stage" attestation="<one line per api-surface feature against stage URLs>"
```

Only after this sub-step passes do you proceed to the `readmes` sub-step. A stage sweep that still reports HTML is a **deploy-blocking** bug — the recipe cannot ship without the source-code half of the dual-runtime pattern working.

</block>

<block name="common-deployment-issues">

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
| Permission denied on `.git/config` | `.git/` created by root (SSHFS), deploy runs as `zerops` | `ssh {hostname} "sudo chown -R zerops:zerops /var/www/.git"` on each dev container before first deploy |
| Env var not updating after zerops.yaml fix | Service-level env var (set via `zerops_env`) shadows zerops.yaml `run.envVariables` | Delete the service-level var (`zerops_env action="delete"`) before redeploying. Never use `zerops_env set serviceHostname=...` as a debugging shortcut for vars that belong in zerops.yaml — the service-level var takes precedence on every subsequent deploy, silently ignoring your zerops.yaml fix. Fix the zerops.yaml and redeploy; if you need to verify a value quickly, read it from logs after deploy, don't inject it as a service-level var. |

</block>

<block name="content-quality-overview">

### The six-surface teaching system — map before you write

Before the `readmes` sub-step, read this once. Every recipe publishes six distinct content surfaces. Each has its own audience, its own rubric, its own check. The agent that authors them without a mental map context-switches six times per run and drifts across boundaries (CLAUDE.md using a pattern README forbids, gotchas restating IG headings, env comments copy-pasted with hostname swapped). This overview is the map.

**Surface → author, step, substep, nature:**

| # | Surface | Step | Substep | Written from |
|---|---|---|---|---|
| 1 | `zerops.yaml` + inline comments | generate | `zerops-yaml` | Smoke-test context — platform × framework reasoning inline with each field |
| 2 | Integration Guide (README fragment) | deploy | `readmes` | Debug rounds you just lived through; IG #1 copies zerops.yaml verbatim |
| 3 | Gotchas (README fragment) | deploy | `readmes` | Debug rounds + platform invariant candidates; distinct from IG |
| 4 | `import.yaml` env comments (×6 envs) | finalize | — | Structured JSON input → auto-rendered YAML |
| 5 | Root `README.md` narrative | close | — | Authored prose + finalize template |
| 6 | `CLAUDE.md` per codebase | deploy | `readmes` | Repo-local ops; must NOT contradict #3 |

**Content flow — what must be true across surfaces:**

```
[1] zerops.yaml          ─ comments explain WHY each value (v8.82 §4.2: 35% reasoning-marker floor)
      │ verbatim copy
      ▼
[2] Integration Guide    ─ IG #1 = zerops.yaml copy; IG #2+ narrate debug-round diffs
                           (per-item: mechanism + symptom + code block — v8.82 §4.5)
      │
      ▼
[3] Gotchas              ─ "what surprises you if you DON'T change something"
                           (causal-anchor: specific Zerops mechanism + concrete failure mode)
                           (predecessor-floor: extend what the prior recipe taught, don't re-trim it)
      │
      ▼
[6] CLAUDE.md            ─ repo-local ops; no code-level mechanisms the README forbids
                           (claude_readme_consistency)
      │
      ▼
[4] import.yaml (×6)     ─ per-tier scaling + availability; 35% reasoning-marker floor
      │
      ▼
[5] Root README          ─ architecture overview, NOT a link aggregator
                           (recipe_architecture_narrative)
```

**Boundary rules — where each fact goes:**

| Boundary | Rule | Which check enforces |
|---|---|---|
| #1 vs #4 | `zerops.yaml` = FRAMEWORK × PLATFORM per-service. `import.yaml` = ENV × SCALING per-tier. | By construction (separate files) |
| #2 vs #3 | IG = "what I changed". Gotcha = "what surprises you if you DON'T". | `gotcha_distinct_from_guide` (token compare) |
| #3 vs #6 | Platform facts = README gotchas. Repo-local ops (SSHFS, fuser, ssh, chown) = CLAUDE.md. | `readme_container_ops_nudge` (v8.82 §4.4 — info-only) |
| #2 vs #4 | IG = integrator's view of `zerops.yaml`. Env comments = deployer's view of `import.yaml`. | Disjoint surfaces |
| #5 vs #2 | Root = architecture overview. Per-codebase README = integration guide + gotchas. | `recipe_architecture_narrative` |

**Rubric strength, ranked — what each surface must carry:**

1. **Gotchas (#3).** Load-bearing per-bullet. Must name a SPECIFIC Zerops mechanism (L7, execOnce, readinessCheck, `${X_hostname}`, httpSupport, minContainers — not generic "container" or "envVariables") AND a CONCRETE failure mode (HTTP status, quoted error in backticks, strong symptom verb: rejects/deadlocks/drops/crashes/times out/throws). Per-role count floor. Cross-codebase unique. Worker production-correctness mandatory. Predecessor floor: the prior recipe's gotchas are the bar — extend, don't re-trim.

2. **Env comments (#4).** 35% reasoning-marker floor. Per-service Jaccard distinctness. Taxonomy: because / otherwise / without / must / rather than / instead of / so that / prevents / at build time / at runtime / rolling / drain.

3. **CLAUDE.md (#6).** ≥ 1200 bytes substantive content + ≥ 2 custom sections beyond the template. Must not contradict README gotchas (`claude_readme_consistency`).

4. **Root README (#5).** Architecture overview — names every service, describes cross-codebase contracts, doesn't just link-aggregate.

5. **Integration Guide (#2).** Per-item floor: ≥ 1 fenced code block, platform-anchor in first paragraph, AND (v8.82 §4.5) a concrete failure-mode anchor in prose body for IG #2+. IG #1 (zerops.yaml copy) is grandfathered on the symptom rule.

6. **`zerops.yaml` comments (#1).** (v8.82 §4.2) 35% reasoning-marker floor at parity with env comments. IG #1 copies this verbatim — shallow comments here inherit directly into the published integration guide.

**Anti-patterns to avoid on authorship:**

- Don't write a gotcha that restates an IG heading — rewrite to focus on the observable symptom.
- Don't duplicate the same fact across two codebases' READMEs — pick one owner, cross-reference from the others.
- Don't put SSHFS / fuser / ssh-config / chown content in README gotchas — it belongs in CLAUDE.md.
- Don't narrate fields in comments ("install dependencies", "start the app") — explain WHY the value was chosen.
- Don't write gotchas only from what broke in this run — the debug experience is a biased sample. Walk the predecessor recipe's gotchas (injected via the chain), the "framework × platform gotcha candidates to consider" section below, and your stack's known platform traps (reconnect-forever for long-lived brokers, SDK ESM/CJS boundaries, bundler build-time vs runtime env, etc.) before attesting your set complete.

**Why this map is eager-injected:** the agent authoring six surfaces in one run without seeing them as a system is the structural reason for surface-crossing mistakes. v8.82 ships this as eager content so the mental model lands in context before authorship begins, not after a checker fails.

</block>

<block name="readme-with-fragments">

### Per-codebase README with extract fragments (post-deploy `readmes` sub-step)

**This is the `readmes` sub-step of deploy.** You land here after `verify-stage`, after every service is verified healthy on both dev and stage. READMEs are written now — not during generate — so the gotchas section narrates the debug rounds you just lived through. A speculative gotchas section written during generate is the root cause of the authenticity failures in v11/v12.

Write **two files per codebase mount**: `README.md` and `CLAUDE.md`. They have different audiences and neither substitutes for the other:

- `README.md` — **published recipe-page content**. Fragments are extracted to zerops.io/recipes at finalize time. Audience: integrators porting their own codebase. Content: platform-forced code changes + symptom-framed gotchas. Fragment format enforced byte-literally.
- `CLAUDE.md` — **repo-local dev-loop operations guide**. Not extracted, not published. Audience: anyone (human or Claude Code) who clones this codebase and needs to work in it. Content: SSH commands, dev server startup, migration/seed commands, container traps (SSHFS uid, npx tsc wrong-package, fuser -k for stuck ports), test commands. Plain markdown, no fragments, no rules other than "be useful."

For a dual-runtime showcase, that is 6 files: `/var/www/appdev/{README.md,CLAUDE.md}`, `/var/www/apidev/{README.md,CLAUDE.md}`, `/var/www/workerdev/{README.md,CLAUDE.md}`. Use `prettyName` from the workflow response for titles (e.g., "Minimal", "Hello World", "Showcase").

**Critical formatting for README.md** — match the structure below exactly. The literal `<!-- #ZEROPS_EXTRACT_START:name# -->` / `<!-- #ZEROPS_EXTRACT_END:name# -->` marker shape is enforced by the checker byte-for-byte. Invented variants like `<!-- FRAGMENT:intro:start -->` or `<!-- BEGIN:intro -->` are rejected.

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
  ... (paste full zerops.yaml with comments — read it back from disk, do not rewrite from memory)
\`\`\`

### 2. Step Title (for each code adjustment you actually made)
Describe the debug round that forced the change. Example: "Bind NestJS to 0.0.0.0" / "Add `allowedHosts: ['.zerops.app']` to vite.config.ts" / "Use `forcePathStyle: true` for MinIO S3 client". Each section is one real thing that broke and how you fixed it, with the code diff.

\`\`\`typescript
// the actual patch you applied
\`\`\`

<!-- #ZEROPS_EXTRACT_END:integration-guide# -->

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->

### Gotchas
- **Concrete symptom 1** — exact error message, HTTP status, or observable misbehavior (e.g. "`AUTHORIZATION_VIOLATION` on first subscribe", "HTTP 200 with plain-text 'Blocked request' body", "`MODULE_NOT_FOUND` for package that IS in node_modules"). Written from memory of the debug round. Clones of the predecessor's stems fail the `knowledge_base_exceeds_predecessor` check; restatements of integration-guide items in THIS README fail the `gotcha_distinct_from_guide` check; facts that also appear in a sibling codebase's README fail `cross_readme_gotcha_uniqueness`.
- **Concrete symptom 2** — same. Showcase tier needs at least 3 net-new gotchas beyond the predecessor AND 3 authentic (platform-anchored or failure-mode described), AND each stem must be cross-README unique.

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
```

**Then write `CLAUDE.md` next to it** — plain markdown, no fragments, no extraction rules. The template below is the MINIMUM. A v17-compliant CLAUDE.md clears **1200 bytes** of substantive content AND carries **≥ 2 custom sections beyond the template** (Resetting dev state / Log tailing / Adding a managed service / Driving a test request — whatever operational knowledge you actually accumulated for this codebase):

```markdown
# {Framework} {PrettyName} — Dev Operations

Repo-local operations guide for anyone (human or Claude Code) working in this codebase after cloning. For the published recipe content (integration guide + platform gotchas), see README.md.

## Dev Loop

SSH into the dev container: `zcli vpn up` then `ssh zerops@{hostname}dev`.
Start the dev server via `zerops_dev_server action=start hostname={hostname}dev command="<exact cmd>" port=<port> healthPath="<path>"` — do NOT hand-roll `ssh host "cmd &"` (hits the 120s SSH channel timeout).
Source lives at `/var/www/` on the container, SSHFS-mounted from zcp at `/var/www/{hostname}dev/`.

## Migrations & Seed

Run manually: `<exact command>` — e.g. `npx ts-node src/migrate.ts` then `npx ts-node src/seed.ts`.
On deploy, these run via `initCommands` wrapped with `zsc execOnce ${appVersionId}`. If the seeder crashed mid-insert and burned the key, touch any source file and redeploy to force a fresh `appVersionId`.

## Container Traps

- **SSHFS ownership** — files land owned by `root`, container runs as `zerops` (uid 2023). `npm install` fails with `EACCES`. Fix: `sudo chown -R zerops:zerops /var/www/`.
- **`npx tsc` resolves to deprecated tsc@2.0.4** — use `node_modules/.bin/tsc` instead.
- **Port 3000 stuck after background command** — `zerops_dev_server action=stop hostname={hostname}dev port=3000` (tolerates "nothing to kill").
- *(add any other container-ecosystem traps you hit during this build)*

## Testing

- Unit tests: `<command>`
- Smoke check: `zerops_dev_server action=status hostname={hostname}dev port=<port> healthPath="<path>"`
- To exercise the full feature path: `<concrete curl sequence the agent actually ran>`
```

**v8.78 cross-file consistency rule (`<codebase>_claude_readme_consistency`).** Procedures in CLAUDE.md must NOT use code-level mechanisms the README's Gotchas explicitly forbid for production. CLAUDE.md is the ambient context an agent reads when operating this codebase — if it teaches a pattern the README warns against, the agent will propagate that pattern into prod-affecting changes. The dev-loop is the prod-loop reduced to dev-scoped arguments — not a different path.

The check parses README gotchas for "do not use X in production" / "X must be off" / "never use X" patterns and greps CLAUDE.md for the identifier. Two ways to pass:

1. **Production-equivalent path (preferred).** If the README forbids `synchronize: true` in production, the CLAUDE.md reset-state procedure runs the real migrations down/up, not `ds.synchronize()`. Same code, dev-scoped arguments. Teaches the recovery path that translates to a production incident.

2. **Explicit cross-reference.** If the dev shortcut is meaningfully faster than the prod-equivalent and you choose to ship it, add an explicit acknowledgment somewhere in CLAUDE.md: `(dev only — see README gotcha against synchronize in production)`. Whole-document marker; the check accepts a single cross-reference covering all uses.

v20 apidev failed this rule: README gotcha #7 said `synchronize: true` must be off in production, while CLAUDE.md "Resetting Dev State" called `ds.synchronize()`. An agent reading both files in one pass got a micro-contradiction — and an agent that reached for CLAUDE.md without re-reading the README would propagate `synchronize` into a feature change.

**Now add at least 2 of these custom sections** (pick the ones that apply to this codebase):

- **Resetting dev state** — how to drop/re-seed the database without a full redeploy (avoids the `appVersionId` rotation dance). Use the same mechanism the README endorses for production (real migrations down/up), not a shortcut that bypasses it.
- **Log tailing** — the exact log file path + `tail -f` command for each long-running process in this codebase, plus when to reach for `zerops_logs` instead.
- **Driving a test job / endpoint** — a real curl (or psql / redis-cli / nats-cli) command sequence that exercises the feature path end-to-end on the dev container. For a worker, the exact NATS message shape + how to dispatch it from the API.
- **Adding a new managed service** — the delta against this recipe's current zerops.yaml / import.yaml when the user wants to bolt on another dependency.
- **Recovering from a burned `zsc execOnce` key** — the exact `touch` or file-mtime trick for THIS codebase's source tree, step by step.

**Rules:**
- Section headings (`## Integration Guide`) go OUTSIDE markers in README.md — they're visible but not extracted
- Content INSIDE fragment markers uses **H3** (`###`), not H2
- **All fragments**: blank line required after the start marker (intro, integration-guide, knowledge-base)
- **Intro content**: plain text, no headings, 1-3 lines
- **Step 1** of integration-guide must be `### 1. Adding \`zerops.yaml\`` with a description paragraph before the code block
- **Worker codebase README** does not need the integration-guide code-block floor (workers rarely have user-facing code adjustments), but still needs all three fragments, the predecessor-floor gotchas, its own CLAUDE.md, AND the two production-correctness gotchas below.
- **Fragment format is byte-literal.** The checker searches for `#ZEROPS_EXTRACT_START:{name}#` exactly. Do not guess.
- **CLAUDE.md is required for every codebase, every tier.** Plain markdown, no fragments. **New v17 floors**: ≥ 1200 bytes of substantive content AND ≥ 2 custom sections beyond the template boilerplate (Dev Loop / Migrations / Container Traps / Testing). A 40-line file that only fills in the template fails the depth check.
- **No fact appears in two README.md files.** If the fact applies to multiple codebases (NATS credentials, shared DB migration ownership), put it in exactly one README — by convention, the service most responsible for owning it (api for server-side wiring, app for frontend config) — and have the others cross-reference: `See apidev/README.md §Gotchas for NATS credential format.`
- **No gotcha restates an integration-guide heading in the same README.** A gotcha must teach a symptom the guide did not cover. If your gotcha stem normalizes to the same tokens as an IG heading, rewrite it to focus on the observable symptom (error message, HTTP status, browser state) instead of the topic.
- **Container-ops content (SSHFS uid fix, npx tsc trap, dev-server restart)** goes in CLAUDE.md, NOT in README.md gotchas. README.md is for platform facts an integrator porting their own code cares about.

**Worker production-correctness gotchas (MANDATORY for every `isWorker: true` target with `sharesCodebaseWith` empty).** A separate-codebase worker README MUST carry gotchas covering BOTH of these production-correctness concerns — they are enforced at deploy-step completion by `{hostname}_worker_queue_group_gotcha` and `{hostname}_worker_shutdown_gotcha`:

1. **Queue-group semantics under `minContainers > 1`.** Whenever a runtime service runs more than one container — whether the replicas exist for throughput scaling OR for HA/rolling-deploy availability — a broker consumer without a queue group (NATS `queue: 'workers'`, Kafka consumer group, etc.) processes every message ONCE PER REPLICA, so a 2-container worker runs every job twice. A reader scaling out a fresh deployment will fill the database with duplicates and never know why. The gotcha stem must name the broker + "queue group" or "consumer group" + "minContainers" / "double-process" / "exactly once" / "per replica", and the body must show the exact client-library option that sets the group.

2. **Graceful shutdown on SIGTERM.** Zerops sends SIGTERM to running containers during rolling deploys. A consumer that exits on SIGTERM without draining in-flight messages acks the batch, crashes, and loses the work. The gotcha stem must name SIGTERM or "graceful shutdown" or "in-flight" or "drain", and the body must show the concrete call sequence (catch SIGTERM → `nc.drain()` or equivalent → await → `process.exit(0)`).

Both of these interact with Zerops-specific mechanisms (`minContainers > 1` replica count — whether the replicas exist for throughput scaling or for HA / rolling-deploy availability, SIGTERM timing during rolling deploys) and belong in the PUBLISHED README, not CLAUDE.md — a porting user needs to know them before their first scaled deploy.

**Per-item IG code-block floor (enforced by `{hostname}_integration_guide_per_item_code`).** Every H3 heading inside the `integration-guide` fragment must carry at least one fenced code block in its section — any language (typescript, javascript, python, go, bash, yaml for a non-zerops.yaml snippet). The v18 appdev regression shipped IG step 3 ("Place `VITE_API_URL` in `build.envVariables` for prod, `run.envVariables` for dev") as prose only, with no code. A reader can't lift prose — they can lift a diff. If a step is prose-only, fold its content into a neighbouring step that carries a code block, or delete it.

**Worker drain code-block floor (enforced by `{hostname}_worker_drain_code_block`).** A separate-codebase worker README must contain at least one fenced code block showing the actual SIGTERM → drain → exit call sequence somewhere in either the integration-guide OR the knowledge-base fragment. The `worker_shutdown_gotcha` check verifies the topic is *mentioned*; this check verifies there's a copy-pasteable *implementation*. v7 shipped this as IG step 3 with full typescript; v18 lost it to prose inside a gotcha. Write the drain sequence as an IG item with the concrete code: `process.on('SIGTERM', ...)` → `await nc.drain()` → `await dataSource.destroy()` → `process.exit(0)`.

**Framework × platform gotcha candidates to consider.** The predecessor floor and authenticity classifier accept any platform-anchored or framework-intersection gotcha. The v7–v14 gold-standard runs included framework-integration insights that v15–v18 systematically filtered out because they didn't hit during the current debug round. When you reach the `readmes` sub-step, actively consider whether any of the following classes applied to *this* recipe — if yes and you have room under the per-codebase limits, write them up:

- **SDK module-system boundary (ESM-only vs CommonJS).** Managed-service client libraries (Meilisearch, Stripe, Prisma edge, some AWS v3 sub-packages) that ship ESM-only bindings fight with CommonJS-output frameworks (NestJS v10, Express, older Next.js). The symptom is `ERR_REQUIRE_ESM` or `Cannot use import statement outside a module` at import time, not at runtime. If your framework is CommonJS-based and you talked to an ESM-only SDK, add the gotcha with the workaround you used (fetch() over HTTP, dynamic import, tsconfig module shift).
- **Bundler major-version shift.** Vite 8 → Rolldown, Webpack 4 → 5, Turbopack → lightningcss — major-version bundler shifts silently change plugin compatibility, CSS handling, or output shape. If the recipe uses a bleeding-edge version that differs from the predecessor's, note what changed and whether ecosystem plugins for the previous bundler still work.
- **Dev-server `preview` mode separate host-check.** Vite-family dev servers have BOTH `server.allowedHosts` and `preview.allowedHosts` — configuring only one breaks the mode you didn't configure. If you set allowedHosts for dev, set the preview variant too, or note explicitly that preview mode isn't used.
- **Reconnect-forever pattern for long-running broker clients.** NATS, RabbitMQ, Kafka clients on Zerops need `reconnect: true` with `maxReconnectAttempts: -1` (or the client-library equivalent) so a brief broker restart doesn't take the worker down. The v7 worker README had this as IG #2; v15+ lost it.
- **Search-index re-push on redeploy seed.** When the seeder guard skips insert because rows already exist, ORM save-hooks never fire and the search index stays empty. The recipe must re-push the current entity set to the search engine regardless of whether the seed insert ran. Applies to every ORM + search combination — TypeORM/Meilisearch, Eloquent/Scout, Django/Whoosh.
- **Auto-indexing skips on idempotent re-seed.** Same root cause as above but the symptom is "search returns empty right after a redeploy". If you encountered it, write it. If you didn't (because your seeder does a raw re-push), still consider writing the gotcha as a "this is what WOULD break if you removed the re-push" warning.
- **Static-mount SPA tilde suffix (`./dist/~`).** The tilde strips the dist directory wrapper so files land at `/var/www/index.html` not `/var/www/dist/index.html`. Without it, Nginx serves a 404 on root. This is a Zerops-specific syntax — users from Vercel/Netlify will miss it.

These are *candidates*, not requirements. Don't pad the README with gotchas that don't apply. But do consciously walk the list instead of only writing gotchas from the specific failures that happened to surface during this particular run's debug rounds — the debug experience is a biased sample.

**Completion:**
```
zerops_workflow action="complete" step="deploy" substep="readmes" attestation="Wrote README.md + CLAUDE.md for appdev/apidev/workerdev. README gotchas narrate: NATS credential split (apidev only, worker cross-refs), Vite allowedHosts symptom (appdev — Blocked request HTTP 200), MinIO forcePathStyle (apidev only). Net-new >= 3, cross-README unique, no restatements. CLAUDE.md covers SSH, dev server startup, migration commands, and the SSHFS/tsc/fuser traps hit during this build."
```

After the sub-step completes, call the full deploy-step completion. The deploy-step checker runs every README content check (fragments, integration-guide code block floor, **integration-guide per-item code block** (v18), comment specificity, predecessor floor, knowledge-base authenticity, cross-README dedup, gotcha-distinct-from-guide, worker queue-group gotcha, worker shutdown gotcha, **worker drain code-block** (v18)) AND the per-codebase CLAUDE.md existence check — iterate on the content until they all pass, then the deploy step closes.

</block>

<block name="deploy-completion">

### Completion
```
zerops_workflow action="complete" step="deploy" attestation="Dev deployed at {dev_url}, stage deployed at {stage_url}. Both healthy. READMEs narrate debug rounds."
```

</block>
</section>

<section name="finalize">
## Finalize — Recipe Repository Files

Recipe files were **auto-generated** in the output directory when deploy completed. The output directory (`outputDir` in the response) contains:
- 6 environment folders with import.yaml (correct structure, scaling, buildFromGit) and README.md
- 1 root README with deploy button, cover image, environment links
- 1 app README scaffold at `appdev/README.md` with correct markers and deploy button — compare with your app README at `/var/www/appdev/` to ensure yours has the same structural elements (deploy button, cover, markers)

### Do NOT edit import.yaml files by hand

The template emits YAML structure + scaling values only — all prose commentary comes from your `envComments` input. Editing files by hand means agents rewrite them from scratch and drop the auto-generated `zeropsSetup` + `buildFromGit` fields. **Pass structured per-env comments instead.** One call bakes all 6 files.

<block name="env-comment-rules">

**Preprocessor directive** (applies to every env import.yaml that uses `<@...>`): when the finalize template emits `<@generateRandomString(<32>)>` or any other `<@` function, the file's FIRST non-empty line MUST be `#zeropsPreprocessor=on`. Without the directive, the Zerops import API imports the literal angle-bracket string as the env var value instead of running the preprocessor. The `{env}_preprocessor` check at finalize-complete enforces this whenever `<@` appears, regardless of whether the plan's `needsAppSecret` flag is set — v16 shipped all 6 files missing the directive because the check was wrongly gated on the flag.

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

Every service that appears in a given env's import.yaml MUST have a comment explaining its role in THAT env. Fetch [topic: env-comments-example] for a complete per-env template.

**What each env's commentary should cover:**
- **Role in the dev lifecycle** (AI agent workspace / remote dev / local validator / staging / small prod / HA prod) — what this env exists for.
- **What `zeropsSetup: dev` / `zeropsSetup: prod` does for THIS framework** (dev dependency install / production build + cache warming / etc.) — where it's relevant.
- **Replica-count & scaling rationale** for fields only present in this env. `minContainers` is a **runtime-service field only** (never on managed services — they use `mode: HA` / `NON_HA`), and `minContainers ≥ 2` only appears on envs 4-5. On envs 0-3 runtime services stay at `minContainers: 1` — rolling-deploy blips are fine in non-prod, dev tiers can require a single container for SSHFS, and a second replica wastes the non-prod budget. On envs 4-5, `minContainers: 2` on a runtime service serves **two independent axes**: **(a) throughput** — one container can't serve the load — and **(b) HA / rolling-deploy availability** — a single-container pool drops traffic on every rolling deploy or container crash. Name whichever axis applies for this specific service. For a service whose throughput fits in one container (static SPA, light-traffic admin panel), (b) is the sole justification — but a comment that only says "no horizontal scaling needed" and stops there misleads the reader into thinking a single replica is safe. Other env-4/5-only fields: `cpuMode: DEDICATED` (env 5), `mode: HA` (env 5 managed services), `corePackage: SERIOUS` (env 5).
- **Managed service role** — what THIS app uses it for (sessions/cache/queue/etc. in minimal tier collapsing to one DB).
- **Project secret** — what the framework uses it for + why it must be shared across containers.

**Comment style:**
- Explain WHY, not WHAT. Don't restate the field name. Include **contextual platform behavior** that makes the file self-contained — how fields interact, what propagates where, what happens at deploy time. The reader should never have to leave the file to understand it.
- 2-3 sentences per service (aim for the upper end — single-sentence comments consistently fail the 30% ratio on first attempt). Lines auto-wrap at 80 chars.
- No section-heading decorators (`# -- Title --`, `# === Foo ===`).
- Dev-to-dev tone — like explaining your config to a colleague.
- Reference framework commands where they add precision (e.g., the framework's dev start command, production dependency install flag, cache-warming CLI).
- **Each env's import.yaml must be self-contained — do NOT reference other envs.** Each env is published as a standalone deploy target on zerops.io/recipes; users land on one env's page, click deploy, and never see the others. Phrases like "same as env 0", "Consider HA (env 5) for higher durability", "zsc execOnce is a no-op here but load-bearing in env 4" are meaningless out of context. Explain what THIS env does and why, without comparing to siblings.

**The "WHY not WHAT" rubric is enforced by the `{env}_import_comment_depth` check.** Every env's import.yaml is scored on the fraction of comment blocks that contain at least one reasoning marker. A block passes when it contains any of:

- **Consequence** — `because`, `otherwise`, `without`, `so that`, `means that`, `prevents`, `causes`, `leads to`, `results in`.
- **Trade-off** — `instead of`, `rather than`, `in favor of`, `trade-off`.
- **Constraint** — `must`, `required`, `cannot`, `forced`, `mandatory`, `never`, `always`, `guaranteed`.
- **Operational consequence** — `rotation`, `rotate`, `redeploy`, `restart`, `scale`, `scaling`, `downtime`, `zero-downtime`, `rolling`, `fan-out`, `concurrent`, `race`, `lock`, `drain`.
- **Framework × platform intersection** — `build time`, `build-time`, `runtime`, `cross-service`, `at startup`, `at runtime`, `at import time`, `at deploy time`.
- **Decision framing** — `we chose`, `picked`, `default here`, `this tier`, `this env`, `matches prod`, `mirrors prod`.

At least **35%** of substantive comment blocks (≥ 20 chars body, grouped across contiguous `#` lines) must hit one of these markers, with a hard floor of 2 reasoning blocks per env. Pure narration — "Small production — minContainers: 2 enables rolling deploys" — fails the check even though the sentence is grammatical and factual. Rewrite it to carry WHY: "minContainers: 2 **because** a single-container production pool can't roll deploys without a traffic gap." The difference is the reasoning marker forcing the comment to answer "what happens if we flip this decision".

**Two-axis reminder for `minContainers ≥ 2` on a runtime service (envs 4-5 only).** On a service with real throughput demand (API, worker, any runtime that takes traffic volume), the comment can name either axis first — throughput OR HA/rolling-deploy — but should usually name both. On a service whose throughput genuinely fits in a single container at this tier's expected load, the HA/rolling-deploy axis is the **sole** justification and the comment MUST name it. A comment that only explains why throughput scaling doesn't apply and stops there is **thin**: it answers why axis (a) is not the reason but silently drops the reason the field is ≥2 anyway. Rewrite to state the remaining reason explicitly — e.g. "minContainers: 2 — this runtime handles the tier's expected concurrent request volume in a single container, so this is not throughput scaling; it exists **because** a single replica drops traffic on every rolling deploy and on container crashes." The reasoning marker forces the HA reason to surface.

v16's env comments regressed to field narration because "describe what the field does" is the path of least resistance. The rubric exists to make reasoning comments cheaper to produce than narration ones, not to trick the agent into stuffing words. Each reasoning marker is a hook to explain what would go wrong if the decision flipped — anchor your comment on that, not on the field name.

**Per-service uniqueness (v8.78 — `<env>_service_comment_uniqueness`).** Within a single env import.yaml, each service's lead-comment block must be distinguishable from every other service's by content tokens. The check computes pairwise Jaccard overlap on the rationale clauses (after stopword strip); pairs above 0.6 fail with both hostnames named.

The failure shape this catches: agent copy-pastes the same rationale across services with only the hostname swapped. Each service must name a mechanism unique to that service in this recipe:

- **Worker** rationales should name worker-specific mechanisms — `nc.drain()`, `queue: 'workers'`, "in-flight job", "queue group", "exactly-once delivery", "duplicate processing".
- **API** rationales should name API-specific mechanisms — `readinessCheck`, `/api/health`, "request handoff", "L7 connection drain", route-level concerns.
- **Static** (Nginx) rationales should name static-specific mechanisms — "Nginx", "asset serving", "static file serving", "free-replica economics", "near-zero CPU per replica".
- **Managed services** (db / cache / queue / storage / search) — name the mode trade-off specific to THAT service: HA failover for db, ephemeral cache miss for cache, queue replay semantics for queue, persisted-across-restarts for storage, rebuilt-from-database for search.

If you find yourself writing the same rationale shape across services, you have lost service-specificity — rewrite from each service's mechanism.

**Refining one env**: call `generate-finalize` again with only that env's entry under `envComments` — other envs are left untouched. Within an env, passing a service key with an empty string deletes its comment. Passing an empty project string leaves the existing project comment.

</block>

<block name="env-comments-example">

### Complete env comment template

```
zerops_workflow action="generate-finalize" \
  envComments={
    "0": {
      "service": {
        "appdev": "Development workspace for AI agents. zeropsSetup:dev deploys the full source tree so the agent can SSH in and edit over SSHFS. Subdomain gives the agent a URL to verify output.",
        "appstage": "Staging slot — agent deploys here with zerops_deploy setup=prod to validate the production build before finishing the task.",
        "db": "{dbDisplayName} — carries schema and app data. Shared by appdev and appstage. NON_HA fine for dev/staging; priority 10 so db starts before the app containers."
      },
      "project": "{appSecretKey} is the framework's encryption/signing key. Project-level so sessions remain valid when the L7 balancer routes a request to any app container."
    },
    "1": {
      "service": {
        "appdev": "Remote development workspace — SSH or IDE-SSHFS into the dev container and edit source live. zeropsSetup:dev installs the full dependency set so dev tools are available on the container.",
        "appstage": "Staging for remote developers — zerops_deploy setup=prod mirrors what CI would build for production.",
        "db": "{dbDisplayName} — same persistence layer as in env 0. NON_HA because remote dev environments are replaceable."
      },
      "project": "{appSecretKey} shared across containers (same rationale as env 0)."
    },
    "2": {
      "service": {
        "app": "Local-env validator — develop against localhost on your machine (zcli vpn up to reach this managed database), then push with zcli to this app container to verify the production build deploys cleanly before tagging a release.",
        "db": "Managed {dbDisplayName} reachable from your laptop via zcli VPN. Priority 10 so db starts before the app."
      },
      "project": "{appSecretKey} shared across containers."
    },
    "3": {
      "service": {
        "app": "Staging — mirrors production config (production dependency install + runtime optimizations) but runs on a single container with lower scaling. Public subdomain for QA and stakeholder review.",
        "db": "{dbDisplayName} — single-node because staging data is replaceable."
      },
      "project": "{appSecretKey} shared across containers."
    },
    "4": {
      "service": {
        "app": "Small production — minContainers: 2 guarantees at least two app containers at all times, spreading load and keeping traffic flowing during rolling deploys and container replacement. Zerops autoscales RAM within verticalAutoscaling bounds.",
        "db": "{dbDisplayName} single-node."
      },
      "project": "{appSecretKey} shared across containers — critical in production because sessions break if containers disagree on the key."
    },
    "5": {
      "service": {
        "app": "HA production. cpuMode: DEDICATED pins cores to this service so shared-tenant noise doesn't pollute request latency under sustained load. minContainers: 2 + autoscaling handles capacity; minFreeRamGB leaves 50% headroom for traffic spikes.",
        "db": "{dbDisplayName} HA — replicates data across multiple nodes so a single node failure causes no data loss or downtime. Dedicated CPU ensures DB ops don't compete with co-located workloads."
      },
      "project": "{appSecretKey} shared across every app container — required for session validity behind the L7 balancer at HA scale. corePackage: SERIOUS moves the project balancer, logging, and metrics onto dedicated infrastructure (no shared-tenant overhead) — essential for consistent latency at production throughput."
    }
  }
```

**Placeholders**: `{appSecretKey}` = the framework's secret key env var name (from research data: `APP_KEY`, `SECRET_KEY_BASE`, `SECRET_KEY`, etc.). `{dbDisplayName}` = the database display name (PostgreSQL, MariaDB, etc.). Replace with your recipe's actual values from the plan's research data.

</block>

<block name="showcase-service-keys">

**Showcase service keys — the key list depends on the worker's `sharesCodebaseWith`.** A shared-codebase worker (`sharesCodebaseWith` set) gets ONLY `workerstage` in envs 0-1 because the host target's dev container runs both processes. A separate-codebase worker (empty `sharesCodebaseWith` — the default, including the 3-repo case) gets both `workerdev` and `workerstage`. Omitting a comment key for a service that appears in the import.yaml produces a service with no comment, which degrades quality and risks failing the comment ratio check. Complete key list per env:

**Full-stack showcase:**
- **Envs 0-1 (shared-codebase worker)**: `"appdev"`, `"appstage"`, `"workerstage"`, plus all managed services (`"db"`, `"cache"`, `"storage"`, `"search"`, etc.)
- **Envs 0-1 (separate-codebase worker)**: `"appdev"`, `"appstage"`, `"workerdev"`, `"workerstage"`, plus all managed services
- **Envs 2-5**: `"app"`, `"worker"`, plus all managed services

**API-first showcase (dual-runtime):**
- **Envs 0-1 (shared-codebase worker)**: `"appdev"`, `"appstage"`, `"apidev"`, `"apistage"`, `"workerstage"`, plus all managed services
- **Envs 0-1 (separate-codebase worker)**: `"appdev"`, `"appstage"`, `"apidev"`, `"apistage"`, `"workerdev"`, `"workerstage"`, plus all managed services
- **Envs 2-5**: `"app"`, `"api"`, `"worker"`, plus all managed services

</block>

<block name="project-env-vars">

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

**Do not comment on the `{appSecretKey}` line yourself.** When `needsAppSecret == true`, the template auto-emits a 3-line rationale above the secret declaration (why it lives at project level: multi-container L7 balancer + signed-token verification must hold across deploy rolls). Your `envComments[i].Project` entry should cover the ENV-SPECIFIC context — AI-agent workspace, local-dev hybrid, small-prod scale, HA-prod scale — and any additional project-level vars you're declaring. Repeating the secret rationale in the agent-authored comment produces a duplicate block.

</block>

<block name="review-readmes">

### Step 2: Review READMEs

- Root README: verify intro text matches what this recipe actually demonstrates
- Env READMEs: descriptions are auto-generated from plan data — verify accuracy

</block>

<block name="comment-voice">

### Comment writing style (applies to both envComments and zerops.yaml fragments)

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

- Don't use "we" or "you" excessively

</block>

<block name="finalize-completion">

### Step 3: Complete

```
zerops_workflow action="complete" step="finalize" attestation="Comments provided via generate-finalize; all 6 import.yaml files regenerated with comments baked in"
```

</block>
</section>

<section name="close">
## Close — Verify (always) & Publish (only when asked)

Recipe creation is complete. The close step has THREE parts, run in order:

1. **1a — Static code review sub-agent (ALWAYS run, regardless of publish request).** A framework-expert sub-agent reviews the code, reports findings, applies fixes. NO browser walk inside this sub-agent — it never calls `zerops_browser` or `agent-browser`.
2. **1b — Main agent browser walk (ALWAYS run for showcase — skip for minimal).** After the sub-agent exits, the main agent performs the browser verification itself by calling the `zerops_browser` MCP tool (see Step 4c). This split is structural: browser work competes with dev processes and the sub-agent's tool calls for the zcp container's fork budget, and v5 proved that fork exhaustion kills everything in flight (the sub-agent's completed static review was nearly lost). Main agent runs single-threaded; `zerops_browser` auto-wraps lifecycle and auto-recovers from fork exhaustion.
3. **2 — Export & publish (ONLY when the user explicitly asks).** If the user did not request publishing, stop after 1a + 1b and any fixes are applied.

Do NOT skip 1a or 1b to save time. Do NOT publish without an explicit user request.

**Showcase close is an enforced sub-step gate.** For showcase recipes, close step complete is gated on BOTH `substep="code-review"` AND `substep="close-browser-walk"` attestations — the same shape as the deploy step's feature sub-agent gate. Attempting to call `zerops_workflow action="complete" step="close"` without attesting both sub-steps returns `recipe complete step: "close" has N required sub-steps — call complete with substep= for each`. This is the v18/v19 regression fix: both runs shipped with `close.browser` silently skipped because nothing gated on it. Minimal recipes skip the gate entirely (no feature dashboard to walk).

<block name="code-review-subagent">

### 1a. Static Code Review Sub-Agent (ALWAYS — mandatory)

Spawn a sub-agent as a **{framework} code expert** — not a Zerops platform expert. The sub-agent has NO Zerops context beyond what's in its brief: no injected guidance, no schema, no platform rules, no predecessor-recipe knowledge. Asking it to review platform config (zerops.yaml, import.yaml, zeropsSetup, envReplace, etc.) invites stale or hallucinated platform knowledge and framework-shaped "fixes" to platform problems. The main agent already owns platform config (injected guidance + live schema validation at finalize); the sub-agent's unique contribution is **framework-level code review** the main agent and automated checks cannot catch.

**The sub-agent does NOT open a browser.** Browser verification (1b below) is the main agent's job. Splitting code review from browser walk is structural: browser work on the zcp container competes with dev processes and the sub-agent's tool calls for the fork budget, and v5 proved that fork exhaustion during browser walk kills the sub-agent mid-run and can cascade to the parent chat. Static review is capability-bounded; browser walk is state-bounded; they belong to different actors.

The brief below is split into three explicit halves: direct-fix scope (framework code), symptom-only scope (observe and report; do NOT propose platform fixes), and out-of-scope (never touch).

**Sub-agent prompt template:**

> You are a {framework} expert reviewing the CODE of a Zerops recipe. You have deep knowledge of {framework} but NO knowledge of the Zerops platform beyond what's in this brief. Do NOT review platform config files (zerops.yaml, import.yaml) — the main agent has platform context and has already validated them against the live schema. Your job is to catch things only a {framework} expert catches.
>
> **CRITICAL — where commands run:** you are on the zcp orchestrator, not the target container. `{appDir}` is an SSHFS mount. All target-side commands (compilers, test runners, linters, package managers, framework CLIs, app-level `curl`) MUST run via `ssh {hostname} "cd /var/www && ..."`, not against the mount. The deploy step's "Where app-level commands run" block has the full principle and command list — read it before starting if anything here is unclear. If you see `fork failed: resource temporarily unavailable` or `pthread_create: Resource temporarily unavailable`, you ran a target-side command on zcp via the mount.
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
> **Silent-swallow antipattern scan (MANDATORY — introduced after v18's Meilisearch-silent-fail class bug):**
> - **In init-phase scripts** (seed, migrate, cache warmup, any file run from `initCommands` or a `execOnce`-gated command): grep for `catch` blocks whose only action is a `console.error` / `log.error` / `fmt.Println` followed by `return`, `continue`, or implicit fallthrough. Every such catch is a `[CRITICAL]` issue — report it with the file path, line number, and the specific side effect that will be silently skipped. The rule is documented in `init-script-loud-failure`: init scripts must `throw` / `exit 1` / `panic` on any unexpected error, no "non-fatal" labels.
> - **In client-side fetch wrappers** (every frontend component that issues an HTTP request): grep for `fetch(` calls without a `res.ok` check and for JSON parsers without a content-type verification. Every bare `const data = await res.json()` that doesn't check `res.ok` first is a `[WRONG]` issue. Every array-consuming store that lacks a `[]` default is a `[WRONG]` issue. The rule is documented in `client-code-observable-failure`.
> - **Async-durable writes without `await` on completion**: Meilisearch `addDocuments` / `updateSearchableAttributes` without a following `waitForTask`, Kafka producer without `flush()`, Elasticsearch bulk without `refresh`. Every such call in an init-phase script is a `[CRITICAL]` issue.
>
> **Feature coverage scan (MANDATORY):**
> - Read the plan's feature list (the main agent will include it in your brief). For each feature declared in `plan.Features`:
>   - If `surface` includes `api`: grep for a matching endpoint at `healthCheck`. Missing = `[CRITICAL]`.
>   - If `surface` includes `ui`: grep for `data-feature="{uiTestId}"` in the frontend sources. Missing = `[CRITICAL]`.
>   - If `surface` includes `worker`: grep for a worker handler matching the feature's subject / queue. Missing = `[CRITICAL]`.
> - Also grep for `data-feature="..."` attributes that are NOT in the declared feature list (extra features the sub-agent invented without a plan entry). Report as `[WRONG]` — the plan is authoritative; orphaned features are either undocumented scope creep or leftover from an earlier iteration that should be deleted.
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

**Close the 1a sub-step (showcase only).** After all CRITICAL / WRONG fixes are applied and the recipe has been redeployed:

```
zerops_workflow action="complete" step="close" substep="code-review" attestation="{framework} expert sub-agent reviewed N files, found X CRIT / Y WRONG / Z STYLE. All CRIT and WRONG fixed and redeployed. Silent-swallow scan: clean. Feature coverage scan: clean (all {N} declared features present)."
```

The attestation must name findings and fixes. Bare "review done" or "no issues found" attestations are rejected at the sub-step validator.

</block>

<block name="close-browser-walk">

### 1b. Main Agent Browser Walk (showcase only — MANDATORY; skip for minimal)

After 1a completes and any redeployments have settled, run the same 3-phase feature-iterating browser walk you ran at deploy Step 4c. The commands array is **re-built from `plan.Features`** — same feature list, same per-feature assertions, fresh browser state. See deploy **Step 4c: Browser verification — `dev-deploy-browser-walk`** for the iteration template, the per-feature pass criteria, and the command vocabulary. All of those rules apply unchanged at close.

**Close-specific rules** (on top of the deploy-step rules):

- **Rebuild the commands from `plan.Features` every time** — do not reuse a command array cached from the deploy walk. The sub-agent may have added data-feature hooks during its implementation pass; the close walk must read the live feature list to pick them up. A stale command array would silently skip features the sub-agent added after the first walk.
- **Re-run the feature-sweep against stage URLs** before starting the browser walk. Code-review 1a may have caused a redeploy; the sweep must be re-green on every api-surface feature BEFORE the browser walk iterates the UI surfaces. The sweep is your curl-level gate; the walk is your user-level gate; both must pass at close.
- Do NOT delegate browser work to a sub-agent. The 1a static review sub-agent explicitly forbids `zerops_browser` (v5 proved fork exhaustion during a sub-agent's browser walk kills the parent chat). Main agent runs single-threaded.
- Do NOT call `zerops_workflow action="complete" step="close"` until every declared feature passes every criterion (MustObserve, error banner empty, no console errors, no network failures) on BOTH the dev walk AND the stage walk, AND any regressions surfaced have been fixed and re-verified.
- If a walk surfaces a problem: the tool has already closed the browser, so fix on mount, redeploy the affected target, re-run the affected sweep, re-call `zerops_browser` for the affected subdomain. This counts toward the 3-iteration close-step limit.

**Close-step pass requires ALL of the following** (belt-and-suspenders):
1. Code review 1a: all `[CRITICAL]` / `[WRONG]` issues fixed, silent-swallow scan clean, feature coverage scan clean.
2. Feature sweep (stage): every api-surface feature returns 2xx + `application/json`, no `text/html`.
3. Browser walk (dev + stage): every UI-surface feature satisfies its `MustObserve`, every `[data-error]` banner empty, no JS console errors.

Close proceeds only when every layer is green.

**Close the 1b sub-step (showcase only).** After the browser walk has iterated every feature clean on both dev AND stage:

```
zerops_workflow action="complete" step="close" substep="close-browser-walk" attestation="Browser walk iterated {N} features on dev AND stage. Every MustObserve satisfied. [data-error] empty across all sections. No JS console errors, no failed network requests. Rebuilt commands from plan.Features live — no cached array."
```

The attestation must name the feature count and explicitly state BOTH dev and stage walks passed. A walk that only covered one subdomain is rejected.

Only after BOTH `substep="code-review"` AND `substep="close-browser-walk"` are attested can the agent call `zerops_workflow action="complete" step="close"` to finish the close step. Attempting the full-step complete without both substeps returns an error naming the pending ones — no silent skip possible.

</block>

<block name="export-publish">

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

**Create app repo(s) and push source**:

Each codebase in the recipe becomes its own GitHub repo under `zerops-recipe-apps/`. The number of `create-repo` + `push-app` pairs equals the number of codebases the plan has — NOT the number of services. Pass `--repo-suffix <hostname>` on both commands so every call lands on its own repo.

**Codebase count rule** — one codebase exists per runtime target that owns its own source tree:
- Every non-worker runtime target (`IsWorker: false`) owns a codebase.
- Every worker target with empty `sharesCodebaseWith` owns a codebase (separate-codebase worker).
- A worker with `sharesCodebaseWith` set owns NO codebase — it lives inside the host target's repo.

The shape depends on the research-step worker decision:

| Plan shape | Codebases | Publish calls |
|---|---|---|
| Single-runtime minimal (`app` + `db`) | 1 | `app` |
| Single-runtime + shared worker (Laravel Horizon, Rails Sidekiq, Django+Celery) | 1 | `app` |
| Single-runtime + separate worker | 2 | `app`, `worker` |
| Dual-runtime + shared worker (worker in API) | 2 | `app`, `api` |
| Dual-runtime + separate worker (3-repo showcase, API-first default) | 3 | `app`, `api`, `worker` |

**Shape of each call pair** — the `--repo-suffix` MUST match the codebase owner's hostname, and the `push-app` path MUST be the mount for that codebase:

```
zcp sync recipe create-repo {slug} --repo-suffix {hostname}
zcp sync recipe push-app    {slug} /var/www/{hostname}dev --repo-suffix {hostname}
```

**Dispatch all pairs in parallel** — the 6 calls (for a 3-repo showcase) have no ordering constraint between each other. Run them as parallel tool calls in a single message. Example for `nestjs-showcase` (dual-runtime + separate worker):

```
zcp sync recipe create-repo nestjs-showcase --repo-suffix app
zcp sync recipe push-app    nestjs-showcase /var/www/appdev    --repo-suffix app

zcp sync recipe create-repo nestjs-showcase --repo-suffix api
zcp sync recipe push-app    nestjs-showcase /var/www/apidev    --repo-suffix api

zcp sync recipe create-repo nestjs-showcase --repo-suffix worker
zcp sync recipe push-app    nestjs-showcase /var/www/workerdev --repo-suffix worker
```

Each repo ends up with its own `README.md` (the 3 fragments you wrote at generate for that codebase), its own `zerops.yaml`, and its own source tree — all three codebases were committed independently at generate.

For a single-codebase recipe you can omit `--repo-suffix` entirely; the default is `app` and the result is `{slug}-app`.

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

</block>

<block name="close-completion">

### 3. Completion
```
zerops_workflow action="complete" step="close" attestation="Recipe verified by {framework} expert sub-agent, {N} issues found and fixed"
```

Or skip if not publishing now:
```
zerops_workflow action="skip" step="close" reason="Will publish later"
```

</block>
</section>

<section name="generate-skeleton">
## Generate — App Code & Configuration

### Constraints
- Dev containers are RUNNING but env vars NOT active until deploy
- Each codebase is independent — never cross-scaffold between mounts
- Comment ratio >= 30% in zerops.yaml (aim 35%)

### Execution order
1. Scaffold each codebase on its mount [topic: where-to-write]
   - What to generate per recipe type [topic: recipe-types]
2. Write app code — skeleton only for showcase [topic: dashboard-skeleton]
3. On-container smoke test — prove install + validate loop works BEFORE committing to zerops.yaml [topic: smoke-test]
4. Write zerops.yaml — YOU, not a sub-agent [topic: zerops-yaml-rules]
   - Comment formatting rules [topic: comment-anti-patterns]
   - Dual-runtime URL pattern applies [topic: dual-runtime-urls]
   - Serve-only dev override [topic: serve-only-dev]
   - Multi-base secondary runtime install [topic: multi-base-dev]
   - Dev-server host-check allow-list [topic: dev-server-hostcheck]
   - Worker setup shape [topic: worker-setup]
   - Code quality and comment ratio [topic: code-quality]
5. Git init + commit

### Readme note
READMEs are NOT written here. They move to the post-deploy `readmes` sub-step so the gotchas section narrates actual debug experience instead of research-time speculation. Do not preemptively draft the knowledge-base fragment.

### Fetch guidance
Call `zerops_guidance topic="{id}"` before each sub-task for detailed rules.
All topics are filtered to your recipe shape — irrelevant content is excluded.
</section>

<section name="deploy-skeleton">
## Deploy — Build, Start, Verify & Narrate

### Constraints
- `zerops_deploy` triggers build from mount files — env vars resolve at deploy time
- Redeployment = fresh container — ALL background processes die, restart everything
- Max 3 iterations per step

### Execution order
1. Deploy appdev [topic: deploy-flow]
   - API-first: deploy apidev FIRST [topic: deploy-api-first]
2. Start ALL dev processes [topic: deploy-flow]
   - Asset dev server [topic: deploy-asset-dev-server]
   - Worker process [topic: deploy-worker-process]
3. Enable subdomain + verify [topic: deploy-target-verification]
4. Run init commands (migrations + seed)
5. Dispatch feature sub-agent (showcase) [topic: subagent-brief]
   - Where commands run [topic: where-commands-run]
6. Snapshot dev (showcase) — re-deploy dev to persist feature-sub-agent output into the deployed artifact. Durability step: the SSHFS mount is live but uncommitted; a mid-run container crash before cross-deploy would eat the work. [topic: deploy-flow]
7. Browser verification (showcase) [topic: browser-walk]
8. Cross-deploy to stage [topic: stage-deploy]
9. Verify stage [topic: deploy-target-verification]
10. Write per-codebase READMEs — narrate gotchas from the debug rounds you just lived through [topic: readme-fragments]
11. Handle failures [topic: deploy-failures]

### Fetch guidance
Call `zerops_guidance topic="{id}"` before each sub-task for detailed rules.
All topics are filtered to your recipe shape — irrelevant content is excluded.
</section>

<section name="finalize-skeleton">
## Finalize — Recipe Repository Files

### Constraints
- Do NOT edit import.yaml files by hand — use `generate-finalize` with structured input
- Each env's import.yaml must be self-contained — do NOT reference other envs
- Comment ratio >= 30% (aim 35%)

### Execution order
1. Write tailored comment set per environment via `generate-finalize` [topic: env-comments]
   - Showcase service key lists [topic: showcase-service-keys]
   - Dual-runtime projectEnvVariables [topic: project-env-vars]
2. Review READMEs
3. Apply comment writing style [topic: comment-style]
4. Complete

### Fetch guidance
Call `zerops_guidance topic="{id}"` before each sub-task for detailed rules.
</section>

<section name="close-skeleton">
## Close — Verify & Publish

### Constraints
- Do NOT skip code review (1a) or browser walk (1b)
- Do NOT publish without explicit user request
- Browser walk is main agent only — never delegate to sub-agent

### Execution order
1a. Static code review sub-agent [topic: code-review-agent]
    - Where commands run [topic: where-commands-run]
1b. Main agent browser walk (showcase only) [topic: close-browser-walk]
2. Export & publish (only when asked) [topic: export-publish]
3. Complete

### Fetch guidance
Call `zerops_guidance topic="{id}"` before each sub-task for detailed rules.
</section>
