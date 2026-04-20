# Dispatch 2 — scaffold-apidev-nestjs-api

**Dispatched at**: `2026-04-20T10:24:38`
**Description**: Scaffold apidev NestJS API
**Subagent type**: `general-purpose`
**Prompt length**: 15627 chars
**Tool-use id**: `toolu_01KxTBHSua3vydGk8zpscLU4`

---

## Transmitted prompt (verbatim)

```
You are a scaffolding sub-agent for the `nestjs-showcase` Zerops recipe. Scaffold the **API codebase** (a NestJS backend) into the SSHFS mount `/var/www/apidev/`. Your working directory is the zcp orchestrator container; `/var/www/apidev/` is a network mount to the dev container `apidev`.

**⚠ TOOL-USE POLICY — read before your first tool call.**

You are a sub-agent. Workflow state is main-agent-only.

**Permitted tools:** Read, Edit, Write, Grep, Glob against `/var/www/apidev/`; Bash ONLY via `ssh apidev "..."`; `mcp__zerops__zerops_dev_server`, `mcp__zerops__zerops_knowledge`, `mcp__zerops__zerops_logs`, `mcp__zerops__zerops_discover`.

**Forbidden:** `zerops_workflow`, `zerops_import`, `zerops_env`, `zerops_deploy`, `zerops_subdomain`, `zerops_mount`, `zerops_verify`. Calling any is a sub-agent-misuse bug.

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

**⚠ CRITICAL: where commands run (read this FIRST, before writing any files)**

You are on zcp. `/var/www/apidev/` is an SSHFS mount. File writes work through the mount. Every `npm install`, `nest build`, `tsc` MUST run via:
```
ssh apidev "cd /var/www && <command>"
```
NEVER `cd /var/www/apidev && <command>`. The zcp user id differs from the container's `zerops` user — running install on zcp leaves `node_modules` with wrong ownership and broken `.bin/` symlinks.

**⚠ Framework scaffolder auto-init git.** The NestJS CLI `nest new` runs `git init` by default. Pass `--skip-git` (or delete `/var/www/.git` after). After the scaffolder returns: `ssh apidev "rm -rf /var/www/.git"`.

## Service plan context

You are scaffolding an API that connects to ALL these managed services:
- `db` — PostgreSQL 18. Env vars: `${db_hostname}`, `${db_port}`, `${db_user}`, `${db_password}`, `${db_dbName}`.
- `redis` — Valkey 7.2 (no auth). Env vars: `${redis_hostname}`, `${redis_port}`.
- `queue` — NATS 2.12 (auth required — password MAY contain URL-reserved chars). Env vars: `${queue_hostname}`, `${queue_port}`, `${queue_user}`, `${queue_password}`.
- `storage` — Object Storage (S3-compatible, path-style, MinIO). Env vars: `${storage_apiUrl}`, `${storage_accessKeyId}`, `${storage_secretAccessKey}`, `${storage_bucketName}`.
- `search` — Meilisearch 1.20. Env vars: `${search_hostname}`, `${search_port}`, `${search_masterKey}`.

Project-level env vars already set: `APP_SECRET`, `DEV_API_URL`, `DEV_APP_URL`, `STAGE_API_URL`, `STAGE_APP_URL`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASS`, `MAIL_FROM`.

## What to scaffold

A **health-dashboard-only** NestJS API scaffold. You write infrastructure. You do NOT write feature endpoints (no /api/items, /api/cache, /api/files, /api/search, /api/jobs, /api/mail). The feature sub-agent implements those after deploy.

### Step 1 — Scaffold NestJS

```
ssh apidev "cd /var/www && npx -y @nestjs/cli new . --skip-git --skip-install --package-manager npm"
```

This creates the standard nest directory layout (src/, test/, eslint, prettier, tsconfig, etc.). Then read the files the scaffolder emitted before editing them.

### Step 2 — Modify / add files

Install additional deps:
```
ssh apidev "cd /var/www && npm install --save @nestjs/typeorm typeorm pg @nestjs/config cache-manager cache-manager-redis-yet redis ioredis nats @aws-sdk/client-s3 meilisearch @nestjs-modules/mailer nodemailer nestjs-pino pino-http pino-pretty"
ssh apidev "cd /var/www && npm install --save-dev @types/nodemailer"
```

Now write these files (on the mount — use Write/Edit):

**`src/main.ts`** — replace the scaffolder default:
- Use `NestFactory.create<NestExpressApplication>(AppModule, { bufferLogs: true })`.
- `useLogger(app.get(Logger))` from `nestjs-pino`.
- Set global prefix: `app.setGlobalPrefix('api')`.
- `app.enableCors({ origin: true, credentials: true })`.
- `app.getHttpAdapter().getInstance().set('trust proxy', true)` — required behind Zerops L7.
- Listen: `await app.listen(process.env.PORT ?? 3000, '0.0.0.0')` — MANDATORY bind to 0.0.0.0.
- Enable shutdown hooks: `app.enableShutdownHooks()`.

**`src/app.module.ts`** — compose the app:
- Import `ConfigModule.forRoot({ isGlobal: true })`.
- Import `LoggerModule.forRoot({ pinoHttp: { transport: process.env.NODE_ENV !== 'production' ? { target: 'pino-pretty' } : undefined } })` from nestjs-pino.
- Import `TypeOrmModule.forRoot` with postgres driver reading from `process.env.DB_*` — `synchronize: false` always, `entities: [Item, Job]`, `autoLoadEntities: true`.
- Import `HealthModule`, `StatusModule`, `ServicesModule`. Each module we add ourselves.
- Keep AppController/AppService only for `/` route returning `{ service: 'nestjs-showcase-api', version: 1 }`.

**`src/health/health.module.ts`** and **`src/health/health.controller.ts`**:
- `GET /api/health` returns `{ ok: true }` with `Content-Type: application/json`. No service calls.

**`src/status/status.module.ts`** and **`src/status/status.controller.ts`**:
- `GET /api/status` returns a flat object `{ db, redis, nats, storage, search }` with values `"ok"` or `"error"`. Each value comes from a live ping of the service via the `ServicesModule` clients.
  - `db`: `SELECT 1` via TypeORM DataSource.query.
  - `redis`: `PING` via ioredis.
  - `nats`: use the injected NatsConnection's `flush()` or `rtt()` method.
  - `storage`: `HeadBucketCommand` via S3Client.
  - `search`: `meilisearch.health()` or `.isHealthy()`.
- Wrap each call in try/catch and return `"error"` on failure. Return 200 OK regardless (this endpoint is for the status UI, not the platform health probe).

**`src/services/services.module.ts`** — exports singleton clients for:
- **S3** — create `new S3Client({ endpoint: process.env.STORAGE_ENDPOINT, region: process.env.STORAGE_REGION ?? 'us-east-1', credentials: { accessKeyId, secretAccessKey }, forcePathStyle: true })`. `forcePathStyle: true` is MANDATORY for MinIO. Export via a provider token `'S3'`.
- **Redis (ioredis)** — `new Redis({ host: process.env.REDIS_HOST, port: Number(process.env.REDIS_PORT), lazyConnect: false })`. NO password (Zerops Valkey has no auth). Export via `'REDIS'`.
- **NATS** — use the `nats` package (v2 client): `await connect({ servers: [`${host}:${port}`], user, pass })`. Pass user/pass as SEPARATE options, NEVER embed in URL — Zerops NATS passwords may contain URL-reserved chars. Export via `'NATS'`.
- **Meilisearch** — `new MeiliSearch({ host: `http://${SEARCH_HOST}:${SEARCH_PORT}`, apiKey: process.env.SEARCH_MASTER_KEY })`. Export via `'MEILI'`.
- Also export a **CacheManager** from `cache-manager-redis-yet` wrapping the redis-client library:
  ```ts
  const redisStore = await redisStore({ url: `redis://${process.env.REDIS_HOST}:${process.env.REDIS_PORT}` });
  ```
  (The module provides a wrapper — check the installed package for the exact import.)

All clients are created in async providers with `useFactory`. Handle `onModuleDestroy` to close the connections cleanly — use NestJS's `OnModuleDestroy` interface. Principle 1 (graceful shutdown) is satisfied here.

**`src/entities/item.entity.ts`** — TypeORM entity `Item`: id (pk, uuid default), title (varchar 200, not null), description (text), createdAt (timestamptz default now). The feature sub-agent will add routes that read/write this.

**`src/entities/job.entity.ts`** — TypeORM entity `Job`: id (uuid pk), payload (jsonb), status (varchar 20, default 'pending'), result (text nullable), createdAt, processedAt (nullable). Feature sub-agent adds routes.

**`src/migrate.ts`** — standalone migration script. Use TypeORM `DataSource`:
```ts
import 'reflect-metadata';
import { DataSource } from 'typeorm';
import { Item } from './entities/item.entity';
import { Job } from './entities/job.entity';

const ds = new DataSource({
  type: 'postgres',
  host: process.env.DB_HOST,
  port: Number(process.env.DB_PORT),
  username: process.env.DB_USER,
  password: process.env.DB_PASS,
  database: process.env.DB_NAME,
  entities: [Item, Job],
  synchronize: false,
  logging: true,
});

async function main() {
  await ds.initialize();
  await ds.query(`
    CREATE TABLE IF NOT EXISTS item (
      id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
      title varchar(200) NOT NULL,
      description text NOT NULL DEFAULT '',
      created_at timestamptz NOT NULL DEFAULT now()
    );
  `);
  await ds.query(`
    CREATE TABLE IF NOT EXISTS job (
      id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
      payload jsonb NOT NULL DEFAULT '{}'::jsonb,
      status varchar(20) NOT NULL DEFAULT 'pending',
      result text,
      created_at timestamptz NOT NULL DEFAULT now(),
      processed_at timestamptz
    );
  `);
  await ds.destroy();
  console.log('migrate: ok');
}

main().catch((e) => { console.error('migrate: FAILED', e); process.exit(1); });
```

The explicit process.exit(1) on failure is the loud-failure rule — execOnce must record failure so deploy sweep catches it.

**`src/seed.ts`** — idempotency via static key in zerops.yaml `initCommands` (`zsc execOnce bootstrap-seed-v1`), NOT row count. Always insert 5 items + sync to Meilisearch:
```ts
import 'reflect-metadata';
import { DataSource } from 'typeorm';
import { MeiliSearch } from 'meilisearch';
import { Item } from './entities/item.entity';
import { Job } from './entities/job.entity';

const ds = new DataSource({ /* same as migrate */ });

async function main() {
  await ds.initialize();
  const repo = ds.getRepository(Item);
  const existing = await repo.count();
  const samples = [
    { title: 'Demo Item 1', description: 'First showcase item' },
    { title: 'Demo Item 2', description: 'Second showcase item' },
    { title: 'NestJS on Zerops', description: 'Platform recipe entry' },
    { title: 'Meilisearch sample', description: 'Full-text search target' },
    { title: 'Valkey cache demo', description: 'Cache layer example' },
  ];
  if (existing === 0) {
    await repo.insert(samples);
    console.log('seed: inserted', samples.length, 'items');
  } else {
    console.log('seed: items already present, skipping inserts');
  }

  // ALWAYS sync to Meilisearch — the guard must NOT skip search-index warmup.
  const meili = new MeiliSearch({
    host: `http://${process.env.SEARCH_HOST}:${process.env.SEARCH_PORT}`,
    apiKey: process.env.SEARCH_MASTER_KEY,
  });
  const index = meili.index('items');
  const all = await repo.find();
  const task = await index.addDocuments(all.map((i) => ({
    id: i.id,
    title: i.title,
    description: i.description,
    createdAt: i.createdAt,
  })));
  await meili.waitForTask(task.taskUid);
  console.log('seed: synced', all.length, 'items to meilisearch');

  await ds.destroy();
}

main().catch((e) => { console.error('seed: FAILED', e); process.exit(1); });
```

**`.gitignore`** — ensure it contains `node_modules`, `dist`, `.env` (but not `.env.example`). NestJS scaffolder's default gitignore is close — just verify.

**`.env.example`** — documenting every env var the app reads. Commented list of `DB_*`, `REDIS_*`, `NATS_*`, `STORAGE_*`, `SEARCH_*`, `SMTP_*`, `APP_SECRET`, `MAIL_FROM`, `STAGE_APP_URL`, `DEV_APP_URL` (for CORS if needed).

**DO NOT WRITE:**
- `README.md` — the main agent writes at deploy-readmes. Delete if nest scaffold created one.
- `zerops.yaml` — main agent writes after scaffold returns.
- `.git/` — delete after scaffolder (`ssh apidev "rm -rf /var/www/.git"`).
- Feature routes — `/api/items`, `/api/cache`, `/api/search`, `/api/files`, `/api/jobs`, `/api/mail`. Owned by feature sub-agent.

## Scaffold pre-flight — platform principles

- **Principle 1 — Graceful shutdown**: `app.enableShutdownHooks()` in main.ts + `OnModuleDestroy` on ServicesModule to close pg, redis, nats connections. Required.
- **Principle 2 — Routable bind**: `app.listen(port, '0.0.0.0')`. Required.
- **Principle 3 — Proxy trust**: `app.getHttpAdapter().getInstance().set('trust proxy', true)`. Required.
- **Principle 5 — Structured credentials for NATS**: pass `user`/`pass` to `nats.connect()` as separate options, never embed in `servers` URL. Required.

Record each as a `record_fact` scope=both after implementation.

## Pre-ship self-verification (MANDATORY)

Run this and ensure exit 0:

```bash
HOST=apidev
MOUNT=/var/www/$HOST
FAIL=0

# Assertion 1 — app.listen binds 0.0.0.0
grep -rn "app.listen" $MOUNT/src/ 2>/dev/null | grep -v node_modules
if ! grep -rq "0.0.0.0" $MOUNT/src/main.ts; then
    echo "FAIL: main.ts missing 0.0.0.0 bind"
    FAIL=1
fi

# Assertion 2 — trust proxy
if ! grep -q "trust proxy" $MOUNT/src/main.ts; then
    echo "FAIL: main.ts missing trust proxy"
    FAIL=1
fi

# Assertion 3 — S3Client uses forcePathStyle
if grep -rq "S3Client" $MOUNT/src/; then
    if ! grep -rq "forcePathStyle: true" $MOUNT/src/; then
        echo "FAIL: S3Client without forcePathStyle: true"
        FAIL=1
    fi
fi

# Assertion 4 — NATS connect uses separate user/pass, not URL-embedded
if grep -rnE "'nats://[^']*:[^']*@|\"nats://[^\"]*:[^\"]*@" $MOUNT/src/ 2>/dev/null | head -1; then
    echo "FAIL: URL-embedded NATS creds"
    FAIL=1
fi

# Assertion 5 — Valkey/Redis connection without :password@
if grep -rnE "redis://[^@]*:[^@]*@|valkey://[^@]*:[^@]*@" $MOUNT/src/ 2>/dev/null | head -1; then
    echo "FAIL: Valkey connection with :password@"
    FAIL=1
fi

# Assertion 6 — no README.md
if [ -f $MOUNT/README.md ]; then
    echo "FAIL: README.md must not exist at generate-complete"
    FAIL=1
fi

# Assertion 7 — no zerops.yaml
if [ -f $MOUNT/zerops.yaml ]; then
    echo "FAIL: zerops.yaml must not exist at generate-complete"
    FAIL=1
fi

# Assertion 8 — no .git on container
if ssh $HOST "test -d /var/www/.git" 2>/dev/null; then
    echo "FAIL: /var/www/.git exists on apidev — delete before returning"
    FAIL=1
fi

# Assertion 9 — .env.example exists and no .env file
if [ -f $MOUNT/.env ]; then
    echo "FAIL: .env exists — delete to avoid shadowing OS env vars"
    FAIL=1
fi
if [ ! -f $MOUNT/.env.example ]; then
    echo "FAIL: .env.example missing"
    FAIL=1
fi

# Assertion 10 — .gitignore covers node_modules
if ! grep -qE "^(\s*node_modules|\s*/node_modules)" $MOUNT/.gitignore 2>/dev/null; then
    echo "FAIL: .gitignore missing node_modules entry"
    FAIL=1
fi

exit $FAIL
```

## Build verification

After writing, verify compilation:
```
ssh apidev "cd /var/www && npm run build 2>&1 | tail -40"
```
If TypeScript errors appear, fix them before returning. Do NOT start a dev server — the main agent handles smoke testing.

## Reporting back

Return a bulleted list of files you wrote, the pre-ship script exit code (must be 0), the `npm run build` result, and every `record_fact` call you made. Report env var names your code reads per managed service. Do NOT claim to have implemented any of the 6 showcase features.
```
