# Codex round P0 PRE-WORK — followup plan approach validation

Date: 2026-04-27
Round type: PRE-WORK (per §10.1 P0 row 1, repurposed for followup plan per §13 step 4)
Plan reviewed: plans/atom-corpus-hygiene-followup-2026-04-27.md
Reviewer: Codex
Reviewer brief: skeptical fresh-eyes approach validation; cite file:line for every claim

## Concerns reviewed (executor's pre-flight)

### C1 — Axis K judgment-test ambiguity
Status: GAP

The plan does contain a conservative rule: Axis K says per-leak Codex review is skipped only when the leak clearly satisfies the judgment test, with borderline cases getting a Codex round (`plans/atom-corpus-hygiene-followup-2026-04-27.md:96-100`), and the risk section says "when uncertain, KEEP" (`plans/atom-corpus-hygiene-followup-2026-04-27.md:680-683`). But the concrete example "No SSHFS mount in local mode" is classified as DROP because the agent "doesn't know SSHFS is a thing" (`plans/atom-corpus-hygiene-followup-2026-04-27.md:91-94`), while current local atoms explicitly contain exactly this class of negative guidance: "no SSHFS, no dev container" in `develop-close-push-dev-local` (`internal/content/atoms/develop-close-push-dev-local.md:14-15`) and "there's no dev container to cross-deploy from" in `develop-push-dev-deploy-local` (`internal/content/atoms/develop-push-dev-deploy-local.md:15-17`). That means the plan's example is too categorical for agents that may carry container-flow expectations across turns. Amendment: revise Axis K to say local/container negative guidance is DROP only when it names a mechanism the current envelope could not plausibly invite; if it prevents a likely cross-flow reflex such as SSHFS/dev-container/cross-deploy in local mode, classify KEEP-AS-GUARDRAIL or REPHRASE. Add "uncertain → KEEP + fact-inventory rationale" directly under the DROP example, not only in the later risk note.

### C2 — Axis L compound qualifiers
Status: GAP

The corpus does contain the exact compound-title pattern from the concern: `develop-push-dev-workflow-dev` has title `"Push-dev iteration cycle (dev mode, container)"` (`internal/content/atoms/develop-push-dev-workflow-dev.md:8-11`) and `develop-push-dev-workflow-simple` has `"Push-dev iteration cycle (simple mode, container)"` (`internal/content/atoms/develop-push-dev-workflow-simple.md:6-10`). A comma-split works for those two, but it is not a reliable general rule because some env-qualified titles also contain load-bearing non-env qualifiers in the same title, e.g. `strategy-push-git-push-container` says `"container env (GIT_TOKEN + .netrc)"` (`internal/content/atoms/strategy-push-git-push-container.md:6-8`) and `strategy-push-git-push-local` says `"local env (user's git)"` (`internal/content/atoms/strategy-push-git-push-local.md:6-8`). Dropping the whole suffix would lose the credential distinction those atoms explain. Amendment: Axis L should require token-level title edits: remove only redundant env tokens such as `container`, `local`, `container env`, `local env`; preserve mechanism qualifiers such as `GIT_TOKEN + .netrc` and `user's git`, or rewrite them into env-neutral titles.

### C3 — Axis M canonical choices, especially "container"
Status: GAP

The plan's proposed canonical for the container concept is explicitly context-dependent (`plans/atom-corpus-hygiene-followup-2026-04-27.md:142-145`), and the current corpus supports that: `develop-first-deploy-execute` uses "Zerops container" for the empty pre-deploy runtime, `develop-push-dev-deploy-container` uses "dev container" for SSH push mechanics, `develop-platform-rules-common` uses "new container" for deploy replacement semantics, `develop-deploy-files-self-deploy` distinguishes build and runtime containers, and `develop-checklist-simple-mode` calls `{hostname}` the single runtime container. The risk is not that per-atom canonicalization is unrealistic; it is that Phase 4 says "pick canonical term per cluster" and "apply via grep + targeted edit" (`plans/atom-corpus-hygiene-followup-2026-04-27.md:461-465`), then samples only about 10% of touched atoms (`plans/atom-corpus-hygiene-followup-2026-04-27.md:466-468`). Amendment: add a mini decision table for "container": use `dev container` only for mutable push-dev/SSHFS contexts, `runtime container` for running service instances, `build container` for build-stage filesystem, and `Zerops container` only for broad first-introduction framing. Require per-occurrence review for this cluster, not 10% sampling.

### C4 — Phase 5/6 ordering
Status: GAP

Phase 5 includes `develop-verify-matrix` in the six broad atoms to deduplicate (`plans/atom-corpus-hygiene-followup-2026-04-27.md:480-489`), and Phase 6 then lists `develop-verify-matrix` as one of four HIGH-risk prose-tightening atoms (`plans/atom-corpus-hygiene-followup-2026-04-27.md:529-536`). The current `axis-b-candidates.md` baseline gives `develop-verify-matrix` at 1715 bytes with a 480-byte HIGH-risk tightening estimate, but Phase 5 may change that atom before Phase 6 starts. Phase 6 first is not necessary — broad dedup first reduces the surface area before prose tightening. Amendment: keep the ordering, but add a Phase 6 ENTRY requirement to re-baseline every Phase 6 atom touched in Phase 5, regenerate byte estimates, and treat prior `axis-b-candidates.md` numbers as stale for those atoms.

### C5 — Phase 1 G5/G6 before content phases vs after
Status: GAP

Phase 1 closes G5/G6 before any Axis K/L/M or broad-atom edits (`plans/atom-corpus-hygiene-followup-2026-04-27.md:241-245`, `plans/atom-corpus-hygiene-followup-2026-04-27.md:391-395`), while Phase 7 re-renders and re-scores fixtures but does not explicitly re-run the live smoke or eval regression (`plans/atom-corpus-hygiene-followup-2026-04-27.md:561-579`). Acceptance says G5 and G6 are closed by Phase 1 (`plans/atom-corpus-hygiene-followup-2026-04-27.md:647-650`), but later phases edit atom bodies and titles, including HIGH-risk broad atoms. Amendment: add a Phase 7 pre-SHIP step to re-run at least the G5 status smoke and the chosen G6 scenario against the post-Phase-6 corpus; Phase 1 can establish baseline availability, but only the Phase 7 rerun should satisfy final G5/G6.

### C6 — G3 strict-improvement on simple-deployed
Status: GAP

Original G3 requires all five baseline fixtures to be re-scored, with coherence/density/task-relevance non-decreasing and redundancy/coverage-gap strictly improving (`plans/atom-corpus-hygiene-2026-04-26.md:1832-1840`). The follow-up plan says Phase 5 closes G3 by improving first-deploy redundancy (`plans/atom-corpus-hygiene-followup-2026-04-27.md:517-519`), but its own target says simple-deployed "already at 2; should hold or improve" (`plans/atom-corpus-hygiene-followup-2026-04-27.md:510-512`). Holding does not satisfy strict improvement, and Phase 5 does not target coverage-gap at all, even though the first cycle final review says G3 failed because redundancy and coverage-gap did not strictly improve across all five fixtures (`plans/audit-composition/final-review.md:21-26`). Amendment: either explicitly amend G3 before Phase 1, or keep clean-SHIP ambition and add required Phase 5/7 work to demonstrate redundancy and coverage-gap strict improvement or flat-at-5 for every fixture, including simple-deployed.

### C7 — Cumulative body recovery target arithmetic
Status: GAP

The follow-up plan has conflicting binding targets. Acceptance says "Cumulative body recovery ≥ 13 KB" while explaining first cycle 11.3 KB plus this cycle ~6.5 KB equals ~17 KB upper end and realistic target ~14-15 KB (`plans/atom-corpus-hygiene-followup-2026-04-27.md:652-654`). Section 12 then says the five-fixture aggregate cumulative target is ≥17,000 B (`plans/atom-corpus-hygiene-followup-2026-04-27.md:847-851`) and says Phase 7 should track cumulative ≥17 KB explicitly (`plans/atom-corpus-hygiene-followup-2026-04-27.md:853-856`). Amendment: make one number binding before Phase 1. Recommended: final G8/G3 evidence should track additional ≥6,000 B and cumulative ≥17,000 B; describe 14-15 KB only as a forecast/risk note, not an acceptance threshold.

### C8 — Per-leak risk classification for Axis K
Status: GAP

Phase 2 says HIGH-risk leaks are REPHRASE or borderline DROP, while LOW-risk leaks are "clear DROP per the judgment test" (`plans/atom-corpus-hygiene-followup-2026-04-27.md:397-411`). That still leaves the executor to decide what "clear" means, and the plan itself admits the judgment test is subjective. Because local/container negative guidance can be a real guardrail in current atoms (`internal/content/atoms/develop-close-push-dev-local.md:14-23`, `internal/content/atoms/develop-dynamic-runtime-start-local.md:60-62`), under-classification is a realistic failure mode. Amendment: codify HIGH-risk signals: negation of a tool/action, SSHFS/dev-container/local/container contrast, command-selection guidance, recovery guidance, or any sentence with "do not"/"never"/"no X" tied to an operational choice. Only pure implementation trivia with no command/action consequence should be LOW-risk DROP.

### C9 — DROP classification audit trail
Status: GAP

The inherited methodology requires fact inventories in commit messages (`plans/atom-corpus-hygiene-2026-04-26.md:320-340`), and Phase 2 has a POST-WORK round to catch dropped guardrails (`plans/atom-corpus-hygiene-followup-2026-04-27.md:415-416`). But LOW-risk Axis K leaks can be dropped without per-edit Codex review (`plans/atom-corpus-hygiene-followup-2026-04-27.md:409-411`), and a single phase-level POST-WORK round may have to review every Phase 2 commit after the fact. Amendment: add an Axis K DROP ledger with one row per dropped leak: atom, exact pre-edit sentence, DROP rationale, risk class, and reviewer status. Require Codex POST-WORK to sample all HIGH/borderline rows and at least every LOW-risk DROP containing "no", "never", "do not", SSHFS, container, local, SSH, git, deploy, or tool names.

### C10 — §11 guardrails cover atom-body edits but NOT plan-shape changes
Status: MISCATEGORISED

The concern is real as an execution scenario, but it is covered outside §11. The follow-up plan explicitly inherits the original plan's Codex protocol, completeness machinery, and prereq checklist (`plans/atom-corpus-hygiene-followup-2026-04-27.md:205-208`), and the original failure-recovery section says if the executor finds an unanticipated issue mid-phase, STOP, document `phase-N-issues.md`, run a Codex investigation, and decide whether to fix, defer, or abandon (`plans/atom-corpus-hygiene-2026-04-26.md:1871-1874`). The original protocol also says if Codex rounds are not being consumed, update §10.1 mid-execution and document the update in `protocol-amendments.md` (`plans/atom-corpus-hygiene-2026-04-26.md:1594-1597`). A Phase 5 discovery that the broad list is 8 atoms rather than 6 fits those inherited protocols.

## Concerns I'd raise (Codex original findings)

### C11 — Phase 1 deferral escape conflicts with clean acceptance

Phase 1 EXIT allows G5/G6 to be "explicitly DOCUMENTED as deferred" for a genuine infra blocker (`plans/atom-corpus-hygiene-followup-2026-04-27.md:383-388`), but acceptance says all G1-G8 must be satisfied with "no DEFERRED-WITH-JUSTIFICATION" and specifically says G5 and G6 close in Phase 1 (`plans/atom-corpus-hygiene-followup-2026-04-27.md:646-650`). That gives the executor a path to proceed after Phase 1 in a state that the plan later says cannot ship. Amendment: Phase 1 may record an infra blocker, but it must not count as Phase 1 EXIT for a clean-followup plan; if infra blocks G5/G6, stop before Phase 2 or explicitly revise the plan verdict target to SHIP-WITH-NOTES.

### C12 — G5 wire-frame variance is weakened too far

The original L5 gate treated probe/wire-frame mismatch as a ship-blocker until root-caused (`plans/atom-corpus-hygiene-2026-04-26.md:840-848`). The follow-up relaxes the assertion to ±5% or ±50 bytes (`plans/atom-corpus-hygiene-followup-2026-04-27.md:266-271`), which is reasonable, but then says if variance is larger than 50 bytes, document and proceed because smoke-test purpose is end-to-end function (`plans/atom-corpus-hygiene-followup-2026-04-27.md:284-288`). That no longer verifies probe accuracy. Amendment: permit proceeding with content phases after a functional smoke, but do not mark G5 GREEN or final-shippable until large variance is root-caused, threshold-adjusted with evidence, or explicitly downgraded to a documented deferral.

### C13 — Axis M sampling is too weak for corpus-wide terminology rewrites

Phase 4 plans to enumerate all inconsistent terms and apply targeted edits (`plans/atom-corpus-hygiene-followup-2026-04-27.md:457-465`), but verification is only a "~10% of touched atoms" Codex sampling round (`plans/atom-corpus-hygiene-followup-2026-04-27.md:466-468`). Current corpus examples show the same word "container" encodes distinct concepts in adjacent atoms. Amendment: require a per-cluster occurrence ledger and Codex review of all touched occurrences for high-risk clusters (`container`, `deploy/redeploy`, `Zerops/platform/ZCP`), with 10% sampling allowed only for low-risk wording clusters.

### C14 — Phase 6 medium-risk work units are under-specified in the follow-up plan

Phase 6 names the 4 HIGH-risk atoms and 7 LOW-risk atoms (`plans/atom-corpus-hygiene-followup-2026-04-27.md:529-547`), but it only says "MEDIUM-risk atoms (14 from prior cycle)" without naming them (`plans/atom-corpus-hygiene-followup-2026-04-27.md:548-549`). The prior `axis-b-candidates.md` has concrete rows and risk classes but the follow-up plan's Phase 6 tracker cannot be checked for completeness unless the executor knows the exact atom list. Amendment: before Phase 1, amend Phase 6 to include the exact MEDIUM list or require Phase 6 ENTRY to regenerate `axis-b-candidates-v2.md` and use that as the authoritative work-unit list.

### C15 — Phase 5 only addresses redundancy, not the documented coverage-gap half of G3

The follow-up plan frames Phase 5 as the closure path for §15.3 G3 (`plans/atom-corpus-hygiene-followup-2026-04-27.md:474-481`), but the work scope is only cross-atom restated facts among six broad atoms. The first cycle score artifact says coverage-gap strict improvement was also only achieved on simple-deployed and was not revalidated for first-deploy fixtures (`plans/audit-composition/post-hygiene-scores.md:73-80`), and final review says G3 failed because both redundancy and coverage-gap did not strictly improve across all five fixtures (`plans/audit-composition/final-review.md:21-26`). Amendment: add a Phase 5 or Phase 7 coverage-gap sub-pass with explicit expected deltas and re-score criteria, or narrow the stated claim from "G3 closure" to "redundancy closure" and add another phase/work unit for coverage-gap.

## Verdict

VERDICT: NEEDS-REVISION

- amendment 1: Revise Axis K to treat local/container negative guidance as guardrail unless clearly pure anti-information; codify HIGH-risk signals and the "uncertain → KEEP" rule next to the DROP example.
- amendment 2: Revise Axis L to remove only env tokens from compound titles while preserving mechanism qualifiers such as credentials, mode, strategy, or runtime distinctions.
- amendment 3: Add an Axis M container decision table and require per-occurrence review for high-risk terminology clusters.
- amendment 4: Keep Phase 5 before Phase 6, but add Phase 6 ENTRY re-baselining for atoms touched by Phase 5, especially `develop-verify-matrix`.
- amendment 5: Add final post-Phase-6/Phase-7 reruns for G5 and G6; Phase 1 runs can establish baseline but should not be the final ship evidence.
- amendment 6: Resolve G3 wording: require strict improvement or flat-at-5 for redundancy and coverage-gap across all five fixtures, or explicitly amend the clean-SHIP gate before execution.
- amendment 7: Pick one binding body-recovery target; recommended binding target is additional ≥6,000 B and cumulative ≥17,000 B across five fixtures.
- amendment 8: Add an Axis K DROP ledger and second-look protocol for dropped facts, especially negative operational guidance.
- amendment 9: Reconcile Phase 1's deferred-exit language with acceptance's no-deferral clean-SHIP requirement.
- amendment 10: Name Phase 6 MEDIUM-risk atoms or require a regenerated authoritative Phase 6 candidate artifact before Phase 6 work starts.

## Executor verification of Codex citations

Per memory rule `feedback_codex_verify_specific_claims.md`, every concrete Codex
file:line claim was spot-checked. 5 of 5 sampled citations verify exactly:

- `develop-close-push-dev-local:14` "Local mode builds from your committed tree
  — no SSHFS, no dev container." ✅
- `develop-push-dev-deploy-local:15-17` "there's no dev container to
  cross-deploy from" ✅
- `develop-push-dev-workflow-dev:8` title `"Push-dev iteration cycle (dev mode,
  container)"` ✅
- `strategy-push-git-push-container:6` title `"push-git push setup — container
  env (GIT_TOKEN + .netrc)"` ✅
- `strategy-push-git-push-local:6` title `"push-git push setup — local env
  (user's git)"` ✅
- C4 verify-matrix overlap: `develop-verify-matrix` literally listed in BOTH
  Phase 5 broad atoms (`followup-plan:486`) AND Phase 6 HIGH-risk atoms
  (`followup-plan:533`) ✅

All 11 amendments (10 numbered + C12 wire-frame variance) are well-grounded in
the corpus + plan text. Executor will apply them in §16 (new appendix) and
in-place edits to §3, §5, §8, §12 of the followup plan before entering Phase 1.

## Executor disposition

VERDICT acted on: NEEDS-REVISION → plan revised in commit
`<phase-0-amendments-commit>`. Phase 0 EXIT can complete only after the plan
revision lands and the verify gate runs green. Phase 1 entry blocked until
amendments commit.
