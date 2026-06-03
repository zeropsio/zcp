# Deploy-failure recovery-hint completeness + classifier operand mis-attribution

**Surfaced**: 2026-06-03, P0c diverse flow-eval pass (recipe-laravel-minimal-standard
run 20260603-075230; recipe-nextjs-ssr-frontend-standard run 20260603-082950). Two
distinct deploy-failure UX gaps, both pre-existing (NOT P0c develop-guidance regressions);
cohesive cluster (failed-build recovery), split if promoted independently.

**Why deferred**: out of P0c scope (develop-guidance de-bloat). Both touch the
deploy-failure / diagnose-before-destruct machinery (prior-session F-area:
`ErrDiagnosisRequired`, `tools.DiagnosedDestruction`, `ops/deploy_failure*.go`),
not the atom corpus. Recording because finding #1 is RECURRING (2 evals).

## Finding 1 — DIAGNOSIS_REQUIRED recovery hint is missing copy-pasteable fields (recurring)
After a failed build on a stage service in READY_TO_DEPLOY, redeploy is refused with
`DIAGNOSIS_REQUIRED` → reimport with `override` + `confirmDestructive`. Agents lost
round-trips because the recovery hint didn't carry every field they had to send:
- **nextjs**: `confirmDestructive` needed `acknowledgedTargets` + `diagnosedFailureClass`;
  the `diagnosedFailureClass` key "wasn't in the error response's recovery hint — I
  constructed it by inference."
- **laravel**: the reimport needed `startWithoutCode: true`; the agent did override-only
  on the first try, landed stage in READY_TO_DEPLOY again, and needed a SECOND reimport.
Both: the `recovery.args` (or `wouldDestroy`/`DiagnosedDestruction` payload) should be a
COMPLETE copy-paste — every required field (override, startWithoutCode, acknowledgedTargets,
diagnosedFailureClass) present with values, so the agent doesn't infer/miss one and re-loop.
Fix lives at the `ErrDiagnosisRequired` rejection payload + the develop/import recovery hint.
This is squarely "give the agent EXACT, paste-and-resend info" (the P0c meta-principle),
applied to the destructive-recovery path.

## Finding 2 — build-failure classifier mis-attributes missing-operand as command-not-found
nextjs: a cross-deploy `buildCommands` ran `cp -r public .next/standalone/public`; `public/`
didn't exist in the build container (empty dirs don't survive the git push). The
`failureClassification` said "buildCommands referenced a binary that doesn't exist" with
signal `build:command-not-found` — but `cp` EXISTS; the OPERAND (`public/`) was missing.
The classification would send an agent to install something via `prepareCommands` (wrong fix)
instead of making the `cp` resilient / ensuring the dir exists. Classifier:
`internal/ops/deploy_failure*.go` pattern library. Fix: distinguish "command not found"
(shell: `command not found` / `not found`) from "operand/path not found"
(`cp: cannot stat 'X': No such file or directory`) — different signal + suggestedAction.
Single signal; verify the real builder error text before tightening the pattern.

## Refs
- Retros: `eval/behavioral/runs/20260603-075230/recipe-laravel-minimal-standard/self-review.md`,
  `eval/behavioral/runs/20260603-082950/recipe-nextjs-ssr-frontend-standard/self-review.md`
- Diagnose-before-destruct invariant: CLAUDE.md "Diagnose-before-destruct gates…"
  (`ErrDiagnosisRequired`, `tools.DiagnosedDestruction`, `ops.LatestFailedAppVersionContext`).
- Classifier: `internal/ops/deploy_failure*.go` + `topology.FailureClass`.
