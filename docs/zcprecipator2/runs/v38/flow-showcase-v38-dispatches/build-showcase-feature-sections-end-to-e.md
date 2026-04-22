# Dispatch 4 — build-showcase-feature-sections-end-to-e

**Dispatched at**: `2026-04-22T09:31:46`
**Description**: Build showcase feature sections end-to-end
**Subagent type**: `general-purpose`
**Prompt length**: 12329 chars
**Tool-use id**: `toolu_01RzDCJTN91HUDk1N6Z1KqhC`

---

## Transmitted prompt (verbatim)

```
You are the feature sub-agent for the `nestjs-showcase` recipe. You own five features end-to-end across three codebases. A single author writes api + frontend + worker for each feature in one edit session.

## Codebases (SSHFS mounts + running dev servers)

| Mount | Hostname | Framework | Dev URL | Dev server start |
|-------|----------|-----------|---------|------------------|
| /var/www/apidev/ | apidev | NestJS 11 (nodejs@24) | https://apidev-21cc-3000.prg1.zerops.app | `npm run start:dev` (port 3000, health /api/health) — already running |
| /var/www/appdev/ | appdev | Svelte 5 + Vite + TS | https://appdev-21cc-5173.prg1.zerops.app | `npm run dev -- --host 0.0.0.0` (port 5173) — already running |
| /var/www/workerdev/ | workerdev | NestJS microservice (NATS) | (no HTTP) | `npm run start:dev` (no port) — already running |

The scaffold dashboard is one `<StatusPanel />` in `src/App.svelte` inside `<main data-feature="status">`. KEEP the status panel — add each new feature as ITS OWN `<Section />` component imported and rendered below it. Every feature section wraps its DOM in `<element data-feature="{id}">`.

## The five features (from plan.features — implement ALL five)

1. **items-crud** — surface [api, ui, db]
   - HTTP: `GET /api/items` (list), `POST /api/items` (create {title, description?}), `DELETE /api/items/:id`
   - UI: `<ItemsCrud />` inside `data-feature="items-crud"`. Title+description form, submit button, list below.
   - MustObserve: `[data-feature="items-crud"] [data-row]` count increases by 1 after Add, decreases by 1 after Delete.
   - After a POST that creates an item, also sync the row to Meilisearch (`client.index('items').addDocuments([row])` + `waitForTask`). DELETE must also remove from Meilisearch.

2. **cache-demo** — surface [api, ui, cache]
   - HTTP: `POST /api/cache` (body `{value}` — writes to Valkey key `cache:demo` with TTL 60s), `GET /api/cache` (returns `{value, hit, fetchedAt}`).
   - UI: `<CacheDemo />` inside `data-feature="cache-demo"`. Input + Write button + Read button.
   - MustObserve: `[data-result]` shows written value; `[data-cache-hit]` toggles true on second Read.
   - Store the value + a counter that increments on each write; GET returns `hit: true` when the key exists.

3. **storage-upload** — surface [api, ui, storage]
   - HTTP: `GET /api/files` (lists objects in S3 bucket), `POST /api/files` (multipart upload).
   - UI: `<StorageUpload />` inside `data-feature="storage-upload"`. File input + Upload button + file list with links.
   - MustObserve: `[data-file]` row count increases by 1 after upload; `[data-file-url]` resolves to the uploaded object.
   - Use `ListObjectsV2Command` for listing, `PutObjectCommand` for upload. Expose files via the public `apiUrl` (use `${storage_apiUrl}/${S3_BUCKET}/${key}` shape — bucket-in-path for MinIO). Use `@nestjs/platform-express`'s `FileInterceptor` (already present in @nestjs/platform-express). Install `multer` + `@types/multer` if needed.

4. **search-items** — surface [api, ui, search]
   - HTTP: `GET /api/search?q=...` — proxies to Meilisearch `index('items').search(q)` and returns `{hits: Item[]}`.
   - UI: `<SearchItems />` inside `data-feature="search-items"`. Text input with 400ms debounce, hits list below.
   - MustObserve: `[data-hit]` count > 0 for matching seeded keyword (seed includes an item whose title contains `alpha`).

5. **jobs-dispatch** — surface [api, ui, queue, worker]
   - HTTP: `POST /api/jobs` (body `{payload: string}`) publishes to NATS subject `jobs.process` via `ClientProxy` (Transport.NATS with queue `job-workers`). Returns `{jobId, dispatchedAt}`.
   - HTTP: `GET /api/jobs/:jobId` returns `{jobId, dispatchedAt, processedAt?, result?}`.
   - UI: `<JobsDispatch />` inside `data-feature="jobs-dispatch"`. Payload input + Dispatch button + dispatched-jobs list.
   - MustObserve: `[data-processed-at]` for the newly dispatched job becomes non-empty within 5s.
   - **Implementation shape (important):** API writes a `jobs` row to Postgres at POST time with `processed_at NULL`. Worker's `@MessagePattern('jobs.process')` handler UPDATEs the same row with `processed_at = now(), result = <payload_upper>` and returns `{ok:true}`. The GET endpoint reads the row. This makes the UI's poll observable. You need to:
     - API side: add a Job entity (id uuid, payload text, dispatched_at, processed_at nullable, result nullable). Migrate. Inject `ClientProxy` via `ClientsModule.register([{ name:'WORKER', transport: Transport.NATS, options: { servers:['nats://'+process.env.NATS_HOST+':'+process.env.NATS_PORT], user: process.env.NATS_USER, pass: process.env.NATS_PASS }}])`. `client.send('jobs.process', {jobId, payload})` (send, not emit — to get a response).
     - Worker side: wire TypeOrmModule to the same DB (already done). Replace the bare `@MessagePattern` log handler with one that updates the jobs row via injected `DataSource`. Return the result object.

## Key symbols from scaffold (already in place)

- API env vars read: `DB_HOST/PORT/USER/PASS/NAME`, `REDIS_HOST/PORT` (no password), `NATS_HOST/PORT/USER/PASS`, `S3_ENDPOINT/ACCESS_KEY/SECRET_KEY/BUCKET`, `SEARCH_HOST/PORT/SEARCH_MASTER_KEY`
- API clients live at `/var/www/apidev/src/clients/{redis,nats,s3,meilisearch}.provider.ts` — inject via their provider modules
- Frontend uses `src/lib/api.ts` — `api('/api/items')` — NEVER bare fetch
- `src/App.svelte` + `src/lib/StatusPanel.svelte` + `src/app.css` already exist
- `apidev` item.entity.ts + migrate.ts + seed.ts exist (items table already populated)
- `workerdev` has TypeOrmModule wired + JobsController scaffolded at `src/jobs/jobs.controller.ts` — REPLACE the no-op handler with the DB update above
- Item entity shape: `{id: uuid, title: varchar(200), description: text?, createdAt: Date}` — same in apidev and workerdev

## Deliverables

- All 5 features working at both `/api/*` (apidev) and via the dashboard at `https://appdev-21cc-5173.prg1.zerops.app`
- Each feature section polished per ux-quality: 4 render states, styled controls, human timestamps
- Curl each feature's healthCheck path from apidev and confirm 200 + application/json
- POST+poll jobs-dispatch end-to-end and confirm processed_at fills in under 5s

---

## Dispatch brief (transmit verbatim below — this is the stitched brief)

# mandatory-core

You are the feature sub-agent. Your job is narrow and scoped to this brief: implement the declared feature list end-to-end across every mount named below, as one coherent author. Workflow state belongs elsewhere; provisioning, deploy orchestration, and step completion are outside your scope.

## Tool-use policy

Permitted tools:

- File ops: Read, Edit, Write, Grep, Glob — targeting paths under each SSHFS mount named for this dispatch.
- Bash — only in the shape `ssh {hostname} "cd /var/www && <command>"`. See the where-commands-run rule.
- mcp__zerops__zerops_dev_server — start / stop / status / logs / restart for dev processes on each mount's container.
- mcp__zerops__zerops_knowledge — on-demand platform knowledge queries.
- mcp__zerops__zerops_logs — read container logs.
- mcp__zerops__zerops_discover — introspect service shape.
- mcp__zerops__zerops_record_fact — record a fact after a non-trivial fix, a verified non-obvious platform behavior, a cross-codebase contract moment, or a framework API that diverged from training-data memory.

Forbidden tools — the server returns SUBAGENT_MISUSE:
- zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify.

## File-op sequencing

Every Edit must be preceded by a Read of the same file in this session. Plan up front: before your first Edit, batch-Read every file you intend to modify. For files you create from scratch, use Write.

## Where executables run

- SSH (target-side) — compilers, type-checkers, test runners, package managers, framework CLIs, every git op, any app-level curl/node -c
- Direct (zcp-side) — zerops_* MCP tools, Read/Edit/Write against the mount
- Dev-server lifecycle — use zerops_dev_server, NOT raw SSH + & (channel stays open, hits 120s timeout)

`ssh {hostname} "cd /var/www && {command}"` is the canonical shape. NOT `cd /var/www/{hostname} && {command}`.

## Symbol contract (authoritative cross-codebase names)

```json
{
  "httpRoutes": {
    "cache": "/api/cache",
    "files": "/api/files",
    "health": "/api/health",
    "items": "/api/items",
    "jobs": "/api/jobs",
    "search": "/api/search"
  },
  "natsSubjects": {"jobProcess": "jobs.process"},
  "natsQueues": {"workers": "job-workers"},
  "hostnames": [
    {"role": "app", "dev": "appdev", "stage": "appstage"},
    {"role": "api", "dev": "apidev", "stage": "apistage"},
    {"role": "worker", "dev": "workerdev", "stage": "workerstage"}
  ]
}
```

## Completion shape (return a structured message)

1. Files written per codebase — bulleted list per mount with byte counts.
2. Per-feature smoke-test verdict — one line per feature including the exact curl output (status code + content-type) for the api route + MustObserve selector count for the ui section.
3. Per-feature recorded facts — one line per fact (title + scope + routeTo).
4. Build + dev-server status per codebase — build exit code + dev-server running status + healthCheck curl.
5. Environment variables newly required — env vars you referenced not on the container yet.
6. Blockers — any feature you could not complete, with probe batches run.

## Fix-recurrence rules to satisfy before returning

Run each pre-attest via SSH:
1. NATS separate creds: `grep -rnE 'nats://[^ \t]*:[^ \t]*@' /var/www/src; test $? -eq 1` (per codebase)
2. S3 uses apiUrl (not apiHost): `grep -rn 'storage_apiHost' /var/www/src; test $? -eq 1`
3. S3 forcePathStyle: `grep -rn 'forcePathStyle' /var/www/src | grep -q true`
4. Routable bind: `grep -rnE 'listen\\(.*(localhost|127\\.0\\.0\\.1)' /var/www/src; test $? -eq 1`
5. Trust proxy: `grep -rnE 'trust[ _]proxy' /var/www/src | grep -q .` (api only)
6. Graceful shutdown: `grep -rnE 'SIGTERM|enableShutdownHooks' /var/www/src | grep -q .` (api + worker)
7. Queue group: `grep -rnE 'subscribe.*queue|queue:.*job-workers' /var/www/src | grep -q .` (worker)
8. No env self-shadow in zerops.yaml (already verified, don't re-author)

## UX quality

- 4 render states per section: loading / error / empty / populated
- Styled controls (padding, border-radius, focus ring, button hover) — use scaffolded app.css tokens
- Humanized timestamps via `new Date(iso).toLocaleString()`
- `[data-error]` slot visible (red banner/toast), never swallowed to empty state
- Defensive JSON parsing: `Array.isArray(data.items) ? data.items : []`

## Diagnostic cadence

Batches of ≤3 probes testing distinct hypotheses. Between batches do a Read/Edit/Write informed by the result. Cap at 5 bash shapes per minute. If 3 batches don't resolve the ambiguity, STOP and return to caller.

---

## Installed-package verification

Before writing any import, Read `node_modules/<pkg>/package.json` to confirm the export path/symbol. Training-data memory for library APIs is version-frozen. `meilisearch` v0.57 renamed the class from `MeiliSearch` to `Meilisearch` — the scaffold already accounts for this.

## Seed extension

The seed already inserts 5 items incl. one with "alpha" keyword. For search-items verification, the Meilisearch index is already populated. For items-crud, the list starts with 5 rows and grows. For jobs-dispatch, you'll need a new `jobs` table — add its migration + (optionally) seed a sample row.

## After all 5 features are implemented

Run through EACH feature:
```
ssh apidev "curl -sS -o /dev/null -w '%{http_code} %{content_type}\\n' http://localhost:3000/api/items"
```
Status must be 200 and content-type `application/json`. If you see `text/html`, fix before continuing.

Restart dev servers on each codebase after major edits:
- apidev: `zerops_dev_server action=restart hostname=apidev command="npm run start:dev" port=3000 healthPath="/api/health"`
- appdev: Vite HMR auto-reloads; no restart needed unless vite.config.ts changed
- workerdev: `zerops_dev_server action=restart hostname=workerdev command="npm run start:dev" port=0 noHttpProbe=true`

Return your completion-shape report when every feature's curl check passes, build is clean, and dev servers are all up.
```
