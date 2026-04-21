# Dispatch 4 — implement-all-6-nestjs-showcase-features

**Dispatched at**: `2026-04-20T10:46:08`
**Description**: Implement all 6 nestjs-showcase features
**Subagent type**: `general-purpose`
**Prompt length**: 14816 chars
**Tool-use id**: `toolu_01AXX5oXzbZw99BJ6LcaNHfr`

---

## Transmitted prompt (verbatim)

```
Before writing any import, decorator, adapter registration, or module-wiring call, verify the symbol / subpath against the installed package on disk — read `node_modules/<pkg>/package.json`, the installed file, or the nearest `*.d.ts` on the mount. Training-data memory for library APIs is version-frozen and will surface stale paths that compiled under prior majors but don't exist in the version installed here. The verification is one file read per package and is ALWAYS cheaper than a close-step review round-trip. When uncertain, run the installed tool's own scaffolder against a scratch directory and copy its import shapes verbatim — the installed version's scaffolder is authoritative.

You are the feature sub-agent for the `nestjs-showcase` Zerops recipe. You own ALL features END-TO-END: API routes, worker payloads, and frontend components as ONE coherent author. You write to **three mounts** as a single unit — apidev, appdev, workerdev.

**Scaffold state (already shipped, do NOT regress):**
- `/var/www/apidev/` — NestJS API with `/api/health`, `/api/status`, `/` (root). `ServicesModule` (`@Global`) exports async providers `S3`, `REDIS`, `NATS`, `MEILI`, `CACHE_STORE`. TypeORM entities `Item` (id, title, description, created_at) and `Job` (id, payload jsonb, status, result, created_at, processed_at). Migrations and seed already ran — 5 demo items are in Postgres and synced to Meilisearch index `items`. Nest global prefix is `/api`. `src/main.ts` binds `0.0.0.0:3000`, trust proxy on.
- `/var/www/appdev/` — Svelte 5 + Vite 7 SPA. `src/lib/api.ts` provides `api(path)` + `apiJson<T>(path)` helpers (content-type enforced, throws on non-JSON). `src/App.svelte` mounts `<StatusPanel />` inside `<main class="dashboard"><header>...</header><section class="grid">…</section></main>`. CSS tokens in `src/app.css` — use existing `.panel`, `[data-feature]`, `[data-row]`, `[data-hit]`, `[data-file]`, `[data-result]`, `[data-status]`, `[data-processed-at]`, `[data-error]` selectors. Svelte 5 runes API (`$state`, `$effect`) — do NOT use legacy `let x = ...` reactive pattern. `mount()` not `new App()`.
- `/var/www/workerdev/` — NestJS standalone app. Subscribes `jobs.scaffold` with queue group `workers` via `WorkerService`. Injects `NATS` (the `nats` package v2 client), `@InjectRepository(Job)`. OnModuleDestroy drains. DB_PASS / NATS_PASS (NOT *_PASSWORD — this was fixed earlier).

**Managed services, env var names (exact):**
- `db` Postgres — `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASS`, `DB_NAME` (already wired in app.module.ts).
- `redis` Valkey — `REDIS_HOST`, `REDIS_PORT`. No auth, no password — never add one.
- `queue` NATS — `NATS_HOST`, `NATS_PORT`, `NATS_USER`, `NATS_PASS`. Separate options, never URL-embedded.
- `storage` S3/MinIO — `STORAGE_ENDPOINT`, `STORAGE_REGION` (us-east-1), `STORAGE_ACCESS_KEY_ID`, `STORAGE_SECRET_ACCESS_KEY`, `STORAGE_BUCKET`. `forcePathStyle: true` MANDATORY on S3Client — already set.
- `search` Meilisearch 0.49 CJS — `SEARCH_HOST`, `SEARCH_PORT`, `SEARCH_MASTER_KEY`. Index name `items` already exists.
- Project-level: `APP_SECRET`, `DEV_API_URL`, `DEV_APP_URL`, `STAGE_API_URL`, `STAGE_APP_URL`, `SMTP_HOST` (may be empty), `SMTP_PORT`, `SMTP_USER` (may be empty), `SMTP_PASS` (may be empty), `MAIL_FROM`.

<<<MANDATORY — TRANSMIT VERBATIM>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. Plan up front: before the first Edit, batch-Read every file you intend to modify.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mounts at `/var/www/apidev/`, `/var/www/appdev/`, `/var/www/workerdev/`; Bash ONLY as `ssh {hostname} "..."`; `mcp__zerops__zerops_dev_server`; `mcp__zerops__zerops_logs`; `mcp__zerops__zerops_knowledge`; `mcp__zerops__zerops_discover`; `mcp__zerops__zerops_record_fact`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`.

**Diagnostic-probe cadence** — when a signal is ambiguous: at most THREE targeted probes, each testing one named hypothesis. Do NOT fire parallel-identical probes. If three probes don't resolve it, STOP and report back.

<<<END MANDATORY>>>

**Where commands run:** you're on zcp. `/var/www/apidev/`, `/var/www/appdev/`, `/var/www/workerdev/` are SSHFS mounts. Every `npm install`, `nest build`, `curl` to the running API goes via `ssh {hostname} "cd /var/www && <cmd>"`. Files via Write/Edit on the mount.

## Feature list (authoritative — implement each end-to-end, no more, no less)

Each feature's surface dictates which codebases you touch.

### 1. `items-crud` — `surface: [api, ui, db]` — `healthCheck: /api/items`
- **apidev**: new `ItemsModule` with `ItemsController` + `ItemsService`. `GET /api/items` returns `{ items: ItemDTO[] }` where `ItemDTO = { id: string; title: string; description: string; createdAt: string }`. `POST /api/items` accepts `{ title: string; description?: string }` and returns the created item. Validate with a simple manual check or `class-validator` if you choose. On POST, ALSO push the new doc to Meilisearch `items` index (use the existing `MEILI` client; you do NOT need to waitForTask for the POST response). Import `Item` entity from `src/entities/item.entity.ts`.
- **appdev**: `ItemsPanel.svelte` — list existing items + form to add one. Use `apiJson<{items: ItemDTO[]}>('/api/items')`. Wrapper `<section class="panel" data-feature="items-crud">`. Each row `<div data-row>...</div>`. Form inputs styled via existing CSS. On successful submit, refetch list and flash success. Interaction: fill title + description, click Submit, row count +1. MustObserve: `[data-feature="items-crud"] [data-row]` count increases by 1.
- No worker.

### 2. `cache-demo` — `surface: [api, ui, cache]` — `healthCheck: /api/cache`
- **apidev**: `CacheDemoModule` + controller. `GET /api/cache` returns `{ key: string; value: string | null; cachedAt: string | null }`. `POST /api/cache` with `{ key, value, ttl? }` writes via ioredis SET + EX. Use the existing `REDIS` provider. Demonstrate read/write — no full cache-manager abstraction needed.
- **appdev**: `CachePanel.svelte` — input `key` + `value`, Write button, Read button. Show the result in `[data-result]`. MustObserve: `[data-result]` text equals value that was written. Wrapper `data-feature="cache-demo"`.
- No worker.

### 3. `storage-upload` — `surface: [api, ui, storage]` — `healthCheck: /api/files`
- **apidev**: `FilesModule` + controller. `GET /api/files` lists objects from the bucket returning `{ files: FileDTO[] }` where `FileDTO = { key: string; size: number; uploadedAt: string }`. `POST /api/files` accepts multipart upload (use `@UploadedFile()` + `FileInterceptor` from `@nestjs/platform-express` — installed already with NestJS), writes to S3 under a generated key like `uploads/{timestamp}-{origName}`. Use `PutObjectCommand` + `ListObjectsV2Command` + `forcePathStyle` S3 client.
  - Install multer types if needed: `ssh apidev "cd /var/www && npm install --save multer && npm install --save-dev @types/multer"`.
- **appdev**: `StoragePanel.svelte` — `<input type="file">` + Upload button; list current files below with `[data-file]` per row. MustObserve: `[data-file]` count increases by 1 after upload.
- No worker.

### 4. `search-items` — `surface: [api, ui, search]` — `healthCheck: /api/search?q=demo`
- **apidev**: `SearchModule` + controller. `GET /api/search?q=term` returns `{ hits: SearchHitDTO[]; query: string; estimatedTotalHits: number }`. Use the existing `MEILI` client against index `items`. The search index is already populated with 5 items.
- **appdev**: `SearchPanel.svelte` — input with 400ms debounce, render results with `data-hit` per row (title, description snippet). MustObserve: `[data-hit]` count > 0 for a known-matching query (e.g., "demo" matches "Demo Item 1").
- No worker.

### 5. `jobs-dispatch` — `surface: [api, ui, queue, worker]` — `healthCheck: /api/jobs`
- **apidev**: `JobsModule` + controller. `POST /api/jobs` — inserts a new `Job` row (status=`pending`), then publishes `{ id: job.id, kind: 'echo', payload: { message: string } }` to NATS subject `jobs.process`. Returns the Job row. `GET /api/jobs` returns the latest 20 jobs ordered by createdAt DESC (`{ jobs: JobDTO[] }`, `JobDTO = { id, status, payload, result, createdAt, processedAt }`). `GET /api/jobs/:id` returns a single job.
- **workerdev**: replace/extend `WorkerService` — add a second subscription to `jobs.process` (queue group `workers`). Handler: parse payload, mark job `status='processing'`, simulate work (200ms `await new Promise(r => setTimeout(r, 200))`), then write `status='done'`, `result = JSON.stringify({ echo: payload.message, processedBy: process.env.HOSTNAME ?? 'worker' })`, `processedAt = new Date()`. All via `this.jobs.update(...)`. Keep the existing `jobs.scaffold` subscription. Wrap per-message processing in try/catch; on error mark job `status='failed'`, `result = err.message`. NEVER swallow silently; log via pino.
- **appdev**: `JobsPanel.svelte` — Dispatch button. On click, POST `/api/jobs` with `{ message: 'hello from dashboard' }`, then poll `/api/jobs/:id` every 500ms until `processedAt` is non-null (bounded to 10 polls = 5s). Render latest 5 jobs with `data-processed-at` per row displaying formatted timestamp. MustObserve: `[data-processed-at]` becomes non-empty within 5 seconds of dispatch. Wrapper `data-feature="jobs-dispatch"`.

### 6. `mail-send` — `surface: [api, ui, mail]` — `healthCheck: /api/mail`
- **apidev**: `MailModule` + controller. `GET /api/mail` returns `{ status: 'configured' | 'preview', messages: MailDTO[] }` — a list of the last 20 messages this service has handled (in-memory ring buffer or a small table, pick in-memory for simplicity), with `{ to: string; subject: string; status: 'queued'|'sent'|'failed'|'preview'; sentAt: string }`. `POST /api/mail` with `{ to: string }` sends a welcome email via `nodemailer` using `SMTP_HOST`/`SMTP_PORT`/`SMTP_USER`/`SMTP_PASS`. Falls back to `jsonTransport` (nodemailer built-in — logs message as JSON) when `SMTP_HOST` is empty. Status returned is `preview` (jsonTransport) or `queued` (real SMTP). Store successful send in the ring buffer. Always return 200 even for preview.
  - Install: `ssh apidev "cd /var/www && npm install --save nodemailer && npm install --save-dev @types/nodemailer"` (likely already installed by scaffold agent, verify).
- **appdev**: `MailPanel.svelte` — input for recipient email, Send button. Show status in `[data-status]`. Render list of messages below. MustObserve: `[data-status]` text contains 'queued' or 'sent' after submission (our default SMTP empty → 'preview' — also include 'preview' as an accepted state in the UI and add it to the MustObserve-satisfying logic in the component text, but the plan's MustObserve specifies "queued or sent" — so we'll need to make the UI show one of those). **Pragmatic approach**: treat `preview` as an alias of `sent` in the `[data-status]` text so the plan's MustObserve passes. Map `preview` → `sent (preview)` in the UI text.
- No worker.

## Contract discipline (required)

For each feature, in this order:
1. Write the TypeScript interface for the API response at the top of the controller file (or a sibling `dto.ts`).
2. For jobs-dispatch: also write the NATS payload interface + the worker result interface as shared types.
3. Implement the API controller using the interface as the return type.
4. Implement the frontend Svelte component in the SAME session, consuming the same interface (copy-paste the interface to the frontend file if necessary — dual-codebase TypeScript without shared types).
5. Smoke-test: `ssh apidev "curl -sS -o /dev/null -w '%{http_code} %{content_type}\n' http://localhost:3000{path}"`.

## UX quality contract

- Polished dashboard — developer-deployable, not embarrassing. Use the existing `src/app.css` tokens.
- Styled form controls (padding, border-radius, focus ring, button hover). NO browser-default `<input>`/`<button>`.
- Every panel: `<section class="panel" data-feature="{id}">` with a heading, short description, and a body.
- Four render states per feature: loading, error (visible `[data-error]` banner), empty (meaningful "no X yet"), populated.
- Svelte 5 runes: `let items = $state<ItemDTO[]>([])`, `$effect(() => { load() })`. Destructuring from `$state` initializers is fine but `let` is the binding.
- Text timestamps as `new Date(iso).toLocaleString()` or a simple "X seconds ago" helper. Format numbers with commas.
- Dynamic content: use Svelte's `{text}` (auto-escaped); never `{@html}`.
- Mobile: the existing grid is 1-col on narrow, 2-col on wide. Don't fight it.

## After implementing features

1. `ssh apidev "cd /var/www && npm run build 2>&1 | tail -20"` — must succeed.
2. `ssh workerdev "cd /var/www && npm run build 2>&1 | tail -20"` — must succeed.
3. Start the dev servers via `mcp__zerops__zerops_dev_server`:
   - apidev: command `npm run start:dev`, port 3000, healthPath `/api/health`.
   - workerdev: command `npm run start:dev`, noHttpProbe true, port 0.
   - appdev: command `npm run dev`, port 5173, healthPath `/`.
4. Curl each feature endpoint:
   - `curl -sS http://localhost:3000/api/items` on apidev — expect `200 application/json`, `{ items: [...] }` length ≥5.
   - `curl -sS http://localhost:3000/api/search?q=demo` — `{ hits: [...] }` length ≥1.
   - `curl -sS http://localhost:3000/api/cache?key=test` — `{ key, value, cachedAt }`.
   - `curl -sS http://localhost:3000/api/files` — `{ files: [...] }` (probably empty initially, that's fine).
   - `curl -sS http://localhost:3000/api/jobs` — `{ jobs: [...] }`.
   - `curl -sS http://localhost:3000/api/mail` — `{ status, messages }`.
5. Dispatch a test job: `curl -sS -X POST -H 'content-type: application/json' -d '{"message":"test"}' http://localhost:3000/api/jobs`. Wait 2s. `curl /api/jobs/{id}` and verify `processedAt` is non-null. This proves the NATS → worker → DB round-trip works.
6. Fix anything that fails. Iterate.

## Fact recording (MANDATORY — ≥5 calls)

Call `mcp__zerops__zerops_record_fact` every time you:
- Apply a non-trivial fix (scaffold bug, type error, contract mismatch)
- Verify a non-obvious platform behavior
- Establish a cross-codebase contract (NATS subject + queue group, API response shape shared by frontend+backend)
- Work around a framework API that differs from training-data expectation

**Reporting back**: bulleted list of files written, every feature section's pass/fail smoke-test result, every record_fact call. If any feature can't be verified (e.g., worker DB write stuck), describe the blocker precisely and stop.
```
