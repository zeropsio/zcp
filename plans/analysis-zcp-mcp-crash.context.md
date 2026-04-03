# Context: analysis-zcp-mcp-crash
**Last updated**: 2026-03-27
**Iterations**: 1
**Task type**: codebase-analysis

## Decision Log
| # | Decision | Evidence | Iteration | Rationale |
|---|----------|----------|-----------|-----------|
| D1 | Root cause is keepalive timeout, not crash/panic | go-sdk `mcp/shared.go:584-602`, SSH: both processes alive | 1 | No crash logs, no OOM, no segfault. Session closes gracefully via `session.Close()` |
| D2 | System-level starvation (not Go scheduler) causes timeout | vmstat r=45, FUSE threads are OS-level not goroutines | 1 | Adversarial corrected primary's Go scheduler attribution |

## Rejected Alternatives
| # | Alternative | Evidence Against | Iteration | Why Rejected |
|---|-------------|-----------------|-----------|--------------|
| A1 | Auto-update restart | `once.go:37-39`: version="dev" skips update | 1 | Dev builds never trigger auto-update |
| A2 | OOM kill | No OOM entries in dmesg/syslog/kern.log | 1 | Container has swap + autoscaling |
| A3 | Goroutine panic | Both ZCP processes alive, no crash logs | 1 | Possible but no evidence of occurrence |
| A4 | .zcp/state/ corruption | `registry_unix.go` has flock, `session.go:126` has atomic rename | 1 | File locking is properly implemented |

## Resolved Concerns
| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|------------|
| C1 | Go scheduler starvation from FUSE threads | FUSE threads are OS-level, not goroutines | 1 | 1 | Corrected to "system-level starvation" — same conclusion, different mechanism |

## Open Questions (Unverified)
- Does Claude Code auto-restart MCP servers after disconnect? Or does the user need to manually restart?
- What is the exact GOMAXPROCS value inside the zcpx cgroup? (Go 1.25 reads cgroup quota)
- Could `KeepAlive: 0` cause issues with Claude Code's MCP client expecting pings?

## Confidence Map
| Section/Area | Confidence | Evidence Basis |
|-------------|-----------|----------------|
| Keepalive timeout mechanism | VERIFIED | go-sdk source code |
| System load as contributing factor | VERIFIED | SSH vmstat, top, load avg |
| Screen cycling (not crash) | VERIFIED | Process tree, session files |
| deploy_local.go panic risk | VERIFIED (code), UNVERIFIED (occurrence) | Code inspection only |
| Remediation effectiveness | LOGICAL | Increasing timeout addresses root cause |
