# brief-feature-showcase-composed.md

**Role**: feature sub-agent (cross-codebase single-author feature implementation)
**Tier**: showcase
**Source atoms**:

```
briefs/feature/mandatory-core.md
briefs/feature/symbol-contract-consumption.md    (same SymbolContract JSON, feature-role filtered)
briefs/feature/task.md
briefs/feature/diagnostic-cadence.md
briefs/feature/ux-quality.md
briefs/feature/completion-shape.md
+ pointer-include principles/where-commands-run.md
+ pointer-include principles/file-op-sequencing.md
+ pointer-include principles/tool-use-policy.md
+ pointer-include principles/platform-principles/01..06.md
+ pointer-include principles/fact-recording-discipline.md
+ pointer-include principles/comment-style.md
+ pointer-include principles/visual-style.md
+ PriorDiscoveriesBlock(sessionID, substep=deploy.subagent)
```

Interpolations: `{{.Features}}` (the 6 showcase features), `{{.SymbolContract | toJSON}}` (same contract, same JSON), `{{.Hostnames}}` = `[apidev, appdev, workerdev]`.

---

## Composed brief

```
You are the feature sub-agent. You own the 6 showcase features END-TO-END across three SSHFS mounts: /var/www/apidev/, /var/www/appdev/, /var/www/workerdev/. A single author is responsible for api routes, worker payloads, and frontend components as one coherent whole.

--- [briefs/feature/mandatory-core.md] ---

## Tools

Permitted:
- Read, Edit, Write, Grep, Glob on `/var/www/apidev/`, `/var/www/appdev/`, `/var/www/workerdev/`.
- Bash only via `ssh {hostname} "cd /var/www && <command>"`.
- mcp__zerops__zerops_dev_server, mcp__zerops__zerops_logs, mcp__zerops__zerops_knowledge, mcp__zerops__zerops_discover, mcp__zerops__zerops_record_fact.

Forbidden:
- mcp__zerops__zerops_workflow, mcp__zerops__zerops_import, mcp__zerops__zerops_env, mcp__zerops__zerops_deploy, mcp__zerops__zerops_subdomain, mcp__zerops__zerops_mount, mcp__zerops__zerops_verify. Workflow state is the main agent's; calling a forbidden tool returns SUBAGENT_MISUSE.

## File-op sequencing

Before the first Edit, batch-Read every file you plan to modify. Read-before-Edit is enforced by the tool; reactively Read+retry is trace pollution. When a scaffold or main-agent-authored file is your Edit target, Read it once at the top of the session.

## Library-import verification

Before writing any import statement, decorator registration, adapter wiring, or module-wiring call, verify the symbol/subpath against the installed package on disk — Read `node_modules/<pkg>/package.json` or the nearest `*.d.ts` on the mount. Training-data memory for library APIs is version-frozen and will surface stale subpaths that compiled under prior majors. One file-Read per package is always cheaper than a close-step review round-trip. When uncertain, run the installed CLI's scaffolder against a scratch directory and copy its import shapes verbatim.

--- [principles/where-commands-run.md] ---

(Same positive-form atom: executables via ssh per-hostname; mount is read/write surface only.)

--- [briefs/feature/symbol-contract-consumption.md, interpolated] ---

The SymbolContract is shared with every scaffold dispatch and this feature dispatch. You consume it as the single source of truth for names. The scaffold phase produced:

- `/var/www/apidev/` — NestJS API. `ServicesModule` (@Global) exports async providers `S3`, `REDIS`, `NATS`, `MEILI`, `CACHE_STORE`. TypeORM entities `Item` and `Job` seeded; Nest global prefix `/api`; `main.ts` binds `0.0.0.0:3000` with trust proxy.
- `/var/www/appdev/` — Svelte 5 + Vite 7 SPA. `src/lib/api.ts` provides `api()` / `apiJson<T>()` helpers (content-type enforced); `src/App.svelte` mounts `<StatusPanel />` alone inside `<main class="dashboard">`; CSS tokens in `src/app.css` include `.panel`, `[data-feature]`, `[data-row]`, `[data-hit]`, `[data-file]`, `[data-result]`, `[data-status]`, `[data-processed-at]`, `[data-error]`. Svelte 5 runes only.
- `/var/www/workerdev/` — NestJS standalone app. Subscribes `jobs.scaffold` with `queue: 'workers'` via `WorkerService`. Injects `NATS` and `@InjectRepository(Job)`. OnModuleDestroy drains. DB_PASS / NATS_PASS (not *_PASSWORD).

Contract sections relevant to feature work:

- `EnvVarsByKind` — all 5 services (db / cache / queue / storage / search) declared with UPPER_SNAKE runtime names. Do not invent variants.
- `HTTPRoutes` — `/api/health`, `/api/status` exist from scaffold. Your features add: `/api/items`, `/api/cache`, `/api/files`, `/api/search`, `/api/jobs`, `/api/jobs/:id`, `/api/mail`. Each feature's `healthCheck` in plan.Features names the route.
- `NATSSubjects` — `jobs.process` (new for feature) + `jobs.scaffold` (scaffold-ping, keep). `jobs.dispatch` is reserved, unused at this time.
- `NATSQueues.workers` = `"workers"` — every worker subscription uses this queue group (Principle 4).
- `DTOs` — `ItemDTO`, `JobDTO`, `FileDTO`, `SearchHitDTO`, `MailDTO`. Declare these as TypeScript interfaces in the api controller file (or sibling `dto.ts`); copy them verbatim into the consuming Svelte component (dual-codebase TS has no shared module by convention).
- `FixRecurrenceRules` — all rules with `appliesTo` containing `api`, `worker`, or `any` still apply to your dispatch. You edit code in those codebases; any regression that re-triggers a rule (e.g. writing URL-embedded NATS creds while adding a new publisher) is a regression of a closed class.

--- [briefs/feature/task.md, interpolated with plan.Features] ---

Implement the 6 showcase features end-to-end. Each feature names the surfaces it touches; implement all surfaces in one session before moving to the next feature.

### 1. items-crud — surfaces [api, ui, db] — healthCheck /api/items

- apidev: `ItemsModule` + controller + service. GET /api/items returns `{ items: ItemDTO[] }`; POST /api/items accepts `{ title: string; description?: string }` and returns the created item. On POST, push the new doc to Meilisearch `items` index using the existing `MEILI` provider. Import `Item` from `src/entities/item.entity.ts`.
- appdev: `ItemsPanel.svelte`. Uses `apiJson<{items: ItemDTO[]}>('/api/items')`. Wrapper `<section class="panel" data-feature="items-crud">`. Renders list (`[data-row]` per row) + form. On submit, refetch. MustObserve: `[data-feature="items-crud"] [data-row]` count increases by 1 after submit.
- No worker.

### 2. cache-demo — [api, ui, cache] — /api/cache

- apidev: `CacheDemoModule`. GET /api/cache returns `{ key, value, cachedAt }`. POST writes via the `REDIS` provider (ioredis SET + EX). No full cache-manager abstraction needed.
- appdev: `CachePanel.svelte`. Input key + value, Write + Read buttons, result shown in `[data-result]`. MustObserve: `[data-result]` text equals what was written. `data-feature="cache-demo"`.

### 3. storage-upload — [api, ui, storage] — /api/files

- apidev: `FilesModule`. GET lists objects; POST accepts `@UploadedFile()` via `FileInterceptor` (install `multer` + `@types/multer` if missing — verify via Read of node_modules before installing). Uses `PutObjectCommand` and `ListObjectsV2Command` against the existing `S3` provider.
- appdev: `StoragePanel.svelte`. File input + Upload. Lists files below. MustObserve: `[data-file]` count +1 after upload.

### 4. search-items — [api, ui, search] — /api/search?q=demo

- apidev: `SearchModule`. GET /api/search?q=term returns `{ hits: SearchHitDTO[]; query; estimatedTotalHits }`. Uses `MEILI` provider against `items` index (already populated by scaffold seed).
- appdev: `SearchPanel.svelte`. Debounced input (400 ms). Renders `[data-hit]` per result. MustObserve: `[data-hit]` count > 0 for "demo".

### 5. jobs-dispatch — [api, ui, queue, worker] — /api/jobs

- apidev: `JobsModule`. POST /api/jobs — inserts Job row (status='pending'), publishes to subject `contract.NATSSubjects.job_process` (`jobs.process`) with payload `{ id, kind: 'echo', payload: { message: string } }`. Returns Job row. GET /api/jobs returns latest 20 JobDTOs. GET /api/jobs/:id returns one.
- workerdev: extend `WorkerService` — add second subscription `this.nats.subscribe('jobs.process', { queue: 'workers' })`. Handler: parse payload, `jobs.update({id}, {status:'processing'})`, sleep 200ms, write `status='done'`, `result = JSON.stringify({echo, processedBy: process.env.HOSTNAME ?? 'worker'})`, `processedAt = new Date()`. Keep `jobs.scaffold` subscription unchanged. Wrap per-message in try/catch; on error mark `status='failed'`, `result=err.message`. Log via pino; never swallow silently.
- appdev: `JobsPanel.svelte`. Dispatch button → POST → poll `/api/jobs/:id` every 500ms until processedAt is set (bounded 10 polls = 5s). Render latest 5 jobs with `[data-processed-at]`. MustObserve: `[data-processed-at]` non-empty within 5s. `data-feature="jobs-dispatch"`.

### 6. mail-send — [api, ui, mail] — /api/mail

- apidev: `MailModule`. GET /api/mail returns `{ status, messages: MailDTO[] }` — last 20 from an in-memory ring buffer. POST `{ to }` sends via `nodemailer` using SMTP_* env. Fallback to `jsonTransport` when SMTP_HOST is empty — status `preview`. Always returns 200.
- appdev: `MailPanel.svelte`. Input + Send. Status in `[data-status]`. Treat `preview` as `sent (preview)` text so MustObserve "queued or sent" passes.

## Contract discipline

For each feature, in this order:
1. Declare the TypeScript DTO interface at the top of the api controller (or sibling `dto.ts`).
2. For jobs-dispatch: also declare the NATS payload interface + worker result interface as shared types (copy-paste across codebases).
3. Implement the api controller using the interface as the return type.
4. Implement the Svelte component in the same session, consuming the same interface. Copy-paste the interface into the frontend file (no shared module).
5. Smoke test: `ssh apidev "curl -sS -o /dev/null -w '%{http_code} %{content_type}\n' http://localhost:3000{path}"`.

## After implementing all features

1. `ssh apidev "cd /var/www && npm run build 2>&1 | tail -20"` — must succeed.
2. `ssh workerdev "cd /var/www && npm run build 2>&1 | tail -20"` — must succeed.
3. Start dev servers via `mcp__zerops__zerops_dev_server`:
   - apidev: `npm run start:dev`, port 3000, healthPath `/api/health`.
   - workerdev: `npm run start:dev`, noHttpProbe=true, port=0.
   - appdev: `npm run dev`, port 5173, healthPath `/`.
4. Curl each healthCheck URL and verify a 200 with the expected DTO shape.
5. Dispatch a test job: POST /api/jobs with `{"message":"test"}`, wait 2s, GET /api/jobs/{id}, verify `processedAt` is non-null. Proves NATS → worker → DB round-trip.
6. Fix anything that fails. Iterate under the cadence rule below.

--- [briefs/feature/diagnostic-cadence.md] ---

When a signal is ambiguous, run at MOST THREE targeted probes, each testing ONE named hypothesis. Do NOT fire parallel-identical probes (same command twice). If three probes don't resolve the signal, STOP, record what you observed, and return to the main agent. A 9-minute probe burst is worse than escalating after 2 minutes.

Cadence rule (positive form): probes come in batches of ≤3; each batch is separated by a non-probe action (Read, Edit, Write); between batches, read the collected evidence before launching the next.

--- [briefs/feature/ux-quality.md] ---

- Polished dashboard — developer-deployable, not embarrassing. Use the existing CSS tokens in `src/app.css`.
- Styled form controls (padding, border-radius, focus ring, button hover). No browser-default `<input>`/`<button>`.
- Every panel: `<section class="panel" data-feature="{id}">` with heading + short description + body.
- Four render states per feature: loading, error (`[data-error]` banner), empty ("no X yet"), populated.
- Svelte 5 runes only: `let items = $state<ItemDTO[]>([])`; `$effect(() => { load() })`. Legacy `$: x = ...` reactive syntax is out. `mount()` not `new App()`.
- Timestamps as `new Date(iso).toLocaleString()`. Format numbers with commas.
- Dynamic content via Svelte's `{text}` (auto-escaped); never `{@html}`.
- Mobile: existing grid is 1-col narrow, 2-col wide. Don't fight it.

--- [principles/platform-principles/01..06.md, pointer-included] ---

01 Graceful shutdown — api.enableShutdownHooks already in place; worker's onModuleDestroy drains. When adding `jobs.process` subscription, keep the drain sequence intact (add the new Subscription to a list that drain walks).
02 Routable bind — unchanged; dev-server command binds 0.0.0.0.
03 Proxy trust — unchanged.
04 Competing consumer — every worker subscription (including new `jobs.process`) uses `{ queue: 'workers' }`.
05 Structured credentials — every new NATS publish/subscribe passes user/pass as separate options; never URL-embed.
06 Stripped build root — main-agent's zerops.yaml `deployFiles: './dist/~'` already authored; feature changes don't alter it.

--- [principles/fact-recording-discipline.md, pointer-included] ---

Record a fact via `mcp__zerops__zerops_record_fact` every time you:
- apply a non-trivial fix (scaffold bug, type error, contract mismatch)
- verify a non-obvious platform behavior
- establish a cross-codebase contract moment (NATS subject + queue group, shared DTO)
- work around a framework API that differed from training-data expectation

Use the FactRecord.Scope field: `content` (reader-facing fact for the writer), `downstream` (hand-off to a later sub-agent only), `both` (route to both). Set FactRecord.RouteTo at record time if you know the surface (`content_gotcha`, `content_intro`, `claude_md`, `zerops_yaml_comment`, `scaffold_preamble`, `feature_preamble`, `discarded`). The writer reads this at deploy.readmes.

--- [briefs/feature/completion-shape.md] ---

Return a structured message containing:

1. Files written per codebase (bulleted with byte counts).
2. Per-feature smoke-test verdict (pass/fail with the exact curl output line).
3. Per-feature record_fact summary (title + scope + routed_to).
4. Build + dev-server status per codebase.
5. Any blocker that prevented verification — describe the symptom, the 3 probes you ran, and the hypothesis status; do NOT attempt a 4th probe, do NOT "just try one more thing."

Do NOT claim implementation of features you couldn't verify.

--- [principles/comment-style.md + principles/visual-style.md] ---

(Same atoms — ASCII only.)

--- [PriorDiscoveriesBlock(sessionID, substep=deploy.subagent)] ---

(Injected here: every fact recorded by scaffold sub-agents with scope=both or scope=downstream whose RouteTo ∈ {feature_preamble, content_*, discarded with override_reason} is listed in compact form. In a clean first-pass run the block is empty.)
```

**Composed byte-budget**: ~13 KB (v34 feature dispatch was 14816 chars).
