# Plan: Deploy-config as a single central point

**Status**: Implemented. `workflow=cicd` retired. `action=strategy` is now the
canonical entry for configuring any service's deploy strategy (push-dev,
push-git, manual). For push-git the handler probes state and returns the
full setup flow (Option A push-only vs Option B full CI/CD, GIT_TOKEN,
optional GitHub Actions or webhook, first push) as a single atom-synthesized
response.

---

## Why

Before this refactor, the same concern ("I want to push my service to git
and optionally set up CI/CD") was split across four fragmented entry points
with duplicated content:

| Entry | What it did | Where Option A/B was asked |
|---|---|---|
| `zerops_workflow action=strategy strategies={X:push-git}` | Wrote `meta.DeployStrategy`, returned develop-phase iteration atoms | Never |
| `zerops_workflow action=start workflow=cicd` | Returned static 6-atom CI/CD setup guidance | `cicd-01-plan.md` |
| `zerops_deploy strategy=git-push` (GIT_TOKEN missing) | Returned hardcoded `gitPushSetupInstructions` text | Inside the error body |
| `zerops_workflow action=start workflow=export` | Pushes via git-push in task 10 | Never |

The LLM had to pick one and there was no way for the "Option A vs B"
decision to be reached proactively. Users who already had `GIT_TOKEN` set
never got the CI/CD offer; users who ran the strategy action only got a
flag-flip, not a real configuration. Two sources of truth (`cicd-01-plan.md`
atom + `gitPushSetupInstructions` Go constant) could drift.

Applying the single-path engineering principle: **one canonical path**.
Configuration of deploy strategy = `action=strategy`. Execution of deploys =
`zerops_deploy`. Any pre-flight failure in the tool points back to the
configuration action for setup, never duplicates its content.

---

## Architecture

### Two distinct operations, one entry each

| Operation | Entry | Responsibility |
|---|---|---|
| Configure strategy | `zerops_workflow action="strategy"` | Listing mode (no strategies param) shows current + options per service. Setup mode (`strategies={X:Y}`) writes meta and, for push-git, emits atom-synthesized setup flow. |
| Execute deploy | `zerops_deploy` | Performs the push. Pre-flight guards missing setup with a **short pointer** back to `action=strategy` — never duplicates setup content. |

### Flow example (user asks "set up git push for appdev")

```
LLM: zerops_workflow action="strategy" strategies={"appdev":"push-git"}

handleStrategy:
  1. Validates strategy value.
  2. Reads ServiceMeta(appdev); refuses if not bootstrapped/complete.
  3. Writes meta.DeployStrategy=push-git, meta.StrategyConfirmed=true.
  4. Calls SynthesizeImmediateWorkflow(PhaseStrategySetup, env).
  5. Returns JSON with status="updated", services, next hint, guidance.

guidance (atom synthesis) contains:
  Task 1: Confirm repo URL
  Task 2: Choose sub-mode (push-only / CI/CD Actions / CI/CD Webhook)
  Task 3: Get + set GIT_TOKEN
  Task 4 [push-only]: Commit + first push
  Task 5-9 [CI/CD Actions]: ZEROPS_TOKEN secret, permissions, workflow file, push
  Task 10 [CI/CD Webhook]: GUI walkthrough in Zerops dashboard
  Task 11: Verify (zerops_events)
  Task 12: Report

LLM follows tasks.
```

### Listing mode (discovery)

```
LLM: zerops_workflow action="strategy"  (no strategies param)

handleStrategy:
  1. Detects empty strategies → handleStrategyList.
  2. Lists each bootstrapped service's current DeployStrategy (or "unset")
     + the three available options + a sample hint.
  3. No mutation.

Response:
  {
    "status": "list",
    "services": [
      {"hostname":"appdev","current":"push-dev","options":[...],"hint":"..."},
      ...
    ],
    "next": "Pick a strategy per service: ..."
  }
```

### Deploy pre-flight (unchanged flow, shorter error)

`zerops_deploy strategy=git-push`:

1. `meta.IsDeployed()` false → `ErrPrerequisiteMissing` (unchanged).
2. `GIT_TOKEN` missing → `GIT_TOKEN_MISSING` with **short** instructions
   pointing at `zerops_workflow action="strategy" strategies={"X":"push-git"}`.
3. All pre-flight green → push.

No duplicated Option A/B content in the tool. Single source of truth lives
in the `strategy-push-git.md` atom.

---

## Code changes

### New atom

- `internal/content/atoms/strategy-push-git.md` — single atom, 12-task
  checklist with Option A/B decision at task 2. Phase: `strategy-setup`.

### Go files deleted

- `internal/tools/workflow_cicd_generate.go`
- `internal/tools/workflow_cicd_generate_test.go`
- `internal/tools/workflow_cicd_context.go`
- `internal/tools/workflow_cicd_context_test.go`

### Atoms deleted

- `internal/content/atoms/cicd-01-plan.md`
- `internal/content/atoms/cicd-03-approach.md`
- `internal/content/atoms/cicd-04-github-actions.md`
- `internal/content/atoms/cicd-05-git-setup.md`
- `internal/content/atoms/cicd-08-webhook.md`
- `internal/content/atoms/cicd-10-verification.md`

### Go changes

- `internal/workflow/envelope.go` — renamed `PhaseCICDActive` → `PhaseStrategySetup`
  (value `"cicd-active"` → `"strategy-setup"`).
- `internal/workflow/state.go` — removed `"cicd"` from `immediateWorkflows`.
- `internal/workflow/build_plan.go` / `render.go` — updated phase references.
- `internal/workflow/router.go` — `cicd` offering replaced with `strategy`
  offering (fires whenever any bootstrapped service exists). Export offering
  now fires for any deployed service regardless of strategy. Removed
  `cicd` from utility offerings.
- `internal/workflow/bootstrap_guide_assembly.go` — removed `cicd`
  post-bootstrap offering (bootstrap hands off to develop; strategy is
  selected post-first-deploy or via develop-strategy-review).
- `internal/tools/workflow.go` — removed `cicd` from tool description,
  Workflow JSON schema, error messages. Added `action="strategy"` to tool
  description.
- `internal/tools/workflow_immediate.go` — removed cicd handling; only
  `export` remains as immediate workflow.
- `internal/tools/workflow_strategy.go` — rewrote `handleStrategy`:
  - New `handleStrategyList` for listing mode (empty strategies map).
  - `handleStrategy` passes `rt runtime.Info` for environment detection.
  - For push-git: synthesizes atoms via `SynthesizeImmediateWorkflow(PhaseStrategySetup, env)`.
  - For push-dev/manual: existing `BuildStrategyGuidance` output.
- `internal/tools/deploy_git_push.go` — replaced 14-line
  `gitPushSetupInstructions` constant with short
  `gitPushSetupPointerInstructions` that routes the LLM to
  `action=strategy strategies={"X":"push-git"}`.
- `internal/server/instructions.go` — updated workflow list in the MCP
  instructions text: removed cicd, added explicit note about `action="strategy"`.

### Atom content updates

- `internal/content/atoms/develop-push-git-deploy.md` — replaced
  `workflow="cicd"` reference with `action="strategy"`.
- `internal/content/atoms/develop-close-push-git.md` — simplified; now
  points at `action="strategy"` for the setup flow instead of duplicating
  Option A/B.

### Fixture / test updates

- `internal/workflow/corpus_coverage_test.go` — `cicd_active` fixture →
  `strategy_setup` fixture. Removed `.netrc` assertion (tool handles auth
  automatically; no atom content mentions manual .netrc creation anymore).
- `internal/workflow/scenarios_test.go` — `TestScenario_S11_CICDActiveEmptyPlan`
  → `TestScenario_S11_StrategySetupEmptyPlan`.
- `internal/workflow/state_test.go` — `cicd` is no longer an immediate
  workflow (retired); `export` kept as the only immediate.
- `internal/content/content_test.go` — removed `cicd` from orchestrated
  workflow list.
- `internal/tools/workflow_strategy_test.go` — rewrote. New tests:
  - `TestHandleStrategy_EmptyStrategies_ListingMode` — listing mode returns
    current + options per complete meta, skips incomplete metas.
  - `TestHandleStrategy_PushGit_SynthSetup` — push-git returns non-empty
    guidance containing "push-only", "full CI/CD", "GIT_TOKEN",
    "ZEROPS_TOKEN", "zerops_deploy". Regression assertion that guidance
    does not reference retired `workflow="cicd"`.
  - `TestAnyStrategyIs` added.
  - Existing `TestHandleStrategy_ValidUpdate` updated: push-git now points
    at `strategy="git-push"` execution, not `workflow="cicd"`.
- `internal/tools/workflow_test.go` — `TestWorkflowTool_Immediate_CICD` →
  `TestWorkflowTool_Immediate_Export`. `Action_Start_Immediate` and
  `Action_Start_ImmediateNoSession` switched to `workflow=export`.
- `internal/tools/workflow_start_test.go` — `TestHandleStart_ImmediateWorkflow_NotRejected`
  switched to `workflow=export`. Workflow name list in
  `TestHandleStart_FreshSession_NoSubagentMisuse` swapped cicd for export.
- `internal/tools/deploy_ssh_test.go` — `TestDeployTool_GitPush_MissingGitToken_ReturnsPrerequisites`
  updated to expect the short pointer format rather than the retired
  Option A/B text.

### Docs

- `docs/spec-workflows.md` — updated scope line, phase table (`cicd-active`
  → `strategy-setup`), stateless invariant wording, Envelope axis value
  list. Added explanation that the strategy action is the central
  deploy-config entry.
- `docs/spec-knowledge-distribution.md` — updated phase enum, stateless
  synthesis description, atom inventory, KD-07 invariant.
- `plans/deploy-config-central-point.md` — this document.

---

## Invariants

1. **Single entry per operation.** Configure = `action=strategy`. Deploy =
   `zerops_deploy`. No aliases, no duplicates.
2. **Tool pre-flight never duplicates atom content.** When `zerops_deploy`
   returns a setup-required response, it emits a short pointer, not
   Option A/B or token instructions. The atom is the single source of truth.
3. **Phase isolation.** `strategy-setup` and `export-active` are stateless
   synthesis phases — no session file is written, no interaction with
   bootstrap/recipe/develop session state.
4. **Listing mode is read-only.** Calling `action=strategy` with no
   strategies map is pure observation — no mutation of ServiceMeta, no
   atoms synthesized that push the user into an action.
5. **Workflow=cicd is permanently retired.** Any test asserting cicd as
   immediate workflow or as a valid workflow name is updated; state_test
   pins `cicd` → false in `IsImmediateWorkflow`.

---

## E2E validation

To run against the live `opus/appdev` (Laravel pair):

1. `zerops_workflow action="strategy"` (listing mode) — should list `appdev`,
   `apidev`, `fizzydev`, `authentikworker`, `zcp` (any bootstrapped complete
   service) with their current strategies.
2. `zerops_workflow action="strategy" strategies={"appdev":"push-git"}` —
   expect `status="updated"`, `services="appdev=push-git"`, and `guidance`
   non-empty containing the 12-task list. The guidance must reference
   "push-only", "full CI/CD", "GIT_TOKEN", "ZEROPS_TOKEN".
3. `zerops_deploy targetService="appdev" strategy="git-push" remoteUrl="..."`
   with no GIT_TOKEN — expect `GIT_TOKEN_MISSING` response with short
   pointer that contains `action="strategy"` and `appdev`.
4. With GIT_TOKEN set — expect successful push.

---

## Known follow-ups (out of scope here)

- `FirstDeployedAt` stamping on adopted services — independent gap; adopt
  workflow doesn't stamp it, so `zerops_deploy strategy=git-push` hits the
  prereq error even when the service has been running for months. File
  separately.
- Platform export leaking `ZCP_API_KEY` into project.envVariables — export
  atom instructs LLM to drop; upstream platform fix is separate.
