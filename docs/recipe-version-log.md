# Recipe Version Log

Running record of every tracked `nestjs-showcase` build from v6 through v16 (and onward). Each version is an end-to-end recipe run the zcp workflow produced; comparing them head-to-head is how we diagnose regressions, validate fixes, and decide which knobs to turn next.

The `nestjs-showcase` recipe is our canonical "hard run" — it exercises 3 separate codebases, 5 managed services, dual-runtime URL wiring, a worker subagent, and the full 6-environment tier ladder. Every sub-feature of the workflow either shows up here or doesn't ship.

- [Why we log versions](#why-we-log-versions)
- [How to explore a version](#how-to-explore-a-version)
- [How to analyze a session](#how-to-analyze-a-session)
- [How to evaluate content quality](#how-to-evaluate-content-quality)
- [Rating methodology](#rating-methodology)
- [Cross-version summary](#cross-version-summary)
- [Per-version log](#per-version-log) — v6 through v16
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
