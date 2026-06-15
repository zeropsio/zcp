# Refinement phase

## Main-agent orchestration (read this before dispatching)

Run-43 Edit D / P6 consolidates the refinement state machine. The
flow at this phase is:

1. **Dispatch refinement-1** via `build-subagent-prompt
   briefKind=refinement`. The sub-agent walks the per-fragment rule
   substrate (`derived_rules.md`) over every stitched fragment;
   transactional snapshot/restore wraps each `record-fragment
   mode=replace` so a regression-causing edit reverts. Refinement-1's
   ACTs are recorded internally — main agent does NOT triage between
   refinement-1 and refinement-2; refinement-1's wrapper is the safety
   net.
2. **Dispatch refinement-2** via `build-subagent-prompt
   briefKind=refinement2`. The sub-agent walks cross-surface defect
   classes (KB↔IG duplication, surface-misplacement,
   aspirational-as-current, yaml-comment-content-drift, etc.) and
   emits a JSON findings block — diagnosis-only, NO
   `record-fragment` calls from the sub-agent itself.
3. **Triage refinement-2 findings per-finding.** For each finding
   from the JSON block, decide ACT (apply the fix via
   `record-fragment mode=replace` per `suggestedAction`), HOLD
   (record per-finding reasoning), or ACCEPT (one-sentence note on
   why the audit fired on a borderline that doesn't violate the
   contract). Bulk-HOLD with one-line reasoning is the documented
   failure pattern.
4. **Re-stitch** via `stitch-content` so the ACTs land in the
   deliverable.
5. **Close** via `complete-phase phase=refinement`. The close gate
   refuses unless BOTH `RefinementDispatched` + `Refinement2Dispatched`
   flags are set (run-43 Edit D), AND re-runs the surface validators
   (CodebaseContentGates + EnvGates) so any defect the ACTs
   introduced (e.g. slug-stem leak, dead env, named-constant drift)
   surfaces at close.

Finalize phase 7 does NOT demand refinement dispatch (pre-Edit-D it
did; the dual-gate scheme produced "three refinement passes, wrong
order" runs where refinement-1 ran twice). Finalize is stitch +
validate only; refinement happens HERE, at this phase.

### Dispatch each sub-agent exactly once per recipe

`refinement-1` and `refinement-2` each dispatch **exactly once per
recipe** — NOT once per phase entry. The engine tracks each
dispatch on the session via the `RefinementDispatched` and
`Refinement2Dispatched` flags. Both flags survive context compaction
and are surfaced on every `status` response.

**Before dispatching either sub-agent, read the prior phase's
status response (or call `zerops_recipe action=status`)** and check
the flags. If `RefinementDispatched` is already true, the
refinement-1 pass has already run — do NOT re-dispatch. Same for
`Refinement2Dispatched`. The dispatch-flag gate at refinement-close
enforces "both flags true" before the close succeeds, so the only
way to land at this phase with one flag false is if the sub-agent
genuinely hasn't run yet. **A status check before each dispatch is
the agent-side primitive that closes the run-42 re-dispatch
failure** (forensics §B-3 — run-42 dispatched a third
refinement-class sub-agent because the rulewalk agent thought it
was the first refinement pass and didn't know refinement-2 had
already run).

The five-step flow assumes both flags start false at this phase's
first entry. If the recipe re-enters this phase post-compaction or
after a separate session, the flags carry forward — skip the
dispatch step whose flag is already true and proceed to triage /
re-stitch / close.

---

## Sub-agent contract (everything below)

You are the refinement sub-agent. The recipe has finished phase 7
(finalize stitch + validate). Every fragment is structurally valid;
every cap is satisfied; every classification routing is internally
consistent. Run-17 §9 introduces this phase as the post-finalize
quality refinement pass.

Your job: read the entire stitched output and refine where the edit
threshold holds. Below the threshold, you do not act.

## What you can do

- Replace fragment bodies via `record-fragment mode=replace`.
- Update fact bodies via `replace-by-topic`.
- Read any file under the run output directory.
- Call `zerops_knowledge` for citation lookups.

## What you cannot do

- Author NEW content (no new IG items, no new KB bullets EXCEPT the
  showcase tier supplement explicit case in the workerdev KB —
  queue-group + SIGTERM drain — when the body lacks them).
- Change a fragment's surface (keep the same fragment id).
- Change a fragment's classification.
- Loop on refusal: per-fragment edit cap is 1 attempt.

## How you make decisions

You walk `derived_rules.md` rule-by-rule against every stitched
document. The rules are golden-grounded principles (V1-V6 universal
voice; R1-R6 root README; T1-T4 tier README; TY1-TY5 tier
import.yaml (+object-storage priority); IG1-IG6 apps-repo Integration Guide; KB1/KB3-KB6 KB;
Y1-Y15 zerops.yaml). For every stitched fragment:

1. Read the fragment end-to-end as a porter would — top-to-bottom,
   no special context.
2. Walk every rule that applies to that fragment's surface.
3. ACT (`record-fragment mode=replace`) on every rule violation —
   cite the rule id + the violating phrase + the preserving edit in
   the replacement body.
4. HOLD when the violation is fuzzy (you can't name the rule, the
   exact fragment, or the precise edit cleanly).

Walk every rule against every document REGARDLESS of how the
fragment got there. The rule fires on the OUTPUT shape, not on
whether the agent's facts/source happened to align — a fragment
that scores clean on every Voice phrasing can still violate IG6
(generic best practice the cloned yaml already does) or V6
(authoring vocabulary).

## The refinement edit threshold

ACT when you can cite the violated rule, the exact fragment, and the
preserving edit. HOLD when any of the three is fuzzy.

Bias toward ACT within this threshold. The snapshot/restore wrapper
means a false-positive ACT reverts automatically when the post-replace
validator catches a regression — the cost of a wrong ACT is one
rule re-check, not a published mistake. The pre-run-23 "100%-sure /
hesitate-to-argue" framing drove default-HOLD on every cross-surface
duplication notice and shipped recipes with documented duplication
the rules already named as violations. Run-23 F-27. Run-33
architectural fix #2 retired the legacy 5-criteria rubric in favor
of rule-walk against derived_rules — pattern-shaped anchors missed
principle-shaped failures (audience-model leaks, tier-prefix intros,
slug-stem leakage); rule-walk against the assembled output catches
those.

## Transactional safety

Each `record-fragment mode=replace` against a `codebase/<host>/...`
fragment at this phase is wrapped in a snapshot/restore primitive:
the engine snapshots the prior body before applying your replacement,
runs codebase-surface validators scoped to the named codebase, and
reverts to the snapshot if your replacement introduces a new blocking
violation that wasn't present before. The `Notices` array on the
response carries a `refinement-replace-reverted` entry naming the
violation that triggered rollback — read it to understand why your
edit didn't stick.

For root and env fragments (`root/intro`, `env/<N>/intro`,
`env/<N>/import-comments/<host>`) the wrapper does NOT fire; the
slot-shape refusal at record time is the safety net. Refinement on
those surfaces is best-effort — apply the same rule-walk threshold
(cite the violated rule + the exact fragment + the preserving edit),
and HOLD when any of the three isn't namable.

## Output

A series of `record-fragment mode=replace` and `replace-by-topic`
calls. End with `complete-phase phase=refinement`.

## Dispatch — multi-file pointer

`build-subagent-prompt briefKind=refinement` ALWAYS returns a
multi-file pointer:

- `response.prompt` is empty.
- `response.briefPath` is the absolute path to `index.md` under
  `<outputRoot>/.briefs/refinement-phase-<unixnano>/`.

The index lists N part files (phase-entry, synthesis-workflow,
rules-from-goldens, references, context, facts) in a "Read order"
section. The sub-agent dispatch wrapper MUST instruct: "Read
`<briefPath>` first; then Read each part file listed in its 'Read
order' section in the order shown before authoring any refinement."
Dispatch with `subagent_type="general-purpose"` — do NOT use
`subagent_type="claude"` (FleetView's default when unspecified).
`claude` triggers worktree isolation on dispatch, which fails on the
non-git recipe-authoring outputRoot and breaks the shared
`zerops_recipe` MCP state. Run-31 Fix #1 closure — multi-file shape
isolates the rule substrate and the recorded-facts stream so neither
crowds the brief past the Read-tool 25K-token cap.

## Read order

1. `phase_entry/refinement.md` — this atom.
2. `briefs/refinement/synthesis_workflow.md` — refinement actions,
   classification × surface table, surface-by-surface decision rules.
3. `briefs/refinement/derived_rules.md` — golden-grounded
   principle-shaped rules. The scoring substrate — walk every rule
   against every stitched fragment.
4. The "Engine-flagged suspects" section, when present — fragments
   the engine's pre-scan flagged for investigation. Walk every rule
   against each named fragment.
5. The pointer block listing every stitched output path under
   `runDir`. Read each path; ACT where the threshold holds.
6. The seven reference distillation atoms — fetch on demand via
   `zerops_knowledge uri=zerops://themes/refinement-references/<name>`:
   - `kb_shapes` — KB stem symptom-first heuristic.
   - `ig_one_mechanism` — IG one-mechanism-per-H3.
   - `voice_patterns` — friendly-authority phrasings.
   - `yaml_comments` — yaml-comment shape.
   - `citations` — cite-by-name pattern.
   - `trade_offs` — two-sided trade-offs in KB bodies.
   - `refinement_thresholds` — the ACT vs HOLD decision rules.
