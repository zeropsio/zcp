# MCP Layer Analysis — Phase 3 + Phase 5

Analysis of PRD sections 5 (Tools Spec), 6 (Streaming/Progress), 9.4 (zcp init), and corresponding source files.

**Source references**:
- PRD: `/Users/macbook/Documents/Zerops-MCP/zcp/design/zcp-prd.md`
- Source MCP server: `/Users/macbook/Documents/Zerops-MCP/zaia-mcp/internal/server/server.go`
- Source tool handlers: `/Users/macbook/Documents/Zerops-MCP/zaia-mcp/internal/tools/`
- MCP SDK: `/Users/macbook/Sites/mcp60/src/tools/base.go`, `/Users/macbook/Sites/mcp60/src/responses/stream.go`
- go-sdk vendored: `/Users/macbook/Sites/mcp60/vendor/github.com/modelcontextprotocol/go-sdk/mcp/`

---

## go-sdk API Patterns (v1.2.0)

### Key Types

```go
// Type alias: CallToolRequest = ServerRequest[*CallToolParamsRaw]
// Source: mcp/requests.go:10

type ServerRequest[P Params] struct {
    Session *ServerSession    // Access to session for NotifyProgress
    Params  P                 // Request params (includes GetProgressToken())
    Extra   *RequestExtra     // Token info, HTTP headers
}

type CallToolResult struct {
    Meta              `json:"_meta,omitempty"`
    Content           []Content `json:"content"`
    StructuredContent any       `json:"structuredContent,omitempty"`
    IsError           bool      `json:"isError,omitempty"`
}

type Tool struct {
    Meta                          `json:"_meta,omitempty"`
    Annotations *ToolAnnotations  `json:"annotations,omitempty"`
    Description string            `json:"description,omitempty"`
    InputSchema *jsonschema.Schema `json:"inputSchema"`
    Name        string            `json:"name"`
    OutputSchema *jsonschema.Schema `json:"outputSchema,omitempty"`
    Title       string            `json:"title,omitempty"`
}

type ToolAnnotations struct {
    DestructiveHint *bool  `json:"destructiveHint,omitempty"` // default: true
    IdempotentHint  bool   `json:"idempotentHint,omitempty"`  // default: false
    OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`   // default: true
    ReadOnlyHint    bool   `json:"readOnlyHint,omitempty"`    // default: false
    Title           string `json:"title,omitempty"`
}
```

### Registration Pattern

```go
// mcp/server.go:364 — Typed AddTool with auto-unmarshaling
func AddTool[In, Out any](s *Server, t *Tool, h ToolHandlerFor[In, Out])

// mcp/tool.go:55 — Handler signature
type ToolHandlerFor[In, Out any] func(
    _ context.Context,
    request *CallToolRequest,
    input In,
) (result *CallToolResult, output Out, _ error)
```

**Key behavior**: `AddTool` auto-generates `InputSchema` from the `In` type's JSON tags. The `In` struct's `json` tags + `jsonschema` tags define the schema. Fields with `omitempty` become optional. Required fields have no `omitempty`.

### Progress Notification Pattern

```go
// mcp/protocol.go:644
type ProgressNotificationParams struct {
    Meta                        `json:"_meta,omitempty"`
    ProgressToken any           `json:"progressToken"`
    Message       string        `json:"message,omitempty"`
    Progress      float64       `json:"progress"`
    Total         float64       `json:"total,omitempty"`  // 0 = unknown
}

// mcp/server.go:839 — Server-side notification
func (ss *ServerSession) NotifyProgress(ctx context.Context, params *ProgressNotificationParams) error

// Usage in handler:
if progressToken := req.Params.GetProgressToken(); progressToken != nil {
    req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
        ProgressToken: progressToken,
        Message:       "...",
        Progress:      50,
        Total:         100,
    })
}
```

### In-Memory Testing Pattern

```go
// mcp/transport.go:107-111
func NewInMemoryTransports() (*InMemoryTransport, *InMemoryTransport)

// Usage:
serverTransport, clientTransport := mcp.NewInMemoryTransports()
_, err := srv.Server().Connect(ctx, serverTransport, nil)
client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v1"}, nil)
session, err := client.Connect(ctx, clientTransport, nil)
result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "...", Arguments: map[string]any{...}})
```

### Server Creation Pattern

```go
// mcp/server.go:106
func NewServer(impl *Implementation, options *ServerOptions) *Server

type ServerOptions struct {
    Instructions                string
    InitializedHandler          func(context.Context, *InitializedRequest)
    PageSize                    int    // Default: DefaultPageSize
    RootsListChangedHandler     func(context.Context, *RootsListChangedRequest)
    ProgressNotificationHandler func(context.Context, *ProgressNotificationServerRequest)
    CompletionHandler           func(context.Context, *CompleteRequest) (*CompleteResult, error)
    KeepAlive                   time.Duration
    // ...more
}

// mcp/transport.go:92-94
func (*StdioTransport) Connect(context.Context) (Connection, error)
```

---

## Tool Handler Analysis

### Architecture Shift: Source vs ZCP

**Source (zaia-mcp)**: Each handler builds CLI args, calls `exec.RunZaia(ctx, args...)`, parses CLI JSON output via `ResultFromCLI()`.
- Source: `zaia-mcp/internal/tools/discover.go:34-47`

**ZCP**: Each handler calls `ops.Fn(ctx, params...)` directly, converts result/error to MCP response.
- No executor, no CLI subprocess, no JSON envelope parsing
- Direct PlatformError → MCP result conversion via `convert.go`

**Common pattern for ALL ZCP tool handlers**:
```go
func registerXxx(srv *mcp.Server, deps *Deps) {
    mcp.AddTool(srv, &mcp.Tool{
        Name:        "zerops_xxx",
        Description: "...",
        Annotations: &mcp.ToolAnnotations{...},
    }, func(ctx context.Context, req *mcp.CallToolRequest, input XxxInput) (*mcp.CallToolResult, any, error) {
        result, err := deps.Ops.Xxx(ctx, input.Field1, input.Field2)
        if err != nil {
            return convertError(err), nil, nil  // PlatformError → MCP error
        }
        return jsonResult(result), nil, nil  // Success → MCP JSON content
    })
}
```

### Handler Deps (dependency injection)

The source uses `executor.Executor` as the single dependency. ZCP needs:
- `platform.Client` — NOT directly (PRD rule: tools/ imports platform/ for types only)
- `ops` layer functions — the business logic
- `knowledge.Store` — for knowledge tool
- `auth.Info` — for deploy (needs token for SSH auth injection)

**Design decision**: A `Deps` struct (or individual function params) bundles what tool handlers need. Tools call ops functions, never platform.Client directly.

```go
// Possible Deps pattern:
type Deps struct {
    Ops       *ops.Service   // or individual funcs
    Knowledge *knowledge.Store
    Auth      *auth.Info
}
```

---

## Tool-by-Tool Analysis

### tools/discover.go (zerops_discover)

**MCP Schema**:
```go
type DiscoverInput struct {
    Service     string `json:"service,omitempty"`     // optional: filter by hostname
    IncludeEnvs bool   `json:"includeEnvs,omitempty"` // optional: include env vars
}
```

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `service` | string | no | Filter by hostname |
| `includeEnvs` | bool | no | Include env vars per service |

**Annotations**: `ReadOnlyHint: true`, `IdempotentHint: true`

**Source reference**: `zaia-mcp/internal/tools/discover.go:11-14` (input struct), `:17-49` (registration)

**Tool → ops call mapping**:
```
DiscoverInput → ops.Discover(ctx, client, projectID, service, includeEnvs)
```

**Error conversion**: `ops.Discover` returns `(*DiscoverResult, error)`. Error is `*platform.PlatformError` → `convertError()`.

**Response format**: JSON with `project` (id, name, status) + `services` array (hostname, type, status, optional envs).

---

### tools/manage.go (zerops_manage)

**MCP Schema**:
```go
type ManageInput struct {
    Action          string  `json:"action"`                       // required
    ServiceHostname string  `json:"serviceHostname"`              // required
    CpuMode         string  `json:"cpuMode,omitempty"`            // optional (scale only)
    MinCpu          int     `json:"minCpu,omitempty"`             // optional (scale only)
    MaxCpu          int     `json:"maxCpu,omitempty"`             // optional (scale only)
    MinRam          float64 `json:"minRam,omitempty"`             // optional (scale only)
    MaxRam          float64 `json:"maxRam,omitempty"`             // optional (scale only)
    MinDisk         float64 `json:"minDisk,omitempty"`            // optional (scale only)
    MaxDisk         float64 `json:"maxDisk,omitempty"`            // optional (scale only)
    StartContainers int     `json:"startContainers,omitempty"`    // optional (scale only)
    MinContainers   int     `json:"minContainers,omitempty"`      // optional (scale only)
    MaxContainers   int     `json:"maxContainers,omitempty"`      // optional (scale only)
}
```

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `action` | string | yes | start, stop, restart, scale |
| `serviceHostname` | string | yes | Target service |
| `cpuMode` | string | no | SHARED or DEDICATED (scale only) |
| `minCpu` / `maxCpu` | int | no | CPU cores (scale only) |
| `minRam` / `maxRam` | float64 | no | RAM in GB (scale only) |
| `minDisk` / `maxDisk` | float64 | no | Disk in GB (scale only) |
| `startContainers` | int | no | Initial container count (scale only) |
| `minContainers` / `maxContainers` | int | no | Horizontal scaling (scale only) |

**Annotations**: `DestructiveHint: boolPtr(true)`

**Source reference**: `zaia-mcp/internal/tools/manage.go:12-25` (input), `:28-99` (registration)

**Async behavior**: Returns `*platform.Process` with process ID. If `ProgressToken` is provided, tool handler wraps `req.Session.NotifyProgress()` into a callback and calls `ops.PollProcess()` (from `ops/progress.go`). Without ProgressToken, returns process info immediately.

**Tool → ops call mapping**:
```
action=start   → ops.StartService(ctx, client, projectID, hostname)   → *Process
action=stop    → ops.StopService(ctx, client, projectID, hostname)    → *Process
action=restart → ops.RestartService(ctx, client, projectID, hostname) → *Process
action=scale   → ops.ScaleService(ctx, client, projectID, hostname, scaleParams) → *Process (may be nil)
```

**Scale parameter mapping** (PRD §5.1):

| Tool Param | API Field (`AutoscalingParams`) |
|------------|-------------------------------|
| `cpuMode` | `CpuMode` |
| `minCpu` / `maxCpu` | `VerticalMinCpu` / `VerticalMaxCpu` |
| `minRam` / `maxRam` | `VerticalMinRam` / `VerticalMaxRam` |
| `minDisk` / `maxDisk` | `VerticalMinDisk` / `VerticalMaxDisk` |
| `minContainers` / `maxContainers` | `HorizontalMinCount` / `HorizontalMaxCount` |
| `startContainers` | (initial container count, no direct API field) |

**Note**: `SetAutoscaling` can return nil Process (sync operation, no async tracking). Handle nil Process gracefully — return success with "scaling applied" message, no process ID.

---

### tools/env.go (zerops_env)

**MCP Schema**:
```go
type EnvInput struct {
    Action          string   `json:"action"`                      // required
    ServiceHostname string   `json:"serviceHostname,omitempty"`   // optional (service scope)
    Project         bool     `json:"project,omitempty"`           // optional (project scope)
    Variables       []string `json:"variables,omitempty"`         // optional (set/delete)
}
```

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `action` | string | yes | get, set, delete |
| `serviceHostname` | string | no | Service scope (mutually exclusive with project) |
| `project` | bool | no | Project scope |
| `variables` | []string | no | KEY=value (set) or KEY (delete) |

**Annotations**: `DestructiveHint: boolPtr(false)`

**Source reference**: `zaia-mcp/internal/tools/env.go:11-16` (input), `:19-63` (registration)

**Mixed sync/async behavior**:
- `action=get` → sync, returns env vars immediately
- `action=set` → async, returns process ID
- `action=delete` → async, returns process ID

**Tool → ops call mapping**:
```
action=get + serviceHostname  → ops.GetServiceEnv(ctx, client, projectID, hostname) → []EnvVar
action=get + project=true     → ops.GetProjectEnv(ctx, client, projectID)           → []EnvVar
action=set + serviceHostname  → ops.SetServiceEnv(ctx, client, projectID, hostname, vars) → *Process
action=set + project=true     → ops.SetProjectEnv(ctx, client, projectID, vars)     → *Process
action=delete + serviceHostname → ops.DeleteServiceEnv(ctx, client, projectID, hostname, varName) → *Process
action=delete + project=true    → ops.DeleteProjectEnv(ctx, client, projectID, varName) → *Process
```

**Validation**: Must have either `serviceHostname` OR `project=true`, not both, not neither.

---

### tools/logs.go (zerops_logs)

**MCP Schema**:
```go
type LogsInput struct {
    ServiceHostname string `json:"serviceHostname"`           // required
    Severity        string `json:"severity,omitempty"`        // optional
    Since           string `json:"since,omitempty"`           // optional
    Limit           int    `json:"limit,omitempty"`           // optional (default 100)
    Search          string `json:"search,omitempty"`          // optional
}
```

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `serviceHostname` | string | yes | Target service |
| `severity` | string | no | error, warning, info, debug |
| `since` | string | no | 30m, 1h, 24h, 7d, or ISO 8601 |
| `limit` | int | no | Max entries (default 100) |
| `search` | string | no | Text search |

**Annotations**: `ReadOnlyHint: true`, `IdempotentHint: true`

**Source reference**: `zaia-mcp/internal/tools/logs.go:12-18` (input), `:22-68` (registration)

**Note on PRD vs source**: PRD mentions `buildId` param. Source has it too (`zaia-mcp/internal/tools/logs.go:18`). ZCP should include `buildId` — needed for build log retrieval.

**Tool → ops call mapping**:
```
LogsInput → ops.FetchLogs(ctx, client, logFetcher, projectID, hostname, logParams)
```

The ops layer does the 2-step: `client.GetProjectLog(ctx, projectID)` → `logFetcher.FetchLogs(ctx, logAccess, params)`.

---

### tools/deploy.go (zerops_deploy)

**MCP Schema** (DIFFERENT from source — SSH mode is new):
```go
type DeployInput struct {
    SourceService string `json:"sourceService,omitempty"`   // optional (SSH mode trigger)
    TargetService string `json:"targetService,omitempty"`   // optional (hostname, resolved to ID)
    Setup         string `json:"setup,omitempty"`           // optional (zerops.yml setup name)
    WorkingDir    string `json:"workingDir,omitempty"`      // optional
}
```

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `sourceService` | string | no | SSH target hostname (triggers SSH mode) |
| `targetService` | string | no | Target service hostname (resolved to ID) |
| `setup` | string | no | zerops.yml setup name (e.g. "dev", "prod") |
| `workingDir` | string | no | Path inside container (SSH) or local dir (local) |

**Annotations**: `DestructiveHint: boolPtr(false)`

**Source reference**: `zaia-mcp/internal/tools/deploy.go:11-14` (source input — much simpler, local only)

**Mode detection** (PRD §8.4):
```
IF sourceService provided  → SSH mode (targetService also required)
ELSE IF workingDir + targetService → Local mode
ELSE → error
```

**Tool → ops call mapping**:
```
SSH mode:   → ops.DeploySSH(ctx, client, auth, projectID, sourceService, targetService, setup, workingDir)
Local mode: → ops.DeployLocal(ctx, targetService, workingDir)
```

**SSH execution** (PRD §8.2):
1. Validate sourceService exists (via discover)
2. Resolve targetService hostname → service ID
3. SSH into sourceService
4. `zcli login $ZCP_API_KEY`
5. `zcli push $resolvedServiceId [--setup=$setup] [--workingDir=$workingDir]`

**Auth sharing**: Tool handler needs `auth.Info` to pass `ZCP_API_KEY` into SSH session.

---

### tools/import.go (zerops_import)

**MCP Schema**:
```go
type ImportInput struct {
    Content  string `json:"content,omitempty"`    // optional (inline YAML)
    FilePath string `json:"filePath,omitempty"`   // optional (path to YAML file)
    DryRun   bool   `json:"dryRun,omitempty"`     // optional
}
```

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `content` | string | no | Inline YAML (mutually exclusive with filePath) |
| `filePath` | string | no | Path to YAML file |
| `dryRun` | bool | no | Preview mode (sync validation) |

**Annotations**: `DestructiveHint: boolPtr(false)`

**Source reference**: `zaia-mcp/internal/tools/import.go:11-15` (input), `:18-65` (registration)

**Mixed sync/async**:
- `dryRun=true` → sync validation, returns validation result
- `dryRun=false` → async import, returns process ID

**Tool → ops call mapping**:
```
dryRun=true  → ops.ImportDryRun(ctx, client, projectID, yamlContent) → *ImportResult (sync)
dryRun=false → ops.Import(ctx, client, projectID, yamlContent)       → *Process (async)
```

**Validation**: Must have either `content` OR `filePath`, not both, not neither. If `filePath`, ops reads the file.

---

### tools/validate.go (zerops_validate)

**MCP Schema**:
```go
type ValidateInput struct {
    Content  string `json:"content,omitempty"`    // optional
    FilePath string `json:"filePath,omitempty"`   // optional
    Type     string `json:"type,omitempty"`       // optional (zerops or import)
}
```

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `content` | string | no | Inline YAML |
| `filePath` | string | no | Path to YAML file |
| `type` | string | no | zerops or import |

**Annotations**: `ReadOnlyHint: true`, `IdempotentHint: true`, `OpenWorldHint: boolPtr(false)`

**Source reference**: `zaia-mcp/internal/tools/validate.go:11-15` (input), `:18-57` (registration)

**Tool → ops call mapping**:
```
ValidateInput → ops.Validate(ctx, yamlContent, yamlType) → *ValidationResult
```

Purely offline — no API calls needed. Uses `gopkg.in/yaml.v3` for parsing + custom validation rules.

---

### tools/knowledge.go (zerops_knowledge)

**MCP Schema**:
```go
type KnowledgeInput struct {
    Query string `json:"query"`              // required
    Limit int    `json:"limit,omitempty"`    // optional
}
```

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `query` | string | yes | BM25 search query |
| `limit` | int | no | Max results |

**Annotations**: `ReadOnlyHint: true`, `IdempotentHint: true`, `OpenWorldHint: boolPtr(false)`

**Source reference**: `zaia-mcp/internal/tools/knowledge.go:12-15` (input), `:18-52` (registration)

**Tool → ops call mapping**: Unlike other tools, knowledge goes directly to `knowledge.Store`:
```
KnowledgeInput → knowledge.Store.Search(query, limit)
```

No ops layer needed — knowledge is self-contained. The tool handler calls `Store.Search()` directly (or through a thin ops wrapper for consistency).

---

### tools/process.go (zerops_process)

**MCP Schema**:
```go
type ProcessInput struct {
    ProcessID string `json:"processId"`            // required
    Action    string `json:"action,omitempty"`      // optional: "status" (default) or "cancel"
}
```

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `processId` | string | yes | Process UUID |
| `action` | string | no | status (default) or cancel |

**Annotations**: `ReadOnlyHint: true`, `IdempotentHint: true` (for status; cancel is not idempotent but same tool)

**Source reference**: `zaia-mcp/internal/tools/process.go:11-14` (input), `:17-62` (registration)

**Note**: Annotations should probably differ by action. Since `cancel` is destructive, consider whether to keep current annotations or split into two tools. PRD keeps it as one tool.

**Tool → ops call mapping**:
```
action=status → ops.GetProcess(ctx, client, processID) → *Process
action=cancel → ops.CancelProcess(ctx, client, processID) → *Process
```

**Status normalization**: Source normalizes `DONE→FINISHED`, `CANCELLED→CANCELED`. This happens in `platform/zerops.go`, not in the tool handler.

---

### tools/delete.go (zerops_delete)

**MCP Schema**:
```go
type DeleteInput struct {
    ServiceHostname string `json:"serviceHostname"`    // required
    Confirm         bool   `json:"confirm"`            // required, must be true
}
```

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `serviceHostname` | string | yes | Service to delete |
| `confirm` | bool | yes | Safety gate (must be true) |

**Annotations**: `DestructiveHint: boolPtr(true)`

**Source reference**: `zaia-mcp/internal/tools/delete.go:11-14` (input), `:17-51` (registration)

**Async behavior**: Returns process ID. ProgressToken polling supported.

**Tool → ops call mapping**:
```
DeleteInput → ops.DeleteService(ctx, client, projectID, hostname, confirm) → *Process
```

**Safety gate**: If `confirm != true`, return MCP error immediately (never reaches ops layer).

---

### tools/subdomain.go (zerops_subdomain)

**MCP Schema**:
```go
type SubdomainInput struct {
    ServiceHostname string `json:"serviceHostname"`    // required
    Action          string `json:"action"`             // required: "enable" or "disable"
}
```

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `serviceHostname` | string | yes | Target service |
| `action` | string | yes | enable or disable |

**Annotations**: `DestructiveHint: boolPtr(false)`, `IdempotentHint: true`

**Source reference**: `zaia-mcp/internal/tools/subdomain.go:11-14` (input), `:17-48` (registration)

**Async behavior**: Returns process ID. Idempotent — enabling already-enabled returns success.

**Tool → ops call mapping**:
```
action=enable  → ops.EnableSubdomain(ctx, client, projectID, hostname)  → *Process
action=disable → ops.DisableSubdomain(ctx, client, projectID, hostname) → *Process
```

**Idempotent handling**: `platform/errors.go` generates `SUBDOMAIN_ALREADY_ENABLED` / `SUBDOMAIN_ALREADY_DISABLED` codes. Ops layer catches these and returns success instead of error.

---

### tools/events.go (zerops_events)

**MCP Schema**:
```go
type EventsInput struct {
    ServiceHostname string `json:"serviceHostname,omitempty"`   // optional
    Limit           int    `json:"limit,omitempty"`             // optional (default 50)
}
```

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `serviceHostname` | string | no | Filter by service |
| `limit` | int | no | Max events (default 50) |

**Annotations**: `ReadOnlyHint: true`, `IdempotentHint: true`

**Source reference**: `zaia-mcp/internal/tools/events.go:12-15` (input), `:18-53` (registration)

**Tool → ops call mapping**:
```
EventsInput → ops.GetEvents(ctx, client, projectID, serviceHostname, limit) → *EventsResult
```

The ops layer merges `SearchProcesses` + `SearchAppVersions` into a unified timeline.

---

### tools/context.go (zerops_context) — NEW

**MCP Schema**:
```go
type ContextInput struct{}  // no parameters
```

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| (none) | — | — | No parameters |

**Annotations**: `ReadOnlyHint: true`, `IdempotentHint: true`, `OpenWorldHint: boolPtr(false)`

**Tool → ops call mapping**:
```
ContextInput → ops.GetContext() → string (static compiled content)
```

**Response**: Static precompiled string (~800-1200 tokens). Content defined in PRD §5.2. Lives in `ops/context.go` as a const/var string.

**No PRD source counterpart** — this is a new tool replacing the bloated instructions message.

---

### tools/workflow.go (zerops_workflow) — NEW

**MCP Schema**:
```go
type WorkflowInput struct {
    Workflow string `json:"workflow,omitempty"`   // optional
}
```

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `workflow` | string | no | Specific workflow name |

**Annotations**: `ReadOnlyHint: true`, `IdempotentHint: true`, `OpenWorldHint: boolPtr(false)`

**Tool → ops call mapping**:
```
workflow=""     → ops.GetWorkflowCatalog()             → string (catalog listing)
workflow="xxx"  → ops.GetWorkflow(workflowName)        → string (specific guidance)
```

**Content source**: `ops/workflow.go` reads from `internal/content/` (shared with `init/`). Workflow catalog is static string; per-workflow content is embedded markdown.

**Available workflows** (PRD §5.3): bootstrap, deploy, debug, scale, configure, monitor.

---

## tools/convert.go — Error Conversion

### Source Pattern (NOT a port)

Source `convert.go` (`zaia-mcp/internal/tools/convert.go:1-116`) parses CLI subprocess JSON envelopes (`CLIResponse` with `type: sync/async/error`). ZCP does NOT use this — it has no subprocess. Use the JSON error format as **output inspiration only**.

### ZCP Implementation

**PlatformError → MCP CallToolResult conversion**:

```go
// convertError converts a PlatformError to an MCP error result.
// Returns nil if err is nil or not a PlatformError.
func convertError(err error) *mcp.CallToolResult {
    var pe *platform.PlatformError
    if !errors.As(err, &pe) {
        // Non-platform error — generic error result
        return &mcp.CallToolResult{
            Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
            IsError: true,
        }
    }

    // Build JSON error matching source format
    result := map[string]string{
        "code":  pe.Code,
        "error": pe.Message,
    }
    if pe.Suggestion != "" {
        result["suggestion"] = pe.Suggestion
    }

    b, _ := json.Marshal(result)
    return &mcp.CallToolResult{
        Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
        IsError: true,
    }
}
```

**JSON error format** (PRD §5.4):
```json
{"code":"SERVICE_NOT_FOUND","error":"service 'xyz' not found","suggestion":"Check hostname with zerops_discover"}
```

**isError flag**: Always `true` for PlatformError conversions. The LLM sees errors in content and can self-correct.

**Process failure reason** (PRD §5.4): When a process has `Status=FAILED`, include `failReason` in the response:
```json
{"processId":"...","status":"FAILED","failReason":"Build failed: npm install returned exit code 1"}
```

### Helper Functions

```go
// jsonResult converts any value to a JSON MCP text result.
func jsonResult(v any) *mcp.CallToolResult {
    b, err := json.Marshal(v)
    if err != nil {
        return &mcp.CallToolResult{
            Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("marshal error: %v", err)}},
            IsError: true,
        }
    }
    return &mcp.CallToolResult{
        Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
    }
}

// textResult creates a plain text MCP result.
func textResult(text string) *mcp.CallToolResult {
    return &mcp.CallToolResult{
        Content: []mcp.Content{&mcp.TextContent{Text: text}},
    }
}
```

---

## server/server.go — MCP Server Setup

### Constructor Pattern

**Source**: `zaia-mcp/internal/server/server.go:62-104`

Source uses `MCPServer` wrapping `*mcp.Server` + `executor.Executor`. ZCP replaces executor with direct dependencies:

```go
type Server struct {
    server    *mcp.Server
    ops       *ops.Service     // or individual deps
    knowledge *knowledge.Store
    auth      *auth.Info
}

func New(client platform.Client, authInfo *auth.Info, store *knowledge.Store) *Server {
    srv := mcp.NewServer(
        &mcp.Implementation{
            Name:    "zcp",
            Version: Version,
        },
        &mcp.ServerOptions{
            Instructions: Instructions,  // from instructions.go
        },
    )

    s := &Server{
        server:    srv,
        // ... deps
    }

    s.registerTools()
    s.registerResources()

    return s
}
```

### Tool Registration

Source pattern (`server.go:117-135`): Individual `tools.RegisterXxx(s.server, s.executor)` calls.

ZCP pattern: Same structure, different deps:
```go
func (s *Server) registerTools() {
    // Sync tools
    tools.RegisterDiscover(s.server, s.ops)
    tools.RegisterLogs(s.server, s.ops)
    tools.RegisterValidate(s.server, s.ops)
    tools.RegisterKnowledge(s.server, s.knowledge)
    tools.RegisterProcess(s.server, s.ops)
    tools.RegisterEvents(s.server, s.ops)
    tools.RegisterContext(s.server, s.ops)
    tools.RegisterWorkflow(s.server, s.ops)

    // Async tools
    tools.RegisterManage(s.server, s.ops)
    tools.RegisterEnv(s.server, s.ops)
    tools.RegisterImport(s.server, s.ops)
    tools.RegisterDelete(s.server, s.ops)
    tools.RegisterSubdomain(s.server, s.ops)

    // Deploy (needs auth for SSH mode)
    tools.RegisterDeploy(s.server, s.ops, s.auth)
}
```

### Resource Registration

Source: `resources.RegisterKnowledgeResources(s.server, s.executor)` — registers `zerops://docs/{+path}`.

ZCP: Same pattern but with `knowledge.Store` directly:
```go
func (s *Server) registerResources() {
    // Register zerops://docs/{+path} for direct document access
    knowledge.RegisterResources(s.server, s.knowledge)
}
```

### Run Method

Source: `server.go:107-109`
```go
func (s *MCPServer) Run(ctx context.Context) error {
    return s.server.Run(ctx, &mcp.StdioTransport{})
}
```

ZCP: Same. STDIO transport.

### Server() Accessor

For testing — returns underlying `*mcp.Server` to use with `InMemoryTransports`:
```go
func (s *Server) Server() *mcp.Server {
    return s.server
}
```

---

## server/instructions.go — Ultra-Minimal Init Message

### Source vs ZCP

**Source** (`server.go:15-56`): ~250 tokens. Includes full service catalog, all critical rules, tool list, defaults.

**ZCP** (PRD §2.1, lines 131-144): ~40-50 tokens. **Deliberately minimal** — pure relevance signal.

### Content Specification (PRD)

**MUST include**:
1. Identity: ZCP = tools for Zerops PaaS infrastructure
2. Scope: infrastructure, services, deployment, configuration, debugging
3. Entry point: `zerops_context` (load platform knowledge when needed)

**MUST NOT include**:
- Tool lists (already in MCP `tools/list`)
- Service types (in `zerops_context`)
- Rules, defaults (in `zerops_context`)

### Implementation

```go
const Instructions = `ZCP provides tools for managing Zerops PaaS infrastructure: services, deployment, configuration, and debugging. Call zerops_context to load platform knowledge when working with Zerops.`
```

**Rationale**: The LLM pays near-zero cost when Zerops is not relevant. MCP's `tools/list` already exposes all tools with descriptions and schemas. `zerops_context` provides full platform knowledge on-demand.

---

## cmd/zcp/main.go — Entrypoint

### Startup Sequence (PRD §9.4, §3.2)

```go
func main() {
    // 1. Init mode dispatch
    if len(os.Args) > 1 && os.Args[1] == "init" {
        init.Run()
        return
    }

    // 2. MCP server mode
    // 2a. Parse env vars
    //   ZCP_API_KEY, ZCP_API_HOST, ZCP_REGION, ZCP_LOG_LEVEL

    // 2b. Auth: resolve token + project
    //   auth.Resolve(ctx) → auth.Info
    //   Fatal on failure

    // 2c. Create platform client
    //   platform.NewZeropsClient(authInfo.Token, authInfo.APIHost)

    // 2d. Create knowledge store
    //   knowledge.GetStore() (sync.Once)

    // 2e. Create MCP server
    //   server.New(client, authInfo, store)

    // 2f. Signal handling
    //   ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)

    // 2g. Run STDIO transport
    //   server.Run(ctx)
    //   On error → log to stderr, exit(1)
}
```

### Signal Handling (PRD §12)

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()

if err := srv.Run(ctx); err != nil {
    // Context cancellation from signal = graceful shutdown (no error log)
    if !errors.Is(err, context.Canceled) {
        log.Fatalf("server error: %v", err)
    }
}
```

**Shutdown timeout**: 5s hard deadline after signal. No drain phase — STDIO is not a listener.

### NOT a CLI Framework

No Cobra, no flag parsing beyond `os.Args[1]`. Two modes only:
- `zcp` → MCP server
- `zcp init` → Bootstrap

---

## init/ Package (Phase 5)

### init/init.go — Orchestrator

```go
func Run() {
    // Steps (all idempotent):
    // 1. Generate CLAUDE.md in working directory
    // 2. Configure MCP server in Claude Code settings
    // 3. Set up Claude Code hooks
    // 4. Configure SSH (~/.ssh/config)

    // Each step: log what it's doing, write file, report success/failure
    // Running again overwrites with defaults (intentional)
}
```

**Key properties** (PRD §9.4):
- **Idempotent**: Re-running resets to defaults
- **Templates compiled into binary**: `go:embed` via `internal/content/`
- **No external downloads needed**

### init/templates.go

Uses `internal/content/` for all template data:

```go
import "github.com/zeropsio/zcp/internal/content"

func generateCLAUDEMD() error {
    return os.WriteFile("CLAUDE.md", []byte(content.CLAUDEMDTemplate), 0644)
}

func generateMCPConfig() error {
    // Write to ~/.claude/settings.json or equivalent
    config := content.MCPConfigTemplate
    // ...
}

func configureSSH() error {
    // Append to ~/.ssh/config
    sshConfig := content.SSHConfigTemplate
    // ...
}
```

### Output Files

| Action | Output |
|--------|--------|
| CLAUDE.md | `./CLAUDE.md` (working dir) |
| MCP config | `~/.claude/settings.json` |
| Hooks | `~/.claude/hooks/` or settings |
| SSH config | `~/.ssh/config` |

---

## Async Tool Pattern (Streaming/Progress)

### PRD §6 Pattern for ZCP

All async tools (manage, env set/delete, import, delete, subdomain) share the same pattern:

```go
func(ctx context.Context, req *mcp.CallToolRequest, input XxxInput) (*mcp.CallToolResult, any, error) {
    // 1. Call ops layer → get *Process
    process, err := ops.Xxx(ctx, ...)
    if err != nil {
        return convertError(err), nil, nil
    }

    // 2. Check for ProgressToken
    progressToken := req.Params.GetProgressToken()
    if progressToken != nil && process != nil && process.ID != "" {
        // 3. Poll with progress notifications
        onProgress := func(message string, progress, total float64) {
            req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
                ProgressToken: progressToken,
                Message:       message,
                Progress:      progress,
                Total:         total,
            })
        }

        finalProcess, err := ops.PollProcess(ctx, client, process.ID, onProgress)
        if err != nil {
            return convertError(err), nil, nil
        }
        return jsonResult(finalProcess), nil, nil
    }

    // 4. No ProgressToken — return immediately
    return jsonResult(process), nil, nil
}
```

### ops/progress.go — Reusable PollProcess

```go
// PollProcess polls a process until terminal state. MCP-agnostic.
// onProgress callback: func(message string, progress, total float64)
func PollProcess(ctx context.Context, client platform.Client, processID string, onProgress func(string, float64, float64)) (*platform.Process, error)
```

**Polling interval** (PRD §6.1): 2s initial, step-up to 5s after 30s.
**Timeout**: 10 minutes.
**Terminal states**: FINISHED, FAILED, CANCELED.

**Reference**: `mcp60/src/responses/stream.go:43-260` — StreamWithProgress pattern. Key differences:
- Source uses `zerops-go` SDK directly in the stream helper
- ZCP keeps ops layer MCP-agnostic — callback pattern instead
- Source polls at 500ms (`stream.go:104`); ZCP uses 2s→5s step-up per PRD

---

## Phase 3 + Phase 5 Implementation Plan

### Ordered Implementation Sequence

**Phase 3: MCP Layer** (depends on Phase 1 platform + Phase 2 ops being complete)

| # | File | Depends On | Description |
|---|------|-----------|-------------|
| 24a | `tools/convert.go` | platform/errors.go | Error conversion + helper functions |
| 24b | `tools/context.go` | ops/context.go | zerops_context handler (simplest — no params, static) |
| 24c | `tools/workflow.go` | ops/workflow.go | zerops_workflow handler |
| 24d | `tools/discover.go` | ops/discover.go | zerops_discover handler |
| 24e | `tools/knowledge.go` | knowledge/ | zerops_knowledge handler |
| 24f | `tools/validate.go` | ops/validate.go | zerops_validate handler |
| 24g | `tools/logs.go` | ops/logs.go | zerops_logs handler |
| 24h | `tools/events.go` | ops/events.go | zerops_events handler |
| 24i | `tools/process.go` | ops/process.go | zerops_process handler |
| 24j | `tools/env.go` | ops/env.go | zerops_env handler (mixed sync/async) |
| 24k | `tools/manage.go` | ops/manage.go | zerops_manage handler (async with progress) |
| 24l | `tools/import.go` | ops/import.go | zerops_import handler (mixed dryRun) |
| 24m | `tools/delete.go` | ops/delete.go | zerops_delete handler (async with confirm) |
| 24n | `tools/subdomain.go` | ops/subdomain.go | zerops_subdomain handler (async, idempotent) |
| 24o | `tools/deploy.go` | ops/deploy.go, auth/ | zerops_deploy handler (SSH + local) |
| 25 | `server/server.go` | all tools/* | Server setup, tool + resource registration |
| 26 | `server/instructions.go` | — | Ultra-minimal init message |
| 27 | `cmd/zcp/main.go` | server/, auth/, platform/, knowledge/ | Entrypoint |

**Phase 5: Init Subcommand**

| # | File | Depends On | Description |
|---|------|-----------|-------------|
| 33 | `init/init.go` | content/ | Init orchestrator |
| 34 | `init/templates.go` | content/ | Template rendering |
| 35 | `cmd/zcp/main.go` (update) | init/ | Add init dispatch |

### Dependencies Between Files

```
convert.go ← ALL tool handlers (error conversion)
tools/*    ← server/server.go (registration)
server/*   ← cmd/zcp/main.go (creation + run)
content/   ← init/ (templates), ops/workflow.go (shared content)
```

### Test Strategy for Phase 3

Each tool handler gets:
1. **Unit test** (mock): In-memory MCP + ops with `platform.MockClient`
   - Verify input validation (missing required params)
   - Verify correct ops function is called with right params
   - Verify MCP response format (JSON content, isError flag)
   - Verify error conversion (PlatformError → MCP error JSON)

2. **API test** (`-tags api`): In-memory MCP + real `ZeropsClient`
   - Full chain: MCP request → tool → ops → platform → API → MCP response
   - Verify real data produces valid MCP responses

**Phase 3 gate**: `go test ./internal/tools/... -tags api -v` — All tools tested end-to-end with real API.

---

## Key Design Decisions

1. **No executor/subprocess**: Tools call ops functions directly, not CLI subprocesses.

2. **Tool handlers are thin**: ~20-30 lines each. Extract input → validate → call ops → convert result. No business logic in tool handlers.

3. **Ops stays MCP-agnostic**: PollProcess uses callback pattern, not direct `Session.NotifyProgress`. Tool handlers wrap the MCP notification into the callback.

4. **Instructions are minimal**: ~40-50 tokens vs source's ~250 tokens. `zerops_context` replaces the bloated instructions.

5. **Two new tools**: `zerops_context` (static platform knowledge) and `zerops_workflow` (workflow routing) replace information that was previously crammed into instructions.

6. **Deploy is fundamentally different**: SSH mode (primary) vs source's local-only `zcli push`. Needs `auth.Info` for token injection into SSH session.

7. **Error format preserved**: JSON `{code, error, suggestion}` matches source output format for LLM familiarity.

8. **Annotations from source**: Preserve `ToolAnnotations` from source handlers (ReadOnlyHint, DestructiveHint, IdempotentHint).

9. **`boolPtr` helper**: Source uses `boolPtr(b bool) *bool` for optional `*bool` annotation fields (`zaia-mcp/internal/tools/annotations.go:5`). ZCP needs same helper.
