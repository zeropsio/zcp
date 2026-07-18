# Run-32 pre-flight execution status (2026-05-08, end-of-session)

**Status:** Pre-flight complete. Working tree carries every intervention testable against captured run-32 output + the sim. Tests + lint clean. Code review (codex) pass complete; all findings addressed. Ready for fresh dogfood whenever scheduled — no further sim/static work blocking.

**Read order for re-entry:**
1. This doc.
2. [run-32-content-quality-deep-diagnosis.md](run-32-content-quality-deep-diagnosis.md) — layered diagnosis + validated corrections.
3. [run-32-rules-from-jetstream.md](run-32-rules-from-jetstream.md) — 39-rule scoring substrate (codex-verified).
4. [run-32-phase-1-baselines.md](run-32-phase-1-baselines.md) — counter baselines on captured run-32.
5. [run-32-phase-2-results.md](run-32-phase-2-results.md) — refinement-substrate replay result (4 → 13 fragments).

---

## What landed

### Cluster A — fact schema/taxonomy (run-32 F-A, F-B, F-D)

| Item | Files | Pinned by |
|---|---|---|
| **F-B** Engine populates `Why` on `tier_decision` shells at emit time | [engine_emitted_facts.go:103-150](../internal/recipe/engine_emitted_facts.go#L103-L150) [facts.go::validateTierDecision](../internal/recipe/facts.go) | `TestEmittedTierDecisionFacts_PopulatesWhy`, fixture updates in `facts_test.go` |
| **F-A** `candidateSurface` enum lock at Validate; brief atom teaches UPPER_SNAKE | [facts.go::validatePorterChange](../internal/recipe/facts.go) [surfaces.go::IsCanonicalSurface](../internal/recipe/surfaces.go) [decision_recording_slim.md](../internal/recipe/content/briefs/scaffold/decision_recording_slim.md) | reuses scaffold brief size pin (44 KB target) |
| **F-D** `FactKindNegation` schema-only — records absences ("this codebase does NOT consume X") | [facts.go::validateNegation](../internal/recipe/facts.go) MCP schema desc updated in [handlers.go:180](../internal/recipe/handlers.go#L180) | `TestFactRecord_Validate_Negation_RequiresFields` |

**Diagnostic correction discovered while implementing F-B:** the captured 10 null-`Why` tier_decisions were ALL `engineEmitted=true` shells the agent never filled. The diagnosis "Validate doesn't require Why" was true-but-irrelevant. Fix is engine fills Why at emit, not stricter validator.

**Engine consumption of negation kind is a follow-up:** schema is in place so scaffold/feature agents can start recording negations; downstream readers (cross-codebase coherence validator skipping a service for codebases with a recorded `negation`) lands when there's evidence of need.

### Step 2 — cross-codebase coherence validator (detection-only)

New gate at [gate_cross_codebase_env_coherence.go](../internal/recipe/gate_cross_codebase_env_coherence.go). Wired into `CodebaseScaffoldGates` ([gates.go:107](../internal/recipe/gates.go#L107)). Severity = `Notice` (detection-only; refusal lands once the contract has a place to be published — open question for next iteration).

Detects:
- Cross-codebase mismatch: `apidev:DB_PASSWORD` vs `workerdev:DB_PASS` for `${db_password}`.
- Intra-codebase aliasing: `apidev` binding both `DB_PASSWORD` and `DB_AUTH` to `${db_password}` while `workerdev` uses only `DB_PASSWORD`. Both surface (codex review fix; pinned by `TestFindCrossCodebaseEnvMismatches_IntraCodebaseAliasingUnioned`).

Robustness:
- Composite values like `${storage_apiUrl}/${storage_bucketName}` ignored (not aliases).
- Project-scope refs (`${APP_SECRET}`, uppercase first char) ignored.
- Malformed refs (`${db_}`, `${db__password}`, `${db_1abc}`) rejected at predicate time. Pinned by `TestMatchSingleServiceRef_RejectsMalformedSuffix`.

Detected in captured run-32 (verified by `TestFindCrossCodebaseEnvMismatches_Run32CapturedMismatches`):
- `${db_password}`: apidev=`DB_PASSWORD`, workerdev=`DB_PASS`
- `${storage_accessKeyId}`: apidev=`S3_ACCESS_KEY_ID`, workerdev=`S3_KEY`
- `${storage_secretAccessKey}`: apidev=`S3_SECRET_ACCESS_KEY`, workerdev=`S3_SECRET`

### Step 3 — citation-guides relabel

[briefs_content_phase.go:528-548](../internal/recipe/briefs_content_phase.go#L528-L548) and parallel `briefs_content_phase_multifile.go` path. Header changed from "Citation guides for this recipe" (read like a menu of options and primed Pattern #2 slug verbalization) to "`zerops_knowledge` lookup keys (NEVER surface in published content)". Body explicitly forbids backticked slug, slug-in-link-text, and English-cased translation forms. Pinned by `TestBuildCodebaseContentBrief_ThreadsCitationGuides`.

### Phase 2 substrate (already established)

39-rule golden-grounded substrate at [content/briefs/refinement/derived_rules.md](../internal/recipe/content/briefs/refinement/derived_rules.md). Loaded alongside `embedded_rubric.md` in both single-file and multi-file refinement composers. Soft-cap raised 60→75 KB provisionally with retire-comment for when `embedded_rubric.md` retires.

Run-32 codex review pass: rubric `embedded_rubric.md` slug-link-text scoring tightened to align with the spec's descriptive-label requirement (slug-as-link-text now explicitly classified as "no resolution"). Hard-rule-first scoring loop added to derived_rules to fix the env-fragment HOLD failure mode where criterion-by-criterion scoring missed V6 violations.

### Phase 1 counter baselines (frozen for delta measurement)

| Counter | Run-32 baseline | Status |
|---|---|---|
| #1 cross-codebase env-var coherence | 3 mismatches | Detection landed (Step 2); refusal pending next iteration |
| #2 slug-leakage (English-cased) | 8 instances | Step 3 relabel + V3 rule + rubric scoring fix should drive toward 0 |
| #3 cross-framework verb count | 22 | IG4 rule + Phase 2 substrate proved 22→0 in replay |
| #4 voice-leak (sharpened regex) | 3 instances | Hard-rule-first scoring loop should catch these |
| #5 fact contamination | 12% (17/142) | Upstream teaching (F-A surface enum) + V6 rule indirectly help; sanitize-at-Append rejected as wrong shape |
| #6 tier_decision Why-fill | 0% | F-B closes at source — engine fills Why at emit time |
| #7 refinement recall | n/a (LLM-in-loop) | Phase 2 dispatch confirmed 4 → 13 (+9 fragments) |
| #8 KB-header consistency | 100% inconsistent | KB1 rule fired in Phase 2 dispatch on 2 of 3 siblings |

## Codex code review — findings addressed

5 findings from codex pass on all unstaged changes:

| # | Severity | Issue | Fix |
|---|---|---|---|
| 1 | **MEDIUM** | First-key-wins in coherence gate hid intra-codebase aliasing | Switched to set-union per (source, codebase); regression test added |
| 2 | NOTICE | `matchSingleServiceRef` accepted malformed `${db_}`, `${db__password}` | Tightened predicate; regression test added |
| 3 | NOTICE | `FactKindNegation` missing from MCP `record-fact` jsonschema description | Description extended with `negation` kind + required fields |
| 4 | NOTICE | `embedded_rubric.md` "Real markdown link" scoring path conflicted with spec's descriptive-label requirement on slug-link-text | Rubric now explicitly classifies slug-name-as-link-text as "no resolution" with cited examples |
| 5 | NOTICE | `briefs_refinement.go` comment claimed "39 rules" but atom IDs aren't strictly 39 | Comment honest about non-contiguous IDs after codex verification cut/merge |

## What is NOT done (deliberately deferred)

| Item | Why deferred |
|---|---|
| **F-C** dedup at recordFact | Cosmetic noise (14% near-duplicates); doesn't move quality counters |
| **Step 2 enforcement (refusal)** | Detection-only first; need fresh-run evidence that the agent self-corrects before flipping to refuse |
| **Step 4** trim `synthesis_workflow.md` overall to ≤60 KB | Multi-week per codex (975 lines, every line test-pinned); low priority since Phase 2 already moved the needle |
| **Step 5** retire `embedded_rubric.md` once derived_rules proves load-bearing | Needs more dispatch cycles before commit to single-substrate path |
| **Step 7** outline-then-write coordinator pass | Architectural change — the "fourth rewrite" boundary; defer until simpler interventions exhaust |
| **F-E** scaffold-phase teaching to prevent cross-framework drift at source | Brief-side work; could be done now but lands more cleanly with fresh dogfood evidence about which alternatives to forbid by name |
| **Engine consumption of negation kind** | Schema is in place; readers land when there's evidence of need |

## Truly fresh-run-required (the residual doubt)

After the pre-flight, the only questions left for a real dogfood:

1. **Does the agent emit Why-populated `field_rationale` and direct (non-shell) `tier_decision` records under F-A/F-B teaching?** Schema enforces it; agent compliance is a fresh-run question.
2. **Does the cross-codebase coherence validator's Notice surface change scaffold-agent behavior?** Detection alone may not move the agent if the brief teaching doesn't reach. F-A's atom edit teaches canonical surface; analogous teaching for cross-codebase contract is pending.
3. **Does the new derived_rules substrate hold across non-NestJS recipes?** The 39 rules were extracted from Laravel goldens applied to NestJS candidate. The "rules are framework-canonical → portable" hypothesis is untested for Django/Rails/Phoenix.
4. **Does scaffold-phase teaching prevent cross-framework drift in fact text at source (F-E)?** Recall: facts.jsonl line 90 cited Express/Fastify as alternatives at scaffold time. The drift was inherited; teaching the agent to NOT enumerate alternatives in fact text closes it upstream.

These four questions are the agenda for the next fresh dogfood. Everything sim-testable has been tested.

## Build / dispatch

```bash
cd /Users/fxck/www/zcp
go test ./internal/recipe/... -short    # must pass
make lint-fast                           # must pass
go build -o /tmp/zcp-recipe-sim ./cmd/zcp-recipe-sim
```

To replay second-half against captured snapshot with new substrate:
```bash
SRC=docs/zcprecipator3/runs/32
DST=docs/zcprecipator3/simulations/<NEW>
/tmp/zcp-recipe-sim emit -run $SRC -out $DST
/tmp/zcp-recipe-sim emit-refinement -dir $DST
# Then dispatch refinement Agent against $DST/briefs/refinement-prompt.md
# (it reads the multi-file brief at $DST/.briefs/refinement-phase/index.md)
```

To verify cross-codebase coherence validator against captured yamls:
```bash
go test ./internal/recipe/... -short -run TestFindCrossCodebaseEnvMismatches_Run32CapturedMismatches
```

## Files changed in pre-flight (all unstaged)

```
M  internal/recipe/briefs_content_phase.go              (Step 3 relabel)
M  internal/recipe/briefs_content_phase_multifile.go    (Step 3 + Phase 2 atom wire)
M  internal/recipe/briefs_content_phase_run17_test.go   (Step 3 test)
M  internal/recipe/briefs_f_test.go                     (F-A target bump)
M  internal/recipe/briefs_refinement.go                 (Phase 2 atom wire)
M  internal/recipe/briefs_refinement_test.go            (cap raise + retire-comment)
M  internal/recipe/engine_emitted_facts.go              (F-B Why population)
M  internal/recipe/engine_emitted_facts_test.go         (F-B regression)
M  internal/recipe/facts.go                             (F-A enum + F-B require + F-D negation)
M  internal/recipe/facts_test.go                        (F-A/B/D test fixtures + tests)
M  internal/recipe/gates.go                             (Step 2 wire)
M  internal/recipe/handlers.go                          (F-D MCP schema)
M  internal/recipe/surfaces.go                          (F-A IsCanonicalSurface)
M  internal/recipe/content/briefs/refinement/embedded_rubric.md  (codex review #4)
M  internal/recipe/content/briefs/scaffold/decision_recording_slim.md  (F-A teaching)
?? internal/recipe/content/briefs/refinement/derived_rules.md          (Phase 2 atom)
?? internal/recipe/gate_cross_codebase_env_coherence.go                (Step 2)
?? internal/recipe/gate_cross_codebase_env_coherence_test.go           (Step 2 tests)
```

Plus pre-existing untracked work from prior session (Pattern #8 + run-32 Pattern #2 fix-pack):
```
?? internal/recipe/prod_runtime_base.go
?? internal/recipe/prod_runtime_base_test.go
[plus pre-existing modifications to spec-content-quality-rubric.md, briefs/codebase-content/synthesis_workflow.md, briefs/env-content/per_tier_authoring.md, briefs/refinement/synthesis_workflow.md, principles/yaml-comment-style.md, phase_entry/codebase-content.md, plan.go, slot_shape_authoring.go, tiers.go, validators_root_env.go, yaml_emitter.go]
```

All present in working tree, no commits yet — that's the user's review surface.

## Aspirational reference

[`docs/zcprecipator3/simulations/32-aspirational-fixed/`](../docs/zcprecipator3/simulations/32-aspirational-fixed/) — manual hand-curated fix of the run-32 captured output. Every defect from the 19-defect head-to-head table fixed where applicable + every applicable rule from the 39-rule golden substrate applied. Use as the BASE for measuring future runs.

Key changes documented at [`32-aspirational-fixed/CHANGES.md`](../docs/zcprecipator3/simulations/32-aspirational-fixed/CHANGES.md). Counter status under aspirational-fixed:

- Cross-codebase env-var coherence mismatches: **0** (was 3)
- Slug-leakage in published markdown: **0** (was 8)
- Cross-framework verb count: **0** (was 22; 2 remaining "Express" mentions are accurate stack-naming since NestJS uses Express under the hood)
- Voice-leak (sharpened regex): **0** (was 3)
- KB-header consistency across siblings: **100% consistent** (was 100% inconsistent)
- Adapt-path framings: **0** (was 8)

Production-vs-Development upgrade map + 3-codebase orientation now present in root README (closing Defects #2 + #6).

## Plans inventory

```
plans/run-32-content-quality-handoff.md            — original kickoff
plans/run-32-content-quality-handoff-iter-2.md     — handoff at session start
plans/run-32-content-quality-overhaul-2026-05-08.md — original framing
plans/run-32-content-quality-deep-diagnosis.md     — layered diagnosis (validated)
plans/run-32-rules-from-jetstream.md               — 39-rule substrate (codex-verified)
plans/run-32-phase-1-baselines.md                  — counter baselines
plans/run-32-phase-2-results.md                    — refinement substrate replay (4→13)
plans/run-32-phase-3-preflight-status.md           — THIS DOC
```
