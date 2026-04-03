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

Create all services defined in the recipe plan using import.yaml.

### Steps
1. Build an import.yaml with all target services from the recipe plan
2. Import via `zerops_import`
3. Verify all services exist via `zerops_discover`
4. Record discovered env vars for managed dependencies

### Import Template
```yaml
project:
  name: {slug}-workspace
services:
  - hostname: {hostname}
    type: {type}
    # Add mode, ports, env vars as needed
```

### Completion
```
zerops_workflow action="complete" step="provision" attestation="All services created and verified: {list}"
```
</section>

<section name="generate">
## Generate — App Code & Configuration

Generate the application code, zerops.yaml, and README with documentation fragments.

### zerops.yaml Requirements
- **base** setup: shared configuration (env vars, build steps)
- **prod** setup: production optimizations (caching, compiled assets)
- **dev** setup: development mode (hot-reload, debug, source deploy)
- Showcase: additional **worker** setup if applicable

### App README Requirements
The app README.md must include documentation fragments marked with extract tags:
- `<!-- #ZEROPS_EXTRACT_START:integration-guide# -->` — complete zerops.yaml with comments
- `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->` — platform knowledge with Gotchas section
- `<!-- #ZEROPS_EXTRACT_START:intro# -->` — 1-3 line introduction (no titles, no images)

### Code Quality
- Comment ratio in zerops.yaml code blocks must be >= 0.3
- No `PLACEHOLDER_*`, `<your-...>`, or `TODO` strings
- All env var references must use discovered variable names

### Completion
```
zerops_workflow action="complete" step="generate" attestation="App code generated with zerops.yaml and README fragments"
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
## Deploy — Build & Verify

Deploy the application to all runtime services and verify health.

### Steps
1. Deploy to each runtime service via `zerops_deploy`
2. Enable subdomain access via `zerops_subdomain`
3. Verify deployment health via `zerops_verify`
4. Check logs for errors via `zerops_logs`

### Health Criteria
- All runtime services in RUNNING status
- HTTP health check passes on subdomain URL
- No error-level logs in the last 5 minutes
- For showcase: worker process running, queue connection established

### Completion
```
zerops_workflow action="complete" step="deploy" attestation="All services deployed and healthy: {urls}"
```
</section>

<section name="finalize">
## Finalize — Recipe Repository Files

Generate the complete recipe repository structure with 6 environment tiers.

### Required Files (13+ total)
For each environment (0-5):
- `{env_folder}/import.yaml` — service import configuration
- `{env_folder}/README.md` — environment-specific documentation

Plus:
- `README.md` — main recipe README

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
