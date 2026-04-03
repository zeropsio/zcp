# Analysis: Self-Delete Guard for ZCP Service
**Date**: 2026-04-03
**Task**: Add a guard condition so that the ZCP service (where MCP is running) cannot be deleted via MCP — deletion of the self-service must be done manually. The delete tool should reject attempts to delete the service ZCP is running on.
**Task type**: implementation-planning
**Reference files**:
- `internal/tools/delete.go` — MCP tool handler for zerops_delete (59 lines)
- `internal/tools/delete_test.go` — Tests for delete tool (235 lines)
- `internal/ops/delete.go` — Business logic for service deletion (36 lines)
- `internal/ops/delete_test.go` — Tests for delete ops (86 lines)
- `internal/runtime/runtime.go` — Container detection, provides ServiceName/ServiceID (29 lines)
- `internal/server/server.go` — Server wiring, passes rtInfo to tool registration (123 lines)
- `internal/platform/errors.go` — Error codes (119 lines)

## Key Observations
- `runtime.Info` already carries `ServiceName` and `ServiceID` when running in a Zerops container
- `RegisterDelete` currently does NOT receive `runtime.Info`
- The guard should be at the ops layer (ops.Delete) to prevent bypass
- Need a new error code (e.g., `ErrSelfDeleteBlocked`)
- When running locally (not in container), `runtime.Info.InContainer` is false — no guard needed
- The guard must compare `hostname` parameter against `runtime.Info.ServiceName`
