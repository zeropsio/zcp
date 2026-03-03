# 06 — Ground G3/G4 Gate Evidence with Real Verification

Synthesized from audit #02 (circular gate evidence) and #07 (instruction vs structure).

## Problem

Bootstrap's `autoCompleteBootstrap()` generates evidence for all 5 phase gates from LLM attestation strings, then validates that evidence. The evidence `Failed` field is never set > 0, so gates literally cannot fail. A deploy that returned BUILD_FAILED still gets a passing gate if the LLM completes the step with a 10-char string.

The most critical gates — G3 (DEPLOY->VERIFY, requires `deploy_evidence`) and G4 (VERIFY->DONE, requires `stage_verify`) — should verify actual system state instead of relying on LLM attestations.

## Current Architecture

### Bootstrap flow (11 steps, sequential)

```
detect → plan → load-knowledge → generate-import → import-services →
mount-dev → discover-envs → generate-code → deploy → verify → report
```

Steps are enforced sequentially by `CompleteStep()` — cannot skip ahead. Each step requires a >= 10 char attestation string. No semantic validation on attestation content.

### Auto-complete + gates (batch validation at end)

When step 11 (report) completes, `BootstrapComplete()` in `engine.go:126-156` detects `!state.Bootstrap.Active` and calls `autoCompleteBootstrap()`.

`autoCompleteBootstrap()` in `bootstrap_evidence.go:19-83`:
1. Collects attestations from all completed steps
2. For each evidence type in `bootstrapEvidenceMap`, creates an `Evidence` struct
3. `Passed` = count of completed steps for that evidence type
4. `Failed` = 0 (zero value, never set)
5. `ServiceResults` = nil (never populated)
6. Writes evidence files to disk
7. Walks through ALL gate transitions (INIT→DISCOVER→...→DONE)
8. Gates check the just-written evidence — always pass

### Evidence → gate mapping

```go
bootstrapEvidenceMap = map[string][]string{
    "recipe_review":   {"detect", "plan", "load-knowledge"},        // → G0
    "discovery":       {"discover-envs"},                            // → G1
    "dev_verify":      {"generate-code", "deploy", "verify"},       // → G2
    "deploy_evidence": {"deploy"},                                   // → G3
    "stage_verify":    {"verify", "report"},                         // → G4
}
```

### Gate definitions (gates.go:26-32)

```go
gates = []gateDefinition{
    {"G0", PhaseInit,     PhaseDiscover, []string{"recipe_review"},   0},
    {"G1", PhaseDiscover, PhaseDevelop,  []string{"discovery"},       24h},
    {"G2", PhaseDevelop,  PhaseDeploy,   []string{"dev_verify"},      24h},
    {"G3", PhaseDeploy,   PhaseVerify,   []string{"deploy_evidence"}, 24h},
    {"G4", PhaseVerify,   PhaseDone,     []string{"stage_verify"},    24h},
}
```

### Gate validation logic (gates.go:47-114)

For each required evidence type:
1. Load evidence file from disk
2. Check session binding (sessionID match)
3. Check freshness (< 24h for G1-G4)
4. `ValidateEvidence()`: `Failed > 0` → fail, empty attestation → fail
5. `validateServiceResults()`: any `ServiceResult` with `status == "fail"` → fail

Steps 4 and 5 are the actual quality checks — but they never trigger because `autoCompleteBootstrap` always writes `Failed: 0` and no `ServiceResults`.

### Evidence struct (evidence.go:19-29)

```go
type Evidence struct {
    SessionID        string          `json:"sessionId"`
    Timestamp        string          `json:"timestamp"`
    VerificationType string          `json:"verificationType"` // always "attestation"
    Service          string          `json:"service,omitempty"`
    Attestation      string          `json:"attestation"`
    Type             string          `json:"type"`
    Passed           int             `json:"passed"`
    Failed           int             `json:"failed"`
    ServiceResults   []ServiceResult `json:"serviceResults,omitempty"`
}

type ServiceResult struct {
    Hostname string `json:"hostname"`
    Status   string `json:"status"` // "pass", "fail", "skip"
    Detail   string `json:"detail,omitempty"`
}
```

## Existing Verification Infrastructure

`ops.Verify()` in `internal/ops/verify.go:66-156` already provides 6 health checks:

1. **service_running** — checks API status (RUNNING/ACTIVE)
2. **no_error_logs** — checks for error logs in last 5 min
3. **startup_detected** — looks for startup patterns in logs
4. **no_recent_errors** — error logs in last 2 min
5. **http_health** — HTTP GET to `{subdomain}/health`
6. **http_status** — HTTP GET to `{subdomain}/status`

For managed services (DB, cache, storage): only check 1 (service_running).

Returns `VerifyResult` with `Status: "healthy" | "degraded" | "unhealthy"`.

Dependencies needed to call `ops.Verify()`:
- `platform.Client` — for API calls
- `platform.LogFetcher` — for log access
- `ops.HTTPDoer` (= `*http.Client`) — for HTTP health checks
- `projectID string` — project context
- `hostname string` — service to verify

## Proposed Fix

### Type definition

Add to `internal/workflow/bootstrap_evidence.go`:

```go
// ServiceVerifier checks real system state for planned services.
// Returns per-service pass/fail results. API/network errors should return
// ServiceResult with status "skip" (don't block gate), while genuinely
// unhealthy services return status "fail" (blocks gate).
type ServiceVerifier func(hostnames []string) ([]ServiceResult, error)
```

### Modified autoCompleteBootstrap

```go
func (e *Engine) autoCompleteBootstrap(state *WorkflowState, verifier ServiceVerifier) error {
    // ... existing attestation collection ...

    for evType, steps := range bootstrapEvidenceMap {
        // ... existing attestation/passed logic ...

        ev := &Evidence{ /* ... existing fields ... */ }

        // NEW: For deploy/verify evidence, call real verifier
        if verifier != nil && (evType == "deploy_evidence" || evType == "stage_verify") {
            runtimeHosts := runtimeHostnames(state.Bootstrap.Plan)
            if len(runtimeHosts) > 0 {
                results, err := verifier(runtimeHosts)
                if err != nil {
                    return fmt.Errorf("auto-evidence %s verify: %w", evType, err)
                }
                ev.ServiceResults = results
                for _, r := range results {
                    if r.Status == "fail" {
                        ev.Failed++
                    }
                }
            }
        }

        if err := SaveEvidence(e.evidenceDir, state.SessionID, ev); err != nil {
            return fmt.Errorf("auto-evidence %s: %w", evType, err)
        }
    }

    // ... existing gate transition loop ...
}
```

### Helper function

```go
func runtimeHostnames(plan *ServicePlan) []string {
    if plan == nil {
        return nil
    }
    var hosts []string
    for _, svc := range plan.Services {
        if !isManagedService(svc.Type) {
            hosts = append(hosts, svc.Hostname)
        }
    }
    return hosts
}
```

Note: `isManagedService()` already exists in `bootstrap.go` and is used by `validateConditionalSkip()`.

### Modified BootstrapComplete signature

`internal/workflow/engine.go`:

```go
func (e *Engine) BootstrapComplete(stepName, attestation string, verifier ServiceVerifier) (*BootstrapResponse, error)
```

Passes verifier to `autoCompleteBootstrap()`. Nil verifier = old behavior (backward compat for tests and non-bootstrap callers).

### Verifier construction in tool layer

`internal/tools/workflow_bootstrap.go` — `handleBootstrapComplete()` builds the verifier:

```go
var verifier workflow.ServiceVerifier
if client != nil {
    verifier = func(hostnames []string) ([]workflow.ServiceResult, error) {
        var results []workflow.ServiceResult
        for _, h := range hostnames {
            vr, err := ops.Verify(ctx, client, fetcher, httpClient, projectID, h)
            if err != nil {
                // API error = skip (don't block), not fail
                results = append(results, workflow.ServiceResult{
                    Hostname: h, Status: "skip", Detail: err.Error(),
                })
                continue
            }
            status := "pass"
            if vr.Status == ops.StatusUnhealthy {
                status = "fail"
            }
            results = append(results, workflow.ServiceResult{
                Hostname: h, Status: status, Detail: vr.Status,
            })
        }
        return results, nil
    }
}
```

### Dependency threading

`RegisterWorkflow()` in `workflow.go:56` needs additional params:

Current: `(srv, client, projectID, cache, engine, tracker)`
New: `(srv, client, projectID, cache, engine, tracker, logFetcher)`

HTTP client created inside (same pattern as `RegisterVerify` in `verify.go:21-25`):
```go
httpClient := &http.Client{
    Timeout: 15 * time.Second,
    Transport: &http.Transport{
        TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
    },
}
```

Call site in `server.go:89`:
```go
tools.RegisterWorkflow(s.server, s.client, projectID, stackCache, wfEngine, knowledgeTracker, s.logFetcher)
```

## Files to Modify

```
internal/workflow/bootstrap_evidence.go  — ServiceVerifier type, modified autoCompleteBootstrap(), runtimeHostnames()
internal/workflow/engine.go              — BootstrapComplete() signature change (add verifier param)
internal/tools/workflow_bootstrap.go     — build verifier closure in handleBootstrapComplete()
internal/tools/workflow.go               — RegisterWorkflow() signature + threading deps to handleBootstrapComplete()
internal/server/server.go                — pass logFetcher to RegisterWorkflow()
```

## Tests (TDD)

### Unit tests — `internal/workflow/bootstrap_evidence_test.go` (new file)

Table-driven, parallel. Setup: create engine with `t.TempDir()`, start bootstrap, complete all 11 steps.

| Test case | Verifier | Expected |
|-----------|----------|----------|
| `NoVerifier_PassesAsToday` | nil | `Failed: 0`, all gates pass |
| `VerifierAllHealthy` | returns all "pass" | `Failed: 0`, ServiceResults populated, gates pass |
| `VerifierUnhealthy` | returns 1 "fail" | `Failed: 1`, auto-complete returns error |
| `VerifierError` | returns error | auto-complete returns error |
| `NoRuntimeServices` | managed-only plan | verifier not called, evidence attestation-based |
| `MixedServices` | 1 runtime + 1 managed | verifier called with runtime hostname only |

### Tool tests — `internal/tools/workflow_bootstrap_test.go`

Integration-style: full bootstrap flow where last step triggers auto-complete with mock verifier returning unhealthy → verify tool returns error.

### Existing tests

All callers of `BootstrapComplete()` need updating to pass nil verifier:
- `internal/workflow/engine.go` calls (already internal)
- `integration/bootstrap_conductor_test.go` — pass nil verifier (backward compat)

## Gaps and Open Questions

### Timing of verification

`autoCompleteBootstrap()` runs right after the LLM completes step 11 (report). But step 9 was deploy, step 10 was verify. If the deploy finished seconds ago, the service might still be starting. `ops.Verify` could return unhealthy for a service that would be healthy in 10 seconds.

**Options:**
- Accept: the verify step (step 10) is where the LLM should have waited for service health. If it didn't, failing auto-complete is correct.
- Add delay: sleep before calling verifier. Ugly, non-deterministic.
- Retry: call verifier with retry + backoff. Adds complexity (~30 LOC).
- `degraded` tolerance: only fail on `unhealthy`, allow `degraded` through. Currently proposed: only `unhealthy` = fail. This seems right.

### Verifier called twice (deploy_evidence AND stage_verify)

Both `deploy_evidence` and `stage_verify` trigger verification. This means `ops.Verify` is called twice per runtime service (same checks, redundant). Two options:
- Cache: call once, reuse for both evidence types. Adds state to autoCompleteBootstrap.
- Accept: two calls. Health checks are cheap (~1-2s per service). Redundancy = two evidence snapshots, which has audit value.

Recommend: accept the double call for simplicity. Optimize later if bootstrap is too slow.

### What about step 10 (verify)?

Step 10 instructs the LLM to run `zerops_verify`. If the LLM actually does this, it sees real health data and can iterate. The auto-complete verifier is a backstop for when the LLM skips or misinterprets the verify step.

But: the LLM can't skip step 10 (it's mandatory). It CAN complete step 10 with a garbage attestation without actually running `zerops_verify`. This is exactly what auto-complete catches.

### Non-bootstrap workflows

Manual `Transition()` + `RecordEvidence()` flow is NOT affected by this change. Evidence is whatever the LLM provides. The fix only applies to `autoCompleteBootstrap()`.

Should manual evidence be grounded too? Maybe, but it's a different scope. Manual workflows are more deliberate — the LLM calls `evidence` action explicitly. The risk of accidental success is lower.

### What if platform.Client is nil?

In tests, `client` can be nil. The verifier won't be built. `autoCompleteBootstrap()` gets nil verifier, falls back to old behavior. This is correct — tests without a client shouldn't do API verification.

In production, `client` is always non-nil (server won't start without auth). So verification always runs in production.

### Error semantics in verifier callback

Current design: the callback itself returns `error` only for fatal issues. Per-service failures are in `ServiceResult.Status`. But what counts as "fatal"?

- `ops.Verify` returns error for: service not found, list services API failure
- `ops.Verify` returns result with unhealthy status for: service not running, error logs, failed HTTP checks

The verifier should convert `ops.Verify` errors to `ServiceResult{Status: "skip"}` (can't verify ≠ verified bad). Only `vr.Status == unhealthy` becomes `fail`.

### SubdomainAccess might not be enabled

`ops.Verify` checks HTTP health on subdomain URL. If subdomain isn't enabled (common during bootstrap — it requires explicit `zerops_subdomain action=enable` after first deploy), HTTP checks are skipped with `Status: "skip"`.

This means: if LLM forgot to enable subdomain, only checks 1-4 run (service_running, error logs, startup). These are still valuable but won't catch "app crashes on HTTP request" issues.

### isManagedService() scope

`isManagedService()` in `bootstrap.go` uses static prefix matching. `isManagedCategory()` in `verify.go` uses API category name. These could disagree if a new service type is added to the platform but not to the static list.

For runtimeHostnames(), using `isManagedService()` is correct — it matches what `validateConditionalSkip()` uses, keeping bootstrap logic consistent.

## Devil's Advocate

**"The LLM could still lie in manual CompleteStep() attestations for steps 1-8"**

True. But steps 1-8 have low blast radius:
- Steps 1-3 (detect, plan, load-knowledge): informational, bad attestation just means LLM didn't read docs properly
- Steps 4-5 (generate-import, import-services): Zerops API validates import YAML
- Step 6-7 (mount-dev, discover-envs): setup steps, wrong attestation means missing context for code generation
- Step 8 (generate-code): code quality is subjective, no code guard can judge it

Steps 9-11 are where real damage happens (broken deploy declared as success), and those are exactly what this fix covers.

**"Adding network I/O to a state machine function is architecturally wrong"**

The callback pattern isolates the concern. `autoCompleteBootstrap()` doesn't know about HTTP or APIs — it calls a function that returns `[]ServiceResult`. The function happens to do network I/O, but that's the caller's concern. The workflow package's dependency graph doesn't change.

**"Two calls to verify (deploy_evidence + stage_verify) is wasteful"**

True but minor. Each verify takes ~1-2s per service. Bootstrap overall takes minutes. The double-snapshot has audit value (evidence of state at two different gate transitions).

**"What if the verifier adds 30s to bootstrap completion?"**

Valid concern. If the project has 4 runtime services, that's 4 x ~2s x 2 evidence types = ~16s. Noticeable but acceptable. If it becomes a problem, the verifier can be parallelized (goroutines per service) in a future iteration.
