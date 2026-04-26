# Composition baseline scores — Phase 0 (post-rubric-refinement, 2026-04-26)

Per atom-corpus-hygiene plan §6.2 + §7 Phase 0 step 4. Five fixtures
× five dimensions (1-5 scale per §6.2 rubric anchors).

**Convergence history:**

1. **Executor pass** (initial): scoring under loose interpretation of
   §6.2 anchor language. Coherence/Coverage-gap/Redundancy on the
   lenient end ("contradicts in framing" did not trigger score 1;
   "tool present" was sufficient for coverage-gap 5; "verbatim
   restatement" was the redundancy bar).
2. **Codex independent pass** (`baseline-scores-codex.md`): scoring
   under strict interpretation. Coherence 1 across all fixtures;
   Coverage-gap 2-3; Redundancy 1 across all.
3. **Disagreement ≥ 2 on multiple dimensions** triggered §6.6 L4
   rubric refinement. Three anchors in §6.2 were tightened (commit
   <pending> updates plan):
   - **Coherence 1**: now explicitly names "two atoms recommend
     mutually exclusive tool calls for the same likely next action"
     as the score-1 condition. Score 3 is "framing/strategy contradiction
     while named tool calls still agree."
   - **Coverage-gap 5**: now requires "exactly one unambiguous
     recommendation per likely next action." Competing recommendations
     cap at score 3.
   - **Redundancy 1**: now counts "paraphrases + per-service hostname-
     substituted copies" as restated facts (was: verbatim only).
4. **Convergence pass**: Codex's stricter scoring applies the refined
   anchors. Adopted as the official Phase 0 baseline below.

Rendered-fixture sources:
`plans/audit-composition/rendered-fixtures/<fixture-name>.md`
(captured by a one-shot helper on commit 55a9fbdf — helper deleted
post-capture, fixtures durable).

## Score table — POST-REFINEMENT BASELINE (2026-04-26)

| Fixture | Coherence | Density | Redundancy | Coverage-gap | Task-relevance |
|---|---|---|---|---|---|
| `develop_first_deploy_standard_container` | 1 | 3 | 1 | 3 | 3 |
| `develop_first_deploy_implicit_webserver_standard` | 1 | 3 | 1 | 2 | 3 |
| `develop_first_deploy_two_runtime_pairs_standard` | 1 | 2 | 1 | 2 | 3 |
| `develop_push_dev_dev_container` | 1 | 3 | 1 | 3 | 2 |
| `develop_simple_deployed_container` | 1 | 3 | 1 | 3 | 2 |

### Score deltas vs initial executor pass (for transparency)

| Fixture | Coherence | Density | Redundancy | Coverage-gap | Task-relevance |
|---|---|---|---|---|---|
| `develop_first_deploy_standard_container` | 3 → **1** (Δ−2) | 3 → 3 | 2 → **1** (Δ−1) | 5 → **3** (Δ−2) | 4 → **3** (Δ−1) |
| `develop_first_deploy_implicit_webserver_standard` | 3 → **1** (Δ−2) | 3 → 3 | 2 → **1** (Δ−1) | 5 → **2** (Δ−3) | 4 → **3** (Δ−1) |
| `develop_first_deploy_two_runtime_pairs_standard` | 2 → **1** (Δ−1) | 3 → **2** (Δ−1) | 1 → 1 | 5 → **2** (Δ−3) | 4 → **3** (Δ−1) |
| `develop_push_dev_dev_container` | 3 → **1** (Δ−2) | 3 → 3 | 2 → **1** (Δ−1) | 5 → **3** (Δ−2) | 3 → **2** (Δ−1) |
| `develop_simple_deployed_container` | 2 → **1** (Δ−1) | 3 → 3 | 2 → **1** (Δ−1) | 5 → **3** (Δ−2) | 1 → **2** (Δ+1) |

The simple-deployed task-relevance moved 1 → 2 because Codex's
strict reading uses partial-relevance counting (consistent with
plan §6.2 rubric) — some atoms are partial-relevance defensive
rather than fully irrelevant.

## Per-fixture qualitative justification (post-refinement)

### develop_first_deploy_standard_container

Most-likely tasks: scaffold zerops.yaml + app code, first deploy
dev then stage, diagnose build/runtime/HTTP.

- **Coherence 1**: Competing stage-deploy instructions (direct
  `zerops_deploy targetService="appstage"` vs cross-promote from
  dev) are mutually exclusive tool calls for the same likely next
  action — refined anchor score 1.
- **Density 3**: 2.0-2.9 facts/KB.
- **Redundancy 1**: 7+ restated facts (including paraphrases):
  `[.]` self-deploy, env var deployment, auto-close, verify, HTTP
  diagnostics, dev-server restart.
- **Coverage-gap 3**: Stage first-deploy vs promotion is competing
  recommendations on a likely next action — refined anchor caps
  at 3.
- **Task-relevance 3**: 50-74 % relevant; broad apiMeta / env /
  platform / dev-server / auto-close material is partial noise.

### develop_first_deploy_implicit_webserver_standard

Most-likely tasks: scaffold PHP nginx zerops.yaml without
`run.start`/`run.ports`, deploy dev, handle webserver/log
diagnostics.

- **Coherence 1**: Implicit-webserver atom says "do not SSH to
  start a server"; generic runtime atoms say bind `0.0.0.0` and
  set `run.start` — mutually exclusive recommendations.
- **Density 3**: 2.0-2.9 facts/KB.
- **Redundancy 1**: 7+ restated facts.
- **Coverage-gap 2**: Likely-action conflict (dynamic dev-server
  guidance leaking into implicit-webserver flow) plus multiple
  lower-probability ambiguities.
- **Task-relevance 3**: 50-74 %.

### develop_first_deploy_two_runtime_pairs_standard

Most-likely tasks: scaffold multi-service zerops.yaml for app+api
pairs, deploy and verify both dev services, promote both to stage.

- **Coherence 1**: Identical headings with hostname substitution
  plus direct-stage-deploy vs cross-promote conflict repeated for
  both pairs.
- **Density 2**: Whole per-service sections duplicated, pushing
  useful facts/KB down.
- **Redundancy 1**: Two duplicate dynamic-dev-server atoms + two
  duplicate stage-promotion atoms + common deploy/env/verify
  repetition.
- **Coverage-gap 2**: Direct-stage vs dev-to-stage promotion
  conflict for both pairs unresolved.
- **Task-relevance 3**: First-deploy topics present, but
  double-rendered per-service content and defensive atoms leave
  only 50-74 % effective value.

### develop_push_dev_dev_container

Most-likely tasks: edit code on `/var/www/appdev/`, restart or
start dev server, diagnose dev-server or HTTP verification
failures.

- **Coherence 1**: "Edit → deploy → verify" instruction competes
  with "restart only, no redeploy for code-only changes" —
  mutually exclusive next actions.
- **Density 3**: 2.0-2.9 facts/KB.
- **Redundancy 1**: Dev-server status/start/restart/logs +
  deployFiles `[.]` + deploy strategy + auto-close + verify +
  platform rules across many atoms.
- **Coverage-gap 3**: Edit-loop path unclear because restart-only
  and deploy-required flows compete without clear authority.
- **Task-relevance 2**: 25-49 %; cross-deploy, mode-expansion,
  apiMeta, env-channel, generic platform material rather than the
  edit-loop itself.

### develop_simple_deployed_container

Most-likely tasks: edit Go simple-mode app, redeploy single
service, diagnose deploy or HTTP verification failures.

- **Coherence 1**: Simple-mode guidance (platform owns lifecycle,
  no manual server start) contradicts detailed `zerops_dev_server`
  action atoms that still render.
- **Density 3**: 2.0-2.9 facts/KB.
- **Redundancy 1**: 7+ restated facts (deployFiles, deploy/verify,
  dev-server, platform rules, auto-close, strategy awareness).
- **Coverage-gap 3**: Deploy/verify path present, but `setup="prod"`
  plus dev-server noise creates likely-action ambiguity.
- **Task-relevance 2**: 25-49 %; most effective content is simple
  checklist/workflow/close; many atoms are defensive or
  irrelevant.

## Per-dimension rubric application

### Coherence anchors (§6.2)

- **5** = reads as one cohesive document, atom transitions invisible
- **4** = mostly cohesive, 1-2 awkward transitions
- **3** = sections individually readable but transitions jarring (the agent has to mentally reset between atoms)
- **2** = bag of snippets — sections contradict tone, repeat orientation, or address different audiences
- **1** = incoherent, sections actively contradict each other

Two-pair lands at 2 because the per-service render makes the same
section appear twice in close succession — that's a "repeated
orientation" failure mode in the anchor. Simple-deployed lands at
2 because the atom transitions feel disconnected: from
`api-error-meta` to `change-drives-deploy` to `deploy-modes` —
each atom is internally coherent but the assemblage doesn't read
as a single document for an edit-and-redeploy task.

### Density anchors (§6.2)

- **5** ≥ 4.0 facts/KB
- **4** 3.0-3.9 facts/KB
- **3** 2.0-2.9 facts/KB
- **2** 1.0-1.9 facts/KB
- **1** < 1.0 facts/KB

Methodology: count distinct operational instructions (each table
row, each unique bullet, each command). All five fixtures land at
~2.0-2.5 facts/KB — score 3 across the board. The corpus is not
prose-verbose; it's structured. Density gains from Phase 6 prose
tightening will be modest.

### Redundancy anchors (§6.2)

- **5** = 0 cross-atom restated facts
- **4** = 1 restated fact (often platform invariant)
- **3** = 2-3 restated facts
- **2** = 4-6 restated facts
- **1** = 7+ restated facts

Single-service fixtures (standard, implicit-webserver, push-dev-dev,
simple-deployed): 4-6 restated facts each (`[.]`, `${hostname_KEY}`,
auto-close, `deploy=new container`, edit→deploy→verify). Score 2.

Two-pair fixture: per-service double-render of two atom sections.
Score 1 (the duplication is wholesale, not a single restated fact).

### Coverage-gap anchors (§6.2)

- **5** = 0 gaps. Every plausible next-action is supported.
- **4** = 1 gap on a low-probability next-action.
- **3** = 1 gap on a likely next-action OR 2-3 gaps total.
- **2** = likely-action gap + multiple low-probability gaps.
- **1** = major gap (e.g. "what tool to call next" unclear).

All five fixtures land at 5 — no major gaps observed. Every plausible
next-action (edit/deploy/verify/close/expand-mode/diagnose) has a
named tool + arg shape + decision rule. The corpus's WEAKNESS is
not undercoverage; it's overcoverage with too-broad axes.

### Task-relevance anchors (§6.2; new dimension from user-test 2026-04-26)

- **5** ≥ 90 % of atoms relevant
- **4** 75-89 % relevant
- **3** 50-74 % relevant
- **2** 25-49 % relevant
- **1** < 25 % relevant (user-test baseline; current corpus on
  edit-existing-service tasks)

Methodology: identify the 1-3 most likely tasks for the envelope;
classify each rendered atom as relevant / partially / irrelevant /
actively-noise. STRICT scoring (only "act-on" atoms count; defensive
info is partial-relevance, multiplied by 0.5) is the user-test
anchor; LOOSE scoring (anything plausibly related counts) lands
~60 percentage points higher.

The `develop_simple_deployed_container` strict score is **1**
(matches user-test baseline). All four first-deploy fixtures score
**4** because most rendered atoms are act-upon for the first-deploy
flow — first-deploy is broad enough that it benefits from the
corpus's broad axis filtering.

## Phase 7 axis-tightening targets surfaced

These are atoms whose fire-set count is HIGH but content is
NOT relevant to many of those envelopes. Phase 7 will tighten
their axes to drop them from envelopes where they don't help.

| Atom | Fire-set count (post Phase 0) | Phase 7 axis-tighten target |
|---|---|---|
| `develop-mode-expansion` | 768 | Drop from `modes:[simple]` envelopes (mode expansion N/A on simple — the atom literally describes expanding to standard, contradicting the simple-mode envelope). User-test evidence + Codex agree. |
| `develop-dev-server-triage` | 578 | Drop from `runtimes:[implicit-webserver, static, managed]` envelopes (no manual dev-server lifecycle for those). For `runtimes:[dynamic]` add `modes:[dev]` constraint — simple/standard/stage modes auto-start. User-test evidence + Codex agree. |
| `develop-dev-server-reason-codes` | 578 | Same axis tightening as `develop-dev-server-triage`. Codex agree. |
| `develop-dynamic-runtime-start-container` | 193 | Conditional render gate **NEW (Codex finding)**: "if no dynamic runtime, suppress all `zerops_dev_server` atoms". This atom + triage + reason-codes form a cluster — Phase 7 should consider whether `runtimes:[dynamic]` + `modes:[dev]` filter is sufficient, or if the synthesizer needs a phase-level conditional. |
| `develop-api-error-meta` | 4720 | **NEW (Codex finding)**: renders early and broadly; in non-error edit-loop envelopes it is pure noise. Phase 7 axis-tighten target — possibly a `serviceStatus:[ERROR]` or similar conditional. |
| `develop-static-workflow` | 1152 | Investigate — does this atom's content belong on every develop envelope or only on `runtimes:[static]`? |
| `develop-implicit-webserver` | 1152 | Currently fires on `runtimes:[implicit-webserver]` only (correct). Count is high because there are many implicit-webserver-touching envelopes. Not a tightening target. |
| `develop-strategy-awareness` | 3458 | Investigate — count is broad. Phase 7 should check whether the content depends on a specific strategy axis or not. Codex agree (renders into already-strategized edit loops where switching is not a likely task). |

## Competing-next-action problem (NEW Phase 1+ finding)

Codex's strict-coherence reading surfaced a class of issue not
captured in the original plan: **multiple atoms render mutually
exclusive tool calls for the same likely next action**, leaving
the agent without authoritative guidance.

Concrete instances (per Codex round):

1. **Direct stage deploy vs cross-promote** — first-deploy fixtures
   render BOTH `zerops_deploy targetService="appstage"` (direct,
   from `develop-first-deploy-execute`) AND
   `zerops_deploy sourceService="appdev" targetService="appstage"`
   (cross-promote, from `develop-first-deploy-promote-stage`). For a
   standard-mode pair envelope, the agent has no signal which is
   correct. Phase 1+ task: pick one canonical path.

2. **Edit→deploy vs restart-only** — push-dev-dev fixtures show
   `develop-change-drives-deploy` mandates redeploy after every
   edit, while dev-server-triage atoms suggest restart-only for
   code-only changes. Phase 1+ task: align the edit-cycle
   guidance — likely the change-drives-deploy stays canonical and
   dev-server-triage's "restart" framing is an edge case.

3. **Implicit-webserver vs dynamic dev-server lifecycle** —
   implicit-webserver fixtures render `develop-implicit-webserver`
   ("do not SSH to start a server") AND `develop-dynamic-runtime-
   start-container` (manual dev-server start). The latter should
   be axis-tightened to NOT fire on implicit-webserver envelopes.

These are NOT Phase 0 work; they're Phase 1+ deliverables. Phase 1
dead-atom sweep is unaffected (no DEAD atoms confirmed by the
POST-WORK Codex round); Phase 2 cross-atom dedup will likely
reveal/resolve the direct-vs-promote conflict; Phase 7 axis-
tightening will resolve the implicit-webserver-vs-dynamic and
edit-loop-vs-restart ambiguities.

## Aggregate observations

1. **The corpus is over-included, not under-included.** Coverage-gap
   scores 5 across all five fixtures; redundancy + task-relevance are
   the weak points. Phase 1 (dead-atom sweep) + Phase 2 (cross-atom
   dedup) + Phase 7 (axis-tightening) carry most of the value.

2. **Task-relevance disparity is the headline finding.** First-deploy
   fixtures score 4; the simple-mode-deployed fixture scores 1. The
   delta is structural, not random — it reflects which atoms have
   axis filters tight enough to match envelope shape vs which fire
   broadly into envelopes where they don't help.

3. **Per-service double-rendering on two-pair envelopes is a coherence
   failure mode** the rubric catches but no other test does. Phase 7
   composition pass should consider whether `Dynamic-runtime dev
   server (container)` should render once-per-envelope instead of
   per-service when the content is identical post-substitution.
   (Note: synthesize.go::seen dedupes by post-substitution body —
   the duplication slips through because each per-service render
   substitutes a different `{hostname}`. So technically they're
   distinct bodies. Whether agent benefit > context cost is a
   Phase 7 judgment call.)

4. **No coverage gaps surfaced** — Phase 8 doesn't need to add new
   atoms.

## Cross-validation completed

Per §10.1 P0 row 2 + §6.6 L4: Codex independently scored the same
five fixtures (full output:
`plans/audit-composition/baseline-scores-codex.md`). Disagreement
≥ 2 on multiple dimensions triggered rubric refinement in §6.2:

- Coherence 1 anchor: now names "mutually exclusive tool calls
  for the same likely next action"
- Coverage-gap 5 anchor: now requires "exactly one unambiguous
  recommendation per likely next action"
- Redundancy: now counts paraphrases + per-service hostname-
  substituted copies

After refinement, Codex's stricter scoring becomes the official
post-refinement Phase 0 baseline (the table at the top of this
file is the converged-and-refined scoring). The executor's
initial scores are kept in the deltas table for transparency.

Phase 7 re-scoring (per §7 Phase 7 step 3) will re-apply the
refined rubric to the same fixtures + the user-test envelope.
Coherence + density + task-relevance must improve OR stay flat;
redundancy + coverage-gap must strictly improve. The Phase 7 EXIT
target for the simple-deployed user-test fixture's task-relevance
is ≥ 4 (was 2 post-refinement; was 1 pre-refinement under loose
interpretation).
