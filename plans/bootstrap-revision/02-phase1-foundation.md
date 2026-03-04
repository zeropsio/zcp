# Phase 1: Foundation — BootstrapTarget Types

**Agent**: ALPHA (workflow specialist)
**Dependencies**: None (can run parallel with Phase 2)
**Risk**: MEDIUM — 48 references across 7 files, but atomic and no backward compat needed

---

## Feature 1A: BootstrapTarget Types + Validation

### New Types (in `internal/workflow/validate.go`)

```go
type BootstrapTarget struct {
    Runtime      RuntimeTarget `json:"runtime"`
    Dependencies []Dependency  `json:"dependencies,omitempty"`
}

type RuntimeTarget struct {
    DevHostname string `json:"devHostname"`
    Type        string `json:"type"`
    IsExisting  bool   `json:"isExisting,omitempty"`
    Simple      bool   `json:"simple,omitempty"`
}

func (r RuntimeTarget) StageHostname() string {
    if r.Simple { return "" }
    if base, ok := strings.CutSuffix(r.DevHostname, "dev"); ok {
        return base + "stage"
    }
    return ""
}

type Dependency struct {
    Hostname   string `json:"hostname"`
    Type       string `json:"type"`
    Mode       string `json:"mode,omitempty"`
    Resolution string `json:"resolution"` // CREATE, EXISTS, SHARED
}

type ServicePlan struct {
    Targets   []BootstrapTarget `json:"targets"`
    CreatedAt string            `json:"createdAt"`
}
```

### Deleted Types
- `PlannedService` — delete entirely
- `ValidateServicePlan()` — replaced by `ValidateBootstrapTargets()`

### Validation Rules (`ValidateBootstrapTargets`)

1. All hostnames pass `platform.ValidateHostname()`
2. H7: validate BOTH dev AND derived stage hostname lengths
3. All types exist in live catalog
4. Dev/stage pairing: `StageHostname()` must return non-empty for standard mode
5. CREATE deps must NOT exist in live services
6. EXISTS deps MUST exist in live services
7. H9: `isManagedStorage()` for shared-storage (no env var checks)
8. C3 SHARED resolution: collect all CREATE hostnames across targets, promote to SHARED if referenced by another target
9. No duplicate hostnames within a target's dependencies
10. Managed services default to NON_HA

### Files Modified

| File | Changes |
|------|---------|
| `internal/workflow/validate.go` | FULL REWRITE — new types, new validation |
| `internal/workflow/bootstrap.go` | Update `BootstrapState.Plan` to use `ServicePlan` with `Targets`. Update `validateConditionalSkip()`. |
| `internal/workflow/engine.go` | `BootstrapCompletePlan()` accepts `[]BootstrapTarget`. Step name check remains `"plan"` for now (changed in Phase 3). |
| `internal/workflow/managed_types.go` | Add `isManagedStorage()` — returns true only for shared-storage |
| `internal/tools/workflow.go` | `WorkflowInput.Plan` type change to `[]workflow.BootstrapTarget` |
| `internal/tools/workflow_bootstrap.go` | Update plan routing |

### Tests (RED first)

```
internal/workflow/validate_test.go — FULL REWRITE:
  TestValidateBootstrapTargets_SingleTarget_Success
  TestValidateBootstrapTargets_EmptyTargets_Error
  TestValidateBootstrapTargets_InvalidHostname_Error
  TestValidateBootstrapTargets_StageHostnameOverflow_Error (H7)
  TestValidateBootstrapTargets_StorageExcluded_FromEnvCheck (H9)
  TestValidateBootstrapTargets_SharedResolution_Success (C3)
  TestValidateBootstrapTargets_SharedResolution_NoCreate_Error
  TestValidateBootstrapTargets_CreateServiceExists_Error
  TestValidateBootstrapTargets_ExistsServiceMissing_Error
  TestValidateBootstrapTargets_SimpleMode_NoStage
  TestValidateBootstrapTargets_DuplicateHostname_Error
  TestValidateBootstrapTargets_UnknownType_Error
  TestValidateBootstrapTargets_ManagedModeDefault_NON_HA
  TestStageHostname_Standard
  TestStageHostname_Simple
  TestStageHostname_NoDevSuffix

internal/workflow/engine_test.go:
  Update TestEngine_BootstrapCompletePlan_* tests for BootstrapTarget input

internal/workflow/bootstrap_test.go:
  Update TestValidateConditionalSkip_* for new types

internal/tools/workflow_test.go:
  Update plan submission tests for new JSON structure
```

---

## Feature 1B: Session-Scoped Lifecycle

### New Fields (in `internal/workflow/bootstrap.go`)

```go
const (
    LifecyclePlanned    = "planned"
    LifecycleCreated    = "created"
    LifecycleConfigured = "configured"
    LifecycleDeployed   = "deployed"
    LifecycleVerified   = "verified"
    LifecycleReady      = "ready"
)

type BootstrapState struct {
    // existing fields...
    DiscoveredEnvVars map[string][]string `json:"discoveredEnvVars,omitempty"`
}
```

Lifecycle is tracked per-target within the `BootstrapTarget` itself (add `Lifecycle string` field to `RuntimeTarget`).

### New Engine Methods

```go
func (e *Engine) StoreDiscoveredEnvVars(hostname string, vars []string) error
func (e *Engine) updateLifecycle(state *WorkflowState, stepName string)
```

### Tests

```
internal/workflow/bootstrap_test.go:
  TestLifecycle_Progression — planned→created→configured→deployed→verified→ready
  TestDiscoveredEnvVars_StoreAndRetrieve
  TestDiscoveredEnvVars_MultipleServices

internal/workflow/engine_test.go:
  TestEngine_StoreDiscoveredEnvVars
  TestEngine_UpdateLifecycle_AfterComplete
```

---

## Deploy & Test

```bash
go test ./internal/workflow/... -count=1 -v
go test ./internal/tools/... -count=1 -v
go test ./integration/... -count=1 -v
go test ./... -count=1 -short
make lint-fast
./eval/scripts/build-deploy.sh
```

### Live Tests
1. `zerops_workflow action="start" workflow="bootstrap"` → `action="complete" step="plan" plan=[{runtime:{devHostname:"testdev",type:"nodejs@22"},dependencies:[{hostname:"db",type:"postgresql@16",resolution:"EXISTS"}]}]`
2. Verify state file has BootstrapTarget structure
3. Verify lifecycle field populated
