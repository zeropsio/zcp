# ZCP-MCP: Product Requirements Document

> **Editing convention**: Do NOT use hardcoded counts (e.g. "14 tools", "24 methods", "29 error codes") in prose, headers, or references. The authoritative count is always the actual list/table in the relevant section. Hardcoded numbers go stale on every addition/removal and create false inconsistencies.

## Context

Currently the Zerops MCP integration uses two separate binaries:
- **zaia** (Go CLI) — platform client, auth, knowledge/BM25, business logic
- **zaia-mcp** (MCP server) — thin wrapper that shells out to zaia as a subprocess

This architecture has friction: two binaries to install/deploy, subprocess overhead, JSON serialization/deserialization between processes, and complexity in error propagation.

**ZCP-MCP** merges both into a single Go binary MCP server that calls the Zerops API directly.

It runs inside a **dedicated `zcp` service** (`type: zcp@1`) within the Zerops project it manages. This service comes pre-installed with:
- **code-server** (VS Code in browser) as the user-facing interface
- **Claude Code** (or another LLM code tool) pre-configured in the terminal
- **zcli** built from source and pre-authenticated
- **SSH access** to all sibling services on the VXLAN private network

The ZCP MCP binary is downloaded during `initCommands` and pre-configured as Claude Code's MCP server. The user opens the service's subdomain URL, gets VS Code with Claude Code already running and connected to the MCP server.

### Execution Model

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                              ZEROPS PROJECT                                      │
├─────────────────────────────────────────────────────────────────────────────────┤
│                                                                                  │
│   ┌─────────────┐    ┌─────────────┐    ┌─────────────┐    ┌─────────────┐      │
│   │   appdev    │    │  appstage   │    │ postgresql  │    │   valkey    │      │
│   │  (runtime)  │    │  (runtime)  │    │  (managed)  │    │  (managed)  │      │
│   │             │    │             │    │             │    │             │      │
│   │  SSH ✓      │    │  SSH ✓      │    │  NO SSH     │    │  NO SSH     │      │
│   │  Mount ✓    │    │  Mount ✓    │    │  psql only  │    │  redis-cli  │      │
│   └──────┬──────┘    └──────┬──────┘    └──────┬──────┘    └──────┬──────┘      │
│          └──────────────────┴──────────────────┴──────────────────┘              │
│                              VXLAN Private Network                               │
│                                     │                                            │
│   ┌─────────────────────────────────┴──────────────────────────────────────┐     │
│   │                    ZCP SERVICE  (type: zcp@1)                           │     │
│   │                                                                         │     │
│   │   ┌─────────────────────────────────────────────────────────────┐       │     │
│   │   │  code-server (VS Code in browser)  :8080                    │       │     │
│   │   │                                                              │       │     │
│   │   │   ┌─────────────────────┐    ┌────────────────────────────┐ │       │     │
│   │   │   │  Claude Code        │───►│  ZCP binary (MCP, STDIO)  │ │       │     │
│   │   │   │  (terminal)         │◄───│                            │ │       │     │
│   │   │   │                     │    │  • MCP tools               │ │       │     │
│   │   │   │  Native bash:       │    │  • BM25 knowledge search   │ │       │     │
│   │   │   │  • SSH to services  │    │  • Progress notifications  │ │       │     │
│   │   │   │  • Mount FS         │    │  • Deploy via SSH + zcli   │ │       │     │
│   │   │   │  • psql, redis-cli  │    └────────────────────────────┘ │       │     │
│   │   │   │  • curl, test       │                                    │       │     │
│   │   │   └─────────────────────┘                                    │       │     │
│   │   └─────────────────────────────────────────────────────────────┘       │     │
│   │                                                                         │     │
│   │   Pre-installed: zcli, jq, yq, psql, redis-cli, SSH keys               │     │
│   │   CLAUDE.md: workflow guidance (downloaded at init)                      │     │
│   └─────────────────────────────────────────────────────────────────────────┘     │
│                                                                                   │
└───────────────────────────────────────────────────────────────────────────────────┘
```

**Key insight**: The agent operates in the same environment where production runs. What it sees is what production uses — same network, same DNS resolution, same env vars, same infrastructure. The user interacts through a browser — no local setup required.

### Three-Layer Architecture

```
┌──────────────────────────────────────────────────────────────────────────────┐
│  Workflow Layer  (zerops_workflow + CLAUDE.md)                                │
│    On-demand guidance, dev/stage patterns, bootstrap, verification            │
│    CLAUDE.md = user-replaceable. zerops_workflow = tool-based routing.        │
├──────────────────────────────────────────────────────────────────────────────┤
│  MCP Tool Layer  (this PRD)                                                  │
│    API operations: discover, manage, env, logs, deploy, import,              │
│    validate, knowledge, process, delete, subdomain, events                   │
│    Context loading: context (platform knowledge), workflow (guidance)         │
├──────────────────────────────────────────────────────────────────────────────┤
│  Agent Bash Layer  (native SSH/mount/tools)                                  │
│    Container exec, mount filesystems, runtime env vars,                      │
│    connectivity testing, live log tailing, build commands                     │
├──────────────────────────────────────────────────────────────────────────────┤
│  Platform  (Zerops API + VXLAN network)                                      │
│    API for service management, private network for direct access             │
└──────────────────────────────────────────────────────────────────────────────┘
```

This PRD defines **Layer 2** (MCP tools). The Workflow Layer lives in `zerops_workflow` (tool) and CLAUDE.md (user-replaceable file). The MCP init message (`instructions.go`) is a minimal relevance signal pointing to `zerops_context`. The Agent Bash Layer is the LLM agent's native capability (SSH, mount, psql, etc.).

The output of this PRD is a document for autonomous LLM agents to implement in TDD fashion.

---

## 1. Scope

### In Scope (v1)
- Single binary MCP server over STDIO transport
- Direct Zerops API calls via `zerops-go` SDK (no subprocess)
- MCP tools (current zaia-mcp tools + `zerops_context` + `zerops_workflow`)
- `zerops_context` — static platform knowledge loader (concepts, rules, service catalog)
- `zerops_workflow` — workflow routing (catalog without param, specific guidance with param)
- BM25 knowledge search with embedded docs
- Progress notifications for async operations (MCP ProgressToken protocol)
- Deploy tool with SSH mode (primary) and local fallback
- Token from `ZCP_API_KEY` service env var (primary), with optional zcli fallback for local dev
- Ultra-minimal MCP init message (~40-50 tokens) — pure relevance signal pointing to `zerops_context`
- `zcp init` subcommand — bootstraps CLAUDE.md, MCP config, hooks, SSH config
- In-container CLAUDE.md (user-replaceable) recommends `zerops_workflow` call before infrastructure work

### Out of Scope (v2+)
- StreamableHTTP transport (architecture is transport-agnostic, ready for later)
- `zerops_exec` tool for structured SSH command execution via MCP
- `zerops_mount` tool for filesystem mount management via MCP
- `zerops_verify` tool for structured endpoint verification
- `zerops_recipes` tool for live recipe/template search from Zerops API

---

## 2. Architecture

### 2.1 Package Structure

```
cmd/zcp/main.go                → Entrypoint: env parsing, server creation, signal handling, STDIO run.
                                  Two modes: `zcp` (no args) = MCP server, `zcp init` = bootstrap.
                                  Simple os.Args[1] check — NOT a CLI framework (no Cobra).
internal/
  server/
    server.go                  → MCP server setup, tool + resource registration
    instructions.go            → Ultra-minimal MCP instructions (~40-50 tokens).
                                  Pure relevance signal — the LLM decides if Zerops tools are needed.
                                  MUST include:
                                  1. Identity: ZCP = tools for Zerops PaaS infrastructure
                                  2. Scope: infrastructure, services, deployment, configuration, debugging
                                  3. Entry point: zerops_context (load platform knowledge when needed)
                                  MUST NOT include: tool lists, service types, rules, defaults.
                                  Those live in zerops_context (on-demand) and CLAUDE.md (user-configurable).
                                  Design principle: The init message exists so the LLM can answer one question:
                                  "Is the user asking about something that needs Zerops tools?" The LLM pays
                                  near-zero cost when Zerops isn't relevant, and loads full context on-demand.
                                  Why no tool list in init: Every tool goes through MCP's tools/list — the LLM
                                  already sees all tools with descriptions and schemas. The init message
                                  doesn't need to duplicate that.
  tools/                       → MCP tool handlers (thin wrappers over ops)
    discover.go                → zerops_discover
    manage.go                  → zerops_manage
    env.go                     → zerops_env
    logs.go                    → zerops_logs
    deploy.go                  → zerops_deploy (SSH mode primary, local fallback)
    import.go                  → zerops_import
    validate.go                → zerops_validate
    knowledge.go               → zerops_knowledge
    process.go                 → zerops_process
    delete.go                  → zerops_delete
    subdomain.go               → zerops_subdomain
    events.go                  → zerops_events
    context.go                 → zerops_context
    workflow.go                → zerops_workflow
    convert.go                 → PlatformError → MCP result conversion
  ops/                         → Business logic, validation, orchestration
    discover.go, manage.go, env.go, logs.go, import.go, validate.go,
    delete.go, subdomain.go, events.go, process.go
    helpers.go                 → resolveServiceID, hostname helpers, time parsing
    progress.go                → Reusable PollProcess(ctx, processID, onProgress callback) helper.
                                  Ops stays MCP-agnostic — tool handler passes notification callback.
    deploy.go                  → Deploy logic: SSH exec into source service, run zcli push.
                                  Local-mode fallback for development outside Zerops.
    context.go                 → Static content: general Zerops knowledge + service type catalog.
                                  Compiled string, not part of BM25 index. Updated with code changes.
    workflow.go                → Workflow catalog + per-workflow content. Reads from
                                  internal/content/ (shared with init/). Not part of BM25 index.
  content/                     → Shared embedded content (single source of truth)
    content.go                 → go:embed for workflow markdown, CLAUDE.md template, etc.
    workflows/                 → Embedded workflow guidance markdown files
    templates/                 → CLAUDE.md template, MCP config template, etc.
  init/
    init.go                    → Init orchestrator: runs all bootstrap steps (CLAUDE.md, MCP config,
                                  hooks, SSH config). Idempotent — running again resets to defaults.
    templates.go               → Uses internal/content/ for embedded templates. Generates files
                                  (CLAUDE.md, MCP config, SSH config, hooks) from shared content.
  platform/                    → Zerops API client abstraction
    client.go                  → Client interface + LogFetcher interface
    zerops.go                  → ZeropsClient implementation (zerops-go SDK)
    types.go                   → All domain types (NEW file split — source has types in client.go)
    errors.go                  → Error codes (29 for ZCP), PlatformError, mapSDKError
    logfetcher.go              → ZeropsLogFetcher (HTTP-based log backend)
    mock.go                    → Mock + MockLogFetcher (builder pattern, thread-safe)
    apitest/
      harness.go               → API test harness: real client setup, skip when no key,
                                  ctx with timeout, cleanup helpers. Shared across all
                                  -tags api and -tags e2e tests.
      cleanup.go               → Resource cleanup: delete services, wait for processes.
                                  Uses fresh context (test ctx may be cancelled).
  auth/
    auth.go                    → Token resolution (env var primary, zcli fallback for local dev),
                                  validate, discover project
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
  ├── "init" arg → internal/init (bootstrap: CLAUDE.md, MCP config, hooks, SSH)
  │                    └──> internal/content (shared embedded templates)
  │
  └── no args → MCP server mode:
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
      │      ├──> internal/knowledge (BM25 search)
      │      └──> internal/content (workflow content)
      │
      └──> internal/knowledge (MCP resource registration)
```

**Rules**:
- `tools/` depends on `ops/` for business logic. May import `platform/` for types only (e.g. `PlatformError`), but NEVER calls `platform.Client` methods directly.
- `ops/` depends on `platform/`, `auth/`, `knowledge/`, `content/`
- `platform/` depends only on `zerops-go` SDK and stdlib
- `auth/` depends on `platform/` (for GetUserInfo, ListProjects)
- `knowledge/` depends only on `bleve` and stdlib
- `content/` depends only on stdlib (go:embed, no external deps)
- `init/` depends on `content/` and stdlib (file I/O)
- No upward dependencies, no cycles

### 2.3 Key Difference from Current Architecture

| Aspect | zaia + zaia-mcp (current) | zcp (new) |
|--------|--------------------------|-----------|
| Binaries | 2 (zaia CLI + zaia-mcp server) | 1 (zcp MCP server) |
| Execution context | Local developer machine | Service inside Zerops project |
| API access | zaia-mcp → subprocess → zaia → zerops-go SDK | zcp → zerops-go SDK directly |
| Network position | Remote (over internet) | Local (VXLAN private network) |
| Auth storage | File-based (zaia.data), login command | In-memory, service env var, no login cmd |
| CLI framework | Cobra (18 commands) | None (MCP server + `zcp init` bootstrap, no Cobra) |
| Deploy path | Local `zcli push` from developer's filesystem | SSH into dev container → `zcli push` to stage |
| Error flow | SDK error → zaia JSON → parse → MCP result | SDK error → PlatformError → MCP result |
| Service access | API only | API + SSH + mount + client tools (via agent bash) |

### 2.4 What the Agent Can Do (by layer)

| Layer | Capability | Example |
|-------|-----------|---------|
| **MCP tools** | Zerops API operations | `zerops_discover`, `zerops_import`, `zerops_deploy` |
| **Agent bash** | SSH exec on runtime services | `ssh appdev "go build && go test"` |
| **Agent bash** | Filesystem mount (future: MCP tool) | Mount service FS for code editing |
| **Agent bash** | Managed service clients | `psql "$db_connectionString"`, `redis-cli -h valkey` |
| **Agent bash** | Network connectivity | `curl http://appdev:8080/health` |
| **Agent bash** | Live log tailing | `ssh appdev "tail -f /tmp/app.log"` |

---

## 3. Auth Flow

### 3.1 Token Resolution (priority order)

1. **`ZCP_API_KEY` env var** — Primary. Injected as service environment variable in Zerops. Always available in production deployment.
2. **zcli fallback** — Development convenience only. If ZCP_API_KEY not set (local development), read `~/.config/zerops/cli.data` (Linux) or `~/Library/Application Support/zerops/cli.data` (macOS). Extract `.Token`, `.RegionData.address`, and `.ScopeProjectId` from JSON.
3. WHEN neither available → fatal error: "ZCP_API_KEY not set and zcli not logged in"

**Production (inside Zerops)**: Path 1 always. `ZCP_API_KEY` is set as a service env var on the ZCP service. No filesystem fallback needed.

**Local development**: Path 2 as convenience. Developer runs ZCP binary on their machine for testing, zcli provides the token.

**zcli cli.data format** (reference for fallback path):
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
- ZCP only reads `Token`, `RegionData.address`, `RegionData.name`, and `ScopeProjectId`.

### 3.2 Startup Sequence

**ZCP_API_KEY path (production — primary):**
```
1. Read ZCP_API_KEY env var
2. Resolve API host: ZCP_API_HOST env var (default: "api.app-prg1.zerops.io")
3. Resolve region: ZCP_REGION env var (default: "prg1")
4. Create ZeropsClient(token, apiHost)
5. GetUserInfo(ctx) → clientID
   FAIL → fatal: "Authentication failed: invalid or expired token"
6. ListProjects(ctx, clientID) → []Project
   0 projects → fatal: "Token has no project access"
   2+ projects → fatal: "Token accesses N projects; use project-scoped token"
   1 project → use it
7. Cache: auth.Info{Token, APIHost, Region, ClientID, ProjectID, ProjectName}
8. Create MCP server with auth context injected into ops layer
9. Run STDIO transport
```

**zcli fallback path (local development only):**
```
1. Read cli.data → Token, RegionData.address, RegionData.name, ScopeProjectId
2. Resolve API host: ZCP_API_HOST env var → cli.data RegionData.address → default
3. Resolve region: ZCP_REGION env var → cli.data RegionData.name → default
4. Create ZeropsClient(token, apiHost)
5. GetUserInfo(ctx) → clientID
   FAIL → fatal: "Authentication failed: invalid or expired token"
6a. IF ScopeProjectId is set:
    GetProject(ctx, ScopeProjectId) → validate it exists and is accessible
    FAIL → fatal: "Scoped project not found or inaccessible; run 'zcli scope project'"
6b. IF ScopeProjectId is null:
    ListProjects(ctx, clientID) → same logic as ZCP_API_KEY path (must be exactly 1)
7. Cache: auth.Info{Token, APIHost, Region, ClientID, ProjectID, ProjectName}
8. Create MCP server with auth context injected into ops layer
9. Run STDIO transport
```

### 3.3 Auth Info (in-memory, no file persistence)

```go
type Info struct {
    Token       string
    APIHost     string
    Region      string  // e.g. "prg1" — needed for zcli login --zeropsRegion in SSH deploy
    ClientID    string
    ProjectID   string
    ProjectName string
}
```

**Naming**: Use `auth.Info` (not `auth.Context`) to avoid collision with `context.Context`. Source zaia uses `auth.Credentials` — `auth.Info` is a cleaner fit for ZCP since it includes project metadata beyond just credentials.

**No auto-mapping between API host and region.** They are independent values — API hosts can be any URL (e.g. `gb-devel.zerops.dev`, `api.app-prg1.zerops.io`), no pattern to parse. Both have separate resolution chains (see §9.1). If `Region` is empty at deploy time, the deploy tool skips zcli's `--zeropsRegion` flag.

---

## 4. Platform Client Interface

### 4.1 Interface

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

**Service status**: `ServiceStack.Status` from the zerops-go SDK includes intermediate states that matter for workflow orchestration. Key statuses: `CREATING`, `READY_TO_DEPLOY` (imported but never deployed — critical for bootstrap), `BUILDING`, `RUNNING`, `STOPPED`. During implementation, verify the full enum returned by the SDK. If `READY_TO_DEPLOY` is not available, document the gap.

### 4.3 ZeropsClient Implementation

Port from: `../zaia/internal/platform/zerops.go` (1025 lines)

Key details:
- Uses `zerops-go` SDK (`sdk.Handler`)
- Lazy clientID caching via `getClientID()` — zaia source uses racy string check; ZCP MUST use `sync.Once` for thread safety (improvement over source)
- `mapSDKError()` converts all SDK errors to PlatformError
- `SetAutoscaling` can return nil Process (sync, no async tracking)
- Status normalization: "DONE" → "FINISHED", "CANCELLED" → "CANCELED"

### 4.4 Error Codes

Port from: `../zaia/internal/platform/errors.go`

Skip CLI-specific setup codes (ErrSetupDownloadFailed, ErrSetupInstallFailed, ErrSetupConfigFailed, ErrSetupUnsupportedOS) — irrelevant for MCP server. Also do NOT port `ExitCodeForError()` or `MapHTTPError()` suggestion text referencing "zaia login" — these are CLI-specific.

Static codes (after skipping CLI-specific):
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

Dynamic codes (generated in `mapAPIError()` for subdomain idempotency):
```
SUBDOMAIN_ALREADY_ENABLED, SUBDOMAIN_ALREADY_DISABLED
```

### 4.5 MockClient

Port from: `../zaia/internal/platform/mock.go`

Builder pattern: `NewMock().WithUserInfo(...).WithServices(...).WithError("MethodName", err)`
Thread-safe with `sync.RWMutex`. Compile-time interface check.

---

## 5. MCP Tools Specification

### 5.1 Tools

| Tool | Type | Key Behavior |
|------|------|-------------|
| `zerops_discover` | Sync | Project info + service list. Optional filter by hostname. Optional includeEnvs. |
| `zerops_manage` | Async | action={start,stop,restart,scale} + serviceHostname. Scale accepts CPU/RAM/disk params (see parameter mapping below). |
| `zerops_env` | Mixed | action={get,set,delete}. Service-level or project-level. Variables in KEY=value format. |
| `zerops_logs` | Sync | 2-step: GetProjectLog → LogFetcher. Severity, since, limit, search, buildId filters. |
| `zerops_deploy` | Mixed | SSH mode (primary): SSH into sourceService, zcli push to targetService (hostname). Local mode (fallback): zcli subprocess. See section 8. |
| `zerops_import` | Mixed | Inline content or filePath. dryRun for validation (sync), real import (async). |
| `zerops_validate` | Sync | Offline YAML validation. zerops.yml or import.yml. No API needed. |
| `zerops_knowledge` | Sync | BM25 search. Query expansion. Full top-result content. Suggestions. |
| `zerops_process` | Sync | Status check or cancel. Normalizes DONE→FINISHED, CANCELLED→CANCELED. |
| `zerops_delete` | Async | Requires confirm=true safety gate. Resolves hostname → serviceID. |
| `zerops_subdomain` | Async | enable/disable. Idempotent (already-enabled = success). |
| `zerops_events` | Sync | Merged process + appVersion timeline. Optional service filter. Default limit 50. |
| `zerops_context` | Sync | Static platform knowledge + service type catalog. No params. Load once per session. |
| `zerops_workflow` | Sync | Workflow routing. No param = workflow catalog. With `workflow` param = specific guidance. |

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

### 5.2 zerops_context

| Field | Value |
|-------|-------|
| Type | Sync, read-only, idempotent |
| Parameters | None |
| Returns | Static precompiled content (~800-1200 tokens) |

Content structure (token-optimized). Focus on **concepts and logic**, not complex specifics:

```
## What is Zerops
Developer PaaS. Full Linux containers (Incus, not serverless). Bare-metal performance.
SSH access to runtime containers. Managed services (DB, cache, search, queue, storage).
Auto-scaling (vertical + horizontal). Private VXLAN networking per project.

## How Zerops Works
Project → Services → Containers. Each project = isolated private network.
Services communicate internally via hostname (e.g. http://postgresql:5432).
Runtime services: you deploy code, Zerops builds and runs it.
Managed services: Zerops provisions and manages them (DB, cache, etc.).

## Critical Rules (will break if wrong)
- Internal networking: ALWAYS http://, NEVER https:// (SSL terminates at L7 balancer)
- Ports: 10-65435 only (0-9 and 65436+ reserved)
- HA mode: immutable after creation (cannot change single↔HA)
- mode: NON_HA or HA REQUIRED for databases/caches in import.yml
- Env var cross-ref: ${service_hostname} (underscore, not dash)
- No localhost — services communicate via hostname
- prepareCommands: cached. initCommands: run every start.

## Configuration
zerops.yml = build + deploy + run config (per service, in repo root)
import.yml = infrastructure-as-code (services array, NO project: section)

## Service Types (use zerops://docs/services/{type} for details)
Runtime: nodejs@22|20|18, go@1, python@3.12|3.11, php@8.4|8.3, rust@1,
         java@21|17, dotnet@8|6, elixir@1.17, bun@1, deno@2|1
Container: alpine, ubuntu, docker(VM-based)
DB: postgresql@17|16|14, mariadb@11|10, clickhouse@24
Cache: valkey@7.2(redis-compatible), keydb(deprecated)
Search: meilisearch@1.10, elasticsearch@8, typesense@27|26, qdrant
Queue: nats@2.10, kafka@3.8|3.7
Storage: object-storage(S3/MinIO), shared-storage(POSIX)
Web: nginx@1, static(SPA-ready)

## Defaults (use unless user specifies otherwise)
postgresql@16, valkey@7.2, meilisearch@1.10, nats@2.10, NON_HA, SHARED CPU

For specific questions → zerops_knowledge (BM25 search)
For per-service details → zerops://docs/services/{type} (MCP resources)
```

**Key design principle**: `zerops_context` provides the *conceptual foundation* — how Zerops thinks, what the mental model is, what will break. It does NOT cover complex workflows (that's `zerops_workflow`) or specific per-service deep-dives (that's `zerops_knowledge` / resources).

**Implementation**: Content lives in `ops/context.go` as a compiled string. Not part of BM25 index. Static — updated with code changes, not at runtime.

**Relationship to existing tools**:
- `zerops_context` = conceptual foundation, service catalog, critical rules (call once to understand Zerops)
- `zerops_knowledge` = BM25 deep search for specific questions (call as needed for details)
- `zerops://docs/{path}` = direct resource access for individual doc pages

### 5.3 zerops_workflow

| Field | Value |
|-------|-------|
| Type | Sync, read-only, idempotent |
| Parameters | `workflow` (string, optional) |
| Returns | Workflow catalog (no param) or specific workflow guidance (with param) |

**Without parameter** — returns workflow catalog:
```
Available workflows:
- bootstrap: Create new services from scratch (import, configure, deploy)
- deploy: Push code to services (dev→stage pattern, zerops.yml)
- debug: Investigate issues (logs, events, SSH, connectivity)
- scale: Adjust service resources (CPU, RAM, disk, containers)
- configure: Manage environment variables and service settings
- monitor: Check service status, logs, activity timeline
```

**With `workflow` parameter** — returns specific workflow guidance:
- Step-by-step workflow with tool calls
- Prerequisites and validation steps
- Common patterns and pitfalls
**Implementation**:
- Workflow catalog: static string in `ops/workflow.go`
- Per-workflow content: embedded markdown files or static strings
- Not part of BM25 index
- Shares content source with `internal/content/` package (shared with `internal/init/templates.go`)
- Workflow hooks/triggers: v2+ (no interface in v1 — define when use cases are clear)

### 5.4 Tool → MCP Error Conversion

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

**Relationship between knowledge tools**: `zerops_context` provides a token-optimized static overview of the platform and service catalog — designed for one-shot loading into context. `zerops_knowledge` provides BM25 search for detailed per-topic documentation. `zerops://docs/{path}` resources provide direct access to individual documents. The LLM calls `zerops_context` once to orient itself, then uses `zerops_knowledge` or direct resources for specific deep-dives.

---

## 8. Deploy Tool

### 8.1 Architecture

In the target deployment, code lives ON runtime service containers (via mounted filesystems). The ZCP service has SSH access to all sibling runtime services on the VXLAN private network. Deploy means: SSH into the source service, run `zcli push` from there to the target service.

For local development (ZCP running on developer's machine), a local `zcli push` fallback is available.

### 8.2 SSH Mode (Primary — Production)

**Parameters:**
```
zerops_deploy (SSH mode):
  sourceService: string     # hostname of source container (SSH target, e.g. "appdev")
  targetService: string     # hostname of target service to push to (e.g. "appstage")
  setup: string             # zerops.yml setup name ("dev" or "prod"), optional
  workingDir: string        # path inside container (default: /var/www)
```

**Execution flow:**
```
1. Validate sourceService exists (resolve via discover)
2. Resolve targetService hostname → service ID (via discover, same as other tools)
3. SSH into sourceService
4. Authenticate zcli: always run `zcli login $ZCP_API_KEY [--zeropsRegion $region]`
   ($region = auth.Info.Region from §3.2. If empty, omit --zeropsRegion flag.)
   Always re-login — idempotent, avoids stale auth detection complexity.
5. Run: zcli push $resolvedServiceId [--setup=$setup] [--workingDir=$workingDir]
   ($resolvedServiceId = targetService hostname resolved to ID in step 2)
6. zcli push uploads code + triggers build pipeline, then returns
7. Return: {status, sourceService, targetService, message}
8. Agent can poll build completion via zerops_events (service filter)
```

**SSH execution**: Uses `exec.CommandContext` to run `ssh $sourceService "cd $workingDir && zcli login ... && zcli push ..."`. The ZCP service has SSH key access to all sibling services via Zerops VXLAN networking — no password or key configuration needed.

**Auth sharing**: ZCP passes its own `ZCP_API_KEY` to zcli inside the SSH session. Always authenticates fresh — no detection of pre-existing auth state.

**Build monitoring**: After `zcli push` returns, the build/deploy pipeline runs asynchronously on Zerops. The agent uses `zerops_events` (with service filter) or `zerops_process` to track completion. The existing progress notification system (section 6) can be adapted — although `zcli push` does not return a process ID, the events API shows build status per service.

### 8.3 Local Mode (Fallback — Development)

**Parameters:**
```
zerops_deploy (local mode):
  workingDir: string        # local directory with zerops.yml
  targetService: string     # hostname of target service (resolved to ID internally)
```

Shell out to local `zcli push` binary. This is the development-time fallback when ZCP runs on the developer's machine (not inside Zerops).

**Auth detection**: Before invoking local `zcli push`, check if `cli.data` file exists and contains a non-empty `Token` field. If file is missing or token is empty → return error: "Deploy requires zcli login. Run 'zcli login' first."

**zcli subprocess pattern** (port from `../zaia-mcp/internal/executor/executor.go`):
- `exec.CommandContext` with resolved PATH (handles nvm/homebrew paths)
- Non-zero exit is NOT a Go error (zcli outputs JSON on stdout)
- Context cancellation checked first
- WHEN zcli not found → clear error with install instructions

### 8.4 Mode Detection

The deploy tool auto-detects which mode to use:

```
IF sourceService parameter is provided → SSH mode (targetService required)
ELSE IF workingDir + targetService provided → Local mode
ELSE → error: "Provide sourceService (SSH deploy) or workingDir + targetService (local deploy)"
```

**Note**: `targetService` is shared between both modes (always hostname, resolved to ID internally). Mode is determined solely by presence of `sourceService`.

---

## 9. Configuration

### 9.1 Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `ZCP_API_KEY` | Yes* | — | Zerops PAT. Set as service env var in Zerops. *Not required if zcli fallback is available (local dev). |
| `ZCP_API_HOST` | No | `api.app-prg1.zerops.io` | API endpoint URL. Any valid host — e.g. `api.app-prg1.zerops.io`, `gb-devel.zerops.dev`. No pattern assumed. |
| `ZCP_REGION` | No | `prg1` | Region label passed to `zcli login --zeropsRegion` in SSH deploy. Independent of `ZCP_API_HOST` — no auto-mapping between them. |
| `ZCP_LOG_LEVEL` | No | `warn` | Stderr log level (debug, info, warn, error) |

**Resolution priority (API host and region are independent):**

API host:
1. `ZCP_API_HOST` env var
2. zcli fallback → `RegionData.address` from `cli.data`
3. Default → `api.app-prg1.zerops.io`

Region (only used for SSH deploy → `zcli login --zeropsRegion`):
1. `ZCP_REGION` env var
2. zcli fallback → `RegionData.name` from `cli.data`
3. Default → `prg1`

### 9.2 Deployment Inside Zerops

The ZCP service uses a dedicated Zerops service type `zcp@1` which comes pre-installed with code-server (VS Code), Claude Code, and tools (jq, yq, psql, redis-cli, SSH). The user imports this service into their project; Zerops handles the rest.

**Service import:**
```yaml
services:
  - hostname: zcp
    type: zcp@1
    enableSubdomainAccess: true
    verticalAutoscaling:
      minRam: 2
    envSecrets:
      ZCP_API_KEY: <project-scoped-PAT>
```

**ZCP binary delivery**: The `zerops.yml` build step compiles zcli from source and downloads the ZCP binary. The `initCommands` run `zcp init` which bootstraps the entire environment in a single idempotent step.

**initCommands integration:**
```yaml
initCommands:
  - zcp init  # Bootstrap: CLAUDE.md, MCP config, hooks, SSH
  # code-server starts with everything pre-configured
```

`zcp init` replaces the previous scattered initCommands (downloading CLAUDE.md, configuring SSH, setting up MCP config separately). Single command, idempotent, all templates compiled into the binary.

**MCP server config** (generated by `zcp init` into Claude Code settings):
```json
{
  "mcpServers": {
    "zerops": {
      "command": "/usr/local/bin/zcp"
    }
  }
}
```

No token in the MCP config — ZCP reads `ZCP_API_KEY` from the service environment. zcli is also pre-authenticated with the same token via `zcli login` in initCommands.

**User flow**: Import the ZCP service → open subdomain URL → VS Code loads in browser → Claude Code terminal is ready with MCP server connected → user starts working.

**SSH pre-configuration**: `zcp init` sets up `~/.ssh/config` with `StrictHostKeyChecking no` for all hosts on the VXLAN network. This enables passwordless SSH to all sibling runtime services (Zerops provides key-based access within the project).

**CLAUDE.md (generated by `zcp init`, user-replaceable):**

Purpose: Workflow guidance layer. Modular — users can swap with their own CLAUDE.md.

Default content recommends:
- Before infrastructure work, call `zerops_workflow` to get recommended approach
- LLM selects appropriate workflow from catalog
- Calls `zerops_workflow(workflow_type)` for specific step-by-step guidance
- Lists tool categories for orientation (not full descriptions — those come from MCP tool schemas)

Key design decision: Workflow guidance lives in CLAUDE.md (file), NOT in MCP instructions (code). This means:
- Users can customize workflows without rebuilding the binary
- Different projects can have different workflow guidance
- The MCP server stays workflow-agnostic — it provides tools and knowledge, not opinions

### 9.3 Local Development (optional)

For developing and testing ZCP outside Zerops:

```json
{
  "mcpServers": {
    "zerops": {
      "command": "/path/to/zcp",
      "env": {
        "ZCP_API_KEY": "<your-PAT>"
      }
    }
  }
}
```

Or without `ZCP_API_KEY`, relying on zcli fallback (requires prior `zcli login`).

### 9.4 `zcp init` Subcommand

The ZCP binary has two modes:
- `zcp` (no args) → MCP server over STDIO
- `zcp init` → Bootstrap: generates configuration files, sets up environment

This is NOT a CLI framework — no Cobra, just a simple `os.Args[1]` check in main.go.

**`zcp init` actions (v1):**

| Action | What it does | Output file/location |
|--------|-------------|---------------------|
| Generate CLAUDE.md | Default workflow guidance (thin: points to `zerops_workflow` tool) | `./CLAUDE.md` (working dir) |
| Configure MCP connection | Write Claude Code MCP server config | `~/.claude/settings.json` or equivalent |
| Set up hooks | Configure Claude Code hooks for ZCP | `~/.claude/hooks/` or settings |
| Configure SSH | `~/.ssh/config` with `StrictHostKeyChecking no` for VXLAN | `~/.ssh/config` |

**Key properties:**
- **Idempotent**: Running again overwrites with defaults. User customizations to CLAUDE.md get reset — this is intentional (binary is source of truth for defaults).
- **Templates compiled into binary**: CLAUDE.md template, MCP config, SSH config all embedded in the binary via `go:embed`. No external downloads needed for init.
- **Shared content source**: The CLAUDE.md template and `zerops_workflow` tool content both live in `internal/content/` — single source of truth. Both `init/` and `ops/workflow.go` import from `content/`. CLAUDE.md is the thin directive ("call zerops_workflow"), workflow content lives in the tool.
- **Future-extensible**: More init tasks can be added without changing the interface.

**Package structure:**
```
internal/
  content/                   → Shared embedded content (go:embed)
    content.go               → Embedded workflow + template files
    workflows/               → Workflow guidance markdown
    templates/               → CLAUDE.md, MCP config, SSH config templates
  init/
    init.go                  → Init orchestrator: runs all setup steps
    templates.go             → Uses content/ package for templates. Generates output files.
```

**cmd/zcp/main.go dispatch:**
```go
func main() {
    if len(os.Args) > 1 && os.Args[1] == "init" {
        // Run bootstrap
        init.Run()
        return
    }
    // Normal MCP server mode
    ...
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

### 11.1 Core Problem & Philosophy

Mock-based tests create a false sense of correctness. The most common failure mode: everything passes with mocks, then real API testing reveals SDK type mapping errors, wrong field names, nil-vs-empty mismatches, unexpected status values, and broken error handling. By the time this is discovered, multiple layers of code are built on wrong assumptions.

**Solution: Progressive API Verification.** Real API tests are written *alongside* mock tests in every phase — not as an afterthought. Each implementation unit is not DONE until both mock tests AND API tests pass. Mock tests verify logic; API tests verify reality.

### 11.2 Test Layers

| Layer | Packages | Mock | Build Tag | t.Parallel |
|-------|----------|------|-----------|------------|
| Platform unit | `platform/` | None (types, error mapping) | — | Yes |
| **Platform contract** | `platform/` | **None — real Zerops API** | `api` | Yes (read), No (mutate) |
| Auth unit | `auth/` | `platform.Mock` | — | Yes |
| **Auth contract** | `auth/` | **Real API** | `api` | Yes |
| Knowledge | `knowledge/` | None (in-memory) | — | Yes |
| Ops unit | `ops/` | `platform.Mock` + `MockLogFetcher` | — | Yes |
| **Ops API** | `ops/` | **Real `ZeropsClient`** | `api` | Yes (read), No (mutate) |
| Tools unit | `tools/` | In-memory MCP + ops with Mock | — | Yes |
| **Tools API** | `tools/` | **In-memory MCP + real `ZeropsClient`** | `api` | Yes (read), No (mutate) |
| Integration | `integration/` | In-memory MCP + Mock | — | Yes |
| **E2E lifecycle** | `e2e/` | **Real API, full lifecycle** | `e2e` | No |

**Build tags:**
- **`-tags api`** — Individual operations against real Zerops API. Can run per-method, per-phase. Read-only tests run in parallel. Mutating tests are sequential within their group.
- **`-tags e2e`** — Full sequential lifecycle test (import→discover→manage→env→deploy→delete). Creates and destroys real resources. Runs at end of Phase 4.

**Run commands:**
```bash
go test ./... -count=1 -short                    # Mock tests only (fast, no API)
go test ./... -count=1 -tags api                 # Mock + API contract tests
go test ./... -count=1 -tags e2e                 # Everything including full lifecycle
go test ./internal/platform/ -tags api -v        # Platform contract tests only
go test ./internal/ops/ -tags api -run TestAPI   # Ops API tests only
```

### 11.3 TDD Flow with API Verification

The standard TDD cycle is extended with an API verification step:

```
RED:    Write mock test (failing) + API test (failing, -tags api)
GREEN:  Implement to pass mock test
VERIFY: Run API test (-tags api) → confirms real API matches assumptions
        IF API test fails → fix implementation (not the mock!)
        Common API mismatches: nil vs empty slice, *string vs string,
        status values, field names in SDK, error response shapes
DONE:   Both mock test AND API test pass
```

**Critical rule**: When an API test fails but the mock test passes, the implementation is wrong — the mock was built on incorrect assumptions. Fix the implementation AND update the mock to match reality. Never "fix" an API test by weakening its assertions.

### 11.4 API Test Harness

Shared test infrastructure for all `-tags api` and `-tags e2e` tests:

```
internal/platform/apitest/          → API test harness (shared across packages)
  harness.go                        → APIHarness: real client setup, skip logic
  cleanup.go                        → Resource cleanup helpers (delete services, etc.)
```

**Harness pattern:**
```go
//go:build api

package platform_test

func TestAPI_GetUserInfo(t *testing.T) {
    h := apitest.New(t)  // Skips if ZCP_API_KEY not set
    client := h.Client() // Real ZeropsClient

    info, err := client.GetUserInfo(h.Ctx())
    require.NoError(t, err)
    assert.NotEmpty(t, info.ID)
    // Verify actual response shape matches domain types
}
```

**Harness behavior:**
- Reads `ZCP_API_KEY` from env (same as production auth path)
- `ZCP_API_HOST` override for non-default regions
- **Skips** (not fails) when `ZCP_API_KEY` is not set — developers without API access can still run mock tests
- Provides `h.Client()` (real `ZeropsClient`), `h.Ctx()` (with timeout), `h.ProjectID()`
- Provides `h.Cleanup(func())` for deferred resource deletion
- Logs all API calls for debugging failed tests

**Test file convention:**
- `zerops_test.go` — unit tests (no build tag, runs always)
- `zerops_api_test.go` — API contract tests (`//go:build api`, runs with `-tags api`)
- `discover_test.go` / `discover_api_test.go` — same pattern for ops, tools

### 11.5 Platform Contract Tests (Phase 1 gate)

Every `ZeropsClient` method gets a contract test against real API. These are the most valuable tests in the entire suite — they catch SDK mapping errors at the source before any downstream code is affected.

**What to verify per method:**
- Response is non-nil when expected
- All domain type fields are populated correctly (not zero-value when API returns data)
- Pointer fields (`*string`, `*int`) are nil/non-nil as expected
- Slice fields are non-nil (empty slice, not nil) when API returns empty collections
- Status/enum values match expected normalized values
- Error responses produce correct `PlatformError` codes

**Example — verify `ListServices` response shape:**
```go
//go:build api

func TestAPI_ListServices_ResponseShape(t *testing.T) {
    h := apitest.New(t)
    services, err := h.Client().ListServices(h.Ctx(), h.ProjectID())
    require.NoError(t, err)
    require.NotEmpty(t, services, "test project must have at least one service")

    svc := services[0]
    assert.NotEmpty(t, svc.ID, "service ID must be populated")
    assert.NotEmpty(t, svc.Name, "service hostname must be populated")
    assert.NotEmpty(t, svc.Status, "service status must be populated")
    assert.NotNil(t, svc.Ports, "ports must be non-nil (may be empty slice)")
    // Verify type info is populated for typed services
    if svc.ServiceType != nil {
        assert.NotEmpty(t, svc.ServiceType.Name)
        assert.NotEmpty(t, svc.ServiceType.Version)
    }
}
```

**Phase 1 gate**: ALL `Client` interface methods must have passing contract tests before proceeding to Phase 2. This ensures every downstream layer (ops, tools) builds on verified type mappings.

### 11.6 Ops & Tools API Tests (Phase 2-3 gates)

**Ops API tests** — Use real `ZeropsClient` instead of mock. Verify business logic works with actual API response shapes:
```go
//go:build api

func TestAPI_Discover_WithRealServices(t *testing.T) {
    h := apitest.New(t)
    client := h.Client()
    result, err := ops.Discover(h.Ctx(), client, h.ProjectID(), "")
    require.NoError(t, err)
    assert.NotEmpty(t, result.Services)
    // Verify ops-level transformations produce correct output
}
```

**Tools API tests** — Full MCP→tool→ops→platform→API chain using in-memory MCP transport but real API backend:
```go
//go:build api

func TestAPI_DiscoverTool_RealBackend(t *testing.T) {
    h := apitest.New(t)
    // In-memory MCP with real ZeropsClient (not mock)
    srv := server.NewWithClient(h.Client())
    session := h.MCPSession(srv)

    result, err := session.CallTool(h.Ctx(), &mcp.CallToolParams{
        Name: "zerops_discover",
    })
    require.NoError(t, err)
    assert.False(t, result.IsError)
    // Verify complete chain produces valid MCP response
}
```

**Read-only vs mutating tests:**
- Read-only API tests (`discover`, `logs`, `events`, `env get`, `knowledge`) run in parallel — safe, idempotent.
- Mutating API tests (`manage`, `env set`, `import`, `delete`, `subdomain`) run sequentially, use unique resource names, and have cleanup functions. Group mutating tests in dedicated test functions with `t.Cleanup()`.

### 11.7 Full Lifecycle E2E (Phase 4 gate)

Sequential multi-step test exercising the complete operational lifecycle. Modeled on zaia-mcp's 17-step pattern but adapted for ZCP's direct API access:

```
Step  Operation                  Validates
──────────────────────────────────────────────────────────────
01    zerops_context             Static content loads, token-sized
02    zerops_discover            Auth works, project+services visible
03    zerops_knowledge           BM25 search returns results
04    zerops_validate            YAML validation (offline)
05    zerops_import (dry-run)    Validates YAML, no side effects
06    zerops_import (real)       Creates 2 services (runtime+managed)
07    poll processes             Wait for import completion (120s max)
08    zerops_discover            Verify both services exist
09    zerops_env (set)           Set env var on runtime service
10    zerops_env (get)           Read back, verify value
11    zerops_manage (stop)       Stop managed service
12    zerops_manage (start)      Start managed service
13    zerops_subdomain (enable)  Enable subdomain (may fail if undeployed — expected)
14    zerops_subdomain (disable) Disable subdomain
15    zerops_logs                Fetch logs (may be empty — OK)
16    zerops_events              Activity timeline shows operations
17    zerops_workflow            Catalog returns, specific workflow returns
18    zerops_delete (both)       Delete test services + wait
19    zerops_discover            Verify services gone
```

**E2E conventions:**
- Build tag: `//go:build e2e`
- Service names: `zcp-test-{random}` suffix to avoid conflicts
- `t.Cleanup()` always deletes created resources (even on failure)
- Cleanup bypasses MCP — uses direct `ZeropsClient` with fresh context (test context may be cancelled)
- Expected failures are non-fatal (e.g., restart/subdomain on undeployed service)
- Process polling: 40 attempts × 3s = 120s max per operation

### 11.8 Tool Test Pattern (in-memory MCP, no subprocess)

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

### 11.9 Deploy Tool Testing

SSH mode requires special test considerations:
- **Unit tests**: Mock the SSH execution layer. Test parameter validation, mode detection, auth injection.
- **Integration tests**: Use mock SSH server or stub the exec layer. Verify the complete flow without actual SSH.
- **API tests** (`-tags api`): Real SSH to a dev container in a test Zerops project. Verify zcli push command construction and auth injection.
- **E2E tests** (`-tags e2e`): Full deploy cycle — SSH into source, push to target, verify via events.

### 11.10 TDD per framework-plan.md

Every implementation unit: RED (failing test) → GREEN (minimal impl) → VERIFY (API test) → REFACTOR.
Test files reference design docs: `// Tests for: design/discover.md § Behavioral Contract`
Table-driven tests, `Test{Op}_{Scenario}_{Result}` naming. API tests: `TestAPI_{Op}_{Scenario}` naming.

---

## 12. Non-Functional Requirements

- **Timeouts**: API 30s, log fetch 30s, progress polling 10min, deploy SSH 5min, deploy local 5min
- **Graceful shutdown**: SIGINT/SIGTERM → cancel context (aborts in-flight ops including PollProcess and SSH sessions) → close STDIO transport → exit. Shutdown timeout: 5s hard deadline after signal. No drain phase needed — STDIO is not a listener, in-flight HTTP requests abort via context cancellation.
- **Context propagation**: All layers pass ctx. Timeout is via `http.Client.Timeout` (30s), not per-call `context.WithTimeout`. Context cancellation from MCP transport works (client closes → ctx cancelled → in-flight HTTP aborts).
- **Thread safety**: `ZeropsClient.clientID` via sync.Once. Knowledge Store via sync.Once. Tool handlers concurrent-safe.
- **No retry policy in v1**: Neither zerops-go SDK nor zaia implement retries. Transient API failures (5xx, network blips) are not retried. Known limitation — consider adding retry with backoff in v2.
- **Binary**: <30MB. `-ldflags "-s -w"`. Target platform: Linux amd64 (Zerops containers). Cross-compile for darwin during development.
- **Module**: `github.com/zeropsio/zcp`, Go 1.24.0

---

## 13. Implementation Sequencing

**Per-unit TDD cycle**: RED (mock test + API test) → GREEN (pass mock test) → VERIFY (pass API test) → REFACTOR.
**Phase gates**: Each phase has an explicit API verification gate. Do NOT proceed to the next phase until the gate passes. This catches SDK mapping errors, response shape mismatches, and wrong assumptions early — before downstream code builds on them.

### Phase 1: Foundation

1. `platform/types.go` — Domain types
2. `platform/errors.go` — Error codes, PlatformError, mapping
3. `platform/client.go` — Client interface + LogFetcher
4. `platform/mock.go` — Mock + MockLogFetcher
5. `platform/apitest/harness.go` — **API test harness** (shared: real client setup, skip logic, cleanup)
6. `platform/zerops.go` — ZeropsClient (zerops-go)
   - For EACH method: write `zerops_test.go` (unit) + `zerops_api_test.go` (contract, `-tags api`)
   - Contract test verifies: response shape, field population, nil/empty handling, status values
7. `platform/logfetcher.go` — ZeropsLogFetcher + API contract test
8. `auth/auth.go` — Token resolution, validation, project discovery + API contract test
9. `knowledge/` — Store, documents, query, embed directory

**Phase 1 gate**: `go test ./internal/platform/... ./internal/auth/... -tags api -v` — ALL Client methods + LogFetcher + auth flow verified against real API. Every domain type field mapping confirmed. This is the single most important test gate — it validates the foundation everything else builds on.

### Phase 2: Business Logic

10. `ops/helpers.go` — Service resolution, time parsing
11. `ops/discover.go` — Discover + `discover_api_test.go` (real client, verify ops-level output)
12. `ops/manage.go` — Start/stop/restart/scale + API tests (read-only: verify param mapping; mutating: sequential)
13. `ops/env.go` — Env get/set/delete + API tests
14. `ops/logs.go` — 2-step log fetch + API test (verify real log response shape)
15. `ops/import.go` — Import with dry-run + API test (dry-run only — safe, no resources created)
16. `ops/validate.go` — YAML validation (offline, no API test needed)
17. `ops/delete.go` — Service deletion (API test grouped with import — create then delete)
18. `ops/subdomain.go` — Subdomain (idempotent) + API test
19. `ops/events.go` — Activity timeline merge + API test (verify real event shapes)
20. `ops/process.go` — Process status/cancel + API test
21. `ops/context.go` — Static platform knowledge content
22. `content/` — Shared embedded content (workflow markdown, CLAUDE.md template)
23. `ops/workflow.go` — Workflow catalog + per-workflow content (reads from `content/`)

**Phase 2 gate**: `go test ./internal/ops/... -tags api -v` — ALL ops functions verified with real API responses. Business logic (filtering, merging, formatting) produces correct results from real data, not just mock data.

### Phase 3: MCP Layer

24. `tools/` — Tool handlers + convert.go
    - For each tool: unit test (mock) + `_api_test.go` (in-memory MCP + real `ZeropsClient`)
    - API tests verify: MCP request → tool → ops → platform → API → MCP response (full chain)
25. `server/server.go` — MCP server + tool registration
26. `server/instructions.go` — Ultra-minimal init message (~40-50 tokens, relevance signal only)
27. `cmd/zcp/main.go` — Entrypoint (MCP server mode + init dispatch)

**Phase 3 gate**: `go test ./internal/tools/... -tags api -v` — All tools tested end-to-end with real API. MCP response format verified with real data. Error paths verified with real API errors (e.g., invalid service hostname → correct MCP error response).

### Phase 4: Streaming + Deploy + E2E

28. `ops/progress.go` — Reusable PollProcess helper with callback (MCP-agnostic)
    - API test: trigger real async op, poll to completion, verify status transitions
29. `ops/deploy.go` — Deploy logic (SSH mode + local fallback)
30. `tools/deploy.go` — Deploy tool handler
31. Integration tests (in-memory MCP + mock, multi-tool flows)
32. **Full lifecycle E2E** (`-tags e2e`) — sequential test (see section 11.7)

**Phase 4 gate**: `go test ./e2e/ -tags e2e -v` — Full lifecycle passes: import→discover→manage→env→subdomain→logs→events→delete. Real resources created and cleaned up. This is the final confidence gate.

### Phase 5: Init Subcommand

33. `internal/init/init.go` — Init orchestrator (CLAUDE.md, MCP config, hooks, SSH)
34. `internal/init/templates.go` — Uses `internal/content/` for shared templates. Generates output files.
35. Update `cmd/zcp/main.go` — Add init mode dispatch

### Test Prerequisites

For API and E2E tests to run, the test environment needs:
- `ZCP_API_KEY` env var with a valid project-scoped PAT
- The target project must have at least one existing service (for read-only tests)
- Network access to Zerops API (`api.app-prg1.zerops.io` or `ZCP_API_HOST` override)
- Tests **skip** (not fail) when `ZCP_API_KEY` is not set — developers without API access can still run mock tests via `go test ./... -short`

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

## 15. Future Tools (v2+)

These capabilities are currently handled by the agent's native bash layer. They are candidates for promotion to structured MCP tools when usage patterns are clear:

| Tool | Purpose | Current Alternative |
|------|---------|-------------------|
| `zerops_exec` | Execute commands on service containers via SSH | Agent runs `ssh hostname "command"` |
| `zerops_mount` | Mount/unmount service filesystems | Agent runs mount/SSHFS commands |
| `zerops_verify` | Structured endpoint verification with evidence | Agent runs `curl` + manual checks |
| `zerops_recipes` | Live recipe/template search from Zerops API | Knowledge base BM25 (static docs) |

**Why not in v1**: The agent's bash layer handles these well enough. Promoting to MCP tools adds value when we need structured input/output, error handling, or cross-tool orchestration that bash doesn't provide cleanly. Evaluate after v1 based on actual usage patterns.

**Architecture readiness**: The package structure (`ops/` + `tools/`) accommodates new tools without restructuring. Each new tool = one ops file + one tool handler + registration in server.go.

---

## 16. Verification

### 16.1 Build & Lint

1. `go build -o bin/zcp ./cmd/zcp` — Binary builds
2. `go test ./... -count=1 -short` — All mock tests pass
3. `golangci-lint run ./...` — No lint errors

### 16.2 API Contract Tests (requires ZCP_API_KEY)

4. `go test ./internal/platform/... -tags api -v` — All Client method contracts verified against real API
5. `go test ./internal/auth/... -tags api -v` — Auth flow verified (token validation, project discovery)
6. `go test ./internal/ops/... -tags api -v` — All ops functions produce correct results from real API data
7. `go test ./internal/tools/... -tags api -v` — Full MCP→tool→ops→platform→API chain verified for all tools

### 16.3 E2E Lifecycle (requires ZCP_API_KEY + creates real resources)

8. `go test ./e2e/ -tags e2e -v` — Full lifecycle passes (import→discover→manage→env→subdomain→logs→events→delete)

### 16.4 Manual Verification

9. `ZCP_API_KEY=<token> ./bin/zcp` — Server starts, responds to MCP initialize
10. MCP init message: ~40-50 tokens, pure relevance signal, mentions `zerops_context` only
11. MCP tool call: `zerops_context` returns platform knowledge + service catalog (~800-1200 tokens)
12. MCP tool call: `zerops_workflow` returns workflow catalog; `zerops_workflow {workflow: "bootstrap"}` returns specific guidance
13. MCP tool call: `zerops_discover` returns project + services
14. MCP tool call: `zerops_knowledge {query: "postgresql"}` returns docs
15. MCP tool call: `zerops_manage {action: "restart", serviceHostname: "api"}` returns process
16. Progress notification test: async op with ProgressToken sends updates
17. Error handling: invalid token → proper MCP error response
18. Graceful shutdown: SIGINT during operation → clean exit
19. Deploy SSH mode: `zerops_deploy {sourceService: "appdev", targetService: "appstage"}` triggers push
20. `zcp init` — generates CLAUDE.md, MCP config, hooks, SSH config. Idempotent on re-run.
21. Tool count: all tools from §5.1 registered in MCP server (verify via `tools/list`)
