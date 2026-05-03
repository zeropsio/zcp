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

## Cold-pickup recipe

Anyone in a fresh session can reproduce + start work with these steps:

```bash
# 1. Pull API key from .mcp.json (per CLAUDE.local.md — eval-zcp project).
export ZCP_API_KEY=$(grep -o '"ZCP_API_KEY": "[^"]*"' .mcp.json | sed 's/.*"\(.*\)".*/\1/')

# 2. Reproduce the failure on any of the affected tests.
go test ./e2e/ -tags e2e -count=1 -v -run TestE2E_LocalDeploy_Success -timeout 120s

# Expected error in output:
#   {"code":"WORKFLOW_REQUIRED","error":"No active workflow. ..."}
# Tool call sequence shows: zerops_workflow action=start (returns discovery),
# then zerops_import (fails because no session was committed).
```

If you want to see what each phase actually returns, add a `t.Logf("%s",
text)` after the `mustCallSuccess` calls in
`e2e/bootstrap_helpers_test.go` `bootstrapAndProvision` and re-run.

## Phase-1 response shape (no route — discovery)

```json
{
  "intent": "...",
  "projectId": "...",
  "routeOptions": [
    {"route": "classic", "rank": 1, "...": "..."},
    {"route": "adopt", "rank": 2, "...": "..."}
  ],
  "message": "..."
}
```

No `sessionId`. No `progress`. No `current` step.

## Phase-2 response shape (route=classic — commit)

```json
{
  "sessionId": "abc123...",
  "intent": "...",
  "progress": {
    "total": 3,
    "completed": 0,
    "steps": [
      {"name": "discover", "status": "in_progress"},
      {"name": "provision", "status": "pending"},
      {"name": "close", "status": "pending"}
    ]
  },
  "current": {"name": "discover", "index": 0}
}
```

This is the shape `bootstrapProgress` (`e2e/bootstrap_workflow_test.go:24`)
expects. It's only returned by Phase 2.

## Affected tests

Verified failing 2026-05-03 with `WORKFLOW_REQUIRED` on subsequent
`zerops_import`:

- `TestE2E_LocalDeploy_Success` (`e2e/deploy_local_test.go`)
- `TestE2E_Subdomain` (`e2e/subdomain_test.go`)
- `TestE2E_SubdomainEnableUrls` (`e2e/discover_subdomain_test.go`)
- `TestE2E_StandardPairFirstDeploy_AutoEnablesSubdomain`
  (`e2e/subdomain_autoenable_test.go`, currently skipped pending this fix)

Verified failing 2026-05-03 with `expected non-empty sessionId`
(blocks at `bootstrapAndProvision` helper line 73):

- All tests in `e2e/bootstrap_modes_test.go` (Bootstrap_SimplePhpNginxMariadb,
  Bootstrap_DevGoValkey, Bootstrap_StandardPythonNats,
  Bootstrap_SimpleStaticObjStorage, Bootstrap_DevDotnetPostgres)
- `TestE2E_BootstrapFresh_FullFlow` (`e2e/bootstrap_workflow_test.go`)

Suspected affected (use the same helpers / patterns):

- `e2e/bootstrap_advanced_test.go` (whatever Bootstrap_* tests live there)
- `e2e/bootstrap_negative_test.go` (likely uses
  `bootstrapAndProvisionExpectFail`)
- `e2e/import_provenance_test.go:51` (inline single-phase start)
- `e2e/laravel_deploy_test.go:30` (inline single-phase start)
- `e2e/build_logs_test.go:50`, `e2e/deploy_error_classification_test.go:63`,
  `e2e/deploy_prepare_fail_test.go:49` (inline starts)

Not affected (different workflow or no workflow start):

- `TestE2E_LocalDeploy_Schema` (schema introspection only — no live calls)
- Anything that only calls `zerops_recipe` (different workflow, different
  start contract — verify if you touch it)

**Why deferred**: scope. The local-mode workingDir fix that surfaced
this is shipped + unit-tested (commit `5d64b12a`). The broader e2e
harness sweep is its own substantial migration — every test using the
old single-phase pattern needs an audit + update, and each test's flow
may have its own quirks (some need `route=classic`, some need
`route=adopt`, the recipe and develop workflows have different starts
entirely). Risk if rushed: masking real test failures behind silent
skip-on-API-shift.

**Trigger to promote**: any of —

- Any new e2e that needs to drive the bootstrap workflow end-to-end
  (auto-enable subdomain e2e is the immediate consumer, but more will
  come).
- A release blocker surfacing because of an e2e regression that the
  broken harness was hiding.
- Routine quarterly e2e harness review.

## Promotion path

Per `plans/backlog/README.md`: when promoting, extract this into
`plans/e2e-bootstrap-helper-two-phase-2026-MM-DD.md` (full plan with
status/phases). This entry can then `git rm` itself.

## Sketch

Two-part work:

**Part A — helper rewrite** (small, atomic, ~1 hour):

1. `e2e/bootstrap_helpers_test.go::bootstrapAndProvision` — call
   `action=start` twice:
   ```go
   // Phase 1: discovery (drop result; we only need the session-context side effect)
   s.mustCallSuccess("zerops_workflow", map[string]any{
       "action": "start", "workflow": "bootstrap", "intent": t.Name(),
   })
   // Phase 2: commit with route
   startText := s.mustCallSuccess("zerops_workflow", map[string]any{
       "action": "start", "workflow": "bootstrap",
       "route": "classic", "intent": t.Name(),
   })
   var startResp bootstrapProgress
   if err := json.Unmarshal([]byte(startText), &startResp); err != nil {
       t.Fatalf("parse Phase-2 start: %v", err)
   }
   if startResp.SessionID == "" {
       t.Fatal("Phase-2 commit must return sessionId")
   }
   ```
2. Same for `bootstrapAndProvisionExpectFail`.
3. Inline single-phase starts (search `e2e/` for
   `"action":\s*"start",\s*"workflow":\s*"bootstrap"` then check whether
   `route` is set):
   - `bootstrap_workflow_test.go`, `import_provenance_test.go`,
     `laravel_deploy_test.go`, `build_logs_test.go`,
     `deploy_error_classification_test.go`, `deploy_prepare_fail_test.go`,
     `deploy_local_test.go`, `subdomain_test.go`,
     `discover_subdomain_test.go`, `subdomain_autoenable_test.go`.
   - For each: replace inline call with the helper, OR add the Phase-1
     prelude inline. Helper is preferred (centralized fix for next time).

**Decision matrix — which route per test**:

| Test (or test class) | Route |
|---|---|
| All `bootstrapAndProvision` callers | `classic` (default — fresh provision) |
| Adopt scenarios (search for `IsExisting=true` in test plan) | `adopt` |
| Tests starting from an empty/fresh project | `classic` |
| `TestE2E_BootstrapFresh_FullFlow` | `classic` (per name + plan shape) |
| Tests that DON'T provision (just need workflow context) | `classic` is safest |

**Part B — per-test audit** (larger, may surface real assertions
broken by the API change, ~half day):

1. Run the full e2e suite against `eval-zcp` after Part A:
   ```bash
   ZCP_API_KEY=$(grep -o '"ZCP_API_KEY": "[^"]*"' .mcp.json | sed 's/.*"\(.*\)".*/\1/') \
   go test ./e2e/ -tags e2e -count=1 -timeout 1800s
   ```
2. For each remaining failure, decide:
   - API-shift broke the test (fix it).
   - The test's intent no longer maps to current behavior (rewrite).
   - The test was always wrong (delete).
3. Honest skip with explanation for any test that genuinely depends on
   broken-behavior-as-fixture.

## Acceptance gate

The promoted plan is "done" when:

1. `bootstrapAndProvision` + `bootstrapAndProvisionExpectFail` use
   two-phase start.
2. All currently-failing tests listed above either PASS or have an
   explicit honest `t.Skip` with a separate backlog entry.
3. `TestE2E_StandardPairFirstDeploy_AutoEnablesSubdomain` (the immediate
   consumer) is unskipped and passes against `eval-zcp`.
4. CI status: doesn't run e2e by default (no env var) — local manual
   verification with `ZCP_API_KEY` set is the gate.

## Landmines (learned 2026-05-03)

Four things a fresh session would otherwise re-discover the hard way:

1. **`newHarness` vs `newLocalHarness` dispatch**: shared `newHarness`
   (`e2e/helpers_test.go:120`) always passes `sshDeployer` →
   `RegisterDeploySSH` is registered. `newLocalHarness`
   (`e2e/deploy_local_test.go:42`) passes `nil` → `RegisterDeployLocal`.
   Tests that want to exercise `ops.DeployLocal` (the
   `workingDir`-respecting path) MUST use `newLocalHarness`. Mixing
   them produces "source mount missing" errors because deploy_ssh
   preflight uses SSHFS-mount-style paths.

2. **`requireAdoption` gate timing**
   (`internal/tools/guard.go:61`): the gate ACTIVATES once
   `<stateDir>/services/` exists. That dir is created when bootstrap's
   `complete provision` step runs (writes ServiceMeta). Tests that skip
   the provision step (like `TestE2E_LocalDeploy_Success`) get a free
   pass through `requireAdoption` because services/ doesn't exist yet.
   If you ADD a `complete provision` step to a test, you must ensure
   the meta hostnames match what the test deploys — otherwise the next
   `zerops_deploy` fails "Service X is not adopted".

3. **Standard-mode bootstrap plan needs `stageHostname` explicit**
   (`internal/workflow/validate.go:61` `RuntimeTarget.StageHostname()`):
   `BootstrapMode=standard` requires `ExplicitStage` (JSON tag
   `stageHostname`). The old "{base}dev → {base}stage" derivation is
   gone — non-conforming hostnames silently misclassified, so it was
   removed. If a test plan omits `stageHostname` for a standard pair,
   complete-discover errors with `"target X: standard mode requires
   explicit stageHostname"`.

4. **deploy_ssh.go vs deploy_local.go workingDir semantics differ**:
   `deploy_ssh.go:92` says `workingDir` is `"Container path for deploy.
   Default: /var/www. In container mode: omit entirely (always
   correct)."` — i.e., it's the path INSIDE the target container.
   `deploy_local.go:42` says `workingDir` is `"Local path to push from.
   Default: current directory."` — the path on the developer's
   machine. Same parameter name, different semantics. Tests using
   `deploy_local`-registered server pass a local path; tests using
   `deploy_ssh`-registered server should usually omit workingDir.

## Refs

- `plans/archive/subdomain-auto-enable-foundation-fix-2026-05-03.md`
  (the work that surfaced this)
- Commit `5d64b12a` `fix(deploy-preflight): honor workingDir in local
  mode` (the fix that resolved the original combined backlog entry)
- `internal/workflow/bootstrap.go::BootstrapDiscoveryResponse`
- `internal/workflow/engine.go::BootstrapStartWithRoute`
- `internal/tools/workflow.go::handleBootstrapStart` (lines ~739-800
  show the route-empty discovery branch vs route-set commit branch)
- `e2e/bootstrap_helpers_test.go::bootstrapAndProvision` (line 58 — the
  current single-phase implementation that needs rewrite)
- `e2e/bootstrap_workflow_test.go:24` `bootstrapProgress` struct (the
  expected Phase-2 response shape)
