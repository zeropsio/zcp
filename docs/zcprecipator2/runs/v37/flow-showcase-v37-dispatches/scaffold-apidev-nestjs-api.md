# Dispatch 2 — scaffold-apidev-nestjs-api

**Dispatched at**: `2026-04-21T19:36:26`
**Description**: Scaffold apidev NestJS API
**Subagent type**: `general-purpose`
**Prompt length**: 10747 chars
**Tool-use id**: `toolu_01JLqc4XTP5jnoNjWZYMQBrD`

---

## Transmitted prompt (verbatim)

```
You are a scaffold sub-agent for a Zerops recipe. Your codebase is the BACKEND API — a NestJS 11 + TypeScript + Express app mounted at `/var/www/apidev/`. Host to SSH into: `apidev`. Mount is SSHFS — writes go via Write/Edit, executables run via `ssh apidev "cd /var/www && <cmd>"`.

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

### Scaffold pre-flight — platform principles

The Zerops platform imposes invariants that every service must honor. Framework scaffolders do not automatically satisfy them. Before pre-ship assertions run, walk this list. For each principle that applies to your codebase type:

1. Identify the framework's specific idiom that satisfies the principle.
2. Verify the idiom is present in the scaffolded code. If absent, implement it.
3. Record a fact naming both the principle number AND the idiom used.
4. If the framework offers no idiom, implement the behavior yourself.

Principles:
**1 — Graceful shutdown within 30s** — stop accepting, drain, close connections, exit.
**2 — Routable network binding** — bind 0.0.0.0, never loopback.
**3 — Client-origin awareness behind a proxy** — configure framework proxy-trust for one hop.
**4 — Competing-consumer semantics** (N/A for this API — no subscription; the worker is a separate codebase).
**5 — Structured credential passing** — user/pass as client options, never URL-embedded.
**6 — Stripped build-output root for static deploys** (N/A for this API — runtime deploy).

<<<END MANDATORY>>>

## Your deliverable — NestJS API "health dashboard only" skeleton

**Framework setup**:
Use `ssh apidev "cd /var/www && npx -y @nestjs/cli@latest new . --package-manager npm --skip-git --strict"` to scaffold NestJS 11. `--skip-git` prevents the scaffolder from creating `.git/`. After the scaffolder returns, confirm no `.git/` on the mount and delete if present.

**Production dependencies to install** (add via `ssh apidev "cd /var/www && npm install <pkg>"`):
- `@nestjs/typeorm typeorm pg` — database
- `@nestjs/cache-manager cache-manager keyv @keyv/redis` — Valkey cache
- `@aws-sdk/client-s3 @aws-sdk/s3-request-presigner` — object storage
- `meilisearch` — search
- `@nestjs/microservices nats` — NATS client (for later feature work)
- `class-validator class-transformer` — DTO validation
- `express` — already pulled by @nestjs/platform-express, don't add explicitly

**devDependencies**: the `nest new` scaffolder already populates these.

**Bind + trust proxy** — edit `src/main.ts`:
- Import `NestFactory`, `AppModule`, and use `app.getHttpAdapter().getInstance().set('trust proxy', true)`.
- Call `app.enableShutdownHooks()` for Principle 1.
- Add a global `/api` prefix via `app.setGlobalPrefix('api')`.
- Listen: `await app.listen(process.env.PORT ?? 3000, '0.0.0.0')`.

**AppModule wiring** — edit `src/app.module.ts`:
- Import `ConfigModule.forRoot({ isGlobal: true })` — reads OS env vars, ignores .env files.
- Import `TypeOrmModule.forRootAsync` — reads DB_HOST, DB_PORT, DB_USER, DB_PASS, DB_NAME from env. Uses `postgres` driver. `entities: [ItemEntity, JobEntity]`. `synchronize: false` ALWAYS (migrations handle schema).
- Import `CacheModule.registerAsync({ isGlobal: true })` — uses `@keyv/redis` connecting to `redis://${REDIS_HOST}:${REDIS_PORT}` (no auth — Valkey has none).
- Register `HealthController`, `StatusController`.

**Entities** — create `src/entities/item.entity.ts` and `src/entities/job.entity.ts`:
- `ItemEntity` — `id` (uuid, primary), `title` (varchar 255 not null), `description` (text nullable), `createdAt` (timestamp default now).
- `JobEntity` — `id` (uuid primary), `payload` (jsonb), `status` (varchar default 'pending'), `processedAt` (timestamp nullable), `createdAt` (timestamp default now).

**Routes** — create `src/health/` and `src/status/`:
- `GET /api/health` → returns `{ ok: true }` unconditionally. No service calls.
- `GET /api/status` → returns `{ db: 'ok'|'error', redis: 'ok'|'error', queue: 'ok'|'error', storage: 'ok'|'error', search: 'ok'|'error' }`.
  - `db`: `SELECT 1` via TypeORM DataSource.
  - `redis`: `PING` via Keyv (or cache-manager set/get roundtrip on a short key).
  - `queue`: connect to NATS using `connect({ servers: [\`nats://${QUEUE_HOST}:${QUEUE_PORT}\`], user: QUEUE_USER, pass: QUEUE_PASS })`, then `close()`. Reuse a cached connection if possible. (Use structured client options per Principle 5 — never `nats://user:pass@host`.)
  - `storage`: `HeadBucketCommand` on the bucket via S3Client. MUST use `forcePathStyle: true` and `region: 'us-east-1'` (MinIO compat). Credentials: `accessKeyId`, `secretAccessKey` from env. Endpoint: `http://${STORAGE_APIHOST}:${STORAGE_APIPORT}` or read apiUrl.
  - `search`: GET `/health` on the Meilisearch server via the `meilisearch` npm client. Use the masterKey from env.
  - Each check wraps in try/catch and maps to `'error'` string. Content-Type MUST be `application/json`.

**Service client modules** — create `src/clients/*.module.ts` providing singleton instances:
- `db` — TypeOrmModule already covers; no extra module needed here.
- `redis` / `cache` — via CacheModule above.
- `queue/nats.client.ts` — exports a lazy NATS connection factory and a `close` hook.
- `storage/s3.client.ts` — exports S3Client configured as described.
- `search/meili.client.ts` — exports a MeiliSearch client instance.

Export these as providers in AppModule so StatusController can inject them.

**Migrations** — create `src/migrate.ts` — compiled to `dist/migrate.js`. Use TypeORM's DataSource with `synchronize: false` and run the two-entity schema sync via explicit CREATE TABLE statements or TypeORM's `dataSource.synchronize()` (safe for initial bootstrap but gated by this one-shot script). Add an index on `items(title)`. Exit 0 on success, non-zero on failure.

**Seed** — create `src/seed.ts`:
- Insert 3–5 items with varied titles/descriptions (so search demo has material).
- Create the `items` Meilisearch index; configure `searchableAttributes: ['title', 'description']`.
- Add the seeded items to Meili via `index.addDocuments` and `await client.tasks.waitForTask(task.taskUid)` — MUST await completion (Principle/loud-failure rule).
- Exit non-zero if any step fails — NO broad try/catch that swallows and returns 0.
- Idempotency: guard only on a fixed key (use `initCommands: zsc execOnce bootstrap-seed-v1` shape later in zerops.yaml) — do NOT short-circuit by row count.

**DO NOT WRITE**:
- `README.md` — delete any Nest scaffolder emits.
- `zerops.yaml` — main agent writes it later.
- `.git/` — delete if present.
- `.env` files — Zerops injects OS env vars.
- Any endpoint beyond /api/health and /api/status. NO items CRUD, NO cache demo, NO uploads, NO search endpoint, NO jobs endpoint. Those come later.

**Env var names your code must read** (from OS env — ConfigModule exposes them):
- `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASS`, `DB_NAME`
- `REDIS_HOST`, `REDIS_PORT`
- `QUEUE_HOST`, `QUEUE_PORT`, `QUEUE_USER`, `QUEUE_PASS`
- `STORAGE_APIHOST`, `STORAGE_APIURL`, `STORAGE_BUCKETNAME`, `STORAGE_ACCESSKEYID`, `STORAGE_SECRETACCESSKEY`
- `SEARCH_HOST`, `SEARCH_PORT`, `SEARCH_MASTERKEY`
- `APP_SECRET` (project-level, for future session/JWT use)
- `STAGE_APP_URL`, `DEV_APP_URL` (for CORS allowlist — set CORS to accept both)
- `NODE_ENV`, `PORT`

Set CORS via `app.enableCors({ origin: [process.env.DEV_APP_URL, process.env.STAGE_APP_URL].filter(Boolean), credentials: true })` in main.ts.

**Installation + smoke validation**:
After writing all files, run:
- `ssh apidev "cd /var/www && npm install"`
- `ssh apidev "cd /var/www && npm run build"` — must succeed (tsc compilation).

### Pre-ship self-verification

Run on zcp (substitute HOST=apidev) and report exit code:

```bash
HOST=apidev
MOUNT=/var/www/$HOST
FAIL=0

# Self-shadow in zerops.yaml (N/A — you don't write zerops.yaml, so skip)

# app.listen with 0.0.0.0
if grep -rq "app\.listen" $MOUNT/src/ 2>/dev/null; then
  if ! grep -rqE "'0\.0\.0\.0'|\"0\.0\.0\.0\"|0\.0\.0\.0" $MOUNT/src/; then
    echo "FAIL: app.listen without 0.0.0.0"; FAIL=1
  fi
fi

# Trust proxy
if ! grep -rq "trust proxy" $MOUNT/src/; then
  echo "FAIL: trust proxy not set"; FAIL=1
fi

# S3Client forcePathStyle
if grep -rq "S3Client" $MOUNT/src/; then
  if ! grep -rqE "forcePathStyle:\s*true" $MOUNT/src/; then
    echo "FAIL: S3Client without forcePathStyle: true"; FAIL=1
  fi
fi

# NATS creds NOT URL-embedded
if grep -rnE "'nats://[^']*:[^']*@|\"nats://[^\"]*:[^\"]*@" $MOUNT/src/ 2>/dev/null | head -1; then
  echo "FAIL: URL-embedded NATS creds"; FAIL=1
fi

# Valkey/Redis no :password@
if grep -rnE "redis://[^@]*:[^@]*@" $MOUNT/src/ 2>/dev/null | head -1; then
  echo "FAIL: Redis/Valkey :password@ — managed service has no auth"; FAIL=1
fi

# .gitignore
if [ ! -f $MOUNT/.gitignore ] || ! grep -qE "^(\s*node_modules|\s*/node_modules)" $MOUNT/.gitignore; then
  echo "FAIL: .gitignore missing or does not ignore node_modules"; FAIL=1
fi

# No residual .git/
if ssh $HOST "test -d /var/www/.git" 2>/dev/null; then
  echo "FAIL: /var/www/.git on apidev — delete before returning"; FAIL=1
fi

# No .env shadow
if [ -f $MOUNT/.env ] && [ ! -f $MOUNT/.env.example ]; then
  echo "FAIL: .env without .env.example — dotenv shadows OS vars"; FAIL=1
fi

# No README
if [ -f $MOUNT/README.md ]; then
  echo "FAIL: README.md exists — delete before returning"; FAIL=1
fi

# No zerops.yaml (main agent writes)
if [ -f $MOUNT/zerops.yaml ]; then
  echo "FAIL: zerops.yaml exists — main agent's job, delete"; FAIL=1
fi

exit $FAIL
```

Must exit 0. Don't leave the script in the codebase.

### Reporting back

Return: files written, scaffolder outputs kept/modified/deleted, pre-ship exit code, principle translations applied (cite principle # + idiom). Do not claim feature implementation.
```
