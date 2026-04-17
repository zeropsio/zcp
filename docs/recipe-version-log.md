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

### The analyzer script

`eval/scripts/analyze_bash_latency.py` is the canonical session analyzer. It reads a stream-json file, pairs every `Bash` tool invocation with its result, and reports:

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

- **`eval/scripts/analyze_bash_latency.py`** — session-log bash latency + pattern analyzer
- **`eval/scripts/version_metrics.sh`** — per-codebase content metrics table across all versions
- **`eval/scripts/extract-tool-calls.py`** — stream-json → JSON summary of tool calls, knowledge queries, workflow actions, errors, retries
- **`internal/tools/workflow_checks_*.go`** — the check suite enforcing content rules; read these to understand what WILL block a future run and why
- **`internal/content/workflows/recipe.md`** — the agent-facing guidance; the rules here are what the next run will read
- **`internal/workflow/recipe_gotcha_shape.go`** — the authenticity classifier (platformTerms, frameworkXPlatformTerms, failureModeTerms, scoring function)

## Related docs

- [spec-recipe-quality-process.md](spec-recipe-quality-process.md) — quality rules and how they're enforced
- [spec-workflows.md](spec-workflows.md) — workflow step contracts, sub-step invariants, state model
- [implementation-v9-findings.md](implementation-v9-findings.md), [implementation-v11-findings.md](implementation-v11-findings.md), [improvement-guide-v7-findings.md](improvement-guide-v7-findings.md), [improvement-guide-v8-findings.md](improvement-guide-v8-findings.md) — per-version deep-dives from the earlier phases; this log supersedes them as the ongoing record but they carry richer narrative for their individual runs
