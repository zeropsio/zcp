# Plan: Make zerops_env and zerops_scale Streamable (Poll to Completion)

## Status: READY FOR IMPLEMENTATION

---

## Background

Currently `zerops_import`, `zerops_deploy`, and `zerops_manage` all poll their processes to completion before returning results. The LLM gets a final FINISHED/FAILED status.

However `zerops_env` and `zerops_scale` return the raw process object immediately (with status=PENDING), forcing the LLM to manually call `zerops_process` to poll. This is inconsistent and error-prone.

## E2E Findings (Real API, Feb 23 2026)

### Env Operations (`stack.updateUserData` / `stack.updateProjectEnvs`)
- **SetServiceEnvFile** returns `Process{ID, ActionName="stack.updateUserData", Status="PENDING"}`
- **DeleteUserData** returns `Process{ID, ActionName="stack.updateUserData", Status="PENDING"}`
- **CreateProjectEnv** returns `Process{ID, ActionName="stack.updateProjectEnvs", Status="PENDING"}`
- **DeleteProjectEnv** returns `Process{ID, Status="PENDING"}`
- Service-level: finish in **2-5 seconds** (set ~2s, delete ~4.5s)
- Project-level: finish in **~2 seconds**
- Status progression: PENDING -> RUNNING -> FINISHED (3-step for env, vs 2-step for scale)
- Env vars are immediately visible via GetServiceEnv after process reaches FINISHED

### Scale Operations (`stack.updateAutoscaling`)
- **SetAutoscaling** returns `Process{ID, ActionName="stack.updateAutoscaling", Status="PENDING"}`
- Finishes in **~2 seconds** (1 poll at 2s interval -> FINISHED)
- Process always goes: PENDING -> FINISHED
- Identical behavior even when restoring original values
- No-op case (same values): still returns a process, still finishes quickly

### Key Insight
Both operations are fast (~2s) but still async — the API returns PENDING and a process ID. Without polling, the LLM sees "PENDING" and doesn't know if it succeeded. Polling makes the tool self-contained.

## Documentation Findings

### Env Vars (from zerops-docs)
- Secret env vars "can be updated without redeployment (though **services need to be reloaded**)"
- The process running in the container receives env vars **only when it starts**
- After env var update, user needs to **restart the runtime service**
- This is important context for the tool description

### Scaling
- Vertical scaling (CPU/RAM/disk) is automatic and fast
- Docs don't mention process tracking for scaling
- Scaling parameters are "eventually applied" but the API process completes quickly (~2s)

### SDK Discovery: `PutServiceStackReload` exists!
The zerops-go SDK has `PutServiceStackReload` (`PUT /service-stack/{id}/reload`) which returns a Process.
This is distinct from `PutServiceStackRestart` — tested on real API:

| Operation | Action Name | Time to FINISHED | Polls (2s) |
|-----------|------------|-----------------|------------|
| **Reload** | `stack.reload` | **~4s** | 2 |
| **Restart** | `stack.restart` | **~14s** | 7 |

Reload is **3.5x faster** than restart. It reloads the service environment without full container restart.
This is exactly what's needed after env var changes.

**Recommendation**: Add `reload` action to `zerops_manage` and use it in `zerops_env` description instead of restart.

---

## Implementation Plan

### 1. Changes to `internal/tools/env.go`

Add process polling after both `set` and `delete` operations, following the `manage.go` pattern.

```go
// RegisterEnv — add req *mcp.CallToolRequest to the callback signature
func RegisterEnv(srv *mcp.Server, client platform.Client, projectID string) {
    mcp.AddTool(srv, &mcp.Tool{
        // Updated description (see below)
    }, func(ctx context.Context, req *mcp.CallToolRequest, input EnvInput) (*mcp.CallToolResult, any, error) {
        onProgress := buildProgressCallback(ctx, req)

        switch input.Action {
        case "set":
            result, err := ops.EnvSet(ctx, client, projectID, ...)
            if err != nil { return convertError(err), nil, nil }
            pollEnvProcess(ctx, client, result, onProgress)
            return jsonResult(result), nil, nil
        case "delete":
            result, err := ops.EnvDelete(ctx, client, projectID, ...)
            if err != nil { return convertError(err), nil, nil }
            pollEnvProcess(ctx, client, result, onProgress)
            return jsonResult(result), nil, nil
        }
    })
}
```

New helper function:
```go
func pollEnvProcess(ctx context.Context, client platform.Client, result interface{ GetProcess() *platform.Process }, onProgress ops.ProgressCallback) {
    // Extract process from EnvSetResult or EnvDeleteResult
    // Use pollManageProcess pattern (already exists in manage.go)
}
```

**Actually simpler**: Both `EnvSetResult` and `EnvDeleteResult` have a `Process *platform.Process` field. We can reuse the existing `pollManageProcess` function directly:

```go
case "set":
    result, err := ops.EnvSet(...)
    if err != nil { return convertError(err), nil, nil }
    if result.Process != nil {
        result.Process = pollManageProcess(ctx, client, result.Process, onProgress)
    }
    return jsonResult(result), nil, nil
```

### 2. Changes to `internal/tools/scale.go`

Same pattern — add polling after Scale operation.

```go
func RegisterScale(srv *mcp.Server, client platform.Client, projectID string) {
    mcp.AddTool(srv, &mcp.Tool{
        // Updated description (see below)
    }, func(ctx context.Context, req *mcp.CallToolRequest, input ScaleInput) (*mcp.CallToolResult, any, error) {
        // ... validation ...
        result, err := ops.Scale(ctx, client, projectID, input.ServiceHostname, ...)
        if err != nil { return convertError(err), nil, nil }

        onProgress := buildProgressCallback(ctx, req)
        if result.Process != nil {
            result.Process = pollManageProcess(ctx, client, result.Process, onProgress)
        }
        return jsonResult(result), nil, nil
    })
}
```

### 3. Add `reload` action to `zerops_manage` + platform layer

**New platform method** (`internal/platform/client.go` + `zerops_ops.go`):
```go
ReloadService(ctx context.Context, serviceID string) (*Process, error)
```
Wraps `PutServiceStackReload` (already in SDK).

**New ops function** (`internal/ops/manage.go`):
```go
func Reload(ctx context.Context, client platform.Client, projectID, hostname string) (*Process, error)
```

**Updated `zerops_manage`** (`internal/tools/manage.go`):
Add `case "reload"` alongside start/stop/restart.

### 4. Updated Tool Descriptions

**zerops_env** (new):
> "Manage environment variables. Actions: set, delete. Scope: service (serviceHostname) or project (project=true). Blocks until the process completes — returns final status (FINISHED/FAILED). After changing env vars, you MUST reload the service (zerops_manage action=reload) for the running application to pick up the new values. Reload is fast (~4s) vs restart (~14s). To read env vars, use zerops_discover with includeEnvs=true."

**zerops_scale** (new):
> "Scale a service: adjust CPU, RAM, disk, and container autoscaling parameters. Blocks until the scaling process completes — returns final status (FINISHED/FAILED)."

**zerops_manage** (new description, add reload):
> "Manage service lifecycle: start, stop, restart, or reload a service. Use reload after env var changes — it's faster (~4s) than restart (~14s) and sufficient for picking up new environment variables."

**zerops_process** (new):
> "Check status or cancel an async process. All mutating tools (import, deploy, manage, env, scale) now poll automatically. Use this only to cancel a running process or check a historical process. Default action is 'status'."

### 5. Move `pollManageProcess` to shared location

Currently `pollManageProcess` lives in `manage.go`. Since env.go and scale.go will also use it, either:
- **Option A**: Keep it in `manage.go` and just reference it from other files (it's in the same package, so it's already accessible) ← **PREFERRED**
- **Option B**: Move to `convert.go` or create `poll.go`

Since all files are in `package tools`, Option A works with no changes. The function is already accessible.

### 6. Files to Change

| File | Change |
|------|--------|
| `internal/platform/client.go` | Add `ReloadService` to interface |
| `internal/platform/zerops_ops.go` | Add `ReloadService` implementation |
| `internal/platform/mock_methods.go` | Add `ReloadService` mock |
| `internal/ops/manage.go` | Add `Reload` function |
| `internal/tools/manage.go` | Add `reload` case + update description |
| `internal/tools/env.go` | Add `req` parameter, add polling after set/delete, update description |
| `internal/tools/scale.go` | Add `req` parameter, add polling after scale, update description |
| `internal/tools/process.go` | Update description text |

### 7. Files NOT to Change

- `internal/ops/env.go` — no changes needed, ops layer stays the same
- `internal/ops/progress.go` — PollProcess already exists and works

---

## Test Plan

### Unit tests (ops layer)
No new tests needed — ops layer doesn't change.

### Tool tests (`internal/tools/`)
Update existing env and scale tool tests to verify:
1. The tool handler calls PollProcess after the operation
2. The result contains final (FINISHED) status, not PENDING
3. Progress callback is invoked when progressToken is provided

### Integration tests
Update any integration flows that use zerops_env or zerops_scale to expect final status in response.

### E2E tests
Add permanent E2E tests:
1. `TestEnvSetPollsToCompletion` — set env var via MCP tool, verify response has FINISHED status
2. `TestScalePollsToCompletion` — scale via MCP tool, verify response has FINISHED status
3. Both should clean up after themselves

---

## Risk Assessment

- **Low risk**: Both operations complete in ~2 seconds, well within the default 10-minute PollProcess timeout
- **No behavior change in ops layer**: Only the tool handler (MCP layer) changes
- **Backward compatible**: The JSON response still includes the process object, just with terminal status instead of PENDING
- **Edge case**: If env/scale process takes longer than expected, PollProcess handles timeout gracefully (returns original process)

---

## Estimated Scope

- ~30 lines changed in tools/env.go (polling + description)
- ~15 lines changed in tools/scale.go (polling + description)
- ~5 lines changed in tools/process.go (description only)
- ~15 lines added to tools/manage.go (reload case + description)
- ~15 lines added to platform/ (ReloadService: client + impl + mock)
- ~5 lines added to ops/manage.go (Reload function)
- ~80 lines of new tool tests (env polling, scale polling, reload)
- ~100 lines of new E2E tests
- Total: ~265 lines
