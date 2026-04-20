# Recipe Version Log

Running record of every tracked `nestjs-showcase` build from v6 through v16 (and onward). Each version is an end-to-end recipe run the zcp workflow produced; comparing them head-to-head is how we diagnose regressions, validate fixes, and decide which knobs to turn next.

The `nestjs-showcase` recipe is our canonical "hard run" — it exercises 3 separate codebases, 5 managed services, dual-runtime URL wiring, a worker subagent, and the full 6-environment tier ladder. Every sub-feature of the workflow either shows up here or doesn't ship.

- [Why we log versions](#why-we-log-versions)
- [How to explore a version](#how-to-explore-a-version)
- [How to analyze a session](#how-to-analyze-a-session)
- [How to evaluate content quality](#how-to-evaluate-content-quality)
- [Rating methodology](#rating-methodology)
- [Cross-version summary](#cross-version-summary)
- [Architectural insights — v20 → v23 trajectory and v8.86's shape](#architectural-insights--the-v20--v23-trajectory-and-v886s-shape) — start here if you're a new instance primed on this doc
- [Per-version log](#per-version-log) — v6 through v23
- [Adding a new version](#adding-a-new-version)

---

## Why we log versions

1. **Regression detection.** When a version ships tighter session wall time but shallower gotchas, both facts belong in the same record so the trade-off is visible. A table with just "pass/fail" misses the shape of the change.
2. **Trend tracking.** Some metrics drift gradually (README length compression from v10 onward) and some change in jumps (authenticity check added at v13). Seeing the whole ladder makes both legible.
3. **Fix validation.** When a code change lands ("v8.67.0 dedup checks"), the next run either confirms the fix held or surfaces the next thing. Without the baseline numbers, "it got better" is anecdotal.
4. **Institutional memory.** Compaction erases prior runs from agent context. The log is the durable artifact — future-you reading this after a compaction can pick up the state of the world in five minutes instead of re-auditing everything.

The log is **additive**: every new version appends an entry. Older entries only get amended when analysis tooling improves and we want to backfill a metric we now collect.

---

## How to explore a version

Every recipe run lands in `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v{N}/` with a consistent shape:

```
nestjs-showcase-v{N}/
├── README.md                     # root recipe README (published to zerops.io/recipes)
├── TIMELINE.md                   # per-step narrative, agent-authored during the run
├── SESSIONS_LOGS/                # (v8+) raw Claude Code stream-json logs
│   ├── main-session.jsonl        # primary agent session (or nestjs-showcase-session.jsonl for v8)
│   └── subagents/                # per-subagent session files
│       ├── agent-{id}.jsonl
│       └── agent-{id}.meta.json
├── environments/                 # 6 env tier folders (published)
│   ├── 0 — AI Agent/
│   ├── 1 — Remote (CDE)/
│   ├── 2 — Local/
│   ├── 3 — Stage/
│   ├── 4 — Small Production/
│   └── 5 — Highly-available Production/
│       ├── import.yaml
│       └── README.md
├── apidev/                       # NestJS API codebase
│   ├── README.md                 # per-codebase README (published extract)
│   ├── CLAUDE.md                 # (v16+) repo-local dev-loop guide
│   ├── zerops.yaml
│   └── src/
├── appdev/                       # Svelte SPA frontend
└── workerdev/                    # (v6+) separate-codebase NATS worker
```

### Starting points

1. **`TIMELINE.md`** — always read this first. The agent narrates each of the 6 workflow steps (research → provision → generate → deploy → finalize → close), records key decisions, and usually lists the close-step code-review findings at the bottom. 80% of what you need is here.
2. **Per-codebase `README.md`** — the published content. Integration guide + gotcha bullets under fragment markers. Reading this side-by-side with a prior version shows whether content regressed or improved.
3. **Per-codebase `CLAUDE.md`** (v16+) — repo-local operations guide (dev-loop, migrations, container traps, testing). Separate from README per the v8.67.0 audience split.
4. **`environments/{tier}/import.yaml`** — the published env manifests. Look at the comments: do they teach WHY or just narrate WHAT? Compare against v7's gold-standard comments.

### Shell one-liners

Count gotchas and integration-guide items per codebase:

```bash
awk '/#ZEROPS_EXTRACT_START:knowledge-base/{f=1;next} /#ZEROPS_EXTRACT_END:knowledge-base/{f=0} f && /^- \*\*/' {codebase}/README.md
awk '/#ZEROPS_EXTRACT_START:integration-guide/{f=1;next} /#ZEROPS_EXTRACT_END:integration-guide/{f=0} f && /^### [0-9]/' {codebase}/README.md
```

Dump the content-metrics table across all versions in one go:

```bash
/Users/fxck/www/zcp/eval/scripts/version_metrics.sh
```

---

## How to analyze a session

For v8 onwards, every run captures raw Claude Code stream-json logs under `SESSIONS_LOGS/`. These are the ground-truth record of what tools the agent actually called, with timestamps and outputs — everything else in the run is a downstream summary.

### The timeline script (start here)

`eval/scripts/timeline.py` is the canonical cross-stream chronological analyzer — read this first for any new run. It pairs every `tool_use` with its `tool_result` by id across main-session + every subagent session under `SESSIONS_LOGS/`, annotates each line with source (`MAIN` / `SUB-{agent-id-prefix}`), computes per-call latency, and surfaces `is_error` flags. Phase auto-detection via `zerops_workflow` action inspection prints section headers (START / RESEARCH / PROVISION / GENERATE / DEPLOY / FINALIZE / CLOSE).

```bash
# Full timeline with phase headers, piped to less
python3 /Users/fxck/www/zcp/eval/scripts/timeline.py \
  /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v{N} --phase | less

# Post-mortem slice (close step of v27, no text blocks)
python3 /Users/fxck/www/zcp/eval/scripts/timeline.py \
  /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v27 \
  --after 2026-04-18T10:14:00 --before 2026-04-18T10:22:00 --no-text --phase

# One-line cost summary for the version-log entry
python3 /Users/fxck/www/zcp/eval/scripts/timeline.py \
  /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v{N} --no-text --stats
```

Flags: `--after` / `--before` (ISO timestamps), `--source MAIN` / `--source SUB-aXX` (filter repeatable), `--tool deploy` (substring match, repeatable), `--no-text` (suppress assistant text blocks, keeps only tool calls), `--json` (emit JSON array instead of human-readable), `--stats` (append per-tool histogram with total latency, errored count, per-source wall clock).

Two patterns timeline.py surfaces that the bash latency analyzer doesn't:
- **Main-context authoring cost** — summed `Write` / `Edit` latency reveals how much the model typed through main instead of delegating (v27: 44 Writes / 1919s = 32 min of scaffold-in-main).
- **Delegation shape** — `--source SUB-*` wall clocks contrasted against MAIN's wall show what was delegated vs absorbed (v27: 14 min feature + 2 min review subagents out of 100 min total = 83 min of main-absorbed work).

### The bash latency analyzer

`eval/scripts/analyze_bash_latency.py` is the deep-dive bash analyzer. Run it when timeline.py shows high `Bash` or `mcp__zerops__zerops_dev_server` latency and you need the per-bucket breakdown. It reads a stream-json file, pairs every `Bash` tool invocation with its result, and reports:

- Total bash calls, total bash time, long (>10s) and very-long (>60s) counts, interrupted / errored
- Breakdown by pattern: SSH calls, dev-server starts, port kills, sleeps, curls (with per-bucket sum duration)
- Failure-signature hits in stdout/stderr: `fork failed`, `EADDRINUSE`, `ECONNREFUSED`, `timeout`, `killed`, etc.
- Top 20 longest bash calls, printed with duration + flags (BG / INT / ERR)
- Multi-host SSH patterns (commands containing ≥2 distinct `ssh HOST` invocations)

Run it:

```bash
python3 /Users/fxck/www/zcp/eval/scripts/analyze_bash_latency.py \
  /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v16/SESSIONS_LOGS/main-session.jsonl
```

Run it on every subagent too — the feature subagent is usually where hidden cost lives:

```bash
for f in /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v16/SESSIONS_LOGS/subagents/*.jsonl; do
  echo "=== $(basename $f) ==="
  python3 /Users/fxck/www/zcp/eval/scripts/analyze_bash_latency.py "$f" | head -12
done
```

### What to look for

| Signal | What it tells you |
|---|---|
| **Very long (>60s) bash calls** | Usually dev-server starts that hit the 120s SSH-channel-hold bug. Zero is the goal. v11/v13/v15 had 4–6 each; v16 main-session had zero but v16 feature subagent still had two. |
| **Errored bash calls** | Retry cost. v13 and v14 hit 17–18 each because of the SSH pkill+fuser `&&` chains failing on "nothing to kill". v16 main reduced to 9. |
| **Dev-server starts sum duration** | The single biggest operational cost driver. v11: 541s, v15: 556s. Target is `zerops_dev_server`-based flows which cap at `waitSeconds` per call (default 15). |
| **Port/process kill count** | Every kill is downstream of a prior orphaned dev-server. High kill count means the start pattern is leaking processes. |
| **Multi-host SSH patterns** | Look for `ssh a && ssh b && ssh c` chains. When they error, the `&&` aborts mid-chain and the agent spends 2–3 retries figuring out why. Almost always pkill or fuser chains. |

### The raw session log structure

Each line is one event. Event types to care about:

- `{"type":"assistant", ...}` — an assistant turn. The `message.content` array contains `text`, `tool_use`, or `thinking` blocks.
- `{"type":"user", ...}` — a user turn. Tool results arrive here as `tool_result` blocks inside `message.content`, keyed to the originating `tool_use_id`.
- `{"type":"queue-operation", ...}` — internal queue state; ignore for analysis.

For a Bash call, the pair looks like:

```
assistant  message.content[]  → { type: "tool_use", name: "Bash", id: "toolu_01...", input: { command, description, timeout } }
user       message.content[]  → { type: "tool_result", tool_use_id: "toolu_01...", content: "..." }
           toolUseResult      → { stdout, stderr, interrupted, isImage, noOutputExpected }
```

Latency is `user.timestamp - assistant.timestamp` for the matching `tool_use_id`. The analyzer script does this automatically; for ad-hoc inspection use `jq`:

```bash
jq 'select(.type == "assistant") | .message.content[] | select(.type == "tool_use" and .name == "Bash") | .input.command' \
  SESSIONS_LOGS/main-session.jsonl | head -30
```

### Session wall clock vs active time

"Wall clock" = first to last assistant event timestamp. It includes user-think gaps. For the recipe workflow the agent is autonomous so wall ≈ active, but subagent runs can stall the parent while they work.

```bash
first=$(grep -m1 '"type":"assistant"' main-session.jsonl | grep -o '"timestamp":"[^"]*"' | head -1)
last=$(grep '"type":"assistant"' main-session.jsonl | tail -1 | grep -o '"timestamp":"[^"]*"' | tail -1)
echo "first=$first last=$last"
```

### Tool call mix

The tool mix tells you which subsystems the agent leaned on:

```bash
grep -oE '"name":"[A-Za-z_][A-Za-z_]*"' main-session.jsonl | sort | uniq -c | sort -rn
grep -oE '"name":"mcp__zerops__[a-z_]+"' main-session.jsonl | sort | uniq -c | sort -rn
```

A high `zerops_workflow` count (20+) is normal — every step has completion calls and subagent sync. A high `zerops_guidance` count (20+) means the agent is pulling guidance repeatedly; when the v14 eager-topic work landed, this dropped from 22 to ~10.

---

## How to evaluate content quality

Counts are a proxy, not a rubric. The v15→v16 run shows this: v15 had MORE gotchas and IG items than v16, but v16 content was structurally cleaner. Quality evaluation requires reading.

### README

For each codebase README:

1. **Does the intro fragment accurately name the managed services?** v16's root README shipped "connected to typeorm" — TypeORM is an ORM library, not a database. That's a factual bug in the published content, caught by running `dbDriver` validation at the source (v17 fix).
2. **Do integration-guide items carry actual code?** Items like "Adding `zerops.yaml`" with the full YAML, or "Bind to 0.0.0.0" with the `app.listen(3000, '0.0.0.0')` diff. Prose-only IG items are thinner.
3. **Are gotchas platform-anchored or framework-narration?**
   - Authentic: names a Zerops mechanism (`zsc execOnce`, `${db_hostname}`, `readinessCheck`, `minContainers`, `deployFiles`) OR a concrete failure mode ("returns 502", "`AUTHORIZATION_VIOLATION`", "HTTP 200 with plain-text 'Blocked request'"). Even better if both — framework × platform intersection.
   - Synthetic: architectural narration ("Shared database with the API"), credential descriptions ("NATS authentication"), scaffold-quirk documentation ("TypeORM afterInsert hooks don't fire during raw SQL seeding").
4. **Are gotchas distinct from the integration-guide headings in the same README?** v15's appdev had 3 gotchas that restated 3 adjacent IG items word-for-word. The v8.67.0 `gotcha_distinct_from_guide` check now catches this.
5. **Do gotchas duplicate across codebases?** v15 had NATS credentials in both apidev and workerdev, SSHFS ownership in both, `zsc execOnce` burn in both. Facts must live in exactly ONE README with others cross-referencing. Caught by `cross_readme_gotcha_uniqueness`.
6. **Does the content belong in README at all?** Container-ops trivia (SSHFS uid fix, `npx tsc` wrong-package, `fuser -k` for stuck ports) belongs in CLAUDE.md. README is for a user porting their own code who doesn't care about your dev-loop.

### CLAUDE.md (v16+)

1. **Does it clear the byte floor?** v17 raised the floor to 1200 bytes. v16's 39–44 line files cleared the old 300-byte floor but were mostly template boilerplate.
2. **Does it have custom sections beyond the template?** The template (Dev Loop / Migrations / Container Traps / Testing) is necessary but not sufficient. Good CLAUDE.md adds codebase-specific operational knowledge: how to add a new managed service, how to reset dev state without a redeploy, how to tail logs, how to drive a test request by hand.
3. **Is the content framework-specific or generic?** Boilerplate ("ssh into the container, start the dev server, run migrations") fails the depth bar. Specific commands for THIS codebase pass.

### Environment import.yaml comments

This is where v7 → v16 regressed the most. The published env files go to `zeropsio/recipes` where a user lands on one tier's page and deploys. The comments there are the final-mile "why this decision" surface.

Read the comments and ask: does each one explain what BREAKS if you flip the decision, or is it describing what the field does?

- **Gold standard (v7)**: *"JWT_SECRET is project-scoped — at production scale this is critical because session-cookie validation fails if any container in the L7 pool disagrees on the signing key. Service-level envSecrets would force every container to be redeployed when the key rotates."* — explains the trade-off AND the operational consequence.
- **Regression (v16)**: *"Small production — minContainers: 2 on runtime services enables zero-downtime rolling deploys. JWT_SECRET shared at project level ensures tokens verify across both containers."* — describes what the field does, doesn't explain the failure mode the project-level placement prevents.

v17 enforces this via `{env}_import_comment_depth` — requires ≥35% of substantive comment blocks to contain a reasoning marker.

---

## Rating methodology

Each version gets a letter grade based on FOUR dimensions. Grade the dimensions independently, then the overall rating is the lowest of the four (regressions in any one dimension sink the whole grade).

### 1. Structural correctness (`S`)

Does the recipe actually work end-to-end?

- **A** — all 6 workflow steps completed, all services RUNNING, both dev and stage URLs load in a browser, feature sections exercise all managed services.
- **B** — recipe completed but required extra iterations at one step (finalize retry for comment ratio, deploy retry for scaffold issue). Close-step review found ≤1 CRITICAL.
- **C** — completed but with CRITICAL issues in production (contract mismatches, entity shape errors, silent double-processing). Code review caught ≥2 CRIT.
- **D** — completed but the deliverable has a hard bug a user would hit on first run (wrong database name in intro, missing preprocessor directive, broken migration).
- **F** — failed to complete. Workflow aborted, or manual intervention was needed.

### 2. Content quality (`C`)

Is the published content worth reading?

- **A** — README gotchas are all authentic + unique across codebases + distinct from IG items. Env comments teach the WHY in ≥35% of blocks. CLAUDE.md files have ≥2 custom operational sections per codebase. Root README intro accurately names services.
- **B** — content is mostly good but has 1–2 synthetic gotchas, or env comments score 25–35%, or one codebase's CLAUDE.md is thin.
- **C** — multiple synthetic gotchas, cross-README duplication, env comments are pure narration, OR CLAUDE.md is stub-shaped. The content "works" but doesn't teach.
- **D** — factual errors in published content (wrong service name, ORM-as-database, broken fragment marker). Would mislead a reader.
- **F** — unpublishable. Fragment markers malformed, scaffold TODO markers left in, deliverable README is blank.

### 3. Operational efficiency (`O`)

How much time and agent effort did the run burn?

- **A** — wall ≤ 90 min. Bash total ≤ 10 min. Zero very-long (>60s) bash calls. Dev-server sum ≤ 300s. Errored bash calls ≤ 10.
- **B** — wall ≤ 120 min. Bash total ≤ 15 min. ≤2 very-long. Dev-server sum ≤ 500s. Errored ≤ 15.
- **C** — wall ≤ 180 min. Bash total ≤ 20 min. ≤5 very-long. Dev-server sum ≤ 900s. Errored ≤ 20.
- **D** — wall > 180 min OR very-long > 5 OR dev-server sum > 900s. Significant operational cost.
- **F** — wall > 300 min OR session stalled / got stuck / timed out repeatedly.

### 4. Workflow discipline (`W`)

Did the agent follow the intended workflow shape?

- **A** — All sub-steps used. Feature subagent fired at deploy.subagent. Browser verification ran at deploy.browser and close.browser. Code review ran with the correct prompt. Finalize was ≤2 iterations.
- **B** — All required subagents ran but one sub-step was skipped OR finalize took 3+ iterations.
- **C** — Missing feature subagent OR missing browser walk OR code review ran with narrow context.
- **D** — Multiple sub-steps missed. Workflow shape improvised rather than followed.
- **F** — Agent worked around the workflow instead of through it (raw deploy commands, manual README writing during generate, etc.).

### Overall = min(S, C, O, W)

Record all four dimensions in the version entry plus the minimum as the overall grade. This makes it clear which dimension is limiting each run.

---

## Cross-version summary

### Content metrics

README line counts (per codebase):

| v | apidev | appdev | workerdev | gotchas (api/app/worker) | IG items (api/app/worker) |
|---|---:|---:|---:|:-:|:-:|
| v6 | 293 | 158 | — | 7 / 5 / — | 5 / 3 / — |
| v7 | 271 | 168 | 167 | 6 / 5 / 4 | 4 / 3 / 3 |
| v8 | 239 | 124 | 171 | 6 / 3 / 4 | 4 / 3 / 2 |
| v9 | 245 | 126 | 196 | 6 / 4 / 4 | 3 / 3 / 3 |
| v10 | 295 | 139 | 112 | 4 / 4 / 0 | 0 / 2 / 0 |
| v11 | 246 | 105 | 162 | 6 / 3 / 4 | 4 / 2 / 2 |
| v12 | 270 | 132 | 153 | 5 / 4 / 4 | 3 / 2 / 2 |
| v13 | 232 | 138 | 155 | 6 / 5 / 5 | 3 / 3 / 3 |
| v14 | 267 | 124 | 141 | 7 / 4 / 4 | 5 / 4 / 3 |
| v15 | 281 | 123 | 166 | 6 / 4 / 4 | 6 / 4 / 3 |
| v16 | 218 | 123 | 162 | 4 / 3 / 3 | 3 / 2 / 2 |
| v17 | — | — | — | — (aborted) | — (aborted) |
| v18 | 257 | 117 | 161 | 4 / 3 / 4 | 4 / 3 / 2 |
| v19 | 237 | 149 | 144 | 4 / 3 / 3 | 4 / 3 / 2 |
| v20 | **349** | **231** | **267** | **7 / 6 / 6** | **6 / 5 / 5** |
| v21 | 230 | 154 | 165 | 6 / 4 / 5 | 6 / 3 / 4 |
| v22 | 341 | 208 | 221 | **7 / 6 / 6** | **8** / 6 / 6 |
| v23 | 250 | 260 | 290 | 6 / 3 / 4 | 4 / 4 / 4 |
| v25 | **435** | 182 | 247 | 6 / 6 / 6 | 6 / 4 / 3 |
| v28 | 261 | 189 | 230 | 5 / 5 / 5 | 5 / 4 / 4 |
| v29 | 282 | 174 | 258 | 5 / 4 / 5 | 5 / 4 / 3 |
| v30 | 263 | 161 | 165 | 6 / 5 / 6 | 3 / 3 / 3 |
| v31 | **354** | **203** | **247** | 3 / 3 / 3 | **8** / 5 / 6 |
| v32† | 303† | 162† | 218† | 5† / 3† / 4† | 5† / 4† / 5† |
| v33 | 320 | 199 | 191 | 8 / 5 / 5 | 6 / 5 / 7 |

† v32 per-codebase READMEs shipped by writer sub-agent but MISSING from the published deliverable tree (close step never completed, export ran premature). Counts taken from TIMELINE.md narrative, not re-readable from the shipped recipe.

**v7 remains the gold standard for gotcha depth** (Meilisearch ESM-only, auto-indexing skips on redeploy, NATS queue group for HA). v10 collapsed to 0 gotchas on apidev and workerdev due to a tooling regression that's since been fixed. v14/v15 peaked on IG item count. v16 is the most compressed but also the most structurally clean.

### Session metrics (v8 onwards)

| v | date | wall | asst events | tool calls | bash calls | bash total | very-long (>60s) | dev-server sum | errored |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|
| v8 | 2026-04-12 | 96 min | 313 | — | 62 | 4.7 min | 2 | 11s | 4 |
| v9 | 2026-04-12 | 183 min | 297 | 199 | 54 | 5.0 min | 1 | 55s | 9 |
| v10 | 2026-04-12 | 64 min | 269 | 171 | 68 | 8.6 min | 4 | 150s | 8 |
| v11 | 2026-04-12→13 | 480 min | 286 | 174 | 72 | 21.1 min | 4 | 542s | 14 |
| v12 | 2026-04-13 | 61 min | 247 | 155 | 40 | 6.4 min | 1 | 134s | 2 |
| v13 | 2026-04-13 | 84 min | 489 | 273 | 108 | 19.0 min | 6 | 968s | 17 |
| v14 | 2026-04-13 | 82 min | 313 | 196 | 90 | 12.7 min | 4 | 170s | 18 |
| v15 | 2026-04-14 | 204 min | 326 | 203 | 63 | 17.0 min | 5 | 557s | 7 |
| v16 | 2026-04-14 | 125 min | 370 | 233 | 78 | 7.5 min | **0** | 250s | 9 |
| v17 | 2026-04-14 | **23 min (abort)** | 146 | 90 | 32 | 1.5 min | 0 | 6.4s | 9 |
| v18 | 2026-04-15 | **65 min** | 223 | 145 | 31 | **0.8 min** | **0** | **13s** | **0** |
| v19 | 2026-04-15 | 75 min | 262 | 174 | 37 | 1.0 min | **0** | 7s | 1 |
| v20 | 2026-04-15 | 71 min | 294 | 177 | 33 | 2.3 min | **0** | ~50s | 7 |
| v21 | 2026-04-16 | **129 min** | 381 | 233 | 72 | **9.2 min** | **4 (main) + 2 (scaffold) = 6** | 0.3s | **17 (bash) + 27 (tool_result is_error)** |
| v22 | 2026-04-16 | **103 min** | **410** | **243** | 77 | 2.5 min | **0 (main)** + 2 (scaffold `nest new scratch`) = 2 | ~0s (delegated to feature subagent) | 7 (bash) + 12 (tool_result is_error) |
| v23 | 2026-04-17 | **119 min** | 384 | 233 | 61 | **0.6 min** | **0 (main)** + 2 (scaffold `nest new scratch`) = 2 | ~0s | 2 (bash) |
| v25 | 2026-04-17 | **71 min** | **225** | 177 | 30 | **0.5 min** | **0 (main)** + 2 (scaffold `nest new scratch`) = 2 | ~0s | **0 (main)** |
| v28 | 2026-04-18 | **85 min** (recipe) / 109 min (total) | 378 | ~501 | 62 | **1.6 min** | **0 (main)** + 2 (scaffold `nest new scratch`) = 2 | ~0.3s (MCP) | 5 (main) |
| v29 | 2026-04-18 | **83 min** | 378 | ~534 | 62 | 4.3 min | **0 (main)** + 2 (scaffold `nest new scratch`) = 2 | ~0.3s (MCP) | 8 (main) |
| v30 | 2026-04-19 | **64 min (new all-time best for complete run)** | **281** | ~458 | 116 | 4.6 min | **0 (main)** + 0 scaffold-layer | ~74s (20 MCP calls) | 13 (tool-total) |
| v31 | 2026-04-19 | 86 min | ~280 (main) | ~720 | 75 | **1.3 min** | **0 (main)** + 2 scaffold-layer | ~0s (MCP-driven) | **8 (main)** |
| v32 | 2026-04-19 | **70 min (hit v8.96 target band)** | 296 | ~430 | 49 | **0.2 min (new low, main only)** | 0 (main) + scaffold-layer | ~75s (19 MCP calls) | 13 (main, mostly workflow rejections) |
| v33 | 2026-04-20 | 88 min (recipe work) + ~21 min post-close export noise | **321** | ~693 | 40 | **0.5 min (30.1s — new all-time best)** | 0 (main) + 1 scaffold-layer (apidev `nest new`) | 109s (26 MCP calls) | 4 (main, 3 cascade from pre-init git) |

**v16 is the first run with zero very-long bash calls on main-session** — the dev-server wait discipline finally held. But the feature subagent in v16 still hit 360s of 404s total on two dev-server starts that used the old SSH pattern. v17 ships `zerops_dev_server` as a dedicated MCP tool to eliminate this class of error entirely.

**v18 is the first run with zero very-long bash calls across main AND all subagents, AND zero errored bash calls on main.** Main bash total collapsed to 47.8s (0.8 min) — the previous record was v8's 4.7 min. Sum across main + 7 subagents: ~253s (4.2 min) / 1 errored / 0 very-long. Single `zerops_dev_server` bash probe at 13s vs v16's 250s sum. This is the full payoff from the v17.1 dev-server spawn-shape fix and the scaffold-subagent SSH preamble landing in recipe.md.

### Milestones and regressions by version

| v | context | key structural change |
|---|---|---|
| v6 | first 3-codebase run, no separate worker yet | first dual-runtime complete |
| v7 | gold-standard content baseline | deep gotchas, full IG code blocks, env comments teach WHY |
| v8 | first with SESSIONS_LOGS captured | content starts compressing, first CRITICAL in close review (status endpoint shape mismatch) |
| v9 | first with separate codebase worker fully wired | 2 CRITICAL in close (worker migration creates uuid-ossp AFTER table, missing CreateJobResults migration) |
| v10 | workflow broke — empty gotchas | apidev+workerdev gotcha sections EMPTY; represented content catastrophe (v8.54.0-era tooling bug) |
| v11 | longest run on record | 8-hour wall clock, 541s dev-server sum; 2 CRITICAL close (worker entity mismatch) |
| v12 | stability recovery | 61 min wall, 2 WRONG in close — fastest clean run |
| v13 | Sonnet model + enforced subagent | 84 min wall but 108 bash calls, 6 very-long. First run with enforced feature-subagent subwk |
| v14 | model gate, eager topics | feature subagent now enforced; close review 0 CRIT/0 WRONG (cleanest close ever) |
| v15 | content quality regression | content peaked at 6/4/4 IG items but 5 WRONG in close (all dev-ops issues — npx tsc, SSHFS, Svelte curly, port 3000, Vite death) |
| v16 | v8.67.0 structural rules landed | zero very-long bash, first run with CLAUDE.md split, content structurally cleanest; BUT 1 CRIT (StatusPanel queue→nats contract drift) + 6 WRONG in close |
| v17 | v8.70.0 content pass + `zerops_dev_server` MCP tool | **first F-grade run — did not complete**. `zerops_dev_server` hung 300s on first call; scaffold sub-agents all ran commands zcp-side instead of ssh'ing into containers. User aborted at 23 min. |
| v18 | v17.1 fixes land — spawn-shape + SSH preamble | **first full-tree zero-very-long run**, 65 min wall, 0.8 min main bash, 0 errored main. All v17 regressions held fixed: `zerops_dev_server` stable (9 MCP calls, 13s bash probe), scaffold subagents ssh'd correctly, root README intro names real managed services, all 6 env yamls have `#zeropsPreprocessor=on`. Close step: 0 CRIT / 2 WRONG (both fixed). |
| v19 | content-check infrastructure working + stale-major import class surfaces | 75 min wall, 1.0 min main bash, 0 very-long, 8 subagents (first run with **two** fix subagents during generate catching 4 MEDIUMs before publish). Content rules held: predecessor-clone dedup, restates-guide, yaml-in-sync, comment specificity — all caught at generate, all fixed before deploy. **First close-step CRIT from the stale-major import class** — `CacheModule` imported from `@nestjs/common/cache` (NestJS 8 path) in a NestJS 10 project. v19 post-mortem fixes shipped as v8.77.0: dev_server `NoHTTPProbe` mode with PID-liveness (no log-string matching), close-step browser-walk sub-step gate, installed-package verification rule in scaffold + feature briefs (framework-agnostic), `minContainers` two-axis semantics teaching, HA-vs-horizontal-scaling conflation purge. |
| v20 | content peaks across the board + load-bearing reform surfaces | **first A-grade complete run in tracked history**. 71 min wall, 2.3 min main bash, 0 very-long, 10 subagents (new record), **first run since v16 with both deploy.browser AND close.browser** (v8.77 sub-step gate working), **first dedicated close-step critical-fix subagent** that re-deploys + re-verifies. Largest READMEs ever (349/231/267), peak gotcha counts (7/6/6), peak IG items (6/5/5). All v7 gold-standard worker gotchas restored. Env 4 comments at v7 quality with explicit two-axis `minContainers` teaching. BUT deep content read surfaces 7 classes of decorative-content drift the v8.67–v8.77 presence rules admitted: generic `.env` advice, predecessor-cloned apidev gotchas (synchronize/trust-proxy), `_nginx.json` topology mismatch in shipped vs documented, CLAUDE.md `synchronize()` against own README gotcha, watchdog declared but unimplemented, IG #2 leans on IG #1, env service comments share rolling-deploy template phrasing. v20 post-mortem fixes shipped as v8.79.0 — load-bearing content reform: 5 new per-codebase checks (content_reality, gotcha_causal_anchor, service_coverage, claude_readme_consistency, ig_per_item_standalone), 1 new finalize check (service_comment_uniqueness), predecessor-floor net-new enforcement rolled back to informational (predecessor overlap is fine for standalone recipes; service-coverage is the new authoritative gate). |
| v21 | v8.78 load-bearing reform ships — regresses on operational + workflow dimensions | **first D-grade run since v13** (v17's F was an abort). Wall 129 min (+82% vs v20's 71 min), main bash 9.2 min (4× v20), 6 very-long bash cumulative, 27 is_error_true tool_results (3.4× v20). **208 MB `node_modules` + 748 KB `dist/` leaked into apidev published output** because main agent's per-codebase brief synthesis dropped `.gitignore`/`.env.example` line for apidev+workerdev (included for appdev). Downstream: 3 parallel zcp-side `git init && git add -A` over SSHFS traversing 209 MB tree hit 120 s each; root-ownership cascade recovery burned another ~2 min. **First run with 0 `zerops_guidance` calls** (v18–v20: 12–16). **5 subagents** vs v20's 10 — delegation patterns (writer, yaml-updater, generate-time fix×2, close-step critical-fix) all collapsed; main agent absorbed iteration in main context (190 KB more tool_use input bytes). Content compressed 26% (230/154/165 README lines, 6/4/5 gotchas) — hit exactly the `service_coverage` floor. **6 new `dev_server stop` exit-255 events** (v20: 1) — pkill-over-ssh matches its own shell child; tool surfaces raw 255 rather than classifying as success. **`claude_readme_consistency` check emitted 0 events** — its `forbiddenPatternRe` regex is so narrow it only matches v20's exact phrasing; 0 matches across v21 content (shadow-tested). Feature subagent hit 6 MCP schema-validation errors using memory-frozen parameter names (`hostname` vs `serviceHostname`, `logLines` vs `lines`, `ramGB` vs `minRam`). Close-step: 1 CRIT (CORS `origin: true, credentials: true`) + 3 runtime CRITs (NATS `ERR_INVALID_URL` on bracketed `@`-in-password, Meilisearch ECONNRESET OOM during seed, CORS preflight). Framework hardcoding audit surfaces `typeorm`/`prisma`/`ioredis`/`keydb` hardcoded in `categoryBrands` + `TypeORM synchronize`/`queue: 'workers'` in `specificMechanismTokens` — violates v8.78's "framework-agnostic by design" claim. v21 post-mortem + v8.80 implementation guide: [docs/implementation-v21-postmortem.md](implementation-v21-postmortem.md). 9 enforced-gate fixes planned (scaffold_hygiene, bash-guard middleware, gotcha_depth_floor, pattern-based claude_readme_consistency rewrite, strip framework tokens, writer-subagent dispatch gate, MCP schema-error rename suggestions, scratch-reference content-diff, pkill-self-kill classification). |
| v22 | v8.80 structural gates hold — first Opus 4.7 run; substance restored, cost axes regress | **Mixed run that rhymes with v21 on cost but not on substance**. Wall 103 min (+45% vs v20's 71 min; −20% vs v21), but **410 assistant events is a new complete-run record** (v21: 381, v20: 294) — and tool calls 243 (v21: 233, v20: 177) — so on the two pure cost-volume axes v22 exceeds v21. First run on `claude-opus-4-7[1m]` — model change likely drives part of the deliberation bloat. **Every v8.80 structural gate held**: `scaffold_hygiene` caught `node_modules`/`dist/` leaks on api+worker at deploy step and forced ssh-side `rm -rf` remediation (25.4 s cost, 4 iteration rounds); published tree 6.1 MB clean (v21: 214 MB, 35× smaller); `claude_readme_consistency` rewrite fired 4+4 times catching TypeORM synchronize in api+worker CLAUDE.md without dev-only marker (was silently dead in v21); pkill-self-kill classification silent on dev_server stop (0 exit-255 events surfaced as failures — the 6 "exit 255" string matches are all guidance warnings about SSH session state after redeploy, not failure reasons); 0 MCP schema-validation errors across main AND feature subagent (v21: 6 in feature); substep gate rejected one premature `deploy` complete with INVALID_PARAMETER "12 required sub-steps" message and forced the agent back through browser-walk + cross-deploy. **9 subagents** (v21: 5, v20: 10) — delegation pattern restored: 3 scaffold + 1 feature + 1 README/CLAUDE writer + 1 env-comments writer + **3 parallel per-codebase framework-expert code reviews** (new split — finished in 71/11/27 s). **Writer subagent gate worked at dispatch but content iteration leaked back to main**: main ran 31 Edits + 4 Writes, including **11 Edits on `workerdev/README.md`** and 8 on `apidev/README.md` after the writer subagent handed off — content checks (`worker_content_reality: 16 fails`, `worker_gotcha_causal_anchor: 16 fails`, `worker_gotcha_distinct_from_guide: 16 fails`) kept bouncing back and the iteration absorbed into main context rather than dispatching a content-fix subagent. **Content restored to v20 peak**: 341/208/221 README lines + 7/6/6 gotchas (matches v20) + **8/6/6 IG items — apidev at new record of 8** (prior peak v15/v20 at 6). CLAUDE.md at 7307/6245/7724 bytes (v20: 3395/2786/3728) — **largest ever across all three codebases**, 7+ custom sections per file. Env-4 comments at v7+v8.77 gold standard — every service block carries explicit WHY, two-axis `minContainers` teaching intact on app/api/worker. Root README intro names real managed services (v17 `dbDriver` validation holds for 5th run). **3 deploy-step CRITs, all fixed in main inline**: NATS `TypeError: Invalid URL` on `${queue_password}` URL-reserved chars (recurrence of the v21 class — the `nats.service.ts` scaffolder still ships URL-embedded creds despite the v21 gotcha being in scope); S3 `HeadBucketCommand` 301 redirect (apidev scaffold used `http://` not `https://` endpoint); workerdev `zerops_dev_server start` returned `post_spawn_exit` because dev `buildCommands` runs `npm install` only (no `npm run build`) so `dist/main.js` doesn't exist — scaffold assumed dist would. Plus one feature-subagent-era DI resolution CRIT: `CacheFeatureController` couldn't resolve `CacheService` after per-feature module split — fixed with a `@Global() ServicesModule` wrapper. **Close review clean (3 parallel framework-expert subagents): 0 CRITICAL / 0 WRONG** — cleanest close since v14's spotless review, and the first since v14 to combine clean close with peak content. Guidance calls 17 (v21: 0, v20: 12) — progressive-guidance delivery restored. Both `deploy.browser` and `close.browser` fired (5+3 `zerops_browser` calls — highest ever). **The regression story is about cost, not deliverable quality**: content is near-peak, close is spotless, hygiene is clean, all v8.80 gates held. Primary cost drivers: (a) Opus 4.7's higher tool-call-per-decision ratio, (b) content-check iteration rounds (every v8.78/v8.79/v8.80 check fired real fails and was resolved — the quality gate is now strict enough to drive real work), (c) 3 runtime CRITs each required rebuild/redeploy/reverify cycles, (d) 11 workerdev-README edits leaked to main because no post-writer content-fix-subagent dispatch pattern exists. |
| v23 | v8.81–v8.85 ship between v22 and v23; content quality recovers, content-fix-loop convergence breaks | **Mixed run: content is the silver lining, convergence is the cost driver**. Wall 119 min (+15% vs v22's 103, +68% vs v20's 71). 384 assistant events (v22: 410, v21: 381). Tool calls 233 (= v21, < v22's 243). Main bash 0.6 min — best operational hygiene since v18 (0 very-long, 2 errored, 38.7s total). 10 subagents matches v20 record (writer ×1 + scaffold ×3 + feature ×1 + **3 README/content fix subagents** + env-comment writer ×1 + code review ×1). **17 deploys** (v22: 11) — 5 clusters of 3, ALL justified by intervening commits (initial dev → snapshot-dev after features → first cross-deploy → code-review redeploy → second cross-deploy). 21 dev_server calls (v22: 15). 18 guidance topic fetches (v22: 17 — peak). 20 `complete deploy` calls (12 substep + 8 retries on README content checks). **The 23-minute README content-fix loop within deploy is the single biggest controllable waste**: writer subagent at 13:08-13:14 → first complete deploy fails on 23 checks → fix subagent SA2 (13:20-13:26) → 11 fails → SA3 (13:27-13:32) → 5 fails → SA4 (13:34-13:37) → 4 fails → main inline pass (13:37-13:41) → 0 fails. **Strictly decreasing fail count (23→11→5→4→2→0), no whack-a-mole — but 5 rounds where 1 should have sufficed**. Three structural defects: (a) `content_reality` truncates findings list with `... and N more`, so each subagent only sees a subset; (b) brief framing was "be surgical, only the listed findings" — SA4 explicitly skipped 3 known-bad lines because they weren't on the visible list; (c) `comment_ratio` reads the YAML embedded in README's IG step 1 fenced block, NOT the on-disk `zerops.yaml` — SA2 wasted 5m 50s editing the wrong files (fixed disk yamls 9% → 57% but checker still reported 9%). **Content quality is the silver lining**: 13 gotchas total (apidev 6 / appdev 3 / workerdev 4 — vs v22's 19) but **gotcha-origin ratio swings from v22's 0% pure-invariant to v23's 38% pure-invariant + 15% mixed + 46% incident-derived**. Compression came WITH quality, not against it. All CLAUDE.md pass byte floor + claude_readme_consistency. Architecture section present in root README (v8.81 `recipe_architecture_narrative` check fired 1×, agent fixed, retry passed). Env-4 comments solid (A−), service uniqueness pass, two-axis `minContainers` teaching at project level + per-service. Root intro names real managed services (v17 `dbDriver` validation holds for 6th run). **0 CRIT / 0 WRONG / 1 STYLE in close review** (3 CRITs caught + fixed pre-publish: StatusPanel `data.services[key]` vs apidev's flat `{db,redis,nats,storage,search}` map, workerdev Item entity divergence vs apidev migration, missing NATS queue group `'jobs-workers'`; 2 WRONG fixed: workerdev SIGTERM drain, apidev cache.drop double-RTT). **Three platform-mental-model defects**: (1) **`zsc execOnce` "burn trap" folk-doctrine enshrined in apidev/CLAUDE.md** — at 12:31:36 the apidev first-deploy seed initCommand returned ✅ in 56ms with zero `[seed] upserted ...` lines and 0 rows in the table; agent invented "execOnce burn from initial workspace deploy" terminology and codified a "Recovering execOnce burn" CLAUDE.md section. The mental model is wrong: `appVersionId` is per-deploy, not per-workspace. Real cause undiagnosed — most likely script-side silent exit or container-labeling artifact (`apidev-runtime-2-2e` suggests this was the second container). (2) **"Parallel cross-deploys rejected" — `Not connected` misattribution to platform** — at 12:32:17-19 main agent issued 3 parallel calls (subdomain enable + 2 deploys), the deploy calls returned `Not connected` (MCP STDIO transport error because `zerops_deploy` blocks the channel for 1-2 min). TIMELINE.md says *"Sequential cross-deploys (parallel called but tool returned 'Not connected' for simultaneous deploys, so serialized)"* — propagating the misattribution to the published TIMELINE. (3) **`module: nodenext` workerdev fix without diagnosis** — workerdev `start:dev` failed with `Cannot find module './app.module.js'`. Apidev tsconfig is IDENTICAL (also `module: nodenext`) but apidev works because `start:dev` uses `nest start --watch` (which proxies through @nestjs/cli's bundler) while workerdev uses raw `ts-node -r tsconfig-paths/register`. Agent flipped workerdev to commonjs without questioning why apidev's identical tsconfig didn't fail. Sustainable fix, shallow understanding. **`scaffold_hygiene` held** (no v21-class node_modules leak). **v8.81 `content_fix_dispatch_required` gate fired correctly** — v22's iteration-into-main pattern eliminated, but the brief construction emitted by the gate is anti-convergent (5 rounds instead of 1). v22's failure: dispatch gate didn't exist, iteration leaked to main. v23's failure: dispatch gate exists but emits convergence-hostile briefs, iteration loops across subagents. **v8.85 `env-var-model` topic + `env_self_shadow` check held** — no shadows in any zerops.yaml; both `${db_*}` and `${redis_*}` referenced via `process.env` correctly. v23 post-mortem at [docs/implementation-v23-postmortem.md](implementation-v23-postmortem.md). v8.86 implementation plan supersedes the postmortem's §7 list — see [docs/implementation-v8.86-plan.md](implementation-v8.86-plan.md). |
| v8.90 | state-coherence fixes ship (pre-v26) | **Implementation-only release** between v25 and v26, addressing the two workflow-discipline defects v25 surfaced. Four upstream fixes, zero new checkers, zero new gates (per rollback-calibration rule): (A) `SUBAGENT_MISUSE` error code added in `internal/platform/errors.go`; `handleStart` in `internal/tools/workflow.go` now rejects `action=start` when a non-immediate workflow is already active AND `active != input.Workflow`, replacing the misleading `PREREQUISITE_MISSING: Run bootstrap first` with a precise message that tells sub-agents to `do NOT call zerops_workflow`. Immediate workflows (cicd, export) are exempt; same-workflow restarts fall through to the workflow-specific idempotency. (B) `subagent-brief` and `readme-fragments` flipped to `Eager: false` in `internal/workflow/recipe_topic_registry.go`; `where-commands-run` stays eager. The existing `subStepToTopic` mapping (SubStepSubagent → subagent-brief, SubStepReadmes → readme-fragments) already lands each brief in the response to `complete substep=<previous>` because `buildGuide` reads `currentSubStepName()` after the advance. (C) A uniform `TOOL-USE POLICY` block was prepended to the scaffold / feature / README-writer / code-review sub-agent briefs, listing 7 forbidden tools (zerops_workflow, import, env, deploy, subdomain, mount, verify) and the 'workflow state is main-agent-only' rule. (D) The `deploy-skeleton` section in `recipe.md` was rewritten with a leading `⚠ Substep-Complete is load-bearing (v8.90)` header, naming the two load-bearing brief-delivery boundaries (`init-commands→subagent`, `feature-sweep-stage→readmes`) and the v25 anti-pattern explicitly. New tests: `internal/tools/workflow_start_test.go` (Fix A, 7 cases), `internal/workflow/recipe_substep_briefs_test.go` (Fix B+D, 10 cases), `internal/workflow/recipe_tool_use_policy_test.go` (Fix C, 3 matrix tests × 4 briefs). Full test suite green, `make lint-local` clean. v26 calibration bar: 0 `SUBAGENT_MISUSE` rejections; 0 out-of-order substep attestations; `complete init-commands` response ≥14 KB containing subagent-brief phrases; `complete feature-sweep-stage` response ≥17 KB containing readme-fragments phrases; step-entry ≤30 KB (confirming de-eager held); ≤2 full-step README content-check failures. Implementation guide: [docs/implementation-v8.90-state-coherence.md](implementation-v8.90-state-coherence.md). |
| v25 | first post-rollback run — operational + content reproduce v20; substep-delivery mechanism structurally bypassed | **Operationally cleanest full run on record; workflow-discipline defect surfaces.** Wall **71 min** (matches v20 exactly). 225 assistant events (lowest since v18's 223). 177 tool calls (matches v20 exactly). Main bash **0.5 min (30.9 s) — new best ever**, 0 very-long, **0 errored**. 7 subagents (scaffold×3 + feature×1 + README/CLAUDE writer×1 + content-fix×1 + code-review×1) with clean role separation. 11 deploys across 4 clusters (initial dev×3 + snapshot×3 + cross-stage×3 + appstage `./dist/~` fix + close-fix), all justified. 6 browser calls (4 deploy + 2 close). **5 guidance calls** (v20: 12, v22: 17, v23: 18) — low, expected post-rollback since v8.84's `EagerAt` scope shift was rolled back and agents fetch less explicitly. Close review: **0 CRIT / 1 WRONG / 3 STYLE** — WRONG was appdev `ItemsCrud.svelte` DELETE using raw `fetch()` bypassing `api.ts` helper, fixed via new `apiVoid()` helper + 1-deploy cycle. Content: apidev **435 README lines (new peak, +86 vs v20's 349)**, appdev 182, workerdev 247; gotchas **6/6/6** (v20: 7/6/6); IG items 6/4/3 (v20: 6/5/5); CLAUDE.md 4945/5576/5835 bytes (v20: 3395/2786/3728). Root README intro names real managed services (v17 `dbDriver` validation holds for 7th run). Preprocessor directive held on all 6 envs. Env-4 comments mostly v7-grade — two-axis `minContainers` teaching intact on api + worker — but **env 4 app-static comment contradicts its own YAML**: claims `"minContainers stays at the platform default"` while YAML declares `minContainers: 2`. Classic v20-era decorative-drift class; rollback calibration explicitly says editorial fix, not new check. No root README architecture section (v8.81 narrative check was rolled back; C+ cross-codebase-coherence limiter returns). **Rollback calibration verdict: PASSED** — v25 reproduces v20's A operational + A− content at v20's wall time with cleaner bash metrics. The v22–v24 cost axes (410/384 events, 23-min fix loops, 11 main-context README edits) do NOT recur. **But the run surfaces a new class of workflow-discipline defect the S/C/O/W rubric didn't capture**: (1) **Main agent bypassed the substep-delivery mechanism entirely** — did all 40 min of deploy work silently, then backfilled all 13 substep completions in 2 min at step end (21:06:55 → 21:08:59); first attestation out-of-order (`INVALID_PARAMETER: expected "deploy-dev", got "subagent"`). Each `complete substep=X` call returns the NEXT substep's scoped brief in `current.detailedGuide` — 73 KB of phase-scoped guidance was delivered into dead phases. Most consequentially, the 14 KB `subagent-brief` (delivered at `complete init-commands`) arrived 33 min AFTER the feature subagent had already dispatched and finished, and the 17 KB `readme-fragments` brief arrived AFTER the README writer had shipped 6 files. The 6-min content-fix-subagent cycle at 21:10:27-21:16:38 is DIRECTLY caused by the writer running without its substep brief: 6 content-check failures (3× `gotcha_distinct_from_guide`, `api_comment_specificity` 14%, `worker_comment_ratio` 28%, `cross_readme_gotcha_uniqueness`) all caught rules the missed brief encodes. (2) **Subagents called `zerops_workflow` at spawn**: feature subagent (20:34:30) first tried empty-input, then `action=start workflow=develop` — both rejected. Server's response was misleading: `PREREQUISITE_MISSING: Run bootstrap first` when the real state is "recipe is already active, subagent should not start a workflow." Appdev scaffold subagent hit the same path at 20:20:46. Recovery worked because subagents rationalised ("main agent orchestrates workflow state"); a subagent following the suggestion literally could have attempted `action=start workflow=bootstrap` and corrupted the recipe session. Blast-radius latent, not hit this run. **Bypass root cause**: `subagent-brief`, `where-commands-run`, and `readme-fragments` topics are all marked `Eager: true` in `recipe_topic_registry.go`. Eager topics get injected into the step-entry guide (the 37 KB response at `complete generate`). The agent therefore has both delegation briefs in hand BEFORE completing any deploy substep — substeps stop being delivery mechanisms and become retroactive checklist items. v25 post-mortem + v8.90 implementation plan at [docs/implementation-v8.90-state-coherence.md](implementation-v8.90-state-coherence.md). Four fixes — all upstream, no new checkers: (A) `SUBAGENT_MISUSE` error at `handleStart` when session already active, replacing the misleading `Run bootstrap first`; (B) de-eager `subagent-brief` and `readme-fragments` (keep `where-commands-run` eager — it applies from substep 1), remap `SubStepInitCommands → subagent-brief` and `SubStepFeatureSweepStage → readme-fragments` so the briefs only land via substep-complete calls; (C) explicit tool-use policy in every subagent brief listing permitted + forbidden tools; (D) substep-complete discipline note at top of deploy step-entry, naming the v25 anti-pattern explicitly. **Rating**: S=A (all 6 steps, all features, both browsers, 0 close CRIT), C=A− (peak apidev README; env-4 mostly gold with the app-static YAML/comment contradiction), O=A (71 min wall, 0.5 min main bash — best ever, 0 errored main, 0 very-long, 0 MCP schema errors, 0 exit-255 classifications), W=B (substep-delivery mechanism bypassed, 73 KB briefs delivered into dead phases, 2 subagents hit `zerops_workflow` misuse rejections, 6-min content-fix cycle structurally attributable to substep bypass) → **B** overall. The headline numbers match v20; the workflow-discipline defect is the next thing worth fixing before the next run. |
| v28 | second post-rollback A-grade on mechanics; honest content audit drops surface from A− to C+ | **Mechanical discipline held; content-authoring mental-model surfaced as the limiter.** Wall **85 min** (v25: 71, v20: 71, v22: 103). 378 assistant events / ~501 tool calls / 5 subagents (scaffold×3 + feature×1 + code-review×1 — no writer, no content-fix, no env-comments dispatches; main agent wrote all content inline). Main bash 1.6 min / 0 very-long / 5 errored. 22 `zerops_guidance` fetches (v25: 5), **3 voluntary `zerops_record_fact` calls** — all three structured incidents became published gotchas. All v8.90 calibration bars passed: 0 `SUBAGENT_MISUSE`, substep attestations in canonical order in real-time (not v25's backfill pattern), ≤2 full-step README check failures (exactly 1 retry, fixed inline in 3 min). Both v8.93 calibration bars passed: recipePlan accepted without type-string retry, 0 zcp-side `cd /var/www/{host} && <exec>` patterns (git init via SSH container-side at provision). **Content metrics** (apidev / appdev / workerdev): README 261 / 189 / 230 lines, bytes 12,245 / 8,749 / 11,067; gotchas 5/5/5 balanced; IG items 5/4/4; CLAUDE.md 4754/4584/5232 bytes. Root README names real services (v17 `dbDriver` holds for 8th run). Preprocessor directive on all 6 envs. **Close review**: 2 CRIT + 1 WRONG + 4 STYLE; all CRIT+WRONG fixed inline by main after code-review subagent findings (missing NATS queue group, `ClientProxy.emit` without `firstValueFrom`, `parseInt(DB_PORT)` no fallback). **v8.85 `env_self_shadow` check-gap defect surfaced**: workerdev shipped `db_hostname: ${db_hostname}` + 8 more shadow keys at generate-write time; `complete step=generate` passed with 11 checks, none of which were `worker_env_self_shadow` — the check exists at [internal/tools/workflow_checks_generate.go:290](../internal/tools/workflow_checks_generate.go#L290) and `DetectSelfShadows` matches the exact pattern, but the worker hostname wasn't enumerated in the generate-complete check surface. The bug class that the rollback-calibration explicitly preserved this check to catch was not caught. Agent discovered the shadow at runtime (~5-10 min recovery), self-reported via `zerops_record_fact`, shipped gotcha about it. **Honest content audit drops C from A− to C+** (user correction — my first-pass grade was token-level, not reader-facing): out of 15 gotchas, only **~5 are genuine Zerops platform teaching** (appdev Vite allowedHosts, tilde suffix, build-vs-run envVars; workerdev queue group, SIGTERM drain); **5 are self-inflicted or wrong-surface** (apidev execOnce-silent-success blames platform for honoring exit-0 of a buggy seed; apidev Valkey-no-password is a 1-line fact bloated to 2 sentences; apidev `setGlobalPrefix` is pure NestJS framework docs; appdev b4 documents own `api.ts` scaffold helper as if universal; appdev b5 npm peer-dep metadata); **4 are framework-quirks borderline** (nats.js v2 URL-strip, MinIO virtual-hosted, Nest CLI tsbuildinfo-empty-emit, TypeORM synchronize-vs-migrate); **1 ships a folk-doctrine defect** — workerdev gotcha #1 claims *"The API codebase avoided the symptom because its resolver path happened to interpolate before the shadow formed; do not rely on that."* which is fabricated (both apidev and workerdev had identical shadow patterns and both were broken); correct rule per `env-var-model` guide (accessible during run, consulted 0 times by author) is "cross-service vars auto-inject project-wide; NEVER declare `key: ${key}`." Same class as v23's "Recovering execOnce burn" fabrication. **Two env-comment factual errors**: env 5 NATS block reads *"clustered broker with JetStream-style durability"* — recipe uses core NATS pub/sub with queue groups, not JetStream; env 1 workerdev block reads *"The tsbuildinfo is gitignored, so the first watch cycle always emits dist cleanly"* — `nest start --watch` uses ts-node, tsbuildinfo only affects prod `nest build`. **Cross-surface duplication**: same facts across 3–4 surfaces (.env shadowing: 3 surfaces; forcePathStyle: 4 surfaces; tsbuildinfo: 4 surfaces with factual error on one). Env 4 comments genuinely v7-gold, internally consistent (v25 app-static contradiction fixed). Env READMEs remain 7-line template boilerplate (no teaching at all — next missed surface). **Rating**: S=A, C=**C+** (not A−; honest audit reveals ~33% genuine-platform-teaching rate, 1 folk-doctrine defect, 2 env factual errors, pervasive cross-surface duplication, wrong-surface items shipped as gotchas; env 4 comments + most IG items prevent a lower grade), O=A− (85 min wall ≤90, 1.6 min main bash, 0 very-long, 0 MCP schema errors), W=A− (5 subagents clean, v8.90+v8.93 held) → **min = C+**. v28 surfaces that the content-authoring mental-model is the next load-bearing fix. The root cause is structural: the agent that debugged for 85 min writes self-narrative, not reader-facing content; token-level checks ("names a mechanism + failure mode") are trivially satisfied by journal entries. The answer is to decouple recording from authoring — see [docs/spec-content-surfaces.md](spec-content-surfaces.md) for the surface contracts + classification taxonomy + citation map, and [docs/implementation-v8.94-content-authoring.md](implementation-v8.94-content-authoring.md) for the concrete plan: (1) `zerops_record_fact` becomes mandatory during deploy, (2) fresh content-authoring subagent reads facts log + zerops_knowledge + surface contracts (NOT the run transcript) and writes all six surface types by classifying each fact and routing it to exactly one destination, (3) citation requirement for any gotcha whose topic matches a `zerops_knowledge` guide, (4) env READMEs become 40–80 lines of tier-transition teaching not boilerplate, (5) scaffold pre-flight traps list to stop recurrent run-incidents becoming recurrent gotcha candidates (self-shadow, URL-embedded creds, forcePathStyle, 0.0.0.0 bind, trust proxy). Plus v8.85 check-surface audit: `env_self_shadow` must enumerate every hostname at generate-complete (currently skips worker). |
| v29 | v8.94 content-authoring subagent ships — gotcha-origin ratio jumps from 33% to 79% genuine; env-README surface inherits the folk-doctrine failure mode |
| v30 | v8.94 reproducibility run — v8.95 plan documented but NOT IMPLEMENTED; gotcha-origin peaks at 79% → **88% genuine** (new record), 0 DISCARD overrides (v29: 2/14), 0 folk-doctrine, 64 min wall (new all-time fastest complete run); persisting defects = unshipped v8.95 Go-template fixes (11 env-README factual drifts reproduce byte-for-byte from v29) + 1 close CRIT (worker SIGTERM handler missing — scaffold pre-flight preamble not shipped) | **v8.94 architecture works for gotchas, migrates the problem to env READMEs.** Wall 83 min (v28: 85, v25: 71). 378 assistant events / ~534 tool calls / main bash 4.3 min / 0 very-long / 8 errored / 6 subagents (scaffold×3 + feature×1 + **fresh-context README/env/CLAUDE writer×1** + code-review×1). **10 `record_fact` calls** (v28: 3) — `zerops_record_fact` hit load-bearing cadence. Content: apidev 282 / appdev 174 / workerdev 258 README lines; gotchas 5/4/5; IG 5/4/3; CLAUDE.md 5,257 / 4,218 / 4,591 bytes (all ≥ 1200 floor + 3+ custom sections). Close review: **0 CRIT / 0 WRONG / 4 STYLE** (cleanest since v22). All v8.90+v8.93 calibrations held; substep attestations real-time in canonical order; single-round README fix loop (3 content-check fails → 3 min inline fix → pass). Root README intro names real services (v17 `dbDriver` holds for 9th run). v8.94 writer brief 12 KB, explicitly marked *"You have no memory of the run that dispatched you"*, points at `/tmp/zcp-facts-{session}.jsonl`, includes tool-use policy + pre-classified fact taxonomy + list of platform-invariant gotchas to actively consider. Writer returned structured classification tallies: 7 invariant / 2 intersection / 5 framework-quirk (4 discarded) / 2 library-meta / 1 scaffold-decision / 5 operational / 2 self-inflicted (1 reframed, 1 discarded). **Gotcha-origin audit**: 14 total — **11/14 (79%) genuine platform teaching** (apidev MinIO `forcePathStyle`, Valkey no-auth URL trap, presigned-URL external host, execOnce-exit-0 contract; appdev Vite `allowedHosts`, `./dist/~` tilde, `VITE_API_URL` bake-time; workerdev queue group, SIGTERM drain, portless-no-readiness, NATS URL-cred cross-ref) + 2 wrong-surface (apidev feature-sweep-healthCheck-bare-GET + appdev Multer-FormData-Content-Type: brief explicitly said DISCARD for both, writer kept them anyway) + 1 borderline (worker `@MessagePattern` fires for fire-and-forget — defensive clarification). **Major improvement vs v28's 33% genuine / 1 folk-doctrine**. **Three confirmed defect classes shipped**: (1) **`apidev/scripts/preship.sh` recipe-infrastructure leak (2,840 bytes, 12 pre-ship assertions)** — apidev scaffold subagent authored the file for its own pre-ship verification, committed via `git add -A`, never cleaned up. Lines 58-63 contain assertions meaningful ONLY while the main agent is assembling the recipe: `fail "README.md must not be written by scaffolder"` + `fail "zerops.yaml must not be written by scaffolder"`. A porter cloning the recipe sees these. asymmetric: appdev + workerdev scaffold subagents used inline `ssh ... "set -e; ..."` chains and shipped nothing. Code-review subagent saw the file and said *"recipe-level self-check script — out of scope for code review"* — acknowledged and ignored rather than flagging. Different subclass of v21's `node_modules` leak (2.8 KB vs 208 MB, different type of leaked content, same class: scaffold-phase artifact in published deliverable). (2) **env 0 README folk-doctrine fabrication on cross-tier data persistence** — `environments/0 — AI Agent/README.md` lines 26 + 33 ship the claim *"Data persists across tier promotions because service hostnames stay stable — you can iterate here and move up without rebuilding the database from scratch"* and the adjacent *"Existing data in DB / cache / storage persists across the tier bump because hostnames are identical."* Factually wrong for the default `zerops_import` path: env 0 is project `nestjs-showcase-agent`, env 1 is project `nestjs-showcase-remote`, env 4 is `nestjs-showcase-small-prod` — six distinct `project.name` values, so deploying a different env's `import.yaml` creates a NEW project; hostnames colliding inside a new project does nothing to bridge data. This is the v23 "execOnce burn" / v28 "timing-dependent interpolator" defect class — in the env-README surface that the gotcha-authoring taxonomy doesn't reach. Scope confirmed: 2 lines in env 0 README; env 1-5 READMEs do NOT repeat the claim. (3) **env 3/4/5 README `minContainers` factual drift — origin attributed correctly via v8.95 second dry-run: Go templates, not writer behavior.** Every broken line traces to a hardcoded string in `internal/workflow/recipe_templates.go`: `envPromotionPath(3)` line 324 → env 3 README L32; `envDiffFromPrevious(4)` line 279 → env 4 README L24; `envPromotionPath(4)` line 330 → env 4 README L33; `envOperationalConcerns(4)` line 370 → env 4 README L41; `envDiffFromPrevious(5)` line 285 → env 5 README L20. The writer's env-README output (per its brief, at `environments/{N — writer-label}/README.md`) is ORPHANED — `OverlayRealREADMEs` only overlays per-codebase READMEs, NOT env READMEs; `BuildFinalizeOutput` always writes Go-template content to `environments/{EnvFolder(N)}/README.md`. v8.94 introduced the 40-80-line env READMEs by adding these `switch envIndex` arms; v29 is the run that surfaced the factually-wrong claims hardcoded in them. Plus **env 4 + env 5 YAML app-static comments each contain an internal contradiction**: *"minContainers:2 on a static service is not needed"* directly above `minContainers: 2` — same class as v25's env 4 app-static contradiction. Not caught by `factual_claims` because the regex catches numeric disagreement and both sides read "2"; semantic contradiction. Finalize `factual_claims` DID catch 4 real import.yaml-comment defects (env 0/1/2 storage 2GB↔objectStorageSize:1; env 4 worker comment 1↔2) but doesn't scan env README PROSE. **`preship.sh` (real writer-layer defect) + Go-template env-README defects (real Go-source defect) are the concrete things v30 needs to close.** v8.95's three fixes: (A) scaffold-brief hygiene rule + generate-step leak check [writer-layer]; (B) edit `recipe_templates.go` lines 172, 279, 285, 305, 324, 330, 370 directly + add regression tests pinning claims to YAML truth [Go-source]; (C) structured `ZCP_CONTENT_MANIFEST.json` writer-output contract for DISCARD enforcement on per-codebase gotcha surfaces [writer-layer]. v29 post-mortem + v8.95 implementation plan at [docs/implementation-v8.95-content-surface-parity.md](implementation-v8.95-content-surface-parity.md). **Rating**: S=A, C=**B−** (79% gotcha-origin genuine is a real step up from v28's 33%, BUT three defect classes in non-gotcha surfaces: scaffold-infra leak + env-0 fabrication + env-README factual drift; writer overrode brief's DISCARD on 2/14 gotchas; env 4 comments still mostly v7-grade, apidev CLAUDE.md substantive, close review spotless — these lift from C+ to B−), O=A (83 min ≤ 90, 4.3 min main bash ≤ 10, 0 very-long, 0 MCP schema errors, 0 exit-255), W=A− (6 subagents clean including v8.94 fresh-context writer, real-time substep attestations, 10 record_fact calls, 1-round content-fix; writer-kept-discarded flag docks from A to A−) → **B−**. |
| v31 | **v8.95 ships + first A− since v20** — all three v8.95 fixes held (scaffold artifact leak prevented, Go-template env-README drifts eliminated byte-for-byte, ZCP_CONTENT_MANIFEST contract delivered); content quality at new peak (100% gotcha-origin genuine, 19 IG items, largest README+CLAUDE.md total ever); two structural convergence loops surface as next load-bearing limiter (deploy-complete README checks 3-rounds, finalize env checks 3-rounds); v8.96 plan shipped targeting author-side rule access + cross-subagent facts routing | **v8.95 validation clean; v31's A− grade is the first since v20's A.** Wall 86 min (v30: 64, v28: 85); **100% gotcha-origin genuine (9/9)** — new peak (v30: 88%, v29: 79%, v28: 33%); **0 DISCARD overrides**, 0 folk-doctrine, 0 fabrications. README lines peak on all three codebases (354/203/247 vs v30's 263/161/165); 19 IG items total (v30: 9) — apidev's 8 matches v22's peak. CLAUDE.md also peaks (6298/4907/5044 bytes). 6 subagents (scaffold×3 + feature + v8.94 fresh-context writer + code-review). Main bash **1.3 min / 0 very-long / 2 errored** — cleanest operational since v18. 17 zerops_knowledge calls (peak — writer-subagent loading platform context). 8 zerops_record_fact calls (load-bearing). Close review: **1 CRIT (apidev missing `app.enableShutdownHooks()` — NEW subclass not covered by v8.95 Fix #4 worker-SIGTERM preamble) + 2 WRONG + 2 STYLE** — all fixed inline by code-review subagent. v17 `dbDriver` validation holds for 10th consecutive run. Preprocessor directive on all 6 envs. **v8.95 calibration bar 4/5 met**: (✅) 0 env-README drifts, (✅) 0 cross-tier persistence fabrications, (✅) 0 scaffold-phase artifact leaks, (❌) 1 close CRIT from NEW mandatory-handler subclass (apidev gateway-drain class), (✅) gotcha-origin ≥ 80%. **Runtime incidents (3, all self-healed)**: (1) workerdev NATS_PASS/NATS_PASSWORD env-name mismatch → `AUTHORIZATION_VIOLATION` fixed via rename, correctly classified as self-inflicted DISCARD; (2) appstage 502 from scalar `deployFiles: build/~` → tilde-unwrap only fires for list items, fixed via `- build/~` list form, **shipped as genuine appdev gotcha with citation** (new discovery this run); (3) browser-tool syntax minor friction. **Top structural limiters identified via timeline.py deep-dive (~6 min cumulative)**: (A) **deploy-complete 3-round README check loop (~2 min)** — `comment_ratio` check reads YAML INSIDE apidev/README.md's integration-guide fragment, not on-disk `zerops.yaml`; agent learned this the hard way on round 2. (B) **finalize 3-round env-comment loop (~4 min)** — 11 violations across 6 env yamls (`comment_ratio`, `cross_env_refs`, `factual_claims`, `comment_depth`); main rewrote all 6 yamls 3× to converge. (C) **Cross-subagent duplicate framework archaeology (~80s)** — scaffold investigates Meilisearch v0.57 API, feature re-investigates same API, code-review re-investigates svelte-check@4/typescript@6 mismatch; none ship as content; each subagent re-discovers because facts log is writer-only. (D) **`zerops_knowledge` schema-churn (~15s, 5 errors)** during appstage 502 recovery — tool-ergonomic concern. **Root cause analysis**: limiters (A)+(B) share the SAME asymmetry — gates know what they read, what they couple, and what to change; authors only know what to write. v8.86's rules-in-brief pattern applies to the readmes-writer subagent but doesn't extend to main-agent authoring (finalize) or to gate-failure-recovery (deploy comment_ratio). Limiter (C) is a separate asymmetry — facts log is single-reader (writer-only). v8.96 plan at [docs/implementation-v8.96-author-side-convergence.md](implementation-v8.96-author-side-convergence.md) addresses both with two structural changes: (1) extend `StepCheck` with optional `ReadSurface`/`Required`/`Actual`/`CoupledWith`/`HowToFix` fields + aggregate `NextRoundPrediction` heuristic on `StepCheckResult`, migrate P0+P1 checks (deploy `comment_ratio`, env-finalize `comment_ratio`/`comment_depth`/`cross_env_refs`/`factual_claims`, README `knowledge_base_authenticity`/`gotcha_distinct_from_guide`/`fragment_intro_blank_after_marker`) so every gate emits its own self-describing failure — predicted to collapse both convergence loops to 1-round; (2) add `Scope` field to `zerops_record_fact` (content/downstream/both) + `IncludePriorDiscoveries bool` on `GuidanceTopic` + hook `BuildPriorDiscoveriesBlock` at `buildSubStepGuide` topic-resolution time → prepends block to feature + code-review subagent briefs, routing scaffold-recorded framework quirks downstream without polluting published content. Additive extensions (no new tool surface, no dry-run flag, no hardcoded check-specific logic). **Rating**: S=A, C=**A−** (100% gotcha-origin is new peak, largest published content ever, 0 drifts/fabrications; gotcha count at floor 3/3/3 — writer consolidated facts into IG items; minor env 5 README framing on queue-group), O=**A** (86 min ≤ 90, 1.3 min main bash, 0 very-long), W=**A−** (5 MCP schema errors on zerops_knowledge during one window; close CRIT apidev enableShutdownHooks class not in scaffold pre-flight preamble) → **A− overall — first since v20**. |
| v32 | v8.96 ships (Themes A+B structured check failures + cross-subagent facts bus + 4 quality fixes) — hits operational target, surfaces five new structural defects that become v8.97+v8.98 scope | **Mixed run: v8.96 themes held partially, but close step never completed and exported deliverable is incomplete.** Wall **70 min** (hit v8.96 target band 66-76). 296 main assistant events, 49 main bash calls, **13.4s main bash (new low for the v30-era pattern)**, 0 very-long. 5 subagents. Deploy content-fix: **still 3 rounds** (v8.96 Theme A target was 1) — first round hit 10+ formatting + token-collision defects; third round finally passed. Finalize 2 rounds (improved from v31's 3). Close step **DID NOT COMPLETE** — TIMELINE narrates up through stage feature-sweep, then "Close (pending) — Static code review sub-agent + optional publish still to run." `zcp sync recipe export` fired anyway (no close-gate before v8.97). **Published deliverable is incomplete**: `/var/www/zcprecipator/nestjs-showcase/apidev/README.md` + `appdev/README.md` + `workerdev/README.md` + all three `CLAUDE.md` files are **MISSING** from the exported tree despite the writer sub-agent authoring them (TIMELINE: "root README 35 lines, apidev 303 / appdev 162 / workerdev 218 lines + CLAUDE.md operational guides"). Spurious `environments/README.md` also shipped — byte-identical duplicate of the root README (2359 bytes, same content). Classic early-export trap: the writer's files lived on the SSHFS mount but the export path walked only the canonical `BuildFinalizeOutput` tree which does NOT include per-codebase README overlays before `OverlayRealREADMEs` runs at close-complete. **execOnce burn-on-workspace-create surfaces again** — apidev seed returned ✅ in 66ms with 0 rows inserted on the first code-bearing deploy. Workspace-create no-op deploy had already burned the `${appVersionId}` key (same class as v23/v28); agent manually re-seeded via SSH and shipped a gotcha about it. Root cause is the seed key-shape choice, not the platform — see v8.104 Fix B (`bootstrap-seed-v1` static key). **Read-before-Edit rule lost across 3 scaffold subagents** (per recipe.md:845 statement explicitly calling out "v32 lost the Read-before-Edit rule across three scaffold subagents") — main-agent dispatch compression dropped the load-bearing rule. **TypeORM entity-table mismatch** (worker `@Entity({name:'job'})` vs API migration `jobs`) — caught by feature subagent mid-run, fixed, shipped as a workerdev gotcha with correct framing ("TypeORM silent no-errored on missing table"). **`agent-browser` synthetic-click miss on Svelte 5 `onclick`** — DB UPDATE didn't fire from click; pipeline verified via curl (276ms round-trip) — documented as platform-observation. **v8.96 calibration partial pass**: (✅) wall band hit, (✅) Theme B Scope=downstream field used (2 downstream-scope facts recorded, per TIMELINE), (❌) 3 deploy rounds not 1, (❌) zero cross-subagent archaeology not verified (subagent traces not deep-audited), (❌) close didn't complete. **Rating**: S=**C** (close never completed; per-codebase content missing from deliverable is v10-class catastrophe), C=**C** (writer produced good content but it didn't ship; env READMEs + env YAMLs correct; spurious environments/README.md duplicate), O=**A−** (70 min wall on target, 13.4s main bash, 0 very-long, 0 schema errors), W=**D** (close never completed, premature export, dispatch compression dropped Read-before-Edit, 13 workflow-rejection errors from ordering confusion) → **C overall**. v8.97+v8.98 response bundles the fixes: close-gate on export (Fix 1), close verify-only sub-steps + publish out of workflow state (Fix 2), MANDATORY sentinels wrapping file-op sequencing / tool policy / SSH-only executables with byte-identical-transmission rule (Fix 3 A+B), surface-derived coupling `StampCoupling` (Fix 4), 6 platform principles in scaffold MANDATORY block (Fix 5), feature-subagent principle sustain (v8.98 Fix A), export/publish in NextSteps post-completion (v8.98 Fix B — later reverted by v8.103 after v33 showed auto-export was a user-objection trigger), close sub-step ordering gate (v8.98 Fix C: reject close-browser-walk until code-review complete). |
| v33 | v8.102.0 validation run (v8.96 + v8.97 + v8.98 + v8.99 + v8.100 compound) — content body at peak quality (18/18 genuine gotchas, env 4+5 YAML v7-gold); two agent-invention defects ship to `/var/www/` (phantom `recipe-nestjs-showcase/` parallel output tree with 7 orphan files under paraphrased env folder names; auto-export triggered at close per v8.98 Fix B framing) plus 9-min feature-subagent diagnostic panic burst and persistent 3-round deploy + 2-round finalize convergence; v8.103 closes the two phantom-tree + auto-export defects immediately, v8.104 guidance-hardening plan targets the remaining six agent-invention classes | **Operationally the cleanest run on record, but the deliverable ships with two agent-invented defects alongside the canonical output.** Build: v8.102.0 (commit `b65a282`), session `b03f3ac88ddf3a10`. Wall **88 min 29s** recipe work (07:04:18 → 08:32:47 canonical close) + ~21 min post-close noise. **Main bash 30.1s / 0.5 min — new all-time best** (prior v25: 30.9s, v31: 78s). 0 very-long main, 0 long main, **0 MCP schema errors** (v8.96 Fix #1 held — v31 had 5). 0 "File has not been read yet" errors (Fix #3 held across main + all subagents — the 16 text occurrences were rule quotations in briefs, not runtime failures). 0 `.git/index.lock` contention (Fix #4 held — the 7 text occurrences were also rule quotations). 0 SUBAGENT_MISUSE rejections (v8.90 held — the 4 text occurrences were rule text). 4 main is_error tool_results (1 real: `ssh host "git add"` before `git init` exists → `fatal: not a git repository` because scaffolds deleted `.git/` per Fix #4; 2 cascade parallel-cancellations; 1 shell-list exit 2). Substep attestations **real-time in canonical order** (v8.90 held). 6 subagents — scaffold appdev (2:18) + apidev (6:41, hit Meili-ESM build failure at v0.47+, pinned to `meilisearch@0.45.0` with class `MeiliSearch` capital-S, recorded as `fix_applied` fact) + workerdev (3:35) + feature (18:37 — including ~9 min diagnostic panic burst at 07:29–07:38) + readmes/CLAUDE writer (8:34) + code-review (2:54). 21 `zerops_record_fact` calls across the run (new peak; v31: 8). **Theme A working**: failed checks emit `readSurface` / `required` / `actual` / `coupledWith` / `howToFix` / `nextRoundPrediction` — confirmed by parsing tool-result payloads (e.g. *"embedded YAML in api/README.md (#ZEROPS_EXTRACT_START:integration-guide fragment)"*, *"5 — Highly-available Production/import.yaml line 20 (comment) vs line 30 (`minContainers` field)"*, `nextRoundPrediction` distribution: `multi-round-likely` ×4 + `single-round-fix-expected` ×2). **Theme B first validated adoption** — **3 facts with `scope=downstream`** (1 from main, 1 apidev-scaffold, 1 feature subagent) — v32 calibration bar #7 passed. **v8.95 Fix B still holding**: env 0 README correctly says *"Service state (DB rows, cache entries, stored files) does NOT carry across tiers by default"* (v29/v30 had the opposite fabrication); env 3/4/5 READMEs carry correct `minContainers:2` semantics byte-identical to v31; zero Go-template drifts. **Content body is peak**: apidev 320 / appdev 199 / workerdev 191 README lines (v31: 354/203/247 — compressed ~10% but not regression-compressed). Gotchas 8/5/5 (total 18; v31: 3/3/3 total 9). IG items 6/5/7 (total 18 — matches v31's 19). CLAUDE.md 3344 / 2788 / 3822 bytes — smaller than v31's 6298/4907/5044 but still clear 1200-byte floor with 3+ custom sections each. **Gotcha-origin audit: 18/18 = 100% genuine platform teaching** (matches v31 peak): apidev's `GET /api/search → 500 Index 'items' not found on the second deploy` (exact-failure-mode + mechanism), `ioredis.ping() NOAUTH` (exact error string), NATS URL-delimiter parse bug (`${queue_password}` containing `@`, `:`, `/`), MinIO `forcePathStyle` 404, Meili `masterKey` vs `defaultSearchKey`, Valkey no-auth, shutdown-hooks dangling-sockets, trust-proxy VXLAN IP; appdev's Vite `server.host: '127.0.0.1'` VXLAN unreachable, `allowedHosts Blocked request`, `VITE_*` bake-vs-run, `./dist/~` tilde, dual-runtime URL pattern; workerdev's queue-group double-process at `minContainers>1`, SIGTERM drain (3-step: `sub.drain() / nc.drain() / app.close()`), `TypeOrmModule.forFeature` for `createApplicationContext`, ports-on-worker deploy-validation, cold-deploy ordering `relation "job" does not exist`. Zero folk-doctrine. Zero wrong-surface. Zero library-meta as gotcha. **Env 4 + env 5 YAML comments at v7-gold standard**: two-axis `minContainers` teaching intact on every runtime service (app static explicitly "NOT throughput scaling, pure availability play"; api both-axes; worker queue-group linear throughput); no self-contradictions (v25/v29's app-static contradiction class did not recur); per-service WHY on db/redis/queue/storage/search. **Close review: 0 CRIT / 2 WRONG / 1 STYLE** — scaffold e2e test expecting HTTP on no-HTTP worker + unused Express deps on worker + StatusPanel `data-feature="status"` overloading the feature-coverage namespace (renamed to `data-panel="status"`). All three fixed inline. v31's apidev `enableShutdownHooks()` CRIT class did NOT recur (both apidev and workerdev ship it this run). v17 `dbDriver` validation holds for 11th consecutive run. Preprocessor directive on all 6 envs.\n\n**Six defects that shipped to `/var/www/` (not the canonical zcprecipator tree, but visible to the user):**\n\n1. **Phantom `/var/www/recipe-nestjs-showcase/` tree with paraphrased env folder names** — writer sub-agent wrote 7 orphan files: root `README.md` + `environments/{0 — Development with agent, 1 — Development, 2 — Review, 3 — Stage, 4 — Small production, 5 — HA production}/README.md`. Those strings don't exist anywhere in recipe.md; main-agent synthesized them when constructing the writer dispatch prompt. Inspection of the dispatch text at offset 2161 + 4694 shows main literally wrote *"Output root for env READMEs + root README: /var/www/recipe-nestjs-showcase/"* + enumerated the paraphrased env names as if they were canonical. Writer followed faithfully. `BuildFinalizeOutput` wrote the correct canonical tree to `/var/www/zcprecipator/nestjs-showcase/` from Go templates. Both trees coexisted in the deliverable. Code-review sub-agent saw the phantom tree and explicitly said *"out of scope for code review"* — acknowledged and ignored. **Root cause: the content-authoring-brief section (recipe.md:2358) leaves output paths + env folder names as free parameters in the brief that main-agent synthesizes.** v8.103 closed the close-section-level guard (`TestCloseSection_SingleCanonicalOutputLocation`); v8.104 Fix A adds a MANDATORY sentinel inside the writer-brief forbidding "output root" / "env folder names" as parameters.\n\n2. **Auto-export at close** — v8.98 Fix B framed export as autonomous NextSteps[0]. Main agent ran `zcp sync recipe export --help` at 08:51:22 followed by three export invocations through 08:54:03 immediately after `complete step=close`. User objected to ANYTHING running unprompted after close, not just publish. **v8.103 reverts**: both export AND publish now "ON REQUEST ONLY", summary explicitly says workflow is done at close.\n\n3. **Feature sub-agent 9-minute diagnostic panic burst (07:29:00 → 07:38:16)** — started with 4 sensible `ssh apidev "cat node_modules/.../package.json"` probes (all succeeded). At 07:29:14 pivoted to ~80 parallel bash-tool calls probing whether basic shell + SSH even worked: `ls`, `ls -la`, `ls /var/www`, `stat /var/www`, `echo HELLOWORLD`, `printf "END\n"`, `ssh -vvv apidev 'echo ok'`, `find / -name config`, `cat /etc/os-release`, `which ssh`. Latencies on trivial commands hit 300+ seconds because harness concurrency queue saturated. Feature work productively started at 07:38:16. **No host-confusion, no tool actually broke — agent convinced itself something was wrong and ran out the clock proving it wasn't.** No existing guidance in recipe.md covers diagnostic-probe cadence. v8.104 Fix D adds to feature-subagent MANDATORY: "≤3 targeted probes per ambiguous signal; no parallel-identical probes; stop and report if three don't resolve."\n\n4. **Unicode box-drawing separators in zerops.yaml comments** — apidev/appdev/workerdev all shipped with `# ──────────────────────────────────────────────────────────────` dividers around section headers. Pure agent stylistic invention. v20-v32 all used plain `# Comment text` (v25 used `# ----- Title -----` with dashes once). recipe.md:2914 covers comment voice but is silent on visual decoration. v8.104 Fix C adds "Visual style" subsection forbidding Unicode box-drawing, ASCII dividers, emoji.\n\n5. **3-round deploy content-fix + 2-round finalize content-fix** — v8.96 Theme A target was ≤1 round each. Deploy attempted at 08:10:03 (fail: 3× `fragment_intro_blank_after_marker`, `app_comment_specificity` 24%, `api_comment_specificity` 14%, `api_comment_ratio` 29%, `app_integration_guide_per_item_code`, 3× `gotcha_distinct_from_guide`, `cross_readme_gotcha_uniqueness`), 08:16:47 (still one cross-README Jaccard overlap: *"Public subdomain returns 502 with empty body"* apidev vs *"Public subdomain returns 502 when Vite uses loopback"* appdev), 08:17:11 passed after appdev stem reworded. Root cause: fixing one check reshuffles similarity across READMEs and trips `cross_readme_gotcha_uniqueness` with a NEW collision. Theme A names what to read + fix on THIS surface but not WHICH OTHER CHECKS will newly fail from the fix. v8.104 Fix E adds `PerturbsChecks []string` to `StepCheck` naming sibling checks whose pass state likely flips.\n\n6. **Pre-init `git commit` sequencing gap** — at 07:19:31 main agent ran `ssh appdev "git add -A && git commit -q -m 'initial scaffold'"` → `fatal: not a git repository`. Scaffold subagents had deleted `/var/www/.git/` per v8.96 Fix #4 (framework-scaffolders' auto-`git init` collides with main's canonical init). Main recovered 6s later with `git init -q -b main && git add -A && git commit`. recipe.md block `git-config-mount` instructs pre-scaffold init but doesn't carry the post-scaffold "re-init before commit" note. v8.104 Fix F adds explicit sequencing.\n\n**Plus the recipe-pattern defect that's been in every version since v20**: `zsc execOnce ${appVersionId} --retryUntilSuccessful -- npx ts-node src/seed.ts`. `${appVersionId}` changes every deploy, so seed runs every deploy; only the in-script `if (count > 0) return` short-circuit prevents duplicate rows. That short-circuit is the literal cause of v33 apidev gotcha #7 — `GET /api/search returns 500 Index 'items' not found on the second deploy` — because the Meili `addDocuments(...)` call lives INSIDE the skipped branch. We ship a gotcha documenting a bug in our own recipe pattern. v8.104 Fix B prescribes `zsc execOnce bootstrap-seed-v1 -- seed` (static key, once per service lifetime) separately from `zsc execOnce ${appVersionId} -- migrate` (per-deploy, idempotent by design). Eliminates gotcha #7 at the source.\n\n**Rating**: S=**A** (all 6 steps completed, all 5 features on dev+stage, both browser walks fired, 0 CRIT shipped after close-step fix), C=**B+** (100% gotcha-origin is peak, env 4+5 YAML v7-gold, CLAUDE.md clean, no folk-doctrine, no drifts — BUT phantom `/var/www/recipe-nestjs-showcase/` tree with 7 orphan files + Unicode box-separator invention in zerops.yaml + auto-export-at-close all ship visible to the user; these drop from A− to B+), O=**A** (88 min wall ≤ 90, 30.1s main bash = all-time best, 0 very-long main, 0 MCP schema errors, 0 exit-255, 4 main is_error — mostly 1 real + 3 cascade), W=**B** (writer dispatch constructed with hallucinated output path + env names, 9-min feature-subagent diagnostic panic burst, 3-round deploy + 2-round finalize, pre-init git commit sequencing, auto-export-at-close per v8.98 Fix B framing) → **B overall** — below v31 (A−), above v29/v30 (B−). The ops substrate is stable-through-pristine; the recipe-pattern layer and the brief-construction layer are the load-bearing problems. v8.103 already fixes (1)+(2); v8.104 guidance-hardening at [docs/implementation-v8.104-guidance-hardening.md](implementation-v8.104-guidance-hardening.md) addresses the remaining four plus the execOnce-seed recipe-pattern bug. |

---

## Architectural insights — the v20 → v23 trajectory and v8.86's shape

By v23 the cumulative pattern across versions had become legible: each version since v20 added quality gates and dispatch machinery on top of the previous, none restored v20's convergence properties, and each "patch" introduced its own failure class. v23's post-mortem dialogue produced four insights that the v8.86 plan is built on. Capturing them here so future readers (human or new agent instance primed on this doc) don't re-derive them.

### Why each version after v20 regressed (the shape story)

v20 ran with two patterns that v23 lost:

- **Author-then-iterate INSIDE Generate.** README writing happened during Generate alongside scaffolds. Fix subagents fired during generate when content checks failed, but the surface was small (only scaffold + plan content) so iteration was fast — 2 fix-subagent rounds, ~5-8 min total.
- **Find-then-fix-then-verify as one subagent at Close.** Code-review found bugs. A dedicated `close-step critical-fix` subagent applied fixes, redeployed all 3 services, cross-deployed to stage, and re-verified — all in one delegated cycle. Main agent stayed orchestrating, never absorbed the redeploy/cross-deploy plumbing.

v23 inherits neither:

- **Author-during-Deploy with downstream re-dispatch.** README writing moved to deploy.readmes substep (to capture deploy-discovered facts — see "Why deep content requires late writing" below). v8.81 added a `content_fix_dispatch_required` gate that re-dispatches fix subagents on writer failure. Each round is a multi-minute round trip. v23 ran 5 rounds (writer + 3 fix subagents + main inline) where 1 should have sufficed, costing 23 minutes of the 119-minute total wall.
- **Find-bugs-and-fix-inline at Close, redeploy on main.** v22 collapsed the close-critical-fix subagent split. v23 inherits the collapsed shape — code-review subagent finds AND fixes; main agent absorbs ~10 minutes of redeploy + cross-deploy orchestration that v20 had delegated.

These two shifts happened structurally between v20 and v22 (recipe.md spec evolution), not by agent choice in v23. The v23 agent was operating within a workflow that had already lost v20's convergence properties — it can't restore them by behaving differently. **v23's "feels wrong" property is a workflow-architecture residue, not an agent-behavior bug.**

### The math of why "add more checks" can't reach 98%

Each content-quality check shipped at ~95% writer-pass-rate (writer ships clean for that specific check 95% of the time). Probability of clean-on-first-write = P^N where N is the active check count and P is the per-check pass-rate.

| Version | Active checks (~) | P(clean on first write) | P(needs ≥1 fix round) |
|---|---:|---:|---:|
| v20 | 10 | 60% | 40% |
| v22 | ~14 | 49% | 51% |
| v23 | 17 | 42% | 58% |

**Adding checks mathematically lowers the probability of clean shipping.** v8.81's "fire a fix subagent on failure" makes each failure cost worse (multi-minute subagent dispatch instead of inline fix). The trajectory v20 → v23 has been adding machinery on top of machinery without adding determinism. The way past 95% can't be more checks — it has to be putting the checks in front of the writer instead of behind it.

### Why deep content REQUIRES late README writing — this part of v21+ stands

The original reason README authoring moved from Generate to deploy.readmes is **correct and stays**. A Generate-time README has access only to plan + scaffold + zerops.yaml. It does NOT have access to:

- That `module: nodenext` + raw ts-node breaks at first dev start (workerdev-class issue)
- That apidev `/api/status` returns flat `{db,redis,nats,storage,search}` while a naive frontend assumes nested `services[]` (StatusPanel-class contract drift)
- That `cache-manager@6` returns absolute-epoch TTLs, not relative durations (cache-TTL-class semantic shift)
- That Svelte 5 `$state` proxy doesn't react to `[created, ...items]` patterns
- That stage subdomains hit CORS preflight differently than dev
- The shape of every cross-codebase contract once features actually wire up

These are the v21+ "deep content" gotchas that v23's content audit shows at 38% pure-invariant origin (vs v18-era's 0%) precisely because they're authored after deploy-discovered ground truth surfaces. **You can't write them at Generate. They emerge during Deploy.** The user porting their own NestJS app is going to hit these same surfaces, and the README has to teach them — which means the README has to be authored after the recipe agent has SEEN them.

The user-side recipe quality bar v21 was pushing toward is real and worth preserving. Rolling back to v20-style early-Generate README writing would re-introduce the decorative-content drift v8.78 was right to address.

### The fix is to invert verification direction

Today (v23):

```
writer ships → external gate verifies → if fail, dispatch fix subagent → loop
```

Multi-round, multi-minute per round, briefs reconstructed each round, truncated findings hide leftover work, "be surgical" framing prevents exhaustive sweeps.

Target (v8.86):

```
writer prepares input → writer self-verifies against rules → writer ships clean
                                                          → external gate confirms
```

Single round in the success case. The external gate becomes a confirmation step that runs but rarely fires. Two preconditions make this work:

- **Pre-collected facts** — writers operating late in the workflow (the README writer at deploy.readmes) need pre-organized input rather than 90 minutes of session log to do archaeology against. v8.86 ships a `zerops_record_fact` MCP tool the agent calls during deploy substeps when relevant facts emerge; the structured facts log feeds the writer.
- **Rules-as-runnable-validation** — every check rule the gate will run gets translated into a concrete pre-return validation command (grep, awk, ratio computation) the writer executes against its own draft before returning. The writer iterates internally until clean.

The check inventory v23 has is the right inventory — what's wrong is WHERE in the pipeline verification happens.

### Why we picked the lower-risk shape (calibrated commitment)

The dialogue that produced v8.86 considered two viable shapes:

| Shape | Change size | Quality preserved | Convergence speed | Risk |
|---|---|---|---|---|
| Self-verifying writer + facts log (v8.86 stage 1) | MEDIUM | Yes (deep content, late writing) | Medium-high | Low |
| Per-substep distributed fragment authoring (v8.87+ stage 2) | LARGE | Yes (each fragment authored at moment of freshest knowledge) | High | Medium-high (untested architecture, new failure classes possible) |

The calibrated commitment: **one architectural change per release**. v8.86 establishes a working baseline. v24 evidence either justifies stage 2 (writer's self-validation loop still has ≥3 internal rounds; deploy.readmes substep ≥8 min) or proves it unnecessary. The trajectory of "commit to a bigger architectural change because the previous one didn't work" is what produced 5 failing two-hour sessions across v21→v22→v23. v8.86 inverts that pattern.

This is also why the original v23-postmortem §7 list (truncation removal, brief-framing tweaks, embedded-yaml docs) was rejected: those addressed the symptoms, not the load-bearing cause (external gate + dispatch is anti-convergent by construction). The smaller-than-stage-2 but bigger-than-symptom-list shape is v8.86.

### Reading order for a new instance primed on this doc

1. Read this section first to get the architectural mental model
2. Read the [milestones-and-regressions table](#milestones-and-regressions-by-version) for the per-version trajectory
3. Read the v23 per-version entry below in detail for the load-bearing failure-mode evidence
4. Read [implementation-v8.86-plan.md](implementation-v8.86-plan.md) for the concrete fix plan
5. Optionally: [implementation-v23-postmortem.md](implementation-v23-postmortem.md) for the analytical deep-dive (note its §7 list is superseded — see top-of-file redirect)

The single most important sentence in this section: **the convergence problem is at the gate layer, not the writer layer; the fix moves verification from after the writer to inside the writer's brief**. Every v8.86 fix flows from that.

---

## Per-version log

### v6 — first full 3-codebase run

- **Date**: 2026-04-10
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Model**: claude-opus-4-6 (pre-model-gate; inferred)
- **Session logs**: none
- **Wall / asst events**: unknown
- **Bash metrics**: unknown

**Content metrics** (apidev / appdev / workerdev):
- README lines: 293 / 158 / — (workerdev README missing)
- Gotchas: 7 / 5 / —
- IG items: 5 / 3 / —

**Close-step bugs**: TIMELINE doesn't enumerate them in the `[CRITICAL]/[WRONG]/[STYLE]` shape used later. Best-effort reconstruction from narrative: this run was a successful API-first run with 3 targets, dual-runtime URL pattern working, but workerdev scaffold README was never written (the agent only produced apidev + appdev READMEs).

**Structural flags**: first run with a separate-codebase worker but workerdev README is MISSING. Classified as incomplete deliverable.

**Rating**: S=B, C=C, O=?, W=C → **C**
*Content baseline present but workerdev documentation gap; workflow didn't ensure all three codebases wrote their README.*

---

### v7 — gold standard for content

- **Date**: 2026-04-11
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Model**: claude-opus-4-6 (pre-model-gate; inferred)
- **Session logs**: none
- **Wall / asst events**: unknown
- **Bash metrics**: unknown

**Content metrics** (apidev / appdev / workerdev):
- README lines: 271 / 168 / 167 — **balanced, workerdev finally has a README**
- Gotchas: 6 / 5 / 4 — mix of shallow (no `.env`) and deep (Meilisearch ESM-only, Auto-indexing skips, `<style>` blocks bypass build, Vite 8 ships Rolldown)
- IG items: 4 / 3 / 3 — full integration guide with code blocks for CORS, TypeORM env reading, worker reconnect-forever, SIGTERM drain

**Close-step bugs**: 0 CRIT / 3 WRONG (trust-proxy typing, async publish, dead StorageSection field) / 3 STYLE (scaffold test leftovers, prettier drift, JwtService manual instantiation). All fixed during close.

**Env import.yaml comments**: Gold standard. JWT rotation rationale, queue-group load-balancing explanation, Meilisearch re-push for TypeORM save-hook skip case, MinIO region stub reasoning. Every comment explains a trade-off or consequence.

**Structural flags**: first complete 3-codebase run with all READMEs written, all env tiers commented, all close-step fixes applied cleanly. This is the content target every subsequent version is measured against.

**Rating**: S=A, C=**A**, O=?, W=A → **A**
*v7 is the benchmark. Content depth has not been matched since. The `#zeropsPreprocessor=on` directive is present (not regressed until v10). Deep gotchas live in README where they belong.*

---

### v8 — first session logs, compression begins

- **Date**: 2026-04-12
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Session logs**: `nestjs-showcase-session.jsonl` (different filename than later versions)
- **Wall**: 07:18 → 08:54 = **96 min**
- **Assistant events**: 313
- **Bash metrics**: 62 calls / 4.7 min total / 2 very-long / 14 dev-server starts (but only 11s total — short probes, not hanging spawns) / 3 port kills / 4 errored

**Content metrics**:
- README lines: 239 / 124 / 171 — appdev starts compressing
- Gotchas: 6 / 3 / 4
- IG items: 4 / 3 / 2

**Close-step bugs**: 1 CRIT (status endpoint shape mismatch — API returned `{db:{...}}` but frontend expected `{services:[{name,status,latency}]}`) + 2 WRONG (fetchApi header merge, XSS via `{@html}`) + 4 STYLE. Only the CRIT was fixed.

**Notable**: first run where contract mismatch between scaffold authors surfaced in close review. This pattern repeated through v11 until v13's feature-subagent consolidation killed it.

**Rating**: S=B, C=B, O=A, W=B → **B**
*Content starts compressing from v7, CRITICAL in close, but session is fast (4.7 min bash, 96 min wall). Operations healthy.*

---

### v9 — worker migration sequencing bug

- **Date**: 2026-04-12
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Session logs**: `main-session.jsonl`
- **Wall**: 15:19 → 18:22 = **183 min**
- **Assistant events**: 297, **Tool calls**: 199
- **Bash metrics**: 54 calls / 5.0 min total / 1 very-long / 7 dev-server starts (54.8s sum) / 6 port kills / 9 errored

**Content metrics**:
- README lines: 245 / 126 / **196** — workerdev longest yet
- Gotchas: 6 / 4 / 4
- IG items: 3 / 3 / 3

**Close-step bugs**: **2 CRITICAL** — (1) worker migration creates `uuid-ossp` extension AFTER the table that needs it; (2) API codebase missing `CreateJobResults` migration (only in worker). Plus WRONG on `$effect` initialization and `@types/multer` in dependencies.

**Notable**: v9 was the run that exposed how much damage parallel scaffold authors can do when they don't agree on migration ownership. The fix landed in v14 as the "single-author feature subagent" rule — one agent writes both sides of every contract.

**Rating**: S=C, C=B, O=C, W=B → **C**
*Two CRITs in close, 183 min wall. Content is fine but the deploy was fragile.*

---

### v10 — content catastrophe

- **Date**: 2026-04-12
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Session logs**: `main-session.jsonl`
- **Wall**: 19:54 → 20:58 = **64 min** (fastest run on record by wall clock)
- **Assistant events**: 269, **Tool calls**: 171
- **Bash metrics**: 68 calls / 8.6 min total / 4 very-long / 2 dev-server starts but 150s sum (each one hit the 75s mark) / 1 port kill / 8 errored

**Content metrics**:
- README lines: 295 / 139 / 112
- Gotchas: **4 / 4 / 0** — workerdev README has NO gotcha bullets
- IG items: **0 / 2 / 0** — apidev AND workerdev have ZERO integration-guide items

**Close-step bugs**: 3 WRONG (worker `@MessagePattern` vs API `emit()`, missing `start:prod` script, tsconfig strict). Plus 3 STYLE noted but not fixed.

**Notable**: v10 is the content-catastrophe datapoint. The generate step emitted README scaffolds with empty knowledge-base AND empty integration-guide fragments for two of three codebases. This run is the justification for the `readme_fragments` byte-count + `knowledge_base_gotchas` checks added in v11.

**Rating**: S=C, C=**F**, O=A, W=C → **F**
*Empty fragments in published content is unshippable. The fast wall clock came from not writing the content the workflow requires.*

---

### v11 — longest run, 2 CRITs in close

- **Date**: 2026-04-12 → 2026-04-13
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Session logs**: `main-session.jsonl`
- **Wall**: 22:39 → 06:39 (next day) = **480 min (8 hours)**
- **Assistant events**: 286, **Tool calls**: 174
- **Bash metrics**: 72 calls / **21.1 min total** / 4 very-long / 6 dev-server starts (541s sum) / 10 port kills / 14 errored

**Content metrics**:
- README lines: 246 / 105 / 162
- Gotchas: 6 / 3 / 4
- IG items: 4 / 2 / 2

**Close-step bugs**: **2 CRITICAL** — (1) worker entity.ts used UUID PK with wrong table name + phantom columns; (2) worker search.service.ts referenced non-existent `price`/`quantity` fields. Plus 4 WRONG — all contract mismatches between parallel scaffold authors (StatusPanel, FileStorage, App.svelte all reading different response shapes than API produces).

**Notable**: The 8-hour wall clock came from the dev-server SSH-channel-hold pattern hit in chain: spawn → 120s wait → kill → retry → 120s wait → kill. Longest single bash call was **358.8s** (worker `ts-node src/worker.ts &` holding the SSH channel until two consecutive bash timeouts fired). This run is the single biggest justification for Tier 1 `zerops_dev_server`.

**Rating**: S=C, C=C, O=**F**, W=C → **F**
*Cost budget blown (8 hours, 21 min of bash time), 2 CRITs in close, content merely passable.*

---

### v12 — stability recovery

- **Date**: 2026-04-13
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Session logs**: `main-session.jsonl`
- **Wall**: 07:34 → 08:35 = **61 min** (new fastest)
- **Assistant events**: 247, **Tool calls**: 155
- **Bash metrics**: 40 calls / 6.4 min total / 1 very-long / 4 dev-server starts (134s sum) / 0 port kills / 2 errored

**Content metrics**:
- README lines: 270 / 132 / 153
- Gotchas: 5 / 4 / 4
- IG items: 3 / 2 / 2

**Close-step bugs**: 5 WRONG (no CRIT) + 2 STYLE. Significant improvement from v11.

**Notable**: v12 demonstrated that a recipe run CAN complete in under an hour. The content isn't as deep as v7 but structure is clean, close-step is contained, session cost is low. What changed between v11 and v12: the workflow's `readme_fragments` byte-count check was enforced, and the agent learned not to emit empty-fragment scaffolds.

**Rating**: S=B, C=B, O=A, W=B → **B**
*Clean recovery run. Nothing special, but nothing broken.*

---

### v13 — Sonnet model, enforced feature subagent, 6 very-long bash

- **Date**: 2026-04-13
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Session logs**: `main-session.jsonl`
- **Wall**: 09:38 → 11:02 = **84 min** — short on wall but 273 tool calls, 108 bash
- **Assistant events**: 489 (highest, due to Sonnet model using more turns per decision)
- **Tool calls**: 273
- **Bash metrics**: 108 calls / 19.0 min total / **6 very-long (highest)** / 19 dev-server starts (968s sum — highest) / 9 port kills / 17 errored

**Content metrics**:
- README lines: 232 / 138 / 155
- Gotchas: 6 / 5 / 5
- IG items: 3 / 3 / 3

**Close-step bugs**: 2 CRITICAL during deploy phase (allowedHosts blocked dev subdomain, NATS AUTHORIZATION_VIOLATION) + code review found 2 CRIT + 1 WRONG (no input validation on POST, ioredis status guard, Svelte fetch swallows errors).

**Notable**: First run after v8.63.0 landed the enforced feature-subagent sub-step and the eager-topic injection. Predecessor-floor + authenticity checks ran for the first time — v13 TIMELINE notes "Generate check failures (2 iterations to resolve)". Also the first run where Sonnet model was intentionally used to pressure-test the workflow against a non-Opus model.

**Rating**: S=C, C=B, O=D, W=A → **D**
*Bash cost is severe (19 min, 6 very-long). Sonnet ran 489 assistant turns — almost 2x the typical — which magnified every dev-server start cost. But the workflow discipline held (feature subagent fired, predecessor floor enforced, close review caught bugs).*

---

### v14 — cleanest close-step on record

- **Date**: 2026-04-13
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Session logs**: `main-session.jsonl`
- **Wall**: 20:09 → 21:31 = **82 min**
- **Assistant events**: 313, **Tool calls**: 196
- **Bash metrics**: 90 calls / 12.7 min total / 4 very-long / 8 dev-server starts (170s sum — LOWEST for a run at this scale) / 3 port kills / 18 errored

**Content metrics**:
- README lines: 267 / 124 / 141
- Gotchas: 7 / 4 / 4 — apidev highest gotcha count since v7
- IG items: 5 / 4 / 3

**Close-step bugs**: **0 CRITICAL, 0 WRONG, STYLE findings only**. Code review sub-agent (aa2978c96) found no contract bugs. This is the cleanest close step ever recorded.

**Notable**: v14 represents what the workflow can do when all the structural rules are in place. Predecessor floor, authenticity check, enforced feature subagent, eager topics, and the model gate all held. The content isn't as deep as v7 on a per-gotcha basis but the close step is clean, the workflow discipline is A+.

**Rating**: S=A, C=B, O=B, W=**A** → **B**
*Best workflow discipline. Content is good but not v7-deep. Close step is spotless.*

---

### v15 — content peaks, dev-ops regression

- **Date**: 2026-04-14
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Model**: claude-opus-4-6[1m]
- **Session logs**: `main-session.jsonl`
- **Wall**: 08:58 → 12:22 = **204 min**
- **Assistant events**: 326, **Tool calls**: 203
- **Bash metrics**: 63 calls / 17.0 min total / 5 very-long / 9 dev-server starts (557s sum — v11-level) / 7 port kills / 7 errored

**Content metrics**:
- README lines: 281 / 123 / 166 — apidev highest since v10's catastrophe
- Gotchas: 6 / 4 / 4
- IG items: **6 / 4 / 3** — highest IG count ever

**Close-step bugs**: 0 CRITICAL + 5 WRONG + 2 STYLE. The 5 WRONG are all dev-ops issues leaked into the published content:
1. `npx tsc` resolves to deprecated tsc@2.0.4 package
2. SSHFS files owned by root, `npm install` EACCES
3. Svelte curly braces in placeholder attribute
4. Port 3000 EADDRINUSE after background command
5. Vite dev server died on redeploy

**Notable**: v15 is the "content regression that the v8.67.0 structural rules caught but had no way to prevent" run. The apidev README had 3 gotchas cloned from workerdev (NATS credentials, SSHFS ownership, `zsc execOnce` burn) and appdev had 3 gotchas that restated 3 adjacent IG items. The 5 WRONG in close are all repo-local dev-loop knowledge that the v8.67.0 rules later pushed into CLAUDE.md.

**Rating**: S=B, C=**C**, O=D, W=B → **D**
*204 min wall, 557s on dev-server starts, content content-regression. This is the run v8.67.0 was designed to prevent.*

---

### v16 — v8.67.0 structural rules land

- **Date**: 2026-04-14
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Model**: claude-opus-4-6[1m]
- **Session logs**: `main-session.jsonl` + 6 subagent logs
- **Wall**: 14:38 → 16:44 = **125 min**
- **Assistant events**: 370, **Tool calls**: 233
- **Bash metrics**: 78 calls / 7.5 min total / **0 very-long** / 9 dev-server starts (250s sum — capped at ~30s each) / 5 port kills / 9 errored
- **Subagents**: 6 (scaffold ×3, feature ×1, READMEs/CLAUDE.md ×1, code review ×1) — first run with dedicated README/CLAUDE writer subagent

**Content metrics**:
- README lines: 218 / 123 / 162 — most compressed since v8
- Gotchas: 4 / 3 / 3 — lowest since v11
- IG items: 3 / 2 / 2 — at the floor
- **CLAUDE.md**: 42 / 39 / 44 lines per codebase (first run with separate CLAUDE.md)

**Close-step bugs**: 1 CRITICAL + 6 WRONG + 3 STYLE. The CRITICAL is a contract drift: StatusPanel used key `"queue"` but API returns `"nats"` → the NATS dot always renders gray. Fixed post-review. The 6 WRONG include a NestJS major version mismatch (api v10 vs worker v11), missing `DB_PORT` default, pagination double-fetch, and three unused-dep findings (`bcryptjs`/`passport*`, `@types/multer`, `ioredis` in worker).

**Notable**:
- First run where **zero bash calls hit the 120s wall on main session**. The main agent learned the correct `ssh host "cmd &" && sleep N && ssh host "curl-health"` pattern. The feature subagent, however, still hit 240s + 120s on its two dev-server starts — that's the 360s hidden cost that motivated the v17 `zerops_dev_server` MCP tool.
- First run where the **v8.67.0 deduplication and restates-guide checks forced content discipline** — 3 README iterations to satisfy `cross_readme_gotcha_uniqueness` + `gotcha_distinct_from_guide` + authenticity on worker (6/8 worker_knowledge_base_authenticity fails before recovery).
- First run with **dedicated READMEs/CLAUDE.md subagent** (agent-ac823b1fe9d00b4f0). Main agent pre-classified gotchas by codebase in the brief, subagent wrote files, cross-README dedup never fired because the dedup was prevented at the brief level.
- Root README intro shipped `"connected to typeorm"` — the agent set `plan.Research.DBDriver = "typeorm"` (an ORM library) and the root README generator rendered it as-is because `dbDisplayName` had no case for it. Caught in audit, fixed in v17 via first-principles `validateDBDriver` at research-complete time.
- All 6 env import.yaml files shipped missing `#zeropsPreprocessor=on` directive despite using `<@generateRandomString(<32>)>`. The finalize check was dead-code-gated on `plan.Research.NeedsAppSecret` (false for NestJS) — fixed in v17 by de-nesting the check.

**Rating**: S=B, C=**C**, O=B, W=B → **C**
*Cleanest operational cost of any run. Structural rules held (dedup, restates, CLAUDE.md split). But content compression went too far — apidev is 218 lines vs v7's 271, IG items hit the floor at 2/2 for two codebases, and deep insights from v7 (queue group HA, ESM-only SDK, auto-indexing skip) stayed filtered out. Plus a real factual bug on the root README intro.*

**This is the run that motivated v17** (the content pass shipped as v8.70.0):
- `zerops_dev_server` MCP tool replaces the hand-rolled SSH+background+sleep pattern
- `dbDriver` validation catches ORM-name-as-database-name at the source
- Preprocessor check de-nested to fire unconditionally
- Classifier `platformTerms` list expanded from ~30 → ~120 Zerops mechanism terms
- Framework × platform intersection bonus admits the "ESM-only SDK" class of deep gotcha
- CLAUDE.md depth bar raised (1200 bytes + ≥2 custom sections)
- Worker production-correctness gotchas required (queue group + SIGTERM drain)
- Env import.yaml "WHY not WHAT" comment depth rubric
- Root README intro "connected to ..." must name plan-declared managed services

---

### v17 — F-grade: tool regression + scaffold sub-agent SSH regression

- **Date**: 2026-04-14
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Model**: claude-opus-4-6[1m]
- **Session logs**: `main-session.jsonl` + 3 scaffold subagent logs (no feature, no code-review, no close — run aborted)
- **Wall**: 18:53:47 → 19:17:05 = **23 min (user abort)** — the fastest run on record for the wrong reason
- **Assistant events**: 146, **Tool calls**: 90
- **Bash metrics**: 32 calls / 1.5 min total / **0 very-long** / 4 dev-server starts (6.4s sum — all failed fast or were killed by context) / 0 port kills / 9 errored in main; subagents added 20 errored calls
- **Subagents**: 3 scaffold (appdev / apidev / workerdev) — all completed "successfully" while running commands on the wrong host; no feature subagent, no code review, no close

**Content metrics** (apidev / appdev / workerdev):
- README lines: — / — / — (run aborted before generate)
- Gotchas: — (never reached README generation)
- IG items: — (never reached README generation)
- CLAUDE.md: — (never reached README generation)

**Close-step bugs**: N/A — run did not reach the close step. The two blocking bugs that aborted the run:

1. **[TOOL REGRESSION]** `zerops_dev_server action=start hostname=apidev` hung for **exactly 300.05s** (the full `deployExecTimeout` ceiling in [internal/platform/deployer.go](../internal/platform/deployer.go)) and returned `dev_server start: spawn: ssh apidev: signal: killed`. The spawn shape in v8.70.0 used `nohup sh -c CMD > LOG 2>&1 < /dev/null & disown` — theoretically correct, empirically hung. Root-cause theories: non-interactive bash job control no-ops `disown`, ssh channel stayed open because backgrounded child inherited an fd, or the remote shell's exit path waited on a child process. The tool did not classify the failure — it surfaced `signal: killed` verbatim, which the agent interpreted as a transient ssh issue and retried. User aborted the retry at 19:17:05.

2. **[PROMPT REGRESSION]** All three scaffold sub-agents ran commands **on the zcp orchestrator** instead of ssh'ing into their target containers. Every subagent bash call was `cd /var/www/{hostname} && <command>` with zero ssh calls across the three subagent logs. The scaffold-subagent-brief topic block in `recipe.md` said `Target mount: /var/www/appdev/` without explaining what a mount is or that executable commands must run inside the container via ssh. Downstream damage observed in the logs:
   - `node_modules/.bin/svelte-check -> /var/svelte-check/bin/svelte-check` — **broken absolute-path symlinks** that don't resolve inside the container, owned by `root`
   - `sh: vite: not found`, `sh: svelte-check: not found` — `.bin/` path resolution broke
   - Main session had to `sudo rm -rf node_modules && npm install` over ssh to recover workerdev
   - `.git/` dirs created zcp-side with wrong ownership — required `ssh {hostname} "sudo chown -R zerops:zerops /var/www/.git"` on all three containers

**Operational breakdown of the 23 minutes**:
- 00:00–03:00 — research, import, mount, env (healthy)
- 03:00–07:00 — scaffold subagents run (3 min 48s total) while doing everything on the wrong host
- 07:00–14:00 — main agent recovers from the scaffold damage over ssh: chown dances, `sudo rm -rf node_modules`, re-installs, debug of nest binary, `.vite-temp` permissions. **16 minutes of recovery work** on a run that should have had zero.
- 14:00–17:30 — main agent completes generate, deploys all 3 services successfully
- 17:30–22:30 — first `zerops_dev_server start apidev` hangs 300s
- 22:30–23:20 — second `zerops_dev_server start` rejected by user abort

**Notable**:
- **First F-grade run** since the log was kept. Every prior run at least reached close step. v11's 8-hour cost was bad; v17's 23-minute abort is worse because nothing was learned about content or workflow discipline.
- **First regression where the v17 flagship feature (`zerops_dev_server`) cost more than the v16 hand-rolled pattern** it was supposed to replace. v16 main-session had zero very-long bash calls; v17 had one 300s call. The tool's spawn-shape and per-call timeout were both wrong.
- **First regression in scaffold subagent SSH discipline** since the log was kept. v6-v16 all had scaffold subagents ssh'ing correctly (implicitly or explicitly); v17 lost it because the brief wording was tightened to "Target mount" without the container-execution-boundary preamble.
- **Hidden success**: the main agent's self-healing path (detect broken scaffold → chown → rm → reinstall over ssh) worked. If the scaffold regression had landed without the `zerops_dev_server` regression, the run would probably have completed 20-30 min late with damaged content. The combination of both regressions is what killed it.

**Rating**: S=**F**, C=**F** (no content produced), O=F (aborted), W=F (workflow never reached deploy step) → **F**
*Did not complete. The two independent regressions (tool + prompt) together produced the first abort in the tracked history.*

**Fix shipped as v17.1 (after the abort, same day)**:
- [internal/platform/deployer.go](../internal/platform/deployer.go) — added `sshArgsBg` (`-T -n -o BatchMode=yes -o ConnectTimeout=5`), `ExecSSHBackground(ctx, host, cmd, timeout)` with default-10s per-call deadline, and `platform.IsSpawnTimeout` classifier.
- [internal/ops/dev_server_start.go](../internal/ops/dev_server_start.go) — new spawn shape:
  ```
  set -e; rm -f LOG || true; cd WORK; setsid sh -c CMD > LOG 2>&1 < /dev/null & echo "zcp-dev-server-spawned pid=$!"; exit 0
  ```
  Three bounded phases: spawn 8s, probe `waitSeconds+5s`, log-tail 5s. Worst-case cost of a future spawn regression: 8 seconds, not 300.
- **Structured failure reasons**: `spawn_timeout`, `spawn_error`, `health_probe_timeout`, `health_probe_connection_refused`, `health_probe_http_<code>` — all documented on `DevServerResult.Reason`. Agents no longer see raw `signal: killed`.
- **Spawn ack marker** (`zcp-dev-server-spawned pid=$!`) — the outer remote shell must print this right before `exit 0`. Its presence proves the shell reached the end of the script AND the backgrounded child's stdio redirects took effect. Missing marker becomes a diagnostic breadcrumb.
- [internal/content/workflows/recipe.md](../internal/content/workflows/recipe.md) — scaffold-subagent-brief block prepended with a **⚠ CRITICAL: where commands run** section that explains SSHFS mount semantics, lists the four specific damage patterns (EACCES, broken `.bin/` symlinks, wrong node ABI, `.git` ownership), and gives the correct `ssh {hostname} "cd /var/www && ..."` shape with the wrong shape as a counter-example.
- [internal/workflow/recipe_topic_registry_test.go](../internal/workflow/recipe_topic_registry_test.go) — eager-topic test now asserts `"SSHFS network mount"`, `"Executable commands"`, `"write surface, not an execution surface"` appear in the injected brief. v17 regression guard.
- **8 SSHDeployer mocks updated** across ops/tools/integration to implement the new `ExecSSHBackground` method.
- Tests written first (RED): spawn uses setsid + bg path + ack marker + tight timeout, spawn timeout returns `spawn_timeout`, spawn generic error returns `spawn_error`, missing ack handled gracefully, spawn ordering invariant (setsid < redirect < `&`).

v17 is the shortest-lived and highest-signal run in the log. Two independent failure modes, both reproducible, both fixed by the next commit — the shortest recipe-log-to-fix loop on record.

---

### v18 — v17.1 fixes hold, full-tree zero-very-long, cleanest operational run on record

- **Date**: 2026-04-15
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Model**: claude-opus-4-6[1m]
- **Session logs**: `main-session.jsonl` + 7 subagent logs
- **Wall**: 07:06:56 → 08:12:13 = **65 min** (second-fastest complete run after v12's 61 min)
- **Assistant events**: 223, **Tool calls**: 145
- **Bash metrics (main)**: 31 calls / **0.8 min total** / **0 very-long** / 1 dev-server bash probe (13s) / 0 port kills / **0 errored**
- **Bash metrics (main + 7 subagents)**: ~91 calls / ~4.2 min total / **0 very-long** / 13s dev-server sum / 0 port kills / 1 errored (one transient svelte scaffold check)
- **Subagents** (7): scaffold×3 (apidev 54.8s / appdev 23.8s / workerdev 36.9s), feature×1 (65.1s / 33 bash), zerops.yaml block updater×1 (0.1s — new), README+CLAUDE writer×1 (0 bash — pure Write), code review×1 (25.0s)
- **MCP tool mix**: 23 `zerops_workflow`, 14 `zerops_guidance`, 11 `zerops_deploy`, **9 `zerops_dev_server`**, 4 `zerops_verify`, 4 `zerops_subdomain`, 3 `zerops_mount`, 2 `zerops_logs`, 1 `zerops_browser`, 1 each of `zerops_knowledge` / `import` / `env` / `discover`

**Content metrics** (apidev / appdev / workerdev):
- README lines: 257 / 117 / 161 — apidev recovers toward v7's 271; appdev still compressed
- Gotchas: 4 / 3 / 4
- IG items: 4 / 3 / 2 — worker IG at the floor
- **CLAUDE.md**: 79 / 41 / 49 lines (4134 / 2565 / 3340 bytes) — **all three clear the 1200-byte floor**, all three have ≥2 custom sections beyond the 4-section template:
  - apidev: +Driving Test Requests, +Resetting Dev State
  - appdev: +Log Tailing, +Adding a New API Endpoint Consumer
  - workerdev: +Driving a Test Job, +Recovering from a Burned `zsc execOnce` Key
- **Root README intro**: ✅ `"connected to PostgreSQL, Valkey (Redis-compatible), NATS, S3-compatible object storage, and Meilisearch"` — names real managed services. v17's `dbDriver` validation held.
- **Preprocessor directive**: ✅ all 6 env import.yaml files carry `#zeropsPreprocessor=on` with `generateRandomString`. v17's de-nested check held.

**Close-step bugs**: 0 CRITICAL / 2 WRONG / 4 STYLE (code review subagent a5a85c078):
1. **WRONG** — `jobs.controller.ts` throws plain `Error` → HTTP 500 instead of `NotFoundException` → 404. Fixed in close.
2. **WRONG** — `busboy` not declared as explicit dependency (pulled in transitively via `@nestjs/platform-express` but explicitly `require()`'d). Added to package.json in close.
3. STYLE — no runtime validation on `POST /items` body (noted, acceptable for recipe demo)
4. STYLE — Redis `lazyConnect` inconsistency
5. STYLE — worker tsconfig less strict than API
6. STYLE — unused Redis import in worker

**Finalize-step issues** (both LOW, self-recovered):
1. Env 3 (Stage) comment ratio at the 30% boundary — expanded with reasoning markers → 37%
2. Env 4 (Small Prod) factual claim mismatch ("2GB vs 1GB storage") — removed incorrect number, described autoscaling behavior

**Env comment quality spot-check (env 4 — Small Production)**: v7-grade. JWT rotation rationale is back: *"APP_SECRET shared across every container behind the L7 balancer — signed tokens and session cookies must verify everywhere, or users see random 401s the moment a deploy rolls."* NATS queue group gotcha tied to the code-level failure mode: *"minContainers: 2 — NATS queue group 'workers' is mandatory here because without it, each replica receives every message and processes jobs twice, filling the database with duplicates."* Postgres NON_HA: *"execOnce migrations handle the concurrent-container race safely."* Finalize reported apidev zerops.yaml comment ratio at 37% and worker at 44% — above the 35% bar.

**Gotcha authenticity audit**:
- **apidev (4/4 authentic)**: Valkey no-auth (platform + concrete `NOAUTH` failure), TypeORM sync col-drop + `zsc execOnce` (framework × platform), Meilisearch masterKey vs defaultSearchKey (Zerops env-var naming + 403 masquerade), pg `client.query() while executing` deprecation warning → pg@9 hard error.
- **appdev (3/3 authentic)**: Vite `"Blocked request"` 200-plain-text (framework × platform, exact failure shape), `VITE_API_URL` undefined from `run.envVariables` vs `build.envVariables` (platform mechanism), `./dist/~` tilde mandatory for static (Zerops-specific deploy mechanism).
- **workerdev (3/4 authentic)**: queue-group-under-minContainers ✓, SIGTERM graceful-drain + `OnModuleDestroy` ✓, "shares DB but not migrations" is architectural but names `QueryFailedError: column "x" does not exist` so passes the failure-mode bar, `AUTHORIZATION_VIOLATION` is a cross-ref to apidev IG rather than standalone content (correct dedup, thin as a standalone gotcha).

**Head-to-head content comparison vs v7 (the gold standard)** — counting README + CLAUDE.md + YAML comments, not just README lines:

- **apidev: net gain vs v7**. v18 total 336 lines (257 README + 79 CLAUDE.md) > v7's 271 README. YAML comment density higher (~60 vs ~50 lines) with new L7-balancer platform insights v7 didn't capture: NATS `user`/`pass`-must-be-separate-options (URL-embedded silently ignored → `AUTHORIZATION_VIOLATION`), `FRONTEND_URL` interpolator shadowing rule, explicit Valkey no-auth, `deployFiles is the ONLY bridge between build and run containers`. CLAUDE.md is net-new: full `zerops_dev_server` dev loop, SSHFS uid trap, `npx tsc`→tsc@2.0.4 trap, 6 endpoint test-request recipes, schema-reset-without-redeploy procedure. Real losses: Meilisearch SDK ESM-only gotcha (fetch() workaround), auto-indexing skips on redeploy seed runs. Two specific v7 gotchas gone; everything else is at or beyond v7.
- **workerdev: roughly equal**. v18 total 210 lines (161 README + 49 CLAUDE.md) > v7's 167 README. Real losses: `reconnect: true, maxReconnectAttempts: -1` pattern fully gone, SIGTERM drain IG code example gone (the gotcha text remains), "no HTTP no healthCheck" teaching reduced to a YAML comment, internal watchdog suggestion gone. Real gains: queue group tied to a specific failure mode (`filling the database with duplicate results`), migration ownership rule with a named `QueryFailedError`, NATS credentials-via-options insight, CLAUDE.md full end-to-end test-job walkthrough + `execOnce` recovery procedure.
- **appdev: real regression**. v18 total 158 lines (117 README + 41 CLAUDE.md) < v7's 168 README. **The only codebase where v18 is thinner than v7 even counting CLAUDE.md.** Lost: `run.base: static`-only-ships-in-prod teaching, Vite 8 ships Rolldown by default, `<style>` blocks bypassing build pipeline, `preview.allowedHosts` in addition to `server.allowedHosts`. IG #3 regressed from a full code block to prose-only. CLAUDE.md gains (Vite HMR-WS-through-L7 insight, test procedures, endpoint-consumer add procedure) don't fully compensate for four lost framework-depth gotchas.

**Notable**:
- **Full payoff from v17.1**: every regression that killed v17 stayed fixed. `zerops_dev_server` fired 9 times from the main agent across both apidev (`npm run start:dev`, port 3000) and appdev (`npx vite`, port 5173) with zero hangs. The single 13s bash call the analyzer flagged was a health-probe, not a spawn. v17's 300s timeout is gone.
- **New workflow shape**: 7 subagents vs v16's 6. The addition is a **dedicated zerops.yaml block-updater subagent** (aba5c47a — 2 bash calls, 0.1s) separate from the README/CLAUDE writer (afee83e9 — 0 bash, pure Write). The main agent pre-classified content by codebase; the writer subagent never touched bash. This is the cleanest content-writing pipeline ever recorded.
- **First run with CLAUDE.md apidev >4KB**. The 79-line apidev CLAUDE.md has two fully custom sections ("Driving Test Requests" with a curl walkthrough, "Resetting Dev State" with a `sudo rm -rf node_modules && npm install` recovery procedure) — these are exactly the repo-local operational details the v8.67.0 split was designed to capture. All three files clear the 1200-byte floor raised in v17.
- **Single `zerops_browser` call at 07:37** — one snapshot + text fetch against appdev during deploy.browser. No close.browser walk visible. The rubric bar is both deploy.browser AND close.browser; only one fired. Minor W dock.
- **Sonnet/Opus note**: ran on opus-4-6[1m] with 223 assistant events / 145 tool calls. v16 ran 370/233 and v13 ran 489/273. v18 is the lowest assistant-event-count for a complete run on record, despite doing MORE work (7 subagents vs 6, 3-codebase, all features implemented and tested). Less deliberation, more decisive tool choice — the workflow's sub-step orchestration is picking up the routing the model used to figure out mid-turn.
- **Content picture once CLAUDE.md + YAML comments are counted**: apidev is a net gain over v7, workerdev is roughly equal with different trade-offs, appdev is the only real thinning. Gotcha counts (4/3/4) are below v7's (6/5/4) but that's the wrong metric — v18 moves platform-anchored knowledge into YAML comments and operational knowledge into CLAUDE.md, so README-only gotcha counts systematically undercount total depth from v16 onward. See "Head-to-head content comparison vs v7" above.
- **Finalize took 2 iterations on LOW issues only** — no comment-ratio CRIT, no authenticity CRIT, no predecessor-floor fails. The content rules are now producing runs that pass the checks on first or second try rather than third or fourth.

**Rating**: S=**A**, C=**A−**, O=**A**, W=**B** → **B**
*Operationally the cleanest run on record — obliterates the A bar on wall clock, bash total, very-long count, dev-server sum, and errored count simultaneously. Content dimension hits every criterion on the A rubric (all gotchas authentic, no dedup, env comments ≥35%, CLAUDE.md ≥2 custom sections per codebase, root README intro accurate) and on apidev+workerdev is at or above v7 once YAML comments and CLAUDE.md are counted — the **A−** is because appdev specifically regressed versus v7 (lost Rolldown, `<style>` pipeline, preview.allowedHosts, and IG #3 code→prose) and because two v7 apidev gotchas (Meilisearch ESM-only, auto-index skip) are genuinely missing. Workflow dimension docked to B because only one `zerops_browser` call fired (deploy.browser yes, close.browser no). v18 validates the v17.1 spawn-shape and SSH-preamble fixes under production load and demonstrates that the operational cost class of problem (120s SSH holds, dev-server hangs, scaffold SSH misuse) is fully solved. What remains is appdev-specific content depth and restoring the worker reconnect-forever + SIGTERM IG code blocks.*

---

### v19 — content checks working, stale-major import class surfaces

- **Date**: 2026-04-15
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Model**: claude-opus-4-6[1m]
- **Session**: 2657f9b08f0d8325
- **Session logs**: `main-session.jsonl` + 8 subagent logs
- **Wall**: 11:43:54 → 12:59:03 = **75 min 9 s** (3rd-fastest complete run after v12's 61 min and v18's 65 min)
- **Assistant events**: 262, **Tool calls**: 174
- **Bash metrics (main)**: 37 calls / **1.0 min total** / **0 very-long** / 1 dev-server bash probe (7s) / 0 port kills / 1 errored
- **Bash metrics (main + 8 subagents)**: ~92 calls / ~4.2 min total / **0 very-long** / 3 long (>10s, all scaffold `npm install` / `nest build` / `svelte-check`) / 6 errored (1 appdev scaffold tsc-driven fix, 4 feature subagent type-check iterations, 1 main-session tsc fix)
- **Subagents** (8 — most ever): scaffold ×3 (apidev 59.1s / worker 51.1s / appdev 24.7s), feature ×1 (57.9s, 23 bash, 4 err — all type-check errors that drove fixes), README/CLAUDE writer ×1 (0.5s, pure Write), code review ×1 (0.3s, static Read/Grep only), **fix README check failures ×1** (NEW — 0.1s, 2 bash), **fix API README yaml block ×1** (NEW — 0s, 1 bash)
- **MCP tool mix**: 28 `zerops_workflow`, 16 `zerops_guidance`, 13 `zerops_deploy`, 9 `zerops_dev_server`, 6 `zerops_verify`, 4 `zerops_subdomain`, 3 `zerops_mount`, 3 `zerops_logs`, 1 `zerops_browser` (deploy.browser only — close.browser silent), 1 each `zerops_knowledge` / `import` / `env` / `discover`

**Content metrics** (apidev / appdev / workerdev):
- README lines: 237 / 149 / 144
- Gotchas: 4 / 3 / 3 — workerdev lost one vs v18's 4
- IG items: 4 / 3 / 2
- **CLAUDE.md**: 111 / 75 / 90 lines (4406 / 3566 / 4005 bytes) — **all three clear the 1200-byte floor AND all three grew vs v18**. Biggest gain: appdev 2565→3566 bytes (+1001), recovering the v18 regression.
- **Root README intro**: ✅ names real managed services (`PostgreSQL, Valkey, NATS, S3-compatible object storage, and Meilisearch`). v17 `dbDriver` validation held for the second run.
- **Preprocessor directive**: ✅ all 6 env import.yaml files carry `#zeropsPreprocessor=on`. v17 de-nested check held.

**Generate-step MEDIUM fixes** (the story of this run) — v8.67.0 content checks caught **four** issues at generate time and routed each to either an inline fix or a dedicated fix subagent:
1. **appdev gotcha restated an IG item** (`preview.allowedHosts ~ allowedHosts`) → `gotcha_distinct_from_guide` fired → replaced with the Vite re-optimization-flood gotcha.
2. **apidev zerops.yaml comment specificity 12%** (below 30% floor) → 15+ comments rewritten with Zerops-specific platform terms.
3. **apidev gotchas cloned from `nestjs-minimal` predecessor** (TypeORM synchronize, Meilisearch stale index) → `predecessor_floor` fired → replaced with two S3-specific gotchas (`forcePathStyle` `AccessDenied`, `S3_REGION` "Missing region in config").
4. **apidev README yaml block out of sync** with the actual `zerops.yaml` → re-synced by a dedicated fix subagent (`aee2886184`).

**Close-step bugs**: **1 CRITICAL + 2 WRONG + 4 STYLE** (code review subagent `ab0a4e7239`):
1. **[CRITICAL]** `CacheModule` imported from `@nestjs/common/cache` — wrong import path for NestJS 10 (moved into `@nestjs/cache-manager` in a separate package) AND the module was unused. **First instance of the "stale-major import" class** in the log — agent wrote NestJS 8-era path from training-data memory. Removed during close.
2. **[WRONG]** Worker NATS URL missing `nats://` protocol prefix — `nc.connect('host:port')` throws at boot. Added prefix.
3. **[WRONG]** `files/:key` DELETE route cannot match S3 keys containing `/` (e.g. `uploads/img.png`). Changed to `:key(*)` wildcard.
4-6. **[STYLE]** no runtime validation on `POST /items` body, Redis `lazyConnect` inconsistency, worker tsconfig less strict than API, unused Redis import in worker.

All fixes compiled clean, redeployed to dev + stage, reverified. No regressions.

**Finalize-step issues** (both LOW, self-recovered):
1. Env 3 (Stage) storage comment claimed "2 GB" but `objectStorageSize: 1` — fixed to "1 GB" (numeric-claim check from recipe.md:1167 fired).
2. Envs 3 and 5 comment ratios below 30% — expanded comments.

**Notable**:
- **First run where content checks saved the run at generate time rather than letting issues surface in close review.** v15 shipped 5 WRONG that were all content-side; v19 caught the equivalent class of 4 MEDIUMs before publish. The check infrastructure is now working as designed — predecessor-clone dedup, gotcha-distinct-from-guide, comment specificity, yaml-in-sync all fired at the right moment.
- **First run with 8 subagents** (v18 had 7). The addition is two short-lived fix subagents dispatched during generate to clear check failures — `agent-ac20e21a` (fix README checks, 0.1s, 2 bash) and `agent-aee2886184` (fix API yaml block, 0s, 1 bash). Generate iteration at the sub-step level rather than "redo from scratch".
- **First CRITICAL from the stale-major import class** (CacheModule NestJS 8 → 10 path move). Detected post-hoc by the framework-expert code review subagent at close time. The v19 post-mortem identified this as a symptom of the scaffold + feature subagent briefs having no directive to verify imports against the installed package. Fix landed as an **installed-package verification rule** in both briefs — framework-agnostic by design (verify against `node_modules/<pkg>/package.json`, `vendor/<pkg>/composer.json`, the module's `go.mod`, the `*.gemspec`, or whatever manifest exists for the installed stack — no per-version lookup table).
- **Worker uses SSH-background pattern** (`nohup npx ts-node src/main.ts & disown` + sleep + cat) twice (12.7s + 3.1s) because `zerops_dev_server` required an HTTP port for its health probe. Worker has no HTTP surface, so the main agent bypassed the tool. **Fixed in the v8.77.0 release**: `zerops_dev_server` now accepts `NoHTTPProbe=true`, spawns via the same setsid + ack path, writes the real child PID to `<logFile>.pid` from inside the inner shell (`echo $$ > PIDFILE; exec CMD`), then decides liveness via `kill -0` — POSIX signal, no log-string matching, framework-agnostic.
- **`close.browser` silent for the second run in a row** — `deploy.browser` fired (1 `zerops_browser` call at the dashboard verification step), but the close-time browser walk prose in recipe.md was skipped because nothing gated close-step complete on it. **Fixed in v8.77.0**: `SubStepCloseReview` + `SubStepCloseBrowserWalk` sub-step constants, `closeSubSteps()` returns them for showcase only, `CompleteStep` gate extended to `RecipeStepClose` for `isShowcase`. Minimal recipes skip the gate.
- **Env-4 app comment thinness (caught in audit, not by the run)** — the `app` (static) service comment said "Nginx handles concurrent requests within a single container, so this is not horizontal scaling" and stopped there, silently dropping the HA / rolling-deploy reason the comment should have named alongside. Initial audit flagged this as a "contradiction"; user correction made the distinction crisp — a runtime service on envs 4-5 with `minContainers ≥ 2` serves **two independent axes** (throughput AND HA / rolling-deploy availability), and a service whose throughput fits in one container still wants ≥2 for the HA reason. The teaching block at recipe.md:2035 and the core.md / model.md knowledge themes were updated accordingly. This is the `minContainers` two-axis semantics fix shipped in v8.77.0.

**Rating**: S=**B** (1 CRIT caught+fixed at close, 2 WRONG), C=**A** (all gotchas authentic, cross-codebase dedup held, CLAUDE.md floor+custom sections per codebase, intro names real services, 4 MEDIUMs caught at generate before publish), O=**A** (75 min wall ≤90, main bash 1.0 min, 0 very-long, dev-server 7s, 1 errored), W=**B** (close.browser silent for the 2nd run, generate took 2 fix subagents in addition to the inline edits) → **B**
*Same letter as v18 but with a different shape. v18's B came from a clean close step and 0 CRIT; v19's B is docked for 1 CRIT in close review but lifts on the content dimension thanks to the generate-time check saves. This is the first run where the v8.67.0 rules visibly saved the run at generate time rather than letting the issues leak into close review. The CacheModule CRIT is not a content-check failure — it's a scaffold-author failure, and the post-mortem fix shipped as v8.77.0 is a structural answer (verify against installed packages) rather than a hardcoded per-framework lookup table.*

**v19 post-mortem fixes shipped as v8.77.0** ([d7fb618](../internal/ops/dev_server.go)):
- `zerops_dev_server` `NoHTTPProbe` mode — spawn → settle → `kill -0 <pid>` via SSH against pidfile written by the inner shell. Port validation relaxed to allow 0 only when `NoHTTPProbe=true`. New reasons: `post_spawn_exit`, `liveness_check_error`. Atomic-backed settle interval for race-safe parallel tests.
- Close-step sub-step gate — `SubStepCloseReview` + `SubStepCloseBrowserWalk` constants, `closeSubSteps()` returns them for showcase, `CompleteStep` enforces for `RecipeStepClose`. Recipe.md close section now carries explicit `substep="..."` attestation call-outs.
- `minContainers` two-axis teaching in recipe.md (env-comment-rules block), core.md, model.md — replica count serves both throughput AND HA/rolling-deploy availability; scoped to runtime services on envs 4-5 only; worker queue-group check error message + code comments no longer conflate "horizontal scaling" with `minContainers > 1`.
- Installed-package verification rule in scaffold-subagent-brief and feature-subagent required-contents — verify imports / decorators / module wiring against the installed package's on-disk manifest before committing. Framework-agnostic by design.
- 10 new tests (6 dev_server no-probe, 4 close sub-step gate). Full test suite green under `-race`, `make lint-local` clean.

---

### v20 — first A-grade complete run; load-bearing-content reform surfaces

- **Date**: 2026-04-15
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Model**: claude-opus-4-6[1m]
- **Session**: c5f46ef9-4ce7-4f75-8b5d-58cf2d5104da
- **Session logs**: `main-session.jsonl` + 10 subagent logs (most ever)
- **Wall**: 16:41:44 → 17:52:36 = **70 min 52 s** (3rd-fastest complete run after v12 61 m and v18 65 m)
- **Assistant events**: 294, **Tool calls**: 177
- **Bash metrics (main)**: 33 calls / **2.3 min total** / **0 very-long** / 7 errored / 1 dev-server bash probe (~30 s smoke-test pattern, not a hang)
- **MCP tool mix**: 34 `zerops_workflow`, 12 `zerops_guidance`, 11 `zerops_deploy`, **10 `zerops_dev_server`**, **2 `zerops_browser` (deploy + close — first since v16)**, 6 `zerops_verify`, 4 `zerops_subdomain`, 4 `zerops_knowledge`, 3 `zerops_mount`, 3 `zerops_logs`, 1 each of `import` / `env` / `discover`
- **Subagents (10 — new record)**: scaffold ×3 (apidev 55.9 s / appdev 30.2 s / workerdev 52.1 s), feature ×1 (70.7 s, 41 bash, 2 long, 0 err, 1 `zerops_dev_server` start), README/CLAUDE writer ×1 (0.2 s pure Write), yaml-block updater ×1 (0.1 s), generate-time fix ×2 (KB format fix 0 bash + gotcha-restatement fix 6 bash), code review ×1 (7 bash / 0.3 s), **close-step critical-fix ×1 (NEW — 4 bash / 12.9 s, ran 1 dev_server start to verify fix)**

**Content metrics** (apidev / appdev / workerdev):

- README lines: **349 / 231 / 267** — **largest ever** for all three (v7 gold was 271/168/167)
- Gotchas: **7 / 6 / 6** — **highest ever**, matches/exceeds v7 (6/5/4) across every codebase
- IG items: **6 / 5 / 5** — ties v15 peak
- CLAUDE.md: 99 / 83 / 106 lines (3395 / 2786 / 3728 bytes) — all above 1200 floor, all 3+ custom sections beyond template
- Root README intro: ✅ names real managed services (v17 `dbDriver` validation held for 3rd run)
- Preprocessor directive: ✅ all 6 envs carry `#zeropsPreprocessor=on` (v17 de-nested check held)

**v7 gold-standard gotchas restored.** workerdev: queue group, SIGTERM drain (4-step commented walkthrough), reconnect-forever (with `nc.closed()` handler that exits so Zerops restarts the container — detail v7 didn't even have), internal watchdog, worker-no-migrations, `createApplicationContext` vs `create` — **all six v7 worker deep gotchas back**. apidev restored Meilisearch re-push (the v7 auto-index-skip class). appdev gained six new deep Nginx/SPA gotchas (200-text/html on `/api`, `serviceStackIsNotHttp`, static build invisible, SPA fallback, dev-noop, static-no-shell).

**Env 4 comments at v7 quality.** Two-axis `minContainers` teaching applied — the app-static comment explicitly distinguishes throughput from HA: *"minContainers: 2 because a single Nginx container drops traffic during rolling deploys, and static file serving has near-zero CPU cost per replica so the second container is essentially free."* That's the HA/rolling axis named as the reason when throughput doesn't apply — the v8.77 conflation purge is visibly shaping content. Env 5 adds DEDICATED CPU rationale + mode-immutability warning. Env 3 (Stage) ships a teaching line other envs miss: *"Queue group is configured even though only one replica exists, because adding minContainers: 2 later would silently double-process every job without it, and staging should match production subscription shape."*

**Generate-step iterations** (3 README iterations):
1. Missing `### Gotchas` heading (individual `###` per gotcha) — format check fired
2. **6 gotchas restated IG items** + `api_comment_specificity 20%` — `gotcha_distinct_from_guide` + specificity floor fired
3. Passed after symptom-focused stems + improved yaml block

Generate-time fix subagents dispatched: KB format fix (0 bash, pure Write) + gotcha restatement fix (6 bash). Same sub-step iteration pattern v19 introduced.

**Finalize iterations** (2): env 2 comment_depth 18% and env 3 comment_ratio 29% → rewritten with reasoning markers → passed.

**Close-step bugs**: **3 CRITICAL / 6 WRONG / 3 STYLE** (code review subagent `a4651e22`):

- **CRIT fixed** (by dedicated critical-fix subagent `ad6f7da` that rebuilt + redeployed + re-verified): StorageUpload bare fetch missing content-type guard; worker `jobs.service.ts:46` silent swallow (zombie job rows in `processing` state forever)
- **CRIT accepted**: NATS fire-and-forget pub/sub (showcase limitation, documented)
- **WRONG findings**: NATS `.info` health check unreliable, Redis `lazyConnect` never explicitly connected, empty-title update path, unsanitized filename, worker `parseInt` radix, dead `Item` entity. Partial fix coverage — TIMELINE doesn't enumerate which were fixed.

Note: CRIT #1 (StorageUpload bare fetch) is arguably a STYLE/consistency issue mis-labeled CRIT — the fetch did check `res.ok`, the gap was content-type validation. The code review subagent was generous with severity labels in this run.

**Notable**:

- **First A-grade complete run in tracked history** (v7 was A but pre-session-logs era, acts as baseline not peer). All four dimensions cleared the A bar simultaneously.
- **First run with `zerops_dev_server` stop returning `INVALID_PARAMETER` for no-probe-started workerdev** — the v8.77 no-probe start writes a pidfile, but the stop path doesn't read it; agent had to bypass with manual SSH. Real v8.77 follow-up gap.
- **First run since v16 with both deploy.browser AND close.browser walks** — the v8.77 sub-step gate (`SubStepCloseBrowserWalk`) forced the close walk that v18 + v19 both silently skipped.
- **First run with a dedicated close-step critical-fix subagent** — `ad6f7da` rebuilt both projects, redeployed, re-verified. Cleanest close-step fix loop ever recorded.
- **Cleanest content-writing pipeline ever recorded** — README/CLAUDE writer subagent did pure `Write` (0 bash). Main agent pre-classified gotchas by codebase + service so the writer subagent never needed to disambiguate.
- 10 `zerops_dev_server` MCP calls from main + 4 from feature subagent — no spawn-shape regressions, no 300 s hangs, the v17.1 fix held under the highest dev_server load any run has put on the tool.

**Deep content read surfaces 7 classes of decorative-content drift** the v8.67–v8.77 presence rules admitted (full analysis in conversation; tl;dr):

1. **Generic-platform leakage** — apidev gotcha "Do not commit `.env` files" is generic Node advice mis-anchored. Says ".env file overrides Zerops-managed values" — factually wrong on Zerops (the runtime never reads `.env` unless app code does); the failure mechanism it claims doesn't exist on the platform.
2. **Predecessor clones** — apidev gotchas #5 (trust proxy) and #7 (`synchronize: true` off in production) are near-verbatim from `nestjs-minimal` predecessor. v19 specifically removed this class; v20 let them back in. *(Pushback in conversation: standalone recipes should ship the most-relevant predecessor gotchas — overlap is fine, gaps are the regression.)*
3. **Topology drift** — appdev gotcha #1 (`200 OK` with text/html on `/api/*`) shows `_nginx.json` `proxy_pass` fix for an architecture the recipe doesn't ship. v20 actually uses absolute-URL via `VITE_API_URL: ${STAGE_API_URL}` + CORS — no `_nginx.json` exists. Reader copy-pasting the fix finds nothing to edit.
4. **Cross-file inconsistency** — apidev CLAUDE.md "Resetting Dev State" calls `ds.synchronize()` while README gotcha #7 forbids `synchronize: true` in production. CLAUDE.md is the ambient context for ops; teaching a pattern the README warns against propagates into prod-affecting changes.
5. **Reality-check gap** — workerdev gotcha #4 (no-health-check + watchdog) ships imperative prose ("Implement an internal watchdog…") with full `setInterval` code, but `lastActivity` and watchdog logic don't exist anywhere in src/. Reads like documentation; is a feature request.
6. **Per-IG-item leaning** — apidev IG #2 ("Binding to `0.0.0.0`") is 3 sentences + 2 lines of code; the why lives in the comment block of the zerops.yaml shown in IG #1. Item leans on its neighbor.
7. **Per-service env comment templating** — env 4 service comments share the "minContainers: 2 because rolling deploys" opening across app/api/worker. Each carries service-specific reasoning AFTER the templated opening, but the template lean is visible.

**Rating**: S=**A** (all 6 steps, all services, both URLs, all 5 features, both browser walks, CRITs triaged), C=**A−** (peak content depth across all codebases, all gotchas authentic, env-4 gold comments — but 7 classes of decorative drift listed above), O=**A** (71 min wall, 2.3 min main bash, 0 very-long, dev-server sum ~50 s, 7 errored), W=**A** (10 subagents with clean role separation, feature + code review + critical-fix subagents fired, deploy.browser + close.browser both ran, 3 generate iterations check-driven, 2 finalize iterations) → **A−**

*First A-grade complete run in the session-logged era. v7 was the content benchmark but is pre-session-logs; v20 is the first fully-instrumented run to match v7 on content AND clear every operational/workflow A-bar simultaneously. The A− is content dimension — peak counts but a careful read surfaces decorative drift that the presence-based rules admitted. The post-mortem reform is the v8.79 release.*

**v20 post-mortem fixes shipped as v8.79.0** ([57de8dd](../internal/tools/workflow_checks_reality.go)) — "load-bearing content" reform. Every README/CLAUDE.md/env-comment artifact must now carry its own weight under its own rubric. Five new per-codebase checks at deploy step, one at finalize, predecessor-floor net-new enforcement rolled back:

- **`<host>_content_reality`** — file paths and declared symbols in gotchas/IG/CLAUDE.md must exist in the codebase OR be framed as advisory ("Pattern to add if…", "Consider adding…"). Catches v20 `_nginx.json` mismatch and watchdog declared-but-unimplemented in one mechanism.
- **`<host>_gotcha_causal_anchor`** — per-bullet rule: every gotcha must name a SPECIFIC Zerops mechanism (curated narrow list — NOT generic `container`/`envVariables`) AND describe a CONCRETE failure mode (HTTP code, quoted error, strong symptom verb). Catches v20 `.env` gotcha — the platform-anchor classifier scored it under v8.67 because `envVariables` matched, but it fails causal-anchor because no Zerops mechanism actually causes the claimed failure.
- **`<host>_service_coverage`** — each managed service the codebase exercises must have ≥1 gotcha that names it (by brand or service-discovery env-var prefix). API codebases cover all categories; workers cover db + queue; static frontends exempt.
- **`<host>_claude_readme_consistency`** — CLAUDE.md procedures must not use mechanisms README forbids in production OR must explicitly cross-reference (`dev only — see README gotcha`). Catches v20 `synchronize`-vs-README conflict in apidev.
- **`<host>_ig_per_item_standalone`** — each `### N.` IG block must ship ≥1 code block AND name a platform anchor in its first prose paragraph. Catches v20 IG #2 leaning on IG #1.
- **`<env>_service_comment_uniqueness`** (finalize step) — per-service env import.yaml lead-comment blocks must be distinguishable by content tokens (Jaccard ≥ 0.6 → fail). Catches templated copy-paste across services with only hostname swapped.
- **Rollback**: `knowledge_base_exceeds_predecessor` net-new enforcement removed. The check now always passes when applicable and emits the count as informational. Standalone recipes are read in isolation; predecessor overlap is fine. `service_coverage` is the new authoritative gate for "this codebase covers enough categories".
- 45 new tests RED-first across 5 new check files, 3 existing predecessor-floor tests updated to reflect rollback, 1 integration test assertions flipped. Full test suite green under `-race`, `make lint-local` clean (0 issues).
- Brief updates in recipe.md: v8.78 enforcement subsection in knowledge-base Fragment, per-IG-item rule in integration-guide Fragment, cross-file consistency rule in CLAUDE.md template, per-service uniqueness rule in env-comments template, scaffold-subagent self-review checklist (verify imports against installed packages, all commands via SSH, no README/zerops.yaml, dashboard is one panel).

The v8.79 reform is structural (no per-framework hardcoding, no per-recipe lookup tables) and addresses the meta-failure class v20 surfaced: presence-based rules can be satisfied by decorative content; load-bearing-ness is what the next reform must enforce per artifact.

---

### v21 — v8.78 reform ships; operational + workflow regression to D

- **Date**: 2026-04-16
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Model**: claude-opus-4-6[1m] (same as v20)
- **Session**: `72a4639463970897`
- **Session logs**: `main-session.jsonl` (3.1 MB) + 5 subagent logs
- **Wall**: 11:10:45 → 13:19:47 = **129 min** (+82% vs v20's 71 min — worst regression from prior version on record)
- **Assistant events**: 381 (v20: 294, +30%)
- **Tool calls**: 233
- **Bash metrics (main)**: 72 calls / **9.2 min total** (v20: 2.3 min, 4×) / **4 very-long** (v20: 0) / 17 errored (v20: 7)
- **Bash metrics (main + 5 subagents)**: ~118 calls / ~18.7 min / **6 very-long total** (4 main + 2 in scaffold subagents during `nest new scratch`) / 20 errored
- **Subagents (5 — down from v20's 10)**: scaffold ×3 (apidev 347.8 s / workerdev 250 s / appdev 195.7 s), feature ×1 (1325.5 s = 22 min, 27 Writes, 6 MCP schema errors), code review ×1 (166.1 s, 1 ripgrep timeout from the 208 MB bloat). **Missing vs v20**: README/CLAUDE writer, yaml-block updater, generate-time fix subagents (×2), close-step critical-fix subagent — all collapsed
- **MCP tool mix**: 31 `zerops_workflow`, 18 `zerops_dev_server`, 15 `zerops_deploy`, 6 `zerops_browser` (both deploy + close walks held), 4 `zerops_subdomain`, 4 `zerops_knowledge`, 3 `zerops_mount`, 3 `zerops_logs`, 2 each `verify`/`discover`, 1 each `import`/`env`. **0 `zerops_guidance` calls** (v20: 12, v19: 16, v18: 14) — first run since session-logged era with zero on-demand guidance fetches
- **Main-session content decomposition**: tool_result bytes 753 KB (v20: 768 KB, **2% less**), step brief bytes 487 KB (v20: 557 KB, **12% less**), assistant `tool_use` input bytes **320 KB (v20: 131 KB, 144% MORE)** — the bloat is entirely in Write/Edit payloads passed through main-context because of delegation collapse

**Content metrics** (apidev / appdev / workerdev):

- README lines: 230 / 154 / 165 — **compressed 34% / 33% / 38% vs v20's 349/231/267**
- Gotchas: 6 / 4 / 5 — hit exactly the `service_coverage` floor (one per managed-service category)
- IG items: 6 / 3 / 4
- CLAUDE.md: 110 / 96 / 82 lines — all above the 1200-byte floor
- Root README intro: ✅ names real managed services (v17 `dbDriver` validation held for 4th run)
- Preprocessor directive: ✅ all 6 envs carry `#zeropsPreprocessor=on`

**Scaffold hygiene catastrophe (root cause #1 for the 129 min wall)**:

- **apidev codebase**: 209 MB — `node_modules/` (208 MB) + `dist/` (748 KB) + `.DS_Store` files ALL committed into the published recipe output tree
- v21 apidev scaffold brief mentioned `.gitignore`: ❌  /  mentioned `.env.example`: ❌  →  subagent wrote 11 files, **none hygiene**
- v20 apidev scaffold brief mentioned `.gitignore`: ✓  /  mentioned `.env.example`: ✓  →  subagent wrote both
- Same model, same recipe, same recipe.md template text. Main agent's per-codebase brief synthesis is stochastic on the conditional line `only if the framework's own scaffolder normally emits one` at [recipe.md:865](../internal/content/workflows/recipe.md#L865)
- Total recipe output directory: **214 MB** (v20: 4 MB — 54× bloat)

**Downstream blast radius of the 209 MB apidev bloat**:

1. Main agent dispatched 3 parallel `cd /var/www/{apidev,workerdev,appdev} && git init -q && git add -A && sudo chown -R ...` at 11:26:41-43. Each ran zcp-side over SSHFS; `git add -A` traversing 208 MB of `node_modules` hit Bash's 120 s timeout on all three (120.0 / 119.5 / 118.7 s). Subsequent `ssh {host} "cd /var/www && git init"` calls at 11:29:10-45 ran in <10 s each — proving the slowness was SSHFS traversal, not git itself.
2. `.git/` dirs left root-owned from the failed zcp-side init → 6 "Permission denied" tool errors recovering (sudo chown, sudo rm -rf with "Directory not empty", fallback to `sudo /usr/bin/find .git -depth -delete`). ~2 min of extra work.
3. `zcp sync recipe export` ran 70.8 s bundling the bloated tree.
4. Code review subagent hit ripgrep's 20 s timeout traversing `apidev/node_modules` — static review was partially blinded by the bloat it was meant to audit.

**Close-step bugs**: 1 CRITICAL (code-reviewer caught) + 2 WRONG + 4 STYLE:

1. **[CRITICAL]** `app.enableCors({ origin: true, credentials: true })` in [apidev/src/main.ts:10](/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v21/apidev/src/main.ts#L10) — wildcard origin rejected by browsers when credentials also enabled; every credentialed cross-origin POST from appstage to apistage would 403 in production. Fixed by pinning `origin: process.env.CORS_ORIGIN`. Rebuilt + redeployed apidev + cross-deployed to apistage.
2. **[WRONG]** `jobs.controller.ts` throws plain `Error` → HTTP 500 instead of `NotFoundException` → 404. Fixed.
3. **[WRONG]** (caught at runtime during deploy) Worker NATS bootstrap `TypeError: Invalid URL` when `${queue_password}` contains `@` — `nc.connect('nats://user:pass@host:port')` parses as IPv6 literal. Split into `NATS_URL` (credential-free) + separate `NATS_USER`/`NATS_PASS` connection options.
4. Meilisearch `ECONNRESET` during seed (0.25 GB RAM can't ingest + serve health at the same time) — scaled search to 2-4 GB + wrapped `waitForTask` in 5-attempt linear-backoff retry.

**v8.78 content-check effectiveness** (parsed from session log pass/fail event stream):

| Check | Pass events | Fail events | Status |
|---|---:|---:|---|
| `<host>_content_reality` | 12 | 6 | ✓ drove 3 iteration rounds |
| `<host>_gotcha_causal_anchor` | 13 | 5 | ✓ caught decorative gotchas |
| `<host>_service_coverage` | 10 | 2 | ✓ drove coverage; floor set compression target |
| `<host>_ig_per_item_standalone` | 10 | 2 | ✓ |
| `<env>_service_comment_uniqueness` | 24 | 0 | ✓ fires; Jaccard 0.6 threshold may be lenient |
| `<host>_claude_readme_consistency` | **0** | **0** | **✗ DEAD** — regex matches zero content across v21 |
| `knowledge_base_exceeds_predecessor` (rollback) | 18 | 0 | informational as designed |
| `cross_readme_gotcha_uniqueness` | many | 1 | fired; agent didn't fully resolve (SSHFS gotcha still triple-referenced) |

**`claude_readme_consistency` is silently non-functional**. Its `forbiddenPatternRe` at [workflow_checks_claude_consistency.go:89](../internal/tools/workflow_checks_claude_consistency.go#L89) requires strict phrasings (`` `X` must be off in production``, `` never use `X` ``). Shadow-tested the regex against all session-logged README content: v20 apidev = 1 hit (the exact string it was designed around); v19/v20-appdev/v20-worker/v21-all = **0 hits**. The check was designed against the one v20 phrasing, can only match that phrasing, and provides no coverage for natural drift.

**Six `zerops_dev_server action=stop` exit-255 events** at 11:39:29-30Z and 12:07:02-03Z (2 clusters × 3 hosts). Root cause: [dev_server_lifecycle.go:22-27](../internal/ops/dev_server_lifecycle.go#L22-L27) runs `pkill -f <match>` over SSH; `pkill -f nest` matches any process containing "nest" in its command-line including the outer `sh -c` running pkill, so SSH drops with exit 255. Tool surfaces raw 255 rather than classifying "process killed, SSH dropped — normal stop outcome". v20 hit this once; v21 hit it 6 times because v21 made 18 dev_server calls (v20: 10) with more stop/restart cycles.

**Feature subagent hit 6 MCP schema-validation errors** using memory-frozen parameter names:

- `zerops_dev_server` missing `hostname` parameter (correct: `hostname` — actually this one's fine, the error was about a different call)
- `zerops_dev_server action=status` called without `port`
- `port="3000"` string instead of integer → `has type "string", want "integer"`
- `zerops_logs` called with `hostname` + `logLines` (correct: `serviceHostname` + `lines`)
- `zerops_scale` called with `ramGB` (no such property)

Each rejection is a retry round-trip. The feature-subagent-brief (8924 chars) doesn't inline exact MCP schemas; agent defaulted to memory-plausible names.

**Framework hardcoding audit** (second-pass after user challenge surfaces 6 HIGH + 4 MEDIUM violations of v8.78's "framework-agnostic by design" claim):

- [`workflow_checks_service_coverage.go:100-101`](../internal/tools/workflow_checks_service_coverage.go#L100): `categoryBrands` hardcodes `typeorm`, `prisma` under db; `ioredis`, `keydb` under cache — ORMs and Node-specific client libraries treated as service-category signals
- [`workflow_checks_causal_anchor.go:127-132`](../internal/tools/workflow_checks_causal_anchor.go#L127): `specificMechanismTokens` hardcodes `TypeORM synchronize`, `TypeORM migrationsRun`, `queue: 'workers'`, `ioredis`, `keydb` as "Zerops mechanisms" — framework + library tokens given platform-anchor credit
- [recipe.md:1957-1961](../internal/content/workflows/recipe.md#L1957): the v8.78 CLAUDE.md cross-file consistency rule section uses the v20 NestJS+TypeORM synchronize case study as its sole worked example (biases agent toward NestJS interpretation)
- [`recipe_templates.go:202-238`](../internal/workflow/recipe_templates.go#L202-L238): 30+ framework→URL map (legitimate pragma for published recipe pages but flagged)
- [`knowledge/engine.go:201-214`](../internal/knowledge/engine.go#L201-L214): `runtimeRecipeHints` has Node frameworks as primary keys
- [`recipe_decisions.go:44-68`](../internal/workflow/recipe_decisions.go#L44-L68): `ResolveDevTooling` hardcodes `laravel || symfony → watch` for dev strategy

**Notable**:

- **First D-grade run since v13** (v17 was F-grade — an abort; v21 completed but at D). 4-grade slide from v20's A− in one version.
- **First run with 0 `zerops_guidance` calls**. Progressive-guidance delivery was unused by main agent AND all subagents. Step briefs themselves were 12% smaller than v20 and `tool_result` bytes were 2% less — workflow served LESS content, not more.
- **Mechanism of delegation collapse remains uncertain**. v18-v20 emergent-dispatched 7-10 subagents (README writer, yaml updater, generate-time fix×2, close-step critical-fix); v21 dispatched only 3 required + feature + review = 5. recipe.md does NOT explicitly instruct these emergent dispatches at either run's time; v21's agent simply chose to absorb the work. No measurable brief-density explanation (step briefs smaller, not larger). True cause appears to be agent-behavior variance without workflow-layer forcing gates.
- **Content compression traces to ceiling-without-floor**. v8.78 added 3 per-gotcha quality checks; rolled back predecessor-floor from gate to informational. With quality pressure down and minimum floor at just "one gotcha per service category", the agent's shortest path to green is to delete weak gotchas. v21 landed at exactly that floor.
- **Per-subagent exhaustive mining** (third-pass after user challenge):
  - apidev scaffold: 347.8 s, 1 very-long (60.4 s `nest new scratch`), 11 Writes (zero hygiene). Ran `nest new scratch` as reference but didn't transcribe `.gitignore` from it.
  - workerdev scaffold: 250 s, 1 very-long (75 s `nest new scratch`), 10 Writes including `.gitignore` (from model memory, content drifts from Nest default) but NO `.env.example`.
  - appdev scaffold: 195.7 s, 14 Writes — the only codebase with both hygiene files correct.
  - feature subagent: 1325.5 s (22 min), 27 Writes + 4 Edits, 6 MCP schema errors, 14 `zerops_dev_server` calls.
  - code review: 166.1 s, 43 Reads + 8 Greps (1 ripgrep timeout), 0 Writes.
- **Attribution math** for the 58-min wall-clock regression: 3 parallel 120 s git-add = 2 min wall; recipe-export 1 min; root-ownership cascade 2 min; 2 extra README iterations absorbed in main 10-15 min; writer+fix subagent work absorbed in main 15-20 min; MCP schema retries 1-2 min; dev_server 255 recovery 2-3 min; longer thinking 3 min. Median ~50 min of ~58 min observed (86% coverage).

**Rating**: S=**B** (all 6 steps completed, 5 features, both browser walks, but 1 close CRIT + 3 runtime CRITs required recovery), C=**C** (v8.78 drove iteration on 4 of 5 new checks correctly but net content compressed 26%; 1 flagship check silently dead; triple-duplicate SSHFS gotcha remained; suspect NATS JetStream gotcha factual accuracy), O=**D** (129 min wall, 9.2 min main bash, 6 very-long cumulative — entirely attributable to the 209 MB scaffold bloat and its SSHFS cascade), W=**D** (0 guidance calls, 5 subagents vs v20's 10, zcp-side git trap re-surfaced on main agent, missing writer/yaml/fix/critical-fix delegation) → **D**

*Worst single-version regression on record. The v8.78 reform's content-quality checks worked structurally — drove real iteration cycles on real content defects — but four systemic interactions collapsed the operational and workflow dimensions: (1) a single conditional line in scaffold-brief template produced 209 MB of hygiene-file bloat → 120 s SSHFS cascades, (2) v17.1's "mount is not an execution surface" preamble lives in scaffold-brief only, not main-agent workflow, (3) the `claude_readme_consistency` regex was designed around v20's exact phrasing and shadow-tests reveal it matches nothing else, (4) emergent delegation patterns that carried v18-v20 were never formalized in recipe.md as required dispatches, so v21's agent variance abolished them with no gate to refuse. Post-mortem + v8.80 implementation guide at [docs/implementation-v21-postmortem.md](implementation-v21-postmortem.md) proposes 9 fixes, every one at the code or workflow-gate layer rather than brief-content layer — determinism upgrade after initial hedge.*

**v21 post-mortem fixes planned for v8.80** (see [implementation-v21-postmortem.md](implementation-v21-postmortem.md) for full specs):

- **§3.1 `scaffold_hygiene` check** — runtime deploy-step check fails when `.gitignore` / `.env.example` missing OR when `node_modules`/`dist`/`build`/`.DS_Store` leaked into published tree. Structurally prevents the 209 MB class.
- **§3.2a bash-guard middleware** — MCP tool-level rejection of `cd /var/www/{host} && <executable>` patterns with structured error + correction suggestion. Closes the v17-class zcp-side execution trap on main-agent surface.
- **§3.3 `gotcha_depth_floor` check** — per-role minimum gotcha count (api 5 / frontend 3 / worker 4). Replaces predecessor-floor's quantitative pressure without re-introducing predecessor coupling.
- **§3.4 `claude_readme_consistency` rewrite** — replace regex-keyed-on-README-phrasing with closed set of pattern-based forbidden hazards + whole-document cross-reference markers. Shadow-tested against v18/v19/v20/v21 content.
- **§3.5 framework-token purge** — strip `typeorm`, `prisma`, `ioredis`, `keydb`, `TypeORM synchronize`, `queue: 'workers'` from check classification lists. Enforces "framework-agnostic by design" claim.
- **§3.6d workflow-gate for subagent dispatches** — `readmes` sub-step complete rejected unless a writer-subagent dispatch was observed; `close` step rejected unless critical-fix subagent dispatched when CRITs found. Formalizes the v18-v20 emergent delegation patterns.
- **§3.6e MCP schema-error rename suggestions** — schema validation errors carry explicit `hostname → serviceHostname` rename hints; guarantees first-retry correction.
- **§3.6f scaffold-reference content-diff check** — runtime diff between each codebase's `.gitignore` and a fresh `npx <framework-cli> new scratch` output. Forces content-equivalence to framework convention.
- **§3.7a `dev_server stop` self-kill classification** — `pkill --ignore-ancestors` where supported + `isSSHSelfKill` classifier that converts ssh exit 255 into structured "stopped, ssh dropped normally" success.

All 9 fixes are at the code or workflow-gate layer (not brief-content). With these enforced gates, v22's deliverable state is constrained to the gate-accepting region; agent stochasticity only operates within the envelope the gates permit.

---

### v22 — v8.80 gates hold; first Opus 4.7 run; deliverable restored, cost axes regress

- **Date**: 2026-04-16
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Model**: **claude-opus-4-7 (1M context) — FIRST RUN on Opus 4.7**, same tier shape as v20/v21 on Opus 4.6
- **Session**: `5854102f96b4c461`
- **Session logs**: `main-session.jsonl` (3.5 MB) + 9 subagent logs
- **Wall**: 21:36:19 → 23:19:56 = **103 min 37 s** (+45% vs v20's 71 min; −20% vs v21's 129 min)
- **Assistant events**: **410** — new complete-run record (v13's 489 was on Sonnet; prior Opus peak was v16's 370)
- **Tool calls**: **243** (v20: 177, v21: 233) — tied-high ever
- **Bash metrics (main)**: 77 calls / **2.5 min total** / **0 very-long** / 3 long / **7 errored** / 0 port kills / 2 sleeps / 12 curls
- **Bash metrics (main + 9 subagents)**: ~230 calls / ~13.4 min total / **2 very-long** (both `nest new scratch` inside scaffold subagents — 84 s apidev, 65 s workerdev) / ~11 errored
- **Subagents (9)**:
  - Scaffold ×3: apidev (`a786b79…`, 245 s, 46 bash, 1 very-long), workerdev (`abba2b5…`, 99 s, 17 bash, 1 very-long), appdev (`afa835c…`, 30 s, 11 bash, 1 long)
  - Feature ×1 (`a504608…`, **14 min 13 s**, 179 events, 41 bash + 28 Write + 28 Read + 15 Edit, **0 MCP schema errors** — v21 had 6)
  - README/CLAUDE writer ×1 (`af6bad9…`, 0.5 s bash — 9 quick `ls`/`awk` probes only, then pure Writes)
  - Env-comments writer ×1 (`af4175f…`, **0 bash** — pure Write) — new subagent role vs v20
  - **3 parallel per-codebase framework-expert code reviews** at close step (NEW split vs v20's single code-review subagent): apidev (`a67388c…`, 71 s / 49 events), appdev (`a1076ba…`, 11 s / 40 events), workerdev (`abb1ea2…`, 27 s / 31 events)
- **MCP tool mix**: 33 `zerops_workflow`, **17 `zerops_guidance`** (v21: 0, v20: 12 — progressive-guidance delivery restored), 15 `zerops_dev_server`, 11 `zerops_deploy`, **8 `zerops_browser`** (new highest — deploy.browser ×5 + close.browser ×3), 5 `zerops_subdomain`, 3 `zerops_mount`, 3 `zerops_logs`, 2 `zerops_verify`, 1 each `zerops_knowledge` / `import` / `env` / `discover`
- **Main-session Edit/Write shape** (the cost story): 31 `Edit` + 4 `Write` = 35 content ops in main context. Distribution: 11 Edits on `workerdev/README.md`, 8 Edits on `apidev/README.md`, 5 Edits on `workerdev/CLAUDE.md`, 2 Edits on `appdev/README.md`, 2 Edits on `apidev/src/services/nats.service.ts` (NATS URL CRIT fix), 1 each on `apidev/src/services/storage.service.ts` (S3 301 fix), `workerdev/src/worker.service.ts` (NATS URL CRIT), `apidev/zerops.yaml`, `appdev/zerops.yaml`, `workerdev/zerops.yaml`, `apidev/CLAUDE.md`. Writer subagent dispatch fired but post-writer iteration leaked back to main.

**Content metrics** (apidev / appdev / workerdev):

- README lines: **341 / 208 / 221** (v20: 349/231/267, v21: 230/154/165) — recovers to v20 range, apidev nearly at v20 peak, appdev/workerdev between v19 and v20
- Gotchas: **7 / 6 / 6** — **matches v20's record** (v21: 6/4/5)
- IG items: **8 / 6 / 6** — **apidev at 8 is NEW RECORD** (prior peak v15/v20: 6); appdev/worker match v20 peak minus 1
- CLAUDE.md: 133 / 103 / 140 lines, **7307 / 6245 / 7724 bytes** — **largest ever across all three codebases** (v20: 3395/2786/3728; v18: 4134/2565/3340). All three carry ≥7 custom sections beyond the 4-section template (e.g. apidev adds Resetting Dev State, Driving a Test Request, Forced Verification, Recovering from a Burned `zsc execOnce` Key, Adding a Managed Service)
- Root README intro: ✅ names real managed services — *"connected to PostgreSQL, Valkey (Redis-compatible), NATS, S3-compatible object storage, and Meilisearch"* (v17 `dbDriver` validation holds for 5th consecutive run)
- Preprocessor directive: ✅ all 6 envs carry `#zeropsPreprocessor=on` with `<@generateRandomString(<32>)>` for `APP_SECRET`

**Env 4 (Small Production) comment quality — gold standard, two-axis teaching fully applied**:

- `app` (static): *"minContainers: 2 — throughput-scaling isn't what drives this because Nginx asset serving has near-zero CPU per replica and one container could sustain the traffic; the sole reason for two replicas is HA and rolling-deploy availability, so that during a deploy the load balancer can drain one replica while the other keeps serving and users never see a blip."*
- `api`: both throughput (*"two Node event loops"*) AND HA (*"readinessCheck on /api/health while the other keeps serving"*) named as distinct reasons.
- `worker`: queue-group `workers` fan-out for throughput + `nc.drain()` mid-deploy for HA — explicit two-axis distinction.
- `db`: `zsc execOnce ${appVersionId}` + `pgcrypto` for `gen_random_uuid()` named.
- `queue`: `${queue_password}` URL-reserved chars named as prod-boot failure mechanism.
- `storage`: `HEAD-BUCKET` gate vs `NoSuchBucket` cold-deploy race.
- `search`: `waitTask` against `${search_masterKey}` + destructive-seed protection.

Every service block carries a WHY — this is the cleanest env-4 comment file since v7's gold-standard baseline.

**Gotcha authenticity audit** (platform-anchor + concrete-failure-mode test):
- **apidev (7/7 pass structural checks)**: `${queue_password}` URL-reserved → `TypeError: Invalid URL`; S3 301 http→https → SDK throws `NotFound`; `DB_*` → native `${db_hostname}` / `${db_dbName}` mapping required (camelCase trap) → `ECONNREFUSED 127.0.0.1:5432`; Valkey env-name divergence (`redis_hostname` vs `redis_host`) → `{"redis":"error"}` on status; stage replica receives traffic before `initCommands` finish without `readinessCheck` → `QueryFailedError: relation "items" does not exist`; Meilisearch `search_masterKey` is the only write-scoped key → `MeiliSearchApiError: The provided API key is invalid.` + `execOnce` burn trap; `seed.ts` silent no-op on `execOnce` retry with `${appVersionId}` stable.
- **appdev (6/6 pass structural checks)**: missing `httpSupport: true` → `serviceStackIsNotHttp`; `run.base: static` for dev breaks every SSH command; CORS `No 'Access-Control-Allow-Origin'` preflight reject; stage `/api/*` returns `200 text/html` (SPA fallback + `SyntaxError: Unexpected token '<'`); SSHFS + chokidar polling; `httpSupport: true` per replica for L7 balancer registration.
- **workerdev (6/6 pass structural checks)**: `processed_at` drift reveals double-processing at `minContainers: 2+` without `{ queue: 'workers' }`; SIGTERM drops in-flight `UPDATE` without `onModuleDestroy`; `ECONNREFUSED 127.0.0.1:5432` from missing `${db_hostname}` mapping; `deploy.readinessCheck` on headless worker rejected with `serviceStackIsNotHttp`; `run.prepareCommands` bloat adds 30+ s cold-start; `JobPayload` drift → literal `"undefined"` in Postgres `result`.

**Cross-README dedup**: holds structurally — each fact lives in exactly one codebase.

**⚠ Deeper content audit — gotchas-as-run-incident-log (user's hypothesis, FULLY VALIDATED)**:

A gotcha-to-session-event correlation audit across all 19 gotchas classified **19/19 as RUN-INCIDENT** (0 framework×platform, 0 generic, 0 mixed). Every single gotcha maps 1:1 to a specific failure the scaffold or feature subagent encountered and fixed in this exact run, with **exact error strings visible only to someone who watched the session** — `TypeError: Invalid URL`, `MeiliSearchApiError: The provided API key is invalid.`, `processed_at` drifts backward, `SyntaxError: Unexpected token '<'`, `sh: 1: npm: not found`, `ECONNREFUSED 127.0.0.1:5432`, `QueryFailedError: relation "items" does not exist`.

The gotchas ARE correctly-phrased platform invariants at the prose level (they name real mechanisms: `readinessCheck`, `${queue_password}`, `serviceStackIsNotHttp`, `zsc execOnce ${appVersionId}`, L7 balancer), and they pass the v8.79 `gotcha_causal_anchor` check at token level — this is **decoratively compliant** content. But their ORIGIN is this run's post-mortem:

- **What this means for future runs**: a v23+ run on the same NestJS scaffold with the same tool set would likely hit the same ~19 incidents. The gotcha set is reproducible-post-mortem shape, not platform-knowledge shape.
- **What this misses for porting users**: the framework × platform grid the recipe doesn't exercise. A user porting their own NestJS code (different ORM, different queue library, streaming multipart, CDN in front, `citext` columns, composite DB indexes) would hit a DIFFERENT set of traps; v22's gotchas don't map the platform's surface — they map this specific scaffold's path through it.
- **Implicit test the structural checks miss**: *"Would a random NestJS → Zerops porter hit exactly these 19 traps, or do these 19 fingerprint THIS scaffold's specific choices?"* — v22's gotcha set fails this test.

The v8.78 `gotcha_causal_anchor` check was designed to prevent decorative gotchas that *don't* name mechanisms. v22 shows the next failure mode: gotchas that *do* name mechanisms but are incident-derived, not invariant-derived. Every gotcha is a valid platform fact AND a scaffold-run fact at the same time — the checks can't distinguish, but the content's load-bearing-ness depends on which origin the gotcha came from.

**Deeper content coherence read — byte-size doesn't measure quality**:

| Axis | Grade | Finding |
|---|---|---|
| Gotcha depth (invariant-shape in prose) | **B+** | 14/19 read as invariants at prose level, 5 as post-mortems in phrasing — but the correlation audit shows ALL 19 are run-incident at origin |
| IG items standalone | **A−** | one decorative duplication: appdev IG #3 restates appdev gotcha #4's substance |
| CLAUDE.md vs README | **A** | tight separation: README = philosophy, CLAUDE.md = procedure; zero shadowing, zero contradicting (v20 `synchronize` regression didn't recur) |
| **Three-codebase coherence** | **C+** | **islands with bridges, not an arc — THE LIMITER**. Root README is a link aggregator, not an architecture narrator. A reader on `workerdev/README.md` learns queue groups but never sees the publisher shape from apidev. appdev sees `proxy_pass http://apidev:3000` but isn't told why apidev's CORS config must name both `${DEV_APP_URL}` and `${STAGE_APP_URL}`. Cross-codebase explicit references are sparse (`appdev → apidev` CORS naming exists, reciprocals don't). |
| Env-4 comment decision-rationale | **A** | all 8 service blocks carry explicit WHY + rejected alternative; two-axis `minContainers` teaching intact on all 3 runtime services; no templated per-service opening |
| Root → deepest gotcha thread | **B** | per-codebase threads are clear; architecture (why three codebases exist, how they contract) is implicit — nowhere does the recipe state "apidev publishes jobs, workerdev consumes via queue group, appdev never subscribes" |
| Signal/noise | **A−** | clean tree, no stale TODOs or placeholder code; appdev CLAUDE.md has one well-framed non-use callout (`vite preview` we-don't-use clause) |

**Overall qualitative content grade: B+** (limited by cross-codebase coherence at C+). The recipe is professionally written and operationally sound per-codebase, but the 3-service integration story lives in the reader's head, not in the doc. A root-level "Architecture" section naming the three-service split and their contract boundaries would close this gap without inflating byte counts.

**v8.80 gate effectiveness** (the v21 post-mortem validation):

| Gate | Fired? | Outcome |
|---|---|---|
| §3.1 `scaffold_hygiene` runtime check | ✅ fired 4× on api+worker | Caught `node_modules/` + `dist/` leaks, forced multi-host `rm -rf` cleanup (25.4 s); published tree 6.1 MB vs v21's 214 MB (35× smaller) |
| §3.2a bash-guard middleware for `cd /var/www/{host}` | Silent — main agent never attempted the zcp-side pattern | Preamble + rule held; no violation to catch |
| §3.3 `gotcha_depth_floor` per-role minimum | Satisfied without enforcement triggering | 7/6/6 exceeds api 5 / frontend 3 / worker 4 floors |
| §3.4 `claude_readme_consistency` pattern-based rewrite | **✅ fired 4+4× on api+worker** | Caught TypeORM synchronize in CLAUDE.md dev-loop without dev-only marker — v21 the original regex was dead, v8.80 rewrite is alive |
| §3.5 framework-token purge from `categoryBrands` / `specificMechanismTokens` | Passive — no token-contamination false-positives observed | Gotcha classification clean; no framework-agnostic violations |
| §3.6d writer-subagent dispatch gate | ✅ dispatch fired (`af6bad9…`) | Gate accepted; but post-writer iteration leaked back to main — gate prevents dispatch-absence, not post-writer-main-drift |
| §3.6e MCP schema-error rename hints | **✅ 0 schema errors observed across main + feature subagent** (v21: 6) | Prevention worked; no rename needed |
| §3.6f scaffold-reference content-diff | Not directly measurable | Scaffold output close to `nest new scratch` baseline |
| §3.7a `dev_server stop` self-kill classification | ✅ silent | 0 exit-255 classifications as failure (v21: 6) — apidev + appdev stops returned clean `"matched \"nest\"/\"vite\""` messages; workerdev stop returned empty (worker was idle at stop time). The 6 `exit 255` string matches in the main log are all guidance-text warnings about prior SSH session state after redeploy, not failure reasons |

**Content-check pass/fail burn** (parsed from session event stream — fail events by check × codebase, driving iteration rounds):

| Check | api | app | worker | cross |
|---|---:|---:|---:|---:|
| `content_reality` | 12 | 0 | **16** | — |
| `gotcha_causal_anchor` | 8 | 8 | **16** | — |
| `gotcha_distinct_from_guide` | 4 | 8 | **16** | — |
| `ig_per_item_standalone` | 4 | 0 | 0 | — |
| `knowledge_base_authenticity` | 4 | 0 | 0 | — |
| `scaffold_hygiene` | 8 | 4 | 8 | — |
| `service_coverage` | 8 | 0 | 4 | — |
| `claude_readme_consistency` | **4** | 0 | **4** | — |
| `cross_readme_gotcha_uniqueness` | — | — | — | 4 |

Worker content took the heaviest iteration load (16/16/16/8 on 4 load-bearing checks). The v8.78 reform's quality pressure is high enough to drive real rewrites — but the dispatch shape routed all rewrites into main context instead of a dedicated content-fix subagent, hence the 11 Edits on `workerdev/README.md`.

**Deploy-step CRITs (all fixed inline in main, then redeployed + re-verified)**:

1. **[CRIT]** NATS `TypeError: Invalid URL` on `${queue_password}` containing URL-reserved chars (`@`, `#`, `/`, `?`) — apidev `NatsService` + workerdev `WorkerService` both shipped `nats://${user}:${pass}@${host}:${port}` string literals. **Recurrence of the v21 class** — the v21 TIMELINE documented the same bracketed-`@` failure, the v21 gotcha was added to apidev README in the post-mortem, but the v22 scaffold subagents re-emitted URL-embedded-creds code despite the gotcha being in scope. Fixed: pass `user` / `pass` as separate `ConnectionOptions` fields; `servers` becomes bare `${queue_hostname}:${queue_port}`.
2. **[CRIT]** S3 `HeadBucketCommand` fails with 301 redirect — apidev `storage.service.ts` built endpoint as `http://${storage_apiHost}` but Zerops' object-storage proxy redirects HTTP→HTTPS and the AWS SDK does not follow. Fixed: prefer `process.env.storage_apiUrl` (already `https://`).
3. **[CRIT]** workerdev `zerops_dev_server start` returned `post_spawn_exit` with `Cannot find module '/var/www/dist/main.js'` — scaffold subagent wired dev start as `node dist/main.js` but dev `buildCommands` runs `npm install` only (no `npm run build`), so `dist/` doesn't exist on the dev container. Fixed: use `npx ts-node -r tsconfig-paths/register src/main.ts` for dev; reserve `node dist/main.js` for prod.

**Feature-subagent-era CRIT (fixed during generate)**:

- **[CRIT]** `Nest can't resolve dependencies of CacheFeatureController (?) ... at index [0]` — after splitting controllers into per-feature modules, the four shared infra services (`CacheService`, `NatsService`, `StorageService`, `SearchService`) weren't reachable from feature modules. Fixed with `@Global() @Module({ providers: [...], exports: [...] })` wrapper in `src/services/services.module.ts`.

**Deploy-step iteration counts** (workflow event stream):
- `scaffold_hygiene` fail events: 4
- `deploy.readmes` substep completes: 2 (one fail iteration)
- `feature-sweep-dev` mentions: 20 (sub-step exercised repeatedly across fix cycles)
- `INVALID_PARAMETER` substep-gate rejections: 1 (agent tried `complete step=deploy` before all 12 required sub-steps — gate forced completion of browser-walk + cross-deploy first)

**Finalize iterations**: 2 (`generate-finalize` fired twice — standard pass-after-fix pattern)

**Close-step bugs** (3 parallel framework-expert subagents, combined result): **0 CRITICAL / 0 WRONG / 4 STYLE**. Cleanest close since **v14's 0/0 review** — and the first run since v14 to combine a spotless close review with peak content depth. STYLE notes: runtime validation on POST bodies (acceptable for recipe demo), `tsconfig` strictness drift between worker + api, minor import-sort drift.

**`is_error=true` tool_results breakdown** (12 total):
- 3× `mcp__zerops__zerops_deploy/subdomain: "Not connected"` — intermittent MCP connection drops (infrastructure flake, agent retried cleanly)
- 2× Bash "Cancelled: parallel tool call" — pre-bash hook cancellations on chained commands
- 1× `INVALID_PARAMETER` substep gate — enforcement working as designed
- 1× `mcp__zerops__zerops_logs` cancelled — parallel-call hook
- 1× Exit 127 `psql: command not found` — agent expected psql on zcp side (not installed), recovered via ssh into db service
- 1× Exit 127 `ssh: command not found` — from inside a `ts-node -e` eval context (ssh unavailable in sandboxed subprocess)
- 1× `Exit code 1` + "nothing to commit" — benign git noop
- 1× Bash `Exit code 1` + `TypeError: A dynamic import callback was not specified` — ts-node ESM edge case in an ad-hoc probe
- 1× Bash git init hint exit-1 (benign)

No MCP schema validation errors (v21: 6). No `dev_server` spawn hangs (v17: 300s). No `pkill` self-kill false failures (v21: 6).

**Notable**:

- **First Opus 4.7 run — likely explains a meaningful slice of the 410 events / 243 tool calls**. Opus 4.7's deliberation pattern produces more tool calls per decision than Opus 4.6; v22's 243 tool calls on a structurally cleaner run than v21's 233 suggests the model-tier shift accounts for some of the operational-cost regression independent of the workflow.
- **v8.80 was the right fix set** — every structural gate v8.80 shipped held under a real run. `scaffold_hygiene` caught the exact v21-class leak and forced remediation; `claude_readme_consistency` caught TypeORM synchronize in CLAUDE.md (v21's regex matched 0 content); MCP schema-rename hints delivered 0 rejection retries vs v21's 6; `pkill --ignore-ancestors` + `isSSHSelfKill` classifier resulted in 0 exit-255 surfaced failures. The v8.80 implementation closed the v21 regression classes exactly as planned.
- **Writer-subagent dispatch gate is necessary but not sufficient**. §3.6d forced the `af6bad9…` dispatch, but content checks continued to fire after the writer returned, and main absorbed the iteration (11 Edits on `workerdev/README.md`). A follow-up pattern — post-writer **content-fix subagent** dispatch when `gotcha_causal_anchor`/`content_reality`/`claude_readme_consistency` fail after `readmes` substep — would move this iteration load back out of main context and match the v20 delegation shape.
- **NATS URL-credentials CRIT is a recurrence class** — v21 TIMELINE ship this as a close-step CRIT with fix; v21 post-mortem added the gotcha to apidev README; v22 scaffold subagents re-emitted URL-embedded-creds NATS client code anyway. The gotcha being in the published README after the run doesn't help the run itself — the scaffold subagent brief needs an **explicit forbid-URL-embedded-NATS-creds preamble** rather than relying on downstream gotcha presence.
- **S3 HTTPS endpoint issue is also a recurrence class** — the S3 SDK `followRedirects` behavior is documented on AWS's side, but Zerops-specific HTTP→HTTPS redirect on object-storage isn't in the scaffold preamble. Either the scaffold brief lists `use storage_apiUrl, not storage_apiHost` explicitly, or the scaffold-reference diff check catches it against a known-good S3 service file.
- **workerdev `dist/main.js` class** is new — v22 surface of a contract drift between `buildCommands` (dev runs `npm install` only) and dev-start command (scaffold chose compiled `node dist/main.js`). Either dev `buildCommands` must run build, or dev-start must use `ts-node src/main.ts`. Knowledge-base addition needed in scaffold brief.
- **Per-codebase code-review split is a new pattern worth keeping** — 3 parallel subagents finished in 71/11/27 s (total wall ≤1:30 vs a single unified review) and each fit its codebase's framework expertise cleanly. Total subagent count 9 vs v20's 10 is the only axis where delegation isn't at-or-above v20, and the 3-split replaces 1 unified review → net additive expertise, not fragmentation.
- **11 Edits on `workerdev/README.md` is the single largest in-main content-iteration load on record**, driven by `worker_content_reality: 16 fails` + `worker_gotcha_causal_anchor: 16 fails` + `worker_gotcha_distinct_from_guide: 16 fails` + `worker_claude_readme_consistency: 4 fails` + `worker_scaffold_hygiene: 8 fails`. The deliverable is strong; the shape of how it got there is costly.
- **User-framing context**: viewed against v21 on pure cost-volume axes, v22 is "almost as bad" (events 410 > 381, tool calls 243 > 233). Viewed against v21 on substance axes, v22 is dramatically better (6.1 MB vs 214 MB, 0 close CRIT vs 1, peak content vs floor, 9 subagents vs 5, 0 schema errors vs 6, 0 exit-255 failures vs 6, claude_readme_consistency alive vs dead). The regression is real in cost — the deliverable is not.

**Rating**: S=**B** (3 deploy CRITs + 1 DI CRIT required inline fix + redeploy cycles; close review spotless), C=**B+** (passing token-level structural checks at peak counts but qualitative read reveals **19/19 gotchas are run-incident-shaped at origin** AND cross-codebase coherence is the C+ limiter — the recipe teaches this scaffold's path through the platform, not the platform's surface; env-4 comments + CLAUDE.md quality carry the overall grade up from C+ to B+), O=**B** (wall 103 min ≤120 = B, bash 2.5 min = A, 0 very-long main = A, dev-server sum ~0 s delegated = A, errored main 7 = A — wall is the single limiter; assistant event 410 + tool call 243 are cost-volume records, but Opus 4.7 think/tool ratio is a healthy ~2:1 so the model is NOT the per-decision driver — the ~15 min Phase-4 content-iteration leak is), W=**B** (9 subagents incl. 3-split code review + env-comments writer NEW pattern, writer dispatch gate fired, both browsers fired, 17 guidance calls, all v8.80 gates held — but writer-to-main post-handoff leak drove 11 workerdev-README edits + 8 apidev-README edits + 5 workerdev-CLAUDE edits into main in a Read-once / Edit-many / no-verify pattern that absorbed ~15 min of wall time into main context; 1 substep gate rejection shows agent tried premature deploy-complete) → **B**

*v22 is a meaningful recovery from v21's D but below v20's A−. The v8.80 structural gate set is fully validated — every v21-regression class closed. But two new structural failure modes surface that v8.78–v8.80 can't catch by token-level inspection: **(1)** gotchas-as-run-incident-log (content passes `gotcha_causal_anchor` because it names real mechanisms, but the gotcha SET is this scaffold's incident-fingerprint rather than the platform's trap surface — 19/19 gotchas correlate 1:1 to session-log incidents with run-specific error strings), and **(2)** content-check iteration rounds absorbed into main after writer-subagent handoff (11 Edits on workerdev/README.md in a Read-once Edit-many pattern, ~15 min wall cost). Primary follow-up: (a) post-writer **content-fix subagent dispatch gate** to move the iteration load out of main context; (b) a new content check that targets gotcha-origin (requires the content set to include gotchas the scaffold did NOT trigger — framework × platform invariants a PORTER would hit even on a clean-first-try scaffold) rather than token-level anchor compliance; (c) scaffold-brief preambles on the three recurrence classes (NATS URL-embedded creds, S3 `storage_apiUrl`, dev-start vs buildCommands contract); (d) root-README architecture section to close the cross-codebase coherence C+ limiter.*

**v22 post-mortem items (candidates for v8.81)**:

- **§4.1 post-writer content-fix subagent dispatch gate** — when any of `<host>_content_reality`, `<host>_gotcha_causal_anchor`, `<host>_gotcha_distinct_from_guide`, `<host>_claude_readme_consistency` fail AFTER the `readmes` substep `complete` event, require a content-fix subagent dispatch before the `complete step=deploy` gate accepts. Structurally prevents the 15-min Phase-4 iteration-into-main pattern. This is the single highest-value fix.
- **§4.2 gotcha-origin guard (the v22 qualitative finding)** — current content rules score gotchas at token level (names a Zerops mechanism + names a concrete failure mode). v22 shows this is satisfied by incident-derived content. Proposed check: `<host>_gotcha_origin_diversity` — require at least N gotchas per codebase that name a mechanism the agent did NOT invoke during the run (prove the gotcha set includes framework × platform invariants a PORTER would hit, not just the scaffold's own incident-fingerprint). Detect incident-origin by string-matching gotcha error-token phrases against the session-log `tool_result` contents — if the exact error string appears in a `tool_result`, flag the gotcha as incident-derived. Hard to automate perfectly but a floor of "≥2 gotchas per codebase whose error strings do NOT appear in the session log" would force the content set to include at least some pure-knowledge gotchas.
- **§4.3 scaffold-brief NATS credentials preamble** — explicit forbid on URL-embedded `user:pass@host` for nats.js@2; pass as separate `ConnectionOptions` fields. Prevents the v21→v22 recurrence class (two consecutive runs hit this exact CRIT; having the gotcha in the v21 published README doesn't help the v22 scaffolder).
- **§4.4 scaffold-brief S3 endpoint preamble** — prefer `process.env.storage_apiUrl` over `http://${storage_apiHost}` (AWS SDK does not follow 301 redirects). Zerops-object-storage-specific pragma.
- **§4.5 dev-start vs `buildCommands` contract check** — at `zerops.yaml` finalize, if `run.start` references `dist/*.js` but dev `buildCommands` does not run a build, fail with specific remediation (use `ts-node` for dev or add build to dev buildCommands).
- **§4.6 cross-codebase coherence check (root README architecture narrative)** — new optional content check at finalize step: `recipe_architecture_narrative` — for showcase-tier recipes with ≥2 codebases, the root `README.md` must contain a section that (a) names each codebase by hostname, (b) names each codebase's role, and (c) names each inter-codebase contract (apidev publishes → queue → workerdev consumes; appdev fetches → apidev CORS-scoped origins; etc.). Closes the C+ cross-codebase coherence limiter without forcing per-codebase cross-references (which get stale). Failure is a LOW (informational) not a hard gate.
- **§4.7 Opus 4.7 cost-budget rubric adjustment** — current O-dimension rubric is bash-centric (wall / bash total / very-long / dev-server / errored). v22 shows assistant-event + tool-call volume can regress without any bash-axis regression (main bash 2.5 min is A-range, but 410 events and 243 tool calls are new records). Add explicit bands: A ≤300 events + ≤200 tool calls; B ≤400 + ≤250; C ≤500 + ≤300. This puts the model-variance cost story directly in the rubric.
- **§4.8 promote 3-split framework-expert code review to default** — v22's 3 parallel per-codebase code-review subagents finished in 71/11/27 s (each framework-scoped review ran in parallel; unified review in v20 was similarly fast but single-codebase-focused). The split pattern scales naturally to multi-codebase recipes, and v22's 0 CRIT / 0 WRONG close outcome proves the pattern's coverage is at-or-above the unified pattern. Promote to the default `close.code-review` substep shape for ≥2-codebase recipes.
- **§4.9 subagent wait-vs-parallel documentation** — the inefficiency audit surfaces that main never waited for any subagent (subagents were fire-and-forget, main interleaved its own tool work). For scaffolds this is correct (3 scaffolds in parallel while main provisions). For writers this was wrong (writer returned in seconds; main then did the content iteration in its own context). recipe.md topic updates should distinguish "fire-and-forget subagents" (scaffolds, env-comments, code-review) from "wait-and-consume subagents" (writer, content-fix) — the latter block main's next work until their output is integrated.

---

### Mid-version — v8.81 → v8.85 (between v22 and v23)

Five structural versions shipped between the v22 session log (2026-04-16) and the upcoming v23 run. Each is driven by a specific v22 post-mortem item or a regression surfaced during implementation. No live run between v22 and v23 — the mid-version entries record what changed in the agent-facing content, check suite, and tool schemas.

**v8.81 — v22 post-mortem fixes (commit `69409b9`).** Addresses §4.1, §4.3–§4.6 from the v22 items above:

- §4.1 post-writer content-fix subagent dispatch gate (`recipe_content_fix_gate.go`) — surfaces a `content_fix_dispatch_required` check on retries of `complete step=deploy` when any of the 4 content-quality checks fail after the `readmes` substep; agent is steered to dispatch a scoped fix subagent before the full-step gate accepts. Closes the 15-minute Phase-4 iteration-into-main pattern.
- §4.3 NATS credentials preamble in the scaffold-subagent brief (`recipe.md` scaffold-subagent-brief block).
- §4.4 S3 endpoint preamble (same block).
- §4.5 dev-start vs `buildCommands` contract check (`{hostname}_dev_start_contract` in `workflow_checks_generate.go`) — fails generate if `run.start` references compiled build output (`dist/*.js`, `build/*.js`, `target/*.jar`) but dev `buildCommands` omits a build step.
- §4.6 root-README architecture-narrative check (`workflow_checks_architecture_narrative.go`, finalize-step, showcase ≥2-codebase only) — requires the root README to name each hostname, role, and cross-codebase contract verb. Informational grade — soft nudge at finalize, not a deploy-step gate.

**v8.82 — content rubric reform (4 of 5 fixes).** Response-body hygiene first:

- §4.2 `zerops_yml_comment_depth` hard gate at generate-complete (`workflow_checks_zerops_yml_depth.go`) — 35% reasoning-marker floor + absolute minimum 2, same taxonomy as env import.yaml depth check. Closes the IG-#1-inherits-shallow-comments regression: the zerops.yaml comments are copied verbatim into the integration guide fragment, so shallow comments there become shallow published content.
- §4.3 `content-quality-overview` eager topic — six-surface teaching map (zerops.yaml / IG / gotchas / env comments / root README / CLAUDE.md). Authors, timing, rubrics, boundaries, anti-patterns. Eager-injected so the agent has the unified mental model before authorship begins, not after check failures.
- §4.4 `container-ops-in-README` soft check (info-only) — flags SSHFS/fuser/ssh/chown tokens in README gotchas with a nudge toward CLAUDE.md.
- §4.5 IG `{hostname}_integration_guide_causal_anchor` parity — every IG item beyond IG #1 must carry a concrete failure-mode anchor (HTTP status, backtick-quoted error, or symptom verb). IG #1 (zerops.yaml copy) is grandfathered.
- §4.1 `gotcha_invariant_coverage` DROPPED per v22 design-pivot. Static-checklist-coverage punishes the first recipe in a new stack. Quality pressure from `gotcha_causal_anchor` + `gotcha_depth_floor` + predecessor-floor already covers the right layer.

**v8.83 — substep response-size fall-through fix (commit `26b0347`).** v22's audit surfaced 3 substep completion responses at 40+ KB against a 1.6 KB baseline for smaller substeps. Root cause: `subStepToTopic` had no case for `SubStepFeatureSweepDev` / `SubStepFeatureSweepStage`, and `buildGuide` had no terminal-substep branch for "all substeps complete, awaiting full-step check" — both fell through to `resolveRecipeGuidance` which emits the full ~40 KB deploy monolith. Registered 2 missing topics + added the 2 missing `subStepToTopic` cases + restructured `buildGuide`'s substep branch to distinguish in-progress from all-complete. Result: feature-sweep-dev 47,188 → 3,344 bytes (14× reduction), feature-sweep-stage 47,188 → 2,474 (19×), after-readmes all-complete ~47,000 → 698 (63×). Regression guards: 5 new tests including a meta-test enumerating every `SubStep*` constant and asserting a non-empty topic mapping.

**v8.84 — step-entry eager scope shift.** Deploy step-entry response was persisting to disk at 50.9 KB (Claude Code's threshold) — agent got a pointer, not the content. Four topics were flagged `Eager: true` and all landed at step entry regardless of where their teaching became actionable. First-principles re-scope: `GuidanceTopic.Eager bool` replaced with `EagerAt string`. Values: empty (not eager), `EagerStepEntry`, or any `SubStep*` constant. Migrations:

| Topic | Before | After |
|---|---|---|
| `where-commands-run` | step-entry (deploy) | step-entry — stays (SSH from the first sub-step) |
| `subagent-brief` | step-entry (deploy) | **removed** — substep focus already serves it |
| `readme-fragments` | step-entry (deploy) | **removed** — substep focus already serves it |
| `content-quality-overview` | step-entry (deploy) | **`EagerAt: SubStepReadmes`** |

New `InjectEagerTopicsForSubStep(topics, plan, subStep, excludeID)` injects sub-step-scoped eager topics inside `buildSubStepGuide`, with `excludeID` dedup so a topic that's both primary and sub-step-eager doesn't double-inline. Measured result (dual-runtime showcase): deploy step-entry 50.9 KB → 7.8 KB (6.5× reduction, no more persist-to-disk); `readmes` substep picks up the `content-quality-overview` inline at 24.9 KB (primary readme-fragments + eager content-quality-overview); all other substeps unchanged at 1.6–14 KB. 5 new tests including size-budget guard rails.

**v8.85 — env-var mental-model correction + self-shadow structural check + setup propagation.** Forensic audit of session-log-16 (the run that was aborted after diagnosing 8 self-shadowed cross-service vars in workerdev/zerops.yaml) proved three things:

1. **The 50.9 KB response-body persist-to-disk was NOT the cause of the bug.** Agent re-read the persisted file immediately; context was fully present. The v8.84 scope shift is still the right fix for response-body hygiene, but didn't address the content bug.
2. **The recipe agent was never taught that cross-service vars auto-inject project-wide.** Grep-verified across every pre-write response body (research step, provision step, generate step, chain recipe, all 4 fetched topics): zero matches for "cross-service vars are auto-injected into every container." The teaching the agent had was the OPPOSITE: `envVariables: contains ONLY cross-service references + mode flags` (from the `zerops-yaml-rules` topic, reinforced by the predecessor recipe's model zerops.yaml which puts `DB_HOST: ${db_hostname}` in envVariables). The predecessor worked by LUCK — the TypeORM-convention rename (`DB_HOST` vs `db_hostname`) happened to use different keys so the self-shadow didn't fire. A showcase with 5 services (db, cache, queue, search, storage) + a worker that reads `process.env.db_hostname` directly, the agent correctly noticed the predecessor's rename wasn't needed, dropped the rename — and walked into the self-shadow on every var.
3. **The setup-vs-hostname confusion at L145 (`zerops_deploy targetService=apidev` with no setup)** was attention-decay, not missing content — the rule is taught 3× in recipe.md. The agent self-corrected in one round via zcli's error. But pre-flight had actually resolved the setup correctly via role fallback; it just didn't echo the resolved name back, so zcli still got empty and failed.

v8.85 ships the coordinated fix:

- **Guide corrected** — `internal/knowledge/guides/environment-variables.md` (gitignored, embedded at build via `go:embed`, push upstream via `zcp sync push guides`). New "Cross-Service References — Auto-Injected Project-Wide" section + "Self-Shadow Trap" section + corrected "Isolation Modes" table (`envIsolation` does NOT gate auto-inject — it governs YAML-level template resolution).
- **Recipe.md `env-var-model` block** (generate section) — the agent-facing distillation of the guide's key rules: auto-inject semantics, two legitimate uses of `run.envVariables` (mode flags + framework-convention renames), self-shadow trap with concrete `db_hostname: ${db_hostname}` example, decision flow per var, pointer back to the guide.
- **Topic `env-var-model` registered** (`recipe_topic_registry.go`) with `EagerAt: SubStepZeropsYAML` — lands the teaching at the exact substep where the agent authors zerops.yaml.
- **Recipe.md corrections of the prior misleading lines**: `shared-across-setups` no longer claims `envVariables` contains "ONLY cross-service references + mode flags" — now correctly says "mode flags + framework-convention renames only." `worker-setup-block` no longer says "envVariables match prod" — now "envVariables = mode flags only (cross-service vars auto-inject for workers too)." `dual-runtime-what-not-to-do` shadow warning generalised to ALL auto-injected vars (not just project-level).
- **Structural `{hostname}_env_self_shadow` check** (`internal/ops/env_shadow.go` + `workflow_checks_generate.go`) — scans both legacy top-level `envVariables` and canonical `run.envVariables` for the `key: ${key}` shape (with whitespace tolerance, ignoring composed strings like `postgres://${db_hostname}:5432/app`). Fails generate with a detail that names every offender and points at the `env-var-model` topic. Replay of session-log-16's bad yaml triggers the check on the 5 shadows, does NOT flag the legitimate `DB_HOST: ${db_hostname}` rename or the `NODE_ENV: production` mode flag.
- **Pre-flight resolved-setup propagation** (`deploy_preflight.go` + both deploy handlers) — `deployPreFlight` now returns the resolved setup name alongside the check result. When the caller passes empty `setup` and pre-flight finds one via role/hostname fallback, the resolved name is echoed back and the handlers pass `--setup=<resolved>` to zcli explicitly. Unknown-setup errors now enumerate the available setups from the parsed zerops.yaml instead of zcli's generic "Cannot find corresponding setup" message.
- **`zerops_deploy.setup` description reworded** (both SSH + local) — accurately says setup names are user-defined identifiers; recipes conventionally use `dev`/`prod` (and `worker` for shared-codebase worker recipes); required whenever zerops.yaml has >1 setup; explicit mapping example for recipe workflows.

**Response-body sizes at v8.85 (dual-runtime showcase, Opus 4.7):**

| Response | v22 / v8.80 | v8.85 |
|---|---|---|
| Deploy step-entry (generate → deploy) | **50.9 KB** (persist-to-disk) | **7.8 KB** (6.5×) |
| feature-sweep-dev substep | 47.2 KB (fall-through) | 3.3 KB (14×) |
| feature-sweep-stage substep | 47.2 KB (fall-through) | 2.5 KB (19×) |
| all-substeps-complete | ~47 KB (fall-through) | 698 B (63×) |
| zerops-yaml substep (generate) | ~6 KB | 6.4 KB (+env-var-model eager body) |
| readmes substep | ~18 KB | 24.9 KB (+ content-quality-overview eager body) |
| subagent substep | ~14 KB | 14.3 KB (unchanged) |
| All other substeps | 1.6–10 KB | 1.6–10 KB (unchanged) |

No response body crosses Claude Code's persist-to-disk threshold. The readmes substep is the heaviest at 24.9 KB, well within the 30 KB absolute ceiling enforced by `TestBuildGuide_DeployStep_AcrossAllSubsteps_SizeCeiling`.

**v8.85 structural changes waiting on v23 live validation:**

- `env-var-model` topic at `SubStepZeropsYAML` — does the agent write zerops.yaml without self-shadows on the first try?
- `{hostname}_env_self_shadow` check — does it fire ONLY on actual shadows (no false positives on legitimate renames)?
- Pre-flight resolved-setup propagation — does the agent never hit the zcli "Cannot find setup" error on its first deploy call?
- v8.84 substep eager-scope shift — does deploy step-entry stay under 10 KB across a full run?
- v8.82 `zerops_yml_comment_depth` gate — does the agent ship IG #1 with reasoning-marker-heavy comments?

**Guide upstream-push status**: the `environment-variables.md` edit is local-only until `zcp sync push guides` creates a PR on zeropsio/docs and that PR merges. The in-binary copy (via `go:embed`) picks up the local edit at build time, so the v23 run will see the corrected guide via the released binary even before the upstream PR merges.

---

### v23 — v8.81–v8.85 ship; content recovers, content-fix-loop convergence breaks

- **Date**: 2026-04-17
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Model**: claude-opus-4-7[1m] (same as v22)
- **Session**: `a15e3224ab4f3916`
- **Session logs**: `main-session.jsonl` (2.86 MB, 384 assistant events) + 10 subagent logs
- **Wall**: 12:10:05 → 14:09:11 = **119 min** (+15% vs v22's 103 min, +68% vs v20's 71 min, −8% vs v21's 129 min)
- **Assistant events**: 384 (v22: 410, v21: 381, v20: 294)
- **Tool calls**: 233 (v22: 243, v21: 233, v20: 177)
- **Bash metrics (main)**: 61 calls / **0.6 min total (38.7 s)** / **0 very-long** / 1 long (11.7 s seed manual run) / **2 errored** / 0 port kills / 3 sleeps / 12 curls — **best operational hygiene since v18 (0.8 min)**
- **Bash metrics (main + subagents)**: ~150 calls / ~5 min total / 2 very-long (both `nest new scratch` inside scaffold subagents — 84 s apidev, 75 s workerdev) / ~10 errored
- **Subagents (10 — matches v20 record)**:
  - Scaffold ×3: apidev (`afc7e4a8…`, **655 s = 10m 55s**, 69 events, 25 Bash + 23 Write), workerdev (`a9611a14…`, 163 s, 25 events), appdev (`a995689d…`, 104 s, 28 events)
  - Feature ×1 (`a4bf1345…`, **822 s = 13m 42s**, 168 events, 55 Bash + 28 Write + 28 Read + 15 Edit, 0 MCP schema errors)
  - README/CLAUDE writer ×1 (`a180aa17…`, 311 s = 5m 11s, 33 events, 6 Writes — initial 6-file create)
  - **Content-fix subagent ×3** (NEW failure pattern): `afb4e132…` (350 s, 55 events, 13 Edits), `af7c2223…` (294 s, 43 events, 13 Edits), `a637f5c4…` (134 s, 33 events, 6 Edits) — all README content fixes after v8.81 dispatch gate refused `complete deploy`
  - Env-comments writer ×1 (`a42cb788…`, **1 event total** — pure JSON build, no bash)
  - Code review ×1 (`a8e7cbdb…`, 372 s = 6m 12s, 134 events, 43 Reads + 8 Greps)
- **MCP tool mix**: 31 `zerops_workflow`, 21 `zerops_dev_server` (new record; v22: 15), **18 `zerops_guidance`** (v22: 17 — peak), 17 `zerops_deploy` (v22: 11), **5 `zerops_browser`** (3 deploy + 2 close), 5 `zerops_subdomain`, 3 `zerops_mount`, 3 `zerops_logs`, 1 each `zerops_knowledge` / `zerops_import` / `zerops_env` / `zerops_discover` / `zerops_verify`
- **Workflow `complete deploy` calls**: **20** — 12 substep completes (rapid-fire 13:14:31→13:18:15) + **8 retries on full-step README content checks** (13:18→13:41)

**Content metrics** (apidev / appdev / workerdev):

- README lines: ~250 / ~260 / ~290 (bytes 7,840 / 8,134 / 8,952) — compressed ~30-40% vs v22's 341/208/221 lines
- Gotchas: **6 / 3 / 4** (v22: 7/6/6 — drop driven by checker convergence, not floor enforcement)
- IG items: 4 / 4 / 4 (v22: 8/6/6)
- CLAUDE.md: 5,487 / 4,446 / 5,886 bytes — all clear 1200-byte floor + ≥3 custom sections
- Root README intro: ✅ names real managed services (PostgreSQL, Valkey, NATS, S3-compatible object storage, Meilisearch — v17 `dbDriver` validation holds for **6th** consecutive run)
- Preprocessor directive: ✅ all 6 envs carry `#zeropsPreprocessor=on` with `<@generateRandomString(<32>)>` for `JWT_SECRET`
- **Architecture section**: ✅ present in root README (v8.81 `recipe_architecture_narrative` check fired 1×, agent fixed, retry passed) — names each codebase by hostname, role, and inter-codebase contract verb (app → api HTTP/JSON CORS, api → worker NATS publish/subscribe on `jobs.process`, worker → db SQL via TypeORM with entity-mirror invariant explicit)

**Step time decomposition**:

| Step | Duration | Notes |
|---|---:|---|
| Research | 1m 6s | clean |
| Provision | 3m 8s | 14 services in one batch, env vars in one batch |
| Generate | 15m 47s | 3 parallel scaffolds (apidev 11m / appdev 1m44s / workerdev 2m43s) + main writes 3 zerops.yaml |
| Deploy | **44m 19s + 23m fix loop = 1h 7m** | 12 substeps (12:30→13:14, ~44 min); full-step gate then took 23 min for README content fixes |
| Finalize | 4m 39s | 1 retry on `recipe_architecture_narrative` |
| Close | 22m 54s | code review (6m12s) + redeploy ×3 + cross-deploy ×3 + close-browser-walk |

**The 23-minute README content-fix loop within deploy is the single biggest controllable waste**. Without it, v23 wall-clocks at ~96 min — competitive with v18/v20/v22.

**Content-check pass/fail burn (parsed from session event stream)**:

| Check | Pass | Fail | Notes |
|---|---:|---:|---|
| `content_reality` (api/app/worker) | 2 | **5** | 5 iteration rounds — top cost driver |
| `gotcha_causal_anchor` (api/app/worker) | 5 | 4 | mostly resolved iteratively |
| `service_coverage` (api/worker) | 5 | 2 | drove apidev to add 6th Meili gotcha |
| `claude_readme_consistency` | 6 | 1 | v8.80 rewrite stayed alive (vs v21 dead) |
| `scaffold_hygiene` | 5 | 1 | held — no v21-class node_modules leak |
| `gotcha_distinct_from_guide` | 6 | 0 | passed clean first try |
| `knowledge_base_authenticity` | 6 | 1 | one round on apidev |
| `ig_per_item_standalone` | 5 | 1 | one round on appdev |
| `comment_specificity` | 4 | 2 | required 2 rounds |
| `comment_ratio` (zerops.yaml IG-embedded) | 6 | 2 | SA2 fixed wrong file (disk yaml not embedded copy) — wasted ~5 min |
| `comment_depth` | 3 | 0 | clean |
| `service_comment_uniqueness` (env 0-5) | 2 | 0 | held |
| `cross_readme_gotcha_uniqueness` | 6 | 0 | held |
| `recipe_architecture_narrative` | 1 | 1 | v8.81 NEW — fired correctly, fixed in finalize iteration |
| `zerops_yml_comment_depth` | 1 | 0 | v8.82 NEW — passed first try |
| `env_self_shadow` | not observed | not observed | v8.85 NEW — silent (no shadows in any zerops.yaml) |

**The README content-fix loop (the 23-minute cost driver)**:

After the writer subagent shipped 6 README+CLAUDE.md files at 13:14, main called `complete deploy` → result event #496 (13:18:15): **23 fails / 47 passes**. v8.81's `content_fix_dispatch_required` gate fired and dispatched 3 fix subagents in series:

| Round | Time | Subagent | Result event | Fails | Notes |
|---|---|---|---:|---:|---|
| Writer | 13:08-13:14 | `a180aa17` | #496 (13:18) | 23 | Initial create — no checks visible at write time |
| Fix #1 | 13:20-13:26 | `afb4e132` | #515 (13:26) | 11 | Fixed disk `zerops.yaml` (9% → 57% comment ratio) — but checker reads embedded README copy, so `comment_ratio` STILL fails 9%/7%/11% |
| Fix #2 | 13:27-13:32 | `af7c2223` | #521 (13:32) | 5 | Synced README-embedded yaml; `content_reality` truncation reveals new phantom paths (`dist/migrate.js`, `process.env`, `res.json`) |
| Fix #3 | 13:34-13:37 | `a637f5c4` | #551 (13:37) | 4 | "Be surgical" brief framing — **explicitly skipped** 3 known-bad lines because they weren't on the visible findings list |
| Inline | 13:37-13:41 | (main agent) | #622 → #651 | 0 | Main grep'd the whole tree, fixed everything |

**Strictly decreasing fail count (23→11→5→4→2→0); no whack-a-mole**. The 5 rounds where 1 should have sufficed are caused by 3 v8.81 brief-construction defects:

(a) `content_reality` truncates findings with `... and N more` — each subagent only sees a subset of failures.
(b) Brief framing was "be surgical, only the listed findings" — SA4's final message literally says: *"Unaddressed / intentionally skipped: apidev/README.md line 21 (IG step 1 prose still names dist/main.js, dist/migrate.js, dist/seed.js) — not flagged in the 4 failing findings."*
(c) `comment_ratio` reads the YAML embedded in README's IG step 1 fenced ```yaml block, NOT the on-disk `zerops.yaml`. SA2 wasted 5m 50s editing the wrong files.

**v22's failure was**: dispatch gate didn't exist, iteration leaked to main (11 Edits on workerdev/README.md). **v23's failure is**: dispatch gate exists but emits convergence-hostile briefs, iteration loops across subagents.

**Three platform-mental-model defects (the user's "missed understanding" perception)**:

1. **`zsc execOnce` "burn trap" folk-doctrine enshrined in apidev/CLAUDE.md** — at 12:31:36 the apidev first-deploy seed initCommand returned ✅ in 56ms with zero `[seed] upserted ...` lines and 0 rows in the table. Manual rerun at 12:36:52 produced 5 rows cleanly. Subsequent deploys (12:53 snapshot-dev) ran cleanly with new `appVersionId`. The agent invented terminology ("execOnce burn from initial workspace deploy") and codified it in apidev/CLAUDE.md under "Recovering `zsc execOnce` burn". The mental model is wrong: `appVersionId` is per-deploy, not per-workspace — there is no concept of a workspace-creation-time deploy that pre-burns the next user's first deploy's key. Real cause undiagnosed (most likely script-side silent exit, or container-labeling artifact — `apidev-runtime-2-2e` suggests this was the second container). **Future runs reading apidev/CLAUDE.md will inherit this folk-doctrine**.

2. **"Parallel cross-deploys are rejected" — `Not connected` misattribution propagated to TIMELINE.md** — at 12:32:17-19 main agent issued 3 parallel calls (subdomain enable + 2 deploys). The subdomain call returned `FINISHED`. The 2 deploy calls returned `Not connected` (`is_error: true`). TIMELINE.md says: *"Sequential cross-deploys (parallel called but tool returned 'Not connected' for simultaneous deploys, so serialized)"* — propagating misattribution to the published TIMELINE. Reality: `Not connected` is the standard MCP STDIO error when the channel is held by a long-running call. `zerops_deploy` blocks the channel for the build duration (1-2 min), so a second concurrent request is dropped at the transport layer. **Not a platform refusal**.

3. **`module: nodenext` workerdev fix without diagnosis** — workerdev `start:dev` (raw `ts-node -r tsconfig-paths/register src/main.ts`) failed at 12:35:26 with `Cannot find module './app.module.js'`. Apidev tsconfig is **identical** (also `module: nodenext`) but apidev `start:dev` uses `nest start --watch` which proxies through @nestjs/cli's bundler. Agent flipped workerdev to commonjs without questioning why apidev's identical tsconfig didn't fail. Sustainable fix, shallow understanding — TIMELINE narrative *"NestJS 11 scratch emitted module: nodenext... ts-node refused to resolve"* is half-right but conflates "raw ts-node + nodenext = bad" (true) with "NestJS scaffold emits broken tsconfig" (false).

**The "senseless deploy repetition" perception (refuted by data)**: 17 deploys, **15 produced builds**, 2 returned `Not connected` instantly without burning build time. **0 redundant deploys** by intervening-change analysis — every same-target redeploy has at least one new commit between it and the prior deploy. The 5 deploy clusters of 3-each are: initial dev (12:30-12:35), snapshot-dev (12:53-12:56 — bake feature commits), first cross-deploy (13:02-13:05 — dev→stage), code-review redeploy (13:53-13:56 — apply 3 CRITs + 2 WRONG fixes), second cross-deploy (13:58-14:01 — promote fixes to stage). Each cluster is necessary. **The user's perception is real (5 clusters of "we just deployed all 3" creates a strong rhythmic feeling of repetition) but the data shows zero waste**.

**Gotcha-origin distribution (the silver lining)** — running v22's incident-vs-invariant audit on v23's 13 gotchas:

| Codebase | Total | Pure invariant | Mixed | Incident-derived |
|---|---:|---:|---:|---:|
| apidev | 6 | 4 (NATS URL, S3 endpoint, trust proxy, TypeORM sync) | 1 (cache-manager TTL) | 1 (Meili idempotent seed) |
| appdev | 3 | 1 (Vite allowedHosts) | 0 | 2 (cross-codebase Meili pointer, Svelte $state) |
| workerdev | 4 | 0 | 1 (#4 schema-drift) | 3 (queue group, SIGTERM, entity divergence) |
| **TOTAL** | **13** | **5 (38%)** | **2 (15%)** | **6 (46%)** |

**vs v22's 19/19 (100%) incident-derived ratio, v23 is 5/13 pure-invariant + 2/13 mixed = 54% non-incident** — a structural improvement on the deepest content failure mode v22 surfaced. Compression came WITH quality, not against it.

**Env-4 comment quality**: A−. Service uniqueness pass, two-axis `minContainers` teaching at project level + per-service (app explicit HA/rolling-deploy axis, api throughput AND HA, worker queue-group + drain). All 8 service blocks carry WHY. No templated-shape phrasings.

**Close-step bugs**: **0 CRIT / 0 WRONG / 1 STYLE post-fix** (3 CRITs caught + fixed by code-review subagent pre-publish: StatusPanel `data.services[key]` vs apidev's flat `{db,redis,nats,storage,search}` map, workerdev Item entity divergence vs apidev migration, missing NATS queue group `'jobs-workers'`; 2 WRONG fixed: workerdev SIGTERM drain via `enableShutdownHooks() + process.on('SIGTERM', ...)`, apidev cache.controller.drop() doubled Redis RTT). Cleanest close since v22's 0/0 review.

**v8.81–v8.85 gate effectiveness** (the validation v23 was meant to provide):

| Version | Gate | Held? | Evidence |
|---|---|---|---|
| v8.81 | post-writer content-fix dispatch gate | ✅ structurally — ✗ converges in 1 round | Fired 3× and dispatched correct subagent type, but anti-convergent brief construction caused 5 rounds |
| v8.81 | NATS credentials scaffold preamble | ✅ | apidev `nats.client.ts` and workerdev `main.ts` both pass user/pass as separate ConnectionOptions; v21+v22 CRIT did NOT recur |
| v8.81 | S3 endpoint scaffold preamble | ✅ | apidev uses `process.env.storage_apiUrl` (https); v22 301 CRIT did NOT recur |
| v8.81 | `dev_start_contract` check | inconclusive | No pass/fail events in stream; workerdev contract `npm run start:prod` (prod) vs `npm run start:dev` (dev) is consistent |
| v8.81 | `recipe_architecture_narrative` (finalize) | ✅ | 1 fail / 1 pass — caught missing root README architecture section; agent wrote it; passed retry |
| v8.82 | `zerops_yml_comment_depth` (generate) | ✅ | 0 fails / 1 pass — clean first try |
| v8.82 | `content-quality-overview` eager topic | inconclusive | No session-level evidence of effect |
| v8.83 | substep response-size fall-through fix | ✅ | All substep responses healthy in tool_results |
| v8.84 | step-entry eager scope shift | ✅ | Deploy step-entry inline (not persisted-to-disk) |
| v8.85 | `env-var-model` eager topic | ✅ | No env_self_shadow events; both `${db_*}` and `${redis_*}` correctly via `process.env` |
| v8.85 | `env_self_shadow` check | held silent | 0 shadows in any zerops.yaml |
| v8.85 | pre-flight resolved-setup propagation | ✅ | All 17 deploys passed `setup=dev`/`setup=prod` correctly; no "Cannot find setup" |

**Notable**:

- **First run since v8.81 shipped that exercised the post-writer content-fix dispatch gate at scale** — 3 fix subagents fired, but the brief-construction defect surfaces a new failure class (anti-convergent briefs).
- **Best operational hygiene since v18** — main bash 0.6 min (v18: 0.8), 0 very-long, 2 errored. The 21 dev_server calls (record) are all justified deploy/feature/code-review lifecycle events, no spurious stop+start.
- **Cleanest close-step bug count** since v22 — 0 CRIT / 0 WRONG / 1 STYLE post-fix. Code-review subagent caught 3 CRIT + 2 WRONG pre-publish; all fixed.
- **`scaffold_hygiene` held** — published tree clean, no v21-class node_modules/dist leaks.
- **Delegation pattern restored** — 10 subagents matches v20 record (vs v21's 5). The 3 fix subagents are NEW — they show v8.81's post-writer gate is doing its job at the dispatch layer.
- **Gotcha-origin diversity** — first run with explicit non-incident-derived gotchas (5/13 pure invariant). v22's deepest content failure mode (100% incident-derived) is structurally improved.
- **Three platform-doctrine defects ship in published artifacts** — apidev/CLAUDE.md "Recovering execOnce burn" section + TIMELINE.md "parallel cross-deploys rejected" + workerdev tsconfig flip without diagnosis. Future runs and porters reading these will inherit the misinformation.

**Rating**: S=**A** (all 6 steps, all features, all browser walks, code-review CRITs caught + fixed pre-publish), C=**A−** (38% pure-invariant + 15% mixed gotcha origin — vs v22's 0%; full architecture section; all CLAUDE.md pass; env-4 comments solid; minor — 4 of 13 gotchas have IG-overlap or split-lesson defects; one folk-doctrine "execOnce burn" enshrined in published CLAUDE.md), O=**C** (119 min wall vs v22's 103, v20's 71; main bash A but 23-min README fix loop is the cost limiter; 17 deploys + 21 dev_server calls + 8 retry `complete deploy` calls — all justified by intervening commits but rhythmically heavy), W=**B** (10 subagents incl. 3 fix dispatches + 1 substep gate retry; both browsers fired; v8.81 dispatch gate fired but failed to converge in one round; 18 guidance topics — peak) → **C**

*v23 is a mixed run. The deliverable is structurally better than v22 (gotcha-origin ratio swings from 0% to 38% pure-invariant, compression came with quality, close review is spotless). But the v8.81 post-writer content-fix dispatch gate — the flagship structural fix that was supposed to close v22's iteration-into-main pattern — did dispatch correctly but emits convergence-hostile briefs that produce 3 fix-subagent rounds where 1 should suffice. The 23-min README fix loop is the entire 119→96 min wall delta. Plus three platform-mental-model defects ship in the published deliverable, the most concerning of which is "Recovering `zsc execOnce` burn" in apidev/CLAUDE.md — fictional Zerops folk-doctrine that future runs and porters will inherit. v23 post-mortem at [docs/implementation-v23-postmortem.md](implementation-v23-postmortem.md) (analysis). The postmortem's original §7 list of 6 narrow fixes (truncation, tokenClass, embedded-yaml docs, etc.) addressed symptoms not root cause, and was superseded after a dialogue surfaced that the load-bearing problem is "external gate + dispatch fix subagent" being anti-convergent by construction (math: 17 active checks × ~95% per-check pass-rate = 42% probability of clean-on-first-write, so 58% of runs are forced into a fix loop). The v8.86 plan at [docs/implementation-v8.86-plan.md](implementation-v8.86-plan.md) inverts the verification direction: writers learn check rules upfront and self-verify before returning. Six fixes: (§3.1) `zerops_record_fact` MCP tool + structured facts log accumulated during deploy, (§3.2) writer subagent brief includes runnable validation commands per check + iterate-until-clean instruction, (§3.3) demote v8.81 dispatch gate to confirmation-only, (§3.4) restore v20's close-step critical-fix subagent, (§3.5) generate-step contract spec referenced by all 3 scaffold subagents, (§3.6) folk-doctrine prevention (execOnce semantics + MCP channel docs). README writing stays at deploy.readmes substep (preserves deep content from deploy-discovered facts). Stage 2 (v8.87+) — only if v24 evidence shows v8.86 doesn't fully close convergence gap — decomposes deploy.readmes into per-substep distributed fragment authoring (each substep authors its own README fragment at moment of freshest knowledge; deploy.readmes becomes pure stitching). Stage 2 is reserved as a higher-risk follow-up because it's a workflow-mental-model change of the same magnitude as v8.78. Expected v24 outcome: A− in ~80-95 min wall (matches v20 peak with v8.78–v8.85 quality bar preserved).*

---

### Interlude — v24 ran, then we rolled back the checker machinery to v20 substrate

- **Date**: 2026-04-17
- **What ran**: v24 against `5fa83e8` (v8.89), outcome was **C / 126 min** — fifth consecutive regression since v20. No per-version entry above: the run was analyzed inline in the rollback plan rather than logged in this doc's usual shape, because the analysis drove a branch decision rather than a next-increment plan.
- **What we did**: opened `rollback-to-v20-substrate`, deleted ~28 check/dispatch-gate files, reverted 19 files to `5022f8d` (last commit before v8.78), kept the genuine tool-layer wins from the intervening commits. Net −7412 lines. Full plan: [docs/implementation-v20-rollback-plan.md](implementation-v20-rollback-plan.md).

**The decision.** The checker-machinery path has produced strictly-monotone regression for five consecutive runs while ~6000 lines of content checks and dispatch gates were added. v20's ten checks → v24's seventeen; v20's 71 min → v24's 126 min; v20's A− → v24's C. The math was legible before v24 (v23 §postmortem: 17 checks × 95% per-check pass rate = 42% clean-on-first-write), but v8.86's direction-inversion answer (self-verifying writer briefs) still produced a 126-min C. At that point the evidence supports a different framing: **the load-bearing problem is not how the checks fire, it's that they fire at all**. v24 surfaced failure modes no amount of brief-rewording fixes — `content_reality` regex flagging `res.json` as a missing file and pushing the agent to write "the response body parser" to satisfy it; `comment_ratio` reading the README-embedded yaml copy so the agent wrote a Python sync script to pass the check; `scaffold_hygiene` reading dev-container mount state so the agent ran `sudo rm -rf node_modules` on live dev servers. When the agent builds tooling to satisfy a check, or rewrites correct content to dodge a regex, the check is the bug.

**What rolled back:**
- v8.78 5-check reform — `causal_anchor`, `content_reality`, `per_item_standalone`, `service_coverage`, `claude_readme_consistency`
- v8.80 — `scaffold_hygiene`, `gotcha_depth_floor`, writer-subagent dispatch gate
- v8.81 — `content_fix_dispatch_required` gate, `architecture_narrative` check, `dev_start_contract` check, scaffold-preamble bloat in `recipe.md`
- v8.82 — `zerops_yml_comment_depth`, `readme_container_ops`, `content-quality-overview` eager topic
- v8.84 — `EagerAt` topic-scope refactor (recipe.md preamble bloat source)
- v8.86 — writer-self-verifying briefs, `contract_spec`, `claude_md_folk` check

**What survived the rollback (genuine tool-layer wins):**
- v8.80 `bash_guard` middleware — rejects `cd /var/www/<host>` SSHFS patterns main-agent-side
- v8.80 `pkill` self-kill classifier — ssh exit-255 on `pkill -f nest` now classifies as clean
- v8.83 substep response-size fix — 14×–63× reduction on `feature-sweep-dev`, `feature-sweep-stage`, `readmes-complete` responses
- v8.85 `env_self_shadow` check — one real bug class, zero false positives across v22/v23/v24
- v8.85 pre-flight resolved-setup propagation — zcli always invokes with `--setup=<resolved>`
- v8.86 `record_fact` MCP tool — orphan but harmless; agents may or may not call it
- All unrelated fixes — cicd, git-push, server refactor, develop-flow WorkSession refactor, opus-4-7 model gate

**Calibration for v25.** The rolled-back substrate IS v20's substrate plus the wins above. If v25 reproduces v20's A-grade at ~70-90 min wall — the machinery was the problem, and the forward direction is to accept v20's content drift classes (all 7 of them were content-editorial issues) as editorial-pass concerns, not check-gate concerns. If v25 lands close to v20's wall time but close-step finds more issues than usual — tool-layer regression independent of checks. If v25 lands far from v20 — substrate has degraded elsewhere (knowledge files updated upstream, platform-layer Zerops change) and the rollback has at least isolated where.

**What v25 is not for.** v25 does not validate "whether the rollback was worth it" on a single run. Two consecutive clean runs are needed before treating this substrate as the new baseline. And importantly: **if v25 exposes a content drift, the response is not a new check**. Every postmortem since v20 has added a check and every subsequent run has regressed. That pattern is what this rollback is breaking.

*Merged to main: fast-forward through `dd0642a` (2026-04-17). Rollback commit: `18e3854`; log-interlude commit: `dd0642a`. v25 will be the first per-version entry below this interlude.*

---

### v25 — first post-rollback run; calibration passed, substep-delivery bypass surfaces

- **Date**: 2026-04-17
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Model**: claude-opus-4-7[1m] (same as v22/v23)
- **Session**: `bef697743a0b77e9`
- **Session logs**: `main-session.jsonl` (1.61 MB, 225 assistant events) + 7 subagent logs
- **Wall**: 20:15:40 → 21:26:49 = **71 min 9 s** — matches v20 (71 min) exactly
- **Assistant events**: **225** — lowest for a complete run since v18's 223 (v20: 294, v22: 410, v23: 384)
- **Tool calls**: **177** — matches v20 exactly (v21: 233, v22: 243, v23: 233)
- **Bash metrics (main)**: 30 calls / **0.5 min total (30.9 s)** / **0 very-long** / 0 long / **0 errored** / 0 port kills / 0 sleeps / 5 curls. **Best operational hygiene across all recorded runs** (prior best v18 at 0.8 min).
- **Bash metrics (main + 7 subagents)**: ~123 calls / ~4.0 min total / **2 very-long** (both `nest new scratch` inside scaffold subagents — 75 s apidev, 35 s workerdev-long-install) / 1 errored (transient scaffold check)
- **Subagents (7)** — clean role separation, matches v22's delegation shape minus env-comments writer (main wrote env comments inline during finalize):
  - Scaffold appdev (`ae4f4d0c…`, 120 s, 60 events, 12 Write + 9 Bash)
  - Scaffold apidev (`a72e29e8…`, 269 s, 110 events, 28 Bash + 18 Write)
  - Scaffold workerdev (`a5986f0b…`, 127 s, 54 events, 9 Write + 8 Bash)
  - Feature subagent (`a547c148…`, **694 s / 11m 34s**, 283 events, 44 Bash + 33 Write + 26 Read + 10 dev_server — single author, all 5 features across all 3 codebases)
  - README/CLAUDE writer (`a88c3ba0…`, 383 s / 6m 23s, 49 events, 12 Read + 6 Write)
  - Content-fix subagent (`a99e56d1…`, 351 s / 5m 51s, 52 events, 9 Edit + 6 Read + 4 Grep — **converged in ONE round** vs v23's 5 rounds/23 min)
  - Code review (`a7cbdce9…`, 131 s, 121 events, 44 Read + 5 Grep + 4 Edit — inline fix of 1 WRONG)
- **MCP tool mix (main)**: 25 `zerops_workflow`, 11 `zerops_deploy`, 7 `zerops_dev_server`, 7 `Agent`, 6 `zerops_browser` (4 deploy + 2 close — v8.77 close-browser gate held), 6 `zerops_mount`, 5 `zerops_subdomain`, **5 `zerops_guidance`** (vs v20: 12, v22: 17, v23: 18 — low; the v8.84 `EagerAt` mechanism was rolled back, so step-entry carries more content by default and fewer on-demand fetches fire), 2 `zerops_discover`, 1 each `zerops_logs`/`zerops_knowledge`/`zerops_import`/`zerops_env`
- **Deploy call breakdown (11 total)**: 3 initial dev (apidev/workerdev/appdev at 20:28–20:30) + 3 snapshot-dev (re-deploy after feature-subagent commits at 20:46–20:48) + 3 first cross-stage (apistage/workerstage/appstage at 20:51–20:54) + 1 appstage `./dist/~` fix (20:56) + 1 close-step redeploy (appstage after code-review WRONG fix at 21:21). All justified by intervening commits.
- **Workflow `complete deploy` calls**: 14 — 12 substep completes (rapid-fire at step end) + 1 substep out-of-order retry + 1 full-step retry after content-fix subagent

**Content metrics** (apidev / appdev / workerdev):

- README lines: **435 / 182 / 247** — apidev **new record** (v20: 349, v22: 341, v23: 250); appdev/workerdev between v22 and v23 range
- README bytes: 20,276 / 9,985 / 12,949
- Gotchas: **6 / 6 / 6** — balanced distribution matching v22/v20 peak minus 1 on apidev; all authentic (platform mechanism + concrete failure mode)
- IG items: **6 / 4 / 3** — apidev matches v20; appdev/worker slightly below v20's 5/5 but above v16/v18 ranges
- CLAUDE.md: 4945 / 5576 / 5835 bytes (v20: 3395/2786/3728; v22: 7307/6245/7724) — **largest appdev+workerdev CLAUDE.md ratio** (all three clear 1200 floor; 5–9 custom sections per file)
- **Root README intro**: ✅ names real managed services — *"connected to PostgreSQL, Valkey (Redis-compatible), NATS, S3-compatible object storage, and Meilisearch"* (v17 `dbDriver` validation holds for **7th** consecutive run)
- **Preprocessor directive**: ✅ all 6 envs carry `#zeropsPreprocessor=on` with `<@generateRandomString(<32>)>` for `APP_SECRET`
- **Architecture section in root README**: ❌ absent (v8.81 `recipe_architecture_narrative` check was rolled back; agent didn't author one — v22 C+ cross-codebase-coherence limiter returns, intentionally ungated per rollback calibration)

**Env 4 (Small Production) comment quality — mostly gold, one decorative-drift bug**:

- `api`: **v7 gold standard** — *"minContainers: 2 because a single-container pool drops traffic on every rolling deploy and on container crashes; this tier's request volume also typically exceeds single-container capacity for one pod, so the replicas serve both axes (throughput + zero-downtime rollover)."* Two-axis teaching intact.
- `worker`: **v7 gold standard** — *"minContainers: 2 because the queue-group subscription (`queue: 'workers'`) distributes jobs exactly once across replicas AND because a single-replica worker pool loses in-flight jobs when Zerops SIGTERMs the container during a rolling deploy."*
- `db` / `redis` / `queue` / `storage` / `search`: per-service WHY explanations, no template-phrasing duplication.
- **`app` (static) — contradicts own YAML**: comment claims *"horizontal scaling on static services applies at the edge cache layer, not at the container replica count, so minContainers stays at the platform default"* while the YAML directly below it declares `minContainers: 2`. The comment reasoning for NOT having multiple containers sits next to a declaration that DOES have multiple containers. Classic v20-era decorative-drift class (one of the 7 the v8.79 reform was built to catch). Per the rollback calibration's explicit rule: **editorial fix on the shipped recipe, not a new checker**.

**Deploy-step content check failures (at full-step retry #1, 21:09:06)** — all 6 surviving-rollback v8.67-era checks:

| Check | File | Fix applied by content-fix subagent |
|---|---|---|
| `app_gotcha_distinct_from_guide` | appdev README | 3 gotchas restated IG items (Vite allowedHosts, VITE_API_URL build time, server.host 0.0.0.0) → replaced with preview.allowedHosts, stale-API-origin corollary, Svelte 5 `$effect` HMR cleanup |
| `api_comment_specificity` | apidev zerops.yaml | 14% → rewrote with Zerops-specific vocab, final 45% |
| `api_gotcha_distinct_from_guide` | apidev README | 4 restated IG → replaced with MinIO NoSuchBucket, bucket-init race, cache-manager v6 ttl-in-ms, DB_PORT-as-string |
| `worker_comment_ratio` | workerdev zerops.yaml | 28% → added `ports: []` rationale, pgrep-vs-httpGet, SIGTERM/30s-SIGKILL window, final 46% |
| `worker_gotcha_distinct_from_guide` | workerdev README | 1 restated IG → rewrote symptom-first "Jobs silently lost mid-processing on rolling deploy — processedAt stays null forever" |
| `cross_readme_gotcha_uniqueness` | worker + app | Both had identical "OS-level env-var injection" cross-ref → deleted from worker, replaced with "Shared jobs entity ownership split" |

All 6 checks passed on retry #2 (21:16:38). **Content-fix-subagent converged in ONE round** — the v23 pathology (5 rounds, 23 min, anti-convergent briefs) did NOT recur, because the v8.81 dispatch gate that emitted those briefs was rolled back. This round is directly caused by the substep-bypass defect documented below — the fix subagent corrected rules the writer violated because the writer never received the `readme-fragments` substep brief.

**Finalize**: passed clean first-try. 140+ checks, comment depth 86/79/64/91/91/91% across envs 0-5. All min_containers/shared_secret/preprocessor/cross_env_refs passed.

**Close-step**: **0 CRITICAL / 1 WRONG / 3 STYLE** (code-review subagent inline-fixed; cleanest close since v22).

- **WRONG (fixed)**: `appdev/src/lib/ItemsCrud.svelte` DELETE handler used raw `fetch()` bypassing `api.ts` helper + missed `res.ok` guard. Added `apiVoid()` helper in `api.ts`, rewired DELETE through it. 1 cross-deploy to appstage (1m 6s) applied the fix.
- **STYLE (not applied, acceptable)**: worker error boundary fire-and-forget, unclosed setTimeout in JobsDispatch poll guard, non-rethrown bucket-init error.
- **0 CRIT → silent-swallow scan clean, feature coverage clean (5/5, no orphans)**.

**State-coherence defects (the next thing worth fixing) — NOT captured by S/C/O/W rubric**:

**Defect 1 — substep-delivery mechanism bypassed by main agent.** The deploy step has 12 sub-steps (showcase); each `complete step=deploy substep=X` call returns the next substep's scoped guide in its `current.detailedGuide` field. The main agent did ALL 40 min of deploy work (20:28 → 21:06:55) without calling `complete substep=X` once, then backfilled all 13 substep completions in 2 min at step end. Evidence:

```
20:28:06  complete step=generate              — deploy step-entry guide delivered (37 KB, includes subagent-brief + readme-fragments eagerly)
20:34:26  Agent: feature subagent dispatched  — feature work begins
21:00:06  Agent: README writer dispatched     — writer begins
21:06:55  complete step=deploy substep=subagent   — FIRST substep-complete call, 38 min late
          → INVALID_PARAMETER: expected "deploy-dev", got "subagent"   (out-of-order; gate rejected)
21:06:57  action=status                       — recovery
21:07:09 → 21:08:59  12 substep completes in canonical order (backfill, 2 min burst)
21:07:30  complete substep=init-commands      ← 14 KB subagent-brief returned HERE, 33 min AFTER feature subagent finished
21:08:45  complete substep=feature-sweep-stage ← 17 KB readme-fragments brief returned HERE, AFTER writer shipped
21:09:06  complete step=deploy                 → 6 content-check failures on writer's output
```

**~73 KB of phase-scoped guidance delivered into dead phases.** The two load-bearing briefs never informed the phases they exist to govern:
- The **14 KB `subagent-brief`** (delivered at `complete init-commands`) would have given the feature subagent its operational rules (installed-package verification, credential formats, contract-first discipline, cross-codebase contract lock-step). The feature subagent improvised by trial-and-error.
- The **17 KB `readme-fragments` brief** (delivered at `complete feature-sweep-stage`) would have given the README writer the rules it violated: gotcha-distinct-from-guide, comment ratio ≥30%, comment specificity ≥25%, cross-README dedup. The writer shipped content blind to all four; the 6-min content-fix cycle is direct cost.

**Defect 2 — subagents called `zerops_workflow` at spawn, server returned misleading error.** Two subagents (appdev scaffold at 20:20:46, feature subagent at 20:34:30) called `zerops_workflow` as their first tool use:

```
# Feature subagent, 20:34:30 — first tool call
TOOL_USE mcp__zerops__zerops_workflow input={} (empty)
TOOL_RESULT is_error=true: INVALID_PARAMETER "No workflow specified"

# Retry 7s later
TOOL_USE input={"action":"start","workflow":"develop","intent":"Implement 5 features..."}
TOOL_RESULT is_error=true:
  {"code":"PREREQUISITE_MISSING","error":"No bootstrapped services found",
   "suggestion":"Run bootstrap first: action=\"start\" workflow=\"bootstrap\""}
```

The server's `Run bootstrap first` suggestion is **misleading and latently dangerous**: the actual state is "a recipe workflow is already active, subagent should not start another workflow." A subagent following the suggestion literally could call `action=start workflow=bootstrap` and corrupt the recipe session state. v25's subagents rationalised correctly (*"main agent orchestrates workflow state"*) and proceeded — but the blast-radius potential is real.

**Root cause (both defects)**: `subagent-brief`, `where-commands-run`, and `readme-fragments` topics are all marked `Eager: true` in `internal/workflow/recipe_topic_registry.go`. Eager topics are inlined into the step-entry guide at `complete generate`. The agent therefore has both delegation briefs in hand BEFORE completing any deploy substep — there is no behavioural pressure to attest substeps in order, because the substep completion adds nothing the step-entry didn't already provide. The substep system becomes retroactive paperwork.

**Notable**:

- **Rollback calibration: PASSED.** v25 reproduces v20's A operational + A− content at v20's wall time (71 min matches exactly). The v22–v24 cost classes (410+ events, 23-min README fix loops, 11 main-context README-edit iterations) do NOT recur. Surviving tool-layer wins (bash_guard middleware, pkill self-kill classifier, env_self_shadow check, pre-flight setup propagation, substep response-size fix) all held.
- **Best operational hygiene across all recorded runs.** Main bash 0.5 min (prior best v18 at 0.8 min), 0 errored main (ties v18), 0 very-long main, 0 MCP schema errors, 0 exit-255 classifications.
- **Content peak on apidev README (435 lines)** — exceeds v20's 349, v22's 341. Gotchas balanced 6/6/6 with authentic platform-mechanism + concrete-failure-mode pairings. Cross-codebase dedup holds (2 cross-refs from appdev + workerdev to apidev, no duplication).
- **Gotcha-origin diversity is meaningfully non-incident** — unlike v22's 19/19 incident-derived, v25's gotchas include pure framework × platform invariants (bucket-init race on multi-container cold start, cache-manager v6 ttl-in-ms, DB_PORT-as-string pg rejection) that a fresh-scaffold porter would hit even on a clean first run.
- **7 subagents** — one less than v20's 10 because env-comments writer was folded into the main agent's `generate-finalize` call (which accepted structured envComments and projectEnvVariables in a single parameter). This is a legitimate delegation-shape choice, not a regression.
- **Two editorial defects ship in the published artifact (not a gate concern per calibration)**: (1) env 4 `app` comment contradicts its own `minContainers: 2` YAML; (2) no architecture section in root README to close the cross-codebase-coherence narrative gap (v22 C+ limiter).
- **v25 dropped from A− to B** on the revised read because workflow-discipline scored B, not A: the substep-delivery mechanism is structurally bypassed. Under the S/C/O/W-min rule, overall = B. The headline numbers (wall, bash, content, close) still say A−/A; the dimension that drops the grade is the one the rubric was designed to catch.

**Rating**: S=**A** (all 6 steps, all 5 features wired, dev + stage URLs both loading, deploy.browser + close.browser both fired, 0 close CRIT), C=**A−** (peak apidev README, balanced gotchas all authentic, env-4 comments mostly gold with two-axis teaching — but env-4 app-static comment contradicts YAML + root README has no architecture section), O=**A** (71 min wall = v20; **0.5 min main bash — new all-time best**; 0 very-long; 0 errored main; 0 MCP schema errors; 0 exit-255 failures; 11 deploys all justified), W=**B** (substep-delivery mechanism bypassed, ~73 KB of phase-scoped guidance delivered into dead phases, 2 subagents hit `zerops_workflow` misuse rejections with misleading server error, 6-min content-fix cycle structurally attributable to substep bypass) → **B** overall (min rule).

*Rollback calibration passed: v25's headline numbers match v20's. But the workflow-discipline defect the v25 run surfaces is not an S/C/O/W-rubric concern — it's a deeper state-coherence issue where the substep system's guidance-delivery mechanism is structurally bypassed because its content is eagerly inlined at step-entry. The defect has bounded blast-radius in this run (one 6-min content-fix cycle, two subagent rationalisation loops) but latent corruption risk if subagents follow the misleading `Run bootstrap first` suggestion literally, and fragility if context compaction hits mid-deploy with the step-entry guide as the only substrate. v25 post-mortem + v8.90 implementation plan at [docs/implementation-v8.90-state-coherence.md](implementation-v8.90-state-coherence.md). Four fixes, all upstream (no new checkers, no new gates): (A) `SUBAGENT_MISUSE` error at `handleStart` when session already active, replacing the misleading bootstrap suggestion; (B) de-eager `subagent-brief` and `readme-fragments` (keep `where-commands-run` eager — applies from substep 1), remap `SubStepInitCommands → subagent-brief` and `SubStepFeatureSweepStage → readme-fragments` so the briefs only land via substep-complete calls; (C) explicit tool-use policy block in every subagent brief listing permitted + forbidden tools, belt-and-braces with Fix A; (D) substep-complete discipline note at top of deploy step-entry explicitly naming the v25 anti-pattern. v26 calibration bar: 0 `SUBAGENT_MISUSE` rejections, 0 out-of-order substep attestations, substep-complete response sizes for `init-commands` ≥14 KB and `feature-sweep-stage` ≥17 KB (confirming substep-scoped delivery is active), step-entry ≤30 KB (confirming de-eager held), ≤2 full-step README check failures (direct measure of whether the writer received its brief in time).*

---

### v26 — aborted early; two defects surfaced, both fixed before v27

- **Date**: 2026-04-18
- **Wall**: ~23 min active work (05:54 → 07:17 UTC), user-aborted
- **Session logs**: `nestjs-showcase-v26/SESSIONS_LOGS/main-session.jsonl` (172 events) + 3 scaffold subagent logs
- **Reached**: research → provision → scaffold subagents (all correctly SSH'd) → main agent zerops.yaml writes. Did NOT reach deploy step; **v8.90's substep-delivery calibration stays inconclusive**.

**Defect 1 — `recipePlan` stringification (opener).** Two wasted round-trips at `complete step=research`: agent passed the plan as a JSON **string** twice (`"recipePlan": "{\"slug\":...}"`) before passing as an object on the third try. MCP schema validator correctly rejected (`type: "string", want one of "null, object"`); harmless but costs ~6-12s. Root cause: the `jsonschema` tag on `WorkflowInput.RecipePlan` was terse ("Structured recipe plan for research step completion") with no explicit "object, not string" hint. Fixed as **v8.93.2** ([f54533e](https://github.com/zeropsio/zcp/commit/f54533e)) — concrete object-shape example + "stringifying costs a retry round-trip" warning in the jsonschema tag.

**Defect 2 — `git init` zcp-side chown dance (terminator).** At 07:16:33 main agent ran `cd /var/www/{host} && git init && git add -A && git commit && sudo chown -R zerops:zerops /var/www/{host}/.git` zcp-side for all three codebases in parallel. The zcp-side `sudo chown` permission-denies against the SSHFS mount. Agent self-corrected in ~5s by ssh'ing the chown; user aborted to ship the fix. Same failure class as v17 and v21. Teaching existed before v26 (`git-config-mount` block at provision said "chown via SSH"; `where-commands-run` eager block at deploy step-entry listed `git init/add/commit` as SSH-only) but was split across step-entries — the SSH-only rule wasn't in hottest context when the scaffold-commit fired at end of generate (BEFORE deploy step-entry delivers the eager rule). Fixed as **v8.93.1** ([2f9cd47](https://github.com/zeropsio/zcp/commit/2f9cd47)) — `git-config-mount` block rewritten as a single container-side SSH call doing config + init + initial commit in one go. `.git/` is owned by `zerops` from the first byte, the chown dance is unreachable, and the brief demonstrates only one shape.

**Not observed (v25-class carryovers that held)**: subagents did NOT call `zerops_workflow` at spawn (v8.90 Fix A + C held in the scaffold subagents that ran); `where-commands-run` eager delivery intact at provision (main agent DID ssh correctly for the initial git config at 05:57 — it's only the POST-scaffold commit at 07:16 that slipped). Substep-delivery bypass could not recur because the run never reached deploy.

**v27 calibration bar (carried forward from v26's inconclusive run + the two v8.93 fixes):**
- `recipePlan` passes on the first `complete step=research` call (no type-string retry)
- 0 `cd /var/www/{host} && <exec>` zcp-side patterns observed anywhere — git or otherwise. Note: `bash_guard` middleware is still inert ([internal/tools/bash_guard.go:44-49](../internal/tools/bash_guard.go#L44-L49) — Claude Code's Bash tool is not interceptable from MCP), so compliance is purely brief-driven
- v25-era v8.90 calibration items still open: 0 `SUBAGENT_MISUSE` rejections, substep-complete responses land briefs in phase (`complete init-commands` ≥14 KB with subagent-brief phrases; `complete feature-sweep-stage` ≥17 KB with readme-fragments phrases), step-entry ≤30 KB, ≤2 full-step README content-check failures. v27 is the first full run since v8.90 shipped — the substep-delivery mechanism has not been tested end-to-end.

---

### v28 — mechanics hold (2nd post-rollback A-grade), honest content audit drops to C+

- **Date**: 2026-04-18
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Model**: `claude-opus-4-7[1m]` (same as v22–v27)
- **Session**: `c0b06dd3b24748be`
- **Session logs**: `main-session.jsonl` (2.4 MB, 739 events) + 5 subagent logs
- **Wall (recipe work)**: 12:25:27 → 13:50:45 = **85 min**. Total session including 22-min idle + export/TIMELINE write: 12:25:27 → 14:14:40 = 109 min.
- **Assistant events (main)**: 378 | **Tool calls (main)**: ~501
- **Bash metrics (main)**: 62 calls / **1.6 min total (96 s)** / **0 very-long** / **5 errored** / 2 multi-host pkill chains (both clean — `pkill` self-kill classifier held)
- **Bash metrics (main + 5 subagents)**: ~123 calls / ~4.0 min total / **0 very-long across the full tree** / 2 long (55–75 s `nest new scratch` in scaffolds — expected)
- **Subagents (5 — minimalist shape)**:
  - Scaffold apidev (`a4563611…`, 3:07 wall, 17 bash / 72.5 s)
  - Scaffold appdev (`a635c7822…`, 3:07 wall, 44 bash / 97.7 s)
  - Scaffold workerdev (`a6f5d561…`, 1:49 wall, 8 bash / 62.1 s)
  - Feature subagent (`a858c077…`, 7:36 wall, 22 bash / 30.5 s, **0 MCP schema errors**)
  - Code-review subagent (`a02689c4…`, 2:54 wall, 8 bash / 37.1 s — inline-fixed findings directly)
  - **No writer / no content-fix / no env-comments / no critical-fix dispatches**. Main agent wrote all READMEs + CLAUDE.md + env comments + applied close-fixes inline.
- **MCP tool mix (main)**: 26 `zerops_workflow`, 25 `zerops_dev_server`, **22 `zerops_guidance`** (v25: 5 — on-demand fetching restored), 18 `zerops_deploy`, 19 TodoWrite, 6 `zerops_browser` (4 deploy + 2 close), 5 Agent, 4 each of `zerops_logs`/`zerops_subdomain`/`zerops_verify`, 3 `zerops_mount`, **3 `zerops_record_fact`** (NEW — voluntary use; each became a published gotcha), 1 each `zerops_knowledge`/`zerops_import`/`zerops_discover`/`zerops_env`
- **Deploys**: 18 total — 3 initial dev (12:47–12:49) + 3 snapshot-dev after features (13:11–13:12) + 3 cross-stage first pass (13:21–13:22) + 1 workerstage rebuild after tsbuildinfo fix (13:25) + 3 dev redeploys after close fixes (13:40–13:44) + 3 cross-stage promotion (13:45–13:49). All justified by intervening commits.

**Content metrics** (apidev / appdev / workerdev):

- README lines: 261 / 189 / 230 — between v23's 250/260/290 and v25's 435/182/247
- README bytes: 12,245 / 8,749 / 11,067
- Gotchas: **5 / 5 / 5** — balanced but below v25's 6/6/6 floor and v20/v22 peak of 7/6/6
- IG items: 5 / 4 / 4 — apidev matches v20/v25, appdev/worker slightly below v25
- CLAUDE.md bytes: 4,754 / 4,584 / 5,232 — all clear the 1200-byte floor; v25 was 4,945 / 5,576 / 5,835 (v28 slightly more compressed on appdev/worker)
- Root README intro: ✅ names real managed services (v17 `dbDriver` validation holds for **8th** consecutive run)
- Preprocessor directive: ✅ all 6 envs carry `#zeropsPreprocessor=on` with `<@generateRandomString(<32>)>` for `APP_SECRET`
- Architecture section in root README: ❌ absent (same as v25 — editorial gap, intentional post-rollback)

**v8.90 calibration (from v25 defect) — ALL PASSED**:

| Bar | Result |
|---|---|
| 0 `SUBAGENT_MISUSE` rejections | ✅ 0 — no subagent called `zerops_workflow` at spawn |
| Substep attestations in canonical order, real-time | ✅ — all 12 deploy substeps completed in canonical order AS work happened (not v25's end-of-step backfill). Times: 12:48:55 deploy-dev → 12:55:50 start-processes → 12:56:18 verify-dev → 12:56:27 init-commands → 13:07:30 subagent → 13:12:01 snapshot-dev → 13:12:22 feature-sweep-dev → 13:14:49 browser-walk → 13:22:06 cross-deploy → 13:22:15 verify-stage → 13:22:25 feature-sweep-stage → 13:28:05 readmes |
| `complete init-commands` delivers subagent-brief | ✅ — feature subagent dispatched AFTER the substep attestation (13:07:30 → feature work to 13:22), confirming the brief landed in time |
| `complete feature-sweep-stage` delivers readme-fragments | ✅ — readmes substep followed at 13:28 |
| step-entry ≤30 KB | ✅ (no persist-to-disk events observed) |
| ≤2 full-step README-check failures | ✅ — exactly **1 retry** (13:28:26 first `complete step=deploy` failed 4 checks → fixed inline in 3 min → 13:31:15 retry passed) |

v25's substep-bypass pathology did **not** recur. v8.90's de-eager of `subagent-brief`/`readme-fragments` + tool-use policy in subagent briefs + `SUBAGENT_MISUSE` error code all held under production load.

**v8.93 calibration (from v26 defects) — ALL PASSED**:

- ✅ `recipePlan` accepted on first `complete step=research` call as object, no type-string retry. The one research-complete rejection at 12:27:00 was valid content (`research.sessionDriver is required for showcase tier`), not stringification — v8.93.2's object-shape jsonschema hint held.
- ✅ 0 `cd /var/www/{host} && <exec>` zcp-side patterns. Provision step git-init'd each codebase container-side via `ssh {host} "git init -q -b main && git commit --allow-empty..."` — v8.93.1's git-config-mount rewrite held.

**Close-step bugs**: 2 CRIT + 1 WRONG + 4 STYLE:

- **CRIT #1 (fixed)**: workerdev `@MessagePattern('jobs.dispatch')` missing `queue: 'jobs-workers'` — double-process at `minContainers > 1`. Fixed by code-review subagent inline.
- **CRIT #2 (fixed)**: apidev `ClientProxy.emit(...)` not wrapped in `firstValueFrom(...)` — cold Observable never subscribes, NATS never publishes. Fixed inline.
- **WRONG (fixed)**: workerdev `parseInt(DB_PORT)` without `?? '5432'` fallback → NaN. Fixed.
- **STYLE (1 taken, 3 left)**: redundant `data-feature="status"` on App.svelte wrapper (taken); untaken — fire-and-forget error boundary, unclosed `setTimeout` in JobsDispatch poll guard, non-rethrown bucket-init error.

No dedicated critical-fix subagent — main agent applied all fixes via 4 Edits, then redeployed all 3 dev targets + cross-deployed to all 3 stage targets. Stage feature-sweep re-ran green. Total fix-cycle wall ~6 min.

**v8.85 `env_self_shadow` check-surface defect (NEW, confirmed)**:

Workerdev zerops.yaml shipped with 9 self-shadow lines at first Write (12:41:37):

```yaml
run:
  envVariables:
    queue_hostname: ${queue_hostname}    # self-shadow
    queue_port: ${queue_port}            # self-shadow
    queue_user: ${queue_user}            # self-shadow
    queue_password: ${queue_password}    # self-shadow
    db_hostname: ${db_hostname}          # self-shadow
    db_port: ${db_port}                  # self-shadow
    db_user: ${db_user}                  # self-shadow
    db_password: ${db_password}          # self-shadow
    db_dbName: ${db_dbName}              # self-shadow
```

At 12:42:12 `complete step=generate` returned **11 checks** — all `app_*` or `api_*` prefixed (+ 2 un-prefixed: `zerops_yml_schema_fields`, `dev_prod_env_divergence`). **Zero `worker_*` checks, including `worker_env_self_shadow`.**

The check implementation exists at [internal/tools/workflow_checks_generate.go:111,290](../internal/tools/workflow_checks_generate.go#L290) and `ops.DetectSelfShadows` matches exactly this pattern. The worker hostname simply isn't enumerated in the generate-complete check surface.

Consequence: v8.85's promise ("one real bug class, zero false positives across v22/v23/v24 — SURVIVED rollback") failed at the first post-rollback run that would have exercised it. Agent discovered the shadow at runtime (~5-10 min recovery including renaming to `DB_HOST`/`NATS_HOST` uppercase aliases), self-reported via `zerops_record_fact`, and shipped the trap as workerdev gotcha #1 — with fabricated "timing-dependent interpolator" folk-doctrine (see below).

**`zerops_record_fact` — voluntary, load-bearing**:

Agent called `zerops_record_fact` 3× without instruction to do so:
1. **12:46:18** — execOnce silent-success seed incident
2. **12:53:13** — cross-service env var shadow loop
3. **13:19:16** — nest build empty-emit with cached tsbuildinfo

Each fact has structured fields (`type: gotcha_candidate`, title, failureMode, mechanism, fixApplied, evidence, codebase, substep). All three became published gotchas. The v8.86 `record_fact` tool is NOT orphan — it's the structured incident ledger the content-authoring pipeline (v8.94) consumes. This is the one piece of v8.86 surviving that will be load-bearing in the next reform.

**Honest content audit (the main finding — user correction drove this)**:

My first-pass review scored content by token-level markers ("names a mechanism", "names a failure mode") and called C=A−. User's audit revealed this was token-matching rather than reader-empathy. The honest grade is **C+**, derived from surface-by-surface reading against the tests in [docs/spec-content-surfaces.md](spec-content-surfaces.md):

- **Gotchas (15 total across 3 codebases)**: ~5 genuine Zerops platform teaching / ~5 self-inflicted or wrong-surface / ~4 framework-quirk borderline / **1 folk-doctrine defect**. Wrong-surface catalog:
  - *apidev #1 execOnce silent-success* — **self-inflicted**. Seed script exited 0 with no stdout (seed bug). `execOnce` correctly honored exit code. Discard class.
  - *apidev #4 Valkey-no-password* — 1-line fact bloated to 2 sentences; second sentence describes what happens if you write `${cache_password}` (which doesn't exist). Discard class.
  - *apidev #5 `setGlobalPrefix`* — pure NestJS framework documentation. Wrong surface (→ framework docs).
  - *appdev #4 api.ts helper* — documents recipe's own scaffold helper as if universal. Porter doesn't have `api.ts`. Wrong surface (→ IG principle + code comment).
  - *appdev #5 `plugin-svelte@^5` peer-dep* — npm registry metadata. Wrong surface (→ package.json notes).
- **Folk-doctrine defect**: workerdev gotcha #1 (env-shadow) claims *"The API codebase avoided the symptom because its resolver path happened to interpolate before the shadow formed; do not rely on that."* — fabricated. Both apidev and workerdev shipped identical self-shadow patterns; both were broken. The correct rule (from `env-var-model` guide, accessible during run, consulted 0 times by author) is: "cross-service vars auto-inject project-wide; NEVER declare `key: ${key}` in run.envVariables." Same class as v23's "Recovering `zsc execOnce` burn" fabrication. Author had the guide available and wrote folk-doctrine anyway.
- **Env-comment factual errors**:
  - *Env 5 (HA Production) NATS block*: *"NATS 2.12 in mode: HA — clustered broker with JetStream-style durability."* The recipe uses core NATS pub/sub with queue groups (`Transport.NATS` + `queue: 'jobs-workers'`), NOT JetStream. "JetStream-style" conflates distinct NATS subsystems.
  - *Env 1 (Remote CDE) workerdev block*: *"The tsbuildinfo is gitignored, so the first watch cycle always emits dist cleanly."* `nest start --watch` uses ts-node (not `nest build`); the `.tsbuildinfo` issue only affects the prod `nest build` path.
- **IG wrong-surface items**:
  - *appdev IG #3* explains the recipe's own `api.ts` wrapper as if it were a Zerops integration step. Principle (SPA fallback returns `200 text/html`) belongs in IG; specific helper implementation belongs in code comments.
  - *appdev IG #4* (Svelte 5 `mount()` bootstrap) is pure Svelte framework documentation. Any Svelte user already knows or finds this in Svelte docs; zero Zerops involvement.
- **Cross-surface duplication (each fact on 3–4 surfaces instead of 1 with cross-refs)**:
  - `.env` shadowing: apidev IG #3 + apidev zerops.yaml comment + apidev CLAUDE.md trap
  - `forcePathStyle: true`: apidev IG #4 + apidev gotcha #2 + env 0/5 import.yaml + apidev zerops.yaml
  - `nest build` tsbuildinfo: workerdev gotcha #2 + workerdev zerops.yaml + apidev CLAUDE.md + env 1 import.yaml (factual error on one)
  - NATS creds-as-options: apidev gotcha #3 + apidev zerops.yaml + env 0 import.yaml + workerdev IG #2
- **What's genuinely A-quality**: env 4 (Small Production) `import.yaml` comments are v7-gold-standard — two-axis `minContainers` teaching applied to every runtime service (throughput + HA axes distinct), NON_HA cost-trade explicit on managed services, no templated per-service phrasing, `app` static comment internally consistent (v25's contradiction fixed). The surface that has always been the content-grade anchor is still an anchor.
- **Env READMEs**: 7 lines of template boilerplate each — zero teaching. The most obviously-undersized surface in the recipe and a missed content opportunity for the dev/stage mental-model teaching the user explicitly called out as priority.

**Rating**: S=**A** (all 6 steps, all 5 features wired + exercised on dev AND stage, both browser walks fired, 0 CRIT shipped), C=**C+** (not A−; honest reader-facing audit: ~33% genuine platform teaching in gotchas + 1 folk-doctrine defect + 2 env-comment factual errors + pervasive cross-surface duplication + env READMEs at 7 lines of boilerplate + wrong-surface items shipped; env 4 comments + most IG items + clean CLAUDE.md lift C− to C+), O=**A−** (85 min wall ≤90 = A-band, 1.6 min main bash = A, 0 very-long, 0 MCP schema errors, 0 exit-255 classifications; 378 assistant events / 501 tool calls in upper-B band per the v22 Opus 4.7 rubric), W=**A−** (5 subagents with clean role separation — minimalist proved sufficient, substep attestations real-time + in-order, both browser walks, 22 guidance fetches, 3 voluntary `zerops_record_fact` calls, all v8.90+v8.93 calibration bars held) → **C+** overall.

*v28 is the second consecutive post-rollback run where mechanics held cleanly (v25 was the first). The substrate is stable. But v28 surfaces that token-level content checks permit decorative and folk-doctrine content because the grading agent (prior me) and the authoring agent (main session) share the same failure mode: scoring by vocabulary rather than reader-empathy. The user's audit caught it. The next load-bearing fix is at the content-authoring layer, not the check layer.*

*The architectural insight: an agent that has debugged for 85 min cannot reliably write reader-facing content about that debug spiral. Its context is saturated with "what confused me"; the reader-facing test is "what will surprise a fresh developer who read the platform docs?" These are different questions. Three sub-pathologies flow from the confusion: fabricated mental models (v23 execOnce-burn, v28 interpolator-timing), wrong-surface placement (framework docs shipped as Zerops gotchas), self-referential decoration (recipe's own scaffold helpers documented as if universal). No token-level check catches any of the three.*

**v29 calibration bar + v8.94 implementation target**:

Implementation plan: [docs/implementation-v8.94-content-authoring.md](implementation-v8.94-content-authoring.md). Authoritative spec (surface contracts + classification taxonomy + citation map + counter-examples from v28): [docs/spec-content-surfaces.md](spec-content-surfaces.md).

Five fixes, in priority order:
1. **Fresh-context content-authoring subagent after deploy.readmes substep**. Inputs: `zerops_record_fact` log + final recipe state + `zerops_knowledge` on demand + surface contracts. NO run transcript in context. Classifies every fact (platform-invariant / framework×platform / framework-quirk / scaffold-decision / operational / self-inflicted); routes to exactly one surface; discards self-inflicted and framework-quirk; cites `zerops_knowledge` guides when topic matches.
2. **`zerops_record_fact` becomes mandatory** during deploy for every incident and every non-obvious scaffold decision. No content writing happens during deploy.
3. **Env READMEs become substantive** (40–80 lines of tier-transition teaching) — written by the authoring subagent from the tier's `import.yaml` + adjacent tiers. This is where the dev/stage mental-model lives.
4. **Scaffold pre-flight traps list** in scaffold-subagent-brief: "before you ship the file, verify your generated code doesn't do X" — list the recurrent run-incident classes (self-shadow, URL-embedded NATS creds, missing `forcePathStyle`, missing `0.0.0.0`, missing `trust proxy`). Prevents recurrent run-incidents from becoming recurrent gotcha candidates.
5. **v8.85 check-surface audit**: `env_self_shadow` must enumerate every hostname at generate-complete. Unit test: three-codebase plan with `workerdev/zerops.yaml` containing `db_hostname: ${db_hostname}` → response `checks` array must contain `worker_env_self_shadow` with `status=fail`. The check existed and would have caught this class — the enumeration loop missed it.

v29 calibration bar:
- ≥80% of gotchas pass the fresh-developer surprise test (per spec-content-surfaces.md §5 Surface 5 test).
- 0 folk-doctrine defects — every gotcha whose topic matches a `zerops_knowledge` guide carries a guide citation and uses the guide's framing.
- 0 cross-surface fact duplication (each fact on exactly one surface; others cross-reference).
- Env READMEs ≥40 lines each with genuine tier-transition teaching.
- `env_self_shadow` check fires for every enumerated host at generate.
- Content-authoring subagent writes all six surface types; main agent writes zero content inline.

---

### v29 — v8.94 ships; gotcha authoring decoupled; env-README surface inherits the problem

- **Date**: 2026-04-18
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Model**: `claude-opus-4-7[1m]` (same as v22–v28)
- **Session**: `bd0c1577-c3f0-4a1b-9114-9aa0f5c75ce3`
- **Session logs**: `main-session.jsonl` (2.3 MB, 620 events) + 6 subagent logs
- **Wall**: 18:22:54 → 19:45:43 = **~83 min** (recipe-complete at 19:44:03; 100s post-close export)
- **Assistant events (main)**: 378 | **Tool calls (main)**: ~534
- **Bash metrics (main)**: 157 calls / **4.3 min total (259 s)** / **0 very-long** / **8 errored** / 0 port kills / multiple sleeps-in-pipeline
- **Bash metrics (main + 6 subagents)**: ~280 calls / ~6.5 min total / **2 long** (both `nest new scratch` inside scaffolds — 75 s apidev, 35 s workerdev) / ~10 errored
- **Subagents (6 — v8.94 shape)**:
  - Scaffold apidev (`a0e86118…`, 3:42 wall, 46 bash, **wrote `apidev/scripts/preship.sh` 2840-byte self-test script — committed, shipped**)
  - Scaffold appdev (`a77d5ab1…`, 1:20 wall, used inline `ssh ... "set -e; ..."` chain — no committed self-test)
  - Scaffold workerdev (`a885b13f…`, 1:46 wall, same pattern as appdev)
  - Feature subagent (`a95d4fed…`, 14:58 wall, 60 Reads + 40 Writes + 15 Edits, **0 MCP schema errors**, 7 `zerops_record_fact` calls — self-directed circular-import fix via `cache.tokens.ts`/`search.tokens.ts` extract, applied proactively to status + items)
  - **README/CLAUDE writer (`a9fbc0f2…`, 9:14 wall — the v8.94 fresh-context content-author** — brief explicitly marked *"You have no memory of the run that dispatched you. Your context is intentionally clean of the debug spiral"*, pointed at `/tmp/zcp-facts-{session}.jsonl`, 12 KB brief including tool-use policy + pre-classified fact taxonomy + list of platform-invariant gotchas to actively consider; returned 13 files with structured classification tallies + env-comment-set JSON payload)
  - Code review (`a9699956…`, 5:23 wall, 50 Reads + 8 Greps + 2 Edits — inline-fixed 2 STYLE findings: Meilisearch silent `.catch` → `logger.warn`, workerdev `job.entity` column types aligned to migration)
- **MCP tool mix (main)**: 31 `zerops_workflow`, 23 `zerops_dev_server`, 18 TodoWrite, 10 `zerops_record_fact` (new load-bearing cadence; v28: 3), 8 `zerops_browser` (4 deploy + 2 close + 2 close-retry), 6 `zerops_guidance`, 5 `zerops_deploy` + 2 `zerops_deploy_batch`, 4 `zerops_subdomain`, 4 `zerops_verify`, 3 each `zerops_mount`/`zerops_logs`/`zerops_knowledge`, 1 each `zerops_import`/`zerops_env`/`zerops_discover`/`zerops_workspace_manifest`
- **`record_fact` structure**: 3 main-authored (TypeORM class-name, seed execOnce silent-success, healthCheck bare-GET) + 7 feature-subagent-authored (Nest circular-import, NATS subject+JSONCodec contract, S3 presigned external host, @types/multer, @MessagePattern fire-and-forget, api.ts FormData, Meili auto-sync). Writer brief pre-classified all 10 with explicit DISCARD / route-to-gotcha / route-to-CLAUDE instructions; writer largely executed the plan but overrode DISCARD on 2 facts (kept healthCheck-bare-GET as apidev gotcha, kept api.ts-FormData as appdev gotcha — both flagged in content audit as wrong-surface).
- **Deploy call breakdown (7 calls)**: 3 individual apidev dev deploys (migration-name fix loop — renamed `InitialSchema0001` → `InitialSchema1700000000000` after TypeORM rejected non-timestamp class name, re-imported in `data-source.ts`, 3rd deploy succeeded) + 1 workerdev dev + 1 appdev dev + 2 `deploy_batch` (snapshot-dev ×3 at 19:04 + cross-stage ×3 at 19:12). Total 11 deploy operations across 7 tool calls. All justified by intervening commits.
- **Workflow `complete deploy` calls**: 14 — 12 substep completes (real-time, canonical order) + 1 full-step fail (3 content checks) + 1 full-step retry after 3-min inline fix

**Content metrics** (apidev / appdev / workerdev):

- README lines: 282 / 174 / 258 (bytes 12,572 / 8,844 / 9,210)
- Gotchas: **5 / 4 / 5** — balanced; all reference platform mechanisms + concrete failure modes
- IG items: 5 / 4 / 3 — apidev matches v28; appdev unchanged; workerdev 3 items all carry code blocks
- CLAUDE.md: 5,257 / 4,218 / 4,591 bytes (v28: 4,754 / 4,584 / 5,232) — all ≥ 1200 floor; 7 sections each with 3 custom beyond template (apidev adds Migrations & Seed, Testing & Driving Feature Paths, Log Tailing, Resetting Dev State, Adding a New Managed Service; workerdev adds Driving a Test Job End-to-End, Testing a Queue-Group Scale-Out Locally, Driving a SIGTERM Drain Test)
- **Root README intro**: ✅ names real managed services (*"PostgreSQL, Valkey (Redis-compatible), NATS, S3-compatible object storage, and Meilisearch"*) — v17 `dbDriver` validation holds for **9th** consecutive run
- **Preprocessor directive**: ✅ all 6 envs carry `#zeropsPreprocessor=on` with `<@generateRandomString(<32>)>` for `APP_SECRET`
- **Env READMEs**: 42/40/40/43/46/46 lines (floor 40 — writer iterated env 0 three times at 37 → 38 → 40 lines to pass floor; self-verifying behavior worked)
- Architecture section in root README: ❌ absent (consistent with v25/v28 post-rollback; `recipe_architecture_narrative` check rolled back)

**Env 4 (Small Production) import.yaml comment quality — mostly gold, one known-class contradiction**:

- `api` block: two-axis `minContainers` teaching intact — *"minContainers:2 because a single-replica pool drops traffic on every rolling deploy and on container crashes — this tier's throughput fits in one container, so this is NOT horizontal scaling for load; the second replica exists for HA / rolling-deploy availability"*.
- `worker` block: queue-group `jobs-worker` + SIGTERM drain mechanisms both named with concrete production-correctness justification.
- `app` (static) block: **self-contradicting** — comment line 24-27 reads *"the platform serves multiple replicas from one build artifact, so minContainers:2 on a static service is not needed"* directly above the YAML declaration `minContainers: 2`. Same defect class as v25's env 4 app-static contradiction. `factual_claims` regex catches numeric disagreement but misses this semantic contradiction (both sides read "2"). Finalize check did NOT fire on this block.
- Managed-service blocks (db/redis/queue/storage/search): clean per-service WHY explanations; no templated phrasing.

**Gotcha-origin audit (the v29 headline win)** — 14 total gotchas across apidev/appdev/workerdev, classified by origin:

| Codebase | Total | Genuine platform teaching | Wrong-surface / self-inflicted | Borderline framework-quirk |
|---|---:|---:|---:|---:|
| apidev | 5 | 4 (MinIO `forcePathStyle` / `NoSuchBucket`; Valkey no-auth URL DNS trap; execOnce-exit-0 contract → `${appVersionId}` keying; presigned S3 GET uses external `storage-prg1.zerops.io` host) | 1 (feature-sweep-bare-GET — recipe-workflow-authoring gotcha, not porter-hazard; brief said DISCARD) | 0 |
| appdev | 4 | 3 (Vite `allowedHosts` `HTTP 200 Blocked request`; `./dist/~` tilde-strip for Nginx static; `VITE_API_URL` bake-at-build vs read-at-run) | 1 (Multer `Unexpected end of form` on Content-Type override — documents recipe's own `api.ts` scaffold; brief said DISCARD) | 0 |
| workerdev | 5 | 4 (queue-group under `minContainers > 1`; SIGTERM drain via `app.close()` + `onModuleDestroy`; portless service needs no readinessCheck block; NATS URL-cred cross-ref to apidev) | 0 | 1 (`@MessagePattern` fires for fire-and-forget — defensive clarification about when to stay vs switch to `@EventPattern`) |
| **Total** | **14** | **11/14 (79%)** | **2 (14%)** | **1 (7%)** |

**vs v28's 33% genuine / 1 folk-doctrine defect, v29 is 79% genuine / 0 folk-doctrine in the gotcha surface.** This is the v8.94 architectural win: fresh-context writer + structured facts log + pre-classification taxonomy + discard-boundary in brief produces higher gotcha-origin quality at the gotcha surface.

**Three confirmed defect classes shipped (triple-verified against published deliverable)**:

1. **`apidev/scripts/preship.sh` recipe-infrastructure leak**. File at [apidev/scripts/preship.sh](apidev/scripts/preship.sh) in the published deliverable (2,840 bytes, 67 lines, 12 `sh` assertions). Written by apidev scaffold subagent (`a0e86118…`) at 18:31:38, committed via `git add -A`, never cleaned up. Lines 58-63 contain assertions meaningful ONLY during recipe authoring: `# 11. no README.md (main agent owns it)` + `fail "README.md must not be written by scaffolder"` + `# 12. no zerops.yaml (main agent owns it)` + `fail "zerops.yaml must not be written by scaffolder"`. A porter cloning the recipe, running the script to "verify the scaffold", sees assertions that reject their own README and zerops.yaml. **Asymmetric** — appdev + workerdev scaffold subagents used inline `ssh ... "set -e; echo assert 1; ..."` chains (no file-based test, nothing to clean up) and shipped nothing. Code-review subagent saw the file at 19:39:37 and explicitly said *"That's a recipe-level self-check script — out of scope for code review"* — acknowledged the anomaly and chose not to flag it. Different subclass of v21's 208 MB `node_modules` leak (2.8 KB vs 208 MB, different leaked content type, SAME structural class: scaffold-phase artifact in published deliverable because there's no explicit rule against committing self-test infrastructure).

2. **env 0 README cross-tier data-persistence fabrication**. `environments/0 — AI Agent/README.md` lines 26 + 33 ship the claim:
   > *"Data persists across tier promotions because service hostnames stay stable — you can iterate here and move up without rebuilding the database from scratch."* (L26)
   > *"Existing data in DB / cache / storage persists across the tier bump because hostnames are identical."* (L33, in "Promoting to the next tier" section)

   **Factually wrong for the default `zerops_import` deployment path.** The six env `import.yaml` files declare six distinct `project.name` values: `nestjs-showcase-agent`, `nestjs-showcase-remote`, `nestjs-showcase-local`, `nestjs-showcase-stage`, `nestjs-showcase-small-prod`, `nestjs-showcase-ha-prod`. Deploying a later-tier `import.yaml` creates a NEW project; service hostnames colliding inside a new project does nothing to bridge data from the old project. `override-import` exists as a Zerops mechanism for modifying an existing project's composition in place, but the env READMEs don't mention it. This is the v23 "execOnce burn from initial workspace deploy" / v28 "timing-dependent interpolator" defect class — fabricated platform semantics, codified in published content, future readers will inherit. Scope triple-confirmed: 2 lines in env 0 README only; env 1-5 READMEs do NOT repeat the claim.

3. **env 3/4/5 README `minContainers` factual drift — sourced from Go templates, not writer behavior** (attribution corrected via second v8.95 dry-run simulation). Four wrong claims across three files, with exact line numbers:

   - env 3 README L32 (in "Promoting to Small Production" section): *"Runtime container stays at `minContainers: 1`"* — **WRONG**, env 4 YAML declares `minContainers: 2` on api/app/worker. Source: [internal/workflow/recipe_templates.go:324](../internal/workflow/recipe_templates.go#L324) (hardcoded in `envPromotionPath(3)`).
   - env 4 README L24: *"Runtime services stay at `minContainers: 1` but are sized for real traffic"* — **WRONG**, already at 2. Source: [recipe_templates.go:279](../internal/workflow/recipe_templates.go#L279) (`envDiffFromPrevious(4)`).
   - env 4 README L33 (in "Promoting to HA" section): *"Runtime `minContainers` flips to `2` so rolling deploys survive zero-downtime"* — **WRONG on both ends**: env 4 is already 2 (no flip), env 5 stays at 2 (no flip). Source: [recipe_templates.go:330](../internal/workflow/recipe_templates.go#L330) (`envPromotionPath(4)`).
   - env 4 README L41: *"Deploys incur downtime because `minContainers: 1` — the single runtime container is replaced during deploy"* — **WRONG**. Source: [recipe_templates.go:370](../internal/workflow/recipe_templates.go#L370) (`envOperationalConcerns(4)`).
   - env 5 README L20 (*"Every runtime `minContainers` flips to `2`"*) — misleading; framing implies a flip from 1 to 2 at the env 4→5 transition, but env 4 is already 2. Source: [recipe_templates.go:285](../internal/workflow/recipe_templates.go#L285) (`envDiffFromPrevious(5)`).

   **Root-cause correction**: `BuildFinalizeOutput` at [internal/workflow/recipe_templates.go:42](../internal/workflow/recipe_templates.go#L42) calls `GenerateEnvREADME(plan, i)` to produce each env README's content, then writes it to `environments/{EnvFolder(i)}/README.md`. The writer subagent's own env README output (written per its brief to `environments/{N — brief-label}/README.md`) is orphaned in a different folder-named directory and never overlaid — `OverlayRealREADMEs` at [internal/workflow/recipe_overlay.go:27](../internal/workflow/recipe_overlay.go#L27) only overlays per-CODEBASE READMEs, not env READMEs. Every line of env README PROSE in the published tree came from Go templates, not writer output. This was misattributed to writer behavior in the initial v29 analysis; the second v8.95 implementation-dry-run surfaced the actual source layer.

   Finalize `factual_claims` check DID fire 4 times and caught 4 real defects (env 0/1/2 storage comment 2GB↔objectStorageSize:1; env 4 worker comment `minContainers:1`↔YAML `2`) — all in `import.yaml` comments. **But the check does not scan env README prose**, so the 4 env 3/4 README prose errors above slipped through; for v8.95 purposes the README-prose check isn't the right fix either — editing the Go templates at the source + regression-testing them against the YAML they're supposed to describe is the root-cause fix. Additionally, env 4 + env 5 YAML app-static comments each contain an internal contradiction of the v25 class: *"minContainers:2 on a static service is not needed"* directly above `minContainers: 2` — `factual_claims` regex catches when numbers disagree but misses when comment says "X is not needed" alongside a YAML that declares X (both sides match "2"; no numeric delta). That one IS writer- or Go-template-generated — depends on env-comment-set flow; v8.95 leaves this as editorial per the rollback calibration rule.

**Operational metrics — best hygiene since v18/v25 class**:

| Axis | v29 | v28 | v25 | v20 |
|---|---:|---:|---:|---:|
| Wall | 83 min | 85 | 71 | 71 |
| Main bash total | 4.3 min | 1.6 | 0.5 | 2.3 |
| Main very-long | 0 | 0 | 0 | 0 |
| Main errored | 8 | 5 | 0 | 7 |
| Tool calls (main) | ~534 | ~501 | 177 | 177 |
| `record_fact` calls | **10** | 3 | 0 | 0 |
| Subagents | 6 | 5 | 7 | 10 |
| Close bugs | **0/0/4** | 2/1/4 | 0/1/3 | 3/6/3 |

Main bash 4.3 min is higher than v25/v28 but below the v20 → v28 → v29 ~5 min threshold — driver is `zerops_browser` ×8 (78 s) + `zerops_deploy` ×5 (395 s) + feature-subagent-era main-context verification curls. Zero very-long, 0 MCP schema errors, 0 exit-255 dev_server failures. Substep attestations real-time in canonical order (v25 bypass did NOT recur; v8.90 held). Deploy-step 52 min of which ~16 min is the readmes-substep cycle (writer 9 min + full-step content-check failure + 3 min main-inline fix + retry pass) — **single fix round**, v23's 5-round anti-convergence spiral did not recur.

**Close review (cleanest since v22)**: 0 CRITICAL / 0 WRONG / 4 STYLE. Feature-coverage scan: all 5 present with exact source-file:line citations. Silent-swallow scan clean. `tsc --noEmit` exit 0 on apidev + workerdev; `npm run check` 0 errors on appdev. Two STYLE fixes applied inline (Meilisearch silent `.catch` → `logger.warn`; workerdev `job.entity` column types aligned to `0001-initial` migration truth). Four STYLE not applied (redundant `DbModule` import, empty `OnModuleDestroy`, DI-token location drift, CORS fallback `origin:'*' + credentials:true` spec-invalid when both `DEV_APP_URL` + `STAGE_APP_URL` absent).

**Writer-kept-despite-DISCARD flag**: the v8.94 brief explicitly pre-classified 2 of the 10 recorded facts as DISCARD (apidev `healthCheck` bare-GET = recipe-authoring concern, appdev Multer FormData Content-Type = framework-quirk documenting own scaffold helper). Writer kept both as published gotchas anyway. v8.94 architecture works at the dispatch and classification layer but doesn't enforce DISCARD as a hard gate — the writer treated it as a suggestion. 2/14 = 14% rate of writer override.

**Feature-subagent recorded-and-discarded defect class — the missing taxonomy route**: feature subagent hit Nest circular-import between `cache.module.ts` and `cache.controller.ts` via `REDIS_CLIENT`, extracted to `cache.tokens.ts`, then proactively applied the same fix to `search`, `status.controller.ts`, and `items.service.ts`. The fix is clean. The recorded fact was classified as "framework-quirk — DISCARD" by the brief. But this is a **recurrent run-incident class** (scaffold pattern that's known to cause the same bug) — it needs to route to the **scaffold pre-flight preamble surface**, not be discarded. Same pattern as v22's NATS URL-embedded creds recurrence: scaffolding the same buggy pattern every run, fixing it every run, discarding the fact every run. v8.94 taxonomy has no "framework-quirk with recurrence risk → scaffold-preamble" route.

**Notable**:

- **v8.94 content-authoring subagent architecture works for the gotcha surface.** Fresh-context write, pre-classified facts, explicit tool-use policy, structured return. 79% genuine gotcha-origin rate is a 2.4× improvement over v28's 33%.
- **But v8.94 exposes a separate defect class in the Go templates that generate env READMEs.** Initial analysis misattributed the env-README defects to writer behavior; the second v8.95 implementation-dry-run traced every defect byte-for-byte to hardcoded strings in `recipe_templates.go` (lines 172, 279, 285, 305, 324, 330, 370). The writer subagent's env-README output is orphaned — `BuildFinalizeOutput` always overwrites it with `GenerateEnvREADME`'s output, which is determined solely by envIndex + plan + hardcoded `switch envIndex` arms. v8.94 introduced the 40-80-line env READMEs with those hardcoded claims; v29 is the run that surfaced them. The correct fix is at the Go-template layer, not at the writer-brief layer.
- **Scaffold self-test machinery shipped as deliverable.** `preship.sh` on apidev only, because the scaffold-brief didn't forbid committing self-test scripts. Asymmetric behavior across three subagents with identical brief shape: apidev wrote to file + committed; appdev + workerdev used inline shell. Different local choice, same brief — no rule forced cleanup.
- **10 `record_fact` calls** (v28: 3) — voluntary cadence hit load-bearing frequency. 7 of 10 became published gotchas (via writer routing); 3 were correctly discarded.
- **Main bash 4.3 min higher than v25/v28** but still A-band (≤10). Driver: 8 `zerops_browser` calls (browser walks fired on both deploy and close — v8.77 sub-step gate held).
- **Cleanest close review since v22**: 0 CRIT / 0 WRONG / 4 STYLE. Code-review subagent finished in 5:23 with 2 STYLE fixes applied inline.

**v29 is the second consecutive run where mechanics held (v25, v28, v29 all clean on S/O/W axes) while content quality varies**. The v8.94 architecture IS the right direction for writer-controlled surfaces (per-codebase READMEs / CLAUDE.md / gotchas — 79% genuine gotcha origin confirms). The env-README surface is NOT writer-controlled today; it's Go-template-generated and the v8.94 templates shipped with hardcoded factually-wrong claims. v8.95 fixes both layers: edits the Go templates directly (for env READMEs) AND enforces brief DISCARD as a hard gate via a structured writer-output contract (`ZCP_CONTENT_MANIFEST.json`). The "framework-quirk with recurrence risk" taxonomy route is deferred to v8.96 — it needs a structural question about cross-run fact comparison.

**Rating**: S=**A** (all 6 steps, all 5 features wired + exercised on dev AND stage, both browser walks fired, 0 CRIT shipped after close), C=**B−** (79% genuine gotcha-origin is a real step up from v28's 33%; 0 folk-doctrine in gotcha surface; BUT three defect classes in non-gotcha surfaces: `preship.sh` leak + env-0 data-persistence fabrication + env-3/4/5 minContainers factual drift; 2/14 writer DISCARD override; env 4 app-static YAML/comment contradiction recurrence — these drop from B+ to B−; env 4 api/worker comments still v7-gold, apidev CLAUDE.md substantive with custom procedures, close review spotless), O=**A** (83 min wall ≤90 = A, 4.3 min main bash ≤10 = A, 0 very-long, 0 MCP schema errors, 0 exit-255 classifications, 8 errored ≤10), W=**A−** (6 subagents with clean role separation including v8.94 fresh-context writer, substep attestations real-time in canonical order, v8.90 + v8.93 calibrations held, 10 `record_fact` at load-bearing cadence, single-round content-fix loop; writer-override-DISCARD behavior docks from A to A−) → **B−** overall.

*v29 is a partial win for v8.94. The gotcha-origin ratio improvement (33% → 79%) is real and measurable. The architecture decouples recording from authoring for the gotcha surface. What v29 reveals is that env-READMEs are a separate content surface with their own "authoring from mental model vs reading disk" failure mode — and the v8.94 writer brief's "read the final recipe state" rule didn't extend there. The defect class is the same (fabrication + factual drift), just in a different surface. v8.95 fix is targeted: extend the read-disk-first rule to env READMEs, enforce brief DISCARD as a hard post-hoc check, add the "framework-quirk with recurrence risk" taxonomy route to route fix-recurrence-class facts to scaffold-preamble instead of discarding. v29 post-mortem + v8.95 implementation plan at [docs/implementation-v8.95-content-surface-parity.md](implementation-v8.95-content-surface-parity.md). Expected v30 outcome: B+ overall with gotcha-origin ≥75% (target-sustain), env-README 0 factual drift, `preship.sh`-class artifacts blocked pre-commit.*

**v30 calibration bar (carried forward from v29's defects):**

- 0 scaffold-phase artifacts committed to published deliverable (grep-verifiable: `find published/ -name 'preship.sh' -o -name '*.assert.sh' -o -name 'scaffold-*' | wc -l == 0`)
- 0 env README factual-drift claims (grep env READMEs for `minContainers: \d`, `objectStorageSize: \d`, `mode: (HA|NON_HA)` claims; every such claim must match the adjacent env's import.yaml for the same service)
- 0 cross-tier-data-persistence fabrications in env READMEs — either the README names `override-import` explicitly as the promotion mechanism OR states that data does not carry across new project imports
- Writer DISCARD override rate ≤ 0 (writer must not ship a gotcha the brief explicitly marked DISCARD — enforced either by stricter brief framing or by post-writer gate checking recorded-facts log vs published gotcha titles)
- Gotcha-origin ratio ≥ 75% genuine platform teaching (sustain v29's 79% win without regression)
- Recurrent run-incident classes route to scaffold-preamble surface: v29's `cache.tokens.ts` extraction pattern added to scaffold brief's "before you ship the file, verify you don't declare DI symbols in the same module file that lists controllers" preamble
- All v8.90+v8.93+v8.94 calibration items held (real-time substep attestations; 0 SUBAGENT_MISUSE; 0 `cd /var/www/{host} && <exec>` zcp-side; fresh-context writer dispatched; facts log ≥ 5 entries)

---

### v30 — v8.94 reproducibility run (v8.95 planned but unshipped); gotcha-origin peaks at 88%, env-README Go-template defects persist

- **Date**: 2026-04-19
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Model**: `claude-opus-4-7[1m]` (same as v22–v29)
- **Code state at run time**: **v8.94 unchanged** — last code commit `2d9e277` (v8.94); `8ecb9c7` is docs-only (v29 entry + v8.95 plan). No branch contains the `workflow_checks_scaffold_artifact.go` / `workflow_checks_content_manifest.go` files the v8.95 plan specifies; `grep -rn 'scaffold_artifact\|content_manifest\|ZCP_CONTENT_MANIFEST' internal/` returns zero hits. Defective Go-template strings identified by the v29 postmortem (L172 data-persistence fabrication, L279/L285/L305/L324/L330/L370 minContainers drift) still present verbatim at their v8.94 line numbers. **v30 is therefore a v8.94 reproducibility / agent-variance test, not a v8.95 validation run**; v31 becomes the v8.95 validation after the plan lands.
- **Session**: `c2e9fc74240a4f2c`
- **Session logs**: `main-session.jsonl` (614 lines) + 6 subagent logs
- **Wall**: 09:24:31 → 10:28:17 = **64 min — new all-time fastest complete showcase run** (v12: 61 min but pre-session-logs and content-thin; v18: 65; v25: 71; v20: 71; v28: 85; v29: 83)
- **Assistant events (main)**: **281** (v29: 378, v28: 378, v25: 225, v20: 294) — −26% vs v29; second-lowest for a complete run after v25
- **Tool calls (main)**: ~458 (v29: ~534, v28: ~501, v25: 177)
- **Bash metrics (main)**: 116 calls / **4.6 min total (276 s)** / **0 very-long** / 13 errored (tool-total across all types) / 0 port kills / multiple short curls
- **Bash metrics (main + 6 subagents)**: ~240 calls / ~7 min total / **0 very-long across the full tree**
- **Subagents (6 — same minimalist v8.94 shape as v29)**:
  - Scaffold appdev (`a3c8716c…`, 0:48 wall)
  - Scaffold apidev (`ae5291de…`, 2:39 wall)
  - Scaffold workerdev (`aef14a6a…`, 1:14 wall)
  - Feature subagent (`aedcd287…`, **8:47 wall**, 60 Reads + 40 Writes + 15 Edits, **0 MCP schema errors**, 6 `zerops_record_fact` calls — fastest feature-subagent on record since v18)
  - README/CLAUDE writer (`a64d22d0…`, 6:12 wall — v8.94 fresh-context writer, 12 Read + 6 Write, brief includes TOOL-USE POLICY + pre-classified fact taxonomy + MANDATORY gotchas list + self-review test per surface)
  - Code review (`af3bc143…`, 2:06 wall, NestJS + Svelte + TypeScript framework expert)
- **MCP tool mix (main)**: 27 `zerops_workflow`, 20 `zerops_dev_server`, 16 TodoWrite, **8 `zerops_record_fact`** (v29: 10 — sustained load-bearing cadence), 6 `zerops_guidance`, 6 `Agent`, 4 `zerops_browser` (2 deploy + 1 close re-walks + 1 extra at close), 4 `zerops_subdomain`, 3 `zerops_mount`, 3 `zerops_deploy`, 3 `zerops_deploy_batch`, 10 ToolSearch, 8 `zerops_knowledge`, 2 `zerops_logs`, 2 `zerops_verify`, 1 each `zerops_import`/`zerops_env`/`zerops_discover`/`zerops_workspace_manifest`
- **Deploy call breakdown (6 individual + 2 batch = 11 operations)**: 3 initial dev (apidev 1m34s, workerdev 1m29s, appdev 1m11s at 09:38–09:42) + feature-subagent inline dev verification + snapshot-dev batch (3 services at 09:58) + cross-deploy batch (3 services at 10:01) + close-step workerdev+workerstage redeploy after SIGTERM CRIT fix (10:23–10:25)
- **Workflow `complete deploy` calls**: 14 total — 12 substep completes (real-time, canonical order; first at 09:42:12, last at 10:12:45) + **2 full-step retries** on content checks (10:12:51 with 5 fails → 10:14:34 with 1 fail → 10:15:15 pass) — exactly at v8.90 calibration bar of ≤2

**Content metrics** (apidev / appdev / workerdev):

- README lines: 263 / 161 / 165 (v29: 282/174/258; apidev −19 lines, workerdev −93 lines)
- README bytes: 11,878 / 7,875 / 8,925
- Gotchas: **6 / 5 / 6** — balanced distribution, +1 each vs v29's 5/4/5; matches v22/v20-range on apidev/worker, below v22 peak of 7/6/6
- IG items: **3 / 3 / 3** — **regression** vs v29's 5/4/3 and v20 peak of 6/5/5; at the floor across all three codebases
- CLAUDE.md lines: 84 / 68 / 85 (v29: 5,257/4,218/4,591 bytes); bytes measured 4,012/3,118/3,714 — **tighter than v29** on appdev/worker (25–33% smaller) but still clear the 1200-byte floor with 3+ custom sections each (apidev adds Resetting Dev State, Driving a Test NATS Job, Testing; workerdev adds Draining the Queue During Iteration, Inspecting NATS Subjects Directly)
- **Root README intro**: ✅ names real managed services (*"PostgreSQL, Valkey (Redis-compatible), NATS, S3-compatible object storage, and Meilisearch"*) — v17 `dbDriver` validation holds for **10th** consecutive run
- **Preprocessor directive**: ✅ all 6 envs carry `#zeropsPreprocessor=on` with `<@generateRandomString(<32>)>` for `APP_SECRET`
- **Env READMEs**: 42 / 40 / 40 / 43 / 46 / 46 lines (envs 0–5) — identical byte patterns to v29 because env READMEs come from Go templates (`GenerateEnvREADME(plan, i)` + `BuildFinalizeOutput`), not from the writer subagent

**Gotcha-origin audit — NEW PEAK**:

17 total gotchas across apidev/appdev/workerdev, classified against the spec-content-surfaces.md fresh-developer surprise test:

| Codebase | Total | Genuine platform teaching | Wrong-surface | Borderline / other |
|---|---:|---:|---:|---:|
| apidev | 6 | **6** (L7 balancer 0.0.0.0 bind; `.env` shadows Zerops env; TypeORM synchronize × `zsc execOnce`; MinIO `forcePathStyle` 403; rolling-deploy `readinessCheck`; cross-service refs resolved at container start) | 0 | 0 |
| appdev | 5 | 4 (dev/stage service-type divergence; `./dist/~` tilde suffix; `VITE_API_URL` build vs run envVariables; dev subdomain needs explicit `ports+httpSupport:true`) | 1 (#5 "VITE_* names are public" — pure Vite framework docs) | 0 |
| workerdev | 6 | 5 (nats.js v2 strips URL creds; missing `queue` option double-processes; no readiness check → verify via logs; SIGTERM drain via `app.close()`; worker FS resets on deploy) | 0 | 1 (#6 shared DB entity lockstep — cross-codebase architecture concern) |
| **Total** | **17** | **15/17 = 88%** | **1 (6%)** | **1 (6%)** |

**vs v29's 79% / v28's 33%, v30 is 88% genuine gotcha-origin — new peak in tracked history.** Zero folk-doctrine defects, zero fabricated mental models, **5 of 6 apidev gotchas carry explicit `zerops://guides/<topic>` citations** (the v8.94 citation-map rule is paying off for the run's most-consulted codebase).

**Writer DISCARD enforcement — fully held**:

Facts log (`/tmp/zcp-facts-c2e9fc74240a4f2c.jsonl`) recorded 8 facts during deploy: 2 from main (both `fix_applied` class — scaffold rename mismatches: worker `DB_PASSWORD` vs yaml `DB_PASS`; apidev storage env names vs yaml keys) + 6 from feature subagent (`jobs.dispatch` payload cross-codebase contract, ClientProxy fire-and-forget, Multer devDep, Meilisearch `waitForTask`, S3 presigned `forcePathStyle`, cache-aside `DEL` invalidation). Writer classification tally: 2 intersection **kept**, 3 framework-quirks **discarded**, 1 library-meta **discarded** (Multer devDep), 2 self-inflicted **discarded** (both rename mismatches). **0 DISCARD overrides** observed in the published gotcha set — zero rename-mismatch gotchas, zero Multer-FormData gotcha, zero library-meta decoration. Contrast v29's 2/14 override rate; v28's 5/15 wrong-surface rate.

**v8.90 + v8.93 calibration bars — ALL PASSED**:

| Bar | Result |
|---|---|
| 0 `SUBAGENT_MISUSE` / `PREREQUISITE_MISSING` / `INVALID_PARAMETER` workflow rejections | ✅ 0 across the full run |
| Substep attestations in canonical order, real-time | ✅ 12 deploy substeps completed 09:42:12 → 10:12:45 in canonical order while work happened (not v25-style end-of-step backfill) |
| `complete init-commands` delivers subagent-brief | ✅ feature subagent dispatched AFTER init-commands attestation, brief landed in phase |
| `complete feature-sweep-stage` delivers readme-fragments | ✅ writer dispatched AFTER feature-sweep-stage attestation |
| step-entry ≤ 30 KB | ✅ no persist-to-disk events observed |
| ≤ 2 full-step README-check failures | ✅ exactly 2 retries (5 fails → 1 fail → pass) |
| `recipePlan` accepted on first `complete step=research` call | ✅ 0 stringification retries (v8.93.2 held) |
| 0 `cd /var/www/{host} && <exec>` zcp-side patterns | ✅ provision step git-init'd container-side (v8.93.1 held) |
| Fresh-context writer dispatched, facts log ≥ 5 entries | ✅ writer brief explicitly marks "NO memory of the debug spiral"; facts log had 8 entries |

**v8.95 calibration bars — MIXED (because v8.95 fixes did not ship)**:

| Bar (from v29 log entry's §v30 calibration) | Result | Reason |
|---|---|---|
| 0 scaffold-phase artifacts committed (`preship.sh` class) | ✅ | `find apidev/scripts/ appdev/scripts/ workerdev/scripts/` returns nothing; preship.sh did not recur |
| 0 env-README factual-drift claims | ❌ | **11 wrong claims** across env 0/4/5 READMEs — identical to v29, attributable to unshipped Go-template fixes |
| 0 cross-tier data-persistence fabrications | ❌ | env 0 README L26 + L33 still ship "Data persists across tier promotions because service hostnames stay stable" — byte-for-byte identical to v29 |
| Writer DISCARD override rate = 0 | **✅** | **0/17 gotchas sourced from DISCARD-classified facts** (v29: 2/14) — writer brief's plain-text taxonomy + self-review test held without structural manifest |
| Gotcha-origin ratio ≥ 75% genuine | **✅ 88%** | new peak — sustained and improved on v29's 79% |
| Recurrent run-incident → scaffold preamble | ❌ | workerdev/src/main.ts shipped WITHOUT `process.on('SIGTERM', …)` handler → 1 CRIT at close review — the writer-brief's MANDATORY gotcha on SIGTERM drain was taught in README while the scaffolded code didn't implement it; v8.95 Fix #4 (scaffold pre-flight traps list) would have closed this |
| All v8.90+v8.93+v8.94 calibration items held | ✅ | see table above |

**Persisting env-README Go-template defects** (all 11 reproduce v29 byte-for-byte — same `recipe_templates.go` lines as v29 postmortem):

| Env README | Wrong claim | Ground truth | Template source |
|---|---|---|---|
| env 0 L26 | "Data persists across tier promotions because service hostnames stay stable" | Fabrication — env 0 `project.name=nestjs-showcase-agent`, env 1 `nestjs-showcase-remote` etc.; deploying a later-tier `import.yaml` creates a new project | [recipe_templates.go:172](../internal/workflow/recipe_templates.go#L172) |
| env 0 L33 | "Existing data in DB / cache / storage persists across the tier bump because hostnames are identical" | Same fabrication | [recipe_templates.go:305](../internal/workflow/recipe_templates.go#L305) |
| env 4 L15 | "Runtime runs one container" | env 4 yaml declares `minContainers: 2` on app/api/worker | `envAudience(4)` (L231-239) |
| env 4 L18 | "Expect brief downtime on every deploy — there is no HA replica" | env 4 has 2 replicas → already HA | `envAudience(4)` (L238) |
| env 4 L24 | "Runtime services stay at `minContainers: 1` but are sized for real traffic" | Wrong | [recipe_templates.go:279](../internal/workflow/recipe_templates.go#L279) |
| env 4 L25 | "Deploys still incur downtime — no HA replicas to absorb traffic" | Wrong | [recipe_templates.go:280](../internal/workflow/recipe_templates.go#L280) |
| env 4 L33 | "Runtime `minContainers` flips to `2` so rolling deploys survive zero-downtime" (in "Promoting to HA" section) | Wrong framing — env 4 is already 2, env 5 stays at 2 | [recipe_templates.go:330](../internal/workflow/recipe_templates.go#L330) |
| env 4 L36 | "After the flip, subsequent deploys incur zero downtime" | Wrong — env 4 already has zero-downtime deploys | [recipe_templates.go:333](../internal/workflow/recipe_templates.go#L333) |
| env 4 L41 | "Deploys incur downtime because `minContainers: 1` — the single runtime container is replaced during deploy" | Wrong | [recipe_templates.go:370](../internal/workflow/recipe_templates.go#L370) |
| env 5 L20 | "Every runtime `minContainers` flips to `2`" | Misleading framing — env 4 is already 2 | [recipe_templates.go:285](../internal/workflow/recipe_templates.go#L285) |
| env 5 L24 | "Workers add a queue-group" | Wrong — env 4 yaml L51 comment already names `queue: 'showcase-workers'` | [recipe_templates.go:289](../internal/workflow/recipe_templates.go#L289) |

**Env 4 YAML comments themselves are CLEAN** — writer-authored `env-comment-set` for env 4 passed through `generate-finalize` without the v25/v29 app-static self-contradiction (*"minContainers:2 on a static service is not needed"* + declared `minContainers: 2`). env 4 import.yaml L22-25 correctly frames the static replica as "HA risk lives entirely in the API layer" with minContainers:2 making sense. The writer's output on the env-comment-set surface is strictly an improvement over v25 and v29. Only the Go-template-generated env README **prose** lies.

**Deploy-step content check failures (2 rounds)**:

Round 1 (10:12:51, 5 fails):
- `fragment_intro_blank_after_marker` ×3 (all 3 codebase READMEs missing blank line after `#ZEROPS_EXTRACT_START:intro#`)
- `app_gotcha_distinct_from_guide` (appdev had 2 gotchas restating IG items: Vite allowedHosts, 0.0.0.0 bind)
- `comment_ratio` (apidev zerops.yaml IG-embedded comment ratio 28%, below 30% floor)

Round 2 (10:14:34, 1 fail):
- `comment_ratio` (still below floor after first pass)

Round 3 (10:15:15): pass. Main agent resolved inline in ~3 min. Single-round convergence after initial detection; v23-class 5-round anti-convergence spiral did NOT recur.

**Finalize-step content check failures (1 round, 11 fails → resolved)**:

First pass (10:16:31, 11 fails):
- `*_import_cross_env_refs` ×6 (all envs 0-5 — comments referenced sibling tiers explicitly; each `import.yaml` is published standalone)
- `*_import_comment_ratio` ×4 (envs 2, 3, 4, 5 below 30% floor)
- `*_import_comment_depth` ×1 (env 2 WHY-depth 9%, below 35% threshold)

Second pass (10:18:18): pass. Main agent rewrote all per-env comments inline with self-contained WHY reasoning. Final comment ratios: 43/41/40/35/36/34%; WHY-depth: 86/93/73/82/82/64%. No content-fix subagent dispatched; iteration absorbed into main context (7 Edits across 6 yaml files + 1 root README retouch). 11 fails in one round is HIGH — but convergence was clean in a single retry, so cost was bounded.

**Close-step review** (`af3bc143…`): **1 CRITICAL + 0 WRONG + 3 STYLE**:

- **[CRITICAL]** workerdev/src/main.ts missing `process.on('SIGTERM', async () => { await app.close(); process.exit(0); })`. The workerdev README §Gotcha #4 **mandates** this handler in the published recipe deliverable, and workerdev/CLAUDE.md §"Draining the queue" documents how to verify it drains — but the feature subagent's scaffolded `main.ts` didn't implement it. Published content taught a requirement the code violated. Fixed inline: added SIGTERM handler, redeployed workerdev + workerstage (batch), stage feature sweep re-green 5/5. **Root cause**: v8.95 Fix #4 (scaffold pre-flight traps list in scaffold-subagent-brief) was not shipped; feature subagent had no hard rule to check "worker codebase MUST install SIGTERM handler before shipping `main.ts`". Writer-brief contains the MANDATORY gotcha framing AND a reference to a `{hostname}_worker_drain_code_block` check that was rolled back and no longer exists in the codebase — brief text is stale post-rollback.
- **[STYLE]** workerdev/src/main.ts:1 missing explicit `import 'reflect-metadata';` for parity with apidev; `@nestjs/core` pulls it in transitively but the canonical bootstrap pattern imports it first.
- **[STYLE]** workerdev/src/app.module.ts:13 `parseInt(process.env.DB_PORT, 10)` without `?? 5432` fallback → NaN on missing env; apidev uses safer `Number(process.env.DB_PORT ?? 5432)`.
- **[STYLE]** apidev/src/cache/cache.controller.ts:61 `redis.keys('cache:demo:*')` uses O(N) `KEYS` scan; `SCAN` stream preferred at scale. Skipped per reviewer judgement — showcase scale doesn't hit the problem. Plus unclosed `setTimeout` flash handles across ItemsCrud/CacheDemo/StorageUpload/JobsDispatch — runes-mode tolerates, but leaky. Skipped.

No silent-swallow issues; feature coverage clean 5/5; type-checks 0 errors on all three codebases; svelte-check 0 warnings on appdev.

**Minor hygiene regression — `.DS_Store` leaks**:

apidev + workerdev each ship 2 `.DS_Store` files in the published tree (apidev root + src/; workerdev root + src/). None of the three codebases have `.DS_Store` in `.gitignore` — v30 .gitignore contents: `node_modules\ndist\n.env\n*.log` (apidev/workerdev), `node_modules\ndist\n.env\n.vite/` (appdev). Same structural class as v21's 208 MB `node_modules` leak and v29's 2.8 KB `preship.sh` leak (scaffold `.gitignore` template drift; nothing in the scaffold brief enforces a standard `.gitignore` baseline), but smaller surface and lower reader-facing impact. The v8.95 plan's §5.1 `scaffold_artifact_leak` check would catch this; not shipped.

**Three-codebase coherence (v22's C+ limiter)**: no root README architecture section — same gap as v25/v28/v29. `recipe_architecture_narrative` check was rolled back per the v20-substrate calibration; user-visible gap remains "intentionally ungated editorial concern" per rollback rule.

**Notable**:

- **Fastest complete showcase run on record.** 64 min wall clock is v12's 61-min territory, but with full 3-codebase + separate worker + all 5 features + both browser walks + dev+stage cross-deploys — a content class v12 never approached. Attributable to: (a) main asst events down 26% vs v29 (281 vs 378), (b) feature subagent down to 8:47 wall vs v29's 14:58, (c) single-round deploy-content-fix loop, (d) no critical-fix subagent needed (main handled the 1 CRIT inline in ~6 min).
- **Agent-variance wins across v29 → v30 on the SAME code**: gotcha-origin 79% → 88%, DISCARD overrides 2/14 → 0/17, folk-doctrine 1 → 0, preship.sh-class leak YES → NO, IG items 5/4/3 → 3/3/3 (regression on this axis), close CRIT 0 → 1 (regression). Net content-quality direction is positive where the architecture holds (writer surface); neutral-to-regressive where the architecture doesn't reach (env READMEs, scaffold pre-flight).
- **v8.94 architecture is proven stable across two consecutive runs.** The content-authoring subagent's fresh-context + facts-log + taxonomy + self-review loop held at 79% and 88% respectively. The remaining defects are NOT writer-behavior defects — they are Go-source defects ([recipe_templates.go](../internal/workflow/recipe_templates.go)) and missing scaffold-preamble facts (SIGTERM). v8.95 Fixes A (hygiene check), B (Go-template edits), and #4 (scaffold pre-flight traps list) target exactly these. Fix C (ZCP_CONTENT_MANIFEST structured contract) is evidence-lowered by v30: at 0/17 voluntary DISCARD compliance the manifest is redundant over the plain-text brief taxonomy.
- **Writer-brief defect surfaced**: the v8.94 writer brief still references `{hostname}_worker_drain_code_block` as an "enforced check" for the SIGTERM gotcha, but this check was rolled back and doesn't exist in the codebase. This brief-to-check phantom reference is what made the SIGTERM case especially confusing — writer TAUGHT the MANDATORY rule while feature subagent lacked any pre-flight enforcement. Either (a) restore the check, (b) remove the brief reference, or (c) ship v8.95 Fix #4's scaffold pre-flight traps list so the feature subagent's own discipline catches it.
- **Env-comment surface continues to improve** — v30's env 4 import.yaml comments are v7-gold-standard with no contradictions (v25: app-static contradicted; v29: app-static contradicted again with "minContainers:2 on a static service is not needed" + `minContainers: 2` declaration). v30 env 4 L22-25 reads "Multiple static replicas are trivial because the bundle is immutable after build — the HA risk lives entirely in the API layer" — coherent with the `minContainers: 2` declaration below it.
- **10 `ToolSearch` calls on main** — unusual. These are deferred-tool-schema fetches (harness surfaces Claude Agent SDK's deferred-tool machinery in v8.94's main agent context). The Agent dispatch uses ToolSearch to load the schema for subagent-specific tools. Not a defect, but worth noting as a new cost-axis vs v28/v29 where these calls weren't separately measured.

**Rating**: S=**A** (all 6 steps, all 5 features wired + exercised on dev AND stage, both browser walks fired, 0 CRIT shipped after close fix), C=**B−** (88% genuine gotcha-origin is a new peak and 0/17 DISCARD overrides is a strict improvement on v29's 2/14 — both driven by agent-variance on unchanged v8.94 code; BUT 11 env-README factual-drift claims persist byte-for-byte from v29 (Go templates unchanged — v8.95 Fix B not shipped), env 0 cross-tier data-persistence fabrication recurs, 1 wrong-surface gotcha (appdev #5 VITE_* names), IG items regressed to 3/3/3 floor from v29's 5/4/3, `.DS_Store` hygiene leak in apidev + workerdev — content lifts from C+ to B− via writer-surface wins but is held back from B+ by unshipped Go-source fixes), O=**A** (**64 min wall = new all-time best for a complete run**, 4.6 min main bash ≤ 10, 0 very-long main, 0 MCP schema errors, 0 exit-255 classifications, 13 errored is tool-total not bash-specific — bash subset likely ≤ 10), W=**A−** (6 subagents with clean role separation including v8.94 fresh-context writer, real-time substep attestations in canonical order, v8.90 + v8.93 + v8.94 calibration bars all held, 8 `zerops_record_fact` at load-bearing cadence, 0 DISCARD overrides, single-round deploy-content-fix + single-round finalize-content-fix loops; writer-brief references a rolled-back check and scaffold pre-flight preamble missing → workerdev SIGTERM CRIT at close → docks from A to A−) → **B−** overall.

*v30 is the reproducibility run that validates v8.94's architecture is stable across agent-variance. The content-surface where the writer has authorship (per-codebase gotchas / IG / CLAUDE.md / env import.yaml comments) got strictly better vs v29: gotcha-origin 79% → 88%, DISCARD respected 0/17, 0 folk-doctrine, env-comment contradictions eliminated. The content-surface where the writer has no reach (env READMEs via Go templates; scaffold pre-flight discipline via recipe.md brief) stayed broken or re-broke in new ways. The v8.95 plan targets exactly those two surfaces — but the plan was documented without shipping. v30 post-mortem recommendation: ship v8.95 Fix A (`scaffold_artifact_leak` check), Fix B (Go-template in-place edits to `recipe_templates.go` lines 172/172/279/285/302/305/324/330/370 with regression tests pinning claims to YAML truth), and Fix #4 (scaffold pre-flight traps list in scaffold-subagent-brief including mandatory SIGTERM handler for worker codebases + mandatory `.DS_Store` entry in `.gitignore`). Defer Fix C (`ZCP_CONTENT_MANIFEST.json` structured DISCARD contract) based on v30 evidence that 0/17 voluntary compliance is achievable with plain-text brief framing — implementation cost outweighs marginal robustness gain at current agent-variance level. Expected v31 outcome: **B+ with 0 env-README factual drifts, 0 close CRIT from missing-handler class, gotcha-origin sustained ≥ 80%, wall ≤ 75 min.**

**v31 calibration bar (carried forward from v30's unshipped defects + sustained wins)**:

- 0 env-README factual-drift claims — shadow-grep env 0/3/4/5 READMEs against target-env YAML values (ground-truth union from `services[*].minContainers` and `envDiffFromPrevious(N)` adjacency)
- 0 cross-tier data-persistence fabrications — env 0 README MUST NOT claim data persists across tier promotions via hostname stability; correct semantics (`override-import` OR "new project = no data bridge") must be named if the topic is covered at all
- 0 scaffold-phase artifact leaks — no `preship.sh`/`.assert.sh`/`.DS_Store` in published tree; every codebase ships `.DS_Store` + `.env` + `node_modules` + `dist` in `.gitignore`
- 0 close-step CRITs from missing-mandatory-handler class — feature subagent's scaffold pre-flight trap list covers SIGTERM handler for worker codebases, `forcePathStyle: true` for S3 clients, `trust proxy` + `0.0.0.0` bind for Express/Fastify, `-*hostname*:-*hostname*` env self-shadow check
- Gotcha-origin ratio ≥ 80% genuine platform teaching (sustain v30's 88% without regression to v29's 79% floor)
- Writer DISCARD override rate = 0 — sustain v30's 0/17 without backsliding to v29's 2/14
- All v8.90 + v8.93 + v8.94 calibration items held (real-time substep attestations; 0 SUBAGENT_MISUSE; 0 zcp-side `cd /var/www/{host}`; fresh-context writer dispatched; facts log ≥ 5 entries)
- Wall clock ≤ 90 min (A-band preserved), main bash ≤ 10 min, 0 very-long, 0 MCP schema errors

---

### v31 — v8.95 ships; A− grade (highest since v20); two convergence loops surface as next structural limiter

- **Date**: 2026-04-19
- **Tier / shape**: Showcase Type 4, API-first dual-runtime + separate-codebase worker, 3-repo
- **Model**: `claude-opus-4-7[1m]`
- **Session**: `bedef9febfe99939`
- **Session logs**: `main-session.jsonl` + 6 subagent logs
- **Wall**: 11:23:17 → 12:49:21 = **86 min** (v30: 64, v28: 85; A-band preserved)
- **Assistant events**: ~280 main, **Tool calls**: ~720 total (213 Bash + 129 Read + 73 Write + 31 Edit + ~270 MCP + 17 ToolSearch/TodoWrite + 12 misc)
- **Bash metrics (main)**: 75 calls / **80.6s total (1.3 min)** / **0 very-long** / 0 long / **2 errored**
- **Bash metrics (subagents)**: scaffold-layer hits 2 very-long (71.6s on SUB-a36; 138.9s on SUB-a62) — expected pattern since v22 (scaffold `nest new scratch`); main path clean
- **Subagents** (6): scaffold×3 (apidev/appdev/workerdev, parallel 2:34-6:47) + feature×1 (10:34) + readmes/CLAUDE writer×1 (7:15, fresh-context v8.94) + code-review×1 (4:26 — both finds AND fixes)
- **MCP tool mix (main)**: 30 zerops_workflow, 23 zerops_dev_server, **17 zerops_knowledge (new peak)**, 8 zerops_record_fact (load-bearing; matches v30), 6 zerops_deploy, 5 zerops_guidance (low — v30: ~8), 3 zerops_deploy_batch, 3 zerops_browser, 4 zerops_subdomain, 1 each import/discover/env
- **Errored tool results (main)**: 8 total — 3 INVALID_PARAMETER (1 dev_server stop missing processMatch; 2 zerops_knowledge mode-mixing), 2 schema validation (unexpected properties `uri`, `base` on zerops_knowledge probes), 1 unread-file, 1 `psql: command not found` (Exit 127), 1 Cancelled-parallel-tool. **All 5 MCP-class errors are zerops_knowledge schema-churn at 12:09:47-12:10:05 during appstage 502 recovery** — the tool's error-messages don't prescribe the argument shape.

**Content metrics** (apidev / appdev / workerdev):
- README lines: **354 / 203 / 247** — new peak across the board (v30: 263/161/165)
- Gotchas: **3 / 3 / 3** — at floor; count dropped vs v30's 6/5/6 because writer consolidated into IG items
- IG items: **8 / 5 / 6** — apidev matches v22's all-time peak; total 19 IG items (v30: 9)
- CLAUDE.md bytes: **6298 / 4907 / 5044** (+1041 / +689 / +453 vs v30) — apidev new peak
- Root README intro: ✅ names real managed services (v17 `dbDriver` validation holds for **10th run**)
- Preprocessor directive: ✅ all 6 env import.yaml files carry `#zeropsPreprocessor=on`

**Gotcha-origin audit (single most important content metric)**: **9/9 = 100% genuine platform teaching** — new peak (v30: 88%, v29: 79%, v28: 33%). Zero folk-doctrine fabrications, zero wrong-surface items, zero self-inflicted content, zero library-meta shipped as content. Writer DISCARD override rate: **0/5** (2 library-meta + 1 self-inflicted NATS_PASS + 1 framework-quirk-kept-as-CLAUDE-operational + 1 scaffold-decision-kept-as-IG — all respecting taxonomy).

**Apidev gotchas (3/3 genuine)**: 0.0.0.0-bind-or-502 (pure invariant + failure mode), Valkey no-auth (pure invariant), self-shadow on project-level vars (pure invariant + concrete NATS_PASS incident).

**Appdev gotchas (3/3 genuine, 1 new discovery)**: `deployFiles: build/~` scalar-vs-list (**intersection, cited, new discovery this run** — Issue 3), Nginx SPA fallback 200+text/html masks misrouted `/api/*` (intersection, concrete "Unexpected token <" failure), static-vs-dynamic subdomain URL shape (pure invariant, NXDOMAIN symptom).

**Workerdev gotchas (3/3 genuine)**: NATS pub/sub fanout without queue group → N-way duplicates (intersection, concrete failure), SIGTERM drain (intersection with Zerops rolling-deploy + NestJS shutdown hooks), no-readiness-check-means-no-health-signal (pure invariant for portless workloads).

**v8.95 calibration bar — 4/5 met**:
| Item | Result |
|---|---|
| 0 env-README factual drifts | ✅ **v8.95 Fix B held** — env 4/5 READMEs correctly describe minContainers:2 from env 4, no "flips to 2" drift; env 3 README correctly gates readinessCheck on promotion |
| 0 cross-tier data-persistence fabrications | ✅ **v8.95 Fix B held** — env 0 README now correctly says *"Service state does NOT carry across tiers by default"* (v29/v30 had the opposite claim) |
| 0 scaffold-phase artifact leaks | ✅ **v8.95 Fix A held** — no `preship.sh`, no `.assert.sh` in published tree (scaffold subagents used `/tmp/zcp-preship-*.sh` correctly); `.DS_Store` files present are macOS artifacts from user's rsync, not in the published Linux tree |
| 0 close-step CRITs from missing-mandatory-handler class | ❌ **NEW subclass** — 1 CRIT: apidev/src/main.ts missing `app.enableShutdownHooks()` → JobsGateway's OnModuleDestroy NATS drain never fires on SIGTERM. workerdev SIGTERM WAS correctly implemented this run (v30 defect fixed); v8.95 Fix #4 scaffold pre-flight preamble covers worker-side only, not apidev-gateway-drain class |
| Gotcha-origin ratio ≥ 80% genuine | ✅ **100%** (new peak) |
| Writer DISCARD override rate = 0 | ✅ 0/5 |
| Wall ≤ 90 min, main bash ≤ 10 min, 0 very-long, 0 MCP schema errors | 4/5 — first three ✅ (86 / 1.3 / 0); 5 MCP schema errors ❌ (all zerops_knowledge argument-shape churn during appstage 502 recovery) |

**Runtime incidents during deploy (all self-healed)**:
1. **workerdev NATS_PASS vs NATS_PASSWORD env-name mismatch** (start-processes substep, ~2 min recovery) → `AUTHORIZATION_VIOLATION` on boot; fixed via rename in `worker.service.ts:24`; correctly classified as self-inflicted → DISCARDED by writer (not shipped as gotcha).
2. **appstage 502 from scalar `deployFiles: build/~`** (cross-deploy substep, ~4 min recovery including the zerops_knowledge schema-churn) → tilde-unwrap only fires for list items; scalar silently ignored with log warning; fixed via `- build/~` list form; shipped as genuine appdev gotcha with citation.
3. **Browser-tool syntax friction** on `find role button Create click` → not a recipe defect.

**Close-step review** (SUB-a34, 4:26 wall): **1 CRIT + 2 WRONG + 2 STYLE** — all CRIT+WRONG fixed inline:
- [CRITICAL] apidev missing `app.enableShutdownHooks()` — code-review subagent BOTH found AND fixed (v22 collapsed pattern).
- [WRONG] Scaffold-leftover e2e tests (`apidev/test/app.e2e-spec.ts`, `workerdev/test/app.e2e-spec.ts` — both GET / expecting "Hello World!") → deleted.
- [WRONG] `data-feature="status"` on StatusPanel not in plan.Features → REPORTED (kept; scaffold's green-dot baseline).
- [STYLE×2] jobs.gateway onModuleInit swallows NATS connect error; items.service Meilisearch sync best-effort.

**Phase wall clocks** (timeline.py):
| Phase | Wall | Notes |
|---|---:|---|
| START | 0:40 | |
| RESEARCH | 2:57 | |
| PROVISION (label artifact) | 11:06 | timeline.py labels AFTER complete-call; actual provision ~4 min, scaffold-overlap consumes rest |
| GENERATE | 5:32 | |
| **DEPLOY** | **47:56** | 56% of wall; dominated by feature-subagent block (11:00), writer-subagent block (8:47), 3-round README checks, 2-round appstage fix |
| FINALIZE | 13:17 | **3 rounds** of env-comment rewriting |
| CLOSE | 4:24 | |

**Top convergence cost drivers (v31 → v8.96 targets)**:

1. **Deploy-complete 3-round loop (~2 min)**: first `complete step=deploy` at 12:25:19 fails on 4 README checks (3× `fragment_intro_blank_after_marker`, 2× `app_gotcha_distinct_from_guide`, `comment_ratio`, `app_knowledge_base_authenticity` at floor). Main fixes inline. Second attempt fails AGAIN on `comment_ratio` — because **the check reads embedded YAML INSIDE apidev/README.md's integration-guide fragment, not the on-disk `zerops.yaml`** the agent had edited. Main re-synced embedded yaml, third attempt passed. **Structural defect**: check's read surface is opaque to the author until a failure round teaches it. Same class as v23's 5-round loop, compressed but not eliminated.

2. **Finalize 3-round loop (~4 min)**: first `generate-finalize` at 12:31:40 → first `complete step=finalize` fails on ~11 env-check violations (`comment_ratio` <30% on 4 envs, `cross_env_refs` on 3 envs, `factual_claims` on 2 envs, `comment_depth` <35% on 1 env). Main rewrites all 6 env yamls. Second attempt still fails. Third attempt passes. Same structural class as #1 — author has prose guidance about env-comment rules but no way to run the check before attesting.

3. **Cross-subagent duplicate archaeology (~80s cumulative)**: scaffold subagent (SUB-a62, ~11:33) spent ~20s grepping Meilisearch v0.57 type defs to discover `EnqueuedTaskPromise` + `waitTask` shape. Feature subagent (SUB-aa9, ~11:49) re-investigated the same API 20 min later (~15s). Code-review subagent (SUB-a34, ~12:37) re-investigated svelte-check@4 vs typescript@6 version mismatch the feature subagent already discovered (~45s). None of these facts ship as content (writer correctly classifies as library-meta/tooling-meta and discards) — but each downstream subagent re-discovers because the facts log is writer-only.

4. **`zerops_knowledge` schema-churn (~15s, 5 errors)** — tool-ergonomics concern; small cost but recurrent pattern.

5. **`psql` command not found** (~1 round-trip) — dev container doesn't ship postgresql-client; agent pivoted to `node -e 'const {Client}=require("pg")...'` correctly on next try.

**Other timeline observations**:
- 7 "File has not been read yet" errors across scaffold subagents (Write-before-Read on `npx @nestjs/cli new`-created files; ~18s cumulative).
- Duplicate `zerops_dev_server action=stop workerdev` at 11:47:08 + 11:47:10 (minor).
- Feature subagent port-kill dance after `dev_server stop` (~6s belt-and-suspenders redundant with tool's post-condition).
- Git-lock transient on `.git/config.lock` during close-step batch redeploy (~90s, SSHFS parallel git ops).

**Rating**: S=**A** (all 6 steps, all 5 features on dev+stage, both browser walks fired, 0 CRIT shipped after close-step fix), C=**A−** (100% genuine gotcha-origin is new peak, 0 env-README drifts, 0 fabrications, largest total published content ever counting CLAUDE.md; gotcha count at floor 3/3/3 but information absorbed into 19 IG items; minor: env 5 README frames queue-group as "Workers add" when already present at env 4 YAML), O=**A** (86 min ≤ 90, 1.3 min main bash ≤ 10, 0 very-long main, scaffold hygiene clean), W=**A−** (5 MCP/schema missteps on zerops_knowledge; close CRIT on apidev `enableShutdownHooks()` is a NEW class not in scaffold pre-flight preamble — should be bundled with v8.97) → **A− overall** — **highest grade since v20's A** (tied with v25's nominal A before workflow-discipline dock).

*v31 is the v8.95 validation run. All three shipped fixes held: scaffold-artifact-leak check prevented preship.sh recurrence, Go-template edits eliminated env-README factual drift byte-for-byte, ZCP_CONTENT_MANIFEST writer-contract delivered (2263B manifest at `/var/www/ZCP_CONTENT_MANIFEST.json`, correctly outside the published recipe tree). Content quality reached its highest mark on record (100% gotcha-origin + new README-line peaks on all three codebases + largest CLAUDE.md across the board). The remaining ~6 min of wall-time waste concentrates in TWO structural convergence loops — deploy-complete README checks (3 rounds) and generate-finalize env checks (3 rounds) — PLUS ~80s in cross-subagent duplicate archaeology. Root cause analysis traces both loops to the same asymmetry: **checks know what they read, what they couple, and what to change; authors only know what to write**. v8.96 post-mortem + plan at [docs/implementation-v8.96-author-side-convergence.md](implementation-v8.96-author-side-convergence.md). Two structural changes (no hardcoding, no new tool surfaces): (A) extend `StepCheck` with `ReadSurface`/`Required`/`Actual`/`CoupledWith`/`HowToFix` so every gate emits its own self-describing failure, collapsing both loops to 1-round convergence; (B) add `Scope` field to `zerops_record_fact` + hook `BuildPriorDiscoveriesBlock` at topic-resolution time in `buildSubStepGuide`, routing scaffold-discovered framework quirks into feature + code-review subagent briefs. Expected v32 outcome: **A or A+; wall ~66-70 min; 1 fail round on deploy + finalize; 0 duplicate framework archaeology; scope=downstream adoption ≥2 facts (calibration bar #7)**.

**v32 calibration bar (carried forward from v31's convergence defects)**:

- Deploy-complete README checks converge in ≤1 fail round (v31: 2; v32 target: 1)
- Finalize-complete converges in ≤1 fail round (v31: 2; v32 target: 1)
- Zero duplicate framework archaeology across subagents — feature subagent spends ≤5s on API shape that scaffold's facts-log already names
- ≥50% of P0 check failures emit non-empty `ReadSurface` + `Required` + `Actual` + `HowToFix` (v8.96 migration coverage)
- `NextRoundPrediction` heuristic correlates with actual round count — `single-round-fix-expected` predictions converge in 1 fail round ≥80% of the time
- ≥1 subagent brief contains a non-empty "Prior discoveries" block AND the receiving subagent demonstrably does NOT re-investigate the recorded fact
- ≥2 facts recorded with `Scope="downstream"` during scaffold/feature subagents — if 0, v8.96 Theme B adoption is UNTESTED regardless of other wins (adoption-gap calibration)
- 0 close-step CRITs from the missing-mandatory-handler class — v8.97 concern if not bundled with v8.96 docs: scaffold pre-flight preamble extended to cover apidev `enableShutdownHooks()` when NATS gateway is present (generalizable rule: any OnModuleDestroy-bearing provider requires `enableShutdownHooks()` at bootstrap)
- Gotcha-origin ratio ≥ 80% genuine (sustain v31's 100% without regression); writer DISCARD override rate = 0
- All v8.90 + v8.93 + v8.94 + v8.95 calibration items held
- Wall ≤ 90 min, main bash ≤ 10 min, 0 very-long main, ≤2 MCP schema errors (v31: 5 on zerops_knowledge — document in v8.97 if persistent)

---

#### v8.96 — what shipped (post-v31, pre-v32)

The v8.96 release bundles **two structural themes from the original plan + four quality fixes added during implementation review** (the fixes were re-scoped from "ergonomic noise" to "trace pollution / false content / partial scaffolding" — quality vectors, not wall-time concerns).

**Theme A — structured, self-describing check failures** ([`internal/workflow/bootstrap_checks.go`](../internal/workflow/bootstrap_checks.go) + 5 check files):

- `StepCheck` extended with optional `ReadSurface`, `Required`, `Actual`, `CoupledWith`, `HowToFix`, `Probe` fields — every failing P0+P1 check now names the file/region it actually inspected, the threshold, the observed value, the coupled files that must stay in sync, and a 1-3 sentence imperative remedy.
- `StepCheckResult.NextRoundPrediction` set by `AnnotateNextRoundPrediction` heuristic: `single-round-fix-expected` / `coupled-surfaces-require-sequencing` / `multi-round-likely`.
- P0 migrations: deploy `comment_ratio` (host-aware via new `hostname` param + `CoupledWith: [{host}/zerops.yaml]`), `*_import_comment_ratio` / `_comment_depth` / `_cross_env_refs` / `_factual_claims` (the last refactored to emit one `StepCheck` per mismatch with line numbers).
- P1 migrations: `knowledge_base_authenticity`, `gotcha_distinct_from_guide`, `fragment_*_blank_after_marker`.
- All three recipe checkers call `AnnotateNextRoundPrediction` before returning.

**Theme B — facts log as cross-subagent message bus** ([`internal/workflow/recipe_brief_facts.go`](../internal/workflow/recipe_brief_facts.go) NEW):

- `FactRecord.Scope` ∈ {`""`, `content`, `downstream`, `both`} with `knownScopes` enum guard against typos like `"downsteam"` (would silently default to content-scope without).
- `BuildPriorDiscoveriesBlock(sessionID, currentSubstep)` reads the session facts log, filters Scope ∈ {downstream, both}, drops facts recorded at substeps DOWNSTREAM of the current dispatch (forward-in-time leak prevention), sorts newest-first, caps at 8 entries with elision footer.
- `GuidanceTopic.IncludePriorDiscoveries` opt-in: `subagent-brief` (feature dispatch) and `code-review-agent` flagged true; scaffold + readmes-writer remain false.
- `sessionID` threaded through `BuildResponse → buildGuide → buildSubStepGuide` (3 method signatures, additive — existing tests updated to pass `""`).
- Writer-side `filterContentScoped` drops `Scope=downstream` from `checkManifestCompleteness` so the writer's manifest stays content-lane-only.

**Quality Fix #1 — `zerops_knowledge` schema mode-clarity** ([`internal/tools/knowledge.go`](../internal/tools/knowledge.go)):

v31 had 5 schema-churn errors during the appstage 502 recovery — agent kept passing two-mode combos (query+recipe, runtime+scope) and learning the rules from rejection messages. Tool description rewritten as a 4-line decision tree (≤60 words; contains `briefing/scope/query/recipe` keywords). Each `jsonschema:` parameter description leads with `MODE N:` so the taxonomy is recoverable from any single field. Rejection messages now NAME the conflicting modes (e.g. `"MODE 1 query= + MODE 2 runtime="`) and end with the positive remediation form. **Expected v32 outcome: 0 zerops_knowledge mode-mixing rejections in main-agent trace** (v31: 2 rejections + 2 schema-validation errors + 1 unknown-scope = 5 total).

**Quality Fix #2 — `dev_server stop` verifies port-free post-kill** ([`internal/ops/dev_server_lifecycle.go`](../internal/ops/dev_server_lifecycle.go)):

v31 had a feature-subagent port-kill dance (~6s) — `dev_server stop` returned immediately after `pkill`+`fuser`, racing the OS reaper, so the next `dev_server start` hit "address already in use" and the agent improvised pgrep+pkill+sleep workarounds. Worst-case quality risk: such a workaround can ship as a "Zerops requires manual port-kill" gotcha in the published README. New behavior: post-kill, polls `ss -tnlp 'sport = :PORT'` for up to 1.5s (covers SO_REUSEADDR linger). If still bound, escalates to `fuser -k -KILL`, polls another 0.8s, and on persistent failure surfaces `Reason=port_still_bound` with an explicit message: *"do NOT add a manual pkill workaround to the recipe — port-stop is the platform's responsibility, not the recipe's."* **Expected v32 outcome: 0 false port-kill gotchas in published content; 0 "address in use" errors in feature-subagent trace immediately after a `dev_server stop` call.**

**Quality Fix #3 — Read-before-Edit sequencing rule in all 5 subagent briefs** ([`internal/content/workflows/recipe.md`](../internal/content/workflows/recipe.md), 5 occurrences):

v31 had ~7 "File has not been read yet" errors across scaffold subagents (~18s cumulative). The deeper concern: the agent learns defensive over-reading from those errors and bloats subsequent calls. New rule (added to scaffold-subagent-brief, dev-deploy-subagent-brief, readme-with-fragments, content-authoring-brief, code-review-subagent): *"every Edit must be preceded by Read of the same file in this session — Edit tool enforces this; reactive Read+retry is trace pollution that trains you into defensive over-reading. Plan up front: before your first Edit, batch-Read every file you intend to modify."* Tailored per-brief: scaffold gets a framework-scaffolder note, writer gets a "Write-from-scratch is your default" note, code-review gets a "Read-heavy by nature" note. **Expected v32 outcome: 0 "File has not been read yet" errors in any subagent trace; subagent's first Edit is always preceded by a planned Read.**

**Quality Fix #4 — git is container-side via SSH for ANY agent; framework scaffolders must `--skip-git` or delete `.git/`** ([`internal/content/workflows/recipe.md`](../internal/content/workflows/recipe.md), `git-config-mount` block + scaffold-subagent-brief):

v31 had ~90s git-lock contention on `.git/config.lock` during close-step batch redeploy. Root cause (per user correction): there is **no main-agent ownership** of git in zcp recipes — the rule is about WHERE git runs, not WHO calls it. Every git operation, by ANY agent (main, scaffold subagent, feature subagent, code-review subagent), runs on the dev container via SSH. Lock contention happens when framework scaffolders (`nest new`, `npm create vite`, etc.) auto-`git init` and leave a partial `.git/` that collides with the canonical container-side `git init` later. New rule (added to scaffold-subagent-brief): pass the scaffolder's `--skip-git` flag if available, OR `ssh {hostname} "rm -rf /var/www/.git"` immediately after the scaffolder's SSH call returns. New pre-ship Assertion 10 fails if `/var/www/.git` exists at scaffold-return time. The `git-config-mount` block was rewritten to lead with the where-not-who framing + a concurrency-on-single-mount paragraph naming `.git/index.lock` enforcement explicitly. **Expected v32 outcome: 0 `.git/index.lock` or `.git/config.lock` contention errors during any phase; 0 framework-scaffolder `.git/` residues at generate-complete time.**

**v32 expected impact summary** (combined with Themes A+B):

| Cost driver | v31 actual | v32 target | Mechanism |
|---|---:|---:|---|
| Deploy README check rounds | 3 rounds (~2 min) | 1 round | Theme A — `ReadSurface` names embedded YAML in apidev/README.md, `CoupledWith` names apidev/zerops.yaml, `HowToFix` says "edit both, keep byte-identical" |
| Finalize env-comment rounds | 3 rounds (~4 min) | 1 round | Theme A — per-mismatch `factual_claims` with line numbers, per-env `comment_ratio`/`_depth`/`_cross_refs` with concrete remedies |
| Cross-subagent archaeology | ~80s | ≤5s | Theme B — `Scope=downstream` facts surface in feature + code-review briefs as "Prior discoveries" |
| zerops_knowledge schema-churn | 5 errors / ~15s | 0–1 | Fix #1 — decision-tree description + named-mode rejections |
| dev_server stop → restart race | ~6s + risk | 0s + 0 false gotchas | Fix #2 — post-kill `ss -tnlp` port-free poll + SIGKILL escalation |
| "File has not been read yet" | 7 errors / ~18s | 0 | Fix #3 — explicit Read-before-Edit rule in all 5 subagent briefs |
| Git-lock SSHFS contention | ~90s | 0s | Fix #4 — framework scaffolders `--skip-git` or delete `.git/`; pre-ship Assertion 10 enforces |

**Estimated v32 wall-time delta**: v31 86 min → v32 **70–76 min** (−10 to −16 min). Combined grade target: **A or A+** (first A+ on record). Quality ceiling raise: **0 false gotchas attributable to tool quirks** (Fix #2), **0 trace-pollution from defensive patterns** (Fix #3), **0 partial-scaffolding from git collisions** (Fix #4), **0 wrong-mode-rejection cascades** (Fix #1).

**The most important v32 measurement is bar #7 — Scope=downstream adoption.** All four quality fixes ship as code or prompt; they cannot fail to take effect. Themes A+B are also code-level, so the diagnostic fields and prior-discoveries block will appear in v32 traces deterministically. But `Scope=downstream` requires the agent to voluntarily set the field on framework-quirk facts. If 0 facts are recorded with `scope: "downstream"` in v32, Theme B's real-world adoption is UNTESTED — the structural mechanism works (verified by tests) but the agent didn't reach for it. That's the single load-bearing unknown going into v32.

---

### v32 — v8.96 validation (reconstructed from commits + TIMELINE); close step never completed, per-codebase content missing from deliverable

- **Date**: 2026-04-19
- **Tier / shape**: Showcase Type 4, dual-runtime + separate-codebase worker
- **Model**: `claude-opus-4-7[1m]`
- **Code state**: v8.96 (Themes A+B + 4 quality fixes)
- **Session**: `bbf7de074756c9f7`
- **Session logs**: `main-session.jsonl` + 5 subagent logs
- **Wall**: 15:43:40 → 16:53:42 = **70 min 2s** (hit v8.96 target band 66–76 min)
- **Assistant events (main)**: 296
- **Bash metrics (main)**: 49 calls / **13.4s (0.2 min)** / 0 very-long / 0 long / 13 errored (mostly workflow-ordering rejections)
- **Subagents (5)**: scaffold×3 + feature + writer + code-review (no critical-fix dispatch)
- **MCP tool mix (main)**: 25 `zerops_workflow`, 19 `zerops_dev_server`, 10 `zerops_knowledge`, **10 `zerops_record_fact`** (load-bearing cadence), 9 `zerops_browser`, 5 Agent, 3 deploy + 2 deploy_batch, 9 ToolSearch, 28 Edit, 15 TodoWrite. 23 is_error tool_results total — high.

**Reconstructed narrative** (TIMELINE.md + v8.97+v8.98 commit message evidence):

v32 was the v8.96 validation run. Hit the operational target band. v8.96 Themes A+B shipped as code so their mechanisms fired deterministically. But the run surfaced **five structural defects** that became v8.97+v8.98 scope:

1. **Close step never completed.** TIMELINE narrates up through stage feature-sweep then stops with *"Close (pending) — Static code review sub-agent + optional publish still to run"*. `zcp sync recipe export` ran anyway — no close-gate existed on export in v8.96. v8.97 Fix 1 (`ExportRecipe` refuses when `step=close` not complete) is the direct response.
2. **Per-codebase READMEs + CLAUDE.md missing from the exported deliverable.** TIMELINE records the writer sub-agent produced apidev 303 / appdev 162 / workerdev 218 lines + 3 CLAUDE.md operational guides. `find` on the exported tree returns ONLY `/var/www/zcprecipator/nestjs-showcase/README.md` (root) + 6 env `README.md` files + a spurious `environments/README.md` (byte-identical duplicate of root, 2359 bytes). The writer's per-codebase files lived on the SSHFS mount but never landed in the canonical tree because `OverlayRealREADMEs` fires at close-complete, which never happened. v8.97 Fix 2 (close verify-only sub-steps + publish out of workflow state) fixed the ordering.
3. **Read-before-Edit rule lost across 3 scaffold subagents.** recipe.md:845 literally calls this out: *"v32 lost the Read-before-Edit rule across three scaffold subagents."* Main-agent dispatch compression dropped the load-bearing rule when it synthesized each brief. v8.97 Fix 3 A+B wrapped file-op sequencing, tool policy, and SSH-only executables in MANDATORY-sentinel blocks with byte-identical-transmission rules — this is why v33 had 0 "File has not been read yet" errors.
4. **`execOnce` key burned on workspace-create no-op deploy.** Apidev seed returned ✅ in 66ms with 0 rows inserted on the first code-bearing deploy. The workspace-create no-op deploy had already burned the `${appVersionId}` key (same class as v23/v28). Agent manually re-ran `npx ts-node src/seed.ts` via SSH + documented as porter-facing gotcha. Root cause is the seed key-shape (see v8.104 Fix B — static `bootstrap-seed-v1` key) — not fixed in v8.97.
5. **Deploy content-fix: 3 rounds**; **finalize: 2 rounds**. Theme A target was 1 round each; coupling-table was hand-maintained and incomplete. v8.97 Fix 4 replaced it with `StampCoupling` (surface-derived: every failed check with populated `ReadSurface` gets `CoupledWith` stamped from shared-surface graph).

**Plus two principle-violation defects** the scaffold+feature path shipped:

- TypeORM entity `@Entity({name:'job'})` mismatch with API migration `jobs` — silently produced 0-row updates. Feature subagent caught mid-run and shipped as workerdev gotcha with correct platform-invariant framing. v8.97 Fix 5 added six platform principles (graceful shutdown, routable bind, proxy trust, competing-consumer, structured creds, stripped build root) to scaffold MANDATORY block.
- `agent-browser` synthetic-click on Svelte 5 `onclick` didn't fire the handler. Pipeline verified via curl (276ms round-trip). Documented as platform-observation fact — not reproducible as a porter-facing gotcha.

**v8.96 Theme B Scope=downstream — validated.** TIMELINE reports 2 downstream-scope facts recorded during feature subagent execution. First run to exercise the mechanism.

**v8.97+v8.98 response shipped as a80d337**: close verify-only (Fix 2), MANDATORY sentinels + byte-identical-transmission rule (Fix 3 A+B), StampCoupling (Fix 4), six platform principles (Fix 5), feature-subagent mirrored MANDATORY (v8.98 Fix A), export at NextSteps[0] autonomous / publish at NextSteps[1] user-gated (v8.98 Fix B — later reverted by v8.103 after v33 showed auto-export was user-objectionable), close sub-step ordering gate (v8.98 Fix C).

**Rating**: S=**C** (close never completed; per-codebase content missing from deliverable = v10-class catastrophe), C=**C** (writer produced content but it didn't ship in the exported tree; env READMEs + env YAMLs correct; spurious `environments/README.md` duplicate of root), O=**A−** (70 min wall hit target, 13.4s main bash = new low for v30-era, 0 very-long, 0 schema errors), W=**D** (close never completed, premature export, dispatch-compression dropped load-bearing rules across 3 scaffold subagents, 13 workflow-ordering rejections) → **C overall**.

*Note on reconstruction: v32 was not fully analyzed at session-log level (user directed "check the commits what we changed since v31 to figure out what went wrong" — no deep forensics). The defect enumeration above is cross-referenced from (a) v32's published TIMELINE.md, (b) v33 v8.102.0 binary running commits `b65a282` compound = v8.96 + v8.97 + v8.98 + v8.99 + v8.100, (c) the v8.97+v8.98 commit message explicitly naming which defects each fix closes, and (d) recipe.md:845's literal reference to v32. Gotcha/IG counts in the metrics tables are daggered (†) to note they come from TIMELINE narrative rather than reading the shipped deliverable.*

---

### v33 — v8.102.0 validation; peak content body, phantom output tree shipped, v8.104 guidance-hardening plan

- **Date**: 2026-04-20
- **Tier / shape**: Showcase Type 4, dual-runtime + separate-codebase worker
- **Model**: `claude-opus-4-7[1m]`
- **Code state**: v8.102.0 (commit `b65a282` = v8.96 + v8.97 + v8.98 + v8.99 + v8.100 compound)
- **Session**: `b03f3ac88ddf3a10`
- **Session logs**: `main-session.jsonl` (321 events) + 6 subagent logs
- **Wall**: 07:04:18 → 08:32:47 = **88 min 29s** recipe work (canonical close) + ~21 min post-close export noise (ended 08:54:03)
- **Assistant events (main)**: 321
- **Tool calls (main)**: ~693
- **Bash metrics (main)**: 40 calls / **30.1s (0.5 min) — new all-time best** (prior v25: 30.9s, v31: 78s) / 0 very-long / 0 long / **4 errored** (1 real `fatal: not a git repository` + 2 cascade + 1 shell exit 2)
- **Subagents (6)**: scaffold appdev (2:18) + apidev (6:41, Meili-ESM fix) + workerdev (3:35) + feature (18:37, includes ~9 min diagnostic burst) + readmes/CLAUDE writer (8:34) + code-review (2:54). No critical-fix dispatch.
- **MCP tool mix (main)**: 27 `zerops_workflow`, 26 `zerops_dev_server`, **21 `zerops_record_fact`** (new peak; v31: 8), 9 `zerops_guidance`, 8 `zerops_knowledge`, 6 Agent, 6 `zerops_browser`, 4 verify, 4 subdomain, 3 mount + 3 logs, 3 deploy + 4 deploy_batch, 24 Edit, 20 TodoWrite, 14 ToolSearch.

**Content metrics** (apidev / appdev / workerdev):

- README lines: 320 / 199 / 191 (bytes 16,858 / 10,035 / 10,650)
- Gotchas: **8 / 5 / 5** (total 18; v31: 3/3/3 total 9)
- IG items: 6 / 5 / 7 (total 18; v31 peak 19)
- CLAUDE.md: 3344 / 2788 / 3822 bytes — all clear 1200 floor with 3+ custom sections each
- Root README intro names real services — v17 `dbDriver` holds for **11th** consecutive run
- Preprocessor directive: all 6 envs

**Gotcha-origin audit: 18/18 = 100% genuine platform teaching** (matches v31 peak). Every gotcha names platform mechanism + concrete failure mode + exact error string where applicable. Zero folk-doctrine. Zero wrong-surface. Zero library-meta as gotcha.

**Env 4 + env 5 YAML comments**: v7-gold standard. Two-axis `minContainers` teaching intact (app static explicitly "NOT throughput scaling, pure availability play"; api both-axes; worker queue-group linear throughput). No self-contradictions. v29's app-static contradiction class did NOT recur.

**v8.95 Fix B still holding**: env 0 README correctly says *"Service state (DB rows, cache entries, stored files) does NOT carry across tiers by default"* (v29/v30 had the opposite fabrication). Zero Go-template env-README drifts.

**Close review**: 0 CRIT / 2 WRONG / 1 STYLE — scaffold e2e test expecting HTTP on no-HTTP worker + unused Express deps + StatusPanel `data-feature="status"` namespace overload (renamed to `data-panel="status"`). All fixed inline. v31's apidev `enableShutdownHooks()` CRIT class did NOT recur.

**v8.96 calibration bars** (9 tracked):

| Bar | Result |
|---|---|
| Deploy-complete README checks converge ≤1 round | ❌ 3 rounds (target was 1) |
| Finalize-complete ≤1 round | ❌ 2 rounds |
| Zero duplicate framework archaeology across subagents | Partial — not deep-audited |
| ≥50% P0 failures emit non-empty `ReadSurface` + `Required` + `Actual` + `HowToFix` | ✅ Theme A emitting structured fields (confirmed via payload parse: 32 `readSurface` + 46 `howToFix` + 8 `nextRoundPrediction` occurrences) |
| `NextRoundPrediction` correlates with round count | ⚠ Mixed: 4 `multi-round-likely` + 2 `single-round-fix-expected`; actual was 3 rounds deploy |
| ≥1 subagent brief contains "Prior discoveries" block + no re-investigation | ✅ Hooks present; re-investigation not deep-audited |
| **≥2 facts recorded with `Scope="downstream"`** | **✅ 3 facts** (1 main, 1 apidev-scaffold, 1 feature subagent) — **first run to validate Theme B** |
| 0 close-step CRITs from missing-mandatory-handler class | ✅ v31's `enableShutdownHooks` class did not recur |
| Gotcha-origin ≥ 80% genuine / DISCARD override rate = 0 | ✅ 100% / 0 |
| Wall ≤ 90, main bash ≤ 10 min, 0 very-long, ≤2 MCP schema errors | ✅ all — 88 min / 30.1s / 0 / **0** (v31: 5 — Fix #1 held) |

**Six defects that shipped to `/var/www/`** (not the canonical `zcprecipator/` tree — visible to user on the container filesystem):

1. **Phantom `/var/www/recipe-nestjs-showcase/` tree with paraphrased env folder names.** Writer sub-agent wrote 7 orphan files: `README.md` + `environments/{0 — Development with agent, 1 — Development, 2 — Review, 3 — Stage, 4 — Small production, 5 — HA production}/README.md`. Those strings appear NOWHERE in recipe.md. Main-agent synthesized them when constructing the writer dispatch prompt — inspection of the dispatch text shows main literally wrote *"Output root for env READMEs + root README: /var/www/recipe-nestjs-showcase/"* at offset 2161 + enumerated the paraphrased names as if canonical. Writer followed faithfully. Canonical tree at `/var/www/zcprecipator/nestjs-showcase/` was written by `BuildFinalizeOutput` Go templates with correct names — both trees coexisted. Code-review sub-agent saw the phantom tree, said *"out of scope for code review"*, moved on. v8.103 added `TestCloseSection_SingleCanonicalOutputLocation`; v8.104 Fix A adds MANDATORY-sentinel inside writer brief forbidding "output root" + "env folder names" as parameters.
2. **Auto-export at close.** v8.98 Fix B framed export as autonomous `NextSteps[0]`. Main ran three `zcp sync recipe export` invocations immediately after `complete step=close` at 08:51:22 → 08:54:03. User objected to ANYTHING unprompted after close. v8.103 reverts: both export AND publish now "ON REQUEST ONLY".
3. **Feature sub-agent ~9-min diagnostic panic burst (07:29:00 → 07:38:16).** First 4 probes succeeded (`ssh apidev "cat node_modules/.../package.json"`). At 07:29:14 pivoted to ~80 parallel bash probes testing whether basic shell + SSH still worked (`ls`, `stat`, `echo HELLOWORLD`, `ssh -vvv apidev 'echo ok'`, `find / -name config`). Harness concurrency queue saturated; latencies on trivial commands hit 300+ seconds. No host-confusion, no tool broke — agent pattern-matched on "this looks weird" and probed until convinced. Productive feature work started at 07:38:16. v8.104 Fix D adds diagnostic-probe cadence rule to feature-subagent MANDATORY.
4. **Unicode box-drawing separators in zerops.yaml comments** (`# ──────────────────────────────────────────────────────────────`). All three codebases shipped with these dividers. Pure agent stylistic invention — v20-v32 all used plain `# Comment text`. recipe.md:2914 covers comment voice but not visual decoration. v8.104 Fix C adds "Visual style" subsection forbidding Unicode box-drawing, ASCII dividers, emoji.
5. **3-round deploy content-fix + 2-round finalize.** Round 1 deploy (08:10:03) failed on 3× `fragment_intro_blank_after_marker`, `app_comment_specificity` 24%, `api_comment_specificity` 14%, `api_comment_ratio` 29%, `app_integration_guide_per_item_code`, 3× `gotcha_distinct_from_guide`, `cross_readme_gotcha_uniqueness`. Round 2 (08:16:47) still failed on one NEW `cross_readme_gotcha_uniqueness` Jaccard collision (rewording round-1 gotchas created similarity to sibling READMEs). Round 3 (08:17:11) passed. Theme A names what to fix on THIS surface but not which SIBLING checks will newly fail. v8.104 Fix E adds `PerturbsChecks []string` to `StepCheck`.
6. **Pre-init `git commit` sequencing gap.** At 07:19:31 main ran `ssh appdev "git add -A && git commit -q -m 'initial scaffold'"` → `fatal: not a git repository`. Scaffold subagents had `rm -rf /var/www/.git/` per v8.96 Fix #4 (framework-scaffolders' auto-`git init` collides with main's canonical init). Main recovered with `git init -q -b main && git add -A && git commit` 6s later. recipe.md block `git-config-mount` instructs pre-scaffold init but doesn't carry the post-scaffold "re-init before commit" note. v8.104 Fix F adds explicit sequencing.

**Plus the recipe-pattern defect in every recipe since v20**: `zsc execOnce ${appVersionId} --retryUntilSuccessful -- npx ts-node src/seed.ts`. `${appVersionId}` changes every deploy, so seed runs every deploy; only the in-script `if (count > 0) return` short-circuit prevents duplicate rows. That short-circuit is the **literal cause** of v33 apidev gotcha #7 — `GET /api/search returns 500 Index 'items' not found on the second deploy` — because the Meili `addDocuments(...)` call lives INSIDE the skipped branch. We're documenting a bug in our own recipe pattern as a user-facing gotcha. v8.104 Fix B prescribes `zsc execOnce bootstrap-seed-v1 -- seed` (static key, once per service lifetime) separately from `zsc execOnce ${appVersionId} -- migrate` (per-deploy, idempotent by design). Eliminates gotcha #7 at the source.

**Notable**:

- **Operationally cleanest run on record.** Main bash 30.1s obliterates v25's 30.9s prior best. 0 very-long, 0 MCP schema errors (v31: 5), 0 `.git/index.lock` contention (v31: ~90s), 0 "File has not been read yet" errors (v31: 7), 0 SUBAGENT_MISUSE. All v8.96 quality fixes held structurally.
- **Content body at peak quality.** 100% gotcha-origin + env 4+5 v7-gold + clean CLAUDE.md + correct env-0 cross-tier semantics + correct env-README minContainers claims (v8.95 Fix B intact across 2 consecutive runs).
- **But the phantom tree + auto-export + box-separator invention + diagnostic-storm + 3-round convergence + pre-init git = five distinct classes of agent invention that filled guidance gaps.** Every single one is documented in v8.104 guidance-hardening plan as a named fix.
- Meilisearch v0.47+ ESM-only build failure discovered by apidev scaffold at 07:15:10 (build error TS1479), pinned to `meilisearch@0.45.0` with class `MeiliSearch` (capital S), recorded as `fix_applied` fact. Shipped correctly in apidev CLAUDE.md Container Traps section.
- Feature subagent recorded **12 structured facts during execution** + 3 main-agent facts during deploy + 6 from other subagents = 21 total (new peak; v31: 8).

**Rating**: S=**A** (all 6 steps, all 5 features on dev+stage, both browser walks, 0 CRIT shipped after close-step fix), C=**B+** (100% gotcha-origin is peak, env 4+5 YAML v7-gold, no folk-doctrine, no drifts — BUT phantom tree + Unicode box-separator + auto-export all ship visible to user; drops from A− to B+), O=**A** (88 min ≤ 90, 30.1s main bash all-time best, 0 very-long, 0 MCP schema errors, 4 is_error mostly 1 real + 3 cascade), W=**B** (writer dispatch hallucinated paths + env names, 9-min feature-subagent diagnostic burst, 3-round deploy + 2-round finalize, pre-init git sequencing, auto-export per v8.98 framing) → **B overall** — below v31 (A−), above v29/v30 (B−).

*v33's ops substrate is pristine. The load-bearing problems are at the recipe-pattern layer (seed `execOnce` shape inherited from v20) and at the brief-construction layer (main-agent hallucinates output paths + env names + visual decoration + diagnostic cadence when the guidance leaves axes unspecified). v8.103 already closed the phantom-tree + auto-export defects. v8.104 guidance-hardening plan at [docs/implementation-v8.104-guidance-hardening.md](implementation-v8.104-guidance-hardening.md) closes the remaining four invention classes + the seed-pattern bug that's been shipping unchallenged since v20. Six fixes, all guidance-layer, zero new checks. Thesis: **every v33 defect is an answer the agent invented to a question recipe.md didn't answer.***

**v34 calibration bar** (from v8.104 plan):

- 0 phantom output directories (`find /var/www -maxdepth 2 -type d -name 'recipe-*'` returns nothing)
- Seed uses static key — `grep 'execOnce ${appVersionId}.*seed' zerops.yaml` returns nothing
- 0 Unicode box-drawing in zerops.yaml (`grep -rP '\p{Box_Drawing}' */zerops.yaml` returns nothing)
- Feature-subagent ≤10 diagnostic Bash calls in any 30s window
- Deploy-complete ≤2 rounds, finalize ≤1 round (target 1 each; if still 3, Fix E needs expansion)
- 0 pre-init git-commit `fatal: not a git repository` failures
- All prior calibration items hold (v8.90 + v8.93 + v8.94 + v8.95 + v8.96 + v8.97 + v8.98 + v8.99 + v8.100 + v8.103)

---

## Adding a new version

When a new recipe run lands at `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v{N}/`:

1. **Run the content metrics script** to capture README/CLAUDE/gotcha/IG counts:
   ```bash
   /Users/fxck/www/zcp/eval/scripts/version_metrics.sh
   ```

2. **Run the session analyzer** on main + every subagent:
   ```bash
   python3 /Users/fxck/www/zcp/eval/scripts/analyze_bash_latency.py \
     /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v{N}/SESSIONS_LOGS/main-session.jsonl
   ```

3. **Compute wall clock** from first to last assistant event:
   ```bash
   f=/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v{N}/SESSIONS_LOGS/main-session.jsonl
   grep -m1 '"type":"assistant"' $f | grep -o '"timestamp":"[^"]*"' | head -1
   grep '"type":"assistant"' $f | tail -1 | grep -o '"timestamp":"[^"]*"' | tail -1
   ```

4. **Read `TIMELINE.md` end-to-end** — agent narrative, decisions, close-step findings.

5. **Read every README.md side-by-side with v7** — evaluate gotcha authenticity, IG item depth, dedup state, fragment correctness.

6. **Read every CLAUDE.md** (v16+) — check byte floor, custom section count beyond template, codebase-specificity.

7. **Read one env import.yaml** (typically env 4 — Small Production) — evaluate comment depth against the WHY-not-WHAT rubric.

8. **Grade each of S / C / O / W** independently (see [Rating methodology](#rating-methodology)). Overall = min.

9. **Append a new entry** to the "Per-version log" section following the shape of v16 above. Include:
   - Date, tier/shape, model, session logs path
   - Wall clock, assistant events, tool calls
   - Bash metrics (calls / total / very-long / dev-server sum / errored)
   - Content metrics per codebase
   - Close-step bugs (CRIT/WRONG/STYLE counts + actual findings list)
   - Notable — what changed vs prior, what regressed, what improved
   - Rating (S / C / O / W → overall) with a one-sentence justification

10. **Update the "Cross-version summary" tables** — add the new row to both the content metrics table and the session metrics table.

11. **Update the "Milestones and regressions" table** with a one-line entry naming the key structural change this version represents.

12. **Commit** the doc change in its own commit so the version log has clean per-run history. Commit message shape:
    ```
    docs(recipe-log): v{N} entry — {one-line summary}
    ```

13. If this version surfaces a new class of regression, update `spec-recipe-quality-process.md` with the new check or rule being proposed, and link back to this doc's v{N} entry.

---

## Tooling references

- **`eval/scripts/timeline.py`** — cross-stream chronological event timeline (main + all subagents), phase-detected, with per-tool latency histogram and per-source wall clock via `--stats`. Start here for any new run.
- **`eval/scripts/analyze_bash_latency.py`** — session-log bash latency + pattern analyzer (deep-dive for bash/dev-server when timeline.py flags high latency)
- **`eval/scripts/version_metrics.sh`** — per-codebase content metrics table across all versions
- **`eval/scripts/extract-tool-calls.py`** — stream-json → JSON summary of tool calls, knowledge queries, workflow actions, errors, retries
- **`internal/tools/workflow_checks_*.go`** — the check suite enforcing content rules; read these to understand what WILL block a future run and why
- **`internal/content/workflows/recipe.md`** — the agent-facing guidance; the rules here are what the next run will read
- **`internal/workflow/recipe_gotcha_shape.go`** — the authenticity classifier (platformTerms, frameworkXPlatformTerms, failureModeTerms, scoring function)

## Related docs

- [spec-recipe-quality-process.md](spec-recipe-quality-process.md) — quality rules and how they're enforced
- [spec-workflows.md](spec-workflows.md) — workflow step contracts, sub-step invariants, state model
- [implementation-v9-findings.md](implementation-v9-findings.md), [implementation-v11-findings.md](implementation-v11-findings.md), [improvement-guide-v7-findings.md](improvement-guide-v7-findings.md), [improvement-guide-v8-findings.md](improvement-guide-v8-findings.md) — per-version deep-dives from the earlier phases; this log supersedes them as the ongoing record but they carry richer narrative for their individual runs
