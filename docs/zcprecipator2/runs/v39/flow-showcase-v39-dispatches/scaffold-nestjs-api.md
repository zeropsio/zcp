# Dispatch 2 — scaffold-nestjs-api

**Dispatched at**: `2026-04-22T18:25:29`
**Description**: Scaffold NestJS API
**Subagent type**: `general-purpose`
**Prompt length**: 14776 chars
**Tool-use id**: `toolu_01XsHTa2gCExPYLjefNGBrar`

---

## Transmitted prompt (verbatim)

```
You are scaffolding the API codebase of the nestjs-showcase Zerops recipe (a multi-codebase showcase with separate frontend / NestJS API / worker repos).

## Codebase scope

- Hostname: **apidev** (mount path `/var/www/apidev/`)
- Stack: **NestJS 11 + Express + TypeScript** on `nodejs@24`. ORM: **TypeORM** with `pg` driver to PostgreSQL.
- HTTP port: **3000**
- Public dev URL: `https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app`
- Public stage URL: `https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app`

Managed services in this project (you wire CLIENTS for ALL of them — but routes are health/status only):

| Service | Type | Env var keys (cross-service refs in zerops.yaml become OS env vars) |
|---|---|---|
| db | postgresql@18 | `DB_HOST=${db_hostname}`, `DB_PORT=${db_port}`, `DB_USER=${db_user}`, `DB_PASS=${db_password}`, `DB_NAME=${db_dbName}` |
| redis | valkey@7.2 (NO auth) | `REDIS_HOST=${redis_hostname}`, `REDIS_PORT=${redis_port}` |
| queue | nats@2.12 | `NATS_HOST=${queue_hostname}`, `NATS_PORT=${queue_port}`, `NATS_USER=${queue_user}`, `NATS_PASS=${queue_password}` |
| storage | object-storage (S3) | `S3_ACCESS_KEY=${storage_accessKeyId}`, `S3_SECRET_KEY=${storage_secretAccessKey}`, `S3_ENDPOINT=${storage_apiUrl}`, `S3_BUCKET=${storage_bucketName}`, `S3_REGION=us-east-1` |
| search | meilisearch@1.20 | `MEILI_HOST=${search_hostname}`, `MEILI_PORT=${search_port}`, `MEILI_KEY=${search_masterKey}` |

Project-level env vars already set (auto-injected): `APP_SECRET`, `DEV_APP_URL`, `STAGE_APP_URL`, `DEV_API_URL`, `STAGE_API_URL`.

The recipe declares 5 features (items-crud, cache-demo, storage-upload, search-items, jobs-dispatch). **You implement NONE of them.** Your scaffold ships /api/health, /api/status, plus client wiring + migration + seed. The feature sub-agent later authors all 5 features as a single coherent unit.

## ⚠ CRITICAL: where commands run (read FIRST)

You are running on the **zcp orchestrator container**, not on the target dev container. The path `/var/www/apidev/` on zcp is an **SSHFS network mount** — a write surface, not an execution surface.

- **File writes** via Write/Edit/Read against `/var/www/apidev/` work correctly.
- **Executable commands** MUST run via SSH:
  ```
  ssh apidev "cd /var/www && <command>"
  ```
  NEVER `cd /var/www/apidev && <command>` from Bash.

Every `npm install`, `nest new`, `npm run build`, `tsc`, `git init/add/commit` runs through SSH. Wrong-side execution → EACCES, broken `.bin/` symlinks, native-module ABI mismatches.

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

### Scaffold pre-flight — platform principles

The Zerops platform imposes invariants that every service must honor. Framework scaffolders do not automatically satisfy them.

- **Principle 1 — Graceful shutdown within 30s**: Register the framework's shutdown hooks (NestJS: `app.enableShutdownHooks()`, then implement `OnModuleDestroy` on long-lived services to close DB pool, Redis, NATS, S3 client cleanly).
- **Principle 2 — Routable network binding**: bind `0.0.0.0`, never `localhost` or `127.0.0.1`. NestJS: `app.listen(port, '0.0.0.0')`.
- **Principle 3 — Client-origin awareness behind a proxy**: enable Express trust-proxy. NestJS: `app.getHttpAdapter().getInstance().set('trust proxy', true)`.
- **Principle 5 — Structured credential passing**: For NATS, do NOT embed `:user:pass@` in the URL — pass `{ servers, user, pass }` object to `connect()`. For Postgres/TypeORM use the structured `host/port/user/password/database` config; for Redis/ioredis use `{ host, port }` (no `redis://...:pass@...` URL — managed Valkey has no auth).

<<<END MANDATORY>>>

## What to write

1. **Scaffold NestJS at `/var/www/` on the apidev container.** Use the official CLI:
   ```
   ssh apidev "cd /var/www && npx -y @nestjs/cli new . --skip-git --skip-install --package-manager npm"
   ```
   `--skip-git` avoids `.git/` collision. Then run `npm install` separately so logs are visible. After that, install the additional dependencies in one go (see step 3).

2. **Trim the scaffolder's demo**: keep `src/main.ts`, `src/app.module.ts`. Delete `src/app.controller.ts`, `src/app.service.ts`, `src/app.controller.spec.ts` — you'll replace AppController with your own modules.

3. **Install dependencies** (single SSH call after scaffold):
   ```
   ssh apidev "cd /var/www && npm install --save \
     @nestjs/typeorm typeorm pg \
     @nestjs/config \
     ioredis \
     nats \
     @aws-sdk/client-s3 \
     meilisearch \
     class-validator class-transformer"
   ```

4. **`src/main.ts`** — production bootstrap:
   - Get `INestApplication` via `NestFactory.create(AppModule)`
   - `app.enableShutdownHooks()` (Principle 1)
   - `app.getHttpAdapter().getInstance().set('trust proxy', true)` (Principle 3)
   - CORS: allow `process.env.DEV_APP_URL` and `process.env.STAGE_APP_URL` (split list, filter empty), credentials `true`.
   - `app.setGlobalPrefix('api')` so all controllers live under `/api/*`.
   - `app.useGlobalPipes(new ValidationPipe({ whitelist: true, transform: true }))`
   - `await app.listen(Number(process.env.PORT) || 3000, '0.0.0.0')` (Principle 2)

5. **`src/app.module.ts`** — wire:
   - `ConfigModule.forRoot({ isGlobal: true })`
   - `TypeOrmModule.forRootAsync({...})` reading DB_* env vars, `synchronize: false`, `entities: [Item]`, `autoLoadEntities: true`. **Never `synchronize: true`** in any env (use migrations).
   - Import: `HealthModule`, `StatusModule`, plus stub modules for the 5 features so the imports compile but routes are empty (only the feature sub-agent fills them later — but to give them a place to land, create stub modules: `ItemsModule`, `CacheModule`, `StorageModule`, `SearchModule`, `JobsModule` each with an empty controller and an empty providers array. **Do NOT add controllers/services with routes**; the feature sub-agent will fill them.) Actually, simpler: do NOT create the stub feature modules — the feature sub-agent creates them. Only `HealthModule` and `StatusModule` here.

6. **`src/health/health.controller.ts`** — `GET /api/health` returns `{ ok: true }`. No service calls.

7. **`src/status/status.module.ts` + `status.service.ts` + `status.controller.ts`** — `GET /api/status` returns `{ db: "ok"|"error", redis: "ok"|"error", nats: "ok"|"error", storage: "ok"|"error", search: "ok"|"error" }`. The service injects clients (or uses raw clients via providers) and pings each:
   - db: TypeORM `dataSource.query('SELECT 1')`
   - redis: `redis.ping()`
   - nats: connection status check (the connection is opened lazily — see step 8 — and `status` checks `connection.isClosed()` after a `flush()`)
   - storage: `s3.send(new HeadBucketCommand({ Bucket }))`
   - search: `meili.health()` returns `{ status: 'available' }`
   - Wrap each in try/catch; on error return `"error"` and log the reason. Run all 5 in parallel via `Promise.allSettled`.

8. **`src/clients/`** — provider modules for each managed service so the StatusService and (later) feature controllers can inject them:
   - `redis.client.ts` — provides `IORedis` instance configured from `REDIS_HOST`, `REDIS_PORT`. **No password segment** (managed Valkey has no auth).
   - `nats.client.ts` — connects via `nats` library's `connect({ servers: [`${NATS_HOST}:${NATS_PORT}`], user: NATS_USER, pass: NATS_PASS })`. Returns the `NatsConnection`. **Never `nats://user:pass@host:port` URL** — Principle 5.
   - `s3.client.ts` — `S3Client` from `@aws-sdk/client-s3` with `endpoint: process.env.S3_ENDPOINT`, `region: process.env.S3_REGION`, `forcePathStyle: true`, credentials from env. Bucket name in `process.env.S3_BUCKET`.
   - `meili.client.ts` — `Meilisearch` client with `host: http://${MEILI_HOST}:${MEILI_PORT}` and `apiKey: MEILI_KEY`. Internal HTTP only.

   Each client module should be `@Global()` so providers are shared, with `OnModuleDestroy` closing the underlying connection (Principle 1).

9. **`src/entities/item.entity.ts`** — minimal entity for the items-crud feature stub:
   ```ts
   @Entity()
   export class Item {
     @PrimaryGeneratedColumn() id: number;
     @Column() title: string;
     @Column({ type: 'text', nullable: true }) body: string | null;
     @CreateDateColumn() createdAt: Date;
   }
   ```
   The feature sub-agent may add columns later — the entity stays minimal here.

10. **`src/migrate.ts`** — standalone TypeORM migration runner:
    - Build a `DataSource` from env vars
    - Create `items` table via raw SQL (CREATE TABLE IF NOT EXISTS) OR via TypeORM migration class. Simpler approach: raw `CREATE TABLE IF NOT EXISTS items (id SERIAL PRIMARY KEY, title TEXT NOT NULL, body TEXT, "createdAt" TIMESTAMP DEFAULT NOW())`.
    - Loud failure: any error → `console.error(...)` + `process.exit(1)`.

11. **`src/seed.ts`** — standalone seed runner:
    - 3-5 row insert into `items` using `INSERT ... ON CONFLICT DO NOTHING` (idempotent).
    - One row's title MUST be exactly `welcome` (the recipe's search-items feature uses this as a known query).
    - **Loud failure**: any error → exit 1. NO try/catch that swallows.
    - **No row-count guard** — idempotency comes from `ON CONFLICT DO NOTHING` and from the `initCommands` key in zerops.yaml (main agent writes a static key like `bootstrap-seed-v1`, NOT `${appVersionId}`).

12. **`src/search-import.ts`** — sync seeded items to Meilisearch. Read all items from db, call `index('items').addDocuments(rows)`, **`await waitForTask(task.taskUid)`** so the script doesn't exit before indexing completes (the recipe's search-items feature depends on this). Loud failure on any error.

13. **`tsconfig.json`** — keep scaffolder default. Make sure `outDir: "./dist"`, `target: "ES2022"`.

14. **Build scripts in `package.json`** — keep the scaffolder defaults (`"build": "nest build"`, `"start:dev": "nest start --watch"`). Add nothing else.

15. **`.env.example`** — document the env var names listed at the top of this brief.

16. **`.gitignore`** — scaffolder ships one with `node_modules`, `dist`, etc. Verify.

17. **Delete `/var/www/.git/`** if the scaffolder created one. `ssh apidev "rm -rf /var/www/.git"`.

## What NOT to write

- **NO `zerops.yaml`** — main agent owns deploy config.
- **NO `README.md`** — main agent writes after deploy.
- **NO feature routes**: no items CRUD, no cache GET/PUT, no upload/list, no search query, no jobs dispatch. The feature sub-agent owns these end-to-end.
- **NO `.env`** file — only `.env.example`. A populated `.env` with empty values shadows OS env vars at runtime.
- **NO `synchronize: true`** in TypeORM config under any condition.
- **NO `nats://user:pass@host:port`** URLs — Principle 5.
- **NO `redis://...@host`** URLs — managed Valkey has no auth.

## Pre-ship self-verification (MANDATORY)

Save to `/tmp/preship-api.sh` (NOT under `/var/www/apidev/`) on zcp and run it:

```bash
#!/bin/bash
HOST=apidev
MOUNT=/var/www/$HOST
FAIL=0

# 1. .gitignore covers node_modules + dist
[ -f $MOUNT/.gitignore ] || { echo "FAIL: .gitignore missing"; FAIL=1; }
grep -qE "^(\s*node_modules|\s*/node_modules)" $MOUNT/.gitignore || { echo "FAIL: .gitignore missing node_modules"; FAIL=1; }

# 2. main.ts binds 0.0.0.0 + trust proxy + shutdown hooks
grep -q "0.0.0.0" $MOUNT/src/main.ts || { echo "FAIL: main.ts not binding 0.0.0.0"; FAIL=1; }
grep -q "trust proxy" $MOUNT/src/main.ts || { echo "FAIL: main.ts missing trust proxy"; FAIL=1; }
grep -q "enableShutdownHooks" $MOUNT/src/main.ts || { echo "FAIL: main.ts missing enableShutdownHooks"; FAIL=1; }

# 3. NO synchronize: true
if grep -rn "synchronize:\s*true" $MOUNT/src/ 2>/dev/null | head -1; then
  echo "FAIL: synchronize: true found - use migrations only"; FAIL=1
fi

# 4. S3Client uses forcePathStyle
if grep -rq "S3Client" $MOUNT/src/; then
  grep -rq "forcePathStyle:\s*true" $MOUNT/src/ || { echo "FAIL: S3Client without forcePathStyle: true"; FAIL=1; }
fi

# 5. NO URL-embedded NATS creds
if grep -rnE "'nats://[^']*:[^']*@|\"nats://[^\"]*:[^\"]*@" $MOUNT/src/ 2>/dev/null | head -1; then
  echo "FAIL: URL-embedded NATS creds — pass user/pass as object props"; FAIL=1
fi

# 6. NO redis://...@ password URLs
if grep -rnE "redis://[^@]*:[^@]*@|valkey://[^@]*:[^@]*@" $MOUNT/src/ 2>/dev/null | head -1; then
  echo "FAIL: Redis URL with embedded password — managed Valkey has no auth"; FAIL=1
fi

# 7. health + status routes exist
grep -rq "/health\|'health'" $MOUNT/src/health/ || { echo "FAIL: health route missing"; FAIL=1; }
grep -rq "/status\|'status'" $MOUNT/src/status/ || { echo "FAIL: status route missing"; FAIL=1; }

# 8. seed has welcome row
grep -q "welcome" $MOUNT/src/seed.ts || { echo "FAIL: seed.ts missing 'welcome' row required by search-items feature"; FAIL=1; }

# 9. No README, no zerops.yaml, no .git, no .env
[ ! -f $MOUNT/README.md ] || { echo "FAIL: README.md exists"; FAIL=1; }
[ ! -f $MOUNT/zerops.yaml ] || { echo "FAIL: zerops.yaml exists"; FAIL=1; }
[ ! -f $MOUNT/.env ] || { echo "FAIL: .env file exists - shadows OS env vars"; FAIL=1; }
ssh $HOST "test -d /var/www/.git" 2>/dev/null && { echo "FAIL: /var/www/.git exists - delete"; FAIL=1; }

# 10. No feature controllers (only health + status)
for f in items.controller cache.controller search.controller storage.controller jobs.controller files.controller; do
  if find $MOUNT/src -name "$f.ts" 2>/dev/null | head -1; then
    echo "FAIL: $f.ts exists - feature sub-agent's territory"; FAIL=1
  fi
done

# 11. Build compiles
ssh $HOST "cd /var/www && npm run build" >/tmp/preship-api-build.log 2>&1 || {
  echo "FAIL: nest build failed - see /tmp/preship-api-build.log"; FAIL=1
}

exit $FAIL
```

Run with `bash /tmp/preship-api.sh`. Pre-ship script must NOT live in the codebase.

## Reporting back

Bulleted list of files written, env var names wired per service, exit code of pre-ship script (must be 0), any fixes applied. Do NOT claim feature implementation.
```
