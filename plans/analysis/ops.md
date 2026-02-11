# Phase 2 Analysis: Ops Layer + Deploy

> Source references: PRD `design/zcp-prd.md` sections 5, 6, 8.
> Platform client: `../zaia/internal/platform/client.go` (interface + types).
> Business logic: `../zaia/internal/commands/` (discover, manage, env, logs, importcmd, validate, delete, subdomain, events, process, cancel, helpers).
> MCP tool handlers: `../zaia-mcp/internal/tools/` (all handler files).

---

## ops/helpers.go

### Functions

```go
// resolveServiceID resolves a service hostname to its ID.
// Requires a pre-fetched service list (avoids repeated API calls).
func resolveServiceID(
    services []platform.ServiceStack,
    projectID string,
    hostname string,
) (*platform.ServiceStack, error)

// findServiceByHostname scans a slice for matching hostname.
func findServiceByHostname(
    services []platform.ServiceStack,
    hostname string,
) *platform.ServiceStack

// listHostnames returns comma-separated hostnames for error messages.
func listHostnames(services []platform.ServiceStack) string

// parseSince converts user-friendly time strings to time.Time.
// Supports: "30m", "1h", "24h", "7d", ISO 8601 (RFC3339).
// Empty string defaults to 1 hour ago.
func parseSince(s string) (time.Time, error)

// parseEnvPairs splits "KEY=value" strings into key/value pairs.
// Splits on first '=' only (value may contain '=').
func parseEnvPairs(vars []string) ([]envPair, error)

// findEnvIDByKey finds an env var ID by key name.
func findEnvIDByKey(envs []platform.EnvVar, key string) string
```

### Source Reference

- `../zaia/internal/commands/helpers.go:27-65` — `findServiceByHostname`, `listHostnames`, `resolveServiceID`
- `../zaia/internal/commands/logs.go:105-145` — `parseSince` (regex-based duration parser)
- `../zaia/internal/commands/env.go:266-310` — `parseEnvPairs`, `findEnvIDByKey`, `listEnvKeys`

### Key Decisions

- **resolveServiceID** takes pre-fetched `[]ServiceStack` (not client) to avoid coupling ops→client for service lookup. The caller (each ops function) fetches services then passes them in.
- **parseSince** validates ranges: minutes 1-1440, hours 1-168, days 1-30. Falls back to `time.Parse(time.RFC3339, s)`.
- **Hostname validation**: ZCP should NOT validate hostname format (no regex for valid chars) — just match against the service list. If not found, return `SERVICE_NOT_FOUND` with available hostnames as suggestion.

### Test Cases

| Test | Input | Expected |
|------|-------|----------|
| `TestResolveServiceID_Found` | hostname="api", services=[api, db] | returns &api service |
| `TestResolveServiceID_NotFound` | hostname="missing", services=[api, db] | PlatformError SERVICE_NOT_FOUND |
| `TestResolveServiceID_EmptyList` | hostname="api", services=[] | PlatformError SERVICE_NOT_FOUND |
| `TestParseSince_Minutes` | "30m" | now - 30min |
| `TestParseSince_Hours` | "1h" | now - 1h |
| `TestParseSince_Days` | "7d" | now - 7d |
| `TestParseSince_ISO8601` | "2024-01-01T00:00:00Z" | parsed time |
| `TestParseSince_Empty` | "" | now - 1h (default) |
| `TestParseSince_Invalid` | "abc" | error |
| `TestParseSince_OutOfRange` | "200h" | error (hours 1-168) |
| `TestParseEnvPairs_Valid` | ["KEY=value", "K2=v=2"] | [{KEY,value},{K2,v=2}] |
| `TestParseEnvPairs_NoEquals` | ["NOVALUE"] | PlatformError INVALID_ENV_FORMAT |
| `TestParseEnvPairs_EmptyKey` | ["=value"] | PlatformError INVALID_ENV_FORMAT |

---

## ops/discover.go

### Function Signature

```go
type DiscoverResult struct {
    Project  ProjectInfo    `json:"project"`
    Services []ServiceInfo  `json:"services"`
}

type ProjectInfo struct {
    ID     string `json:"id"`
    Name   string `json:"name"`
    Status string `json:"status"`
}

type ServiceInfo struct {
    Hostname  string                 `json:"hostname"`
    ServiceID string                 `json:"serviceId"`
    Type      string                 `json:"type"`
    Status    string                 `json:"status"`
    Created   string                 `json:"created,omitempty"`
    Containers map[string]interface{} `json:"containers,omitempty"`
    Resources  map[string]interface{} `json:"resources,omitempty"`
    Ports      []map[string]interface{} `json:"ports,omitempty"`
    Envs       []map[string]interface{} `json:"envs,omitempty"`
}

func Discover(
    ctx context.Context,
    client platform.Client,
    projectID string,
    hostname string,      // optional: filter to single service
    includeEnvs bool,     // optional: include env vars per service
) (*DiscoverResult, error)
```

### Platform.Client Methods Called

1. `client.GetProject(ctx, projectID)` — get project info
2. `client.ListServices(ctx, projectID)` — get all services
3. `client.GetServiceEnv(ctx, serviceID)` — per service, only if `includeEnvs=true`

### Logic

1. Fetch project info via `GetProject`.
2. Fetch all services via `ListServices`.
3. If `hostname != ""`: filter to matching service via `findServiceByHostname`. If not found, return `SERVICE_NOT_FOUND`. Build detailed view (containers, resources, ports).
4. If `hostname == ""`: build summary list of all services.
5. If `includeEnvs`: for each service in result, call `GetServiceEnv` and attach. Errors silently ignored (env fetch failure doesn't fail the whole discover).

### Source Reference

- `../zaia/internal/commands/discover.go:12-148` — full discover logic
- `../zaia-mcp/internal/tools/discover.go:10-49` — tool handler (CLI passthrough)

### Test Cases

| Test | Mock Setup | Expected |
|------|-----------|----------|
| `TestDiscover_AllServices` | 3 services, no filter | result with 3 services |
| `TestDiscover_SingleService_Found` | 3 services, hostname="api" | result with 1 detailed service |
| `TestDiscover_SingleService_NotFound` | 3 services, hostname="missing" | PlatformError SERVICE_NOT_FOUND |
| `TestDiscover_WithEnvs` | 2 services, includeEnvs=true | envs populated per service |
| `TestDiscover_EnvFetchError_Graceful` | service exists, GetServiceEnv errors | service returned without envs |
| `TestDiscover_ProjectNotFound` | GetProject returns error | PlatformError propagated |
| API: `TestAPI_Discover_AllServices` | real client | result with services, non-empty list |
| API: `TestAPI_Discover_WithEnvs` | real client | envs populated |

---

## ops/manage.go

### Function Signatures

```go
func Start(ctx context.Context, client platform.Client, projectID, hostname string) (*platform.Process, error)
func Stop(ctx context.Context, client platform.Client, projectID, hostname string) (*platform.Process, error)
func Restart(ctx context.Context, client platform.Client, projectID, hostname string) (*platform.Process, error)

type ScaleResult struct {
    Process  *platform.Process      `json:"process,omitempty"`
    Message  string                 `json:"message,omitempty"`
    Hostname string                 `json:"serviceHostname"`
    ServiceID string               `json:"serviceId"`
}

func Scale(
    ctx context.Context,
    client platform.Client,
    projectID string,
    hostname string,
    params ScaleParams,
) (*ScaleResult, error)
```

### ScaleParams Mapping (PRD section 5.1)

```go
type ScaleParams struct {
    CPUMode         string  // → AutoscalingParams.VerticalCpuMode
    MinCPU          int     // → AutoscalingParams.VerticalMinCpu
    MaxCPU          int     // → AutoscalingParams.VerticalMaxCpu
    MinRAM          float64 // → AutoscalingParams.VerticalMinRam
    MaxRAM          float64 // → AutoscalingParams.VerticalMaxRam
    MinDisk         float64 // → AutoscalingParams.VerticalMinDisk
    MaxDisk         float64 // → AutoscalingParams.VerticalMaxDisk
    StartContainers int     // → (initial container count, not mapped to AutoscalingParams)
    MinContainers   int     // → AutoscalingParams.HorizontalMinCount
    MaxContainers   int     // → AutoscalingParams.HorizontalMaxCount
}
```

### Platform.Client Methods Called

| Action | Method |
|--------|--------|
| start | `client.StartService(ctx, serviceID)` |
| stop | `client.StopService(ctx, serviceID)` |
| restart | `client.RestartService(ctx, serviceID)` |
| scale | `client.SetAutoscaling(ctx, serviceID, params)` |

All lifecycle actions share the same pattern:
1. `client.ListServices(ctx, projectID)` — fetch service list
2. `resolveServiceID(services, projectID, hostname)` — resolve hostname
3. Call the appropriate lifecycle method

### Validation Logic

- **Action validation**: done at tool layer, not ops. Ops functions are per-action (Start, Stop, Restart, Scale).
- **Scale params**: at least one param must be non-zero (source: `../zaia/internal/commands/manage.go:210-214`).
- **Min/max pairs**: `minCpu <= maxCpu`, `minRam <= maxRam`, `minDisk <= maxDisk`, `minContainers <= maxContainers` (source: `../zaia/internal/commands/manage.go:217-236`).
- **CPUMode**: must be "SHARED" or "DEDICATED" if provided (source: `../zaia/internal/commands/manage.go:160-163`).
- **SetAutoscaling nil Process**: API may return nil process (sync, immediate). In that case, return ScaleResult with message "Scaling parameters updated" and no process (source: `../zaia/internal/commands/manage.go:125-133`).

### Source Reference

- `../zaia/internal/commands/manage.go:17-239` — lifecycle commands + scaling param parsing
- `../zaia/internal/platform/zerops.go:247-265` — SetAutoscaling with nil process handling
- `../zaia/internal/platform/zerops.go:776-856` — buildAutoscalingBody (SDK type mapping)

### Test Cases

| Test | Mock Setup | Expected |
|------|-----------|----------|
| `TestStart_Success` | service found | Process returned |
| `TestStop_Success` | service found | Process returned |
| `TestRestart_Success` | service found | Process returned |
| `TestStart_ServiceNotFound` | hostname not in list | PlatformError SERVICE_NOT_FOUND |
| `TestScale_AllParams` | valid params | Process returned |
| `TestScale_NilProcess` | SetAutoscaling returns nil | ScaleResult with message |
| `TestScale_NoParams` | all zero | PlatformError INVALID_SCALING |
| `TestScale_MinGtMax` | minCpu=4, maxCpu=2 | PlatformError INVALID_SCALING |
| `TestScale_InvalidCPUMode` | cpuMode="INVALID" | PlatformError INVALID_SCALING |
| API: `TestAPI_Restart_RunningService` | real client | Process with valid ID |

---

## ops/env.go

### Function Signatures

```go
type EnvGetResult struct {
    Scope    string                   `json:"scope"`    // "service" or "project"
    Hostname string                   `json:"serviceHostname,omitempty"`
    Vars     []map[string]interface{} `json:"vars"`
}

func EnvGet(
    ctx context.Context,
    client platform.Client,
    projectID string,
    hostname string,   // service scope (mutually exclusive with isProject)
    isProject bool,    // project scope
) (*EnvGetResult, error)

type EnvSetResult struct {
    Process *platform.Process `json:"process,omitempty"`
}

func EnvSet(
    ctx context.Context,
    client platform.Client,
    projectID string,
    hostname string,
    isProject bool,
    variables []string,  // ["KEY=value", ...]
) (*EnvSetResult, error)

type EnvDeleteResult struct {
    Process *platform.Process `json:"process,omitempty"`
}

func EnvDelete(
    ctx context.Context,
    client platform.Client,
    projectID string,
    hostname string,
    isProject bool,
    variables []string,  // ["KEY", ...]
) (*EnvDeleteResult, error)
```

### Platform.Client Methods Called

| Operation | Scope | Methods |
|-----------|-------|---------|
| Get | service | `ListServices` → `resolveServiceID` → `GetServiceEnv(serviceID)` |
| Get | project | `GetProjectEnv(projectID)` |
| Set | service | `ListServices` → `resolveServiceID` → `SetServiceEnvFile(serviceID, content)` |
| Set | project | `CreateProjectEnv(projectID, key, content, sensitive)` per pair |
| Delete | service | `ListServices` → `resolveServiceID` → `GetServiceEnv(serviceID)` → `findEnvIDByKey` → `DeleteUserData(userDataID)` per key |
| Delete | project | `GetProjectEnv(projectID)` → `findEnvIDByKey` → `DeleteProjectEnv(envID)` per key |

### KEY=value Parsing Logic

- Split on first `=` only (value may contain `=`).
- Empty key is an error (`INVALID_ENV_FORMAT`).
- Empty value is valid (`KEY=` sets key to empty string).
- Source: `../zaia/internal/commands/env.go:271-290`

### Service Env Set Content Format

For service env set, pairs are joined into `.env` format: `KEY=value\n` per line, passed to `SetServiceEnvFile` as a single string (source: `../zaia/internal/commands/env.go:153-158`).

### Validation Logic

- Either `hostname` or `isProject=true` must be provided (not both, not neither).
- For delete: if key not found in current env vars, return error (source: `../zaia/internal/commands/env.go:199-201`, `243-244`).

### Source Reference

- `../zaia/internal/commands/env.go:13-310` — full env command logic
- `../zaia-mcp/internal/tools/env.go:10-63` — tool handler

### Test Cases

| Test | Mock Setup | Expected |
|------|-----------|----------|
| `TestEnvGet_Service` | service with envs | vars populated |
| `TestEnvGet_Project` | project envs | vars populated |
| `TestEnvGet_NoScope` | neither service nor project | error |
| `TestEnvSet_Service` | service exists | Process returned |
| `TestEnvSet_Project` | project scope | CreateProjectEnv called per pair |
| `TestEnvSet_InvalidFormat` | ["NOEQUALS"] | PlatformError INVALID_ENV_FORMAT |
| `TestEnvDelete_Service_Found` | env "DB_HOST" exists | Process returned |
| `TestEnvDelete_Service_NotFound` | env "MISSING" not in list | error "not found" |
| `TestEnvDelete_Project` | project env exists | DeleteProjectEnv called |
| API: `TestAPI_EnvGet_Service` | real client | vars returned |
| API: `TestAPI_EnvSet_Delete_Cycle` | set then delete | round-trip works |

---

## ops/logs.go

### Function Signature

```go
type LogsResult struct {
    Entries []LogEntryOutput `json:"entries"`
    HasMore bool             `json:"hasMore"`
}

type LogEntryOutput struct {
    Timestamp string `json:"timestamp"`
    Severity  string `json:"severity"`
    Message   string `json:"message"`
    Container string `json:"container,omitempty"`
}

func FetchLogs(
    ctx context.Context,
    client platform.Client,
    fetcher platform.LogFetcher,
    projectID string,
    hostname string,
    severity string,   // error, warning, info, debug, all
    since string,      // "30m", "1h", "24h", "7d", ISO 8601
    limit int,         // default 100
    search string,
) (*LogsResult, error)
```

### 2-Step Fetch Pattern

1. `client.ListServices(ctx, projectID)` → resolve hostname to serviceID
2. `client.GetProjectLog(ctx, projectID)` → get `*LogAccess` (temporary credentials + URL)
3. `fetcher.FetchLogs(ctx, logAccess, LogFetchParams{ServiceID, Severity, Since, Limit, Search})` → `[]LogEntry`

### LogFetchParams Construction

```
Tool "since" string  →  parseSince()  →  LogFetchParams.Since (time.Time)
Tool "severity"      →  pass-through   →  LogFetchParams.Severity
Tool "limit"         →  default to 100 →  LogFetchParams.Limit
Tool "search"        →  pass-through   →  LogFetchParams.Search
Tool "serviceHostname" → resolveServiceID → LogFetchParams.ServiceID
```

### Source Reference

- `../zaia/internal/commands/logs.go:15-145` — logs command with parseSince
- `../zaia/internal/platform/logfetcher.go:1-157` — ZeropsLogFetcher HTTP implementation
- `../zaia-mcp/internal/tools/logs.go:10-68` — tool handler with buildId param

### Note on buildId

The current zaia-mcp tool exposes `buildId` parameter, but the zaia commands/logs.go has a `--build` flag that is declared but not actually used in the fetch logic. The platform `LogFetchParams` does not have a BuildID field. This parameter may need investigation during implementation to determine if it maps to a different log endpoint or query parameter.

### Test Cases

| Test | Mock Setup | Expected |
|------|-----------|----------|
| `TestFetchLogs_Success` | mock client + mock fetcher | entries returned |
| `TestFetchLogs_ServiceNotFound` | hostname not in services | PlatformError |
| `TestFetchLogs_EmptyResult` | fetcher returns [] | empty entries, hasMore=false |
| `TestFetchLogs_HasMore` | entries.len >= limit | hasMore=true |
| `TestFetchLogs_InvalidSince` | since="badformat" | PlatformError INVALID_PARAMETER |
| `TestFetchLogs_DefaultLimit` | limit=0 | defaults to 100 |
| API: `TestAPI_FetchLogs` | real client + real fetcher | entries (may be empty) |

---

## ops/import.go

### Function Signature

```go
type ImportDryRunResult struct {
    DryRun   bool                     `json:"dryRun"`
    Valid    bool                     `json:"valid"`
    Services []map[string]interface{} `json:"services"`
    Warnings []string                 `json:"warnings"`
}

type ImportRealResult struct {
    ProjectID   string                    `json:"projectId"`
    ProjectName string                    `json:"projectName"`
    Processes   []ImportProcessOutput     `json:"processes"`
}

type ImportProcessOutput struct {
    ProcessID  string `json:"processId"`
    ActionName string `json:"actionName"`
    Status     string `json:"status"`
    Service    string `json:"service"`
    ServiceID  string `json:"serviceId"`
}

func Import(
    ctx context.Context,
    client platform.Client,
    projectID string,
    content string,    // inline YAML (mutually exclusive with filePath)
    filePath string,   // path to YAML file
    dryRun bool,
) (interface{}, error)  // returns *ImportDryRunResult or *ImportRealResult
```

### Logic

1. **Input resolution**: either `content` or `filePath` (not both, not neither).
2. If `filePath`: read file via `os.ReadFile`. If not found, return `FILE_NOT_FOUND`.
3. **Project section check**: parse YAML, check for `project:` key. If present, return `IMPORT_HAS_PROJECT`.
4. **Dry run** (sync): parse YAML, extract `services[]`, build preview with hostname + type + action="create". Return `ImportDryRunResult`.
5. **Real import** (async): `client.ImportServices(ctx, projectID, yamlContent)` → `ImportResult`. Extract processes from nested `ServiceStacks[].Processes[]`.

### Platform.Client Methods Called

- `client.ImportServices(ctx, projectID, yamlContent)` — real import only

### Source Reference

- `../zaia/internal/commands/importcmd.go:13-144` — import command + dry run logic
- `../zaia-mcp/internal/tools/import.go:10-65` — tool handler
- `../zaia/internal/platform/zerops.go:407-442` — ImportServices SDK mapping

### Test Cases

| Test | Mock Setup | Expected |
|------|-----------|----------|
| `TestImport_DryRun_Valid` | valid YAML with 2 services | preview with 2 entries |
| `TestImport_DryRun_InvalidYAML` | bad YAML | PlatformError INVALID_IMPORT_YML |
| `TestImport_DryRun_MissingServices` | YAML without services: | PlatformError INVALID_IMPORT_YML |
| `TestImport_DryRun_HasProjectSection` | YAML with project: | PlatformError IMPORT_HAS_PROJECT |
| `TestImport_Real_Success` | mock returns ImportResult | processes extracted |
| `TestImport_NoInput` | empty content and empty filePath | error |
| `TestImport_BothInputs` | both content and filePath | error |
| `TestImport_FileNotFound` | filePath to nonexistent file | PlatformError FILE_NOT_FOUND |
| API: `TestAPI_Import_DryRun` | real client, valid YAML, dryRun=true | dry run passes (no resources) |

---

## ops/validate.go

### Function Signature

```go
type ValidateResult struct {
    Valid    bool     `json:"valid"`
    File     string   `json:"file"`
    Type     string   `json:"type"`    // "zerops.yml" or "import.yml"
    Errors   []ValidationError `json:"errors,omitempty"`
    Warnings []string `json:"warnings"`
    Info     []string `json:"info"`
}

type ValidationError struct {
    Path  string `json:"path"`
    Error string `json:"error"`
    Fix   string `json:"fix"`
}

func Validate(
    content string,
    filePath string,
    fileType string,   // "zerops.yml", "import.yml", or "" (auto-detect)
) (*ValidateResult, error)
```

### Validation Rules

**zerops.yml:**
1. Valid YAML syntax
2. Must have `zerops` top-level key
3. `zerops` must be an array
4. Array must not be empty

**import.yml:**
1. Valid YAML syntax
2. Must NOT have `project:` section (`IMPORT_HAS_PROJECT`)
3. Must have `services` top-level key

**Type detection** (when `fileType` is empty):
1. If source filename contains "import" → import.yml
2. If YAML has `services:` key → import.yml
3. If YAML has `zerops:` key → zerops.yml
4. Default → zerops.yml

### No API Needed

This is pure offline validation. No `platform.Client` dependency.

### Source Reference

- `../zaia/internal/commands/validate.go:17-209` — full validate logic
- `../zaia-mcp/internal/tools/validate.go:10-57` — tool handler

### Test Cases

| Test | Input | Expected |
|------|-------|----------|
| `TestValidate_ZeropsYml_Valid` | valid zerops.yml | valid=true |
| `TestValidate_ZeropsYml_MissingKey` | YAML without `zerops:` | valid=false, error |
| `TestValidate_ZeropsYml_EmptyArray` | `zerops: []` | valid=false, error |
| `TestValidate_ZeropsYml_BadSyntax` | invalid YAML | INVALID_ZEROPS_YML |
| `TestValidate_ImportYml_Valid` | valid import.yml | valid=true |
| `TestValidate_ImportYml_HasProject` | YAML with `project:` | IMPORT_HAS_PROJECT |
| `TestValidate_ImportYml_MissingServices` | YAML without `services:` | valid=false, error |
| `TestValidate_AutoDetect_Import` | YAML with `services:` key | type=import.yml |
| `TestValidate_AutoDetect_Zerops` | YAML with `zerops:` key | type=zerops.yml |
| `TestValidate_FileRead` | filePath=valid file | reads and validates |
| `TestValidate_FileNotFound` | filePath=nonexistent | FILE_NOT_FOUND |

---

## ops/delete.go

### Function Signature

```go
func Delete(
    ctx context.Context,
    client platform.Client,
    projectID string,
    hostname string,
    confirm bool,
) (*platform.Process, error)
```

### Logic

1. **Safety gate**: if `!confirm`, return `PlatformError{Code: CONFIRM_REQUIRED}`.
2. Validate hostname is not empty.
3. `client.ListServices(ctx, projectID)` → fetch services.
4. `resolveServiceID(services, projectID, hostname)` → get service.
5. `client.DeleteService(ctx, serviceID)` → returns Process.

### Platform.Client Methods Called

- `client.ListServices(ctx, projectID)`
- `client.DeleteService(ctx, serviceID)`

### Source Reference

- `../zaia/internal/commands/delete.go:12-70` — delete command
- `../zaia-mcp/internal/tools/delete.go:10-51` — tool handler

### Test Cases

| Test | Mock Setup | Expected |
|------|-----------|----------|
| `TestDelete_Success` | service exists, confirm=true | Process returned |
| `TestDelete_NoConfirm` | confirm=false | PlatformError CONFIRM_REQUIRED |
| `TestDelete_ServiceNotFound` | hostname not in list | PlatformError SERVICE_NOT_FOUND |
| `TestDelete_EmptyHostname` | hostname="" | PlatformError SERVICE_REQUIRED |
| API: `TestAPI_Delete_Service` | create then delete | Process returned, service gone |

---

## ops/subdomain.go

### Function Signature

```go
type SubdomainResult struct {
    Process  *platform.Process `json:"process,omitempty"`
    Hostname string            `json:"serviceHostname"`
    ServiceID string           `json:"serviceId"`
    Action   string            `json:"action"`
    Status   string            `json:"status,omitempty"` // "already_enabled", "already_disabled"
}

func Subdomain(
    ctx context.Context,
    client platform.Client,
    projectID string,
    hostname string,
    action string,   // "enable" or "disable"
) (*SubdomainResult, error)
```

### Idempotent Handling

The `platform.Client` methods `EnableSubdomainAccess` / `DisableSubdomainAccess` may return `PlatformError` with codes:
- `SUBDOMAIN_ALREADY_ENABLED` — treat as success, return `status: "already_enabled"`
- `SUBDOMAIN_ALREADY_DISABLED` — treat as success, return `status: "already_disabled"`

These error codes are generated dynamically in `mapAPIError()` in `../zaia/internal/platform/zerops.go:1010-1017`.

The ops layer catches these specific `PlatformError` codes and converts them to success results (source: `../zaia/internal/commands/subdomain.go:71-88`).

### Platform.Client Methods Called

- `client.ListServices(ctx, projectID)` — resolve hostname
- `client.EnableSubdomainAccess(ctx, serviceID)` or `client.DisableSubdomainAccess(ctx, serviceID)`

### Source Reference

- `../zaia/internal/commands/subdomain.go:15-103` — subdomain command with idempotent handling
- `../zaia-mcp/internal/tools/subdomain.go:10-48` — tool handler
- `../zaia/internal/platform/zerops.go:1010-1017` — `SUBDOMAIN_ALREADY_ENABLED/DISABLED` error mapping

### Test Cases

| Test | Mock Setup | Expected |
|------|-----------|----------|
| `TestSubdomain_Enable_Success` | service exists | Process returned |
| `TestSubdomain_Disable_Success` | service exists | Process returned |
| `TestSubdomain_Enable_AlreadyEnabled` | EnableSubdomainAccess → SUBDOMAIN_ALREADY_ENABLED | status="already_enabled", no error |
| `TestSubdomain_Disable_AlreadyDisabled` | DisableSubdomainAccess → SUBDOMAIN_ALREADY_DISABLED | status="already_disabled", no error |
| `TestSubdomain_InvalidAction` | action="toggle" | error |
| `TestSubdomain_ServiceNotFound` | hostname not in list | PlatformError SERVICE_NOT_FOUND |
| API: `TestAPI_Subdomain_EnableDisable` | real client | enable then disable works |

---

## ops/events.go

### Function Signature

```go
type EventsResult struct {
    ProjectID string          `json:"projectId"`
    Events    []TimelineEvent `json:"events"`
    Summary   EventsSummary   `json:"summary"`
}

type EventsSummary struct {
    Total     int `json:"total"`
    Processes int `json:"processes"`
    Deploys   int `json:"deploys"`
}

type TimelineEvent struct {
    Timestamp   string `json:"timestamp"`
    Type        string `json:"type"`        // "process", "build", "deploy"
    Action      string `json:"action"`      // "start", "stop", "restart", "scale", etc.
    Status      string `json:"status"`
    Service     string `json:"service"`     // hostname
    ServiceType string `json:"serviceType,omitempty"`
    Detail      string `json:"detail,omitempty"`
    Duration    string `json:"duration,omitempty"`
    User        string `json:"user,omitempty"`
    ProcessID   string `json:"processId,omitempty"`
}

func Events(
    ctx context.Context,
    client platform.Client,
    projectID string,
    serviceHostname string,  // optional filter
    limit int,               // default 50
) (*EventsResult, error)
```

### Merged Timeline: Processes + AppVersions

1. **Parallel fetch** (3 goroutines):
   - `client.SearchProcesses(ctx, projectID, limit)` → process events
   - `client.SearchAppVersions(ctx, projectID, limit)` → build/deploy events
   - `client.ListServices(ctx, projectID)` → for serviceID→hostname mapping
2. **Build serviceID→hostname map** from services list.
3. **Map process events**: use `actionNameMap` to normalize action names (e.g., `serviceStackStart` → `start`).
4. **Map appVersion events**: detect build vs deploy based on `Build.PipelineStart != nil`.
5. **Filter by service** if `serviceHostname` provided.
6. **Sort by timestamp descending**.
7. **Trim to limit**.

### Action Name Map

```
serviceStackStart                  → start
serviceStackStop                   → stop
serviceStackRestart                → restart
serviceStackAutoscaling            → scale
serviceStackImport                 → import
serviceStackDelete                 → delete
serviceStackUserDataFile           → env-update
serviceStackEnableSubdomainAccess  → subdomain-enable
serviceStackDisableSubdomainAccess → subdomain-disable
```
Source: `../zaia/internal/commands/events.go:229-239`

### Duration Calculation

`calcDuration(started, finished *string) string` — parses RFC3339 timestamps, returns human-readable duration (e.g., "5s", "2m30s", "1h15m"). Source: `../zaia/internal/commands/events.go:260-283`.

### Platform.Client Methods Called

- `client.SearchProcesses(ctx, projectID, limit)`
- `client.SearchAppVersions(ctx, projectID, limit)`
- `client.ListServices(ctx, projectID)` — for hostname resolution

### Source Reference

- `../zaia/internal/commands/events.go:14-283` — full events logic with parallel fetch, timeline merge
- `../zaia-mcp/internal/tools/events.go:10-53` — tool handler

### Test Cases

| Test | Mock Setup | Expected |
|------|-----------|----------|
| `TestEvents_MergedTimeline` | 3 processes + 2 appVersions | 5 events sorted desc |
| `TestEvents_FilterByService` | mixed services, filter="api" | only api events |
| `TestEvents_LimitApplied` | 10 events, limit=3 | 3 events returned |
| `TestEvents_EmptyResult` | no events | empty events, summary all zeros |
| `TestEvents_ActionNameMapping` | process with `serviceStackStart` | action="start" |
| `TestEvents_DurationCalculation` | started + finished times | human-readable duration |
| `TestEvents_ParallelFetchError` | SearchProcesses fails | error propagated |
| API: `TestAPI_Events_Timeline` | real client | events returned, properly sorted |

---

## ops/process.go

### Function Signatures

```go
type ProcessStatusResult struct {
    ProcessID  string  `json:"processId"`
    Action     string  `json:"actionName"`
    Status     string  `json:"status"`
    Created    string  `json:"created"`
    Started    *string `json:"started,omitempty"`
    Finished   *string `json:"finished,omitempty"`
    FailReason *string `json:"failReason,omitempty"`
}

func GetProcessStatus(
    ctx context.Context,
    client platform.Client,
    processID string,
) (*ProcessStatusResult, error)

type ProcessCancelResult struct {
    ProcessID string `json:"processId"`
    Status    string `json:"status"`
    Message   string `json:"message"`
}

func CancelProcess(
    ctx context.Context,
    client platform.Client,
    processID string,
) (*ProcessCancelResult, error)
```

### Logic

**GetProcessStatus:**
1. Validate processID is non-empty.
2. `client.GetProcess(ctx, processID)` → returns `*Process`.
3. If error → `PROCESS_NOT_FOUND`.
4. Map to `ProcessStatusResult` (include FailReason from Process).

**CancelProcess:**
1. Validate processID.
2. `client.GetProcess(ctx, processID)` → check current status.
3. If already terminal (FINISHED, FAILED, CANCELED) → return `PROCESS_ALREADY_TERMINAL`.
4. `client.CancelProcess(ctx, processID)` → cancel.
5. Return `ProcessCancelResult{status: "CANCELED"}`.

### Status Normalization

Status values are already normalized by `platform.ZeropsClient.mapProcess()`:
- `DONE` → `FINISHED`
- `CANCELLED` → `CANCELED`

The ops layer receives normalized values. No additional normalization needed.

### FailReason Gap

**IMPORTANT**: The source `mapProcess()` in `../zaia/internal/platform/zerops.go:645-684` does NOT map `FailReason` from the SDK response. The PRD (section 4.2, 5.4) explicitly requires `Process.FailReason (*string)` to be populated. ZCP's `mapProcess` MUST add FailReason extraction from the SDK's `output.Process` type. This is a known gap that needs to be fixed in the platform layer (Phase 1), not the ops layer.

### Platform.Client Methods Called

- `client.GetProcess(ctx, processID)`
- `client.CancelProcess(ctx, processID)`

### Source Reference

- `../zaia/internal/commands/process.go:12-37` — process status
- `../zaia/internal/commands/cancel.go:12-55` — cancel with terminal state check

### Test Cases

| Test | Mock Setup | Expected |
|------|-----------|----------|
| `TestGetProcessStatus_Success` | process exists, RUNNING | status fields populated |
| `TestGetProcessStatus_Failed_WithReason` | FAILED + FailReason | failReason in result |
| `TestGetProcessStatus_NotFound` | GetProcess returns error | PROCESS_NOT_FOUND |
| `TestCancelProcess_Success` | process RUNNING | CANCELED result |
| `TestCancelProcess_AlreadyTerminal` | process FINISHED | PROCESS_ALREADY_TERMINAL |
| `TestCancelProcess_NotFound` | GetProcess returns error | PROCESS_NOT_FOUND |
| API: `TestAPI_GetProcessStatus` | real client, known process | status returned |

---

## ops/context.go

### Function Signature

```go
func GetContext() string
```

### Content Structure (PRD section 5.2)

Returns a static precompiled string (~800-1200 tokens) containing:

1. **What is Zerops** — PaaS, Incus containers, bare-metal, SSH, managed services
2. **How Zerops Works** — Project → Services → Containers, VXLAN networking
3. **Critical Rules** — http:// internal, ports 10-65435, HA immutable, mode required, underscore env refs, no localhost, prepareCommands cached
4. **Configuration** — zerops.yml vs import.yml
5. **Service Types** — full catalog with versions
6. **Defaults** — postgresql@16, valkey@7.2, etc.
7. **Pointers** — zerops_knowledge for search, zerops://docs for resources

### Implementation

Content is a Go string constant (or `var` with string literal) in `ops/context.go`. Not part of BM25 index. Updated with code changes only.

### Test Cases

| Test | Expected |
|------|----------|
| `TestGetContext_NonEmpty` | result is non-empty string |
| `TestGetContext_ContainsCriticalSections` | contains "Critical Rules", "Service Types" |
| `TestGetContext_TokenSize` | len(result) within expected range |

---

## ops/workflow.go

### Function Signatures

```go
func GetWorkflowCatalog() string

func GetWorkflow(workflowName string) (string, error)
```

### Logic

**GetWorkflowCatalog** (no param):
Returns the static workflow catalog listing available workflows:
- bootstrap, deploy, debug, scale, configure, monitor

**GetWorkflow** (with param):
1. Look up `workflowName` in the workflow content map.
2. If found, return the workflow content (embedded markdown from `internal/content/workflows/`).
3. If not found, return error with available workflows listed.

### Content Source

Both functions read from `internal/content/` package (shared with `internal/init/`). Workflow content is embedded via `go:embed` in the content package.

### Test Cases

| Test | Input | Expected |
|------|-------|----------|
| `TestGetWorkflowCatalog_NonEmpty` | — | catalog string with all workflow names |
| `TestGetWorkflow_Bootstrap` | "bootstrap" | non-empty guidance content |
| `TestGetWorkflow_Deploy` | "deploy" | non-empty guidance content |
| `TestGetWorkflow_Unknown` | "nonexistent" | error with available list |

---

## content/ Package

### Structure

```
internal/content/
  content.go          → go:embed declarations + accessor functions
  workflows/
    bootstrap.md      → Bootstrap workflow guidance
    deploy.md         → Deploy workflow guidance
    debug.md          → Debug workflow guidance
    scale.md          → Scale workflow guidance
    configure.md      → Configure workflow guidance
    monitor.md        → Monitor workflow guidance
  templates/
    claude.md         → CLAUDE.md template for zcp init
    mcp-config.json   → MCP server config template
    ssh-config        → SSH config template
```

### content.go

```go
package content

import "embed"

//go:embed workflows/*.md
var workflowFS embed.FS

//go:embed templates/*
var templateFS embed.FS

func GetWorkflow(name string) (string, error)  // reads from workflowFS
func GetTemplate(name string) (string, error)   // reads from templateFS
func ListWorkflows() []string                    // lists available workflow names
```

### Dependencies

- Only stdlib (`embed`, `io/fs`)
- No external dependencies
- Consumed by `ops/workflow.go` and `init/templates.go`

---

## ops/deploy.go

### Function Signatures

```go
type DeployResult struct {
    Status        string `json:"status"`
    SourceService string `json:"sourceService,omitempty"`
    TargetService string `json:"targetService"`
    Message       string `json:"message"`
}

// SSHDeployer abstracts SSH command execution for testing.
type SSHDeployer interface {
    ExecSSH(ctx context.Context, host, command string) ([]byte, error)
}

// LocalDeployer abstracts local zcli execution for testing.
type LocalDeployer interface {
    ExecZcli(ctx context.Context, args ...string) ([]byte, error)
}

func Deploy(
    ctx context.Context,
    client platform.Client,
    projectID string,
    sshDeployer SSHDeployer,    // nil for local mode
    localDeployer LocalDeployer, // nil for SSH mode
    authInfo auth.Info,
    sourceService string,   // SSH mode: hostname to SSH into
    targetService string,   // hostname of target service (resolved to ID)
    setup string,           // zerops.yml setup name (optional)
    workingDir string,      // path inside container or local dir
) (*DeployResult, error)
```

### Mode Detection (PRD section 8.4)

```
IF sourceService != "" → SSH mode (targetService required)
ELSE IF targetService != "" → Local mode (workingDir optional)
ELSE → error: "Provide sourceService (SSH deploy) or targetService (local deploy)"
```

### SSH Mode Execution (PRD section 8.2)

1. Validate `sourceService` exists via `client.ListServices` + `resolveServiceID`.
2. Resolve `targetService` hostname → service ID via `resolveServiceID`.
3. Build SSH command:
   ```
   ssh {sourceService} "cd {workingDir} && zcli login {ZCP_API_KEY} [--zeropsRegion {region}] && zcli push {targetServiceID} [--setup={setup}]"
   ```
4. `workingDir` defaults to `/var/www` if empty.
5. `--zeropsRegion` only included if `authInfo.Region` is non-empty.
6. Execute via `sshDeployer.ExecSSH(ctx, sourceService, command)`.
7. Return `DeployResult{Status: "initiated", SourceService, TargetService, Message}`.

### Local Mode Execution (PRD section 8.3)

1. Resolve `targetService` hostname → service ID via `resolveServiceID`.
2. Check zcli auth (cli.data file exists with non-empty Token).
3. Build args: `["push", "--serviceId", serviceID]`.
4. If `workingDir` provided, add `["--workingDir", workingDir]`.
5. Execute via `localDeployer.ExecZcli(ctx, args...)`.
6. Return `DeployResult`.

### Auth Injection

- SSH mode: `zcli login $ZCP_API_KEY [--zeropsRegion $region]` — always fresh login (idempotent).
- Local mode: assumes zcli already authenticated. Check `cli.data` exists with Token.

### Source Reference

- `../zaia-mcp/internal/tools/deploy.go:10-51` — current deploy tool (zcli push passthrough)
- PRD section 8 — complete deploy architecture

**NOTE**: The current zaia-mcp deploy tool is a simple zcli push passthrough. ZCP's deploy is fundamentally different — SSH-based primary mode. This is new implementation, not a port.

### Test Cases

| Test | Input | Expected |
|------|-------|----------|
| `TestDeploy_SSHMode_Success` | sourceService="appdev", target="appstage" | SSH command built correctly |
| `TestDeploy_SSHMode_WithSetup` | setup="prod" | `--setup=prod` in command |
| `TestDeploy_SSHMode_DefaultWorkingDir` | workingDir="" | `/var/www` used |
| `TestDeploy_SSHMode_WithRegion` | region="prg1" | `--zeropsRegion prg1` included |
| `TestDeploy_SSHMode_NoRegion` | region="" | `--zeropsRegion` omitted |
| `TestDeploy_SSHMode_SourceNotFound` | sourceService not in services | SERVICE_NOT_FOUND |
| `TestDeploy_SSHMode_TargetNotFound` | targetService not in services | SERVICE_NOT_FOUND |
| `TestDeploy_LocalMode_Success` | no sourceService, target="appstage" | zcli push called |
| `TestDeploy_LocalMode_NoAuth` | cli.data missing | error "zcli login required" |
| `TestDeploy_NoParams` | no sourceService, no targetService | error |
| `TestDeploy_ModeDetection_SSH` | sourceService provided | SSH mode selected |
| `TestDeploy_ModeDetection_Local` | only targetService | local mode selected |

---

## ops/progress.go

### Function Signature

```go
// ProgressCallback is called by PollProcess to report progress.
// The ops layer is MCP-agnostic — the tool handler wraps
// req.Session.NotifyProgress() into this callback.
type ProgressCallback func(message string, progress, total float64)

func PollProcess(
    ctx context.Context,
    client platform.Client,
    processID string,
    onProgress ProgressCallback,  // may be nil (no notifications)
) (*platform.Process, error)
```

### Polling Behavior (PRD section 6.1)

- **Initial interval**: 2 seconds
- **Step-up**: after 30 seconds, increase to 5 seconds
- **Timeout**: 10 minutes total
- **Terminal states**: FINISHED, FAILED, CANCELED — stop polling
- **Progress reporting**: call `onProgress` after each poll with current status

### Algorithm

```
ticker starts at 2s
elapsed = 0
loop:
  GetProcess(processID)
  if terminal → return process
  if onProgress != nil → onProgress(status message, elapsed/timeout*100, 100)
  if elapsed > 30s → switch to 5s interval
  if elapsed > 10min → return timeout error
  wait ticker
```

### Source Reference

- PRD section 6 — progress notifications specification
- `/Users/macbook/Sites/mcp60/src/responses/stream.go` — polling pattern reference (not a direct port)

### Test Cases

| Test | Mock Setup | Expected |
|------|-----------|----------|
| `TestPollProcess_ImmediateFinish` | GetProcess returns FINISHED first call | process returned, 1 call |
| `TestPollProcess_PollThenFinish` | PENDING → RUNNING → FINISHED | process returned after 3 calls |
| `TestPollProcess_Failed` | PENDING → FAILED | failed process returned |
| `TestPollProcess_Timeout` | always RUNNING | timeout error after 10min |
| `TestPollProcess_IntervalStepUp` | track call timestamps | 2s initial, 5s after 30s |
| `TestPollProcess_ContextCanceled` | cancel ctx mid-poll | context error |
| `TestPollProcess_CallbackCalled` | provide onProgress | callback invoked per poll |
| `TestPollProcess_NilCallback` | onProgress=nil | no panic, works normally |

---

## Parameter Mapping Table

### zerops_discover

| Tool Parameter | Ops Parameter | Client Method | SDK Call |
|---------------|---------------|---------------|----------|
| `service` | `hostname` | `GetProject` + `ListServices` + `GetServiceEnv` | `GetProject`, `PostServiceStackSearch`, `GetServiceStackEnv` |
| `includeEnvs` | `includeEnvs` | `GetServiceEnv` per service | `GetServiceStackEnv` |

### zerops_manage

| Tool Parameter | Ops Parameter | Client Method | SDK Call |
|---------------|---------------|---------------|----------|
| `action` | (dispatch) | Start/Stop/Restart/Scale | varies |
| `serviceHostname` | `hostname` | `ListServices` → resolve | `PostServiceStackSearch` |
| `cpuMode` | `ScaleParams.CPUMode` → `AutoscalingParams.VerticalCpuMode` | `SetAutoscaling` | `PutServiceStackAutoscaling` |
| `minCpu` | `ScaleParams.MinCPU` → `AutoscalingParams.VerticalMinCpu` | `SetAutoscaling` | `PutServiceStackAutoscaling` |
| `maxCpu` | `ScaleParams.MaxCPU` → `AutoscalingParams.VerticalMaxCpu` | `SetAutoscaling` | `PutServiceStackAutoscaling` |
| `minRam` | `ScaleParams.MinRAM` → `AutoscalingParams.VerticalMinRam` | `SetAutoscaling` | `PutServiceStackAutoscaling` |
| `maxRam` | `ScaleParams.MaxRAM` → `AutoscalingParams.VerticalMaxRam` | `SetAutoscaling` | `PutServiceStackAutoscaling` |
| `minDisk` | `ScaleParams.MinDisk` → `AutoscalingParams.VerticalMinDisk` | `SetAutoscaling` | `PutServiceStackAutoscaling` |
| `maxDisk` | `ScaleParams.MaxDisk` → `AutoscalingParams.VerticalMaxDisk` | `SetAutoscaling` | `PutServiceStackAutoscaling` |
| `startContainers` | `ScaleParams.StartContainers` | (initial count, not mapped to AutoscalingParams) | — |
| `minContainers` | `ScaleParams.MinContainers` → `AutoscalingParams.HorizontalMinCount` | `SetAutoscaling` | `PutServiceStackAutoscaling` |
| `maxContainers` | `ScaleParams.MaxContainers` → `AutoscalingParams.HorizontalMaxCount` | `SetAutoscaling` | `PutServiceStackAutoscaling` |

### zerops_env

| Tool Parameter | Ops Parameter | Client Method | SDK Call |
|---------------|---------------|---------------|----------|
| `action` | (dispatch: get/set/delete) | varies | varies |
| `serviceHostname` | `hostname` | `ListServices` → resolve | `PostServiceStackSearch` |
| `project` | `isProject` | varies | varies |
| `variables` | `variables` | varies | varies |

**get/service**: `GetServiceEnv` → `GetServiceStackEnv`
**get/project**: `GetProjectEnv` → `PostProjectSearch` (filter by projectID)
**set/service**: `SetServiceEnvFile` → `PutServiceStackUserDataEnvFile`
**set/project**: `CreateProjectEnv` per pair → `PostProjectEnv`
**delete/service**: `GetServiceEnv` + `DeleteUserData` per key → `GetServiceStackEnv` + `DeleteUserData`
**delete/project**: `GetProjectEnv` + `DeleteProjectEnv` per key → `PostProjectSearch` + `DeleteProjectEnv`

### zerops_logs

| Tool Parameter | Ops Parameter | Client Method | SDK Call |
|---------------|---------------|---------------|----------|
| `serviceHostname` | `hostname` | `ListServices` → resolve | `PostServiceStackSearch` |
| `severity` | `severity` | `LogFetcher.FetchLogs` | HTTP query param |
| `since` | `since` → `parseSince()` → `time.Time` | `LogFetcher.FetchLogs` | HTTP query param |
| `limit` | `limit` (default 100) | `LogFetcher.FetchLogs` | HTTP query param `tail` |
| `search` | `search` | `LogFetcher.FetchLogs` | HTTP query param |
| (also) | | `GetProjectLog` | `GetProjectLog` → LogAccess |

### zerops_deploy

| Tool Parameter | Ops Parameter | Client Method | SDK Call |
|---------------|---------------|---------------|----------|
| `sourceService` | `sourceService` | `ListServices` → resolve | `PostServiceStackSearch` |
| `targetService` | `targetService` | `ListServices` → resolve → ID | `PostServiceStackSearch` |
| `setup` | `setup` | — (passed to zcli) | — |
| `workingDir` | `workingDir` (default `/var/www`) | — (path on container) | — |

### zerops_import

| Tool Parameter | Ops Parameter | Client Method | SDK Call |
|---------------|---------------|---------------|----------|
| `content` | `content` | `ImportServices` | `PostProjectServiceStackImport` |
| `filePath` | `filePath` (→ os.ReadFile) | `ImportServices` | `PostProjectServiceStackImport` |
| `dryRun` | `dryRun` | — (offline parse) or `ImportServices` | — or `PostProjectServiceStackImport` |

### zerops_validate

| Tool Parameter | Ops Parameter | Client Method | SDK Call |
|---------------|---------------|---------------|----------|
| `content` | `content` | — (offline) | — |
| `filePath` | `filePath` | — (offline) | — |
| `type` | `fileType` | — (offline) | — |

### zerops_knowledge

| Tool Parameter | Ops Parameter | Client Method | SDK Call |
|---------------|---------------|---------------|----------|
| `query` | `query` | `knowledge.Store.Search()` | — (in-memory BM25) |
| `limit` | `limit` | `knowledge.Store.Search()` | — |

### zerops_process

| Tool Parameter | Ops Parameter | Client Method | SDK Call |
|---------------|---------------|---------------|----------|
| `processId` | `processID` | `GetProcess` or `CancelProcess` | `GetProcess` or `PutProcessCancel` |
| `action` | (dispatch: status/cancel) | varies | varies |

### zerops_delete

| Tool Parameter | Ops Parameter | Client Method | SDK Call |
|---------------|---------------|---------------|----------|
| `serviceHostname` | `hostname` | `ListServices` → resolve → `DeleteService` | `PostServiceStackSearch` → `DeleteServiceStack` |
| `confirm` | `confirm` | — (safety gate) | — |

### zerops_subdomain

| Tool Parameter | Ops Parameter | Client Method | SDK Call |
|---------------|---------------|---------------|----------|
| `serviceHostname` | `hostname` | `ListServices` → resolve | `PostServiceStackSearch` |
| `action` | `action` | `EnableSubdomainAccess` / `DisableSubdomainAccess` | `PutServiceStackEnableSubdomainAccess` / `PutServiceStackDisableSubdomainAccess` |

### zerops_events

| Tool Parameter | Ops Parameter | Client Method | SDK Call |
|---------------|---------------|---------------|----------|
| `serviceHostname` | `serviceHostname` | `ListServices` (for hostname map) | `PostServiceStackSearch` |
| `limit` | `limit` (default 50) | `SearchProcesses` + `SearchAppVersions` | `PostProcessSearch` + `PostAppVersionSearch` |

### zerops_context

| Tool Parameter | Ops Parameter | Client Method | SDK Call |
|---------------|---------------|---------------|----------|
| (none) | (none) | (none) | (none — static content) |

### zerops_workflow

| Tool Parameter | Ops Parameter | Client Method | SDK Call |
|---------------|---------------|---------------|----------|
| `workflow` | `workflowName` | (none) | (none — embedded content) |

---

## Phase 2 Gate Criteria

Phase 2 is DONE when:

1. All ops functions implemented with tests passing:
   - `go test ./internal/ops/... -count=1 -v` — all mock tests pass
2. API verification tests pass:
   - `go test ./internal/ops/... -tags api -v` — all API tests pass against real Zerops
3. Specific verifications:
   - `resolveServiceID` correctly handles found/not-found cases
   - `parseSince` handles all documented formats
   - Scale parameter min/max validation works
   - Env KEY=value parsing handles edge cases (empty value, equals in value)
   - Import dry-run catches project section
   - Subdomain idempotent handling works (ALREADY_ENABLED = success)
   - Events timeline merges processes + appVersions correctly
   - Process cancel checks terminal state first
   - Deploy mode detection works (SSH vs local)
   - PollProcess respects timeout and interval step-up
   - Context returns non-empty static content
   - Workflow catalog and per-workflow content load from embedded FS

### Dependencies on Phase 1

Phase 2 requires from Phase 1:
- `platform.Client` interface (all methods)
- `platform.MockClient` (builder pattern)
- `platform.PlatformError` and error codes
- `platform.LogFetcher` and `MockLogFetcher`
- `auth.Info` struct (for deploy auth injection)
- All domain types (`ServiceStack`, `Process`, `EnvVar`, `ImportResult`, etc.)

### Key Divergences from Source

1. **No Cobra/CLI framework** — ops functions take explicit parameters, not command flags.
2. **FailReason mapping** — ZCP must add `FailReason` extraction in `mapProcess()` (Phase 1 platform layer).
3. **Deploy is fundamentally new** — SSH-based primary mode, not zcli passthrough.
4. **PollProcess is new** — source has no polling; zaia-mcp's convert.go just returns process info.
5. **Content package is new** — `go:embed` for workflow markdown, shared between ops and init.
6. **zerops_context and zerops_workflow are new tools** — no source equivalent.
7. **Thread safety** — ZCP uses `sync.Once` for knowledge store init (source uses racy nil check).
