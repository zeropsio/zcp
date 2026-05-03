# E2E bootstrap-helper two-phase start wiring

**Surfaced**: 2026-05-03 — while validating the local-mode workingDir fix
on the live `eval-zcp` platform. The e2e harness's bootstrap-workflow
helpers and several deploy-related e2e tests assume a single-phase
`zerops_workflow action="start" workflow="bootstrap"` returns a
`sessionId` and commits a workflow context. The engine's actual flow has
been two-phase for some time:

1. `action=start workflow=bootstrap` (no route) → returns
   `BootstrapDiscoveryResponse` with route options. **No session
   committed**, no sessionId in the body.
2. `action=start workflow=bootstrap route=classic` (or other route) →
   commits the session, returns `sessionId` + progress.

Affected tests (verified failing 2026-05-03 with `WORKFLOW_REQUIRED`
on subsequent `zerops_import`):

- `TestE2E_LocalDeploy_Success` (`e2e/deploy_local_test.go`)
- `TestE2E_LocalDeploy_Schema` (same file — schema-only, unaffected)
- `TestE2E_Subdomain` (`e2e/subdomain_test.go`)
- `TestE2E_SubdomainEnableUrls` (`e2e/discover_subdomain_test.go`)
- `TestE2E_StandardPairFirstDeploy_AutoEnablesSubdomain`
  (`e2e/subdomain_autoenable_test.go`, skipped pending this fix)
- All tests using `bootstrapAndProvision` /
  `bootstrapAndProvisionExpectFail` helpers
  (`e2e/bootstrap_modes_test.go`, `e2e/bootstrap_advanced_test.go`,
  `e2e/bootstrap_negative_test.go`, etc.)

Likely also: `TestE2E_BootstrapFresh_FullFlow` and friends in
`e2e/bootstrap_workflow_test.go` (uses inline single-phase start).

**Why deferred**: scope. The local-mode workingDir fix that surfaced
this is shipped + unit-tested. The broader e2e harness sweep is its own
substantial migration — every test using the old single-phase pattern
needs an audit + update, and each test's flow may have its own quirks
(some need `route=classic`, some need `route=adopt`, the recipe and
develop workflows have different starts entirely). Risk if rushed:
masking real test failures behind silent skip-on-API-shift.

**Trigger to promote**: any of —

- Any new e2e that needs to drive the bootstrap workflow end-to-end
  (auto-enable subdomain e2e is the immediate consumer, but more will
  come).
- A release blocker surfacing because of an e2e regression that the
  broken harness was hiding.
- Routine quarterly e2e harness review.

## Sketch

Two-part work:

**Part A — helper rewrite** (small, atomic):
1. Update `e2e/bootstrap_helpers_test.go::bootstrapAndProvision` to
   call `action=start` twice (Phase 1 discovery, Phase 2 commit with
   `route=<chosen>`). Default to `route=classic` for fresh provisions.
2. Mirror in `bootstrapAndProvisionExpectFail`.
3. Anywhere a test inlines `action=start` + asserts on sessionId
   (search `e2e/`, replace with the helper).

**Part B — per-test audit** (larger, may surface real assertions
broken by the API change):
1. Run the full e2e suite against `eval-zcp` after Part A.
2. For each remaining failure, decide: API-shift broke the test (fix
   it), or the test's intent no longer maps to current behavior
   (rewrite or delete).

After Part A lands, the auto-enable e2e
(`e2e/subdomain_autoenable_test.go`) can be unskipped and run end-to-
end. It's the immediate validation surface for the
2026-05-03 subdomain foundation fix.

## Refs

- `plans/archive/subdomain-auto-enable-foundation-fix-2026-05-03.md`
- `plans/archive/local-mode-workingdir-fix-2026-05-03.md` (this work,
  if archived as a separate plan)
- `internal/workflow/bootstrap.go::BootstrapDiscoveryResponse`
- `internal/workflow/engine.go::BootstrapStartWithRoute`
- `internal/tools/workflow.go::handleBootstrapStart`
- `e2e/bootstrap_helpers_test.go::bootstrapAndProvision`
