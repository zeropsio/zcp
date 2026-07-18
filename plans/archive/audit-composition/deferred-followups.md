# Deferred follow-ups — atom-corpus-hygiene 2026-04-26

Per `plans/atom-corpus-hygiene-2026-04-26.md` §15.3 ship-gate
mechanism: "if ANY G1-G8 fails, the executor either: remediates...
OR documents a deferred follow-up in
`plans/audit-composition/deferred-followups.md` with a
justification for why this hygiene cycle ships without it".

## Deferred at SHIP

### G5 — L5 live smoke test on eval-zcp

**What it would have done**: SSH into the eval-zcp `zcp` container,
push the patched binary, issue an MCP STDIO `initialize` →
`status` call for an idle envelope and a develop-active envelope,
verify the wire-frame size matches the probe number ±1 byte,
verify the decoded `text` parses as valid markdown.

**Why deferred**: requires the operator to run SSH against
eval-zcp; the verify gate (G4) green tests + Phase 0..7 probe
runs against the production code path establish the wire-frame
numbers. The probe binary was deleted in G8 but Phase 7 final
rendered-fixture output is preserved in
`plans/audit-composition/rendered-fixtures-post-phase7/` for
byte verification. If a follow-up session has SSH access, this
gate can be run against the released binary.

**Risk if skipped**: low — the verify gate (G4 race-tests) covers
the synthesise + render path; the wire-frame number is a function
of synthesised body + JSON envelope encoding (deterministic).

### G6 — eval-scenario regression check

**What it would have done**: run a known eval scenario from
`internal/eval/scenarios/` against the post-hygiene corpus + the
pre-hygiene corpus, compare agent moves and document divergence.

**Why deferred**: eval scenarios for the simple-deployed user-test
envelope haven't been authored as standalone scenarios; the
user-test feedback in
`plans/audit-composition/user-test-feedback-2026-04-26-followup.md`
serves as the regression baseline. The Phase 7 composition
re-score on simple-deployed (task-relevance 1 → 4 under the
refined rubric) is a strong leading indicator that the user-test
agent's experience improved.

**Risk if skipped**: medium — without a real-agent re-run, the
user-test feedback's headline claim ("~75 % of atoms are
context-window pollution") could only be partially validated by
composition scoring. A future eval-scenario authored against the
simple-deployed envelope will close this gap.

## Phase 6 + Phase 2 deferred dedups

Per `phase-6-tracker.md` and `phase-2-tracker.md`, several
candidates were DEFERRED-TO-FOLLOW-UP-PLAN with rationale:

- **Phase 2 row 1 (push-git mechanics)**: 760 B target. Multi-file,
  axis-care; better tackled with Phase 6 prose tightening.
- **Phase 2 row 2 (push-git downstream trigger)**: 300 B; folds with row 1.
- **Phase 2 row 6 (local-mode topology)**: 520 B; off-probe (local-env fixtures not in baseline-5).
- **Phase 2 rows 8-15**: smaller axis-justified or off-probe; ~1.5 KB total.
- **Phase 6 MEDIUM/HIGH-risk atoms (14 atoms)**: ~6.5 KB additional;
  requires mandatory per-edit Codex rounds; deferred for context-budget.
- **Phase 6 LOW-risk atoms 5-11 (7 atoms)**: ~2.0 KB; mostly off-probe.

Total deferred body recovery: ~11 KB across the corpus. Future
hygiene cycle could pick these up.

## §15.3 G3 first-deploy strict-improvement (formalized deferral)

Codex final-round verdict (round 3, preserved verbatim in
`final-review.md`) returned NO-SHIP citing §15.3 G3 strict-
improvement: redundancy + coverage-gap held flat on first-deploy
fixtures while simple-deployed (the user-test target) strictly
improved. Per §15.3 ship-gate failure-mode the executor either
remediates OR documents the deferred follow-up here:

**Why deferred**: the gap is the broad-atom redundancy issue
(below). First-deploy fixtures' Redundancy = 1 score is driven
by 6+ broad atoms (`develop-api-error-meta`, `develop-env-var-
channels`, `develop-verify-matrix`, `develop-platform-rules-
common`, `develop-auto-close-semantics`, `develop-change-drives-
deploy`) co-rendering with cross-atom paraphrased restatements.
Each individual atom is reasonable; the AGGREGATE is the
redundancy problem.

Resolving requires either (a) cross-cluster broad-atom dedup
(trim each atom of overlapping content), or (b) further axis-
tightening to reduce co-firing breadth. Both are Phase 8+
follow-up work — distinct from the phase 2-6 axis-specific
dedups. Estimated ~3-5 KB additional first-deploy body recovery.

**Risk if skipped**: low for the user-test target (already met
under refined rubric); medium for first-deploy fixture
composition quality (broad atoms still represent a noise-fraction
that a future user-test on first-deploy envelopes might surface).

## Phase 7 broad-atom redundancy

Per `phase-7-tracker.md`: Codex's composition re-score flagged
Redundancy stuck at 1 on 4 of 5 fixtures (simple-deployed
improved to 2). Root cause: broad atoms (`develop-api-error-meta`,
`develop-env-var-channels`, `develop-verify-matrix`,
`develop-platform-rules-common`, `develop-auto-close-semantics`,
`develop-change-drives-deploy`) co-render across many fixtures
with cross-atom restatements above the "7+" threshold.

**Why deferred**: each individual atom is reasonable; the
AGGREGATE problem requires cross-cluster broad-atom dedup work
that's distinct from the phase 2-6 axis-specific dedups. Phase 8+
follow-up.

## Composition coverage-gap on first-deploy fixtures

Codex's Phase 7 re-score happened BEFORE the
`develop-first-deploy-execute-cmds` axis-tightening (which dropped
stage services from execute-cmds, resolving the
direct-vs-cross-promote competing-action conflict). Re-running
composition score post-fix would likely bump first-deploy
coverage-gap from 2-3 toward 4. **Not re-run** for work-economics
(executor's analysis of the conflict pinpointed the fix; the fix
is mechanical; bumping all 4 fixtures' coverage-gap doesn't
unlock anything additional).

## Process-improvement notes for future hygiene cycles

1. **Codex per-edit rounds for HIGH-risk atoms**: Phase 6 deferred
   the 4 HIGH-risk atoms (`develop-ready-to-deploy`,
   `develop-first-deploy-write-app`, `develop-verify-matrix`,
   `develop-deploy-files-self-deploy`) explicitly because per-edit
   rounds add up. Future cycle should budget Codex compute upfront
   for these (~15 minutes per round × 4 = ~1 hour).

2. **isPlaceholderToken trap**: this hygiene cycle hit the
   `{hostname:value}` placeholder leak twice (once in
   bootstrap-recipe-close — F0-DEAD-1 sidecar; once in
   develop-api-error-meta — Phase 6). Future cycles should
   pre-grep for `{[a-z][a-z-]*}` patterns in atom bodies before
   commit, or `synthesize.go::isPlaceholderToken` should reject
   non-placeholder shapes (e.g. tokens with a `:` could be made to
   fail the predicate).

3. **Pin-coverage closure pattern**: the bulk-pin
   `TestScenario_PinCoverage_AllAtomsReachable` pattern (one big
   `requireAtomIDsContain` call against a union of synthesise
   results) made G2 closure mechanical. Future hygiene cycles
   should adopt this pattern early — avoids per-atom scenario
   writing while still satisfying the AST pin-density gate.

---

# Deferred from atom-corpus-hygiene-followup-2026-04-27

The followup cycle shipped SHIP-WITH-NOTES per
`plans/audit-composition/final-review-v2.md` "Executor's response"
section. Cumulative byte recovery massively exceeded §8 binding
target (-29,231 B vs ≥17,000 B = 1.7×). Two PRE-AUTHORIZED notes
+ one EXECUTOR FOLLOW-UP NOTE remain as Phase 8+ work for future
sessions:

## Note 1 — Two-pair fixture STRUCTURAL Redundancy fail (engine-level)

**State**: 4/5 fixtures G3 PASS post-followup; `develop_first_deploy_two_runtime_pairs_standard` Redundancy held at 1.

**Why**: §6.2 rubric explicitly counts per-service hostname-substituted copies as restated facts. The two-pair fixture renders `develop-dynamic-runtime-start-container` twice (`appdev` + `apidev`) and `develop-first-deploy-promote-stage` twice (`appdev → appstage` + `apidev → apistage`). Phase 5/6 trim shrank each rendered atom-body but did not eliminate the per-service rendering itself.

**Status**: NOT a corpus content issue — it's a render-engine behavior. `Synthesize` (in `internal/workflow/synthesize.go`) renders per-matching-service for service-scoped atoms.

**Resolution paths** (pick one for Phase 8+):

A. **Atom-level "render once with service-list/table" support**: extend `Synthesize` to optionally fold per-service renders into a single atom-body with a `{services-list}` or `{services-table}` template token. Atoms can opt-in via a frontmatter axis (e.g. `multi-service: fold`).

B. **Atom-axis tightening to fire on first service only**: add a frontmatter flag like `per-service-render: false` so the atom fires once per envelope regardless of matching-service count. Loses per-service hostname substitution; would need template-time iteration in atom body.

C. **Accept SHIP-WITH-NOTES indefinitely**: the rendered output is correct — both `appdev` + `apidev` get the same dev-server-start guidance with their own hostname substituted. The "redundancy" by §6.2 rubric is real but is the natural shape of multi-service envelopes. Document as "rubric scoring limitation; agent doesn't experience it as friction" and remove from the deferred list.

**Pre-authorized by user 2026-04-27 at Phase 5.2 SHIP-WITH-NOTES prompt.**

**Files involved**: `internal/workflow/synthesize.go` (engine), `internal/content/atoms/develop-dynamic-runtime-start-container.md` + `develop-first-deploy-promote-stage.md` (atoms that duplicate per-service), `internal/workflow/atom.go` (axis vector definition if option A or B chosen).

## Note 2 — Single-service hypothetical fixture comparison-limitation

**State**: 4/5 fixtures G3 PASS; `develop_first_deploy_standard_single_service (hypothetical)` PASSes strict-improvement vs the `standard` baseline (its inferred parent shape) but has no §4.2 first-baseline of its own to anchor strict-improvement claims.

**Why**: this fixture was added LATE in the first cycle (Phase 0 G7 prep) for stretch coverage; the §4.2 baseline scoring round didn't include it. Codex's Phase 7 re-score noted this as a "comparison limitation" rather than a regression.

**Status**: NOT a corpus issue — it's a baseline-scoring-coverage gap.

**Resolution path** (pick one for Phase 8+):

A. **Score the fixture against historical content**: use `git show <pre-first-cycle-commit>:plans/audit-composition/baseline-scores.md` to see if any earlier scoring covered an equivalent-shape fixture. Backfill §4.2 with that data.

B. **Drop the fixture from the §15.3 G3 strict-improvement contract**: declare it stretch-only (axis-J / multi-pair stretch coverage) without G3 pass requirement.

C. **Explicitly score the fixture in `baseline-scores.md` post-hoc** at the first-cycle Phase-0 corpus state (via worktree at the appropriate commit), then re-evaluate G3.

**Pre-authorized by user 2026-04-27 at Phase 5.2 SHIP-WITH-NOTES prompt.**

## Note 3 — K/L/M lint enforcement (executor follow-up)

**State**: §11.5 of `docs/spec-knowledge-distribution.md` documents axes K + L + M as author-facing rules. The lint enforcement (in `internal/content/atoms_lint.go`) covers §11.2 forbidden patterns only — it does NOT yet check for axis-K abstraction-leak patterns, axis-L title env-qualifiers, or axis-M terminology drift.

**Why deferred**: §8 acceptance criterion was "documented in atom-authoring contract" — DOCUMENTATION, not lint enforcement. Lint enforcement is a stronger guarantee (catches drift in future atom edits without requiring a hygiene cycle re-audit) but is out of scope for the followup plan.

**Resolution path** (Phase 8+ candidate):

A. **Axis-K patterns**: codify HIGH-risk signal detection (`Don't run X`, `Never use Y`, etc. tied to operational choice) and FORBID their removal without per-edit Codex review. Probably needs git-blame-aware tooling (didn't get removed in this commit; needs a "deletions check" in the lint pass).

B. **Axis-L patterns**: detect env-only qualifiers in atom titles (`(container)` / `(local)` / `— container`) and lint-fail when present. Easy to enforce via regex.

C. **Axis-M patterns**: detect drift in cluster-#1 (container concept) — fail when a single atom uses both "the container" and "runtime container" inconsistently. Per-cluster decision-table enforcement.

**Files involved**: `internal/content/atoms_lint.go` (extend `atomLintAllowlist` + add new pattern checks), `internal/content/atoms_lint_test.go` (test cases for new rules), `docs/spec-knowledge-distribution.md` §11.5 (update "not lint-enforced (yet)" note when enforcement lands).

## Note 4 — Codex sandbox `TestOnce_NoUpdate_Returns` environment artifact

**State**: in the followup cycle's final SHIP VERDICT round, Codex's sandbox observed `FAIL` lines on `internal/update.TestOnce_NoUpdate_Returns` when running `go test ./... -count=1 -short`. The dev-box authoritative environment showed 0 FAIL across multiple sequential runs (5/5 PASS in 5 sequential `-run TestOnce_NoUpdate_Returns -v`; 3/3 PASS on full -short suite; 1/1 PASS on -race).

**Why deferred**: this is an environment artifact, not a corpus regression. The test uses `httptest.NewServer` (localhost-bound), `t.Setenv`, `t.TempDir`, and isolated `CacheDir` — no external network or time-of-day dependency. Codex's sandbox likely restricts loopback HTTP binding or `t.Setenv` of `ZCP_UPDATE_URL`.

**Resolution path** (Phase 8+ candidate):

A. **Diagnose Codex sandbox restriction**: re-run the test inside the sandbox with verbose output + strace/ktrace equivalent to identify the failure point. Patch the test to skip cleanly under sandbox restrictions OR fix the sandbox shim.

B. **Add a sandbox-compat skip**: detect sandbox-restricted environment in the test setup and skip with `t.Skip("sandbox env: ...")` while still running on dev-box / CI.

C. **Document as canonical resolution**: future hygiene-followup cycles invoking Codex's FINAL-VERDICT round may see the same FAIL. Document the dev-box-is-authoritative resolution as the canonical path; no test fix needed.

**Files involved**: `internal/update/once_test.go::TestOnce_NoUpdate_Returns`, `internal/update/once.go`.

---

# Pickup contract for future sessions

A fresh Claude session picking up Phase 8+ work should:

1. Read this file (`plans/audit-composition/deferred-followups.md`) end-to-end.
2. Read `plans/archive/atom-corpus-hygiene-followup-2026-04-27.md` for the followup plan's context (especially §16 amendments + §3 axes K/L/M definitions).
3. Read `plans/audit-composition/final-review-v2.md` "Executor's response" for the SHIP-WITH-NOTES disposition rationale.
4. Read the Phase 7 v2 tracker (`phase-7-tracker-v2.md`) for the final state at PLAN COMPLETE.
5. Pick ONE of the four notes above and write a focused plan (`plans/<note-N-slug>-<date>.md`) addressing it. Do NOT bundle multiple notes into one plan — each has different scope, risk profile, and resolution paths.

Each note's resolution path A/B/C is a starting point. The actual plan must include §17-style prereq checklist + §15-style EXIT criteria + Codex protocol per inherited §10.

