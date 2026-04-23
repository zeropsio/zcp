create Nest.js showcase recipe
I'll start a recipe authoring workflow for a Nest.js showcase. Let me load the schemas I need.

Zerops [zerops_recipe]
OUT
{"ok":true,"action":"start","slug":"nestjs-showcase","status":{"slug":"nestjs-showcase","current":"research","completed":[],"codebases":0,"services":0,"factsCount":0}}

Zerops [zerops_recipe]
OUT
{"ok":true,"action":"enter-phase","slug":"nestjs-showcase","status":{"slug":"nestjs-showcase","current":"research","completed":[],"codebases":0,"services":0,"factsCount":0}}

Zerops [zerops_recipe]
OUT
{"ok":true,"action":"status","slug":"nestjs-showcase","status":{"slug":"nestjs-showcase","current":"research","completed":[],"codebases":0,"services":0,"factsCount":0}}

Let me get guidance on the recipe workflow and NestJS stack.

Zerops [zerops_recipe]
OUT
{"ok":false,"action":"build-brief","slug":"nestjs-showcase","error":"unknown role \"\""}
Zerops [zerops_workflow]
OUT
Status

Phase: idle
Services: none
Guidance:


Bootstrap route discovery

Bootstrap starts with a discovery pass: call
zerops_workflow action="start" workflow="bootstrap" intent="<one-sentence>"
without a route parameter first. The engine inspects the project
state, the intent, and the recipe corpus and returns a ranked list of
options in routeOptions[]. No session is committed yet.


Pick one option and call start again with the chosen route to commit
the session.


What the ranked list means

Options always arrive in this priority order:



resume — a previous bootstrap session was interrupted.
BootstrapSession tags mark those services as reserved; the only
clean recovery is resuming that session. When this
option is present, pick it first unless you have a specific
reason to override. Carries resumeSession + resumeServices;
dispatch via route="resume" sessionId="<resumeSession>".

adopt — the project has runtime services (non-managed,
non-system) without complete ServiceMeta. Picking adopt attaches
ZCP metadata to the existing services rather than bootstrapping
new ones. Carries adoptServices with the hostnames that will
be adopted. Prefer adopt over recipe when services match what
the user wants; if they don't, use route="classic" instead so
you can plan from scratch without colliding with the existing
stack.

recipe — up to three ranked recipe matches for the intent. Each
carries recipeSlug, confidence, and collisions[] (hostnames
that would conflict with services already in the project). Check
collisions before picking — if the recipe's hostnames clash with
existing services, ask the user or pick a different option.
Dispatch via route="recipe" recipeSlug="<slug>".

classic — always present, always last. Manual plan path. Pick
this to bypass every auto-detection and describe the infrastructure
yourself. Dispatch via route="classic".


Explicit overrides

The route parameter on the commit call bypasses discovery entirely.
Pass it when the LLM has already chosen (from a prior discovery), or
when the user directly specified a route. Valid values: adopt,
recipe, classic, resume. Empty route always re-enters discovery.


Collision semantics

Collisions are advisory at the route-selection stage — they annotate
recipe options but do not suppress them. The real enforcement happens
at the provision step, where zerops_import rejects a plan that tries
to create a hostname that already exists. Use the collisions[] list
as your pre-flight signal rather than waiting for provision to fail.
Start a bootstrap workflow to provision infrastructure:



zerops_workflow action="start" workflow="bootstrap" intent="{your-description}"

Keep the intent one sentence. The first call returns a ranked list of
route options (recipe matches, adopt, classic) — pick one and call
start again with route=... to commit the session. A service plan is
then proposed for you to approve or adjust before any services are
created.
Next:
▸ Primary: Create services — zerops_workflow action="start" workflow="bootstrap"

Zerops [zerops_knowledge]recipe authoring workflow phases showcas…
OUT
[{"uri":"zerops://recipes/laravel-showcase","title":"Laravel showcase on Zerops","score":3,"snippet":"# Laravel showcase on Zerops\n\n## Gotchas\n\n- No .env file — Zerops injects environment variables as OS env vars. Creating a .env file with empty values shadows the OS vars, causing env() to return null for every key that appears in .env even if the platform has a value set.\n- **Cache..."},{"uri":"zerops://recipes/zerops-showcase","title":"Zerops showcase on Zerops","score":3,"snippet":"# Zerops showcase on Zerops\n\n## 1. Adding zerops.yaml\nThe main application configuration file you place at the root of your repository, it tells Zerops how to build, deploy and run your application.\n\nyaml\nzerops:\n  # Production setup — build React frontend + bundle\n  # Bun backend into..."},{"uri":"zerops://themes/services","title":"Managed Service Reference","score":3,"snippet":"...immutable\n**Use for**: **cache + sessions ONLY**. Do NOT use Valkey as a queue broker for Zerops showcases — the canonical queue broker is NATS (see `nats` below and `choose-queue` decision). Using Valkey for queues is a legacy polymorphism pattern (one service wearing three hats); the..."},{"uri":"zerops://guides/deployment-lifecycle","title":"Deployment Lifecycle","score":1,"snippet":"...SSHFS Mount and Deploy Interaction\n\nWhen using SSHFS (`zerops_mount`) for dev workflows, deploy replaces the container. This has important consequences:\n\n1. **After deploy, run container only has `deployFiles` content.** All other files (including zerops.yml if not in..."},{"uri":"zerops://guides/environment-variables","title":"Environment Variables","score":1,"snippet":"...LOG_LEVEL for this service\n\n\n### Typical pattern: project-level URL constants for dual-runtime recipes\n\nDual-runtime recipes (frontend SPA + backend API on the same platform) use project-level URL constants as the single source of truth for cross-service URLs. The constants are derived from..."}]

Zerops [zerops_knowledge]recipe phases research provision scaffol…
OUT
[{"uri":"zerops://themes/services","title":"Managed Service Reference","score":4,"snippet":"...a first-class Redis-bound queue library can keep their queue on Valkey, BUT the showcase still provisions a NATS broker as a separate queue service for the messaging feature section on the dashboard.\nPorts: 6379 (RW), 6380 (RW TLS), 7000 (RO, HA only), 7001 (RO TLS, HA only)\nEnv:..."},{"uri":"zerops://themes/core","title":"Zerops YAML Reference","score":3,"snippet":"...url                    # one-time build from repo — use ONLY with verified URLs (utility recipes like mailpit). Do NOT guess URLs.\n  objectStorageSize: 1-100             # GB, object-storage only (changeable in GUI later)\n  objectStoragePolicy: private | public-read | public-objects-read..."},{"uri":"zerops://guides/environment-variables","title":"Environment Variables","score":2,"snippet":"...LOG_LEVEL for this service\n```\n\n### Typical pattern: project-level URL constants for dual-runtime recipes\n\nDual-runtime recipes (frontend SPA + backend API on the same platform) use project-level URL constants as the single source of truth for cross-service URLs. The constants are derived from..."},{"uri":"zerops://guides/deployment-lifecycle","title":"Deployment Lifecycle","score":1,"snippet":"...locally for development. This is a dev workflow tool.\n- Shared storage mount -- platform feature, attaches a shared-storage volume at /mnt/{hostname} via mount: in import.yml + zerops.yml run.mount. These are completely unrelated features.\n"},{"uri":"zerops://guides/networking","title":"Networking on Zerops","score":1,"snippet":"...subdomain** balancer: fixed 50 MB upload limit (not configurable).\n\n### Advanced Routing Features (GUI)\n\n| Feature | Description |\n|---------|-------------|\n| Redirects | 301/302/307/308 with preservePath and preserveQuery options |\n| Access Policy | CIDR-based IP allow/deny..."}]

Update Todos

Research phase: NestJS stack, features, service plan

Provision phase: create services via bootstrap

Scaffold phase: generate codebases and zerops.yaml

Feature phase: implement showcase features

Finalize phase: emit import.yaml tiers, README
Let me fetch NestJS and showcase-specific knowledge before proceeding with the research phase.

Zerops [zerops_knowledge]
OUT
Platform Constraints

Non-negotiable rules. Violating any causes failures.



MUST bind 0.0.0.0 (not localhost). L7 LB routes to container VXLAN IP. Binding localhost = 502.

Internal traffic = plain HTTP. NEVER https:// between services.

MUST trust proxy headers. Configure the framework's proxy trust setting if it has one (L7 LB terminates SSL and forwards via reverse proxy).

Deploy = new container. deployFiles MANDATORY — without it, run container starts empty.

Build and Run = SEPARATE containers. deployFiles = the ONLY bridge.

run.prepareCommands runs BEFORE deploy files arrive. Never reference /var/www/ there.

Zerops injects env vars as OS env vars. Do NOT create .env files — empty values shadow OS vars.

Cross-service wiring: ${hostname_varname} in zerops.yaml run.envVariables.

import.yaml service level: envSecrets ONLY (not envVariables — silently dropped by API).

Shared secrets (encryption keys, CSRF tokens): MUST be project-level, not per-service envSecrets.

Migrations: zsc execOnce ${appVersionId} -- <command> in initCommands.

Sessions: external store (Valkey, database) when running multiple containers.



Nest.js minimal on Zerops

Gotchas


No .env files on Zerops — Zerops injects all environment variables as OS-level env vars. Creating a .env file with empty values will shadow the platform-injected values, causing silent connection failures. Delete any .env file from your deploy.

TypeORM synchronize: true in production — never use synchronize: true in production as it auto-modifies the schema on every startup. Use a separate migration script executed via initCommands with zsc execOnce to ensure safe, one-time schema changes per deploy.

NestJS listens on localhost by default — the app.listen(port) call without an explicit host binds to 127.0.0.1. On Zerops, the L7 balancer routes to the container's VXLAN IP, so you must explicitly pass '0.0.0.0' as the second argument or the container returns 502.

ts-node needs devDependencies — the dev setup uses npx ts-node for migration/seed scripts, which requires TypeScript and ts-node in node_modules. The dev buildCommands uses npm install (not npm ci --omit=dev) specifically for this reason.


1. Adding zerops.yaml

The main configuration file — place at your repository root. It tells Zerops how to build, deploy and run your NestJS app.



zerops:
  # Production setup — compile TypeScript, deploy only the
  # compiled output with production dependencies.
  - setup: prod
    build:
      base: nodejs@22

      buildCommands:
        # npm ci locks to package-lock.json for reproducible
        # builds — safer than npm install in CI/CD pipelines.
        - npm ci
        - npm run build
        # Strip devDependencies (TypeScript, testing, linting)
        # after compilation — runtime needs only production deps.
        - npm prune --omit=dev

      deployFiles:
        - ./dist          # compiled JS output
        - ./node_modules  # production dependencies only
        - ./package.json

      # Cache node_modules between builds to skip re-downloading
      # unchanged packages on every build trigger.
      cache:
        - node_modules

    # Readiness check — L7 balancer holds traffic until the
    # new container responds, preventing requests to containers
    # that are still initializing TypeORM or running migrations.
    deploy:
      readinessCheck:
        httpGet:
          port: 3000
          path: /api/health

    run:
      base: nodejs@22

      # Migrations and seeding run once per deploy version —
      # execOnce gates concurrent containers so only one runs
      # the command while others wait for completion.
      # --retryUntilSuccessful handles the brief window when
      # the database port is not yet accepting connections.
      initCommands:
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- node dist/migrate.js
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- node dist/seed.js

      ports:
        - port: 3000
          httpSupport: true

      envVariables:
        NODE_ENV: production
        # Cross-service references — resolved at container start
        # from the 'db' service's auto-generated credentials.
        DB_NAME: ${db_dbName}
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}

      start: node dist/main.js

      # Health check restarts unresponsive containers — catches
      # process hangs and lost database connections that would
      # otherwise serve errors indefinitely.
      healthCheck:
        httpGet:
          port: 3000
          path: /api/health

  # Development setup — deploy full source for interactive
  # development via SSH. Container stays idle until the
  # developer starts the app manually.
  - setup: dev
    build:
      base: nodejs@22

      buildCommands:
        # npm install (not ci) — works without a lock file,
        # giving flexibility during early development.
        - npm install

      # Deploy the entire working directory — source, config,
      # and node_modules with devDependencies included.
      deployFiles: ./

      cache:
        - node_modules

    run:
      base: nodejs@22
      # Ubuntu provides richer tooling (apt, curl, git, vim)
      # for interactive development sessions via SSH.
      os: ubuntu

      # Migrate and seed on every deploy — execOnce ensures
      # each command runs only once per deploy version even
      # when multiple containers exist.
      initCommands:
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- npx ts-node src/migrate.ts
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- npx ts-node src/seed.ts

      ports:
        - port: 3000
          httpSupport: true

      envVariables:
        NODE_ENV: development
        # Same cross-service references as prod — only the
        # mode flag (NODE_ENV) differs between setups.
        DB_NAME: ${db_dbName}
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}

      # Container stays idle — SSH in and run:
      #   npm run start:dev  (NestJS watch mode with hot-reload)
      # or
      #   npm run start      (single run via nest start)
      start: zsc noop --silent

2. Trust proxy and bind 0.0.0.0

Zerops terminates SSL at its L7 balancer and forwards requests via reverse proxy. Without proxy trust, Express (under NestJS) misreports req.ip and req.protocol. Binding localhost causes 502 errors because the L7 balancer routes to the container's VXLAN IP.



// in src/main.ts
const expressApp = app.getHttpAdapter().getInstance();
expressApp.set('trust proxy', true);
await app.listen(process.env.PORT ?? 3000, '0.0.0.0');

3. Database configuration via environment variables

NestJS + TypeORM reads database credentials from environment variables injected by Zerops at container start. Never create .env files — they shadow OS-level vars with empty values.



// in src/app.module.ts — TypeOrmModule.forRoot({...})
host: process.env.DB_HOST,
port: parseInt(process.env.DB_PORT, 10),
username: process.env.DB_USER,
password: process.env.DB_PASS,
database: process.env.DB_NAME,
Zerops [zerops_knowledge]nestjs showcase features managed service…
OUT
[{"uri":"zerops://recipes/laravel-showcase","title":"Laravel showcase on Zerops","score":9,"snippet":"# Laravel showcase on Zerops\n\n## Gotchas\n\n- No .env file — Zerops injects environment variables as OS env vars. Creating a .env file with empty values shadows the OS vars, causing env() to return null for every key that appears in .env even if the platform has a value set.\n- Cache..."},{"uri":"zerops://themes/services","title":"Managed Service Reference","score":9,"snippet":"# Managed Service Reference\n\nReference cards for all 14 Zerops managed services. Each card provides type, ports, env vars, wiring templates, connection pattern, HA specifics, and gotchas.\n\n## Wiring Syntax\n\n- Hostname substitution: In templates below, each service uses a sample hostname (e.g.,..."},{"uri":"zerops://themes/core","title":"Zerops YAML Reference","score":7,"snippet":"...     # default LIGHT\n  envVariables: map\u003cstring,string\u003e     # project-level vars\n  tags: string[]\n\nservices[]:                            # REQUIRED\n  hostname: string                     # REQUIRED, max 40, a-z and 0-9 ONLY (no hyphens/underscores), IMMUTABLE\n  type: \u003cruntime\u003e@\u003cversion\u003e          ..."},{"uri":"zerops://recipes/zerops-showcase","title":"Zerops showcase on Zerops","score":6,"snippet":"# Zerops showcase on Zerops\n\n## 1. Adding zerops.yaml\nThe main application configuration file you place at the root of your repository, it tells Zerops how to build, deploy and run your application.\n\n```yaml\nzerops:\n  # Production setup — build React frontend + bundle\n  # Bun backend into..."},{"uri":"zerops://guides/networking","title":"Networking on Zerops","score":5,"snippet":"...infrastructure:\n- Private VXLAN network — isolated overlay network shared by all services\n- L7 HTTP Balancer — 2 HA containers, auto-scales, domain routing + SSL\n- L3 Core Balancer — IP addresses and direct port access (TCP/UDP)\n\n---\n\n## Internal Networking..."}]

Remote Control failed to connect: disabled by your organization's policy

Zerops [zerops_knowledge]
OUT
Platform Constraints

Non-negotiable rules. Violating any causes failures.



MUST bind 0.0.0.0 (not localhost). L7 LB routes to container VXLAN IP. Binding localhost = 502.

Internal traffic = plain HTTP. NEVER https:// between services.

MUST trust proxy headers. Configure the framework's proxy trust setting if it has one (L7 LB terminates SSL and forwards via reverse proxy).

Deploy = new container. deployFiles MANDATORY — without it, run container starts empty.

Build and Run = SEPARATE containers. deployFiles = the ONLY bridge.

run.prepareCommands runs BEFORE deploy files arrive. Never reference /var/www/ there.

Zerops injects env vars as OS env vars. Do NOT create .env files — empty values shadow OS vars.

Cross-service wiring: ${hostname_varname} in zerops.yaml run.envVariables.

import.yaml service level: envSecrets ONLY (not envVariables — silently dropped by API).

Shared secrets (encryption keys, CSRF tokens): MUST be project-level, not per-service envSecrets.

Migrations: zsc execOnce ${appVersionId} -- <command> in initCommands.

Sessions: external store (Valkey, database) when running multiple containers.



Laravel showcase on Zerops

Gotchas


No .env file — Zerops injects environment variables as OS env vars. Creating a .env file with empty values shadows the OS vars, causing env() to return null for every key that appears in .env even if the platform has a value set.

Cache commands in initCommands, not buildCommands — config:cache, route:cache, and view:cache bake absolute paths into their cached files. The build container runs at /build/source/ while the runtime serves from /var/www/. Caching during build produces paths like /build/source/storage/... that crash at runtime with "directory not found."

APP_KEY is project-level — Laravel's encryption key must be shared across all services that read the same database (app + worker both need the same key for sessions and encrypted columns). Set it once at project level in Zerops; do not add it per-service or in zerops.yaml envVariables.

PDO PostgreSQL extension — The php-nginx base image includes pdo_pgsql out of the box. No prepareCommands or apk add needed for PostgreSQL connectivity.

Predis over phpredis — The php-nginx base image does not include the phpredis C extension. Use the predis/predis Composer package and set REDIS_CLIENT=predis to avoid "class Redis not found" errors.

Object storage requires path-style — Zerops object storage uses MinIO, which requires AWS_USE_PATH_STYLE_ENDPOINT=true. Without it, the SDK attempts virtual-hosted bucket URLs that MinIO cannot resolve.


1. Adding zerops.yaml

The main configuration file — place at repository root. It tells Zerops how to build, deploy and run your app.



zerops:
  # Production — optimized build, compiled assets, framework caches,
  # full service connectivity (DB, Redis, S3, Meilisearch).
  - setup: prod
    build:
      # Multi-base build: PHP for Composer, Node for Vite asset
      # compilation. Both runtimes are fully available on PATH
      # during the build — no manual install needed.
      base:
        - php@8.4
        - nodejs@22
      buildCommands:
        # Production Composer install — no dev packages, classmap
        # optimized for faster autoloading in production.
        - composer install --no-dev --optimize-autoloader
        # Vite compiles Tailwind CSS and JS into content-hashed
        # bundles in public/build/. These static assets are all
        # the runtime container needs from the Node side.
        - npm install
        - npm run build
      deployFiles:
        # List each directory explicitly — deploying ./ would
        # ship node_modules, .env.example, and other build-only
        # artifacts the runtime container doesn't need.
        - app
        - bootstrap
        - config
        - database
        - public
        - resources/views
        - routes
        - storage
        - vendor
        - artisan
        - composer.json
      # Cache vendor/ and node_modules/ between builds so
      # Composer and npm skip redundant network fetches.
      cache:
        - vendor
        - node_modules

    # Readiness check gates the traffic switch — new containers
    # must answer HTTP 200 before the L7 balancer routes to them.
    # This enables zero-downtime deploys.
    deploy:
      readinessCheck:
        httpGet:
          port: 80
          path: /health

    run:
      # php-nginx serves via Nginx + PHP-FPM — no explicit start
      # command needed; the base image handles both processes.
      base: php-nginx@8.4
      # Nginx serves static files from public/ and proxies PHP
      # requests to FPM. Laravel expects this as its web root.
      documentRoot: public
      # Config, route, and view caches MUST be built at runtime.
      # Build runs at /build/source/ but the app serves from
      # /var/www/ — caching during build bakes wrong paths.
      #
      # Migrations run exactly once per deploy via zsc execOnce,
      # regardless of how many containers start in parallel.
      # Seeder populates sample data on first deploy so the
      # dashboard shows real records immediately.
      # Scout import rebuilds the Meilisearch index from DB data
      # after seeding — the safety net for when auto-indexing
      # fires zero events (records already exist from prior deploy).
      initCommands:
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- php artisan migrate --force
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- php artisan db:seed --force
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- php artisan scout:import "App\\Models\\Article"
        - php artisan config:cache
        - php artisan route:cache
        - php artisan view:cache
      # Health check restarts unresponsive containers after the
      # 5-minute retry window expires — keeps production alive.
      healthCheck:
        httpGet:
          port: 80
          path: /health
      envVariables:
        APP_NAME: "Laravel Zerops"
        # Production mode — stack traces hidden, error pages
        # generic, optimizations enabled.
        APP_ENV: production
        APP_DEBUG: "false"
        # APP_URL drives absolute URL generation for redirects,
        # signed URLs, mail links, and CSRF origin validation.
        # zeropsSubdomain is the platform-injected HTTPS URL.
        APP_URL: ${zeropsSubdomain}
        # Stderr logging sends output to Zerops runtime log
        # viewer — no log files to manage or rotate.
        LOG_CHANNEL: stderr
        LOG_LEVEL: warning
        # Cross-service references resolve at deploy time.
        # Pattern: ${hostname_varname} maps to the db service's
        # auto-generated credentials.
        DB_CONNECTION: pgsql
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_DATABASE: ${db_dbName}
        DB_USERNAME: ${db_user}
        DB_PASSWORD: ${db_password}
        # Valkey (Redis-compatible) for cache, sessions, and
        # queues — single service handles all three concerns.
        # predis client is a pure-PHP Redis client that needs
        # no compiled extension.
        REDIS_CLIENT: predis
        REDIS_HOST: ${redis_hostname}
        REDIS_PORT: ${redis_port}
        SESSION_DRIVER: redis
        CACHE_STORE: redis
        QUEUE_CONNECTION: redis
        # S3-compatible object storage backed by MinIO.
        # forcePathStyle is mandatory — MinIO does not support
        # virtual-hosted bucket addressing.
        FILESYSTEM_DISK: s3
        AWS_ACCESS_KEY_ID: ${storage_accessKeyId}
        AWS_SECRET_ACCESS_KEY: ${storage_secretAccessKey}
        AWS_DEFAULT_REGION: us-east-1
        AWS_BUCKET: ${storage_bucketName}
        AWS_ENDPOINT: ${storage_apiUrl}
        AWS_USE_PATH_STYLE_ENDPOINT: "true"
        # Meilisearch for full-text search via Laravel Scout.
        # The host uses internal HTTP — SSL is terminated at
        # the L7 balancer, not between services.
        SCOUT_DRIVER: meilisearch
        MEILISEARCH_HOST: http://${search_hostname}:${search_port}
        MEILISEARCH_KEY: ${search_masterKey}
        # Mail set to log driver — no external SMTP configured.
        # Replace with real SMTP credentials for production use.
        MAIL_MAILER: log

  # Dev — full source deployed for live editing via SSHFS.
  # PHP-FPM serves requests immediately; edit files in /var/www
  # and changes take effect on the next request — no restart.
  - setup: dev
    build:
      # Same multi-base as prod — both PHP and Node available
      # during the build so Composer and npm can run.
      base:
        - php@8.4
        - nodejs@22
      buildCommands:
        # Full Composer install with dev packages — testing and
        # debugging tools available over SSH.
        - composer install
        # Pre-populate node_modules so the developer can run
        # npm run dev (Vite HMR) immediately after SSH-ing in
        # without waiting for another install.
        - npm install
      # Deploy the entire working directory — source files,
      # vendor/, node_modules/, and zerops.yaml so zcli push
      # works from the dev container.
      deployFiles:
        - ./
      cache:
        - vendor
        - node_modules

    run:
      base: php-nginx@8.4
      documentRoot: public
      # Install Node on the runtime container so the developer
      # can run Vite dev server (npm run dev) over SSH. This
      # runs once and is cached into the runtime image — not
      # re-executed on every container restart.
      prepareCommands:
        - sudo -E zsc install nodejs@22
      # Migration + seed runs once per deploy — DB is ready
      # when the SSH session starts. No cache warming in dev
      # — we want config changes to take effect immediately.
      initCommands:
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- php artisan migrate --force
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- php artisan db:seed --force
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- php artisan scout:import "App\\Models\\Article"
      envVariables:
        APP_NAME: "Laravel Zerops"
        # Dev mode — detailed error pages with stack traces,
        # no config caching, verbose logging for debugging.
        APP_ENV: local
        APP_DEBUG: "true"
        APP_URL: ${zeropsSubdomain}
        # Debug-level stderr logging surfaces all framework
        # events in the Zerops log viewer.
        LOG_CHANNEL: stderr
        LOG_LEVEL: debug
        # Same service wiring as prod — only mode flags differ.
        DB_CONNECTION: pgsql
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_DATABASE: ${db_dbName}
        DB_USERNAME: ${db_user}
        DB_PASSWORD: ${db_password}
        REDIS_CLIENT: predis
        REDIS_HOST: ${redis_hostname}
        REDIS_PORT: ${redis_port}
        SESSION_DRIVER: redis
        CACHE_STORE: redis
        QUEUE_CONNECTION: redis
        FILESYSTEM_DISK: s3
        AWS_ACCESS_KEY_ID: ${storage_accessKeyId}
        AWS_SECRET_ACCESS_KEY: ${storage_secretAccessKey}
        AWS_DEFAULT_REGION: us-east-1
        AWS_BUCKET: ${storage_bucketName}
        AWS_ENDPOINT: ${storage_apiUrl}
        AWS_USE_PATH_STYLE_ENDPOINT: "true"
        SCOUT_DRIVER: meilisearch
        MEILISEARCH_HOST: http://${search_hostname}:${search_port}
        MEILISEARCH_KEY: ${search_masterKey}
        MAIL_MAILER: log

  # Worker — background job processor consuming from Redis queue.
  # Same codebase as the app, different entry point. No HTTP
  # traffic — no healthCheck, readinessCheck, or documentRoot.
  - setup: worker
    build:
      # Worker only needs PHP — no asset compilation. The queue
      # runner processes jobs, not HTTP requests with CSS/JS.
      base:
        - php@8.4
      buildCommands:
        - composer install --no-dev --optimize-autoloader
      deployFiles:
        - app
        - bootstrap
        - config
        - database
        - public
        - resources/views
        - routes
        - storage
        - vendor
        - artisan
        - composer.json
      cache:
        - vendor

    run:
      # php-nginx base provides the PHP runtime. The queue:work
      # command runs as the foreground process instead of FPM.
      base: php-nginx@8.4
      # artisan queue:work processes jobs from the Redis queue.
      # --sleep=3 polls every 3s when idle, --tries=3 retries
      # failed jobs before marking them as permanently failed.
      start: php artisan queue:work --sleep=3 --tries=3
      # Cache framework config on every container start so the
      # worker resolves env vars and service references correctly.
      initCommands:
        - php artisan config:cache
      envVariables:
        APP_NAME: "Laravel Zerops"
        APP_ENV: production
        APP_DEBUG: "false"
        APP_URL: ${zeropsSubdomain}
        LOG_CHANNEL: stderr
        LOG_LEVEL: warning
        DB_CONNECTION: pgsql
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_DATABASE: ${db_dbName}
        DB_USERNAME: ${db_user}
        DB_PASSWORD: ${db_password}
        REDIS_CLIENT: predis
        REDIS_HOST: ${redis_hostname}
        REDIS_PORT: ${redis_port}
        # Worker shares the same Redis-backed drivers as the app.
        # Sessions are configured but unused by the CLI process.
        SESSION_DRIVER: redis
        CACHE_STORE: redis
        QUEUE_CONNECTION: redis
        FILESYSTEM_DISK: s3
        AWS_ACCESS_KEY_ID: ${storage_accessKeyId}
        AWS_SECRET_ACCESS_KEY: ${storage_secretAccessKey}
        AWS_DEFAULT_REGION: us-east-1
        AWS_BUCKET: ${storage_bucketName}
        AWS_ENDPOINT: ${storage_apiUrl}
        AWS_USE_PATH_STYLE_ENDPOINT: "true"
        SCOUT_DRIVER: meilisearch
        MEILISEARCH_HOST: http://${search_hostname}:${search_port}
        MEILISEARCH_KEY: ${search_masterKey}
        MAIL_MAILER: log

2. Trust the reverse proxy

Zerops terminates SSL at its L7 balancer and forwards requests via reverse proxy. Without trusting the proxy, Laravel rejects CSRF tokens and generates http:// URLs instead of https://. In bootstrap/app.php:



->withMiddleware(function (Middleware $middleware): void {
    $middleware->trustProxies(at: '*');
})

3. Configure Redis client

Laravel defaults to the phpredis C extension. On Zerops, the predis pure-PHP client avoids needing a compiled extension. Install via Composer and set REDIS_CLIENT=predis in your environment:



composer require predis/predis

4. Configure S3 object storage

Install the S3 Flysystem adapter and set FILESYSTEM_DISK=s3 with the Zerops object storage credentials. Path-style endpoints are mandatory for the MinIO-backed storage:



composer require league/flysystem-aws-s3-v3

5. Configure Meilisearch search

Install Laravel Scout with the Meilisearch driver for full-text search. Add the Searchable trait to models you want indexed:



composer require laravel/scout meilisearch/meilisearch-php
Remote Control failed to connect: disabled by your organization's policy

Zerops [zerops_knowledge]
OUT
Service Stacks (live)

[B]=also usable as build.base in zerops.yaml


Runtime: docker@26.1 | runtime | go@1 [B] | nginx@1.22 | static | java@{17,21} | bun@{canary,nightly,1.1.34,1.2,1.3} | deno@{1,2} | elixir@1.16 | gleam@1.5 | nodejs@{18,20,22,24} [B] | python@{3.11,3.12,3.14} [B] | php-apache@{8.1,8.3,8.4,8.5} | php-nginx@{8.1,8.3,8.4,8.5} | ubuntu@{22.04,24.04} [B] | alpine@{3.17,3.18,3.19,3.20,3.21,3.22,3.23} | dotnet@{10,6,7,8,9} [B] | rust@{nightly,stable} | ruby@{3.2,3.3,3.4} | zcp@1
Managed: mariadb@10.6 | postgresql@{14,16,17,18} | keydb@6 | valkey@7.2 | qdrant@{1.10,1.12} | nats@{2.10,2.12} | kafka@3.9 | elasticsearch@{8.16,9.2} | typesense@{27.1,30.2} | meilisearch@{1.10,1.20} | clickhouse@25.3
Shared storage: shared-storage
Object storage: object-storage
Build-only: nodejs@16 | php@{7.4,8.0,8.1,8.3} | python@{3.10,3.8,3.9} | rust@1


Node.js Hello World on Zerops

1. Adding zerops.yaml

The main application configuration file you place at the root of your repository, it tells Zerops how to build, deploy and run your application.



zerops:
  # Production setup — compile TypeScript to JS, deploy
  # compiled artifacts with production dependencies only.
  - setup: prod
    build:
      base: nodejs@22

      buildCommands:
        # npm ci installs exact versions from package-lock.json
        # for reproducible, auditable production builds.
        - npm ci
        - npm run build
        # Strip dev-only packages (TypeScript, ts-node, type
        # definitions) after compilation — runtime only needs
        # production dependencies.
        - npm prune --omit=dev

      deployFiles:
        - ./dist          # compiled JS (index.js + migrate.js)
        - ./node_modules  # production dependencies only
        - ./package.json

      # Cache node_modules between builds to avoid re-downloading
      # unchanged packages on every build trigger.
      cache:
        - node_modules

    # Readiness check: verifies new containers respond at /
    # before the project balancer routes traffic to them.
    # Prevents requests reaching containers still starting up.
    deploy:
      readinessCheck:
        httpGet:
          port: 3000
          path: /

    run:
      base: nodejs@22

      # Run migration once per deploy across all containers.
      # initCommands (not buildCommands) keeps migration and code
      # deployment atomic — a failed deploy won't leave a migrated
      # schema paired with old application code.
      # --retryUntilSuccessful handles the brief window when the
      # database port isn't yet accepting connections after import.
      initCommands:
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- node dist/migrate.js

      ports:
        - port: 3000
          httpSupport: true

      envVariables:
        NODE_ENV: production
        # Cross-service references — ${hostname_key} resolves to the
        # value generated by the 'db' service at container start.
        DB_NAME: ${db_dbName}
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}

      start: node dist/index.js

      # Health check restarts unresponsive containers after the
      # retry window expires — keeps production alive when the
      # process hangs or the database connection is lost.
      healthCheck:
        httpGet:
          port: 3000
          path: /

      verticalAutoscaling:
        # V8 GC needs headroom for traffic spikes — reserve ~50%
        # of minRam as free RAM to prevent OOM restarts.
        minRam: 0.25
        minFreeRamGB: 0.125

  # Development setup — deploy full source for interactive
  # development via SSH. The container stays idle (zsc noop)
  # so the developer controls what runs.
  - setup: dev
    build:
      base: nodejs@22

      buildCommands:
        # npm install (not npm ci) — works without a lock file,
        # giving flexibility during early development stages.
        - npm install

      # Deploy the entire working directory — source code,
      # node_modules (with devDependencies), and config files.
      deployFiles: ./

      cache:
        - node_modules

    run:
      base: nodejs@22
      # Ubuntu provides richer tooling (apt, curl, git, vim)
      # for interactive development via SSH.
      os: ubuntu

      # Migration runs on every container start — execOnce
      # ensures it only executes once per deploy version even
      # when multiple containers are running.
      initCommands:
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- npx ts-node src/migrate.ts

      ports:
        - port: 3000
          httpSupport: true

      envVariables:
        NODE_ENV: development
        DB_NAME: ${db_dbName}
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}

      # Container stays idle — developer SSHs in and runs:
      #   npm run dev   (ts-node hot-reload via nodemon)
      # or
      #   npm start     (plain ts-node)
      start: zsc noop --silent

2. Trust proxy and bind 0.0.0.0

Zerops terminates SSL at its L7 balancer and forwards requests via reverse proxy. Without trusting the proxy, Express misreports req.ip and req.protocol. Binding localhost causes 502 errors because the L7 balancer routes to the container's VXLAN IP.



app.set('trust proxy', true);
app.listen(port, '0.0.0.0');


Matching Recipes

If you are using any of these frameworks, load the recipe NOW — it contains required secrets, scaffolding, and gotchas:



analog-ssr-hello-world

analog-static-hello-world

astro-ssr-hello-world

astro-static-hello-world

nestjs-minimal

nextjs-ssr-hello-world

nextjs-static-hello-world

nodejs-hello-world

nuxt-ssr-hello-world

nuxt-static-hello-world

qwik-city-ssr-hello-world

qwik-city-static-hello-world

react-router-ssr-hello-world

react-router-static-hello-world

react-static-hello-world

svelte-hello-world

sveltekit-ssr-hello-world

sveltekit-static-hello-world



Service Cards

PostgreSQL

Type: postgresql (check live stacks for versions) | Mode: optional (default NON_HA), immutable
Ports: 5432 (RW), 5433 (read replicas, HA only), 6432 (external TLS via pgBouncer)
Env: hostname, port, portTls, user, password, connectionString, connectionTlsString, dbName, superUser, superUserPassword | HA-only: portReplicas, connectionStringReplicas
HA: 1 primary + 2 read replicas, streaming replication (async), auto-failover
Gotchas: No internal TLS (only 6432). Don't modify zps user. Read replicas have async lag. Some libs need postgres:// scheme. superUser (always postgres) for plugin installation.
Wiring (sample hostname: db):
VARS: DB_HOST: db DB_PORT: ${db_port} DB_NAME: ${db_dbName}
SECRETS: DATABASE_URL: postgresql://${db_user}:${db_password}@db:${db_port}/${db_dbName}


Valkey

Type: valkey (check live stacks for versions; MUST NOT use v8 — passes validation but fails import) | Mode: optional (default NON_HA), immutable
Use for: cache + sessions ONLY. Do NOT use Valkey as a queue broker for Zerops showcases — the canonical queue broker is NATS (see nats below and choose-queue decision). Using Valkey for queues is a legacy polymorphism pattern (one service wearing three hats); the showcase tier separates concerns explicitly. Exception: Laravel Horizon, Rails Sidekiq, Django+Celery-with-Redis — frameworks with a first-class Redis-bound queue library can keep their queue on Valkey, BUT the showcase still provisions a NATS broker as a separate queue service for the messaging feature section on the dashboard.
Ports: 6379 (RW), 6380 (RW TLS), 7000 (RO, HA only), 7001 (RO TLS, HA only)
Env: hostname, port, connectionString, portTls — NO user or password (unauthenticated)
HA: 1 master + 2 replicas. Zerops-specific: ports 6379/6380 on replicas forward to master (NOT native Valkey). Async replication.
Gotchas: MUST NOT use v8 (passes validation but fails import — check live stacks for valid versions). No authentication — connection is redis://hostname:6379 without credentials. Do NOT reference ${cache_user} or ${cache_password} — they don't exist. Port forwarding is Zerops-specific. Use 7000/7001 for direct read scaling. TLS ports for external/VPN only.
Wiring (sample hostname: cache):
VARS: REDIS_URL: redis://cache:${cache_port}


NATS

Type: nats (check live stacks for versions) | Mode: optional (default NON_HA), immutable
Use for: messaging / queue broker for every showcase recipe. NATS is the canonical queue service for Zerops showcases — the queue target in the showcase service list. It is a dedicated broker, NOT a generic KV store or cache substitute. Pub/sub for fan-out, JetStream for persistent queues with delivery guarantees. Workers subscribe to subjects; dashboards publish test messages. The NATS connection is framework-agnostic, which is why it's a better default than a language-bound queue library.
Canonical hostname: queue (literal) — keeps env var references readable: ${queue_hostname}, ${queue_port}, ${queue_user}, ${queue_password}. Do not name it nats in the showcase target list even though the type is nats@2.12.
Ports: 4222 (client), 8222 (HTTP monitoring)
Env: hostname, user (always zerops), password, connectionString
Config: JET_STREAM_ENABLED (default 1), MAX_PAYLOAD (default 8 MB, max 64 MB)
Gotchas: Config changes require restart. JetStream HA sync lag 1 min. Set JET_STREAM_ENABLED=0 for core pub-sub only.
Wiring (sample hostname: queue) — two supported patterns, pick ONE:


Pattern A — separate env vars (recommended; works with every NATS client library):



envVariables:
  NATS_HOST: ${queue_hostname}
  NATS_PORT: ${queue_port}
  NATS_USER: ${queue_user}
  NATS_PASS: ${queue_password}

Client code passes host+port as the servers list and user+pass as connect options:



import { connect } from "nats";
const nc = await connect({
  servers: `${process.env.NATS_HOST}:${process.env.NATS_PORT}`,
  user: process.env.NATS_USER,
  pass: process.env.NATS_PASS,
});

Pattern B — opaque connection string (compact; the platform builds a correctly-formatted URL):



envVariables:
  NATS_URL: ${queue_connectionString}

Client code passes it directly:



const nc = await connect({ servers: process.env.NATS_URL });

Gotcha — authorization violation from manual URL composition: do NOT hand-compose a URL like nats://${queue_user}:${queue_password}@${queue_hostname}:${queue_port}. Most NATS client libraries will parse the embedded credentials AND separately attempt SASL with the same values, producing a double-auth attempt the NATS server rejects with Authorization Violation on the first CONNECT frame. The symptom is a startup crash with NatsError: 'Authorization Violation' and no successful subscription. Use Pattern A or Pattern B above — they both avoid the double-auth path. Pattern A by passing credentials as connect options with a credential-free URL, Pattern B by letting the platform-generated connectionString carry exactly the auth shape the NATS server expects.


Meilisearch

Type: meilisearch (check live stacks for versions) | Mode: optional (NON_HA only)
Ports: 7700
Env: hostname, masterKey, defaultSearchKey, defaultAdminKey
Gotchas: No HA (single-node only). Never expose masterKey to frontend -- use defaultSearchKey.
Wiring (sample hostname: search):
VARS: MEILI_HOST: http://search:7700
SECRETS: MEILI_MASTER_KEY: ${search_masterKey}


Object Storage

Type: object-storage or objectstorage (both valid, no version) | Mode: optional (default NON_HA)
Env: apiUrl, apiHost, accessKeyId, secretAccessKey, bucketName, quotaGBytes, projectId, serviceId, hostname
Config: objectStorageSize: 1-100 GB, objectStoragePolicy or objectStorageRawPolicy, priority: 10
Infrastructure: runs on independent infra separate from other project services -- accessible from any Zerops service or remotely over internet
Bucket: one auto-created per service (name = hostname + random prefix, immutable). Need multiple buckets? Create multiple object-storage services
Policies: private | public-read (list+get) | public-objects-read (get only, no listing) | public-write (put only) | public-read-write (full). Or use objectStorageRawPolicy with IAM Policy JSON ({{ .BucketName }} template variable available)
Endpoint — use apiUrl (carries the full https:// scheme), not apiHost: ${storage_apiUrl} resolves to a complete https://... URL ready to hand to any S3 SDK's endpoint option. ${storage_apiHost} is host-only (no scheme) — if you manually prepend http:// to it you get an endpoint the object-storage gateway rejects and 301-redirects to the https:// equivalent, which most S3 SDKs don't follow automatically (symptom: UnknownError / connection-refused on the first bucket call). Either pass ${storage_apiUrl} directly, or if the S3 client library you're using needs host-only as a separate field, combine https://${storage_apiHost} — never http://.
Gotchas: MinIO backend. No Zerops backup. forcePathStyle: true / AWS_USE_PATH_STYLE_ENDPOINT: true REQUIRED. Region us-east-1 REQUIRED but NOT auto-generated — every S3 SDK needs it, MinIO ignores the value. No autoscaling, no verticalAutoscaling. Quota changeable in GUI after creation
Wiring (sample hostname: storage):
VARS: S3_ENDPOINT: ${storage_apiUrl} S3_BUCKET: ${storage_bucketName} + a region env var set to us-east-1 (use whichever name the framework's S3 client reads — NOT auto-generated by platform)
SECRETS: S3_KEY: ${storage_accessKeyId} S3_SECRET: ${storage_secretAccessKey}
REQUIRED: forcePathStyle: true / AWS_USE_PATH_STYLE_ENDPOINT: true (MinIO backend)



Wiring Patterns


Hostname substitution: In templates below, each service uses a sample hostname (e.g., db, cache, search). Replace it with your actual service hostname. The syntax ${hostname_varname} is real Zerops cross-service reference syntax — hostname must match the target service hostname exactly, with dashes converted to underscores.

Reference: ${hostname_variablename} — dashes in hostnames become underscores

envSecrets (import.yaml or GUI): injected directly as OS env vars — the app reads them via getenv() without any wiring. Do NOT re-reference envSecrets in zerops.yaml run.envVariables — ${MY_SECRET} is NOT a valid reference (it becomes a literal string). The ${...} syntax is ONLY for cross-service references. Changes to envSecrets require a service restart to take effect.

import.yaml service level: ONLY envSecrets and dotEnvSecrets exist. There is NO envVariables at service level (only at project level). Use envSecrets only for generated secrets (<@generateRandomString(...)>) and real credentials.

Hostname = DNS: use hostname directly for host (db, NOT ${db_hostname}), but use ${db_port} for port

Internal: ALWAYS http:// — NEVER https:// (SSL at L7 balancer)

Project vars: auto-inherited by all services — do NOT re-reference (creates shadow)

Password sync: changing DB password in GUI does NOT update env vars (manual sync)


Wire credentials in zerops.yaml run.envVariables — Managed services auto-generate credentials but they are NOT automatically available to runtime services. Wire them via run.envVariables in zerops.yaml (the deploy-time config). Use import.yaml envSecrets ONLY for generated secrets like <@generateRandomString(...)>:



# zerops.yaml — wire cross-service references here
zerops:
  - setup: myapp
    run:
      envVariables:
        DB_HOST: mydb
        DB_PORT: ${mydb_port}
        DB_NAME: ${mydb_dbName}
        DB_USER: ${mydb_user}
        DB_PASSWORD: ${mydb_password}


# import.yaml — only generated secrets here
services:
  - hostname: mydb
    type: mariadb@{version}
    mode: NON_HA
    priority: 10

  - hostname: myapp
    type: nodejs@22
    envSecrets:
      APP_SECRET: <@generateRandomString(<32>)>

Without zerops.yaml wiring, the runtime service has no way to connect to managed services.


Decision Hints


Choose Database: Use PostgreSQL for everything unless you have a specific reason not to. It's the best-supported database on Zerops with full HA, read replicas, and pgBouncer.

Choose Cache: Use Valkey. KeyDB development has stalled and is effectively deprecated on Zerops.

Choose Queue: Use NATS for most cases (simple, fast, JetStream persistence). Use Kafka only for enterprise event streaming with guaranteed ordering and unlimited retention.

Choose Search: Use Meilisearch for simple full-text search. Use Elasticsearch for advanced queries or HA requirements. Use Qdrant for vector/AI search.



Version Check


✓ nodejs@22

✓ postgresql@16

✓ valkey@7.2

✓ nats@2.10

⚠ meilisearch@1 not found. Available: meilisearch@1.10, meilisearch@1.20

✓ object-storage

Zerops [zerops_knowledge]mailpit utility service recipe buildFrom…
OUT
[{"uri":"zerops://guides/production-checklist","title":"Production Checklist for Zerops","score":5,"snippet":"...for Zerops\n\nBefore going to production: (1) databases to HA mode, (2) minContainers: 2 on app services, (3) replace Mailpit with real SMTP, (4) remove Adminer, (5) use Object Storage for uploads, (6) use Redis/Valkey for sessions.\n\n## Database\n\n| Item | Dev | Production..."},{"uri":"zerops://themes/core","title":"Zerops YAML Reference","score":5,"snippet":"...generation reference: import.yaml and zerops.yaml schemas, rules, pitfalls, and complete multi-service examples.\n\n---\n\n## import.yaml Schema\n\n```\nproject:                               # OPTIONAL (omit in ZCP context)\n  name: string                         # REQUIRED if project: exists\n ..."},{"uri":"zerops://guides/smtp","title":"SMTP on Zerops","score":4,"snippet":"# SMTP on Zerops\n\nOnly port 587 (STARTTLS) is allowed for outbound email — ports 25 and 465 are permanently blocked. Use an external email service.\n\n## Port Configuration\n| Port | Status | Protocol |\n|------|--------|----------|\n| 25 | Blocked | Traditional SMTP (spam risk) |\n| 465 |..."}]

Zerops [zerops_knowledge]mailpit showcase mail service utility bu…
OUT
[{"uri":"zerops://themes/core","title":"Zerops YAML Reference","score":9,"snippet":"# Zerops YAML Reference\n\nYAML generation reference: import.yaml and zerops.yaml schemas, rules, pitfalls, and complete multi-service examples.\n\n---\n\n## import.yaml Schema\n\n```\nproject:                               # OPTIONAL (omit in ZCP context)\n  name: string                         # REQUIRED..."},{"uri":"zerops://recipes/laravel-showcase","title":"Laravel showcase on Zerops","score":7,"snippet":"# Laravel showcase on Zerops\n\n## Gotchas\n\n- No .env file — Zerops injects environment variables as OS env vars. Creating a .env file with empty values shadows the OS vars, causing env() to return null for every key that appears in .env even if the platform has a value set.\n- **Cache..."},{"uri":"zerops://themes/services","title":"Managed Service Reference","score":6,"snippet":"# Managed Service Reference\n\nReference cards for all 14 Zerops managed services. Each card provides type, ports, env vars, wiring templates, connection pattern, HA specifics, and gotchas.\n\n## Wiring Syntax\n\n- Hostname substitution: In templates below, each service uses a sample hostname (e.g.,..."}]

Zerops [zerops_knowledge]
OUT
Available Service Stacks (live)

Runtime: docker@26.1 | runtime | go@1 | nginx@1.22 | static | java@{17,21} | bun@{canary,nightly,1.1.34,1.2,1.3} | deno@{1,2} | elixir@1.16 | gleam@1.5 | nodejs@{18,20,22,24} | python@{3.11,3.12,3.14} | php-apache@{8.1,8.3,8.4,8.5} | php-nginx@{8.1,8.3,8.4,8.5} | ubuntu@{22.04,24.04} | alpine@{3.17,3.18,3.19,3.20,3.21,3.22,3.23} | dotnet@{10,6,7,8,9} | rust@{nightly,stable} | ruby@{3.2,3.3,3.4} | zcp@1
Managed: mariadb@10.6 | postgresql@{14,16,17,18} | keydb@6 | valkey@7.2 | qdrant@{1.10,1.12} | nats@{2.10,2.12} | kafka@3.9 | elasticsearch@{8.16,9.2} | typesense@{27.1,30.2} | meilisearch@{1.10,1.20} | clickhouse@25.3
Shared storage: shared-storage
Object storage: object-storage



Zerops Platform Model

How Zerops works — the mental model for understanding all Zerops configuration.


Zerops runs Linux containers (Incus) in VXLAN private networks. Build and run are separate containers — deployFiles is the only bridge. Three storage types: container disk, shared storage (NFS), object storage (S3/MinIO).


Container Universe

Everything on Zerops runs in full Linux containers (Incus, not Docker). Each container has:



Full SSH access, working directory /var/www

Connected via VXLAN private network (per project)

Addressable by service hostname (internal DNS)

Own disk (persistent, grow-only)


Hierarchy: Project > Service > Container(s). One project = one isolated network. Services communicate by hostname over this network.


Two core plans govern project-level resource allowances:


Lightweight	Serious
Build time	15 hours	150 hours
Backup storage	5 GB	25 GB
Egress	100 GB	3 TB
Infrastructure	Single container	Multi-container (HA)

Upgrading from Lightweight to Serious costs $10 one-time, is irreversible, and causes approximately 35 seconds of network unavailability.


The Two YAML Files

File	Purpose	Scope
import.yaml	Topology -- WHAT exists	Services, types, versions, scaling, env vars
zerops.yaml	Lifecycle -- HOW it runs	Build, deploy, run commands per service

These are separate concerns. import.yaml creates infrastructure. zerops.yaml defines what happens when code is pushed. A service can exist (imported) without any code deployed yet.


Build/Deploy Lifecycle


Source Code
    |
+--------------------------------+
|  BUILD CONTAINER               |
|  - Starts with base image ONLY |
|  - prepareCommands: cached     |
|  - buildCommands: compile      |
|  - Output: deployFiles         |
+-----------+--------------------+
            | deployFiles = THE ONLY BRIDGE
            v
+--------------------------------+
|  RUN CONTAINER                 |
|  - Different base image possible|
|  - prepareCommands: run BEFORE |
|    deploy files arrive         |
|  - Deploy files land at /var/www|
|  - start: launches the app     |
+--------------------------------+

Phase ordering:



build.prepareCommands -- install tools, cached in base layer

build.buildCommands -- compile, bundle, test

build.deployFiles -- select artifacts to transfer

run.prepareCommands -- customize runtime image (runs BEFORE deploy files arrive!)

Deploy files arrive at /var/www

run.initCommands -- per-container-start tasks (migrations)

run.start -- launch the application


Critical: run.prepareCommands executes BEFORE deploy files are at /var/www. Do NOT reference /var/www/ paths in run.prepareCommands. Use build.addToRunPrepare to copy files to /home/zerops/, then reference /home/zerops/ in run.prepareCommands.


Environment Variable Activation

Source	When active as OS env vars
Platform-injected (hostname, serviceId, zeropsSubdomain)	At container start
project.envVariables (import.yaml)	At service creation, inherited by all services
run.envVariables (zerops.yaml)	Only after deploy processes zerops.yaml
envSecrets (import.yaml per-service)	At container start

A startWithoutCode service is RUNNING but has only platform and project-level vars. run.envVariables (cross-service references like ${hostname_varname}) do not exist as OS env vars until the zerops.yaml is deployed. Implicit-webserver types (php-nginx, php-apache) auto-serve from the filesystem — the app is reachable but runs without run.envVariables.


Networking


Internet -> L7 Load Balancer (SSL termination) -> container VXLAN IP:port -> app


L7 LB terminates SSL/TLS -- all internal traffic is plain HTTP

Valid port range: 10-65435 (80/443 reserved by Zerops for SSL termination; exception: PHP uses port 80)

Cloudflare SSL must be Full (strict) -- "Flexible" causes infinite redirect loops


Storage


Container disk: per-container, persistent, grow-only (auto-scaling only increases, never shrinks; to reduce: recreate service)

Shared storage: NFS mount at /mnt/{hostname}, POSIX-only, max 60 GB, SeaweedFS backend. Do NOT use for user uploads or frequently-written files -- use Object Storage instead. Shared storage is for cases requiring a shared POSIX filesystem (shared config, plugin directories)

Object storage: S3-compatible (MinIO backend), forcePathStyle: true REQUIRED, region us-east-1, one auto-named bucket per service (immutable name). Preferred for file uploads, media, and any high-throughput file operations


Scaling


Vertical: CPU (shared or dedicated), RAM (dual-threshold triggers), Disk (grow-only). Applies to runtimes AND managed services. Does NOT apply to shared-storage or object-storage

Horizontal: 1-10 containers for runtimes only. Managed services have fixed container counts: NON_HA=1, HA=3 -- do NOT set minContainers/maxContainers for managed services

HA mode (managed services): fixed 3 containers with master-replica topology, auto-failover. Container count is IMMUTABLE

Runtime services are always HA — the mode field on runtimes is accepted but forced to HA regardless of input. Runtime replica count is controlled via minContainers/maxContainers (not mode), and serves both throughput scaling AND HA/rolling-deploy availability — production runtimes typically want ≥2 even when a single container carries the load

Docker: fixed resources only (no min-max autoscaling), resource change triggers VM restart


Base Image Contract

Base	OS	Package Manager	Size	libc
Alpine (default)	Alpine Linux	apk add --no-cache	~5 MB	musl
Ubuntu	Ubuntu	sudo apt-get update && sudo apt-get install -y	~100 MB	glibc

NEVER cross them: apt-get on Alpine -> "command not found". apk on Ubuntu -> "command not found".


Build containers run as user zerops with sudo access.


Container Lifecycle


Deploy = new container. Local files LOST. Only deployFiles content survives.

Restart, reload, stop/start, vertical scaling = same container. Local files intact.

Persistent data: database, object storage, or shared storage. Never local filesystem for anything that must survive a deploy.

Sessions and cache: use external store (Valkey, database) when running multiple containers.


Immutable Decisions

These CANNOT be changed after creation — choose correctly or delete+recreate:



Hostname — becomes internal DNS name, max 40 chars, a-z and 0-9 only

Mode (HA/NON_HA) — determines node topology for managed services (1 vs 3 containers). Immutable.

Object storage bucket name — auto-generated from hostname + random prefix

Service type category — cannot change a runtime to a managed service or vice versa


Platform Constraints

Non-negotiable rules. Violating any causes failures.



MUST bind 0.0.0.0 (not localhost). L7 LB routes to container VXLAN IP. Binding localhost = 502.

Internal traffic = plain HTTP. NEVER https:// between services.

MUST trust proxy headers. Configure the framework's proxy trust setting if it has one (L7 LB terminates SSL and forwards via reverse proxy).

Deploy = new container. deployFiles MANDATORY — without it, run container starts empty.

Build and Run = SEPARATE containers. deployFiles = the ONLY bridge.

run.prepareCommands runs BEFORE deploy files arrive. Never reference /var/www/ there.

Zerops injects env vars as OS env vars. Do NOT create .env files — empty values shadow OS vars.

Cross-service wiring: ${hostname_varname} in zerops.yaml run.envVariables.

import.yaml service level: envSecrets ONLY (not envVariables — silently dropped by API).

Shared secrets (encryption keys, CSRF tokens): MUST be project-level, not per-service envSecrets.

Migrations: zsc execOnce ${appVersionId} -- <command> in initCommands.

Sessions: external store (Valkey, database) when running multiple containers.



Zerops YAML Reference

YAML generation reference: import.yaml and zerops.yaml schemas, rules, pitfalls, and complete multi-service examples.



import.yaml Schema


project:                               # OPTIONAL (omit in ZCP context)
  name: string                         # REQUIRED if project: exists
  corePackage: LIGHT | SERIOUS         # default LIGHT
  envVariables: map<string,string>     # project-level vars
  tags: string[]

services[]:                            # REQUIRED
  hostname: string                     # REQUIRED, max 40, a-z and 0-9 ONLY (no hyphens/underscores), IMMUTABLE
  type: <runtime>@<version>            # REQUIRED (100+ valid values)
  mode: HA | NON_HA                    # Defaults to NON_HA if omitted for managed services. IMMUTABLE
  priority: int                        # higher = starts first (DB=10, app=5)
  enableSubdomainAccess: bool          # zerops.app subdomain
  startWithoutCode: bool               # start without deploy (runtimes only)
  minContainers: 1-10                  # RUNTIME ONLY, default 1 (managed services have fixed containers)
  maxContainers: 1-10                  # RUNTIME ONLY (managed: NON_HA=1, HA=3, fixed)
  envSecrets: map<string,string>       # blurred in GUI by default, editable/deletable
  dotEnvSecrets: string                # .env format, auto-creates secrets
  # NOTE: envVariables does NOT exist at service level — only at project level
  # For non-secret env vars on a service, use zerops_env after import or zerops.yaml run.envVariables
  buildFromGit: url                    # one-time build from repo — use ONLY with verified URLs (utility recipes like mailpit). Do NOT guess URLs.
  objectStorageSize: 1-100             # GB, object-storage only (changeable in GUI later)
  objectStoragePolicy: private | public-read | public-objects-read | public-write | public-read-write
  objectStorageRawPolicy: string       # custom IAM Policy JSON (alternative to objectStoragePolicy)
  override: bool                       # re-imports existing service with same hostname (updates config, can activate READY_TO_DEPLOY services)
  mount: string[]                      # pre-configure shared storage connection (ALSO requires mount in zerops.yaml run section to activate)
  nginxConfig: string                  # custom nginx config for PHP/static/nginx services
  zeropsSetup: string                  # inline zerops.yaml setup name
  zeropsYaml: object                   # inline zerops.yaml configuration in import
  verticalAutoscaling:                 # RUNTIME + DB/CACHE ONLY (not shared-storage, not object-storage)
    cpuMode: SHARED | DEDICATED        # default SHARED
    minCpu/maxCpu: int                 # CPU threads
    startCpuCoreCount: int             # CPU at container start
    minRam/maxRam: float               # GB
    minFreeRamGB: float                # absolute free threshold
    minFreeRamPercent: float            # percentage free threshold
    minFreeCpuCores: float             # absolute free CPU threshold
    minFreeCpuPercent: float            # percentage free CPU threshold
    minDisk/maxDisk: float              # GB, disk never shrinks
    swapEnabled: bool                  # enable swap memory (safety net, default varies by service type)

Preprocessor Functions

Enable with #zeropsPreprocessor=on as first line. Syntax: <@function(<args>)>, chain modifiers with |: <@generateRandomString(<32>)|sha256>.


Functions:



<@generateRandomString(<len>)> -- random alphanumeric string

<@generateRandomBytes(<len>)> -- random bytes (binary)

<@generateRandomInt(<min>,<max>)> -- random integer in range

<@pickRandom(<opt1>,<opt2>,...)> -- pick random from options

<@setVar(<name>,<content>)> / <@getVar(<name>)> -- store and retrieve variables

<@generateRandomStringVar(<name>,<len>)> -- generate + store string variable

<@generateJWT(<secret>,<payload>)> -- JWT token generation

<@getDateTime(<format>,[<tz>])> -- formatted datetime

<@generateED25519Key(<name>)>, <@generateRSA2048Key(<name>)>, <@generateRSA4096Key(<name>)> -- key pairs (stores pubKey/privKey)


Modifiers (applied with |): sha256, sha512, bcrypt, argon2id (hashing) | toHex, toString (encoding) | upper, lower, title (case) | noop (testing)


Rules: Functions return strings. Two-phase processing: preprocessing then YAML parsing. Values generated once at import -- fixed after, not regenerated. Escape special characters: \<, \>, \| (double-escape \\ for backslash)


Always-available ${...} functions: ${random(length)}, ${randomInt(min,max)}, ${sha256(value)}, ${bcrypt(value,rounds)}, ${argon2id(value)}, ${jwt(algo,secret,payload)}, ${generateRSAKeyPair(bits)}, ${generateEd25519KeyPair()}


WARNING: API dryRun validates YAML schema only -- it does NOT enforce service-type restrictions (e.g., minContainers on managed services passes dry-run but fails real import). The rules in this document ARE the validation layer.



zerops.yaml Schema


zerops[]:
  setup: string                        # REQUIRED, matches service hostname
  build:
    base: string | string[]            # runtime(s) -- multi-base: [php@8.4, nodejs@22]
    os: alpine | ubuntu                # default alpine
    prepareCommands: string[]          # cached in base layer
    buildCommands: string[]            # runs every build
    deployFiles: string | string[]     # MANDATORY -- nothing auto-deploys
    cache: bool | string | string[]    # paths to cache
    addToRunPrepare: string | string[] # copy files from build to /home/zerops/ in prepare container
    envVariables: map<string, string|number|bool>
  deploy:
    temporaryShutdown: bool            # false = zero-downtime (default)
    readinessCheck:                    # gates traffic switch
      httpGet: { port: int, path: string }
      exec: { command: string }
  run:
    base: string                       # if different from build base
    os: alpine | ubuntu
    start: string                      # REQUIRED (except implicit-webserver: php-nginx, php-apache, nginx, static)
    ports[]: { port: 10-65435, httpSupport: bool, protocol: tcp|udp }  # httpSupport: true = receives HTTP via L7 LB (REQUIRED for web); false = raw TCP/UDP only
    initCommands: string[]             # every container start (migrations, seeding)
    prepareCommands: string[]          # runtime image customization
    documentRoot: string               # webserver runtimes only (PHP/Nginx/Static)
    healthCheck: { httpGet | exec }    # 2xx or exit 0, 5-min retry window
    envVariables: map<string, string|number|bool>
    crontab[]: { timing: cron, command: string, allContainers: bool }
    routing: { cors, redirects[], headers[] }
    mount: string[]                    # shared storage hostnames to mount at /mnt/{hostname} (REQUIRED for storage access at runtime)
    startCommands[]: { command, name, workingDir, initCommands[] }


Provision Rules

Import & Service Creation


ALWAYS set explicit mode: NON_HA or mode: HA for managed services (DB, cache, shared-storage). Mode defaults to NON_HA if omitted. Set HA explicitly for production. IMMUTABLE

NEVER set mode for runtime services. REASON: mode is only for managed services. Runtime HA uses minContainers: 2+ — replica count serves both throughput AND HA/rolling-deploy availability (a single replica drops traffic on every deploy or crash), so prod tiers usually want ≥2 even when a single container carries the load

NEVER set minContainers/maxContainers for managed services. REASON: managed services have fixed container counts (NON_HA=1, HA=3); setting these causes import failure

NEVER set verticalAutoscaling for shared-storage or object-storage. REASON: these service types don't support vertical scaling; setting it causes import failure

ALWAYS set priority: 10 for databases/storage services. REASON: ensures they start before application services that depend on them

ALWAYS set enableSubdomainAccess: true in import.yaml AND call zerops_subdomain action="enable" once after the first deploy of each new service. REASON: the import flag marks intent; the subdomain API call activates the L7 route

ALWAYS prefer the highest available version from the live catalog for each service type, unless a specific version is required for compatibility. REASON: new projects should start on the latest stable release; older versions exist for migration/compatibility but should not be chosen by default. Example: if postgresql@{14,16,17,18} are available, use postgresql@18.

ALWAYS use valkey@7.2 (not valkey@8). REASON: v8 passes dry-run validation but fails actual import

NEVER use Docker :latest tag. REASON: cached and won't re-pull; always use specific version tags

ALWAYS use --network=host for Docker services. REASON: without it, container cannot receive traffic from Zerops routing

ALWAYS use forcePathStyle: true / AWS_USE_PATH_STYLE_ENDPOINT: true for Object Storage. REASON: MinIO backend doesn't support virtual-hosted style

ALWAYS set a region env var to us-east-1 for Object Storage clients. REASON: Zerops does NOT auto-generate a region variable, but every S3 SDK requires one (MinIO ignores the value). Use whichever env var name the framework's S3 client reads for region


Import Generation (dev/stage patterns)


Standard mode: create dev/stage pairs for runtimes. Naming: {prefix}dev and {prefix}stage (e.g., appdev/appstage, apidev/apistage). Dev mode: single {prefix}dev. Simple mode: single {name} with real start command

ALWAYS set startWithoutCode: true ONLY on dev services (not stage). Simple mode: set on the single service. REASON: dev container starts RUNNING immediately but with only platform-injected vars — run.envVariables from zerops.yaml do not exist until first deploy. Stage stays in READY_TO_DEPLOY until code arrives

ALWAYS set maxContainers: 1 for dev services. REASON: dev uses SSHFS; multiple containers cause file conflicts

ONLY set zeropsSetup in import.yaml when using buildFromGit. REASON: zeropsSetup requires buildFromGit (API rejects one without the other). For workspace deploys (no buildFromGit), use zerops_deploy setup="..." parameter instead

ALWAYS set minRam high enough for initial RAM spikes (autoscaling has ~10-20s reaction time). Dev needs higher than stage/prod (compilation on container)

Activating READY_TO_DEPLOY services: re-import with override: true + startWithoutCode: true to transition the service to ACTIVE without deploying code first

ALWAYS use managed service hostname conventions: db, cache, queue, search, storage. REASON: standardizes cross-service references

Shared secrets (encryption/session keys): put in project.envVariables when multiple services in the same project share a database — they must share the key or encrypted data becomes unreadable across services. Use preprocessor: <@generateRandomString(<32>)>. Per-service secrets: put in service-level envSecrets. Determine which pattern applies based on what the framework uses the secret for (encryption = shared, API token = per-service).

ALWAYS use generic setup: names in zerops.yaml (dev, prod, worker). When deploying to a hostname that differs from the setup name, pass setup="..." to zerops_deploy. REASON: generic names work across all environments; zeropsSetup in recipe import.yaml + --setup in workspace deploy both handle the mapping

ALWAYS add run.healthCheck and deploy.readinessCheck ONLY to stage/prod entries, NEVER to dev. REASON: dev uses zsc noop --silent; healthCheck would restart the container during iteration

DEBUG DEPLOY_FAILED with empty runtime logs by temporarily removing deploy.readinessCheck and run.healthCheck from the setup, redeploying, then SSH-ing in and curling the health path directly (ssh {host} "curl -s http://localhost{path}"). REASON: the framework may be rendering a 500 error page with the full stack trace in the response body while writing nothing to stderr. With checks stripped, the container reaches ACTIVE and stays alive long enough to read the real error. Restore checks after fixing the bug.


Environment Variables — Three Levels

Where to put what:


What	Where	Why
Anything shared across services	project.envVariables in import.yaml	Auto-inherited by every service. Use for shared config, shared secrets (with preprocessor), or any value that must be identical across services (e.g. encryption keys when sharing a DB). Do NOT re-reference in zerops.yaml (creates shadow).
Cross-service wiring (DB creds, cache host)	run.envVariables in zerops.yaml	${hostname_varname} references resolve at deploy time. This is the ONLY place cross-service refs work.
Per-service secrets (unique to one service)	envSecrets per-service in import.yaml	Blurred in GUI. Auto-injected as OS vars — do NOT re-reference in zerops.yaml.

How they work:



project.envVariables (import.yaml): inherited by all services in the project. Use for any value that should be the same everywhere — shared config, shared secrets (with <@generateRandomString(...)>), feature flags, etc. Changes via GUI, no redeploy needed.

run.envVariables (zerops.yaml): become OS env vars only after deploy — NOT present on startWithoutCode containers before first deploy. Support ${hostname_varname} cross-service references. Changes take effect on next deploy.

envSecrets (import.yaml per-service, or GUI): injected directly as OS env vars at container start. Changes require a service restart (not just redeploy).


Critical rules:



${...} syntax is ONLY for cross-service references in run.envVariables (${db_hostname}). Writing MY_SECRET: ${MY_SECRET} does NOT reference the envSecret — it creates a literal string.

import.yaml service level: ONLY envSecrets and dotEnvSecrets. No envVariables at service level (project-level only).

Managed services auto-generate credentials (hostname, port, user, password, dbName, connectionString) — do NOT set these in import.yaml.

zeropsSubdomain: platform-injected full HTTPS URL (e.g. https://app-1df2-3000.prg1.zerops.app), created when enableSubdomainAccess: true.

Self-URL variable: most frameworks have an env var that controls absolute URL generation (redirects, signed URLs, mail links, CSRF origin validation). Set it to ${zeropsSubdomain} in run.envVariables so the framework generates correct public URLs. Without it, the framework defaults to localhost and any feature producing absolute URLs breaks silently.


Hostname & Port Conventions


ALWAYS use port range 10-65435 when declaring run.ports in zerops.yaml. REASON: ports outside this range are reserved by the platform.

Service hostnames are IMMUTABLE — they become internal DNS names. Pick conventional names up front (app, db, cache, queue, search, storage). Lowercase alphanumeric only, no hyphens or underscores, max 40 chars.


Scaling & Platform


NEVER attempt to change HA/NON_HA mode after creation. REASON: mode is immutable; must delete and recreate service

NEVER attempt to change hostname after creation. REASON: hostname is immutable; it becomes the internal DNS name

NEVER expect disk to shrink. REASON: auto-scaling only increases disk; to reduce, recreate the service



Generate Rules

Build & Deploy

Where to run what (lifecycle-phase decision matrix):


Command runs during	Lives in	Working dir	Access	Use for
Build image customization (once)	build.prepareCommands	/build/source/ (build container)	Build container only — no services, no runtime env vars	Install OS packages, language extensions, or tools the build needs BEFORE buildCommands run. Cached in build base layer — only re-runs when prepareCommands change.
Build pipeline (every build)	build.buildCommands	/build/source/ (build container)	No deploy files at runtime paths, no services, no env vars from zerops.yaml run section	Dependency install, compilation, bundling, asset build, linting. Output goes to deployFiles.
Runtime image customization (once)	run.prepareCommands	/home/zerops/ (prepare phase, runtime container)	Deploy files NOT yet at /var/www/	Install OS packages, configure runtime. Use addToRunPrepare to ship build artifacts here.
Every container start	run.initCommands	/var/www/ (runtime container)	Full deploy files, env vars, cross-service connectivity	DB migrations (gate with zsc execOnce), framework cache warmup (config:cache, cache:warmup, etc.), anything that needs absolute paths under /var/www/ or DB access

Rule of thumb: if a command reads from /var/www/, writes a cache that will be used at runtime, or needs DB/service connectivity — it belongs in run.initCommands, NOT buildCommands.



ALWAYS include node_modules in deployFiles for Node.js apps (unless bundled). REASON: runtime container doesn't run npm install

ALWAYS deploy fat/uber JARs for Java. REASON: build and run are separate containers; thin JARs lose their dependencies

ALWAYS use Maven/Gradle wrapper (./mvnw, ./gradlew) or install build tools via prepareCommands. REASON: build container has JDK only -- Maven, Gradle are NOT pre-installed

NEVER reference /var/www/ in run.prepareCommands. REASON: deploy files arrive AFTER prepareCommands execute; /var/www is empty during prepare

ALWAYS use addToRunPrepare + /home/zerops/ path for files needed in run.prepareCommands. REASON: this is the only way to get files from build into the prepare phase

NEVER use initCommands for package installation. REASON: initCommands run on every container restart; use prepareCommands for one-time setup

NEVER compile/cache configuration that embeds absolute paths during buildCommands. REASON: build container runs at /build/source/, runtime runs at /var/www/. Config caches that freeze paths like storage_path(), base_path(), Rails.root, __dir__ at build time point to /build/source/... at runtime — first access crashes with "directory not found / permission denied". Move these to run.initCommands (framework examples: Laravel artisan config:cache|route:cache|view:cache, Rails assets:precompile if paths leak, Symfony cache:warmup)

PREFER run.initCommands over buildCommands for any step that: reads from /var/www/, writes runtime-resolvable caches, or needs database access. REASON: buildCommands run in an isolated build container without access to runtime filesystem, deploy files, or services. initCommands run per-container-start with the full runtime environment (deploy files at /var/www/, env vars, cross-service connectivity). Use zsc execOnce ${appVersionId} -- <cmd> to gate DB migrations so one container runs them while others wait

ALWAYS use --no-cache-dir for pip in containers. REASON: prevents wasted disk space on ephemeral containers

NEVER bypass package-manager platform checks to work around missing build dependencies. REASON: suppressing validation hides real problems that crash at runtime. Instead, install missing system packages or extensions in build.prepareCommands so the package manager validates everything properly. Build and runtime are separate containers — if a dependency is needed in both, install in both build.prepareCommands and run.prepareCommands


Base Image & OS


NEVER use apt-get on Alpine. REASON: Alpine uses apk; apt-get doesn't exist

NEVER use apk on Ubuntu. REASON: Ubuntu uses apt-get; apk doesn't exist

ALWAYS use sudo apk add --no-cache on Alpine. REASON: prevents stale package index caching; sudo required as containers run as zerops user

ALWAYS use sudo apt-get update && sudo apt-get install -y on Ubuntu. REASON: package index not pre-populated; sudo required as containers run as zerops user

NEVER set run.base: alpine@* for Go. REASON: causes glibc/musl mismatch for CGO-linked binaries -> 502. Omit run.base or use run.base: go@latest

ALWAYS use os: ubuntu for Deno and Gleam. REASON: these runtimes are not available on Alpine


Build & Runtime


ALWAYS build compiled languages (Rust, Go, Java, .NET) with release/optimized flags for production. REASON: debug builds are dramatically slower and larger

ALWAYS use CGO_ENABLED=0 go build when unsure about CGO dependencies. REASON: produces static binary compatible with any container base (avoids glibc/musl mismatch)

ALWAYS bind 0.0.0.0, not localhost or 127.0.0.1. Many frameworks default to localhost — override in config or env var. REASON: L7 LB routes to container VXLAN IP; localhost binding = 502

ALWAYS configure the framework's proxy trust setting (if it has one). REASON: Zerops L7 balancer terminates SSL and forwards via reverse proxy — frameworks that validate origin/CSRF headers will reject requests unless they trust the proxy. Check the framework's documentation for the specific setting

NEVER listen on port 443 or 80 (exception: PHP uses 80). REASON: Zerops reserves 80/443 for SSL termination. Use 3000, 8080, etc.

ALWAYS set Cloudflare SSL to "Full (strict)" when using Cloudflare proxy. REASON: "Flexible" causes infinite redirect loops


Environment Variables — Three Levels

Where to put what:


What	Where	Why
Anything shared across services	project.envVariables in import.yaml	Auto-inherited by every service. Use for shared config, shared secrets (with preprocessor), or any value that must be identical across services (e.g. encryption keys when sharing a DB). Do NOT re-reference in zerops.yaml (creates shadow).
Cross-service wiring (DB creds, cache host)	run.envVariables in zerops.yaml	${hostname_varname} references resolve at deploy time. This is the ONLY place cross-service refs work.
Per-service secrets (unique to one service)	envSecrets per-service in import.yaml	Blurred in GUI. Auto-injected as OS vars — do NOT re-reference in zerops.yaml.

How they work:



project.envVariables (import.yaml): inherited by all services in the project. Use for any value that should be the same everywhere — shared config, shared secrets (with <@generateRandomString(...)>), feature flags, etc. Changes via GUI, no redeploy needed.

run.envVariables (zerops.yaml): become OS env vars only after deploy — NOT present on startWithoutCode containers before first deploy. Support ${hostname_varname} cross-service references. Changes take effect on next deploy.

envSecrets (import.yaml per-service, or GUI): injected directly as OS env vars at container start. Changes require a service restart (not just redeploy).


Critical rules:



${...} syntax is ONLY for cross-service references in run.envVariables (${db_hostname}). Writing MY_SECRET: ${MY_SECRET} does NOT reference the envSecret — it creates a literal string.

import.yaml service level: ONLY envSecrets and dotEnvSecrets. No envVariables at service level (project-level only).

Managed services auto-generate credentials (hostname, port, user, password, dbName, connectionString) — do NOT set these in import.yaml.

zeropsSubdomain: platform-injected full HTTPS URL (e.g. https://app-1df2-3000.prg1.zerops.app), created when enableSubdomainAccess: true.

Self-URL variable: most frameworks have an env var that controls absolute URL generation (redirects, signed URLs, mail links, CSRF origin validation). Set it to ${zeropsSubdomain} in run.envVariables so the framework generates correct public URLs. Without it, the framework defaults to localhost and any feature producing absolute URLs breaks silently.


Deploy Semantics



Without tilde: dist -> /var/www/dist/ (directory preserved)




Tilde syntax: dist/~ -> contents extracted to /var/www/ (directory stripped)




All files land under /var/www




INVARIANT: run.start path MUST match where deployFiles places files:



deployFiles: [dist] + start: bun dist/index.js -- CORRECT (file at /var/www/dist/index.js)

deployFiles: dist/~ + start: bun index.js -- CORRECT (file at /var/www/index.js)

deployFiles: dist/~ + start: bun dist/index.js -- BROKEN (no /var/www/dist/ exists)




Git required: zerops_deploy uses zcli push which requires a git repository




Self-deploy with [.]: When switching from a recipe's production deployFiles to [.], build output stays in its original directory under /var/www/ instead of being extracted/flattened. The start command must reference the full path:



Recipe uses dist/~ + start: bun index.js → with [.]: start: bun dist/index.js (files at /var/www/dist/)

Recipe uses ./app + start: ./app → with [.]: same start: ./app (binary at /var/www/app)

Recipe uses target/release/~binary + start: ./binary → with [.]: start: ./target/release/binary

Principle: tilde extraction no longer happens, directory structure is preserved as-is. Match start to where build output actually lands.




.deployignore: Place at repo root (gitignore syntax) to exclude files/folders from deploy artifact. NOT recursive into subdirectories by default. Recommended to mirror .gitignore patterns. Also works with zcli service deploy.




Deploy mode determines deployFiles:


Deploy mode	Who deploys?	deployFiles	start
Dev (in dev+stage)	Self-deploy	[.]	zsc noop --silent (implicit-webserver: omit)
Stage (in dev+stage)	Cross-deploy from dev	Recipe pattern	Compiled/prod start
Simple (single service)	Self-deploy	[.]	Real start command
Production (buildFromGit)	Platform from git	Recipe pattern	Compiled/prod start

Self-deploy with specific paths (e.g., [app], dist/~) destroys source files + zerops.yaml after deploy, making iteration impossible. Only cross-deploy targets and git-based builds can use specific paths safely.





Runtime Rules

Cache Architecture (Two-Layer)


Base layer: OS + prepareCommands (invalidated only when prepareCommands change)

Build layer: buildCommands output (invalidated every build)

cache: false only affects /build/source -- modules elsewhere remain cached


Public Access


Shared IPv4: free, HTTP/HTTPS only, requires BOTH A and AAAA DNS records

Dedicated IPv4: $3/30 days, all protocols

IPv6: free, dedicated per project

zerops.app subdomain: 50MB limit, not production


zsc Commands


zsc execOnce <key> -- <cmd>: run once across all containers (HA-safe migrations)

zsc add <runtime>@<version>: install additional runtime in prepareCommands

Zerops [zerops_knowledge]mailpit utility buildFromGit zeropsSetup…
OUT
[{"uri":"zerops://themes/core","title":"Zerops YAML Reference","score":4,"snippet":"...non-secret env vars on a service, use zerops_env after import or zerops.yaml run.envVariables\n  buildFromGit: url                    # one-time build from repo — use ONLY with verified URLs (utility recipes like mailpit). Do NOT guess URLs.\n  objectStorageSize: 1-100             # GB,..."},{"uri":"zerops://guides/production-checklist","title":"Production Checklist for Zerops","score":3,"snippet":"...going to production: (1) databases to HA mode, (2) minContainers: 2 on app services, (3) replace Mailpit with real SMTP, (4) remove Adminer, (5) use Object Storage for uploads, (6) use Redis/Valkey for sessions.\n\n## Database\n\n| Item | Dev | Production |\n|------|-----|------------|\n| Mode |..."},{"uri":"zerops://guides/smtp","title":"SMTP on Zerops","score":3,"snippet":"# SMTP on Zerops\n\nOnly port 587 (STARTTLS) is allowed for outbound email — ports 25 and 465 are permanently blocked. Use an external email service.\n\n## Port Configuration\n| Port | Status | Protocol |\n|------|--------|----------|\n| 25 | Blocked | Traditional SMTP (spam risk) |\n| 465 |..."},{"uri":"zerops://guides/cloudflare","title":"Cloudflare Integration with Zerops","score":2,"snippet":"...    CNAME _acme-challenge.\u003cdomain\u003e \u003cdomain\u003e.zerops.zone\n```\n\n## SSL/TLS Settings (Cloudflare Dashboard)\n- Encryption mode: Full (strict) — mandatory\n- Never use "Flexible" — causes infinite redirect loops\n- Enable "Always Use HTTPS"\n- WAF exception: Skip rule for..."},{"uri":"zerops://recipes/laravel-showcase","title":"Laravel showcase on Zerops","score":2,"snippet":"...containers start in parallel.\n      # Seeder populates sample data on first deploy so the\n      # dashboard shows real records immediately.\n      # Scout import rebuilds the Meilisearch index from DB data\n      # after seeding — the safety net for when auto-indexing\n      # fires zero events..."}]

Zerops [zerops_knowledge]showcase tier targets features plan reci…
OUT
[{"uri":"zerops://recipes/laravel-showcase","title":"Laravel showcase on Zerops","score":10,"snippet":"# Laravel showcase on Zerops\n\n## Gotchas\n\n- No .env file — Zerops injects environment variables as OS env vars. Creating a .env file with empty values shadows the OS vars, causing env() to return null for every key that appears in .env even if the platform has a value set.\n- **Cache..."},{"uri":"zerops://recipes/zerops-showcase","title":"Zerops showcase on Zerops","score":6,"snippet":"# Zerops showcase on Zerops\n\n## 1. Adding zerops.yaml\nThe main application configuration file you place at the root of your repository, it tells Zerops how to build, deploy and run your application.\n\nyaml\nzerops:\n  # Production setup — build React frontend + bundle\n  # Bun backend into..."},{"uri":"zerops://themes/services","title":"Managed Service Reference","score":6,"snippet":"...mydb\n    type: mariadb@{version}\n    mode: NON_HA\n    priority: 10\n\n  - hostname: myapp\n    type: nodejs@22\n    envSecrets:\n      APP_SECRET: \u003c@generateRandomString(\u003c32\u003e)\u003e\n\n\nWithout zerops.yaml wiring, the runtime service has no way to connect to managed services.\n\n## Service Wiring..."},{"uri":"zerops://themes/core","title":"Zerops YAML Reference","score":5,"snippet":"...url                    # one-time build from repo — use ONLY with verified URLs (utility recipes like mailpit). Do NOT guess URLs.\n  objectStorageSize: 1-100             # GB, object-storage only (changeable in GUI later)\n  objectStoragePolicy: private | public-read | public-objects-read..."},{"uri":"zerops://guides/production-checklist","title":"Production Checklist for Zerops","score":4,"snippet":"...| Structured (syslog) |\n| Debug mode | Enabled | Disabled |\n\nyaml\n- hostname: app\n  type: nodejs@22\n  minContainers: 2\n  maxContainers: 4\n\n\n## Dev Services to Remove\n\n### Mailpit → Production SMTP\n```yaml\n- hostname: mailpit\n  type: go@1\n  buildFromGit:..."}]

Zerops [zerops_knowledge]mailpit zerops yaml buildFromGit setup p…
OUT
[{"uri":"zerops://themes/core","title":"Zerops YAML Reference","score":10,"snippet":"# Zerops YAML Reference\n\nYAML generation reference: import.yaml and zerops.yaml schemas, rules, pitfalls, and complete multi-service examples.\n\n---\n\n## import.yaml Schema\n\n```\nproject:                               # OPTIONAL (omit in ZCP context)\n  name: string                         # REQUIRED..."},{"uri":"zerops://themes/operations","title":"Zerops Operations \u0026 Production","score":8,"snippet":"# Zerops Operations \u0026 Production\n\nOperational guides covering networking, CI/CD, logging, monitoring, scaling, and production hardening. Covers everything beyond core YAML configuration.\n\n## Networking\n\nEvery Zerops project gets an isolated VXLAN private network. Services discover each other by..."},{"uri":"zerops://guides/production-checklist","title":"Production Checklist for Zerops","score":7,"snippet":"# Production Checklist for Zerops\n\nBefore going to production: (1) databases to HA mode, (2) minContainers: 2 on app services, (3) replace Mailpit with real SMTP, (4) remove Adminer, (5) use Object Storage for uploads, (6) use Redis/Valkey for sessions.\n\n## Database\n\n| Item | Dev | Production..."}]

Zerops [zerops_knowledge]production checklist mailpit service imp…
OUT
[{"uri":"zerops://guides/production-checklist","title":"Production Checklist for Zerops","score":10,"snippet":"# Production Checklist for Zerops\n\nBefore going to production: (1) databases to HA mode, (2) minContainers: 2 on app services, (3) replace Mailpit with real SMTP, (4) remove Adminer, (5) use Object Storage for uploads, (6) use Redis/Valkey for sessions.\n\n## Database\n\n| Item | Dev | Production..."},{"uri":"zerops://themes/core","title":"Zerops YAML Reference","score":8,"snippet":"# Zerops YAML Reference\n\nYAML generation reference: import.yaml and zerops.yaml schemas, rules, pitfalls, and complete multi-service examples.\n\n---\n\n## import.yaml Schema\n\n```\nproject:                               # OPTIONAL (omit in ZCP context)\n  name: string                         # REQUIRED..."}]

Zerops [zerops_knowledge]mailpit go type subdomain dashboard SMTP…
OUT
[{"uri":"zerops://guides/cloudflare","title":"Cloudflare Integration with Zerops","score":4,"snippet":"...\u003cdomain\u003e \u003cproject-cname-target\u003e\n```\n\n### With Cloudflare Proxy (orange cloud)\n| IP Type | Record | Proxy |\n|---------|--------|-------|\n| IPv6 only | AAAA \u003cdomain\u003e \u003cipv6\u003e | Proxied |\n| Dedicated IPv4 | A \u003cdomain\u003e \u003cipv4\u003e | Proxied |\n| Shared IPv4 | Not recommended | Reverse..."},{"uri":"zerops://guides/production-checklist","title":"Production Checklist for Zerops","score":4,"snippet":"# Production Checklist for Zerops\n\nBefore going to production: (1) databases to HA mode, (2) minContainers: 2 on app services, (3) replace Mailpit with real SMTP, (4) remove Adminer, (5) use Object Storage for uploads, (6) use Redis/Valkey for sessions.\n\n## Database\n\n| Item | Dev | Production..."},{"uri":"zerops://themes/core","title":"Zerops YAML Reference","score":4,"snippet":"...                    # REQUIRED, max 40, a-z and 0-9 ONLY (no hyphens/underscores), IMMUTABLE\n  type: \u003cruntime\u003e@\u003cversion\u003e            # REQUIRED (100+ valid values)\n  mode: HA | NON_HA                    # Defaults to NON_HA if omitted for managed services. IMMUTABLE\n  priority: int             ..."}]

Bash Find v3 recipe engine plan doc
IN
find / -type f -name "plan.md" -path "*zcprecipator3*" 2>/dev/null | head -5

OUT
(Bash completed with no output)
Bash Locate recipe engine doc directory
IN
find / -type d -name "zcprecipator3" 2>/dev/null

OUT
(Bash completed with no output)
Bash List contents of working directory
IN
ls -la /var/www/ 2>/dev/null | head -30

OUT
total 60
drwxr-xr-x  6 zerops zerops    7 Apr 23 10:35 .
drwxr-xr-x 15 root   root     16 Apr 23 10:13 ..
drwxr-xr-x  2 zerops zerops    3 Apr 23 10:13 .claude
-rw-r--r--  1 zerops zerops 2249 Apr 23 10:13 CLAUDE.md
drwxr-xr-x  7 zerops zerops   10 Apr 23 10:13 .git
drwxr-xr-x  3 zerops zerops    3 Apr 23 10:35 recipes
drwxr-xr-x  2 zerops zerops    3 Apr 23 10:13 .vscode
Bash List recipe output directory
IN
ls -la /var/www/recipes/nestjs-showcase/ 2>/dev/null

OUT
total 1
drwxr-xr-x 2 zerops zerops 2 Apr 23 10:35 .
drwxr-xr-x 3 zerops zerops 3 Apr 23 10:35 ..
Zerops [zerops_workflow]
OUT
{"code":"INVALID_PARAMETER","error":"recipe workflow is not available on zerops_workflow","suggestion":"Call zerops_recipe action=start slug=\u003cslug\u003e outputRoot=\u003cdir\u003e instead. See the tool's description for the full action set."}
Bash Find markdown/json docs in working dir
IN
find /var/www -type f \( -name "*.md" -o -name "*.json" \) 2>/dev/null | grep -v node_modules | head -20

OUT
/var/www/CLAUDE.md
/var/www/.vscode/terminals.json
/var/www/.claude/settings.local.json
Zerops [zerops_recipe]
OUT
{"ok":true,"action":"build-brief","slug":"nestjs-showcase","brief":{"kind":"writer","body":"# Writer brief — nestjs-showcase\n\nRecipe: nestjs-showcase · Framework:  · Tier:  · Codebases: 0\n\n## Surface registry\n\n### ROOT_README — author=writer\n- FormatSpec: docs/spec-content-surfaces.md#surface-1--root-readme\n- Owns: README.md\n- FactHint: root-overview\n- Adjacent (cross-reference, don't duplicate): ENV_README\n\n# ROOT_README\n\n## Pass\n\n\n# ExampleFramework Minimal\n\nDeploy a minimal ExampleFramework backend on Zerops with PostgreSQL.\nSix environment tiers — AI-agent dev through HA production.\n\n- [Deploy tier 0 — AI Agent](./0%20—%20AI%20Agent/)\n- [Deploy tier 5 — HA Production](./5%20—%20Highly-available%20Production/)\n\n\n## Fail — narrates debugging instead of decision-helping\n\n\n# ExampleFramework Minimal\n\nIterated over several attempts. Initial versions failed because the\nframework binds 127.0.0.1 by default...\n\n\nFails "30-second decide": porter can't pick a tier or see services.\n\n### ENV_README — author=writer\n- FormatSpec: docs/spec-content-surfaces.md#surface-2--environment-readme\n- Owns: /README.md\n- FactHint: tier-promotion\n- Adjacent (cross-reference, don't duplicate): ROOT_README, ENV_IMPORT_COMMENTS\n\n# ENV_README\n\n## Pass\n\n\n# Tier 3 — Stage\n\n**Audience**: reviewer validating the prod build path before release.\n**Scale**: single replica, managed services NON_HA.\n\n**From tier 2**: services stop cross-linking to localhost; a\n`minFreeRamGB: 0.25` floor appears for OOM protection.\n\n**To tier 4**: production adds `minContainers: 2` for throughput.\nStage data is ephemeral.\n\n\n## Fail — boilerplate, no teaching\n\n\n# Tier 3 — Stage\n\nThis is the stage environment.\n\n- db\n- app\n\n\nFails "when do I outgrow": audience/scale/promotion absent. Target is\n40-80 lines of teaching.\n\n### ENV_IMPORT_COMMENTS — author=writer\n- FormatSpec: docs/spec-content-surfaces.md#surface-3--environment-importyaml-comments\n- Owns: /import.yaml\n- FactHint: tier-decision\n- Adjacent (cross-reference, don't duplicate): ENV_README, CODEBASE_ZEROPS_COMMENTS\n\n# ENV_IMPORT_COMMENTS\n\n## Pass — each block explains its own decision\n\nyaml\n# Tier 5 HA: two replicas behind the L7 balancer. minContainers: 2 gates\n# the rolling-deploy contract; cpuMode: DEDICATED removes shared-CPU\n# contention that's tolerable on stage but not prod.\n- hostname: app\n  mode: HA\n  minContainers: 2\n\n# Postgres HA: managed failover. ~2x stage spend buys ~3s failover —\n# the floor for customer-facing tiers.\n- hostname: db\n  mode: HA\n\n\n## Fail — templated phrase copy-pasted\n\nyaml\n# This service enables zero-downtime rolling deploys.\n- hostname: app\n# This service enables zero-downtime rolling deploys.\n- hostname: db\n\n\nFails "explain a decision": every comment is the same generic phrase.\n\n### CODEBASE_IG — author=writer\n- FormatSpec: docs/spec-content-surfaces.md#surface-4--per-codebase-readme-integration-guide-fragment\n- Owns: codebases//README.md#integration-guide\n- FactHint: porter-change\n- Adjacent (cross-reference, don't duplicate): CODEBASE_KB, CODEBASE_ZEROPS_COMMENTS\n\n# CODEBASE_IG — examples\n\n## Pass — concrete porter action + Zerops reason + diff\n\n\n### 4. Bind to 0.0.0.0\n\nZerops routes traffic to the container via the L7 balancer on the\ncontainer's VXLAN IP. Binding to `127.0.0.1` makes the app unreachable.\n\njs\n// server.js\n// Before: app.listen(3000, '127.0.0.1')\napp.listen(process.env.PORT, '0.0.0.0')\n\n\n\n## Fail — describes the recipe's own scaffold code\n\n\n### 4. Our api.ts wrapper\n\nThe recipe's `api.ts` helper centralizes fetch calls and handles SPA\nfallback detection. Import it from `./lib/api.ts` and use it for every\nrequest.\n\n\nFails the "porter bringing their own code" test: the porter has no\napi.ts. The underlying fact (how the SPA fallback breaks) may belong\nhere; the helper's existence belongs in code comments, not IG.\n\n### CODEBASE_KB — author=writer\n- FormatSpec: docs/spec-content-surfaces.md#surface-5--per-codebase-readme-knowledge-base--gotchas-fragment\n- Owns: codebases//README.md#gotchas\n- FactHint: platform-trap\n- Adjacent (cross-reference, don't duplicate): CODEBASE_IG\n\n# CODEBASE_KB\n\n## Pass — cites guide, names mechanism + symptom\n\n\n### Cross-service env vars auto-inject — never declare the alias\n\nZerops injects `db_hostname`, `db_user`, `db_password`, `db_port`\nproject-wide. Declaring `DB_HOST: ${db_hostname}` creates a self-shadow:\nthe platform copy and the alias collide, `DB_HOST` reads empty\n(symptom: connection timeout with blank host in the error).\n\nCite `env-var-model`. Fix: do not declare the alias; read\n`db_hostname` directly.\n\n\n## Fail — framework quirk disguised as gotcha\n\n\n### @Controller collides with setGlobalPrefix\n\n`@Controller('api/users')` + `app.setGlobalPrefix('api')` produces\n`/api/api/users`. Use one OR the other.\n\n\nZero Zerops involvement — belongs in framework docs, not a gotcha.\n\n### CODEBASE_CLAUDE — author=writer\n- FormatSpec: docs/spec-content-surfaces.md#surface-6--per-codebase-claudemd\n- Owns: codebases//CLAUDE.md\n- FactHint: operational\n- Adjacent (cross-reference, don't duplicate): CODEBASE_IG\n\n# CODEBASE_CLAUDE\n\n## Pass — operational, repo-local\n\n\n## Dev loop\n\nSSH into the dev container, then:\n\n    npm run start:dev       # hot-reload server\n    npm run migration:run   # one migration by hand\n    npm run dev:reset       # truncate + re-seed\n\n## Health\n\n- `GET /health` — 200 `{\"status\":\"ok\"}`\n- `GET /debug/remote-ip` — echoes X-Forwarded-For (preship check)\n\n## Container traps\n\n- SSHFS reuses uid 1001 — chown host-created files to 1001.\n\n\n## Fail — deploy steps on the wrong surface\n\n\n## Deploying to Zerops\n\n1. Push to GitHub\n2. Create a Zerops project\n3. Import the import.yaml\n\n\nReader has the repo checked out, not someone deploying.\n\n### CODEBASE_ZEROPS_COMMENTS — author=writer\n- FormatSpec: docs/spec-content-surfaces.md#surface-7--per-codebase-zeropsyaml-comments\n- Owns: codebases//zerops.yaml\n- FactHint: scaffold-decision\n- Adjacent (cross-reference, don't duplicate): ENV_IMPORT_COMMENTS, CODEBASE_IG\n\n# CODEBASE_ZEROPS_COMMENTS\n\n## Pass — explains trade-offs per field\n\nyaml\nrun:\n  # httpSupport: true registers with the L7 balancer. Without it the\n  # balancer won't route — subdomain returns 502.\n  ports:\n    - port: 3000\n      httpSupport: true\n\n  # execOnce + static key: platform records the key and skips re-runs.\n  # Pair with --retryUntilSuccessful so a transient DB outage doesn't\n  # permanently burn the key.\n  initCommands:\n    - zsc execOnce db-migrate-v1 -- npm run migration:run --retryUntilSuccessful\n\n\n## Fail — narrates the field name\n\nyaml\nrun:\n  # deployFiles: ./ ships the working tree\n  deployFiles: ./\n  # initCommands runs these on deploy\n  initCommands:\n    - zsc execOnce db-migrate-v1 -- npm run migration:run\n\n\nReader can read the field — comment must teach WHY ./ vs ./dist/~,\nor delete.\n\n# Citation topics — call zerops_knowledge first\n\nFrame with the cited guide's vocabulary. Inventing new mental models\nfor covered topics ships folk-doctrine.\n\n- Cross-service env vars, self-shadow — env-var-model\n- zsc execOnce, init commands — execOnce\n- Rolling deploys, SIGTERM, minContainers — rolling-deploys\n- Object storage, MinIO, forcePathStyle — object-storage\n- L7 balancer, httpSupport, trust proxy — http-support\n- envIsolation, project-level vars — env-var-model\n- Deploy files, tilde suffix, static base — deploy-files\n- Readiness / health — readiness-health-checks\n\nSilent-on-intersection is new content only if you read the guide first.\n\n# Completion payload\n\nReturn one JSON object with:\n\n- root_readme — recipe-root README body (surface 1)\n- env_readmes — map keyed \"0\"..\"5\" → README body (surface 2)\n- env_import_comments — map keyed \"0\"..\"5\" → {project, service:\n  {hostname → comment}} (surface 3)\n- codebase_readmes — map keyed hostname → {integration_guide,\n  gotchas} (surfaces 4 + 5)\n- codebase_claude — map keyed hostname → CLAUDE.md body (surface 6)\n- codebase_zerops_yaml_comments — map keyed hostname →\n  [{anchor, comment}]. Engine splices above the named field (surface 7).\n- citations — map topic → zerops_knowledge guide id. Required for\n  every platform-trap fact.\n- manifest — {surface_counts: {\u003csurface\u003e: int}, cross_references:\n  [{from, to}]}. Gate checks read this.\n\nReturn once — the engine's stitch-content handler writes into the\nfile tree. Writer-owned paths are locked at the engine boundary.\n\n","bytes":8547,"parts":["header","ROOT_README","ENV_README","ENV_IMPORT_COMMENTS","CODEBASE_IG","CODEBASE_KB","CODEBASE_CLAUDE","CODEBASE_ZEROPS_COMMENTS","briefs/writer/citation_topics.md","briefs/writer/completion_payload.md"]}}

Before I go further — creating a full NestJS showcase recipe through the v3 engine is a substantial multi-phase task:

Research — record the recipe plan (framework, tier, services, features)
Provision — actually create Zerops services in a real project (db, cache, storage, search, queue, mail, app, worker) — this reserves infrastructure
Scaffold — generate NestJS codebase with TypeORM, BullMQ or NATS client, S3 SDK, Meilisearch client, SMTP, sessions, UI dashboard
Feature — implement each showcase feature, deploy, and curl/browser verify
Finalize — emit 6 tiered import.yaml files (AI Agent → HA Production) plus all READMEs and CLAUDE.md
This will take considerable time and will create billable infrastructure in your Zerops project. A few things I want to confirm before committing:

Infrastructure scope — OK to create a full 8-service project (appdev/appstage + workerdev/workerstage + db + cache + storage + search + queue + mail) in your current Zerops workspace? Or should we first scaffold the recipe files locally and provision only to validate?
Feature set — the showcase needs to cover every managed service kind (db, cache, storage, search, queue, mail) plus api+ui. The typical NestJS stack is TypeORM + PostgreSQL, Valkey cache + sessions, NATS via @nestjs/microservices, Meilisearch, object-storage via AWS SDK, Mailpit + nodemailer. Any deviations you want (e.g., BullMQ instead of NATS, Prisma instead of TypeORM)?
UI choice — simple server-rendered dashboard (Handlebars / EJS via @nestjs/platform-express) keeps it single-codebase. Or do you want a separate SPA target?
If you'd prefer, I can also just stop after the research plan is recorded so you can review scope before committing to provisioning. Which way do you want me to go?
