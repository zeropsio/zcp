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
| 3a | Phase 0 PRE-WORK Codex round 1 (test design + §6.3 fire-set generator + §4.2 baseline list review) | not run | NEEDS-REVISION (4/6 axes) | — | NEEDS-REVISION | round 1 found: substring-pin-detection self-counting (Axis 1+4); fire-set generator missing services / Trigger / strategy / export / closed-auto / multi-service / wrong step-set (Axis 5); fixture empty MustContain (Axis 6). Findings in `codex-round-p0-design-review.md`. |
| 3b | Plan revisions applying Codex round 1 findings | NEEDS-REVISION | revisions committed | — | N-A | §6.3 generator rewritten with corrected axes; §7 step 3 test sketch rewritten with AST-based pin detection in dedicated file; §7 step 4 fixture given non-empty MustContain. |
| 3c | Phase 0 PRE-WORK Codex round 2 (re-validate against revised plan) | not run | NEEDS-REVISION (1/6 axes — Axis 6 only) | — | NEEDS-REVISION | round 2 found: 5 of 6 axes resolved (Axes 1,2,3,4,5 all APPROVE; round 1's 7 recommendations: 6 of 7 RESOLVED). Single blocker: Axis 6 fixture MustContain pins were not verified for uniqueness — `zerops_workflow action="close"` does not appear in `develop-close-push-dev-simple` and `zerops_deploy` is non-unique. Findings in `codex-round-p0-design-review-round-2.md`. Codex's sandbox blocked artifact write; Claude reconstructed verbatim. |
| 3d | Plan revision 2 — replace fixture MustContain with grep-verified-unique phrases | NEEDS-REVISION | revisions committed | — | N-A | three new phrases verified appearing in EXACTLY ONE atom each: `Push-Dev Deploy Strategy — container` (deploy-container), `auto-starts with its \`healthCheck\`` (workflow-simple), `Simple-mode services auto-start on deploy` (close-push-dev-simple). |
| 3e | Phase 0 PRE-WORK Codex round 3 (Axis 6 verify-only re-validation) | not run | — | — | PENDING | round 3 scope is narrow: confirm the three new phrases each appear in EXACTLY ONE atom; confirm Axes 1-5 verdicts carry forward; APPROVE so Phase 0 substantive work may begin (§16.1). |
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
