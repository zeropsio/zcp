# Deploy-failure recovery-hint completeness + classifier operand mis-attribution

**Surfaced**: 2026-06-03, P0c diverse flow-eval pass (recipe-laravel-minimal-standard
run 20260603-075230; recipe-nextjs-ssr-frontend-standard run 20260603-082950). Two
distinct deploy-failure UX gaps, both pre-existing (NOT P0c develop-guidance regressions);
cohesive cluster (failed-build recovery), split if promoted independently.

**Why deferred**: out of P0c scope (develop-guidance de-bloat). Both touch the
deploy-failure / diagnose-before-destruct machinery (prior-session F-area:
`ErrDiagnosisRequired`, `tools.DiagnosedDestruction`, `ops/deploy_failure*.go`),
not the atom corpus. Recording because finding #1 is RECURRING (2 evals).

## Finding 1 — CLOSED 2026-09-13
Superseded by `docs/spec-workflows.md §8 R2`: the override gate emits a ready-made retry only for a never-deployed misconfigured service (with `startWithoutCode`, `acknowledgedTargets`, `diagnosedFailureClass` filled); every failed-build shape gets `zerops_events` → `zerops_deploy` instead, so the copy-paste completeness question no longer arises there.

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
