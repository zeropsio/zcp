# Dispatch 5 — recipe-writer-sub-agent

**Dispatched at**: `2026-04-21T15:24:20`
**Description**: Recipe writer sub-agent
**Subagent type**: `general-purpose`
**Prompt length**: 16842 chars
**Tool-use id**: `toolu_01RF5etCTraSUZhJEFPcmvB6`

---

## Transmitted prompt (verbatim)

```
# Mandatory core — writer sub-agent

You are authoring reader-facing content. Workflow state is held elsewhere; your job is narrow and scoped to this brief.

## Tools

Permitted:

- File ops on the SSHFS mount: `Read`, `Write`, `Edit`, `Grep`, `Glob`. `Write` is your primary tool because most of your output is authored from scratch (per-codebase README, per-codebase CLAUDE.md, per-codebase INTEGRATION-GUIDE.md, per-codebase GOTCHAS.md, per-env README, root README, content manifest).
- `mcp__zerops__zerops_knowledge` — on-demand platform topic lookup. Mandatory when the fact you are writing about matches the Citation Map.
- `mcp__zerops__zerops_logs` — read container logs when verifying a gotcha's observable symptom.
- `mcp__zerops__zerops_discover` — introspect service shape for service-keys tables.
- `mcp__zerops__zerops_record_fact` — record any new fact you discover while reviewing the recipe state that was not already in the facts log.

Forbidden (calling any of these is a sub-agent-misuse bug; workflow state belongs to the step above you):

- `mcp__zerops__zerops_workflow` — no `action=start`, `complete`, `status`, `reset`, `iterate`, `generate-finalize`.
- `mcp__zerops__zerops_import`, `mcp__zerops__zerops_env`, `mcp__zerops__zerops_deploy`, `mcp__zerops__zerops_subdomain`, `mcp__zerops__zerops_mount`, `mcp__zerops__zerops_verify`.
- Bash is reserved for file-local utilities (`cat`, `jq`, `wc`, `grep`, `test`). You rarely need SSH; when you do, it follows the container-side rule.

## File-op sequencing

Most of your output is Write-from-scratch. Use `Write` for every new file you author.

For the files that another phase already put on the mount (for example a zerops.yaml the generate phase authored), you may refresh comments only. Every `Edit` to any file is preceded by exactly one `Read` of that same file in this session.

---

# Fresh-context premise

You have no memory of the run that brought the recipe to this point. Three inputs carry everything you need.

## Input 1 — The facts log

Path: `/tmp/zcp-facts-7743c6d8c8a912fd.jsonl`.

Read with `cat` or `jq`. Each line is a FactRecord JSON object. Fields: `title`, `type`, `codebase`, `substep`, `mechanism`, `failureMode`, `fixApplied`, `evidence`, `scope`, `routeTo`.

`scope=content` or unset → publishable content candidate; `scope=downstream` → skip; `scope=both` → consider for publication.

## Input 2 — Recipe state on disk

Project root: `/var/www`

Codebase mounts:
- `/var/www/appdev/` — Svelte 5 + Vite SPA dashboard (5 feature cards + StatusPanel)
- `/var/www/apidev/` — NestJS 11 + TypeORM + Postgres + Valkey + NATS + S3 + Meilisearch
- `/var/www/workerdev/` — NestJS standalone microservice, NATS subscriber with queue group `workers`

Each has its own `zerops.yaml` (prod + dev setups) already authored at generate time. Do NOT rewrite them unless comment-refresh is needed. Read them to reflect their content in the integration-guide fragments.

Plan summary:
- Recipe slug: `nestjs-showcase`
- Framework: NestJS
- Tier: showcase (dual-runtime API-first, separate-codebase worker)
- Managed services: postgresql@18 (db), valkey@7.2 (cache), nats@2.12 (queue), object-storage (storage), meilisearch@1.20 (search)
- Features: items-crud, cache-demo, storage-upload, search-items, jobs-dispatch — all verified via dev+stage sweep (200 application/json) and end-to-end jobs round-trip (processedAt populated in <500ms).
- Project-level env vars: APP_SECRET (32-char random), DEV_API_URL, DEV_APP_URL, STAGE_API_URL, STAGE_APP_URL.

## Input 3 — Platform topic knowledge

Call `mcp__zerops__zerops_knowledge topic=<id>` when the fact you're writing about matches the Citation Map.

---

# Canonical output tree

The complete list of files you may create or modify. Any path not on this list is out of scope.

## Per-codebase files (hostname list: `appdev`, `apidev`, `workerdev`)

For each `{h}`:

- `/var/www/{h}/README.md` — reader-facing content with three extract fragments (intro, integration-guide, knowledge-base).
- `/var/www/{h}/CLAUDE.md` — repo-local operational guide. Plain markdown. Not published.
- `/var/www/{h}/INTEGRATION-GUIDE.md` — stand-alone integration-guide document.
- `/var/www/{h}/GOTCHAS.md` — stand-alone gotchas document.

## Per-environment files (6 envs for showcase tier)

For env folders:
- `/var/www/environments/dev-and-stage-hypercde/README.md` (env 0: AI agent workspace)
- `/var/www/environments/remote-cde-and-stage/README.md` (env 1: remote CDE)
- `/var/www/environments/local-validator/README.md` (env 2: local dev loop)
- `/var/www/environments/stage-only/README.md` (env 3: stage reviewer)
- `/var/www/environments/small-prod/README.md` (env 4: small prod, minContainers=1)
- `/var/www/environments/prod-ha/README.md` (env 5: HA prod, DEDICATED CPU + corePackage)

If you find evidence on-disk that env-folder names diverge from these guesses, prefer the on-disk names. If the directories don't exist yet, create them with the guesses above.

## Root-level files

- `/var/www/README.md` — one-paragraph recipe summary + deploy-button row per tier.
- `/var/www/ZCP_CONTENT_MANIFEST.json` — classification manifest for every recorded fact.

## Out of scope

Anything else on the mount. You do NOT author or rewrite the `zerops.yaml` files; you may Read them to mirror their content in the integration-guide fragments.

---

# Content surface contracts

Six surfaces. Each has one reader, one purpose, one test, one shape, one length range.

## Surface 1 — Root README (`/var/www/README.md`)

Reader: a dev browsing the recipe page, deciding whether to deploy.
Purpose: name the managed services, list env tiers with deploy buttons, link to recipe category.
Test: *Can a reader decide in 30 seconds whether this recipe deploys what they need, and pick the right tier?*
Length: 20–30 lines.

## Surface 2 — Env README (`/var/www/environments/{env}/README.md`)

Reader: someone picking a tier, or evaluating promotion.
Purpose: teach tier audience, scale profile, and what changes vs adjacent tier.
Test: *Does this teach me when I would outgrow this tier and what the next tier changes?*
Shape: required H2 sections: "Who this is for", "What changes vs the adjacent tier" (or "Entry-level tier"), "Promoting to the next tier" (or "Terminal tier"), "Tier-specific operational concerns".
Length: 40–80 lines.

## Surface 3 — Env `import.yaml` comments

Emitted via `env-comment-set` payload in return. Per-service block of ASCII `#` comments, each 4–10 lines, explaining the decision (why service at this tier, why scale, why mode). Project-level comment per env too.

## Surface 4 — Per-codebase README integration-guide + `INTEGRATION-GUIDE.md`

Reader: a porter bringing their own NestJS/Svelte/NATS worker codebase.
Purpose: enumerate concrete Zerops-forced changes to a porter's own code.
Shape: 3–6 H3 items inside `<!-- #ZEROPS_EXTRACT_START:integration-guide -->` / `<!-- #ZEROPS_EXTRACT_END:integration-guide -->` markers. Item 1 is always "Adding `zerops.yaml`" with the full file content in a fenced yaml block. Items 2+ are platform-forced code changes with action + reason + fenced code block diff.

## Surface 5 — Per-codebase README knowledge-base + `GOTCHAS.md`

Reader: a dev hitting a confusing Zerops failure.
Purpose: surface non-obvious platform traps even after reading Zerops + framework docs.
Shape: 3–6 gotcha bullets inside `<!-- #ZEROPS_EXTRACT_START:knowledge-base -->` / `<!-- #ZEROPS_EXTRACT_END:knowledge-base -->`.
Format: `- **<observable symptom>** — <mechanism>. <1-2 sentence explanation>.`

## Surface 6 — Per-codebase CLAUDE.md

Reader: someone with this specific repo checked out, working on the codebase.
Purpose: operational guide for dev loop + iterating on this repo.
Shape: plain markdown. 4 base H2 sections (Dev Loop, Migrations, Container Traps, Testing) + at least 2 custom sections.
Length: ≥1200 bytes.

## Showcase worker supplements

For `workerdev` specifically, knowledge-base fragment MUST include:
- One gotcha on queue-group semantics (NATS + `queue: 'workers'` + minContainers ≥ 2). Cite the `rolling-deploys` guide.
- One gotcha on SIGTERM graceful shutdown (drain + exit sequence). Cite the `rolling-deploys` guide. Include a fenced code block.

---

# Classification taxonomy

Six classes. Every fact classifies into exactly one before routing.

1. **framework-invariant** — true of Zerops regardless of framework. Default route: `content_gotcha` + citation.
2. **intersection** — framework × platform. Default route: `content_gotcha` + citation naming both sides.
3. **framework-quirk** — no Zerops involvement. Default route: `discarded`. Override requires non-empty `override_reason`.
4. **scaffold-decision** — recipe's own design choice. Sub-route: YAML-level → `zerops_yaml_comment`; code-principle → `content_ig`; operational → `claude_md`.
5. **operational** — how to iterate on THIS specific repo. Default route: `claude_md`.
6. **self-inflicted** — our code had a bug, we fixed it. Default route: `discarded`. Override requires non-empty `override_reason` reframing as porter-facing symptom.

---

# Routing matrix

Writer-emitted `routed_to` values in manifest: `content_gotcha`, `content_intro`, `content_ig`, `content_env_comment`, `claude_md`, `zerops_yaml_comment`, `scaffold_preamble`, `feature_preamble`, `discarded`.

Allowed cells (non-exhaustive):
- framework-invariant → content_gotcha (+ citation), content_intro (paraphrase), content_ig (principle-level), content_env_comment, claude_md, zerops_yaml_comment.
- intersection → content_gotcha (+ citation), content_ig, content_env_comment, claude_md, zerops_yaml_comment.
- framework-quirk → discarded (default). Other cells require `override_reason`.
- scaffold-decision (YAML) → zerops_yaml_comment.
- scaffold-decision (code) → content_ig or zerops_yaml_comment.
- scaffold-decision (operational) → claude_md.
- operational → claude_md.
- self-inflicted → discarded (default). Other cells require `override_reason` reframe.

Each fact appears on exactly ONE surface. Other surfaces cross-reference by prose.

---

# Citation map

Call `mcp__zerops__zerops_knowledge topic=<id>` before writing about these topics. Cite the guide by name in the published content.

| Topic area | Guide ID |
|---|---|
| Cross-service env vars, self-shadow, aliasing | `env-var-model` |
| `zsc execOnce`, `appVersionId`, init commands | `init-commands` |
| Rolling deploys, SIGTERM, HA replicas | `rolling-deploys` |
| Object Storage, MinIO, `forcePathStyle` | `object-storage` |
| L7 balancer, `httpSupport`, VXLAN IP routing | `http-support` |
| Deploy files, tilde suffix, static base | `deploy-files` |
| Readiness check, health check, routing gates | `readiness-health-checks` |

Every gotcha whose topic matches a Citation Map row MUST reference the cited platform topic in its body prose.

---

# Manifest contract

Write `/var/www/ZCP_CONTENT_MANIFEST.json`:

```json
{
  "version": 1,
  "facts": [
    {
      "fact_title": "<exact Title from FactRecord>",
      "classification": "framework-invariant|intersection|framework-quirk|scaffold-decision|operational|self-inflicted",
      "routed_to": "content_gotcha|content_intro|content_ig|content_env_comment|claude_md|zerops_yaml_comment|scaffold_preamble|feature_preamble|discarded",
      "override_reason": ""
    }
  ]
}
```

One entry per distinct `fact_title` in the facts log (scope content, both, or unset). `override_reason` non-empty when routing framework-quirk or self-inflicted somewhere other than `discarded`.

File must be valid JSON (`jq empty` passes). ASCII only.

---

# Self-review

Run these checks before returning. Report aggregate exit code.

```bash
# Manifest
test -f /var/www/ZCP_CONTENT_MANIFEST.json
jq empty /var/www/ZCP_CONTENT_MANIFEST.json
jq '[.facts[] | select(.routed_to == null or .routed_to == "")] | length' /var/www/ZCP_CONTENT_MANIFEST.json | grep -qE '^0$'
jq '[.facts[] | select(.classification == "framework-quirk" or .classification == "self-inflicted") | select(.routed_to != "discarded") | select((.override_reason // "") == "")] | length' /var/www/ZCP_CONTENT_MANIFEST.json | grep -qE '^0$'

# No invented sibling dirs
! find /var/www -maxdepth 2 -type d -name 'recipe-*'
! find /var/www -maxdepth 2 -type d -name '*-output'

# Per-codebase fragment markers present
for h in appdev apidev workerdev; do
  grep -q '#ZEROPS_EXTRACT_START:intro'             /var/www/$h/README.md &&
  grep -q '#ZEROPS_EXTRACT_START:integration-guide' /var/www/$h/README.md &&
  grep -q '#ZEROPS_EXTRACT_START:knowledge-base'    /var/www/$h/README.md || { echo "missing markers in $h"; exit 1; }
done

# CLAUDE.md byte floor
for h in appdev apidev workerdev; do
  test $(wc -c < /var/www/$h/CLAUDE.md) -ge 1200 || { echo "$h CLAUDE.md too short"; exit 1; }
done

# Root README length
test $(wc -l < /var/www/README.md) -ge 20
test $(wc -l < /var/www/README.md) -le 30
```

---

# Completion shape

Return these sections (under ~600 words):

1. **Files written** — one line per authored file with byte count.
2. **Manifest summary** — total entry count, per-classification totals, per-routed_to totals.
3. **`env-comment-set` JSON payload** — per env tier, per service, comment text. Shape:
   ```json
   {
     "environments": {
       "dev-and-stage-hypercde": {
         "project": "<project-level comment text>",
         "services": { "appdev": "<service comment text>", ... }
       },
       ...
     }
   }
   ```
4. **Discarded facts** — list titles with one-line reason each.
5. **Pre-attest aggregate exit code** — from the self-review checks.

---

# Additional context for this specific recipe

Facts log path: `/tmp/zcp-facts-7743c6d8c8a912fd.jsonl`

Service keys (for env-comment-set payload):
- **Envs 0–1** (dev-and-stage-hypercde, remote-cde-and-stage): `appdev`, `appstage`, `apidev`, `apistage`, `workerdev`, `workerstage`, `db`, `cache`, `queue`, `storage`, `search`
- **Envs 2–5** (local-validator, stage-only, small-prod, prod-ha): `app`, `api`, `worker`, `db`, `cache`, `queue`, `storage`, `search`

Key recipe-specific facts to weave into content:
- NATS subject `jobs.process` with queue group `workers` — publisher on apidev, consumer on workerdev. Multi-replica workers use the queue group to compete for messages instead of fanout.
- NATS v2 client silently drops URL-embedded creds — use structured `{ servers, user, pass }` ConnectionOptions.
- S3Client with `endpoint: process.env.storage_apiUrl` (HTTPS) and `forcePathStyle: true` — required for MinIO-backed Object Storage.
- Code reads `process.env.db_hostname`, `process.env.queue_user` directly — zerops-native lowercase names, no rename layer.
- Declaring `db_hostname: ${db_hostname}` in `run.envVariables` creates a literal-string self-shadow — project-wide cross-service refs already auto-inject as OS env vars. Only declare mode flags (NODE_ENV, PORT literal) and renames (VITE_API_URL: ${STAGE_API_URL}).
- `zsc execOnce ${appVersionId}` bare key shared across two commands makes the second command silently no-op — use distinct keys like `${appVersionId}-migrate` and `${appVersionId}-seed`.
- Meilisearch v0.57 moved `waitForTask` to `client.tasks.waitForTask(taskUid)` — not `client.waitForTask()`. (scope=downstream, discard or claude_md.)
- Vite dev server requires `server.allowedHosts: ['.zerops.app']` or the zerops subdomain returns "Blocked request / Invalid Host header".
- `deployFiles: ./dist/~` with tilde strips the dist/ prefix so Nginx serves dist/index.html as root.
- NestJS `app.enableShutdownHooks()` + `await app.listen(port, '0.0.0.0')` + `trust proxy` on the Express adapter.

Feature list (all 5 verified green):
- items-crud (api, ui, db) — GET/POST /api/items, DELETE /api/items/:id
- cache-demo (api, ui, cache) — GET/POST /api/cache — GET default key='demo' when no query
- storage-upload (api, ui, storage) — GET/POST /api/files (multipart)
- search-items (api, ui, search) — GET /api/search?q=…
- jobs-dispatch (api, ui, queue, worker) — POST /api/jobs (202 + NATS publish) → workerdev consumer UPDATE → GET /api/jobs/:id → processedAt populated <500ms

URL templates (envs 0-1 dev+stage pair):
- Dev: `https://appdev-${zeropsSubdomainHost}-5173.prg1.zerops.app` (Vite), `https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app`
- Stage: `https://appstage-${zeropsSubdomainHost}.prg1.zerops.app` (static, no port), `https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app`

Envs 2-5 single-slot:
- `https://app-${zeropsSubdomainHost}.prg1.zerops.app`, `https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app`

Begin with reading the facts log (`cat /tmp/zcp-facts-7743c6d8c8a912fd.jsonl`). Classify each distinct title, then author every surface. Keep all published content ASCII-only (use `--` for em-dashes; no Unicode box-drawing, no emoji, no smart quotes).
```
