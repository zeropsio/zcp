# Dispatch 4 — build-5-showcase-features

**Dispatched at**: `2026-04-21T07:59:06`
**Description**: Build 5 showcase features
**Subagent type**: `general-purpose`
**Prompt length**: 13521 chars
**Tool-use id**: `toolu_01GZiywfjBy5ioGRtyxVxJD8`

---

## Transmitted prompt (verbatim)

```
You are the feature sub-agent for the **nestjs-showcase** Zerops recipe. You implement FIVE declared features end-to-end across three codebases as a single coherent author.

## Codebases and mounts

- `/var/www/apidev/` — NestJS 11 API (SSH alias: apidev). Dev server runs on port 3000 via `npm run start:dev`. Already has: AppModule with TypeOrmModule.forRoot+forFeature([Item,Job]), CacheModule (@keyv/redis), ClientsModule NATS (name: NATS_CLIENT, queue nestjs-showcase-workers), S3Service, SearchService. HealthController + StatusController already exist. Seeded items table (5 rows) + empty jobs table.
- `/var/www/appdev/` — Svelte 5 + Vite 8 SPA (SSH alias: appdev). Dev server runs on port 5173 via `npm run dev`. Already has: src/lib/api.ts helper (reads VITE_API_URL, throws on non-OK or non-JSON), src/lib/StatusPanel.svelte (renders data-feature="status"), src/App.svelte mounts `<StatusPanel />`.
- `/var/www/workerdev/` — NestJS 11 standalone microservice (SSH alias: workerdev). No HTTP. Already has: main.ts bootstraps Transport.NATS with queue "nestjs-showcase-workers", JobsController with `@MessagePattern('jobs.dispatch')` handler that does 500ms simulated work then writes status='done' + processedAt.

Package versions (verify against node_modules before importing):
- apidev: @nestjs/common 11.1.19, @nestjs/typeorm 11.0.0, typeorm 0.3.x, @aws-sdk/client-s3 latest, meilisearch 0.57.0 (class name `Meilisearch` — capital M lower s — ESM-only, use dynamic import)
- appdev: svelte 5.55.4, vite 8.0.9
- workerdev: @nestjs/microservices 11.1.19, nats 2.29.x

## Features to implement (authoritative, exact contract)

### 1. items-crud — surface [api, ui, db]
- **API routes** at `/api/items` (all JSON, all go through /api prefix already set globally):
  - `GET /api/items` → returns `{ items: Item[] }` where Item is `{ id: number, title: string, body: string, createdAt: string }`.
  - `POST /api/items` body `{ title: string, body: string }` → returns the created Item. Validate with class-validator.
  - `PUT /api/items/:id` body `{ title?, body? }` → returns updated Item.
  - `DELETE /api/items/:id` → returns `{ deleted: true, id }`.
  - Also sync every mutation to Meilisearch index "items" (addDocuments on create/update; deleteDocument on delete). Use the SearchService getter (async dynamic import).
- **UI section** with outer wrapper `data-feature="items-crud"`. Fetch `/api/items` on mount, render a list where each row has `data-row` attribute. Form with title + body input + Submit button. After submit, append the new row. Show loading / error / empty / populated states; error in a `[data-error]` slot.
- **MustObserve**: `[data-feature="items-crud"] [data-row]` count increases by 1 within 2s of submit.

### 2. cache-demo — surface [api, ui, cache]
- **API routes**:
  - `POST /api/cache` body `{ key: string, value: string }` → stores via cacheManager.set(key, value, 60_000). Returns `{ ok: true, key }`.
  - `GET /api/cache/:key` → returns `{ key, value: string|null, hit: boolean }`. Uses cacheManager.get(key).
- **UI section** with `data-feature="cache-demo"`. Two inputs (key + value) + "Write" button + "Read" button. After Read, display the returned value in a `[data-result]` element.
- **MustObserve**: `[data-feature="cache-demo"] [data-result]` text equals the written value.

### 3. storage-upload — surface [api, ui, storage]
- **API routes**:
  - `POST /api/files` multipart/form-data with file field "file" → uploads to S3 bucket under key `uploads/{uuid}-{originalname}`, returns `{ key, size, originalName }`. Use @nestjs/platform-express FileInterceptor + Multer memoryStorage. Execute PutObjectCommand with structured body (Buffer), ContentType set from multer mimetype.
  - `GET /api/files` → ListObjectsV2Command on bucket with Prefix="uploads/". Returns `{ files: [{ key: string, size: number, lastModified: string }] }`.
- **UI section** with `data-feature="storage-upload"`. `<input type="file">` + "Upload" button. After upload, refresh the file list and show each file in a `[data-file]` element.
- **MustObserve**: `[data-feature="storage-upload"] [data-file]` count increases by 1 within 3s.

### 4. search-items — surface [api, ui, search]
- **API route**: `GET /api/search?q=...` → uses the Meilisearch `items` index to search the title + body fields, returns `{ hits: [{ id, title, body }] }`. On empty q, return `{ hits: [] }`.
- **UI section** with `data-feature="search-items"`. Input field (debounced 400ms on keystroke). For each hit, render a `[data-hit]` element showing title + body snippet.
- **MustObserve**: `[data-feature="search-items"] [data-hit]` count > 0 for a seeded keyword (e.g. "Zerops", "NestJS", "cache", "NATS", "storage").

**Important**: you must create and populate the Meilisearch items index. The seed.ts already seeded 5 DB rows. Add a step to the seed script (or a new init step) that also syncs those 5 rows to Meilisearch and **awaits** task completion via `waitForTask`. Update `src/seed.ts` so it also does `searchClient.index('items').addDocuments(rows, {primaryKey:'id'})` then awaits the task's completion. Use the dynamic import pattern for meilisearch.

### 5. jobs-dispatch — surface [api, ui, queue, worker]
- **API routes**:
  - `POST /api/jobs` body `{ payload?: object }` → creates a Job row with status='queued', payload, nulls elsewhere. Then `natsClient.emit('jobs.dispatch', { id: job.id })`. Returns the Job (status='queued', processedAt=null).
  - `GET /api/jobs/:id` → returns the Job.
  - `GET /api/jobs` → returns `{ jobs: Job[] }` (latest 20 ordered by createdAt DESC).
- **UI section** with `data-feature="jobs-dispatch"`. Button "Dispatch Job". On click, POST /api/jobs, then poll GET /api/jobs/:id every 500ms until `processedAt` is non-null (max 10s). Render each dispatched job in a row with `[data-job-id]` + `[data-processed-at]` showing the processedAt timestamp (or "pending…").
- **Worker**: already handles `jobs.dispatch` — verify it still works. The worker is in `/var/www/workerdev/` and needs no changes unless the contract shifts.
- **MustObserve**: `[data-feature="jobs-dispatch"] [data-processed-at]` text is non-empty within 5s of clicking Dispatch.

## CORS

Main app.ts already has `app.enableCors({ origin: true, credentials: true })`. If you need adjustment, use FRONTEND_URL env var. Keep credentials off unless you use cookies (you don't).

## Dashboard layout (appdev)

Extend `src/App.svelte` to render all feature sections below StatusPanel. Keep StatusPanel. Each feature gets its own Svelte component file `src/lib/<Feature>.svelte`. Order:
1. StatusPanel (already there)
2. ItemsCrud
3. CacheDemo
4. SearchItems
5. StorageUpload
6. JobsDispatch

Apply consistent, polished styling: CSS custom properties for colors, spacing, typography; form inputs with padding + border-radius + focus ring; buttons with hover; card-style sections with `<h2>` + short description; monospace for IDs; humanized timestamps; system font stack.

## Cross-codebase DTO discipline

Each feature's response/request DTO is defined as a TypeScript `interface` at the top of its API controller, then copy-pasted byte-identically into the Svelte component file that consumes it and into the worker file if applicable. Do not import across codebases.

## Installed-package verification

Before importing anything non-trivial, `cat /var/www/apidev/node_modules/{pkg}/package.json` to confirm the version AND check its main entry point for the exported symbols you plan to use. This is mandatory for:
- @aws-sdk/client-s3 (PutObjectCommand, ListObjectsV2Command, HeadBucketCommand)
- meilisearch (class name `Meilisearch`, ESM-only, `waitForTask` method)
- @keyv/redis (createKeyv)
- class-validator + class-transformer (decorators)
- multer (types + memoryStorage)

## Workflow

1. Read the existing scaffold files in all three codebases so you know what exists.
2. For each feature, in order, implement api → ui → worker (if applicable) in ONE edit session. Then smoke-test via curl:
   - `ssh apidev "curl -sS -o /dev/null -w '%{http_code} %{content_type}\n' http://localhost:3000/api/items"` — must return 200 application/json.
   - Repeat for each feature's GET endpoint.
3. Install any new npm deps via `ssh apidev "cd /var/www && npm install {pkg}"` and similarly for appdev/workerdev. Verify installation by reading the lockfile.
4. After all features implemented:
   - `ssh apidev "cd /var/www && npm run build 2>&1 | tail -20"` — build must succeed.
   - `ssh appdev "cd /var/www && npm run build 2>&1 | tail -20"` — build must succeed.
   - `ssh workerdev "cd /var/www && npm run build 2>&1 | tail -20"` — build must succeed.
5. If you modified AppModule or main.ts, restart apidev dev server: `mcp__zerops__zerops_dev_server action=restart hostname=apidev command="npm run start:dev" port=3000 healthPath="/api/health" waitSeconds=25`.
6. If you modified workerdev source, restart it: `mcp__zerops__zerops_dev_server action=restart hostname=workerdev command="npm run start:dev" port=0 noHttpProbe=true waitSeconds=15`.
7. Vite has HMR — no restart needed for appdev unless vite.config.ts changes.
8. End-to-end smoke: for jobs-dispatch, curl POST /api/jobs, sleep 2, curl GET /api/jobs/:id — processedAt must be non-null. Demonstrates producer → broker → worker → DB round-trip.

## The twelve seeded fix-recurrence rules (re-verify before returning)

Before returning, run these on each relevant host. Non-zero exit on any applicable rule = fix first.

1. **nats-separate-creds** (apidev, workerdev): `ssh {host} "grep -rnE 'nats://[^ \t]*:[^ \t]*@' /var/www 2>/dev/null; test $? -eq 1"` — no URL-embedded NATS creds.
2. **s3-uses-api-url** (apidev): `ssh apidev "grep -rn 'storage_apiHost' /var/www/src 2>/dev/null; test $? -eq 1"`.
3. **s3-force-path-style** (apidev): `ssh apidev "grep -rn 'forcePathStyle' /var/www/src 2>/dev/null | grep -q true"`.
4. **routable-bind** (apidev, appdev): `ssh {host} "grep -rnE 'listen\\(.*(localhost|127\\.0\\.0\\.1)' /var/www/src 2>/dev/null; test $? -eq 1"`.
5. **trust-proxy** (apidev): `ssh apidev "grep -rnE 'trust[ _]proxy' /var/www/src 2>/dev/null | grep -q ."`.
6. **graceful-shutdown** (apidev, workerdev): `ssh {host} "grep -rnE 'SIGTERM|enableShutdownHooks' /var/www/src 2>/dev/null | grep -q ."`.
7. **queue-group** (workerdev): `ssh workerdev "grep -rnE 'queue:' /var/www/src 2>/dev/null | grep -q ."`.
8. **env-self-shadow** (any): `ssh {host} "grep -nE '^[[:space:]]+([A-Z_a-z]+):[[:space:]]+\\$\\{\\1\\}[[:space:]]*$' /var/www/zerops.yaml 2>/dev/null; test $? -eq 1"`.
9. **gitignore-baseline** (any).
10. **env-example-preserved** (any).
11. **no-scaffold-test-artifacts** (any).
12. **skip-git** (any): `.git/` may be present (managed by main agent) — rule 12 tolerates either presence or absence.

## Facts to record

Call `zerops_record_fact` for:
- Any non-trivial fix you apply (e.g. Meilisearch ESM dynamic import workaround, multer memoryStorage tuning, CORS)
- Cross-codebase contract bindings (the 5 DTO shapes + the NATS subject/queue pair)
- Any platform behavior surprising in context

## Completion report format

Return:
1. Files written/modified per codebase with byte counts
2. Per-feature smoke-test verdict (curl status + content-type for API; selector count for UI)
3. Per-feature recorded facts (title + scope)
4. Build + dev-server status per codebase (exit code, running, healthcheck)
5. Any new env vars required (list with reason)
6. Blockers (if any)

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

### Scaffold pre-flight — platform principles

The Zerops platform imposes invariants every service must honor.

**Principle 1 — Graceful shutdown** — apidev + workerdev must drain SIGTERM within 30s. apidev uses `app.enableShutdownHooks()`; workerdev uses the microservice shutdown hook.

**Principle 2 — Routable network binding** — apidev binds `0.0.0.0:3000`; appdev's Vite dev server binds `0.0.0.0:5173`.

**Principle 3 — Client-origin awareness** — apidev sets `trust proxy` on Express.

**Principle 4 — Competing-consumer semantics** — workerdev passes `queue: 'nestjs-showcase-workers'` to the NATS microservice. Do not strip this.

**Principle 5 — Structured credential passing** — NATS creds as `{ user, pass }` structured options; S3 creds as `credentials: { accessKeyId, secretAccessKey }`. Never URL-embedded.

**Principle 6 — Stripped build-output root** — zerops.yaml for appstage already uses `./dist/~` tilde suffix; feature changes must not shift output path.

<<<END MANDATORY>>>

```
