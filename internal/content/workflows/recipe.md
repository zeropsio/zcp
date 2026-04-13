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
- **Type** — pick the **highest available version** from `availableStacks` for each stack. Must include the `@version` suffix (e.g. `nodejs@22`, not bare `nodejs`). The same versioned form is required for the top-level `runtimeType` field on the plan.
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

**Scaffold each codebase inside its own mount — never cross-contaminate.** Framework scaffolders write config files (`tsconfig.json`, `package.json`, `.npmrc`, `.vscode/`, `.gitignore`, framework-specific dotfiles) into whatever directory they run from, and they trust the process working directory as the project root. Running a scaffolder from the wrong SSH session — or `cd`-ing inside one SSH session to a path that belongs to a different service — silently overwrites the other codebase's config. Rules that apply to every multi-codebase plan:

1. SSH into the dev service whose mount you are about to scaffold. Scaffolding the API codebase means SSH to the API dev service; scaffolding the frontend means SSH to the frontend dev service; scaffolding a separate-codebase worker means SSH to the worker dev service.
2. `cd /var/www/{that service's hostname}/` before invoking the scaffolder.
3. If the target dev service's base image does not ship the scaffolder's runtime (common example: a static-base frontend service has no Node interpreter), write the scaffold files directly via SSHFS from the agent's host context instead of invoking the scaffolder on the container.
4. Never scaffold into `/tmp` and copy — scaffolder footprints always include hidden files you will miss.
5. Never invoke a scaffolder from one service's SSH session while `cd`'d into another service's mount. The process working directory wins, and the "wrong" codebase's config files will be overwritten even though the shell prompt looks correct.

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

**For the 6 deliverable import.yaml files**: pass `projectEnvVariables` as a first-class input to `zerops_workflow action="generate-finalize"` at finalize — the full per-env shape lives in finalize Step 1b. Do NOT hand-edit the generated files; re-running `generate-finalize` re-renders from template.

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

**Order of operations — main agent first, sub-agents second:**

1. **Main agent writes zerops.yaml for every codebase sequentially.** Platform config stays the main agent's responsibility.
2. **Main agent writes the README skeleton for every codebase sequentially** — intro + integration-guide fragment (with the zerops.yaml verbatim) + empty knowledge-base placeholder. The main agent returns after deploy to narrate gotchas.
3. **THEN dispatch scaffolding sub-agents in parallel, one per codebase**, each with the brief template below.

**Scaffold sub-agent brief — include verbatim (edit only the codebase-specific names and service list from the plan):**

> You are scaffolding a health-dashboard-only skeleton. **You write infrastructure. You do NOT write features.** A feature sub-agent runs later with SSH access to live services and authors every feature section end-to-end (API routes + frontend components + worker payloads as a single unit). Your job is to give that sub-agent a healthy, deployable, empty canvas to build on.
>
> **WRITE (frontend codebase):**
>
> - `package.json` — production dependencies for the framework and any CSS tooling the scaffold would normally include
> - Framework config (`vite.config.ts`, `tsconfig.json`, `.env.example`)
> - `App.svelte` (or equivalent entry) that renders `<StatusPanel />` **and nothing else** — no routing, no layout with empty slots, no tabs, no nav. One component mounted.
> - `StatusPanel.svelte` — polls `GET /api/status` every 5s, renders one row per managed service in the plan with a colored dot (green = "ok", yellow = "degraded", red = missing/error) and the service name. That's the whole UI. No forms, no buttons, no tables, no tabs.
> - `main.ts` / `main.js` — framework bootstrap
>
> **WRITE (API codebase):**
>
> - `package.json` with production dependencies for the framework, ORM, and every managed-service client in the plan (Redis, NATS, S3, Meilisearch, etc.)
> - `GET /api/health` — liveness probe returning `{ ok: true }`. No service calls.
> - `GET /api/status` — deep connectivity check. Returns a flat object with one key per service in the plan: `{ db: "ok", redis: "ok", nats: "ok", storage: "ok", search: "ok" }`. Each value is `"ok"` on successful ping, `"error"` otherwise. Exactly these keys; exactly these values.
> - Service client initialization for **every** managed service in the plan, from env vars. Import and configure the client library, expose the client for later use.
> - Migrations for the primary data model. Full schema — the feature sub-agent will add read/write endpoints against it.
> - Seed data — 3 to 5 rows. **Not 15-25.** The feature sub-agent expands seeds as it implements features that need more.
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
> - `.gitignore`, `.env.example`, framework lint config only if the framework's own scaffolder normally emits one
>
> **DO NOT WRITE (any codebase):**
>
> - Item CRUD endpoints, item list components, create-item forms, item detail views
> - Cache-demo routes, cached-vs-fresh components
> - Search endpoints or search UI
> - Jobs-dispatch endpoints, jobs UI, jobs history tables, worker job processors
> - Storage upload endpoints, file list components, upload forms
> - Anything that calls a managed service beyond the one connectivity ping in `/api/status`
> - Rich UX: styled forms, tables with headers, submit-state badges, contextual hints, error flashes, empty states, `$effect` hooks that auto-load data, typed response interfaces for feature payloads, inline section-level styles
> - Routing, tabs, layouts with multiple sections, nav components, pagination
> - CORS config, proxy rules, `types.ts` shared between codebases — the main agent resolves cross-codebase integration during verification
>
> **The dashboard you ship is one green-dot panel.** A reader looking at the deployed page should see five rows: `db • green`, `redis • green`, `nats • green`, `storage • green`, `search • green` (with the service names from the plan). That is the correct, expected, final output of the scaffold phase. The feature sub-agent at deploy step 4b builds every showcase section on top of this — owning API routes, frontend components, and worker payloads as a single coherent author — so the dashboard at close time is rich and feature-complete. If you are tempted to add a "small demo" or "minimal example" of any managed service, stop: that is the feature sub-agent's job.
>
> **Reporting back:** return a bulleted list of the files you wrote and the env var names you wired for each managed service. Do not claim you implemented any features. You didn't. If your return value makes the main agent think step 4b is already done, the brief was not followed.

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

**The injected predecessor recipe's gotchas are a starting inventory, not the answer.** The generate step injects the direct predecessor recipe's `## Gotchas` section into your context so you have a working baseline. Treat it as a **floor, not a ceiling**:

1. **Re-evaluate each predecessor gotcha** against this recipe's actual library and architecture choices. Keep the ones that still apply (same ORM, same runtime model, same framework idiom). Drop the ones that don't (e.g. swap TypeORM for Prisma → the `synchronize: true` gotcha is irrelevant; swap Express for Fastify → the trust-proxy gotcha doesn't apply).
2. **Add net-new gotchas narrated from what actually happened during THIS build.** The predecessor covers only the services and patterns its own tier provisions — a hello-world has no managed services, a minimal usually has just a database. A showcase adds caches, queues, object storage, search engines, background workers, frontend build pipelines, cross-service env wiring. Every one of those is a surface where you made decisions, hit bugs, and worked around platform behavior. Write those up.
3. **Each README's knowledge-base is validated against the predecessor baseline.** Showcase-tier runs fail if the knowledge-base fragment is mostly a clone of the predecessor — the check counts gotcha stems that don't match any predecessor stem and requires at least 2 per README. Cloning the predecessor gotchas verbatim (or with cosmetic rewording like "needs" → "requires") does not clear the floor.

Sources of narratable gotchas from this session (use these as raw material):
- **Managed services not in the predecessor** — cache connection patterns, queue client auth, S3 path-style, search index rebuilds. One per service is the minimum.
- **Framework-library quirks discovered at generate time** — ESM/CJS mismatches, peer dep conflicts, build-time vs run-time env var boundaries.
- **Deploy failures you fixed** — build output mismatches, stale build artifacts committed to git, readiness check timing, migration races, init-command gating.
- **Feature-implementation decisions** — why you chose one client library over another, why a workaround exists, what the alternative would have broken.
- **Platform behavior that surprised you** — not "what Zerops does" in general, but "what Zerops did differently from what I expected while building this specific recipe."

**What belongs in knowledge-base vs integration-guide:**
- If it's a **required code change** → integration-guide step (the user needs to do this)
- If it's a **gotcha or quirk** the user should know about → knowledge-base (awareness, not action)
- If both: put the actionable step in integration-guide, put the "why it matters" explanation in knowledge-base. Example: trustProxies config is an integration step (action), but "CSRF fails without it because L7 terminates SSL" is a gotcha (awareness).

Do NOT include:
- Config values already visible in zerops.yaml (don't re-explain what the comments already cover)
- Platform universals (build/run separation, L7 routing, tilde behavior, autoscaling timing)
- Generic framework knowledge (how the framework works, what build tools do)
- **Verbatim paraphrases of the predecessor recipe's gotchas** — the predecessor is already in the injected chain; your job is to extend it, not mirror it.

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

- Every mount path the sub-agent owns — **apidev AND appdev AND workerdev** (when a separate-codebase worker exists). The sub-agent writes to all three as a single unit so API routes, worker payloads, and frontend consumers stay in contract lock-step. This is non-negotiable and is the single biggest reason v10/v11/v12 shipped contract-mismatch bugs — parallel authors cannot keep contracts consistent.
- The plan's managed-service list and which feature section each maps to
- **Contract-first rule**: for every feature section, the sub-agent defines the API response shape FIRST, the worker payload shape FIRST (if a worker is involved), then implements the backend, then consumes the same exact shape on the frontend. Frontend and backend for the same feature are written as adjacent edits, not as separate passes.
- **Seed expansion**: the scaffold left 3-5 rows. The sub-agent expands the seed to 15-25 records as part of implementing the features that need them.
- **Search indexing**: if a search engine is provisioned, the sub-agent writes the search-sync step (after `db:seed`) in `initCommands` — the scaffold intentionally left this out.
- **UX quality contract** (see below)
- **Where app-level commands run** (hard rule, see below) — include verbatim
- **Port hygiene**: before starting any dev server, kill any existing holder of the port first: `ssh {hostname}dev "fuser -k {httpPort}/tcp 2>/dev/null || true"`
- **Verify each feature as you write it** — the sub-agent has SSH access to every dev container and every managed service is reachable. After each controller + frontend pair, hit the endpoint via `ssh {hostname}dev "curl -s localhost:{port}/..."` and verify the response shape matches what the frontend consumer expects. Fix immediately; do not write ahead of verification.

**Managed service connection patterns** — before writing the sub-agent brief, use `zerops_knowledge query="connection pattern {serviceType}"` for every managed service in the plan. Include auth format, connection string construction, and known client-library pitfalls directly in the brief. Key pitfalls to inject:
- **Valkey/KeyDB (cache)**: no authentication — use `redis://hostname:port` without credentials. Do NOT reference `${cache_user}` or `${cache_password}`.
- **NATS (queue)**: credentials must be passed as separate connection options (`user`, `pass`), NOT embedded in the URL. URL-embedded credentials are silently ignored by most NATS client libraries.
- **Object Storage (S3)**: requires `apiUrl`, `accessKeyId`, `secretAccessKey`, `bucketName` — NOT the `connectionString` format used by databases.

**Dependency hygiene**: when adding packages, check the existing lockfile for the major version of the framework's core package. Pin new packages from the same framework family to the same major version. Run the install command after each batch of package additions to catch peer-dependency conflicts immediately.

**Feature sections the sub-agent owns end-to-end** — for each provisioned service, the sub-agent authors the API route, the backing logic, the worker payload (if applicable), AND the frontend component that consumes the response, as a **single edit session** (not as separate passes):

- **Database** — list seeded records + create-record form. Typed response interface, paginated table with headers and row shading, submit-state feedback on the form.
- **Cache** (if provisioned) — store-a-value-with-TTL route + cached-vs-fresh demonstration showing timing. Cache is for cache + sessions only; the queue uses NATS, a separate broker.
- **Object storage** (if provisioned) — upload-file (multipart) + list-files routes. Frontend form shows upload progress and a list of previously-uploaded files.
- **Search engine** (if provisioned) — live search over seeded records. Frontend debounces input and renders the result array.
- **Messaging broker + worker** (if provisioned) — dispatch-job POST publishes to a NATS subject; the worker (which the sub-agent implements) consumes, does simulated work, writes the result to a DB table or Redis key; the frontend polls the result endpoint and renders (a) dispatched timestamp, (b) processed timestamp, (c) result payload. This exercises the full NATS → worker → result round-trip.

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

**Where app-level commands run** (hard rule — include verbatim in sub-agent briefs):

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

If a walk reveals a problem curl missed: the batch has already closed the browser, so fix on mount, redeploy, and run the affected phase again (counts toward the 3-iteration limit). Do NOT advance to publish until BOTH appdev AND appstage walks show empty errors and populated sections.

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

<block name="deploy-completion">

### Completion
```
zerops_workflow action="complete" step="deploy" attestation="Dev deployed at {dev_url}, stage deployed at {stage_url}. Both healthy."
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
- **Scaling rationale** for fields only present in this env: `minContainers: 2` (envs 4-5), `cpuMode: DEDICATED` (env 5), `mode: HA` (env 5), `corePackage: SERIOUS` (env 5).
- **Managed service role** — what THIS app uses it for (sessions/cache/queue/etc. in minimal tier collapsing to one DB).
- **Project secret** — what the framework uses it for + why it must be shared across containers.

**Comment style:**
- Explain WHY, not WHAT. Don't restate the field name. Include **contextual platform behavior** that makes the file self-contained — how fields interact, what propagates where, what happens at deploy time. The reader should never have to leave the file to understand it.
- 2-3 sentences per service (aim for the upper end — single-sentence comments consistently fail the 30% ratio on first attempt). Lines auto-wrap at 80 chars.
- No section-heading decorators (`# -- Title --`, `# === Foo ===`).
- Dev-to-dev tone — like explaining your config to a colleague.
- Reference framework commands where they add precision (e.g., the framework's dev start command, production dependency install flag, cache-warming CLI).
- **Each env's import.yaml must be self-contained — do NOT reference other envs.** Each env is published as a standalone deploy target on zerops.io/recipes; users land on one env's page, click deploy, and never see the others. Phrases like "same as env 0", "Consider HA (env 5) for higher durability", "zsc execOnce is a no-op here but load-bearing in env 4" are meaningless out of context. Explain what THIS env does and why, without comparing to siblings.

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

</block>

<block name="close-browser-walk">

### 1b. Main Agent Browser Walk (showcase only — MANDATORY; skip for minimal)

After 1a completes and any redeployments have settled, run the same 3-phase browser walk you ran at deploy Step 4c: Phase 1 (dev walk while dev processes are running) → Phase 2 (kill dev processes via SSH) → Phase 3 (stage walk after dev processes are dead). See deploy **Step 4c: Browser verification** for the full rules, the `zerops_browser` tool usage, the command vocabulary, and the `forkRecoveryAttempted` recovery procedure — they are unchanged at close.

**Close-specific rules** (on top of the deploy-step rules):

- Do NOT delegate browser work to a sub-agent. The 1a static review sub-agent explicitly forbids `zerops_browser` (v5 proved fork exhaustion during a sub-agent's browser walk kills the parent chat). Main agent runs single-threaded.
- Do NOT call `zerops_workflow action="complete" step="close"` until `zerops_browser` has returned clean output (`errorsOutput` empty, all sections populated, `forkRecoveryAttempted: false`) for BOTH the dev walk AND the stage walk AND any regressions surfaced have been fixed and re-verified.
- If a walk surfaces a problem: the tool has already closed the browser, so fix on mount, redeploy the affected target, re-call `zerops_browser` for the affected subdomain. This counts toward the 3-iteration close-step limit.

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
