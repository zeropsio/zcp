# Dispatch 5 — author-recipe-readmes-claude-md-manifest

**Dispatched at**: `2026-04-21T20:36:55`
**Description**: Author recipe READMEs + CLAUDE.md + manifest
**Subagent type**: `general-purpose`
**Prompt length**: 14366 chars
**Tool-use id**: `toolu_01Y9YeoVsA67tmS86pK3XZiC`

---

## Transmitted prompt (verbatim)

```
# Mandatory core — writer sub-agent

You are authoring reader-facing content for the Zerops nestjs-showcase recipe. Workflow state is held elsewhere; your job is narrow and scoped to this brief.

## Tools

Permitted:
- File ops on the SSHFS mount: `Read`, `Write`, `Edit`, `Grep`, `Glob`. Write is primary.
- `mcp__zerops__zerops_knowledge` — on-demand platform topic lookup. Mandatory when the fact you are writing about matches the Citation Map.
- `mcp__zerops__zerops_logs` — for verifying a gotcha's observable symptom.
- `mcp__zerops__zerops_discover` — service shape.
- `mcp__zerops__zerops_record_fact` — for any new fact surfaced while reviewing state.

Forbidden (SUBAGENT_MISUSE): zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify.

Bash is reserved for file-local utilities (cat, jq, wc, grep, test). If you need SSH, it's `ssh {hostname} "..."`.

## File-op sequencing

Most output is Write-from-scratch. Every Edit must be preceded by one Read of the same file in this session. Batch-Read before any Edit.

---

## Fresh-context premise

You have no memory of the run. Inputs:

- **Facts log**: `/tmp/zcp-facts-9c9cce67e644ae35.jsonl` (9 facts recorded across deploy substeps)
- **Project root**: `/var/www` — mount root containing `/var/www/appdev`, `/var/www/apidev`, `/var/www/workerdev` codebase mounts, plus `/var/www/environments/` for env-tier READMEs and `/var/www/README.md` for the root-level recipe README.
- **Plan summary**: Recipe slug `nestjs-showcase`, tier `showcase`, framework `nestjs`, runtime `nodejs@22`. API-first dual-runtime + separate-codebase worker. Three codebases: `appdev` (Svelte 5 + Vite 8 SPA), `apidev` (NestJS 11 API), `workerdev` (NestJS 11 NATS microservice). Managed services: postgresql@18 (db), valkey@7.2 (redis), nats@2.12 (queue), object-storage (storage), meilisearch@1.20 (search). Features: items-crud (db), cache-demo (cache), search-items (search), storage-upload (storage), jobs-dispatch (queue + worker).
- **Platform topic knowledge**: via `mcp__zerops__zerops_knowledge`.

---

## Canonical output tree

Paths you may create or modify. Anything else is out of scope.

### Per-codebase (one set each for appdev, apidev, workerdev)

- `/var/www/{hostname}/README.md` — three extract fragments (intro, integration-guide, knowledge-base). Uses `#ZEROPS_EXTRACT_START:intro` and `#ZEROPS_EXTRACT_END:intro` markers (same pattern for `integration-guide` and `knowledge-base`).
- `/var/www/{hostname}/CLAUDE.md` — repo-local operational guide. Plain markdown, no fragments, not published.
- `/var/www/{hostname}/INTEGRATION-GUIDE.md` — stand-alone IG document mirroring the README integration-guide fragment's H3 items.
- `/var/www/{hostname}/GOTCHAS.md` — stand-alone gotchas document mirroring the README knowledge-base fragment.

### Per-environment (6 env tiers — showcase)

Use these env folder names:
0. `ai-agent` (AI-agent workspace hosted on Zerops ZCP)
1. `remote-dev` (remote Code-Dev Environment / VS Code tunnel)
2. `local-dev` (engineer laptop using zcli VPN)
3. `stage` (always-on QA / reviewer tier)
4. `prod` (single-replica production with minContainers:1)
5. `prod-ha` (HA production with DEDICATED CPU + minContainers:2+)

- `/var/www/environments/{name}/README.md` — 40-80 line teaching content per tier: audience, scale, what changes vs adjacent tier, operational concerns.

### Root

- `/var/www/README.md` — 20-30 line recipe summary + deploy-button rows for each env tier + link to recipe category.
- `/var/www/ZCP_CONTENT_MANIFEST.json` — classification manifest.

### NOT authored by you

- `zerops.yaml` comments (already authored at generate).
- `environments/{name}/import.yaml` comments (you emit the `env-comment-set` payload in your return; you do NOT write the files).

---

## Content surface contracts (summarized)

Six surfaces. Each fact lives on EXACTLY ONE. Cross-surface references are allowed in prose; cross-surface re-authoring is not.

1. **Root README** — recipe-page browser decides in 30s whether to deploy + picks tier. 20-30 lines. Deploy-button row per env.
2. **Env README** — tier chooser / promotion evaluator. 40-80 lines. Sections: Who this is for / Scale / What changes vs adjacent tier / Operational concerns.
3. **Env import.yaml comments (emitted payload)** — dashboard manifest reader. 4-10 `#` comment lines per service block. Each block explains decision (why this service at this tier, why this scale, why this mode) not field-narration.
4. **Per-codebase README integration-guide + INTEGRATION-GUIDE.md** — porter with their own app. 3-6 H3 items. Item 1 always "Adding `zerops.yaml`" with full commented YAML read from the mount. Items 2+: one platform-forced change each (routable bind, trust-proxy, init commands/execOnce, forcePathStyle, NATS structured-creds, allowedHosts bundler dev-server, worker SIGTERM drain). Each H3: action + reason tied to Zerops mechanism + fenced code block (minimal diff).
5. **Per-codebase README knowledge-base + GOTCHAS.md** — dev hitting platform failure. 3-6 gotcha bullets. Each bullet: `- **<concrete observable symptom>** — <mechanism>. <evidence or 1-2 sentences>.` Stem names HTTP status, quoted error string, measurable wrong state. Body names the platform mechanism and cites Citation Map topic when applicable.
6. **Per-codebase CLAUDE.md** — repo operator. Plain markdown. Min 1200 bytes. 4 template sections (Dev Loop, Migrations, Container Traps, Testing) + at least 2 custom.

---

## Showcase supplements

The `workerdev` codebase's README knowledge-base MUST contain:
- One gotcha on NATS **queue-group** semantics under multi-replica deployment (stem: broker name + "queue group" + "minContainers" or "per replica" or "exactly once"; body: shows client-library option; cites `rolling-deploys`).
- One gotcha on **SIGTERM graceful shutdown** with in-flight-drain (stem: SIGTERM/"drain"/"graceful shutdown"; body: fenced code block showing catch SIGTERM → drain → exit; cites `rolling-deploys`).

---

## Classification taxonomy

Six classes. Every fact classifies into EXACTLY ONE before routing.

1. **framework-invariant** — true of Zerops regardless of framework. Default route: `content_gotcha`, with citation.
2. **intersection** (framework × platform) — framework+platform combo produces the failure. Default route: `content_gotcha`, with citation.
3. **framework-quirk** — pure framework, no Zerops involvement. Default route: `discarded`. Override requires `override_reason`.
4. **scaffold-decision** — recipe's own design choice. Split by sub-kind:
   - Config choice in YAML → `zerops_yaml_comment`
   - Code-level principle → `content_ig`
   - Operational choice → `claude_md`
5. **operational** — how to iterate on this specific repo. Default route: `claude_md`.
6. **self-inflicted** — recipe bug we fixed, porter would not hit. Default route: `discarded`. Override requires reframing.

---

## Routing matrix (enum)

`content_gotcha`, `content_intro`, `content_ig`, `content_env_comment`, `claude_md`, `zerops_yaml_comment`, `scaffold_preamble`, `feature_preamble`, `discarded`.

Cells: default-discarded classifications (framework-quirk, self-inflicted) routed anywhere except `discarded` require non-empty `override_reason`. Framework-invariant and intersection classifications route freely to content surfaces with citation requirement honored.

Single-routing rule: every fact has exactly one `routed_to` value. Other surfaces cross-reference in prose; they do not re-author.

---

## Citation map (when to call `zerops_knowledge topic=<id>`)

| Topic area | Guide ID |
|---|---|
| Cross-service env vars, self-shadow, aliasing | `env-var-model` |
| `zsc execOnce`, `appVersionId`, init commands | `init-commands` |
| Rolling deploys, SIGTERM, HA replicas | `rolling-deploys` |
| Object Storage, MinIO, `forcePathStyle` | `object-storage` |
| L7 balancer, `httpSupport`, VXLAN IP routing | `http-support` |
| Deploy files, tilde suffix, static base | `deploy-files` |
| Readiness check, health check, routing gates | `readiness-health-checks` |

Every gotcha whose topic matches a Citation Map row MUST reference the cited topic in the body. Call `mcp__zerops__zerops_knowledge topic=<id>` BEFORE writing, align framing with the guide, cite by name.

Missing-guide disposition: if `knowledge` returns "no matching topic", record the gap in your completion return, write without citation but keep framing neutral and evidence-based.

---

## Manifest contract

Write `/var/www/ZCP_CONTENT_MANIFEST.json`:

```json
{
  "version": 1,
  "facts": [
    {
      "fact_title": "<exact FactRecord.Title>",
      "classification": "framework-invariant|intersection|framework-quirk|scaffold-decision|operational|self-inflicted",
      "routed_to": "content_gotcha|content_intro|content_ig|content_env_comment|claude_md|zerops_yaml_comment|scaffold_preamble|feature_preamble|discarded",
      "override_reason": ""
    }
  ]
}
```

One entry per distinct fact. For every FactRecord.Title in the facts log where `scope` is `content` | `both` | unset, emit exactly one manifest entry. Skip `scope=downstream`.

Honor the recorded `routeTo` when present; override with reason documented.

Default-discarded consistency: framework-quirk + self-inflicted that route anywhere except `discarded` need non-empty `override_reason`.

File is valid JSON parseable by `jq empty`. ASCII-only.

---

## Self-review per surface (pre-return checks)

Run these locally before returning. Exit 0 in aggregate = green:

```bash
# Manifest parses
test -f /var/www/ZCP_CONTENT_MANIFEST.json
jq empty /var/www/ZCP_CONTENT_MANIFEST.json

# Every fact has non-empty routed_to
jq '[.facts[] | select(.routed_to == null or .routed_to == "")] | length' /var/www/ZCP_CONTENT_MANIFEST.json | grep -qE '^0$'

# Default-discard overrides have non-empty reason
jq '[.facts[] | select(.classification == "framework-quirk" or .classification == "self-inflicted") | select(.routed_to != "discarded") | select((.override_reason // "") == "")] | length' /var/www/ZCP_CONTENT_MANIFEST.json | grep -qE '^0$'

# Per-codebase fragments present
for h in appdev apidev workerdev; do
  grep -q '#ZEROPS_EXTRACT_START:intro'             /var/www/$h/README.md &&
  grep -q '#ZEROPS_EXTRACT_START:integration-guide' /var/www/$h/README.md &&
  grep -q '#ZEROPS_EXTRACT_START:knowledge-base'    /var/www/$h/README.md || exit 1
done

# CLAUDE.md byte floor
for h in appdev apidev workerdev; do
  test $(wc -c < /var/www/$h/CLAUDE.md) -ge 1200 || exit 1
done

# Root README line count
test $(wc -l < /var/www/README.md) -ge 20 && test $(wc -l < /var/www/README.md) -le 30

# Env READMEs line count
for e in ai-agent remote-dev local-dev stage prod prod-ha; do
  L=$(wc -l < /var/www/environments/$e/README.md)
  test $L -ge 40 && test $L -le 80 || { echo "env $e: $L lines (need 40-80)"; exit 1; }
done
```

---

## Completion shape

Return:

1. **Files written** — one line per authored file with byte count.
2. **Manifest summary** — total entries, per-classification totals, per-routed_to totals.
3. **env-comment-set JSON payload** — per env folder, per service block, comment text:
   ```json
   {
     "environments": {
       "ai-agent": {
         "project": "...",
         "services": {"db": "...", "redis": "...", "queue": "...", "storage": "...", "search": "...", "app": "...", "api": "...", "worker": "..."}
       },
       "remote-dev": {...},
       ...
     }
   }
   ```
   For env 0 (ai-agent) + env 1 (remote-dev) [dev+stage paired envs], service keys include BOTH `appdev`+`appstage`, `apidev`+`apistage`, `workerdev`+`workerstage`. For envs 2-5 (single-slot), use base hostnames `app`, `api`, `worker`.
4. **Discarded facts with reasoning** — list + one-line reason each.
5. **Pre-attest aggregate exit code** — 0 = green.

---

## Principles

### file-op-sequencing
Read-before-Edit. Batch-Read N files before first Edit.

### tool-use-policy
Read / Write / Edit / Grep / Glob for mount. Bash for local utilities. `zerops_knowledge` / `zerops_logs` / `zerops_discover` / `zerops_record_fact` for MCP.

### fact-recording-discipline
Record at freshest knowledge when you observe something non-obvious during content authoring.

### comment-style
Imports/import.yaml: ASCII `#`, one hash + one space + prose. 4-10 line blocks per service. No Unicode box-drawing, no decorative dividers, no emoji.

### visual-style
ASCII only. `--` for em-dashes (two hyphens). ASCII diagrams with `+`, `-`, `|`.

---

## Facts log — inputs

9 facts in `/tmp/zcp-facts-9c9cce67e644ae35.jsonl`. Read it first via `cat /tmp/zcp-facts-9c9cce67e644ae35.jsonl | jq -s '.[] | {title, type, codebase, substep, scope, routeTo: .routeTo // "unset"}'` to see the list, then classify + route each.

Known highlights (use the log as source of truth):
1. `ts-node + NodeNext .js imports: CJS cannot remap .js to .ts source` (scope:both) — scaffold-decision code-principle OR framework-quirk (depending on framing). Route to `content_ig` with override_reason (reframe as "NodeNext + ts-node dev-loop: use compiled dist/ in initCommands").
2. `items-crud DTO: ItemDto shape shared between apidev + appdev` — cross_codebase_contract, relevant to CLAUDE.md or discarded as scaffold-internal.
3. Several verified_behavior facts per feature — likely discarded or claude_md.
4. `cross-codebase NATS subject+queue+payload contract` — scaffold_preamble or content_ig (worker queue group).
5. Worker `JobEntity column mapping gotcha` — scaffold-decision; route to claude_md (workerdev CLAUDE.md).
6. `appdev api.ts helper extensions` — scaffold-decision code-principle; content_ig.
7. `pg@8 concurrent-query deprecation` — framework-quirk (pg internal); discarded (or claude_md with reframe).

Start by reading the whole log.

## Dispatch kickoff

Begin with:
1. `cat /tmp/zcp-facts-9c9cce67e644ae35.jsonl | jq -s '.'` — see all 9 facts.
2. `zerops_knowledge` consultations for each Citation Map topic you'll cite (env-var-model, init-commands, rolling-deploys, object-storage, http-support, deploy-files, readiness-health-checks).
3. Draft the manifest classification decisions in your head.
4. Author files in this order: three codebase READMEs → three CLAUDE.md → three INTEGRATION-GUIDE.md → three GOTCHAS.md → six env READMEs → root README → manifest.
5. Run the pre-return checks.

Good luck.
```
