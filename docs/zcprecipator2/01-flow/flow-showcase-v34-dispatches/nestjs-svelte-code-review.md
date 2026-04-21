# Dispatch 6 — nestjs-svelte-code-review

**Dispatched at**: `2026-04-20T11:20:21`
**Description**: NestJS/Svelte code review
**Subagent type**: `general-purpose`
**Prompt length**: 6256 chars
**Tool-use id**: `toolu_01ELj7gXdjkmFyPAJive6wnf`

---

## Transmitted prompt (verbatim)

```
You are a NestJS + Svelte 5 code expert reviewing the CODE of a Zerops recipe. You have deep knowledge of NestJS 11 and Svelte 5 + Vite 7 but NO knowledge of the Zerops platform beyond what's in this brief. Do NOT review platform config files (zerops.yaml, import.yaml) — the main agent has platform context and has already validated them against the live schema. Your job is to catch things only a framework expert catches.

<<<MANDATORY — TRANSMIT VERBATIM>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. Plan: Read every file you intend to inspect or modify before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify, zerops_browser, agent-browser. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`.

<<<END MANDATORY>>>

**CRITICAL — where commands run**: you are on the zcp orchestrator, not the target container. `/var/www/{apidev,appdev,workerdev}/` are SSHFS mounts. All target-side commands (tsc, eslint, npm run, nest build, vite build, curl to running dev servers, etc.) MUST run via `ssh {hostname} "cd /var/www && ..."`, not against the mount on zcp.

## Mounts to review

- `/var/www/apidev/` — NestJS 11 JSON API on nodejs@22.
- `/var/www/appdev/` — Svelte 5 + Vite 7 SPA.
- `/var/www/workerdev/` — NestJS standalone app (NATS consumer, no HTTP).

## Read and review (direct fixes allowed)

- All source files in each mount's `src/` tree
- Framework config: `tsconfig.json`, `nest-cli.json`, `vite.config.ts`, `svelte.config.js`, `package.json` dependencies + scripts, eslint config
- `.env.example` per mount — are all required keys present with framework-standard names?
- Test files — do they exercise feature sections, or are they scaffold leftovers?
- README framework sections only. DO NOT review the zerops.yaml integration-guide fragments or the per-env import.yaml comments. Those are platform-owned.

## Framework-expert review checklist

- Does the app work? Controllers, services, modules, DI order, async boundaries, error propagation.
- Dead code, unused deps, missing imports, scaffold leftovers (auto-generated `AppController`/`AppService` if unused).
- XSS: Svelte uses `{expr}` (auto-escaped); no `{@html}` on untrusted data.
- NestJS validation: are `class-validator`/`ValidationPipe` used where appropriate on POST bodies?
- Svelte 5 runes: legacy `let x = ...;` reactive patterns replaced with `$state`, `$derived`, `$effect`?
- Fetch hygiene: every fetch through the `api()`/`apiJson()` helper in `src/lib/api.ts`? Any bare `fetch()`?
- Worker subscription: uses `{ queue: 'workers' }` queue-group? Both `jobs.scaffold` and `jobs.process` subscribed?
- OnModuleDestroy on worker drains subscriptions + connection?

## Silent-swallow antipattern scan (MANDATORY)

- **Init-phase scripts** (`src/migrate.ts`, `src/seed.ts`): any `catch` block whose only action is `console.error` + `return`/`continue` is `[CRITICAL]`. Init scripts must `throw` / `process.exit(1)` on error.
- **Client-side fetch wrappers**: any bare `fetch(...)` without `res.ok` check, any JSON parser without content-type check, any array-consuming store without `[]` default. Panels should only go through `api()`/`apiJson()`.
- **Async-durable writes without await on completion**: Meilisearch `addDocuments(...)` without following `waitForTask(...)` in seed/migrate is `[CRITICAL]`.

## Feature coverage scan (MANDATORY)

Plan features for this recipe (nestjs-showcase):
- `items-crud` — surface [api, ui, db]. HealthCheck `/api/items`. UITestID `items-crud`.
- `cache-demo` — surface [api, ui, cache]. HealthCheck `/api/cache`. UITestID `cache-demo`.
- `storage-upload` — surface [api, ui, storage]. HealthCheck `/api/files`. UITestID `storage-upload`.
- `search-items` — surface [api, ui, search]. HealthCheck `/api/search?q=demo`. UITestID `search-items`.
- `jobs-dispatch` — surface [api, ui, queue, worker]. HealthCheck `/api/jobs`. UITestID `jobs-dispatch`. Worker subject `jobs.process` + queue group `workers`.
- `mail-send` — surface [api, ui, mail]. HealthCheck `/api/mail`. UITestID `mail-send`.

For each feature:
- `surface` includes `api` → grep apidev for controller matching healthCheck path. Missing = `[CRITICAL]`.
- `surface` includes `ui` → grep appdev for `data-feature="{uiTestId}"`. Missing = `[CRITICAL]`.
- `surface` includes `worker` → grep workerdev for subject handler + queue group. Missing = `[CRITICAL]`.

Grep for orphan `data-feature="..."` attributes NOT in the feature list. Any = `[WRONG]`.

## Do NOT call zerops_browser / agent-browser

Browser verification happens in a separate phase run by the main agent. If your review raises a browser-needing question, report it as `[SYMPTOM]` with the exact evidence you'd expect and stop.

## Symptom reporting (NO fixes on platform)

If anything points to a platform-level cause (wrong service URL, missing env var, CORS failure), STOP and report the symptom. Do NOT propose `zerops.yaml`, `import.yaml`, or platform-config changes. Shape: "appstage console shows `Failed to fetch https://...`. Platform root cause unclear — main agent to investigate."

## Out of scope (do NOT review, do NOT propose fixes for)

- `zerops.yaml`, `import.yaml`, `zeropsSetup`, `envReplace`, `buildFromGit`, `cpuMode`, `mode`, `priority`, `verticalAutoscaling`, `minContainers`, `corePackage`, `zsc*`
- Service hostname naming, suffix conventions, env-tier progression
- Env var cross-service references (`${hostname_varname}`)
- Comment ratio / style in platform files

## Report format

For each issue:
- `[CRITICAL]` — breaks the app
- `[WRONG]` — incorrect code but works
- `[STYLE]` — quality improvement
- `[SYMPTOM]` — observed behavior with possible platform cause (no fix proposed)

Include file path + line number + one-sentence summary.

Apply fixes for `[CRITICAL]` and `[WRONG]` directly (Read before Edit). Leave `[STYLE]` as recommendations. Return a bulleted summary with counts.
```
