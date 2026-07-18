# Run-34 validation — first prod dogfood after v9.76.0

**Date:** 2026-05-09
**Run dir:** `docs/zcprecipator3/runs/34/`
**Substrate:** v9.76.0 (commit 7927fd1f) — Changes 1-5 from
[`run-33-architectural-fixes-2026-05-09.md`](run-33-architectural-fixes-2026-05-09.md)
+ voice-mix substrate edit (V1, Y2, IG2, TY2/TY4) + RF1/PD1 teaching for
api codebase only.
**Sim baseline:** `docs/zcprecipator3/simulations/33-postfixes-3/`
**Codex-verified:** every claim below confirmed via codex:codex-rescue.

---

## Headline

**Substantial regression vs sim-3 across audience-model, sibling-coherence,
and persistence axes.** Counter-driven measurement still passes most green
checks, but **all four canonical run-33 audience-model failures are back**
and the apidev README ships without RF1+PD1 (which were the new sim-3
deliverables).

The diagnosis is **NOT a substrate gap**. The substrate landed and the
agents partially complied; what failed are three concrete engine /
brief-wiring gaps that surfaced only on a fresh prod run. **Recommend
brief + engine fixes, not substrate iteration.** v9.76.0 is **NOT
production-ready.**

---

## Counter table

| Counter | Run-33 | Sim-3 (success-floor) | **Run-34** | Verdict |
|---|---|---|---|---|
| #1 cross-codebase env coherence | 3 mismatches | 0 | **2 mismatches** (S3_*/STORAGE_* + MEILI_*/SEARCH_*) | **REGRESSION** vs sim-3 |
| #2 strict slug-leak | 0 | 0 | **0** | at-bar |
| #2c slug-stem in link-text | 7 | 0 | **0** | at-bar |
| #3 cross-framework verb | 1 | 1 | **1** (NestJS-Express stack mention) | at-bar |
| #4 voice-leak (sharpened regex) | 0 | 0 | **0** | at-bar |
| Adapt-path framing | 0 | 0 | **0** | at-bar |
| Tier-vocab on codebase surfaces | 1 | 0 | **0** | at-bar |
| `${peer_alias}` in porter prose | 7 | 0 | **3** (all in trap-explanation context) | partial regression |
| **Tier intro lead-prefix `Tier N — ...`** | 6 | 0 | **6 (every tier)** | **REGRESSION** to run-33 baseline |
| KB sibling-consistency | 100% inconsistent | `### Gotchas` H3 ×3 | **`## Tips and Others` ×2 + `## Tips and Gotchas` ×1** | partial — promoted to H2 but title-divergent |
| yaml density apidev | 50.0% | 38.9% | **45.5%** (5.5pp over target) | **REGRESSION** |
| yaml density appdev | 59.4% | 38.4% | **61.7%** (21.7pp over target) | **REGRESSION** to worse-than-run-33 |
| yaml density workerdev | 51.8% | 36.1% | **48.3%** (8.3pp over) | **REGRESSION** |
| RF1 (`## Recipe features`) on apidev | absent | present | **absent** | **REGRESSION** vs sim-3 |
| PD1 (`## Production vs. Development`) on apidev | absent | present | **absent** | **REGRESSION** vs sim-3 |
| `## Understand Zerops Core Concepts` on apidev | varies | present | **absent** | **REGRESSION** |
| facts.jsonl strict-token contamination | 14/103 (13.6%) | not exercised | **15/117 (12.8%)** | **NO MOVEMENT** vs run-33 |

**Eight regression rows.** Three (env-coherence, tier-prefix, yaml-density-appdev)
are at-or-worse than run-33 baseline. Three (RF1/PD1/Understand on apidev)
are sim-3 wins lost. Counter-only readout would say "5 green / 8 regressed";
porter-empathy readout (below) says the regression is wider.

---

## Per-surface porter-empathy read

### apidev/README.md — substantial below-bar

What a porter sees:
- Intro: clean. Names the framework + capabilities (NestJS REST API + items CRUD + 5 managed services).
- IG #1 (Adding zerops.yaml): yaml block at 175 lines with comment density 45.5%. Heavy paragraph commentary on env-var aliasing, init-command decomposition, NATS Pattern A trap. Reads as platform-engineering reasoning, not porter-actionable code-paste.
- IG #2-#5: bind 0.0.0.0 / trust proxy / **alias managed-service env vars** / drain-on-SIGTERM. **IG #4 is back to the IG6-violating alias-IG** that sim-3 had eliminated (replaced with drain-on-SIGTERM there). Generic best-practice teaching the cloned recipe yaml already does.
- **NO `## Understand Zerops Core Concepts`** section.
- **NO `## Recipe features`** section (RF1 missing).
- **NO `## Production vs. Development`** section (PD1 missing).
- KB at `## Tips and Others`: 8 bullets, intersection-trap shape, mostly post-deploy traps. Substance at-bar with sim-3.

The published apidev README is **structurally incomplete vs sim-3** — three H2 sections sim-3 shipped are missing.

### appdev/README.md — at-bar with sim-3

What a porter sees:
- Intro: clean.
- IG #1-#5: bind Vite / allow Zerops subdomain / bake API origin / strip dist prefix. All Zerops-forced or recipe-feature topics. **No alias-IG.**
- `## Understand Zerops Core Concepts` H2 present.
- KB at `## Tips and Gotchas` (H2): 2 flat bullets + `### Material 3 token contract` H3 with `> [!CAUTION]` callout. Closer to jetstream's KB shape than sim-3 was.
- yaml at 61.7% comment density — **way over target** (jetstream golden = 36%).

### workerdev/README.md — substantial below-bar

What a porter sees:
- Intro: clean.
- IG #1-#5: bootstrap-as-application-context / drain-subscriptions / **alias cross-service variables** / migrations-with-execOnce. **IG #4 is the IG6-violating alias-IG.**
- `## Understand Zerops Core Concepts` H2 present.
- KB at `## Tips and Others` (H2): 7 bullets. First bullet is queue-group teaching (which sim-3 placed at IG#4). Substance at-bar.

### Tier READMEs — every tier regressed

All 6 tier intros lead with `Tier N — ...` prefix prose:
- `Tier 0 — AI agent workspace. Dev-pair slots ...`
- `Tier 1 — Remote (CDE). Same dev-pair shape as tier 0 ...`
- `Tier 2 — Local. Single-slot prod runtimes ...`
- `Tier 3 — Stage. Single-slot prod runtimes with 0.25 GB minFreeRamGB headroom ...`
- `Tier 4 — Small Production. minContainers: 2 on every runtime ...`
- `Tier 5 — Highly-available Production. Dedicated CPU, SERIOUS corePackage ...`

Compare sim-3 (which led with porter-card descriptions: "Disposable
workspace where AI coding agents iterate on the api, frontend, and
worker..."). Run-34 reverted to **the exact run-33 baseline shape**.

### Tier import.yamls — leads with scaling math

Stage tier per-service yaml comments (run-34):
- db: "Single-instance PostgreSQL — restoring from snapshot still means downtime, the rehearsal-grade tradeoff. minFreeRamGB 0.25 absorbs query-burst spikes; bump verticalAutoscaling.minRam if working-set growth pushes query latency past your stage SLO."

Sim-3:
- db: "Single-node PostgreSQL — used by the api codebase to store items + job logs. Snapshots-only durability at this rehearsal tier; bump `verticalAutoscaling.minRam` if your stage workload pushes query latency."

Jetstream golden:
- db: "Deploy single node PostgreSQL database, used by the Laravel app to store data. Automatic, encrypted backups are enabled by default."

Run-34 leads with the operational tradeoff + scaling math — exactly the
TY2 BAD example the substrate teaches against. Worker + service-name
preambles (e.g. "NestJS HTTP API on stage") do lead with role, but
managed-service comments (db / cache / broker / search) lead with
scaling math.

### Root README — at-bar with sim-3

R1-R6 shape clean: intro extract → deploy button → cover → 6-tier list
→ catalog punt → Discord. No `## H2` content sections. 25 lines.

---

## Untested-in-sim items verdict

### Change 4 (fact-recording voice teaching) — DID NOT LAND

| Token | Run-33 | Run-34 |
|---|---|---|
| `zerops_dev_server` in `why` | 17 records | **15 records** |
| `the agent` in `why` | 8 records | **2 records** |
| Total strict-token contaminated | 14/103 (13.6%) | **15/117 (12.8%)** |

The brief edits **did land** (codex confirmed:
[`scaffold/decision_recording_slim.md:129-137`](../internal/recipe/content/briefs/scaffold/decision_recording_slim.md#L129-L137)
+ [`feature/decision_recording.md:30-37`](../internal/recipe/content/briefs/feature/decision_recording.md#L30-L37)
both contain the forbidden-tokens block). But **scaffold + feature
agents ignored the rule at runtime** — fact text still leads with
"... is owned by zerops_dev_server" / "... the agent owns the long-running
nest --watch process". The contamination rate is statistically
unchanged from run-33.

### Change 3 (mid-phase stitch + self-review) — PARTIALLY LANDED

cc-content sub-agent disk-Reads of assembled README after scoped complete-phase:
- cc-content-api ([`agent-a5ee308400068c30d.jsonl`](../docs/zcprecipator3/runs/34/SESSION_LOGS/subagents/agent-a5ee308400068c30d.jsonl)): **0 reads** of `/var/www/apidev/README.md`
- cc-content-app: 5 reads of `/var/www/appdev/README.md`
- cc-content-worker: 7 reads of `/var/www/workerdev/README.md`

[`synthesis_workflow.md`](../internal/recipe/content/briefs/codebase-content/synthesis_workflow.md)
teaches grounding on facts/source but **does not explicitly require
the disk-Read step** after scoped complete-phase. The two agents that
did it inferred the workflow; the cc-api agent skipped it. Result:
apidev shipped without the mid-phase audit that catches missing H2
sections + IG6 violations.

This **is exactly the failure mode predicted in the run-33 sim report**
(footnote on Change 3: *"the engine doesn't surface assembled-doc path
in the response payload, so sub-agents in the offline replay simulated
by re-reading their own fragment-new outputs as if assembled. In
production this would land as scoped-`complete-phase` then `Read` from
`<cb.SourceRoot>/README.md`. Worth confirming on next prod dogfood."*).
Production confirmed the gap.

---

## Specific defect classes — verify-absent results

| Class | Run-33 | Sim-3 | Run-34 | Verdict |
|---|---|---|---|---|
| Tier intros leading with `Tier N — ...` | 6 | 0 | **6** | **regressed to run-33** |
| `${<host>_zeropsSubdomain}` in porter prose | 7 | 0 | **3** (all in trap-explanation context) | partial regression |
| KB describing already-fixed problems | yes (3 cases) | 0 | **0** (all KB items are post-deploy traps) | at-bar |
| `[Zerops <slug-stem> reference/guide/service]` link text | 7 | 0 | **0** | at-bar |
| Cross-recipe references (`parent recipe nestjs-minimal`) | 1 | 0 | **0** | at-bar |
| IG6 violations (alias-IG; generic best practice) | 1 (worker) | 0 | **2 (apidev IG#4 + worker IG#4)** | **regressed worse than run-33** |

The IG6 regression is striking: run-33 had ONE alias-IG (worker only).
Sim-3 cleanly eliminated it. Run-34 has TWO — one in api, one in
worker. The substrate explicitly teaches against this class.

---

## Side-by-side voice comparison vs jetstream

| Surface | Jetstream golden | Run-34 | Verdict |
|---|---|---|---|
| Tier-3 README intro | "**Stage** environment uses the same configuration as production, but runs on the lowest scaling settings." (delta-shape) | "Tier 3 — Stage. Single-slot prod runtimes with 0.25 GB minFreeRamGB headroom across every service so rehearsal load spikes don't trigger swap thrash..." | **below** — Tier prefix + scaling-math |
| Tier-3 db comment | "Deploy single node PostgreSQL database, used by the Laravel app to store data. Automatic, encrypted backups are enabled by default." (plain-English service intro + framework bridge) | "Single-instance PostgreSQL — restoring from snapshot still means downtime, the rehearsal-grade tradeoff. minFreeRamGB 0.25 absorbs query-burst spikes..." | **below** — operational-engineer voice |
| apps-repo H2 structure | `## Integration Guide` / `## Understand Zerops Core Concepts` / `## Recipe features` / `## Production vs. Development` / `## Tips and Others` | apidev: `## Integration Guide` / `## Tips and Others` only | **below** — three sections missing |
| apidev/CLAUDE.md | framework-only, `claude /init`-shape | framework-only, `claude /init`-shape | **at-bar** |

---

## Top 5 surprises

### 1. Refinement env-intro replaces silently no-op (engine persistence gap)

Refinement called `record-fragment mode=replace` for env/0/intro through
env/5/intro with NEW bodies (e.g. env/0 → "An AI-agent workspace tuned
for cheap iterate-and-discard cycles..."). Engine returned `ok:true`
for each. **But the published env tier READMEs still show the original
priorBody** ("Tier 0 — AI agent workspace...").

Root cause:
[`handlers.go:715-733`](../internal/recipe/handlers.go#L715-L733) wraps
refinement-replace + re-stitches **only when `host != ""`** (i.e. for
codebase fragments). [`preStitchCodebases`](../internal/recipe/handlers.go#L1236)
is the only re-stitch path; there is no `preStitchEnv` equivalent. Env
fragments enter the engine's session state but never propagate to disk
after refinement modifies them. Refinement spent 6 ACTs on tier-prefix
intros and **none of them landed**. This single bug accounts for the
entire tier-intro regression.

### 2. cc-content-api skipped the mid-phase disk-Read; apidev shipped without RF1+PD1

cc-content-api recorded the un-slotted `codebase/api/integration-guide`
fragment containing `## Understand Zerops Core Concepts` + `## Recipe features`
+ `## Production vs. Development` (engine returned `ok:true` with
`bodyBytes:1684`). Sibling cc-content-app + cc-content-worker did
similar un-slotted appends and **theirs stitched**. Apidev's didn't.

The asymmetry traces to the mid-phase disk-Read: cc-content-api made
**zero** Read calls to `/var/www/apidev/README.md` during its run,
whereas cc-content-app made 5 and cc-content-worker made 7. Whatever
the un-slotted-IG-stitch interaction is, the agents that did the
disk-Read also got their content into the assembled README; the agent
that skipped didn't. The brief
[`synthesis_workflow.md`](../internal/recipe/content/briefs/codebase-content/synthesis_workflow.md)
teaches grounding-on-facts but doesn't *explicitly require* the
post-scoped-complete-phase disk-Read — leaving compliance to agent
inference.

### 3. Refinement loads contradictory teaching (rubric-walk + rule-walk)

Codex-verified:
- [`briefs/refinement/synthesis_workflow.md:3-14`](../internal/recipe/content/briefs/refinement/synthesis_workflow.md#L3-L14): teaches rule-walk against derived_rules; explicitly retires the 5-criteria rubric.
- [`phase_entry/refinement.md:28-39`](../internal/recipe/content/phase_entry/refinement.md#L28-L39): still teaches **"You apply the rubric (5 criteria × 3 anchors each... Score against each rubric criterion"**. Read order at line 99-103 still cites `embedded_rubric.md`.

Change 2 was supposed to delete `embedded_rubric.md` from the brief and
flip refinement to rule-walk. The synthesis_workflow update landed; the
phase_entry update didn't. Refinement received contradictory scoring
instructions. Evidence-of-confusion: refinement made 11 replace ACTs on
env intros + KB shape (the rubric-shape signals) but missed RF1/PD1
absence on apidev (rule-shape signal — RF1 specifically named in
derived_rules but not in embedded_rubric).

### 4. Cross-codebase env-coherence regressed from sim-3

apidev uses `S3_*` prefix for object-storage; workerdev uses `STORAGE_*`.
apidev uses `MEILI_*` (referencing `${search_defaultAdminKey}`);
workerdev uses `SEARCH_*` (referencing `${search_masterKey}`). Both
target the same managed services. Sim-3 had aligned these.

Counter #1 explicitly catches this; the validator didn't fire. Either
the validator's been quiet since run-32 reactivated or Counter #1's
upstream gate isn't on the cc-content `complete-phase` path. Worth
confirming the validator is actually wired.

### 5. yaml comment density worse than run-33 across all three codebases

| Codebase | Run-33 | Sim-3 | Run-34 |
|---|---|---|---|
| apidev | 50.0% | 38.9% | **45.5%** |
| appdev | 59.4% | 38.4% | **61.7%** |
| workerdev | 51.8% | 36.1% | **48.3%** |

**appdev is worse than run-33** (61.7% vs 59.4%). Sim-3 had cleanly
trimmed all three; run-34 reverted apidev/workerdev partway and pushed
appdev past the run-33 baseline. Y15 target is ≤40%.

---

## Diagnosis — what shipped vs what landed

| Change shipped (v9.76.0) | Brief edit landed | Production semantics landed | Audience-model defect closed |
|---|---|---|---|
| 1 — Wire derived_rules into cc/env/finalize | yes | yes (cc-content briefs include rules-from-goldens part) | partial (some classes closed in app+worker; apidev regressed) |
| 2 — Refinement rule-walk | half (synthesis_workflow updated; phase_entry NOT updated) | conflicted (mixed signals) | NO (refinement still rubric-shape) |
| 3 — Mid-phase stitch | brief teaches grounding, doesn't require disk-Read | partial (2 of 3 cc-content sub-agents did it; cc-api skipped) | NO for api codebase |
| 4 — Forbidden tokens in fact text | yes | NO (agents ignored at runtime; 12.8% contamination unchanged) | NO |
| 5 — Y13 in derived_rules | yes | yes (Y13-shape comments visible in apidev yaml) | yes (Y13 fact-pattern present) |
| Voice-mix substrate edit (V1/Y2/IG2/TY2/TY4) | yes | NO (managed-service yaml comments still lead with scaling math) | NO |
| RF1/PD1 teaching for api codebase | yes (visible in cc-api brief dispatch) | NO (un-slotted IG record `ok:true` but didn't stitch on apidev) | NO |

**Three of seven shipped changes failed to land in production semantics.**
Two more landed brief-side but didn't move agent behavior at runtime.
Only Y13 + Change 1 (partially) actually moved the needle.

---

## Recommended next step — structural revisit (NOT substrate iteration)

The substrate landed as designed. The audience-model regression is
**not a content-quality teaching gap**; it's three concrete engine /
brief-wiring gaps:

1. **`preStitchEnv` parity for refinement-time env-fragment writes.**
   Mirror [`preStitchCodebases`](../internal/recipe/handlers.go#L1236)
   for env fragments so refinement-replace of `env/N/intro` (and
   `env/N/import-comments/<host>`) actually persists to disk. Without
   this, every env-fragment refinement ACT is a silent no-op.
   Cost: ~1-2 hours engine + tests. **Highest priority** — this
   accounts for the entire tier-intro regression and any future
   refinement work on env surfaces is dead.

2. **Finish Change 2: update `phase_entry/refinement.md`.** Delete or
   rewrite lines 28-39 (rubric-walk teaching) and lines 99-103
   (embedded_rubric in read order). The synthesis_workflow.md edit was
   half the change; the phase-entry atom is the load-bearing teaching
   the agent reads first. Cost: ~30 min atom edit + test fixture
   update.

3. **Make Change 3 mandatory, not advisory, in
   `briefs/codebase-content/synthesis_workflow.md`.** Add an explicit
   step: "After scoped `complete-phase codebase=<self>`, you MUST
   `Read` `<cb.SourceRoot>/README.md` from disk and walk every rule
   in derived_rules.md against the assembled document. ACT via
   `record-fragment mode=replace` on every violation found." Make it
   a numbered required step, not "you may also". Cost: ~1 hour brief
   edit + sim verification.

4. **Investigate the un-slotted IG stitch asymmetry on apidev.** Why
   did appdev + workerdev's un-slotted `codebase/<host>/integration-guide`
   appends stitch into the published README, while apidev's didn't?
   Same `ok:true` response from the engine; same fragment shape. The
   asymmetry might be classification-routing, codebase-order, or some
   other engine-side bug. Cost: investigation; can't estimate
   without dig.

5. **Add an engine-side runtime gate for the Change 4 forbidden-tokens.**
   The brief landed but agents ignore it. Move enforcement from "brief
   teaches" to "engine rejects record-fact when `why`/`mechanism`/etc.
   contain forbidden tokens" — analogous to how Counter #1's S3_/STORAGE_
   regression should be caught at the cc-content `complete-phase` gate.
   Cost: ~1-2 hours engine + tests.

After these five land, **sim-replay against the captured run-34 facts
before paying for run-35.** The captured run-34 input now exercises
every defect class run-33 + sim-3 didn't (because run-34 produced the
data that triggers the unfixed paths).

If sim-replay against run-34 facts shows the regressions close, run-35
becomes the validation gate. If they don't close, the next layer of
diagnosis is needed — but the data above suggests the gaps are concrete
and fixable, not "the substrate is wrong".

---

## What we're NOT doing

| Not doing | Why |
|---|---|
| More substrate iteration | The substrate landed and works where wired; the gaps are wiring + persistence, not teaching. Adding more rules without fixing the wiring is dead inventory. |
| Running run-35 before fix-pack | The captured run-34 facts now exercise the unfixed code paths. Sim-replay is faster, cheaper, and faithful per the verified emit pipeline. |
| Re-deriving the diagnosis | The 32→33 architectural diagnosis stands. Run-34 surfaces engine + brief-wiring gaps that the architectural-fixes plan implicitly assumed were done. |
| Iterating on apidev RF1/PD1 voice | The content was authored correctly (`bodyBytes:1684`, `ok:true`). The bug is downstream of the agent. Don't blame the agent for an engine stitch gap. |
| Adding more counters | Counter #1 already catches the S3_/STORAGE_ mismatch; it didn't fire because the validator isn't on the right path. Fix the wiring of existing counters before adding new ones. |

---

## Verdict

**v9.76.0 is NOT production-ready.** Three architectural gaps + one
brief-half-landing + one runtime-enforcement-missing combine to
reintroduce most of run-33's audience-model defects on a fresh run,
while the sim-3 wins (RF1/PD1 on apidev, clean tier intros) silently
disappear at the engine boundary.

The path forward is the 5-step fix-pack above (engine + brief edits;
no new substrate). Each step is independently verifiable in sim
against the captured run-34 facts. After all five land, sim-replay
first; only then run-35.
