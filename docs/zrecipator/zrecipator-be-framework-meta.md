# AI Meta-Prompt: Backend Framework Recipe Generators

**This guide is for AI agents generating framework-specific recipe prompts**, not for direct recipe implementation. You are Opus-level intelligence producing implementation guides that other AI agents will use to create production-quality Zerops recipes.

---

## Architectural Gates

1. **Research** — framework ecosystem, build pipeline, idiomatic libraries
2. **Bootstrap Imports** — generate import.yaml files to stand up working environments
3. **Minimal Prompt** — generate `zrecipator-{framework}-minimal.md`
4. **Showcase Prompt** — generate `zrecipator-{framework}-showcase.md`
5. **Verification** — validate all outputs against gate checklist

---

## Mission

Given a backend framework name (e.g., "Laravel", "NestJS", "Django", "Rails", "Spring Boot", "Phoenix"), produce **two complete AI implementation guides** and **two bootstrap import files**:

1. **`zrecipator-{framework}-minimal.md`** — Minimal recipe prompt: framework + PostgreSQL, health check dashboard confirming DB connection
2. **`zrecipator-{framework}-showcase.md`** — Showcase recipe prompt: framework + PostgreSQL + Valkey + Object Storage + Mailpit + Meilisearch, dashboard confirming ALL connections
3. **`bootstrap-{framework}-minimal.yaml`** — Import file to create the Zerops project where an agent will execute the minimal prompt
4. **`bootstrap-{framework}-showcase.yaml`** — Import file to create the Zerops project where an agent will execute the showcase prompt

**Workflow**: User runs a bootstrap import → Zerops creates a project with all services → user gives the agent the corresponding prompt → agent works inside the project (SSH into appdev, writes code, deploys, creates recipe files).

**Success criteria**: An AI agent reading either output prompt can produce a complete, deployable recipe without needing any other document. Every Zerops platform rule, every framework-specific detail, every template — everything is in the prompt.

---

## Research Phase

Before writing any output, you MUST research and determine the following for the target framework. Use web search, documentation, and your training knowledge. **Every answer must be precise and current** — wrong build commands or library names will produce broken recipes.

### Step 0: Read the Runtime Hello World Recipe

**Before anything else**, fetch the existing runtime hello-world recipe for the framework's language:

```
https://raw.githubusercontent.com/zeropsio/recipes/refs/heads/main/{lang}-hello-world/0%20%E2%80%94%20AI%20Agent/import.yaml
```

Where `{lang}` is the language slug (e.g., `php`, `python`, `nodejs`, `go`, `rust`, `dotnet`, `bun`, `deno`).

Also read the recipe app's `zerops.yaml`:

```
https://raw.githubusercontent.com/zerops-recipe-apps/{lang}-hello-world-app/refs/heads/main/zerops.yaml
```

These are **proven, deployed configurations**. They show exactly how this language runs on Zerops — service type, build commands, deploy files, cache strategy, OS choice, env vars, health checks, migrations. The framework prompt must build on top of these patterns, not contradict them. If the runtime recipe uses `cache: true` for Go, the framework recipe must too. If it uses `os: ubuntu` for dev, so must yours.

### Framework Identity

| Question | Example (Laravel) | Your Answer |
| ----- | ----- | ----- |
| Zerops service type (`run.base`) | `php-nginx@8.4` | |
| Zerops build base(s) | `[php@8.4, nodejs@22]` | |
| Package manager + lockfile | Composer (`composer.lock`) | |
| Frontend package manager (if integrated) | npm (`package-lock.json`) | |
| Language version constraint | PHP ^8.2 | |
| Default HTTP port | 80 (Nginx) | |
| Needs web server (Nginx/Apache)? | Yes (php-nginx type) | |
| Custom web server config? | Yes (`siteConfigPath: site.conf.tmpl`) | |
| Process manager (if not PHP) | N/A (PHP-FPM built into php-nginx) | |
| Must bind to `0.0.0.0`? | N/A (Nginx handles) | |
| OS preference (build) | Ubuntu | |
| OS preference (run) | Ubuntu | |

### Build & Deploy Pipeline

| Question | Example (Laravel) | Your Answer |
| ----- | ----- | ----- |
| Production build commands | `composer install --optimize-autoloader --no-dev && npm install && npm run build` | |
| Production deploy files | `./` (full source — PHP is interpreted) | |
| Production start command | (none — PHP-FPM managed by Nginx) | |
| Dev build base | `ubuntu@latest` | |
| Dev build commands | (none — source only) | |
| Dev deploy files | `./` | |
| Dev start command | `zsc noop --silent` | |
| Build cache paths | `[vendor, composer.lock, node_modules, package-lock.json]` | |
| Framework has integrated frontend? | Yes (Inertia + Vue/React) | |
| Frontend build step needed? | Yes (`npm run build`) | |

### Database & Migrations

| Question | Example (Laravel) | Your Answer |
| ----- | ----- | ----- |
| Database driver/connection config | `DB_CONNECTION=pgsql` | |
| Migration command (prod) | `php artisan migrate --isolated --force` | |
| Migration command (dev) | `php artisan migrate --force` | |
| Needs `zsc execOnce`? | Yes — always use as defense-in-depth | |
| Idempotent by default? | Yes (tracks in `migrations` table) | |
| Seeding command (if needed) | `php artisan db:seed --force` | |
| Migration creates its own tracking table? | Yes | |

### Environment & Secrets

| Question | Example (Laravel) | Your Answer |
| ----- | ----- | ----- |
| App secret/key env var | `APP_KEY` | |
| Secret generation method | `<@generateRandomString(<32>)>` via preprocessor | |
| How env vars are loaded | `.env` file or system env vars | |
| Trusted proxy config | `TRUSTED_PROXIES=*` (for Zerops L7 balancer) | |
| Logging for multi-container | `LOG_CHANNEL=syslog` | |
| Maintenance mode in multi-container | `APP_MAINTENANCE_DRIVER=cache` | |

### Dev Environment Specifics

| Question | Example (Laravel) | Your Answer |
| ----- | ----- | ----- |
| Extra runtime tools needed for dev? | Node.js (for Inertia frontend) | |
| How to install extra tools | `run.prepareCommands` with `zsc install nodejs@22` | |
| Dev init commands (beyond migration) | `composer install && npm install` | |
| Needs temporary RAM scaling? | Yes (`zsc scale ram +0.5GB 10m`) | |

### Showcase: Idiomatic Libraries

For each service category, determine the framework's **most popular, production-proven** library. Not experimental, not niche — the one that 80%+ of production apps use.

| Service | Example (Laravel) | Your Answer |
| ----- | ----- | ----- |
| **Cache driver/library** | Built-in (`CACHE_STORE=redis`, uses `phpredis`) | |
| **Session driver** | Built-in (`SESSION_DRIVER=redis`) | |
| **Queue driver/library** | Built-in (`QUEUE_CONNECTION=redis`) | |
| **Queue worker command** | `php artisan queue:work` | |
| **Object storage (S3) library** | `league/flysystem-aws-s3-v3` | |
| **Storage config** | `FILESYSTEM_DISK=s3` + AWS_* env vars | |
| **Search library** | Laravel Scout + Meilisearch driver | |
| **Search config** | `SCOUT_DRIVER=meilisearch` + `MEILISEARCH_HOST` | |
| **Mail library** | Built-in (`MAIL_MAILER=smtp`) | |
| **Mail config** | `MAIL_HOST=mailpit`, `MAIL_PORT=1025` | |

---

## Bootstrap Import Files

The bootstrap imports create Zerops projects where the executing agent will work. They are NOT the recipe's 6 environment configs — they're the agent's workspace.

### Structure

Each bootstrap import creates:
- `appdev` — dev service (agent SSHs in, writes code)
- `appstage` — prod service (agent deploys to test the build pipeline)
- Data services — whatever the tier requires

**No `buildFromGit`** — the app doesn't exist yet. The agent will create it and push via `zcli push`.

**Dev services need `startWithoutCode: true`** — without `buildFromGit`, Zerops won't start a service unless explicitly told it can run without deployed code. Add this to every dev service (`appdev`, `workerdev`).

**Only staging/prod services need `enableSubdomainAccess`** — dev services are accessed via SSH (`zcli service ssh`), not HTTP. Only `appstage` (and `mailpit`) need subdomain access for browser testing.

### Bootstrap ≠ Recipe Environment Import

Bootstrap imports and recipe environment imports (env 0-5) look superficially similar but serve completely different purposes. Do NOT confuse them.

| Property | Bootstrap Import | Recipe Environment Import (env 0-5) |
| --- | --- | --- |
| **Purpose** | Agent's workspace to BUILD the recipe | Final deliverable — users deploy this |
| **`zeropsSetup`** | NEVER (app code doesn't exist yet) | Always (`prod` or `dev`) |
| **`buildFromGit`** | NEVER on app services (agent creates code via SSH) | Always (points to finished recipe app repo) |
| **`startWithoutCode`** | YES on every dev service | Not needed (`buildFromGit` provides code) |
| **File naming** | `bootstrap-{fw}-minimal.yaml` / `bootstrap-{fw}-showcase.yaml` | `import.yaml` inside each env folder |
| **Count** | 2 total (one per tier) | 6 per tier (env 0-5) |
| **Project name** | `{framework}-dev-minimal` / `{framework}-dev-showcase` | `{recipe-slug}-{environment}` |

### Bootstrap: Minimal

```yaml
# Bootstrap import for {framework} minimal recipe development.
# Creates a workspace project with app services and PostgreSQL.
# Run: zcli project project-import bootstrap-{framework}-minimal.yaml

project:
  name: {framework}-dev-minimal

services:
  # Agent workspace — SSH in to develop the app.
  # No buildFromGit: agent creates the code from scratch.
  - hostname: appdev
    type: <framework-service-type>
    startWithoutCode: true
    verticalAutoscaling:
      minRam: 0.5

  # Staging service — agent deploys here to test
  # the production build pipeline.
  - hostname: appstage
    type: <framework-service-type>
    enableSubdomainAccess: true
    verticalAutoscaling:
      minRam: 0.5
      minFreeRamGB: 0.25

  # PostgreSQL for the app's data.
  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
```

### Bootstrap: Showcase

Same as minimal, plus all showcase services:

```yaml
# Bootstrap import for {framework} showcase recipe development.
# Creates a workspace project with app services and full service stack.

project:
  name: {framework}-dev-showcase

services:
  - hostname: appdev
    type: <framework-service-type>
    startWithoutCode: true
    verticalAutoscaling:
      minRam: 0.5

  - hostname: appstage
    type: <framework-service-type>
    enableSubdomainAccess: true
    verticalAutoscaling:
      minRam: 0.5
      minFreeRamGB: 0.25

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10

  - hostname: redis
    type: valkey@7.2
    mode: NON_HA
    priority: 10

  - hostname: storage
    type: object-storage
    objectStorageSize: 2
    objectStoragePolicy: public-read
    priority: 10

  - hostname: search
    type: meilisearch@1.12
    mode: NON_HA
    priority: 10

  # Queue worker (staging) — processes background jobs.
  # Same framework type as the app but will use the
  # 'worker' zeropsSetup.
  - hostname: workerstage
    type: <framework-service-type>
    verticalAutoscaling:
      minRam: 0.5

  # Queue worker (dev) — workspace for worker development.
  - hostname: workerdev
    type: <framework-service-type>
    startWithoutCode: true
    verticalAutoscaling:
      minRam: 0.5

  - hostname: mailpit
    type: go@1
    buildFromGit: https://github.com/zerops-recipe-apps/mailpit-app
    enableSubdomainAccess: true
```

**Framework-specific additions**: If the framework needs `envSecrets` (like Laravel's `APP_KEY`), add them to both `appdev` and `appstage` with `#zeropsPreprocessor=on` at the top.

---

## Research → Output Mapping

Every researched value must appear in a specific location in the output prompts. This table prevents the agent from doing research and then not using the results.

| Research Field | Where It Goes in Output Prompt |
| ----- | ----- |
| Runtime hello-world patterns | Baseline for ALL Zerops-specific config — service type, OS, cache, deploy, env vars. Framework prompt inherits and extends. |
| Zerops service type | zerops.yaml `run.base`, import.yaml `type:`, bootstrap `type:` |
| Build base(s) | zerops.yaml `build.base` in prod setup |
| Package manager commands | zerops.yaml `buildCommands` |
| Deploy files | zerops.yaml `deployFiles` |
| Start command | zerops.yaml `run.start` (omit for PHP-Nginx) |
| HTTP port | zerops.yaml `run.ports`, `readinessCheck`, `healthCheck` |
| Migration command | zerops.yaml `run.initCommands` |
| App secret env var | import.yaml `envSecrets`, zerops.yaml env var docs |
| Cache paths | zerops.yaml `build.cache` |
| OS preference | zerops.yaml `build.os` and `run.os` |
| Process manager | zerops.yaml `run.start` command, Port Config section |
| Trusted proxy config | zerops.yaml base `envVariables` |
| Logging config | zerops.yaml base `envVariables` |
| Nginx config needed? | zerops.yaml `siteConfigPath`, app code `site.conf.tmpl` |
| Extra dev tools | zerops.yaml dev `run.prepareCommands` |
| Dev init commands | zerops.yaml dev `initCommands` |
| Cache/session/queue/storage/search/mail libs | zerops.yaml showcase base `envVariables`, app code dependencies |

## Architectural Decision Trees

These replace all "if needed" / "optional" conditionals. Walk through each tree ONCE during research, record the branch taken, and apply it consistently across all output files.

### Tree 1: Web Server Architecture

```
Is the framework PHP-based?
├── YES → use `php-nginx@X.X` as service type
│         ├── Create `site.conf.tmpl` in app code
│         ├── Add `siteConfigPath: site.conf.tmpl` to base run config
│         ├── Port is 80 (implicit, do NOT declare `ports` in zerops.yaml)
│         ├── No `start` command (PHP-FPM is managed by Nginx)
│         └── readinessCheck and healthCheck use port: 80
│
└── NO → use language runtime as service type (nodejs@22, python@3.12, etc.)
          ├── No site.conf.tmpl needed
          ├── No siteConfigPath
          ├── MUST declare `ports` with `httpSupport: true` in zerops.yaml
          ├── MUST have `start` command (e.g., `node dist/main.js`, `gunicorn ...`)
          ├── Start command MUST bind to 0.0.0.0 (not localhost)
          └── readinessCheck and healthCheck use the declared port
```

### Tree 2: Build Base Configuration

```
Does the framework need multiple runtimes at build time?
├── YES (e.g., PHP + Node.js for Inertia/Vite)
│   └── Use multi-base: `base: [php@8.4, nodejs@22]`
│
└── NO (e.g., pure Node.js, pure Python)
    └── Use single base: `base: nodejs@22`

Does the framework compile to a binary?
├── YES (Go, Rust, Java)
│   ├── Prod: build commands include compilation
│   ├── Prod: deployFiles lists specific binaries
│   ├── Dev: build commands are no-op (`true`)
│   ├── Dev: deployFiles is `./` (source only)
│   └── Build cache: `cache: true` (global dep cache)
│
└── NO (PHP, Python, Node.js, Ruby)
    ├── Prod: deployFiles is `./` (full source)
    ├── Dev: deployFiles is `./` (same)
    └── Build cache: `cache:` with explicit paths (vendor, node_modules)
```

### Tree 3: OS Selection

```
Build OS:
├── PHP → ubuntu (extension compatibility)
├── Python → ubuntu (C extension compilation)
├── Ruby → ubuntu (native gem compilation)
├── Java → ubuntu (JDK tooling)
├── Node.js → default alpine (sufficient)
├── Go → default alpine (sufficient)
└── Other → ubuntu (safer default)

Run OS:
├── PHP → ubuntu (same reason as build)
├── Python → ubuntu (runtime C libraries)
├── Ruby → ubuntu (native gems at runtime)
├── Dev setup (any language) → ubuntu (richer SSH toolset)
└── Prod Node.js/Go/Rust/Elixir → default alpine (smaller)
```

### Tree 4: Dev Setup Extra Tooling

```
Does the framework have an integrated frontend that needs a DIFFERENT runtime?
├── YES (e.g., Laravel + Inertia needs Node.js, but run.base is php-nginx)
│   ├── Add `run.prepareCommands` with `zsc install <runtime>` (e.g., `zsc install nodejs@22`)
│   ├── Add `zsc scale ram +0.5GB 10m` to initCommands (dep install is heavy)
│   └── Add both package managers to initCommands (e.g., `composer install && npm install`)
│
└── NO (e.g., NestJS is already Node.js, Django has no JS frontend)
    ├── No prepareCommands needed
    ├── initCommands installs deps with the framework's package manager only
    └── RAM scaling may still be needed if dep install is heavy
```

---

## Output Prompt Structure

### Minimal Prompt — Self-Contained

The minimal prompt (`zrecipator-{framework}-minimal.md`) MUST be fully self-contained. It follows this exact section ordering (same structure used by all existing zrecipator prompts):

```
1.  Title & Preamble
2.  Architectural Gates
3.  Mission
4.  Zerops Platform Terminology
5.  Build & Deploy Pipeline
6.  Zerops Platform Model
7.  Two-Repository Structure
8.  Application Code Requirements
9.  The zerops.yaml File
10. Comment System
11. Service Priority
12. Port Configuration
13. healthCheck vs readinessCheck
14. Process Managers (non-PHP only)
15. Resource Configuration
16. initCommands Intelligence
17. The Six Environments
18. README Templates
19. Import.yaml Conventions
20. Operational Workflow
21. Verification Gates
```

### Showcase Prompt — Delta Document Referencing Minimal

The showcase prompt (`zrecipator-{framework}-showcase.md`) is a **delta document**. It does NOT duplicate content from minimal. Instead, it starts with a prerequisite block and only contains sections that are NEW or OVERRIDDEN.

**Showcase must begin with this block:**

```markdown
## Prerequisite — Read the Minimal Prompt First

This prompt extends the minimal recipe prompt. Before reading further,
load and internalize the complete minimal prompt:

→ `zrecipator-{framework}-minimal.md` (same directory as this file)

That document contains all shared Zerops platform rules: terminology,
build/deploy pipeline, platform model, comment system, service priority,
port configuration, healthCheck vs readinessCheck, process managers,
resource configuration, initCommands intelligence, README templates,
import.yaml conventions, and operational workflow.

**Everything in the minimal prompt applies here unless explicitly
overridden below.** This prompt only specifies what CHANGES or ADDS
to the minimal foundation.
```

**Showcase sections (only these are included):**

```
1.  Title & Preamble (adapted — showcase naming)
2.  Prerequisite block (above)
3.  Architectural Gates (adapted — mentions showcase services)
4.  Mission (rewritten — showcase deliverables)
5.  Two-Repository Structure (adapted — showcase naming)
6.  Service Stack (NEW — table of all showcase services)
7.  Application Code Requirements (rewritten — showcase dashboard, models, migrations, worker, libraries)
8.  The zerops.yaml File (rewritten — 4 setups: base + prod + dev + worker, all showcase env vars)
9.  The Six Environments (OVERRIDDEN — showcase services per environment, object storage sizing)
10. README Templates (OVERRIDDEN — showcase naming, services line text)
11. Import.yaml Conventions (OVERRIDDEN — showcase services in import.yaml)
12. Operational Workflow (DELTA — only showcase-specific steps, references minimal for shared steps)
13. Verification Gates (ADDITIVE — minimal gates + showcase-specific gates)
```

**Sections from minimal that are NOT repeated in showcase** (they apply unchanged):

- Zerops Platform Terminology
- Build & Deploy Pipeline
- Zerops Platform Model
- Comment System
- Service Priority
- Port Configuration
- healthCheck vs readinessCheck
- Process Managers
- Resource Configuration
- initCommands Intelligence

### Section Authoring Guide (Minimal Prompt)

**Sections copied word-for-word** (no adaptation needed — platform rules are framework-agnostic):

- Zerops Platform Terminology (entire table)
- Build & Deploy Pipeline (three phases, exactly as written below)
- Zerops Platform Model (two config files, two service types, service architecture)
- Comment System (distribution target, line width, three-tier strategy, self-containment rule, two additional rules)
- Service Priority (explanation + example structure — adapt `type:` in YAML examples)

**Sections adapted by substituting researched values** (structure stays identical, replace placeholders):

- Port Configuration → insert the framework's port, remove PHP-Nginx if not applicable
- healthCheck vs readinessCheck → insert the framework's port in examples
- Process Managers → include only if NOT PHP-Nginx; insert the framework's process manager
- Resource Configuration → insert the framework's language in RAM guidance table
- initCommands Intelligence → insert the framework's migration command in examples
- Operational Workflow → insert framework-specific commands in test/debug steps

**Sections rewritten for minimal** (structure follows the template, content differs):

- Mission → minimal deliverables
- Two-Repository Structure → minimal naming
- Application Code Requirements → minimal dashboard (DB only)
- The zerops.yaml File → 3 setups (base + prod + dev), DB env vars only
- The Six Environments → minimal services per environment
- README Templates → minimal naming, minimal services line text
- Import.yaml Conventions → minimal services per environment
- Verification Gates → minimal gates only

---

## Shared Zerops Platform Foundation

The following sections go into both output prompts. Copy them as-is, substituting only the framework's service type in YAML examples (e.g., `php-nginx@8.4` → `nodejs@22`).

### Zerops Platform Terminology

Use correct terminology in all comments and documentation. Reference: https://docs.zerops.io

| Correct Term | Wrong/Outdated Term | Docs Reference |
| ----- | ----- | ----- |
| **build container** | ~~build machine~~ | "Zerops starts a temporary build container" — [build process](https://docs.zerops.io/nodejs/how-to/build-process) |
| **application artifact** | ~~container image~~ | "the application artefact is stored in internal Zerops storage" — [deploy process](https://docs.zerops.io/nodejs/how-to/deploy-process) |
| **project balancer** | ~~load balancer~~ (generic) | "removed from the project balancer" — [deploy process](https://docs.zerops.io/nodejs/how-to/deploy-process) |
| **readiness check** (`deploy.readinessCheck`) | ~~health check~~ (for deployment) | [zerops.yaml spec](https://docs.zerops.io/zerops-yaml/specification#readinesscheck-) |
| **runtime container** | (correct as-is) | [infrastructure](https://docs.zerops.io/features/infrastructure) |
| **Lightweight / Serious core** | ~~core package~~ (informal) | [infrastructure](https://docs.zerops.io/features/infrastructure#project-core-options) |
| **referencing variables** | ~~service discovery~~ | [env variables](https://docs.zerops.io/features/env-variables#referencing-variables) |

### Build & Deploy Pipeline (Per Docs)

Three phases. This is the canonical reference for comments:

**PHASE 1: BUILD** (temporary build container — auto-deleted after)

1. Install build environment (base OS + runtime)
2. Download source code (from GitHub/GitLab/zCLI)
3. Run `prepareCommands` (customize build environment)
4. Run `buildCommands` (compile, package)
5. Upload application artifact to internal Zerops storage
6. Cache selected files for faster future builds
7. Build container is deleted

Note: `run.prepareCommands` is the runtime equivalent of `build.prepareCommands` — it customizes the runtime container (install tools, configure environment). Runs once per container creation, cached in the container image.

**PHASE 2: DEPLOY** (new runtime containers)

1. Install runtime environment (or use custom runtime image)
2. Download application artifact from internal storage
3. Run `initCommands` (optional, per-container initialization)
4. Start application via `start` command
5. Run readiness check (if `deploy.readinessCheck` configured)
6. Container becomes active, receives traffic

**PHASE 3: TRAFFIC SWITCH** (for subsequent deploys, zero-downtime by default)

- New containers start alongside old containers
- Old containers are removed from the project balancer (stop receiving new requests)
- Processes inside old containers terminate
- Old containers are deleted

This is zero-downtime deployment by default (`deploy.temporaryShutdown: false`). Setting `temporaryShutdown: true` removes old containers BEFORE starting new ones (causes downtime but uses fewer resources).

Docs: https://docs.zerops.io/features/pipeline

### Zerops Platform Model

Understanding the relationship between files, services, and containers prevents architectural mistakes in recipes.

#### Two config files, two purposes

- **import.yaml** — One-time infrastructure provisioning. Run via `zcli project project-import`, REST API, or the Zerops UI. Creates a project and its services. You run it once and may never open the file again — which is why import.yaml comments must be self-documenting.
- **zerops.yaml** — Versioned build/deploy pipeline. Lives at the root of the app repository. Defines how to build, deploy, and run the application. Developers have this open alongside their code — comments can be lighter.

#### Two types of services

- **Semi-managed** (databases, caches, storage): You select a mode (`NON_HA` = single node, `HA` = replicated) and Zerops creates an active, running service immediately. No build pipeline needed.
- **App services** (runtime containers): Created as an empty shell. Need a pipeline trigger to build and deploy code. Triggers include `buildFromGit` (in import.yaml — pulls from a public repo), `zcli push` (from terminal/CI), or git integration (push-to-branch).

#### Service architecture

A service = multiple containers behind a single hostname. Each container runs an image built from `run.base` + `run.prepareCommands`, started with `run.start`, using files produced by the build phase and delivered via `build.deployFiles`. Services reference each other by hostname (e.g., `db_hostname`).

### Environment Variables — Three Categories

Zerops has three categories of environment variables:

1. **zerops.yaml envs** (`build.envVariables`, `run.envVariables`): Non-sensitive constants, versioned with code. Can reference generated variables via `${hostname_key}` syntax.
2. **Secret envs** (`envSecrets` in import.yaml, or managed in UI): Sensitive or runtime-mutable values (API keys, app secrets). Set once at import time, persist across deploys. Use `#zeropsPreprocessor=on` with `<@generateRandomString(<N>)>` to auto-generate secrets.
3. **Generated envs**: Platform-provided variables for each service (e.g., `db_hostname`, `db_port`, `db_connectionString`). Referenced from zerops.yaml or secret envs using `${hostname_key}` syntax.

Env vars can be set at **project level** (visible to all services) or **service level** (visible only to that service, but other services can reference them).

#### Referencing Pattern

Variables follow `{hostname}_{credential}`. For a database service with `hostname: db`:

```yaml
DB_HOST: ${db_hostname}    # NOT ${database_hostname} or ${postgresql_hostname}
DB_PORT: ${db_port}
DB_USER: ${db_user}
DB_PASS: ${db_password}
DB_NAME: db                # Static name, no substitution needed
```

If you create a service with `hostname: cache`, the vars are `cache_hostname`, `cache_port`, etc. See [referencing variables](https://docs.zerops.io/features/env-variables#referencing-variables).

### The zerops.yaml Intelligence Principle

Every line in zerops.yaml must result from thinking about the use case, not copying a pattern.

For every decision, ask:

1. **What is the use case?** (prod deployment? dev environment? first-time setup?)
2. **What does this framework need?** (which build tool? what output structure?)
3. **What tools are available?** (composer install vs composer update, pip vs pip freeze)
4. **Which tool matches the use case?** (strict vs flexible, fast vs safe)

**Anti-patterns**: `composer install || composer update` (hides real problems), same approach for dev and prod (not thinking), copy-paste without understanding (PHP doesn't produce binaries like Go).

### deployFiles and start Command Validation

**The #1 deployment failure cause**: Mismatch between `deployFiles` paths and `start` command paths.

Zerops preserves the directory structure you specify. If you deploy `./dist`, it creates `/var/www/dist` in runtime. Your `start` command path must match the deployed structure exactly.

**Validation mental model**: If I deploy `./target/release/app`, where does it land? → `/var/www/target/release/app`. Does my `start` command match? If not, fix it.

```yaml
# ❌ WRONG: Binary at ./target/release/app but start looks at ./app
deployFiles:
  - ./target/release/app
start: ./app  # ERROR: No such file

# ✅ CORRECT: Paths match
deployFiles:
  - ./target/release/app
start: ./target/release/app

# Alternative: Use ~/ to strip path prefix
deployFiles:
  - ./target/release/~/app  # Deployed as ./app
start: ./app
```

Use explicit file/folder names in `deployFiles`, not glob patterns like `*.js` or `*.py` — globs often fail in Zerops build system. Folders work fine (`node_modules`, `dist`, `src`, `vendor`).

**For interpreted languages** (PHP, Python, Node.js): `deployFiles: ./` deploys the entire working directory. Start command paths are relative to `/var/www/`.

**For compiled languages** (Go, Rust): deploy only the binary. Migration binary must also be included in `deployFiles` alongside the main binary.

### Build Cache Strategy

Zerops offers two caching mechanisms. **Use one or the other — never combine them.**

**Option 1: `cache:` with explicit paths** — when dependency install produces a folder **inside the build directory** (e.g., `node_modules`, `vendor`).

```yaml
# PHP, Node.js, Python — deps land in the project tree
build:
  buildCommands:
    - composer install --no-dev
  cache:
    - vendor
    - composer.lock
```

**Option 2: `cache: true`** — when dependencies use a **global/system folder** outside the build directory (e.g., Go module cache, Rust cargo registry). This snapshots the entire build container image but **cleans the build directory**, so only the global toolchain state is preserved.

```yaml
# Go, Rust — deps live in system-level cache dirs
build:
  buildCommands:
    - go mod download
    - go build -o app
  cache: true
```

**Decision table:**

| Language/Runtime | Cache strategy | Why |
| ----- | ----- | ----- |
| PHP | `cache: [vendor, composer.lock]` | `composer install` writes to `./vendor` |
| Node.js | `cache: [node_modules]` | `npm ci` writes to `./node_modules` |
| Python | `cache: [vendor]` | `pip install --target=./vendor` writes to `./vendor` |
| Go | `cache: true` | `go mod download` uses global `GOMODCACHE` |
| Rust | `cache: true` | `cargo` uses global `CARGO_HOME` registry |

**Fallback (last resort)**: Only when neither option above works, redirect the global cache into the build directory via `build.envVariables` and cache those paths explicitly.

### .gitignore Alignment

The `.gitignore` must match the actual artifact locations produced by your build setup.

**For languages using `cache: [folder]`** (PHP, Node.js, Python): `.gitignore` must list those folders — they live in the project tree.

**For languages using `cache: true`** (Go, Rust): dependency caches are in system directories, so no special `.gitignore` entries needed for cache paths. Only ignore build output (e.g., `/app`, `/target/`).

**Alignment rule**: `.gitignore`, `cache`, and `deployFiles` must all be consistent. If you cache `vendor/`, it must be in `.gitignore`. If you deploy `./`, every cached folder ships to runtime.

### Runtime Base Image OS

Zerops defaults to **Alpine** for both build and runtime environments. To use Ubuntu, add `os: ubuntu` to the `build` or `run` section. **Always specify `os:` explicitly in recipes** — don't rely on the default, make the choice visible.

Build and run can use different OS choices.

| Scenario | `run.base` | `run.os` | Why |
| ----- | ----- | ----- | ----- |
| PHP dev | `php-nginx@8.4` | `ubuntu` | PHP ecosystem and extensions work better on Ubuntu |
| PHP prod | `php-nginx@8.4` | `ubuntu` | Same — PHP on Alpine has extension compatibility issues |
| Node.js dev | `nodejs@22` | `ubuntu` | Richer toolset for interactive development via SSH |
| Node.js prod | `nodejs@22` | (default alpine) | Smaller image, sufficient for production |
| Python dev | `python@3.12` | `ubuntu` | System packages, C extensions compile easier |
| Go prod | `golang@latest` | (default alpine) | Go produces static binaries — no glibc needed |

**Rule of thumb**: Use `os: ubuntu` for dev setups and for languages where the ecosystem assumes glibc (PHP, Python). Use the Alpine default for production Node.js and Go.

### Comment System — The 85% Balanced Standard

Recipes are **infrastructure integration templates**. Application code will be replaced by users — the zerops.yaml and import.yaml files are what they keep. Comments must teach infrastructure patterns concisely.

**NOT**: Comment every line (overwhelming) or comment nothing (cryptic). **BUT**: Teach patterns ONCE briefly, then reference. Explain non-obvious decisions. Let self-explanatory config speak for itself.

#### Distribution Target

| Level | Weight | Use For | Length |
| ----- | ----- | ----- | ----- |
| **Narrative (95%)** | 5% | First occurrence of critical patterns only | 1-3 lines |
| **Educational (85%)** | 55% | Non-obvious decisions, key concepts | 1 line |
| **Contextual (70%) or none** | 40% | Self-explanatory config, brief label or skip | 0-1 lines |

**Ratio**: ~0.4-0.6 comment lines per config line for import.yaml (primary learning documents). ~0.3-0.4 for zerops.yaml (developers read alongside their code).

#### Comment Line Width

**All YAML comments must break at 80 characters per line.** Wrap long comments onto continuation lines starting with `#`.

```yaml
# WRONG — over 80 chars
# Deploy the staging service — Zerops pulls source from the 'buildFromGit' repo and uses the 'prod' setup.

# RIGHT — wrapped at 80 chars
# Deploy the staging service — Zerops pulls source
# from the 'buildFromGit' repo and uses the 'prod'
# setup to build and deploy.
```

#### Example Progression (same config at each level)

- **70% Contextual**: `# Production setup`
- **85% Educational**: `# Production setup — build optimized artifacts, deploy minimal footprint.`
- **95% Narrative**: `# Production setup — compile, optimize, deploy minimal artifacts.` + `# Matching build/runtime versions prevents native module failures.`

#### Three-Tier Comment Strategy

**Tier 1 — TEACH ONCE** (first data service — teach the priority pattern):

```yaml
# PostgreSQL for app data. Priority 10 starts data services
# before app containers, preventing connection errors.
- hostname: db
  type: postgresql@16
  mode: NON_HA
  priority: 10
```

**Tier 2 — EXPLAIN DECISIONS** (subsequent services — reference, don't repeat):

```yaml
# Valkey for sessions — enables multi-container session sharing
- hostname: redis
  type: valkey@7.2
  mode: NON_HA
  priority: 10  # Startup ordering explained at 'db' above
```

**Tier 3 — LABEL ONLY** (self-explanatory config):

```yaml
# Object storage for uploads (S3-compatible)
- hostname: storage
  type: object-storage
  objectStorageSize: 5
  priority: 10  # Startup ordering explained at 'db' above
```

**App services** — Tier 2 (context for the service's role):

```yaml
# Staging app — test build pipeline via 'zcli push'
- hostname: appstage
  type: php-nginx@8.4
  zeropsSetup: prod
```

Conversational markers (use strategically): "Notice that...", "Since [X], we [Y]..."

#### Self-Containment Rule

**Each environment's import.yaml is viewed in complete isolation.** A user deploying "Small Production" will never see the "AI Agent" import.yaml. Every comment must make sense to someone who has only that file open. This means each file must explain Zerops primitives at least once — see "Minimum Zerops Primitive Coverage" in Import.yaml Conventions.

- Never reference other environments by name or number ("Environment 0-3", "in the HA environment")
- Never reference services not in the current import.yaml
- Reference other services within the SAME import.yaml freely ("Pattern explained at 'db' service above")
- In zerops.yaml, cross-references between its own prod/dev setups are fine

**WRONG**:

```yaml
# Production setup used by 'appstage' (Environments 0-3)
- setup: prod
```

**RIGHT**:

```yaml
# Production setup — build optimized artifacts, deploy minimal footprint.
- setup: prod
```

#### Two Additional Rules

1. **Anticipate questions** — address "why not X?" before the reader asks
2. **Know when to stop** — if deleting the comment loses no knowledge, skip it

**The "So What?" Test**: For every comment, ask "Why does the reader care?" `# Set DB_CONNECTION to pgsql` fails. `# Tell the framework to use the PostgreSQL driver` passes.

### Service Priority

Data services (databases, caches, storage) always get `priority: 10`. Higher priority = starts first.

```yaml
services:
  - hostname: app
    type: php-nginx@8.4
    zeropsSetup: prod
    # No priority = default 0 → starts AFTER priority 10 services

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10  # Starts FIRST, before app
```

Without priority: App starts → connects → DB not ready → crash loop. With priority: DB starts → ready → app starts → connects → success.

### Port Configuration

App services that serve HTTP must declare their ports in zerops.yaml:

```yaml
run:
  ports:
    - port: 3000
      httpSupport: true
```

**PHP-Nginx exception**: Services using `php-nginx@X.X` handle port configuration internally (Nginx listens on port 80). Do NOT declare `ports` for PHP-Nginx services.

**All other frameworks** (Node.js, Python, Go, Ruby, Java, Elixir): Must declare the port the application listens on. The port in `run.ports` must match what the framework's start command binds to.

```yaml
# Node.js/NestJS — Express/Fastify listens on 3000
ports:
  - port: 3000
    httpSupport: true

# Python/Django — Gunicorn binds to 8000
ports:
  - port: 8000
    httpSupport: true

# Go — net/http listens on 8080
ports:
  - port: 8080
    httpSupport: true
```

### healthCheck vs readinessCheck

Two distinct mechanisms — both use the same syntax but serve different purposes:

- **`deploy.readinessCheck`**: Runs during deployment. New containers must pass this check BEFORE the project balancer routes traffic to them. If it fails, the deploy is aborted and the previous version keeps running. This is your deploy safety gate.

- **`run.healthCheck`**: Runs continuously on already-deployed containers. If a running container fails this check, Zerops restarts it. This catches runtime crashes, memory leaks, and zombie processes.

```yaml
# Both in the same zerops.yaml setup:
deploy:
  readinessCheck:
    httpGet:
      port: 3000
      path: /api/health

run:
  healthCheck:
    httpGet:
      port: 3000
      path: /api/health
```

**For PHP-Nginx**: Both use port 80 (Nginx's port), not a custom port.

**Rule**: Always include `readinessCheck` in prod setup. Include `healthCheck` when the framework benefits from automatic restart on failure.

### Process Managers for Non-PHP Frameworks

PHP has a built-in process manager (PHP-FPM) managed by Nginx via `php-nginx@X.X`. Other frameworks need their own:

| Framework Ecosystem | Process Manager | Start Command Example | Port |
| ----- | ----- | ----- | ----- |
| **PHP** (Laravel) | PHP-FPM (built into `php-nginx`) | (none — managed by Nginx) | 80 |
| **Python** (Django) | Gunicorn | `gunicorn project.wsgi:application --bind 0.0.0.0:8000` | 8000 |
| **Python** (FastAPI) | Uvicorn/Gunicorn | `uvicorn app.main:app --host 0.0.0.0 --port 8000` | 8000 |
| **Node.js** (NestJS) | Node.js directly | `node dist/main.js` | 3000 |
| **Node.js** (Express) | Node.js directly | `node server.js` | 3000 |
| **Ruby** (Rails) | Puma | `bundle exec puma -C config/puma.rb` | 3000 |
| **Java** (Spring Boot) | Embedded Tomcat | `java -jar app.jar` | 8080 |
| **Elixir** (Phoenix) | BEAM VM | `bin/server` | 4000 |
| **Go** (any) | Go binary directly | `./app` | 8080 |

**Key insight**: The `start` command must bind to `0.0.0.0` (not `localhost` or `127.0.0.1`) — Zerops routes traffic to the container's IP, not loopback. Most frameworks default to `0.0.0.0` in production mode, but verify this in the research phase.

### Resource Configuration

#### Vertical Autoscaling Nesting

`minRam`, `minFreeRamGB`, and `cpuMode` MUST ALWAYS be nested under the `verticalAutoscaling` key. **Never place them at the top level of the service configuration.**

```yaml
# CORRECT
- hostname: app
  type: php-nginx@8.4
  verticalAutoscaling:
    minRam: 0.25
    minFreeRamGB: 0.125

# CORRECT — cpuMode is ALSO under verticalAutoscaling
- hostname: app
  type: php-nginx@8.4
  verticalAutoscaling:
    cpuMode: DEDICATED
    minRam: 0.5
    minFreeRamGB: 0.25

# ❌ WRONG — minRam at service level
- hostname: app
  type: php-nginx@8.4
  minRam: 0.25              # WRONG! Must be under verticalAutoscaling
```

#### Why minFreeRamGB Matters

Without `minFreeRamGB`, the container gets exactly `minRam`. When usage hits the limit → OOM kill → restart. Zerops monitors RAM every 10 seconds. Reserve free RAM for GC cycles, traffic spikes, background tasks, and memory fragmentation. See [scaling docs](https://docs.zerops.io/features/scaling).

#### Language-Specific RAM Guidance

| Language/Runtime | minRam | minFreeRamGB | Reserve % | Rationale |
| ----- | ----- | ----- | ----- | ----- |
| Node.js | 0.25 | 0.125 | ~50% | V8 GC needs headroom for traffic spikes |
| Python | 0.5 | 0.25 | 40-50% | Can accumulate memory, Gunicorn workers |
| Go | 0.25 | 0.1 | 30-40% | Efficient GC, but still need spike buffer |
| PHP | 0.25 | 0.125 | ~50% | PHP-FPM workers need breathing room |
| Rust | 0.125 | 0.05 | ~30% | No GC, but reserve for request spikes |
| Java/JVM | 0.5 | 0.25 | ~50% | JVM heap + metaspace, GC pauses |
| Ruby | 0.5 | 0.25 | ~50% | Ruby GC, Puma workers |
| Elixir/BEAM | 0.25 | 0.125 | ~50% | BEAM scheduler, process memory |

Production services reserve 30-50% of `minRam` as free RAM. Dev services can skip `minFreeRamGB`.

### initCommands Intelligence

`initCommands` run **each time** a runtime container starts or restarts, BEFORE the start command — every deploy, restart, scale-up event.

**Use for**: Lightweight, idempotent operations (migrations, cache warming, temp cleanup).
**Do NOT use for**: Installing dependencies or compiling code — that belongs in `buildCommands`.
**Do NOT use for**: Customizing runtime environment — use `run.prepareCommands`. See [docs](https://docs.zerops.io/zerops-yaml/specification#initcommands-).

#### What NEVER Goes in initCommands

| ❌ WRONG | Why | ✅ WHERE it belongs |
| ----- | ----- | ----- |
| `npm install` | Runs on every restart, deps already deployed | `buildCommands` |
| `composer install` | Reinstalls what's in vendor/ | `buildCommands` |
| `pip install -r requirements.txt` | Reinstalls deployed deps | `buildCommands` |
| `go build -o app` | Recompiles on every restart | `buildCommands` (prod only) |
| `cargo build` | Full compilation on every restart | `buildCommands` (prod only) |

**Exception**: In `dev` setup, `initCommands` CAN run `composer install` / `npm install` because the dev setup deploys source only (no pre-built deps) and `zsc execOnce` or similar ensures it runs once.

#### zsc execOnce — Run Once Across Containers

**All database initialization or migration commands MUST be wrapped in `zsc execOnce ${appVersionId}`.** This ensures migrations run only once per version, even with multiple containers, preventing race conditions.

```bash
# CORRECT: Run migration once per version
zsc execOnce ${appVersionId} -- php artisan migrate --force

# CORRECT: One-time init with retry
zsc execOnce someStaticKey --retryUntilSuccessful -- ./init-script.sh
```

One container executes, all others wait. On success, all proceed. On failure, all report failure (unless `--retryUntilSuccessful`).

#### Advanced: Temporary Resource Scaling

```yaml
initCommands:
  - zsc scale ram +0.5GB 10m  # Add 0.5GB for 10 minutes
  - zsc execOnce ${appVersionId} -- php artisan migrate --force
```

### The Six Environments

Each environment has specific purpose and resource profile. Each import.yaml's comments reference only services defined within that file (see Self-Containment Rule).

#### Environment Comparison Matrix (reference only — never embed in comments)

| Environment | Purpose | App Services | Key Characteristics |
| ----- | ----- | ----- | ----- |
| **0 — AI Agent** | AI agents building apps | appdev + appstage | Low resources (0.5GB), both have subdomains |
| **1 — Remote (CDE)** | Human developers via SSH | appdev + appstage | Same as Agent, comments about IDE mounting, suggest 4GB |
| **2 — Local** | Local dev with cloud services | app only | Developer runs locally, uses `zcli vpn up` |
| **3 — Stage** | Pre-production testing | app | Uses prod setup, minimal resources |
| **4 — Small Production** | Moderate traffic prod | app | `minContainers: 2`, `minFreeRamGB` |
| **5 — HA Production** | Enterprise-grade | app | `corePackage: SERIOUS`, `cpuMode: DEDICATED`, HA services |

#### Base Template Structure

```yaml
project:
  name: {recipe-slug}-{environment}

services:
  - hostname: app  # or appstage in dev environments
    type: <framework-service-type>
    zeropsSetup: prod  # or dev for appdev
    buildFromGit: https://github.com/zerops-recipe-apps/{recipe-slug}-app
    enableSubdomainAccess: true
    envSecrets:
      APP_KEY: <@generateRandomString(<32>)>  # if framework needs it
    verticalAutoscaling:
      minRam: 0.5
      minFreeRamGB: 0.125

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
```

#### Environment-Specific Deltas (reference — don't leak into comments)

**Env 0 (AI Agent)**: `appdev` + `appstage` + data services. `minRam: 0.5` on appdev. Comments: AI agent workflow, dev + staging services.

**Env 1 (Remote/CDE)**: Same services as Env 0. Comments: SSH + IDE mounting, suggest `minRam: 4` (commented out).

**Env 2 (Local)**: `app` (prod setup) + data services only. No dev service — developer runs the app locally. Comments: Local development, `zcli vpn up`.

**Env 3 (Stage)**: `app` (prod setup) + data services. Comments: Pre-production testing with production config.

**Env 4 (Small Production)**: `app` + data services. `minContainers: 2`, `minRam: 0.25`, `minFreeRamGB: 0.125`. Comments: Production scaling, minimum containers.

**Env 5 (HA Production)**: `project.corePackage: SERIOUS`. `minContainers: 2`, `minRam: 0.5`, `minFreeRamGB: 0.25`, `cpuMode: DEDICATED`. All data services: `mode: HA`, `cpuMode: DEDICATED`, `minRam: 1`. **All of `cpuMode`, `minRam`, `minFreeRamGB` are under `verticalAutoscaling`.**

Example Env 5 service (correct nesting):

```yaml
- hostname: app
  type: php-nginx@8.4
  zeropsSetup: prod
  buildFromGit: https://github.com/zerops-recipe-apps/{recipe-slug}-app
  enableSubdomainAccess: true
  envSecrets:
    APP_KEY: <@generateRandomString(<32>)>
  minContainers: 2
  verticalAutoscaling:
    cpuMode: DEDICATED
    minRam: 0.5
    minFreeRamGB: 0.25

- hostname: db
  type: postgresql@16
  mode: HA
  priority: 10
  verticalAutoscaling:
    cpuMode: DEDICATED
    minRam: 1
    minFreeRamGB: 0.5
```

### README Templates

#### Fragment System

Zerops extracts specific sections from README files using HTML comment markers with this **exact** format:

```html
<!-- #ZEROPS_EXTRACT_START:key# -->
Content to extract
<!-- #ZEROPS_EXTRACT_END:key# -->
```

**CRITICAL FORMAT**: Tags must use exactly `#ZEROPS_EXTRACT_START:key#` and `#ZEROPS_EXTRACT_END:key#` inside HTML comments. The `#` delimiters on both sides of the key name are required. Any deviation causes **silent extraction failure**.

**Keys**: `intro` (required in ALL READMEs — recipe, environment, and app), `integration-guide` (required in app README only).

**CRITICAL PLACEMENT RULE**: The `intro` fragment wraps ONLY the descriptive paragraph(s) — never the title, never the "This is..." link line (in environment READMEs), never the deploy button, never the cover image. Zerops extracts this fragment and embeds it elsewhere; if it contains the title or images, they'll be duplicated or misplaced.

#### Main Recipe README Template

**File**: `recipes/{recipe-slug}/README.md`

Follow structure exactly. **Variables**: `{recipe-slug}` = URL/folder slug (e.g., `laravel-hello-world`). `{Recipe Name}` = display name (e.g., `Laravel Hello World`). `{lang-tag}` = Zerops recipe filter tag (e.g., `php`, `nodejs`, `python`). `{Language}` = language display name (e.g., `PHP`, `Node.js`).

The deploy button links to `?environment=small-production` by default. Environment list separators are em dashes (`—`), not hyphens. The `⬇️` emoji before the deploy button text is required.

```markdown
# {Recipe Name} Recipe

<!-- #ZEROPS_EXTRACT_START:intro# -->
[1-3 lines: what the app is, what it connects to. See Intro Content Rules below.]
<!-- #ZEROPS_EXTRACT_END:intro# -->

⬇️ **Full recipe page and deploy with one-click**

[![Deploy on Zerops](https://github.com/zeropsio/recipe-shared-assets/blob/main/deploy-button/light/deploy-button.svg)](https://app.zerops.io/recipes/{recipe-slug}?environment=small-production)

![cover](https://github.com/zeropsio/recipe-shared-assets/blob/main/covers/svg/cover-{lang-tag}.svg)

Offered in examples for the whole development lifecycle — from environments for AI agents like [Claude Code](https://www.anthropic.com/claude-code) or [opencode](https://opencode.ai) through environments for remote (CDE) or local development of each developer to stage and productions of all sizes.

- **AI agent** [[info]](/0%20—%20AI%20Agent) — [[deploy with one click]](https://app.zerops.io/recipes/{recipe-slug}?environment=ai-agent)
- **Remote (CDE)** [[info]](/1%20—%20Remote%20(CDE)) — [[deploy with one click]](https://app.zerops.io/recipes/{recipe-slug}?environment=remote-cde)
- **Local** [[info]](/2%20—%20Local) — [[deploy with one click]](https://app.zerops.io/recipes/{recipe-slug}?environment=local)
- **Stage** [[info]](/3%20—%20Stage) — [[deploy with one click]](https://app.zerops.io/recipes/{recipe-slug}?environment=stage)
- **Small Production** [[info]](/4%20—%20Small%20Production) — [[deploy with one click]](https://app.zerops.io/recipes/{recipe-slug}?environment=small-production)
- **Highly-available Production** [[info]](/5%20—%20Highly-available%20Production) — [[deploy with one click]](https://app.zerops.io/recipes/{recipe-slug}?environment=highly-available-production)

---

For more advanced examples see all [{Language} recipes](https://app.zerops.io/recipes?lf={lang-tag}) on Zerops.

Need help setting your project up? Join [Zerops Discord community](https://discord.gg/zeropsio).
```

#### Environment README Template

**File**: `recipes/{recipe-slug}/{N} — {Environment Name}/README.md`

These are deliberately **short** — 4-6 lines of content total. Each is self-contained. The structure has **three distinct parts**: title (outside fragment), "This is..." link line (outside fragment), then the fragment with bold environment name + description.

Both the title AND the "This is..." link line are **OUTSIDE** the `intro` fragment. Only the bold description paragraph(s) are inside.

```markdown
# {Recipe Name} — {Environment Title Suffix}

This is [env-description] for [{Recipe Name} (info + deploy)](https://app.zerops.io/recipes/{recipe-slug}?environment={env-slug}) recipe on [Zerops](https://zerops.io).

<!-- #ZEROPS_EXTRACT_START:intro# -->
**{Environment name}** environment [description].
[Optional services line for env 0-2 only.]
<!-- #ZEROPS_EXTRACT_END:intro# -->
```

**Environment-specific content — fixed text per environment tier**:

Environments 3-5 use **stable boilerplate** that is identical across all recipes (only the recipe name/slug changes). Environments 0-2 have a fixed first line plus a variable services line that depends on what's in the import.yaml.

| Env | env-slug | Title suffix | "This is..." text | Fragment first line (fixed for all recipes) |
| ----- | ----- | ----- | ----- | ----- |
| 0 | ai-agent | AI Agent Environment | "an AI agent environment" | `**AI agent** environment provides a development space for AI agents to build and version the app.` |
| 1 | remote-cde | Remote Environment | "a remote (CDE) environment" | `**Remote (CDE)** environment allows developers to build the app **within Zerops** via SSH, supporting the full development lifecycle without local tool installation.` |
| 2 | local | Local Environment | "a local environment" | `**Local** environment supports local app development using zCLI VPN for database access, while ensuring valid deployment processes using a staged app in Zerops.` |
| 3 | stage | Stage Environment | "a stage environment" | `**Stage** environment uses the same configuration as production, but runs on a single container with lower scaling settings.` |
| 4 | small-production | Small Production Environment | "a small production environment" | `**Small production** environment offers a production-ready setup optimized for moderate throughput.` |
| 5 | highly-available-production | Highly-available Production Environment | "a highly-available production environment" | `**Highly-available production** environment provides a production setup with enhanced scaling, dedicated resources, and HA components for improved durability and performance.` |

**Services line (env 0-2 only)** — list what's deployed, adapted to the tier:

- **Minimal** (app + db): `It includes a dev service with the code repository and necessary development tools, a staging service, and a low-resource database.`
- **Minimal with cache** (app + db + redis): `It includes a dev service with the code repository and necessary development tools, a staging service, and low-resource database and cache.`
- **Showcase** (app + db + redis + storage + mailpit + search): `Comes with a dev service with the source code and necessary development tools, a staging service, email & SMTP testing tool, search engine, and low-resource databases and storage.`
- **Env 2 (Local)**: Only mention extra services beyond the core app + db if they exist. Simple recipes have no services line at all for env 2.

**Env 3-5 do NOT list services** — their descriptions are generic and stable.

**Concrete example** (Laravel Hello World — AI Agent env):

```markdown
# Laravel Hello World — AI Agent Environment

This is an AI agent environment for [Laravel Hello World (info + deploy)](https://app.zerops.io/recipes/laravel-hello-world?environment=ai-agent) recipe on [Zerops](https://zerops.io).

<!-- #ZEROPS_EXTRACT_START:intro# -->
**AI agent** environment provides a development space for AI agents to build and version the app.
It includes a dev service with the code repository and necessary development tools, a staging service, and a low-resource database.
<!-- #ZEROPS_EXTRACT_END:intro# -->
```

**Concrete example** (Laravel Hello World — Small Production env):

```markdown
# Laravel Hello World — Small Production Environment

This is a small production environment for [Laravel Hello World (info + deploy)](https://app.zerops.io/recipes/laravel-hello-world?environment=small-production) recipe on [Zerops](https://zerops.io).

<!-- #ZEROPS_EXTRACT_START:intro# -->
**Small production** environment offers a production-ready setup optimized for moderate throughput.
<!-- #ZEROPS_EXTRACT_END:intro# -->
```

#### Application README Template

**File**: `zerops-recipe-apps/{recipe-slug}-app/README.md`

Must include complete zerops.yaml in integration-guide fragment.

```markdown
# {Recipe Name} Recipe App

<!-- #ZEROPS_EXTRACT_START:intro# -->
[1-2 sentences: what the app is, what infrastructure it connects to.
Focus on stack topology — not HTTP codes, response formats, binary sizes.]
Used within [{Recipe Name} recipe](https://app.zerops.io/recipes/{recipe-slug}) for [Zerops](https://zerops.io) platform.
<!-- #ZEROPS_EXTRACT_END:intro# -->

⬇️ **Full recipe page and deploy with one-click**

[![Deploy on Zerops](https://github.com/zeropsio/recipe-shared-assets/blob/main/deploy-button/light/deploy-button.svg)](https://app.zerops.io/recipes/{recipe-slug}?environment=small-production)

![cover](https://github.com/zeropsio/recipe-shared-assets/blob/main/covers/svg/cover-{lang-tag}.svg)

## Integration Guide

<!-- #ZEROPS_EXTRACT_START:integration-guide# -->

### 1. Adding `zerops.yaml`
The main application configuration file you place at the root of your repository, it tells Zerops how to build, deploy and run your application.

```yaml
zerops:
  # PASTE THE ENTIRE zerops.yaml HERE WITH ALL COMMENTS
```
<!-- #ZEROPS_EXTRACT_END:integration-guide# -->
```

#### README Intro Content Rules

**Recipe README intro** (1-3 lines): Describe what the recipe provides as a whole — the framework, the infrastructure services it connects to, and the fact that it includes ready-made environment configurations. Write a unique description for each recipe; do NOT reuse phrasing from these examples.
  - ✅ Mentions the framework with a link, links to Zerops, names the database/services involved, and conveys the recipe's scope.
  - ❌ Never parrot a fixed sentence template. Never include: HTTP status codes, JSON response shapes, deployment timings, port numbers, internal file paths.

**App README intro** (1-2 lines): Same rules as recipe README. End with `Used within [{Recipe Name} recipe](url) for [Zerops](https://zerops.io) platform.`

### Import.yaml Conventions

**Top-level comment**: Each import.yaml starts with a comment block that mirrors the environment README intro text. This creates consistency when users read either file.

Example (Laravel Hello World — AI Agent):

```yaml
# AI agent environment provides a development space
# for AI agents to build and version the app. It includes
# a dev service with the code repository and necessary
# development tools, a staging service, and a low-resource
# database.
```

Example (Laravel Hello World — Stage):

```yaml
# Stage environment uses the same configuration as production,
# but runs on a single container with lower scaling settings.
```

**Comment style per service**: Comments explain the service's role and why it's there. Use natural language, not repeating the YAML keys. Env 0-1 are most verbose (explaining workflow). Env 3-5 are more concise but still teach Zerops primitives.

#### Import.yaml Service Comment Patterns

- **App service (prod/stage)**:
  ```yaml
  # Deploy the staging service — Zerops pulls source
  # from the 'buildFromGit' repo and uses the 'prod'
  # zeropsSetup to build and deploy. Subdomain access
  # provides a public HTTPS URL.
  ```
- **App service (dev)**:
  ```yaml
  # Set up the AI agent development environment —
  # Zerops pulls source from the 'buildFromGit' repo,
  # using the 'dev' setup which includes the framework
  # pre-installed. SSH in and start developing.
  ```
- **Database service**:
  ```yaml
  # PostgreSQL single-node database for development.
  # Accessible as 'db' hostname from 'appdev' and
  # 'appstage' services. NON_HA is suitable for
  # dev/staging where HA durability isn't required.
  ```
- **Additional services (Redis, storage)**:
  ```yaml
  # Spin up Redis-compatible Valkey, used by
  # the app for caching, sessions, queues, etc.
  ```
  ```yaml
  # Create 'public-read' object storage bucket,
  # with minimal storage capacity.
  ```

#### Production Database Backup Note (env 4-5)

In env 4-5 (production), include a brief note about backups on the database:

```yaml
# PostgreSQL single-node — automatic encrypted
# backups are on by default. For production traffic,
# consider HA mode or your own backup strategy.
- hostname: db
  type: postgresql@16
  mode: NON_HA
  priority: 10
```

#### Minimum Zerops Primitive Coverage (per import.yaml)

Each import.yaml is self-contained — a reader may see only this one file. Every file must explain these Zerops-specific concepts at least once (brief, Tier 2 level). Weave them naturally into service comments rather than creating a separate explanation block:

| Concept | Where to explain | What to convey |
| ----- | ----- | ----- |
| `buildFromGit` | First app service | Zerops pulls source code and zerops.yaml from this public repo to trigger the build/deploy pipeline (one of several triggers — users can also use `zcli push` or git integration) |
| `zeropsSetup` | First app service | Selects which setup (prod/dev) from the zerops.yaml in the source repo |
| `enableSubdomainAccess` | First app service with it | Gives the service a public HTTPS URL on a Zerops subdomain |
| `mode: NON_HA` | Database service | Single-node — suitable for dev/staging (not production-grade durability) |
| `mode: HA` | Database service (env 5) | Replicates data across multiple nodes — no single point of failure |
| `priority` | First data service | Higher priority = starts first; prevents app connecting to not-yet-ready database |
| `minContainers` | Where used (env 4-5) | Always runs at least N containers for availability and load distribution |
| `verticalAutoscaling` | First service with non-trivial settings | Zerops auto-scales RAM (and CPU if DEDICATED) within these bounds |
| `corePackage: SERIOUS` | Project level (env 5) | Dedicated infrastructure for the project's balancer, logging, and metrics |
| `readinessCheck` | prod setup in zerops.yaml (not import.yaml) | Verifies new containers are healthy before the project balancer sends them traffic |
| `healthCheck` | prod setup in zerops.yaml (not import.yaml) | Continuously monitors running containers; Zerops restarts on failure |
| `ports` | prod setup in zerops.yaml | Declares which port the app listens on; `httpSupport: true` enables HTTP routing |
| `envSecrets` | First service with secrets | Sensitive values set at import time, persist across deploys — not redeployed with code |

These are 1-line explanations woven into existing comments — not separate paragraphs. For example:

```yaml
# Staging app — validates the production build pipeline.
# Zerops pulls source and zerops.yaml from the
# 'buildFromGit' repo, using the 'prod' setup
# to build and deploy. Subdomain access provides
# a public HTTPS URL for testing.
- hostname: app
  type: php-nginx@8.4
  zeropsSetup: prod
  buildFromGit: https://github.com/zerops-recipe-apps/laravel-hello-world-app
  enableSubdomainAccess: true
  envSecrets:
    APP_KEY: <@generateRandomString(<32>)>
```

#### Project Naming Conventions

| Environment | Project name |
| ----- | ----- |
| 0 — AI Agent | `{recipe-slug}-agent` |
| 1 — Remote | `{recipe-slug}-remote` |
| 2 — Local | `{recipe-slug}-local` |
| 3 — Stage | `{recipe-slug}-stage` |
| 4 — Small Prod | `{recipe-slug}-small-prod` |
| 5 — HA Prod | `{recipe-slug}-ha-prod` |

#### Additional import.yaml Features (use when needed)

- **YAML Preprocessor**: `#zeropsPreprocessor=on` as first line enables `<@generateRandomString(<N>)>` for secret generation via `envSecrets`. See [preprocessor docs](https://docs.zerops.io/references/import-yaml/pre-processor).
- **envSecrets**: Per-service secrets set at project creation (e.g., `APP_KEY`). Unlike `envVariables` in zerops.yaml, these persist and aren't redeployed.
- **Setup Inheritance (`extends`)**: Backend framework recipes always use `extends: base` — frameworks invariably share 10+ env vars between prod and dev.
- **Multi-base builds**: `base: [php@8.4, nodejs@22]` when build needs multiple runtimes.
- **`run.prepareCommands`**: Install additional runtime tools via `zsc install <runtime>` (e.g., `zsc install nodejs@22` in a PHP container). See [zsc docs](https://docs.zerops.io/references/zsc). Runs once per container creation, cached in the container image.
- **`siteConfigPath`**: Custom Nginx config for PHP-Nginx services.
- **Object storage**: `objectStorageSize: <GB>` and `objectStoragePolicy: public-read | private`. Typical: 2GB for dev/stage, 5GB small prod, 100GB HA prod.
- **Mailpit**: SMTP testing tool — `hostname: mailpit`, `type: go@1`, `buildFromGit: https://github.com/zerops-recipe-apps/mailpit-app`, `enableSubdomainAccess: true`.
- **Meilisearch**: Search engine — `type: meilisearch@1.12`, `mode: NON_HA | HA`, `priority: 10`.

### Operational Workflow

#### YOU WILL RECEIVE
- **Framework to implement** (e.g., "Laravel", "Django", "NestJS")
- **Link to old recipe** (for reference, may be outdated)
- **Service IDs**: `db`, `appdev` (mounted via SSHFS), `appstage`, and any additional services

#### FILE SYSTEM SETUP
- **Mounted**: `appdev` at `/var/www/appdev/` (source code goes here — not `/var/www/`)
- **Local filesystem**: Recipe structure in your working directory
- **Templates**: All patterns in this prompt

#### DELIVERABLES
- **Application code** in `/var/www/appdev/` — user downloads and pushes to GitHub
- **Recipe structure** in `/var/www/recipes/{recipe-slug}` — user downloads and pushes
- **NO GIT OPERATIONS** — you only create files

#### Step 0: Initial Environment Setup

Never use `env | grep` or `echo` to inspect `ZCP_API_KEY` — reference directly in commands only.

```bash
# 1. Understand file system layout
mount | grep appdev
# Shows: appdev:/var/www on /var/www/appdev type fuse.sshfs

# 2. Login to zcli on the agent container
zcli login $ZCP_API_KEY

# 3. Get all service IDs (use -P flag with projectId)
zcli list services -P $projectId > /tmp/zerops-services.txt

# 4. Login to zcli on appdev (before any other SSH commands)
ssh appdev "zcli login \$ZCP_API_KEY"

# 5. Setup git config on appdev
ssh appdev "git config --global user.email 'claude@zerops.io'"
ssh appdev "git config --global user.name 'Claude Code'"
```

#### Step 1: Review Existing Recipe (If Provided)

Clone locally — do NOT use web_fetch (GitHub pages show HTML, not raw files). Use a timestamped folder. Only clone the legacy recipe the user provided.

```bash
mkdir -p /tmp/learn
SESSION_DIR=/tmp/learn/$(date +%Y%m%d-%H%M%S)
mkdir -p $SESSION_DIR
git clone <legacy-recipe-url> $SESSION_DIR/old-recipe
tree $SESSION_DIR/old-recipe
```

#### Step 2: Write Application Code

```bash
# Write to /var/www/appdev/ (the SSHFS mount)
# Create: framework source, migration, config files,
# .gitignore, zerops.yaml
```

#### Step 3: Initialize Git on appdev

```bash
# Required before first zcli push
ssh appdev "cd /var/www && git init && git add . && git commit -m 'Initial commit'"
```

#### Step 4: Deploy Dev Setup to appdev

```bash
ssh appdev "zcli push <appdev-service-id> --setup=dev -g"

# SSH may close during deployment (container restart) — this is normal
# Re-login if needed:
ssh appdev "zcli login \$ZCP_API_KEY"

# CRITICAL: Wait for deployment to finish before testing.
# The container restarts during deploy —
# files won't be there until it completes.
# Poll service status until it leaves the 'upgrading' state:
for i in $(seq 1 30); do
  STATUS=$(zcli service status <appdev-service-id> -P $projectId 2>/dev/null | grep -i status || echo "unknown")
  echo "Attempt $i: $STATUS"
  echo "$STATUS" | grep -qi "running" && break
  sleep 10
done
```

#### Step 5: Test Dev Setup (before proceeding to prod)

```bash
# Dev uses "zsc noop --silent" — start server manually
# Note: initCommands (migration) already ran during deploy, DB is ready

# Verify dependencies
ssh appdev "ls -la /var/www/vendor"  # or node_modules, etc.

# Start dev server (framework-specific)
ssh appdev "nohup <framework-dev-command> > /tmp/app.log 2>&1 &"
sleep 5
ssh appdev "ps aux | grep -v grep | grep <process-name>"
ssh appdev "tail -20 /tmp/app.log"

# Test internally
ssh appdev "curl -s localhost:<port>/api/health"
# Expected: {"type":"<framework>",
#   "greeting":"Hello from Zerops!",
#   "status":{"database":"OK"}}

# Test externally
zcli service enable-subdomain <appdev-service-id>
APPDEV_SUBDOMAIN=$(ssh appdev "echo \$zeropsSubdomain")
# $zeropsSubdomain already includes https://
curl -s "$APPDEV_SUBDOMAIN/api/health"

# Proceed only when dev works
```

**Debug if health check fails**:

```bash
ssh appdev "env | grep DB_"
ssh appdev "tail -50 /tmp/app.log"
zcli service log -S <appdev-service-id>
```

#### Step 6: Deploy Prod Setup to appstage

```bash
ssh appdev "zcli push <appstage-service-id> --setup=prod"

# Wait for deployment to complete (same pattern as Step 4)
for i in $(seq 1 30); do
  STATUS=$(zcli service status <appstage-service-id> -P $projectId 2>/dev/null | grep -i status || echo "unknown")
  echo "Attempt $i: $STATUS"
  echo "$STATUS" | grep -qi "running" && break
  sleep 10
done
```

#### Step 7: Verify Prod Deployment

```bash
zcli service enable-subdomain <appstage-service-id>
APPSTAGE_SUBDOMAIN=$(ssh appstage "echo \$zeropsSubdomain")
curl -s "$APPSTAGE_SUBDOMAIN/api/health"
```

#### Step 8: Create Recipe Structure

```bash
mkdir -p /var/www/recipes/{recipe-slug}
cd /var/www/recipes/{recipe-slug}
for env in "0 — AI Agent" "1 — Remote (CDE)" "2 — Local" "3 — Stage" "4 — Small Production" "5 — Highly-available Production"; do
  mkdir "$env"
done
```

#### Step 9: Create All Recipe Files

Create in order:

1. `/var/www/appdev/README.md` (with integration-guide fragment)
2. `/var/www/recipes/{recipe-slug}/README.md` (main recipe page)
3. All 6 environment import.yaml files
4. All 6 environment README.md files (with intro fragments)

#### Step 10: Final Verification

```bash
# Structure
tree /var/www/recipes/{recipe-slug}  # 6 folders, 13 files
tree /var/www/appdev  # source + zerops.yaml + README.md

# Deployments still work
curl -s "$APPDEV_SUBDOMAIN/api/health" | grep '"greeting":"Hello from Zerops!"'
curl -s "$APPSTAGE_SUBDOMAIN/api/health" | grep '"greeting":"Hello from Zerops!"'
```

#### Common Pitfalls

| Pitfall | Wrong | Right |
| ----- | ----- | ----- |
| Files in wrong location | Write to `/var/www/` | Write to `/var/www/appdev/` (SSHFS mount) |
| No git init | `zcli push` without git | `git init && git add . && git commit` first |
| Double https:// | `https://$zeropsSubdomain` | `$zeropsSubdomain` (already has https://) |
| SSH loss after deploy | Panic | Normal — re-login: `zcli login \$ZCP_API_KEY` |
| Checking files before deploy finishes | `zcli push` then immediately `ls /var/www/` | Poll `zcli service status` until running, then check |
| deployFiles/start mismatch | `deployFiles: ./dist` + `start: node server.js` | `start: node dist/server.js` — paths must match |

#### Key Commands Reference

```bash
# Deploy (MUST run from appdev via SSH — reads source code from container)
ssh appdev "zcli push <appdev-service-id> --setup=dev -g"
ssh appdev "zcli push <appstage-service-id> --setup=prod"

# All other zcli commands run directly from the control plane (no SSH needed):

# Subdomains
zcli service enable-subdomain <service-id>
ssh appdev "echo \$zeropsSubdomain"   # Env var only exists ON the container

# Debugging
zcli service log -S <service-id>                    # Runtime logs
zcli service log -S <service-id> --show-build-logs  # Build logs
zcli service list -P $projectId                     # List services
```

---

## Tier 1: Minimal Recipe — Specific Rules

### Naming

- Recipe slug: `{framework}-hello-world` (e.g., `laravel-hello-world`, `nestjs-hello-world`)
- App repo: `zerops-recipe-apps/{framework}-hello-world-app`
- Recipe filter tag: the language tag (e.g., `php`, `nodejs`, `python`)

### Services per Environment

| Environment | App Services | Data Services |
| ----- | ----- | ----- |
| 0 — AI Agent | appdev (dev) + appstage (prod) | db (PostgreSQL) |
| 1 — Remote (CDE) | appdev (dev) + appstage (prod) | db (PostgreSQL) |
| 2 — Local | app (prod) | db (PostgreSQL) |
| 3 — Stage | app (prod) | db (PostgreSQL) |
| 4 — Small Prod | app (prod) | db (PostgreSQL) |
| 5 — HA Prod | app (prod) | db (PostgreSQL, HA) |

### Application Code Requirements

#### Health Check Dashboard

**Endpoint**: `GET /` — the root path IS the app. It serves a dashboard page (HTML, rendered by the framework's template engine or view layer) that confirms infrastructure connectivity.

**Dashboard must show**:

1. **Framework identity**: Name, version, runtime info
2. **Database status**: Connection test result ("OK" or error message)
3. **Greeting from DB**: The `message` column from the `greetings` table, proving migrations ran

**Also expose `GET /api/health`** — returns JSON for programmatic checks and readiness check:

```json
{
  "type": "laravel",
  "greeting": "Hello from Zerops!",
  "status": {
    "database": "OK"
  }
}
```

**Rules**:

1. **Actually test connections** — call the framework's DB layer, not fake it
2. **Return HTTP 200** if all services OK, **HTTP 503** if any fail
3. **Include error details**: `"database": "ERROR: connection refused"`
4. **Query migrated data** — the `greeting` field comes from the database, not a hardcoded string
5. **Extensible**: If recipe has redis, add `"cache": "OK"` to status

**Readiness check** in zerops.yaml must point to the JSON endpoint:

```yaml
deploy:
  readinessCheck:
    httpGet:
      port: <framework-port>
      path: /api/health
```

#### Migration Demo

The recipe uses the **framework's own migration system** (not raw SQL scripts). This demonstrates the idiomatic pattern developers will actually use.

**Schema**: Create a `greetings` table with `id` (primary key) and `message` (text). Seed one row: `(1, 'Hello from Zerops!')`.

**How it runs**: Via the framework's migration command in `run.initCommands`:

```yaml
run:
  initCommands:
    # Always wrap in zsc execOnce as defense-in-depth,
    # even if the framework has its own locking.
    - zsc execOnce ${appVersionId} -- <framework-migration-command>
```

**Why initCommands, not buildCommands?** Migrations mutate the database. If they ran during build and the subsequent deploy failed, you'd have a migrated schema with old application code — a dangerous mismatch. Running in `initCommands` ensures migrations and code are deployed atomically.

**Why `zsc execOnce`?** Production environments run multiple containers (e.g., `minContainers: 2`). Without `zsc execOnce`, every container would attempt the migration simultaneously — race conditions, duplicate key errors, or worse. `zsc execOnce ${appVersionId}` ensures exactly one container executes, all others wait for it to succeed.

#### Environment Variables Pattern

Backend frameworks always share many env vars between prod and dev (database credentials, Redis config, storage config, framework settings). **Always use the `extends: base` pattern** — it prevents duplication and ensures prod/dev stay in sync:

```yaml
zerops:
  # Shared configuration — prevents duplication between
  # prod and dev setups. Database credentials, cache config,
  # and framework settings are identical across both.
  - setup: base
    run:
      base: <framework-base>
      envVariables:
        # Database
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}
        DB_NAME: db
        # Framework
        APP_URL: ${zeropsSubdomain}
        # ... all shared vars go here

  - setup: prod
    extends: base
    # ... prod-specific config (build, deploy, start)

  - setup: dev
    extends: base
    # ... dev-specific config (source deploy, noop start)
```

### zerops.yaml Structure (Minimal)

The output prompt must include a **complete, concrete zerops.yaml example** for the framework. Not pseudocode — a real, deployable config.

The template below uses `# DECISION:` comments to mark lines that depend on the decision trees above. Replace every `<placeholder>` with the researched value. Remove `# DECISION:` comments and the lines they mark if the condition is false.

```yaml
# This app uses two setups:
# 'prod' for building, deploying, and running the app
# in production or staging environments.
# 'dev' for deploying source code into a development
# environment with the required toolset.
zerops:
  # Define shared 'base' setup to prevent duplication.
  # Both final setups will use the same subset of
  # environment variables.
  - setup: base
    run:
      base: <run-base>
      os: <run-os>                              # From Decision Tree 3
      siteConfigPath: site.conf.tmpl            # DECISION: PHP-Nginx only (Tree 1 → YES). Remove entire line if not PHP.
      envVariables:
        # Database connection
        DB_CONNECTION: <driver>
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_DATABASE: ${db_dbName}
        DB_USERNAME: ${db_user}
        DB_PASSWORD: ${db_password}
        # Framework essentials
        APP_URL: ${zeropsSubdomain}
        LOG_CHANNEL: <logging-value>            # From Research: Environment & Secrets
        TRUSTED_PROXIES: "<proxy-value>"        # From Research: Environment & Secrets

  - setup: prod
    extends: base
    build:
      base: <build-base>
      os: <build-os>                            # From Decision Tree 3
      buildCommands:
        - <install-deps-prod>
        - <build-assets>                        # DECISION: Only if framework has integrated frontend (Tree 4). Remove if not.
      deployFiles: <deploy-files>               # From Research: Build & Deploy Pipeline
      cache: <cache-paths>                      # From Research: Build & Deploy Pipeline
    deploy:
      readinessCheck:
        httpGet:
          port: <port>
          path: /api/health
    run:
      envVariables:
        APP_ENV: production
        APP_DEBUG: false
      initCommands:
        - <cache-warming-commands>              # Framework-specific (e.g., php artisan config:cache). Remove if framework has none.
        - zsc execOnce ${appVersionId} -- <migration-command>
      ports:                                    # DECISION: Non-PHP only (Tree 1 → NO). Remove ports block entirely if PHP-Nginx.
        - port: <port>
          httpSupport: true
      start: <start-command>                    # DECISION: Non-PHP only (Tree 1 → NO). Remove entire line if PHP-Nginx.
      healthCheck:
        httpGet:
          port: <port>
          path: /api/health

  - setup: dev
    extends: base
    build:
      base: <dev-build-base>
      deployFiles: ./
    run:
      prepareCommands:                          # DECISION: Only if extra runtime needed (Tree 4 → YES). Remove block if not.
        - zsc install <extra-runtime>           # e.g., zsc install nodejs@22
      envVariables:
        APP_ENV: development
        APP_DEBUG: true
      initCommands:
        - zsc scale ram +0.5GB 10m              # DECISION: Only if dev dep install is heavy (Tree 4). Remove if not needed.
        - <dev-dep-install>
      ports:                                    # DECISION: Non-PHP only (same as prod). Remove if PHP-Nginx.
        - port: <port>
          httpSupport: true
      start: zsc noop --silent
```

### Setup: dev (Development) — Key Principles

**The dev setup exists so a developer (human or AI) can SSH in and start working immediately.** Zerops prepares the workspace and gets out of the way. The developer runs whatever they want.

**Key differences from prod**:

1. **Deploy ALL source code**: `deployFiles: ./`
2. **OS**: Set `os: ubuntu` on the run section for dev — richer toolset for SSH work
3. **APP_ENV: development** (or equivalent)
4. **Build commands** — minimal or none (source code only for interpreted languages)
5. **Start command: `zsc noop --silent`** — Zerops starts nothing, developer drives via SSH
6. **initCommands may install deps** — acceptable in dev because source-only deploy doesn't include pre-built deps

**The flow**:

1. `buildCommands` runs on build container → may install in-tree deps (if applicable)
2. `deployFiles: ./` deploys the working directory to runtime container
3. `initCommands` runs migration + installs deps (with temp RAM scaling if needed)
4. `start: zsc noop --silent` → container stays idle
5. Developer SSHs in → workspace ready, DB migrated → runs their app

**Dev philosophy**: The dev setup is a workspace, not a build pipeline. Pre-install only what's needed for the developer to start immediately.

### Verification Gates (Minimal)

#### Gate 0: BOOTSTRAP IMPORTS — workspace files, NOT recipe deliverables

- [ ] Files named exactly `bootstrap-{framework}-minimal.yaml` and `bootstrap-{framework}-showcase.yaml`
- [ ] NO `zeropsSetup` on any service (app code doesn't exist yet — agent creates it)
- [ ] NO `buildFromGit` on app services (agent creates code from scratch via SSH + `zcli push`)
- [ ] `startWithoutCode: true` on every dev service (`appdev`, `workerdev`)
- [ ] `enableSubdomainAccess` ONLY on `appstage` (and `mailpit`), NEVER on `appdev`
- [ ] Project names: `{framework}-dev-minimal` / `{framework}-dev-showcase`
- [ ] Minimal bootstrap has: `appdev`, `appstage`, `db` — nothing else
- [ ] Showcase bootstrap has: `appdev`, `appstage`, `db`, `redis`, `storage`, `search`, `workerstage`, `workerdev`, `mailpit`

#### Gate 1: SILENT KILLERS — config errors that produce no error message

- [ ] `minRam`, `minFreeRamGB`, `cpuMode` nested under `verticalAutoscaling`
- [ ] Fragment tags use exact `#ZEROPS_EXTRACT_START:key#` / `#ZEROPS_EXTRACT_END:key#` format
- [ ] `intro` fragment wraps ONLY description text — not title, link line, deploy button, or image
- [ ] All data services have `priority: 10`
- [ ] Env vars follow `{hostname}_{key}` pattern (not `{service_type}_{key}`)
- [ ] Folder names use em dash (`—`), not hyphen (`-`)
- [ ] `envSecrets` used for app secrets (APP_KEY, SECRET_KEY, etc.)
- [ ] `#zeropsPreprocessor=on` present when using `<@generateRandomString>`
- [ ] `deployFiles` paths match `start` command paths

#### Gate 2: BUILD & RUNTIME — errors that fail loudly

- [ ] Dashboard at `/` renders with framework template engine, shows DB status
- [ ] `/api/health` returns JSON with real DB status + greeting from `greetings` table
- [ ] Prod build pipeline compiles and deploys correctly
- [ ] Dev environment: SSH in → migration ran → install deps → start app → works
- [ ] Migrations wrapped in `zsc execOnce ${appVersionId}`
- [ ] Migration uses framework's own migration system
- [ ] Production services have `minFreeRamGB` (30-50% of minRam)
- [ ] Correct build cache strategy (`cache: [folder]` for in-tree deps, `cache: true` for global)
- [ ] Small Prod: `minContainers: 2`
- [ ] HA Prod: `corePackage: SERIOUS` + `cpuMode: DEDICATED` + HA database
- [ ] Framework-specific: web server config (Nginx template), logging (syslog), trusted proxy

#### Gate 3: DOCUMENTATION — quality issues that don't break functionality

- [ ] All 6 import.yaml + 6 env READMEs + main README + app README exist
- [ ] Project names follow `{recipe-slug}-{environment}` convention (unique per env)
- [ ] Each import.yaml is self-contained: covers Minimum Zerops Primitives, references only its own services
- [ ] Comments follow Three-Tier strategy at 85% educational level
- [ ] All YAML comments wrap at 80 characters per line
- [ ] Correct Zerops terminology (build container, application artifact, project balancer)
- [ ] `integration-guide` fragment includes full zerops.yaml with comments

---

## Tier 2: Showcase Recipe — Specific Rules

The showcase recipe **builds on top of the minimal recipe**. Same framework, same database, same patterns — plus additional services that demonstrate the framework's full infrastructure integration capabilities.

### Naming

- Recipe slug: `{framework}-showcase` (e.g., `laravel-showcase`, `nestjs-showcase`)
- App repo: `zerops-recipe-apps/{framework}-showcase-app`

### Service Stack

Every showcase recipe includes exactly these services:

| Service | Hostname | Type | Purpose |
| ----- | ----- | ----- | ----- |
| **App** | `app` / `appdev` / `appstage` | Framework-specific | The application |
| **PostgreSQL** | `db` | `postgresql@16` | Primary database |
| **Valkey** | `redis` | `valkey@7.2` | Cache, sessions, queues |
| **Object Storage** | `storage` | `object-storage` | File uploads (S3-compatible) |
| **Mailpit** | `mailpit` | `go@1` | SMTP testing tool |
| **Meilisearch** | `search` | `meilisearch@1.12` | Full-text search |

**Production environments (4-5)**: Mailpit is excluded (use real SMTP provider). All other services included.

### Services per Environment

| Environment | App Services | Data Services |
| ----- | ----- | ----- |
| 0 — AI Agent | appdev + appstage + workerdev + workerstage | db, redis, storage, mailpit, search |
| 1 — Remote (CDE) | appdev + appstage + workerdev + workerstage | db, redis, storage, mailpit, search |
| 2 — Local | app + worker | db, redis, storage, mailpit, search |
| 3 — Stage | app + worker | db, redis, storage, mailpit, search |
| 4 — Small Prod | app + worker | db, redis, storage, search |
| 5 — HA Prod | app + worker | db (HA), redis (HA), storage (100GB), search (HA) |

**Worker services**: The queue worker runs the same application code but with a different `zeropsSetup` (`worker` instead of `prod`). In env 0-1, both dev and staging variants exist for the worker — `workerdev` (dev setup, SSH workspace) and `workerstage` (worker setup, processes jobs). In env 2-5, a single `worker` service runs the worker setup.

### Object Storage Sizing

| Environment | Size |
| ----- | ----- |
| 0-3 (dev/stage) | 2 GB |
| 4 (small prod) | 5 GB |
| 5 (HA prod) | 100 GB |

### Application Code Requirements

#### Showcase Dashboard

**Endpoint**: `GET /` — a dashboard page (rendered by the framework's template engine) that confirms ALL infrastructure connections and displays data from each service.

The dashboard is NOT a functional app. It's a **proof of wiring** — "here's your cache working, here's your queue processing, here's your file in storage, here's your search index." All on one page.

**Dashboard panels** (each shows status + demonstration data):

1. **Database**: Connection status, greeting from `greetings` table (same as minimal)
2. **Cache**: Writes a test value (`cache_test: "Hello from cache at {timestamp}"`), reads it back, shows TTL
3. **Queue**: Dispatches a test job that writes a timestamp to the `queue_results` table, shows latest result. The job demonstrates async processing — dispatch happens on page load, result appears after worker processes it
4. **Storage**: Uploads a small test file (e.g., a text file with timestamp), retrieves its public URL, displays it
5. **Search**: Indexes a few test documents (from seeded data), performs a search query, shows results with highlighting
6. **Mail**: Shows SMTP connection status ("Connected to mailpit:1025" or error). Does NOT send on every page load — just verifies the transport is configured

**Also expose `GET /api/health`** — returns JSON:

```json
{
  "type": "laravel",
  "greeting": "Hello from Zerops!",
  "status": {
    "database": "OK",
    "cache": "OK",
    "queue": "OK",
    "storage": "OK",
    "search": "OK",
    "mail": "OK"
  }
}
```

#### Migration & Seeding (Showcase)

Beyond the `greetings` table from minimal, the showcase needs:

1. **`queue_results` table**: `id`, `processed_at` (timestamp), `message` (text) — written to by the queue worker
2. **`searchable_items` table**: `id`, `title` (text), `body` (text) — seeded with 3-5 sample documents for search indexing

The seeder should populate `greetings` (1 row) and `searchable_items` (3-5 rows). Run via `zsc execOnce` in `initCommands`.

#### Queue Worker

The showcase MUST include a queue worker process. This is either:

- **A separate service** in import.yaml (e.g., `hostname: worker`, same framework type, `zeropsSetup: worker`) if the framework benefits from a dedicated worker process
- **An initCommand** that starts the worker in the background if the framework's worker is lightweight

**Preferred approach**: A dedicated `worker` service in import.yaml. This demonstrates the proper pattern for production queue processing. The worker uses the same app code but runs the queue worker command instead of serving HTTP.

**Worker zerops.yaml setup** (add a third setup):

```yaml
- setup: worker
  extends: base  # same env vars as prod
  build:
    # Same build as prod
  run:
    envVariables:
      # Same as prod, possibly with worker-specific vars
    start: <framework-queue-worker-command>
    # No readiness check — worker has no HTTP port
```

### zerops.yaml Structure (Showcase)

Extends the minimal pattern with:

1. **More env vars**: Cache, queue, storage, search, mail config
2. **Worker setup** (third setup alongside prod/dev)
3. **More initCommands**: Search index seeding, storage bucket validation

The base setup has significantly more env vars:

```yaml
- setup: base
  run:
    base: <framework-base>
    envVariables:
      # Database (same as minimal)
      DB_HOST: ${db_hostname}
      DB_PORT: ${db_port}
      # ...

      # Cache & Sessions (Valkey/Redis)
      CACHE_DRIVER: redis  # framework-specific key
      SESSION_DRIVER: redis
      REDIS_HOST: redis
      REDIS_PORT: 6379

      # Queue
      QUEUE_DRIVER: redis

      # Object Storage (S3-compatible)
      AWS_ACCESS_KEY_ID: ${storage_accessKeyId}
      AWS_SECRET_ACCESS_KEY: ${storage_secretAccessKey}
      AWS_BUCKET: ${storage_bucketName}
      AWS_ENDPOINT: ${storage_apiUrl}
      AWS_URL: ${storage_apiUrl}/${storage_bucketName}
      AWS_USE_PATH_STYLE_ENDPOINT: true

      # Search (Meilisearch)
      SEARCH_DRIVER: meilisearch  # framework-specific key
      MEILISEARCH_HOST: http://search:7700
      MEILISEARCH_KEY: ${search_masterKey}

      # Mail (SMTP via Mailpit for dev/stage)
      MAIL_HOST: mailpit
      MAIL_PORT: 1025
      MAIL_DRIVER: smtp

      # Framework essentials
      APP_URL: ${zeropsSubdomain}
      LOG_CHANNEL: syslog
      TRUSTED_PROXIES: "*"
```

### Verification Gates (Showcase)

All gates from Minimal, plus:

#### Gate 1 additions:

- [ ] All 6 services defined in env 0-3 (app, db, redis, storage, mailpit, search)
- [ ] Mailpit excluded from env 4-5
- [ ] Object storage sizing correct per environment
- [ ] `search_masterKey` referenced correctly (`${search_masterKey}`)
- [ ] Worker service defined (if using separate worker)

#### Gate 2 additions:

- [ ] Dashboard shows all 6 service statuses
- [ ] Cache write/read works via the dashboard
- [ ] Queue job dispatch + worker processing works
- [ ] File upload to object storage works, URL is accessible
- [ ] Search indexing + querying works
- [ ] Mail transport connection verified
- [ ] Worker process starts and processes jobs
- [ ] All showcase libraries are the framework's idiomatic/popular choice

---

## Reference: Laravel on Zerops

This is a real-world example of a backend framework on Zerops. Use it to understand patterns, NOT to copy-paste. Each framework will differ in details.

### Laravel zerops.yaml (key patterns)

- Uses `extends: base` for shared env vars (large env var surface)
- `run.base: php-nginx@8.4` — PHP needs a web server, Zerops provides PHP+Nginx combo
- `run.os: ubuntu` on base — PHP ecosystem works better on Ubuntu
- `siteConfigPath: site.conf.tmpl` — custom Nginx config for Laravel's `public/` directory
- Build needs `[php@8.4, nodejs@22]` — PHP for Composer, Node for Inertia/Vite
- `deployFiles: ./` — PHP is interpreted, deploy full source
- `composer install --optimize-autoloader --no-dev` for prod
- `php artisan view:cache`, `config:cache`, `route:cache` — Laravel-specific cache warming
- `php artisan migrate --isolated --force` — `--isolated` handles multi-container locking
- Dev: `run.prepareCommands` installs Node.js via `zsc install nodejs@22` (can't multi-base on run)
- Dev: `initCommands` runs `composer install && npm install` with temporary RAM scaling (`zsc scale ram +0.5GB 10m`)
- `APP_KEY` via `envSecrets` with `<@generateRandomString(<32>)>`
- `LOG_CHANNEL: syslog` for multi-container log collection
- `TRUSTED_PROXIES: "*"` for Zerops L7 HTTP balancer
- `APP_MAINTENANCE_DRIVER: cache` for multi-container maintenance mode

### Nginx Config Template (Laravel)

PHP-Nginx services need a `site.conf.tmpl` file. This is specific to PHP frameworks — other frameworks (Node.js, Python, Go) serve HTTP directly and don't need this.

```nginx
server {
    listen 80;
    listen [::]:80;
    server_name _;
    root /var/www/public;
    add_header X-Frame-Options "SAMEORIGIN";
    add_header X-Content-Type-Options "nosniff";
    index index.php;
    charset utf-8;
    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }
    location = /favicon.ico { access_log off; log_not_found off; }
    location = /robots.txt  { access_log off; log_not_found off; }
    error_page 404 /index.php;
    location ~ \.php$ {
        fastcgi_pass unix:{{.PhpSocket}};
        fastcgi_param SCRIPT_FILENAME $realpath_root$fastcgi_script_name;
        include fastcgi_params;
    }
    location ~ /\.(?!well-known).* {
        deny all;
    }
    access_log syslog:server=unix:/dev/log,facility=local1 default_short;
    error_log syslog:server=unix:/dev/log,facility=local1;
}
```

Note: `{{.PhpSocket}}` is a Zerops template variable — not a user-configurable value.

### Laravel import.yaml (AI Agent env — key patterns)

- `#zeropsPreprocessor=on` enables secret generation
- `type: php-nginx@8.4` — not `php@8.4` (needs web server)
- `envSecrets` for `APP_KEY` with `<@generateRandomString(<32>)>`
- `buildFromGit` points to the app repository
- `valkey@7.2` for cache/sessions/queues (Redis-compatible)
- `object-storage` with `objectStorageSize: 2` and `objectStoragePolicy: public-read`
- Mailpit deployed as `type: go@1` with `buildFromGit` pointing to mailpit-app repo
- All data services have `priority: 10`

---

## Framework Research Checklist

Before writing either output prompt, verify you have definitive answers to ALL of the following. If uncertain about any answer, use web search to confirm.

### Must-Know (blocks prompt generation if unknown)

- [ ] Zerops service type for this framework's runtime
- [ ] Build base image(s) needed
- [ ] Package manager commands (install, build)
- [ ] How the framework starts (command, port)
- [ ] How to configure PostgreSQL connection
- [ ] Framework's migration system and commands
- [ ] Where to put the Nginx config (if PHP) or whether the framework serves HTTP directly
- [ ] What env var holds the app secret/key
- [ ] How to configure logging for multi-container (syslog or equivalent)
- [ ] How to configure trusted proxies for the L7 balancer
- [ ] What goes in `.gitignore`

### Must-Know for Showcase (blocks showcase prompt if unknown)

- [ ] Idiomatic cache library/driver and its config
- [ ] Idiomatic session backend and its config
- [ ] Idiomatic queue library/driver and its worker command
- [ ] Idiomatic S3/object storage library and its config
- [ ] Idiomatic search library (Meilisearch integration) and its config
- [ ] Idiomatic mail library/driver and its config
- [ ] Whether the framework has an integrated frontend (templates, Inertia, etc.)
- [ ] If frontend exists: what build tools are needed, how to configure them

### Nice-to-Know (improves prompt quality)

- [ ] Framework-specific cache warming commands (like `artisan config:cache`)
- [ ] Framework-specific dev server command
- [ ] Common gotchas when deploying this framework (permissions, symlinks, etc.)
- [ ] Whether the framework needs any `run.prepareCommands` (extra system packages)
- [ ] Database connection pooling considerations

---

## Writing the Output Prompts

### Process

1. **Complete the Research Phase** — fill in all tables, walk all decision trees
2. **Walk the Research → Output Mapping** — confirm every answer has a destination
3. **Write the bootstrap imports** — These are workspace YAML files, NOT recipe environment imports. They create the project where the agent will SSH in and build the app from scratch. Verify: no `zeropsSetup`, no `buildFromGit`, `startWithoutCode: true` on dev services, `enableSubdomainAccess` only on `appstage`. Use the templates from the "Bootstrap Import Files" section exactly. File names: `bootstrap-{framework}-minimal.yaml` and `bootstrap-{framework}-showcase.yaml`.
4. **Write the minimal prompt first** — fully self-contained, all 21 sections, ~1000-1300 lines
5. **Write the showcase prompt second** — delta document, references minimal, only new/overridden sections, ~500-700 lines
6. **Cross-validate** — ensure minimal is self-contained; ensure showcase + minimal together cover everything needed for showcase recipe
7. **Run Verification Gates** — check every item in both gate lists

### Content Rules

- **Be concrete, not abstract**: Include real build commands, real env var names, real file paths. The executing agent should never need to guess.
- **Include complete examples**: The zerops.yaml in the output prompt must be a full, runnable config — not pseudocode with `<placeholders>`. Every placeholder from the template in this meta-prompt must be replaced with the actual researched value.
- **Teach the "why"**: Comments in examples should follow the 85% standard — explain decisions, not syntax.
- **Framework-idiomatic**: Use the framework's conventions for everything. Laravel uses Eloquent, not raw SQL. Django uses `manage.py`, not a custom script. NestJS uses decorators, not Express middleware.
- **Test mentally**: For every config you write, trace through the build → deploy → run pipeline. Does the artifact include everything the start command needs? Does the migration command work in the runtime container? Does the health check endpoint actually exist in the app code you're describing?
- **No leftover decision markers**: The output prompts must have no `# DECISION:` comments, no `<placeholder>` tokens, no bracket-conditional notation. Every conditional has been resolved.

### What NOT to Include in Output Prompts

- Do not include framework comparison tables — each prompt is for ONE framework
- Do not include "Research Phase" instructions — the research is already done, the results are baked in
- Do not include the zerops-showcase reference — it's Bun+Python, irrelevant to per-framework prompts
- Do not include decision trees — decisions are already resolved
- Do not include research-to-output mapping — that's internal tooling

### Concrete Example: First Lines of an Output Prompt

This shows what the beginning of `zrecipator-laravel-minimal.md` would look like — a fully resolved, self-contained document with no placeholders:

```markdown
# AI Implementation Guide for Zerops Hello World Recipes — Laravel

**This guide is for AI agents implementing Laravel recipes**, not for humans.
You are Opus-level intelligence creating production code that will train
other LLMs and serve as baseline for thousands of developers.

---

## Architectural Gates

1. **System Planning** — Laravel + PostgreSQL, service topology
2. **Infrastructure Import** — nesting rules, priority ordering
3. **Build & Runtime** — Artisan migrations, Composer/npm pipeline
4. **Verification** — deploy and validate health check

---

## Mission

Create a complete "Hello World" recipe for Laravel:

1. **Application code** with health check dashboard and migration demo
2. **zerops.yaml** with balanced comments explaining non-obvious decisions
3. **6 environment configurations** (import.yaml + README per environment)
4. **Integration guide** that teaches infrastructure patterns

**Success criteria**: A developer or AI can copy this recipe and understand
the infrastructure integration patterns. Application code is a reference
implementation — users will replace it. The zerops.yaml and import.yaml
files are the core deliverable.
```

Note how every section has concrete, Laravel-specific content — no generics, no "insert framework here."

---

## Final Output

Deliver four files:

1. **`bootstrap-{framework}-minimal.yaml`** — Import file to create the minimal workspace project (all `<placeholder>` tokens replaced with real values)
2. **`bootstrap-{framework}-showcase.yaml`** — Import file to create the showcase workspace project (all `<placeholder>` tokens replaced with real values)
3. **`zrecipator-{framework}-minimal.md`** — Complete AI implementation guide for the minimal recipe (~1000-1300 lines, fully self-contained). An agent reading ONLY this file can produce a complete minimal recipe.
4. **`zrecipator-{framework}-showcase.md`** — Delta implementation guide for the showcase recipe (~500-700 lines). References `zrecipator-{framework}-minimal.md` for shared platform rules. An agent reading BOTH files (minimal first, then showcase) can produce a complete showcase recipe.

**Zero placeholders. Zero ambiguity.** The minimal prompt has zero external dependencies. The showcase prompt has exactly one dependency: the minimal prompt in the same directory.
