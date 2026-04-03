# Recipe Workflow

Create a Zerops recipe: a deployable reference implementation with 6 environment tiers and structured documentation.

<section name="research-minimal">
## Research — Minimal Recipe (Type 3)

Fill in all research fields by examining the framework's documentation and existing recipes.

### Reference Loading
Hello-world recipes exist per RUNTIME, not per framework. Load the runtime's recipe:
```
zerops_knowledge recipe="{runtime-base}-hello-world"
```
Example: for Laravel (php-nginx runtime), load `php-hello-world`, NOT `laravel-hello-world`.

Load the runtime briefing for platform-specific rules:
```
zerops_knowledge runtime="{runtime-base}"
```
Example: `zerops_knowledge runtime="php-nginx"` — returns PHP deployment patterns, build lifecycle, env var conventions.

Load the import.yaml schema for type validation:
```
zerops_knowledge scope="infrastructure"
```

### Framework Identity
- **Service type** (from available stacks): match against live catalog
- **Package manager**: npm, yarn, pnpm, bun, composer, pip, cargo, go mod
- **HTTP port**: the port the framework listens on by default

### Build & Deploy Pipeline
- **Build commands**: ordered list (e.g., `npm install`, `npm run build`)
- **Deploy files**: what to deploy (`.` for dev, build output dir for prod)
- **Start command**: the RUN command (not build). Leave empty for implicit webserver types (php-nginx, php-apache, nginx, static) where the server auto-starts.
- **Cache strategy**: directories to cache between builds (e.g., `node_modules`, `vendor`)

### Database & Migration
- **DB driver**: mysql, postgresql, sqlite, mongodb, none
- **Migration command**: framework-specific (e.g., `php artisan migrate`)
- **Seed command**: optional data seeding

### Environment & Secrets
- **Needs app secret**: does the framework require an APP_KEY/SECRET_KEY?
- **Logging driver**: stderr (preferred), file, syslog

### Decision Tree Resolution
Resolve these 4 decisions (ZCP provides defaults, you may override with justification):
1. **Web server**: builtin (Node/Go/Rust), nginx-sidecar (PHP), nginx-proxy (static)
2. **Build base**: primary runtime; add nodejs to buildBases if Vite/Webpack needed
3. **OS**: ubuntu-22 (default), alpine (Go/Rust static binaries)
4. **Dev tooling**: hot-reload (Node/Bun), watch (Python/PHP), manual (Go/Rust/Java)

### Targets
Define workspace services for minimal recipe:
- **app**: the runtime service (all 6 environments)
- **db**: database service if needed (all 6 environments)

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

**Managed service conventions:**
- Hostname: `db` (postgresql/mariadb), `cache` (valkey), `queue` (nats), `search` (meilisearch), `storage` (object-storage)
- `priority: 10` for all managed services (start before app)
- `mode: NON_HA` for workspace
- `object-storage` requires `objectStorageSize` field

**Shared storage mount** (if shared-storage in plan): Add `mount: [{storage-hostname}]` to both dev and stage in import.yaml. This pre-configures the connection but does NOT activate runtime mount. You MUST also add `mount: [{storage-hostname}]` in zerops.yaml `run:` section.

**Framework secrets**: If `needsAppSecret == true`, add `envSecrets` with `<@generateRandomString(<32>)>` and add `#yamlPreprocessor=on` as the first line.

**Validation checklist:**

| Check | What to verify |
|-------|---------------|
| Hostnames | [a-z0-9] pattern, max 25 chars |
| Service types | Match available stacks from research |
| Mode present | Managed services have `mode: NON_HA` |
| Priority | Data services: `priority: 10` |
| Preprocessor | `#yamlPreprocessor=on` if using `<@...>` functions |

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

### 4. Discover env vars (mandatory before generate)

After services reach RUNNING, discover actual env vars:
```
zerops_discover includeEnvs=true
```

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

### zerops.yaml — Dev entry ONLY first

Write the **dev** entry only. The stage entry comes after dev is verified in the deploy step.

**Dev setup rules (CRITICAL):**
- `setup: appdev` — must match the dev hostname
- `deployFiles: [.]` — **MANDATORY for self-deploy, no exceptions**
- `start: zsc noop --silent` — agent controls server manually (exception: omit `start` entirely for implicit-webserver runtimes: php-nginx, php-apache, nginx, static)
- **NO buildCommands with compilation** — dev only does dependency installation (npm install, composer install, etc.)
- **NO healthCheck** — agent controls lifecycle; healthCheck would restart container during iteration
- `envVariables:` — map discovered vars to what the app expects:
  ```yaml
  envVariables:
    DATABASE_URL: ${db_connectionString}
    REDIS_HOST: ${cache_host}
    # ONLY variables from zerops_discover — never guess
  ```

**Base setup** (shared between dev and prod):
- Common env vars shared across both
- Use `extends: base` pattern if the runtime supports it

### Required endpoints

The app must expose:
- `GET /` — health dashboard (HTML, shows framework name + service connectivity)
- `GET /health` or `GET /api/health` — JSON health endpoint
- `GET /status` — JSON status with actual connectivity checks (DB ping, cache ping, latency)

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
- [ ] zerops.yaml has `setup: appdev` matching hostname
- [ ] `deployFiles: [.]` on dev
- [ ] `start: zsc noop --silent` (or omitted for implicit-webserver)
- [ ] No compilation in buildCommands (dependency install only for dev)
- [ ] No healthCheck on dev entry
- [ ] All env vars from discovery, none guessed
- [ ] README has all 3 extract fragments with proper markers

### Completion
```
zerops_workflow action="complete" step="generate" attestation="App code and zerops.yaml written to /var/www/appdev/. README with 3 fragments."
```
</section>

<section name="generate-fragments">
## Fragment Quality Requirements

### integration-guide Fragment
Must contain:
- Complete zerops.yaml with ALL setups (base, prod, dev, worker if showcase)
- Every config line should have an inline comment explaining WHY
- Build commands must be ordered correctly
- Deploy files must differ between dev (`.`) and prod (build output)

### knowledge-base Fragment
Must contain:
- `### Gotchas` section with at least 2 framework-specific pitfalls on Zerops
- Common deployment issues and solutions
- Environment variable conventions

### intro Fragment
- 1-3 lines only
- No markdown titles (no `#`)
- No deploy buttons or badges
- No images
- Plain text describing what the recipe demonstrates

### Comment Conventions
- YAML comments: `# Explanation` on the line above or inline
- Comment ratio: at least 30% of config lines should have comments
- Comments explain WHY, not WHAT (don't restate the key name)
- Max 80 chars per comment line
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

**Step 2: Start the dev server** (skip for implicit-webserver: php-nginx, php-apache, nginx, static)

After deploy, env vars are OS env vars. Start the server via SSH:
```bash
ssh appdev "cd /var/www && {start_command} &"
```
Example: `ssh appdev "cd /var/www && node index.js &"`

**Step 3: Enable dev subdomain**
```
zerops_subdomain action="enable" serviceHostname="appdev"
```

**Step 4: Verify appdev**
```
zerops_verify serviceHostname="appdev"
```
Check: service RUNNING, subdomain returns 200, health endpoint responds.

**Step 5: Iterate if needed** (max 3 iterations)
If verification fails: check logs (`zerops_logs serviceHostname="appdev"`), fix code on mount, kill previous server, restart via SSH, re-verify.

### Stage deployment flow

**Step 6: Generate stage entry in zerops.yaml**
Add a `setup: appstage` entry to zerops.yaml on the appdev mount. Stage differences from dev:
- Real `start` command (not `zsc noop`)
- Real `buildCommands` with compilation/bundling
- Real `deployFiles` (build output, not `.`)
- Add `healthCheck` (httpGet on app port)
- Add `deploy.readinessCheck` if app has `initCommands` (migrations)
- Copy `envVariables` from dev entry
- Use runtime knowledge Prod patterns as reference

**Step 7: Deploy appstage from appdev (cross-deploy)**
```
zerops_deploy serviceHostname="appstage" sourceServiceHostname="appdev"
```
Stage builds from dev's source code with the stage zerops.yaml entry. Server auto-starts via the real `start` command.

**Step 7b: Connect shared storage** (if applicable)
After stage transitions from READY_TO_DEPLOY to ACTIVE, connect storage:
```
zerops_manage action="connect-storage" serviceHostname="appstage" storageHostname="storage"
```
Import `mount:` only applies to ACTIVE services — stage was READY_TO_DEPLOY during import.

**Step 8: Enable stage subdomain**
```
zerops_subdomain action="enable" serviceHostname="appstage"
```

**Step 9: Verify appstage**
```
zerops_verify serviceHostname="appstage"
```
Check: service RUNNING, subdomain returns 200, health endpoint responds with real data connections.

**Step 10: Present both URLs**

### Common deployment issues

| Issue | Diagnosis | Fix |
|-------|-----------|-----|
| HTTP 502 | App not listening on 0.0.0.0, or wrong port | Fix bind address in app config |
| Empty env vars | Deploy hasn't happened yet | Deploy first — env vars activate at deploy time |
| Build fails | Wrong build commands, missing dependencies | Check `zerops_logs`, fix and redeploy |
| Stage deploy fails | zerops.yaml setup name doesn't match hostname | Ensure `setup: appstage` matches |
| Health check fails | healthCheck configured on dev entry | Remove healthCheck from dev; agent controls lifecycle |

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

### import.yaml Rules
- `priority: 10` on all data services (ensures they start before app)
- `envSecrets` where `needsAppSecret == true`
- `# zeropsPreprocessor=on` when using `<@generateRandomString>`
- `verticalAutoscaling` nesting: minRam, minFreeRamGB, cpuMode under it
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
