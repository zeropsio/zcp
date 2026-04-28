# Run-16 — what to do after the dogfood

Tranches 0-5 of [run-16-readiness.md](run-16-readiness.md) shipped on
this branch. Tranches 6 + 7 are dogfood-gated. This doc is the compact
checklist for the post-dogfood return.

## Step 1 — run the dogfood

After tranches 0-5 are merged + the binary is released, run a recipe-
authoring dogfood through the new pipeline. Use a small-shape recipe
first (single-codebase, no parent) to smoke-test, then a showcase-shape
(api + frontend + worker + 5 managed services) for full coverage.

## Step 2 — run the operational gates

```
make verify-dogfood-no-manual-subdomain-enable
make verify-claudemd-zerops-free
```

Both gates must PASS:

- **`verify-dogfood-no-manual-subdomain-enable`** (R-15-1 closure) —
  zero manual `zerops_subdomain action=enable` invocations in the
  dogfood session jsonl. Recipe-authoring auto-enable now works via
  the dual-signal predicate (`detail.SubdomainAccess` OR
  `Ports[].HTTPSupport`); a manual enable means a regression.

- **`verify-claudemd-zerops-free`** (§0.6 mechanism gate) — every
  `codebase/<h>/claude-md` fragment authored by the claudemd-author
  sub-agent contains zero Zerops content. Static prohibited tokens
  (`## Zerops`, `zsc`, `zerops_*`, `zcp`, `zcli`) plus every hostname
  in `plan.Services` (read from `<run>/plan.json` when present).

If either gate FAILs, fix the underlying defect before proceeding.

## Step 3 — review the run

Open `docs/zcprecipator3/runs/<run-N>/` and walk:

- **README.md** + per-tier READMEs — does the env-content sub-agent
  produce acceptable per-tier prose?
- **<codebase>/README.md** — IG slotted form (1..5 items), KB bullets
  (`- **Topic** —` shape, ≤8), zerops.yaml comments injected per block.
- **<codebase>/CLAUDE.md** — `/init`-shape, zero Zerops content, 2-4
  `## ` sections (project overview implicit + Build & run + Architecture +
  optional extra).
- **environments/facts.jsonl** — porter_change + field_rationale +
  tier_decision + contract subtypes present; engine-emitted shells
  filled via `fill-fact-slot` (Why + CandidateHeading non-empty,
  EngineEmitted=false).

## Step 4 — Tranche 6 (legacy atom deletion)

**Conditional**: ships only if the dogfood produced acceptable output
across all surfaces without regressions traced to missing atoms.

Per [run-16-readiness.md §11 Tranche 6](run-16-readiness.md):

```
git rm internal/recipe/content/briefs/scaffold/content_authoring.md   # -500 LoC
git rm internal/recipe/content/briefs/feature/content_extension.md    # -250 LoC
git rm internal/recipe/content/briefs/finalize/intro.md               # part of -160 LoC
git rm internal/recipe/content/briefs/finalize/validator_tripwires.md
git rm internal/recipe/content/briefs/finalize/anti_patterns.md
```

Verify nothing references the deleted atoms:

```
grep -rln "content_authoring\.md\|content_extension\.md" internal/
grep -rln "briefs/finalize/" internal/recipe/briefs.go
```

Run `go test ./... -short` + `make lint-local`. If the deletion breaks
anything, the legacy atom was load-bearing in a way the new atoms
don't replace — patch the new atom set instead and delay deletion
further.

**Specifically**: tranche 6 ships only after the **showcase-shape**
dogfood passes, not the small-shape. Showcase exercises 6 tier yamls ×
~8 service blocks of env-content authoring that the small shape never
touches.

## Step 5 — Tranche 7 (sign-off)

Per [run-16-readiness.md §11 Tranche 7](run-16-readiness.md):

1. **Spec rewrite** — `docs/spec-content-surfaces.md §Surface 6`
   replaced per [§15 of the readiness doc](run-16-readiness.md#15-spec-surface-6-rewrite--lands-alongside-engine-implementation-tranche-7-commit-1).
   Drop-in replacement provided in §15 verbatim.

2. **CHANGELOG entry** — name what the run-16 architecture pivot
   delivered. Verdict-table rows pre-specified in [§11 Tranche 7
   commit 2 of the readiness doc](run-16-readiness.md):

   | Artifact | TEACH/DISCOVER | Status |
   |---|---|---|
   | Sub-agent-authored CLAUDE.md via dedicated `claudemd-author` | DISCOVER | ✅ |
   | Slot-shape refusal at record-fragment time | TEACH | ✅ |
   | 7-phase pipeline (research → provision → scaffold → feature → codebase-content → env-content → finalize) | — | ✅ |
   | FactRecord polymorphic Kind discriminator | TEACH | ✅ |
   | Engine-emit Class B/C umbrella facts | TEACH | ✅ |
   | Per-managed-service fact shells (fill-fact-slot pattern) | Hybrid | ✅ |
   | Engine-emit tier_decision facts | TEACH | ✅ |
   | Subdomain dual-signal eligibility (R-15-1) | TEACH | ✅ |
   | validateCrossSurfaceDuplication / validateCrossRecipeDuplication | TEACH (defensible) | ⚠️ Notice |
   | validateCodebaseCLAUDE reshape | TEACH | ✅ |

3. **Archive the plan docs**:

   ```
   git mv docs/zcprecipator3/plans/run-16-prep.md docs/zcprecipator3/plans/archive/
   git mv docs/zcprecipator3/plans/run-16-readiness.md docs/zcprecipator3/plans/archive/
   git mv docs/zcprecipator3/plans/run-16-post-dogfood.md docs/zcprecipator3/plans/archive/
   ```

4. **Spec amendments** — anything that drifted during implementation
   that wasn't caught by §15 lands as a follow-up commit on
   `spec-content-surfaces.md` and/or `system.md`.

## Reviewer-fix recap (already landed pre-dogfood)

A separate code review surfaced 6 defects + 3 minors against the
tranche 0-5 implementation; all addressed before this commit. See the
final commit message for the full list. The dogfood runs against the
post-fix implementation, not the original landing.

## Reverting tranches 1-5 if dogfood reveals fundamental issue

If the dogfood reveals the architecture pivot itself (not a tranche-6
deletion concern) is broken in a way no atom patch can fix, the revert
path is `git revert <T1>..<T5>` on the tranche commits. The Tranche 0
work (R-15-1 + verify gates + scaffold subdomain teaching + run-15
fact annotation) is independent and stays.
