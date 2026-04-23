create Nest.js showcase recipe
Zerops [zerops_recipe]
OUT
{"ok":true,"action":"start","slug":"nestjs-showcase","status":{"slug":"nestjs-showcase","current":"research","completed":[],"codebases":0,"services":0,"factsCount":0},"guidance":"# Research phase — classify framework, decide codebase shape, submit typed plan\n\nYour job in this phase: classify the framework and produce a typed Plan\ndescribing the recipe. No code written yet — this is the upstream\ndecision that every downstream phase depends on.\n\n## Classification — full-stack vs API-first\n\nApply this decision tree using your knowledge of the framework:\n\n\nDoes the framework have a built-in view/template engine designed for\nrendering HTML from the same process that handles routing?\n│\n├─ YES → full-stack. Single codebase (\"monolith\") serves HTML and API\n│        routes. Dashboard uses the framework's templates. Worker runs\n│        the same codebase in a different process model.\n│        Examples of shape: Laravel+Blade, Rails+ERB, Django+Jinja2,\n│        Phoenix+HEEx, ASP.NET+Razor, SvelteKit w/ server routes,\n│        Next.js w/ server routes.\n│\n└─ NO  → API-first. Framework serves JSON/plain text only. Dashboard\n         lives in a separate codebase (static frontend — default Svelte).\n         Worker lives in a third codebase OR shares the API's codebase\n         depending on the framework's process model (see below).\n         Examples of shape: NestJS, Express, Fastify, Hono, FastAPI,\n         Flask (API mode), Spring Boot (API mode), Go (chi/fiber/echo),\n         Rust (axum/actix), Phoenix as API-only.\n\n\nClassification rule of thumb: if the predecessor {framework}-minimal\n(or hello-world-{lang}) renders HTML via a framework-integrated\ntemplate engine, it's full-stack. If the predecessor returns JSON or\nplain text, it's API-first.\n\n## Codebase shape\n\nAfter classification, pick shape:\n\n- Shape 1 (monolith) — full-stack frameworks. One codebase owns\n  routes, views, and worker process. role=monolith.\n- Shape 2 (api + frontend, worker shares api) — API-first frameworks\n  whose queue library runs naturally as a sibling process of the API\n  (shared codebase, different zeropsSetup). Two codebases.\n  role=api, role=frontend. Worker declared with\n  isWorker=true, sharesCodebaseWith=\"\u003capi-hostname\u003e\".\n- Shape 3 (api + frontend + worker-separate) — API-first frameworks\n  whose worker process uses a first-class long-lived context\n  distinct from the API (e.g. NestJS createApplicationContext,\n  Express standalone worker). Three codebases. Worker is\n  isWorker=true, sharesCodebaseWith=\"\".\n\nHello-world and minimal tiers collapse to shape 1 regardless of framework\n— they prove the language+platform contract, not service fan-out. Shape\n2/3 only applies at showcase tier.\n\n## Default service set per tier\n\n- hello-world-{lang} — no managed services. Runtime only.\n- {framework}-minimal — framework + 1 database (PostgreSQL default).\n  Framework-idiomatic ORM + migrations + one CRUD endpoint.\n- {framework}-showcase — framework + db + cache + broker +\n  storage + search. Managed-service hostnames: db, cache,\n  broker, storage, search. Types: default PostgreSQL, Valkey,\n  NATS, Object Storage, Meilisearch. Mail (SMTP) is NOT part of the\n  showcase service set — Zerops customers use external SMTP providers.\n\nShowcase MUST NOT add services beyond this set without a signal in the\nparent recipe. Laravel-showcase's Mailpit is Laravel-specific and does\nnot transfer.\n\n## Frontend default (API-first only)\n\nWhen shape is 2 or 3, the frontend codebase defaults to Svelte\n(Vite) compiled to static assets. Rationale: smallest bundle, HTML-\nsuperset syntax, deploys on static runtime (pure Nginx) in prod,\nsingle npm ci \u0026\u0026 npm run build. Don't pick React/Vue/Angular unless\nthe user asked for one by name.\n\n## Parent recipe inheritance\n\nIf parent is populated in the session (the chain resolver found a\npublished parent), do NOT re-derive the parent's decisions:\n\n- Service hostnames + types: copy from parent, add showcase-new services.\n- Runtime type (e.g. nodejs@22): copy from parent, don't bump unless\n  the framework released a new stable that the parent pre-dates.\n- Codebase hostnames: preserve (showcase extends minimal, same names).\n- Gotchas / IG items: the writer cross-references the parent later;\n  don't plan to re-author.\n\nParent content is inlined in zerops_recipe action=start response under\nparent.codebases[].readme and parent.envImports[\"0\"]. Read it there\n— do not call zerops_knowledge with freeform queries for the parent.\n\n## Required output — submit via action=update-plan\n\nBuild a payload of shape:\n\njson\n{\n  \"framework\": \"\u003cslug without -minimal/-showcase, e.g. nestjs\u003e\",\n  \"tier\": \"hello-world | minimal | showcase\",\n  \"research\": {\n    \"codebaseShape\": \"1 | 2 | 3\",\n    \"needsAppSecret\": true/false,\n    \"appSecretKey\": \"\u003cenv-var name the framework expects, or empty\u003e\",\n    \"description\": \"one-sentence recipe purpose\"\n  },\n  \"codebases\": [\n    {\"hostname\": \"\u003chost\u003e\", \"role\": \"monolith|api|frontend|worker\",\n     \"baseRuntime\": \"\u003ctype@version\u003e\", \"isWorker\": false,\n     \"sharesCodebaseWith\": \"\"}\n  ],\n  \"services\": [\n    {\"hostname\": \"db\", \"type\": \"postgresql@18\", \"kind\": \"managed\",\n     \"priority\": 10}\n  ]\n}\n\n\nCall: zerops_recipe action=update-plan slug=\u003cslug\u003e plan=\u003cpayload\u003e.\n\nWhen the plan is in place, call zerops_recipe action=complete-phase\nslug=\u003cslug\u003e to run the research gate (checks classification/shape\nconsistency, required services per tier, parent inheritance). Gate\nfailures return structured violations — fix the plan and retry.\n\nDo NOT call build-brief before update-plan: scaffold briefs read\ncodebases from the plan, so an empty plan causes the brief composer to\nfail with unknown role.\n"}

I'll research NestJS specifics and the latest service versions before submitting the plan.

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
Zerops [zerops_knowledge]nestjs framework research classification
OUT
[{"uri":"zerops://bases/static","title":"Static on Zerops","score":1,"snippet":"...run static.\n\n### Build Procedure\n\n1. Set build.base: nodejs@22\n2. buildCommands: framework build command\n3. deployFiles: dist/~ (tilde MANDATORY for correct root)\n4. No run.start needed, no port config (serves on 80 internally)\n\n### SPA Fallback\n\nAutomatic ($uri -\u003e $uri.html..."},{"uri":"zerops://guides/environment-variables","title":"Environment Variables","score":1,"snippet":"...in the project** — no declaration required. run.envVariables exists only for mode flags and framework-convention renames. Re-declaring an auto-injected var under its own name creates a literal-string self-shadow. Secret vars are write-only after creation. Changes require service..."},{"uri":"zerops://guides/networking","title":"Networking on Zerops","score":1,"snippet":"...Timeout settings — For slow responses, increase send_timeout (default 2s)\n\nCommon framework fixes:\nbash\napp.listen(3000, '0.0.0.0')\n\nflask run --host=0.0.0.0\n\nhttp.ListenAndServe(\":8080\", handler)  // implicit 0.0.0.0\n\nserver.address=0.0.0.0\n\n\n---\n\n## Shared vs Dedicated..."},{"uri":"zerops://guides/object-storage-integration","title":"Object Storage Integration on Zerops","score":1,"snippet":"...S3 client must be configured for path-style access.**\n\n## Framework Integration\n\n### PHP (Laravel — Flysystem)\nphp\n// config/filesystems.php\n's3' =\u003e [\n    'driver' =\u003e 's3',\n    'endpoint' =\u003e env('S3_ENDPOINT'),\n    'use_path_style_endpoint' =\u003e true, ..."},{"uri":"zerops://guides/production-checklist","title":"Production Checklist for Zerops","score":1,"snippet":"...store |\n\nyaml\n- hostname: cache\n  type: valkey@7.2\n  mode: NON_HA  # HA for production\n```\n\n## Framework-Specific Production Settings\n\n### PHP/Laravel\n- APP_ENV: production, APP_DEBUG: \"false\"\n- Trusted proxies: TRUSTED_PROXIES: 127.0.0.1,10.0.0.0/8\n- Sessions in Redis, not files\n-..."}]

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

✓ postgresql@18

✓ valkey@7.2

✓ nats@2.10

⚠ meilisearch@1.19 not found. Available: meilisearch@1.10, meilisearch@1.20

✓ object-storage
