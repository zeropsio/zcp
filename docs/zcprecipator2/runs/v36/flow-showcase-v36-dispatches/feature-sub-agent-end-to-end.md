# Dispatch 4 — feature-sub-agent-end-to-end

**Dispatched at**: `2026-04-21T14:45:56`
**Description**: Feature sub-agent: end-to-end
**Subagent type**: `general-purpose`
**Prompt length**: 12572 chars
**Tool-use id**: `toolu_01VSdrgfZXwbqC6xrrNVvKKs`

---

## Transmitted prompt (verbatim)

```
You are the feature sub-agent in a Zerops recipe workflow. Working state: three mounted codebases with a passing health dashboard skeleton already deployed. Your task is to implement five features end-to-end as a single coherent author — API routes + frontend components + worker payloads authored in one session per feature so the contracts stay consistent.

Mounts (all live, SSHFS into their dev containers):
- `/var/www/apidev/` — NestJS 11 + TypeORM + Postgres + Valkey (ioredis) + NATS + S3 (`@aws-sdk/client-s3`, `forcePathStyle: true`) + Meilisearch. Global prefix `/api`. Running on :3000. Services wired in `src/services/{cache,queue,storage,search}.service.ts`, entity in `src/entities/item.entity.ts`, data source in `src/data-source.ts`, migrations in `src/migrations/`, seed in `src/seed.ts`.
- `/var/www/workerdev/` — standalone NestJS with NATS subscription in `src/worker.service.ts`. Currently logs only — you extend this to process jobs end-to-end.
- `/var/www/appdev/` — Svelte 5 + Vite 8 SPA. Single fetch helper at `src/lib/api.ts` (NEVER fetch directly from components). Current dashboard: `src/App.svelte` renders `<StatusPanel />` only. You add feature sections.

SSH hostnames: `apidev`, `workerdev`, `appdev`.

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

**Dev-server lifecycle** — use `mcp__zerops__zerops_dev_server` for every start/stop/status/logs/restart. Raw `ssh host "cmd &"` holds the SSH channel open and times out after 120s.

<<<END MANDATORY>>>

## Prior discoveries (do not re-investigate)

- **Zerops-native env var names** — code reads `process.env.db_hostname`, `queue_user`, `storage_apiUrl`, `storage_bucketName`, `storage_accessKeyId`, `storage_secretAccessKey`, `cache_hostname`, `cache_port` (no auth on managed Valkey), `search_hostname`, `search_port`, `search_masterKey`, etc. directly. Cross-service refs auto-inject as OS env vars project-wide — no `run.envVariables` declaration required.
- **NATS contract** — subject `jobs.process`, queue group `workers`. apidev's `QueueService.publish('jobs.process', payload)` → workerdev subscribes with `{ queue: 'workers' }`.
- **S3 endpoint** — use `process.env.storage_apiUrl` (HTTPS) not `storage_apiHost`. `forcePathStyle: true`.
- **execOnce key discipline** — each `initCommand` needs a DISTINCT key (e.g. `${appVersionId}-migrate` vs `${appVersionId}-seed`). The bare `${appVersionId}` reused across commands makes later commands silently no-op.
- **Seed idempotency** — use fixed UUIDs in `src/seed.ts`; pattern is `findOne({where:{id}}) + skip-on-exists`. Keep seed LOUD: exit 1 on error.

## Feature list (authoritative)

Implement exactly these five features. Every feature gets an API route under `/api/...`, a dashboard section wrapped in `<section data-feature="{id}">` with four render states (loading / error / empty / populated), and any worker/managed-service wiring.

### 1. items-crud  — surface [api, ui, db]
- `GET /api/items` → `{ items: Item[] }` sorted newest-first
- `POST /api/items` with JSON `{ title: string, description?: string }` → 201 Created + the new row
- `DELETE /api/items/:id` → 204 No Content
- Dashboard section `[data-feature="items-crud"]` with a form (title input + optional description textarea + Submit button) and a table of rows. Each row has `data-row` + `data-row-id="{id}"` attributes + a Delete button.
- `MustObserve`: `[data-feature="items-crud"] [data-row]` count increases by 1 after submit.
- IMPORTANT: after creating or deleting an item, the search index must be kept in sync (see feature 4). Put this in a reusable helper in `apidev/src/services/search.service.ts` or in an `ItemsService`.

### 2. cache-demo — surface [api, ui, cache]
- `GET /api/cache?key=demo` → `{ key, value: string|null, ttl: number|null }` — reads the key from Valkey
- `POST /api/cache` with JSON `{ key: string, value: string, ttlSeconds?: number }` → 200 + `{ key, value, ttlSeconds }`. Default `ttlSeconds=60`.
- Dashboard section with key + value inputs + Write button + Read button. Show `[data-result]` with the last read value.
- `MustObserve`: `[data-feature="cache-demo"] [data-result]` text equals the value that was just written.

### 3. storage-upload — surface [api, ui, storage]
- `POST /api/files` multipart/form-data with field `file` → 200 `{ key, size, contentType }`. Use `StorageService.putObject` + a generated key like `uploads/{uuid}-{filename}`.
- `GET /api/files` → `{ files: Array<{ key, size, lastModified }> }` via `ListObjectsV2Command` filtered to the `uploads/` prefix.
- Dashboard section with file picker + Upload button + `[data-file]` list rows. Files sorted newest-first.
- `MustObserve`: `[data-feature="storage-upload"] [data-file]` count increases by 1 after upload.

### 4. search-items — surface [api, ui, search]
- `GET /api/search?q=<term>` → `{ query, hits: Item[] }`. Uses MeiliSearch index `items` with searchable attributes `title`, `description`.
- Index setup: in `apidev/src/seed.ts` (or a separate init script), after seeding Postgres items, sync them to Meilisearch and **await `waitForTask`** before the script exits. Configure `searchableAttributes: ['title', 'description']`, `sortableAttributes: ['createdAt']`. Key each doc by the Postgres `id`.
- Items CRUD must keep Meili in sync: on create add the doc, on delete remove it. Use `client.index('items').addDocuments([...])` / `deleteDocument(id)`.
- Dashboard section with a search input (debounced 400ms) and a `[data-hit]` list. Show empty state "Start typing" when input is empty.
- `MustObserve`: `[data-feature="search-items"] [data-hit]` count > 0 for a known-matching query (try `Zerops`).

### 5. jobs-dispatch — surface [api, ui, queue, worker]
- New entity `Job` in `apidev/src/entities/job.entity.ts`: `{ id uuid PK default, kind varchar, payload jsonb, status varchar ('pending'|'processed'|'failed'), processedAt timestamptz nullable, errorMessage text nullable, createdAt timestamptz default now }`. Add migration `1700000000001-JobsTable.ts`.
- `POST /api/jobs` with JSON `{ kind: string, payload?: any }` → 202 Accepted + the `{ id, kind, status: 'pending' }`. Insert the DB row, then publish to NATS subject `jobs.process` with `{ jobId, kind, payload }`.
- `GET /api/jobs` → `{ jobs: Job[] }` newest-first, limit 20.
- `GET /api/jobs/:id` → `{ job: Job }` — frontend polls this after dispatch.
- Worker (`workerdev/src/worker.service.ts`): subscribe to `jobs.process` with queue `workers` (already done); on message, UPDATE the matching job row to status='processed', set processedAt=now, optionally echo kind into an extra column. On handler error set status='failed' + errorMessage. Must NOT swallow errors silently.
  - The worker needs its own `data-source.ts` (same pattern as apidev, reading `db_*` env vars) and the `Job` entity. COPY the entity file byte-identically; don't share code across mounts.
  - Update `workerdev/package.json` to add `@nestjs/typeorm typeorm pg reflect-metadata` dependencies, then `ssh workerdev "cd /var/www && npm install"`.
- Dashboard section: "Kind" input + Dispatch button. On submit, POST, then poll `/api/jobs/:id` every 500ms (max 20s) until `processedAt` is set. Show a list `[data-job]` with each job's id (monospace), kind, status badge, and processedAt (if present, formatted via `new Date(iso).toLocaleString()`).
- `MustObserve`: `[data-feature="jobs-dispatch"] [data-processed-at]` non-empty within 5s of dispatch (wait up to 8s to be safe).

## Mandatory shape rules

- **All frontend fetches go through `appdev/src/lib/api.ts`** — NEVER bare `fetch('/api/...')` in components. The helper enforces `res.ok` + JSON content-type.
- **DTOs** — declare TypeScript interfaces (Item, Job, DispatchJobDTO, etc.) in the owning apidev controller; copy-paste byte-identically into appdev components and workerdev handlers.
- **NATS** — subject `jobs.process`, queue `workers`. `QueueService.publish` already flushes.
- **Env vars** — zerops-native lowercase names (`db_hostname`, `queue_user`, `storage_apiUrl`, `search_masterKey`, etc.). Never invent variants.
- **S3** — every S3Client construction uses `forcePathStyle: true` + `region: 'us-east-1'`. Endpoint is `process.env.storage_apiUrl`.
- **Loud failure** — migrations and seed exit 1 on error. Handlers that reach Meilisearch's `waitForTask` actually `await` it. Worker handler catches per-message errors, writes `status='failed'` + errorMessage, does NOT re-throw (otherwise the queue loop dies).
- **Graceful shutdown** — apidev has `app.enableShutdownHooks()`; workerdev has it too. If you add new providers with external connections (e.g. Meilisearch index init), implement `onModuleDestroy` that closes gracefully.
- **No README.md, no zerops.yaml edits** — the main agent owns those. If you need a new env var or new service, STOP and report in the return message.

## Cross-codebase sequencing

Use the existing `ItemsModule`/`JobsModule` pattern when adding new controllers. Wire them in `apidev/src/app.module.ts`. Existing `app.controller.ts` keeps `/api/health` and `/api/status`; new features go in dedicated modules (`items/`, `cache/`, `files/`, `search/`, `jobs/`). Or keep everything in `app.controller.ts` — your call for brevity, but one module per feature is cleaner.

## Dashboard polish

Modernize `appdev/src/App.svelte` into a grid of feature cards (keep StatusPanel as the top card). Each feature section needs:
- Heading + 1-sentence description
- Styled form controls (padding, border-radius, focus ring — use the existing CSS tokens in `src/app.css`; extend them as needed)
- Loading / error / empty / populated render states
- `data-feature="{id}"` on outer wrapper

Use Svelte 5 runes (`$state`, `$derived`, `$effect`). Do NOT use legacy `export let` / `$:`.

## Per-feature smoke-test loop

After implementing each feature, before moving to the next:
```
ssh apidev "curl -sS -o /dev/null -w '%{http_code} %{content_type}\n' http://localhost:3000/api/<route>"
```
Must return `200 application/json` (or the expected 201/202 for POST). Fix immediately if wrong.

Note: `apidev` nest dev server is already running on :3000 (watch mode — it recompiles on file changes). `workerdev` nest dev server is also running. `appdev` vite dev server is running on :5173. If any crashes during your edits, restart via `mcp__zerops__zerops_dev_server`.

## Package installs

- apidev: if you add new packages, `ssh apidev "cd /var/www && npm install <pkg>"`.
- workerdev: you will definitely need `@nestjs/typeorm typeorm pg reflect-metadata` for DB writes from the worker.
- appdev: unlikely to need new packages. If you do: `ssh appdev "cd /var/www && npm install <pkg>"`.

## After implementation

1. `ssh apidev "cd /var/www && npx tsc --noEmit"` exit 0
2. `ssh workerdev "cd /var/www && npx tsc --noEmit"` exit 0
3. `ssh appdev "cd /var/www && npm run build 2>&1 | tail -20"` exit 0 (Vite prod build)
4. Start / restart all three dev servers via `zerops_dev_server`
5. Curl every feature's healthCheck path → 200 + application/json
6. Dispatch a job via POST /api/jobs, poll GET /api/jobs/:id, confirm processedAt is populated within ~5s

## Return format (under 400 words)

1. Files written per codebase (bulleted)
2. Per-feature smoke-test verdict: status + content-type
3. Build exit codes for all three codebases
4. Notable facts you recorded via `zerops_record_fact` (list titles)
5. Any blockers — use the diagnostic-cadence rule: max 3 probe batches, then stop and report

Do not claim implementation of a feature you could not verify. An honest "items-crud works, search-items blocked because Meilisearch index creation fails" is worth more than a dishonest green.
```
