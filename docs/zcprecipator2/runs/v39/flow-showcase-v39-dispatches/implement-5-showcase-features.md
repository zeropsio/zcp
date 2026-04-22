# Dispatch 4 — implement-5-showcase-features

**Dispatched at**: `2026-04-22T18:45:15`
**Description**: Implement 5 showcase features
**Subagent type**: `general-purpose`
**Prompt length**: 16857 chars
**Tool-use id**: `toolu_01ALwmvmYTAmiRP9ZtUFkfvx`

---

## Transmitted prompt (verbatim)

```
# mandatory-core

You are the FEATURE sub-agent for the nestjs-showcase Zerops recipe. Your job is to implement the declared 5-feature list end-to-end across THREE mounts as one coherent author. Workflow state belongs elsewhere; provisioning, deploy orchestration, and step completion are outside your scope.

## Tool-use policy

Permitted tools:
- File ops: `Read`, `Edit`, `Write`, `Grep`, `Glob` against `/var/www/{appdev,apidev,workerdev}/`
- `Bash` — only in shape `ssh {hostname} "cd /var/www && <command>"` (or zcp-side mount inspection)
- `mcp__zerops__zerops_dev_server` — start/stop/status/logs/restart for dev processes
- `mcp__zerops__zerops_knowledge` — on-demand platform knowledge
- `mcp__zerops__zerops_logs` — container logs
- `mcp__zerops__zerops_discover` — service introspection
- `mcp__zerops__zerops_record_fact` — record facts at moment of freshest knowledge

Forbidden tools (server returns SUBAGENT_MISUSE): `mcp__zerops__zerops_workflow`, `_import`, `_env`, `_deploy`, `_subdomain`, `_mount`, `_verify`. Workflow state is main-agent-only.

## File-op sequencing

Every Edit must be preceded by a Read of the same file in this session. Plan up front: batch-Read every file you intend to modify across every mount before the first Edit.

## Where executables run

Each mount is a write surface only. Every executable (compilers, type-checkers, package managers, framework CLIs, git, app-level curl) runs inside its target container via `ssh {hostname} "cd /var/www && <command>"`. File writes use Write/Edit/Read against the mount.

---

# SymbolContract (authoritative — DO NOT diverge)

```json
{
  "envVarsByKind": {
    "db":      { "host": "DB_HOST",    "port": "DB_PORT",    "user": "DB_USER",    "pass": "DB_PASS",    "name": "DB_NAME" },
    "cache":   { "host": "REDIS_HOST", "port": "REDIS_PORT" },
    "queue":   { "host": "NATS_HOST",  "port": "NATS_PORT",  "user": "NATS_USER",  "pass": "NATS_PASS" },
    "storage": { "accessKey": "S3_ACCESS_KEY", "secretKey": "S3_SECRET_KEY", "endpoint": "S3_ENDPOINT", "bucket": "S3_BUCKET", "region": "S3_REGION" },
    "search":  { "host": "MEILI_HOST", "port": "MEILI_PORT", "key": "MEILI_KEY" }
  },
  "httpRoutes": {
    "health":      "/api/health",
    "status":      "/api/status",
    "items_list":  "/api/items",
    "items_create":"/api/items",
    "items_delete":"/api/items/:id",
    "cache_read":  "/api/cache/:key",
    "cache_write": "/api/cache",
    "files_list":  "/api/files",
    "files_upload":"/api/files",
    "search_query":"/api/search",
    "jobs_list":   "/api/jobs",
    "jobs_dispatch":"/api/jobs"
  },
  "natsSubjects": { "job_dispatch": "jobs.dispatch" },
  "natsQueues":   { "workers": "showcase-workers" },
  "hostnames": [
    { "role": "app",    "dev": "appdev",    "stage": "appstage" },
    { "role": "api",    "dev": "apidev",    "stage": "apistage" },
    { "role": "worker", "dev": "workerdev", "stage": "workerstage" }
  ],
  "dtos": ["Item","ItemCreateBody","JobRecord","JobDispatchBody","CachePutBody","CacheGetResponse","FileRecord","SearchHit","StatusReport"]
}
```

The contract is the binding for cross-codebase names: env keys, route paths, NATS subject + queue group, DTO interface names. Read names BYTE-IDENTICALLY from this contract — do NOT abbreviate or paraphrase.

---

# Existing scaffold (do NOT redo, only extend)

| Mount | Stack | Already provided |
|---|---|---|
| `/var/www/apidev/` | NestJS 11 + Express + TypeORM + Postgres | `src/main.ts` (0.0.0.0 + trust proxy + shutdown hooks + CORS), `src/app.module.ts`, `src/clients/{redis,nats,s3,meili}.client.ts` (all `@Global` providers — REDIS_CLIENT, NATS_CLIENT, S3_CLIENT, S3_BUCKET, MEILI_CLIENT exported tokens), `src/entities/item.entity.ts`, `src/health`, `src/status`, `src/migrate.ts`, `src/seed.ts` (5 rows incl. 'welcome'), `src/search-import.ts`. The 5 feature modules do NOT exist yet — you create them. |
| `/var/www/appdev/` | Svelte 5 + Vite + TypeScript | `src/lib/api.ts` (the canonical helper — every fetch goes through `api(path)`), `src/lib/StatusPanel.svelte` (polls /api/status, one row per service), `src/App.svelte` (mounts only `<StatusPanel />` inside `<main data-feature="status">`). You add 5 more feature panels and mount them in App.svelte. |
| `/var/www/workerdev/` | NestJS 11 standalone context | `src/main.ts` (createApplicationContext + SIGTERM drain), `src/app.module.ts` (TypeORM, autoLoadEntities), `src/worker/nats.connection.ts` (NATS_CONNECTION + structured creds + drain on destroy), `src/worker/worker.service.ts` (subscribes to `jobs.>` with queue group `showcase-workers`, no-op handler logs subject + JSON). You replace the no-op handler with real job processing for the `jobs-dispatch` feature. |

Existing migrate.ts created the `items` table. The dev DB already has 5 seeded rows: `welcome, getting-started, cache-demo, search-demo, jobs-demo` (use `welcome` as the known-matching search query — DO NOT change this).

---

# Feature list (the contract — implement EXACTLY these 5)

For each feature, implement API + UI (+ worker for jobs-dispatch) as one vertical slice.

## 1. items-crud (surface: api, ui, db)

- API:
  - `GET /api/items` → `Item[]` (id, title, body, createdAt) ordered by id desc
  - `POST /api/items` → 201 `Item` from body `{title: string, body?: string|null}` (validate via `class-validator` — title required, max 200 chars, body optional max 5000)
  - `DELETE /api/items/:id` → 204 No Content
- UI: `<section data-feature="items-crud">` with header "Items", a form (title input + optional body textarea + Add button), then a table `<table>` with `[data-row]` per item (id mono, title, createdAt humanized via `toLocaleString`, "Delete" button). On Add success: refetch list, count of `[data-row]` increases by 1. Loading/error/empty/populated states.
- mustObserve: `[data-feature="items-crud"] [data-row]` count increases by 1 after submit.

## 2. cache-demo (surface: api, ui, cache)

- API: backed by `redis` (Valkey, no auth). TTL: 60 seconds.
  - `POST /api/cache` body `{key: string, value: string}` → 200 `{ok: true, key, ttl: 60}`. Stores via `redis.set(key, value, 'EX', 60)`.
  - `GET /api/cache/:key` → 200 `{key, value: string|null, source: "cache"|"miss"}` (source is `"cache"` if hit, `"miss"` if `null`). Validate key path-segment (alphanumeric + dash + underscore, max 64).
- UI: `<section data-feature="cache-demo">` with two stacked panels: Write (key + value inputs + Write button) and Read (key input + Read button + result display). Result display shows `[data-result]` with the returned value and `[data-source]` with `cache` / `miss`. On a successful write-then-read of the same key, `[data-result]` text equals what was written and `[data-source]` reads `cache`.
- mustObserve: `[data-result]` text equals just-written value; `[data-source]` reads `cache`.

## 3. storage-upload (surface: api, ui, storage)

- API: S3 client already provisioned. Use `S3_BUCKET` token + `forcePathStyle: true`.
  - `GET /api/files` → 200 `FileRecord[]` (`{key, size, lastModified}`). Use `ListObjectsV2Command` (sorted by key desc, max 50).
  - `POST /api/files` accepts `multipart/form-data` with one field `file`. Use `@nestjs/platform-express` + `FileInterceptor` (already installed via @nestjs/platform-express dep — verify in node_modules). Generate a key like `${Date.now()}-${sanitize(originalName)}`. Upload via `PutObjectCommand`. Reject files > 5 MB (return 413). Return 201 `FileRecord` of the new file.
- UI: `<section data-feature="storage-upload">` with file `<input type="file">` + Upload button + below: list `<ul>` with `[data-file]` per uploaded file showing key, size (formatted KB/MB), lastModified humanized. On upload success: refetch list, `[data-file]` count increases.
- mustObserve: `[data-file]` count increases by 1 after upload completes.

## 4. search-items (surface: api, ui, search)

- API:
  - `GET /api/search?q=xxx` → 200 `{hits: SearchHit[]}` where `SearchHit = {id, title, body}`. Empty `q` returns `{hits: []}`. Use `meili.index('items').search(q, { limit: 20 })`.
- UI: `<section data-feature="search-items">` with debounced `<input data-query>` (debounce 400ms via setTimeout) and below: list `<ul>` with `[data-hit]` per hit showing id mono + title (highlight matched substring optionally). Type `welcome` (the seeded item) → at least 1 `[data-hit]` appears.
- mustObserve: `[data-hit]` count > 0 for query `welcome`.

## 5. jobs-dispatch (surface: api, ui, queue, worker)

This is the worker feature.

- New entity `Job` (file `apidev/src/entities/job.entity.ts` AND mirror in `workerdev/src/entities/job.entity.ts` byte-identically — the worker shares the DB):
  ```ts
  @Entity('jobs')
  export class Job {
    @PrimaryGeneratedColumn() id: number;
    @Column() type: string;
    @Column({ type: 'jsonb' }) payload: Record<string, unknown>;
    @Column({ default: 'pending' }) status: 'pending' | 'processing' | 'done' | 'failed';
    @Column({ type: 'text', nullable: true }) result: string | null;
    @CreateDateColumn() createdAt: Date;
    @Column({ type: 'timestamptz', nullable: true }) processedAt: Date | null;
  }
  ```
  Add migration: `CREATE TABLE IF NOT EXISTS jobs(id SERIAL PRIMARY KEY, type TEXT NOT NULL, payload JSONB NOT NULL DEFAULT '{}'::jsonb, status TEXT NOT NULL DEFAULT 'pending', result TEXT, "createdAt" TIMESTAMPTZ DEFAULT NOW(), "processedAt" TIMESTAMPTZ)`. Append to `apidev/src/migrate.ts`.
- API:
  - `POST /api/jobs` body `{type: string, payload?: object}` → 201 `JobRecord`. Insert row in `jobs` (status=pending, processedAt=null). Then publish to NATS subject `jobs.dispatch` with payload `{jobId, type, payload}` via `nats.publish(subject, sc.encode(JSON.stringify(payload)))` (use `StringCodec` from the `nats` package).
  - `GET /api/jobs` → 200 `JobRecord[]` (last 20, ordered by id desc). Each row: `{id, type, payload, status, result, createdAt, processedAt}`.
- WORKER (`workerdev/src/worker/worker.service.ts`): replace the no-op handler. On message:
  1. Parse JSON `{jobId, type, payload}`.
  2. Update DB: `status='processing'` for `jobId`.
  3. "Process" — synthesize a result string like `Processed type=${type} at ${new Date().toISOString()}`. For type `echo` echo the payload as JSON; for type `sleep` await `setTimeout(payload.ms || 100)`; for any other type just record the synthetic result.
  4. Update DB: `status='done', result=<string>, processedAt=NOW()` for `jobId`.
  5. On any error: `status='failed', result=<errorMessage>, processedAt=NOW()`. Log loudly. Do NOT swallow.
  - Worker MUST keep `queue: 'showcase-workers'` queue group on subscription.
- UI: `<section data-feature="jobs-dispatch">` with: form (type select: echo/sleep/noop, optional payload textarea JSON), Dispatch button. Below: table `<table>` with `[data-row]` per recent job: id, type, status (colored badge: pending=gray, processing=yellow, done=green, failed=red), `[data-processed-at]` (humanized createdAt → processedAt or em-dash if null). Auto-refresh every 1s for 10s after Dispatch. On a successful dispatch: within 5s, the latest job's `[data-processed-at]` becomes non-empty.
- mustObserve: `[data-feature="jobs-dispatch"] [data-processed-at]` non-empty within 5s of dispatch (use type `echo` for the test).

---

# How to wire each Nest feature module

Pattern (use this for every new feature):

```
apidev/src/items/
  items.module.ts         // @Module imports TypeOrmModule.forFeature([Item]); declares ItemsService + ItemsController
  items.service.ts        // injects @InjectRepository(Item); methods list/create/delete
  items.controller.ts     // @Controller('items'); routes use class-validator DTOs
  dto/create-item.dto.ts  // class with @IsString @MaxLength etc
```

Then add `ItemsModule` to `apidev/src/app.module.ts` imports. Repeat for `cache`, `files`, `search`, `jobs`. The `entities` field of `TypeOrmModule.forRootAsync` is empty — `autoLoadEntities: true` already covers `Item` + the new `Job`.

For DTOs that the FRONTEND consumes, declare them as TypeScript `interface` at the top of the api controller (NOT class — class is server-side validation, interface is shape contract). Copy the interface byte-identically into the consumer Svelte component.

---

# Frontend pattern

Each new component lives in `appdev/src/lib/{Items,CacheDemo,StorageUpload,SearchItems,JobsDispatch}.svelte`. Mount each in `appdev/src/App.svelte` after `<StatusPanel />`. App.svelte structure:

```svelte
<main>
  <header>
    <h1>NestJS Showcase</h1>
    <p>End-to-end NestJS + every managed Zerops service</p>
  </header>
  <StatusPanel />
  <Items />
  <CacheDemo />
  <StorageUpload />
  <SearchItems />
  <JobsDispatch />
</main>
```

Each panel: `<section data-feature="{id}">` with `<h2>{Name}</h2>`, a short `<p>` description, then the form/list. Use Svelte 5 runes (`$state`, `$effect`, `$derived`). Every fetch via `api(path)` from `src/lib/api.ts`. NEVER bare `fetch()`.

Inline `<style>` per component is fine for this recipe. Use a small consistent palette via CSS variables defined in `src/app.css` (create it; import in main.ts). Suggested tokens:
- `--bg: #f7f8fa; --card: #ffffff; --border: #e5e7eb; --text: #111827; --muted: #6b7280; --accent: #2563eb; --success: #059669; --warn: #d97706; --error: #dc2626;`

Sections rendered as cards with padding, border-radius 12px, soft shadow. Forms use styled inputs with focus ring. Buttons styled with hover. Tables with cell padding + alternating row shading.

---

# Symbol contract pre-attest (run before returning)

After all 5 features are implemented:

```bash
ssh apidev "cd /var/www && grep -rnE 'nats://[^ \\t]*:[^ \\t]*@' src 2>/dev/null; test \$? -eq 1"
ssh workerdev "cd /var/www && grep -rnE 'nats://[^ \\t]*:[^ \\t]*@' src 2>/dev/null; test \$? -eq 1"
ssh apidev "cd /var/www && grep -rn 'forcePathStyle' src/clients/s3.client.ts | grep -q true"
ssh workerdev "cd /var/www && grep -rE \"queue:\\s*'showcase-workers'\" src/worker/worker.service.ts | grep -q ."
ssh apidev "cd /var/www && grep -q 'nats.publish\\|js.publish\\|publish(' src/jobs/"
ssh apidev "cd /var/www && npm run build 2>&1 | tail -10"
ssh workerdev "cd /var/www && npm run build 2>&1 | tail -10"
ssh appdev "cd /var/www && npm run build 2>&1 | tail -10"
```

All must pass.

---

# Smoke tests after build (each codebase)

Restart dev processes via `mcp__zerops__zerops_dev_server` (NOT raw ssh + &) — apidev port 3000 path /api/health, workerdev no-probe, appdev port 5173 path /. Then curl each new endpoint:

```bash
ssh apidev "curl -sS -o /dev/null -w '%{http_code} %{content_type}\\n' http://localhost:3000/api/items"
ssh apidev "curl -sS -o /dev/null -w '%{http_code} %{content_type}\\n' -X POST -H 'Content-Type: application/json' -d '{\"key\":\"k1\",\"value\":\"v1\"}' http://localhost:3000/api/cache"
ssh apidev "curl -sS -o /dev/null -w '%{http_code} %{content_type}\\n' http://localhost:3000/api/cache/k1"
ssh apidev "curl -sS -o /dev/null -w '%{http_code} %{content_type}\\n' http://localhost:3000/api/files"
ssh apidev "curl -sS -o /dev/null -w '%{http_code} %{content_type}\\n' 'http://localhost:3000/api/search?q=welcome'"
ssh apidev "curl -sS -o /dev/null -w '%{http_code} %{content_type}\\n' http://localhost:3000/api/jobs"
```

All must return `200 application/json`. The worker round-trip:

```bash
ssh apidev "curl -sS -X POST -H 'Content-Type: application/json' -d '{\"type\":\"echo\",\"payload\":{\"x\":1}}' http://localhost:3000/api/jobs" 
# Wait 2s
sleep 2
ssh apidev "curl -sS http://localhost:3000/api/jobs | head -c 500"
```

The first job in the list must show `status: \"done\"` and `processedAt` non-null.

---

# Things to avoid (re-statements)

- NO bare `fetch()` in Svelte components — only `api()` helper.
- NO `synchronize: true` in TypeORM.
- NO `nats://user:pass@host:port` URLs.
- NO `redis://...:pass@host` URLs.
- NO `${KEY}: ${KEY}` self-shadow lines in zerops.yaml.
- NO new env var names invented — re-use the contract.
- NO `__filename`/`__dirname` (project is ESM in Svelte; CommonJS in Nest, so use Node's standard).

---

# Completion shape

Return:
1. Files written per codebase (bullets)
2. Per-feature smoke verdict — one line per feature with the curl status+content-type for the api route AND the mustObserve count for the UI section
3. Recorded facts — one line per fact (title + scope + routeTo)
4. Build status per codebase — exit code + dev-server running status
5. Any blocker — describe symptom + 3 probe batches you ran + status of each
6. The exact NATS round-trip evidence (POST /api/jobs response + GET /api/jobs first row showing processedAt non-null)

You have a working dashboard skeleton already deployed. Build the 5 features as ONE coherent author. Single session per feature: API + DTOs + UI + (worker if applicable) before moving to the next feature. Do NOT split features across passes.
```
