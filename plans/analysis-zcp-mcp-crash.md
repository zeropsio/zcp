# Analysis: ZCP MCP Crash Investigation on zcpx
**Date**: 2026-03-27
**Task**: Investigate why the ZCP MCP server crashes/becomes unresponsive on the zcpx Zerops service
**Task Type**: codebase-analysis
**Complexity**: Deep (ultrathink) — 4 agents

## Reference files:
- `cmd/zcp/main.go` — Entrypoint, signal handling, auto-update goroutine
- `internal/server/server.go` — MCP server setup, KeepAlive=30s, tool registration
- `internal/update/once.go` — Auto-update: check, apply, wait-for-idle, shutdown
- `internal/update/idle.go` — IdleWaiter: tracks active MCP requests via middleware

## Live Evidence (SSH into zcpx, 2026-03-27 06:25 UTC):

### System State
- **vCPU**: 1 (nproc=1)
- **RAM**: 1.5GB total, 702MB used, 145MB free, 259MB/512MB swap used
- **Load average**: 23.81, 25.31, 25.29 (on a SINGLE core)
- **CPU idle**: 93.3% — load is from FUSE/IO threads, not real CPU usage
- **vmstat runnable threads (r)**: 45, 25, 11 in 3 consecutive samples

### Process State
- **Two ZCP instances running simultaneously**:
  - PID 1547219 (Mar26, VmRSS=5576KB, 8 threads) — old, parent=claude(1547200), in DETACHED screen
  - PID 1687037 (06:21 today, VmRSS=16884KB, 8 threads) — new, in ATTACHED screen
- **8 SSHFS mounts**: evagb34d95, docs, testphp, testgo, kamaradka, term, adminer, adminerevo
- **probe-docs.sh**: Running since Mar23, curl probes every 330s with follow-up bursts on non-200
- **python3 → zcli**: PID 1009963/1009964, running indefinitely

### Claude Sessions
- Session 1547200: started 1774542769700 (Mar 26), DETACHED screen, still running
- Session 1687018: started 1774592469852 (Mar 27 06:21), ATTACHED screen, active

### Key Code Facts
- **No panic recovery** anywhere in ZCP codebase (grep for recover()/panic() returns 0 results)
- **KeepAlive: 30s** in server.go — MCP client pings every 30s
- **Auto-update version check**: Skipped for "dev" builds (once.go:37-39)
- **No OOM kills** in dmesg/syslog/kern.log
- **No crash dumps** or error logs found

## Hypotheses to Test:
1. **H1: SSHFS load + single vCPU → MCP keepalive timeout** — 8 SSHFS mounts create ~45 FUSE threads, inflating load avg to 25x on 1 core. Go runtime scheduler starvation → MCP can't respond to keepalive pings → Claude Code considers it dead.
2. **H2: Memory pressure + swap thrashing** — 259MB in swap, Go GC under pressure, allocation stalls.
3. **H3: Unrecovered goroutine panic** — No recover() anywhere; any panic in any goroutine = instant process death with no log.
4. **H4: Screen disconnect misinterpreted as crash** — User's terminal disconnects, starts new session, old ZCP orphaned.
5. **H5: Auto-update graceful restart** — Once() downloads new binary, shuts down server. (Ruled out: version="dev" skips this.)
