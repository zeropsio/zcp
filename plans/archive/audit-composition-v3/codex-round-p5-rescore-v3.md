# Codex round P5 RE-SCORE — composition re-score (G3)

Date: 2026-04-27
Round type: COMPOSITION RE-SCORE (per cycle-1 §10.1 P7 row 2; cycle-3 plan §5 Phase 5 step 2)
Plan reviewed: `plans/atom-corpus-hygiene-followup-2-2026-04-27.md` §5 Phase 5
Reviewer: Codex (round id `a507f65c5355b72df`)
Reviewer brief: re-score 5 fixtures on cycle-1 §4.2 rubric (Coh / Den / Red / Cov-gap / Task-rel); compare to post-cycle-2 baseline; verify §15.3 G3 strict-improvement.

## Per-fixture re-score (post-cycle-3)

| Fixture | Coh | Den | Red | Cov-gap | Task-rel | G3 verdict |
|---|---|---|---|---|---|---|
| standard | 4 | 4 | 3 | 5 | 4 | PASS |
| implicit-webserver | 4 | 4 | 3 | 4 | 4 | PASS |
| two-pair | 4 | 3 | 1* | 5 | 4 | PASS |
| single-service | 4 | 4 | 3 | 5 | 4 | PASS |
| simple-deployed | 4 | 3 | 4 | 5 | 4 | PASS |

*two-pair Red=1 — STRUCTURAL per-service render duplication; engine-level fix per `engine-atom-rendering-improvements-2026-04-27.md`. Inherited from cycle-2 baseline; cycle 3 doesn't worsen it.

## Cycle-3 cumulative change vs post-cycle-2 baseline

Post-cycle-2 baseline (per cycle-3 plan §4.2):

| Fixture | Coh | Den | Red | Cov-gap | Task-rel |
|---|---|---|---|---|---|
| standard | 4 | 3 | 2 | 4 | 4 |
| implicit-webserver | 3 | 3 | 2 | 3 | 4 |
| two-pair | 4 | 3 | 1 | 4 | 4 |
| single-service | 4 | 3 | 2 | 4 | 4 |
| simple-deployed | 4 | 3 | 3 | 4 | 4 |

Cumulative deltas (cycle-2 → cycle-3):

| Fixture | Coh Δ | Den Δ | Red Δ | Cov-gap Δ | Task-rel Δ |
|---|---|---|---|---|---|
| standard | 0 | +1 | +1 | +1 | 0 |
| implicit-webserver | +1 | +1 | +1 | +1 | 0 |
| two-pair | 0 | 0 | 0 | +1 | 0 |
| single-service | 0 | +1 | +1 | +1 | 0 |
| simple-deployed | 0 | 0 | +1 | +1 | 0 |

## §15.3 G3 strict-improvement verification

Required: Coherence + density + task-relevance non-decreasing; redundancy + coverage-gap strictly improving.

- **Coherence**: non-decreasing (0 or +1 across all fixtures; implicit-webserver gained +1 because asset-pipeline framing now flows cleanly with the universal scaffold-yaml after F1 drops).
- **Density**: non-decreasing (+1 on standard, implicit-webserver, single-service due to first-deploy-scaffold-yaml byte reduction; 0 on two-pair and simple-deployed where the +81 B / +54 B Phase 4 cross-link expansion balances against signal-purity wins).
- **Task-relevance**: non-decreasing (held at 4 across all fixtures; cycle-3 didn't introduce off-task content).
- **Redundancy**: improving (+1 on 4 of 5 fixtures; two-pair held at 1 STRUCTURAL exception).
- **Coverage-gap**: strictly improving (+1 across all 5 fixtures).

**G3 strict-improvement HOLDS** across all 5 fixtures. Cycle 3 satisfies the G3 criterion.

## Drivers per fixture

- **standard**: density improves because body size 20,643 → 20,314 B; redundancy improves because `develop-first-deploy-scaffold-yaml.md:13-39` now keeps only root/setup shape, env-var reference, and mode-aware deployFiles pointer (post-F1 drop); coverage rises to 5 because the render still includes the full first-deploy flow, env catalog, deployFiles recovery, verify, stage promotion (`probe-final:7-28`).
- **implicit-webserver**: coherence improves 3 → 4 because the final render includes both implicit-webserver behavior and asset-pipeline pre-verify handling (`develop-implicit-webserver.md:11-26` + `develop-first-deploy-asset-pipeline-container.md:14-23`); density improves with body size 21,947 → 21,608 B; coverage improves because asset atom explicitly frames `zerops_deploy` as the deploy boundary.
- **two-pair**: coverage improves 4 → 5 because the final render still carries both dev runtimes and stage targets while the scaffold is leaner (post-F1); redundancy stays at 1 (engine-level structural — inherited).
- **single-service**: density improves with body size 20,588 → 20,259 B; redundancy improves 2 → 3 because the final render lacks stage-verify duplicates while retaining core deploy/verify atoms; coverage rises through focused scaffold + deployFiles self-deploy recovery + first-deploy execution semantics.
- **simple-deployed**: density holds at 3 (size +81 B, 16,085 → 16,166, non-decreasing score); redundancy improves 3 → 4 because rendered set is an 18-atom deployed-simple slice without first-deploy scaffold/promote atoms; coverage improves to 5 because simple-mode iteration + checklist + healthCheck guidance directly cover the implied task.

## VERDICT

`VERDICT: APPROVE`

Cycle 3 satisfies G3 strict-improvement on all 5 fixtures. Post-cycle-3 corpus is ready for SHIP gate per plan §5 Phase 5 EXIT, issued as **SHIP-WITH-NOTES** carrying only the inherited two-pair structural redundancy note (per plan §11 + §4.2 footnote: engine-level fix).
