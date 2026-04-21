# zcprecipator2 — research and rewrite plan

**Status**: planning document. No code changes yet.
**Owner**: continuation of the 34-version recipe-workflow trajectory documented in [`../recipe-version-log.md`](../recipe-version-log.md).
**Scope**: research plan for a ground-up rewrite of the recipe workflow's **guidance + check + sub-agent brief composition** layers. Operational substrate (`internal/platform`, `internal/ops`, `internal/tools/zerops_dev_server`, `internal/tools/zerops_browser`, `internal/ops/facts_log.go`, SSH preamble, substep state machine, env-var Go-templates) stays — v34 confirmed it is pristine.

---

## 0. Why this document exists

Thirty-four end-to-end recipe runs of `nestjs-showcase` have produced a trajectory in which every post-mortem adds checks, gates, MANDATORY blocks, topics, and brief content — and the deliverable has not durably improved. The data:

- **Convergence architecture is structurally wrong.** v31 → v33 → v34 all shipped with richer check-failure metadata (v8.96 Theme A `ReadSurface`/`HowToFix`/`CoupledWith`, v8.104 Fix E `PerturbsChecks`). Deploy fix rounds went 3 → 3 → **4**. Finalize rounds went 3 → 2 → **3**. Two generations of "emit richer diagnostics on failure" empirically did not collapse rounds. The gate-after-writer direction is the issue.
- **Agent invention in unspecified axes.** v32 shipped per-codebase READMEs that never landed in the export tree. v33 shipped a phantom `/var/www/recipe-nestjs-showcase/` tree with paraphrased env folder names, Unicode box-drawing separators in zerops.yaml, auto-export at close, a nine-minute feature-subagent diagnostic panic burst. v34 shipped a workerdev gotcha that the writer's own manifest had classified as self-inflicted → CLAUDE.md. Every one of these is an answer the agent invented to a question recipe.md left unspecified.
- **Bloat.** `internal/content/workflows/recipe.md` is 3,438 lines. 60+ named `<block>` regions. 5 sub-agent briefs with mixed dispatcher/sub-agent audience inside each block. Version anchors (v5, v7, v10-v18, v20-v28, v33, v8.85-v8.104) threaded through operational guidance the sub-agents read. Sub-agent permit/forbid tool lists inconsistent with mentioned-tool-use inside the same brief (`zerops_record_fact`, `zerops_workspace_manifest` mentioned but not listed).
- **Redundancy the agent pays for.** v34's TodoWrite was fired 12 times, each a full-list rewrite, because recipe.md's step-entry guidance reads like fresh planning context at each step while the server's `zerops_workflow` already tracks substeps authoritatively. The agent maintains two workflow models in parallel.

The thesis of this plan: **thirty-four versions of accumulated knowledge are enough to design the system cleanly from scratch**. What we cannot do is keep iterating on recipe.md — every accretion since v20 has produced either regression or stalemate.

---

## 1. Thesis

Rewrite the guidance + check + brief composition layers from scratch, informed by the full 34-version trajectory, while preserving the operational substrate that v34 proved works.

Specifically:

**Stays as-is (pristine, validated operational layer)**
- MCP tool implementations: `zerops_deploy`, `zerops_dev_server`, `zerops_browser`, `zerops_logs`, `zerops_verify`, `zerops_subdomain`, `zerops_mount`, `zerops_import`, `zerops_env`, `zerops_discover`, `zerops_knowledge`, `zerops_record_fact`
- Workflow state machine: substep attestation validation, `SUBAGENT_MISUSE` handling, ordering gates
- SSH execution boundary (v8.90 held), git-config-mount pre-scaffold init (v8.93.1 held), post-scaffold `.git/` cleanup rule (v8.96 Fix #4 held), export-on-request (v8.103 held), Read-before-Edit sentinel (v8.97 Fix 3 held)
- Facts log (`internal/ops/facts_log.go`) with `FactRecord.Scope` field
- Env-README Go-source templates (`recipe_templates.go` edits from v8.95 Fix B)
- `zerops_workspace_manifest` tool (v8.94 architecture — fresh-context writer input)
- Dev-server spawn shape (v17.1), pkill self-kill classifier (v8.80), port-stop polling (v8.104 Quality Fix #2)

**Gets rewritten from scratch**
- `internal/content/workflows/recipe.md` → atomic per-topic files under a new tree
- Sub-agent brief composition: the mixed dispatcher/sub-agent blocks become physically separated artifacts
- Check suite (`internal/tools/workflow_checks_*.go`): every check re-evaluated for "author can run this locally before attesting"; non-runnable checks either gain a runnable form or get deleted
- Topic registry + eager-scope machinery: reduced to the minimum required for the new atomic layout
- Writer manifest contract: the honesty dimensions expanded to close v34's manifest ↔ content inconsistency
- Minimal tier's README-writer flow: currently uses the superseded v8 `readme-with-fragments` brief; gets rewritten to the v8.94 fresh-context shape

**Gets dropped**
- Version anchors (v5/v7/…/v8.104) inside operational briefs — they belong in `recipe-version-log.md`, not in sub-agent context
- Dispatcher-facing instructions mixed into sub-agent blocks — moved to a separate "how to dispatch" document not transmitted to sub-agents
- Enumeration-based prohibitions that name the invented forbidden path (turns into a menu of attack options) — replaced with positive allow-lists
- Internal check-name vocabulary inside sub-agent briefs (`writer_manifest_completeness`, `writer_discard_classification_consistency`) — sub-agents see what the check reads and requires, not its implementation name
- Go-source file references inside sub-agent briefs (`internal/workflow/recipe_templates.go`, `internal/ops/facts_log.go`) — the agent cannot open these
- TodoWrite as parallel workflow model — server's substep state IS the plan; TodoWrite becomes check-off-only or is dropped entirely (open decision in §9)

---

## 2. Constraints

### Tier coverage

**Both minimal AND showcase recipes get equal rigor.** The current system treats them asymmetrically:

| Concern | Minimal (nestjs-minimal, laravel-minimal) | Showcase (nestjs-showcase) |
|---|---|---|
| Scaffold sub-agent | single, inline in main | three parallel (apidev + appdev + workerdev) |
| Feature sub-agent | none (main writes features inline) | one single-author across three mounts |
| Writer sub-agent | `readme-with-fragments` (old v8 shape) | `content-authoring-brief` (v8.94 fresh-context) |
| Code-review sub-agent | always | always; v22+ can split into 3 parallel framework-experts |
| env READMEs | Go-template (shared) | Go-template (shared) |
| env import.yaml comments | via `generate-finalize` input | via `generate-finalize` input |
| Worker codebase | never | conditional (`sharesCodebaseWith` empty) |

The rewrite must ship both tier shapes as first-class, not showcase-first-with-minimal-as-afterthought. The current minimal flow has been under-audited because the forensic logs have all been on showcase runs. **Step 1's flow reconstruction must cover both tiers** — this may require commissioning a minimal run specifically for session-log capture if no existing minimal run has SESSIONS_LOGS (verified: none do).

### Architectural constraints (discovered, not optional)

1. **No version anchors in operational guidance.** Every reference to `v20-v28`, `v8.85`, `v32 lost the Read-before-Edit rule`, etc., comes out of recipe content. They belong in `recipe-version-log.md`. Rationale: agent reading the brief has no version history to anchor on; references turn into noise or imitation vectors.

2. **Atomic files, not one 3,438-line monolith.** Per-topic guidance, stitched at dispatch time by the Go layer. Rationale: debuggability, per-topic testability, per-topic versioning, clean blast-radius when any one block changes.

3. **No untestable checks.** Every check has a command (ideally shell) the author can run locally against its own draft before attesting. If a check has no runnable form, either it gains one or it gets deleted. Rationale: v31/v33/v34 convergence data.

4. **Server's workflow state IS the plan.** `zerops_workflow action=status` is the authoritative workflow model. Sub-agents and main agent read it; they do not maintain parallel TODO lists (unless TodoWrite is a pure check-off mirror).

5. **Symbol-naming contracts shared across scaffold sub-agents.** Env var names, endpoint paths, entity table names, hostname conventions get passed as a shared contract object to every scaffold sub-agent, not re-derived per-scaffolder. Closes v22 NATS-URL + v34 DB_PASS/DB_PASSWORD class.

6. **Transmitted brief is a leaf artifact.** The file a sub-agent receives is distinct from any file a human author edits. Dispatcher-facing instructions ("compress this", "include verbatim") live in a separate document outside the transmitted brief. Closes v32's "lost Read-before-Edit across three scaffold subagents" dispatch-compression class.

7. **Fact routing is a graph verified both ways.** Every fact has a manifest entry; every published item has a fact source. The `writer_manifest_honesty` check gains dimensions beyond `(discarded, published_gotcha)` — adds `(routed_to_claude_md, published_gotcha)`, `(routed_to_ig, published_gotcha)`, etc. Closes v34 DB_PASS-as-gotcha class.

---

## 3. The six-step research protocol

Each step specifies: **inputs**, **activity**, **structured output artifact**, **success criteria**, **tier coverage**.

### Step 1 — Flow reconstruction from raw session logs

**Purpose**: establish ground truth for what the current system actually does. No design decisions here. Read-only.

**Inputs**:
- **Showcase**: `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v34/SESSIONS_LOGS/` — main session + 6 sub-agents with meta.json.
- **Minimal**: no existing minimal run has `SESSIONS_LOGS/` captured. **Decision required (see §9)**: commission a minimal run now specifically for this reconstruction, OR reconstruct minimal flow from `recipe.md` + `internal/workflow/*.go` + deliverable artifacts alone (less reliable, but available immediately).

**Activity**: for each session log (main + per-sub-agent), produce a chronological per-tool trace with these fields:

| Column | Content |
|---|---|
| `timestamp` | ISO 8601 |
| `source` | `MAIN` or `SUB-<agent-id-prefix>` |
| `phase/substep` | derived from nearest preceding `zerops_workflow action=complete` call |
| `tool` | tool name (`Bash`, `Read`, `Edit`, `Write`, `mcp__zerops__*`, `Agent`) |
| `input_summary` | first 200 chars of the tool input, with paths + action parameters preserved |
| `result_size` | bytes of the tool-result content |
| `result_summary` | first 200 chars of the result, or error class if errored |
| `guidance_landed` | if the tool is `zerops_workflow action=complete`, the size + topic of the `current.detailedGuide` block returned; otherwise empty |
| `decision_next` | one-sentence inference of what the agent decided to do as a result of this tool call |

In addition, flag every event where:
- A tool-use rejected with `is_error: true` (classify the error class)
- A guidance block arrives AFTER the work it was meant to govern (the v25 substep-bypass class)
- A fact is recorded with `scope=downstream` (v8.96 Theme B adoption)
- A TodoWrite fires (count and classify: full-rewrite vs. check-off)
- A sub-agent dispatch fires (capture the Agent tool's `prompt` parameter verbatim — this is the **actually-transmitted brief** for step 4's diff)

**Structured outputs** (all under `docs/zcprecipator2/01-flow/`):

- `flow-showcase-v34-main.md` — main agent trace
- `flow-showcase-v34-sub-<role>.md` — per sub-agent trace (scaffold-apidev, scaffold-appdev, scaffold-workerdev, feature, writer, code-review)
- `flow-showcase-v34-dispatches/` — captured Agent-tool prompt payloads, one file per dispatch (the transmitted briefs)
- `flow-minimal-<ref>-main.md` — minimal main agent trace
- `flow-minimal-<ref>-sub-<role>.md` — minimal sub-agents
- `flow-minimal-<ref>-dispatches/` — minimal dispatched briefs
- `flow-comparison.md` — tier diff: substeps that exist in one tier only, sub-agent dispatches per tier, guidance topics delivered per tier, check surfaces per tier

**Success criteria**:
1. Every substep in the v34 showcase run has a documented (tool, result, next-decision) line in the trace
2. Every sub-agent dispatch has its transmitted prompt captured verbatim
3. The minimal tier has the same level of detail for whichever run is selected
4. `flow-comparison.md` enumerates every structural difference between minimal and showcase flows — no hand-waving

**Pre-reading already done** (from earlier analysis of v34, retain for this step): 267 main asst events / 169 main tool calls / 46 bash / 73.1s total / 0 very-long main / 0 MCP schema errors. 6 subagents: scaffold apidev (9:13 / 112 asst / 82 tools), appdev (2:12 / 36 asst / 26 tools), workerdev (3:34 / 62 asst / 41 tools), feature (8:49 / 131 asst / 90 tools), readmes writer (6:12 / 26 asst / 20 tools), code-review (5:03 / 122 asst / 89 tools). 19 `record_fact` calls (1 main + 7+2+3+6 sub). 12 TodoWrite rewrites. Deploy rounds 4; finalize rounds 3.

### Step 2 — Global knowledge inventory

**Purpose**: determine what each agent (main + each sub-agent) actually has access to at each phase/substep, and where each piece of knowledge originates. This is where redundancy, gaps, and mis-routed context become visible.

**Inputs**:
- Step 1's flow reconstructions (for evidence of what landed where)
- Current `internal/content/workflows/recipe.md` (for what's declared)
- Current `internal/workflow/recipe_topic_registry.go` (for topic-to-block mapping + eager-scope declarations)
- Current `internal/workflow/recipe_guidance.go` (for `buildSubStepGuide` assembly logic)
- Current `internal/workflow/recipe_brief_facts.go` (for `BuildPriorDiscoveriesBlock` mechanism)
- Current `internal/tools/workflow_checks_*.go` (for check surface per substep)

**Activity**: build a **knowledge matrix** — one row per (tier × phase × substep × agent), columns enumerating the knowledge sources:

| Source | Example |
|---|---|
| Tool schema (permitted) | what MCP tools the brief declares permitted |
| Tool schema (forbidden) | what the forbidden list names |
| Eager topic body | which topic(s) were inlined into the step-entry guide |
| Substep-scoped topic body | which topic the substep's `detailedGuide` delivered |
| `record_fact` log | whether the agent at this phase reads it |
| Workspace manifest | whether available at this phase |
| Prior sub-agent return | what the main agent carried forward from a previous dispatch |
| Plan fields | which `plan.Research` fields were interpolated into the brief |
| Env var catalog | delivered from `zerops_discover includeEnvs=true` |
| Deploy check failure | if this substep is a retry, what failure metadata the agent was given |
| `zerops_knowledge` guide | if called, which topic + size of result |

For each cell: (a) what's delivered, (b) how many bytes / lines, (c) whether the delivery is authoritative or duplicated elsewhere. Then produce three derived artifacts:

- **Redundancy map**: facts delivered to the same agent via 3+ paths. v34 example: SSH-only rule appears in permit-list preamble, MANDATORY sentinel, and `where-commands-run` block — three deliveries of one rule.
- **Gap map**: facts the agent needs but doesn't have at the right phase. v34 example: feature sub-agent needs to know scaffold-phase env-var naming conventions (DB_PASS) but the scaffold-brief's naming decisions aren't routed to the feature brief.
- **Mis-routed map**: facts delivered AFTER the phase they would have governed. v25 example: `readme-fragments` brief delivered at `complete feature-sweep-stage` AFTER the writer already shipped content.

**Structured outputs** (under `docs/zcprecipator2/02-knowledge/`):

- `knowledge-matrix-showcase.md` — the big matrix for showcase tier
- `knowledge-matrix-minimal.md` — the big matrix for minimal tier
- `redundancy-map.md` — facts delivered N>1 times with evidence pointers
- `gap-map.md` — facts missing where needed with incident pointers from the version log
- `misroute-map.md` — facts delivered after their load-bearing window

**Success criteria**:
1. Every cell in both tier matrices is populated (evidence: file:line or trace timestamp)
2. Every item in the redundancy / gap / misroute maps cites a concrete defect class from v20-v34
3. No hand-waving: a cell reading "X is probably delivered somewhere" is not accepted — find it or mark it as gap

### Step 3 — Architecture design

**Purpose**: from step 1+2 evidence, design a clean system. Defer implementation detail; focus on structural invariants and data flow.

**Inputs**: Step 1 traces + Step 2 matrices.

**Activity**: produce four artifacts.

#### 3a — Structural invariants (principles)

A short list (target: 5–10). Each principle:
- Stated as a testable invariant ("for all X, Y holds")
- Cites one or more specific v20-v34 defect classes it closes
- Names the mechanism enforcing it in the new architecture
- Identifies what in the current system it replaces

**Stake-in-the-ground starting list** (subject to revision during step 3):

| # | Invariant | Defect class closed | Replaces |
|---|---|---|---|
| 1 | Every content check has a command the author runs locally before attesting | v31/v33/v34 convergence 3-round → 4-round trajectory | External-gate + dispatch fix subagent pattern |
| 2 | Transmitted sub-agent briefs are leaf artifacts — no dispatcher instructions, no version anchors, no internal check vocabulary, no Go-source paths | v32 dispatch compression dropping Read-before-Edit; v33 version-log leakage into briefs | Current `<block>` structure with mixed audience |
| 3 | Parallel scaffold sub-agents share a symbol-naming contract | v22 NATS URL-embedded creds recurrence; v34 DB_PASS/DB_PASSWORD mismatch | Independent per-scaffold decisions |
| 4 | Server's workflow state is the authoritative plan; agents mirror, never rebuild | v34 12 TodoWrite full-rewrites; v25 substep-bypass backfill | TodoWrite as parallel workflow model |
| 5 | Fact routing is a graph verified both ways (every fact → manifest; every published item → fact source) | v34 manifest↔content inconsistency (DB_PASS) | Single-direction `writer_manifest_honesty` |
| 6 | Guidance atomized per topic; stitched at dispatch time; version anchors live only in the archive | v33 Unicode box-drawing invention; recipe.md 3,438-line monolith | Monolithic recipe.md |
| 7 | Every sub-agent dispatch brief is reviewable cold by a fresh reader in under 3 minutes and is testable against a simulation fixture | Every dispatch-compression failure class | Ad-hoc brief quality |

These are **hypotheses entering step 3**, not conclusions. Step 3's activity is to pressure-test each against step 1+2 data. A principle that doesn't trace to a specific defect class gets cut. A defect class with no principle coverage gets a new principle.

#### 3b — Atomic file layout

The current monolith must split. Target structure (final shape TBD during step 3):

```
internal/content/workflows/recipe/
├── phases/
│   ├── research/
│   │   ├── entry.md                    — step-entry guide
│   │   └── completion.md               — completion shape
│   ├── provision/
│   │   ├── entry.md
│   │   ├── import-yaml/
│   │   │   ├── standard-mode.md
│   │   │   ├── dual-runtime.md
│   │   │   └── static-frontend.md
│   │   ├── mount-filesystem.md
│   │   ├── git-config.md
│   │   ├── env-discovery.md
│   │   └── completion.md
│   ├── generate/
│   │   ├── entry.md
│   │   ├── zerops-yaml/
│   │   │   ├── schema.md
│   │   │   ├── env-var-model.md
│   │   │   ├── dual-runtime-shapes.md
│   │   │   ├── setup-rules-dev.md
│   │   │   ├── setup-rules-prod.md
│   │   │   └── setup-rules-worker.md
│   │   ├── scaffold/
│   │   │   ├── dashboard-skeleton.md
│   │   │   ├── platform-principles.md
│   │   │   └── preship-assertions.md
│   │   └── completion.md
│   ├── deploy/
│   │   ├── entry.md
│   │   ├── dev-flow.md
│   │   ├── feature-sweep-dev.md
│   │   ├── browser-walk.md
│   │   ├── stage-flow.md
│   │   ├── readmes-substep.md
│   │   └── completion.md
│   ├── finalize/
│   │   ├── entry.md
│   │   ├── env-comments.md
│   │   └── completion.md
│   └── close/
│       ├── entry.md
│       ├── code-review.md
│       ├── browser-walk.md
│       └── completion.md
├── briefs/
│   ├── scaffold/
│   │   ├── _shared-mandatory.md       — MANDATORY blocks (file-op, tool-use, SSH)
│   │   ├── api-codebase.md
│   │   ├── frontend-codebase.md
│   │   └── worker-codebase.md
│   ├── feature/
│   │   └── brief.md
│   ├── writer/
│   │   ├── showcase.md                 — fresh-context content-authoring
│   │   └── minimal.md                  — rewritten from old readme-with-fragments
│   └── code-review/
│       └── brief.md
├── principles/                         — referenced by multiple phases/briefs
│   ├── where-commands-run.md
│   ├── git-workflow.md
│   ├── platform-principles/
│   │   ├── 01-graceful-shutdown.md
│   │   ├── 02-routable-bind.md
│   │   ├── 03-proxy-trust.md
│   │   ├── 04-competing-consumer.md
│   │   ├── 05-structured-creds.md
│   │   └── 06-stripped-build-root.md
│   └── symbol-naming-contract.md       — NEW, closes v34 class
└── README.md                           — architecture overview, not loaded by agents
```

The Go stitching layer (`recipe_guidance.go`, `recipe_topic_registry.go`) composes a response from these atoms at dispatch time. An atomic file is one topic, one concern, one audience. Reviewing any one atom is a bounded task.

**Dispatcher vs transmitted-brief separation**: `briefs/scaffold/_shared-mandatory.md` is what the scaffold sub-agent receives. The instructions to the main agent ("compress everything else, include this verbatim") live in `docs/zcprecipator2/DISPATCH.md` — a human-facing doc, never transmitted.

#### 3c — Data flow diagrams

For each tier (minimal, showcase) and each phase:
- What the server delivers to the main agent (step-entry guide + runtime-injected data)
- What the main agent delivers to each sub-agent (composed from atoms + plan fields)
- What each sub-agent returns to the main agent
- What the main agent attests via `zerops_workflow complete`
- What the checker reads and emits

Diagrams as sequence diagrams (ASCII / mermaid), one per (tier, phase).

#### 3d — Check-surface rewrite

Walk every check in `internal/tools/workflow_checks_*.go`. For each:
- What does it read? (exact file/region)
- What does it assert? (exact predicate)
- Can the author run this exact predicate locally before attesting? (yes / no / requires what wrapper)
- What's the runnable form? (exact shell command — `grep`, `awk`, `ratio-computation.sh`)
- Under the new architecture: keep / rewrite-to-runnable / delete (with rationale)

**Structured outputs** (under `docs/zcprecipator2/03-architecture/`):

- `principles.md` — final invariant list with defect-class trace
- `atomic-layout.md` — final file tree with rationale for each split
- `data-flow-showcase.md` — diagrams per phase for showcase
- `data-flow-minimal.md` — diagrams per phase for minimal
- `check-rewrite.md` — per-check keep/rewrite/delete table

**Success criteria**:
1. Every principle has ≥1 defect-class trace from v20-v34
2. Every defect class closed by v8.78-v8.104 has a principle covering it (if not: new principle)
3. Atomic layout has no topic >300 lines (the `scaffold-subagent-brief` is currently 335 lines; post-split, each atom is bounded)
4. Every check in the current suite has a disposition (keep / rewrite-to-runnable / delete)

### Step 4 — Context verification (triple-check)

**Purpose**: for each sub-agent brief in the new architecture, simulate receiving it cold and verify no defect class from v20-v34 can recur.

**Inputs**: step 3's atomic layout + step 1's captured v34 dispatches + the full defect-class registry from `recipe-version-log.md`.

**Activity**: per sub-agent × per tier:

1. **Draft the composed brief** that the new architecture would produce (concatenate the relevant atoms using the new Go stitching logic, interpolate plan fields, prepend Prior Discoveries block).
2. **Cold-simulate receiving it.** Read it as if with no prior context. Document what's ambiguous, contradictory, or impossible to act on.
3. **Diff against v34's transmitted brief.** What content is gone? What's new? For each removed line, trace: was it scar tissue, version-log noise, dispatcher instruction, or load-bearing? For each added line, trace: which defect class does it close?
4. **Run the defect-class check.** Against the registry (§5 below), verify every v20-v34 closed defect class has an enforcement point in the new brief OR in the new check suite OR in the new Go-source injection. No defect class unaddressed.

**Structured outputs** (under `docs/zcprecipator2/04-verification/`):

For each (sub-agent role × tier) pair — showcase has scaffold-api, scaffold-frontend, scaffold-worker, feature, writer, code-review = 6; minimal has scaffold (single inline), writer, code-review = 3 (feature is main-agent-inline for minimal):

- `brief-<role>-<tier>-composed.md` — the brief the new system would transmit
- `brief-<role>-<tier>-simulation.md` — cold-read report: ambiguities, contradictions, impossible actions
- `brief-<role>-<tier>-diff.md` — diff against v34's captured dispatch
- `brief-<role>-<tier>-coverage.md` — defect-class coverage table

**Success criteria**:
1. Every composed brief reads cleanly cold-read (no contradictions, no unresolved ambiguities)
2. Every removed-vs-v34 line has a disposition (scar/noise/dispatcher/load-bearing-moved-where)
3. Every v20-v34 closed defect class has a prevention mechanism cited in the appropriate coverage doc

### Step 5 — Regression fixture

**Purpose**: codify the defect classes closed by v8.78–v8.104 into testable scenarios that the new architecture must still prevent. Without this, the rewrite is flying blind.

**Inputs**: `recipe-version-log.md` defect enumeration; step 4's coverage docs.

**Activity**: build a **defect-class registry** — one row per closed defect class, each with:

| Field | Content |
|---|---|
| `id` | short slug (e.g. `v21-scaffold-hygiene`, `v34-manifest-content-inconsistency`) |
| `origin_run` | first version where it surfaced |
| `class` | short description |
| `mechanism_closed_in` | version that shipped the fix (e.g. v8.80, v8.95) |
| `current_enforcement` | where the current system blocks it (check name, brief sentinel, Go code) |
| `new_enforcement` | where the new architecture blocks it (principle N, atom file, check, runtime injection) |
| `test_scenario` | executable description: given X, expected Y |
| `calibration_bar` | measurable v35-run threshold (e.g. "0 occurrences of pattern P in deliverable tree") |

Scenarios to include at minimum (derived from the version log, non-exhaustive):

- v10: empty fragment markers
- v11: 8-hour dev-server SSH hangs
- v13: preprocessor check dead-code-gated
- v16: `dbDriver` typeorm-as-database leak
- v17: zcp-side executable commands (SSHFS write-surface-as-execution-surface)
- v19: stale-major import class (CacheModule wrong path)
- v20: decorative content drift (generic .env advice, predecessor clones)
- v21: scaffold node_modules leak; `claude_readme_consistency` dead regex
- v22: NATS URL-embedded creds recurrence; gotchas-as-run-incident-log
- v23: folk-doctrine invention (execOnce burn); 5-round convergence spiral
- v25: substep-delivery bypass; subagents calling `zerops_workflow` at spawn
- v28: content authoring by debug-spiraled agent (33% genuine)
- v29: preship.sh scaffold-artifact leak; env-0 cross-tier persistence fabrication; env-README Go-template drift
- v31: apidev enableShutdownHooks missing; deploy+finalize 3-round convergence
- v32: close-never-completed; per-codebase content missing from export; dispatch-compression dropped Read-before-Edit
- v33: phantom recipe-nestjs-showcase/ tree; auto-export-at-close; Unicode box-drawing; 9-min diagnostic panic; execOnce ${appVersionId} seed key
- v34: writer manifest↔content inconsistency; cross-scaffold env-var coordination; Fix E convergence refutation

**Structured outputs** (under `docs/zcprecipator2/05-regression/`):

- `defect-class-registry.md` — the table above, one row per class
- `calibration-bars-v35.md` — aggregated measurable thresholds for the first run on the new system

**Success criteria**:
1. Every defect class named in `recipe-version-log.md` with a ✅/❌ verdict has a registry entry
2. Every entry has a `test_scenario` expressible without the current system's specific Go code (i.e. the scenario could be rerun in a v2+ system and meaningful)
3. Calibration bars are numeric / grep-verifiable, not qualitative

### Step 6 — Migration path

**Purpose**: decide how we get from current to new without regression cascades.

**Inputs**: all prior steps.

**Activity**: produce a rollout proposal. Two candidate shapes:

**Candidate A — Parallel run on v35**
- New system runs alongside current. v35 dispatches through the new code path; the old code path stays loaded and can be reverted per commit.
- v35's session log is captured under both systems (new primary, old as shadow if feasible).
- Diff outputs; any divergence is triaged.
- Risk: partial-old/partial-new states during rollout; two code paths to maintain briefly.

**Candidate B — Cleanroom**
- New system replaces current in one commit. v35 runs on new only.
- Rollback = git revert of the commit.
- Risk: if v35 regresses on an unforeseen class, recovery is a full revert.

Decision criteria to articulate:
- How big is the delta? (If recipe.md→atoms is additive + check suite is incremental rewrites, parallel is plausible. If data-flow model changes such that old and new can't coexist, cleanroom.)
- What's the shadow-diff cost? (Runtime-only overhead vs code-structure overhead.)
- What's the rollback cost?

**Structured outputs** (under `docs/zcprecipator2/06-migration/`):

- `migration-proposal.md` — candidates A and B with trade-offs
- `rollout-sequence.md` — commit-level plan if candidate chosen
- `rollback-criteria.md` — go/no-go thresholds for reverting after v35 evidence

**Success criteria**:
1. An explicit recommendation with justification
2. Rollback criteria are measurable (not "looks wrong")
3. v35 calibration bars (from §5) are the go/no-go triggers

---

## 4. Directory structure

```
docs/zcprecipator2/
├── README.md                           ← this file
├── DISPATCH.md                         ← dispatcher-facing composition guide
├── HANDOFF-to-I<N>.md                  ← instance-scoped handoffs (one per fresh-instance transition)
├── 01-flow/                            ← v34-era flow traces (historical; new runs land in runs/vN/)
│   ├── flow-showcase-v34-main.md
│   ├── flow-showcase-v34-sub-*.md
│   └── flow-showcase-v34-dispatches/
├── 02-knowledge/
│   ├── knowledge-matrix-showcase.md
│   ├── knowledge-matrix-minimal.md
│   ├── redundancy-map.md
│   ├── gap-map.md
│   └── misroute-map.md
├── 03-architecture/
│   ├── principles.md
│   ├── atomic-layout.md
│   ├── data-flow-showcase.md
│   ├── data-flow-minimal.md
│   └── check-rewrite.md
├── 04-verification/
│   ├── brief-*-composed.md
│   ├── brief-*-simulation.md
│   ├── brief-*-diff.md
│   └── brief-*-coverage.md
├── 05-regression/                      ← standing, multi-run docs
│   └── defect-class-registry.md        ← append-only; one section per run's new defect classes
├── 06-migration/                       ← standing migration docs
│   ├── migration-proposal.md
│   └── rollout-sequence.md
├── runs/                               ← per-run analyses (see runs/README.md for runbook)
│   ├── README.md                       ← index of past runs + runbook for analysing a new one
│   └── v<N>/                           ← one folder per commissioned run
│       ├── README.md                   ← TL;DR + file index + verdict one-liner
│       ├── analysis.md                 ← narrative post-mortem
│       ├── verdict.md                  ← decision + measurement tightenings
│       ├── calibration-bars.md         ← snapshot of bars this run was measured against
│       ├── rollback-criteria.md        ← snapshot of T-triggers used to arbitrate
│       ├── flow-main.md                ← main-agent session trace
│       ├── sub-*.md                    ← per-subagent traces
│       ├── flow-dispatches/            ← verbatim dispatch prompts
│       └── role_map.json
└── scripts/
    ├── extract_flow.py                 ← per-stream trace + dispatch capture
    └── ...                             ← (see scripts/ for others)
```

**Per-run pattern**: snapshot docs (`calibration-bars.md`, `rollback-criteria.md`) live under `runs/vN/` so each run's measurement surface is frozen in place. Standing docs (`defect-class-registry.md`, `HANDOFF-to-I*.md`, `06-migration/*`) grow across runs. See [`runs/README.md`](runs/README.md) for the analysis runbook used to populate a new `runs/vN/`.

---

## 5. Architectural principles — stake-in-the-ground list

Restated from §3a above, for quick reference during step 3:

1. **Every content check has an author-runnable pre-attest form.** Gate runs become confirmation, not discovery. Closes convergence 3→4 round regression.
2. **Transmitted briefs are leaf artifacts.** No dispatcher text, no version anchors, no internal check vocabulary, no Go-source paths inside a sub-agent brief. Closes v32 dispatch-compression + v33 version-leak classes.
3. **Scaffold sub-agents share a symbol-naming contract.** Env vars, endpoint paths, entity names, hostname conventions flow from one shared source. Closes v22 NATS-URL + v34 DB_PASS classes.
4. **Server's workflow state IS the plan.** Agents mirror via check-offs; they don't maintain parallel TODO lists. Closes v25 substep-bypass + v34 TodoWrite-rewrite class.
5. **Fact routing is a two-way graph.** `writer_manifest_honesty` expands beyond `(discarded, gotcha)` to cover every routed-to-X vs published-as-Y dimension. Closes v34 manifest↔content class.
6. **Guidance is atomic; provenance lives only in the archive.** One topic per file, stitched at dispatch time. `recipe-version-log.md` is the only place version anchors appear. Closes the 3,438-line monolith class.
7. **Every brief passes a cold-read + defect-coverage test before shipping.** Review gate on every atomic file AND every composed dispatch. Closes ad-hoc brief quality.

---

## 6. Defect-class registry (seed list)

The full registry is a step-5 output. Seed rows (non-exhaustive, to be filled in during research):

| id | origin | class | current enforcement | target enforcement |
|---|---|---|---|---|
| `v17-sshfs-write-not-exec` | v17 | scaffold subagent ran `cd /var/www/{host} && <exec>` zcp-side instead of SSH | scaffold-brief MANDATORY block + `bash_guard` middleware | `principles/where-commands-run.md` atom, referenced from scaffold/feature/writer/code-review briefs |
| `v21-scaffold-hygiene` | v21 | 209 MB `node_modules` committed into deliverable | `scaffold_hygiene` check | principles: preship assertion 8 (.gitignore baseline); additionally pre-export hygiene check |
| `v22-nats-url-creds` | v22 | URL-embedded NATS creds recurring despite v21 gotcha in README | scaffold-brief NATS preamble + platform principle 5 | symbol-naming-contract shared across scaffolders |
| `v25-substep-bypass` | v25 | main did 40min of deploy work silently, backfilled attestations | v8.90: de-eager `subagent-brief`/`readme-fragments`, `SUBAGENT_MISUSE` error | same (survives rewrite) |
| `v28-debug-agent-writes-content` | v28 | debugging agent writing reader-facing content = 33% genuine gotchas | v8.94: fresh-context writer subagent | same (survives rewrite, expanded to minimal tier) |
| `v29-env-0-cross-tier-fabrication` | v29 | env 0 README claimed data persists across tiers | v8.95 Fix B: Go template edits | same |
| `v32-dispatch-compression` | v32 | main dropped Read-before-Edit across 3 scaffold subagents | MANDATORY sentinels with byte-identical transmission | separate dispatcher-vs-transmitted-brief files |
| `v33-phantom-output-tree` | v33 | writer wrote to `/var/www/recipe-{slug}/` + paraphrased env folder names | v8.103: close-section canonical-output guard; v8.104 Fix A | positive allow-list in writer brief (not negative prohibition) |
| `v34-manifest-content-inconsistency` | v34 | workerdev DB_PASS gotcha shipped despite manifest routing fact to claude-md | (unaddressed) | expanded `writer_manifest_honesty` check covering all routing dimensions |
| `v34-cross-scaffold-env-var` | v34 | apidev read `DB_PASS`, workerdev read `DB_PASSWORD` | (unaddressed; caught downstream) | symbol-naming-contract shared across scaffold dispatches |
| `v34-convergence-architecture` | v34 | Fix E refuted: metadata-on-failure doesn't collapse rounds | (unaddressed) | principle 1: author-runnable pre-attest checks |

(Full registry populated in step 5.)

---

## 7. v34 findings that precondition the research

From the per-version entry just added — these are the facts entering step 1, so step 1 doesn't re-derive them:

**Wall + cost**
- Recipe wall: 73 min (10:17:56 → 11:30:55)
- Main asst events: 267 / tool calls: 169
- Main bash: 46 calls / 73.1s / 0 very-long / 0 errored
- 6 sub-agents (scaffold×3 + feature + writer + code-review)
- 19 `record_fact` calls (1 main + 18 sub)
- 12 TodoWrite full-rewrites
- 3 `zerops_browser` calls
- 6 `zerops_guidance` calls
- 8 `zerops_knowledge` calls
- 6 `Agent` dispatches

**Mechanism-layer fixes that held**
- v8.104 Fix A (phantom tree) ✅
- v8.104 Fix B (seed static key) ✅ — `bootstrap-seed-v1` in apidev zerops.yaml both dev + prod setups
- v8.104 Fix C (no Unicode box-drawing) ✅
- v8.104 Fix D (diagnostic-probe cadence) ✅ — feature subagent max 5 bash/min
- v8.104 Fix F (pre-init git sequencing) ✅ — no runtime `fatal: not a git repository`
- v8.103 (export-on-request) ✅ — export at 11:35 was user-triggered post-close message
- v8.90 (substep state coherence) ✅ — all attestations real-time in canonical order
- v8.97 Fix 3 (Read-before-Edit) ✅ — 0 "File has not been read yet" errors
- v8.96 Fix #1 (zerops_knowledge schema clarity) ✅ — 0 MCP schema errors
- v8.96 Fix #4 (git lock contention) ✅ — 0 `.git/index.lock` contention

**Mechanism-layer fix that did NOT collapse rounds**
- v8.104 Fix E (`PerturbsChecks` on dedup checks) ❌ — shipped structurally, deploy rounds 3→4, finalize 2→3

**Content defects shipped**
- workerdev gotcha #3: `SASL: client password must be a string` — self-inflicted per own manifest, but shipped as gotcha
- apidev gotcha #6: `/api/status` vs `/api/health` — self-referential framing of a real principle
- Cross-scaffold env var naming: apidev `DB_PASS` vs workerdev `DB_PASSWORD` — runtime crash + close-review WRONG

**Operational substrate validation**
- `zerops_dev_server` stable (18.1s probe, 0 hangs)
- SSH boundary held (0 zcp-side execs)
- env-README Go templates correct (minContainers claims match YAML truth byte-for-byte)
- Facts log + `scope=downstream` working
- Writer ZCP_CONTENT_MANIFEST.json produced correctly
- Close code-review caught 3 WRONG, all fixed inline
- Stage browser walk green

---

## 8. Open decisions (before step 1 starts)

1. **Minimal-tier flow reconstruction input**: commission a new minimal run now (e.g. fresh `laravel-minimal` or `nestjs-minimal`) specifically to capture SESSIONS_LOGS, OR reconstruct from recipe.md + Go-source + v34 showcase-as-reference (less reliable)?
   - **Lean**: commission new run. The current system is cheap to re-run (minimal runs typically 25-35 min wall) and the session logs are the only way to catch minimal-specific behavior (e.g. how main agent writes feature code inline vs. how showcase dispatches).

2. **Session-log reading granularity**: solo pass (one artifact per run) vs. split (one artifact per agent within each run)?
   - **Lean**: split. Each sub-agent gets its own trace doc so step 4's cold-read simulation has direct input.

3. **TodoWrite disposition**: keep as check-off-only (principle 4), or drop entirely?
   - **Lean**: keep as check-off-only. TodoWrite remains useful for within-substep ad-hoc planning (e.g. 3-round README fix loop). What's eliminated is the full-rewrite pattern at step-entries.

4. **Migration shape preference**: parallel-run on v35 vs. cleanroom?
   - Deferred until step 6. Depends on how much Go code the new system replaces vs. adds-alongside.

5. **Check deletion threshold**: how aggressive is step 3d's delete-column? Safest is "only delete when provably redundant given new principle enforcement"; most aggressive is "delete anything the new architecture handles upstream."
   - **Lean**: conservative by default. Every delete has a one-sentence justification + a test scenario proving the new upstream handles it.

6. **Who produces what**: is this a single long research pass (one agent does steps 1-6 sequentially), or parallel-spawned (step 1 + step 2 run concurrently since they have disjoint input surfaces)?
   - **Lean**: sequential for steps 1→2→3; steps 4 and 5 can parallelize after step 3 lands; step 6 is sequential after 4+5.

---

## 9. Anti-goals

Things this rewrite is **not** trying to do:

- **Not** trying to eliminate all sub-agent dispatches. The multi-agent pattern works (scaffold parallelism, single-author feature subagent, fresh-context writer, code-review). What's broken is the brief-composition layer.
- **Not** trying to replace `zerops_workflow` as the state authority. Workflow state remains server-side authoritative; agents mirror.
- **Not** trying to produce a "maximum-check" system. The convergence data says more checks is worse, not better; the target is fewer checks, each runnable pre-attest.
- **Not** trying to collapse minimal and showcase flows into one. They have structurally different sub-agent shapes; the rewrite keeps them as two first-class flows sharing atoms.
- **Not** trying to ship the new system without regression coverage. Step 5's regression fixture is non-negotiable.

---

## 10. Success criteria for the rewrite (as measured on v35)

Derived from the defect-class registry (step 5 output). At minimum:

- **Operational** (v34 parity): wall ≤ 90 min; main bash ≤ 10 min; 0 very-long; 0 MCP schema errors; 0 SUBAGENT_MISUSE; 0 `.git/index.lock` contention; 0 "File has not been read yet"; 0 zcp-side execs; 0 phantom output trees; 0 auto-export; 0 Unicode box-drawing; seed uses static key.
- **Convergence**: deploy fix rounds **≤ 2** (v34: 4); finalize rounds **≤ 1** (v34: 3). First calibration bar that's been missed across 4 consecutive runs — becomes the single most important v35 measurement.
- **Content**: gotcha-origin ≥ 80% genuine; 0 self-inflicted shipped as gotchas; 0 folk-doctrine fabrications; 0 version anchors in published content; no wrong-surface items; CLAUDE.md ≥ 1200 bytes with ≥ 2 custom sections per codebase.
- **Manifest↔content consistency**: 0 facts shipped as gotchas while manifest routes them elsewhere.
- **Cross-scaffold coordination**: 0 env-var-naming mismatches caught at deploy runtime or close review.
- **Close review**: 0 CRIT shipped after close; WRONG count ≤ v34's 3.
- **Tier coverage**: both minimal and showcase runs on the new system hit their tier-specific bars (not just showcase).

---

## 11. Known unknowns

Things the research will discover that might force plan revision:

- **Atom boundaries** — how many atoms the guidance actually decomposes into. Could be 30, could be 80. Too many = orchestration complexity. Too few = we didn't actually atomize.
- **Check-rewrite cost** — how many checks can be made author-runnable vs. must be deleted. If most checks can't have a runnable form even in principle, principle 1 is wrong and step 3 needs a different answer.
- **Symbol-naming-contract shape** — is this a plan-field, a separate JSON artifact, a shared topic, or runtime-injected? Step 2's knowledge matrix will reveal what's already carried along the dispatch chain and what would need to be added.
- **Minimal-tier sub-agent shape** — current minimal uses inline main-agent feature writing. Is this the right shape, or should minimal also adopt the fresh-context writer pattern? Step 1's minimal reconstruction will inform.
- **Migration feasibility** — whether parallel-run is actually feasible depends on how much Go state the two systems would share. Unknown until step 3's atomic layout is concrete.

---

## 12. Next actions

Before research begins, the open decisions in §8 need explicit answers:
1. Commission a minimal run or reconstruct from code alone?
2. Split session-log passes per sub-agent?
3. TodoWrite disposition?
4. Step parallelization shape?

Once decided, step 1 starts. Expected delivery per step, working sequentially:
- Step 1: ~4-8 hours (split into showcase + minimal sub-tasks)
- Step 2: ~3-5 hours
- Step 3: ~4-6 hours (design work, iteration expected)
- Step 4: ~3-5 hours (per-brief simulation)
- Step 5: ~2-3 hours (compilation from version log)
- Step 6: ~1-2 hours (decision doc)

Total: 17-29 hours of focused research before any code changes. Output is a complete design artifact that a fresh instance can read and implement against.
