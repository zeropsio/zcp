# Dispatch 6 — code-review-nestjs-svelte-showcase

**Dispatched at**: `2026-04-22T19:30:59`
**Description**: Code review: NestJS + Svelte showcase
**Subagent type**: `general-purpose`
**Prompt length**: 8177 chars
**Tool-use id**: `toolu_01KdQuGNk83dwuNk2b8haw1i`

---

## Transmitted prompt (verbatim)

```
You are a NestJS 11 + Svelte 5 expert reviewing the CODE of a Zerops recipe (`nestjs-showcase`). You have deep knowledge of NestJS, Svelte 5, TypeORM, Vite, and the Node ecosystem but NO knowledge of the Zerops platform beyond what's in this brief. Do NOT review platform config files (zerops.yaml, import.yaml) — the main agent has platform context and has already validated them against the live schema. Your job is to catch things only a NestJS+Svelte expert catches.

**CRITICAL — where commands run:** you are on the zcp orchestrator, not the target container. `/var/www/{appdev,apidev,workerdev}/` is an SSHFS mount. All target-side commands (compilers, test runners, linters, package managers, framework CLIs, app-level `curl`) MUST run via `ssh {hostname} "cd /var/www && ..."`, not against the mount. If you see `fork failed: resource temporarily unavailable`, you ran a target-side command on zcp via the mount.

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Code review is mostly Read-heavy (you're inspecting, not authoring). Plan: Read every file you intend to inspect or modify before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify, zerops_browser, agent-browser. Violating any of these corrupts workflow state or forks the orchestrator.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

## Codebases to review (3 mounts)

- `/var/www/apidev/` — NestJS 11 API + TypeORM + PostgreSQL + ioredis (Valkey) + nats + @aws-sdk/client-s3 + meilisearch. Routes under `/api/*` global prefix. Migrations in `src/migrate.ts`, seed in `src/seed.ts`, search-import in `src/search-import.ts`.
- `/var/www/appdev/` — Svelte 5 + Vite SPA. Single page mounting `<StatusPanel />` + 5 feature panels (Items, CacheDemo, StorageUpload, SearchItems, JobsDispatch). Single fetch helper at `src/lib/api.ts`. Vite v8 + TypeScript v6 (next-major prereleases pinned by `npm create vite@latest -- --template svelte-ts`).
- `/var/www/workerdev/` — NestJS standalone application context (no HTTP). Subscribes to NATS `jobs.>` with queue group `showcase-workers`. SIGTERM drain. Shares Postgres `jobs` table with apidev.

## Plan feature list (authoritative)

```
1. items-crud   surface=[api,ui,db]            healthCheck=/api/items     uiTestId=items-crud
2. cache-demo   surface=[api,ui,cache]         healthCheck=/api/cache     uiTestId=cache-demo
3. storage-upload surface=[api,ui,storage]     healthCheck=/api/files     uiTestId=storage-upload
4. search-items surface=[api,ui,search]        healthCheck=/api/search    uiTestId=search-items
5. jobs-dispatch surface=[api,ui,queue,worker] healthCheck=/api/jobs      uiTestId=jobs-dispatch
```

## Review checklist (NestJS + Svelte expert)

For apidev:
- Module wiring: every feature module is imported in `app.module.ts`. Service provider tokens (REDIS_CLIENT, NATS_CLIENT, S3_CLIENT, MEILI_CLIENT, S3_BUCKET) are exported as `@Global` providers and consumed via `@Inject(TOKEN)`.
- Class-validator DTOs on every `@Body()` / `@Query()` / `@Param()` route. ValidationPipe global with `whitelist: true, transform: true` (in main.ts).
- TypeORM repository methods are awaited. No N+1 patterns in list endpoints.
- Async resource cleanup: every `@Global()` client provider implements `OnModuleDestroy` and closes its underlying connection.
- No `synchronize: true` on TypeORM config under any condition.
- StatusService pings every managed service via `Promise.allSettled` so a single dependency failure doesn't blow up `/api/status`.
- main.ts: `app.enableShutdownHooks()`, `app.getHttpAdapter().getInstance().set('trust proxy', true)`, `app.listen(port, '0.0.0.0')`.

For appdev (Svelte 5):
- Every `fetch()` goes through `src/lib/api.ts` `api()` helper — bare `fetch()` in components is forbidden (the helper enforces content-type + status checks).
- Svelte 5 idioms: `$state`, `$derived`, `$effect`, `onMount` from svelte. No legacy `<script>` reactive labels.
- Each panel is wrapped in `<section data-feature="{uiTestId}">`. Each panel renders four states: loading, error (with `[data-error]` banner), empty, populated.
- No bundled framework defaults (Counter.svelte, vite logo) leftover.
- vite.config.ts: `server.host: '0.0.0.0'`, `server.port: 5173`, `server.allowedHosts: ['.zerops.app']`, same for preview.

For workerdev:
- main.ts: `NestFactory.createApplicationContext` (NOT `NestFactory.create` — workers have no HTTP), `app.enableShutdownHooks()`, SIGTERM + SIGINT handlers that call `worker.stop()` then `app.close()` then `process.exit(0)`.
- worker.service.ts: NATS subscribe MUST pass `{ queue: 'showcase-workers' }` for competing-consumer semantics under `minContainers >= 2`. Handler tracks in-flight handlers in a Set so `stop()` can `Promise.allSettled` them before unsubscribe.
- nats.connection.ts: connect via `connect({ servers, user, pass })` — NOT URL-embedded credentials. `OnModuleDestroy` calls `await connection.drain()`.
- Job entity (`src/entities/job.entity.ts`) MUST be byte-identical to apidev's `src/entities/job.entity.ts` (the worker shares the DB).
- Worker handler updates row state pending → processing → done/failed. Writes `processedAt` timestamp on completion. Loud failure: errors set status=failed with the error message, NEVER swallow.

## Silent-swallow antipattern scan (MANDATORY)

For each of the 3 codebases:
1. **Init scripts** (apidev/src/migrate.ts, src/seed.ts, src/search-import.ts): every catch block must `process.exit(1)` on error, NEVER just log and return. Search index sync must `.waitTask()` before script exits.
2. **Client-side fetch** (appdev/src/**/*.svelte and src/lib/api.ts): every fetch checks `res.ok`, every JSON-consuming code checks the content-type contains `application/json`. Every array consumer in components has `[]` default (e.g. `let items: Item[] = $state([])`).
3. **Async-durable writes**: Meilisearch `addDocuments()` followed by `.waitTask()` (verify this is in `apidev/src/search-import.ts`).

## Feature coverage scan (MANDATORY)

For each of the 5 plan features above:
- API surface: grep apidev for the controller path matching `healthCheck`. Missing = `[CRITICAL]`.
- UI surface: grep appdev for `data-feature="{uiTestId}"`. Missing = `[CRITICAL]`.
- Worker surface (jobs-dispatch only): grep workerdev for the NATS subject + queue group. Missing = `[CRITICAL]`.

Also grep for `data-feature="..."` attributes that are NOT in the declared list (extra features = `[WRONG]`, leftover scope creep).

## Out of scope (do NOT review)

- `zerops.yaml` files — main agent owns platform config
- `import.yaml` files in `/var/www/zcprecipator/nestjs-showcase/` — main agent owns env-tier config
- README integration-guide YAML blocks (those mirror zerops.yaml — main agent owns)
- Service hostname naming, env var cross-service refs, mode flags, scaling fields

## Reporting

Issues classified as: `[CRITICAL]` (breaks the app), `[WRONG]` (incorrect code but works), `[STYLE]` (quality improvement), `[SYMPTOM]` (observed behavior that might have a platform cause — main agent to investigate).

For each issue: file path, line number, what's wrong, proposed fix.

Apply fixes for CRITICAL and WRONG findings YOURSELF via Read/Edit on the SSHFS mount. After applying: `ssh {hostname} "cd /var/www && npm run build"` to verify the codebase still compiles. Report STYLE/SYMPTOM only.

## Final report shape

1. Files reviewed: count per codebase
2. Findings: N CRITICAL / N WRONG / N STYLE / N SYMPTOM with one-line each
3. Fixes applied: list of CRITICAL + WRONG fixes you made (file:line, before → after)
4. Silent-swallow scan: clean / list violations
5. Feature coverage scan: clean / missing features
6. Build status per codebase: exit code of `npm run build`
```
