# Analysis: RefreshRegistry() (A8)

**Date**: 2026-03-21
**Status**: Pending decision
**Scope**: `RefreshRegistry()` in `internal/workflow/registry.go:110-122`

---

## What It Does

Two-phase registry maintenance:

1. **`pruneDeadSessions()`** — removes entries where PID is dead (via `syscall.Kill(pid, 0)`) or entries older than 24h
2. **`cleanOrphanedFiles()`** — deletes `.zcp/state/sessions/{id}.json` files that have no corresponding registry entry

```go
func RefreshRegistry(stateDir string) error {
    return withRegistryLock(stateDir, func(reg *Registry) (*Registry, error) {
        reg.Sessions = pruneDeadSessions(reg.Sessions)
        liveIDs := make(map[string]bool, len(reg.Sessions))
        for _, s := range reg.Sessions {
            liveIDs[s.SessionID] = true
        }
        cleanOrphanedFiles(stateDir, liveIDs)
        return reg, nil
    })
}
```

---

## Current State

### Zero Production Callers

- Not called from any tool handler, engine method, CLI command, or server code
- 5 tests in `registry_test.go` (lines 119-312)

### History

- **Added**: commit `6e0409f` (Mar 7, 2026) — "feat: add session registry for multi-process workflow coordination"
- **Originally called**: in `Engine.Start()` as initialization step
- **Removed from Engine.Start()**: during workflow evolution refactor (commit `f96a769`)
- **Replaced by**: inline pruning in `InitSessionAtomic()`

---

## Comparison with InitSessionAtomic

| Feature | InitSessionAtomic | RefreshRegistry |
|---------|-------------------|-----------------|
| Prunes dead PIDs | YES (line 191) | YES |
| Prunes >24h entries | YES (via pruneDeadSessions) | YES |
| Cleans orphaned files | **NO** | YES |
| Called in prod | YES (every session start) | NO |
| Exclusive lock | YES | YES |

**Key difference**: `InitSessionAtomic` does NOT clean orphaned session files. `RefreshRegistry` does both pruning AND file cleanup.

### When Do Orphans Exist?

Orphaned session files occur when:
1. Process crashes AFTER writing session file but BEFORE registering in registry — extremely rare (atomic write + register in same lock)
2. Process crashes AFTER registering but session file survives while registry entry is pruned later
3. Manual file creation (debugging, testing)

In practice, orphans are extremely rare because `InitSessionAtomic` writes both atomically under the same lock.

---

## Decision: Delete or Keep?

### If Delete (recommended if orphan cleanup is unnecessary)

Remove:
- `RefreshRegistry()` function (13 lines)
- 5 tests in `registry_test.go` (~100 lines):
  - `TestRefreshRegistry_PrunesDeadPIDs`
  - `TestRefreshRegistry_KeepsLivePIDs`
  - `TestRefreshRegistry_NoRegistryFile`
  - `TestRefreshRegistry_CleansOrphanedSessionFiles`
  - `TestRefreshRegistry_KeepsLiveSessionFiles`
- Check if `cleanOrphanedFiles()` has other callers — if not, delete it too (~15 lines)

Impact: ~130 lines deleted. Zero behavior change (function never called in prod).

### If Keep (if explicit maintenance is wanted)

Wire into production — two options:
1. **Add `action="cleanup"` to zerops_workflow** — explicit maintenance command
2. **Call in `action="list"`** — clean up before listing (like `git gc` on fetch)
3. **Call in `Engine.Start()`** — restore original design (clean on every session start)

Option 2 is cheapest: add `RefreshRegistry(stateDir)` before `ListSessions` in the list handler.

### Key Question

Is orphan file cleanup worth maintaining? Given that:
- `InitSessionAtomic` handles the common case (dead PID pruning)
- Orphan files waste disk space but don't affect correctness
- The state directory is under `.zcp/` which can be safely rm -rf'd
- No user has ever reported orphan file issues

If the answer is "not worth it", delete. If the answer is "defense-in-depth", wire option 2 (~3 lines of prod code).
