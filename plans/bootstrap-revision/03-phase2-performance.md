# Phase 2: Performance — Verify Speedup + Batch

**Agent**: BRAVO (ops specialist)
**Dependencies**: None (parallel with Phase 1, except 2B needs 1B)
**Risk**: LOW — ops-layer only, independent from workflow changes

---

## Feature 2A: Verify Speedup + Runtime Classification (C1)

### Runtime Classification

```go
// internal/ops/verify_checks.go
type RuntimeClass int
const (
    RuntimeDynamic       RuntimeClass = iota // nodejs, go, bun, python, rust, java, deno, dotnet
    RuntimeImplicit                           // php-apache, php-nginx
    RuntimeStatic                             // static, nginx
    RuntimeWorker                             // any runtime with no run.ports
    RuntimeManaged                            // postgresql, valkey, etc.
)

func classifyRuntime(serviceType string, hasPorts bool) RuntimeClass
```

### Check Changes

| Old Check | New Check | Applies To |
|-----------|-----------|-----------|
| `checkHTTPHealth` (GET /health) | **REMOVED** — redundant with platform probe | — |
| — | `checkHTTPRoot` (GET / → 200) | dynamic, implicit, static |
| `checkHTTPStatus` (GET /status) | unchanged | dynamic, implicit only (SKIP for static) |
| `checkStartupDetected` | unchanged | dynamic only (SKIP for implicit, static, worker) |
| `checkErrorLogs(5m)` + `checkErrorLogs2m()` | `batchLogChecks()` — single fetch, filter locally | dynamic, implicit, worker |

### Parallelization

```go
// internal/ops/verify.go
func Verify(...) (*VerifyResult, error) {
    // Phase 1: service_running (must pass first)
    // Phase 2: parallel groups
    //   Group A: batchLogChecks (1 API call)
    //   Group B: checkHTTPRoot + checkHTTPStatus (2 HTTP calls)
    // Phase 3: aggregate results
}
```

### Files Modified

| File | Changes |
|------|---------|
| `internal/ops/verify.go` | Parallel pipeline, runtime classification dispatch |
| `internal/ops/verify_checks.go` | Add `checkHTTPRoot()`, `batchLogChecks()`, `classifyRuntime()`. Remove `checkHTTPHealth()`, `checkErrorLogs2m()`. |
| `internal/ops/deploy_validate.go` | Fix `Base` field to `any` + `baseStrings()` normalizer (C4). Stage `zsc noop` warning. |
| `internal/ops/progress.go` | Poll interval speedup (if not done in Phase 0) |

### Tests (RED first)

```
internal/ops/verify_test.go:
  TestVerify_DynamicRuntime_AllChecks
  TestVerify_StaticRuntime_SkipsStatusAndStartup
  TestVerify_ImplicitWebserver_SkipsStartup
  TestVerify_WorkerRuntime_NoHTTPChecks
  TestVerify_ManagedService_SingleCheck
  TestCheckHTTPRoot_Success
  TestCheckHTTPRoot_Non200_Fail
  TestBatchLogChecks_NoErrors_Pass
  TestBatchLogChecks_ErrorsFound_Info
  TestClassifyRuntime_Dynamic
  TestClassifyRuntime_Static
  TestClassifyRuntime_Implicit
  TestClassifyRuntime_Worker

internal/ops/deploy_validate_test.go:
  TestValidateZeropsYml_MultiBaseType — base: [php@8.4, nodejs@22]
  TestValidateZeropsYml_StageZscNoop_Warning
```

---

## Feature 2B: Env Ref Validation (C2)

### Problem
Zerops silently keeps invalid `${hostname_varName}` refs as literal strings. No API error, no deploy failure, silent data corruption.

### Implementation

```go
// internal/ops/deploy_validate.go
func ValidateEnvReferences(envVars map[string]string, discoveredEnvVars map[string][]string, liveHostnames []string) []EnvRefError {
    // For each env var value, find ${hostname_varName} patterns
    // Validate: hostname exists in liveHostnames
    // Validate: varName exists in discoveredEnvVars[hostname] (case-sensitive)
}

type EnvRefError struct {
    Variable  string // env var name containing the bad ref
    Reference string // the ${hostname_varName} reference
    Reason    string // "unknown hostname" or "unknown variable"
}
```

### Tests

```
internal/ops/deploy_validate_test.go:
  TestValidateEnvReferences_ValidRef_NoError
  TestValidateEnvReferences_InvalidHostname_Error
  TestValidateEnvReferences_InvalidVarName_Error
  TestValidateEnvReferences_CaseSensitive_Error — ${db_ConnectionString} vs ${db_connectionString}
  TestValidateEnvReferences_MultipleRefs_AllChecked
  TestValidateEnvReferences_NoRefs_NoError
  TestValidateEnvReferences_LiteralDollar_Ignored
```

### Dependency
Needs `DiscoveredEnvVars` from Feature 1B to function within bootstrap. Can be tested standalone with manual input.

---

## Feature 2C: Batch Verify (VerifyAll)

### Implementation

```go
// internal/ops/verify.go
type VerifyAllResult struct {
    Summary  string         `json:"summary"`   // "5/5 healthy" or "3/5 healthy, 2 unhealthy"
    Status   string         `json:"status"`    // healthy/degraded/unhealthy
    Services []VerifyResult `json:"services"`
}

func VerifyAll(ctx context.Context, client platform.Client, fetcher platform.LogFetcher,
    httpClient *http.Client, projectID string) (*VerifyAllResult, error) {
    // 1. ListServices
    // 2. Run Verify per non-system service in parallel (errgroup, max 5)
    // 3. Aggregate results
}
```

### MCP Tool Change

```go
// internal/tools/verify.go
type VerifyInput struct {
    ServiceHostname string `json:"serviceHostname,omitempty"` // was required, now optional
}
// When empty: call VerifyAll
// When set: call Verify (existing behavior)
```

### Tests

```
internal/ops/verify_test.go:
  TestVerifyAll_AllHealthy
  TestVerifyAll_MixedResults
  TestVerifyAll_EmptyProject
  TestVerifyAll_ParallelExecution — verify errgroup concurrency

internal/tools/verify_test.go:
  TestVerifyTool_BatchMode — no hostname, returns VerifyAllResult
  TestVerifyTool_SingleMode — with hostname, returns VerifyResult (unchanged)
```

### Depends on: Feature 2A (runtime classification must exist for correct per-service checks)

---

## Deploy & Test

```bash
go test ./internal/ops/... -count=1 -v
go test ./internal/tools/... -count=1 -v
go test ./... -count=1 -short
make lint-fast
./eval/scripts/build-deploy.sh
```

### Live Tests on zcpx
1. `zerops_verify` (no hostname) → batch verify all 8 services, expect ~7-10s
2. `zerops_verify serviceHostname=phpdev` → single verify with new check names
3. `zerops_verify serviceHostname=nodestage` → verify static-like behavior (READY_TO_DEPLOY may skip)
4. Compare batch time vs sequential: 8 services should complete in ~10-15s vs old ~2min
