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
Hello-world recipes exist per RUNTIME, not per framework. The hello-world IS the runtime guide — it contains the proven zerops.yaml patterns, build lifecycle, env var wiring, and platform-specific comments for that runtime. Load it:
```
zerops_knowledge recipe="{runtime-base}-hello-world"
```
Example: for Laravel (php-nginx runtime), load `php-hello-world`. For Next.js (nodejs runtime), load `nodejs-hello-world`. For React static, load `nodejs-hello-world` (build base reference).

Your job is to extend this base with framework-specific knowledge: `documentRoot` for PHP frameworks, multi-base builds for asset pipelines (Vite/Webpack), trusted proxy config, framework CLI commands, etc. These discoveries go into the zerops.yaml comments and the knowledge-base README fragment.

Load the import.yaml schema for type validation:
```
zerops_knowledge scope="infrastructure"
```

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

Dev starts immediately with an empty container (RUNNING). Stage stays in READY_TO_DEPLOY until first deploy from dev.

**Static frontends (type 2a):** `run.base: static` serves via built-in Nginx — both dev and stage use `type: static`. Dev still gets `startWithoutCode: true` for the build container. The runtime for building is `nodejs@22` (or similar) as `build.base` in zerops.yaml, NOT as the service type.

**Managed service conventions:**
- Hostname: `db` (postgresql/mariadb), `cache` (valkey), `queue` (nats), `search` (meilisearch), `storage` (object-storage)
- `priority: 10` for all managed services (start before app)
- `mode: NON_HA` for workspace
- `object-storage` requires `objectStorageSize` field

**If the plan has NO database** (type 2a static frontend): the import.yaml only contains the app dev/stage pair. Skip managed service conventions.

**Framework secrets**: If `needsAppSecret == true`, add per-service `envSecrets` with `<@generateRandomString(<32>)>` on each app/worker service and add `#zeropsPreprocessor=on` as the first line. Each service gets its own generated key — do NOT put envSecrets at project level.

**Validation checklist:**

| Check | What to verify |
|-------|---------------|
| Hostnames | [a-z0-9] pattern, max 25 chars |
| Service types | Match available stacks from research |
| Mode present | Managed services have `mode: NON_HA` |
| Priority | Data services: `priority: 10` |
| Preprocessor | `#zeropsPreprocessor=on` if using `<@...>` functions |
| envSecrets | Per-service on app/worker, NOT at project level |

### 2. Import services

```
zerops_import content="..."
```

Wait for all services to reach RUNNING.

### 3. Mount dev filesystem

Mount the dev service for direct file access:
```
zerops_mount serviceHostname="appdev"
```

This gives SSHFS access to `/var/www/appdev/` — all code writes go here.

> **Two kinds of "mount" (disambiguation):** (1) `zerops_mount` — SSHFS tool, mounts service `/var/www` locally for development. (2) Shared storage mount — platform feature, attaches a shared-storage volume at `/mnt/{hostname}`. These are completely unrelated.

### 4. Discover env vars (mandatory before generate — skip if no managed services)

After services reach RUNNING, discover actual env vars:
```
zerops_discover includeEnvs=true
```

**If the plan has no managed services** (type 2a static frontend): skip this step — there are no env vars to discover.

Record which env vars exist. Common patterns:

| Service type | Available env vars |
|-------------|-------------------|
| PostgreSQL | `${db_connectionString}`, `${db_host}`, `${db_port}`, `${db_user}`, `${db_password}`, `${db_dbName}` |
| MariaDB | `${db_connectionString}`, `${db_host}`, `${db_port}`, `${db_user}`, `${db_password}`, `${db_dbName}` |
| Valkey | `${cache_host}`, `${cache_port}` (no password — private network) |
| Object Storage | `${storage_apiUrl}`, `${storage_accessKeyId}`, `${storage_secretAccessKey}`, `${storage_bucketName}` |

**ONLY use variables that were actually discovered.** Guessing variable names causes runtime failures.

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

### zerops.yaml — Setup naming convention

zerops.yaml uses **generic setup names**: `setup: dev` and `setup: prod` (never hostname-specific like `appdev`/`appstage`). The import.yaml `zeropsSetup` field maps hostnames to setups:
- `hostname: appdev` → `zeropsSetup: dev`
- `hostname: appstage` → `zeropsSetup: prod`
- `hostname: app` (env 2-5) → `zeropsSetup: prod`

### zerops.yaml — Dev entry ONLY first

Write the **dev** entry only. The stage entry comes after dev is verified in the deploy step.

**Dev setup rules (CRITICAL):**
- `setup: dev` — generic name, mapped via `zeropsSetup` in import.yaml
- `deployFiles: [.]` — **MANDATORY for self-deploy, no exceptions**
- `start: zsc noop --silent` — agent controls server manually
  - **Exception**: omit `start` entirely for implicit-webserver runtimes (php-nginx, php-apache, nginx, static)
  - **Static frontends (type 2a)**: omit `start` — Nginx auto-serves from documentRoot
- **NO buildCommands with compilation** — dev only does dependency installation (npm install, composer install, etc.)
  - **Static frontends**: dev may need `npm run build` since the output needs to exist for Nginx to serve
- **NO healthCheck** — agent controls lifecycle; healthCheck would restart container during iteration
- `envVariables:` — map discovered vars to what the app expects (skip if no managed services):
  ```yaml
  envVariables:
    DATABASE_URL: ${db_connectionString}
    REDIS_HOST: ${cache_host}
    # ONLY variables from zerops_discover — never guess
  ```

**Static frontends:** Set `documentRoot: dist` (or `build`, `.output/public`, etc.) matching the build output. No `start` command needed.

### .env.example preservation

If the framework scaffolds a `.env.example` file (e.g., `composer create-project`), **keep it** — it documents the expected environment variable keys for local development. Remove `.env` (contains generated secrets), but preserve `.env.example` with empty values as a reference for users running locally.

### Required endpoints

**Types 1, 2b, 3, 4 (server-side):**
- `GET /` — health dashboard (HTML, shows framework name + service connectivity)
- `GET /health` or `GET /api/health` — JSON health endpoint
- `GET /status` — JSON status with actual connectivity checks (DB ping, cache ping, latency)

**Type 2a (static frontend):**
- `GET /` — simple page showing framework name, greeting, timestamp, environment indicator
- No server-side health endpoint (static files only)

### App README with extract fragments

Write `README.md` at `/var/www/appdev/README.md` with three documentation fragments:
- `<!-- #ZEROPS_EXTRACT_START:integration-guide# -->` — complete zerops.yaml with comments
- `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->` — platform knowledge with Gotchas section
- `<!-- #ZEROPS_EXTRACT_START:intro# -->` — 1-3 line introduction (no titles, no images)

### Code Quality
- Comment ratio in zerops.yaml code blocks must be >= 0.3
- No `PLACEHOLDER_*`, `<your-...>`, or `TODO` strings
- All env var references must use discovered variable names
- Comments explain WHY, not WHAT (don't restate the key name)
- Max 80 chars per comment line

### Pre-deploy checklist
Before completing generate:
- [ ] zerops.yaml has `setup: dev` (generic name, not hostname-specific)
- [ ] `deployFiles: [.]` on dev
- [ ] `start: zsc noop --silent` (or omitted for implicit-webserver/static)
- [ ] No compilation in buildCommands (dependency install only — exception: static frontend build)
- [ ] No healthCheck on dev entry
- [ ] All env vars from discovery, none guessed (skip if no managed services)
- [ ] README has all 3 extract fragments with proper markers
- [ ] `.env.example` preserved if framework-scaffolded (`.env` removed)

### Completion
```
zerops_workflow action="complete" step="generate" attestation="App code and zerops.yaml written to /var/www/appdev/. README with 3 fragments."
```
</section>

<section name="generate-fragments">
## Fragment Quality Requirements

### integration-guide Fragment
Must contain:
- Complete zerops.yaml with ALL setups (`prod`, `dev`; `worker` if showcase)
- Setup names are generic (`prod`/`dev`), NOT hostname-specific
- Every config line should have an inline comment explaining WHY
- Build commands must be ordered correctly
- Deploy files must differ between dev (`.`) and prod (build output)

### knowledge-base Fragment

Each item must be **irreducible** — not learnable from the zerops.yaml comments, platform docs, or general framework docs. Do NOT repeat what's already documented in the integration-guide zerops.yaml comments.

Must contain:
- `### Gotchas` section with at least 2 framework-specific pitfalls on Zerops
- Only things the zerops.yaml comments DON'T cover: code-level changes needed (e.g., trusted proxy middleware config in app code), base image contents, runtime-specific cache paths

Do NOT include:
- Config values already visible in zerops.yaml (e.g., don't re-explain `TRUSTED_PROXIES` value — explain the code-side change needed instead)
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
zerops_deploy serviceHostname="appdev"
```
This triggers a build from files already on the mount. Blocks until complete.

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
Add a `setup: prod` entry to zerops.yaml on the appdev mount. The `zeropsSetup: prod` in import.yaml maps `appstage` hostname to this setup. Prod differences from dev:
- Real `start` command (not `zsc noop`). For static: still no `start` (Nginx serves).
- Real `buildCommands` with compilation/bundling
- Real `deployFiles` (build output, not `.`)
- Add `healthCheck` (httpGet on app port) — for server-side apps only, not static
- Add `deploy.readinessCheck` if app has `initCommands` (migrations)
- Copy `envVariables` from dev entry (if any)
- Use runtime knowledge Prod patterns as reference

**Step 7: Deploy appstage from appdev (cross-deploy)**
```
zerops_deploy serviceHostname="appstage" sourceServiceHostname="appdev"
```
Stage builds from dev's source code with the stage zerops.yaml entry. Server auto-starts via the real `start` command (or Nginx for static).

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
| HTTP 502 | App not listening on 0.0.0.0, or wrong port | Fix bind address in app config |
| Empty env vars | Deploy hasn't happened yet | Deploy first — env vars activate at deploy time |
| Build fails | Wrong build commands, missing dependencies | Check `zerops_logs`, fix and redeploy |
| Stage deploy fails | zerops.yaml setup name doesn't match zeropsSetup | Ensure `setup: prod` matches `zeropsSetup: prod` in import.yaml |
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
- `priority: 10` on all data services (ensures they start before app)
- `envSecrets` per-service on app/worker services (NOT at project level) where `needsAppSecret == true`
- `#zeropsPreprocessor=on` when using `<@generateRandomString(<32>)>`
- `corePackage: SERIOUS` at **project level** for env 5 (NOT under verticalAutoscaling)
- `verticalAutoscaling` nesting: minFreeRamGB, cpuMode under it
- `zeropsSetup: prod` on app/worker services (maps to `setup: prod` in zerops.yaml)
- `zeropsSetup: dev` on dev services in env 0-1 (maps to `setup: dev` in zerops.yaml)
- `buildFromGit: https://github.com/zerops-recipe-apps/{slug}-app` on all non-startWithoutCode app/worker services
- `startWithoutCode: true` + `maxContainers: 1` on dev services in env 0-1
- Comment line width <= 80 chars
- Comment ratio >= 0.3 per file
- No `PLACEHOLDER_*` strings
- No cross-environment references in comments
- Project names: `{slug}-{env-suffix}` convention

### Completion
```
zerops_workflow action="complete" step="finalize" attestation="All 13+ recipe files generated and validated"
```
</section>

<section name="close">
## Close — Publish

Recipe creation is complete. Finalize and publish.

### Publishing Steps
1. Push recipe to GitHub:
   ```
   zcp sync push recipes {slug}
   ```
2. After PR is merged, clear Strapi cache:
   ```
   zcp sync cache-clear {slug}
   ```
3. Pull merged version:
   ```
   zcp sync pull recipes {slug}
   ```

### Testing
Run the recipe through eval to verify quality:
```
zcp eval run --recipe {slug}
```

### Completion
```
zerops_workflow action="complete" step="close" attestation="Recipe published and tested"
```

Or skip if not publishing now:
```
zerops_workflow action="skip" step="close" reason="Will publish later"
```
</section>
