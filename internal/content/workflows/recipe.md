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
Hello-world recipes exist per RUNTIME, not per framework. The hello-world IS the runtime guide — proven zerops.yaml patterns for that runtime. Load it:
```
zerops_knowledge recipe="{runtime-base}-hello-world"
```
Example: for Laravel (php-nginx runtime), load `php-hello-world`. For Next.js (nodejs runtime), load `nodejs-hello-world`.

Your job is to extend this base with framework-specific knowledge (documentRoot, multi-base builds, trusted proxy config, etc.). These discoveries go into the zerops.yaml comments and knowledge-base fragment.

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
- **Needs app secret**: does the framework require an APP_KEY/SECRET_KEY?
- **Logging driver**: stderr (preferred), file, syslog

### Decision Tree Resolution
Resolve these 4 decisions (ZCP provides defaults, you may override with justification):
1. **Web server**: builtin (Node/Go/Rust), nginx-sidecar (PHP), nginx-proxy (static)
2. **Build base**: primary runtime; add nodejs to buildBases if frontend asset build needed (Vite/Webpack)
3. **OS**: ubuntu-22 (default), alpine (Go/Rust static binaries)
4. **Dev tooling**: hot-reload (Node/Bun), watch (Python/PHP), manual (Go/Rust/Java), none (static)

### Targets
Define workspace services based on recipe type:
- **Type 1 (runtime hello world)**: app + db
- **Type 2a (frontend static)**: app only (NO database)
- **Type 2b (frontend SSR)**: app + db
- **Type 3 (backend framework)**: app + db

### Submission
Submit via:
```
zerops_workflow action="complete" step="research" recipePlan={...}
```
</section>

<section name="research-showcase">
## Research — Showcase Recipe (Type 4)

Includes everything from minimal research, PLUS:

### Additional Showcase Fields
- **Cache library**: Redis client library for the framework
- **Session driver**: Redis-backed session configuration
- **Queue driver**: queue/job system (e.g., Laravel Horizon, Bull, Celery)
- **Storage driver**: object storage integration (S3-compatible)
- **Search library**: search integration (e.g., Meilisearch, Elasticsearch)
- **Mail library**: email sending (e.g., SMTP via Mailpit for dev)

### Showcase Targets
Define workspace services for showcase recipe:
- **app**: runtime service (all 6 environments)
- **worker**: background job processor (environments 0-1, 3-5)
- **db**: primary database (all 6 environments)
- **redis**: cache + sessions + queues (environments 0-1, 3-5)
- **storage**: S3-compatible object storage (environments 0-1, 3-5)
- **mailpit**: dev email testing (environments 0-1 only)
- **search**: search engine (environments 3-5 only)

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

Recipes always use **standard mode**: each runtime gets a `{name}dev` + `{name}stage` pair.

**Dev vs stage properties:**

| Property | Dev (`appdev`) | Stage (`appstage`) |
|----------|---------------|-------------------|
| `startWithoutCode` | `true` | omit |
| `maxContainers` | `1` | omit (default) |
| `enableSubdomainAccess` | `true` | `true` |
| `verticalAutoscaling.minRam` | `1.0` for compiled runtimes (Go, Rust, Java, .NET, Elixir, Gleam) | omit (default) |

**DO NOT add `zeropsSetup` or `buildFromGit` to the workspace import.** These fields require each other — `zeropsSetup` without `buildFromGit` causes API errors. The workspace deploys via `zerops_deploy` with the `setup` parameter instead.

Dev starts immediately with an empty container (RUNNING). Stage stays in READY_TO_DEPLOY until first deploy from dev.

**Static frontends (type 2a):** `run.base: static` serves via built-in Nginx — both dev and stage use `type: static`. Dev still gets `startWithoutCode: true` for the build container. The runtime for building is `nodejs@22` (or similar) as `build.base` in zerops.yaml, NOT as the service type.

**If the plan has NO database** (type 2a static frontend): the import.yaml only contains the app dev/stage pair.

**Framework secrets**: If `needsAppSecret == true`, determine during research whether the secret is used for encryption/sessions (shared by services hitting the same DB) or is per-service.
- **Shared** (e.g. Laravel APP_KEY, Django SECRET_KEY, Rails SECRET_KEY_BASE — used for encryption): do NOT add to workspace import. After services reach RUNNING, set at project level so all services inherit it:
  ```
  zerops_env project=true action=set key=APP_KEY value="$(openssl rand -base64 32)"
  ```
  The recipe deliverable uses `project.envVariables` with the preprocessor to generate at import time.
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

### zerops.yaml — Dev entry ONLY first

Write the **dev** entry only. The prod entry comes after dev is verified in the deploy step. Follow the injected **zerops.yaml Schema** for all field rules. Recipe-specific conventions:

- `setup: dev` — generic name. Deploy uses `zerops_deploy targetService="appdev" setup="dev"` to map it.
- `deployFiles: [.]` — **MANDATORY for self-deploy**
- `start: zsc noop --silent` — exception: omit `start` for implicit-webserver runtimes (php-nginx, php-apache, nginx, static)
- **NO healthCheck on dev** — agent controls lifecycle; healthCheck restarts during iteration
- `envVariables:` — ONLY cross-service references from `zerops_discover`. **Do NOT add envSecrets** (APP_KEY, SECRET_KEY_BASE) — they are already injected as OS env vars automatically.

### .env.example preservation

If the framework scaffolds a `.env.example` file (e.g., `composer create-project`), **keep it** — it documents the expected environment variable keys for local development. Remove `.env` (contains generated secrets), but preserve `.env.example` with empty values as a reference for users running locally.

### Framework environment conventions

Use the framework's **standard** environment names and values — don't invent new ones:
- **Laravel**: `APP_ENV=local` (not `development`) — `local` enables detailed error pages and stack traces
- **Node.js/Next.js**: `NODE_ENV=development`
- **Django**: `DEBUG=True`, `DJANGO_SETTINGS_MODULE=config.settings.development`
- **Rails**: `RAILS_ENV=development`

Check the framework's documentation if unsure — wrong env names cause subtle behavior differences (e.g., Laravel's `local` enables whoops error handler, `development` does not).

### Required endpoints

**Types 1, 2b, 3, 4 (server-side):**
- `GET /` — health dashboard (HTML, shows framework name + service connectivity)
- `GET /health` or `GET /api/health` — JSON health endpoint
- `GET /status` — JSON status with actual connectivity checks (DB ping, cache ping, latency)

**Type 2a (static frontend):**
- `GET /` — simple page showing framework name, greeting, timestamp, environment indicator
- No server-side health endpoint (static files only)

### App README with extract fragments

Write `README.md` at `/var/www/appdev/README.md` with three extract fragments. **Critical formatting** — match this structure exactly:

```markdown
# Recipe Name — Zerops Recipe

<!-- #ZEROPS_EXTRACT_START:intro# -->
One-line description of what this recipe demonstrates.
<!-- #ZEROPS_EXTRACT_END:intro# -->

## Integration Guide

<!-- #ZEROPS_EXTRACT_START:integration-guide# -->

### zerops.yaml

\`\`\`yaml
zerops:
  ...
\`\`\`

### 2. Step Title (if any code changes needed)
...

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
- Blank line after each start marker
- Intro has no heading inside — plain text only

### Code Quality
- Comment ratio in zerops.yaml code blocks must be >= 0.3
- No `PLACEHOLDER_*`, `<your-...>`, or `TODO` strings
- All env var references must use discovered variable names
- Comments explain WHY, not WHAT (don't restate the key name)
- Max 80 chars per comment line

### Pre-deploy checklist
- [ ] `setup: dev` (generic name), `deployFiles: [.]`, no healthCheck
- [ ] envVariables has only cross-service refs — no envSecrets re-referenced
- [ ] All env var names from `zerops_discover`, none guessed
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
- Code-level changes the agent made that are required to work on Zerops (e.g., trusted proxy middleware in `bootstrap/app.php` — without it, CSRF breaks behind the L7 balancer)
- Framework config file changes for the platform (e.g., wiring S3 credentials in Django settings, configuring Redis session driver)
- Any modification to app source that a user bringing their own app would also need to do

**What does NOT belong in integration steps:**
- Demo-specific scaffolding (custom routes, dashboard views, sample controllers) — these exist only in the recipe app, a real user wouldn't replicate them
- Config values already visible in zerops.yaml (the user can read those inline)
- Generic framework setup (how to install Laravel, what Vite does)

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
- Generic framework knowledge (how Laravel works, what Vite does)

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
- **Implicit-webserver runtimes** (php-nginx, php-apache, nginx): Skip — server auto-starts.
- **Static frontends** (type 2a): Skip — Nginx serves the built files automatically.

**Step 3: Enable dev subdomain**
```
zerops_subdomain action="enable" serviceHostname="appdev"
```

**Step 4: Verify appdev**
```
zerops_verify serviceHostname="appdev"
```
Check: service RUNNING, subdomain returns 200, health endpoint responds (or page loads for static).

**Step 5: Iterate if needed** (max 3 iterations)
If verification fails: check logs (`zerops_logs serviceHostname="appdev"`), fix code on mount, kill previous server, restart via SSH, re-verify.

### Stage deployment flow

**Step 6: Generate prod entry in zerops.yaml**
Add a `setup: prod` entry to zerops.yaml on the appdev mount. Prod differences from dev:
- Real `start` command (not `zsc noop`). For static: still no `start` (Nginx serves).
- Real `buildCommands` with compilation/bundling
- Real `deployFiles` (build output, not `.`) — **verify completeness**: list ALL dirs/files your start command and framework need at runtime. Common misses: `app/` (Laravel), `src/` (many frameworks), `storage/` (Laravel), lock files. When cherry-picking (not using `.`), run `ls` to see what exists and cross-reference with your start command and framework requirements.
- Add `healthCheck` (httpGet on app port) — **required for prod** (restarts unresponsive containers). Omit only on dev and static.
- Add `deploy.readinessCheck` if app has `initCommands` (migrations)
- Copy `envVariables` from dev entry (if any), adjust APP_ENV/APP_DEBUG for production
- Use runtime knowledge Prod patterns as reference

**Step 7: Deploy appstage from appdev (cross-deploy)**
```
zerops_deploy sourceService="appdev" targetService="appstage" setup="prod"
```
The `setup="prod"` maps hostname `appstage` to `setup: prod` in zerops.yaml. Stage builds from dev's source code with the prod config. Server auto-starts via the real `start` command (or Nginx for static).

**Step 7b: Connect shared storage** (if applicable)
After stage transitions from READY_TO_DEPLOY to ACTIVE, connect storage:
```
zerops_manage action="connect-storage" serviceHostname="appstage" storageHostname="storage"
```

**Step 8: Enable stage subdomain**
```
zerops_subdomain action="enable" serviceHostname="appstage"
```

**Step 9: Verify appstage**
```
zerops_verify serviceHostname="appstage"
```

**Step 10: Present both URLs**

### Common deployment issues

| Issue | Diagnosis | Fix |
|-------|-----------|-----|
| HTTP 502 | App not binding 0.0.0.0 or wrong port | Check runtime knowledge for bind rules |
| Empty env vars | Deploy hasn't happened yet | Deploy first — envVariables activate at deploy time. envSecrets need restart. |
| Build fails | Wrong build commands, missing dependencies | Check `zerops_logs`, fix and redeploy |
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

Generate the complete recipe repository structure with 6 environment tiers. These files go to the **recipe output directory** (shown in `outputDir` field), NOT to the mounted service filesystem.

**Output directory**: `{outputDir}` — e.g., `/var/www/zcprecipator/laravel-minimal/`. Create the directory if it doesn't exist.

### Required Files (13+ total)
For each environment (0-5):
- `{outputDir}/{env_folder}/import.yaml` — service import configuration
- `{outputDir}/{env_folder}/README.md` — environment-specific documentation

Plus:
- `{outputDir}/README.md` — main recipe README

### Environment Folders
- `0 — AI Agent` — ZCP/AI-driven development
- `1 — Remote (CDE)` — cloud development environment
- `2 — Local` — local development with Zerops
- `3 — Stage` — staging environment
- `4 — Small Production` — small production (minContainers: 2)
- `5 — Highly-available Production` — HA production (dedicated CPU, HA mode)

### Scaling Matrix
| Property | Env 0-1 | Env 2 | Env 3 | Env 4 | Env 5 |
|----------|---------|-------|-------|-------|-------|
| App setups | dev + prod | prod | prod | prod | prod |
| DB mode | NON_HA | NON_HA | NON_HA | NON_HA | HA |
| minContainers | — | — | — | 2 | 2 |
| cpuMode | — | — | — | — | DEDICATED |
| corePackage | — | — | — | — | SERIOUS |
| minFreeRamGB | — | — | 0.25 | 0.125 | 0.25 |
| enableSubdomainAccess | yes | yes | yes | yes | yes |

**If plan has no database** (type 2a): skip DB rows in scaling matrix. Environment import.yaml files contain only the app service.

### import.yaml Rules

Follow the injected **import.yaml Schema** for all platform rules (priority, mode, hostname conventions, preprocessor). Recipe-specific rules:

- Every runtime service uses `buildFromGit` — **NO `startWithoutCode`** (workspace-only, never in finalize)
- `zeropsSetup: prod` + `buildFromGit` on prod/stage services; `zeropsSetup: dev` + `buildFromGit` on dev services (env 0-1)
- `buildFromGit: https://github.com/zerops-recipe-apps/{slug}-app`
- Env 0-1 hostnames: `appdev`/`appstage` (suffixed). Env 2+ uses bare hostname: `app`, `worker`.
- `corePackage: SERIOUS` at **project level** for env 5 (NOT under verticalAutoscaling)
- Comment line width <= 80 chars, comment ratio >= 0.3 per file
- No `PLACEHOLDER_*` strings, no cross-environment references in comments
- Project names: `{slug}-{env-suffix}` convention

### Completion
```
zerops_workflow action="complete" step="finalize" attestation="All 13+ recipe files generated and validated"
```
</section>

<section name="close">
## Close — Verify & Publish

Recipe creation is complete. Before publishing, dispatch a verification sub-agent to review the recipe from the perspective of a framework expert.

### 1. Verification Sub-Agent (mandatory)

Spawn a sub-agent to perform a final review of the entire recipe. The sub-agent should act as a **{framework} expert** who has never seen this recipe before, reviewing it for correctness, completeness, and usability.

**Sub-agent prompt template:**

> You are a {framework} expert reviewing a Zerops recipe. Read ALL files in {outputDir}/ and {appDir}/ and verify:
>
> **App code review:**
> - Does the app actually work? Check routes, views, config, migrations.
> - Are framework conventions followed? (e.g., Laravel: APP_ENV should be `local` not `development`, trusted proxies wired in code AND env)
> - Is there dead code, unused dependencies, or missing files? (e.g., Tailwind plugin in vite.config.js but no Tailwind classes used)
> - Does `.env.example` exist with the right keys?
>
> **zerops.yaml review:**
> - Do `setup: dev` and `setup: prod` entries have correct build/deploy/run config?
> - Does `setup: prod` have `healthCheck` (httpGet on the health endpoint)? Missing healthCheck means unhealthy containers are never restarted.
> - Does `setup: prod` have `deploy.readinessCheck`? Missing readinessCheck means broken builds get traffic.
> - Are deployFiles complete for prod? (common miss: `composer.lock`, `.env.example`)
> - Are env vars correct for the framework? (e.g., Laravel SESSION_DRIVER, CACHE_STORE)
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

### 2. Export & Publish

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
