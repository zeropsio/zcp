# Dispatch 4 — feature-sub-agent-implement-5-showcase-f

**Dispatched at**: `2026-04-21T19:58:22`
**Description**: Feature sub-agent: implement 5 showcase features
**Subagent type**: `general-purpose`
**Prompt length**: 12591 chars
**Tool-use id**: `toolu_01T7LYbq5KWJR41Nht5RxYTq`

---

## Transmitted prompt (verbatim)

```
# mandatory-core

You are the feature sub-agent for the Zerops nestjs-showcase recipe. Your job is narrow and scoped to this brief: implement the declared feature list end-to-end across every mount named below, as one coherent author. Workflow state belongs elsewhere; provisioning, deploy orchestration, and step completion are outside your scope.

## Tool-use policy

Permitted tools:
- File ops: `Read`, `Edit`, `Write`, `Grep`, `Glob` — targeting paths under each SSHFS mount named for this dispatch.
- `Bash` — only in the shape `ssh {hostname} "cd /var/www && <command>"`. See the where-commands-run rule.
- `mcp__zerops__zerops_dev_server` — start / stop / status / logs / restart for dev processes on each mount's container.
- `mcp__zerops__zerops_knowledge` — on-demand platform knowledge queries.
- `mcp__zerops__zerops_logs` — read container logs.
- `mcp__zerops__zerops_discover` — introspect service shape.
- `mcp__zerops__zerops_record_fact` — record facts at moment of freshest knowledge.

Forbidden tools (server returns SUBAGENT_MISUSE): zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify.

## File-op sequencing

Every `Edit` must be preceded by a `Read` of the same file in this session. Batch-Read every file you intend to modify across every mount before the first Edit. Files you create from scratch use `Write` (no Read required).

## Where executables run

Every executable — compilers, type-checkers, test runners, linters, package managers, framework CLIs, git operations, app-level `curl` — runs inside its target container via `ssh {hostname} "cd /var/www && <command>"`. File writes use Write / Edit / Read against the mount. Never `cd /var/www/{hostname}` on zcp — the mount is NOT an execution surface.

Dev-server lifecycle uses `mcp__zerops__zerops_dev_server`, never raw SSH `&`.

---

## This dispatch

### Codebases (3 mounts, 3 hostnames — already scaffolded, builds green, dev servers running)

| Hostname  | Mount              | Stack                          | Dev-server command          | Port  | Health         |
|-----------|--------------------|--------------------------------|-----------------------------|-------|----------------|
| apidev    | /var/www/apidev    | NestJS 11 + TypeORM + Express  | `npm run start:dev`         | 3000  | /api/health    |
| appdev    | /var/www/appdev    | Svelte 5 + Vite 8 + TS         | `npm run dev`               | 5173  | /              |
| workerdev | /var/www/workerdev | NestJS 11 microservice (NATS)  | `npm run start:dev`         | —     | no HTTP probe  |

Dev servers are CURRENTLY RUNNING on all three hosts. After any source change, Nest watch rebuilds automatically; Vite HMR updates in-browser automatically. Only if a dev server crashes do you need to restart via `mcp__zerops__zerops_dev_server`.

Public subdomains (for smoke tests):
- API: https://apidev-21c7-3000.prg1.zerops.app
- App: https://appdev-21c7-5173.prg1.zerops.app

### Managed services (already provisioned, seeded, verified reachable via /api/status)

| Kind    | Hostname | Env var names on apidev & workerdev                                                                              |
|---------|----------|------------------------------------------------------------------------------------------------------------------|
| db      | db       | DB_HOST, DB_PORT, DB_USER, DB_PASS, DB_NAME                                                                      |
| cache   | redis    | REDIS_HOST, REDIS_PORT (no auth — Valkey managed has none)                                                       |
| queue   | queue    | QUEUE_HOST, QUEUE_PORT, QUEUE_USER, QUEUE_PASS (pass as structured NATS options, never URL-embedded)             |
| storage | storage  | STORAGE_APIHOST, STORAGE_APIURL, STORAGE_BUCKETNAME, STORAGE_ACCESSKEYID, STORAGE_SECRETACCESSKEY                |
| search  | search   | SEARCH_HOST, SEARCH_PORT, SEARCH_MASTERKEY                                                                       |

DB schema already migrated (tables `items`, `jobs`). 5 rows already seeded into `items`; Meilisearch `items` index has 5 docs.

### Features to implement (5)

Each feature is a vertical slice: API route + UI section + worker handler (when applicable). Each UI section wraps in `<section data-feature="{id}">` and renders the selector the MustObserve assertion checks.

1. **items-crud** — `[api, ui, db]`
   - HealthCheck: `GET /api/items` → 200 JSON array of `{id, title, description, createdAt}`.
   - Also: `POST /api/items` (body: `{title, description?}`) → 201 JSON of created item; `DELETE /api/items/:id` → 204.
   - UI: form with title input + description textarea + Submit button. List below with one `<div data-row data-id="{id}">` per item showing title + desc + delete button.
   - MustObserve: `[data-feature="items-crud"] [data-row]` count increments by 1 after submit.
   - Interaction: fill title, click Submit, observe new row.

2. **cache-demo** — `[api, ui, cache]`
   - HealthCheck: `GET /api/cache` → 200 JSON `{key, value, source}` where `source` is `"miss"` or `"cache"`. If no `?key=` given, use a default demo key.
   - Also: `POST /api/cache` (body `{key, value}`) writes a value with 60s TTL, returns `{ok:true}`.
   - UI: value input + Write button; Read button shows current value + source label (`data-source`) and the written value (`data-result`).
   - MustObserve: `[data-result]` text equals value written; `[data-source]` text = `"cache"` on second read.

3. **storage-upload** — `[api, ui, storage]`
   - HealthCheck: `GET /api/files` → 200 JSON array of `{key, size, lastModified, url}` where url is a presigned GET valid 5 min.
   - Also: `POST /api/files` (multipart form-data, field `file`) uploads to bucket; returns `{key, size, url}`.
   - UI: file input + Upload button. List below with one `<div data-file data-key="{key}">` per uploaded file showing filename + size + a "download" link using the presigned URL.
   - MustObserve: `[data-file]` count increments by 1 after upload.

4. **search-items** — `[api, ui, search]`
   - HealthCheck: `GET /api/search?q=X` → 200 JSON `{hits: [{id, title, description}], total}`.
   - UI: search input. On each keystroke, debounce 400 ms, call `/api/search?q={value}`. Render `<div data-hit data-id="{id}">` per hit.
   - MustObserve: `[data-hit]` count > 0 for a seeded query (matches any of the seeded titles: "Blue Wool Socks", "Vintage Camera Strap", "Ceramic Pour-Over Dripper", "Trail Running Headlamp", "Linen Napkin Set").

5. **jobs-dispatch** — `[api, ui, queue, worker]`
   - HealthCheck: `GET /api/jobs` → 200 JSON array of `{id, status, payload, processedAt, createdAt}` sorted by createdAt DESC.
   - Also: `POST /api/jobs` (body `{payload}`) inserts a `jobs` row with status `"pending"`, publishes `{jobId, payload}` on NATS subject `jobs.run`, returns the created row JSON.
   - Worker (workerdev): `@MessagePattern('jobs.run')` handler — simulate work (e.g., 500 ms delay), update the `jobs` row: `processedAt = now()`, `status = "done"`. Use TypeORM from the worker's DataSource — the worker codebase already has TypeOrmModule configured.
   - UI: Dispatch button + recent-jobs table with `<tr data-job data-id="{id}">` rows showing id, status, processedAt (or "…" if null). Poll `/api/jobs` every 1 s.
   - MustObserve: latest `[data-job]` row's `[data-processed-at]` element has non-empty text within 5 s of dispatch.

### SymbolContract (authoritative)

```json
{
  "envVarsByKind": {
    "db": {"host": "DB_HOST", "port": "DB_PORT", "user": "DB_USER", "pass": "DB_PASS", "name": "DB_NAME"},
    "cache": {"host": "REDIS_HOST", "port": "REDIS_PORT"},
    "queue": {"host": "QUEUE_HOST", "port": "QUEUE_PORT", "user": "QUEUE_USER", "pass": "QUEUE_PASS"},
    "storage": {"apiHost": "STORAGE_APIHOST", "apiUrl": "STORAGE_APIURL", "bucket": "STORAGE_BUCKETNAME", "accessKey": "STORAGE_ACCESSKEYID", "secretKey": "STORAGE_SECRETACCESSKEY"},
    "search": {"host": "SEARCH_HOST", "port": "SEARCH_PORT", "masterKey": "SEARCH_MASTERKEY"}
  },
  "httpRoutes": {
    "items": "/api/items",
    "items_item": "/api/items/:id",
    "cache": "/api/cache",
    "files": "/api/files",
    "files_item": "/api/files/:key",
    "search": "/api/search",
    "jobs": "/api/jobs"
  },
  "natsSubjects": {
    "job_dispatch": "jobs.run"
  },
  "natsQueues": {
    "workers": "jobs-worker"
  },
  "dtos": ["ItemDto", "CacheResultDto", "FileDto", "SearchResponseDto", "JobDto", "JobDispatchPayload"]
}
```

DTOs are copy-pasted byte-identically between codebases. Declare each at the top of the owning API controller (apidev), then copy into the consuming Svelte component (appdev) and the worker handler (workerdev).

### Fix-recurrence rules applicable here

- `nats-separate-creds` — NATS client options pass `{servers, user, pass}`, never `nats://user:pass@host`.
- `s3-uses-api-url` — `endpoint: process.env.STORAGE_APIURL` (https://), not STORAGE_APIHOST.
- `s3-force-path-style` — `forcePathStyle: true`.
- `routable-bind` — HTTP servers bind 0.0.0.0.
- `trust-proxy` — Express trust proxy set.
- `graceful-shutdown` — `app.enableShutdownHooks()`.
- `queue-group` — NATS subscribers declare `queue: 'jobs-worker'`.
- `env-self-shadow` — no `KEY: ${KEY}` in zerops.yaml run.envVariables.

Run the pre-attest greps before returning; fix before reporting done.

### Ordering

Implement features in this order (strictly sequential — never split a feature across codebase passes):

1. **items-crud** — grounds CRUD + DTO pattern, needed for search-items to query.
2. **cache-demo** — quickest roundtrip, builds confidence in cache-manager API on prod lockfile.
3. **search-items** — reuses the seeded index; API calls Meilisearch client with the master key.
4. **storage-upload** — multipart form handling on NestJS (needs `@nestjs/platform-express` multer middleware); presigned GET URL generation.
5. **jobs-dispatch** — last because it touches ALL three codebases. Implement worker handler in same session as API endpoint + UI.

For each feature, after writing code:
- `ssh apidev "cd /var/www && npm run build 2>&1 | tail -30"` — confirm tsc succeeds in API.
- For `jobs-dispatch`: also `ssh workerdev "cd /var/www && npm run build 2>&1 | tail -30"`.
- For UI: `ssh appdev "cd /var/www && npx vite build 2>&1 | tail -30"`.
- Smoke test: `ssh apidev "curl -sS -o /dev/null -w '%{http_code} %{content_type}\n' http://localhost:3000{route}"`.
- Nest watch auto-restarts on source changes; Vite HMR auto-updates. If a process dies, restart via `zerops_dev_server restart`.

### Dashboard layout

Rewrite `appdev/src/App.svelte` to render, in order:
- `<h1>Zerops NestJS Showcase</h1>` + a one-line tagline.
- `<StatusPanel />` (existing scaffold, keep it — 5 service health dots).
- One `<section data-feature="items-crud">`, one `<section data-feature="cache-demo">`, one `<section data-feature="search-items">`, one `<section data-feature="storage-upload">`, one `<section data-feature="jobs-dispatch">`.

Each section: heading + description + body. Use the Svelte 5 runes syntax (`$state`, `$derived`, `$effect`). Style with a shared CSS tokens file — consistent padding, border-radius, focus rings, buttons with hover, table striping, monospace for ids. System font stack. Use ONE accent color across all sections.

Every fetch goes through the scaffold's `src/lib/api.ts` helper. Absolutely no raw `fetch()` in components. Extend `api.ts` if needed to support POST/DELETE with JSON or multipart (add an `apiMultipart(path, formData)` helper if you prefer).

### UX acceptance (hard gates)

- Each section has loading / error / empty / populated render states. Error is visible (red banner in a `[data-error]` slot), not swallowed into empty.
- `[data-error]` element visible on API failure.
- Timestamps humanized via `new Date(iso).toLocaleString()`.
- Monospace IDs.
- Auto-escape everything (Svelte does this by default — never use `{@html}` on user input).
- Build passes: tsc on apidev + workerdev, vite build on appdev.

### Reporting

Return the completion-shape report:
1. Files written per codebase with byte counts.
2. Per-feature smoke-test verdict (curl line + MustObserve selector count observed from `ssh appdev "curl -s http://localhost:5173/ | grep -c 'data-feature'"` or equivalent).
3. Per-feature recorded facts (title + scope + routeTo).
4. Build + dev-server status per codebase.
5. New env vars required (none expected; if any, surface).
6. Blockers (none expected; if any, describe probes + hypotheses).

Start with items-crud. Good luck.
```
