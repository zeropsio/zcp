# Phase 1 Implementation Analysis: Platform Client + Auth + Mock

> **Source of truth**: `design/zcp-prd.md` (PRD). All section references use `PRD §N`.
> Source files referenced with `file:line` format.

---

## 1. platform/types.go

**What**: All domain types extracted from source `client.go:70-253` into a dedicated file.
**Why split**: PRD §4.2 — "ZCP splits them into a separate `platform/types.go` for clarity. This is a **new file split**, not a direct port of an existing file."

### Types to Define

| Type | Fields (Go types) | Source |
|------|-------------------|--------|
| `UserInfo` | `ID string`, `FullName string`, `Email string` | `client.go:71-75` — direct port |
| `Project` | `ID string`, `Name string`, `Status string` | `client.go:78-82` — direct port |
| `ServiceStack` | `ID string`, `Name string`, `ProjectID string`, `ServiceStackTypeInfo ServiceTypeInfo`, `Status string`, `Mode string`, `Ports []Port`, `CustomAutoscaling *CustomAutoscaling`, `Created string`, `LastUpdate string` | `client.go:85-96` — direct port |
| `ServiceTypeInfo` | `ServiceStackTypeVersionName string` | `client.go:99-101` — direct port |
| `Port` | `Port int`, `Protocol string`, `Public bool` | `client.go:104-108` — direct port |
| `CustomAutoscaling` | `HorizontalMinCount int32`, `HorizontalMaxCount int32`, `CpuMode string`, `StartCpuCoreCount int32`, `MinCpu int32`, `MaxCpu int32`, `MinRam float64`, `MaxRam float64`, `MinDisk float64`, `MaxDisk float64` | `client.go:111-122` — direct port |
| `AutoscalingParams` | `HorizontalMinCount *int32`, `HorizontalMaxCount *int32`, `VerticalCpuMode *string`, `VerticalStartCpu *int32`, `VerticalMinCpu *int32`, `VerticalMaxCpu *int32`, `VerticalMinRam *float64`, `VerticalMaxRam *float64`, `VerticalMinDisk *float64`, `VerticalMaxDisk *float64`, `VerticalSwapEnabled *bool` | `client.go:125-137` — direct port |
| `Process` | `ID string`, `ActionName string`, `Status string`, `ServiceStacks []ServiceStackRef`, `Created string`, `Started *string`, `Finished *string`, `FailReason *string` | `client.go:140-149` — direct port. **Note**: `FailReason` exists in source type but is NEVER populated by `mapProcess()` in `zerops.go:645-684`. ZCP MUST populate it. PRD §5.4 requires it. |
| `ServiceStackRef` | `ID string`, `Name string` | `client.go:152-155` — direct port |
| `EnvVar` | `ID string`, `Key string`, `Content string` | `client.go:158-162` — direct port |
| `ImportResult` | `ProjectID string`, `ProjectName string`, `ServiceStacks []ImportedServiceStack` | `client.go:165-169` — direct port |
| `ImportedServiceStack` | `ID string`, `Name string`, `Processes []Process`, `Error *APIError` | `client.go:172-177` — direct port |
| `APIError` | `Code string`, `Message string` + `Error() string` method | `client.go:180-187` — direct port |
| `LogAccess` | `AccessToken string`, `Expiration string`, `URL string`, `URLPlain string` | `client.go:190-195` — direct port |
| `LogFetchParams` | `ServiceID string`, `Severity string`, `Since time.Time`, `Limit int`, `Search string` | `client.go:198-204` — direct port |
| `LogEntry` | `ID string`, `Timestamp string`, `Severity string`, `Message string`, `Container string` | `client.go:207-213` — direct port |
| `ProcessEvent` | `ID string`, `ProjectID string`, `ServiceStacks []ServiceStackRef`, `ActionName string`, `Status string`, `Created string`, `Started *string`, `Finished *string`, `CreatedByUser *UserRef`, `CreatedBySystem bool` | `client.go:216-227` — direct port |
| `AppVersionEvent` | `ID string`, `ProjectID string`, `ServiceStackID string`, `Source string`, `Status string`, `Sequence int`, `Build *BuildInfo`, `Created string`, `LastUpdate string` | `client.go:230-240` — direct port |
| `BuildInfo` | `PipelineStart *string`, `PipelineFinish *string`, `PipelineFailed *string` | `client.go:243-247` — direct port |
| `UserRef` | `FullName string`, `Email string` | `client.go:250-253` — direct port |

### Constants

```go
const DefaultAPITimeout = 30 * time.Second  // source client.go:68
```

### What to Port vs Write New

- **Direct port**: All types above (copy from `client.go:70-253`, add json tags).
- **New**: The file itself (source has types in `client.go`, not a separate file).

### Estimated Lines

~200 lines (types + json tags + `APIError.Error()` method + `DefaultAPITimeout` constant).

### Dependencies

```go
import "time"  // for DefaultAPITimeout
```

---

## 2. platform/errors.go

### Error Codes — Keep

From `errors.go:11-38`, keep all except CLI-specific setup codes (PRD §4.4):

```go
const (
    ErrAuthRequired           = "AUTH_REQUIRED"
    ErrAuthInvalidToken       = "AUTH_INVALID_TOKEN"
    ErrAuthTokenExpired       = "AUTH_TOKEN_EXPIRED"
    ErrAuthAPIError           = "AUTH_API_ERROR"
    ErrTokenNoProject         = "TOKEN_NO_PROJECT"
    ErrTokenMultiProject      = "TOKEN_MULTI_PROJECT"
    ErrServiceNotFound        = "SERVICE_NOT_FOUND"
    ErrServiceRequired        = "SERVICE_REQUIRED"
    ErrConfirmRequired        = "CONFIRM_REQUIRED"
    ErrFileNotFound           = "FILE_NOT_FOUND"
    ErrZeropsYmlNotFound      = "ZEROPS_YML_NOT_FOUND"
    ErrInvalidZeropsYml       = "INVALID_ZEROPS_YML"
    ErrInvalidImportYml       = "INVALID_IMPORT_YML"
    ErrImportHasProject       = "IMPORT_HAS_PROJECT"
    ErrInvalidScaling         = "INVALID_SCALING"
    ErrInvalidParameter       = "INVALID_PARAMETER"
    ErrInvalidEnvFormat       = "INVALID_ENV_FORMAT"
    ErrInvalidHostname        = "INVALID_HOSTNAME"
    ErrUnknownType            = "UNKNOWN_TYPE"
    ErrProcessNotFound        = "PROCESS_NOT_FOUND"
    ErrProcessAlreadyTerminal = "PROCESS_ALREADY_TERMINAL"
    ErrPermissionDenied       = "PERMISSION_DENIED"
    ErrAPIError               = "API_ERROR"
    ErrAPITimeout             = "API_TIMEOUT"
    ErrAPIRateLimited         = "API_RATE_LIMITED"
    ErrNetworkError           = "NETWORK_ERROR"
    ErrInvalidUsage           = "INVALID_USAGE"
)
```

### Error Codes — Skip

- `ErrSetupDownloadFailed` — CLI-specific (`errors.go:39`)
- `ErrSetupInstallFailed` — CLI-specific (`errors.go:40`)
- `ErrSetupConfigFailed` — CLI-specific (`errors.go:41`)
- `ErrSetupUnsupportedOS` — CLI-specific (`errors.go:42`)

### PlatformError Struct

Direct port from `errors.go:131-148`:

```go
type PlatformError struct {
    Code       string
    Message    string
    Suggestion string
}

func (e *PlatformError) Error() string { return e.Message }

func NewPlatformError(code, message, suggestion string) *PlatformError {
    return &PlatformError{Code: code, Message: message, Suggestion: suggestion}
}
```

### mapSDKError() Logic

Port from `zerops.go:952-986`. This function is defined in `zerops.go` in source but logically belongs with errors. In ZCP, keep it in `zerops.go` (same as source) since it depends on SDK types.

Logic:
1. `errors.As(err, &apiErr)` → call `mapAPIError()`
2. `errors.As(err, &netErr)` → `ErrNetworkError`
3. `errors.As(err, &dnsErr)` → `ErrNetworkError`
4. `errors.Is(err, context.DeadlineExceeded)` → `ErrAPITimeout`
5. `errors.Is(err, context.Canceled)` → `ErrAPIError`
6. String-based fallback: "connection refused", "no such host" → `ErrNetworkError`
7. Default → `ErrAPIError`

### mapAPIError() Logic

Port from `zerops.go:988-1024`. Maps HTTP status codes to platform errors:
- 401 → `ErrAuthTokenExpired` (PRD: change suggestion from "zaia login" to "Check token validity")
- 403 → `ErrPermissionDenied`
- 404 + entity "process" → `ErrProcessNotFound`; default → `ErrServiceNotFound`
- 429 → `ErrAPIRateLimited`
- Contains "SubdomainAccessAlreadyEnabled" → `SUBDOMAIN_ALREADY_ENABLED` (dynamic code, PRD §4.4)
- Contains "SubdomainAccessAlreadyDisabled" → `SUBDOMAIN_ALREADY_DISABLED` (dynamic code)
- 5xx → `ErrAPIError`

### What to Port vs Write New

- **Port**: Error code constants, `PlatformError` struct, `NewPlatformError`.
- **Skip**: `MapHTTPError()` (`errors.go:46-62`) — CLI-specific, replaced by `mapAPIError()`.
- **Skip**: `MapNetworkError()` (`errors.go:65-93`) — replaced by inline logic in `mapSDKError()`. However, `logfetcher.go:88-91` calls `MapNetworkError()` — keep as internal helper or inline in logfetcher.
- **Skip**: `ExitCodeForError()` (`errors.go:96-117`) — CLI exit codes, irrelevant for MCP server.
- **Skip**: `HTTPError` struct (`errors.go:119-127`) — CLI-specific.
- **Keep**: `MapNetworkError()` — still needed by `logfetcher.go`. Port from `errors.go:65-93`.

### Estimated Lines

~100 lines (constants + PlatformError + NewPlatformError + MapNetworkError).

### Dependencies

```go
import (
    "errors"
    "net"
    "strings"
)
```

---

## 3. platform/client.go

### Client Interface

Direct port from `client.go:10-59`. Exact method signatures:

```go
type Client interface {
    // Auth
    GetUserInfo(ctx context.Context) (*UserInfo, error)

    // Project discovery
    ListProjects(ctx context.Context, clientID string) ([]Project, error)
    GetProject(ctx context.Context, projectID string) (*Project, error)

    // Service discovery
    ListServices(ctx context.Context, projectID string) ([]ServiceStack, error)
    GetService(ctx context.Context, serviceID string) (*ServiceStack, error)

    // Service management
    StartService(ctx context.Context, serviceID string) (*Process, error)
    StopService(ctx context.Context, serviceID string) (*Process, error)
    RestartService(ctx context.Context, serviceID string) (*Process, error)
    SetAutoscaling(ctx context.Context, serviceID string, params AutoscalingParams) (*Process, error)

    // Environment variables
    GetServiceEnv(ctx context.Context, serviceID string) ([]EnvVar, error)
    SetServiceEnvFile(ctx context.Context, serviceID string, content string) (*Process, error)
    DeleteUserData(ctx context.Context, userDataID string) (*Process, error)
    GetProjectEnv(ctx context.Context, projectID string) ([]EnvVar, error)
    CreateProjectEnv(ctx context.Context, projectID string, key, content string, sensitive bool) (*Process, error)
    DeleteProjectEnv(ctx context.Context, envID string) (*Process, error)

    // Import
    ImportServices(ctx context.Context, projectID string, yaml string) (*ImportResult, error)

    // Delete
    DeleteService(ctx context.Context, serviceID string) (*Process, error)

    // Process
    GetProcess(ctx context.Context, processID string) (*Process, error)
    CancelProcess(ctx context.Context, processID string) (*Process, error)

    // Subdomain
    EnableSubdomainAccess(ctx context.Context, serviceID string) (*Process, error)
    DisableSubdomainAccess(ctx context.Context, serviceID string) (*Process, error)

    // Logs
    GetProjectLog(ctx context.Context, projectID string) (*LogAccess, error)

    // Activity
    SearchProcesses(ctx context.Context, projectID string, limit int) ([]ProcessEvent, error)
    SearchAppVersions(ctx context.Context, projectID string, limit int) ([]AppVersionEvent, error)
}
```

### LogFetcher Interface

Direct port from `client.go:62-65`:

```go
type LogFetcher interface {
    FetchLogs(ctx context.Context, logAccess *LogAccess, params LogFetchParams) ([]LogEntry, error)
}
```

### What to Port vs Write New

- **Direct port**: Both interfaces verbatim from source `client.go:10-65`.
- **New**: File will be much smaller since types moved to `types.go`.

### Estimated Lines

~50 lines (interfaces + imports + comments).

### Dependencies

```go
import "context"
```

---

## 4. platform/mock.go

### MockClient Struct

Port from `mock.go:1-457`. Key design:

```go
const statusCancelled = "CANCELLED"  // mock.go:9

type Mock struct {
    mu sync.RWMutex

    userInfo         *UserInfo
    projects         []Project
    project          *Project
    services         []ServiceStack
    service          *ServiceStack
    processes        map[string]*Process
    envVars          map[string][]EnvVar   // serviceID -> env vars
    projectEnv       []EnvVar
    logAccess        *LogAccess
    importResult     *ImportResult
    processEvents    []ProcessEvent
    appVersionEvents []AppVersionEvent

    errors map[string]error   // method name -> error
}
```

### Builder Methods

All from `mock.go:36-146`:
- `NewMock() *Mock`
- `WithUserInfo(info *UserInfo) *Mock`
- `WithProjects(projects []Project) *Mock`
- `WithProject(project *Project) *Mock`
- `WithServices(services []ServiceStack) *Mock`
- `WithService(service *ServiceStack) *Mock`
- `WithProcess(process *Process) *Mock`
- `WithServiceEnv(serviceID string, vars []EnvVar) *Mock`
- `WithProjectEnv(vars []EnvVar) *Mock`
- `WithLogAccess(access *LogAccess) *Mock`
- `WithImportResult(result *ImportResult) *Mock`
- `WithProcessEvents(events []ProcessEvent) *Mock`
- `WithAppVersionEvents(events []AppVersionEvent) *Mock`
- `WithError(method string, err error) *Mock`

### Interface Method Implementations

All 26 Client methods, from `mock.go:154-424`. Pattern: check `getError(method)` first, then return configured data.

Notable behaviors:
- `GetService`: Falls back to searching `services` list by ID (`mock.go:196-212`)
- `SetAutoscaling`: Returns `nil, nil` (sync operation, `mock.go:250-256`)
- `CancelProcess`: Mutates process status to `statusCancelled` (`mock.go:358-370`)
- `CreateProjectEnv`: Source `mock.go:299` has wrong signature — 5 params instead of matching interface (4 string params + bool). **ZCP must fix**: match the interface signature exactly.

### MockLogFetcher

Port from `mock.go:427-457`:

```go
type MockLogFetcher struct {
    entries []LogEntry
    err     error
}

func NewMockLogFetcher() *MockLogFetcher
func (f *MockLogFetcher) WithEntries(entries []LogEntry) *MockLogFetcher
func (f *MockLogFetcher) WithError(err error) *MockLogFetcher
func (f *MockLogFetcher) FetchLogs(ctx, logAccess, params) ([]LogEntry, error)
```

### Thread-Safety

- `sync.RWMutex` protects all fields.
- Builder methods (`With*`) use `Lock()`.
- Read methods use `RLock()`.
- `CancelProcess` uses `Lock()` (mutates state).
- `getError()` uses `RLock()`.

### Compile-time Interface Checks

```go
var _ Client = (*Mock)(nil)         // mock.go:12
var _ LogFetcher = (*MockLogFetcher)(nil)  // mock.go:427
```

### What to Port vs Write New

- **Direct port**: All of `mock.go` with one fix — `CreateProjectEnv` mock signature.
- **Fix**: Source `mock.go:299` has `CreateProjectEnv(_ context.Context, _ string, _ string, _ string, _ bool)` which doesn't match the interface `CreateProjectEnv(ctx, projectID, key, content string, sensitive bool)`. The mock has an extra `_ string` param. ZCP must use the correct interface signature.

### Estimated Lines

~460 lines (matching source).

### Dependencies

```go
import (
    "context"
    "fmt"
    "sync"
)
```

---

## 5. platform/apitest/harness.go

### Build Tag

```go
//go:build api
```

### APIHarness Struct Design

**New implementation** — no source equivalent. Designed per PRD §11.4.

```go
type APIHarness struct {
    t         *testing.T
    client    *ZeropsClient   // uses parent package platform
    projectID string
    ctx       context.Context
    cancel    context.CancelFunc
    cleanups  []func()
}
```

### New(t) Function Behavior

```go
func New(t *testing.T) *APIHarness
```

1. Read `ZCP_API_KEY` from env. If empty → `t.Skip("ZCP_API_KEY not set")`.
2. Read `ZCP_API_HOST` from env (default: `"api.app-prg1.zerops.io"`).
3. Create `platform.NewZeropsClient(token, apiHost)`.
4. Call `GetUserInfo(ctx)` → get `clientID`.
5. Call `ListProjects(ctx, clientID)` → get project. If 0 projects → `t.Fatal`. If >1 → use first.
6. Create context with 60s timeout.
7. Register `t.Cleanup()` to run all cleanup functions + cancel context.

### Methods

| Method | Return | Purpose |
|--------|--------|---------|
| `Client()` | `platform.Client` | Returns real `ZeropsClient` |
| `Ctx()` | `context.Context` | Returns timeout-bounded context |
| `ProjectID()` | `string` | Returns discovered project ID |
| `Cleanup(fn func())` | — | Registers cleanup function for `t.Cleanup` |

### Package Consideration

The harness lives in `internal/platform/apitest/` as a separate package. It imports `platform` (parent). This avoids circular deps since test files in `platform/` use `platform_test` package and can import `apitest`.

### Estimated Lines

~80 lines.

### Dependencies

```go
import (
    "context"
    "os"
    "testing"
    "time"

    "github.com/zeropsio/zcp/internal/platform"
)
```

---

## 6. platform/zerops.go

### ZeropsClient Struct

Port from `zerops.go:26-30`:

```go
type ZeropsClient struct {
    handler  sdk.Handler
    apiHost  string
    clientID string         // removed — use sync.Once
    once     sync.Once      // NEW: thread-safe clientID caching
    cachedID string         // NEW: cached result
    idErr    error          // NEW: cached error
}
```

**Critical change** (PRD §4.3): Source uses racy string check (`zerops.go:57-67`). ZCP MUST use `sync.Once`:

```go
func (z *ZeropsClient) getClientID(ctx context.Context) (string, error) {
    z.once.Do(func() {
        info, err := z.GetUserInfo(ctx)
        if err != nil {
            z.idErr = err
            return
        }
        z.cachedID = info.ID
    })
    return z.cachedID, z.idErr
}
```

### Constructor: New(token, apiHost)

Port from `zerops.go:33-50`:

```go
func NewZeropsClient(token, apiHost string) (*ZeropsClient, error)
```

Logic:
1. Prefix `https://` if no scheme.
2. Ensure trailing `/`.
3. `sdkBase.DefaultConfig(sdkBase.WithCustomEndpoint(endpoint))`.
4. `sdk.New(config, &http.Client{Timeout: DefaultAPITimeout})`.
5. `sdk.AuthorizeSdk(handler, token)`.
6. Return `&ZeropsClient{handler, apiHost, ...}`.

### Each Client Method Implementation

**Auth group:**

| Method | SDK Call | Source Line | Notes |
|--------|---------|-------------|-------|
| `GetUserInfo` | `handler.GetUserInfo(ctx)` | `zerops.go:73-93` | Maps `ClientUserList[0].ClientId` → UserInfo.ID |
| `ListProjects` | `handler.PostProjectSearch(ctx, filter)` | `zerops.go:95-125` | Filter by clientID (EsFilter). Maps items to `[]Project`. |
| `GetProject` | `handler.GetProject(ctx, pathParam)` | `zerops.go:127-143` | `path.ProjectId{Id: uuid.ProjectId(projectID)}` |

**Discovery group:**

| Method | SDK Call | Source Line | Notes |
|--------|---------|-------------|-------|
| `ListServices` | `handler.PostServiceStackSearch(ctx, filter)` | `zerops.go:149-185` | Filters by clientID, then client-side filter by projectID. Uses `mapEsServiceStack()`. |
| `GetService` | `handler.GetServiceStack(ctx, pathParam)` | `zerops.go:187-199` | Uses `mapFullServiceStack()`. |

**Lifecycle group:**

| Method | SDK Call | Source Line | Notes |
|--------|---------|-------------|-------|
| `StartService` | `handler.PutServiceStackStart(ctx, pathParam)` | `zerops.go:205-217` | Standard process return. |
| `StopService` | `handler.PutServiceStackStop(ctx, pathParam)` | `zerops.go:219-231` | Standard process return. |
| `RestartService` | `handler.PutServiceStackRestart(ctx, pathParam)` | `zerops.go:233-245` | Standard process return. |
| `SetAutoscaling` | `handler.PutServiceStackAutoscaling(ctx, pathParam, body)` | `zerops.go:247-265` | Uses `buildAutoscalingBody()`. Returns `nil, nil` if `out.Process == nil`. |

**Environment group:**

| Method | SDK Call | Source Line | Notes |
|--------|---------|-------------|-------|
| `GetServiceEnv` | `handler.GetServiceStackEnv(ctx, pathParam)` | `zerops.go:271-291` | Maps items. Content via `string(e.Content)`. |
| `SetServiceEnvFile` | `handler.PutServiceStackUserDataEnvFile(ctx, pathParam, body)` | `zerops.go:293-308` | Body: `body.UserDataPutEnvFile{EnvFile: types.NewText(content)}`. |
| `DeleteUserData` | `handler.DeleteUserData(ctx, pathParam)` | `zerops.go:310-322` | `path.UserDataId`. |
| `GetProjectEnv` | `handler.PostProjectSearch(ctx, filter)` | `zerops.go:324-368` | Dual filter (clientID + projectID). Maps `project.EnvList`. |
| `CreateProjectEnv` | `handler.PostProjectEnv(ctx, pathParam, body)` | `zerops.go:370-387` | Body: `body.ProjectEnvPost{Key, Content, Sensitive}`. |
| `DeleteProjectEnv` | `handler.DeleteProjectEnv(ctx, pathParam)` | `zerops.go:389-401` | `path.ProjectEnvId{Id: uuid.EnvId(envID)}`. |

**Import/Delete group:**

| Method | SDK Call | Source Line | Notes |
|--------|---------|-------------|-------|
| `ImportServices` | `handler.PostProjectServiceStackImport(ctx, pathParam, body)` | `zerops.go:407-442` | Body: `body.ServiceStackImport{Yaml: types.Text(yamlContent)}`. Maps stacks + error + processes. |
| `DeleteService` | `handler.DeleteServiceStack(ctx, pathParam)` | `zerops.go:444-456` | Standard process return. |

**Process group:**

| Method | SDK Call | Source Line | Notes |
|--------|---------|-------------|-------|
| `GetProcess` | `handler.GetProcess(ctx, pathParam)` | `zerops.go:462-474` | Standard process mapping. |
| `CancelProcess` | `handler.PutProcessCancel(ctx, pathParam)` | `zerops.go:476-488` | Standard process mapping. |

**Subdomain group:**

| Method | SDK Call | Source Line | Notes |
|--------|---------|-------------|-------|
| `EnableSubdomainAccess` | `handler.PutServiceStackEnableSubdomainAccess(ctx, pathParam)` | `zerops.go:494-506` | Standard process return. |
| `DisableSubdomainAccess` | `handler.PutServiceStackDisableSubdomainAccess(ctx, pathParam)` | `zerops.go:508-520` | Standard process return. |

**Logs group:**

| Method | SDK Call | Source Line | Notes |
|--------|---------|-------------|-------|
| `GetProjectLog` | `handler.GetProjectLog(ctx, pathParam, queryParam)` | `zerops.go:526-549` | URL prefix stripping: `"GET "` then add `"https://"`. |

**Activity group:**

| Method | SDK Call | Source Line | Notes |
|--------|---------|-------------|-------|
| `SearchProcesses` | `handler.PostProcessSearch(ctx, filter)` | `zerops.go:555-596` | Client-side filter by projectID. Sort by created desc. |
| `SearchAppVersions` | `handler.PostAppVersionSearch(ctx, filter)` | `zerops.go:598-639` | Client-side filter by projectID. Sort by created desc. |

### Mapping Helpers

| Function | Source Line | Purpose |
|----------|-------------|---------|
| `mapProcess(output.Process) Process` | `zerops.go:645-684` | Status normalization: DONE→FINISHED, CANCELLED→CANCELED. Maps optional fields (Started, Finished). **ZCP FIX**: Must also map `FailReason` (source omits it, PRD §5.4 requires it). |
| `mapEsServiceStack(output.EsServiceStack) ServiceStack` | `zerops.go:686-710` | Maps search result to domain type. Ports NOT mapped in source. |
| `mapFullServiceStack(output.ServiceStack) ServiceStack` | `zerops.go:712-731` | Maps direct-get result. Ports NOT mapped in source. |
| `mapOutputCustomAutoscaling(*output.CustomAutoscaling) *CustomAutoscaling` | `zerops.go:733-774` | Deep mapping of vertical + horizontal scaling. Uses `.Get()` for nullable fields. |
| `buildAutoscalingBody(AutoscalingParams) body.Autoscaling` | `zerops.go:776-856` | Builds SDK request body from sparse params. |
| `mapEsProcessEvent(output.EsProcess) ProcessEvent` | `zerops.go:858-907` | Status normalization. Maps user ref. |
| `mapEsAppVersionEvent(output.EsAppVersion) AppVersionEvent` | `zerops.go:909-945` | Maps build pipeline info. |
| `mapSDKError(err, entityType) error` | `zerops.go:952-986` | SDK → PlatformError. |
| `mapAPIError(apiErr, entityType) error` | `zerops.go:988-1024` | HTTP status → PlatformError. |

### ZCP-Specific Fixes Over Source

1. **`sync.Once` for clientID** — replaces racy string check (PRD §4.3, `zerops.go:57-67`).
2. **`mapProcess` must map `FailReason`** — source `zerops.go:645-684` omits it. PRD §5.4 requires `failReason` in MCP results.
3. **Suggestion text must NOT reference "zaia login"** — source `zerops.go:995` says "Run: zaia login <token>". ZCP should say "Check token validity" or similar.
4. **`Ports` mapping** — source never maps `Ports` field in `mapEsServiceStack` or `mapFullServiceStack`. The SDK's `output.ServiceStack` and `output.EsServiceStack` likely have port data. ZCP should investigate during implementation and map if available.

### Estimated Lines

~900 lines (source is 1025, ZCP removes some unused code and adds `sync.Once` + `FailReason` mapping).

### Dependencies

```go
import (
    "context"
    "errors"
    "net"
    "net/http"
    "strings"
    "sync"

    "github.com/zeropsio/zerops-go/apiError"
    "github.com/zeropsio/zerops-go/dto/input/body"
    "github.com/zeropsio/zerops-go/dto/input/path"
    "github.com/zeropsio/zerops-go/dto/input/query"
    "github.com/zeropsio/zerops-go/dto/output"
    "github.com/zeropsio/zerops-go/sdk"
    "github.com/zeropsio/zerops-go/sdkBase"
    "github.com/zeropsio/zerops-go/types"
    "github.com/zeropsio/zerops-go/types/enum"
    "github.com/zeropsio/zerops-go/types/uuid"
)
```

---

## 7. platform/logfetcher.go

### ZeropsLogFetcher Implementation

Port from `logfetcher.go:1-157`.

```go
type ZeropsLogFetcher struct {
    httpClient *http.Client
}

func NewLogFetcher() *ZeropsLogFetcher  // logfetcher.go:26-32
var _ LogFetcher = (*ZeropsLogFetcher)(nil)  // compile-time check
```

### FetchLogs Logic

From `logfetcher.go:39-123`:
1. Validate `access != nil`.
2. Parse URL: strip `"GET "` prefix, add `"https://"` if no scheme.
3. Build query params: `serviceStackId`, `tail` (limit, default 100), `since` (RFC3339), `severity`, `search`.
4. HTTP GET with `Authorization: Bearer` header.
5. Error handling: `MapNetworkError()` for network errors, HTTP status check.
6. Parse JSON response via `parseLogResponse()`.
7. Sort chronologically.
8. Apply limit (tail behavior — return last N entries).

### Internal Types

From `logfetcher.go:126-157`:

```go
type logAPIResponse struct {
    Items []logAPIItem `json:"items"`
}

type logAPIItem struct {
    ID            string `json:"id"`
    Timestamp     string `json:"timestamp"`
    Hostname      string `json:"hostname"`
    Message       string `json:"message"`
    SeverityLabel string `json:"severityLabel"`
}
```

### Constants

```go
const (
    maxLogResponseBytes   = 50 << 20 // 50 MB
    maxErrorResponseBytes = 1 << 20  // 1 MB
)
```

### What to Port vs Write New

- **Direct port**: Entire file from `logfetcher.go`.

### Estimated Lines

~160 lines (matching source).

### Dependencies

```go
import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "sort"
    "strings"
    "time"
)
```

---

## 8. auth/auth.go

### Info Struct

New implementation (PRD §3.3). NOT a port of `auth.Credentials`:

```go
type Info struct {
    Token       string
    APIHost     string
    Region      string   // e.g. "prg1" — region metadata (diagnostics, future use)
    ClientID    string
    ProjectID   string
    ProjectName string
}
```

Source `manager.go:13-18` has `Credentials` with 5 fields (no `ClientID`). ZCP's `Info` adds `ClientID` (cached from GetUserInfo) and `Region`.

### Resolve() Function

New implementation (PRD §3.1-3.2). Reference patterns from `manager.go:49-136` and `storage.go:55-69`.

```go
func Resolve(ctx context.Context, client platform.Client) (*Info, error)
```

**Logic** (PRD §3.2):

1. **Check `ZCP_API_KEY` env var** (primary path):
   - Read `os.Getenv("ZCP_API_KEY")`.
   - If set → resolve API host: `ZCP_API_HOST` env var (default: `"api.app-prg1.zerops.io"`).
   - Resolve region: `ZCP_REGION` env var (default: `"prg1"`).
   - Proceed to validation.

2. **zcli fallback** (if `ZCP_API_KEY` is empty):
   - Read `cli.data` from default path (see below).
   - Extract `.Token`, `.RegionData.address`, `.RegionData.name`, `.ScopeProjectId`.
   - Resolve API host: `ZCP_API_HOST` → `RegionData.address` → default.
   - Resolve region: `ZCP_REGION` → `RegionData.name` → default.
   - If token empty → error.

3. **Validation** (both paths):
   - `client.GetUserInfo(ctx)` → get `clientID`.
   - Failure → `"Authentication failed: invalid or expired token"`.

4. **Project discovery**:
   - **zcli path with `ScopeProjectId` set** → `client.GetProject(ctx, scopeProjectId)`.
   - **Otherwise** → `client.ListProjects(ctx, clientID)`:
     - 0 projects → error: `"Token has no project access"`.
     - 2+ projects → error: `"Token accesses N projects; use project-scoped token"`.
     - 1 project → use it.

5. Return `Info{Token, APIHost, Region, ClientID, ProjectID, ProjectName}`.

### cli.data Reading Logic

Reference `storage.go:116-136` for path patterns. **Different schema** from zaia's `zaia.data`:

zcli's `cli.data` format (PRD §3.1):
```json
{
  "Token": "<PAT>",
  "RegionData": {
    "name": "prg1",
    "isDefault": true,
    "address": "api.app-prg1.zerops.io"
  },
  "ScopeProjectId": null
}
```

**Path resolution** (reference `storage.go:116-136` but for `cli.data`):
- macOS: `~/Library/Application Support/zerops/cli.data`
- Linux: `~/.config/zerops/cli.data` (or `$XDG_CONFIG_HOME/zerops/cli.data`)

Internal struct for parsing:
```go
type cliData struct {
    Token           string      `json:"Token"`
    RegionData      cliRegion   `json:"RegionData"`
    ScopeProjectId  *string     `json:"ScopeProjectId"`  // pointer: null → nil
}

type cliRegion struct {
    Name    string `json:"name"`
    Address string `json:"address"`
}
```

**Key PRD note** (§3.1): `ScopeProjectId` is JSON `null` when unset → Go `*string` will be `nil`.

### What to Port vs Write New

- **New implementation**: Everything. ZCP's auth is fundamentally different from zaia's — no Login/Logout, no file persistence, no Storage. It reads env var or cli.data read-only.
- **Reference only**: `manager.go:49-136` (project discovery pattern), `storage.go:116-136` (OS-specific config paths).

### Estimated Lines

~150 lines (Resolve function + cliData struct + path helpers).

### Dependencies

```go
import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "runtime"
    "strings"

    "github.com/zeropsio/zcp/internal/platform"
)
```

---

## 9. Test Cases per File

### platform/types.go — Tests

No tests needed. Pure data types with no logic (except `APIError.Error()` which is trivial).

### platform/errors.go — Tests

**File**: `errors_test.go`

| Test Function | Scenarios |
|--------------|-----------|
| `TestNewPlatformError_Fields` | Verify Code, Message, Suggestion populated. Error() returns Message. |
| `TestMapNetworkError_Detection` | `*net.OpError` → NETWORK_ERROR; `*net.DNSError` → NETWORK_ERROR; "connection refused" → NETWORK_ERROR; "context deadline exceeded" → API_TIMEOUT; nil → ("", false); random error → ("", false). |

### platform/client.go — Tests

No tests needed. Pure interface definition.

### platform/mock.go — Tests

**File**: `mock_test.go`

| Test Function | Scenarios |
|--------------|-----------|
| `TestMock_WithUserInfo_Success` | Configure UserInfo, call GetUserInfo → returns configured value. |
| `TestMock_WithError_Override` | Configure error for "GetUserInfo", call → returns error. |
| `TestMock_GetService_FallbackToList` | Configure services list, call GetService with matching ID → finds it. |
| `TestMock_GetService_NotFound` | No service configured, call GetService → error. |
| `TestMock_SetAutoscaling_NilProcess` | Call SetAutoscaling → returns nil, nil. |
| `TestMock_CancelProcess_StatusChange` | Add process, cancel → status is CANCELLED. |
| `TestMock_MockLogFetcher_Success` | Configure entries, call FetchLogs → returns entries. |
| `TestMock_MockLogFetcher_Error` | Configure error, call FetchLogs → returns error. |
| `TestMock_InterfaceCompliance` | Compile-time checks (already via var _ Client). |

### platform/zerops.go — Mock Tests

**File**: `zerops_test.go` (unit, no build tag)

| Test Function | Scenarios |
|--------------|-----------|
| `TestMapProcess_StatusNormalization` | "DONE" → "FINISHED"; "CANCELLED" → "CANCELED"; "RUNNING" stays; "FAILED" stays. |
| `TestMapProcess_FailReason` | Process with FailReason set → mapped. Process without → nil. |
| `TestMapProcess_OptionalFields` | Started nil → nil; Started set → mapped. Same for Finished. |
| `TestBuildAutoscalingBody_MinimalParams` | Only CpuMode set → vertical block created, no horizontal. |
| `TestBuildAutoscalingBody_AllParams` | All params set → both vertical and horizontal. |
| `TestBuildAutoscalingBody_EmptyParams` | No params → empty body. |
| `TestMapSDKError_NetworkErrors` | `*net.OpError` → NETWORK_ERROR; `context.DeadlineExceeded` → API_TIMEOUT; `context.Canceled` → API_ERROR. |
| `TestMapAPIError_StatusCodes` | 401 → AUTH_TOKEN_EXPIRED; 403 → PERMISSION_DENIED; 404 service → SERVICE_NOT_FOUND; 404 process → PROCESS_NOT_FOUND; 429 → API_RATE_LIMITED; 500 → API_ERROR. |
| `TestMapAPIError_SubdomainIdempotent` | "SubdomainAccessAlreadyEnabled" → SUBDOMAIN_ALREADY_ENABLED; "SubdomainAccessAlreadyDisabled" → SUBDOMAIN_ALREADY_DISABLED. |
| `TestNewZeropsClient_URLNormalization` | No scheme → adds https://; no trailing slash → adds /. |

### platform/zerops.go — API Contract Tests

**File**: `zerops_api_test.go` (`//go:build api`)

| Test Function | What It Verifies |
|--------------|------------------|
| `TestAPI_GetUserInfo` | Response non-nil, ID non-empty, Email non-empty. |
| `TestAPI_ListProjects` | Returns >= 1 project. ID, Name, Status non-empty. |
| `TestAPI_GetProject` | Single project retrieval. Fields match ListProjects result. |
| `TestAPI_ListServices` | Returns services for project. ID, Name, Status, ProjectID populated. ServiceStackTypeInfo.ServiceStackTypeVersionName non-empty. |
| `TestAPI_GetService` | Single service by ID. All fields populated. Matches ListServices entry. |
| `TestAPI_GetServiceEnv` | Returns env vars (may be empty slice, NOT nil). |
| `TestAPI_GetProjectEnv` | Returns project env vars. |
| `TestAPI_GetProjectLog` | Returns LogAccess with non-empty URL and AccessToken. |
| `TestAPI_SearchProcesses` | Returns process events (may be empty). If non-empty: ID, Status, ActionName populated. Status values are normalized (no "DONE" or "CANCELLED"). |
| `TestAPI_SearchAppVersions` | Returns app version events (may be empty). If non-empty: ID, Status, Source populated. |
| `TestAPI_GetProcess_NotFound` | Invalid process ID → PlatformError with code PROCESS_NOT_FOUND. |
| `TestAPI_InvalidToken` | Client with bad token → GetUserInfo fails with AUTH_TOKEN_EXPIRED. |

### platform/logfetcher.go — Tests

**File**: `logfetcher_test.go` (unit)

| Test Function | Scenarios |
|--------------|-----------|
| `TestParseLogResponse_ValidJSON` | Well-formed JSON → correct LogEntry fields. |
| `TestParseLogResponse_EmptyItems` | Empty items array → empty slice (not nil). |
| `TestParseLogResponse_InvalidJSON` | Malformed → error. |
| `TestFetchLogs_NilAccess` | nil access → PlatformError. |
| `TestFetchLogs_URLParsing` | "GET https://..." → strips prefix. "host:1234" → adds https://. |

**File**: `logfetcher_api_test.go` (`//go:build api`)

| Test Function | What It Verifies |
|--------------|------------------|
| `TestAPI_FetchLogs_RealBackend` | GetProjectLog → FetchLogs with real access. Returns entries (may be empty). If non-empty: Timestamp, Severity, Message populated. |

### auth/auth.go — Tests

**File**: `auth_test.go` (unit, uses platform.Mock)

| Test Function | Scenarios |
|--------------|-----------|
| `TestResolve_EnvVar_SingleProject` | ZCP_API_KEY set, 1 project → success with correct Info fields. |
| `TestResolve_EnvVar_NoProject` | ZCP_API_KEY set, 0 projects → TOKEN_NO_PROJECT error. |
| `TestResolve_EnvVar_MultiProject` | ZCP_API_KEY set, 2+ projects → TOKEN_MULTI_PROJECT error. |
| `TestResolve_EnvVar_InvalidToken` | ZCP_API_KEY set, GetUserInfo fails → AUTH error. |
| `TestResolve_EnvVar_CustomAPIHost` | ZCP_API_HOST set → uses custom host. |
| `TestResolve_EnvVar_CustomRegion` | ZCP_REGION set → uses custom region. |
| `TestResolve_ZcliFallback_Success` | No ZCP_API_KEY, valid cli.data file → reads token and region. |
| `TestResolve_ZcliFallback_ScopeProject` | cli.data has ScopeProjectId → calls GetProject instead of ListProjects. |
| `TestResolve_ZcliFallback_NullScope` | cli.data has ScopeProjectId: null → calls ListProjects. |
| `TestResolve_NoAuth` | No ZCP_API_KEY, no cli.data → AUTH_REQUIRED error. |

**File**: `auth_api_test.go` (`//go:build api`)

| Test Function | What It Verifies |
|--------------|------------------|
| `TestAPI_Resolve_RealToken` | Full resolve with ZCP_API_KEY → Info populated. ClientID, ProjectID, ProjectName all non-empty. |

---

## 10. Phase 1 Gate Criteria

Per PRD §13 (Phase 1 gate):

### Must Pass

```bash
go test ./internal/platform/... -tags api -v
go test ./internal/auth/... -tags api -v
```

### Specific Verifications

1. **All Client interface methods have passing contract tests** — every method in the 26-method interface verified against real Zerops API.
2. **LogFetcher contract test passes** — real log fetch completes (may return empty entries).
3. **Auth flow verified** — `Resolve()` with real `ZCP_API_KEY` produces valid `Info`.
4. **Domain type field mappings confirmed** — no zero-value fields where API returns data. Pointer fields nil/non-nil as expected.
5. **Status normalization verified** — real API never returns raw "DONE" or "CANCELLED" after mapping.
6. **Error mapping verified** — invalid token produces correct `PlatformError` code.
7. **Mock tests pass** — `go test ./internal/platform/... ./internal/auth/... -short -count=1`.

### Must NOT Proceed Until

- Any Client method contract test fails (even one).
- `FailReason` is not mapped in `mapProcess()`.
- `sync.Once` is not used for `getClientID()`.
- Mock's `CreateProjectEnv` signature doesn't match interface.

---

## 11. Implementation Order Within Phase 1

Following PRD §13:

```
1. types.go        — pure data, no deps, no tests needed
2. errors.go       — pure data + 1 helper, minimal tests
3. client.go       — interface only, no tests
4. mock.go         — needs types + interface, test with mock_test.go
5. apitest/harness.go — needs ZeropsClient (can stub initially)
6. zerops.go       — SDK implementation, each method gets:
   a. zerops_test.go (unit — mappers, error handling)
   b. zerops_api_test.go (contract — real API)
7. logfetcher.go   — HTTP implementation + tests
8. auth/auth.go    — Resolve() + tests
```

Items 1-4 can be committed as one unit. Items 5-8 each produce testable increments.

---

## 12. Source Divergences Log

Decisions where ZCP intentionally diverges from source:

| Source | ZCP | Reason |
|--------|-----|--------|
| `zerops.go:57-67` racy getClientID | `sync.Once` | PRD §4.3 thread safety |
| `zerops.go:645-684` omits FailReason | Map FailReason | PRD §5.4 requires it in tool responses |
| `mock.go:299` wrong CreateProjectEnv signature | Match interface | Bug fix |
| `errors.go:39-42` setup error codes | Skip | PRD §4.4: CLI-specific |
| `errors.go:46-62` MapHTTPError | Skip (use mapAPIError) | CLI-specific, suggestion references "zaia login" |
| `errors.go:96-117` ExitCodeForError | Skip | CLI exit codes irrelevant for MCP |
| `errors.go:119-127` HTTPError struct | Skip | CLI-specific |
| `zerops.go:995` "Run: zaia login" suggestion | "Check token validity" | No CLI login in ZCP |
| `mock.go:368` CancelProcess sets CANCELLED | Keep CANCELLED (raw) | Mock does not normalize; ZeropsClient normalizes in mapProcess |
| Types in `client.go` | Separate `types.go` | PRD §4.2: new file split for clarity |
