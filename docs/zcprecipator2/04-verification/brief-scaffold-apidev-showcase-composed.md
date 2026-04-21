# brief-scaffold-apidev-showcase-composed.md

**Role**: scaffold sub-agent (api codebase)
**Tier**: showcase
**Source atoms** (per [atomic-layout.md §6 stitching](../03-architecture/atomic-layout.md#6-stitching-conventions-guidancego-rewrite-surface)):

```
briefs/scaffold/mandatory-core.md
briefs/scaffold/symbol-contract-consumption.md  (interpolated with SymbolContract JSON)
briefs/scaffold/framework-task.md
briefs/scaffold/api-codebase-addendum.md
briefs/scaffold/pre-ship-assertions.md
briefs/scaffold/completion-shape.md
+ pointer-include principles/where-commands-run.md
+ pointer-include principles/file-op-sequencing.md
+ pointer-include principles/tool-use-policy.md
+ pointer-include principles/platform-principles/01..06.md
+ pointer-include principles/comment-style.md
+ pointer-include principles/visual-style.md
+ pointer-include principles/symbol-naming-contract.md (consumption conventions only)
+ PriorDiscoveriesBlock(sessionID, substep=generate.scaffold)
```

Interpolations:
- `{{.Hostname}} = apidev`
- `{{.SymbolContract | toJSON}}` — showcase contract with 5 services (db/redis/queue/storage/search)
- `{{.Framework}} = NestJS 11 / nodejs@22`

---

## Composed brief (as the sub-agent would receive it)

```
You are a scaffolding sub-agent. Scaffold the API codebase for hostname `apidev` on the SSHFS mount `/var/www/apidev/`.

--- [briefs/scaffold/mandatory-core.md] ---

## Where your tools may act

Permitted tools:
- File ops: Read, Edit, Write, Grep, Glob — all targeting paths under `/var/www/apidev/`.
- Bash — only in the shape `ssh apidev "cd /var/www && <command>"`. Everything else the shell runs is out of scope for this role.
- mcp__zerops__zerops_dev_server, mcp__zerops__zerops_knowledge, mcp__zerops__zerops_logs, mcp__zerops__zerops_discover, mcp__zerops__zerops_record_fact.

Forbidden tools: mcp__zerops__zerops_workflow, mcp__zerops__zerops_import, mcp__zerops__zerops_env, mcp__zerops__zerops_deploy, mcp__zerops__zerops_subdomain, mcp__zerops__zerops_mount, mcp__zerops__zerops_verify. Workflow state belongs to the main agent; calling workflow-related tools returns SUBAGENT_MISUSE.

## File-op sequencing

Every Edit is preceded by a Read of the same file in this session. For files the framework scaffolder creates (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before the first Edit. Batch-Read every file you intend to modify before the first Edit.

--- [principles/where-commands-run.md, pointer-included] ---

You run on the zcp orchestrator. `/var/www/apidev/` is a network-mounted view of the `apidev` dev container. Mount paths are a write surface only: Read/Edit/Write change files on the target, but running executables on zcp against the mount leaves binaries unusable on the target.

Positive form: every executable runs inside the target container over SSH.

    ssh apidev "cd /var/www && <command>"

This shape holds for: package installs, framework scaffolders (nest new, npm create vite, cargo new), compilers (tsc, go build), linters, tests, curl against local services, and git operations.

--- [briefs/scaffold/symbol-contract-consumption.md, interpolated] ---

The SymbolContract below is shared byte-identically across every scaffold dispatch in this run. Consume it directly; do not re-derive symbol names from framework documentation.

```json
{
  "EnvVarsByKind": {
    "db":     {"host": "DB_HOST",     "port": "DB_PORT",     "user": "DB_USER",     "pass": "DB_PASS",     "name": "DB_NAME"},
    "cache":  {"host": "REDIS_HOST",  "port": "REDIS_PORT"},
    "queue":  {"host": "NATS_HOST",   "port": "NATS_PORT",   "user": "NATS_USER",   "pass": "NATS_PASS"},
    "storage":{"endpoint": "STORAGE_ENDPOINT", "region": "STORAGE_REGION", "accessKey": "STORAGE_ACCESS_KEY_ID", "secretKey": "STORAGE_SECRET_ACCESS_KEY", "bucket": "STORAGE_BUCKET"},
    "search": {"host": "SEARCH_HOST", "port": "SEARCH_PORT", "key":  "SEARCH_MASTER_KEY"}
  },
  "HTTPRoutes": {
    "health": "/api/health",
    "status": "/api/status"
  },
  "NATSSubjects": {
    "job_dispatch": "jobs.dispatch",
    "job_process":  "jobs.process",
    "scaffold_ping":"jobs.scaffold"
  },
  "NATSQueues": {
    "workers": "workers"
  },
  "Hostnames": [
    {"role": "api",      "dev": "apidev",    "stage": "apistage"},
    {"role": "frontend", "dev": "appdev",    "stage": "appstage"},
    {"role": "worker",   "dev": "workerdev", "stage": "workerstage"}
  ],
  "DTOs": ["ItemDTO", "JobDTO", "FileDTO", "SearchHitDTO", "MailDTO"],
  "FixRecurrenceRules": [
    {"id":"nats-separate-creds",      "positiveForm":"pass user + pass as separate ConnectionOptions fields; servers is `${NATS_HOST}:${NATS_PORT}` only", "preAttestCmd":"grep -rnE \"'nats://[^']*:[^']*@|\\\"nats://[^\\\"]*:[^\\\"]*@\" /var/www/apidev/src/ && exit 1 || exit 0", "appliesTo":["api","worker"]},
    {"id":"s3-uses-api-url",          "positiveForm":"S3 client endpoint is process.env.STORAGE_ENDPOINT",                                           "preAttestCmd":"grep -rn 'storage_apiHost' /var/www/apidev/src/ && exit 1 || exit 0",       "appliesTo":["api"]},
    {"id":"s3-force-path-style",      "positiveForm":"S3 client forcePathStyle: true",                                                                  "preAttestCmd":"grep -rq 'S3Client' /var/www/apidev/src/ && ! grep -rq 'forcePathStyle: true' /var/www/apidev/src/ && exit 1 || exit 0", "appliesTo":["api"]},
    {"id":"routable-bind",            "positiveForm":"HTTP servers listen on 0.0.0.0",                                                                  "preAttestCmd":"grep -q \"'0.0.0.0'\" /var/www/apidev/src/main.ts",                            "appliesTo":["api","frontend"]},
    {"id":"trust-proxy",              "positiveForm":"Express/Fastify trust proxy is enabled for L7 forwarded-for",                                    "preAttestCmd":"grep -q 'trust proxy' /var/www/apidev/src/main.ts",                           "appliesTo":["api"]},
    {"id":"graceful-shutdown",        "positiveForm":"api calls app.enableShutdownHooks(); worker registers SIGTERM → drain → exit",                   "preAttestCmd":"grep -q 'enableShutdownHooks' /var/www/apidev/src/main.ts",                   "appliesTo":["api","worker"]},
    {"id":"env-self-shadow",          "positiveForm":"no `KEY: ${KEY}` lines in run.envVariables",                                                      "preAttestCmd":"(later, once zerops.yaml exists; main-agent substep)",                        "appliesTo":["any"]},
    {"id":"gitignore-baseline",       "positiveForm":".gitignore contains node_modules, dist, .env, .DS_Store",                                        "preAttestCmd":"grep -qE '^(node_modules|/node_modules)' /var/www/apidev/.gitignore && grep -q 'dist' /var/www/apidev/.gitignore && grep -q '.env' /var/www/apidev/.gitignore", "appliesTo":["any"]},
    {"id":"env-example-preserved",    "positiveForm":"framework scaffolder's .env.example is kept if present",                                          "preAttestCmd":"test -f /var/www/apidev/.env.example",                                         "appliesTo":["any"]},
    {"id":"no-scaffold-test-artifacts","positiveForm":"no preship.sh / *.assert.sh / self-test shell scripts committed under the codebase",             "preAttestCmd":"! find /var/www/apidev -maxdepth 3 \\( -name 'preship.sh' -o -name '*.assert.sh' \\) | grep -q .", "appliesTo":["any"]},
    {"id":"skip-git",                 "positiveForm":"framework scaffolder invoked with --skip-git OR `.git/` removed after return",                   "preAttestCmd":"! ssh apidev 'test -d /var/www/.git'",                                         "appliesTo":["any"]}
  ]
}
```

Before returning, execute every FixRule.preAttestCmd where appliesTo includes `api` or `any`. Non-zero exit = fix the code and re-run. This is your author-runnable pre-attest layer; the server gate runs the same commands.

--- [briefs/scaffold/framework-task.md] ---

You scaffold a health-dashboard-only API on the mount. Feature endpoints are owned by a later sub-agent. You own: framework skeleton, service-client wiring, entities + migrate + seed scripts, health + status endpoints, .env.example, .gitignore.

Workflow:

1. Run the framework scaffolder via SSH with --skip-git:

       ssh apidev "cd /var/www && npx -y @nestjs/cli new . --skip-git --skip-install --package-manager npm"

2. Install dependencies via SSH (NestJS + TypeORM + pg + config + cache-manager + redis + nats + S3 SDK + Meilisearch + Mailer + pino):

       ssh apidev "cd /var/www && npm install --save @nestjs/typeorm typeorm pg @nestjs/config cache-manager cache-manager-redis-yet redis ioredis nats @aws-sdk/client-s3 meilisearch @nestjs-modules/mailer nodemailer nestjs-pino pino-http pino-pretty"
       ssh apidev "cd /var/www && npm install --save-dev @types/nodemailer"

3. Read every file the scaffolder emitted (scan `src/`, `test/`, config roots) before the first Edit.

4. Modify or create files per the api-codebase-addendum below.

--- [briefs/scaffold/api-codebase-addendum.md] ---

Files you write:

- `src/main.ts` — Nest entrypoint. Use NestFactory.create<NestExpressApplication>(AppModule, { bufferLogs: true }). Wire nestjs-pino as the logger. app.setGlobalPrefix('api'). app.enableCors({ origin: true, credentials: true }). app.getHttpAdapter().getInstance().set('trust proxy', true). app.listen(process.env.PORT ?? 3000, '0.0.0.0'). app.enableShutdownHooks().
- `src/app.module.ts` — ConfigModule.forRoot({ isGlobal: true }); LoggerModule from nestjs-pino with pino-pretty in non-production; TypeOrmModule.forRoot reading DB_HOST/DB_PORT/DB_USER/DB_PASS/DB_NAME (from the contract), synchronize: false, entities: [Item, Job]; Import HealthModule, StatusModule, ServicesModule. AppController/AppService keep the root route returning `{ service: 'nestjs-showcase-api', version: 1 }`.
- `src/health/health.module.ts` + `src/health/health.controller.ts` — GET /api/health returns `{ ok: true }`, Content-Type application/json, no service calls.
- `src/status/status.module.ts` + `src/status/status.controller.ts` — GET /api/status returns a flat object `{ db, redis, nats, storage, search }` with string values "ok" or "error" from a live ping per service. Each call is try/catch; the endpoint returns 200 regardless (it's for the UI, not the platform readiness probe).
- `src/services/services.module.ts` — @Global module exporting async providers for S3 (`new S3Client({ endpoint: process.env.STORAGE_ENDPOINT, region: process.env.STORAGE_REGION ?? 'us-east-1', credentials: {...}, forcePathStyle: true })`), Redis (`new Redis({ host: process.env.REDIS_HOST, port: Number(process.env.REDIS_PORT), lazyConnect: false })` — no password), NATS (via `connect({ servers: ['${NATS_HOST}:${NATS_PORT}'], user: process.env.NATS_USER, pass: process.env.NATS_PASS })`; user and pass are separate options, never embedded in the server URL), Meilisearch (`new MeiliSearch({ host: 'http://${SEARCH_HOST}:${SEARCH_PORT}', apiKey: process.env.SEARCH_MASTER_KEY })`), and a CACHE_STORE provider wrapping cache-manager-redis-yet. Every client has an onModuleDestroy that closes the connection.
- `src/entities/item.entity.ts` — TypeORM entity Item: id uuid pk, title varchar(200), description text, createdAt timestamptz default now(). Feature sub-agent adds routes later.
- `src/entities/job.entity.ts` — TypeORM entity Job: id uuid pk, payload jsonb, status varchar(20) default 'pending', result text nullable, createdAt, processedAt nullable.
- `src/migrate.ts` — standalone TypeORM DataSource script; CREATE TABLE IF NOT EXISTS for item + job; .catch(e => { console.error; process.exit(1) }). The explicit exit(1) is the loud-failure contract — init scripts must not swallow errors.
- `src/seed.ts` — idempotency is enforced by the static execOnce key in zerops.yaml (bootstrap-seed-v1 — main agent writes it). Insert 5 demo items if Item.count() is zero; ALWAYS push all items to the Meilisearch `items` index (waitForTask on the addDocuments task). Never skip the Meilisearch push when the row-count guard hits.
- `.gitignore` — verify framework defaults cover node_modules, dist, .env (not .env.example), .DS_Store.
- `.env.example` — list every env var the app reads. Keep the framework-provided one if the scaffolder emitted it.

Files you do NOT write at this substep:
- README.md — main agent writes it at deploy.readmes. Delete it if the framework scaffolder created one.
- zerops.yaml — main agent writes it after this scaffold returns.
- .git/ — deleted post-scaffold via `ssh apidev "rm -rf /var/www/.git"` (or --skip-git on the scaffolder, which you already passed).
- Feature routes — /api/items, /api/cache, /api/files, /api/search, /api/jobs, /api/mail.

--- [principles/platform-principles/01..06.md, pointer-included] ---

01 Graceful shutdown — api: app.enableShutdownHooks(); every service client has onModuleDestroy.
02 Routable bind — app.listen(PORT, '0.0.0.0').
03 Proxy trust — express trust proxy enabled.
04 Competing consumer — worker-only; not required here.
05 Structured credentials — NATS user/pass as separate fields; no URL-embedded creds anywhere.
06 Stripped build root — deployFiles authoring is main-agent territory; this scaffold emits `dist/` output only.

Record a fact via mcp__zerops__zerops_record_fact after implementing each principle so the writer has an audit trail.

--- [principles/comment-style.md + principles/visual-style.md, pointer-included] ---

- YAML comments: one `#` per line, no decorative separators, no Unicode box-drawing characters, no dividers built out of `=`, `*`, `-`.
- Any text you author (READMEs, comments, logs): ASCII characters only; no Unicode section rulers, no emoji art.
- If you need to visually group lines, use a blank line and a short comment; never decorate.

--- [briefs/scaffold/pre-ship-assertions.md] ---

Before returning, execute each FixRule.preAttestCmd whose appliesTo includes `api` or `any`. The aggregate exit code must be 0. When any rule fails, repair the code and re-run that specific rule.

Also run these codebase-level assertions:

    HOST=apidev
    MOUNT=/var/www/$HOST
    grep -q '0.0.0.0' $MOUNT/src/main.ts                                      || { echo FAIL: 0.0.0.0 bind; exit 1; }
    grep -q 'trust proxy' $MOUNT/src/main.ts                                  || { echo FAIL: trust proxy; exit 1; }
    grep -q 'enableShutdownHooks' $MOUNT/src/main.ts                          || { echo FAIL: shutdown hooks; exit 1; }
    ! grep -rnE "'nats://[^']*:[^']*@|\"nats://[^\"]*:[^\"]*@" $MOUNT/src/   || { echo FAIL: URL-embedded NATS creds; exit 1; }
    ! grep -rnE "redis://[^@]*:[^@]*@|valkey://[^@]*:[^@]*@" $MOUNT/src/     || { echo FAIL: Redis with :password@; exit 1; }
    grep -rq 'S3Client' $MOUNT/src/ && grep -rq 'forcePathStyle: true' $MOUNT/src/ || { echo FAIL: S3 without forcePathStyle; exit 1; }
    test ! -f $MOUNT/README.md                                                || { echo FAIL: README.md present; exit 1; }
    test ! -f $MOUNT/zerops.yaml                                              || { echo FAIL: zerops.yaml present; exit 1; }
    ! ssh $HOST "test -d /var/www/.git"                                       || { echo FAIL: /var/www/.git present; exit 1; }
    test ! -f $MOUNT/.env                                                     || { echo FAIL: .env present; exit 1; }
    test -f $MOUNT/.env.example                                               || { echo FAIL: .env.example missing; exit 1; }
    grep -qE '^(node_modules|/node_modules)' $MOUNT/.gitignore                || { echo FAIL: .gitignore missing node_modules; exit 1; }
    exit 0

After assertions pass, run a build:

    ssh apidev "cd /var/www && npm run build 2>&1 | tail -40"

Fix any TypeScript errors before returning. Do NOT start a dev server — the main agent owns smoke testing.

--- [briefs/scaffold/completion-shape.md] ---

Return a single structured message to the main agent containing:

1. Files you wrote (bulleted list with byte counts).
2. The pre-attest runnable aggregate exit code (must be 0).
3. The `npm run build` tail output.
4. Every record_fact call you made, with its title and scope.
5. The env var names your code reads per managed service (ordered by service kind).

Do NOT claim any of the 6 showcase features are implemented. The feature sub-agent runs next.

--- [PriorDiscoveriesBlock(sessionID, substep=generate.scaffold)] ---

(Empty at first scaffold dispatch of the run. If this sub-agent re-runs after a provisional failure, prior downstream-scope facts appear here.)
```

**Composed byte-budget**: ~11 KB (≈ atomic-layout.md §8 estimate for the scaffold brief; compares to v34 scaffold-apidev dispatch at 15627 chars).
