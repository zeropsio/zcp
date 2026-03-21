# Analysis: RefreshRegistry() (A8)

**Date**: 2026-03-21
**Status**: DELETE (with prerequisite)
**Scope**: `RefreshRegistry()` in `internal/workflow/registry.go:110-122`
**Review**: review-1 (2026-03-21) — 5 reviewers unanimous on DELETE

---

## Decision: DELETE

All 5 independent reviewers + knowledge scout confirmed deletion is the correct approach. Evidence:

1. **Zero production callers** — not called from any tool handler, engine method, CLI command, or server code [VERIFIED: grep]
2. **CLAUDE.md mandate** — "Remove, don't disable. When a feature is no longer needed, delete it entirely" [VERIFIED: CLAUDE.md]
3. **Orphan cleanup solves a non-problem** — `InitSessionAtomic` writes session file + registry entry under same exclusive lock (session.go:189-225), making orphans near-impossible [VERIFIED: code inspection]
4. **~~Zerops container model makes file cleanup moot~~** — INCORRECT: zcpx is a persistent service, NOT a deployed app. `.zcp/state/` persists indefinitely. Orphaned files DO accumulate but are small (~1-2 KB) and harmless (never read by production code)
5. **No user-facing value** — orphaned files never read by ListSessions, adding `action="cleanup"` would bloat MCP API [VERIFIED: code + API review]

---

## Prerequisite: Standalone pruneDeadSessions Tests

**BLOCKING** — must complete before deletion.

`pruneDeadSessions()` (registry.go:222-235) has TWO callers:
- `RefreshRegistry()` (line 112) — DEAD, being deleted
- `InitSessionAtomic()` (session.go:191) — ACTIVE production code

Currently tested ONLY via RefreshRegistry tests. The 24-hour TTL logic (line 229-231) has zero standalone coverage.

### Required Tests (RED phase)

```go
func TestPruneDeadSessions(t *testing.T) {
    t.Parallel()
    tests := []struct {
        name     string
        sessions []SessionEntry
        wantLen  int
        wantIDs  []string
    }{
        // Dead PID removed regardless of age
        // Live PID + fresh session kept
        // Live PID + 24h01m old session removed (TTL boundary)
        // Live PID + 23h59m old session kept (TTL boundary)
        // Mixed: dead PID + old + young sessions
        // Malformed CreatedAt: session kept (parse error = keep)
        // Empty CreatedAt: session kept
    }
    // ...
}
```

### Implementation Sequence

1. **RED**: Write `TestPruneDeadSessions` in `registry_test.go` — table-driven, covers TTL boundary + dead PID + mixed scenarios (~30-40 lines)
2. **GREEN**: Verify tests pass (pruneDeadSessions already exists)
3. **DELETE**: Remove `RefreshRegistry()` (15 lines) + `cleanOrphanedFiles()` (16 lines) + 5 RefreshRegistry tests (~100 lines)
4. **VERIFY**: `go test ./internal/workflow/... -count=1` passes

**Net result**: ~130 lines deleted, ~35 lines added = ~95 lines net reduction. Zero behavior change. Full test coverage preserved.

---

## What Gets Deleted

| Item | Location | Lines |
|------|----------|-------|
| `RefreshRegistry()` | registry.go:108-122 | 15 |
| `cleanOrphanedFiles()` | registry.go:204-219 | 16 |
| `TestRefreshRegistry_PrunesDeadPIDs` | registry_test.go:104-130 | 27 |
| `TestRefreshRegistry_KeepsLivePIDs` | registry_test.go:132-157 | 26 |
| `TestRefreshRegistry_NoRegistryFile` | registry_test.go:225-233 | 9 |
| `TestRefreshRegistry_CleansOrphanedSessionFiles` | registry_test.go:235-256 | 22 |
| `TestRefreshRegistry_KeepsLiveSessionFiles` | registry_test.go:258-291 | 34 |
| **Total removed** | | **~149** |
| **New tests added** | `TestPruneDeadSessions` | **~35** |
| **Net reduction** | | **~114** |

## What Survives

- `pruneDeadSessions()` — active caller in `InitSessionAtomic`, now with standalone tests
- All other registry functions: `RegisterSession`, `UnregisterSession`, `ListSessions`, `ClassifySessions`
- All registry infrastructure: `withRegistryLock`, `readRegistry`, `writeRegistry`, locking primitives
