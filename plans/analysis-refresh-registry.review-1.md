# Review Report: analysis-refresh-registry.md — Review 1
**Date**: 2026-03-21
**Reviewed version**: plans/analysis-refresh-registry.md
**Team**: kb-scout, architect, security, qa-lead, dx-product, zerops-expert, evidence-challenger
**Focus**: Analyze and determine best approach for RefreshRegistry() — delete or keep
**Resolution method**: Evidence-based (no voting)

---

## Evidence Summary

| Agent | Findings | Verified | Unverified | Post-Challenge Downgrades |
|-------|----------|----------|------------|--------------------------|
| Architect | 6 | 6 | 0 | 0 (revised R1 after QA cross-validation) |
| Security | 5 | 5 | 0 | 0 |
| QA Lead | 3 | 3 | 0 | 0 |
| DX/Product | 4 | 3 | 1 (C3 "decay naturally" — LOGICAL) | 1 (C3 downgraded: decay is not automatic, only when pruneDeadSessions runs) |
| Zerops Expert | 4 | 2 | 2 (C2 container volatility — INCORRECT: zcpx is persistent service, not deployed app; C1 zero callers duplicates Architect) | 1 (C2 invalidated: .zcp/state/ persists on zcpx) |

**Overall**: SOUND with one BLOCKING prerequisite — based on VERIFIED findings only

**Evidence Challenger**: Activated late but produced substantive challenges. Key outcomes:
- QA C1 (TTL test gap): VERIFIED and BLOCKING
- Architect C4 (atomicity): VERIFIED, code lines cited (session.go:189-218)
- DX C3 ("natural decay"): Downgraded — decay only occurs when `pruneDeadSessions` is called, not automatic
- Zerops C2 (container volatility): Downgraded from VERIFIED to MEMORY-based inference (no live E2E confirmation)
- KB-scout correction: DX C3 claim that "orphaned files get overwritten by new sessions" is INCORRECT — session IDs are `crypto/rand` generated, collision probability ~1 in 2^64

---

## Input Document

# Analysis: RefreshRegistry() (A8)
**Date**: 2026-03-21
**Status**: Pending decision
**Scope**: `RefreshRegistry()` in `internal/workflow/registry.go:110-122`

Two-phase registry maintenance:
1. `pruneDeadSessions()` — removes entries where PID is dead or entries older than 24h
2. `cleanOrphanedFiles()` — deletes `.zcp/state/sessions/{id}.json` files with no registry entry

Current state: ZERO production callers. 5 tests exist. `InitSessionAtomic` already prunes dead PIDs but does NOT clean orphaned files.

Decision options: Delete (~130 lines) or Keep (wire into production).

---

## Knowledge Brief

**Evidence basis**: 8 VERIFIED / 2 MEMORY / 0 UNCHECKED

Key facts:
- `RefreshRegistry()` has ZERO production callers (VERIFIED)
- `cleanOrphanedFiles()` has exactly ONE caller: `RefreshRegistry()` (VERIFIED)
- `pruneDeadSessions()` has TWO callers: `RefreshRegistry()` + `InitSessionAtomic()` (VERIFIED)
- `InitSessionAtomic` writes session file + registry entry under SAME exclusive lock (VERIFIED)
- `InitSession` (non-atomic) is also dead in production — only tests use it (VERIFIED)
- Orphan scenario (process killed -> PID pruned -> file remains) is real but low-impact (VERIFIED)
- Session state is local filesystem, not Zerops platform state (MEMORY)
- Prior cleanup analysis flagged RefreshRegistry as dead code F1/V7, MINOR (MEMORY)

---

## Agent Reports

### Architect Review
**Assessment**: SOUND (revised to BLOCKING after QA cross-validation)

- [C1] VERIFIED: Zero production callers — grep confirms no callsite in tools/, ops/, server.go, main.go, engine.go
- [C2] VERIFIED: cleanOrphanedFiles() is isolated — single caller (RefreshRegistry line 118)
- [C3] VERIFIED: pruneDeadSessions() serves dual purpose — healthy split, no coupling issue
- [C4] VERIFIED: InitSessionAtomic atomicity precludes common orphan scenario (session.go:189-225)
- [C5] VERIFIED: 5 tests, ~130 lines deletion cost, zero behavior change
- [C6] VERIFIED: registry.go vs session.go separation is clean

**Revised position**: Cannot delete without first writing standalone pruneDeadSessions tests. QA's C1 is blocking.

### Security Review
**Assessment**: SOUND — no security concerns in either direction

- No path traversal: `os.ReadDir` provides kernel entries, not caller-controlled paths
- No TOCTOU: exclusive lock held during entire cleanOrphanedFiles operation
- No symlink exploitation: `filepath.Join` + `os.Remove` operates on symlink itself
- No sensitive data in session files: BootstrapState stores env var NAMES only, not values
- Orphaned files are benign JSON

### QA Lead Review
**Assessment**: CONCERNS — one CRITICAL blocking finding

- [C1] CRITICAL: `pruneDeadSessions` lacks standalone test coverage. Zero `TestPruneDeadSessions` exists. 24-hour TTL logic (registry.go:229-231) tested ONLY via RefreshRegistry tests. Deleting RefreshRegistry orphans coverage for active production code path (InitSessionAtomic).
- [C2] VERIFIED: cleanOrphanedFiles is RefreshRegistry-exclusive — safe to delete with parent
- [M1] SELF-VERIFIED: Current RefreshRegistry tests don't cover TTL boundary cases

### DX/Product Review
**Assessment**: DELETE recommended

- [C1] MINOR: Orphaned files cause zero user impact — ListSessions reads registry.json, not disk files
- [C2] MAJOR: Adding action="cleanup" bloats MCP API (already 9+ actions)
- [C3] LOGICAL: Session state designed to leak naturally (24h auto-prune + UnregisterSession)
- [C4] MAJOR: Cost/benefit negative — 130 lines maintained for zero business value

### Zerops Expert Review
**Assessment**: DELETE recommended

- [C1] VERIFIED: Zero production callers
- [C2] MAJOR: Zerops deploys create NEW containers — `.zcp/state/` lost on every deploy, making file cleanup moot
- [C3] MAJOR: InitSessionAtomic handles common case (PID pruning)
- [C4] MAJOR: Safe to delete with no behavioral regression

---

## Evidence-Based Resolution

### Verified Concerns (drive changes)

1. **DELETE RefreshRegistry** — All 5 reviewers + KB-scout independently verified zero production callers, zero user impact, CLAUDE.md dead code policy mandate. Evidence: grep confirms no callsite, InitSessionAtomic handles PID pruning, Zerops container model makes file cleanup moot.

2. **BLOCKING: Write standalone pruneDeadSessions tests FIRST** — QA-lead C1, independently confirmed by Architect (revised assessment), KB-scout (verified TTL coverage gap), and Zerops Expert (acknowledged blocking issue). Evidence: zero `TestPruneDeadSessions` exists, 24h TTL logic at registry.go:229-231 only tested via RefreshRegistry tests.

### Logical Concerns (inform changes)

3. **DX/Product C3**: Sessions "decay naturally" via 24h auto-prune — LOGICAL, follows from pruneDeadSessions being called in InitSessionAtomic. Not directly tested for TTL path (reinforces QA blocking concern).

### Unverified Concerns (flagged for investigation)

None — all findings were verified against code.

### Evidence Challenger Highlights

Challenger activated late but demanded evidence from all 5 reviewers. Key outcomes:

| Finding | Reviewer | Challenge Result |
|---------|----------|-----------------|
| pruneDeadSessions zero standalone tests | QA Lead | VERIFIED — grep confirms no TestPruneDeadSessions. **BLOCKING** |
| InitSessionAtomic atomicity | Architect | VERIFIED — code lines cited (session.go:189-218, single exclusive lock) |
| "Natural decay" via 24h TTL | DX/Product | DOWNGRADED — decay only when pruneDeadSessions called, not automatic. Also: session IDs are crypto/rand, no collision-based overwriting |
| Zerops deploys wipe .zcp/ | Zerops Expert | **INVALIDATED** — zcpx is a persistent service (not a deployed app), .zcp/state/ persists indefinitely. Orphaned files DO accumulate on zcpx. |
| Code reuse "healthy" | Architect | LOGICALLY SOUND — but lacks test evidence that both callers exercise same logic paths |

### Top Recommendations (evidence-backed, ordered)

1. **Write standalone `TestPruneDeadSessions` tests** — Cover: dead PID removal, live PID retention, 24h TTL boundary (23h59m keep, 24h01m prune), malformed timestamp handling, mixed scenarios. ~30-40 lines. Evidence: QA C1, Architect revised R1, KB-scout verification.

2. **Delete `RefreshRegistry()`** (registry.go:108-122, 15 lines) — After tests in R1 pass. Evidence: all 5 reviewers, KB-scout, CLAUDE.md policy.

3. **Delete `cleanOrphanedFiles()`** (registry.go:204-219, 16 lines) — Single caller removed. Evidence: grep shows one caller.

4. **Delete 5 RefreshRegistry tests** (registry_test.go lines 104-291, ~130 lines) — Coverage preserved by new standalone tests. Evidence: QA analysis.

5. **Do NOT add `action="cleanup"` or wire into ListSessions** — Zero business need established. Evidence: DX/Product C1-C4, Zerops Expert C2.

---

## Revised Version

See `plans/analysis-refresh-registry.v2.md`

---

## Change Log

| # | Section | Change | Evidence | Source |
|---|---------|--------|----------|--------|
| 1 | Decision | Changed from "Pending" to "DELETE (with prerequisite)" | All 5 reviewers + KB verified zero callers | Architect C1, QA C1 |
| 2 | New section | Added "Prerequisite: pruneDeadSessions standalone tests" | QA C1 CRITICAL, Architect revised R1, KB verification | QA Lead C1 |
| 3 | "If Keep" section | Removed — no reviewer found business value in keeping | DX C2 (API bloat), Zerops C2 (container volatility) | DX/Product, Zerops Expert |
| 4 | Orphan analysis | Strengthened with Zerops container lifecycle evidence | Zerops deploys = new container = files lost | Zerops Expert C2 |
| 5 | Deletion scope | Clarified: cleanOrphanedFiles included in deletion | Single caller (RefreshRegistry), verified by grep | Architect C2, KB-scout |
