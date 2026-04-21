# brief-editorial-review-minimal-composed.md — composed transmitted brief (minimal tier)

**Purpose**: composed prompt the new architecture transmits to the editorial-review sub-agent at `close.editorial-review` for minimal tier. Produced by `buildEditorialReviewDispatchBrief(plan, factsLogPath, manifestPath)` with minimal tier-branch applied.

**Role**: editorial-review
**Tier**: minimal (dispatch-Path-B default-on, ungated-discretionary matching close.code-review)

**Tier delta from showcase** (see [data-flow-minimal.md §1 + §7](../03-architecture/data-flow-minimal.md)):
- 1-2 codebases instead of 3 (single `appdev` OR dual-runtime `appdev` + `apidev`)
- No worker codebase
- 4 env tiers typical instead of 6 showcase
- Close step ungated (but editorial-review dispatch fires by default)
- No browser-walk at close

**Stitching recipe** (identical to showcase except `surface-walk-task.md` tier-branches fewer surfaces):
```
briefs/editorial-review/mandatory-core.md +
briefs/editorial-review/porter-premise.md [same — porter premise tier-invariant] +
briefs/editorial-review/surface-walk-task.md [MINIMAL tier-branch: 1-2 codebases × 4 env tiers; no worker codebase] +
briefs/editorial-review/single-question-tests.md [same — tier-invariant] +
briefs/editorial-review/classification-reclassify.md [same — tier-invariant] +
briefs/editorial-review/citation-audit.md [same — tier-invariant] +
briefs/editorial-review/counter-example-reference.md [same — tier-invariant; v28 anti-patterns apply regardless] +
briefs/editorial-review/cross-surface-ledger.md [MINIMAL tier-branch: fewer cross-surface permutations] +
briefs/editorial-review/reporting-taxonomy.md [same] +
briefs/editorial-review/completion-shape.md [same] +
pointer-include principles/where-commands-run.md +
pointer-include principles/file-op-sequencing.md +
pointer-include principles/tool-use-policy.md +
interpolate {factsLogPath, manifestPath}
```

**Expected prompt length**: ~8-9 KB (slightly smaller than showcase's 9-10 KB due to fewer surfaces in the walk).

---

## Why editorial-review is ESPECIALLY load-bearing on minimal

Per [data-flow-minimal.md §7](../03-architecture/data-flow-minimal.md) + atomic-layout.md §7 tier-branching:

Minimal tier uses **Path A main-inline writer** by default (not dispatched writer sub-agent). This means on minimal, the main agent writes the content inline during deploy.readmes with full session context loaded (deploy rounds, debugging narratives, fix journals). This is EXACTLY the failure mode [spec-content-surfaces.md](../../spec-content-surfaces.md) diagnoses at line 4-5: *"the agent which debugs the recipe also writes the reader-facing content, and after 85+ minutes of debug-spiral its mental model is 'what confused me' rather than 'what a reader needs.'"*

The v8.94 fresh-context writer sub-agent pattern (on showcase) fixes this at author-time. Minimal doesn't get that fix — authorship and judgment collapse onto main. Editorial-review on minimal is the ONLY mechanism restoring the author/judge split. A reviewer sub-agent with porter-premise + fresh context reads the deliverable cold.

**Implication for v35.5 minimal run**: editorial-review Path B default-on is more load-bearing for minimal than for showcase. If v35.5 minimal regresses on ER-1 (wrong-surface CRIT shipped), the fix is not to disable Path B — it's to strengthen the reviewer atoms (more counter-examples, more explicit porter-premise framing, more worked classification-reclassify examples).

---

## Tier-branched section: surface-walk-task.md (minimal variant)

The walk order differs from showcase. For single-codebase minimal:

```
1. Root README at /var/www/ZCPRECIPATOR-OUTPUT/README.md (if present)
2. Environment READMEs at environments/{0..3}/README.md (typically 4 tiers: dev, review/stage, stage, prod — varies per recipe)
3. Environment import.yaml comments at environments/{0..3}/import.yaml
4. Per-codebase README intro/IG/KB at appdev/README.md
5. Per-codebase CLAUDE.md at appdev/CLAUDE.md
6. Per-codebase zerops.yaml comments at appdev/zerops.yaml
```

For dual-runtime minimal (e.g., `nestjs-minimal-v3` with separate frontend codebase):

```
1. Root README
2. Environment READMEs × 4 tiers
3. Environment import.yaml comments × 4 tiers
4. Per-codebase README intro/IG/KB × 2 codebases (appdev + apidev)
5. Per-codebase CLAUDE.md × 2 codebases
6. Per-codebase zerops.yaml comments × 2 codebases
```

Single-question tests are applied per-item identically to showcase. Classification-reclassify, citation-audit, counter-example-reference, reporting-taxonomy, completion-shape — all tier-invariant.

---

## Composed prompt (representative)

The prompt text is identical to [brief-editorial-review-showcase-composed.md](brief-editorial-review-showcase-composed.md) except:

1. **Porter premise**: adjusted to name the minimal framework (e.g., "NestJS application" instead of "NestJS + Svelte + NATS-worker multi-service application"); tier-appropriate scale signalled.
2. **Surface walk**: enumerates 1-2 codebases × 4 env tiers instead of 3 × 6.
3. **Counter-example reference**: the v28 anti-patterns section still lists ALL v28 classes (framework-quirk, self-inflicted, scaffold-decision, folk-doctrine, factually-wrong, cross-surface-dup). These apply across tiers — minimal recipes can hit the same classes, just in smaller surface-count.
4. **Cross-surface ledger**: explicit note that minimal has fewer cross-surface permutations, so duplicate detection is simpler; same mechanism.
5. **Completion shape**: identical return payload schema.

Actual byte count ≈ 8-9 KB transmitted.
