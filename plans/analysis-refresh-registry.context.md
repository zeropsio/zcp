# Review Context: analysis-refresh-registry
**Last updated**: 2026-03-21
**Reviews completed**: 1
**Resolution method**: Evidence-based

## Decision Log
| # | Decision | Evidence | Review | Rationale |
|---|----------|----------|--------|-----------|
| 1 | DELETE RefreshRegistry + cleanOrphanedFiles | Zero production callers (grep), CLAUDE.md policy, all 5 reviewers unanimous | review-1 | Dead code: never called, solves non-problem, Zerops deploys make file cleanup moot |
| 2 | Write standalone pruneDeadSessions tests FIRST | QA C1: zero TestPruneDeadSessions exists, 24h TTL logic untested | review-1 | TDD mandate: cannot delete without preserving coverage for active production caller |
| 3 | Do NOT wire RefreshRegistry into production | DX C2 (API bloat), Zerops C2 (container volatility), Architect R2 (conditional, overridden) | review-1 | No business value established; all "keep" options add complexity for zero benefit |

## Rejected Alternatives
| # | Alternative | Evidence Against | Review | Why Rejected |
|---|------------|-----------------|--------|--------------|
| 1 | Add `action="cleanup"` to zerops_workflow | DX C2: MCP API already 9+ actions, zero user need | review-1 | Bloats API surface for non-problem |
| 2 | Call RefreshRegistry before ListSessions | Architect R2 (conditional): cleanest if kept, but no need to keep | review-1 | Orphans are harmless, rare, and lost on Zerops deploy anyway |
| 3 | Restore in Engine.Start() | Original design, removed during refactor | review-1 | Was already removed for good reason; InitSessionAtomic handles PID pruning |
| 4 | Delete without writing pruneDeadSessions tests | QA C1 CRITICAL: would orphan TTL coverage | review-1 | Violates TDD mandate — CLAUDE.md requires clean removal |

## Resolved Concerns
| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|------------|
| 1 | Path traversal in cleanOrphanedFiles | Security: os.ReadDir provides kernel entries, exclusive lock held | review-1 | review-1 | No vulnerability — safe to delete |
| 2 | Sensitive data in orphaned session files | Security: BootstrapState stores env var NAMES only | review-1 | review-1 | No secret exposure risk |
| 3 | Orphan accumulation on local dev | Zerops Expert: deploys create new containers; DX: files never read | review-1 | review-1 | Non-problem — harmless, small JSON files |

## Open Questions (Unverified)
None — all findings verified against code.

## Confidence Map
| Section/Area | Confidence | Evidence Basis |
|-------------|------------|---------------|
| Delete decision | HIGH | 5 reviewers + KB verified, zero callers confirmed by grep |
| pruneDeadSessions test gap | HIGH | QA verified, Architect + KB independently confirmed |
| Orphan rarity | HIGH | InitSessionAtomic atomicity verified in code (session.go:189-225) |
| Zerops container volatility | **INVALIDATED** | zcpx is persistent service, not deployed app — .zcp/state/ persists, orphans accumulate |
| Deletion scope (~149 lines) | HIGH | Line-by-line audit in review |
