# E2E deploy preflight workingDir resolution wiring

**Surfaced**: 2026-05-03 — Phase 0 of subdomain auto-enable foundation fix.
While writing `e2e/subdomain_autoenable_test.go` to prove the RED state,
the test stubbed at the deploy-preflight gate: `findAndParseZeropsYml`
expects yaml at `<projectRoot>/<sourceHostname>/zerops.yaml` (mount-style
path) for self-deploys, but local e2e tests use temp directories
(`workingDir=/tmp/...`). Pre-existing pattern in other e2e tests
(`subdomain_test.go`, `discover_subdomain_test.go`) — they rely on
deploy paths that don't engage preflight, or on bootstrap workflow
state that's also broken (see related entry below).

**Why deferred**: the foundation fix proof landed at the unit level (3
phase-1-RED tests in `internal/tools/deploy_subdomain_test.go`) plus
live verification via the `probe` service experiment recorded in the
plan §2.2. The e2e test body is committed but skipped with `phase-3:
end-to-end harness still needs deploy-preflight workingDir resolution
wiring`. Phase 1 is the load-bearing change and works without this
e2e — but the broader e2e suite for recipe-authoring / override
re-import / worker / adopt scenarios named in the plan §5 Phase 3
also depends on this wiring.

**Trigger to promote**: any of —

- Next time someone touches the deploy preflight path, fix the local-env
  workingDir resolution as part of that change.
- Running into another e2e that needs to assert post-deploy state and
  bumps the same bug.
- Phase 4 / 5 of any future develop-flow plan that wants to use these
  e2e scenarios.

## Sketch

Two-part fix:

1. `internal/tools/workflow_checks_deploy.go::findAndParseZeropsYml` —
   when `sourceHostname == ""` (local env), accept either
   `<projectRoot>/zerops.yaml` (current) OR `<workingDir>/zerops.yaml`
   passed in from the deploy handler. The local-mode deploy handler
   already has `input.WorkingDir` in scope — thread it through.

2. `e2e/bootstrap_helpers_test.go::bootstrapAndProvision` — also broken,
   pre-existing. Helper expects sessionId from `action=start workflow=bootstrap`
   without route, but the engine's two-phase flow returns discovery
   options (no sessionId) until `route=classic` is committed. Either
   update helper to do two-phase start, or add a new helper for the
   foundation-fix e2e suite.

After both fixes, the four scenarios from
plans/archive/subdomain-auto-enable-foundation-fix-2026-05-03.md
Phase 3 can be enabled:

- `TestE2E_StandardPairFirstDeploy_AutoEnablesSubdomain` (already written, skipped)
- `TestE2E_RecipeAuthoringDeploy_AutoEnablesSubdomain`
- `TestE2E_OverrideReimport_AutoEnablesSubdomain`
- `TestE2E_WorkerService_BenignSkip` (with companion unit test)
- `TestE2E_AdoptedService_AutoEnablesOnDeploy`

## Refs

- `plans/archive/subdomain-auto-enable-foundation-fix-2026-05-03.md` §5 Phase 3
- `e2e/subdomain_autoenable_test.go` (skipped scaffold)
- `internal/tools/workflow_checks_deploy.go::findAndParseZeropsYml`
- `e2e/bootstrap_helpers_test.go::bootstrapAndProvision` (pre-existing tech debt)
