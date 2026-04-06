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
- **Seed command**: optional data seeding

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

Includes everything from minimal research, PLUS the fields and targets below.

**Reference loading**: load both the hello-world AND the minimal recipe for your framework (see Reference Loading in the minimal section). The minimal recipe's gotchas and zerops.yaml patterns are your starting point — showcase extends them with additional services, not replaces them.

### Additional Showcase Fields
- **Cache library**: Redis client library for the framework
- **Session driver**: Redis-backed session configuration
- **Queue driver**: queue/job system for the framework
- **Storage driver**: object storage integration (S3-compatible)
- **Search library**: search integration (e.g., Meilisearch, Elasticsearch)
- **Mail library**: email sending (e.g., SMTP via Mailpit for dev)

### Showcase Targets
Define workspace services for showcase recipe. All targets appear in all 6 environment tiers (the finalize step handles per-env scaling and mode differences):
- **app**: runtime service — HTTP-serving primary application
- **worker**: background job processor (`isWorker: true`) — consumes from queue, no HTTP
- **db**: primary database
- **redis**: cache + sessions + queues (Valkey or KeyDB)
- **storage**: S3-compatible object storage
- **mailpit**: dev email testing (web UI for intercepted mail)
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

Recipes always use **standard mode**: each runtime gets a `{name}dev` + `{name}stage` pair. **Exception**: monorepo workers (same runtime as app) get only `{name}stage` — the app's dev container serves as the shared workspace for both processes. Polyglot workers (different runtime) get their own dev+stage pair.

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

> **Two kinds of "mount" (disambiguation):** (1) `zerops_mount` — SSHFS tool, mounts service `/var/www` locally for development. (2) Shared storage mount — platform feature, attaches a shared-storage volume at `/mnt/{hostname}`. These are completely unrelated.

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
zerops_workflow action="complete" step="provision" attestation="Services created: {list}. Env vars discovered: {list}. Dev mounted at /var/www/appdev/"
```
</section>

<section name="generate">
## Generate — App Code & Configuration

Write the application code, zerops.yaml, and README with documentation fragments. All files are written to the **mounted dev filesystem** at `/var/www/appdev/`.

### WHERE to write files

**SSHFS mount**: `/var/www/appdev/` — write all source code, zerops.yaml, and README here.
**Use SSHFS for file operations**, SSH for running commands (dependency installs, git init).
Files placed on the mount are already on the dev container — deploy doesn't "send" them, it triggers a build from what's already there.

### What to generate per recipe type

**Type 1 (runtime hello world):** Raw HTTP server with a single file. DB connection via standard library. Raw SQL migration for a `greetings` table. No framework, no ORM.

**Type 2a (frontend static):** SPA/static site. Framework project (React/Vue/Svelte) with a simple page showing framework name, greeting, and environment indicator. Build-time env var injection. No DB connection.

**Type 2b (frontend SSR):** SSR framework project (Next.js/Nuxt/SvelteKit). Server-rendered pages with DB connection. Framework's API routes for health endpoint.

**Type 3 (backend framework):** Full framework project. ORM-based migrations, template-rendered dashboard, framework CLI tools. Uses the framework's conventions throughout.

### Two kinds of import.yaml (critical distinction)

1. **Workspace import** (provision step) — creates the agent's dev/stage infrastructure. NO `zeropsSetup`, NO `buildFromGit`. Services use `startWithoutCode` (dev) or wait for deploy (stage).
2. **Recipe import** (finalize step) — the 6 deliverable files for end users. Uses `zeropsSetup: dev`/`zeropsSetup: prod` + `buildFromGit` to map hostnames to setup names.

zerops.yaml ALWAYS uses **generic setup names**: `setup: dev` and `setup: prod`. During workspace deploy, the `zerops_deploy` tool's `setup` parameter maps the service hostname to the correct setup name (e.g. `targetService="appdev" setup="dev"`). In recipe import.yaml, `zeropsSetup: dev`/`zeropsSetup: prod` does the same mapping for `buildFromGit` deploys.

### zerops.yaml — Write ALL setups at once

Write the complete zerops.yaml with ALL setup entries in a single file. Minimal recipes have TWO setups (`dev` + `prod`). Showcase recipes have THREE (`dev` + `prod` + `worker`). The same file is the source of truth for the deploy step AND for the README integration-guide fragment — writing it once eliminates drift between what deploys and what the README documents. The deploy step will verify dev against the live service, then cross-deploy the already-written prod/worker configs to stage.

Follow the injected **zerops.yaml Schema** for all field rules. Recipe-specific conventions for each setup:

**`setup: dev`** (self-deploy from SSHFS mount — agent iterates here):
- `deployFiles: [.]` — **MANDATORY for self-deploy**; anything else destroys the source tree
- `start: zsc noop --silent` — exception: omit `start` for implicit-webserver runtimes (php-nginx, php-apache, nginx, static)
- **NO healthCheck, NO readinessCheck** — agent controls lifecycle; checks would restart the container during iteration
- Framework mode flags set to dev values (`APP_ENV: local`, `NODE_ENV: development`, `DEBUG: "true"`, verbose logging)
- Same cross-service refs from `zerops_discover` as prod — only mode flags differ
- **Dev dependency pre-install**: if the build base includes a secondary runtime for an asset pipeline, dev `buildCommands` MUST include the dependency install step for that runtime's package manager. This ensures the dev container ships with dependencies pre-populated — the developer (or agent) can SSH in and immediately run the dev server without a manual install step first. Omit the asset compilation step — that's for prod only; dev uses the live dev server.

**`setup: prod`** (cross-deployed from dev to stage — end-user production target):
- Real `buildCommands` (dependency install with prod flags, asset compilation, binary compilation, etc.)
- Real `deployFiles` listing only what the runtime needs (not `.`) — verify completeness: every path your start command and framework touch at runtime MUST appear here
- `healthCheck` (httpGet on app port + health path) — **required**; unresponsive containers get restarted
- `deploy.readinessCheck` if `initCommands` contains migrations
- `initCommands` for framework cache warming (Laravel `config:cache|route:cache|view:cache`, Rails `assets:precompile` if paths leak, Symfony `cache:warmup`) — **never** in buildCommands; those caches bake `/build/source/...` paths that break at `/var/www/...`
- Framework mode flags set to prod values (`APP_ENV: production`, `NODE_ENV: production`, `DEBUG: "false"`)
- Same cross-service ref keys as dev — **only values on mode flags differ**

**`setup: worker`** (showcase only — background job processor):

Whether the worker shares the app's codebase or is a separate project depends on the runtime: **same base runtime type = monorepo** (e.g., both `php-nginx@8.4`), **different type = polyglot** (e.g., `bun@1.2` app + `python@3.12` worker). The system detects this from the plan targets and adjusts the workspace, deploy flow, and published repos automatically.

**Monorepo workers** (same runtime): write a `setup: worker` block in the SAME zerops.yaml alongside `dev` and `prod`. The worker setup shares the build pipeline but runs a different start command. During development, the agent starts both the web server and queue worker as separate SSH processes from the single `appdev` container — no `workerdev` needed.

**Polyglot workers** (different runtime): the worker is a separate codebase with its own zerops.yaml containing `dev` and `prod` setups. The agent writes it to a separate mount (`/var/www/workerdev/`). No `setup: worker` in the app's zerops.yaml — each codebase has its own `dev`/`prod` pair.

Worker setup conventions (apply to both patterns):
- `start` is the framework's queue/job runner command. MANDATORY — workers have no implicit webserver.
- **NO healthCheck, NO readinessCheck** — workers don't serve HTTP. They consume from a queue (Redis, NATS, RabbitMQ) and crash-restart is handled by the platform automatically.
- **NO `ports` section** — workers don't bind any port.
- `envVariables` shares the same cross-service refs as prod (DB, cache, queue connection vars) PLUS any worker-specific vars (concurrency, retry config). Mode flags match prod.
- `initCommands` same as prod where applicable (migrations via `zsc execOnce`, cache warming).
- Build section typically identical to prod (same dependencies, same compilation).

**Shared across all setups:**
- `envVariables:` contains ONLY cross-service references from `zerops_discover` + framework mode flags. **Do NOT add envSecrets** (framework secret keys) — they are already injected as OS env vars automatically by the platform.
- Setup names are generic (`dev`/`prod`/`worker`). `zerops_deploy targetService=... setup=...` maps hostnames to setup names at deploy time.
- dev and prod env maps must NOT be bit-identical — a structural check fails the generate step if they are, because it means the dev container behaves exactly like prod (caches enabled, stack traces hidden during iteration).

### .env.example preservation

If the framework scaffolds a `.env.example` file (e.g., `composer create-project`), **keep it** — it documents the expected environment variable keys for local development. Remove `.env` (contains generated secrets), but preserve `.env.example` with empty values as a reference for users running locally.

### Framework environment conventions

Use the framework's **standard** environment names and values — don't invent new ones. Check the framework's documentation for the correct dev/production mode flag. Wrong env names cause subtle behavior differences (e.g., debug mode not activating, error pages not showing, optimizations not running). The runtime hello-world recipe loaded during research documents the base patterns.

If the framework has a "base URL" / "app URL" / "public URL" environment variable that controls absolute URL generation, set it to `${zeropsSubdomain}` in `run.envVariables`. Without it, the framework defaults to localhost and any feature producing absolute URLs (redirects, mail links, signed URLs, CSRF origin) breaks silently.

### Required endpoints

**Types 1, 2b, 3, 4 (server-side):**
- `GET /` — health dashboard (HTML, shows framework name + service connectivity)
- `GET /health` or `GET /api/health` — JSON health endpoint
- `GET /status` — JSON status with actual connectivity checks (DB ping, cache ping, latency)

**Type 2a (static frontend):**
- `GET /` — simple page showing framework name, greeting, timestamp, environment indicator
- No server-side health endpoint (static files only)

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
- Comment ratio in zerops.yaml code blocks must be >= 0.3
- No `PLACEHOLDER_*`, `<your-...>`, or `TODO` strings
- All env var references must use discovered variable names
- Comments explain WHY, not WHAT (don't restate the key name)
- Max 80 chars per comment line

### Pre-deploy checklist
- [ ] Both `setup: dev` AND `setup: prod` present (generic names)
- [ ] Showcase: `setup: worker` present — no healthCheck, no ports, mandatory start command
- [ ] dev: `deployFiles: [.]`, no healthCheck, no readinessCheck
- [ ] prod: real buildCommands, specific deployFiles, healthCheck + readinessCheck
- [ ] dev and prod envVariables differ on mode flags (APP_ENV/NODE_ENV/DEBUG/LOG_LEVEL)
- [ ] envVariables has only cross-service refs + mode flags — no envSecrets re-referenced
- [ ] All env var refs use names from `zerops_discover`, none guessed
- [ ] If prod `buildCommands` compiles assets, primary view loads them via framework asset helper (not inline CSS/JS)
- [ ] If dev build base includes a secondary runtime for an asset pipeline, dev `buildCommands` includes the package manager install
- [ ] README has all 3 extract fragments with proper markers
- [ ] `.env.example` preserved (`.env` removed)

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

**Voice:**
- Direct, concise, no filler ("Install production deps only" not "In this step we will install the production dependencies")
- Explain the WHY and the consequence, not the WHAT ("CGO_ENABLED=0 produces a fully static binary — no C libraries linked at runtime" not "Set CGO_ENABLED to 0")
- Mention Zerops-specific behavior when it differs from standard ("npm prune after build — runtime container doesn't re-install" not just "npm prune")
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

Deploy follows the same flow as bootstrap standard mode. Deploy dev first, verify it works, then generate stage and deploy stage.

### Dev deployment flow

**Step 1: Deploy appdev (self-deploy)**
```
zerops_deploy targetService="appdev" setup="dev"
```
The `setup="dev"` parameter maps hostname `appdev` to `setup: dev` in zerops.yaml. This triggers a build from files already on the mount. Blocks until complete.

**Step 2: Start the dev server**
- **Server-side apps** (types 1, 2b, 3, 4): Start via SSH:
  ```bash
  ssh appdev "cd /var/www && {start_command} &"
  ```
- **Implicit-webserver runtimes** (php-nginx, php-apache, nginx): Skip primary server — auto-starts.
- **Static frontends** (type 2a): Skip — Nginx serves the built files automatically.

**Step 2b: Start auxiliary dev processes**
If the build pipeline includes a secondary runtime (installed via `sudo -E zsc install` in `run.prepareCommands`), check whether the scaffold defines a dev server or watch process for that runtime. If it does, start it via SSH after deploy:
```bash
ssh appdev "cd /var/www && {dev_server_command} &"
```
The dev server command comes from the scaffold's package manager scripts (the `dev` script in `package.json`, `composer.json`, `Makefile`, etc.) — use whatever the scaffold provides. If the dev server needs to accept connections from outside the container (asset servers typically do), pass the appropriate host binding flag so it listens on `0.0.0.0` instead of localhost.

This applies even when the primary server auto-starts (implicit-webserver runtimes) — the primary server handles HTTP requests, but auxiliary dev tooling (asset compilation, HMR, file watchers) is a separate process that must be started explicitly.

Without this step, templates that reference build-pipeline outputs will fail at runtime (missing manifests, uncompiled assets). Do NOT work around this by replacing framework asset helpers with inline CSS/JS — that disconnects the build pipeline (see "Asset pipeline consistency" in the generate section). The dev container must prove the full development experience works, including live asset compilation and any watch processes the scaffold defines.

**Step 3: Enable dev subdomain**
```
zerops_subdomain action="enable" serviceHostname="appdev"
```

**Step 4: Verify appdev**
```
zerops_verify serviceHostname="appdev"
```
Check: service RUNNING, subdomain returns 200, health endpoint responds (or page loads for static).

**Step 5: Start worker dev process** (showcase only — skip for minimal)
If the recipe has worker targets, how you start the dev worker depends on the architecture:

- **Monorepo** (worker same runtime as app): start the queue worker as an SSH process on appdev alongside the web server:
  ```bash
  ssh appdev "cd /var/www && {queue_worker_command} &"
  ```
  No workerdev service exists — appdev hosts both processes from the same source.

- **Polyglot** (worker different runtime): deploy the separate worker codebase:
  ```
  zerops_deploy targetService="workerdev" setup="dev"
  ```
  Then start the worker process via SSH on workerdev.

Verify worker is running via logs (no HTTP endpoint):
```
zerops_logs serviceHostname="{worker_hostname}" limit=20
```

**Step 6: Iterate if needed** (max 3 iterations)
If verification fails: check logs (`zerops_logs serviceHostname="appdev"`), fix code on mount, kill previous server, restart via SSH, re-verify.

### Stage deployment flow

**Step 7: Verify prod setup (already written at generate)**
The prod setup block was written to zerops.yaml during the generate step. Before cross-deploying, verify it matches what a real user building from git will need:
- `deployFiles` lists every path the start command and framework need at runtime — run `ls` on the mount and cross-reference. When cherry-picking (not using `.`), missing one path will DEPLOY_FAILED at first request.
- `healthCheck` + `deploy.readinessCheck` are present (required for prod — unresponsive containers get restarted; broken builds are gated from traffic).
- `initCommands` covers framework cache warming + migrations (NEVER in buildCommands — `/build/source/...` paths break at `/var/www/...`).
- Mode flags differ from dev (APP_ENV/NODE_ENV/DEBUG/LOG_LEVEL).

If anything is missing, edit zerops.yaml on the mount now — the change propagates to the README via the integration-guide fragment (which mirrors the file content).

**Step 8: Deploy appstage from appdev (cross-deploy)**
```
zerops_deploy sourceService="appdev" targetService="appstage" setup="prod"
```
The `setup="prod"` maps hostname `appstage` to `setup: prod` in zerops.yaml. Stage builds from dev's source code with the prod config. Server auto-starts via the real `start` command (or Nginx for static).

**Step 8b: Connect shared storage** (if applicable)
After stage transitions from READY_TO_DEPLOY to ACTIVE, connect storage:
```
zerops_manage action="connect-storage" serviceHostname="appstage" storageHostname="storage"
```

**Step 9: Deploy workerstage** (showcase only — skip for minimal)
- **Monorepo**: cross-deploy from appdev with the worker setup:
  ```
  zerops_deploy sourceService="appdev" targetService="workerstage" setup="worker"
  ```
  The `setup="worker"` maps to `setup: worker` in the shared zerops.yaml — same build pipeline, different start command.
- **Polyglot**: cross-deploy from workerdev:
  ```
  zerops_deploy sourceService="workerdev" targetService="workerstage" setup="prod"
  ```
  The worker has its own zerops.yaml with `setup: prod`.

**Step 10: Enable stage subdomain**
```
zerops_subdomain action="enable" serviceHostname="appstage"
```

**Step 11: Verify appstage**
```
zerops_verify serviceHostname="appstage"
```
For showcase, also verify the worker is running:
```
zerops_logs serviceHostname="workerstage" limit=20
```

**Step 12: Present URLs**

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

**Showcase service keys**: in envs 2-5, worker is `"worker"`. In envs 0-1, the keys depend on the worker architecture: **monorepo** (same runtime as app) has only `"workerstage"` — no workerdev exists. **Polyglot** (different runtime) has both `"workerdev"` and `"workerstage"`. Other showcase services use base hostname everywhere: `"redis"`, `"storage"`, `"mailpit"`, `"search"`. Every service that appears in a given env's import.yaml should have a comment explaining its role in THAT env.

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
- Explain WHY, not WHAT. Don't restate the field name.
- 1-3 sentences per service. Lines auto-wrap at 80 chars.
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
>
> **README review:**
> - Does the integration-guide include numbered steps for code changes the agent made that any user would also need? (e.g., trusted proxy config, storage driver wiring). Demo-specific code (custom routes, views) does NOT belong — only changes that apply to any app on Zerops.
> - Does the knowledge-base fragment contain ONLY irreducible content (not repeating zerops.yaml)?
> - Is there clear separation: integration-guide = actionable steps, knowledge-base = awareness/gotchas?
> - Are there exactly 3 extract fragments with proper markers?
>
> Report issues as: `[CRITICAL]` (breaks deploy), `[WRONG]` (incorrect but works), `[STYLE]` (quality improvement).

Apply any CRITICAL or WRONG fixes, then **redeploy** to verify the fixes work:
- If zerops.yaml or app code changed: `zerops_deploy targetService="appdev" setup="dev"` then cross-deploy to stage
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
