# Phase 2 — Refinement substrate test results

**Test method:** Replay refinement against captured run-32 stitched output using a new principle-shaped rule substrate (`derived_rules.md`, 39 rules from goldens). Compare what flipped vs the original run.

**Captured target:** `/Users/fxck/www/zcp/docs/zcprecipator3/simulations/32-phase2-rules-substrate/`
**Date:** 2026-05-08

## Headline

**Original run flipped 4 fragments. Phase-2 run flipped 13 fragments. ~3.25× recall improvement.**

| Metric | Original | Phase 2 | Delta |
|---|---|---|---|
| Fragments flipped | 4 | 13 | +9 |
| Slug-leakage (V3) caught | 0 of 8 | 8 of 8 | full |
| Cross-framework (IG4) caught | 0 of 22 | 22 of 22 | full |
| KB-header inconsistency (KB1) | 0 of 3 | 2 of 3 (one was already correct) | full |

## Code changes for Phase 2

| File | Change |
|---|---|
| [internal/recipe/content/briefs/refinement/derived_rules.md](../internal/recipe/content/briefs/refinement/derived_rules.md) | NEW atom — 39 golden-grounded rules, ~10.7 KB |
| [internal/recipe/briefs_refinement.go](../internal/recipe/briefs_refinement.go#L88-L97) | Composer loads new atom alongside `embedded_rubric.md` |
| [internal/recipe/briefs_content_phase_multifile.go](../internal/recipe/briefs_content_phase_multifile.go#L389-L399) | Multi-file composer adds Part 3b "rules-from-goldens" |
| [internal/recipe/briefs_refinement_test.go](../internal/recipe/briefs_refinement_test.go#L116) | Soft cap raised 60→75 KB (provisional, with retire-when-rubric-replaced note) |

All tests pass: `go test ./internal/recipe/... -short` — clean.

## What flipped (13 ACTs)

| Fragment | Violated rules | Why ACT |
|---|---|---|
| codebase/api/knowledge-base | V3 | Replaced `[managed NATS service]`, `[Zerops object-storage service]` with descriptive labels |
| codebase/api/integration-guide/2 | IG4 | Dropped `Adapt path: any Node HTTP framework... Express's app.listen, Fastify's...` |
| codebase/api/integration-guide/3 | IG4 | Dropped `Adapt path: Fastify uses trustProxy: true... Plain Express is...` |
| codebase/api/integration-guide/5 | IG4 | Dropped `Adapt path: same shape for any framework... Express's cors middleware, Fastify's @fastify/cors` |
| codebase/app/knowledge-base | KB1 + IG4 | Removed `### Gotchas` header; stripped Webpack/Astro/SvelteKit/Next tail |
| codebase/app/integration-guide/2 | IG4 | Dropped `Webpack/Next.js/Nuxt` cross-framework knob enumeration |
| codebase/app/integration-guide/3 | IG4 | Dropped `Webpack DefinePlugin / Astro / Next NEXT_PUBLIC_* / SvelteKit PUBLIC_*` tail |
| codebase/app/integration-guide/4 | IG4 | Dropped `SvelteKit's static adapter / Astro / Next's static export` cross-framework tail |
| codebase/worker/knowledge-base | V3 (×3) + KB1 | Removed `## Knowledge base` header; replaced 3 slug-link-text instances |
| codebase/worker/integration-guide/2 | IG4 | Dropped `Adapt path: any Node worker... Express/Fastify workers...` |
| codebase/worker/integration-guide/3 | IG4 | Dropped `NATS JS client.subscription.drain(); amqplib uses channel.close(); AWS SDK SQS...` |
| codebase/worker/integration-guide/4 | V3 + IG4 | Replaced `[Zerops env-var model]` link text; trimmed adapt-path drift |
| codebase/worker/integration-guide/5 | V3 + IG4 | Replaced `[Zerops object-storage service]`; dropped `Boto3, Go SDK UsePathStyle` |

## What HELD (correctly out-of-scope)

| Class | Evidence | Why HOLD |
|---|---|---|
| IG2 length (worker/IG#5 ~62 lines) | Worker IG #5 still long after trim | Splitting requires NEW IG items — exceeds refinement scope (no new authoring); content-phase issue |
| Tier yaml voice | All tier envs score above 8.5 on Voice criterion | Already meets bar — nothing to fix |
| Cross-codebase env-var coherence (S3 keys, DB_PASS) | apidev `S3_ACCESS_KEY_ID` vs workerdev `S3_KEY`; apidev `DB_PASSWORD` vs workerdev `DB_PASS` | Out of scope per part-4 explicit boundary — Step 2 (engine validator) territory |
| URL factuality | `docs.zerops.io/services/nats` vs `docs.zerops.io/services/managed-services/nats` | Out of scope per part-4 — needs URL allowlist, separate work |

## Notable divergence — investigate before committing

**Original run flipped 2 env fragments the new run did NOT flip:**
- `env/0/import-comments/api`
- `env/0/import-comments/worker`

The new agent's reasoning: under part-4 rules, these score solid on V1 (porter-clones-and-runs framing), V4 (porter-actionable phrasings), Y2 (mechanism-first comments), Y10 (per-setup intro). Either:
- (a) The new substrate is more permissive on tier yaml voice and missed a real defect, OR
- (b) The original substrate over-flipped on these (low-value edits)

**Action item:** read the original 2 env replacements + the current state, decide which is right. If (a), tighten Y2/Y10 anchors. If (b), ratify the divergence — the new substrate has higher precision.

## Other observations

- **Codebase-content phase issue surfaced as HOLD:** worker IG #5 length is genuinely too long but refinement can't split items — this is a Phase 4a (codebase-content authoring) constraint, not refinement. The principle of "do one thing per IG step" needs to land at authoring time, not refinement.
- **Cross-codebase factuality surfaced as out-of-scope notice:** the agent correctly identified S3-key + DB_PASS mismatches but held them out — exactly the boundary the rule list draws. Step 2 (engine cross-codebase coherence validator) is the right tool.
- **The agent did NOT flip CLAUDE.md:** out of scope per user direction Q2. Correct.

## Recommendation on the substrate change

KEEP the new substrate change. The result establishes that:
1. Principle-shaped rules with observable anchors give the agent recognizable ACT triggers the existing rubric did not.
2. The substrate respects scope — out-of-scope classes (cross-codebase, factuality, structural splits) correctly fall through to other interventions.
3. The cost is one new ~10 KB atom + ~10 lines of composer change. No new gates, no behavioral rewrites elsewhere.

**Next steps (if user agrees to keep the substrate change):**
1. Investigate the env-fragment divergence (1-2 hours).
2. Decide retire-or-keep `embedded_rubric.md`. The current "augment, don't replace" shape is provisional; once the rule substrate is confirmed load-bearing, the old rubric is mostly redundant. Retiring frees ~24 KB of brief budget.
3. Commit the change.

## Risk register

| Risk | Severity | Notes |
|---|---|---|
| The 9 net new ACTs include a wrong flip | Low | Each ACT has a cited rule + violated phrase + preserving edit. Rule list is golden-grounded. |
| New substrate misses a defect class the original caught | Medium | The 2 env-fragment divergence is unexplained. Investigate before committing. |
| Soft cap creep | Low | Provisional 75 KB raise has retire-comment; can come back down once `embedded_rubric.md` retires. |
| Substrate doesn't hold for non-NestJS recipes | Unknown | The 39 rules were extracted from Laravel goldens applied to NestJS candidate. Pattern of "rules are framework-canonical → portable" should hold but unverified for Django/Rails/Phoenix. |

## Substrate quality observations from the test agent's behavior

The test agent's reasoning (from its report) shows it engaged with the rules at principle level:
- "Splitting [worker IG #5] requires authoring a new IG item, which exceeds refinement scope" — derived from threshold language + IG2 rule.
- "Tier import.yaml comments score above 8.5 on Voice" — engaged with V4/Y2/Y10 rules together.
- "Cross-codebase coherence is explicitly out-of-scope per part-4 boundary" — engaged with the explicit out-of-scope section.

This is the recognitional-vs-generative behavior we wanted: the agent is APPLYING rules to fragments, not pattern-matching anchors. Validation of the rule-substrate hypothesis from the diagnosis.
