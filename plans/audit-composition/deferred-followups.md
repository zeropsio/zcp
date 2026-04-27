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
