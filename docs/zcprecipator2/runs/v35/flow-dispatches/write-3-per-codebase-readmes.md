# Dispatch 5 — write-3-per-codebase-readmes

**Dispatched at**: `2026-04-21T08:16:21`
**Description**: Write 3 per-codebase READMEs
**Subagent type**: `general-purpose`
**Prompt length**: 6268 chars
**Tool-use id**: `toolu_01MmELEXm9dwqss7xHhDcU5H`

---

## Transmitted prompt (verbatim)

```
You are the README-writer sub-agent for the **nestjs-showcase** Zerops recipe (tier: showcase, API-first dual-runtime + separate-codebase worker). Three per-codebase README.md files need to be written — one in each mount — and each carries all three extract fragments (intro, integration-guide, knowledge-base).

## Inputs you MUST read first

1. **Full brief with requirements, schema, and every structural rule**: `/home/zerops/.claude/projects/-var-www/b89ede1d-77d7-4a0c-a1f0-c4e4f5a8e667/tool-results/mcp-zerops-zerops_workflow-1776759321598.txt` — this is ~71KB on a single line. Read it in 80,000-char slices via `python3 -c "print(open('…').read()[A:B])"`. The `detailedGuide` field inside the JSON contains the readmes sub-step brief; that is what you must follow byte-for-byte for extract fragment markers, content discipline, ASCII-only constraint, gotcha format, integration-guide shape.

2. **Facts log**: `/tmp/zcp-facts-8324884b199361d9.jsonl` — every fact the agents recorded during this session, keyed by scope. Only facts with `scope: content` or `scope: both` and `type: gotcha_candidate | ig_item_candidate | verified_behavior | platform_observation | cross_codebase_contract` feed published content. Facts with `scope: downstream` are NOT published; facts with `type: fix_applied` or `routeTo: discarded` are NOT published.

3. **Per-codebase current state** (read these before drafting):
   - `/var/www/apidev/` — NestJS 11 API. Read: `zerops.yaml`, `package.json`, `src/main.ts`, `src/app.module.ts`, `src/status.controller.ts`, `src/items.controller.ts`, `src/cache.controller.ts`, `src/files.controller.ts`, `src/search.controller.ts`, `src/jobs.controller.ts`, `src/s3.service.ts`, `src/search.service.ts`, `src/migrate.ts`, `src/seed.ts`.
   - `/var/www/appdev/` — Svelte 5 + Vite 8 SPA. Read: `zerops.yaml`, `package.json`, `vite.config.ts`, `src/App.svelte`, `src/lib/api.ts`, `src/lib/StatusPanel.svelte`, `src/lib/ItemsCrud.svelte`, `src/lib/CacheDemo.svelte`, `src/lib/StorageUpload.svelte`, `src/lib/SearchItems.svelte`, `src/lib/JobsDispatch.svelte`.
   - `/var/www/workerdev/` — NestJS standalone microservice. Read: `zerops.yaml`, `package.json`, `src/main.ts`, `src/worker.module.ts`, `src/jobs.controller.ts`, `src/entities/job.entity.ts`.

## Context about this recipe

- **Framework**: NestJS 11 (API + worker) + Svelte 5 / Vite 8 (frontend SPA).
- **Slug**: nestjs-showcase. Recipe presents a live dashboard with 5 feature sections (items-crud, cache-demo, search-items, storage-upload, jobs-dispatch) plus a status panel.
- **Managed services**: postgresql@18 (db), valkey@7.2 (redis/cache, no auth), nats@2.12 (queue), object-storage (S3-compatible MinIO), meilisearch@1.20 (search).
- **Project env vars**: APP_SECRET, DEV_APP_URL, DEV_API_URL, STAGE_APP_URL, STAGE_API_URL.
- **Live dev URLs**: https://appdev-21c2-5173.prg1.zerops.app, https://apidev-21c2-3000.prg1.zerops.app.
- **Live stage URLs**: https://appstage-21c2.prg1.zerops.app, https://apistage-21c2-3000.prg1.zerops.app.
- **NATS contract**: subject `jobs.dispatch`, queue group `nestjs-showcase-workers`.
- **Dual-runtime URL baking**: vite build reads VITE_API_URL from `build.envVariables` (in `appdev` zerops.yaml `setup: prod`). Dev reads VITE_API_URL from `run.envVariables`. Source code side: `src/lib/api.ts` reads `import.meta.env.VITE_API_URL` and throws on non-JSON content-type (SPA fallback detector).

## Your deliverables

Write one `README.md` per mount:
- `/var/www/apidev/README.md`
- `/var/www/appdev/README.md`
- `/var/www/workerdev/README.md`

Each file contains the three fragments the brief's schema defines (typically intro, integration-guide, knowledge-base — confirm from brief), each bounded by the exact marker strings the brief specifies. Integration-guide fragment embeds the codebase's own `zerops.yaml` verbatim (with the same comments already on disk). Knowledge-base fragment narrates the gotchas the facts log recorded that are relevant to THAT codebase's role — do not cross-post apidev's gotchas to appdev's README.

## Discipline rules

- **ASCII only** — no smart quotes, no em-dashes (write `--`), no Unicode whitespace. The brief lists the exact character surface.
- **Derive from facts, not speculation** — if a gotcha isn't in the facts log, it doesn't get published. You may re-state platform invariants (trust proxy, 0.0.0.0 bind, forcePathStyle, NATS creds, static tilde) since those are broad platform facts; framework-specific quirks must trace back to a recorded fact.
- **Integration guide is WHAT A USER DOES** — steps they'd take when porting their own NestJS API to Zerops, not a retelling of the recipe build.
- **No README content that retells the build log** — narrate gotchas as porter-facing traps, not as "we discovered X then fixed Y."
- **Don't invent env vars** — use only what exists in the codebase's zerops.yaml or status.

## After writing

For each mount, git-commit the new README via `ssh <hostname> "cd /var/www && git add README.md && git commit -q -m 'docs: recipe README'"`.

## Return report

- Files written + byte counts
- Which facts you consumed (titles), which you discarded and why
- Any discrepancy between brief expectations and reality (e.g. "brief wants N fragments; I wrote N but noted X")

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

```
