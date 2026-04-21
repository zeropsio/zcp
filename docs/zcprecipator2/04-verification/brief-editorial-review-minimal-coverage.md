# brief-editorial-review-minimal-coverage.md — defect-class coverage (minimal)

**Role**: editorial-review
**Tier**: minimal

---

## 1. Coverage table — minimal tier

All defect classes from [brief-editorial-review-showcase-coverage.md §1](brief-editorial-review-showcase-coverage.md) apply to minimal with the same editorial-review contribution. Minimal-specific notes below.

| Registry row | Class | Minimal-specific note |
|---|---|---|
| **15.1** (NEW) | classification-error-at-source | **ESPECIALLY load-bearing for minimal** — Path A main-inline writer collapses author+judge; editorial-review reclassification is the ONLY mechanism restoring the split. Showcase has fresh-context writer + editorial-review (belt + suspenders); minimal has editorial-review only. |
| 8.2 | v28 wrong-surface gotchas | Higher risk class on minimal than showcase because Path A main-inline writer carries authorship fingerprints heavily. Editorial-review's porter-premise essential. |
| 8.3 | v28 folk-doctrine fabrication | Higher risk class on minimal (same reason as 8.2). |
| 8.4 | v28 cross-surface duplication | Lower risk on minimal (fewer surfaces to duplicate across) but still applies. |
| 14.1 | v34 manifest-content-inconsistency | Same risk; P5 manifest honesty applies to minimal identically; editorial-review reclassification adds tertiary defense. |
| 14.4 | v34 self-referential gotcha | Same risk; editorial-review porter-premise applies. |
| 2.1, 2.2, 2.4 | v20 content-reform classes | Applies; citation-audit + classification-reclassify cover. |
| 1.4 | v15 predecessor-clone gotchas | Lower risk on minimal (fewer codebases); still applies. |
| 1.5 | v15 IG-restates-gotchas | Same risk; single-question-tests separate IG vs KB intent. |

## 2. Minimal-exclusive classes editorial-review catches

| Class | Description | Mechanism |
|---|---|---|
| Main-inline writer authorship artifact | Main-agent's deploy-round debug narratives bleed into published content because main authored the content with full session context. Showcase doesn't have this class (fresh-context writer). | Editorial-review porter-premise + classification-reclassify catches fabricated mental models; counter-example-reference pattern-matches v23-era folk-doctrine from debug-spiral |
| Thin single-codebase deliverable | Single codebase means less cross-reference discipline; easier to over-concentrate content on one surface or leave surfaces thin | Surface-walk-task + single-question-tests catches (env README boilerplate, CLAUDE.md byte-floor failures, IG thinness) |

## 3. Minimal-tier coverage completeness

### 3.1. Every applicable defect class from registry has ≥1 mechanism

Registry rows tier-applicable to minimal (all rows except showcase-specific ones like 3.1 v21 scaffold hygiene multi-codebase variant) covered per §1.

### 3.2. Every composed-brief instruction on minimal tier traces to defect class or spec section

Same orphan check as [brief-editorial-review-showcase-coverage.md §3](brief-editorial-review-showcase-coverage.md) applies. Zero orphan instructions. Minimal tier-branch in `surface-walk-task.md` (fewer codebases, fewer env tiers) does not introduce orphans — it subsets the walk.

## 4. Path A caveat resolution

Step-4 coverage docs previously (pre-refinement) flagged Path A main-inline writer as having partial v28-class coverage ([atomic-layout.md §7](../03-architecture/atomic-layout.md) + RESUME.md step-4 findings). The editorial-review role is the **primary resolution** of that caveat.

Before refinement: "Path A has partial coverage of v28-debug-agent-writes-content because main cannot truly forget its deploy rounds. Enforcement shifts from process (fresh context) to output (pre-attest aggregate + manifest honesty + classification taxonomy)." — this list stops short of "independent post-hoc review."

After refinement: Path A gets editorial-review independent reclassification + porter-premise cold-read. The author/judge split is restored at close-phase, not at author-phase.

**Path A resolution is now**: Path A main-inline writer is production-viable on minimal PROVIDED Path B editorial-review dispatches at close. If editorial-review regresses (T-11 fires), the Path A caveat resurfaces and minimal should pilot Path B writer dispatch as the v36 patch per RESUME Q4.

## 5. Coverage verdict (minimal)

**PASS**. Same as showcase coverage. Editorial-review on minimal is the primary closure mechanism for the classification-error-at-source class (15.1) AND is the Path A writer caveat's principal resolution. If any one of those closures fails in v35.5 minimal, the refinement is partially ineffective for minimal tier — rollback or v36-patch per rollback-criteria decision matrix.

Post-v35 + v35.5 status:
- If v35 showcase ER-1..ER-5 clean AND v35.5 minimal ER-1..ER-5 clean: editorial-review architecture validated across both tiers.
- If v35 showcase clean AND v35.5 minimal regresses: investigate whether Path B writer dispatch should be promoted to minimal default (per Q4 deferred decision).
- If v35 showcase regresses: revert editorial-review per T-11/T-12 rollback; minimal v35.5 doesn't run until architecture fixed.
