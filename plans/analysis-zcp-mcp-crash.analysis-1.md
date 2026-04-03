# Analysis: ZCP MCP Crash on zcpx — Iteration 1
**Date**: 2026-03-27
**Scope**: `cmd/zcp/main.go`, `internal/server/server.go`, `internal/ops/deploy_local.go`, go-sdk v1.4.1 `mcp/shared.go`
**Agents**: kb (zerops-knowledge), primary (architecture+correctness), adversarial (challenge)
**Complexity**: Deep (ultrathink, 4 agents)
**Task**: Investigate why the ZCP MCP server crashes/becomes unresponsive on zcpx

## Summary

The ZCP MCP server is **not crashing** — it's being **disconnected by its own keepalive timeout**. The MCP go-sdk sends a ping every 30s and waits 15s for a response. On the zcpx container (1 vCPU, load avg 23-27 from 8 SSHFS/FUSE mounts), system-level CPU contention can delay the ping response beyond 15s, causing `session.Close()` — which silently terminates the MCP connection. The ZCP process stays alive but orphaned. Claude Code then starts a new session with a fresh ZCP instance.

Secondary finding: two goroutines in `deploy_local.go` lack panic recovery, creating a theoretical (but unobserved) crash vector.

## Findings by Severity

### Critical

| # | Finding | Evidence | Source |
|---|---------|----------|--------|
| F1 | MCP keepalive timeout (30s interval, 15s response timeout) closes session under system load. go-sdk `startKeepalive()` calls `session.Close()` on ping timeout. | go-sdk v1.4.1 `mcp/shared.go:584-602`: `pingCtx, pingCancel := context.WithTimeout(context.Background(), interval/2)` → 15s. On timeout: `session.Close()` | primary (VERIFIED), adversarial (CONFIRMED) |

### Major

| # | Finding | Evidence | Source |
|---|---------|----------|--------|
| F2 | Goroutines in `deploy_local.go:55-76` (PTY reader + cmd.Wait) lack `recover()`. Any panic kills the entire MCP server silently. | `internal/ops/deploy_local.go:55-76`: two `go func()` without `defer recover()` | primary (VERIFIED), adversarial (CONFIRMED) |
| F3-revised | System-level CPU starvation (load avg 23-27 on 1 vCPU) delays ALL processes including Go runtime, making 15s keepalive timeout unreliable. Original F3 incorrectly attributed this to Go scheduler starvation from FUSE threads — FUSE threads are OS-level, not goroutines. | vmstat: r=45 runnable threads, 93% idle. nproc=1. SSHFS creates kernel-level FUSE threads, not Go goroutines. | primary (VERIFIED mechanism), adversarial (CORRECTED premise) |

### Minor

| # | Finding | Evidence | Source |
|---|---------|----------|--------|
| F4 | Two ZCP instances running simultaneously (old orphaned in detached screen). Not a crash — screen session cycling. | SSH: PID 1547219 (Mar26, 5.5MB RSS) + PID 1687037 (06:21, 16.5MB RSS). Both alive, 5 FDs each. | primary (VERIFIED) |
| F5 | .zcp/state/ concurrent access is SAFE — file locking via `syscall.Flock()` + atomic rename exists. | `internal/workflow/registry_unix.go`: exclusive flock. `session.go:126`: temp+fsync+rename. | adversarial (VERIFIED) |

## Recommendations (max 7, evidence-backed)

| # | Recommendation | Priority | Evidence | Effort |
|---|---------------|----------|----------|--------|
| R1 | Increase `KeepAlive` from `30s` to `120s` or disable (`0`) in `server.go:53`. STDIO transport detects disconnect via pipe closure — explicit pings are redundant. | CRITICAL | go-sdk `startKeepalive()` at `mcp/shared.go:584`: 15s timeout too aggressive for loaded containers. One-line change. | 1 min |
| R2 | Add `defer func() { if r := recover(); r != nil { ... } }()` to both goroutines in `deploy_local.go:55-76`. Log panic + signal error channel instead of crashing. | MAJOR | `deploy_local.go:55,74`: unrecovered goroutines performing blocking I/O. | 15 min |
| R3 | Clean up zcpx: kill orphaned ZCP (PID 1547219), detached screen (1287788), probe-docs.sh (767739), python3/zcli (1009963/1009964). Unmount unused SSHFS mounts. | MAJOR | 8 SSHFS mounts + background processes inflate load avg to 25x on 1 vCPU. | 5 min |
| R4 | Document KeepAlive behavior in CLAUDE.md Architecture section. | MINOR | Undocumented setting with critical operational impact. | 5 min |

## Evidence Map

| Finding | Confidence | Basis |
|---------|-----------|-------|
| F1 (keepalive timeout) | VERIFIED | go-sdk source code `mcp/shared.go:584-602`, live SSH evidence |
| F2 (panic recovery) | VERIFIED | `deploy_local.go:55-76` — code inspection |
| F3-revised (system starvation) | LOGICAL | vmstat r=45, nproc=1, 93% idle → IO-bound load. Adversarial corrected: FUSE threads ≠ goroutines |
| F4 (screen cycling) | VERIFIED | SSH process tree, Claude session files |
| F5 (file locking safe) | VERIFIED | `registry_unix.go` flock, `session.go:126` atomic rename |

## Adversarial Challenges

### Challenged
| # | Original | Challenge | Resolution |
|---|----------|-----------|------------|
| CH2 | F3: "45 runnable threads → Go scheduler starvation" | FUSE threads are OS-level, not Go goroutines. Go M:N scheduler is independent. | **ACCEPTED**: Premise corrected. System-level starvation (OS scheduler) is the real mechanism, not Go scheduler starvation. F1 conclusion unchanged. |

### Confirmed
| # | Finding | Why it survived |
|---|---------|----------------|
| F1 | KeepAlive timeout closes session | Verified in go-sdk source: `context.WithTimeout(ctx, interval/2)` → `session.Close()` |
| F2 | deploy_local.go goroutines lack recovery | Code inspection confirmed, no `recover()` found |
| F4 | Screen cycling, not crash | Both processes alive, session files intact |

## Root Cause Chain

```
8 SSHFS mounts on 1 vCPU container
    → 45 FUSE kernel threads competing for CPU
    → load avg 23-27 (IO-bound, 93% idle)
    → OS scheduler delays ALL processes (including Go runtime)
    → MCP keepalive ping response delayed >15s
    → go-sdk startKeepalive() calls session.Close()
    → MCP connection terminated (ZCP process stays alive but orphaned)
    → Claude Code starts new session → new ZCP instance
    → User sees "MCP was down"
```

**NOT a crash. NOT a panic. A keepalive timeout under system load.**
