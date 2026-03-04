# Bootstrap Flow Revision -- Implementation Plan

Source: `plans/analysis-bootstrap-revision.md` (the single source of truth for design decisions).

---

## Execution Model

### Agent Team

| Agent | Scope | Dependencies |
|-------|-------|-------------|
| **ALPHA** | Phase 1-2: Types + Lifecycle | None (foundational) |
| **BRAVO** | Phase 3: Verify Speedup + Runtime Classification | None (independent of ALPHA) |
| **CHARLIE** | Phase 4: Hard Checks + Step Consolidation | ALPHA (needs BootstrapTarget types), BRAVO (needs revised verify) |
| **DELTA** | Phase 5: Outputs + Content | CHARLIE (needs 5-step model working) |
| **ECHO** | Phase 6: Integration + E2E | All prior phases |

ALPHA and BRAVO work in parallel. CHARLIE waits for both. DELTA follows CHARLIE. ECHO is final validation.

### Per-Phase Protocol

Every phase follows this cycle:

1. **RED**: Write failing tests at all affected layers
2. **GREEN**: Minimal implementation to pass
3. **REFACTOR**: Clean up, verify all tests green
4. **BUILD**: `go build -o bin/zcp ./cmd/zcp`
5. **DEPLOY**: `sudo cp bin/zcp /usr/bin/zcp && ssh zcpx sudo supervisorctl restart zcpx`
6. **SMOKE**: Test on live platform via MCP client
7. **GATE**: `go test ./... -count=1 -short && make lint-fast`

---

## Phase 1: BootstrapTarget Types + Plan Validation (ALPHA)

**Goal**: Replace `PlannedService` / `ServicePlan` with `BootstrapTarget` / runtime-centric plan. This is the foundational type change that everything else depends on.

**Estimated**: ~450 lines changed/added, ~300 lines deleted, 5-7 files modified, 2 new files.

### Step 1.1: New Types (RED then GREEN)

**File**: `internal/workflow/validate.go` (rewrite)

Current types to DELETE:
- `PlannedService` struct
- `ServicePlan` struct (replace with new version)
- `ValidateServicePlan()` function

New types to ADD:
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

func (r RuntimeTarget) StageHostname() string { ... }

type Dependency struct {
    Hostname   string `json:"hostname"`
    Type       string `json:"type"`
    Mode       string `json:"mode,omitempty"`
    Resolution string `json:"resolution"`
}

type ServicePlan struct {
    Targets   []BootstrapTarget `json:"targets"`
    CreatedAt string            `json:"createdAt"`
}
```

New function: `ValidateBootstrapTargets(targets []BootstrapTarget, liveTypes []platform.ServiceStackType, liveServices []platform.ServiceStack) ([]string, error)`

Validation rules (from analysis 3.2):
- All hostnames pass `ValidateHostname()`
- H7: validate BOTH dev AND derived stage hostname lengths (stage adds 2 chars)
- All types exist in live catalog (existing check)
- Dev/stage pairing enforced via `StageHostname()` (skipped when `Simple=true`)
- H9: storage classification -- `MANAGED_WITH_ENVS` vs `MANAGED_STORAGE` (shared-storage only)
- CREATE dependencies must NOT exist in live services
- EXISTS dependencies MUST exist in live services
- C3: Cross-target SHARED resolution (target 1 creates `db`, target 2 references it)
- Managed service modes default to `NON_HA`

**Tests**: `internal/workflow/validate_test.go` (rewrite)

```
TestValidateBootstrapTargets_SingleTarget_Success
TestValidateBootstrapTargets_EmptyTargets_Error
TestValidateBootstrapTargets_InvalidHostname_Error
TestValidateBootstrapTargets_StageLengthOverflow_Error (H7)
TestValidateBootstrapTargets_SimpleMode_NoStageRequired
TestValidateBootstrapTargets_CreateDependencyExists_Error
TestValidateBootstrapTargets_ExistsDependencyMissing_Error
TestValidateBootstrapTargets_SharedResolution_Success (C3)
TestValidateBootstrapTargets_SharedResolution_NoCreator_Error
TestValidateBootstrapTargets_ManagedDefaultsNonHA
TestValidateBootstrapTargets_SharedStorageNoEnvCheck (H9)
TestValidateBootstrapTargets_ObjectStorageHasEnvs (H9)
TestValidateBootstrapTargets_TypeNotInCatalog_Error
TestValidateBootstrapTargets_MultipleTargets_SharedDeps
TestStageHostname_DevSuffix_ReturnsStage
TestStageHostname_Simple_ReturnsEmpty
TestStageHostname_NoDevSuffix_ReturnsEmpty
```

~15 test cases, ~250 lines of tests.

### Step 1.2: Update BootstrapState to Use New Plan Type

**File**: `internal/workflow/bootstrap.go`

Changes:
- `BootstrapState.Plan` field: `*ServicePlan` stays (same name, new struct definition)
- `BootstrapResponse`: add `CheckResult *StepCheckResult` field (for Phase 4, but type needed now)
- Add `DiscoveredEnvVars map[string][]string` field to `BootstrapState` (from analysis 4.4.1)
- Update `validateConditionalSkip()` to work with `[]BootstrapTarget` instead of `[]PlannedService`
- Update step name constants from 11-step names to 5-step names (prepare for Phase 4)

**File**: `internal/workflow/bootstrap_test.go` (update)

All existing tests that reference `PlannedService` or the old `ServicePlan` must be rewritten against `BootstrapTarget`. Since this is early development with no backward compat, delete old tests and write new ones.

### Step 1.3: Update Engine to Accept New Types

**File**: `internal/workflow/engine.go`

Changes:
- `BootstrapCompletePlan()`: rename parameter from `services []PlannedService` to `targets []BootstrapTarget`
- Update internal validation call from `ValidateServicePlan()` to `ValidateBootstrapTargets()`
- `BootstrapCompletePlan()` now checks `CurrentStepName() == "discover"` instead of `"plan"`
- Add `liveServices []platform.ServiceStack` parameter to `BootstrapCompletePlan` (for EXISTS validation)

**File**: `internal/workflow/engine_test.go` (update)

Rewrite tests that exercise `BootstrapCompletePlan` with new types.

### Step 1.4: Update Tool Layer

**File**: `internal/tools/workflow.go`

Changes:
- `WorkflowInput.Plan`: change type from `[]workflow.PlannedService` to `[]workflow.BootstrapTarget`
- Update JSON schema description

**File**: `internal/tools/workflow_bootstrap.go`

Changes:
- `handleBootstrapComplete()`: route `input.Step == "discover"` (not "plan") with plan data
- Pass `liveServices` to `BootstrapCompletePlan`
- Fetch live services via `client.ListServices()` when plan is submitted

**File**: `internal/tools/workflow_test.go` (update)

Rewrite tests that submit plans. Change step from "plan" to "discover".

### Step 1.5: Update Integration Tests

**File**: `integration/bootstrap_conductor_test.go`, `integration/bootstrap_realistic_test.go`

Update all plan submissions to use `BootstrapTarget` format.

### Step 1.6: Delete Dead Code

After all tests pass:
- Delete `PlannedService` type entirely
- Delete `ValidateServicePlan()` function entirely
- Delete any helper functions that only served `PlannedService`
- Clean all imports

### Files Summary (Phase 1)

| File | Action | ~Lines Changed |
|------|--------|---------------|
| `internal/workflow/validate.go` | Rewrite | 113 -> ~180 |
| `internal/workflow/validate_test.go` | Rewrite | ~250 |
| `internal/workflow/bootstrap.go` | Modify | ~30 lines changed |
| `internal/workflow/bootstrap_test.go` | Modify | ~80 lines changed |
| `internal/workflow/engine.go` | Modify | ~40 lines changed |
| `internal/workflow/engine_test.go` | Modify | ~60 lines changed |
| `internal/tools/workflow.go` | Modify | ~10 lines changed |
| `internal/tools/workflow_bootstrap.go` | Modify | ~20 lines changed |
| `internal/tools/workflow_test.go` | Modify | ~100 lines changed |
| `integration/bootstrap_conductor_test.go` | Modify | ~40 lines changed |
| `integration/bootstrap_realistic_test.go` | Modify | ~40 lines changed |

---

## Phase 2: Session-Scoped Lifecycle + Service Metadata (ALPHA)

**Goal**: Add per-service lifecycle tracking within bootstrap sessions and per-service decision metadata files. Registry model (H4) for session management.

**Estimated**: ~400 lines new, 3 new files, 3 modified files.

### Step 2.1: Service Lifecycle in BootstrapState

**File**: `internal/workflow/bootstrap.go` (modify)

Add lifecycle constants and tracking:
```go
const (
    LifecyclePlanned    = "planned"
    LifecycleCreated    = "created"
    LifecycleConfigured = "configured"
    LifecycleDeployed   = "deployed"
    LifecycleVerified   = "verified"
    LifecycleReady      = "ready"
)
```

Add `ServiceLifecycles map[string]string` to `BootstrapState`.
Add `UpdateLifecycle(hostname, lifecycle string)` method.

**Tests**: `internal/workflow/bootstrap_test.go` -- add lifecycle tracking test cases.

### Step 2.2: Per-Service Decision Metadata

**File**: `internal/workflow/service_meta.go` (NEW, ~60 lines)

```go
type ServiceMeta struct {
    Hostname         string            `json:"hostname"`
    Type             string            `json:"type"`
    Mode             string            `json:"mode"`
    StageHostname    string            `json:"stageHostname,omitempty"`
    DeployFlow       string            `json:"deployFlow"`
    Dependencies     []string          `json:"dependencies,omitempty"`
    BootstrapSession string            `json:"bootstrapSession"`
    BootstrappedAt   string            `json:"bootstrappedAt"`
    Decisions        map[string]string `json:"decisions,omitempty"`
}

func WriteServiceMeta(baseDir string, meta *ServiceMeta) error { ... }
func ReadServiceMeta(baseDir, hostname string) (*ServiceMeta, error) { ... }
```

**Tests**: `internal/workflow/service_meta_test.go` (NEW, ~80 lines)

```
TestWriteServiceMeta_Success
TestReadServiceMeta_Success
TestReadServiceMeta_NotFound
TestWriteServiceMeta_Overwrites
```

### Step 2.3: Session Registry (H4)

**File**: `internal/workflow/registry.go` (NEW, ~130 lines)

```go
type Registry struct {
    Version  string         `json:"version"`
    Sessions []SessionEntry `json:"sessions"`
}

type SessionEntry struct {
    SessionID string `json:"sessionId"`
    Workflow  string `json:"workflow"`
    Phase     Phase  `json:"phase"`
    PID       int    `json:"pid"`
    Stale     bool   `json:"stale"`
    CreatedAt string `json:"createdAt"`
    UpdatedAt string `json:"updatedAt"`
}

func withRegistryLock(registryPath string, fn func() error) error { ... }
func registerSession(registryDir string, entry SessionEntry) error { ... }
func unregisterSession(registryDir, sessionID string) error { ... }
func listSessions(registryDir string) (*Registry, error) { ... }
func refreshStale(registry *Registry) { ... }
func processAlive(pid int) bool { ... }
```

**Tests**: `internal/workflow/registry_test.go` (NEW, ~120 lines)

```
TestRegisterSession_Success
TestUnregisterSession_Success
TestListSessions_Empty
TestRefreshStale_DeadPID
TestRefreshStale_AlivePID
TestRegistryLock_Concurrent
```

### Step 2.4: Engine Integration

**File**: `internal/workflow/engine.go` (modify)
**File**: `internal/workflow/session.go` (modify)

Update `Start()`, `Reset()`, `HasActiveSession()` to use registry instead of singleton state file. Per-session files in `.zcp/state/sessions/{id}.json`.

### Files Summary (Phase 2)

| File | Action | ~Lines |
|------|--------|--------|
| `internal/workflow/bootstrap.go` | Modify | ~30 new |
| `internal/workflow/bootstrap_test.go` | Modify | ~40 new |
| `internal/workflow/service_meta.go` | NEW | ~60 |
| `internal/workflow/service_meta_test.go` | NEW | ~80 |
| `internal/workflow/registry.go` | NEW | ~130 |
| `internal/workflow/registry_test.go` | NEW | ~120 |
| `internal/workflow/engine.go` | Modify | ~40 changed |
| `internal/workflow/session.go` | Modify | ~50 changed |

---

## Phase 3: Verify Speedup + Runtime Classification (BRAVO, parallel with ALPHA)

**Goal**: Batch verify, log deduplication, runtime-class awareness, 2-endpoint model (/health eliminated).

**Estimated**: ~350 lines changed/added, ~100 lines deleted.

### Step 3.1: Runtime Classification

**File**: `internal/ops/verify.go` (modify)

Add runtime classification:
```go
type RuntimeClass string
const (
    RuntimeDynamic  RuntimeClass = "dynamic"   // nodejs, go, bun, python, etc.
    RuntimeStatic   RuntimeClass = "static"    // static, nginx
    RuntimeImplicit RuntimeClass = "implicit"  // php-apache, php-nginx
    RuntimeWorker   RuntimeClass = "worker"    // no run.ports
    RuntimeManaged  RuntimeClass = "managed"   // databases, caches
)

func classifyRuntime(svc *platform.ServiceStack) RuntimeClass { ... }
```

**Tests**: `internal/ops/verify_test.go` -- add classification test cases.

```
TestClassifyRuntime_Nodejs_Dynamic
TestClassifyRuntime_Static_Static
TestClassifyRuntime_PhpNginx_Implicit
TestClassifyRuntime_NoPorts_Worker
TestClassifyRuntime_Postgresql_Managed
```

### Step 3.2: Replace /health with /root Check (C1)

**File**: `internal/ops/verify_checks.go` (modify)

- Rename `checkHTTPHealth()` to `checkHTTPRoot()`
- Change URL from `/health` to `/`
- Update all references in `verify.go`
- Skip `startup_detected` for implicit webserver + static runtimes (M1)
- Skip `http_status` for static/nginx runtimes (C1)

**Tests**: Update all http_health test cases to http_root.

### Step 3.3: Log Batch Optimization

**File**: `internal/ops/verify_checks.go` (modify)

Add `batchLogChecks()`:
```go
func batchLogChecks(ctx context.Context, fetcher platform.LogFetcher,
    logAccess *platform.LogAccess, serviceID string) (errorCheck5m, errorCheck2m, startupCheck CheckResult) {
    // Single fetch for errors (5m), derive 2m locally
    // Separate fetch for startup
}
```

Replace 3 sequential log calls with 2 batched calls.

### Step 3.4: Batch Verify (VerifyAll)

**File**: `internal/ops/verify.go` (add ~80 lines)

```go
type VerifyAllResult struct {
    Summary  string         `json:"summary"`
    Status   string         `json:"status"`
    Services []VerifyResult `json:"services"`
}

func VerifyAll(ctx context.Context, client platform.Client, fetcher platform.LogFetcher,
    httpClient HTTPDoer, projectID string) (*VerifyAllResult, error) {
    // ListServices once, verify each in parallel (errgroup, max 5 concurrency)
}
```

**Tests**: `internal/ops/verify_test.go` -- add VerifyAll tests.

```
TestVerifyAll_AllHealthy
TestVerifyAll_OneDegraded
TestVerifyAll_OneUnhealthy
TestVerifyAll_Empty
TestVerifyAll_MixedRuntimeClasses
```

### Step 3.5: Update zerops_verify Tool for Batch Mode

**File**: `internal/tools/verify.go` (modify)

When `serviceHostname` is omitted, call `VerifyAll()` instead of single `Verify()`.

**Tests**: `internal/tools/verify_test.go` (if exists, otherwise within tools_test.go)

### Step 3.6: Build Poll Speedup

**File**: `internal/ops/process.go` (modify, ~5 lines)

Change constants: `initial=1s, stepUp=5s, stepUpAfter=30s`.

### Step 3.7: Build Base Type Fix (C4)

**File**: `internal/ops/deploy_validate.go` (modify)

Change `zeropsYmlBuild.Base` from `string` to `any`. Add `baseStrings()` normalizer.
Update `hasImplicitWebServer()` to accept `any` for both parameters.

**Tests**: `internal/ops/deploy_validate_test.go`

```
TestHasImplicitWebServer_ArrayBase
TestHasImplicitWebServer_StringBase
TestBaseStrings_String
TestBaseStrings_Array
TestBaseStrings_Nil
```

### Files Summary (Phase 3)

| File | Action | ~Lines |
|------|--------|--------|
| `internal/ops/verify.go` | Modify + Add | ~100 lines added |
| `internal/ops/verify_checks.go` | Modify | ~60 lines changed |
| `internal/ops/verify_test.go` | Modify | ~150 lines added |
| `internal/ops/deploy_validate.go` | Modify | ~30 lines changed |
| `internal/ops/deploy_validate_test.go` | Modify/NEW | ~60 lines |
| `internal/ops/process.go` | Modify | ~5 lines |
| `internal/tools/verify.go` | Modify | ~20 lines |

---

## Phase 4: Hard Checks + Step Consolidation (CHARLIE)

**Depends on**: Phase 1 (BootstrapTarget types), Phase 3 (VerifyAll, runtime classification).

**Goal**: Server-side hard checks at step boundaries. Consolidate 11 steps to 5. This is the core value delivery.

**Estimated**: ~600 lines new/changed, 3 new files, 8 modified files.

### Step 4.1: Hard Check Types

**File**: `internal/workflow/bootstrap_checks.go` (NEW, ~50 lines for types)

```go
type StepCheckResult struct {
    Passed  bool        `json:"passed"`
    Checks  []StepCheck `json:"checks"`
    Summary string      `json:"summary"`
}

type StepCheck struct {
    Name   string `json:"name"`
    Status string `json:"status"` // "pass", "fail", "skip"
    Detail string `json:"detail,omitempty"`
}

type StepChecker func(ctx context.Context, plan *ServicePlan) (*StepCheckResult, error)
```

**Tests**: `internal/workflow/bootstrap_checks_test.go` (NEW, ~100 lines)

```
TestStepCheckResult_AllPassed
TestStepCheckResult_SomeFailed
TestStepCheckResult_Summary
```

### Step 4.2: BootstrapComplete Signature Change (H1)

**File**: `internal/workflow/engine.go` (modify)

Change signature:
```go
func (e *Engine) BootstrapComplete(ctx context.Context, stepName string, checker StepChecker) (*BootstrapResponse, error)
```

- Add `context.Context` parameter
- Add `StepChecker` parameter (nullable)
- Run checker before completing step
- On check failure: return response with `CheckResult` populated (NOT a Go error)
- Remove attestation requirement for auto-completable steps

**File**: `internal/workflow/engine_test.go` (modify)

Update all `BootstrapComplete` call sites.

### Step 4.3: Step Consolidation (11 -> 5)

**File**: `internal/workflow/bootstrap_steps.go` (rewrite)

Replace 11 `stepDetails` entries with 5:

```go
var stepDetails = []StepDetail{
    {Name: "discover",  Category: CategoryCreative,  Skippable: false, AutoComplete: false, ...},
    {Name: "provision", Category: CategoryFixed,     Skippable: false, AutoComplete: true,  ...},
    {Name: "generate",  Category: CategoryCreative,  Skippable: true,  AutoComplete: false, ...},
    {Name: "deploy",    Category: CategoryBranching, Skippable: true,  AutoComplete: false, ...},
    {Name: "verify",    Category: CategoryFixed,     Skippable: false, AutoComplete: true,  ...},
}
```

Add `AutoComplete bool` field to `StepDetail`.

Update all guidance text for 5-step model.

**File**: `internal/workflow/bootstrap.go` (modify)

- Update step name constants: `stepDiscoverEnvs` etc. -> new names
- Update `validateConditionalSkip()` for new step names (H6)
- `NewBootstrapState()` now creates 5 steps, not 11

**Tests**: All bootstrap tests rewritten for 5 steps.

### Step 4.4: Build Step Checkers in Tool Layer

**File**: `internal/tools/workflow_checks.go` (NEW, ~120 lines)

```go
func buildStepChecker(ctx context.Context, step string, client platform.Client,
    fetcher platform.LogFetcher, projectID string, tracker *ops.KnowledgeTracker,
    state *workflow.BootstrapState) workflow.StepChecker {

    switch step {
    case "discover":
        return nil // plan validation handled by BootstrapCompletePlan
    case "provision":
        return func(ctx context.Context, plan *workflow.ServicePlan) (*workflow.StepCheckResult, error) {
            return checkProvision(ctx, client, projectID, plan)
        }
    case "generate":
        return func(ctx context.Context, plan *workflow.ServicePlan) (*workflow.StepCheckResult, error) {
            return checkGenerate(ctx, client, projectID, plan, state)
        }
    case "deploy":
        return func(ctx context.Context, plan *workflow.ServicePlan) (*workflow.StepCheckResult, error) {
            return checkDeploy(ctx, client, fetcher, projectID, plan)
        }
    case "verify":
        return func(ctx context.Context, plan *workflow.ServicePlan) (*workflow.StepCheckResult, error) {
            return checkVerify(ctx, client, fetcher, projectID, plan)
        }
    default:
        return nil
    }
}
```

Individual checker implementations:

- `checkProvision()`: ListServices -> verify all planned exist; verify statuses; verify dev mounts; verify env vars for MANAGED_WITH_ENVS services
- `checkGenerate()`: Read zerops.yml from mount path; validate structure per target; C2 env ref full validation using `BootstrapState.DiscoveredEnvVars`; validate no healthCheck on dev
- `checkDeploy()`: Verify build status ACTIVE per deployed target; subdomain gate check
- `checkVerify()`: Call `VerifyAll()`, filter results by bootstrap targets (M2)

**Tests**: `internal/tools/workflow_checks_test.go` (NEW, ~200 lines)

```
TestCheckProvision_AllServicesExist_Pass
TestCheckProvision_MissingService_Fail
TestCheckProvision_ManagedNoEnvVars_Fail
TestCheckProvision_SharedStorageNoEnvCheck_Pass (H9)
TestCheckGenerate_ValidZeropsYml_Pass
TestCheckGenerate_MissingSetupEntry_Fail
TestCheckGenerate_DevHasHealthCheck_Fail
TestCheckGenerate_InvalidEnvRef_Fail (C2)
TestCheckGenerate_ValidEnvRef_Pass (C2)
TestCheckDeploy_AllActive_Pass
TestCheckDeploy_BuildFailed_Fail
TestCheckDeploy_NoSubdomain_Fail
TestCheckVerify_AllHealthy_Pass
TestCheckVerify_FilterByTargets_Pass (M2)
```

### Step 4.5: Wire Checkers into Tool Handler

**File**: `internal/tools/workflow_bootstrap.go` (modify)

- `handleBootstrapComplete()`: build checker via `buildStepChecker()`, pass to `engine.BootstrapComplete(ctx, ...)`
- Add `logFetcher` parameter to handler

**File**: `internal/tools/workflow.go` (modify)

- `RegisterWorkflow()`: add `logFetcher` parameter (H7.7 from analysis)
- Pass to handlers

**File**: `internal/server/server.go` (modify)

- Update `RegisterWorkflow()` call to pass `s.logFetcher`

### Step 4.6: Auto-Completion for Mechanical Steps

**File**: `internal/workflow/engine.go` (modify)

When step has `AutoComplete: true` and checker passes, auto-advance without LLM calling `action="complete"`. This happens within the tool call that triggers the checks (e.g., after `zerops_import` succeeds, the provision step auto-checks and auto-completes).

Implementation: Add auto-check call at end of relevant tool handlers (import, verify).

### Step 4.7: Phase Gate Simplification

**File**: `internal/workflow/bootstrap_evidence.go` (modify)

`autoCompleteBootstrap()`: simplify to just transition phases without synthetic evidence. Hard checks already validated. Keep gate transitions for non-bootstrap workflows.

**File**: `internal/workflow/gates.go` (no change needed -- gates stay for non-bootstrap)

### Step 4.8: Import Error Surfacing (C5)

**File**: `internal/ops/import.go` (modify)

Add `ServiceErrors []ServiceImportError` to `ImportResult`:
```go
type ServiceImportError struct {
    Hostname string `json:"hostname"`
    Error    string `json:"error"`
}
```

In the `ServiceStacks` loop, check `ss.Error` field. Collect non-empty errors.

**Tests**: `internal/ops/import_test.go` (add test cases for partial import errors)

### Step 4.9: Env Var Reference Validation (C2)

**File**: `internal/ops/deploy_validate.go` (add ~50 lines)

```go
func ValidateEnvReferences(envVars map[string]string, discoveredEnvs map[string][]string, liveHostnames []string) []string {
    // Parse ${hostname_varName} patterns
    // Validate hostname exists
    // Validate varName exists in discovered env vars (case-sensitive)
}
```

**Tests**: `internal/ops/deploy_validate_test.go`

```
TestValidateEnvReferences_ValidRef_NoWarning
TestValidateEnvReferences_InvalidHostname_Warning
TestValidateEnvReferences_InvalidVarName_Warning
TestValidateEnvReferences_CaseSensitive_Warning
TestValidateEnvReferences_NoRefs_NoWarning
```

### Files Summary (Phase 4)

| File | Action | ~Lines |
|------|--------|--------|
| `internal/workflow/bootstrap_checks.go` | NEW | ~50 |
| `internal/workflow/bootstrap_checks_test.go` | NEW | ~100 |
| `internal/workflow/engine.go` | Modify | ~60 changed |
| `internal/workflow/engine_test.go` | Modify | ~80 changed |
| `internal/workflow/bootstrap_steps.go` | Rewrite | 275 -> ~150 |
| `internal/workflow/bootstrap.go` | Modify | ~40 changed |
| `internal/workflow/bootstrap_test.go` | Modify | ~100 changed |
| `internal/workflow/bootstrap_evidence.go` | Modify | ~30 changed |
| `internal/tools/workflow_checks.go` | NEW | ~120 |
| `internal/tools/workflow_checks_test.go` | NEW | ~200 |
| `internal/tools/workflow_bootstrap.go` | Modify | ~40 changed |
| `internal/tools/workflow.go` | Modify | ~15 changed |
| `internal/server/server.go` | Modify | ~5 changed |
| `internal/ops/import.go` | Modify | ~20 changed |
| `internal/ops/deploy_validate.go` | Modify | ~50 added |
| `internal/ops/deploy_validate_test.go` | Modify | ~60 added |

---

## Phase 5: Outputs + Content (DELTA)

**Depends on**: Phase 4 (5-step model working).

**Goal**: CLAUDE.md reflog, content deduplication, guidance updates, BuildInstructions routing fix.

**Estimated**: ~400 lines new/changed.

### Step 5.1: CLAUDE.md Reflog

**File**: `internal/workflow/reflog.go` (NEW, ~60 lines)

```go
func AppendReflogEntry(claudeMDPath string, intent string, targets []BootstrapTarget,
    sessionID string, timestamp time.Time) error {
    // Generate markdown entry
    // Append to file (create if needed)
    // Wrap in <!-- ZEROPS:REFLOG --> markers
}
```

**Tests**: `internal/workflow/reflog_test.go` (NEW, ~80 lines)

```
TestAppendReflogEntry_NewFile
TestAppendReflogEntry_ExistingFile
TestAppendReflogEntry_MultipleEntries
TestAppendReflogEntry_TargetFormatting
```

### Step 5.2: Wire Reflog into Bootstrap Completion

**File**: `internal/workflow/engine.go` (modify)

After verify step auto-completes (all hard checks pass), call `AppendReflogEntry()`.
Need CLAUDE.md path -- derive from CWD or pass as config.

### Step 5.3: KnowledgeTracker Per-Type Tracking (H10)

**File**: `internal/ops/knowledge_tracker.go` (modify)

Change from boolean `briefingCalls` to `map[string]bool` keyed by runtime type:
```go
type KnowledgeTracker struct {
    mu              sync.Mutex
    runtimeBriefings map[string]bool // "php-nginx@8.4" -> true
    scopeLoaded     bool
}

func (kt *KnowledgeTracker) IsLoadedForRuntime(runtimeType string) bool { ... }
```

**Tests**: `internal/ops/knowledge_tracker_test.go` (modify)

```
TestKnowledgeTracker_IsLoadedForRuntime_Specific
TestKnowledgeTracker_IsLoadedForRuntime_Missing
TestKnowledgeTracker_MultipleRuntimes
```

### Step 5.4: BuildInstructions Routing Fix (H2)

**File**: `internal/server/instructions.go` (modify)

In `buildProjectSummary()`: when project is CONFORMANT but user intent includes a runtime type not present in existing services, route to bootstrap instead of deploy.

Add stack-match detection: compare intent runtime types against existing service types. If new runtime type requested, project needs bootstrap even if CONFORMANT.

**Tests**: `internal/server/instructions_test.go`

```
TestBuildProjectSummary_ConformantNewRuntime_SuggestsBootstrap
TestBuildProjectSummary_ConformantSameRuntime_SuggestsDeploy
TestBuildProjectSummary_Fresh_SuggestsBootstrap
```

### Step 5.5: Content Deduplication (bootstrap.md)

**File**: `internal/content/workflows/bootstrap.md` (rewrite)

- Merge 11 section tags into 5 (matching new step names)
- Deduplicate repeated content into reference appendix:
  - /status endpoint specification
  - Hostname rules
  - Dev vs stage configuration matrix
  - PHP runtime exceptions
- Mode-aware guidance: filter by plan mode (standard vs simple)
- Update guidance text for 5-step model

This is the largest single file change but is pure content, no code logic.

### Step 5.6: Clarification Guidance in Discover Step

Update discover step guidance in `bootstrap_steps.go` to include clarification flow:
1. Gather context (zerops_discover)
2. Load knowledge (zerops_knowledge)
3. Clarify with user if needed
4. Submit structured plan

### Files Summary (Phase 5)

| File | Action | ~Lines |
|------|--------|--------|
| `internal/workflow/reflog.go` | NEW | ~60 |
| `internal/workflow/reflog_test.go` | NEW | ~80 |
| `internal/workflow/engine.go` | Modify | ~20 changed |
| `internal/ops/knowledge_tracker.go` | Modify | ~30 changed |
| `internal/ops/knowledge_tracker_test.go` | Modify | ~40 changed |
| `internal/server/instructions.go` | Modify | ~30 changed |
| `internal/server/instructions_test.go` | Modify/NEW | ~60 |
| `internal/content/workflows/bootstrap.md` | Rewrite | ~751 -> ~500 |
| `internal/workflow/bootstrap_steps.go` | Modify | ~30 (guidance text) |
| `internal/workflow/bootstrap_guidance_test.go` | Modify | ~20 |

---

## Phase 6: Integration + E2E Validation (ECHO)

**Depends on**: All prior phases.

**Goal**: Full integration tests for 5-step model, E2E on live platform, final cleanup.

**Estimated**: ~300 lines new tests.

### Step 6.1: Integration Tests

**File**: `integration/bootstrap_conductor_test.go` (rewrite for 5-step model)
**File**: `integration/bootstrap_realistic_test.go` (rewrite for 5-step model)

New test scenarios:
```
TestBootstrap5Step_Fresh_SingleRuntime
TestBootstrap5Step_Fresh_MultiRuntime
TestBootstrap5Step_AddRuntime_Scenario_B
TestBootstrap5Step_AddManaged_Scenario_C
TestBootstrap5Step_HardCheck_ProvisionFails
TestBootstrap5Step_HardCheck_GenerateFails
TestBootstrap5Step_AutoComplete_Provision
TestBootstrap5Step_AutoComplete_Verify
```

### Step 6.2: Full Test Suite

```bash
go test ./... -count=1 -short    # All tests pass
go test ./... -count=1 -race     # No race conditions
make lint-fast                   # No lint issues
make lint-local                  # Full lint clean
```

### Step 6.3: Build + Deploy + Manual E2E

```bash
go build -o bin/zcp ./cmd/zcp
sudo cp bin/zcp /usr/bin/zcp
ssh zcpx sudo supervisorctl restart zcpx
```

Manual E2E test matrix:

1. **Scenario A**: "Create a PHP app with PostgreSQL"
   - Verify: 5 steps, reflog written, decision metadata in .zcp/services/
2. **Scenario B**: Add "Node.js API" to existing infrastructure
   - Verify: EXISTS resolution, only new services imported
3. **Scenario C**: "Add Valkey caching" to existing runtime
   - Verify: IsExisting=true, cache created, runtime redeployed
4. **Scenario D**: "PHP frontend + Node.js API + shared DB"
   - Verify: Multi-target, SHARED resolution, one import.yml

### Step 6.4: Final Cleanup

- Remove any remaining references to 11-step model
- Remove any dead code from type migration
- Verify no orphaned test helpers
- Verify `go vet ./...` clean
- Verify annotations_test.go still passes (tool descriptions)

---

## Dependency Graph

```
Phase 1 (ALPHA) ─────────┐
                          ├──> Phase 4 (CHARLIE) ──> Phase 5 (DELTA) ──> Phase 6 (ECHO)
Phase 3 (BRAVO) ──────────┘
                          │
Phase 2 (ALPHA) ──────────┘
```

Phases 1+3 are parallel (ALPHA, BRAVO).
Phase 2 is sequential after Phase 1 (ALPHA continues).
Phase 4 requires Phases 1, 2, 3.
Phase 5 requires Phase 4.
Phase 6 requires all.

## Execution Timeline

| Day | ALPHA | BRAVO |
|-----|-------|-------|
| 1 | Phase 1: Steps 1.1-1.3 (types + engine) | Phase 3: Steps 3.1-3.3 (classify + /root + logs) |
| 2 | Phase 1: Steps 1.4-1.6 (tools + cleanup) | Phase 3: Steps 3.4-3.7 (batch verify + poll + C4) |
| 3 | Phase 2: Steps 2.1-2.3 (lifecycle + meta + registry) | -- |
| 4 | Phase 2: Step 2.4 (engine integration) | -- |

| Day | CHARLIE |
|-----|---------|
| 5 | Phase 4: Steps 4.1-4.3 (types + signature + consolidation) |
| 6 | Phase 4: Steps 4.4-4.6 (checkers + wiring + auto-complete) |
| 7 | Phase 4: Steps 4.7-4.9 (gates + import + env refs) |

| Day | DELTA |
|-----|-------|
| 8 | Phase 5: Steps 5.1-5.3 (reflog + tracker) |
| 9 | Phase 5: Steps 5.4-5.6 (instructions + content) |

| Day | ECHO |
|-----|------|
| 10 | Phase 6: Integration tests + E2E + cleanup |

---

## Risk Mitigations

### Risk: Type Migration Breaks Existing Tests
**Mitigation**: Phase 1 is first and rewrites all tests. No backward compat shims. Delete `PlannedService` outright. If integration tests fail, fix them as part of Phase 1.

### Risk: Hard Checks Need API Access in Tests
**Mitigation**: Use `platform.MockClient` (existing pattern). Step checkers receive `platform.Client` interface, testable with mocks.

### Risk: 5-Step Consolidation Changes Tool Descriptions
**Mitigation**: Update `annotations_test.go` in Phase 4. Step names in JSON schema descriptions must match new step names.

### Risk: Content Changes (bootstrap.md) Break Guidance Extraction
**Mitigation**: `bootstrap_guidance_test.go` validates section tag extraction. Update test expectations in Phase 5 to match new section names.

### Risk: Registry (H4) Introduces File Locking Complexity
**Mitigation**: Registry uses flock on a single file. Brief lock duration (read-modify-write). Single-writer per session (PID ownership). If locking adds instability, can defer to Phase 6 and keep singleton model initially.

---

## Total Impact Estimate

| Metric | Count |
|--------|-------|
| Files modified | ~25 |
| Files created | ~8 |
| Files deleted | 0 (content absorbed into modified files) |
| Lines added | ~1,800 |
| Lines deleted | ~800 |
| Net change | ~+1,000 lines |
| Test cases added | ~60 |
| Test cases modified | ~40 |
| Test cases deleted | ~20 (replaced by new) |

---

## Verification Checklist (Per-Phase Gate)

Each phase must pass ALL before proceeding:

- [ ] `go test ./internal/workflow/... -count=1 -v` -- GREEN
- [ ] `go test ./internal/ops/... -count=1 -v` -- GREEN
- [ ] `go test ./internal/tools/... -count=1 -v` -- GREEN
- [ ] `go test ./integration/... -count=1 -v` -- GREEN
- [ ] `go test ./... -count=1 -short` -- GREEN
- [ ] `make lint-fast` -- CLEAN
- [ ] `go build -o bin/zcp ./cmd/zcp` -- SUCCESS
- [ ] Deploy to zcpx and smoke test -- PASS
