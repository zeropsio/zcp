# Composition baseline scores — Phase 0 (executor pass, 2026-04-26)

Per atom-corpus-hygiene plan §6.2 + §7 Phase 0 step 4. Five fixtures
× five dimensions (1-5 scale per §6.2 rubric anchors). Scoring done
by reading the full rendered output of each fixture against the
rubric. Per §6.6 L4 cross-validation, Codex CORPUS-SCAN round 2
(§10.1 P0 row 2) re-scores independently — disagreements ≥ 2 on
any dimension trigger rubric refinement.

Rendered-fixture sources:
`plans/audit-composition/rendered-fixtures/<fixture-name>.md`
(captured by a one-shot helper on commit 55a9fbdf — helper deleted
post-capture, fixtures durable).

## Score table

| Fixture | Coherence | Density | Redundancy | Coverage-gap | Task-relevance | Notes |
|---|---|---|---|---|---|---|
| `develop_first_deploy_standard_container` | 3 | 3 | 2 | 5 | 4 | 23 atoms / 27228 B / 26.6 KB. Most atoms task-critical (scaffold, write-app, deploy, verify, promote-stage, dev-server-start). Restated facts: `[.]` in 3 atoms; `${hostname_KEY}` in 2 atoms; auto-close in 3 atoms. Transitions OK except apiMeta jumps in early. |
| `develop_first_deploy_implicit_webserver_standard` | 3 | 3 | 2 | 5 | 4 | 24 atoms / 28869 B / 28.2 KB. Adds `Implicit-Webserver Runtime` + `Dev/simple + frontend asset pipeline` atoms; drops `Dynamic-runtime dev server`. Same restatement profile as standard. |
| `develop_first_deploy_two_runtime_pairs_standard` | 2 | 3 | 1 | 5 | 4 | 29 atoms / 30028 B / 29.3 KB. **Per-service double-render**: `Dynamic-runtime dev server (container)` and `Promote the first deploy to stage` BOTH appear twice (once per runtime). Coherence drops to 2 because reader hits the same heading twice in the same document. Redundancy drops to 1 because of the double-render. Task-relevance stays high — both pairs need their own scaffold/deploy. |
| `develop_push_dev_dev_container` | 3 | 3 | 2 | 5 | 3 | 21 atoms / 26588 B / 26.0 KB. Post-deploy edit loop for dev-mode standard service. Task-relevance is 3 not 4 because some atoms aren't act-upon for an EDIT cycle: `Two deploy classes, one tool` (cross-deploy details), `Self-deploy invariant`, `Cross-deploy has opposite semantics` are info, not act-on. Mode-expansion atom fires here despite envelope already being standard — one unneeded atom, but small. |
| `develop_simple_deployed_container` | 2 | 3 | 2 | 5 | **1** | 20 atoms / 23258 B / 22.7 KB. **User-test 2026-04-26 anchor**: only ~3 atoms are STRICTLY act-upon for "edit a simple-mode Go service + deploy" task — `develop-push-dev-workflow-simple` (the deploy command pair), `develop-checklist-simple-mode` (healthCheck reminder), `develop-close-push-dev-simple` (close commands). 5+ atoms are partial-relevance defensive (apiMeta, env-channels, http-diagnostic). 3 atoms are IRRELEVANT for this task: `develop-dev-server-triage` (1908 B — simple-mode auto-starts, no manual dev-server lifecycle), `develop-dev-server-reason-codes` (1264 B — dev-server tool not used in simple mode), `develop-mode-expansion` (1282 B — user is editing existing service, not expanding). Total irrelevant noise: ~4500 B / 22.7 KB ≈ 20 % pure waste. |

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
| `develop-mode-expansion` | 768 | Drop from `modes:[simple]` envelopes (mode expansion N/A on simple — the atom literally describes expanding to standard, contradicting the simple-mode envelope). User-test evidence. |
| `develop-dev-server-triage` | 578 | Drop from `runtimes:[implicit-webserver, static, managed]` envelopes (no manual dev-server lifecycle for those). For `runtimes:[dynamic]` add `modes:[dev]` constraint — simple/standard/stage modes auto-start. User-test evidence. |
| `develop-dev-server-reason-codes` | 578 | Same axis tightening as `develop-dev-server-triage`. |
| `develop-static-workflow` | 1152 | Investigate — does this atom's content belong on every develop envelope or only on `runtimes:[static]`? |
| `develop-implicit-webserver` | 1152 | Currently fires on `runtimes:[implicit-webserver]` only (correct). Count is high because there are many implicit-webserver-touching envelopes. Not a tightening target. |
| `develop-strategy-awareness` | 3458 | Investigate — count is broad. Phase 7 should check whether the content depends on a specific strategy axis or not. |

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

## Cross-validation handoff to Codex CORPUS-SCAN round 2

Per §10.1 P0 row 2, Codex independently scores the same five
fixtures using the same rubric. The Codex output must arrive at:

- score-by-score deltas vs this baseline (acceptable: ±1 per
  dimension; ≥2 disagreement triggers rubric refinement per §6.6 L4)
- agreement on the Phase 7 axis-tightening targets (or counter-
  proposals)
- agreement on the aggregate observations (or counter-narratives)

Save Codex output as
`plans/audit-composition/baseline-scores-codex.md` (per §10.1).

If ≥ 2 disagreement on any dimension, refine the rubric anchor in
§6.2 (commit) and both executors re-score before treating any
score as ground truth.
