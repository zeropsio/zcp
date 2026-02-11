# ZCP Testing Analysis

> Source: `design/zcp-prd.md` section 11 (Testing Strategy), source test files from `zaia/` and `zaia-mcp/`.

---

## Source Test Pattern Analysis

### Table-Driven Test Structure

All source tests use Go's standard table-driven pattern. Example from `zaia/internal/platform/zerops_test.go:12-33`:

```go
func TestNewZeropsClient(t *testing.T) {
    tests := []struct {
        name    string
        token   string
        apiHost string
    }{ ... }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) { ... })
    }
}
```

Key observations:
- **Struct fields**: `name` always first, then inputs, then `want*` expected outputs
- **No testify in platform tests**: Uses `t.Fatalf`/`t.Errorf` directly (stdlib only)
- **No `t.Parallel()`**: Source platform tests do NOT use parallel (internal package, global state)
- **Tool tests use external `_test` package**: `package tools_test` (black-box testing)
- **Knowledge tests use internal package**: `package knowledge` (white-box, tests unexported functions)

### Mock Setup Patterns

**zaia platform mock** (`zaia/internal/platform/mock_test.go:13-47`):
- Builder pattern: `NewMock().WithUserInfo(...).WithServices(...).WithError("Method", err)`
- Thread-safe with `sync.RWMutex`
- Compile-time interface check: `var _ Client = NewMock()`
- Error override per method name string

**zaia-mcp executor mock** (`zaia-mcp/internal/tools/helpers_test.go:11-20`):
- `executor.NewMockExecutor().WithZaiaResponse("cmd args", result).WithDefault(result)`
- Records calls in `mock.Calls` for assertion
- Supports `SyncResult()`, `AsyncResult()`, `ErrorResult()` factories

### Assertion Patterns

**Platform tests**: Raw stdlib assertions with explicit messages:
```go
if pe.Code != tt.wantCode {
    t.Errorf("code = %s, want %s", pe.Code, tt.wantCode)
}
```

**Tool tests**: Custom helpers in `helpers_test.go`:
- `assertArgs(t, got, want...)` — positional arg matching
- `assertContains(t, args, want)` — checks arg slice contains value
- `getTextContent(t, result)` — extracts text from MCP CallToolResult
- `callToolExpectError(t, srv, name, args)` — expects protocol-level error

### Test Naming Conventions

| Source | Pattern | Example |
|--------|---------|---------|
| Platform | `Test{Type}_{Scenario}` | `TestMapSDKError_NetworkErrors`, `TestLogFetcher_FetchLogs_Success` |
| Knowledge | `Test{Component}_{Scenario}` | `TestSearch_PostgreSQLConnectionString`, `TestExpandQuery` |
| Tools | `Test{Tool}_{Scenario}` | `TestDiscover_WithService`, `TestManage_Scale` |
| E2E | `TestE2E_FullLifecycle` (subtests: `01_discover`) | Sequential numbered subtests |

### In-Memory MCP Pattern (from zaia-mcp)

`zaia-mcp/internal/tools/helpers_test.go:23-46`:
```go
func callTool(t *testing.T, srv *mcp.Server, name string, args map[string]interface{}) *mcp.CallToolResult {
    t1, t2 := mcp.NewInMemoryTransports()
    srv.Connect(ctx, t1, nil)
    client := mcp.NewClient(...)
    session, _ := client.Connect(ctx, t2, nil)
    defer session.Close()
    result, _ := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
    return result
}
```

### What ZCP Should Adopt vs Change

| Adopt | Change |
|-------|--------|
| Table-driven test structure | Add `t.Parallel()` where safe (PRD specifies per-layer) |
| Builder-pattern mock with fluent API | Use `testify/require` + `testify/assert` (PRD examples use them) |
| In-memory MCP transport for tool tests | Add API contract tests alongside every mock test (new) |
| Custom test helpers (assertArgs, getTextContent) | Add `apitest.Harness` infrastructure (new) |
| External `_test` package for tool tests | Add build tag strategy: `api`, `e2e` (new) |
| Numbered sequential E2E subtests | Expand from 17 to 19 steps (PRD adds context + workflow) |

---

## Build Tag Strategy

| Tag | Scope | When |
|-----|-------|------|
| (none) | Mock/unit tests only | Always — `go test ./...` |
| `-short` | Skip long tests (`testing.Short()`) | Fast feedback: `go test ./... -short` |
| `-tags api` | Mock + API contract tests | Per-phase verification against real Zerops API |
| `-tags e2e` | Everything including lifecycle | Final Phase 4 gate — creates/destroys real resources |

**Environment for API/E2E:**
- `ZCP_API_KEY` — required (skips if not set, does not fail)
- `ZCP_API_HOST` — optional override (default: `api.app-prg1.zerops.io`)
- Target project must have at least one existing service (for read-only tests)

---

## Test Infrastructure to Build First

### 1. `internal/platform/apitest/harness.go`

Shared test infrastructure for ALL `-tags api` and `-tags e2e` tests. Must exist before any API contract tests.

**What it provides:**
- `apitest.New(t)` — creates harness, **skips** test if `ZCP_API_KEY` not set
- `h.Client()` — real `ZeropsClient` connected to Zerops API
- `h.Ctx()` — `context.Context` with 30s timeout
- `h.ProjectID()` — discovered project ID (same auth flow as production)
- `h.Cleanup(func())` — deferred resource deletion (uses fresh context, not test context)
- `h.MCPSession(srv)` — creates in-memory MCP client session connected to given server (for tools API tests)
- API call logging for debugging failed tests

**Why first:** Every API test in every package imports `apitest`. Without it, no API tests can be written. It's the foundation of the progressive verification strategy.

### 2. `internal/platform/apitest/cleanup.go`

Resource cleanup helpers:
- `DeleteService(ctx, client, serviceID)` — direct API delete
- `WaitForProcess(ctx, client, processID, timeout)` — poll until terminal
- Uses fresh `context.Background()` with own timeout (test context may be cancelled on failure)

### 3. Test Helpers (per package)

**Platform tests:** Stdlib assertions (following source pattern).

**Tool tests:** Shared helpers similar to source `helpers_test.go`:
- `testServer(t, registerFunc, mockClient)` — creates MCP server with one tool + mock
- `callTool(t, srv, name, args)` — in-memory MCP call
- `callToolExpectError(t, srv, name, args)` — expects protocol error
- `getTextContent(t, result)` — extract text from MCP result
- `assertArgs(t, got, want...)` — positional arg check

---

## Complete Test File Inventory

### Phase 1 Tests

#### `platform/types_test.go`

| Test Function | Table Cases | Validates |
|--------------|-------------|-----------|
| `TestServiceStack_Fields` | N/A (struct verification) | All ServiceStack fields are populated correctly from construction |
| `TestAutoScalingParams_Defaults` | empty, partial, full | Zero-value vs explicit params |
| `TestProcess_StatusNormalization` | `DONE→FINISHED`, `CANCELLED→CANCELED`, `RUNNING→RUNNING` | Status normalization works on type construction |
| `TestImportResult_ProcessIDs` | 0 processes, 1, multiple | ProcessIDs extraction from ImportResult |
| `TestEnvVar_Fields` | basic env var, sensitive, with ID | All EnvVar fields populated |

Estimated: **5 test functions, ~12 table cases**

#### `platform/errors_test.go`

Source reference: `zaia/internal/platform/errors_test.go:1-103`

| Test Function | Table Cases | Validates |
|--------------|-------------|-----------|
| `TestPlatformError_Error` | basic, with suggestion, without context | `PlatformError.Error()` string formatting |
| `TestPlatformError_Is` | same code, different code | `errors.Is` / `errors.As` integration |
| `TestMapSDKError_Nil` | N/A | nil input returns nil |
| `TestMapSDKError_NetworkErrors` | `net.OpError`, `net.DNSError`, `context.DeadlineExceeded`, `context.Canceled`, "connection refused" string, unknown | Network error classification → correct error code |
| `TestMapSDKError_APIError` | 401, 403, 404, 429, 500 | SDK `apiError.Error` → correct PlatformError code |
| `TestMapAPIError_HTTPStatusCodes` | 401→AUTH_TOKEN_EXPIRED, 403→PERMISSION_DENIED, 404 service→SERVICE_NOT_FOUND, 404 process→PROCESS_NOT_FOUND, 429→API_RATE_LIMITED, 500→API_ERROR, 503→API_ERROR | HTTP status → error code mapping |
| `TestMapAPIError_SubdomainIdempotency` | SubdomainAccessAlreadyEnabled, serviceStackSubdomainAccessAlreadyDisabled | Special error code strings → SUBDOMAIN_ALREADY_ENABLED / _DISABLED |
| `TestErrorCodes_AllDefined` | N/A | All error code constants are unique, non-empty |

Estimated: **8 test functions, ~30 table cases**

#### `platform/zerops_test.go` (unit, no build tag)

Source reference: `zaia/internal/platform/zerops_test.go:1-148`

| Test Function | Table Cases | Validates |
|--------------|-------------|-----------|
| `TestNewZeropsClient` | bare host, https host, trailing slash | Client creation with various API host formats |
| `TestNewZeropsClient_EmptyToken` | N/A | Error on empty token |
| `TestNewZeropsClient_EmptyHost` | N/A | Error on empty host |
| `TestZeropsClient_GetUserInfo_Error` | mock returns error | Error propagation |
| `TestZeropsClient_ListServices_Error` | mock returns error | Error propagation |
| `TestZeropsClient_StartService_Error` | mock returns error | Error propagation |
| `TestZeropsClient_SetAutoscaling_NilProcess` | success returns nil process | SetAutoscaling may return nil |
| `TestZeropsClient_StatusNormalization` | DONE→FINISHED, CANCELLED→CANCELED | Process status normalization |

Estimated: **8 test functions, ~15 table cases**

#### `platform/zerops_api_test.go` (`//go:build api`)

PRD reference: section 11.5 (Platform Contract Tests)

| Test Function | Validates |
|--------------|-----------|
| `TestAPI_GetUserInfo` | Response non-nil, ID populated, FullName populated |
| `TestAPI_ListProjects` | Non-empty list, each has ID + Name |
| `TestAPI_GetProject` | Fetches known project, fields populated |
| `TestAPI_ListServices` | Non-empty for test project, each has ID + Name + Status |
| `TestAPI_ListServices_ResponseShape` | Ports non-nil (may be empty slice), ServiceType fields populated |
| `TestAPI_GetService` | Single service by ID, all fields populated |
| `TestAPI_GetServiceEnv` | Returns env vars (may be empty slice, not nil) |
| `TestAPI_GetProjectEnv` | Returns project-level env vars |
| `TestAPI_GetProjectLog` | Returns LogAccess with URL and token |
| `TestAPI_SearchProcesses` | Returns process events (may be empty) |
| `TestAPI_SearchAppVersions` | Returns app version events (may be empty) |
| `TestAPI_GetProcess_NotFound` | Invalid ID → PROCESS_NOT_FOUND error |
| `TestAPI_GetService_NotFound` | Invalid ID → SERVICE_NOT_FOUND error |

Estimated: **13 test functions** (read-only, run in parallel)

#### `platform/mock_test.go`

Source reference: `zaia/internal/platform/mock_test.go:1-197`

| Test Function | Table Cases | Validates |
|--------------|-------------|-----------|
| `TestMock_ImplementsClient` | N/A | Compile-time + runtime interface check |
| `TestMock_FluentAPI` | N/A | Builder chain: WithUserInfo, WithProjects, WithServices |
| `TestMock_ErrorOverride` | N/A | WithError("MethodName", err) overrides return |
| `TestMock_ProcessLifecycle` | get → cancel | GetProcess returns data, CancelProcess sets CANCELLED |
| `TestMock_ProcessNotFound` | N/A | Nonexistent process → error |
| `TestMock_ServiceManagement` | start, stop, restart | Each returns Process with correct action + PENDING status |
| `TestMock_SetAutoscaling_ReturnsNil` | N/A | SetAutoscaling returns nil process (sync) |
| `TestMock_EnvVars` | service env, project env | WithServiceEnv + WithProjectEnv return correct data |
| `TestMock_GetServiceByID` | found, not found | GetService lookup by ID |
| `TestMock_ThreadSafety` | N/A | Concurrent reads + writes don't race (10 goroutines) |
| `TestMock_WithLogAccess` | N/A | MockLogFetcher returns configured entries |

Estimated: **11 test functions, ~12 table cases**

#### `platform/logfetcher_test.go`

Source reference: `zaia/internal/platform/logfetcher_test.go:1-265`

| Test Function | Table Cases | Validates |
|--------------|-------------|-----------|
| `TestLogFetcher_FetchLogs_Success` | N/A | 2 entries returned, sorted chronologically, fields populated |
| `TestLogFetcher_FetchLogs_QueryParams` | N/A | All query params (serviceStackId, severity, tail, search, since) sent |
| `TestLogFetcher_FetchLogs_ServerError` | N/A | 500 response → error |
| `TestLogFetcher_FetchLogs_NilAccess` | N/A | nil LogAccess → error |
| `TestLogFetcher_FetchLogs_URLPrefix` | N/A | "GET " prefix stripped from URL |
| `TestLogFetcher_FetchLogs_LimitApplied` | N/A | 5 items + limit 2 → 2 returned |
| `TestLogFetcher_SeverityAllNotSent` | N/A | severity="all" omitted from query |
| `TestParseLogResponse` | valid JSON | Parses single entry correctly |
| `TestParseLogResponse_InvalidJSON` | N/A | Invalid JSON → error |
| `TestParseLogResponse_EmptyItems` | N/A | Empty items → empty slice |
| `TestLogFetcher_NoTokenInQueryString` | N/A | Token in Authorization header, not query string |

Estimated: **11 test functions**

#### `platform/logfetcher_api_test.go` (`//go:build api`)

| Test Function | Validates |
|--------------|-----------|
| `TestAPI_LogFetcher_FetchLogs` | Real log fetch: GetProjectLog → FetchLogs chain works, entries have correct fields |
| `TestAPI_LogFetcher_SeverityFilter` | Severity filter returns only matching entries |

Estimated: **2 test functions**

#### `platform/apitest/harness_test.go`

| Test Function | Validates |
|--------------|-----------|
| `TestHarness_SkipsWithoutKey` | Skips when ZCP_API_KEY not set |
| `TestHarness_ClientCreation` | `h.Client()` returns non-nil client |
| `TestHarness_CtxTimeout` | `h.Ctx()` has timeout set |
| `TestHarness_ProjectID` | `h.ProjectID()` returns non-empty string |
| `TestHarness_Cleanup` | `h.Cleanup(fn)` runs fn on test cleanup |

Estimated: **5 test functions** (these are `//go:build api` themselves)

#### `auth/auth_test.go` (unit)

| Test Function | Table Cases | Validates |
|--------------|-------------|-----------|
| `TestResolve_EnvVar` | token set, host set, host default | Reads ZCP_API_KEY, resolves host/region |
| `TestResolve_ZcliData` | valid cli.data, missing file, corrupt JSON | Reads zcli cli.data fallback |
| `TestResolve_ZcliData_ScopeProject` | ScopeProjectId set, ScopeProjectId null | Uses scoped project or falls back to ListProjects |
| `TestResolve_NeitherAvailable` | N/A | Neither env nor zcli → AUTH_REQUIRED error |
| `TestResolve_InvalidToken` | N/A | Mock GetUserInfo fails → AUTH_INVALID_TOKEN |
| `TestResolve_NoProjects` | N/A | ListProjects returns 0 → TOKEN_NO_PROJECT |
| `TestResolve_MultipleProjects` | N/A | ListProjects returns 2+ → TOKEN_MULTI_PROJECT |
| `TestResolve_SingleProject` | N/A | ListProjects returns 1 → uses it |

Estimated: **8 test functions, ~12 table cases**

#### `auth/auth_api_test.go` (`//go:build api`)

| Test Function | Validates |
|--------------|-----------|
| `TestAPI_Resolve_FullFlow` | Real token → GetUserInfo → ListProjects → auth.Info populated |
| `TestAPI_Resolve_InvalidToken` | Fake token → correct auth error |

Estimated: **2 test functions**

#### `knowledge/engine_test.go`

Source reference: `zaia/internal/knowledge/engine_test.go:1-510` (30 test functions)

| Test Function | Validates |
|--------------|-----------|
| `TestStore_DocumentCount` | >= 60 embedded docs |
| `TestStore_List` | All resources have valid URI prefix, name, mimeType |
| `TestStore_Get` | Known doc (postgresql) returns content + keywords |
| `TestStore_GetNotFound` | Nonexistent URI → error |
| `TestSearch_PostgreSQLConnectionString` | postgresql in top 3 |
| `TestSearch_PostgresPort` | postgresql in results for "postgres port" |
| `TestSearch_RedisCache` | valkey in top 3 for "redis cache" |
| `TestSearch_NodejsDeploy` | nodejs in top 3 |
| `TestSearch_MysqlSetup` | mariadb in top 3 for "mysql setup" |
| `TestSearch_ElasticsearchFulltext` | elasticsearch in top 3 |
| `TestSearch_S3ObjectStorage` | object-storage in top 3 |
| `TestSearch_ZeropsYmlBuildCache` | zerops-yml or build-cache in top 3 |
| `TestSearch_ImportYmlServices` | import-yml in top 3 |
| `TestSearch_EnvironmentVariables` | env-variables in top 3 |
| `TestSearch_ScalingAutoscale` | platform/scaling in top 3 |
| `TestSearch_ConnectionStringNodejsPostgresql` | postgresql or connection-strings in top 3 |
| `TestSearch_NoResults_MongoDB` | Suggestions for unsupported service |
| `TestSearch_NoResults_Kubernetes` | Suggestions for unsupported concept |
| `TestSearch_TopResultHasFullContent` | Top result doc has Keywords section |
| `TestExpandQuery` | 7 cases: postgres→postgresql, redis→valkey, mysql→mariadb, etc. |
| `TestPathToURI` | 3 cases: embed path → zerops:// URI |
| `TestURIToPath` | 2 cases: zerops:// URI → embed path |
| `TestParseDocument_Keywords` | postgresql has "postgresql" keyword |
| `TestParseDocument_TLDR` | postgresql has TL;DR |
| `TestParseDocument_Title` | postgresql has title containing "PostgreSQL" |
| `TestExtractSnippet` | Snippet contains query term |
| `TestExtractSnippet_NoMatch` | Fallback snippet for non-match |
| `TestGenerateSuggestions_UnsupportedService` | dynamodb → DynamoDB suggestion |
| `TestGenerateSuggestions_WithResults` | No panic with results |
| `TestGenerateSuggestions_NoResults` | Fallback suggestion |
| `TestHitRate` | 8 queries: tracks hit@1 and hit@3 rates |

Estimated: **31 test functions, ~20 table cases**

---

### Phase 2 Tests

#### `ops/helpers_test.go`

| Test Function | Table Cases | Validates |
|--------------|-------------|-----------|
| `TestResolveServiceID_ByHostname` | found, not found, empty hostname | Hostname → ServiceStack.ID resolution |
| `TestResolveServiceID_Error` | list error | Mock error propagation |
| `TestParseSince_Duration` | "30m", "1h", "24h", "7d" | Duration string → time.Time |
| `TestParseSince_ISO8601` | valid ISO, invalid | ISO 8601 timestamp parsing |
| `TestParseSince_Default` | empty string | Returns zero time |
| `TestValidateHostname` | valid, empty, invalid chars | Hostname validation |

Estimated: **6 test functions, ~18 table cases**

#### `ops/helpers_api_test.go` (`//go:build api`)

| Test Function | Validates |
|--------------|-----------|
| `TestAPI_ResolveServiceID_RealService` | Known hostname resolves to real ID |

Estimated: **1 test function**

#### `ops/discover_test.go`

| Test Function | Table Cases | Validates |
|--------------|-------------|-----------|
| `TestDiscover_AllServices_Success` | N/A | Returns project + services list |
| `TestDiscover_WithService_Success` | known hostname | Filters to single service |
| `TestDiscover_WithService_NotFound` | unknown hostname | SERVICE_NOT_FOUND error |
| `TestDiscover_WithEnvs_Success` | N/A | Includes env vars when requested |
| `TestDiscover_AuthError` | N/A | Mock auth error propagates |

Estimated: **5 test functions, ~6 table cases**

#### `ops/discover_api_test.go` (`//go:build api`)

| Test Function | Validates |
|--------------|-----------|
| `TestAPI_Discover_AllServices` | Real services returned with correct shapes |
| `TestAPI_Discover_WithService` | Single service filtered correctly |
| `TestAPI_Discover_WithEnvs` | Env vars populated for real service |

Estimated: **3 test functions**

#### `ops/manage_test.go`

| Test Function | Table Cases | Validates |
|--------------|-------------|-----------|
| `TestManage_Start_Success` | N/A | Returns process with start action |
| `TestManage_Stop_Success` | N/A | Returns process with stop action |
| `TestManage_Restart_Success` | N/A | Returns process with restart action |
| `TestManage_Scale_Success` | all params, partial params | AutoscalingParams mapped correctly |
| `TestManage_Scale_CpuMode` | SHARED, DEDICATED | CpuMode validation |
| `TestManage_InvalidAction` | "invalid" | Error for unknown action |
| `TestManage_EmptyHostname` | N/A | SERVICE_REQUIRED error |
| `TestManage_ServiceNotFound` | N/A | Mock returns SERVICE_NOT_FOUND |

Estimated: **8 test functions, ~10 table cases**

#### `ops/manage_api_test.go` (`//go:build api`)

| Test Function | Validates |
|--------------|-----------|
| `TestAPI_Manage_Scale_ReadOnly` | Scale param mapping produces valid API request (dry-run-like) |

Estimated: **1 test function** (mutating tests in E2E only)

#### `ops/env_test.go`

| Test Function | Table Cases | Validates |
|--------------|-------------|-----------|
| `TestEnv_Get_Success` | service, project | Returns env vars |
| `TestEnv_Set_Success` | single var, multiple vars | Sets env vars, returns process |
| `TestEnv_Delete_Success` | single key | Deletes env var, returns process |
| `TestEnv_MissingScope` | no service, no project | Error for missing scope |
| `TestEnv_InvalidFormat` | "no_equals_sign", empty | INVALID_ENV_FORMAT |
| `TestEnv_InvalidAction` | "invalid" | Error for unknown action |

Estimated: **6 test functions, ~10 table cases**

#### `ops/env_api_test.go` (`//go:build api`)

| Test Function | Validates |
|--------------|-----------|
| `TestAPI_Env_Get_ServiceEnv` | Real env vars returned for known service |
| `TestAPI_Env_Get_ProjectEnv` | Real project-level env vars returned |

Estimated: **2 test functions** (read-only; set/delete in E2E)

#### `ops/logs_test.go`

| Test Function | Table Cases | Validates |
|--------------|-------------|-----------|
| `TestLogs_Success` | N/A | 2-step: GetProjectLog → FetchLogs chain |
| `TestLogs_WithFilters` | severity, since, limit, search | Filter params passed to LogFetcher |
| `TestLogs_EmptyHostname` | N/A | SERVICE_REQUIRED error |
| `TestLogs_GetProjectLogError` | N/A | First step error propagates |
| `TestLogs_FetchLogsError` | N/A | Second step error propagates |

Estimated: **5 test functions, ~6 table cases**

#### `ops/logs_api_test.go` (`//go:build api`)

| Test Function | Validates |
|--------------|-----------|
| `TestAPI_Logs_Fetch` | Real log fetch returns entries (may be empty) |
| `TestAPI_Logs_SeverityFilter` | Severity filter works with real data |

Estimated: **2 test functions**

#### `ops/import_test.go`

| Test Function | Table Cases | Validates |
|--------------|-------------|-----------|
| `TestImport_DryRun_Success` | valid YAML | Dry-run returns validation result |
| `TestImport_DryRun_InvalidYAML` | syntax error, missing fields | Error for invalid YAML |
| `TestImport_Real_Success` | N/A | Real import returns ImportResult with processes |
| `TestImport_MissingContent` | N/A | Error when no content or file |
| `TestImport_HasProjectSection` | "project:" in YAML | IMPORT_HAS_PROJECT error |

Estimated: **5 test functions, ~8 table cases**

#### `ops/import_api_test.go` (`//go:build api`)

| Test Function | Validates |
|--------------|-----------|
| `TestAPI_Import_DryRun` | Real dry-run validates YAML without side effects |

Estimated: **1 test function** (real import in E2E only)

#### `ops/validate_test.go`

| Test Function | Table Cases | Validates |
|--------------|-------------|-----------|
| `TestValidate_ZeropsYml_Valid` | minimal, full | Valid zerops.yml passes |
| `TestValidate_ZeropsYml_Invalid` | syntax error, missing required | Invalid YAML returns errors |
| `TestValidate_ImportYml_Valid` | minimal services | Valid import.yml passes |
| `TestValidate_ImportYml_Invalid` | has project section, invalid type | Invalid import.yml returns errors |
| `TestValidate_UnknownType` | "unknown.yml" | UNKNOWN_TYPE error |
| `TestValidate_Content_vs_FilePath` | content, filePath | Both modes work |

Estimated: **6 test functions, ~12 table cases** (no API test — offline)

#### `ops/delete_test.go`

| Test Function | Table Cases | Validates |
|--------------|-------------|-----------|
| `TestDelete_Success` | N/A | Returns process after deletion |
| `TestDelete_NotConfirmed` | confirm=false | CONFIRM_REQUIRED error |
| `TestDelete_ServiceNotFound` | N/A | SERVICE_NOT_FOUND error |
| `TestDelete_EmptyHostname` | N/A | SERVICE_REQUIRED error |

Estimated: **4 test functions, ~4 table cases** (real delete in E2E only)

#### `ops/subdomain_test.go`

| Test Function | Table Cases | Validates |
|--------------|-------------|-----------|
| `TestSubdomain_Enable_Success` | N/A | Returns process |
| `TestSubdomain_Disable_Success` | N/A | Returns process |
| `TestSubdomain_InvalidAction` | "toggle" | Error for invalid action |
| `TestSubdomain_EmptyHostname` | N/A | SERVICE_REQUIRED error |
| `TestSubdomain_AlreadyEnabled` | N/A | SUBDOMAIN_ALREADY_ENABLED → success (idempotent) |
| `TestSubdomain_AlreadyDisabled` | N/A | SUBDOMAIN_ALREADY_DISABLED → success (idempotent) |

Estimated: **6 test functions, ~6 table cases**

#### `ops/subdomain_api_test.go` (`//go:build api`)

| Test Function | Validates |
|--------------|-----------|
| `TestAPI_Subdomain_EnableDisable` | Enable + disable cycle on real service (sequential, with cleanup) |

Estimated: **1 test function** (mutating, sequential)

#### `ops/events_test.go`

| Test Function | Table Cases | Validates |
|--------------|-------------|-----------|
| `TestEvents_Success` | N/A | Returns merged timeline |
| `TestEvents_WithService` | known hostname | Filtered by service |
| `TestEvents_WithLimit` | 10, 50, default | Limit applied |
| `TestEvents_MergeSort` | interleaved timestamps | Process + AppVersion merged chronologically |

Estimated: **4 test functions, ~6 table cases**

#### `ops/events_api_test.go` (`//go:build api`)

| Test Function | Validates |
|--------------|-----------|
| `TestAPI_Events_Timeline` | Real events returned with timestamps |
| `TestAPI_Events_WithService` | Service filter returns correct subset |

Estimated: **2 test functions**

#### `ops/process_test.go`

| Test Function | Table Cases | Validates |
|--------------|-------------|-----------|
| `TestProcess_Status_Success` | RUNNING, FINISHED, FAILED | Status check returns correct status |
| `TestProcess_Cancel_Success` | N/A | Cancel returns CANCELED status |
| `TestProcess_NotFound` | N/A | PROCESS_NOT_FOUND error |
| `TestProcess_InvalidAction` | "invalid" | Error for unknown action |
| `TestProcess_StatusNormalization` | DONE→FINISHED, CANCELLED→CANCELED | Status normalized in response |

Estimated: **5 test functions, ~8 table cases**

#### `ops/process_api_test.go` (`//go:build api`)

| Test Function | Validates |
|--------------|-----------|
| `TestAPI_Process_GetExisting` | Known process ID returns status |
| `TestAPI_Process_NotFound` | Invalid ID → PROCESS_NOT_FOUND |

Estimated: **2 test functions**

#### `ops/context_test.go`

| Test Function | Table Cases | Validates |
|--------------|-------------|-----------|
| `TestContext_Content` | N/A | Returns non-empty static content |
| `TestContext_TokenSize` | N/A | Content is within ~800-1200 token budget |
| `TestContext_ContainsServiceCatalog` | N/A | Contains service type information |

Estimated: **3 test functions** (no API test — static content)

#### `ops/workflow_test.go`

| Test Function | Table Cases | Validates |
|--------------|-------------|-----------|
| `TestWorkflow_Catalog` | no param | Returns catalog of available workflows |
| `TestWorkflow_Specific` | "bootstrap", "deploy", "debug" | Returns specific workflow guidance |
| `TestWorkflow_NotFound` | "nonexistent" | Error for unknown workflow |

Estimated: **3 test functions, ~5 table cases** (no API test — static content)

#### `ops/progress_test.go`

| Test Function | Table Cases | Validates |
|--------------|-------------|-----------|
| `TestPollProcess_FinishesImmediately` | N/A | Process already FINISHED → callback once → return |
| `TestPollProcess_PollingToCompletion` | RUNNING, RUNNING, FINISHED | Multiple polls, callback each time |
| `TestPollProcess_Failed` | RUNNING, FAILED | Returns FAILED status |
| `TestPollProcess_ContextCancelled` | N/A | Context cancel stops polling |
| `TestPollProcess_Timeout` | never finishes | Exceeds max polls → error |

Estimated: **5 test functions, ~6 table cases**

#### `ops/progress_api_test.go` (`//go:build api`)

| Test Function | Validates |
|--------------|-----------|
| `TestAPI_PollProcess_RealOp` | Trigger real async op → poll to completion |

Estimated: **1 test function** (mutating, sequential)

#### `ops/deploy_test.go`

| Test Function | Table Cases | Validates |
|--------------|-------------|-----------|
| `TestDeploy_SSHMode_ParamValidation` | valid, missing source, missing target | SSH mode parameter validation |
| `TestDeploy_SSHMode_CommandConstruction` | N/A | Correct zcli push command built |
| `TestDeploy_SSHMode_AuthInjection` | N/A | Token + region injected into zcli login |
| `TestDeploy_LocalMode_Basic` | N/A | Local fallback mode works |
| `TestDeploy_ModeDetection` | inside Zerops (env), outside | Auto-detect SSH vs local mode |

Estimated: **5 test functions, ~8 table cases**

---

### Phase 3 Tests

For each tool handler: unit test (in-memory MCP + mock) + API test (in-memory MCP + real `ZeropsClient`).

#### `tools/discover_test.go`

Source reference: `zaia-mcp/internal/tools/discover_test.go:1-61`

| Test Function | Validates |
|--------------|-----------|
| `TestDiscoverTool_Basic` | No params → project + services |
| `TestDiscoverTool_WithService` | serviceHostname param → single service |
| `TestDiscoverTool_WithEnvs` | includeEnvs=true → env vars included |
| `TestDiscoverTool_ServiceNotFound` | Unknown hostname → IsError=true |
| `TestDiscoverTool_AuthError` | Auth failure → IsError=true with error code |

Estimated: **5 test functions**

#### `tools/discover_api_test.go` (`//go:build api`)

| Test Function | Validates |
|--------------|-----------|
| `TestAPI_DiscoverTool_RealBackend` | Full MCP chain → real API → valid response |
| `TestAPI_DiscoverTool_WithService` | Real service filter works end-to-end |

Estimated: **2 test functions**

#### `tools/manage_test.go`

Source reference: `zaia-mcp/internal/tools/manage_test.go:1-96`

| Test Function | Validates |
|--------------|-----------|
| `TestManageTool_Start` | action=start → process returned |
| `TestManageTool_Stop` | action=stop → process returned |
| `TestManageTool_Restart` | action=restart → process returned |
| `TestManageTool_Scale` | action=scale with CPU/RAM params |
| `TestManageTool_ScaleWithDisk` | action=scale with disk params |
| `TestManageTool_MissingAction` | No action → protocol error |
| `TestManageTool_MissingService` | No serviceHostname → protocol error |
| `TestManageTool_EmptyAction` | action="" → IsError=true |
| `TestManageTool_EmptyServiceHostname` | serviceHostname="" → IsError=true |
| `TestManageTool_InvalidAction` | action="invalid" → IsError=true |

Estimated: **10 test functions**

#### `tools/manage_api_test.go` (`//go:build api`)

| Test Function | Validates |
|--------------|-----------|
| `TestAPI_ManageTool_Scale` | Scale params reach real API correctly |

Estimated: **1 test function** (mutating ops in E2E)

#### `tools/env_test.go`

Source reference: `zaia-mcp/internal/tools/env_test.go:1-84`

| Test Function | Validates |
|--------------|-----------|
| `TestEnvTool_Get` | action=get → env vars returned |
| `TestEnvTool_Set` | action=set with variables → process returned |
| `TestEnvTool_Delete` | action=delete → process returned |
| `TestEnvTool_ProjectScope` | project=true → project-level env |
| `TestEnvTool_MissingScope` | No service or project → IsError=true |
| `TestEnvTool_EmptyAction` | action="" → IsError=true |
| `TestEnvTool_InvalidAction` | action="invalid" → IsError=true |

Estimated: **7 test functions**

#### `tools/env_api_test.go` (`//go:build api`)

| Test Function | Validates |
|--------------|-----------|
| `TestAPI_EnvTool_Get` | Real env vars via MCP chain |

Estimated: **1 test function**

#### `tools/logs_test.go`

Source reference: `zaia-mcp/internal/tools/logs_test.go:1-70`

| Test Function | Validates |
|--------------|-----------|
| `TestLogsTool_Basic` | serviceHostname → logs returned |
| `TestLogsTool_WithFilters` | severity + since + limit → params passed |
| `TestLogsTool_MissingService` | No serviceHostname → protocol error |
| `TestLogsTool_EmptyServiceHostname` | serviceHostname="" → IsError=true |
| `TestLogsTool_ZeroLimit` | limit=0 → no limit param sent |

Estimated: **5 test functions**

#### `tools/logs_api_test.go` (`//go:build api`)

| Test Function | Validates |
|--------------|-----------|
| `TestAPI_LogsTool_RealBackend` | Real log fetch via full MCP chain |

Estimated: **1 test function**

#### `tools/import_test.go`

Source reference: `zaia-mcp/internal/tools/import_test.go:1-66`

| Test Function | Validates |
|--------------|-----------|
| `TestImportTool_Content` | Inline YAML → process returned |
| `TestImportTool_DryRun` | dryRun=true → sync validation result |
| `TestImportTool_FilePath` | filePath → process returned |
| `TestImportTool_MissingContentAndFile` | Neither → IsError=true |
| `TestImportTool_BothContentAndFile` | Both → IsError=true |

Estimated: **5 test functions**

#### `tools/import_api_test.go` (`//go:build api`)

| Test Function | Validates |
|--------------|-----------|
| `TestAPI_ImportTool_DryRun` | Real dry-run via MCP chain |

Estimated: **1 test function**

#### `tools/validate_test.go`

Source reference: `zaia-mcp/internal/tools/validate_test.go:1-33`

| Test Function | Validates |
|--------------|-----------|
| `TestValidateTool_Content` | Inline content + type → validation result |
| `TestValidateTool_File` | filePath → validation result |
| `TestValidateTool_InvalidYAML` | Syntax error → IsError=true |
| `TestValidateTool_NoInput` | No content or file → IsError=true |

Estimated: **4 test functions** (no API test — offline)

#### `tools/knowledge_test.go`

Source reference: `zaia-mcp/internal/tools/knowledge_test.go:1-38`

| Test Function | Validates |
|--------------|-----------|
| `TestKnowledgeTool_Basic` | query → results returned |
| `TestKnowledgeTool_WithLimit` | limit param passed |
| `TestKnowledgeTool_MissingQuery` | No query → protocol error |
| `TestKnowledgeTool_EmptyQuery` | query="" → IsError=true |

Estimated: **4 test functions** (no API test — local BM25)

#### `tools/process_test.go`

Source reference: `zaia-mcp/internal/tools/process_test.go:1-50`

| Test Function | Validates |
|--------------|-----------|
| `TestProcessTool_Status` | processId → status returned |
| `TestProcessTool_Cancel` | action=cancel → CANCELED status |
| `TestProcessTool_MissingID` | No processId → protocol error |
| `TestProcessTool_InvalidAction` | action="invalid" → IsError=true |

Estimated: **4 test functions**

#### `tools/process_api_test.go` (`//go:build api`)

| Test Function | Validates |
|--------------|-----------|
| `TestAPI_ProcessTool_Status` | Real process status via MCP chain |

Estimated: **1 test function**

#### `tools/delete_test.go`

Source reference: `zaia-mcp/internal/tools/delete_test.go:1-43`

| Test Function | Validates |
|--------------|-----------|
| `TestDeleteTool_Confirmed` | confirm=true → process returned |
| `TestDeleteTool_NotConfirmed` | confirm=false → IsError=true |
| `TestDeleteTool_MissingService` | No serviceHostname → protocol error |
| `TestDeleteTool_EmptyHostname` | serviceHostname="" → IsError=true |

Estimated: **4 test functions** (real delete in E2E)

#### `tools/subdomain_test.go`

Source reference: `zaia-mcp/internal/tools/subdomain_test.go:1-80`

| Test Function | Validates |
|--------------|-----------|
| `TestSubdomainTool_Enable` | action=enable → process returned |
| `TestSubdomainTool_Disable` | action=disable → process returned |
| `TestSubdomainTool_InvalidAction` | action="toggle" → IsError=true |
| `TestSubdomainTool_MissingService` | No serviceHostname → protocol error |
| `TestSubdomainTool_EmptyServiceHostname` | serviceHostname="" → IsError=true |
| `TestSubdomainTool_EmptyAction` | action="" → IsError=true |

Estimated: **6 test functions**

#### `tools/subdomain_api_test.go` (`//go:build api`)

| Test Function | Validates |
|--------------|-----------|
| `TestAPI_SubdomainTool_EnableDisable` | Real enable/disable via MCP chain |

Estimated: **1 test function**

#### `tools/events_test.go`

Source reference: `zaia-mcp/internal/tools/events_test.go:1-57`

| Test Function | Validates |
|--------------|-----------|
| `TestEventsTool_Basic` | No params → events + summary returned |
| `TestEventsTool_WithService` | serviceHostname filter |
| `TestEventsTool_WithLimit` | limit param |
| `TestEventsTool_Error` | Auth error → IsError=true |

Estimated: **4 test functions**

#### `tools/events_api_test.go` (`//go:build api`)

| Test Function | Validates |
|--------------|-----------|
| `TestAPI_EventsTool_RealBackend` | Real events via MCP chain |

Estimated: **1 test function**

#### `tools/context_test.go`

| Test Function | Validates |
|--------------|-----------|
| `TestContextTool_Success` | Returns static content, non-empty |
| `TestContextTool_TokenBudget` | Content within ~800-1200 tokens |

Estimated: **2 test functions** (no API test — static)

#### `tools/workflow_test.go`

| Test Function | Validates |
|--------------|-----------|
| `TestWorkflowTool_Catalog` | No param → workflow list |
| `TestWorkflowTool_Specific` | workflow="bootstrap" → specific guidance |
| `TestWorkflowTool_NotFound` | workflow="nonexistent" → IsError=true |

Estimated: **3 test functions** (no API test — static)

#### `tools/convert_test.go`

Source reference: `zaia-mcp/internal/tools/convert_test.go:1-178` (NOTE: ZCP convert.go has different responsibility — PlatformError → MCP result, not CLI JSON parsing)

| Test Function | Validates |
|--------------|-----------|
| `TestPlatformErrorToMCPResult` | PlatformError → MCP result with IsError=true, JSON body |
| `TestPlatformErrorToMCPResult_WithSuggestion` | Includes suggestion in output |
| `TestSuccessResult_Sync` | Data → MCP result with IsError=false |
| `TestSuccessResult_Async` | Process list → MCP result with IsError=false |
| `TestSuccessResult_EmptyData` | Empty/nil data handling |

Estimated: **5 test functions**

#### `tools/annotations_test.go`

Source reference: `zaia-mcp/internal/tools/annotations_test.go:1-119`

| Test Function | Validates |
|--------------|-----------|
| `TestAnnotations_AllToolsHaveTitleAndAnnotations` | All 14 tools have correct title, readOnly, destructive, idempotent, openWorld hints |

Estimated: **1 test function, 14 subtests** (one per tool)

#### `server/server_test.go`

| Test Function | Validates |
|--------------|-----------|
| `TestServer_AllToolsRegistered` | All expected tools present in tools/list |
| `TestServer_Instructions` | Init message is ~40-50 tokens, mentions zerops_context |
| `TestServer_Connect` | In-memory transport connect succeeds |

Estimated: **3 test functions**

---

### Phase 4 Tests

#### `integration/multi_tool_test.go`

| Test Function | Scenarios | Validates |
|--------------|-----------|-----------|
| `TestIntegration_DiscoverThenManage` | Discover → use hostname → manage | Cross-tool data flow |
| `TestIntegration_ImportThenDiscover` | Import dry-run → discover | Import result matches discover |
| `TestIntegration_EnvSetThenGet` | Set env → get env → verify value | Env round-trip |
| `TestIntegration_DeleteWithConfirmGate` | Delete without confirm → retry with confirm | Safety gate works |
| `TestIntegration_ProcessPolling` | Async op → poll process | Process status transitions |
| `TestIntegration_ErrorPropagation` | Auth error → verify MCP error format | Error formatting end-to-end |
| `TestIntegration_ContextThenWorkflow` | Load context → load workflow | Content tools return data |

Estimated: **7 test functions** (mock only, no build tag)

#### `e2e/lifecycle_test.go` (`//go:build e2e`)

PRD reference: section 11.7 — 19 steps

Source reference: `zaia-mcp/e2e/e2e_test.go:1-304` (17 steps)

**Test structure:**
```go
func TestE2E_FullLifecycle(t *testing.T) {
    h := apitest.New(t)
    srv := server.NewWithClient(h.Client())
    s := newSession(t, srv)

    suffix := randomSuffix()
    rtHost := "zcp-test-rt" + suffix
    dbHost := "zcp-test-db" + suffix

    t.Cleanup(func() { cleanupServices(h, rtHost, dbHost) })

    // 19 sequential subtests...
}
```

| Step | Subtest | Operation | Validates |
|------|---------|-----------|-----------|
| 01 | `01_context` | `zerops_context` | Static content loads, non-empty, within token budget |
| 02 | `02_discover` | `zerops_discover` | Auth works, project visible, services listed |
| 03 | `03_knowledge` | `zerops_knowledge {query: "postgresql"}` | BM25 search returns results |
| 04 | `04_validate` | `zerops_validate {content: importYAML}` | Offline YAML validation passes |
| 05 | `05_import_dry_run` | `zerops_import {content, dryRun: true}` | Validates YAML, no side effects |
| 06 | `06_import_real` | `zerops_import {content}` | Creates 2 services (runtime + managed) |
| 07 | `07_wait_import` | `zerops_process` polling | Wait for import completion (120s max) |
| 08 | `08_discover_both` | `zerops_discover` | Both services exist |
| 09 | `09_env_set` | `zerops_env {action: set}` | Set env var on runtime service |
| 10 | `10_env_get` | `zerops_env {action: get}` | Read back, verify value |
| 11 | `11_stop_managed` | `zerops_manage {action: stop}` | Stop managed service |
| 12 | `12_start_managed` | `zerops_manage {action: start}` | Start managed service |
| 13 | `13_subdomain_enable` | `zerops_subdomain {action: enable}` | Enable subdomain (may fail if undeployed — expected) |
| 14 | `14_subdomain_disable` | `zerops_subdomain {action: disable}` | Disable subdomain |
| 15 | `15_logs` | `zerops_logs` | Fetch logs (may be empty — OK) |
| 16 | `16_events` | `zerops_events` | Activity timeline shows operations |
| 17 | `17_workflow` | `zerops_workflow` | Catalog returns; specific workflow returns |
| 18 | `18_delete_both` | `zerops_delete` (x2) | Delete test services + wait for processes |
| 19 | `19_discover_gone` | `zerops_discover` | Verify services no longer exist |

**Cleanup strategy:**
- `t.Cleanup()` always deletes created resources (even on failure)
- Cleanup uses `apitest.Harness` with fresh context (test context may be cancelled)
- Direct `ZeropsClient.DeleteService()` + `WaitForProcess()` — bypasses MCP

**Timeout handling:**
- Process polling: 40 attempts x 3s = 120s max per operation
- Test overall: `go test -timeout 600s` (10 minutes for full lifecycle)

Estimated: **1 test function, 19 sequential subtests**

#### `e2e/helpers_test.go` (`//go:build e2e`)

Source reference: `zaia-mcp/e2e/helpers_test.go:1-112`

| Helper | Purpose |
|--------|---------|
| `newSession(t, srv)` | Creates in-memory MCP session connected to server |
| `session.callTool(name, args)` | Calls MCP tool, returns result |
| `session.mustCallSuccess(name, args)` | Calls tool, fatals on error |
| `getTextContent(t, result)` | Extracts text from MCP result |
| `cleanupServices(h, hostnames...)` | Direct API cleanup with fresh context |
| `randomSuffix()` | 8-char hex for unique hostnames |

#### `e2e/process_helpers_test.go` (`//go:build e2e`)

Source reference: `zaia-mcp/e2e/process_helpers_test.go:1-52`

| Helper | Purpose |
|--------|---------|
| `waitForProcess(s, processID)` | Polls zerops_process until terminal state |
| `parseProcesses(t, text)` | Parses JSON array of processes from async response |
| Constants: `maxPollAttempts=40`, `pollInterval=3s` | 120s max per operation |

---

## Test Counts per Phase

### Phase 1: Foundation

| Package | Mock Tests | API Tests | Functions | Table Cases |
|---------|-----------|-----------|-----------|-------------|
| `platform/types_test.go` | 5 | — | 5 | ~12 |
| `platform/errors_test.go` | 8 | — | 8 | ~30 |
| `platform/zerops_test.go` | 8 | — | 8 | ~15 |
| `platform/zerops_api_test.go` | — | 13 | 13 | — |
| `platform/mock_test.go` | 11 | — | 11 | ~12 |
| `platform/logfetcher_test.go` | 11 | — | 11 | — |
| `platform/logfetcher_api_test.go` | — | 2 | 2 | — |
| `platform/apitest/harness_test.go` | — | 5 | 5 | — |
| `auth/auth_test.go` | 8 | — | 8 | ~12 |
| `auth/auth_api_test.go` | — | 2 | 2 | — |
| `knowledge/engine_test.go` | 31 | — | 31 | ~20 |
| **Phase 1 Total** | **82** | **22** | **104** | **~101** |

### Phase 2: Business Logic

| Package | Mock Tests | API Tests | Functions | Table Cases |
|---------|-----------|-----------|-----------|-------------|
| `ops/helpers_test.go` | 6 | — | 6 | ~18 |
| `ops/helpers_api_test.go` | — | 1 | 1 | — |
| `ops/discover_test.go` | 5 | — | 5 | ~6 |
| `ops/discover_api_test.go` | — | 3 | 3 | — |
| `ops/manage_test.go` | 8 | — | 8 | ~10 |
| `ops/manage_api_test.go` | — | 1 | 1 | — |
| `ops/env_test.go` | 6 | — | 6 | ~10 |
| `ops/env_api_test.go` | — | 2 | 2 | — |
| `ops/logs_test.go` | 5 | — | 5 | ~6 |
| `ops/logs_api_test.go` | — | 2 | 2 | — |
| `ops/import_test.go` | 5 | — | 5 | ~8 |
| `ops/import_api_test.go` | — | 1 | 1 | — |
| `ops/validate_test.go` | 6 | — | 6 | ~12 |
| `ops/delete_test.go` | 4 | — | 4 | ~4 |
| `ops/subdomain_test.go` | 6 | — | 6 | ~6 |
| `ops/subdomain_api_test.go` | — | 1 | 1 | — |
| `ops/events_test.go` | 4 | — | 4 | ~6 |
| `ops/events_api_test.go` | — | 2 | 2 | — |
| `ops/process_test.go` | 5 | — | 5 | ~8 |
| `ops/process_api_test.go` | — | 2 | 2 | — |
| `ops/context_test.go` | 3 | — | 3 | — |
| `ops/workflow_test.go` | 3 | — | 3 | ~5 |
| `ops/progress_test.go` | 5 | — | 5 | ~6 |
| `ops/progress_api_test.go` | — | 1 | 1 | — |
| `ops/deploy_test.go` | 5 | — | 5 | ~8 |
| **Phase 2 Total** | **76** | **16** | **92** | **~113** |

### Phase 3: MCP Layer

| Package | Mock Tests | API Tests | Functions | Table Cases |
|---------|-----------|-----------|-----------|-------------|
| `tools/discover_test.go` | 5 | — | 5 | — |
| `tools/discover_api_test.go` | — | 2 | 2 | — |
| `tools/manage_test.go` | 10 | — | 10 | — |
| `tools/manage_api_test.go` | — | 1 | 1 | — |
| `tools/env_test.go` | 7 | — | 7 | — |
| `tools/env_api_test.go` | — | 1 | 1 | — |
| `tools/logs_test.go` | 5 | — | 5 | — |
| `tools/logs_api_test.go` | — | 1 | 1 | — |
| `tools/import_test.go` | 5 | — | 5 | — |
| `tools/import_api_test.go` | — | 1 | 1 | — |
| `tools/validate_test.go` | 4 | — | 4 | — |
| `tools/knowledge_test.go` | 4 | — | 4 | — |
| `tools/process_test.go` | 4 | — | 4 | — |
| `tools/process_api_test.go` | — | 1 | 1 | — |
| `tools/delete_test.go` | 4 | — | 4 | — |
| `tools/subdomain_test.go` | 6 | — | 6 | — |
| `tools/subdomain_api_test.go` | — | 1 | 1 | — |
| `tools/events_test.go` | 4 | — | 4 | — |
| `tools/events_api_test.go` | — | 1 | 1 | — |
| `tools/context_test.go` | 2 | — | 2 | — |
| `tools/workflow_test.go` | 3 | — | 3 | — |
| `tools/convert_test.go` | 5 | — | 5 | — |
| `tools/annotations_test.go` | 1 (14 subtests) | — | 1 | — |
| `server/server_test.go` | 3 | — | 3 | — |
| **Phase 3 Total** | **72** | **9** | **81** | — |

### Phase 4: Integration + E2E

| Package | Mock Tests | API Tests | E2E Steps | Functions |
|---------|-----------|-----------|-----------|-----------|
| `integration/multi_tool_test.go` | 7 | — | — | 7 |
| `e2e/lifecycle_test.go` | — | — | 19 | 1 (19 subtests) |
| **Phase 4 Total** | **7** | — | **19** | **8** |

### Grand Total

| Category | Count |
|----------|-------|
| Mock/unit test functions | **237** |
| API contract test functions | **47** |
| Integration test functions | **7** |
| E2E test functions | **1** (19 subtests) |
| **Total test functions** | **285** |
| **Total table-driven cases** | **~214** |
| **Total E2E steps** | **19** |

---

## Test File Summary (Complete Inventory)

### Files that run always (no build tag)

```
internal/platform/types_test.go
internal/platform/errors_test.go
internal/platform/zerops_test.go
internal/platform/mock_test.go
internal/platform/logfetcher_test.go
internal/auth/auth_test.go
internal/knowledge/engine_test.go
internal/ops/helpers_test.go
internal/ops/discover_test.go
internal/ops/manage_test.go
internal/ops/env_test.go
internal/ops/logs_test.go
internal/ops/import_test.go
internal/ops/validate_test.go
internal/ops/delete_test.go
internal/ops/subdomain_test.go
internal/ops/events_test.go
internal/ops/process_test.go
internal/ops/context_test.go
internal/ops/workflow_test.go
internal/ops/progress_test.go
internal/ops/deploy_test.go
internal/tools/discover_test.go
internal/tools/manage_test.go
internal/tools/env_test.go
internal/tools/logs_test.go
internal/tools/import_test.go
internal/tools/validate_test.go
internal/tools/knowledge_test.go
internal/tools/process_test.go
internal/tools/delete_test.go
internal/tools/subdomain_test.go
internal/tools/events_test.go
internal/tools/context_test.go
internal/tools/workflow_test.go
internal/tools/convert_test.go
internal/tools/annotations_test.go
internal/server/server_test.go
integration/multi_tool_test.go
```
Total: **39 test files**

### Files with `//go:build api`

```
internal/platform/zerops_api_test.go
internal/platform/logfetcher_api_test.go
internal/platform/apitest/harness_test.go
internal/auth/auth_api_test.go
internal/ops/helpers_api_test.go
internal/ops/discover_api_test.go
internal/ops/manage_api_test.go
internal/ops/env_api_test.go
internal/ops/logs_api_test.go
internal/ops/import_api_test.go
internal/ops/subdomain_api_test.go
internal/ops/events_api_test.go
internal/ops/process_api_test.go
internal/ops/progress_api_test.go
internal/tools/discover_api_test.go
internal/tools/manage_api_test.go
internal/tools/env_api_test.go
internal/tools/logs_api_test.go
internal/tools/import_api_test.go
internal/tools/process_api_test.go
internal/tools/subdomain_api_test.go
internal/tools/events_api_test.go
```
Total: **22 API test files**

### Files with `//go:build e2e`

```
e2e/lifecycle_test.go
e2e/helpers_test.go
e2e/process_helpers_test.go
```
Total: **3 E2E test files**

### Grand total: **64 test files**
