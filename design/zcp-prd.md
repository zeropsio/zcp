# ZCP-MCP: Product Requirements Document

## Context

Currently the Zerops MCP integration uses two separate binaries:
- **zaia** (Go CLI) — platform client, auth, knowledge/BM25, business logic
- **zaia-mcp** (MCP server) — thin wrapper that shells out to zaia as a subprocess

This architecture has friction: two binaries to install/deploy, subprocess overhead, JSON serialization/deserialization between processes, and complexity in error propagation.

**ZCP-MCP** merges both into a single Go binary MCP server that calls the Zerops API directly. It is local-only, connects via STDIO, and is project-scoped. The output of this PRD is a document for autonomous LLM agents to implement in TDD fashion.

---

## 1. Scope

### In Scope (v1)
- Single binary MCP server over STDIO transport
- Direct Zerops API calls via `zerops-go` SDK (no subprocess)
- 12 MCP tools (same as current zaia-mcp)
- BM25 knowledge search with embedded docs
- Progress notifications for async operations (MCP ProgressToken protocol)
- Deploy tool as zcli subprocess wrapper
- Token from env var with optional zcli fallback

### Out of Scope (v2+)
- StreamableHTTP transport (architecture is transport-agnostic, ready for later)

---

## 2. Architecture

### 2.1 Package Structure

```
cmd/zcp/main.go                → Entrypoint: env parsing, server creation, signal handling, STDIO run
internal/
  server/
    server.go                  → MCP server setup, tool + resource registration
    instructions.go            → Embedded LLM instructions (~250 tokens). MUST list all 12 ZCP tool names:
                                  zerops_discover, zerops_manage, zerops_env, zerops_logs, zerops_deploy,
                                  zerops_import, zerops_validate, zerops_knowledge, zerops_process,
                                  zerops_delete, zerops_subdomain, zerops_events.
                                  Do NOT copy zaia-mcp's "configure" reference — ZCP has separate tools.
  tools/                       → MCP tool handlers (thin wrappers over ops)
    discover.go                → zerops_discover
    manage.go                  → zerops_manage
    env.go                     → zerops_env
    logs.go                    → zerops_logs
    deploy.go                  → zerops_deploy (zcli subprocess)
    import.go                  → zerops_import
    validate.go                → zerops_validate
    knowledge.go               → zerops_knowledge
    process.go                 → zerops_process
    delete.go                  → zerops_delete
    subdomain.go               → zerops_subdomain
    events.go                  → zerops_events
    convert.go                 → PlatformError → MCP result conversion
  ops/                         → Business logic, validation, orchestration
    discover.go, manage.go, env.go, logs.go, import.go, validate.go,
    delete.go, subdomain.go, events.go, process.go
    helpers.go                 → resolveServiceID, hostname helpers, time parsing
    progress.go                → Reusable PollProcess(ctx, processID, onProgress callback) helper.
                                  Ops stays MCP-agnostic — tool handler passes notification callback.
  platform/                    → Zerops API client abstraction
    client.go                  → Client interface (24 methods) + LogFetcher interface
    zerops.go                  → ZeropsClient implementation (zerops-go SDK)
    types.go                   → All domain types (NEW file split — source has types in client.go)
    errors.go                  → Error codes (29 for ZCP), PlatformError, mapSDKError
    logfetcher.go              → ZeropsLogFetcher (HTTP-based log backend)
    mock.go                    → Mock + MockLogFetcher (builder pattern, thread-safe)
  auth/
    auth.go                    → Token resolution (env var → zcli fallback), validate, discover project
  knowledge/                   → BM25 search engine
    engine.go                  → Store, Search(), List(), Get()
    documents.go               → Document type, go:embed, frontmatter parsing
    query.go                   → Query expansion, suggestions, snippets
    embed/                     → 65+ embedded markdown docs
```

### 2.2 Dependency Flow

```
cmd/zcp/main.go
  │
  v
internal/server (MCP server setup)
  │
  ├──> internal/tools (MCP tool handlers)
  │      │
  │      v
  │    internal/ops (business logic)
  │      │
  │      ├──> internal/platform (Zerops API client)
  │      ├──> internal/auth (token + project discovery)
  │      └──> internal/knowledge (BM25 search)
  │
  └──> internal/knowledge (MCP resource registration)
```

**Rules**:
- `tools/` depends on `ops/` for business logic. May import `platform/` for types only (e.g. `PlatformError`), but NEVER calls `platform.Client` methods directly.
- `ops/` depends on `platform/`, `auth/`, `knowledge/`
- `platform/` depends only on `zerops-go` SDK and stdlib
- `auth/` depends on `platform/` (for GetUserInfo, ListProjects)
- `knowledge/` depends only on `bleve` and stdlib
- No upward dependencies, no cycles

### 2.3 Key Difference from Current Architecture

| Aspect | zaia + zaia-mcp (current) | zcp (new) |
|--------|--------------------------|-----------|
| Binaries | 2 (zaia CLI + zaia-mcp server) | 1 (zcp MCP server) |
| API access | zaia-mcp → subprocess → zaia → zerops-go SDK | zcp → zerops-go SDK directly |
| Auth storage | File-based (zaia.data), login command | In-memory, env var token, no login cmd |
| CLI framework | Cobra (18 commands) | None (pure MCP server) |
| Error flow | SDK error → zaia JSON → parse → MCP result | SDK error → PlatformError → MCP result |

---

## 3. Auth Flow

### 3.1 Token Resolution (priority order)

1. **`ZEROPS_TOKEN` env var** — Primary. Set in MCP server config.
2. **zcli fallback** — If ZEROPS_TOKEN not set, read `~/Library/Application Support/zerops/cli.data` (macOS) or `~/.config/zerops/cli.data` (Linux). Extract `.Token`, `.RegionData.address`, and `.ScopeProjectId` from JSON.
3. WHEN neither available → fatal error: "ZEROPS_TOKEN not set and zcli not logged in"

**zcli cli.data format** (discovered at `/Users/macbook/Library/Application Support/zerops/cli.data`):
```json
{
  "Token": "<PAT>",
  "RegionData": {
    "name": "prg1",
    "isDefault": true,
    "address": "api.app-prg1.zerops.io",
    "guiAddress": null
  },
  "ScopeProjectId": null,
  "ProjectVpnKeyRegistry": {"<key>": {"..."}}
}
```

**Notes**:
- `ScopeProjectId` is JSON `null` when unset (not empty string). Go's `json.Unmarshal` into `*string` will produce `nil` for `null` — use pointer type.
- `ScopeProjectId` is set by `zcli scope project` command. When set, ZCP uses it directly instead of project discovery via ListProjects.
- `ProjectVpnKeyRegistry`, `isDefault`, `guiAddress` are additional fields in cli.data. Go's `json.Unmarshal` silently ignores unknown fields (OK).
- ZCP only reads `Token`, `RegionData.address`, and `ScopeProjectId`.

### 3.2 Startup Sequence

**ZEROPS_TOKEN path:**
```
1. Read ZEROPS_TOKEN env var
2. Resolve API host: ZEROPS_API_HOST env var (default: "api.app-prg1.zerops.io")
3. Create ZeropsClient(token, apiHost)
4. GetUserInfo(ctx) → clientID
   FAIL → fatal: "Authentication failed: invalid or expired token"
5. ListProjects(ctx, clientID) → []Project
   0 projects → fatal: "Token has no project access"
   2+ projects → fatal: "Token accesses N projects; use project-scoped token"
   1 project → use it
6. Cache: auth.Info{Token, APIHost, ClientID, ProjectID, ProjectName}
7. Create MCP server with auth context injected into ops layer
8. Run STDIO transport
```

**zcli fallback path:**
```
1. Read cli.data → Token, RegionData.address, ScopeProjectId
2. Resolve API host: RegionData.address from cli.data
3. Create ZeropsClient(token, apiHost)
4. GetUserInfo(ctx) → clientID
   FAIL → fatal: "Authentication failed: invalid or expired token"
5a. IF ScopeProjectId is set:
    GetProject(ctx, ScopeProjectId) → validate it exists and is accessible
    FAIL → fatal: "Scoped project not found or inaccessible; run 'zcli scope project'"
5b. IF ScopeProjectId is null:
    ListProjects(ctx, clientID) → same logic as ZEROPS_TOKEN path (must be exactly 1)
6. Cache: auth.Info{Token, APIHost, ClientID, ProjectID, ProjectName}
7. Create MCP server with auth context injected into ops layer
8. Run STDIO transport
```

### 3.3 Auth Info (in-memory, no file persistence)

```go
type Info struct {
    Token       string
    APIHost     string
    ClientID    string
    ProjectID   string
    ProjectName string
}
```

**Naming**: Use `auth.Info` (not `auth.Context`) to avoid collision with `context.Context`. Source zaia uses `auth.Credentials` — `auth.Info` is a cleaner fit for ZCP since it includes project metadata beyond just credentials.

### 3.4 Project-Scoped Filtering (API Bug Workaround)

The Zerops API has a known bug where scoped tokens return data from all projects in search endpoints (GET on individual entities correctly returns 403). This affects `service-stack/search`, `project/search`, and process search endpoints.

**Workaround:** Filter by `auth.Info.ProjectID` client-side in the **ops/ layer** (not platform/) using a shared helper in `ops/helpers.go`:
- `ops/discover.go` — filter ListServices results where `svc.ProjectID == projectID`
- `ops/events.go` — filter SearchProcesses where `p.ProjectId == projectID`
- `ops/events.go` — filter SearchAppVersions where `av.ProjectId == projectID`

This filter is designed to be easy to remove per-endpoint once the API is fixed. The bug will be fixed server-side — this is a temporary workaround. All filter call sites MUST include the comment `// API-BUG-WORKAROUND: remove when search endpoints are project-scoped` to enable grep-based removal.

---

## 4. Platform Client Interface

### 4.1 Interface (24 methods)

Port from: `../zaia/internal/platform/client.go`

```
Auth:       GetUserInfo(ctx) → (*UserInfo, error)
Projects:   ListProjects(ctx, clientID) → ([]Project, error)
            GetProject(ctx, projectID) → (*Project, error)
Services:   ListServices(ctx, projectID) → ([]ServiceStack, error)
            GetService(ctx, serviceID) → (*ServiceStack, error)
Lifecycle:  StartService(ctx, serviceID) → (*Process, error)
            StopService(ctx, serviceID) → (*Process, error)
            RestartService(ctx, serviceID) → (*Process, error)
            SetAutoscaling(ctx, serviceID, params) → (*Process, error)  // may return nil Process
Env:        GetServiceEnv(ctx, serviceID) → ([]EnvVar, error)
            SetServiceEnvFile(ctx, serviceID, content) → (*Process, error)
            DeleteUserData(ctx, userDataID) → (*Process, error)
            GetProjectEnv(ctx, projectID) → ([]EnvVar, error)
            CreateProjectEnv(ctx, projectID, key, content, sensitive) → (*Process, error)
            DeleteProjectEnv(ctx, envID) → (*Process, error)
Import:     ImportServices(ctx, projectID, yamlContent) → (*ImportResult, error)
Delete:     DeleteService(ctx, serviceID) → (*Process, error)
Process:    GetProcess(ctx, processID) → (*Process, error)
            CancelProcess(ctx, processID) → (*Process, error)
Subdomain:  EnableSubdomainAccess(ctx, serviceID) → (*Process, error)
            DisableSubdomainAccess(ctx, serviceID) → (*Process, error)
Logs:       GetProjectLog(ctx, projectID) → (*LogAccess, error)
Activity:   SearchProcesses(ctx, projectID, limit) → ([]ProcessEvent, error)
            SearchAppVersions(ctx, projectID, limit) → ([]AppVersionEvent, error)
```

Separate interface: `LogFetcher` with `FetchLogs(ctx, *LogAccess, LogFetchParams) → ([]LogEntry, error)`

### 4.2 Key Types

Port from: `../zaia/internal/platform/client.go` (types section, lines 70-253).

**Note**: Source has all types defined in `client.go` alongside the interface — there is no `types.go` in zaia. ZCP splits them into a separate `platform/types.go` for clarity. This is a **new file split**, not a direct port of an existing file.

Core: `UserInfo`, `Project`, `ServiceStack`, `ServiceTypeInfo`, `Port`, `CustomAutoscaling`, `AutoscalingParams`, `Process`, `ServiceStackRef`, `EnvVar`, `ImportResult`, `ImportedServiceStack`, `APIError`, `LogAccess`, `LogFetchParams`, `LogEntry`, `ProcessEvent`, `AppVersionEvent`, `BuildInfo`, `UserRef`

**Note**: `Process.Status` carries **normalized** values after mapping in zerops.go: `DONE→FINISHED`, `CANCELLED→CANCELED`. Source `client.go` comments document raw API statuses, but ZCP consumers always see normalized values. `Process.FailReason` (`*string`) contains the failure reason when status is FAILED — must be exposed in tool responses.

### 4.3 ZeropsClient Implementation

Port from: `../zaia/internal/platform/zerops.go` (1025 lines)

Key details:
- Uses `zerops-go` SDK (`sdk.Handler`)
- Lazy clientID caching via `getClientID()` — zaia source uses racy string check; ZCP MUST use `sync.Once` for thread safety (improvement over source)
- `mapSDKError()` converts all SDK errors to PlatformError
- `SetAutoscaling` can return nil Process (sync, no async tracking)
- Status normalization: "DONE" → "FINISHED", "CANCELLED" → "CANCELED"

### 4.4 Error Codes (29 for ZCP)

Port from: `../zaia/internal/platform/errors.go` (31 static codes in source)

Skip 4 CLI-specific setup codes (ErrSetupDownloadFailed, ErrSetupInstallFailed, ErrSetupConfigFailed, ErrSetupUnsupportedOS) — irrelevant for MCP server. Also do NOT port `ExitCodeForError()` or `MapHTTPError()` suggestion text referencing "zaia login" — these are CLI-specific.

27 static codes (after skipping 4 CLI-specific):
```
AUTH_REQUIRED, AUTH_INVALID_TOKEN, AUTH_TOKEN_EXPIRED, AUTH_API_ERROR,
TOKEN_NO_PROJECT, TOKEN_MULTI_PROJECT,
SERVICE_NOT_FOUND, SERVICE_REQUIRED, CONFIRM_REQUIRED,
FILE_NOT_FOUND, ZEROPS_YML_NOT_FOUND,
INVALID_ZEROPS_YML, INVALID_IMPORT_YML, IMPORT_HAS_PROJECT,
INVALID_SCALING, INVALID_PARAMETER, INVALID_ENV_FORMAT,
INVALID_HOSTNAME, UNKNOWN_TYPE,
PROCESS_NOT_FOUND, PROCESS_ALREADY_TERMINAL,
PERMISSION_DENIED,
API_ERROR, API_TIMEOUT, API_RATE_LIMITED, NETWORK_ERROR,
INVALID_USAGE
```

2 dynamic codes (generated in `mapAPIError()` for subdomain idempotency):
```
SUBDOMAIN_ALREADY_ENABLED, SUBDOMAIN_ALREADY_DISABLED
```

### 4.5 MockClient

Port from: `../zaia/internal/platform/mock.go`

Builder pattern: `NewMock().WithUserInfo(...).WithServices(...).WithError("MethodName", err)`
Thread-safe with `sync.RWMutex`. Compile-time interface check.

---

## 5. MCP Tools Specification

### 5.1 All 12 Tools

| Tool | Type | Key Behavior |
|------|------|-------------|
| `zerops_discover` | Sync | Project info + service list. Optional filter by hostname. Optional includeEnvs. |
| `zerops_manage` | Async | action={start,stop,restart,scale} + serviceHostname. Scale accepts CPU/RAM/disk params (see parameter mapping below). |
| `zerops_env` | Mixed | action={get,set,delete}. Service-level or project-level. Variables in KEY=value format. |
| `zerops_logs` | Sync | 2-step: GetProjectLog → LogFetcher. Severity, since, limit, search, buildId filters. |
| `zerops_deploy` | Sync (blocking) | zcli subprocess wrapper. workingDir + serviceId params. Blocks until zcli exits — no process ID to poll. |
| `zerops_import` | Mixed | Inline content or filePath. dryRun for validation (sync), real import (async). |
| `zerops_validate` | Sync | Offline YAML validation. zerops.yml or import.yml. No API needed. |
| `zerops_knowledge` | Sync | BM25 search. Query expansion. Full top-result content. Suggestions. |
| `zerops_process` | Sync | Status check or cancel. Normalizes DONE→FINISHED, CANCELLED→CANCELED. |
| `zerops_delete` | Async | Requires confirm=true safety gate. Resolves hostname → serviceID. |
| `zerops_subdomain` | Async | enable/disable. Idempotent (already-enabled = success). |
| `zerops_events` | Sync | Merged process + appVersion timeline. Optional service filter. Default limit 50. |

**Manage/scale parameter mapping** (tool params → `AutoscalingParams` fields):

| Tool Parameter | API Field (`AutoscalingParams`) |
|----------------|-------------------------------|
| `cpuMode` | `CpuMode` (SHARED / DEDICATED) |
| `minCpu` / `maxCpu` | `VerticalMinCpu` / `VerticalMaxCpu` |
| `minRam` / `maxRam` | `VerticalMinRam` / `VerticalMaxRam` |
| `minDisk` / `maxDisk` | `VerticalMinDisk` / `VerticalMaxDisk` |
| `minContainers` / `maxContainers` | `HorizontalMinCount` / `HorizontalMaxCount` |
| `startContainers` | (no direct API field — controls initial container count at creation) |

The ops layer maps user-friendly tool params to `AutoscalingParams` struct fields. zaia-mcp builds sparse CLI args (`--min-containers`, etc.) which zaia internally maps — ZCP maps directly.

### 5.2 Tool → MCP Error Conversion

PlatformError → MCP CallToolResult with IsError:true:
```json
{"content": [{"type": "text", "text": "{\"code\":\"SERVICE_NOT_FOUND\",\"error\":\"...\",\"suggestion\":\"...\"}"}], "isError": true}
```

**Process failure reason**: When a process status is FAILED, include `failReason` in the MCP result JSON. Source `Process.FailReason` (`*string` field in `client.go:148`) must be exposed in tool responses so the LLM agent can diagnose deployment failures, import errors, etc.

New implementation (NOT a port). zaia-mcp's convert.go parses CLI subprocess JSON envelopes — ZCP converts PlatformError → MCP result directly. Use zaia-mcp's JSON error structure as inspiration for the output format only.

---

## 6. Streaming / Progress Notifications

### 6.1 Pattern

For async operations (manage, env set/delete, import, delete, subdomain):

1. Execute operation → Process{ID, Status: PENDING}
2. IF client provided ProgressToken:
   - `ops/progress.go` provides reusable `PollProcess(ctx, client, processID, onProgress)` helper
   - `onProgress` is a callback: `func(message string, progress, total float64)`
   - **Tool handler** wraps `req.Session.NotifyProgress()` into the callback — ops stays MCP-agnostic
   - PollProcess polls GetProcess periodically, calls onProgress, returns final Process
   - Continue until FINISHED/FAILED/CANCELED or timeout (10min)
   - Polling interval: 2s initial, step-up to 5s after 30s (reduces API load for slow operations while keeping fast ones responsive)
   - Return final status
3. IF no ProgressToken:
   - Return process info immediately (no polling)
   - Client uses zerops_process to check manually

The ops layer provides the polling mechanism; the tool handler provides the notification strategy.
This keeps ops/ testable without MCP dependencies — tests pass a recording callback.

### 6.2 Progress Notification (works over STDIO, verified for go-sdk v1.2.0)

```go
if progressToken := req.Params.GetProgressToken(); progressToken != nil {
    req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
        ProgressToken: progressToken,
        Message:       "Restarting service api...",
        Progress:      50,
        Total:         100,
    })
}
```

Reference for inspiration: `/Users/macbook/Sites/mcp60/src/responses/stream.go`

---

## 7. Knowledge System

Port from: `../zaia/internal/knowledge/`

- `Store` with `docs map[string]*Document` and `index bleve.Index` (in-memory)
- `Store` exposes: `List()`, `Get(uri)`, `Search(query, limit)`, `GenerateSuggestions()`
- 65+ embedded markdown docs via `go:embed embed/**/*.md` — **copied into ZCP repo** (`internal/knowledge/embed/`), not symlinked. Independent of zaia repo. Update manually or via sync script.
- BM25 with field boosts: title (2.0), keywords (1.5), content (1.0)
- Query expansion: 25 aliases that **expand** (not replace): `redis` → `redis valkey`, `postgres` → `postgres postgresql`, etc.
- MCP resources: `zerops://docs/{+path}` for direct document access

**Race condition**: Source `GetEmbeddedStore()` uses bare nil check without synchronization (TOCTOU race). ZCP MUST use `sync.Once` for knowledge store initialization (same fix as clientID).

**Panic on init failure**: `engine.go` calls `panic()` if BM25 index creation or batch indexing fails. This is intentional fail-fast behavior — acceptable for init. Binary crashes if embedded docs are corrupted.

**UTF-8 safety**: Source `extractSnippet()` in `query.go` slices by byte position, which can split multi-byte UTF-8 characters. Knowledge docs are English (low risk), but ZCP should use rune-safe slicing as a correctness improvement.

---

## 8. Deploy Tool (zcli Wrapper)

### 8.1 Approach

Shell out to `zcli push` binary (same as current zaia-mcp). The zcli binary is a Node.js CLI (`@zerops/zcli@v1.0.58`).

### 8.2 Auth Sharing

zcli stores its token at `~/Library/Application Support/zerops/cli.data`. When zcp starts:
- If using zcli fallback auth, the same token is already in zcli
- If using ZEROPS_TOKEN env var, zcli may have a different token

For deploy, zcli uses its OWN token from cli.data. zcli does NOT support a `--zeropsToken` flag. This means:
- WHEN user logged into zcli → deploy works without extra config
- WHEN user only has ZEROPS_TOKEN → return clear error: "Deploy requires zcli login. Run 'zcli login' first."

**Auth detection approach**: Before invoking `zcli push`, check if `cli.data` file exists and contains a non-empty `Token` field. If file is missing or token is empty → return the clear error above. This is faster than letting `zcli push` fail and parsing the error, and avoids subprocess overhead for a predictable failure.

### 8.3 zcli Subprocess Pattern

Port from: `../zaia-mcp/internal/executor/executor.go`
- `exec.CommandContext` with resolved PATH (handles nvm/homebrew paths)
- Non-zero exit is NOT a Go error (zcli outputs JSON on stdout)
- Context cancellation checked first
- WHEN zcli not found → clear error with install instructions

---

## 9. Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `ZEROPS_TOKEN` | No* | — | Zerops PAT. *Falls back to zcli token if not set. |
| `ZEROPS_API_HOST` | No | `api.app-prg1.zerops.io` | API endpoint. Auto-detected from zcli if using fallback. |
| `ZCP_LOG_LEVEL` | No | `warn` | Stderr log level (debug, info, warn, error) |

### MCP Client Config (e.g., claude_desktop_config.json)

```json
{
  "mcpServers": {
    "zerops": {
      "command": "/path/to/zcp",
      "env": {
        "ZEROPS_TOKEN": "ya0_2DI.PMpvyEq9Jtp_qzL7l4TRyR-cX6qqxuYFTc_SQgnOaHl-tyRoAFOmPjCD-i"
      }
    }
  }
}
```

---

## 10. Dependencies

| Package | Version | Purpose | Source |
|---------|---------|---------|--------|
| `github.com/modelcontextprotocol/go-sdk` | v1.2.0 | MCP server, tools, STDIO | zaia-mcp. Use latest stable at implementation time. |
| `github.com/zeropsio/zerops-go` | v1.0.16 | Zerops API SDK | zaia. Verify latest version at implementation time (v1.0.16 may not be latest). |
| `github.com/blevesearch/bleve/v2` | v2.5.7 | BM25 full-text search | zaia |
| `gopkg.in/yaml.v3` | latest | YAML parsing (validate) | zaia |

NOT needed: `spf13/cobra` (no CLI), subprocess executor infrastructure.

---

## 11. Testing Strategy

### 11.1 Test Layers

| Layer | Packages | Mock | t.Parallel |
|-------|----------|------|------------|
| Platform | `platform/` | None (types, error mapping) | Yes |
| Auth | `auth/` | `platform.Mock` | Yes |
| Knowledge | `knowledge/` | None (in-memory) | Yes |
| Ops | `ops/` | `platform.Mock` + `MockLogFetcher` | Yes |
| Tools | `tools/` | In-memory MCP + ops with Mock | Yes |
| Integration | `integration/` | In-memory MCP + Mock | Yes |
| E2E | `e2e/` | Real Zerops API (`-tags e2e`) | No |

### 11.2 Tool Test Pattern (in-memory MCP, no subprocess)

Uses go-sdk `InMemoryTransports` for zero-subprocess testing:

```go
// Setup: create server with mock platform client
mock := platform.NewMock().WithUserInfo(...).WithServices(...)
srv := server.NewWithClient(mock)

// Create in-memory transport pair
serverTransport, clientTransport := mcp.NewInMemoryTransports()
_, err := srv.Server().Connect(ctx, serverTransport, nil)

client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v1"}, nil)
session, err := client.Connect(ctx, clientTransport, nil)

// Call tool via client session
result, err := session.CallTool(ctx, &mcp.CallToolParams{
    Name:      "zerops_discover",
    Arguments: map[string]any{"serviceHostname": "api"},
})
// Assert on result.Content, result.IsError
```

### 11.3 TDD per framework-plan.md

Every implementation unit: RED (failing test) → GREEN (minimal impl) → REFACTOR.
Test files reference design docs: `// Tests for: design/discover.md § Behavioral Contract`
Table-driven tests, `Test{Op}_{Scenario}_{Result}` naming.

---

## 12. Non-Functional Requirements

- **Timeouts**: API 30s, log fetch 30s, progress polling 10min, deploy (zcli) 5min
- **Graceful shutdown**: SIGINT/SIGTERM → cancel context (aborts in-flight ops including PollProcess) → close STDIO transport → exit. Shutdown timeout: 5s hard deadline after signal. No drain phase needed — STDIO is not a listener, in-flight HTTP requests abort via context cancellation.
- **Context propagation**: All layers pass ctx. Timeout is via `http.Client.Timeout` (30s), not per-call `context.WithTimeout`. Context cancellation from MCP transport works (client closes → ctx cancelled → in-flight HTTP aborts).
- **Thread safety**: `ZeropsClient.clientID` via sync.Once. Knowledge Store via sync.Once. Tool handlers concurrent-safe.
- **No retry policy in v1**: Neither zerops-go SDK nor zaia implement retries. Transient API failures (5xx, network blips) are not retried. Known limitation — consider adding retry with backoff in v2.
- **Binary**: <30MB. `-ldflags "-s -w"`. UPX for Linux CI builds.
- **Module**: `github.com/zeropsio/zcp`, Go 1.24.0

---

## 13. Implementation Sequencing

### Phase 1: Foundation
1. `platform/types.go` — Domain types
2. `platform/errors.go` — Error codes, PlatformError, mapping
3. `platform/client.go` — Client interface (24 methods) + LogFetcher
4. `platform/mock.go` — Mock + MockLogFetcher
5. `platform/zerops.go` — ZeropsClient (zerops-go)
6. `platform/logfetcher.go` — ZeropsLogFetcher
7. `auth/auth.go` — Token resolution, validation, project discovery
8. `knowledge/` — Store, documents, query, embed directory

### Phase 2: Business Logic
9. `ops/helpers.go` — Service resolution, time parsing
10. `ops/discover.go` — Discover operation
11. `ops/manage.go` — Start/stop/restart/scale
12. `ops/env.go` — Env get/set/delete
13. `ops/logs.go` — 2-step log fetch
14. `ops/import.go` — Import with dry-run
15. `ops/validate.go` — YAML validation (offline)
16. `ops/delete.go` — Service deletion
17. `ops/subdomain.go` — Subdomain (idempotent)
18. `ops/events.go` — Activity timeline merge
19. `ops/process.go` — Process status/cancel

### Phase 3: MCP Layer
20. `tools/` — 12 tool handlers + convert.go
21. `server/server.go` — MCP server + registration
22. `cmd/zcp/main.go` — Entrypoint

### Phase 4: Streaming + Deploy
23. `ops/progress.go` — Reusable PollProcess helper with callback (MCP-agnostic)
24. `tools/deploy.go` — zcli subprocess
25. Integration tests

Each phase follows TDD. Each unit = one RED-GREEN-REFACTOR cycle.

---

## 14. Critical Source Files

| File | What to port | Lines |
|------|-------------|-------|
| `../zaia/internal/platform/client.go` | Interface + types | ~253 |
| `../zaia/internal/platform/zerops.go` | SDK implementation | ~1025 |
| `../zaia/internal/platform/errors.go` | Error codes + mapping | ~200 |
| `../zaia/internal/platform/mock.go` | Mock builder | ~200 |
| `../zaia/internal/auth/storage.go` | **Reference only** — config path patterns. ZCP reads `cli.data` (different schema from `zaia.data`). New implementation, not a port. | ~130 |
| `../zaia/internal/auth/manager.go` | **Reference only** — startup flow patterns. ZCP has no Login/Logout, no file persistence, reads env var or cli.data read-only. New implementation. | ~150 |
| `../zaia/internal/knowledge/engine.go` | BM25 search | ~200 |
| `../zaia/internal/knowledge/query.go` | Query expansion | ~170 |
| `../zaia-mcp/internal/server/server.go` | MCP setup pattern | ~141 |
| `../zaia-mcp/internal/tools/convert.go` | JSON error format inspiration only (NOT a port — different responsibility) | ~100 |
| `/Users/macbook/Sites/mcp60/src/responses/stream.go` | Progress polling pattern | ~250 |
| `/Users/macbook/Sites/mcp60/src/tools/base.go` | Tool type patterns | ~285 |

---

## 15. Verification

After implementation, verify:

1. `go build -o bin/zcp ./cmd/zcp` — Binary builds
2. `go test ./... -count=1 -short` — All tests pass
3. `golangci-lint run ./...` — No lint errors
4. Manual: `ZEROPS_TOKEN=<token> ./bin/zcp` — Server starts, responds to MCP initialize
5. MCP tool call: `zerops_discover` returns project + services
6. MCP tool call: `zerops_knowledge {query: "postgresql"}` returns docs
7. MCP tool call: `zerops_manage {action: "restart", serviceHostname: "api"}` returns process
8. Progress notification test: async op with ProgressToken sends updates
9. Error handling: invalid token → proper MCP error response
10. Graceful shutdown: SIGINT during operation → clean exit
