# brief-editorial-review-minimal-simulation.md — cold-read simulation (minimal)

**Purpose**: cold-read the composed minimal-tier editorial-review brief (from [brief-editorial-review-minimal-composed.md](brief-editorial-review-minimal-composed.md)) with fresh-reader premise. Document ambiguities and contradictions specific to minimal tier.

**Role**: editorial-review
**Tier**: minimal

---

## 1. What transfers cleanly from showcase simulation

All issues identified in [brief-editorial-review-showcase-simulation.md](brief-editorial-review-showcase-simulation.md) §2–§3 apply to minimal with identical disposition:
- Reading-order discipline (walk before manifest)
- Tool-use clarification (Edit/Write vs Bash)
- Classification edge cases with worked examples
- Cross-reference definition positive form
- `zerops_knowledge` guides as external-doc access

These are tier-invariant atom clarifications. Same proposed edits apply.

## 2. Minimal-specific ambiguities

### 2.1. Single-codebase cross-surface ledger

> `cross-surface-ledger.md` tracks fact bodies across 7 surfaces

For single-codebase minimal, fewer surfaces exist (no 3×-codebase-READMEs, no worker codebase). The atom's instruction to "track across 7 surfaces" may read as over-specified when only 5-6 actually exist in the deliverable.

**Proposed clarification**: `cross-surface-ledger.md` should open with *"Walk whichever of the 7 surface categories exist in the deliverable. Minimal single-codebase: ~5 surface types present (root/env-README/env-import-yaml/per-codebase-README/CLAUDE.md/zerops.yaml — no worker codebase splits applies). Showcase: all 7 × 3 codebases apply."* Remove the strict "across 7" framing.

### 2.2. Path A main-inline writer implication

The composed brief's porter-premise reads the same on minimal as showcase: "you have not worked on this recipe before." But on minimal, the content was authored main-inline (Path A) — so the content might carry the main-agent's authorship fingerprints more heavily than showcase's fresh-context writer output.

**Implication**: editorial reviewer on minimal is likely to find MORE wrong-surface, folk-doctrine, and self-referential items than on showcase. The atoms don't need modification — this is precisely why Path B default-on for minimal matters — but v35.5 minimal run's editorial-review CRIT/WRONG counts should be expected higher than v35 showcase's, and that's not a regression signal, it's the author+judge-split doing its job on a tier that needs it most.

**Proposed note for rollback-criteria** (already addressed in T-11 / T-12 thresholds): minimal's editorial CRIT counts are expected non-zero before inline-fix; the gate is POST-INLINE-FIX shipping = 0. Add a clarifying note in rollback-criteria.md that minimal's pre-inline-fix editorial CRIT counts can be significantly higher than showcase's without signaling regression.

### 2.3. Single-framework counter-example matching

The `counter-example-reference.md` atom lists v28 anti-patterns specific to v28's 3-codebase showcase (api.ts helper, NestJS setGlobalPrefix, Svelte plugin-svelte peer-dep). A reviewer on a minimal Laravel recipe, for example, might not find direct pattern matches for those specific examples.

**Proposed clarification**: `counter-example-reference.md` should declare each anti-pattern with its CLASS not just its instance:

- "Class: scaffold-decision-disguised-as-gotcha. Instance: v28 appdev api.ts helper."
- "Class: framework-quirk-as-Zerops-gotcha. Instance: v28 apidev setGlobalPrefix."
- "Class: npm-registry-metadata-as-gotcha. Instance: v28 appdev plugin-svelte peer-dep."

The reviewer pattern-matches on CLASS (applies across frameworks) not on INSTANCE. The atom should emphasize class-level generalization to handle minimal recipes using different frameworks than v28's NestJS/Svelte.

## 3. Minimal-specific contradictions

### 3.1. Ungated close + default-on dispatch tension

The brief says:
> Return when all surfaces walked. Do not call zerops_workflow. Do not signal completion of your dispatch step via any tool — the main agent will complete the substep upon receiving your return payload.

But for minimal, close is ungated — there IS no `complete close.editorial-review substep`. How does main agent "complete the substep" when the substep isn't gated?

**Resolution**: the brief wording is generic and accurate enough — main agent handles whatever workflow action is appropriate for the tier. On showcase, it's `complete close.editorial-review`. On minimal, since close is ungated, main proceeds to the next action without substep-complete attestation. The reviewer's return payload populates check results either way (dispatch-runnable checks fire regardless of substep-gating).

**Proposed clarification**: `completion-shape.md` should declare: *"Return payload is consumed by the main agent. On gated substeps (showcase close), main attests `complete close.editorial-review` with your return payload. On ungated substeps (minimal close), main proceeds to close-complete with your return payload informing post-close checks. Either way, do not attempt to attest anything yourself."*

## 4. Minimal-tier cold-read verdict

**PASS conditional on §2 + §3 clarifications** in addition to the 5 showcase-level clarifications from [brief-editorial-review-showcase-simulation.md §5](brief-editorial-review-showcase-simulation.md). Total proposed in-atom edits: 8 (5 tier-invariant + 3 minimal-specific). All are content clarifications, not structural.

## 5. Minimal-specific defect-class catches cold-read verifies

- **Path-A writer-inherited authorship artifacts**: does the reviewer catch "writer wrote from debug-spiral context" class? → porter-premise + counter-example-reference + classification-reclassify catches it via reclassification of facts the writer framed as platform-invariant when they're actually self-inflicted scaffold bugs.
- **Thin-content floor on minimal**: does reviewer catch template-boilerplate env-READMEs? → `single-question-tests.md` env-README test "does this teach me when to outgrow this tier" fails on boilerplate; WRONG.
- **Cross-tier claim on single-codebase**: nestjs-minimal-v3 showed ambiguous canonical-output-tree + main-inline writing. Does the reviewer catch if content crosses tier? → `cross-surface-ledger.md` catches duplication.

## 6. Ratification status

PASS conditional on 8 total in-atom clarifications (5 shared + 3 minimal-specific). All ship as C-4 atom-authoring decisions, no structural change.
