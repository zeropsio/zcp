# Dispatch 2 — scaffold-apidev-nestjs-api

**Dispatched at**: `2026-04-21T14:21:56`
**Description**: Scaffold apidev (NestJS API)
**Subagent type**: `general-purpose`
**Prompt length**: 10663 chars
**Tool-use id**: `toolu_01B7mnkzhgU3LK5nHaLQveyf`

---

## Transmitted prompt (verbatim)

```
You are a scaffolding sub-agent in the Zerops recipe workflow. Your codebase is **apidev** — a NestJS JSON API backing a Svelte SPA showcase. Mount: `/var/www/apidev/`. SSH hostname: `apidev`.

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

**Principle 1 — Graceful shutdown within thirty seconds** — on SIGTERM, stop accepting new work, await in-flight work, close DB/broker/cache/storage/search connections, exit cleanly. NestJS idiom: `app.enableShutdownHooks()` + module `onModuleDestroy()` lifecycle hooks on each client.

**Principle 2 — Routable network binding** — bind `0.0.0.0`, not `localhost`. NestJS idiom: `await app.listen(port, '0.0.0.0')`.

**Principle 3 — Client-origin awareness behind a proxy** — set trust-proxy for one upstream hop. NestJS+Express idiom: `app.getHttpAdapter().getInstance().set('trust proxy', true)`.

**Principle 5 — Structured credential passing** — pass user/password as structured client options, NOT URL-embedded. Matters for NATS (`nats://user:pass@host` is stripped silently by `nats` v2 client — use `{ servers, user, pass }` options). Postgres/TypeORM options object handles this correctly by default; Meilisearch uses `{ apiKey }`; S3 uses credentials object.

<<<END MANDATORY>>>

## Your job

Build a NestJS health-dashboard skeleton:
- `GET /api/health` — liveness returns `{ ok: true }` JSON
- `GET /api/status` — deep connectivity check returning `{ db, cache, queue, storage, search }` each `"ok"` or `"error"`
- Service clients initialized from env vars for all managed services in the plan
- TypeORM entities + migrations for `items` (the primary data model the feature sub-agent will extend)
- Seed script seeding 3-5 sample items, idempotent by static key
- **NO feature endpoints.** The feature sub-agent at deploy step 4b writes CRUD, cache-demo, search, jobs-dispatch, storage-upload routes.

## Env var contract (from provision)

- `db`: `db_hostname`, `db_port`, `db_user`, `db_password`, `db_dbName`
- `cache`: `cache_hostname`, `cache_port`
- `queue`: `queue_hostname`, `queue_port`, `queue_user`, `queue_password`
- `storage`: `storage_hostname`, `storage_apiUrl`, `storage_bucketName`, `storage_accessKeyId`, `storage_secretAccessKey`
- `search`: `search_hostname`, `search_port`, `search_masterKey`
- Project-level: `APP_SECRET`, `STAGE_APP_URL`, `DEV_APP_URL`

These are OS env vars in the container at runtime. Map them to your NestJS config keys (e.g. `DB_HOST=process.env.db_hostname` pattern — or read the `*_hostname` keys directly; no remapping needed if code reads them verbatim).

## Steps

1. **Scaffold NestJS** via `ssh apidev "cd /var/www && npx --yes @nestjs/cli@latest new . --package-manager npm --skip-git"`. The CLI prompts for package manager — `--package-manager npm` avoids the prompt. `--skip-git` prevents the scaffolder's `git init`.

2. **Install runtime deps**: `ssh apidev "cd /var/www && npm install @nestjs/typeorm typeorm pg ioredis nats @aws-sdk/client-s3 meilisearch"` — all clients for managed services. Also dev deps: `npm install -D ts-node tsconfig-paths @types/node`.

3. **Read scaffolded files before any Edit** (batched Read calls): `package.json`, `tsconfig.json`, `tsconfig.build.json`, `nest-cli.json`, `.gitignore`, `src/main.ts`, `src/app.module.ts`, `src/app.controller.ts`, `src/app.service.ts`.

4. **Delete leftover .git** if scaffolder created one: `ssh apidev "rm -rf /var/www/.git"`.

5. **Rewrite `src/main.ts`**: bootstrap, trust proxy, shutdown hooks, listen 0.0.0.0:3000.
   ```ts
   const app = await NestFactory.create(AppModule, { cors: true });
   app.getHttpAdapter().getInstance().set('trust proxy', true);
   app.enableShutdownHooks();
   app.setGlobalPrefix('api', { exclude: [] });
   await app.listen(Number(process.env.PORT ?? 3000), '0.0.0.0');
   ```
   Note: `setGlobalPrefix('api')` means controllers write routes without the `/api` prefix and the platform serves them at `/api/...`.

6. **Write `src/entities/item.entity.ts`**: TypeORM `@Entity('items')` with columns: `id` (uuid PK), `title` (varchar), `description` (text nullable), `createdAt` (timestamp with default). The feature sub-agent will extend.

7. **Write `src/data-source.ts`**: TypeORM DataSource config reading `process.env.db_hostname`, `process.env.db_port` (parseInt), `process.env.db_user`, `process.env.db_password`, `process.env.db_dbName`. Options: `type: 'postgres'`, `synchronize: false`, `migrations: [path.join(__dirname, 'migrations/*.{js,ts}')]`, `entities: [Item]`.

8. **Write `src/migrations/1700000000000-InitSchema.ts`**: TypeORM migration creating the `items` table (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), title varchar(255) NOT NULL, description text, created_at timestamptz NOT NULL DEFAULT now()). Use `CREATE EXTENSION IF NOT EXISTS "pgcrypto";` at the top of `up()` for `gen_random_uuid()`.

9. **Write `src/migrate.ts`**: script that runs `dataSource.initialize()` then `dataSource.runMigrations()`, then `dataSource.destroy()`. Loud-fail: unhandled rejection = process exit 1.

10. **Write `src/seed.ts`**: idempotent by static key (check for 3 specific items by fixed ids; skip if present; insert otherwise). Loud-fail on errors. 3-5 rows with varied titles.

11. **Write service-client modules** under `src/services/`:
    - `cache.service.ts` — ioredis client connecting to `cache_hostname:cache_port` (no password — Zerops managed Valkey has no auth). `onModuleDestroy` closes it. Expose `.ping()`.
    - `queue.service.ts` — NATS client from `nats` package. Connect via `{ servers: [`nats://${process.env.queue_hostname}:${process.env.queue_port}`], user: process.env.queue_user, pass: process.env.queue_password }`. **Structured credentials — never URL-embedded** (Principle 5). Expose `.publish(subject, payload)`, `.subscribe(...)`, graceful close.
    - `storage.service.ts` — S3Client from `@aws-sdk/client-s3`: `endpoint: process.env.storage_apiUrl`, `region: 'us-east-1'`, `forcePathStyle: true`, credentials from `storage_accessKeyId`/`storage_secretAccessKey`. Expose `.headBucket()` for health check.
    - `search.service.ts` — MeiliSearch client: `host: http://${search_hostname}:${search_port}`, `apiKey: process.env.search_masterKey`. Expose `.health()`.

12. **Write `src/app.module.ts`**: wires `TypeOrmModule.forRoot({ ...dataSourceOptions, autoLoadEntities: true })`, registers cache/queue/storage/search service providers globally.

13. **Write `src/app.controller.ts`**:
    - `GET health` → `{ ok: true }` (route is `/api/health` because of global prefix).
    - `GET status` → parallel ping each service; return `{ db: 'ok'|'error', cache: ..., queue: ..., storage: ..., search: ... }`. Each key is `"ok"` on success, `"error"` on exception. Wrap each in try/catch so one failure doesn't collapse the response.

14. **Write `.env.example`** documenting expected env vars (reference Zerops env var names: `db_hostname`, `cache_hostname`, etc. — plus `APP_SECRET`, `STAGE_APP_URL`).

15. **Ensure `.gitignore`** covers `node_modules`, `dist`, `.env`.

16. **Pre-ship assertions** (inline `bash -c`, not saved):

```
HOST=apidev
MOUNT=/var/www/$HOST
FAIL=0
# Listen 0.0.0.0
grep -q "'0\.0\.0\.0'\|\"0\.0\.0\.0\"" $MOUNT/src/main.ts || { echo "FAIL: main.ts missing 0.0.0.0 bind"; FAIL=1; }
# trust proxy
grep -q "trust proxy" $MOUNT/src/main.ts || { echo "FAIL: missing trust proxy"; FAIL=1; }
# shutdown hooks
grep -q "enableShutdownHooks" $MOUNT/src/main.ts || { echo "FAIL: missing enableShutdownHooks"; FAIL=1; }
# forcePathStyle for S3
grep -rq "forcePathStyle:\s*true" $MOUNT/src/services/ || { echo "FAIL: S3Client missing forcePathStyle: true"; FAIL=1; }
# No NATS URL-embedded creds
grep -rnE "'nats://[^']*:[^']*@|\"nats://[^\"]*:[^\"]*@" $MOUNT/src/ 2>/dev/null | head -1 && { echo "FAIL: URL-embedded NATS creds"; FAIL=1; } || true
# No Valkey password in URL
grep -rnE "redis://[^@]*:[^@]*@|valkey://[^@]*:[^@]*@" $MOUNT/src/ 2>/dev/null | head -1 && { echo "FAIL: Valkey URL with creds"; FAIL=1; } || true
# .gitignore
[ -f $MOUNT/.gitignore ] && grep -qE "^(\s*node_modules|\s*/node_modules)" $MOUNT/.gitignore || { echo "FAIL: .gitignore missing node_modules"; FAIL=1; }
# No leftover .git
ssh $HOST "test -d /var/www/.git" 2>/dev/null && { echo "FAIL: /var/www/.git exists"; FAIL=1; } || true
# No README.md
[ -f $MOUNT/README.md ] && { echo "FAIL: README.md must not exist at generate"; FAIL=1; } || true
# No zerops.yaml
[ -f $MOUNT/zerops.yaml ] && { echo "FAIL: zerops.yaml must not exist at generate"; FAIL=1; } || true
exit $FAIL
```

Run compile smoke after assertions: `ssh apidev "cd /var/www && npx tsc --noEmit"` must exit 0.

17. **Do NOT write**: `zerops.yaml`, `README.md`, items CRUD endpoints, cache-demo endpoints, search endpoints, jobs-dispatch endpoints, storage-upload endpoints.

## Return

Report files written, env vars wired per managed service, assertion exit code (must 0), and `npx tsc --noEmit` exit code (must 0). Under 200 words.
```
