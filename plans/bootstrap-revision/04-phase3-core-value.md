# Phase 3: Core Value — Hard Checks + Step Consolidation

**Agents**: CHARLIE (hard checks) + ALPHA (step consolidation)
**Dependencies**: Phase 1 AND Phase 2 BOTH complete
**Risk**: HIGH — core behavioral change. MUST be atomic (3A+3B deployed together).

---

## Feature 3A: Hard Checks (StepChecker) — CHARLIE

### New Types

```go
// internal/workflow/bootstrap_checks.go (NEW ~40 lines)
type StepCheckResult struct {
    Passed  bool        `json:"passed"`
    Checks  []StepCheck `json:"checks"`
    Summary string      `json:"summary"`
}

type StepCheck struct {
    Name   string `json:"name"`
    Status string `json:"status"` // pass, fail, skip
    Detail string `json:"detail,omitempty"`
}

type StepChecker func(ctx context.Context, plan *ServicePlan) (*StepCheckResult, error)
```

### Engine Signature Change (H1)

```go
// internal/workflow/engine.go — BEFORE:
func (e *Engine) BootstrapComplete(stepName, attestation string) (*BootstrapResponse, error)

// AFTER:
func (e *Engine) BootstrapComplete(ctx context.Context, stepName string, attestation string, checker StepChecker) (*BootstrapResponse, error) {
    // ... existing validation ...
    if checker != nil {
        result, err := checker(ctx, state.Bootstrap.Plan)
        if err != nil {
            return nil, fmt.Errorf("step check: %w", err)
        }
        if result != nil && !result.Passed {
            resp := state.Bootstrap.BuildResponse(state.SessionID, state.Intent)
            resp.CheckResult = result
            resp.Message = fmt.Sprintf("Step %q: %s — fix issues and retry", stepName, result.Summary)
            return resp, nil // NOT an error — structured failure
        }
    }
    // ... proceed with step completion ...
}
```

### Step Checkers (in tool layer — no layering violation)

```go
// internal/tools/workflow_checks.go (NEW ~200 lines)

func buildStepChecker(step string, client platform.Client,
    fetcher platform.LogFetcher, projectID string,
    tracker *ops.KnowledgeTracker, httpClient *http.Client) workflow.StepChecker {
    switch step {
    case "discover":
        return nil // plan validation in BootstrapCompletePlan
    case "provision":
        return checkProvision(client, projectID)
    case "generate":
        return checkGenerate(client, projectID)
    case "deploy":
        return checkDeploy(client, fetcher, projectID, httpClient)
    case "verify":
        return checkVerify(client, fetcher, projectID, httpClient)
    }
    return nil
}
```

### Individual Checkers

#### checkProvision
- All planned services exist (ListServices)
- Dev runtimes: status RUNNING
- Managed services: status RUNNING
- Stage runtimes: status NEW or READY_TO_DEPLOY
- Each MANAGED_WITH_ENVS service has non-empty env vars
- MANAGED_STORAGE (shared-storage) excluded from env var check

#### checkGenerate
- zerops.yml exists for each target (file check via mount path or API)
- Has setup entries for both dev and stage hostnames
- Dev: no healthCheck, deployFiles contains `.`
- Stage: start is NOT `zsc noop --silent`
- Env ref validation (C2): all `${hostname_varName}` references valid
- All entries: `run.start` non-empty (except implicit webserver)
- All entries: `run.ports` non-empty (except workers)

#### checkDeploy
- All target services: build status ACTIVE (not BUILD_FAILED)
- Subdomain enabled for HTTP services

#### checkVerify
- `VerifyAll()` results — all targets healthy or degraded (not unhealthy)
- Filter by current plan targets (pre-existing unhealthy services don't cause failure)

### Registration Changes

```go
// internal/server/server.go
tools.RegisterWorkflow(s.server, s.client, projectID, stackCache,
    wfEngine, knowledgeTracker, s.logFetcher, s.httpClient) // ADD logFetcher, httpClient

// internal/tools/workflow.go
func RegisterWorkflow(srv, client, projectID, cache, engine, tracker,
    logFetcher, httpClient) { ... } // ADD parameters
```

### BootstrapResponse Extension

```go
// internal/workflow/bootstrap.go
type BootstrapResponse struct {
    // existing fields...
    CheckResult *StepCheckResult `json:"checkResult,omitempty"`
}
```

### Tests (RED first)

```
internal/workflow/engine_test.go:
  TestEngine_BootstrapComplete_WithChecker_Pass
  TestEngine_BootstrapComplete_WithChecker_Fail_ReturnsStructured
  TestEngine_BootstrapComplete_NilChecker_SkipsCheck
  TestEngine_BootstrapComplete_CheckerError_ReturnsError

internal/tools/workflow_checks_test.go (NEW ~300 lines):
  TestCheckProvision_AllServicesExist_Pass
  TestCheckProvision_MissingService_Fail
  TestCheckProvision_NoEnvVars_Fail
  TestCheckProvision_SharedStorage_SkipEnvCheck
  TestCheckGenerate_ValidYml_Pass
  TestCheckGenerate_MissingYml_Fail
  TestCheckGenerate_InvalidEnvRef_Fail
  TestCheckGenerate_DevWithHealthCheck_Fail
  TestCheckGenerate_StageWithNoop_Fail
  TestCheckDeploy_AllActive_Pass
  TestCheckDeploy_BuildFailed_Fail
  TestCheckVerify_AllHealthy_Pass
  TestCheckVerify_PreExistingUnhealthy_Ignored
  TestCheckVerify_TargetUnhealthy_Fail
```

---

## Feature 3B: Step Consolidation (11 → 5) — ALPHA

### Step Mapping

| # | New Step | Old Steps Merged | Skippable |
|---|----------|-----------------|-----------|
| 0 | discover | detect + plan + load-knowledge | no |
| 1 | provision | generate-import + import + mount + discover-envs | no |
| 2 | generate | generate-code | yes |
| 3 | deploy | deploy | yes |
| 4 | verify | verify + report | no |

### Files Modified

#### `internal/workflow/bootstrap_steps.go` — FULL REWRITE
Replace 11 `stepDetails` with 5. Each step has:
- Comprehensive guidance merging all sub-step guidance
- Combined tool lists
- Hard check-oriented verification descriptions
- Correct skippable flags

#### `internal/workflow/bootstrap.go`
- Update step constants: `stepDiscoverEnvs` → removed, `stepMountDev` → removed, `stepGenerateCode` → `stepGenerate`, `stepDeploy` → unchanged
- Update `validateConditionalSkip()`: only `generate` and `deploy` skippable
- `NewBootstrapState()` creates 5 steps instead of 11

#### `internal/workflow/bootstrap_evidence.go`
- Update `bootstrapEvidenceMap` with new step names

#### `internal/workflow/bootstrap_guidance.go`
- Update section tag extraction for new names

#### `internal/tools/workflow_bootstrap.go`
- Remove `injectKnowledgeHint` (knowledge loading is now part of discover step)
- Update step name references

#### `internal/content/workflows/bootstrap.md` — MAJOR REWRITE
- 5 sections matching new step names
- Merge guidance from old steps into new consolidated sections
- Keep detailed subsections within each step

### Tests (RED first)

```
internal/workflow/bootstrap_test.go:
  TestNewBootstrapState_Has5Steps
  TestBootstrapState_CompleteStep_Discover
  TestBootstrapState_CompleteStep_AllSteps
  TestValidateConditionalSkip_GenerateSkippable
  TestValidateConditionalSkip_DeploySkippable
  TestValidateConditionalSkip_DiscoverNotSkippable
  TestValidateConditionalSkip_ProvisionNotSkippable
  TestValidateConditionalSkip_VerifyNotSkippable

internal/workflow/engine_test.go:
  Update ALL bootstrap tests with 5-step names

internal/workflow/bootstrap_guidance_test.go:
  TestExtractSection_Discover
  TestExtractSection_Provision
  TestExtractSection_Generate
  TestExtractSection_Deploy
  TestExtractSection_Verify

internal/tools/workflow_test.go:
  Update complete/skip tests with new step names
```

---

## CRITICAL: Atomic Deployment

Features 3A and 3B MUST be deployed together. The 5-step model relies on hard checks for safety that the 11-step model provided through granularity.

### Deploy & Test

```bash
# MUST all pass before deploy
go test ./internal/workflow/... -count=1 -v
go test ./internal/tools/... -count=1 -v
go test ./integration/... -count=1 -v
go test ./... -count=1 -short
make lint-fast

./eval/scripts/build-deploy.sh
```

### Critical Live Tests on zcpx
1. **Full bootstrap flow**: `zerops_workflow action="start" workflow="bootstrap"` → submit plan → provision → generate → deploy → verify
2. **Hard check failure**: submit incomplete plan → verify structured error response
3. **Auto-completion**: provision step auto-completes when all services exist + env vars present
4. **Verify batch**: final verify step runs VerifyAll
5. **Iteration**: deploy failure → iteration loop → fix → redeploy → re-verify
