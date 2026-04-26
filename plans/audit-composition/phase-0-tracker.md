# Phase 0 tracker — Calibration

Started: 2026-04-26
Closed: open

> Phase contract per `plans/atom-corpus-hygiene-2026-04-26.md` §7 Phase 0 +
> §15.1 schema. Every row's "final state" is non-empty before phase EXIT.
> Every row that took action cites its commit hash. Every row whose
> action required a Codex round cites the round outcome.

## Work units (§7 Phase 0 WORK-SCOPE)

| # | work unit | initial state | final state | commit | codex round | notes |
|---|---|---|---|---|---|---|
| 1 | tracker-dir + this tracker | absent | scaffolded | — | N-A | scaffold commit (no Codex needed) |
| 2 | empirical 68-atom unpinned list (§4.2 derivation) | derived ad-hoc | committed as `unpinned-atoms-baseline.md` | — | N-A | source of truth for `knownUnpinnedAtoms` |
| 3 | Phase 0 PRE-WORK Codex round (test design + §6.3 fire-set generator + §4.2 baseline list review) | not run | — | — | PENDING | per §10.1 P0 row 1 (CORPUS-SCAN: pin-coverage gap derivation review) |
| 4 | recreate `cmd/atomsize_probe/main.go` from `c8d87406` + add `develop_simple_deployed_container` fixture | absent | — | — | N-A | mechanical: git show + adjust |
| 5 | build `cmd/atom_fire_audit/main.go` per §6.3 sketch | absent | — | — | N-A | implementation; output committed as `fire-set-matrix.md` |
| 6 | add `develop_simple_deployed_container` fixture to `coverageFixtures()` if absent | absent | — | — | N-A | per §4.4 + Phase 0 step 4 |
| 7 | add `TestCorpusCoverage_PinDensity` + `knownUnpinnedAtoms` allowlist + `TestCorpusCoverage_PinDensity_StillUnpinned` | absent | — | — | N-A | new test pair; 68-entry allowlist; ratchet-shrink-only |
| 8 | run §6.2 composition audit on 5 fixtures (4 first-deploy + simple-deployed) | not run | — | — | N-A | renders + reads + scores |
| 9 | Phase 0 CORPUS-SCAN #2: Codex composition baseline scoring (5 fixtures × 5 dimensions = 25 scores) | not run | — | — | PENDING | per §10.1 P0 row 2 |
| 10 | Phase 0 POST-WORK Codex round: walk `ComputeEnvelope` for fire-set=∅ atoms; confirm or reject DEAD | not run | — | — | PENDING | per §10.1 P0 row 3; output → Phase 1 tracker `candidate state` |
| 11 | commit `plans/audit-composition/baseline-scores.md` (executor scores + Codex scores per L4) | absent | — | — | N-A | composition baseline frozen for Phase 7 comparison |
| 12 | commit `plans/audit-composition/fire-set-matrix.md` (full per-atom fire-set table) | absent | — | — | N-A | source of truth for Phase 1 dead-atom sweep |

## Phase 0 EXIT (§7)

- [ ] Both probes built + run; output committed to `plans/audit-composition/`.
- [ ] `TestCorpusCoverage_PinDensity` exists; `knownUnpinnedAtoms` populated to current 68-atom state.
- [ ] Baseline composition scores committed.
- [ ] Full test suite green; no assertion semantics changed (only scaffolding + Logf + allowlist + docs).

## §15.2 EXIT enforcement

- [ ] Every row above has non-empty final state.
- [ ] Every row that took action cites a commit hash.
- [ ] Every row whose phase required a Codex round cites the round outcome.
- [ ] `Closed:` date filled in.

Phase 1 may not enter until all four above are checked.
