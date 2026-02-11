# ZCP Implementation Plan

> Synthesized from 5 analysis files in `plans/analysis/`. Each task = one PRD §13 implementation unit.
> TDD cycle: RED (failing test) → GREEN (minimal impl) → VERIFY (API test) → REFACTOR.

---

## How to Use This Plan

1. Work tasks **in order** — dependencies are explicit
2. Each task lists files, functions, test cases, and acceptance criteria
3. Cross-references point to PRD sections and analysis files — **do not copy, follow the reference**
4. Phase gates are mandatory — do not skip to next phase until gate passes

---

## Dependencies & go.mod

**Module**: `github.com/zeropsio/zcp` | **Go**: `1.24.0`

```
Direct:  go-sdk v1.2.0, zerops-go v1.0.16, bleve/v2 v2.5.7, yaml.v3 v3.0.1
Test:    testify v1.10.0
```

See `plans/analysis/dependencies.md` for full transitive dependency analysis and version conflict resolution.

**Internal package dependency graph** (arrows = imports):
```
cmd/zcp → server, init
server  → tools, knowledge
tools   → ops, platform (types only — NOT Client methods)
ops     → platform, auth, knowledge, content
platform → zerops-go SDK
auth     → platform
knowledge → bleve
content  → stdlib (go:embed)
init     → content
```

---

## Phase 1: Foundation (Units 1–9)

### Task 1: platform/types.go

**PRD**: §4.2 | **Analysis**: `analysis/platform.md` §1

**Create**: `internal/platform/types.go` (~200 lines)

**Types to define** (port from `../zaia/internal/platform/client.go:70-253`):
- `UserInfo`, `Project`, `ServiceStack`, `ServiceTypeInfo`, `Port`, `CustomAutoscaling`
- `AutoscalingParams`, `Process`, `ServiceStackRef`, `EnvVar`
- `ImportResult`, `ImportedServiceStack`, `APIError` (with `Error()` method)
- `LogAccess`, `LogFetchParams`, `LogEntry`
- `ProcessEvent`, `AppVersionEvent`, `BuildInfo`, `UserRef`
- `const DefaultAPITimeout = 30 * time.Second`

**Tests**: None needed — pure data types with no logic.

**Acceptance**: `go build ./internal/platform/...` compiles.

**Dependencies**: None (first task).

---

### Task 2: platform/errors.go

**PRD**: §4.4 | **Analysis**: `analysis/platform.md` §2

**Create**: `internal/platform/errors.go` (~100 lines)

**Implement**:
- 25 error code constants (skip 4 CLI-specific: `ErrSetupDownloadFailed`, `ErrSetupInstallFailed`, `ErrSetupConfigFailed`, `ErrSetupUnsupportedOS`)
- `PlatformError` struct with `Code`, `Message`, `Suggestion` + `Error()` method
- `NewPlatformError(code, message, suggestion)` constructor
- `MapNetworkError(err)` — needed by logfetcher.go (port from `errors.go:65-93`)

**Tests**: `internal/platform/errors_test.go`
- `TestPlatformError_Error` — string formatting (3 cases)
- `TestPlatformError_Is` — `errors.As` integration (2 cases)
- `TestMapNetworkError_Detection` — `*net.OpError`, `*net.DNSError`, "connection refused", `context.DeadlineExceeded`, nil, unknown (6 cases)
- `TestErrorCodes_AllDefined` — all constants unique, non-empty

**Acceptance**: `go test ./internal/platform/... -run TestPlatformError -v` passes.

**Dependencies**: Task 1 (same package).

---

### Task 3: platform/client.go

**PRD**: §4.1 | **Analysis**: `analysis/platform.md` §3

**Create**: `internal/platform/client.go` (~50 lines)

**Implement**:
- `Client` interface — 26 methods (exact signatures in analysis §3)
- `LogFetcher` interface — `FetchLogs(ctx, *LogAccess, LogFetchParams) ([]LogEntry, error)`

**Tests**: None — pure interface definition.

**Acceptance**: `go build ./internal/platform/...` compiles.

**Dependencies**: Task 1 (uses types from same package).

---

### Task 4: platform/mock.go

**PRD**: §4.1 | **Analysis**: `analysis/platform.md` §4

**Create**: `internal/platform/mock.go` (~460 lines)

**Implement** (port from `../zaia/internal/platform/mock.go:1-457`):
- `Mock` struct with `sync.RWMutex` + all data fields
- 14 builder methods: `NewMock()`, `WithUserInfo()`, ..., `WithError(method, err)`
- 26 Client interface method implementations (check `getError()` first, return data)
- `MockLogFetcher` struct + `NewMockLogFetcher()` + `WithEntries()` + `WithError()` + `FetchLogs()`
- Compile-time checks: `var _ Client = (*Mock)(nil)`, `var _ LogFetcher = (*MockLogFetcher)(nil)`

**Fix**: `CreateProjectEnv` mock signature must match interface (source `mock.go:299` has wrong param count).

**Tests**: `internal/platform/mock_test.go`
- `TestMock_ImplementsClient` — compile-time + runtime interface check
- `TestMock_FluentAPI` — builder chain works
- `TestMock_ErrorOverride` — `WithError` overrides return
- `TestMock_ProcessLifecycle` — get → cancel flow, status mutation
- `TestMock_ServiceManagement` — start/stop/restart return Process
- `TestMock_SetAutoscaling_ReturnsNil` — nil process (sync operation)
- `TestMock_EnvVars` — service + project env separation
- `TestMock_GetServiceByID` — found/not found fallback to list
- `TestMock_ThreadSafety` — concurrent reads + writes (10 goroutines)
- `TestMock_WithLogAccess` — MockLogFetcher returns configured entries

**Acceptance**: `go test ./internal/platform/... -run TestMock -v` passes.

**Dependencies**: Tasks 1–3 (types, errors, client interface).

---

### Task 5: platform/apitest/harness.go

**PRD**: §11.4 | **Analysis**: `analysis/platform.md` §5

**Create**: `internal/platform/apitest/harness.go` (~80 lines) + `cleanup.go` (~40 lines)

**Build tag**: `//go:build api`

**Implement**:
- `APIHarness` struct: `t *testing.T`, real `ZeropsClient`, `projectID`, context with timeout, cleanups
- `New(t)` — reads `ZCP_API_KEY` (skip if empty), reads `ZCP_API_HOST` (default), creates client, discovers project
- `Client()`, `Ctx()`, `ProjectID()`, `Cleanup(fn)` methods
- `cleanup.go`: `DeleteService(ctx, client, serviceID)`, `WaitForProcess(ctx, client, processID, timeout)` — fresh context for cleanup

**Tests**: `internal/platform/apitest/harness_test.go` (`//go:build api`)
- `TestHarness_SkipsWithoutKey`
- `TestHarness_ClientCreation`
- `TestHarness_CtxTimeout`
- `TestHarness_ProjectID`
- `TestHarness_Cleanup`

**Acceptance**: `go test ./internal/platform/apitest/... -tags api -v` passes (or skips if no key).

**Dependencies**: Tasks 1–3 (needs types + client interface). Task 6 (needs ZeropsClient) — can stub initially, finalize after Task 6.

**Note**: This is test infrastructure. Create early even if partially stubbed, because all subsequent API tests depend on it.

---

### Task 6: platform/zerops.go

**PRD**: §4.3 | **Analysis**: `analysis/platform.md` §6

**Create**: `internal/platform/zerops.go` (~900 lines)

**Implement** (port from `../zaia/internal/platform/zerops.go:1-1025`):
- `ZeropsClient` struct with `handler`, `apiHost`, `once sync.Once`, `cachedID`, `idErr`
- `NewZeropsClient(token, apiHost)` constructor — URL normalization, SDK setup
- `getClientID(ctx)` — **sync.Once** (fix for source race at `zerops.go:57-67`)
- All 26 Client interface methods (see analysis §6 for SDK call mapping table)
- Mapping helpers: `mapProcess` (**must map FailReason** — source omits it), `mapEsServiceStack`, `mapFullServiceStack`, `mapOutputCustomAutoscaling`, `buildAutoscalingBody`, `mapEsProcessEvent`, `mapEsAppVersionEvent`
- `mapSDKError(err, entityType)` + `mapAPIError(apiErr, entityType)` — error classification

**ZCP fixes over source**:
1. `sync.Once` for `clientID` (PRD §4.3)
2. `mapProcess` must map `FailReason` (PRD §5.4)
3. Suggestion text: "Check token validity" not "Run: zaia login" (`zerops.go:995`)
4. Investigate `Ports` mapping in service stack (source never maps it)

**Tests**: `internal/platform/zerops_test.go` (unit, no tag)
- `TestNewZeropsClient` — URL normalization (3 cases)
- `TestNewZeropsClient_EmptyToken` / `_EmptyHost` — error cases
- `TestMapProcess_StatusNormalization` — DONE→FINISHED, CANCELLED→CANCELED (4 cases)
- `TestMapProcess_FailReason` — mapped when present, nil when absent
- `TestMapProcess_OptionalFields` — Started/Finished nil vs set
- `TestBuildAutoscalingBody_MinimalParams` / `_AllParams` / `_EmptyParams` (3 cases)
- `TestMapSDKError_NetworkErrors` — net.OpError, context.DeadlineExceeded, etc. (6 cases)
- `TestMapAPIError_StatusCodes` — 401/403/404/429/500 (7 cases)
- `TestMapAPIError_SubdomainIdempotent` — SUBDOMAIN_ALREADY_ENABLED/DISABLED

**Tests**: `internal/platform/zerops_api_test.go` (`//go:build api`)
- `TestAPI_GetUserInfo` — response non-nil, ID+Email populated
- `TestAPI_ListProjects` — >=1 project, fields populated
- `TestAPI_GetProject` — fields match ListProjects
- `TestAPI_ListServices` — services for project, fields populated
- `TestAPI_GetService` — single service, all fields
- `TestAPI_GetServiceEnv` — returns slice (may be empty, not nil)
- `TestAPI_GetProjectEnv` — project-level env vars
- `TestAPI_GetProjectLog` — LogAccess with URL + token
- `TestAPI_SearchProcesses` — events, normalized status
- `TestAPI_SearchAppVersions` — events with fields
- `TestAPI_GetProcess_NotFound` — PROCESS_NOT_FOUND error
- `TestAPI_GetService_NotFound` — SERVICE_NOT_FOUND error
- `TestAPI_InvalidToken` — AUTH_TOKEN_EXPIRED error

**Acceptance**: Both test files pass: `go test ./internal/platform/... -tags api -v`

**Dependencies**: Tasks 1–4 (types, errors, client, mock). Task 5 (apitest harness for API tests).

---

### Task 7: platform/logfetcher.go

**PRD**: §4.5 | **Analysis**: `analysis/platform.md` §7

**Create**: `internal/platform/logfetcher.go` (~160 lines)

**Implement** (port from `../zaia/internal/platform/logfetcher.go:1-157`):
- `ZeropsLogFetcher` struct + `NewLogFetcher()` constructor
- `FetchLogs(ctx, *LogAccess, LogFetchParams)` — URL parsing, query params, HTTP GET, JSON parse, sort, limit
- Internal types: `logAPIResponse`, `logAPIItem`
- Constants: `maxLogResponseBytes`, `maxErrorResponseBytes`
- Compile-time check: `var _ LogFetcher = (*ZeropsLogFetcher)(nil)`

**Tests**: `internal/platform/logfetcher_test.go` (unit — uses httptest)
- `TestLogFetcher_FetchLogs_Success` — 2 entries sorted chronologically
- `TestLogFetcher_FetchLogs_QueryParams` — all params sent
- `TestLogFetcher_FetchLogs_ServerError` — 500 → error
- `TestLogFetcher_FetchLogs_NilAccess` — nil → error
- `TestLogFetcher_FetchLogs_URLPrefix` — "GET " prefix stripped
- `TestLogFetcher_FetchLogs_LimitApplied` — tail behavior
- `TestParseLogResponse` / `_InvalidJSON` / `_EmptyItems`

**Tests**: `internal/platform/logfetcher_api_test.go` (`//go:build api`)
- `TestAPI_LogFetcher_FetchLogs` — real log fetch chain
- `TestAPI_LogFetcher_SeverityFilter` — severity filter works

**Acceptance**: `go test ./internal/platform/... -tags api -run TestLogFetcher -v` passes.

**Dependencies**: Tasks 1–3 (types, errors, client). Task 5 (apitest harness). Task 6 (needs real client for API tests).

---

### Task 8: auth/auth.go

**PRD**: §3.1–3.3 | **Analysis**: `analysis/platform.md` §8

**Create**: `internal/auth/auth.go` (~150 lines)

**Implement** (new — not a port):
- `Info` struct: `Token`, `APIHost`, `Region`, `ClientID`, `ProjectID`, `ProjectName`
- `Resolve(ctx, client platform.Client) (*Info, error)` — main auth flow:
  1. Check `ZCP_API_KEY` env var (primary)
  2. zcli fallback: read `cli.data` (macOS: `~/Library/Application Support/zerops/cli.data`, Linux: `~/.config/zerops/cli.data`)
  3. `ZCP_API_HOST` and `ZCP_REGION` env var overrides
  4. Validate: `client.GetUserInfo(ctx)` → get clientID
  5. Project discovery: scoped or list (0=error, 2+=error, 1=use)
- Internal types: `cliData`, `cliRegion` (JSON parsing)
- Platform-specific path: `os.UserConfigDir()` or `runtime.GOOS` check

**Tests**: `internal/auth/auth_test.go` (unit — uses platform.Mock + temp files)
- `TestResolve_EnvVar_SingleProject` — happy path
- `TestResolve_EnvVar_NoProject` — TOKEN_NO_PROJECT
- `TestResolve_EnvVar_MultiProject` — TOKEN_MULTI_PROJECT
- `TestResolve_EnvVar_InvalidToken` — auth error
- `TestResolve_EnvVar_CustomAPIHost` / `_CustomRegion`
- `TestResolve_ZcliFallback_Success` — reads cli.data
- `TestResolve_ZcliFallback_ScopeProject` — ScopeProjectId set
- `TestResolve_ZcliFallback_NullScope` — ScopeProjectId null → ListProjects
- `TestResolve_NoAuth` — AUTH_REQUIRED

**Tests**: `internal/auth/auth_api_test.go` (`//go:build api`)
- `TestAPI_Resolve_FullFlow` — real token → Info populated
- `TestAPI_Resolve_InvalidToken` — fake token → auth error

**Acceptance**: `go test ./internal/auth/... -tags api -v` passes.

**Dependencies**: Tasks 1–4 (platform types + mock for unit tests). Task 6 (real client for API tests).

---

### Task 9: knowledge/ package

**PRD**: §7 | **Analysis**: `analysis/testing.md` §Phase 1 (knowledge tests)

**Create**: `internal/knowledge/` — port from `../zaia/internal/knowledge/`
- `documents.go` — Document type, `go:embed`, frontmatter parsing
- `query.go` — query expansion, suggestions, snippets
- `engine.go` — Store, `Search()`, `List()`, `Get()`, `sync.Once` init
- `embed/` directory — 65+ markdown files (copy from `../zaia/internal/knowledge/embed/`)

**Fix**: `GetEmbeddedStore()` must use `sync.Once` (source uses racy nil check).

**Tests**: `internal/knowledge/engine_test.go` (31 test functions — port from source)
- Search quality tests: postgresql, redis/valkey, nodejs, mysql/mariadb, elasticsearch, S3, zerops-yml, import-yml, env-variables, scaling
- Query expansion: postgres→postgresql, redis→valkey, mysql→mariadb
- Path/URI conversion, document parsing, snippet extraction, suggestion generation
- `TestHitRate` — 8 queries with hit@1/hit@3 tracking

**Acceptance**: `go test ./internal/knowledge/... -v` passes. Document count ≥ 60.

**Dependencies**: None (standalone package, no internal deps).

**Note**: Can be developed in parallel with Tasks 1–8.

---

### Phase 1 Gate

```bash
# Mock tests:
go test ./internal/platform/... ./internal/auth/... ./internal/knowledge/... -count=1 -short -v

# API contract tests (requires ZCP_API_KEY):
go test ./internal/platform/... ./internal/auth/... -tags api -v
```

**Must pass before Phase 2**: All 26 Client methods verified against real API. LogFetcher chain works. Auth flow produces valid Info. FailReason mapped. sync.Once used for clientID.

---

## Phase 2: Business Logic (Units 10–23)

### Task 10: ops/helpers.go

**PRD**: §5.1 | **Analysis**: `analysis/ops.md` §helpers

**Create**: `internal/ops/helpers.go`

**Implement**:
- `resolveServiceID(services []platform.ServiceStack, projectID, hostname string) (*platform.ServiceStack, error)`
- `findServiceByHostname(services, hostname) *platform.ServiceStack`
- `listHostnames(services) string` — for error messages
- `parseSince(s string) (time.Time, error)` — "30m", "1h", "24h", "7d", ISO 8601, empty=1h default
- `parseEnvPairs(vars []string) ([]envPair, error)` — split on first `=`
- `findEnvIDByKey(envs []platform.EnvVar, key string) string`

**Tests**: `internal/ops/helpers_test.go`
- `TestResolveServiceID_Found` / `_NotFound` / `_EmptyList` (3 cases)
- `TestParseSince_Minutes` / `_Hours` / `_Days` / `_ISO8601` / `_Empty` / `_Invalid` / `_OutOfRange` (7 cases)
- `TestParseEnvPairs_Valid` / `_NoEquals` / `_EmptyKey` (3 cases)

**Tests**: `internal/ops/helpers_api_test.go` (`//go:build api`)
- `TestAPI_ResolveServiceID_RealService`

**Acceptance**: `go test ./internal/ops/... -run TestResolve -v` and `go test ./internal/ops/... -run TestParse -v` pass.

**Dependencies**: Phase 1 complete (platform types, mock).

---

### Task 11: ops/discover.go

**PRD**: §5.1 (zerops_discover) | **Analysis**: `analysis/ops.md` §discover

**Create**: `internal/ops/discover.go`

**Implement**:
- Result types: `DiscoverResult`, `ProjectInfo`, `ServiceInfo`
- `Discover(ctx, client, projectID, hostname, includeEnvs) (*DiscoverResult, error)`
- Logic: GetProject → ListServices → optional filter → optional GetServiceEnv per service

**Tests**: `internal/ops/discover_test.go`
- `TestDiscover_AllServices_Success` — 3 services returned
- `TestDiscover_SingleService_Found` / `_NotFound`
- `TestDiscover_WithEnvs_Success` — envs populated
- `TestDiscover_EnvFetchError_Graceful` — env error silently ignored
- `TestDiscover_ProjectNotFound` — error propagated

**Tests**: `internal/ops/discover_api_test.go` (`//go:build api`)
- `TestAPI_Discover_AllServices` / `_WithService` / `_WithEnvs`

**Acceptance**: `go test ./internal/ops/... -tags api -run TestDiscover -v` passes.

**Dependencies**: Task 10 (helpers).

---

### Task 12: ops/manage.go

**PRD**: §5.1 (zerops_manage) | **Analysis**: `analysis/ops.md` §manage

**Create**: `internal/ops/manage.go`

**Implement**:
- `ScaleParams` struct + `ScaleResult` struct
- `Start(ctx, client, projectID, hostname) (*platform.Process, error)`
- `Stop(ctx, client, projectID, hostname) (*platform.Process, error)`
- `Restart(ctx, client, projectID, hostname) (*platform.Process, error)`
- `Scale(ctx, client, projectID, hostname, params ScaleParams) (*ScaleResult, error)`
- Validation: at least one scale param non-zero, min ≤ max, CPUMode SHARED|DEDICATED

**Tests**: `internal/ops/manage_test.go`
- `TestStart_Success` / `TestStop_Success` / `TestRestart_Success`
- `TestStart_ServiceNotFound`
- `TestScale_AllParams` / `_NilProcess` / `_NoParams` / `_MinGtMax` / `_InvalidCPUMode`

**Tests**: `internal/ops/manage_api_test.go` (`//go:build api`)
- `TestAPI_Manage_Scale_ReadOnly` — verify param mapping (non-mutating)

**Acceptance**: `go test ./internal/ops/... -tags api -run TestManage -v` passes.

**Dependencies**: Task 10 (helpers).

---

### Task 13: ops/env.go

**PRD**: §5.1 (zerops_env) | **Analysis**: `analysis/ops.md` §env

**Create**: `internal/ops/env.go`

**Implement**:
- Result types: `EnvGetResult`, `EnvSetResult`, `EnvDeleteResult`
- `EnvGet(ctx, client, projectID, hostname, isProject) (*EnvGetResult, error)`
- `EnvSet(ctx, client, projectID, hostname, isProject, variables) (*EnvSetResult, error)`
- `EnvDelete(ctx, client, projectID, hostname, isProject, variables) (*EnvDeleteResult, error)`
- Service set: join pairs as `.env` format → `SetServiceEnvFile`
- Project set: `CreateProjectEnv` per pair
- Delete: find ID by key → `DeleteUserData` (service) or `DeleteProjectEnv` (project)

**Tests**: `internal/ops/env_test.go`
- `TestEnvGet_Service` / `_Project` / `_NoScope`
- `TestEnvSet_Service` / `_Project` / `_InvalidFormat`
- `TestEnvDelete_Service_Found` / `_NotFound` / `_Project`

**Tests**: `internal/ops/env_api_test.go` (`//go:build api`)
- `TestAPI_EnvGet_Service` / `_Project` (read-only)

**Acceptance**: `go test ./internal/ops/... -tags api -run TestEnv -v` passes.

**Dependencies**: Task 10 (helpers — parseEnvPairs, findEnvIDByKey).

---

### Task 14: ops/logs.go

**PRD**: §5.1 (zerops_logs) | **Analysis**: `analysis/ops.md` §logs

**Create**: `internal/ops/logs.go`

**Implement**:
- Result types: `LogsResult`, `LogEntryOutput`
- `FetchLogs(ctx, client, fetcher, projectID, hostname, severity, since, limit, search) (*LogsResult, error)`
- 2-step: resolve service → GetProjectLog → LogFetcher.FetchLogs
- Default limit: 100. `hasMore` = len(entries) >= limit

**Tests**: `internal/ops/logs_test.go`
- `TestFetchLogs_Success` / `_ServiceNotFound` / `_EmptyResult` / `_HasMore` / `_InvalidSince` / `_DefaultLimit`

**Tests**: `internal/ops/logs_api_test.go` (`//go:build api`)
- `TestAPI_FetchLogs` / `_SeverityFilter`

**Acceptance**: `go test ./internal/ops/... -tags api -run TestFetchLogs -v` passes.

**Dependencies**: Task 10 (helpers — parseSince, resolveServiceID).

---

### Task 15: ops/import.go

**PRD**: §5.1 (zerops_import) | **Analysis**: `analysis/ops.md` §import

**Create**: `internal/ops/import.go`

**Implement**:
- Result types: `ImportDryRunResult`, `ImportRealResult`, `ImportProcessOutput`
- `Import(ctx, client, projectID, content, filePath, dryRun) (interface{}, error)`
- Input: content XOR filePath. filePath → os.ReadFile.
- Check for `project:` key → IMPORT_HAS_PROJECT
- Dry run: parse YAML, extract services, return preview
- Real: `client.ImportServices` → extract processes

**Tests**: `internal/ops/import_test.go`
- `TestImport_DryRun_Valid` / `_InvalidYAML` / `_MissingServices` / `_HasProjectSection`
- `TestImport_Real_Success` / `_NoInput` / `_BothInputs` / `_FileNotFound`

**Tests**: `internal/ops/import_api_test.go` (`//go:build api`)
- `TestAPI_Import_DryRun` — safe, no resources created

**Acceptance**: `go test ./internal/ops/... -tags api -run TestImport -v` passes.

**Dependencies**: Task 10 (helpers). External: `yaml.v3`.

---

### Task 16: ops/validate.go

**PRD**: §5.1 (zerops_validate) | **Analysis**: `analysis/ops.md` §validate

**Create**: `internal/ops/validate.go`

**Implement**:
- Result types: `ValidateResult`, `ValidationError`
- `Validate(content, filePath, fileType string) (*ValidateResult, error)`
- Offline — no platform.Client needed
- zerops.yml: `zerops` key must exist, must be array, non-empty
- import.yml: `services` key required, no `project:` key
- Type auto-detection from filename or YAML keys

**Tests**: `internal/ops/validate_test.go`
- `TestValidate_ZeropsYml_Valid` / `_MissingKey` / `_EmptyArray` / `_BadSyntax`
- `TestValidate_ImportYml_Valid` / `_HasProject` / `_MissingServices`
- `TestValidate_AutoDetect_Import` / `_Zerops`
- `TestValidate_FileRead` / `_FileNotFound`

**Acceptance**: `go test ./internal/ops/... -run TestValidate -v` passes. No API test needed.

**Dependencies**: External: `yaml.v3`. No internal deps beyond platform error types.

---

### Task 17: ops/delete.go

**PRD**: §5.1 (zerops_delete) | **Analysis**: `analysis/ops.md` §delete

**Create**: `internal/ops/delete.go`

**Implement**:
- `Delete(ctx, client, projectID, hostname string, confirm bool) (*platform.Process, error)`
- Safety gate: `!confirm` → CONFIRM_REQUIRED
- Resolve hostname → DeleteService

**Tests**: `internal/ops/delete_test.go`
- `TestDelete_Success` / `_NoConfirm` / `_ServiceNotFound` / `_EmptyHostname`

**Acceptance**: `go test ./internal/ops/... -run TestDelete -v` passes. Real delete tested in E2E only.

**Dependencies**: Task 10 (helpers).

---

### Task 18: ops/subdomain.go

**PRD**: §5.1 (zerops_subdomain) | **Analysis**: `analysis/ops.md` §subdomain

**Create**: `internal/ops/subdomain.go`

**Implement**:
- `SubdomainResult` struct
- `Subdomain(ctx, client, projectID, hostname, action string) (*SubdomainResult, error)`
- Idempotent: catch `SUBDOMAIN_ALREADY_ENABLED/DISABLED` → return success with status

**Tests**: `internal/ops/subdomain_test.go`
- `TestSubdomain_Enable_Success` / `_Disable_Success`
- `TestSubdomain_Enable_AlreadyEnabled` / `_Disable_AlreadyDisabled` — idempotent handling
- `TestSubdomain_InvalidAction` / `_ServiceNotFound`

**Tests**: `internal/ops/subdomain_api_test.go` (`//go:build api`)
- `TestAPI_Subdomain_EnableDisable` — enable + disable cycle

**Acceptance**: `go test ./internal/ops/... -tags api -run TestSubdomain -v` passes.

**Dependencies**: Task 10 (helpers).

---

### Task 19: ops/events.go

**PRD**: §5.1 (zerops_events) | **Analysis**: `analysis/ops.md` §events

**Create**: `internal/ops/events.go`

**Implement**:
- Result types: `EventsResult`, `TimelineEvent`, `EventsSummary`
- `Events(ctx, client, projectID, serviceHostname string, limit int) (*EventsResult, error)`
- Parallel fetch: SearchProcesses + SearchAppVersions + ListServices
- Build serviceID→hostname map
- Action name normalization map (serviceStackStart→start, etc.)
- `calcDuration(started, finished)` helper
- Sort by timestamp descending, trim to limit

**Tests**: `internal/ops/events_test.go`
- `TestEvents_MergedTimeline` — 5 events sorted desc
- `TestEvents_FilterByService` / `_LimitApplied` / `_EmptyResult`
- `TestEvents_ActionNameMapping` / `_DurationCalculation`
- `TestEvents_ParallelFetchError` — error propagated

**Tests**: `internal/ops/events_api_test.go` (`//go:build api`)
- `TestAPI_Events_Timeline` / `_WithService`

**Acceptance**: `go test ./internal/ops/... -tags api -run TestEvents -v` passes.

**Dependencies**: Task 10 (helpers).

---

### Task 20: ops/process.go

**PRD**: §5.1 (zerops_process) | **Analysis**: `analysis/ops.md` §process

**Create**: `internal/ops/process.go`

**Implement**:
- `ProcessStatusResult` struct (includes FailReason)
- `ProcessCancelResult` struct
- `GetProcessStatus(ctx, client, processID) (*ProcessStatusResult, error)`
- `CancelProcess(ctx, client, processID) (*ProcessCancelResult, error)`
- Cancel: check terminal state first → PROCESS_ALREADY_TERMINAL

**Tests**: `internal/ops/process_test.go`
- `TestGetProcessStatus_Success` / `_Failed_WithReason` / `_NotFound`
- `TestCancelProcess_Success` / `_AlreadyTerminal` / `_NotFound`

**Tests**: `internal/ops/process_api_test.go` (`//go:build api`)
- `TestAPI_GetProcessStatus` / `_NotFound`

**Acceptance**: `go test ./internal/ops/... -tags api -run TestProcess -v` passes.

**Dependencies**: Phase 1 complete (platform types, Process.FailReason mapping).

---

### Task 21: ops/context.go

**PRD**: §5.2 | **Analysis**: `analysis/ops.md` §context

**Create**: `internal/ops/context.go`

**Implement**:
- `GetContext() string` — returns static precompiled string (~800-1200 tokens)
- Content: What is Zerops, How it Works, Critical Rules, Configuration, Service Types, Defaults, Pointers

**Tests**: `internal/ops/context_test.go`
- `TestGetContext_NonEmpty` — result non-empty
- `TestGetContext_ContainsCriticalSections` — contains "Critical Rules", "Service Types"
- `TestGetContext_TokenSize` — within expected range

**Acceptance**: `go test ./internal/ops/... -run TestContext -v` passes. No API test — static content.

**Dependencies**: None (pure static content).

---

### Task 22: content/ package

**PRD**: §9.4 | **Analysis**: `analysis/ops.md` §content

**Create**: `internal/content/`
- `content.go` — `go:embed` declarations + accessor functions
- `workflows/` — 6 markdown files (bootstrap, deploy, debug, scale, configure, monitor)
- `templates/` — CLAUDE.md template, MCP config template, SSH config template

**Implement**:
- `GetWorkflow(name string) (string, error)` — reads from embedded FS
- `GetTemplate(name string) (string, error)` — reads from embedded FS
- `ListWorkflows() []string` — lists available workflow names

**Tests**: Minimal — accessor tests in ops/workflow_test.go.

**Acceptance**: `go build ./internal/content/...` compiles.

**Dependencies**: None (stdlib only).

**Note**: Can be developed in parallel with Task 21. Shared between ops/workflow.go and init/templates.go.

---

### Task 23: ops/workflow.go

**PRD**: §5.3 | **Analysis**: `analysis/ops.md` §workflow

**Create**: `internal/ops/workflow.go`

**Implement**:
- `GetWorkflowCatalog() string` — static catalog listing
- `GetWorkflow(workflowName string) (string, error)` — reads from content package

**Tests**: `internal/ops/workflow_test.go`
- `TestGetWorkflowCatalog_NonEmpty`
- `TestGetWorkflow_Bootstrap` / `_Deploy` — non-empty content
- `TestGetWorkflow_Unknown` — error with available list

**Acceptance**: `go test ./internal/ops/... -run TestWorkflow -v` passes. No API test — static content.

**Dependencies**: Task 22 (content package).

---

### Phase 2 Gate

```bash
# Mock tests:
go test ./internal/ops/... -count=1 -short -v

# API contract tests:
go test ./internal/ops/... -tags api -v
```

**Must pass before Phase 3**: All ops functions verified with real API responses. Filtering, merging, formatting produce correct results from real data.

---

## Phase 3: MCP Layer (Units 24–27)

### Task 24: tools/ — All Tool Handlers

**PRD**: §5 | **Analysis**: `analysis/mcp.md`

**Create** (in order — convert.go first, then any order):

**24a. `internal/tools/convert.go`** (~50 lines)
- `convertError(err) *mcp.CallToolResult` — PlatformError → MCP error JSON
- `jsonResult(v any) *mcp.CallToolResult` — success → JSON content
- `textResult(text string) *mcp.CallToolResult` — plain text content
- `boolPtr(b bool) *bool` — for ToolAnnotations

**Tests**: `internal/tools/convert_test.go`
- `TestPlatformErrorToMCPResult` / `_WithSuggestion` / `TestSuccessResult_Sync` / `_Async` / `_EmptyData`

**24b–24o. One handler per tool** — each ~20-30 lines, same pattern:
```go
func RegisterXxx(srv *mcp.Server, /* deps */) {
    mcp.AddTool(srv, &mcp.Tool{Name: "zerops_xxx", ...}, handler)
}
```

| # | File | Tool | Input struct | Annotations | Async |
|---|------|------|-------------|-------------|-------|
| 24b | `context.go` | `zerops_context` | `ContextInput{}` (no params) | readOnly, idempotent | no |
| 24c | `workflow.go` | `zerops_workflow` | `WorkflowInput{Workflow}` | readOnly, idempotent | no |
| 24d | `discover.go` | `zerops_discover` | `DiscoverInput{Service, IncludeEnvs}` | readOnly, idempotent | no |
| 24e | `knowledge.go` | `zerops_knowledge` | `KnowledgeInput{Query, Limit}` | readOnly, idempotent | no |
| 24f | `validate.go` | `zerops_validate` | `ValidateInput{Content, FilePath, Type}` | readOnly, idempotent | no |
| 24g | `logs.go` | `zerops_logs` | `LogsInput{ServiceHostname, Severity, Since, Limit, Search}` | readOnly, idempotent | no |
| 24h | `events.go` | `zerops_events` | `EventsInput{ServiceHostname, Limit}` | readOnly, idempotent | no |
| 24i | `process.go` | `zerops_process` | `ProcessInput{ProcessID, Action}` | readOnly, idempotent | no |
| 24j | `env.go` | `zerops_env` | `EnvInput{Action, ServiceHostname, Project, Variables}` | — | mixed |
| 24k | `manage.go` | `zerops_manage` | `ManageInput{Action, ServiceHostname, ...scale params}` | destructive | yes |
| 24l | `import.go` | `zerops_import` | `ImportInput{Content, FilePath, DryRun}` | — | mixed |
| 24m | `delete.go` | `zerops_delete` | `DeleteInput{ServiceHostname, Confirm}` | destructive | yes |
| 24n | `subdomain.go` | `zerops_subdomain` | `SubdomainInput{ServiceHostname, Action}` | idempotent | yes |
| 24o | `deploy.go` | `zerops_deploy` | `DeployInput{SourceService, TargetService, Setup, WorkingDir}` | — | yes |

**Async pattern** (manage, delete, subdomain, env set/delete, import real): Check ProgressToken → wrap `req.Session.NotifyProgress` into callback → call `ops.PollProcess` (from Task 28).

**Tests per handler**: Unit (in-memory MCP + mock) + API (`//go:build api`, in-memory MCP + real client). See `analysis/testing.md` Phase 3 for full test inventory per tool.

**Tests**: `internal/tools/annotations_test.go`
- `TestAnnotations_AllToolsHaveTitleAndAnnotations` — 14 subtests, one per tool

**Acceptance**: `go test ./internal/tools/... -tags api -v` — all tools pass.

**Dependencies**: Phase 2 complete (all ops functions). Task 9 (knowledge for zerops_knowledge).

---

### Task 25: server/server.go

**PRD**: §9.1 | **Analysis**: `analysis/mcp.md` §server

**Create**: `internal/server/server.go`

**Implement**:
- `Server` struct: wraps `*mcp.Server` + deps
- `New(client, authInfo, store) *Server` — creates MCP server, registers tools + resources
- `registerTools()` — calls all `tools.RegisterXxx()` functions
- `registerResources()` — registers `zerops://docs/{+path}` via knowledge package
- `Run(ctx) error` — `server.Run(ctx, &mcp.StdioTransport{})`
- `Server() *mcp.Server` — accessor for testing

**Tests**: `internal/server/server_test.go`
- `TestServer_AllToolsRegistered` — all 14 tools present
- `TestServer_Instructions` — ~40-50 tokens, mentions zerops_context
- `TestServer_Connect` — in-memory transport succeeds

**Acceptance**: `go test ./internal/server/... -v` passes.

**Dependencies**: Task 24 (all tool handlers). Task 26 (instructions).

---

### Task 26: server/instructions.go

**PRD**: §2.1 | **Analysis**: `analysis/mcp.md` §instructions

**Create**: `internal/server/instructions.go` (~5 lines)

**Implement**:
```go
const Instructions = `ZCP provides tools for managing Zerops PaaS infrastructure: services, deployment, configuration, and debugging. Call zerops_context to load platform knowledge when working with Zerops.`
```

**Tests**: Covered by Task 25 server tests.

**Acceptance**: Instructions constant is ~40-50 tokens.

**Dependencies**: None.

---

### Task 27: cmd/zcp/main.go

**PRD**: §9.4, §12 | **Analysis**: `analysis/mcp.md` §main

**Create/Update**: `cmd/zcp/main.go`

**Implement**:
- Init dispatch: `os.Args[1] == "init"` → `init.Run()`
- MCP server mode:
  1. `auth.Resolve(ctx, client)` → `Info`
  2. `platform.NewZeropsClient(token, apiHost)`
  3. `knowledge.GetStore()` (sync.Once)
  4. `server.New(client, authInfo, store)`
  5. Signal handling: `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`
  6. `server.Run(ctx)` — graceful shutdown on signal
- No Cobra, no flag parsing beyond `os.Args[1]`

**Tests**: Build verification only — `go build -o bin/zcp ./cmd/zcp`

**Acceptance**: Binary builds and starts with valid `ZCP_API_KEY`.

**Dependencies**: Tasks 25–26 (server). Task 8 (auth). Task 6 (platform client). Task 9 (knowledge).

---

### Phase 3 Gate

```bash
# Tool tests:
go test ./internal/tools/... ./internal/server/... -count=1 -short -v

# API contract tests (full MCP chain):
go test ./internal/tools/... -tags api -v

# Binary builds:
go build -o bin/zcp ./cmd/zcp
```

---

## Phase 4: Streaming + Deploy + E2E (Units 28–32)

### Task 28: ops/progress.go

**PRD**: §6.1 | **Analysis**: `analysis/ops.md` §progress

**Create**: `internal/ops/progress.go`

**Implement**:
- `ProgressCallback func(message string, progress, total float64)`
- `PollProcess(ctx, client, processID, onProgress) (*platform.Process, error)`
- Algorithm: 2s initial → 5s after 30s → timeout at 10min
- Terminal states: FINISHED, FAILED, CANCELED

**Tests**: `internal/ops/progress_test.go`
- `TestPollProcess_ImmediateFinish` — FINISHED first call
- `TestPollProcess_PollThenFinish` — PENDING → RUNNING → FINISHED
- `TestPollProcess_Failed` — FAILED returned
- `TestPollProcess_Timeout` — exceeds 10min
- `TestPollProcess_ContextCanceled`
- `TestPollProcess_CallbackCalled` / `_NilCallback`

**Tests**: `internal/ops/progress_api_test.go` (`//go:build api`)
- `TestAPI_PollProcess_RealOp` — trigger async op, poll to completion

**Acceptance**: `go test ./internal/ops/... -tags api -run TestPollProcess -v` passes.

**Dependencies**: Phase 1 (platform client). Should be implemented before async tool handlers use it.

**Note**: While listed in Phase 4 per PRD, async tool handlers (Task 24) need this. Implement early or stub the polling in tool handlers initially.

---

### Task 29: ops/deploy.go

**PRD**: §8 | **Analysis**: `analysis/ops.md` §deploy

**Create**: `internal/ops/deploy.go`

**Implement**:
- `DeployResult` struct
- `SSHDeployer` interface + `LocalDeployer` interface (for testing)
- `Deploy(ctx, client, projectID, sshDeployer, localDeployer, authInfo, sourceService, targetService, setup, workingDir) (*DeployResult, error)`
- Mode detection: sourceService → SSH mode, else → local mode
- SSH: build `zcli login + zcli push` command, execute via SSHDeployer
- Local: execute via LocalDeployer
- Default workingDir: `/var/www` (SSH mode)

**Tests**: `internal/ops/deploy_test.go`
- `TestDeploy_SSHMode_Success` / `_WithSetup` / `_DefaultWorkingDir` / `_WithRegion` / `_NoRegion`
- `TestDeploy_SSHMode_SourceNotFound` / `_TargetNotFound`
- `TestDeploy_LocalMode_Success` / `_NoAuth`
- `TestDeploy_NoParams` / `_ModeDetection`

**Acceptance**: `go test ./internal/ops/... -run TestDeploy -v` passes.

**Dependencies**: Task 8 (auth.Info for token injection). Task 10 (helpers).

---

### Task 30: tools/deploy.go (handler)

Already covered in Task 24o. Ensure it uses `ops.Deploy` + progress polling.

---

### Task 31: Integration Tests

**Create**: `integration/multi_tool_test.go`

**Implement** (mock-based, no build tag):
- `TestIntegration_DiscoverThenManage` — cross-tool data flow
- `TestIntegration_ImportThenDiscover` — import result matches discover
- `TestIntegration_EnvSetThenGet` — env round-trip
- `TestIntegration_DeleteWithConfirmGate` — safety gate works
- `TestIntegration_ProcessPolling` — status transitions
- `TestIntegration_ErrorPropagation` — auth error → MCP error format
- `TestIntegration_ContextThenWorkflow` — content tools return data

**Acceptance**: `go test ./integration/ -v` passes.

**Dependencies**: Tasks 24–27 (full MCP stack).

---

### Task 32: E2E Lifecycle Test

**PRD**: §11.7 | **Analysis**: `analysis/testing.md` §Phase 4

**Create**: `e2e/lifecycle_test.go` + `e2e/helpers_test.go` + `e2e/process_helpers_test.go`

**Build tag**: `//go:build e2e`

**19 sequential steps**:
1. `zerops_context` — static content loads
2. `zerops_discover` — auth works, services visible
3. `zerops_knowledge` — BM25 returns results
4. `zerops_validate` — offline YAML validation
5. `zerops_import (dry-run)` — no side effects
6. `zerops_import (real)` — creates 2 services
7. Poll processes — wait for import (120s max)
8. `zerops_discover` — both services exist
9. `zerops_env (set)` — set env var
10. `zerops_env (get)` — read back, verify
11. `zerops_manage (stop)` — stop managed service
12. `zerops_manage (start)` — start managed service
13. `zerops_subdomain (enable)` — may fail if undeployed (expected)
14. `zerops_subdomain (disable)`
15. `zerops_logs` — fetch logs (may be empty)
16. `zerops_events` — activity timeline
17. `zerops_workflow` — catalog + specific
18. `zerops_delete (both)` — delete + wait
19. `zerops_discover` — verify gone

**Cleanup**: `t.Cleanup()` always deletes, uses fresh context.
**Naming**: `zcp-test-{random}` suffix.
**Timeout**: `go test -timeout 600s`

**Acceptance**: `go test ./e2e/ -tags e2e -v -timeout 600s` passes.

**Dependencies**: All previous tasks complete. Full MCP stack + real API.

---

### Phase 4 Gate

```bash
go test ./e2e/ -tags e2e -v -timeout 600s
```

---

## Phase 5: Init Subcommand (Units 33–35)

### Task 33: init/init.go

**PRD**: §9.4 | **Analysis**: `analysis/mcp.md` §init

**Create**: `internal/init/init.go`

**Implement**:
- `Run()` — idempotent init orchestrator:
  1. Generate CLAUDE.md in working directory
  2. Configure MCP server in Claude Code settings
  3. Set up Claude Code hooks
  4. Configure SSH (~/.ssh/config)
- Log each step, report success/failure

**Acceptance**: `go build ./internal/init/...` compiles.

**Dependencies**: Task 22 (content package for templates).

---

### Task 34: init/templates.go

**PRD**: §9.4 | **Analysis**: `analysis/mcp.md` §init

**Create**: `internal/init/templates.go`

**Implement**:
- `generateCLAUDEMD()` — writes from content.CLAUDEMDTemplate
- `generateMCPConfig()` — writes to ~/.claude/settings.json
- `configureSSH()` — appends to ~/.ssh/config

**Tests**: `internal/init/init_test.go`
- Test idempotency: run twice, same output
- Test file generation: CLAUDE.md content correct

**Acceptance**: `go test ./internal/init/... -v` passes.

**Dependencies**: Task 22 (content package).

---

### Task 35: Update cmd/zcp/main.go

Already handled in Task 27 (init dispatch). Verify `zcp init` subcommand works.

**Acceptance**: `./bin/zcp init` generates expected files.

---

## Knowledge Preservation

### Key Decisions

| Decision | Rationale | PRD Ref |
|----------|-----------|---------|
| `sync.Once` for clientID | Source has race condition | §4.3 |
| Map FailReason in mapProcess | Source omits it, MCP needs it | §5.4 |
| Fix CreateProjectEnv mock sig | Source has wrong param count | Bug fix |
| ~40-50 token instructions | zerops_context replaces bloated init message | §2.1 |
| SSH deploy as primary mode | ZCP runs inside Zerops containers | §8 |
| Callback pattern for PollProcess | Keeps ops layer MCP-agnostic | §6.1 |
| No Cobra | Two modes only: MCP server + init | §10 |

### Gotchas

1. **json-iterator/go** version `v0.0.0-20171115153421-f7279a603ede` — unusual but stable (bleve transitive)
2. **CGO_ENABLED=0** is safe — bleve works pure Go, go-faiss is no-op
3. **Go 1.24.0** pinned — Go 1.25 has darwin Mach-O regression
4. **cli.data paths** differ by OS: macOS `~/Library/Application Support/zerops/`, Linux `~/.config/zerops/`
5. **go:embed directories** must exist with content before compilation
6. **SetAutoscaling returns nil Process** — sync operation, handle gracefully
7. **Subdomain idempotent errors** — SUBDOMAIN_ALREADY_ENABLED/DISABLED are success, not errors
8. **FailReason** — source `mapProcess` at `zerops.go:645-684` never populates it; ZCP must add extraction
9. **Suggestion text** — replace "Run: zaia login" with "Check token validity" in error mapping

### Source Divergences

See `plans/analysis/platform.md` §12 for complete divergence log.

---

## Test Summary

| Phase | Mock Tests | API Tests | E2E Steps | Total Functions |
|-------|-----------|-----------|-----------|-----------------|
| 1: Foundation | 82 | 22 | — | 104 |
| 2: Business Logic | 76 | 16 | — | 92 |
| 3: MCP Layer | 72 | 9 | — | 81 |
| 4: Integration+E2E | 7 | — | 19 | 8 |
| **Total** | **237** | **47** | **19** | **285** |

64 test files total (39 always-run, 22 api-tagged, 3 e2e-tagged).

---

## Quick Reference: Build Commands

```bash
# Per-file feedback:
go test ./internal/<pkg>/... -run TestName -v

# Phase gates:
go test ./internal/platform/... ./internal/auth/... -tags api -v       # Phase 1
go test ./internal/ops/... -tags api -v                                 # Phase 2
go test ./internal/tools/... -tags api -v                               # Phase 3
go test ./e2e/ -tags e2e -v -timeout 600s                              # Phase 4

# Full suite:
go test ./... -count=1 -short     # Fast (mock only)
go test ./... -count=1            # Full (mock only, with race: add -race)
go build -o bin/zcp ./cmd/zcp    # Binary
```
