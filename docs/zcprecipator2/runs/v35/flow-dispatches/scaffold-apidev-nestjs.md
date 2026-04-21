# Dispatch 1 — scaffold-apidev-nestjs

**Dispatched at**: `2026-04-21T07:31:20`
**Description**: Scaffold apidev (NestJS)
**Subagent type**: `general-purpose`
**Prompt length**: 12337 chars
**Tool-use id**: `toolu_01EWJuHbHdbtES6Pn3w4fpvn`

---

## Transmitted prompt (verbatim)

```
You are a scaffolding sub-agent for a Zerops recipe session. You scaffold the **NestJS API codebase** on the dev container mounted at `/var/www/apidev/` (SSH alias `apidev`).

## Recipe plan (nestjs-showcase)

- Framework: NestJS (latest @nestjs/cli with `nest new`)
- Runtime: `nodejs@22`
- Package manager: npm
- HTTP port: 3000
- Primary DB: postgresql@18 via TypeORM
- Managed services wired on this codebase:
  - db (postgresql@18) — env vars: `db_hostname`, `db_port`, `db_user`, `db_password`, `db_dbName`
  - redis (valkey@7.2) — env vars: `redis_hostname`, `redis_port` (NO password — managed Valkey on Zerops has no auth)
  - queue (nats@2.12) — env vars: `queue_hostname`, `queue_port`, `queue_user`, `queue_password` (pass user+pass as structured options; NEVER URL-embedded)
  - storage (object-storage) — env vars: `storage_apiUrl`, `storage_accessKeyId`, `storage_secretAccessKey`, `storage_bucketName`
  - search (meilisearch@1.20) — env vars: `search_hostname`, `search_port`, `search_masterKey`
- Project-level env vars available: `APP_SECRET`, `DEV_APP_URL`, `DEV_API_URL`, `STAGE_APP_URL`, `STAGE_API_URL`

## What to install and configure

Production deps in `package.json`:
- `@nestjs/common`, `@nestjs/core`, `@nestjs/platform-express`, `@nestjs/config`, `@nestjs/typeorm`, `@nestjs/cache-manager`, `@nestjs/microservices`
- `typeorm`, `pg`, `reflect-metadata`, `rxjs`, `class-validator`, `class-transformer`
- `cache-manager`, `@keyv/redis`, `keyv`
- `nats` (NATS client used by @nestjs/microservices NATS transport)
- `@aws-sdk/client-s3`, `@aws-sdk/s3-request-presigner`
- `meilisearch`

Dev deps: `@nestjs/cli`, `@nestjs/schematics`, `@nestjs/testing`, `typescript`, `ts-node`, `tsconfig-paths`, `@types/node`, `@types/express`, types for any other packages.

## Files to write (API scaffold — health dashboard only)

**src/main.ts**:
- Bootstrap with `NestFactory.create(AppModule)`
- Trust proxy: `app.getHttpAdapter().getInstance().set('trust proxy', true)`
- Enable CORS with `origin: true, credentials: true` (ports DEV_APP_URL and STAGE_APP_URL)
- Set global prefix `api` so routes are `/api/...`
- Graceful shutdown: `app.enableShutdownHooks()` — satisfies Principle 1
- Bind `await app.listen(3000, '0.0.0.0')` — satisfies Principle 2

**src/app.module.ts**:
- `ConfigModule.forRoot({ isGlobal: true })`
- `TypeOrmModule.forRoot({ type: 'postgres', host: process.env.db_hostname, port: +process.env.db_port, username: process.env.db_user, password: process.env.db_password, database: process.env.db_dbName, synchronize: false, autoLoadEntities: true, entities: [__dirname+'/**/*.entity.js'] })`
- `TypeOrmModule.forFeature([Item, Job])`
- `CacheModule.registerAsync({ isGlobal: true, useFactory: async () => ({ stores: [new KeyvRedis(\`redis://\${process.env.redis_hostname}:\${process.env.redis_port}\`)] }) })` (or similar; verify against the installed @nestjs/cache-manager + @keyv/redis packages — see "Verify imports" note below)
- `ClientsModule.registerAsync([{ name: 'NATS_CLIENT', useFactory: () => ({ transport: Transport.NATS, options: { servers: [\`nats://\${process.env.queue_hostname}:\${process.env.queue_port}\`], user: process.env.queue_user, pass: process.env.queue_password } }) }])` — creds as structured options, NEVER embedded in URL
- Controllers: HealthController, StatusController
- Services: S3Service, SearchService (thin singletons that initialize clients from env vars). Do NOT write feature controllers.

**src/entities/item.entity.ts**: `@Entity() class Item { @PrimaryGeneratedColumn() id; @Column() title; @Column({type:'text'}) body; @CreateDateColumn() createdAt }`

**src/entities/job.entity.ts**: `@Entity() class Job { @PrimaryGeneratedColumn('uuid') id; @Column() payload; @Column({default:'queued'}) status; @Column({nullable:true,type:'timestamptz'}) processedAt; @CreateDateColumn() createdAt }`

**src/health.controller.ts**: `@Controller('health') GET / → { ok: true }` (content-type JSON).

**src/status.controller.ts**: `@Controller('status') GET /` — pings every service. Returns `{ db: 'ok'|'error', redis: 'ok'|'error', nats: 'ok'|'error', storage: 'ok'|'error', search: 'ok'|'error' }`. Each ping is a tiny operation (db: `SELECT 1` via DataSource; redis: cacheManager.set+get; nats: `natsClient.emit('ping',{})`; storage: `s3.send(new HeadBucketCommand)`; search: `meili.health()`). Each one wrapped in try/catch so one failure doesn't crash the whole response. Return `Content-Type: application/json`.

**src/s3.service.ts**: injectable service creating `new S3Client({ endpoint: process.env.storage_apiUrl, region: 'us-east-1', credentials: { accessKeyId: process.env.storage_accessKeyId, secretAccessKey: process.env.storage_secretAccessKey }, forcePathStyle: true })`. Satisfies Principle 5 (structured credential passing) + MinIO path-style constraint.

**src/search.service.ts**: injectable service wrapping `new MeiliSearch({ host: \`http://\${process.env.search_hostname}:\${process.env.search_port}\`, apiKey: process.env.search_masterKey })`. Expose `getClient()`. Do NOT create indexes — feature sub-agent will.

**src/migrate.ts**: standalone TypeORM DataSource migration runner. Reads env vars, creates DataSource with `migrations: [__dirname+'/migrations/*.js']`, runs `dataSource.initialize()` then `dataSource.runMigrations()`. Exit 1 on any error (loud failure — execOnce records failure).

**src/seed.ts**: standalone DataSource. Inserts 3-5 Item rows with different titles/bodies the feature sub-agent can use later. Use a static `bootstrap-seed-v1` key implicit in the seed (no row-count guard — seed is keyed by static string in zerops.yaml initCommands). If the seed row already exists (UNIQUE violation), that's fine — catch and continue. Exit 1 on any OTHER error.

**src/migrations/1700000000000-InitialSchema.ts**: TypeORM migration creating `items` and `jobs` tables matching the entities.

**tsconfig.json**, **tsconfig.build.json**, **nest-cli.json**: what `nest new` generates.

**.gitignore**: `dist/`, `node_modules/`, `*.log`, `.env`, `*.local`, `coverage/`.

**.env.example**: document every env var the code reads.

## What NOT to write

- No item CRUD controllers, no cache-demo routes, no search controller, no jobs-dispatch controller, no file-upload controller. Those belong to the feature sub-agent at deploy step 4b.
- No README.md — delete it if `nest new` creates one.
- No zerops.yaml — main agent writes it.
- No `.git/` — `nest new` creates one; delete with `ssh apidev "rm -rf /var/www/.git"` before returning.
- No `.env` file (only `.env.example`).

## Workflow

1. SSH into apidev and run `nest new` via `npx @nestjs/cli new . --package-manager npm --skip-git --skip-install`. Force it into `/var/www`. If nest CLI balks on non-empty dir, create files manually instead.
2. Install dependencies: `ssh apidev "cd /var/www && npm install"`.
3. Edit/write the source files listed above via Write/Edit on the mount `/var/www/apidev/`.
4. Read-before-Edit: before your first Edit of a scaffolder-created file, Read it.
5. **Verify imports** — for @nestjs/cache-manager, @keyv/redis, @nestjs/microservices NATS transport, and @aws-sdk/client-s3, open `node_modules/{pkg}/package.json` (or the index.d.ts the manifest points at) and confirm the symbol names and subpath exports you use exist in the installed version. Meilisearch class name was renamed mid-v0.x — check the installed version's d.ts for the exported class name.
6. Delete `.git/` and `README.md`: `ssh apidev "rm -rf /var/www/.git /var/www/README.md"`.

## Pre-ship self-verification

Run this script via `bash -c` (do NOT save it into /var/www/apidev):

```bash
HOST=apidev
MOUNT=/var/www/$HOST
FAIL=0

# Assertion 2 — app.listen binds 0.0.0.0
if grep -rq "app\.listen(" $MOUNT/src/ 2>/dev/null; then
    if ! grep -rqE "'0\.0\.0\.0'|\"0\.0\.0\.0\"" $MOUNT/src/; then
        echo "FAIL: app.listen without 0.0.0.0"; FAIL=1
    fi
fi

# Assertion 3 — trust proxy
if ! grep -rq "trust proxy" $MOUNT/src/; then
    echo "FAIL: no trust proxy"; FAIL=1
fi

# Assertion 4 — S3Client with forcePathStyle
if grep -rq "new S3Client\|S3Client(" $MOUNT/src/; then
    if ! grep -rq "forcePathStyle:\s*true" $MOUNT/src/; then
        echo "FAIL: S3Client without forcePathStyle"; FAIL=1
    fi
fi

# Assertion 5 — NATS not URL-embedded creds
if grep -rnE "'nats://[^']*:[^']*@|\"nats://[^\"]*:[^\"]*@" $MOUNT/src/ 2>/dev/null | head -1; then
    echo "FAIL: URL-embedded NATS creds"; FAIL=1
fi

# Assertion 6 — Valkey/Redis no :password@
if grep -rnE "redis://[^@]*:[^@]*@|valkey://[^@]*:[^@]*@" $MOUNT/src/ 2>/dev/null | head -1; then
    echo "FAIL: Valkey with password"; FAIL=1
fi

# Assertion 8 — .gitignore covers node_modules
if ! grep -qE "^(\s*node_modules|\s*/node_modules)" $MOUNT/.gitignore 2>/dev/null; then
    echo "FAIL: .gitignore missing node_modules"; FAIL=1
fi

# Assertion 10 — no .git/ left
if ssh $HOST "test -d /var/www/.git" 2>/dev/null; then
    echo "FAIL: .git exists"; FAIL=1
fi

# No README.md
if [ -f $MOUNT/README.md ]; then echo "FAIL: README.md exists"; FAIL=1; fi

# No zerops.yaml
if [ -f $MOUNT/zerops.yaml ]; then echo "FAIL: zerops.yaml exists"; FAIL=1; fi

exit $FAIL
```

Fix any failure and re-run the FULL script until it exits 0.

## Return report

When done, return:
- Files written (flat list)
- Env var names wired per managed service
- Pre-ship script exit code (must be 0)
- Any noteworthy decisions (e.g., cache-manager store resolution, which Meilisearch class name you used)

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

### Scaffold pre-flight — platform principles

The Zerops platform imposes invariants that every service must honor. Framework scaffolders do not automatically satisfy them. Before pre-ship assertions run, walk this list. For each principle that applies to your codebase type:

1. Identify the framework's specific idiom that satisfies the principle (you just scaffolded the framework — you know its APIs).
2. Verify the idiom is present in the scaffolded code. If absent, implement it.
3. Record a fact with scope=both naming both the principle number AND the idiom used.
4. If the framework offers no idiom for a principle that applies, implement the behavior yourself AND record a fact explaining the implementation.

Principles are absolute. Idioms are framework-specific and not listed here; the subagent translates.

**Principle 1 — Graceful shutdown within thirty seconds** — applies to any long-running service. On termination signal, stop accepting new work, await completion of in-flight work, close long-lived connections, exit within thirty seconds.

**Principle 2 — Routable network binding** — bind to all network interfaces (wildcard) or advertised IP, never loopback.

**Principle 3 — Client-origin awareness behind a proxy** — configure framework's proxy-trust for exactly one upstream hop.

**Principle 4 — Competing-consumer semantics at replica count two or more** — applies to subscription workers. Enable broker's competing-consumer mechanism on every subscription (queue group for NATS).

**Principle 5 — Structured credential passing** — pass user and pass as structured client options, not URL components.

**Principle 6 — Stripped build-output root for static deploys** — applies to static/SPA. Not applicable to this API codebase.

<<<END MANDATORY>>>

```
